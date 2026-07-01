package vm

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// stackMax bounds the evaluation stack depth. Boolean rule expressions are
// shallow once compiled to postfix, so this is generous; on overflow Eval
// returns false rather than panicking.
const stackMax = 256

// Eval runs the program against a row and returns its boolean result.
//
// The stack is a fixed-size local array, so Eval allocates nothing and is safe
// for concurrent use from many goroutines (each call has its own stack). This is
// what lets the worker pool share one *Program across all workers.
func (p *Program) Eval(row map[string]any) bool {
	var st [stackMax]Value
	sp := 0

	for i := 0; i < len(p.Code); i++ {
		in := p.Code[i]
		switch in.Op {

		case OpLoadField, OpConstNum, OpConstStr, OpCurrentDate, OpCurrentTs: // push one value
			if sp >= stackMax {
				return false
			}
			st[sp] = p.load(in, row)
			sp++

		case OpBetween: // pop hi,lo,x -> push lo <= x <= hi
			if sp < 3 {
				return false
			}
			x, lo, hi := st[sp-3], st[sp-2], st[sp-1]
			sp -= 2
			st[sp-1] = boolV(x.k == kNum && lo.k == kNum && hi.k == kNum && lo.n <= x.n && x.n <= hi.n)

		case OpEq, OpNe, OpGt, OpGe, OpLt, OpLe, OpAnd, OpOr: // pop b,a -> push result
			if sp < 2 {
				return false
			}
			a, b := st[sp-2], st[sp-1]
			sp--
			st[sp-1] = boolV(binaryOp(in.Op, a, b))

		case OpIn, OpLikePrefix, OpLikeSuffix, OpLikeContains, OpLikeEq, OpIsNull, OpIsNotNull, OpNot, OpRegexp: // pop x -> push result
			if sp < 1 {
				return false
			}
			st[sp-1] = boolV(p.unaryOp(in, st[sp-1]))

		case OpUpper, OpLower, OpTrim, OpLength, OpAbs, OpRound, OpCeil, OpFloor, OpYear, OpMonth, OpDay, OpArrLen: // pop x -> push computed value
			if sp < 1 {
				return false
			}
			st[sp-1] = callValue(in.Op, st[sp-1])

		case OpSubstr: // pop s,start,len -> push substring
			if sp < 3 {
				return false
			}
			res := substr(st[sp-3], st[sp-2], st[sp-1])
			sp -= 2
			st[sp-1] = res

		case OpDateDiff: // pop a,b -> push whole days (a - b)
			if sp < 2 {
				return false
			}
			d := dateDiff(st[sp-2], st[sp-1])
			sp--
			st[sp-1] = d

		case OpArrContains: // pop arr,value -> bool
			if sp < 2 {
				return false
			}
			st[sp-2] = boolV(arrContains(st[sp-2], st[sp-1]))
			sp--

		case OpArrIntersect: // pop arr1,arr2 -> bool (non-empty intersection)
			if sp < 2 {
				return false
			}
			st[sp-2] = boolV(arrIntersect(st[sp-2], st[sp-1]))
			sp--

		case OpRound2: // pop x,d -> round x to d decimal places
			if sp < 2 {
				return false
			}
			st[sp-2] = round2(st[sp-2], st[sp-1])
			sp--

		case OpDateAdd2, OpDateSub2: // pop n,date -> date shifted by whole days
			if sp < 2 {
				return false
			}
			res := dateShift(st[sp-2], st[sp-1], "DAY", in.Op == OpDateSub2)
			sp--
			st[sp-1] = res

		case OpDateAdd3, OpDateSub3: // pop unit,n,date -> date shifted by n units
			if sp < 3 {
				return false
			}
			res := dateShift(st[sp-3], st[sp-2], st[sp-1].asString(), in.Op == OpDateSub3)
			sp -= 2
			st[sp-1] = res

		case OpQuantArr: // pop arr,left -> left op ANY/ALL of elements
			if sp < 2 {
				return false
			}
			res := quantArr(st[sp-2], st[sp-1], in.A)
			sp--
			st[sp-1] = boolV(res)

		case OpExistsSub: // push EXISTS(collection)
			if sp >= stackMax {
				return false
			}
			st[sp] = boolV(p.existsSub(in.A, row))
			sp++

		case OpQuantSub: // pop left -> left op ANY/ALL of projected column
			if sp < 1 {
				return false
			}
			st[sp-1] = boolV(p.quantSub(in.A, st[sp-1], row))

		case OpJSONField: // push JSON scalar extracted from a field document
			if sp >= stackMax {
				return false
			}
			jo := p.JSONs[in.A]
			st[sp] = jsonExtract(row[p.Fields[jo.FieldIdx]], jo.Path)
			sp++

		case OpJSONExpr: // pop doc-string -> push JSON scalar
			if sp < 1 {
				return false
			}
			jo := p.JSONs[in.A]
			st[sp-1] = jsonExtract(st[sp-1].asString(), jo.Path)

		case OpAggSub: // push aggregate scalar over a collection
			if sp >= stackMax {
				return false
			}
			st[sp] = p.aggSub(in.A, row)
			sp++

		default:
			return false
		}
	}

	if sp != 1 || st[0].k != kBool {
		return false
	}
	return st[0].b
}

