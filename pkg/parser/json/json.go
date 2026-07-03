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
// Extended predicates (mirror the native SQL front-end):
//
//	{"field":"name","op":"regexp","value":"^a","flags":"i"}   // or "not_regexp"
//	{"field":"profile","json":"$.city","op":"=","value":"深圳"} // JSON_EXTRACT left
//	{"exists":{"coll":"tags"}}                                  // non-empty
//	{"exists":{"coll":"orders","where":{"field":"amount","op":">","value":100}}}
//	{"field":"score","op":"<","any":{"array":"scores"}}        // x < ANY(scores)
//	{"field":"prov","op":"=","any":{"values":["A","B"]}}       // x = ANY(list)
//	{"field":"budget","op":">=","all":{"select":"amount","coll":"orders",
//	    "where":{"field":"status","op":"=","value":"paid"}}}   // sub-query quantifier
//	{"agg":{"fn":"COUNT","col":"*","from":"orders",
//	    "where":{"field":"status","op":"=","value":"paid"}},"op":">=","value":2}
//
// op ∈ { =, ==, !=, <>, >, >=, <, <=, between, in, not_in, like, not_like,
//
//	isnull, isnotnull, regexp, not_regexp }. The '<>' alias is accepted as
//	not-equal; '==' as equal.
package json

import (
	encjson "encoding/json"
	"fmt"
	"strconv"
	"strings"

	"tcg-rulex-engine/pkg/api"
	"tcg-rulex-engine/pkg/ir"
)

// Parser implements api.Parser for JSON rule documents.
type Parser struct{}

// New returns a JSON rule parser.
func New() *Parser { return &Parser{} }

// Name identifies the parser.
func (*Parser) Name() string { return "json" }

// maxJSONDepth bounds JSON-rule nesting. The recursive toIR walk below — and
// every later recursive consumer of the IR (vm.Compile, the AST runtime, Emit)
// — would blow the goroutine stack on a deeply nested document, which Go
// cannot recover. This mirrors the native SQL parser's and engine.JSONFrontend's
// bound so all JSON rule paths reject pathological nesting at load time with a
// clean error. Well beyond any real rule.
const maxJSONDepth = 200

// Parse parses a JSON rule string into IR.
func (*Parser) Parse(rule string) (api.Program, error) {
	var raw node
	if err := encjson.Unmarshal([]byte(rule), &raw); err != nil {
		return nil, fmt.Errorf("json rule: %w", err)
	}
	// Bound nesting before the recursive lowering. checkDepth stops descending
	// the instant it passes the cap, so its own recursion is capped too (it
	// never goes deeper than maxJSONDepth+1 frames) — a hostile document fails
	// here instead of overflowing the stack in toIR.
	if err := checkDepth(&raw, 0); err != nil {
		return nil, err
	}
	return raw.toIR()
}

// checkDepth verifies the decoded rule tree does not nest past maxJSONDepth.
// It follows exactly the child links that toIR recurses through: not / and /
// or, and the optional WHERE predicates of EXISTS, ANY/ALL and aggregate
// sub-queries. It returns as soon as the cap is exceeded, so it is itself
// depth-bounded and cannot overflow the stack while checking.
func checkDepth(n *node, depth int) error {
	if n == nil {
		return nil
	}
	if depth > maxJSONDepth {
		return fmt.Errorf("json rule: nested too deeply (max %d levels)", maxJSONDepth)
	}
	if err := checkDepth(n.Not, depth+1); err != nil {
		return err
	}
	for i := range n.And {
		if err := checkDepth(&n.And[i], depth+1); err != nil {
			return err
		}
	}
	for i := range n.Or {
		if err := checkDepth(&n.Or[i], depth+1); err != nil {
			return err
		}
	}
	for _, sq := range []*subq{n.Exists, n.Any, n.All} {
		if sq != nil {
			if err := checkDepth(sq.Where, depth+1); err != nil {
				return err
			}
		}
	}
	if n.Agg != nil {
		if err := checkDepth(n.Agg.Where, depth+1); err != nil {
			return err
		}
	}
	return nil
}

