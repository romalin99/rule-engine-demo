// Package expr is an expr-lang (github.com/expr-lang/expr) front-end: it parses
// an Expr boolean expression and lowers its AST into the unified IR.
//
// NOTE: requires `github.com/expr-lang/expr`. After adding this package run
// `go mod tidy` locally to resolve the dependency (it is not yet in go.sum).
//
// Supported shapes (all emit the same IR as the SQL/JSON front-ends):
//
//	a && b ...                         → ir.Logic{AND}
//	a || b ...                         → ir.Logic{OR}
//	field == v / != / < / <= / > / >=  → ir.Compare
//	field in [v1, v2, ...]             → ir.In
//	hasPrefix(field,"x") startsWith    → ir.Like "x%"
//	hasSuffix(field,"x") endsWith      → ir.Like "%x"
//	contains(field,"x")                → ir.Like "%x%"
//
// BETWEEN is expressed naturally as `field >= lo && field <= hi`.
package expr

import (
	"fmt"
	"strconv"

	exprast "github.com/expr-lang/expr/ast"
	exprparser "github.com/expr-lang/expr/parser"

	"tcg-rulex-engine/pkg/api"
	"tcg-rulex-engine/pkg/ir"
)

// Parser implements api.Parser using the expr-lang parser.
type Parser struct{}

// New returns an Expr front-end.
func New() *Parser { return &Parser{} }

// Name identifies the parser.
func (*Parser) Name() string { return "expr" }

// Parse parses an Expr expression and converts its AST to IR.
func (*Parser) Parse(rule string) (api.Program, error) {
	tree, err := exprparser.Parse(rule)
	if err != nil {
		return nil, fmt.Errorf("expr parse: %w", err)
	}
	return convert(tree.Node)
}

func convert(n exprast.Node) (ir.Node, error) {
	switch t := n.(type) {
	case *exprast.BinaryNode:
		switch t.Operator {
		case "&&", "and":
			return logic("AND", t.Left, t.Right)
		case "||", "or":
			return logic("OR", t.Left, t.Right)
		case "==", "!=", "<", "<=", ">", ">=":
			field, err := identName(t.Left)
			if err != nil {
				return nil, err
			}
			v, err := litVal(t.Right)
			if err != nil {
				return nil, err
			}
			op := t.Operator
			if op == "==" {
				op = "="
			}
			return ir.Compare{Field: field, Op: op, Val: v}, nil
		case "in", "not in":
			field, err := identName(t.Left)
			if err != nil {
				return nil, err
			}
			arr, ok := t.Right.(*exprast.ArrayNode)
			if !ok {
				return nil, fmt.Errorf("expr: `in` expects an array, got %T", t.Right)
			}
			vals := make([]ir.Value, 0, len(arr.Nodes))
			for _, e := range arr.Nodes {
				v, err := litVal(e)
				if err != nil {
					return nil, err
				}
				vals = append(vals, v)
			}
			return ir.In{Field: field, Vals: vals, Negate: t.Operator == "not in"}, nil
		}
		return nil, fmt.Errorf("expr: unsupported operator %q", t.Operator)

	case *exprast.UnaryNode: // !expr / not expr -> ir.Not (covers !contains(...), etc.)
		switch t.Operator {
		case "!", "not":
			arg, err := convert(t.Node)
			if err != nil {
				return nil, err
			}
			return ir.Not{Arg: arg}, nil
		}
		return nil, fmt.Errorf("expr: unsupported unary operator %q", t.Operator)

	case *exprast.CallNode:
		return callToIR(calleeName(t.Callee), t.Arguments)

	case *exprast.BuiltinNode:
		return callToIR(t.Name, t.Arguments)
	}
	return nil, fmt.Errorf("expr: unsupported node %T", n)
}

func logic(op string, l, r exprast.Node) (ir.Node, error) {
	ln, err := convert(l)
	if err != nil {
		return nil, err
	}
	rn, err := convert(r)
	if err != nil {
		return nil, err
	}
	return ir.Logic{Op: op, Args: []ir.Node{ln, rn}}, nil
}

// callToIR maps string helper calls to ir.Like patterns.
func callToIR(name string, args []exprast.Node) (ir.Node, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("expr: %s expects 2 args", name)
	}
	field, err := identName(args[0])
	if err != nil {
		return nil, err
	}
	lit, err := litVal(args[1])
	if err != nil {
		return nil, err
	}
	core := lit.Str
	switch name {
	case "hasPrefix", "startsWith":
		return ir.Like{Field: field, Pattern: core + "%"}, nil
	case "hasSuffix", "endsWith":
		return ir.Like{Field: field, Pattern: "%" + core}, nil
	case "contains":
		return ir.Like{Field: field, Pattern: "%" + core + "%"}, nil
	}
	return nil, fmt.Errorf("expr: unsupported call %q", name)
}

func calleeName(n exprast.Node) string {
	if id, ok := n.(*exprast.IdentifierNode); ok {
		return id.Value
	}
	return ""
}

func identName(n exprast.Node) (string, error) {
	if id, ok := n.(*exprast.IdentifierNode); ok {
		return id.Value, nil
	}
	return "", fmt.Errorf("expr: expected identifier, got %T", n)
}

func litVal(n exprast.Node) (ir.Value, error) {
	switch t := n.(type) {
	case *exprast.IntegerNode:
		return ir.Value{Num: strconv.Itoa(t.Value)}, nil
	case *exprast.FloatNode:
		return ir.Value{Num: strconv.FormatFloat(t.Value, 'f', -1, 64)}, nil
	case *exprast.StringNode:
		return ir.Value{IsString: true, Str: t.Value}, nil
	case *exprast.BoolNode:
		return ir.Value{IsString: true, Str: strconv.FormatBool(t.Value)}, nil
	}
	return ir.Value{}, fmt.Errorf("expr: unsupported literal %T", n)
}
