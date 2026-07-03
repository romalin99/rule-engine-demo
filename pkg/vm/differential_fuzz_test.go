// differential_fuzz_test.go — round twelve: seeded differential fuzzing of the
// two runtimes plus the SQL emit round trip.
//
// Rationale: across eleven audit rounds, the single most recurrent bug class
// was BYTECODE-VM ↔ AST-RUNTIME divergence (NULL LIKE/IN, boolean terms,
// LENGTH(array), computed JSON docs, …) and EMIT round-trip drift. This test
// generates thousands of random-but-valid rules over the full documented
// grammar and asserts, for every rule and row:
//
//	vm.Compile(rule).Eval(row) == astRuntime.Execute(rule, row)      (parity)
//	eval(rule) == eval(ir.Parse(ir.Emit(rule, SQL)))                 (round trip)
//
// The generator is deterministic (fixed seed): a failure prints the iteration,
// rule text, emitted SQL and row for exact reproduction. Every template below
// is parse- and compile-clean by construction, so any Fatal here is a real
// engine bug, not fuzz noise.
//
// Run: go test ./pkg/vm/ -run TestDifferentialFuzz -v
package vm

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	"tcg-rulex-engine/pkg/ir"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

// ---- literal / fragment pools (all safe inside single-quoted SQL) ----------

var fzWords = []string{"a", "b", "ab", "x9", "深圳", "数码", "vip", "", "50%", "a_b"}
var fzFields = []string{"name", "city", "age", "score", "rate", "flag", "reg_date", "csv", "extra"}
var fzArrFields = []string{"tags", "nums"}
var fzRegexps = []string{`^a`, `[0-9]+$`, `^1[3-9]\d{9}$`, `(?i)ab`, `a.c`, `深`}
var fzJSONPaths = []string{`$.city`, `$.addr.zip`, `$.nope`}
var fzLikeParts = []string{"a", "b", "深", "%", "_", `\%`, `\_`, "9"}
var fzCmps = []string{"=", "!=", ">", ">=", "<", "<="}
var fzUnary = []string{"LOWER", "UPPER", "TRIM", "LENGTH", "ABS", "CEIL", "FLOOR", "YEAR", "MONTH", "DAY"}

type fzGen struct{ r *rand.Rand }

func (g *fzGen) pick(ss []string) string { return ss[g.r.Intn(len(ss))] }
func (g *fzGen) num() string             { return fmt.Sprintf("%d", g.r.Intn(201)-100) }
func (g *fzGen) flt() string             { return fmt.Sprintf("%d.%d", g.r.Intn(20)-10, g.r.Intn(100)) }
func (g *fzGen) str() string             { return "'" + g.pick(fzWords) + "'" }

func (g *fzGen) likePattern() string {
	n := 1 + g.r.Intn(4)
	out := ""
	for i := 0; i < n; i++ {
		out += g.pick(fzLikeParts)
	}
	return out
}

