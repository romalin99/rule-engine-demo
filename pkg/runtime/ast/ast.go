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
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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

	case ir.CompareTerm:
		return compareVals(evalTerm(t.Left, row), t.Op, evalTerm(t.Right, row)), nil

	case ir.LikeTerm:
		v := evalTerm(t.Left, row)
		matched := v != nil && like(valStr(v), t.Pattern)
		return matched != t.Negate, nil // NOT LIKE flips the match

	case ir.IsNullTerm:
		isNull := evalTerm(t.Left, row) == nil
		return isNull != t.Negate, nil // IS NOT NULL flips the test

	case ir.PredCall:
		return evalPred(t.Fn, t.Args, row), nil

	case ir.QuantArr:
		return quantArrAST(t, row), nil

	case ir.Exists:
		return existsAST(t, row), nil

	case ir.QuantSub:
		return quantSubAST(t, row), nil

	case ir.Regexp:
		v := evalTerm(t.Left, row)
		if v == nil {
			return t.Negate, nil // NULL never matches; NOT REGEXP on NULL -> true
		}
		re, err := compileRegexpCached(t.Pattern)
		if err != nil {
			return false, fmt.Errorf("ast: bad regexp %q: %w", t.Pattern, err)
		}
		return re.MatchString(valStr(v)) != t.Negate, nil
	}
	return false, fmt.Errorf("ast: unsupported node %T", n)
}

// reCache memoizes compiled regular expressions across Execute calls (the AST
// runtime compiles lazily, unlike the bytecode VM which compiles at load).
var reCache sync.Map

// compileRegexpCached returns a compiled (and cached) RE2 regular expression.
func compileRegexpCached(pattern string) (*regexp.Regexp, error) {
	if v, ok := reCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	reCache.Store(pattern, re)
	return re, nil
}

// quantArrAST evaluates `left <op> ANY/ALL (arrayField)`: compare left against
// every element. Empty/absent array makes ANY false and ALL true. Elements
// compare numerically when left is numeric and the element parses as a number.
func quantArrAST(t ir.QuantArr, row map[string]any) bool {
	left := evalTerm(t.Left, row)
	arr := arrayOf(t.Array, row)
	if len(arr) == 0 {
		return t.All
	}
	for _, e := range arr {
		match := compareVals(left, t.Op, elemValAST(left, e))
		if t.All && !match {
			return false
		}
		if !t.All && match {
			return true
		}
	}
	return t.All
}

// elemValAST renders an array element as a number when left is numeric and the
// element parses as one, otherwise as a string (mirrors the VM's elemValue).
func elemValAST(left any, e string) any {
	if _, ok := left.(float64); ok {
		if f, err := strconv.ParseFloat(e, 64); err == nil {
			return f
		}
	}
	return e
}

// existsAST evaluates EXISTS over a collection: non-empty when Where is nil,
// otherwise ∃ a nested row satisfying Where.
func existsAST(t ir.Exists, row map[string]any) bool {
	if t.Where == nil {
		return collLenAST(row[t.Coll]) > 0
	}
	for _, nr := range rowsOfAST(row[t.Coll]) {
		if ok, _ := eval(t.Where, nr); ok {
			return true
		}
	}
	return false
}

// quantSubAST evaluates `left <op> ANY/ALL (SELECT col FROM coll [WHERE pred])`,
// comparing left against the projected col of each matching nested row. ANY is
// ∃, ALL is ∀ (vacuously true over an empty result set).
func quantSubAST(t ir.QuantSub, row map[string]any) bool {
	left := evalTerm(t.Left, row)
	for _, nr := range rowsOfAST(row[t.Coll]) {
		if t.Where != nil {
			if ok, _ := eval(t.Where, nr); !ok {
				continue
			}
		}
		match := compareVals(left, t.Op, termVal(nr[t.Col]))
		if t.All && !match {
			return false
		}
		if !t.All && match {
			return true
		}
	}
	return t.All
}

