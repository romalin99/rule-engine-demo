package engine

import (
	"fmt"

	"github.com/araddon/qlbridge/expr"
	"github.com/araddon/qlbridge/lex"
	"github.com/araddon/qlbridge/value"

	"tcg-rulex-engine/pkg/ir"
)

// QLBridgeFrontend uses qlbridge purely as a SQL parser: it parses rule text to
// a qlbridge AST and then converts that AST into the project IR. qlbridge does
// NOT participate in evaluation — the bytecode VM does.
//
//	Rule(SQL) ─qlbridge.ParseExpression─▶ AST(Node) ─convert─▶ ir.Node
//
// qlbridge only understands the basic predicate subset (AND/OR, comparisons,
// IN, LIKE, numeric BETWEEN). For the richer SQL surface the engine advertises —
// scalar functions (LOWER/ROUND/DATE_*/…), IS [NOT] NULL, NOT (…), EXISTS/ANY/
// ALL, REGEXP, JSON_EXTRACT, array predicates — this frontend transparently
// falls back to the native parser (package ir), which is a strict superset.
// This keeps the default engine (engine.New) able to parse every documented
// feature while still exercising qlbridge for the common fast path.
type QLBridgeFrontend struct{}

// Name identifies the frontend.
func (QLBridgeFrontend) Name() string { return "qlbridge" }

// Parse parses with qlbridge then converts the AST to IR, falling back to the
// native parser for anything qlbridge cannot parse or convert.
func (QLBridgeFrontend) Parse(rule string) (ir.Node, error) {
	node, err := expr.ParseExpression(rule)
	if err != nil {
		// qlbridge cannot lex/parse this construct (functions it rejects, IS NULL,
		// EXISTS, REGEXP, JSON accessors, …). Defer to the native SQL parser.
		return ir.Parse(rule)
	}
	irNode, err := qlToIR(node)
	if err != nil {
		// qlbridge parsed it, but into a shape the converter does not support
		// (e.g. a function call, NOT, a quantifier). Defer to the native parser,
		// which produces the same IR the VM evaluates.
		return ir.Parse(rule)
	}
	return irNode, nil
}

// qlToIR converts a qlbridge AST node into the project IR.
func qlToIR(n expr.Node) (ir.Node, error) {
	switch t := n.(type) {

	case *expr.BooleanNode: // AND / OR with N args
		op, err := logicOp(t.Operator.T)
		if err != nil {
			return nil, err
		}
		args := make([]ir.Node, 0, len(t.Args))
		for _, a := range t.Args {
			c, err := qlToIR(a)
			if err != nil {
				return nil, err
			}
			args = append(args, c)
		}
		return ir.Logic{Op: op, Args: args}, nil

	case *expr.BinaryNode:
		switch t.Operator.T {
		case lex.TokenLogicAnd, lex.TokenAnd, lex.TokenLogicOr, lex.TokenOr:
			op, _ := logicOp(t.Operator.T)
			l, err := qlToIR(t.Args[0])
			if err != nil {
				return nil, err
			}
			r, err := qlToIR(t.Args[1])
			if err != nil {
				return nil, err
			}
			return ir.Logic{Op: op, Args: []ir.Node{l, r}}, nil

		case lex.TokenIN:
			field, err := identName(t.Args[0])
			if err != nil {
				return nil, err
			}
			arr, ok := t.Args[1].(*expr.ArrayNode)
			if !ok {
				return nil, fmt.Errorf("qlbridge IN: expected array, got %T", t.Args[1])
			}
			vals := make([]ir.Value, 0, len(arr.Args))
			for _, e := range arr.Args {
				v, err := litVal(e)
				if err != nil {
					return nil, err
				}
				vals = append(vals, v)
			}
			return ir.In{Field: field, Vals: vals}, nil

		case lex.TokenLike:
			field, err := identName(t.Args[0])
			if err != nil {
				return nil, err
			}
			v, err := litVal(t.Args[1])
			if err != nil {
				return nil, err
			}
			return ir.Like{Field: field, Pattern: v.Str}, nil

		default: // comparison: field <op> literal
			field, err := identName(t.Args[0])
			if err != nil {
				return nil, err
			}
			v, err := litVal(t.Args[1])
			if err != nil {
				return nil, err
			}
			op, err := cmpName(t.Operator.T)
			if err != nil {
				return nil, err
			}
			return ir.Compare{Field: field, Op: op, Val: v}, nil
		}

	case *expr.TriNode: // BETWEEN
		if t.Operator.T != lex.TokenBetween || len(t.Args) != 3 {
			return nil, fmt.Errorf("qlbridge: unsupported tri-node %v", t.Operator.T)
		}
		field, err := identName(t.Args[0])
		if err != nil {
			return nil, err
		}
		lo, err := litVal(t.Args[1])
		if err != nil {
			return nil, err
		}
		hi, err := litVal(t.Args[2])
		if err != nil {
			return nil, err
		}
		// String/date bounds desugar to `field >= lo AND field <= hi` (lexical
		// order), mirroring the native parser; the compact Between node is
		// numeric-only. Keeps both frontends consistent for date-range BETWEEN.
		if lo.IsString || hi.IsString {
			return ir.Logic{Op: "AND", Args: []ir.Node{
				ir.Compare{Field: field, Op: ">=", Val: lo},
				ir.Compare{Field: field, Op: "<=", Val: hi},
			}}, nil
		}
		return ir.Between{Field: field, Lo: lo, Hi: hi}, nil
	}

	return nil, fmt.Errorf("qlbridge: unsupported node %T", n)
}

func logicOp(t lex.TokenType) (string, error) {
	switch t {
	case lex.TokenLogicAnd, lex.TokenAnd:
		return "AND", nil
	case lex.TokenLogicOr, lex.TokenOr:
		return "OR", nil
	}
	return "", fmt.Errorf("qlbridge: not a logic op %v", t)
}

func cmpName(t lex.TokenType) (string, error) {
	switch t {
	case lex.TokenEqual, lex.TokenEqualEqual:
		return "=", nil
	case lex.TokenNE:
		return "!=", nil
	case lex.TokenGT:
		return ">", nil
	case lex.TokenGE:
		return ">=", nil
	case lex.TokenLT:
		return "<", nil
	case lex.TokenLE:
		return "<=", nil
	}
	return "", fmt.Errorf("qlbridge: unsupported operator %v", t)
}

func identName(n expr.Node) (string, error) {
	id, ok := n.(*expr.IdentityNode)
	if !ok {
		return "", fmt.Errorf("qlbridge: expected identifier, got %T", n)
	}
	return id.Text, nil
}

// litVal extracts a literal (number or string) from a qlbridge node.
func litVal(n expr.Node) (ir.Value, error) {
	switch t := n.(type) {
	case *expr.NumberNode:
		return ir.Value{Num: t.Text}, nil
	case *expr.StringNode:
		return ir.Value{IsString: true, Str: t.Text}, nil
	case *expr.ValueNode:
		if t.Value != nil && t.Value.Type() == value.StringType {
			return ir.Value{IsString: true, Str: t.Value.ToString()}, nil
		}
		if t.Value != nil {
			return ir.Value{Num: t.Value.ToString()}, nil
		}
	case *expr.IdentityNode:
		// Bareword used as a value (qlbridge sometimes lexes unquoted text as an
		// identity); treat it as a string literal.
		return ir.Value{IsString: true, Str: t.Text}, nil
	}
	return ir.Value{}, fmt.Errorf("qlbridge: unsupported literal %T", n)
}