// leaf emits one parse+compile-clean predicate.
func (g *fzGen) leaf() string {
	switch g.r.Intn(25) {
	case 0: // field cmp literal (number or string)
		if g.r.Intn(2) == 0 {
			return g.pick(fzFields) + " " + g.pick(fzCmps) + " " + g.num()
		}
		return g.pick(fzFields) + " " + g.pick(fzCmps) + " " + g.str()
	case 1: // numeric BETWEEN (same-typed bounds; order deliberately random)
		return g.pick(fzFields) + " BETWEEN " + g.num() + " AND " + g.num()
	case 2: // string BETWEEN (desugars to >= AND <=)
		return g.pick(fzFields) + " BETWEEN " + g.str() + " AND " + g.str()
	case 3: // IN / NOT IN with 1-3 mixed literals
		not := ""
		if g.r.Intn(2) == 0 {
			not = "NOT "
		}
		n := 1 + g.r.Intn(3)
		list := ""
		for i := 0; i < n; i++ {
			if i > 0 {
				list += ", "
			}
			if g.r.Intn(2) == 0 {
				list += g.num()
			} else {
				list += g.str()
			}
		}
		return g.pick(fzFields) + " " + not + "IN (" + list + ")"
	case 4: // LIKE / NOT LIKE with full wildcard grammar
		not := ""
		if g.r.Intn(2) == 0 {
			not = "NOT "
		}
		return g.pick(fzFields) + " " + not + "LIKE '" + g.likePattern() + "'"
	case 5: // IS [NOT] NULL
		if g.r.Intn(2) == 0 {
			return g.pick(fzFields) + " IS NULL"
		}
		return g.pick(fzFields) + " IS NOT NULL"
	case 6: // unary core function comparison
		return g.pick(fzUnary) + "(" + g.pick(fzFields) + ") " + g.pick(fzCmps) + " " + g.num()
	case 7: // ROUND(x, d) and SUBSTRING forms
		if g.r.Intn(2) == 0 {
			return "ROUND(" + g.pick(fzFields) + ", " + fmt.Sprintf("%d", g.r.Intn(6)-2) + ") >= " + g.flt()
		}
		if g.r.Intn(2) == 0 {
			return "SUBSTRING(" + g.pick(fzFields) + ", " + fmt.Sprintf("%d", g.r.Intn(5)-1) + ") = " + g.str()
		}
		return "SUBSTRING(" + g.pick(fzFields) + ", 1, " + fmt.Sprintf("%d", g.r.Intn(4)) + ") = " + g.str()
	case 8: // date arithmetic
		switch g.r.Intn(3) {
		case 0:
			return "DATEDIFF(CURRENT_DATE, " + g.pick(fzFields) + ") <= " + g.num()
		case 1:
			return "DATE_ADD(" + g.pick(fzFields) + ", " + g.num() + ") >= CURRENT_DATE"
		default:
			return "DATE_SUB(CURRENT_DATE, 30) <= " + g.pick(fzFields)
		}
	case 9: // regexp from the safe pool
		not := ""
		if g.r.Intn(2) == 0 {
			not = "NOT "
		}
		return g.pick(fzFields) + " " + not + "REGEXP '" + g.pick(fzRegexps) + "'"
	case 10: // REGEXP_LIKE with flags
		return "REGEXP_LIKE(" + g.pick(fzFields) + ", '" + g.pick(fzRegexps) + "', 'i')"
	case 11: // array predicates
		switch g.r.Intn(3) {
		case 0:
			return "ARRAY_CONTAINS(" + g.pick(fzArrFields) + ", " + g.str() + ")"
		case 1:
			return "ARRAY_LENGTH(" + g.pick(fzArrFields) + ") >= " + g.num()
		default:
			return "ARRAY_INTERSECT(tags, nums)"
		}
	case 12: // JSON access
		return "JSON_EXTRACT(profile, '" + g.pick(fzJSONPaths) + "') = " + g.str()
	case 13: // EXISTS over a collection / sub-query
		if g.r.Intn(2) == 0 {
			return "EXISTS (" + g.pick(fzArrFields) + ")"
		}
		return "EXISTS (SELECT 1 FROM orders WHERE amount > " + g.num() + ")"
	case 14: // quantified sub-query
		if g.r.Intn(2) == 0 {
			return g.str() + " = ANY (SELECT label FROM tags_rows)"
		}
		return g.num() + " <= ALL (SELECT amount FROM orders)"
	case 15: // aggregate sub-query on either side
		agg := []string{"COUNT(*)", "SUM(amount)", "MIN(amount)", "MAX(amount)", "AVG(amount)"}[g.r.Intn(5)]
		if g.r.Intn(2) == 0 {
			return "(SELECT " + agg + " FROM orders) >= " + g.num()
		}
		return "(SELECT " + agg + " FROM orders WHERE status = '已付') <= " + g.num()
	case 16: // quantifier over a computed array
		return g.str() + " = ANY (SPLIT(csv, ','))"
	case 17: // extended boolean builtins as standalone predicates
		switch g.r.Intn(3) {
		case 0:
			return "CONTAINS(" + g.pick(fzFields) + ", " + g.str() + ")"
		case 1:
			return "STARTSWITH(" + g.pick(fzFields) + ", " + g.str() + ")"
		default:
			return "EQ(" + g.pick(fzFields) + ", " + g.num() + ")"
		}
	case 18: // COALESCE comparison
		return "COALESCE(" + g.pick(fzFields) + ", " + g.pick(fzFields) + ") != ''"
	case 19: // field vs field / literal-left comparison
		if g.r.Intn(2) == 0 {
			return g.pick(fzFields) + " " + g.pick(fzCmps) + " " + g.pick(fzFields)
		}
		return g.num() + " <= " + g.pick(fzFields)
	case 20: // IS NULL on a term
		return "LOWER(" + g.pick(fzFields) + ") IS NULL"
	case 21: // field-left quantifiers (left operand may be missing/NULL)
		if g.r.Intn(2) == 0 {
			return g.pick(fzFields) + " = ANY (tags)"
		}
		return g.pick(fzFields) + " >= ALL (SELECT amount FROM orders)"
	case 22: // non-scalar operands in scalar predicate positions (round 13:
		// arrays/objects behave like NULL for =/IN/LIKE/REGEXP, present for
		// IS NULL — the class the VM's ""-rendering used to diverge on)
		af := g.pick(fzArrFields)
		switch g.r.Intn(7) {
		case 0:
			return af + " " + g.pick(fzCmps) + " " + g.pick(fzArrFields)
		case 1:
			return af + " " + g.pick(fzCmps) + " " + g.pick(fzFields)
		case 2:
			return g.pick(fzFields) + " " + g.pick(fzCmps) + " " + af
		case 3:
			return af + " LIKE '" + g.likePattern() + "'"
		case 4:
			return af + " NOT REGEXP '" + g.pick(fzRegexps) + "'"
		case 5:
			return "profile " + g.pick(fzCmps) + " " + g.str()
		default:
			if g.r.Intn(2) == 0 {
				return af + " IN ('', 'vip')"
			}
			return "profile IS NOT NULL"
		}
	case 23: // round-14 common-SQL functions (strings/math/dates/JSON/regexp)
		switch g.r.Intn(8) {
		case 0:
			return "LOCATE(" + g.str() + ", " + g.pick(fzFields) + ") >= " + g.num()
		case 1:
			return "MOD(" + g.pick(fzFields) + ", 7) = " + g.num()
		case 2:
			return "GREATEST(" + g.pick(fzFields) + ", " + g.pick(fzFields) + ") " + g.pick(fzCmps) + " " + g.str()
		case 3:
			return "QUARTER(" + g.pick(fzFields) + ") = " + g.num()
		case 4:
			return "JSON_LENGTH(profile) >= " + g.num()
		case 5:
			return "JSON_CONTAINS(profile, " + g.str() + ", '$.city')"
		case 6:
			return "JSON_VALID(" + g.pick(fzFields) + ")"
		default:
			return "REGEXP_SUBSTR(" + g.pick(fzFields) + ", '" + g.pick(fzRegexps) + "') = " + g.str()
		}
	case 24: // IN (SELECT ...) sub-query membership (round 14)
		not := ""
		if g.r.Intn(2) == 0 {
			not = "NOT "
		}
		if g.r.Intn(2) == 0 {
			return g.pick(fzFields) + " " + not + "IN (SELECT label FROM tags_rows)"
		}
		return g.str() + " " + not + "IN (SELECT status FROM orders WHERE amount > " + g.num() + ")"
	default: // function-left LIKE (LikeTerm)
		return "LOWER(" + g.pick(fzFields) + ") LIKE '" + g.likePattern() + "'"
	}
}