// load produces the value pushed by a load/const opcode (OpLoadField,
// OpConstNum, OpConstStr). A missing field yields undef.
func (p *Program) load(in Instr, row map[string]any) Value {
	switch in.Op {
	case OpConstNum:
		return numV(p.Nums[in.A])
	case OpConstStr:
		return strV(p.Strs[in.A])
	case OpCurrentDate:
		return strV(time.Now().Format(dateLayout))
	case OpCurrentTs:
		return strV(time.Now().Format(tsLayout))
	default: // OpLoadField
		if raw, ok := row[p.Fields[in.A]]; ok {
			return toValue(raw)
		}
		return undef
	}
}

// binaryOp evaluates a two-operand opcode: the boolean combinators AND/OR, with
// every comparison delegated to compare.
func binaryOp(op OpCode, a, b Value) bool {
	switch op {
	case OpAnd:
		return a.b && b.b
	case OpOr:
		return a.b || b.b
	default:
		return compare(a, b, op)
	}
}

// unaryOp evaluates a single-operand opcode (set membership, the LIKE variants,
// the IS [NOT] NULL pair, and NOT) against the top-of-stack value x.
func (p *Program) unaryOp(in Instr, x Value) bool {
	switch in.Op {
	case OpIn:
		_, ok := p.Sets[in.A][x.asString()]
		return ok && x.k != kUndef
	case OpLikePrefix:
		return x.k != kUndef && strings.HasPrefix(x.asString(), p.Strs[in.A])
	case OpLikeSuffix:
		return x.k != kUndef && strings.HasSuffix(x.asString(), p.Strs[in.A])
	case OpLikeContains:
		return x.k != kUndef && strings.Contains(x.asString(), p.Strs[in.A])
	case OpLikeEq:
		return x.k != kUndef && x.asString() == p.Strs[in.A]
	case OpIsNull:
		return x.k == kUndef
	case OpIsNotNull:
		return x.k != kUndef
	case OpNot:
		// Negate a boolean result; anything non-boolean is treated as false
		// (so !non-bool stays false rather than silently becoming true).
		return x.k == kBool && !x.b
	case OpRegexp:
		return x.k != kUndef && p.Regexps[in.A].MatchString(x.asString())
	default:
		return false
	}
}

