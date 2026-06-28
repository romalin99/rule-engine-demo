// Package cel is a Google CEL (github.com/google/cel-go) front-end: it parses a
// CEL boolean expression and lowers its AST into the unified IR.
//
// NOTE: requires `github.com/google/cel-go`. After adding this package run
// `go mod tidy` locally to resolve the dependency. This conversion targets the
// cel-go `common/ast` navigable API (v0.18+, NativeRep added ~v0.21); if your
// cel-go version differs, adjust the AST accessor names accordingly.
//
// Supported shapes:
//
//	a && b           → ir.Logic{AND}
//	a || b           → ir.Logic{OR}
//	field == v ...   → ir.Compare   (==, !=, <, <=, >, >=)
//	field in [..]    → ir.In
//	field.startsWith("x") / endsWith / contains → ir.Like
//
// BETWEEN is expressed naturally as `field >= lo && field <= hi`.
package cel

import (
	"fmt"
	"strconv"

	celgo "github.com/google/cel-go/cel"
	celast "github.com/google/cel-go/common/ast"
	celops "github.com/google/cel-go/common/operators"

	"tcg-rulex-engine/pkg/api"
	"tcg-rulex-engine/pkg/ir"
)

// Parser implements api.Parser using the CEL parser.
type Parser struct {
	env *celgo.Env
}

// New returns a CEL front-end. It builds a parse-only environment; variables do
// not need to be declared because we only parse (not type-check) expressions.
func New() *Parser {
	env, _ := celgo.NewEnv()
	return &Parser{env: env}
}

// Name identifies the parser.
func (*Parser) Name() string { return "cel" }

// Parse parses a CEL expression and converts its AST to IR.
func (p *Parser) Parse(rule string) (api.Program, error) {
	parsed, iss := p.env.Parse(rule)
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("cel parse: %w", iss.Err())
	}
	return convert(parsed.NativeRep().Expr())
}

func convert(e celast.Expr) (ir.Node, error) {
	switch e.Kind() {
	case celast.CallKind:
		return callToIR(e.AsCall())
	}
	return nil, fmt.Errorf("cel: unsupported expr kind %v", e.Kind())
}

func callToIR(call celast.CallExpr) (ir.Node, error) {
	fn := call.FunctionName()
	args := call.Args()

	switch fn {
	case celops.LogicalAnd, celops.LogicalOr:
		op := "AND"
		if fn == celops.LogicalOr {
			op = "OR"
		}
		nodes := make([]ir.Node, 0, len(args))
		for _, a := range args {
			n, err := convert(a)
			if err != nil {
				return nil, err
			}
			nodes = append(nodes, n)
		}
		return ir.Logic{Op: op, Args: nodes}, nil

	case celops.LogicalNot: // !expr  -> ir.Not (covers !(x in [..]), !startsWith, ...)
		if len(args) != 1 {
			return nil, fmt.Errorf("cel: ! needs 1 arg")
		}
		arg, err := convert(args[0])
		if err != nil {
			return nil, err
		}
		return ir.Not{Arg: arg}, nil

	case celops.Equals, celops.NotEquals, celops.Less, celops.LessEquals,
		celops.Greater, celops.GreaterEquals:
		if len(args) != 2 {
			return nil, fmt.Errorf("cel: comparison needs 2 args")
		}
		field, err := identName(args[0])
		if err != nil {
			return nil, err
		}
		v, err := litVal(args[1])
		if err != nil {
			return nil, err
		}
		return ir.Compare{Field: field, Op: cmpOp(fn), Val: v}, nil

	case celops.In:
		if len(args) != 2 {
			return nil, fmt.Errorf("cel: `in` needs 2 args")
		}
		field, err := identName(args[0])
		if err != nil {
			return nil, err
		}
		if args[1].Kind() != celast.ListKind {
			return nil, fmt.Errorf("cel: `in` expects a list")
		}
		elems := args[1].AsList().Elements()
		vals := make([]ir.Value, 0, len(elems))
		for _, el := range elems {
			v, err := litVal(el)
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
		}
		return ir.In{Field: field, Vals: vals}, nil

	case "startsWith", "endsWith", "contains":
		// Receiver-style string functions: field.startsWith("x").
		field, err := identName(call.Target())
		if err != nil {
			return nil, err
		}
		if len(args) != 1 {
			return nil, fmt.Errorf("cel: %s needs 1 arg", fn)
		}
		lit, err := litVal(args[0])
		if err != nil {
			return nil, err
		}
		switch fn {
		case "startsWith":
			return ir.Like{Field: field, Pattern: lit.Str + "%"}, nil
		case "endsWith":
			return ir.Like{Field: field, Pattern: "%" + lit.Str}, nil
		default:
			return ir.Like{Field: field, Pattern: "%" + lit.Str + "%"}, nil
		}
	}
	return nil, fmt.Errorf("cel: unsupported function %q", fn)
}

func cmpOp(fn string) string {
	switch fn {
	case celops.Equals:
		return "="
	case celops.NotEquals:
		return "!="
	case celops.Less:
		return "<"
	case celops.LessEquals:
		return "<="
	case celops.Greater:
		return ">"
	case celops.GreaterEquals:
		return ">="
	}
	return "="
}

func identName(e celast.Expr) (string, error) {
	if e.Kind() == celast.IdentKind {
		return e.AsIdent(), nil
	}
	return "", fmt.Errorf("cel: expected identifier, got kind %v", e.Kind())
}

func litVal(e celast.Expr) (ir.Value, error) {
	if e.Kind() != celast.LiteralKind {
		return ir.Value{}, fmt.Errorf("cel: expected literal, got kind %v", e.Kind())
	}
	switch x := e.AsLiteral().Value().(type) {
	case string:
		return ir.Value{IsString: true, Str: x}, nil
	case int64:
		return ir.Value{Num: strconv.FormatInt(x, 10)}, nil
	case float64:
		return ir.Value{Num: strconv.FormatFloat(x, 'f', -1, 64)}, nil
	case bool:
		return ir.Value{IsString: true, Str: strconv.FormatBool(x)}, nil
	}
	return ir.Value{}, fmt.Errorf("cel: unsupported literal type")
}
