// Package ast is a tree-walking runtime: it evaluates the unified IR directly,
// node by node, without compiling to bytecode. It is simpler (and slower) than
// the bytecode runtime and exists mainly as a correctness reference and as an
// A/B baseline for benchmarks (ast vs bytecode).
//
// Semantics mirror the bytecode VM: numeric comparison when both operands are
// numeric, otherwise string comparison; LIKE supports leading/trailing '%';
// a field is NULL when absent or nil.
package ast

import (
	"fmt"
	"strconv"
	"strings"

	"tcg-rulex-engine/pkg/api"
	"tcg-rulex-engine/pkg/ir"
)

// Runtime implements api.Runtime by walking the IR tree.
type Runtime struct{}

// New returns a tree-walking runtime.
func New() *Runtime { return &Runtime{} }

// Name identifies the runtime.
func (*Runtime) Name() string { return "ast" }

// Compile validates the program and returns it unchanged: the plan is the IR.
func (*Runtime) Compile(program api.Program) (api.Plan, error) {
	if program == nil {
		return nil, fmt.Errorf("ast: nil program")
	}
	return program, nil
}

// Execute walks the IR plan against a row.
func (*Runtime) Execute(plan api.Plan, row map[string]any) (bool, error) {
	node, ok := plan.(ir.Node)
	if !ok {
		return false, fmt.Errorf("ast: invalid plan type %T", plan)
	}
	return eval(node, row)
}

func eval(n ir.Node, row map[string]any) (bool, error) {
	switch t := n.(type) {
	case ir.Logic:
		if t.Op == "OR" {
			for _, a := range t.Args {
				ok, err := eval(a, row)
				if err != nil {
					return false, err
				}
				if ok {
					return true, nil
				}
			}
			return false, nil
		}
		// default: AND
		for _, a := range t.Args {
			ok, err := eval(a, row)
			if err != nil {
				return false, err
			}
			if !ok {
				return false, nil
			}
		}
		return true, nil

	case ir.Compare:
		return compare(row[t.Field], t.Op, t.Val), nil

	case ir.Between:
		f, ok := toNum(row[t.Field])
		if !ok {
			return false, nil
		}
		lo, lok := litNum(t.Lo)
		hi, hok := litNum(t.Hi)
		if !lok || !hok {
			return false, nil
		}
		return f >= lo && f <= hi, nil

	case ir.In:
		s := asString(row[t.Field])
		in := false
		for _, v := range t.Vals {
			if s == litString(v) {
				in = true
				break
			}
		}
		return in != t.Negate, nil // NOT IN flips membership

	case ir.Like:
		m := like(asString(row[t.Field]), t.Pattern)
		return m != t.Negate, nil // NOT LIKE flips the match

	case ir.IsNull:
		v, present := row[t.Field]
		isNull := !present || v == nil
		if t.Negate {
			return !isNull, nil
		}
		return isNull, nil

	case ir.Not:
		ok, err := eval(t.Arg, row)
		if err != nil {
			return false, err
		}
		return !ok, nil
	}
	return false, fmt.Errorf("ast: unsupported node %T", n)
}

// compare evaluates `field <op> literal`, numeric when both sides are numeric.
func compare(raw any, op string, lit ir.Value) bool {
	if !lit.IsString {
		if fn, ok := toNum(raw); ok {
			if ln, err := strconv.ParseFloat(lit.Num, 64); err == nil {
				switch op {
				case "=":
					return fn == ln
				case "!=":
					return fn != ln
				case ">":
					return fn > ln
				case ">=":
					return fn >= ln
				case "<":
					return fn < ln
				case "<=":
					return fn <= ln
				}
			}
		}
	}
	fs, ls := asString(raw), litString(lit)
	switch op {
	case "=":
		return fs == ls
	case "!=":
		return fs != ls
	case ">":
		return fs > ls
	case ">=":
		return fs >= ls
	case "<":
		return fs < ls
	case "<=":
		return fs <= ls
	}
	return false
}

// like matches a SQL LIKE pattern using only leading/trailing '%' wildcards.
func like(s, pattern string) bool {
	hasPre := strings.HasPrefix(pattern, "%")
	hasSuf := strings.HasSuffix(pattern, "%")
	core := strings.Trim(pattern, "%")
	switch {
	case hasPre && hasSuf:
		return strings.Contains(s, core)
	case hasSuf:
		return strings.HasPrefix(s, core)
	case hasPre:
		return strings.HasSuffix(s, core)
	default:
		return s == pattern
	}
}

// toNum converts a numeric field value to float64 (strings are NOT numeric,
// matching the VM's tagged-value semantics).
func toNum(raw any) (float64, bool) {
	switch x := raw.(type) {
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint8:
		return float64(x), true
	case uint16:
		return float64(x), true
	case uint32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	default:
		return 0, false
	}
}

