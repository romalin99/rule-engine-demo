// Package json parses a JSON rule document into the shared IR. This gives a
// machine-friendly, UI-buildable rule format that compiles to the very same
// plan as SQL/qlbridge rules.
//
// Grammar (recursive):
//
//	{"and": [ <node>, ... ]}
//	{"or":  [ <node>, ... ]}
//	{"not": <node>}                              // NOT (sub-expression)
//	{"field":"age","op":"between","values":[25,40]}
//	{"field":"province","op":"in","values":["广东","江苏"]}
//	{"field":"income_level","op":"not_in","values":["<5k"]}
//	{"field":"favorite_category","op":"like","value":"数%"}
//	{"field":"occupation","op":"not_like","value":"%学生%"}
//	{"field":"active_score","op":">=","value":85}
//	{"field":"phone","op":"isnull"}              // or "isnotnull"
//
// op ∈ { =, ==, !=, <>, >, >=, <, <=, between, in, not_in, like, not_like,
//        isnull, isnotnull }. The '<>' alias is accepted as not-equal.
package json

import (
	encjson "encoding/json"
	"fmt"
	"strconv"

	"github.com/example/rule-engine-demo/pkg/api"
	"github.com/example/rule-engine-demo/pkg/ir"
)

// Parser implements api.Parser for JSON rule documents.
type Parser struct{}

// New returns a JSON rule parser.
func New() *Parser { return &Parser{} }

// Name identifies the parser.
func (*Parser) Name() string { return "json" }

// Parse parses a JSON rule string into IR.
func (*Parser) Parse(rule string) (api.Program, error) {
	var raw node
	if err := encjson.Unmarshal([]byte(rule), &raw); err != nil {
		return nil, fmt.Errorf("json rule: %w", err)
	}
	return raw.toIR()
}

type node struct {
	And    []node `json:"and"`
	Or     []node `json:"or"`
	Not    *node  `json:"not"` // {"not": <node>} -> ir.Not
	Field  string `json:"field"`
	Op     string `json:"op"`
	Value  any    `json:"value"`
	Values []any  `json:"values"`
}

func (n node) toIR() (ir.Node, error) {
	switch {
	case len(n.And) > 0:
		return n.logic("AND", n.And)
	case len(n.Or) > 0:
		return n.logic("OR", n.Or)
	case n.Not != nil:
		arg, err := n.Not.toIR()
		if err != nil {
			return nil, err
		}
		return ir.Not{Arg: arg}, nil
	case n.Field != "":
		return n.leaf()
	}
	return nil, fmt.Errorf("json rule: empty/!recognized node")
}

func (n node) logic(op string, children []node) (ir.Node, error) {
	args := make([]ir.Node, 0, len(children))
	for _, c := range children {
		ch, err := c.toIR()
		if err != nil {
			return nil, err
		}
		args = append(args, ch)
	}
	if len(args) == 1 {
		return args[0], nil
	}
	return ir.Logic{Op: op, Args: args}, nil
}

func (n node) leaf() (ir.Node, error) {
	switch n.Op {
	case "=", "==", "!=", "<>", ">", ">=", "<", "<=":
		v, err := jsonVal(n.Value)
		if err != nil {
			return nil, err
		}
		op := n.Op
		switch op {
		case "==":
			op = "="
		case "<>":
			op = "!="
		}
		return ir.Compare{Field: n.Field, Op: op, Val: v}, nil
	case "between":
		if len(n.Values) != 2 {
			return nil, fmt.Errorf("json rule: between needs 2 values")
		}
		lo, err := jsonVal(n.Values[0])
		if err != nil {
			return nil, err
		}
		hi, err := jsonVal(n.Values[1])
		if err != nil {
			return nil, err
		}
		return ir.Between{Field: n.Field, Lo: lo, Hi: hi}, nil
	case "in", "not_in", "notin":
		vals := make([]ir.Value, 0, len(n.Values))
		for _, raw := range n.Values {
			v, err := jsonVal(raw)
			if err != nil {
				return nil, err
			}
			vals = append(vals, v)
		}
		return ir.In{Field: n.Field, Vals: vals, Negate: n.Op != "in"}, nil
	case "like", "not_like", "notlike":
		s, ok := n.Value.(string)
		if !ok {
			return nil, fmt.Errorf("json rule: like value must be string")
		}
		return ir.Like{Field: n.Field, Pattern: s, Negate: n.Op != "like"}, nil
	case "isnull":
		return ir.IsNull{Field: n.Field, Negate: false}, nil
	case "isnotnull":
		return ir.IsNull{Field: n.Field, Negate: true}, nil
	}
	return nil, fmt.Errorf("json rule: unsupported op %q", n.Op)
}

// jsonVal converts a decoded JSON scalar into an IR value.
func jsonVal(v any) (ir.Value, error) {
	switch x := v.(type) {
	case string:
		return ir.Value{IsString: true, Str: x}, nil
	case float64:
		return ir.Value{Num: strconv.FormatFloat(x, 'f', -1, 64)}, nil
	case bool:
		return ir.Value{IsString: true, Str: strconv.FormatBool(x)}, nil
	case encjson.Number:
		return ir.Value{Num: x.String()}, nil
	}
	return ir.Value{}, fmt.Errorf("json rule: unsupported value %T", v)
}
