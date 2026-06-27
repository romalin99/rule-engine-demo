package engine

import (
	"fmt"
	"strconv"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"

	"github.com/example/rule-engine-demo/ir"
)

// ExprFrontend uses expr-lang/expr purely as a parser: it parses an Expr
// expression to expr's AST and converts that AST into the project IR. expr does
// NOT participate in evaluation — the bytecode VM does, exactly like the SQL and
// CEL front-ends.
//
//	Rule(Expr) ─parser.Parse─▶ ast.Node ─convert─▶ ir.Node ─vm.Compile─▶ ByteCode
//
// Supported subset (everything that maps cleanly onto the IR):
//
//	&&  ||                          logical AND / OR (nested, flattened)
//	==  !=  <  <=  >  >=            field <op> literal (operands may be swapped)
//	field in ["a","b", ...]         membership
//	field == nil / field != nil     IS NULL / IS NOT NULL
//	startsWith/endsWith/contains    LIKE prefix/suffix/substring
//
// Anything outside this subset returns an error so a bad rule fails loudly at
// compile time rather than silently mis-evaluating.
type ExprFrontend struct{}

// Name identifies the frontend.
func (ExprFrontend) Name() string { return "expr" }

// Parse parses with expr-lang then converts the AST to IR.
func (ExprFrontend) Parse(rule string) (ir.Node, error) {
	tree, err := parser.Parse(rule)
	if err != nil {
		return nil, fmt.Errorf("expr parse: %w", err)
	}
	return exprToIR(tree.Node)
}

// exprToIR converts an expr-lang AST node into the project IR.
func exprToIR(n ast.Node) (ir.Node, error) {
	switch t := n.(type) {

	case *ast.BinaryNode:
		switch t.Operator {
		case "&&", "and":
			return exprLogic("AND", t)
		case "||", "or":
			return exprLogic("OR", t)
		case "in":
			return exprIn(t.Left, t.Right)
		case "==", "!=", "<", "<=", ">", ">=":
			return exprCompare(t.Operator, t.Left, t.Right)
		}
		return nil, fmt.Errorf("expr: unsupported binary operator %q", t.Operator)

	case *ast.BuiltinNode:
		return exprBuiltin(t.Name, t.Arguments)

	case *ast.CallNode:
		if id, ok := t.Callee.(*ast.IdentifierNode); ok {
			return exprBuiltin(id.Value, t.Arguments)
		}
		return nil, fmt.Errorf("expr: unsupported call expression")

	case *ast.NilNode:
		return nil, fmt.Errorf("expr: bare nil is not a predicate")
	}

	return nil, fmt.Errorf("expr: unsupported node %T", n)
}

// exprLogic flattens a chain of same-operator binary nodes into one ir.Logic.
func exprLogic(op string, t *ast.BinaryNode) (ir.Node, error) {
	l, err := exprToIR(t.Left)
	if err != nil {
		return nil, err
	}
	r, err := exprToIR(t.Right)
	if err != nil {
		return nil, err
	}
	args := make([]ir.Node, 0, 2)
	args = appendLogic(args, op, l)
	args = appendLogic(args, op, r)
	return ir.Logic{Op: op, Args: args}, nil
}

// appendLogic merges a child Logic of the same operator into the parent's args
// (so `a && b && c` becomes a single 3-arg AND rather than nested 2-arg ANDs).
func appendLogic(args []ir.Node, op string, child ir.Node) []ir.Node {
	if lg, ok := child.(ir.Logic); ok && lg.Op == op {
		return append(args, lg.Args...)
	}
	return append(args, child)
}

// exprCompare builds an ir.Compare or, when comparing against nil, an ir.IsNull.
func exprCompare(op string, left, right ast.Node) (ir.Node, error) {
	// field == nil / field != nil  ->  IS (NOT) NULL
	if isNil(right) || isNil(left) {
		fieldNode := left
		if isNil(left) {
			fieldNode = right
		}
		field, err := exprIdent(fieldNode)
		if err != nil {
			return nil, err
		}
		switch op {
		case "==":
			return ir.IsNull{Field: field, Negate: false}, nil
		case "!=":
			return ir.IsNull{Field: field, Negate: true}, nil
		}
		return nil, fmt.Errorf("expr: only == / != allowed against nil")
	}

	// Normalise to `field <op> literal`, swapping operands if needed.
	field, errF := exprIdent(left)
	if errF == nil {
		val, err := exprValue(right)
		if err != nil {
			return nil, err
		}
		return ir.Compare{Field: field, Op: op, Val: val}, nil
	}
	// literal <op> field  ->  flip the operator
	field, errF2 := exprIdent(right)
	if errF2 != nil {
		return nil, fmt.Errorf("expr: comparison needs exactly one field identifier")
	}
	val, err := exprValue(left)
	if err != nil {
		return nil, err
	}
	return ir.Compare{Field: field, Op: flipOp(op), Val: val}, nil
}

// exprIn builds an ir.In from `field in [literal, ...]`.
func exprIn(left, right ast.Node) (ir.Node, error) {
	field, err := exprIdent(left)
	if err != nil {
		return nil, err
	}
	arr, ok := right.(*ast.ArrayNode)
	if !ok {
		return nil, fmt.Errorf("expr: `in` right-hand side must be an array literal")
	}
	vals := make([]ir.Value, 0, len(arr.Nodes))
	for _, e := range arr.Nodes {
		v, err := exprValue(e)
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	return ir.In{Field: field, Vals: vals}, nil
}

// exprBuiltin maps startsWith/endsWith/contains(field, "lit") to ir.Like.
func exprBuiltin(name string, args []ast.Node) (ir.Node, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("expr: %s expects 2 arguments", name)
	}
	field, err := exprIdent(args[0])
	if err != nil {
		return nil, err
	}
	lit, err := exprValue(args[1])
	if err != nil {
		return nil, err
	}
	if !lit.IsString {
		return nil, fmt.Errorf("expr: %s pattern must be a string", name)
	}
	switch name {
	case "startsWith", "hasPrefix":
		return ir.Like{Field: field, Pattern: lit.Str + "%"}, nil
	case "endsWith", "hasSuffix":
		return ir.Like{Field: field, Pattern: "%" + lit.Str}, nil
	case "contains":
		return ir.Like{Field: field, Pattern: "%" + lit.Str + "%"}, nil
	}
	return nil, fmt.Errorf("expr: unsupported function %q", name)
}

func exprIdent(n ast.Node) (string, error) {
	if id, ok := n.(*ast.IdentifierNode); ok {
		return id.Value, nil
	}
	return "", fmt.Errorf("expr: expected field identifier, got %T", n)
}

func exprValue(n ast.Node) (ir.Value, error) {
	switch t := n.(type) {
	case *ast.IntegerNode:
		return ir.Value{Num: strconv.Itoa(t.Value)}, nil
	case *ast.FloatNode:
		return ir.Value{Num: strconv.FormatFloat(t.Value, 'g', -1, 64)}, nil
	case *ast.StringNode:
		return ir.Value{IsString: true, Str: t.Value}, nil
	}
	return ir.Value{}, fmt.Errorf("expr: unsupported literal %T", n)
}

func isNil(n ast.Node) bool {
	_, ok := n.(*ast.NilNode)
	return ok
}

// flipOp reverses a comparison so `lit <op> field` becomes `field <flip> lit`.
func flipOp(op string) string {
	switch op {
	case "<":
		return ">"
	case "<=":
		return ">="
	case ">":
		return "<"
	case ">=":
		return "<="
	}
	return op // == and != are symmetric
}
