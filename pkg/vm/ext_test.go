// ext_test.go — date arithmetic (DATE_ADD/DATE_SUB) and the set predicates
// (ANY / ALL quantifiers and EXISTS sub-queries). Every case asserts BOTH that
// the bytecode VM and the tree-walking AST runtime agree, and that the result
// matches the expected boolean.
//
// Run: go test ./pkg/vm/ -run TestExt -v

package vm

import (
	"testing"

	"tcg-rulex-engine/pkg/ir"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

func TestExtDateAndSetPredicates(t *testing.T) {
	row := map[string]any{
		"minv": 90, "maxv": 95, "lo": 50, "need": 85,
		"n": 30, "big": 1000, "threshold": 100, "prov": "B",
		"scores": []any{70, 85, 92},
		"tags":   []string{"vip", "gold"},
		"empty":  []string{},
		"orders": []any{
			map[string]any{"amount": 120, "status": "paid"},
			map[string]any{"amount": 50, "status": "refunded"},
		},
	}
	empty := map[string]any{} // every field missing

	cases := []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// ---- date arithmetic (DATE_ADD / DATE_SUB) ----
		{"DATE_ADD('2026-06-01', 30) = '2026-07-01'", row, true},
		{"DATE_SUB('2026-06-28', 28) = '2026-05-31'", row, true},
		{"DATE_ADD('2026-01-15', 1, 'MONTH') = '2026-02-15'", row, true},
		{"DATE_ADD('2025-02-28', 1, 'YEAR') = '2026-02-28'", row, true},
		{"DATE_ADD('2026-06-01 10:00:00', 2, 'HOUR') = '2026-06-01 12:00:00'", row, true},
		{"DATE_SUB('2026-06-01', 1, 'DAY') = '2026-05-31'", row, true},
		{"DATE_ADD(missing, 1) = '2020-01-01'", row, false}, // NULL date -> NULL -> false

		// ---- ANY / ALL over an array field ----
		{"minv < ANY(scores)", row, true},  // 90 < 92
		{"maxv < ANY(scores)", row, false}, // 95 < none
		{"minv <= ALL(scores)", row, false},
		{"lo <= ALL(scores)", row, true},
		{"need = ANY(scores)", row, true},    // 85 in {70,85,92}
		{"minv < ANY(scores)", empty, false}, // ANY over an empty/missing set -> false
		{"minv <= ALL(scores)", empty, true}, // ALL over an empty/missing set -> vacuously true

		// ---- ANY / ALL over a value list (desugared) ----
		{"prov = ANY('A','B','C')", row, true},
		{"prov = ANY('X','Y')", row, false},
		{"n >= ALL(10, 20, 30)", row, true},
		{"n >= ALL(10, 40)", row, false},

		// ---- EXISTS over a collection (non-empty) ----
		{"EXISTS(tags)", row, true},
		{"EXISTS(empty)", row, false},
		{"EXISTS(tags)", empty, false},
		{"NOT EXISTS(tags)", row, false},
		{"NOT EXISTS(empty)", row, true},

		// ---- EXISTS sub-query over nested object rows ----
		{"EXISTS(SELECT 1 FROM orders WHERE amount > 100)", row, true},
		{"EXISTS(SELECT 1 FROM orders WHERE amount > 200)", row, false},
		{"EXISTS(SELECT 1 FROM orders WHERE status = 'paid')", row, true},
		{"NOT EXISTS(SELECT 1 FROM orders WHERE amount > 200)", row, true},

		// ---- quantified sub-query (projected column) ----
		{"threshold < ANY(SELECT amount FROM orders)", row, true},
		{"threshold < ALL(SELECT amount FROM orders)", row, false},
		{"threshold < ALL(SELECT amount FROM orders WHERE status = 'paid')", row, true},
		{"big > ALL(SELECT amount FROM orders)", row, true},

		// ---- combined with the rest of the grammar ----
		{"EXISTS(SELECT 1 FROM orders WHERE amount > 100) AND need = ANY(scores)", row, true},
		{"DATE_ADD('2026-06-01', 30) = '2026-07-01' OR maxv < ANY(scores)", row, true},
	}

	rt := astrt.New()
	for _, c := range cases {
		node, err := ir.Parse(c.rule)
		if err != nil {
			t.Fatalf("parse %q: %v", c.rule, err)
		}
		node = ir.Optimize(node)

		prog, err := Compile(node)
		if err != nil {
			t.Fatalf("compile %q: %v", c.rule, err)
		}
		plan, err := rt.Compile(node)
		if err != nil {
			t.Fatalf("ast compile %q: %v", c.rule, err)
		}

		bc := prog.Eval(c.row)
		a, err := rt.Execute(plan, c.row)
		if err != nil {
			t.Fatalf("ast execute %q: %v", c.rule, err)
		}
		if bc != a {
			t.Errorf("AST/VM disagree on %q\n row=%v\n bytecode=%v ast=%v", c.rule, c.row, bc, a)
		}
		if bc != c.want {
			t.Errorf("wrong result for %q: got %v want %v", c.rule, bc, c.want)
		}
	}
}