func (g *fzGen) node(depth int) string {
	if depth <= 0 || g.r.Intn(100) < 55 {
		return g.leaf()
	}
	switch g.r.Intn(3) {
	case 0:
		return "NOT (" + g.node(depth-1) + ")"
	case 1:
		return "(" + g.node(depth-1) + " AND " + g.node(depth-1) + ")"
	default:
		return "(" + g.node(depth-1) + " OR " + g.node(depth-1) + ")"
	}
}

// row generates a random row; fields may be absent, null, or "wrong"-typed on
// purpose — coercion behaviour must still agree between the runtimes.
func (g *fzGen) row() map[string]any {
	row := map[string]any{}
	for _, f := range fzFields {
		switch g.r.Intn(6) {
		case 0: // absent
		case 1:
			row[f] = nil
		case 2:
			row[f] = g.pick(fzWords)
		case 3:
			row[f] = g.r.Intn(201) - 100
		case 4:
			row[f] = float64(g.r.Intn(2000))/100 - 10
		default:
			row[f] = g.r.Intn(2) == 0
		}
	}
	if g.r.Intn(3) > 0 {
		row["reg_date"] = fmt.Sprintf("2026-0%d-1%d", 1+g.r.Intn(9), g.r.Intn(9))
	}
	if g.r.Intn(3) > 0 {
		if g.r.Intn(2) == 0 { // typed vs generic arrays: same kArr either way
			row["tags"] = []string{"vip", g.pick(fzWords)}
		} else {
			row["tags"] = []any{"vip", g.pick(fzWords)}
		}
		row["nums"] = []any{1, 2, g.r.Intn(9)}
	}
	if g.r.Intn(3) > 0 {
		row["csv"] = "a,b," + g.pick(fzWords)
	}
	if g.r.Intn(3) > 0 {
		if g.r.Intn(2) == 0 { // JSON string document
			row["profile"] = `{"city":"深圳","addr":{"zip":"518000"}}`
		} else { // the same document pre-decoded: kOpaque (round 13)
			row["profile"] = map[string]any{"city": "深圳", "addr": map[string]any{"zip": "518000"}}
		}
	}
	if g.r.Intn(3) > 0 {
		n := g.r.Intn(3)
		if g.r.Intn(2) == 0 { // []any of objects: kArr collection
			orders := make([]any, 0, n)
			for i := 0; i < n; i++ {
				orders = append(orders, map[string]any{
					"amount": g.r.Intn(500), "status": []string{"已付", "退款"}[g.r.Intn(2)], "qty": 1 + g.r.Intn(3),
				})
			}
			row["orders"] = orders
		} else { // typed nested rows: kOpaque collection (round 13)
			orders := make([]map[string]any, 0, n)
			for i := 0; i < n; i++ {
				orders = append(orders, map[string]any{
					"amount": g.r.Intn(500), "status": []string{"已付", "退款"}[g.r.Intn(2)], "qty": 1 + g.r.Intn(3),
				})
			}
			row["orders"] = orders
		}
		row["tags_rows"] = []any{map[string]any{"label": g.pick(fzWords)}}
	}
	return row
}