// compare evaluates an ordering/equality opcode on two values.
func compare(a, b Value, op OpCode) bool {
	if a.k == kUndef || b.k == kUndef {
		return false
	}
	switch op {
	case OpEq:
		return eq(a, b)
	case OpNe:
		return !eq(a, b)
	}
	// Ordering: prefer numeric comparison; fall back to lexical.
	if a.k == kNum && b.k == kNum {
		switch op {
		case OpGt:
			return a.n > b.n
		case OpGe:
			return a.n >= b.n
		case OpLt:
			return a.n < b.n
		case OpLe:
			return a.n <= b.n
		}
	}
	as, bs := a.asString(), b.asString()
	switch op {
	case OpGt:
		return as > bs
	case OpGe:
		return as >= bs
	case OpLt:
		return as < bs
	case OpLe:
		return as <= bs
	}
	return false
}

// callValue applies a single-operand scalar function to x. A NULL/undefined
// input propagates as undef, and the math functions require a numeric operand.
func callValue(op OpCode, x Value) Value {
	if x.k == kUndef {
		return undef
	}
	switch op {
	case OpUpper:
		return strV(strings.ToUpper(x.asString()))
	case OpLower:
		return strV(strings.ToLower(x.asString()))
	case OpTrim:
		return strV(strings.TrimSpace(x.asString()))
	case OpLength:
		return numV(float64(utf8.RuneCountInString(x.asString())))
	case OpAbs:
		if x.k != kNum {
			return undef
		}
		return numV(math.Abs(x.n))
	case OpRound:
		if x.k != kNum {
			return undef
		}
		return numV(math.Round(x.n))
	case OpCeil:
		if x.k != kNum {
			return undef
		}
		return numV(math.Ceil(x.n))
	case OpFloor:
		if x.k != kNum {
			return undef
		}
		return numV(math.Floor(x.n))
	case OpYear:
		if t, ok := parseDate(x.asString()); ok {
			return numV(float64(t.Year()))
		}
		return undef
	case OpMonth:
		if t, ok := parseDate(x.asString()); ok {
			return numV(float64(t.Month()))
		}
		return undef
	case OpDay:
		if t, ok := parseDate(x.asString()); ok {
			return numV(float64(t.Day()))
		}
		return undef
	case OpArrLen:
		if x.k != kArr {
			return undef
		}
		return numV(float64(len(x.arr)))
	default:
		return undef
	}
}

// substr implements SQL SUBSTRING(s, start, length): 1-indexed and rune-based,
// with out-of-range start/length clamped to the empty string. A NULL input or
// non-numeric start/length yields undef.
func substr(s, start, length Value) Value {
	if s.k == kUndef || start.k != kNum || length.k != kNum {
		return undef
	}
	rs := []rune(s.asString())
	from := int(start.n) - 1 // SQL SUBSTRING is 1-indexed
	count := int(length.n)
	if from < 0 {
		from = 0
	}
	if from >= len(rs) || count <= 0 {
		return strV("")
	}
	to := from + count
	if to > len(rs) {
		to = len(rs)
	}
	return strV(string(rs[from:to]))
}

const (
	dateLayout = "2006-01-02"
	tsLayout   = "2006-01-02 15:04:05"
)

// dateLayouts are tried in order when parsing date / datetime strings.
var dateLayouts = []string{
	tsLayout,
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	dateLayout,
}

