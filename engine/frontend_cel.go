package engine

import (
	"fmt"
	"strconv"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/operators"
	exprpb "google.golang.org/genproto/googleapis/api/expr/v1alpha1"

	"github.com/example/rule-engine-demo/ir"
)

// CELFrontend uses Google CEL (github.com/google/cel-go) purely as a parser: it
// parses a CEL expression and converts the resulting AST into the project IR.
// CEL does NOT participate in evaluation — the bytecode VM does, exactly like
// the SQL and Expr front-ends.
//
//	Rule(CEL) ─env.Parse─▶ AST ─convert─▶ ir.Node ─vm.Compile─▶ ByteCode ─VM─▶ bool
//
// Supported subset (everything that maps cleanly onto the IR):
//
//	&&  ||                          logical AND / OR (nested, flattened)
//	==  !=  <  <=  >  >=            field <op> literal (operands may be swapped)
//	field in ["a","b", ...]         membership
//	field == null / field != null   IS NULL / IS NOT NULL
//	field.startsWith/endsWith/contains("x")   LIKE prefix/suffix/substring
//
// Anything outside this subset returns an error so a bad rule fails loudly at
// compile time rather than silently mis-evaluating.
type CELFrontend struct{}

// Name identifies the frontend.
func (CELFrontend) Name() string { return "cel" }

// celEnv is a parse-only CEL environment, created once and reused (Parse is
// safe for concurrent use). No type declarations are needed because evaluation
// happens in the VM, not in CEL.
var celEnv, _ = cel.NewEnv()

// Parse parses with cel-go then converts the AST to IR.
func (CELFrontend) Parse(rule string) (ir.Node, error) {
	ast, iss := celEnv.Parse(rule)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("cel parse: %w", iss.Err())
	}
	pe, err := cel.AstToParsedExpr(ast)
	if err != nil {
		return nil, fmt.Errorf("cel ast: %w", err)
	}
	return celToIR(pe.GetExpr())
}

// celToIR converts a CEL protobuf expression into the project IR.
func celToIR(e *exprpb.Expr) (ir.Node, error) {
	call := e.GetCallExpr()
	if call == nil {
		return nil, fmt.Errorf("cel: expected a boolean expression, got %T", e.GetExprKind())
	}

	fn := call.GetFunction()
	switch fn {
	case operators.LogicalAnd:
		return celLogic("AND", call.GetArgs())
	case operators.LogicalOr:
		return celLogic("OR", call.GetArgs())
	case operators.In, "in":
		return celIn(call.GetArgs())
	case operators.Equals, operators.NotEquals,
		operators.Less, operators.LessEquals,
		operators.Greater, operators.GreaterEquals:
		return celCompare(fn, call.GetArgs())
	case "startsWith", "endsWith", "contains":
		return celReceiverLike(fn, call)
	}
	return nil, fmt.Errorf("cel: unsupported function %q", fn)
}

// celLogic flattens same-operator children into one ir.Logic.
func celLogic(op string, args []*exprpb.Expr) (ir.Node, error) {
	out := make([]ir.Node, 0, len(args))
	for _, a := range args {
		child, err := celToIR(a)
		if err != nil {
			return nil, err
		}
		if lg, ok := child.(ir.Logic); ok && lg.Op == op {
			out = append(out, lg.Args...)
		} else {
			out = append(out, child)
		}
	}
	return ir.Logic{Op: op, Args: out}, nil
}

// celCompare builds an ir.Compare, or ir.IsNull when comparing against null.
func celCompare(fn string, args []*exprpb.Expr) (ir.Node, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("cel: comparison needs 2 operands")
	}
	left, right := args[0], args[1]

	// field == null / field != null  ->  IS (NOT) NULL
	if isCELNull(left) || isCELNull(right) {
		fieldNode := left
		if isCELNull(left) {
			fieldNode = right
		}
		field, err := celIdent(fieldNode)
		if err != nil {
			return nil, err
		}
		switch fn {
		case operators.Equals:
			return ir.IsNull{Field: field, Negate: false}, nil
		case operators.NotEquals:
			return ir.IsNull{Field: field, Negate: true}, nil
		}
		return nil, fmt.Errorf("cel: only == / != allowed against null")
	}

	op := celCmpOp(fn)
	if field, err := celIdent(left); err == nil {
		val, err := celValue(right)
		if err != nil {
			return nil, err
		}
		return ir.Compare{Field: field, Op: op, Val: val}, nil
	}
	// literal <op> field  ->  flip the operator
	field, err := celIdent(right)
	if err != nil {
		return nil, fmt.Errorf("cel: comparison needs exactly one field identifier")
	}
	val, err := celValue(left)
	if err != nil {
		return nil, err
	}
	return ir.Compare{Field: field, Op: flipOp(op), Val: val}, nil
}