// TestExtRoundTrip checks that the new syntax survives an emit -> parse -> emit
// cycle unchanged (so Optimize's SQL-keyed dedup and rule export stay stable).
func TestExtRoundTrip(t *testing.T) {
	rules := []string{
		"score >= ANY (grades)",
		"score <= ALL (grades)",
		"EXISTS (orders)",
		"EXISTS (SELECT 1 FROM orders WHERE amount > 100)",
		"threshold < ANY (SELECT amount FROM orders WHERE status = 'paid')",
		"name REGEXP '^a.*z$'",
		"name NOT REGEXP '[0-9]'",
		"REGEXP_LIKE(name, 'abc', 'i')", // desugars to: name REGEXP '(?i)abc'
		"JSON_EXTRACT(profile, '$.city') = '深圳'",
		"JSON_EXTRACT(profile, '$.addr.zip') = '518000'",
		"(SELECT COUNT(*) FROM orders WHERE status = 'paid') >= 2",
		"(SELECT SUM(amount) FROM orders) >= 1000",
		"budget > (SELECT MAX(amount) FROM orders)",
	}
	for _, r := range rules {
		n1, err := ir.Parse(r)
		if err != nil {
			t.Fatalf("parse %q: %v", r, err)
		}
		s1 := ir.Emit(n1, ir.SQL)
		n2, err := ir.Parse(s1)
		if err != nil {
			t.Fatalf("re-parse %q (from %q): %v", s1, r, err)
		}
		if s2 := ir.Emit(n2, ir.SQL); s1 != s2 {
			t.Errorf("round-trip not stable:\n  first:  %q\n  second: %q", s1, s2)
		}
	}
}

func TestExtAggregateSubqueries(t *testing.T) {
	row := map[string]any{
		"budget": 500,
		"orders": []any{
			map[string]any{"amount": 120, "status": "paid", "qty": 2},
			map[string]any{"amount": 80, "status": "paid", "qty": 1},
			map[string]any{"amount": 50, "status": "refunded", "qty": 3},
		},
	}
	noOrders := map[string]any{"budget": 500, "orders": []any{}}

	cases := []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"(SELECT COUNT(*) FROM orders) = 3", row, true},
		{"(SELECT COUNT(*) FROM orders WHERE status = 'paid') = 2", row, true},
		{"(SELECT COUNT(*) FROM orders WHERE amount > 1000) = 0", row, true},
		{"(SELECT SUM(amount) FROM orders) = 250", row, true},
		{"(SELECT SUM(amount) FROM orders WHERE status = 'paid') = 200", row, true},
		{"(SELECT AVG(amount) FROM orders WHERE status = 'paid') = 100", row, true},
		{"(SELECT MIN(qty) FROM orders) = 1", row, true},
		{"(SELECT MAX(amount) FROM orders) = 120", row, true},
		{"budget > (SELECT SUM(amount) FROM orders)", row, true},  // 500 > 250
		{"budget >= (SELECT MAX(amount) FROM orders)", row, true}, // 500 >= 120
		{"(SELECT COUNT(*) FROM orders WHERE status = 'paid') >= 2 AND budget > (SELECT SUM(amount) FROM orders)", row, true},
		// empty set: COUNT -> 0; SUM/AVG/MIN/MAX -> NULL (comparison false, IS NULL true)
		{"(SELECT COUNT(*) FROM orders) = 0", noOrders, true},
		{"(SELECT SUM(amount) FROM orders) >= 0", noOrders, false},
		{"(SELECT SUM(amount) FROM orders) IS NULL", noOrders, true},
		{"(SELECT AVG(amount) FROM orders) IS NULL", noOrders, true},
	}

	rt := astrt.New()
	for _, c := range cases {
		node, err := ir.Parse(c.rule)
		if err != nil {
			t.Fatalf("parse %q: %v", c.rule, err)
		}
		node = ir.Optimize(node)

		prog, err := Compile(node)
		if err != nil {
			t.Fatalf("compile %q: %v", c.rule, err)
		}
		plan, err := rt.Compile(node)
		if err != nil {
			t.Fatalf("ast compile %q: %v", c.rule, err)
		}

		bc := prog.Eval(c.row)
		a, err := rt.Execute(plan, c.row)
		if err != nil {
			t.Fatalf("ast execute %q: %v", c.rule, err)
		}
		if bc != a {
			t.Errorf("AST/VM disagree on %q\n row=%v\n bytecode=%v ast=%v", c.rule, c.row, bc, a)
		}
		if bc != c.want {
			t.Errorf("wrong result for %q: got %v want %v", c.rule, bc, c.want)
		}
	}
}