func TestDifferentialFuzz(t *testing.T) {
	iters := 2500
	if testing.Short() {
		iters = 400
	}
	g := &fzGen{r: rand.New(rand.NewSource(20260703))}
	rt := astrt.New()

	for i := 0; i < iters; i++ {
		rule := g.node(3)

		node, err := ir.Parse(rule)
		if err != nil {
			t.Fatalf("iter %d: generator produced unparseable rule %q: %v", i, rule, err)
		}
		node = ir.Optimize(node)
		prog, err := Compile(node)
		if err != nil {
			t.Fatalf("iter %d: vm compile %q: %v", i, rule, err)
		}
		plan, err := rt.Compile(node)
		if err != nil {
			t.Fatalf("iter %d: ast compile %q: %v", i, rule, err)
		}

		// SQL emit round trip: must re-parse and re-compile.
		sql2 := ir.Emit(node, ir.SQL)
		node2, err := ir.Parse(sql2)
		if err != nil {
			t.Fatalf("iter %d: emitted SQL unparseable\n rule=%q\n emit=%q\n err=%v", i, rule, sql2, err)
		}
		prog2, err := Compile(node2)
		if err != nil {
			t.Fatalf("iter %d: vm compile of emitted %q: %v", i, sql2, err)
		}
		plan2, err := rt.Compile(node2)
		if err != nil {
			t.Fatalf("iter %d: ast compile of emitted %q: %v", i, sql2, err)
		}

		rows := []map[string]any{g.row(), g.row(), g.row(), {}}
		for _, row := range rows {
			bc := prog.Eval(row)
			av, err := rt.Execute(plan, row)
			if err != nil {
				t.Fatalf("iter %d: ast execute %q: %v", i, rule, err)
			}
			bc2 := prog2.Eval(row)
			av2, err := rt.Execute(plan2, row)
			if err != nil {
				t.Fatalf("iter %d: ast execute emitted %q: %v", i, sql2, err)
			}
			if bc != av || bc != bc2 || bc2 != av2 {
				rj, _ := json.Marshal(row)
				t.Fatalf("iter %d: DIVERGENCE\n rule=%q\n emit=%q\n row=%s\n vm=%v ast=%v vm(emit)=%v ast(emit)=%v",
					i, rule, sql2, rj, bc, av, bc2, av2)
			}
		}
	}
}
