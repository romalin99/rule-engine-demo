package vm

import (
	"encoding/json"
	"math"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"tcg-rulex-engine/pkg/sqlfn"
)

// stackMax bounds the evaluation stack depth. Boolean rule expressions are
// shallow once compiled to postfix, so this is generous; on overflow Eval
// returns false rather than panicking.
const stackMax = 256

// Eval runs the program against a row and returns its boolean result.
//
// The stack is a fixed-size local array, so Eval allocates nothing (except for
// extended-builtin calls — OpCallB boxes its arguments) and is safe for
// concurrent use from many goroutines (each call has its own stack). This is
// what lets the worker pool share one *Program across all workers.
func (p *Program) Eval(row map[string]any) bool {
	var st [stackMax]Value
	sp := 0

	for i := 0; i < len(p.Code); i++ {
		in := p.Code[i]
		switch in.Op {

		case OpLoadField, OpConstNum, OpConstStr, OpCurrentDate, OpCurrentTs,
			OpExistsSub, OpJSONField, OpAggSub, OpMatchPre, OpMapKeys, OpMapVals,
			OpJmesField: // push one value (loads, consts and row-backed factors)
			if sp >= stackMax {
				return false
			}
			st[sp] = p.pushValue(in, row)
			sp++

		case OpEq, OpNe, OpGt, OpGe, OpLt, OpLe, OpAnd, OpOr: // pop b,a -> push bool
			if sp < 2 {
				return false
			}
			a, b := st[sp-2], st[sp-1]
			sp--
			st[sp-1] = boolV(binaryOp(in.Op, a, b))

		case OpIn, OpLikePrefix, OpLikeSuffix, OpLikeContains, OpLikeEq, OpIsNull, OpIsNotNull, OpNot, OpRegexp: // pop x -> push bool
			if sp < 1 {
				return false
			}
			st[sp-1] = boolV(p.unaryOp(in, st[sp-1]))

		case OpUpper, OpLower, OpTrim, OpLength, OpAbs, OpRound, OpCeil, OpFloor,
			OpYear, OpMonth, OpDay, OpArrLen: // pop x -> push computed value
			if sp < 1 {
				return false
			}
			st[sp-1] = callValue(in.Op, st[sp-1])

		case OpQuantSub, OpJSONExpr, OpJmesExpr: // pop x -> push computed value (pool-backed)
			if sp < 1 {
				return false
			}
			st[sp-1] = p.replaceValue(in, st[sp-1], row)

		case OpDateDiff, OpArrContains, OpArrIntersect, OpRound2, OpDateAdd2,
			OpDateSub2, OpQuantArr: // pop b,a -> push f(a, b)
			if sp < 2 {
				return false
			}
			res := binaryValue(in, st[sp-2], st[sp-1])
			sp--
			st[sp-1] = res

		case OpBetween, OpSubstr, OpDateAdd3, OpDateSub3: // pop c,b,a -> push f(a, b, c)
			if sp < 3 {
				return false
			}
			res := ternaryValue(in.Op, st[sp-3], st[sp-2], st[sp-1])
			sp -= 2
			st[sp-1] = res

		case OpCallB: // pop argc args -> push extended-builtin result
			id, argc := in.A>>8, int(in.A&0xff)
			b := sqlfn.ByID(id)
			// The last term guards the argc == 0 push against a full stack; it
			// is only evaluated once sp >= argc, so sp-argc is non-negative.
			if b == nil || sp < argc || sp-argc >= stackMax {
				return false
			}
			args := make([]any, argc)
			for j := range argc {
				args[j] = valueAny(st[sp-argc+j])
			}
			sp -= argc
			st[sp] = toValue(b.Fn(args))
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

// pushValue produces the value for every no-pop opcode: field loads and
// constants (via load) plus the row-backed factors — EXISTS, JSON / JMESPath
// field access, aggregate sub-queries, MATCH and MAPKEYS / MAPVALUES.
func (p *Program) pushValue(in Instr, row map[string]any) Value {
	switch in.Op {
	case OpExistsSub:
		return boolV(p.existsSub(in.A, row))
	case OpJSONField:
		jo := p.JSONs[in.A]
		return jsonExtract(row[p.Fields[jo.FieldIdx]], jo.Path)
	case OpAggSub:
		return p.aggSub(in.A, row)
	case OpMatchPre:
		return boolV(matchPrefix(row, p.Strs[in.A]))
	case OpMapKeys, OpMapVals:
		return mapParts(row[p.Fields[in.A]], in.Op == OpMapVals)
	case OpJmesField:
		jo := p.JSONs[in.A]
		return toValue(sqlfn.JmesEval(row[p.Fields[jo.FieldIdx]], jo.Path))
	default: // OpLoadField, OpConstNum, OpConstStr, OpCurrentDate, OpCurrentTs
		return p.load(in, row)
	}
}

// replaceValue computes the pool-backed single-operand opcodes that replace
// the top of stack: quantified sub-queries and the JSON / JMESPath document
// operators.
func (p *Program) replaceValue(in Instr, x Value, row map[string]any) Value {
	switch in.Op {
	case OpQuantSub: // left op ANY/ALL of a projected sub-query column
		return boolV(p.quantSub(in.A, x, row))
	case OpJSONExpr: // JSON scalar from a computed document string
		return jsonExtract(x.asString(), p.JSONs[in.A].Path)
	default: // OpJmesExpr: JMESPath result from a computed document string
		return toValue(sqlfn.JmesEval(x.asString(), p.JSONs[in.A].Path))
	}
}

// binaryValue computes a two-operand value opcode (pop b, a; push f(a, b)).
func binaryValue(in Instr, a, b Value) Value {
	switch in.Op {
	case OpDateDiff: // whole days (a - b)
		return dateDiff(a, b)
	case OpArrContains: // b is an element of array a
		return boolV(arrContains(a, b))
	case OpArrIntersect: // arrays a and b share an element
		return boolV(arrIntersect(a, b))
	case OpRound2: // round a to b decimal places
		return round2(a, b)
	case OpQuantArr: // a op ANY/ALL of array b (op and ALL packed in in.A)
		return boolV(quantArr(a, b, in.A))
	default: // OpDateAdd2, OpDateSub2: date a shifted by b whole days
		return dateShift(a, b, "DAY", in.Op == OpDateSub2)
	}
}

// ternaryValue computes a three-operand value opcode (pop c, b, a; push
// f(a, b, c)).
func ternaryValue(op OpCode, a, b, c Value) Value {
	switch op {
	case OpBetween: // b <= a <= c (numeric)
		return boolV(a.k == kNum && b.k == kNum && c.k == kNum && b.n <= a.n && a.n <= c.n)
	case OpSubstr: // SUBSTRING(a, b, c)
		return substr(a, b, c)
	default: // OpDateAdd3, OpDateSub3: date a shifted by b units of c
		return dateShift(a, b, c.asString(), op == OpDateSub3)
	}
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
	// NULL never matches, and neither does an ARRAY or OPAQUE operand (they
	// are not scalar text; asString would render "" and let `tags IN ('')`,
	// `tags LIKE '%'` or `tags REGEXP '^'` match — see compare). IS [NOT]
	// NULL below intentionally still sees arrays/objects as present values.
	scalar := scalarKind(x.k)
	switch in.Op {
	case OpIn:
		_, ok := p.Sets[in.A][x.asString()]
		return ok && scalar
	case OpLikePrefix:
		return scalar && strings.HasPrefix(x.asString(), p.Strs[in.A])
	case OpLikeSuffix:
		return scalar && strings.HasSuffix(x.asString(), p.Strs[in.A])
	case OpLikeContains:
		return scalar && strings.Contains(x.asString(), p.Strs[in.A])
	case OpLikeEq:
		return scalar && x.asString() == p.Strs[in.A]
	case OpIsNull:
		return x.k == kUndef
	case OpIsNotNull:
		return x.k != kUndef
	case OpNot:
		// Negate a boolean result; anything non-boolean is treated as false
		// (so !non-bool stays false rather than silently becoming true).
		return x.k == kBool && !x.b
	case OpRegexp:
		return scalar && p.Regexps[in.A].MatchString(x.asString())
	default:
		return false
	}
}

// compare evaluates an ordering/equality opcode on two values.
//
// An ARRAY operand is treated like NULL: an array is not a scalar, so no
// comparison operator matches it (round thirteen). Without this guard the
// string fallback rendered kArr as "" — making `tags = other_tags` true for
// ANY two array fields, `name = tags` true for an empty-string name, and
// `tags REGEXP '^'`-style matches succeed — while the AST runtime (termVal
// nils arrays) said false. Array semantics live in the ARRAY_* predicates
// and the ANY/ALL quantifiers.
func compare(a, b Value, op OpCode) bool {
	if !scalarKind(a.k) || !scalarKind(b.k) {
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

// scalarKind reports whether a value kind carries scalar text/number/boolean
// content. kUndef (NULL), kArr (arrays) and kOpaque (objects / typed nested
// collections) are excluded: no comparison, IN, LIKE or REGEXP matches them.
func scalarKind(k vkind) bool {
	return k == kNum || k == kStr || k == kBool
}

// callValue applies a single-operand scalar function to x. A NULL/undefined
// input propagates as undef, and the math functions require a numeric operand.
func callValue(op OpCode, x Value) Value {
	if x.k == kUndef || x.k == kOpaque {
		return undef
	}
	// Scalar functions have no meaning for an array operand: NULL, matching
	// the AST runtime. LENGTH counts elements and OpArrLen requires an array.
	if x.k == kArr && op != OpLength && op != OpArrLen {
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
		// LENGTH of an array is its element count (qlbridge len semantics and
		// the same answer as ARRAY_LENGTH); of anything else, the rune count.
		if x.k == kArr {
			return numV(float64(len(x.arr)))
		}
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

// matchPrefix reports whether any row field whose name starts with prefix
// holds a non-null value (the MATCH predicate; qlbridge exists(match("k_"))).
func matchPrefix(row map[string]any, prefix string) bool {
	for k, v := range row {
		if v != nil && strings.HasPrefix(k, prefix) {
			return true
		}
	}
	return false
}

// mapParts renders a map field (a map[string]any or a JSON-object string) as
// an array value: its sorted keys, or its values ordered by sorted key
// (MAPKEYS / MAPVALUES). Keys are sorted because Go map iteration order is
// random — rule results must be deterministic. Non-map input yields undef.
func mapParts(raw any, wantValues bool) Value {
	m, ok := jsonRoot(raw).(map[string]any)
	if !ok {
		return undef
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if !wantValues {
		return arrV(keys)
	}
	vals := make([]string, len(keys))
	for i, k := range keys {
		vals[i] = toValue(m[k]).asString()
	}
	return arrV(vals)
}

// valueAny converts a VM stack value to the extended-builtin value model
// (pkg/sqlfn): nil / float64 / string / bool / []string.
func valueAny(v Value) any {
	switch v.k {
	case kNum:
		return v.n
	case kStr:
		return v.s
	case kBool:
		return v.b
	case kArr:
		return v.arr
	default:
		return nil
	}
}

// substr implements SQL SUBSTRING(s, start, length): 1-indexed and rune-based,
// with out-of-range start/length clamped to the empty string. A NULL input or
// non-numeric start/length yields undef.
func substr(s, start, length Value) Value {
	if !scalarKind(s.k) || start.k != kNum || length.k != kNum {
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
	// Clamp count BEFORE adding: with a near-MaxInt64 literal length,
	// from+count would overflow to a negative slice bound and panic — and a
	// panic inside Eval kills the whole worker process. After the clamp,
	// from+count <= len(rs) by construction.
	if count > len(rs)-from {
		count = len(rs) - from
	}
	return strV(string(rs[from : from+count]))
}

const (
	dateLayout = "2006-01-02"
	tsLayout   = "2006-01-02 15:04:05"
	// Slash variants (round eight): accepted by the core date opcodes so
	// YEAR/DATEDIFF/DATE_ADD/… agree with the extended date builtins
	// (pkg/sqlfn MM/DAYOFWEEK/TODATE/…) on which strings are dates.
	dateSlashLayout = "2006/01/02"
	tsSlashLayout   = "2006/01/02 15:04:05"
)

// dateLayouts are tried in order when parsing date / datetime strings. It
// mirrors pkg/sqlfn's list; pkg/runtime/ast carries the same set.
var dateLayouts = []string{
	tsLayout,
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	dateLayout,
	tsSlashLayout,
	dateSlashLayout,
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
	return slices.Contains(arr.arr, vs)
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
			return tt, layout == dateLayout || layout == dateSlashLayout, true
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
	return slices.ContainsFunc(asRows(row[s.Coll]), s.Where.Eval)
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