// parseDate parses a date/datetime string with the supported layouts.
func parseDate(s string) (time.Time, bool) {
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// dateDiff returns the whole days between two date values (a - b), or undef when
// either is NULL or cannot be parsed.
func dateDiff(a, b Value) Value {
	if a.k == kUndef || b.k == kUndef {
		return undef
	}
	ta, oka := parseDate(a.asString())
	tb, okb := parseDate(b.asString())
	if !oka || !okb {
		return undef
	}
	return numV(float64(int(ta.Sub(tb).Hours() / 24)))
}

// arrContains reports whether v (rendered as text) is an element of array arr.
func arrContains(arr, v Value) bool {
	if arr.k != kArr {
		return false
	}
	vs := v.asString()
	for _, e := range arr.arr {
		if e == vs {
			return true
		}
	}
	return false
}

// arrIntersect reports whether two array values share at least one element
// (their set intersection is non-empty). Elements are compared as text, so it
// works across []string / []any fields. A non-array or empty operand yields
// false (nothing to intersect).
func arrIntersect(a, b Value) bool {
	if a.k != kArr || b.k != kArr || len(a.arr) == 0 || len(b.arr) == 0 {
		return false
	}
	set := make(map[string]struct{}, len(a.arr))
	for _, e := range a.arr {
		set[e] = struct{}{}
	}
	for _, e := range b.arr {
		if _, ok := set[e]; ok {
			return true
		}
	}
	return false
}

// round2 implements ROUND(x, d): round x to d decimal places (d may be negative
// to round to tens/hundreds). Both operands must be numeric; a NULL or
// non-numeric operand yields undef. Rounding is half-away-from-zero (math.Round),
// matching single-argument ROUND.
func round2(x, d Value) Value {
	if x.k != kNum || d.k != kNum {
		return undef
	}
	pow := math.Pow(10, d.n)
	if pow == 0 || math.IsInf(pow, 0) {
		return undef
	}
	return numV(math.Round(x.n*pow) / pow)
}

// parseDateFull parses a date/datetime string and reports whether the matched
// layout was date-only, so the result can be re-formatted at the same precision.
func parseDateFull(s string) (t time.Time, dateOnly, ok bool) {
	for _, layout := range dateLayouts {
		if tt, err := time.Parse(layout, s); err == nil {
			return tt, layout == dateLayout, true
		}
	}
	return time.Time{}, false, false
}

// dateShift implements DATE_ADD / DATE_SUB: it shifts a date value by n of unit
// (DAY when unspecified). A NULL date, non-numeric n, or unparseable date yields
// undef; the result keeps the input's granularity (date vs datetime).
func dateShift(dateV, nV Value, unit string, sub bool) Value {
	if dateV.k == kUndef || nV.k != kNum {
		return undef
	}
	t, dateOnly, ok := parseDateFull(dateV.asString())
	if !ok {
		return undef
	}
	n := int(nV.n)
	if sub {
		n = -n
	}
	t = addUnits(t, n, unit)
	layout := tsLayout
	if dateOnly {
		layout = dateLayout
	}
	return strV(t.Format(layout))
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

// quantArr evaluates `left <op> ANY/ALL (arr)`. The comparison op and ALL flag
// are packed in a (op = a>>1, all = a&1). Elements compare numerically when left
// is numeric and the element parses as a number, else lexically. An empty or
// non-array operand makes ANY false and ALL true (SQL semantics).
func quantArr(left, arr Value, a int32) bool {
	op := OpCode(a >> 1)
	all := a&1 == 1
	if arr.k != kArr || len(arr.arr) == 0 {
		return all
	}
	for _, e := range arr.arr {
		match := compare(left, elemValue(left, e), op)
		if all && !match {
			return false
		}
		if !all && match {
			return true
		}
	}
	return all
}

// elemValue renders an array element as a number when left is numeric and the
// element parses as one, otherwise as a string.
func elemValue(left Value, e string) Value {
	if left.k == kNum {
		if f, err := strconv.ParseFloat(e, 64); err == nil {
			return numV(f)
		}
	}
	return strV(e)
}

// existsSub evaluates EXISTS over a collection field. Without a WHERE it is a
// non-empty test (scalar or nested-object arrays); with a WHERE sub-program it
// is true when at least one nested row satisfies it.
func (p *Program) existsSub(idx int32, row map[string]any) bool {
	s := p.Subs[idx]
	if s.Where == nil {
		return collLen(row[s.Coll]) > 0
	}
	for _, nr := range asRows(row[s.Coll]) {
		if s.Where.Eval(nr) {
			return true
		}
	}
	return false
}

// quantSub evaluates `left <op> ANY/ALL (SELECT col FROM coll [WHERE pred])`,
// comparing left against the projected col of each nested row that passes the
// optional predicate. ANY is ∃, ALL is ∀ (vacuously true over an empty set).
func (p *Program) quantSub(idx int32, left Value, row map[string]any) bool {
	s := p.Subs[idx]
	for _, nr := range asRows(row[s.Coll]) {
		if s.Where != nil && !s.Where.Eval(nr) {
			continue
		}
		ok := compare(left, toValue(nr[s.Col]), s.Op)
		if s.All && !ok {
			return false
		}
		if !s.All && ok {
			return true
		}
	}
	return s.All
}

// aggSub evaluates a scalar aggregate sub-query (COUNT/SUM/MIN/MAX/AVG) over the
// nested rows of a collection, filtered by an optional WHERE sub-program.
// COUNT(*) counts qualifying rows; COUNT(col) counts non-null col; SUM/MIN/MAX/
// AVG run over numeric col values and return NULL when none qualify.
func (p *Program) aggSub(idx int32, row map[string]any) Value {
	a := p.Aggs[idx]
	n := 0 // qualifying rows (COUNT) or numeric values (SUM/MIN/MAX/AVG)
	var sum, mn, mx float64
	for _, nr := range asRows(row[a.Coll]) {
		if a.Where != nil && !a.Where.Eval(nr) {
			continue
		}
		if a.Fn == "COUNT" {
			if a.Col == "" {
				n++
			} else if v, ok := nr[a.Col]; ok && v != nil {
				n++
			}
			continue
		}
		cv := toValue(nr[a.Col])
		if cv.k != kNum {
			continue
		}
		if n == 0 {
			mn, mx = cv.n, cv.n
		} else {
			if cv.n < mn {
				mn = cv.n
			}
			if cv.n > mx {
				mx = cv.n
			}
		}
		sum += cv.n
		n++
	}
	switch a.Fn {
	case "COUNT":
		return numV(float64(n))
	case "SUM":
		if n == 0 {
			return undef
		}
		return numV(sum)
	case "AVG":
		if n == 0 {
			return undef
		}
		return numV(sum / float64(n))
	case "MIN":
		if n == 0 {
			return undef
		}
		return numV(mn)
	case "MAX":
		if n == 0 {
			return undef
		}
		return numV(mx)
	}
	return undef
}

// asRows coerces a collection field into nested rows for sub-query evaluation;
// only object elements are kept, so scalar arrays yield no rows.
func asRows(raw any) []map[string]any {
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

// collLen reports the element count of a collection field (scalar or object
// array), or 0 when the field is absent or not a slice.
func collLen(raw any) int {
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

// jsonExtract implements JSON_EXTRACT / JSON_VALUE: it reads raw as a JSON
// document (a JSON string is parsed; an already-decoded map/slice is used as-is),
// follows the `$.a.b[0]` path, and returns the leaf scalar as a VM value. A
// missing path, a non-JSON document, or a non-scalar leaf yields undef (NULL).
func jsonExtract(raw any, path string) Value {
	root := jsonRoot(raw)
	if root == nil {
		return undef
	}
	cur, ok := navigateJSON(root, path)
	if !ok {
		return undef
	}
	return jsonScalar(cur)
}

// jsonRoot returns a navigable JSON value: it parses a JSON string, passes a
// pre-decoded object/array through, and rejects anything else.
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

// jsonScalar converts a JSON leaf into a VM value. Booleans render as the
// strings "true"/"false" (the engine has no boolean literal); every numeric kind
// (incl. json.Number from a decoder, or int from a pre-built object) becomes a
// number; objects, arrays and null become undef.
func jsonScalar(v any) Value {
	switch x := v.(type) {
	case bool:
		if x {
			return strV("true")
		}
		return strV("false")
	case json.Number:
		if f, err := x.Float64(); err == nil {
			return numV(f)
		}
		return undef
	case map[string]any, []any, []string, nil:
		return undef
	default:
		return toValue(v) // string -> strV, any numeric -> numV
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

// parseJSONPath parses a `$`-rooted accessor into segments. It supports dotted
// keys (`$.a.b`), array indices (`$[0]`, `$.a[2]`) and bracket-quoted keys
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