// rowsOfAST coerces a collection field into nested rows (object elements only).
func rowsOfAST(raw any) []map[string]any {
	switch x := raw.(type) {
	case []map[string]any:
		return x
	case []any:
		out := make([]map[string]any, 0, len(x))
		for _, e := range x {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// collLenAST reports the element count of a collection field (scalar or object
// array), or 0 when the field is absent or not a slice.
func collLenAST(raw any) int {
	switch x := raw.(type) {
	case []any:
		return len(x)
	case []string:
		return len(x)
	case []map[string]any:
		return len(x)
	}
	return 0
}

// jsonExtractAST mirrors the VM's jsonExtract: it reads the document (raw JSON
// string or already-decoded object/array) from a field or expression, follows
// the path, and returns the leaf scalar (string / float64 / "true"|"false") or
// nil. Booleans render as strings to match the VM's value model.
func jsonExtractAST(args []ir.Term, row map[string]any) any {
	if len(args) != 2 {
		return nil
	}
	lit, ok := args[1].(ir.LitTerm)
	if !ok || !lit.Val.IsString {
		return nil
	}
	var raw any
	if ft, ok := args[0].(ir.FieldTerm); ok {
		raw = row[ft.Name] // raw value: JSON string or parsed object
	} else {
		raw = evalTerm(args[0], row)
	}
	root := jsonRoot(raw)
	if root == nil {
		return nil
	}
	cur, ok := navigateJSON(root, lit.Val.Str)
	if !ok {
		return nil
	}
	switch x := cur.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return f
		}
		return nil
	default: // numeric kinds -> float64; nil/map/slice -> nil
		if f, ok := toNum(cur); ok {
			return f
		}
		return nil
	}
}

// jsonRoot returns a navigable JSON value: parses a JSON string, passes a
// pre-decoded object/array through, rejects anything else.
func jsonRoot(raw any) any {
	switch x := raw.(type) {
	case string:
		var v any
		if json.Unmarshal([]byte(x), &v) != nil {
			return nil
		}
		return v
	case map[string]any:
		return x
	case []any:
		return x
	default:
		return nil
	}
}

// jsonSeg is one step of a JSON path: a map key or an array index.
type jsonSeg struct {
	key   string
	idx   int
	isIdx bool
}

// navigateJSON walks root following path; ok is false on any miss or type clash.
func navigateJSON(root any, path string) (any, bool) {
	segs, ok := parseJSONPath(path)
	if !ok {
		return nil, false
	}
	cur := root
	for _, s := range segs {
		if s.isIdx {
			arr, ok := cur.([]any)
			if !ok || s.idx < 0 || s.idx >= len(arr) {
				return nil, false
			}
			cur = arr[s.idx]
			continue
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[s.key]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// parseJSONPath parses a `$`-rooted accessor into segments: dotted keys
// (`$.a.b`), array indices (`$[0]`, `$.a[2]`) and bracket-quoted keys
// (`$["a.b"]`). Anything else makes ok=false.
func parseJSONPath(p string) ([]jsonSeg, bool) {
	if p == "" || p[0] != '$' {
		return nil, false
	}
	var segs []jsonSeg
	i := 1
	for i < len(p) {
		switch p[i] {
		case '.':
			i++
			start := i
			for i < len(p) && p[i] != '.' && p[i] != '[' {
				i++
			}
			if i == start {
				return nil, false
			}
			segs = append(segs, jsonSeg{key: p[start:i]})
		case '[':
			i++
			if i < len(p) && (p[i] == '"' || p[i] == '\'') {
				q := p[i]
				i++
				start := i
				for i < len(p) && p[i] != q {
					i++
				}
				if i >= len(p) {
					return nil, false
				}
				key := p[start:i]
				i++ // closing quote
				if i >= len(p) || p[i] != ']' {
					return nil, false
				}
				i++ // ']'
				segs = append(segs, jsonSeg{key: key})
			} else {
				start := i
				for i < len(p) && p[i] >= '0' && p[i] <= '9' {
					i++
				}
				if i == start || i >= len(p) || p[i] != ']' {
					return nil, false
				}
				n, err := strconv.Atoi(p[start:i])
				if err != nil {
					return nil, false
				}
				i++ // ']'
				segs = append(segs, jsonSeg{idx: n, isIdx: true})
			}
		default:
			return nil, false
		}
	}
	return segs, true
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

// evalTerm evaluates a scalar operand (field, literal, or function call) to a
// normalized value — float64 for any numeric, string for text, or nil for
// NULL/undefined — exactly matching the bytecode VM's value model.
func evalTerm(t ir.Term, row map[string]any) any {
	switch x := t.(type) {
	case ir.FieldTerm:
		return termVal(row[x.Name])
	case ir.LitTerm:
		if x.Val.IsString {
			return x.Val.Str
		}
		if f, err := strconv.ParseFloat(x.Val.Num, 64); err == nil {
			return f
		}
		return nil
	case ir.CallTerm:
		return applyFunc(x.Fn, x.Args, row)
	case ir.AggSub:
		return aggSubAST(x, row)
	}
	return nil
}

// aggSubAST mirrors the bytecode VM's aggSub: a scalar aggregate over a nested
// collection (optionally filtered). COUNT returns a count; SUM/MIN/MAX/AVG
// return nil when no numeric values qualify.
func aggSubAST(t ir.AggSub, row map[string]any) any {
	n := 0
	var sum, mn, mx float64
	for _, nr := range rowsOfAST(row[t.Coll]) {
		if t.Where != nil {
			if ok, _ := eval(t.Where, nr); !ok {
				continue
			}
		}
		if t.Fn == "COUNT" {
			if t.Col == "" {
				n++
			} else if v, ok := nr[t.Col]; ok && v != nil {
				n++
			}
			continue
		}
		f, ok := toNum(nr[t.Col])
		if !ok {
			continue
		}
		if n == 0 {
			mn, mx = f, f
		} else {
			if f < mn {
				mn = f
			}
			if f > mx {
				mx = f
			}
		}
		sum += f
		n++
	}
	switch t.Fn {
	case "COUNT":
		return float64(n)
	case "SUM":
		if n == 0 {
			return nil
		}
		return sum
	case "AVG":
		if n == 0 {
			return nil
		}
		return sum / float64(n)
	case "MIN":
		if n == 0 {
			return nil
		}
		return mn
	case "MAX":
		if n == 0 {
			return nil
		}
		return mx
	}
	return nil
}

// termVal normalizes a raw row value like the VM's toValue: numerics become
// float64, strings stay strings, and anything else (nil/bool/unknown) is NULL.
func termVal(raw any) any {
	if f, ok := toNum(raw); ok {
		return f
	}
	if s, ok := raw.(string); ok {
		return s
	}
	return nil
}

// applyFunc evaluates a function call, mirroring the VM's callValue/substr:
// NULL operands propagate, math functions require a numeric operand, and a
// wrong argument count yields NULL.
func applyFunc(fn string, args []ir.Term, row map[string]any) any {
	vals := make([]any, len(args))
	for i, a := range args {
		vals[i] = evalTerm(a, row)
	}
	switch fn {
	case "UPPER":
		if s, ok := strOperand(vals); ok {
			return strings.ToUpper(s)
		}
	case "LOWER":
		if s, ok := strOperand(vals); ok {
			return strings.ToLower(s)
		}
	case "TRIM":
		if s, ok := strOperand(vals); ok {
			return strings.TrimSpace(s)
		}
	case "LENGTH", "LEN":
		if s, ok := strOperand(vals); ok {
			return float64(utf8.RuneCountInString(s))
		}
	case "ABS":
		if f, ok := numOperand(vals); ok {
			return math.Abs(f)
		}
	case "ROUND":
		// ROUND(x, d) rounds to d decimal places; ROUND(x) to the nearest integer.
		if len(vals) == 2 {
			x, xok := vals[0].(float64)
			d, dok := vals[1].(float64)
			if xok && dok {
				pow := math.Pow(10, d)
				if pow != 0 && !math.IsInf(pow, 0) {
					return math.Round(x*pow) / pow
				}
			}
			return nil
		}
		if f, ok := numOperand(vals); ok {
			return math.Round(f)
		}
	case "CEIL", "CEILING":
		if f, ok := numOperand(vals); ok {
			return math.Ceil(f)
		}
	case "FLOOR":
		if f, ok := numOperand(vals); ok {
			return math.Floor(f)
		}
	case "SUBSTRING", "SUBSTR":
		return substrAST(vals)
	case "CURRENT_DATE":
		return time.Now().Format("2006-01-02")
	case "CURRENT_TIMESTAMP":
		return time.Now().Format("2006-01-02 15:04:05")
	case "YEAR":
		if t, ok := dateArg(vals); ok {
			return float64(t.Year())
		}
	case "MONTH":
		if t, ok := dateArg(vals); ok {
			return float64(t.Month())
		}
	case "DAY":
		if t, ok := dateArg(vals); ok {
			return float64(t.Day())
		}
	case "DATEDIFF":
		if len(vals) == 2 && vals[0] != nil && vals[1] != nil {
			ta, oka := parseDateAST(valStr(vals[0]))
			tb, okb := parseDateAST(valStr(vals[1]))
			if oka && okb {
				return float64(int(ta.Sub(tb).Hours() / 24))
			}
		}
	case "DATE_ADD", "DATE_SUB":
		return dateShiftAST(fn, vals)
	case "JSON_EXTRACT", "JSON_VALUE":
		return jsonExtractAST(args, row)
	case "ARRAY_LENGTH":
		if len(args) == 1 {
			if arr := arrayOf(args[0], row); arr != nil {
				return float64(len(arr))
			}
		}
	}
	return nil
}

// dateShiftAST mirrors the bytecode VM's dateShift: DATE_ADD / DATE_SUB shift a
// date by n of a unit (DAY when omitted), keeping the input's granularity. A
// NULL date, non-numeric n, or unparseable date yields NULL.
func dateShiftAST(fn string, vals []any) any {
	if len(vals) < 2 || len(vals) > 3 || vals[0] == nil || vals[1] == nil {
		return nil
	}
	n, ok := vals[1].(float64)
	if !ok {
		return nil
	}
	unit := "DAY"
	if len(vals) == 3 {
		if vals[2] == nil {
			return nil
		}
		unit = valStr(vals[2])
	}
	t, dateOnly, ok := parseDateFullAST(valStr(vals[0]))
	if !ok {
		return nil
	}
	k := int(n)
	if fn == "DATE_SUB" {
		k = -k
	}
	t = addUnits(t, k, unit)
	if dateOnly {
		return t.Format("2006-01-02")
	}
	return t.Format("2006-01-02 15:04:05")
}

// parseDateFullAST parses a date/datetime string and reports whether the matched
// layout was date-only, so the result is re-formatted at the same precision.
func parseDateFullAST(s string) (t time.Time, dateOnly, ok bool) {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if tt, err := time.Parse(layout, s); err == nil {
			return tt, layout == "2006-01-02", true
		}
	}
	return time.Time{}, false, false
}

// addUnits adds n of unit to t (DAY/WEEK/MONTH/YEAR/HOUR/MINUTE/SECOND).
func addUnits(t time.Time, n int, unit string) time.Time {
	switch normUnit(unit) {
	case "YEAR":
		return t.AddDate(n, 0, 0)
	case "MONTH":
		return t.AddDate(0, n, 0)
	case "WEEK":
		return t.AddDate(0, 0, 7*n)
	case "HOUR":
		return t.Add(time.Duration(n) * time.Hour)
	case "MINUTE":
		return t.Add(time.Duration(n) * time.Minute)
	case "SECOND":
		return t.Add(time.Duration(n) * time.Second)
	default: // DAY
		return t.AddDate(0, 0, n)
	}
}

// normUnit upper-cases a unit and strips a trailing plural 's' (DAYS -> DAY).
func normUnit(u string) string {
	return strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(u)), "S")
}

// strOperand returns the single string operand of a unary function; NULL or a
// wrong arity yields ok=false. A numeric operand is rendered as text.
func strOperand(vals []any) (string, bool) {
	if len(vals) != 1 || vals[0] == nil {
		return "", false
	}
	return valStr(vals[0]), true
}

// numOperand returns the single numeric operand of a unary function; a string,
// NULL, or wrong arity yields ok=false.
func numOperand(vals []any) (float64, bool) {
	if len(vals) != 1 || vals[0] == nil {
		return 0, false
	}
	f, ok := vals[0].(float64)
	return f, ok
}

// substrAST mirrors the VM's substr: SQL SUBSTRING(s, start[, length]),
// 1-indexed and rune-based, with out-of-range start/length clamped to the empty
// string. The 2-argument form returns the suffix from start to the end of the
// string (matching the bytecode VM, which lowers it with a to-the-end length).
func substrAST(vals []any) any {
	if len(vals) != 2 && len(vals) != 3 {
		return nil
	}
	if vals[0] == nil {
		return nil
	}
	start, sok := vals[1].(float64)
	if !sok {
		return nil
	}
	rs := []rune(valStr(vals[0]))
	from := int(start) - 1
	if from < 0 {
		from = 0
	}
	count := len(rs) // 2-arg SUBSTRING(s, start): to the end of the string
	if len(vals) == 3 {
		length, lok := vals[2].(float64)
		if !lok {
			return nil
		}
		count = int(length)
	}
	if from >= len(rs) || count <= 0 {
		return ""
	}
	to := from + count
	if to > len(rs) {
		to = len(rs)
	}
	return string(rs[from:to])
}

// valStr renders a normalized value (float64 or string) as text.
func valStr(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return ""
}

// compareVals compares two normalized values with the VM's semantics: numeric
// when both are numbers, otherwise lexical; a NULL operand makes it false.
func compareVals(a any, op string, b any) bool {
	if a == nil || b == nil {
		return false
	}
	if af, aok := a.(float64); aok {
		if bf, bok := b.(float64); bok {
			switch op {
			case "=":
				return af == bf
			case "!=":
				return af != bf
			case ">":
				return af > bf
			case ">=":
				return af >= bf
			case "<":
				return af < bf
			case "<=":
				return af <= bf
			}
		}
	}
	as, bs := valStr(a), valStr(b)
	switch op {
	case "=":
		return as == bs
	case "!=":
		return as != bs
	case ">":
		return as > bs
	case ">=":
		return as >= bs
	case "<":
		return as < bs
	case "<=":
		return as <= bs
	}
	return false
}

// dateArg parses the single date operand of YEAR / MONTH / DAY.
func dateArg(vals []any) (time.Time, bool) {
	if len(vals) != 1 || vals[0] == nil {
		return time.Time{}, false
	}
	return parseDateAST(valStr(vals[0]))
}

// parseDateAST parses a date/datetime string, mirroring the bytecode VM's parseDate.
func parseDateAST(s string) (time.Time, bool) {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// evalPred evaluates a boolean-valued array predicate (ARRAY_CONTAINS /
// ARRAY_INTERSECT), mirroring the bytecode VM. Array operands must be field
// references.
func evalPred(fn string, args []ir.Term, row map[string]any) bool {
	if len(args) < 1 {
		return false
	}
	arr := arrayOf(args[0], row)
	if arr == nil {
		return false
	}
	if fn == "ARRAY_CONTAINS" && len(args) == 2 {
		return sliceContains(arr, valStr(evalTerm(args[1], row)))
	}
	if fn == "ARRAY_INTERSECT" && len(args) == 2 {
		return sliceIntersect(arr, arrayOf(args[1], row))
	}
	return false
}

// arrayOf reads the array referenced by a field term, or nil when t is not a
// field or the field does not hold an array.
func arrayOf(t ir.Term, row map[string]any) []string {
	ft, ok := t.(ir.FieldTerm)
	if !ok {
		return nil
	}
	return toStringSlice(row[ft.Name])
}

// toStringSlice renders an array field value as []string, mirroring the VM's
// toValue (each element as text). Non-array values yield nil.
func toStringSlice(raw any) []string {
	switch x := raw.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, len(x))
		for i, e := range x {
			out[i] = valStr(termVal(e))
		}
		return out
	}
	return nil
}

func sliceContains(arr []string, v string) bool {
	for _, e := range arr {
		if e == v {
			return true
		}
	}
	return false
}

// sliceIntersect reports whether two string slices share at least one element
// (non-empty set intersection). An empty operand yields false, mirroring the
// bytecode VM's arrIntersect.
func sliceIntersect(a, b []string) bool {
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(a))
	for _, e := range a {
		set[e] = struct{}{}
	}
	for _, e := range b {
		if _, ok := set[e]; ok {
			return true
		}
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
