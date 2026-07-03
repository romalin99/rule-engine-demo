package engine

import (
	"encoding/json"
	"fmt"
	"strconv"

	"tcg-rulex-engine/pkg/ir"
)

// JSONFrontend parses a JSON rule document into the shared IR. This gives a
// machine-friendly, UI-buildable rule format that compiles to the very same
// bytecode as SQL/qlbridge rules.
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
//
//	isnull, isnotnull }
type JSONFrontend struct{}

// Name identifies the frontend.
func (JSONFrontend) Name() string { return "json" }

// maxJSONDepth bounds JSON-rule nesting. json.Unmarshal happily decodes very
// deeply nested {"and":[{"and":[…]}]} / {"not":{"not":…}} documents, and the
// recursive toIR walk below — plus every later recursive consumer (vm.Compile,
// the AST runtime, Emit) — would then blow the goroutine stack, which Go
// cannot recover. This mirrors the native SQL parser's maxParseDepth so both
// front-ends reject pathological nesting at load time with a clean error
// instead of crashing the process. It is well beyond any real rule.
const maxJSONDepth = 200

// Parse parses a JSON rule string into IR.
func (JSONFrontend) Parse(rule string) (ir.Node, error) {
	var raw jsonNode
	if err := json.Unmarshal([]byte(rule), &raw); err != nil {
		return nil, fmt.Errorf("json rule: %w", err)
	}
	return raw.toIR(0)
}

type jsonNode struct {
	And    []jsonNode `json:"and"`
	Or     []jsonNode `json:"or"`
	Not    *jsonNode  `json:"not"` // {"not": <node>} -> ir.Not
	Field  string     `json:"field"`
	Op     string     `json:"op"`
	Value  any        `json:"value"`
	Values []any      `json:"values"`
}

func (n jsonNode) toIR(depth int) (ir.Node, error) {
	if depth > maxJSONDepth {
		return nil, fmt.Errorf("json rule: nested too deeply (max %d levels)", maxJSONDepth)
	}
	switch {
	case len(n.And) > 0:
		return n.logic("AND", n.And, depth)
	case len(n.Or) > 0:
		return n.logic("OR", n.Or, depth)
	case n.Not != nil:
		arg, err := n.Not.toIR(depth + 1)
		if err != nil {
			return nil, err
		}
		return ir.Not{Arg: arg}, nil
	case n.Field != "":
		return n.leaf()
	}
	return nil, fmt.Errorf("json rule: empty/!recognized node")
}

func (n jsonNode) logic(op string, children []jsonNode, depth int) (ir.Node, error) {
	args := make([]ir.Node, 0, len(children))
	for _, c := range children {
		ch, err := c.toIR(depth + 1)
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

func (n jsonNode) leaf() (ir.Node, error) {
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
		// String/date bounds desugar to `field >= lo AND field <= hi` (lexical
		// order), mirroring the native SQL parser so a JSON rule compiles to the
		// very same bytecode as its SQL equivalent. The compact Between node is
		// numeric-only.
		if lo.IsString || hi.IsString {
			return ir.Logic{Op: "AND", Args: []ir.Node{
				ir.Compare{Field: n.Field, Op: ">=", Val: lo},
				ir.Compare{Field: n.Field, Op: "<=", Val: hi},
			}}, nil
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
		return ir.Like{Field: n.Field, Pattern: s, Negate: n.Op != "like", Wildcards: true}, nil
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
	case json.Number:
		return ir.Value{Num: x.String()}, nil
	}
	return ir.Value{}, fmt.Errorf("json rule: unsupported value %T", v)
}