type node struct {
	Value  any    `json:"value"`
	Exists *subq  `json:"exists"`
	Agg    *aggq  `json:"agg"`
	Not    *node  `json:"not"`
	All    *subq  `json:"all"`
	Any    *subq  `json:"any"`
	Field  string `json:"field"`
	JSON   string `json:"json"`
	Flags  string `json:"flags"`
	Op     string `json:"op"`
	Values []any  `json:"values"`
	And    []node `json:"and"`
	Or     []node `json:"or"`
}

// subq is the body of an EXISTS or ANY/ALL construct. Exactly one of Array,
// Values or (Select+Coll) is used for ANY/ALL; EXISTS uses Coll [+ Where].
type subq struct {
	Where  *node  `json:"where"`
	Array  string `json:"array"`
	Select string `json:"select"`
	Coll   string `json:"coll"`
	Values []any  `json:"values"`
}

// aggq is a scalar aggregate sub-query: (SELECT Fn(Col|*) FROM Coll [WHERE ...]).
type aggq struct {
	Where *node  `json:"where"`
	Fn    string `json:"fn"`
	Col   string `json:"col"`
	Coll  string `json:"from"`
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
	case n.Exists != nil:
		return n.existsIR()
	case n.Agg != nil:
		return n.aggIR()
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

// leftTerm builds the left operand of a leaf: a JSON_EXTRACT call when a "json"
// path modifier is present, otherwise a bare field reference.
func (n node) leftTerm() ir.Term {
	if n.JSON != "" {
		return ir.CallTerm{Fn: "JSON_EXTRACT", Args: []ir.Term{
			ir.FieldTerm{Name: n.Field},
			ir.LitTerm{Val: ir.Value{IsString: true, Str: n.JSON}},
		}}
	}
	return ir.FieldTerm{Name: n.Field}
}

func (n node) leaf() (ir.Node, error) {
	if n.Any != nil {
		return n.quant(n.Any, false)
	}
	if n.All != nil {
		return n.quant(n.All, true)
	}

	left := n.leftTerm()
	_, plain := left.(ir.FieldTerm)

	switch n.Op {
	case "=", "==", "!=", "<>", ">", ">=", "<", "<=":
		v, err := jsonVal(n.Value)
		if err != nil {
			return nil, err
		}
		if plain {
			return ir.Compare{Field: n.Field, Op: normOp(n.Op), Val: v}, nil
		}
		return ir.CompareTerm{Left: left, Op: normOp(n.Op), Right: ir.LitTerm{Val: v}}, nil

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
		if plain {
			return ir.Between{Field: n.Field, Lo: lo, Hi: hi}, nil
		}
		return ir.Logic{Op: "AND", Args: []ir.Node{
			ir.CompareTerm{Left: left, Op: ">=", Right: ir.LitTerm{Val: lo}},
			ir.CompareTerm{Left: left, Op: "<=", Right: ir.LitTerm{Val: hi}},
		}}, nil

	case "in", "not_in", "notin":
		negate := n.Op != "in"
		if plain {
			vals := make([]ir.Value, 0, len(n.Values))
			for _, raw := range n.Values {
				v, err := jsonVal(raw)
				if err != nil {
					return nil, err
				}
				vals = append(vals, v)
			}
			return ir.In{Field: n.Field, Vals: vals, Negate: negate}, nil
		}
		return n.inTerm(left, negate)

	case "like", "not_like", "notlike":
		s, ok := n.Value.(string)
		if !ok {
			return nil, fmt.Errorf("json rule: like value must be string")
		}
		if plain {
			return ir.Like{Field: n.Field, Pattern: s, Negate: n.Op != "like", Wildcards: true}, nil
		}
		return ir.LikeTerm{Left: left, Pattern: s, Negate: n.Op != "like", Wildcards: true}, nil

	case "regexp", "not_regexp", "rlike":
		s, ok := n.Value.(string)
		if !ok {
			return nil, fmt.Errorf("json rule: regexp value must be string")
		}
		if strings.Contains(n.Flags, "i") {
			s = "(?i)" + s
		}
		return ir.Regexp{Left: left, Pattern: s, Negate: n.Op == "not_regexp"}, nil

	case "isnull":
		if plain {
			return ir.IsNull{Field: n.Field, Negate: false}, nil
		}
		return ir.IsNullTerm{Left: left, Negate: false}, nil
	case "isnotnull":
		if plain {
			return ir.IsNull{Field: n.Field, Negate: true}, nil
		}
		return ir.IsNullTerm{Left: left, Negate: true}, nil
	}
	return nil, fmt.Errorf("json rule: unsupported op %q", n.Op)
}

// inTerm desugars `<term> IN (v...)` / `NOT IN` when the left is not a bare
// field: OR of equals (IN) or AND of not-equals (NOT IN).
func (n node) inTerm(left ir.Term, negate bool) (ir.Node, error) {
	op, logicOp := "=", "OR"
	if negate {
		op, logicOp = "!=", "AND"
	}
	args := make([]ir.Node, 0, len(n.Values))
	for _, raw := range n.Values {
		v, err := jsonVal(raw)
		if err != nil {
			return nil, err
		}
		args = append(args, ir.CompareTerm{Left: left, Op: op, Right: ir.LitTerm{Val: v}})
	}
	if len(args) == 1 {
		return args[0], nil
	}
	return ir.Logic{Op: logicOp, Args: args}, nil
}

// quant builds a quantified comparison `<left> <op> ANY|ALL (...)` from a subq.
func (n node) quant(sq *subq, all bool) (ir.Node, error) {
	op := normOp(n.Op)
	if op == "" {
		return nil, fmt.Errorf("json rule: ANY/ALL needs an op")
	}
	left := n.leftTerm()

	switch {
	case sq.Select != "":
		where, err := sq.whereIR()
		if err != nil {
			return nil, err
		}
		return ir.QuantSub{Left: left, Op: op, All: all, Col: sq.Select, Coll: sq.Coll, Where: where}, nil
	case sq.Array != "":
		return ir.QuantArr{Left: left, Op: op, All: all, Array: ir.FieldTerm{Name: sq.Array}}, nil
	case len(sq.Values) > 0:
		logicOp := "OR"
		if all {
			logicOp = "AND"
		}
		args := make([]ir.Node, 0, len(sq.Values))
		for _, raw := range sq.Values {
			v, err := jsonVal(raw)
			if err != nil {
				return nil, err
			}
			args = append(args, ir.CompareTerm{Left: left, Op: op, Right: ir.LitTerm{Val: v}})
		}
		if len(args) == 1 {
			return args[0], nil
		}
		return ir.Logic{Op: logicOp, Args: args}, nil
	}
	return nil, fmt.Errorf("json rule: ANY/ALL needs one of array/values/select")
}

func (n node) existsIR() (ir.Node, error) {
	if n.Exists.Coll == "" {
		return nil, fmt.Errorf("json rule: exists needs a coll")
	}
	where, err := n.Exists.whereIR()
	if err != nil {
		return nil, err
	}
	return ir.Exists{Coll: n.Exists.Coll, Where: where}, nil
}

func (n node) aggIR() (ir.Node, error) {
	fn := strings.ToUpper(n.Agg.Fn)
	switch fn {
	case "COUNT", "SUM", "MIN", "MAX", "AVG":
	default:
		return nil, fmt.Errorf("json rule: unsupported aggregate %q", n.Agg.Fn)
	}
	col := n.Agg.Col
	if col == "*" {
		col = ""
	}
	if fn != "COUNT" && col == "" {
		return nil, fmt.Errorf("json rule: %s needs a column", fn)
	}
	where, err := n.Agg.whereIR()
	if err != nil {
		return nil, err
	}
	left := ir.AggSub{Fn: fn, Col: col, Coll: n.Agg.Coll, Where: where}
	op := normOp(n.Op)
	if op == "" {
		return nil, fmt.Errorf("json rule: agg needs a comparison op")
	}
	v, err := jsonVal(n.Value)
	if err != nil {
		return nil, err
	}
	return ir.CompareTerm{Left: left, Op: op, Right: ir.LitTerm{Val: v}}, nil
}

// whereIR converts an optional nested predicate to IR (nil when absent).
func (s *subq) whereIR() (ir.Node, error) {
	if s.Where == nil {
		return nil, nil
	}
	return s.Where.toIR()
}

func (a *aggq) whereIR() (ir.Node, error) {
	if a.Where == nil {
		return nil, nil
	}
	return a.Where.toIR()
}

// normOp normalizes the JSON comparison aliases to the IR's canonical operators.
func normOp(op string) string {
	switch op {
	case "==":
		return "="
	case "<>":
		return "!="
	}
	return op
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