// asString renders a field value as text for string comparison / IN / LIKE.
func asString(raw any) string {
	switch x := raw.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	default:
		if f, ok := toNum(raw); ok {
			return strconv.FormatFloat(f, 'f', -1, 64)
		}
		return ""
	}
}

func litNum(v ir.Value) (float64, bool) {
	if v.IsString {
		f, err := strconv.ParseFloat(v.Str, 64)
		return f, err == nil
	}
	f, err := strconv.ParseFloat(v.Num, 64)
	return f, err == nil
}

func litString(v ir.Value) string {
	if v.IsString {
		return v.Str
	}
	return v.Num
}

// Reason explains why one predicate or sub-expression failed during Explain.
type Reason struct {
	Expr   string `json:"expr"`   // the failing predicate / group, in SQL form
	Detail string `json:"detail"` // human-readable cause, including the actual value
}

// Explain evaluates n against row and, when it does not pass, returns the
// predicates responsible. It uses the same semantics as Execute (and therefore
// the bytecode VM), so `passed` always matches a normal match; when passed is
// true the reasons slice is empty.
//
//   - AND  : reports every failing child.
//   - OR   : reports a single reason when no alternative held.
//   - NOT  : reports a reason when the negated condition was satisfied.
//   - leaf : reports a value-aware reason (field=value vs the operator).
func Explain(n ir.Node, row map[string]any) (passed bool, reasons []Reason) {
	switch t := n.(type) {
	case ir.Logic:
		if t.Op == "OR" {
			var details []string
			for _, a := range t.Args {
				ok, rs := Explain(a, row)
				if ok {
					return true, nil // one alternative held -> OR passes
				}
				for _, r := range rs {
					details = append(details, r.Detail)
				}
			}
			return false, []Reason{{
				Expr:   ir.Emit(t, ir.SQL),
				Detail: "no OR alternative held — " + strings.Join(details, "; "),
			}}
		}
		// AND: every child must pass; collect all failures.
		var rs []Reason
		for _, a := range t.Args {
			if ok, r := Explain(a, row); !ok {
				rs = append(rs, r...)
			}
		}
		return len(rs) == 0, rs

	case ir.Not:
		if inner, _ := eval(t.Arg, row); !inner {
			return true, nil // inner condition failed -> NOT passes
		}
		return false, []Reason{{
			Expr:   ir.Emit(t, ir.SQL),
			Detail: "the negated condition was satisfied",
		}}

	default: // leaf predicate
		if ok, _ := eval(n, row); ok {
			return true, nil
		}
		return false, []Reason{{Expr: ir.Emit(n, ir.SQL), Detail: describeLeaf(n, row)}}
	}
}

// describeLeaf renders why a leaf predicate failed, including the row's value.
func describeLeaf(n ir.Node, row map[string]any) string {
	switch t := n.(type) {
	case ir.Compare:
		return fmt.Sprintf("%s=%s does not satisfy %s %s", t.Field, fieldRepr(row, t.Field), t.Op, litRepr(t.Val))
	case ir.Between:
		return fmt.Sprintf("%s=%s is outside [%s, %s]", t.Field, fieldRepr(row, t.Field), litRepr(t.Lo), litRepr(t.Hi))
	case ir.In:
		if t.Negate {
			return fmt.Sprintf("%s=%s is in the excluded set", t.Field, fieldRepr(row, t.Field))
		}
		return fmt.Sprintf("%s=%s is not in the allowed set", t.Field, fieldRepr(row, t.Field))
	case ir.Like:
		if t.Negate {
			return fmt.Sprintf("%s=%s matches the excluded pattern '%s'", t.Field, fieldRepr(row, t.Field), t.Pattern)
		}
		return fmt.Sprintf("%s=%s does not match pattern '%s'", t.Field, fieldRepr(row, t.Field), t.Pattern)
	case ir.IsNull:
		if t.Negate {
			return fmt.Sprintf("%s is missing or null (IS NOT NULL required)", t.Field)
		}
		return fmt.Sprintf("%s=%s is present but IS NULL required", t.Field, fieldRepr(row, t.Field))
	}
	return "condition not satisfied"
}

// fieldRepr renders a row field for messages; missing/nil becomes <null>.
func fieldRepr(row map[string]any, field string) string {
	v, ok := row[field]
	if !ok || v == nil {
		return "<null>"
	}
	return asString(v)
}

// litRepr renders an IR literal: quoted when it is a string.
func litRepr(v ir.Value) string {
	if v.IsString {
		return "'" + v.Str + "'"
	}
	return v.Num
}