func TestExtJSONAndRegexp(t *testing.T) {
	row := map[string]any{
		"profile": `{"city":"深圳","age":30,"vip":true,"tags":["a","b"],"addr":{"zip":"518000"}}`,
		"parsed":  map[string]any{"city": "北京", "n": 5}, // pre-parsed object (int value)
		"name":    "Alice123",
		"code":    "ABC-123",
		"phone":   "13912345678",
	}
	empty := map[string]any{}

	cases := []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// ---- JSON_EXTRACT over a JSON-string field ----
		{"JSON_EXTRACT(profile, '$.city') = '深圳'", row, true},
		{"JSON_EXTRACT(profile, '$.city') = '上海'", row, false},
		{"JSON_EXTRACT(profile, '$.age') = 30", row, true}, // number
		{"JSON_EXTRACT(profile, '$.age') > 18", row, true},
		{"JSON_EXTRACT(profile, '$.vip') = 'true'", row, true},        // bool -> "true"
		{"JSON_EXTRACT(profile, '$.tags[1]') = 'b'", row, true},       // array index
		{"JSON_EXTRACT(profile, '$.addr.zip') = '518000'", row, true}, // nested
		{"JSON_EXTRACT(profile, '$.missing') IS NULL", row, true},
		{"JSON_VALUE(profile, '$.city') = '深圳'", row, true}, // alias
		// ---- JSON_EXTRACT over an already-parsed object field ----
		{"JSON_EXTRACT(parsed, '$.city') = '北京'", row, true},
		{"JSON_EXTRACT(parsed, '$.n') < 10", row, true}, // int leaf coerced to number
		// ---- JSON on a missing field -> NULL ----
		{"JSON_EXTRACT(profile, '$.city') = '深圳'", empty, false},

		// ---- REGEXP (infix operator) ----
		{"name REGEXP '^Alice'", row, true},
		{"name REGEXP '[0-9]+$'", row, true},
		{"name REGEXP '^bob'", row, false},
		{"name NOT REGEXP '^Bob'", row, true},
		{"code REGEXP '^[A-Z]+-[0-9]+$'", row, true},
		{"phone REGEXP '^139'", row, true},
		{"missing REGEXP '.*'", row, false}, // NULL never matches
		{"missing NOT REGEXP '.*'", row, true},
		// ---- REGEXP_LIKE (function form, optional 'i' flag) ----
		{"REGEXP_LIKE(name, 'alice')", row, false},     // case-sensitive
		{"REGEXP_LIKE(name, 'alice', 'i')", row, true}, // case-insensitive
		// ---- combined: JSON value as REGEXP left, and with AND ----
		{"JSON_EXTRACT(profile, '$.city') REGEXP '深'", row, true},
		{"name REGEXP '^A' AND JSON_EXTRACT(profile, '$.age') >= 18", row, true},
	}

	rt := astrt.New()
	for _, c := range cases {
		node, err := ir.Parse(c.rule)
		if err != nil {
			t.Fatalf("parse %q: %v", c.rule, err)
		}
		node = ir.Optimize(node)

		prog, err := Compile(node)
		if err != nil {
			t.Fatalf("compile %q: %v", c.rule, err)
		}
		plan, err := rt.Compile(node)
		if err != nil {
			t.Fatalf("ast compile %q: %v", c.rule, err)
		}

		bc := prog.Eval(c.row)
		a, err := rt.Execute(plan, c.row)
		if err != nil {
			t.Fatalf("ast execute %q: %v", c.rule, err)
		}
		if bc != a {
			t.Errorf("AST/VM disagree on %q\n row=%v\n bytecode=%v ast=%v", c.rule, c.row, bc, a)
		}
		if bc != c.want {
			t.Errorf("wrong result for %q: got %v want %v", c.rule, bc, c.want)
		}
	}
}