// celIn builds ir.In from `field in ["a", ...]`.
func celIn(args []*exprpb.Expr) (ir.Node, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("cel: `in` needs 2 operands")
	}
	field, err := celIdent(args[0])
	if err != nil {
		return nil, err
	}
	list := args[1].GetListExpr()
	if list == nil {
		return nil, fmt.Errorf("cel: `in` right-hand side must be a list literal")
	}
	vals := make([]ir.Value, 0, len(list.GetElements()))
	for _, el := range list.GetElements() {
		v, err := celValue(el)
		if err != nil {
			return nil, err
		}
		vals = append(vals, v)
	}
	return ir.In{Field: field, Vals: vals}, nil
}

// celReceiverLike maps field.startsWith/endsWith/contains("x") to ir.Like.
func celReceiverLike(fn string, call *exprpb.Expr_Call) (ir.Node, error) {
	field, err := celIdent(call.GetTarget())
	if err != nil {
		return nil, err
	}
	args := call.GetArgs()
	if len(args) != 1 {
		return nil, fmt.Errorf("cel: %s expects 1 argument", fn)
	}
	lit, err := celValue(args[0])
	if err != nil {
		return nil, err
	}
	if !lit.IsString {
		return nil, fmt.Errorf("cel: %s pattern must be a string", fn)
	}
	switch fn {
	case "startsWith":
		return ir.Like{Field: field, Pattern: lit.Str + "%"}, nil
	case "endsWith":
		return ir.Like{Field: field, Pattern: "%" + lit.Str}, nil
	case "contains":
		return ir.Like{Field: field, Pattern: "%" + lit.Str + "%"}, nil
	}
	return nil, fmt.Errorf("cel: unsupported function %q", fn)
}

func celIdent(e *exprpb.Expr) (string, error) {
	if id := e.GetIdentExpr(); id != nil {
		return id.GetName(), nil
	}
	// Support a.b selection as a dotted field name.
	if sel := e.GetSelectExpr(); sel != nil {
		base, err := celIdent(sel.GetOperand())
		if err != nil {
			return "", err
		}
		return base + "." + sel.GetField(), nil
	}
	return "", fmt.Errorf("cel: expected field identifier")
}

func celValue(e *exprpb.Expr) (ir.Value, error) {
	c := e.GetConstExpr()
	if c == nil {
		return ir.Value{}, fmt.Errorf("cel: expected literal, got %T", e.GetExprKind())
	}
	switch k := c.GetConstantKind().(type) {
	case *exprpb.Constant_StringValue:
		return ir.Value{IsString: true, Str: k.StringValue}, nil
	case *exprpb.Constant_Int64Value:
		return ir.Value{Num: strconv.FormatInt(k.Int64Value, 10)}, nil
	case *exprpb.Constant_Uint64Value:
		return ir.Value{Num: strconv.FormatUint(k.Uint64Value, 10)}, nil
	case *exprpb.Constant_DoubleValue:
		return ir.Value{Num: strconv.FormatFloat(k.DoubleValue, 'g', -1, 64)}, nil
	}
	return ir.Value{}, fmt.Errorf("cel: unsupported literal kind")
}

func isCELNull(e *exprpb.Expr) bool {
	c := e.GetConstExpr()
	if c == nil {
		return false
	}
	_, ok := c.GetConstantKind().(*exprpb.Constant_NullValue)
	return ok
}

func celCmpOp(fn string) string {
	switch fn {
	case operators.Equals:
		return "="
	case operators.NotEquals:
		return "!="
	case operators.Less:
		return "<"
	case operators.LessEquals:
		return "<="
	case operators.Greater:
		return ">"
	case operators.GreaterEquals:
		return ">="
	}
	return fn
}
