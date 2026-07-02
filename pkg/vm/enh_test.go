// enh_test.go — regression tests for the SQL-feature gaps closed in this change
// set. Every deterministic case asserts BOTH that the bytecode VM and the
// tree-walking AST runtime agree AND that the result matches the expectation:
//
//   - CURRENT_DATE / CURRENT_TIMESTAMP as the LEFT operand of a comparison
//     (previously parsed as a field literally named "CURRENT_DATE").
//   - String / date BETWEEN (`x BETWEEN '2020-01-01' AND '2020-12-31'`), which
//     used to fail to compile on the VM and silently return false on the AST.
//   - ROUND(x, d) — round to d decimal places (2-arg form).
//   - SUBSTRING(s, start) — 2-arg form (from start to end of string).
//   - ARRAY_INTERSECT(a, b) — non-empty intersection of two array fields.
//
// Run: go test ./pkg/vm/ -run TestEnh -v
package vm

import (
	"testing"

	"tcg-rulex-engine/pkg/ir"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

// runAgree parses/optimizes/compiles each rule on both runtimes and asserts they
// agree with each other and with the expected boolean.
func runAgree(t *testing.T, cases []struct {
	rule string
	row  map[string]any
	want bool
},
) {
	t.Helper()
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

// TestEnhCurrentDateLeftOperand verifies CURRENT_DATE / CURRENT_TIMESTAMP work as
// the left operand of a comparison (they resolve to today/now, not a missing
// field). Results are checked directly (time.Now is evaluated per-runtime, so an
// exact AST/VM byte-for-byte agreement is not asserted here — every comparison is
// against a far-past bound, so both runtimes are unambiguously true).
func TestEnhCurrentDateLeftOperand(t *testing.T) {
	rt := astrt.New()
	cases := []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"CURRENT_DATE >= '2000-01-01'", map[string]any{}, true},
		{"CURRENT_DATE > last_login", map[string]any{"last_login": "2000-01-01"}, true},
		{"CURRENT_DATE = CURRENT_DATE", map[string]any{}, true},
		{"CURRENT_TIMESTAMP >= '2000-01-01 00:00:00'", map[string]any{}, true},
		{"CURRENT_TIMESTAMP > '2000-01-01'", map[string]any{}, true},
		// still works on the right-hand side / as an argument (unchanged behaviour)
		{"last_login <= CURRENT_DATE", map[string]any{"last_login": "2000-01-01"}, true},
		{"YEAR(CURRENT_DATE) >= 2026", map[string]any{}, true},
	}
	for _, c := range cases {
		node, err := ir.Parse(c.rule)
		if err != nil {
			t.Fatalf("parse %q: %v", c.rule, err)
		}
		prog, err := Compile(node)
		if err != nil {
			t.Fatalf("compile %q: %v", c.rule, err)
		}
		if got := prog.Eval(c.row); got != c.want {
			t.Errorf("bytecode %q: got %v want %v", c.rule, got, c.want)
		}
		plan, err := rt.Compile(node)
		if err != nil {
			t.Fatalf("ast compile %q: %v", c.rule, err)
		}
		if got, _ := rt.Execute(plan, c.row); got != c.want {
			t.Errorf("ast %q: got %v want %v", c.rule, got, c.want)
		}
	}
}

func TestEnhStringBetween(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"register_date BETWEEN '2020-01-01' AND '2020-12-31'", map[string]any{"register_date": "2020-06-15"}, true},
		{"register_date BETWEEN '2020-01-01' AND '2020-12-31'", map[string]any{"register_date": "2019-12-31"}, false},
		{"register_date BETWEEN '2020-01-01' AND '2020-12-31'", map[string]any{"register_date": "2021-01-01"}, false},
		{"register_date BETWEEN '2020-01-01' AND '2020-12-31'", map[string]any{"register_date": "2020-01-01"}, true}, // inclusive lo
		{"register_date BETWEEN '2020-01-01' AND '2020-12-31'", map[string]any{"register_date": "2020-12-31"}, true}, // inclusive hi
		{"register_date BETWEEN '2020-01-01' AND '2020-12-31'", map[string]any{}, false},                             // missing -> false
		{"grade BETWEEN 'A' AND 'C'", map[string]any{"grade": "B"}, true},
		{"grade BETWEEN 'A' AND 'C'", map[string]any{"grade": "D"}, false},
		// numeric BETWEEN still works (fast path, unchanged)
		{"age BETWEEN 25 AND 40", map[string]any{"age": 30}, true},
		{"age BETWEEN 25 AND 40", map[string]any{"age": 50}, false},
	})
}

func TestEnhRound2(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"ROUND(price, 2) = 3.14", map[string]any{"price": 3.14159}, true},
		{"ROUND(price, 2) = 3.15", map[string]any{"price": 3.14559}, true},
		{"ROUND(price, 0) = 86", map[string]any{"price": 85.6}, true},
		{"ROUND(price, 1) = 2.7", map[string]any{"price": 2.65}, true},
		{"ROUND(amount, -1) = 120", map[string]any{"amount": 123.456}, true},
		{"ROUND(price, 2) = 100", map[string]any{"price": 99.996}, true},
		{"ROUND(price, 2) = 10.5", map[string]any{"price": 10.499}, true},  // 10.499 -> 10.50 (3rd decimal rounds up)
		{"ROUND(price, 2) = 10.5", map[string]any{"price": 10.494}, false}, // 10.494 -> 10.49
		// 1-arg ROUND still works
		{"ROUND(score) = 86", map[string]any{"score": 85.6}, true},
		// NULL / non-numeric propagates -> compare false
		{"ROUND(missing, 2) = 0", map[string]any{}, false},
	})
}

func TestEnhSubstr2(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"SUBSTRING(phone, 8) = '5678'", map[string]any{"phone": "13912345678"}, true},
		{"SUBSTR(name, 2) = '码城'", map[string]any{"name": "数码城"}, true},
		{"SUBSTRING(name, 1) = '数码城'", map[string]any{"name": "数码城"}, true}, // whole string
		{"SUBSTRING(name, 4) = ''", map[string]any{"name": "数码城"}, true},    // past end -> empty
		{"SUBSTRING(code, 3) = 'CDE'", map[string]any{"code": "ABCDE"}, true},
		// 3-arg SUBSTRING still works
		{"SUBSTRING(name, 1, 2) = '数码'", map[string]any{"name": "数码城"}, true},
		// NULL input -> undef -> compare false
		{"SUBSTRING(missing, 2) = ''", map[string]any{}, false},
	})
}

func TestEnhArrayIntersect(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"ARRAY_INTERSECT(tags, vips)", map[string]any{"tags": []string{"a", "b", "c"}, "vips": []string{"x", "b"}}, true},
		{"ARRAY_INTERSECT(tags, vips)", map[string]any{"tags": []string{"a", "b", "c"}, "vips": []string{"x", "y"}}, false},
		{"ARRAY_INTERSECT(ids, others)", map[string]any{"ids": []any{1, 5, 9}, "others": []any{5, 7}}, true},  // numeric, common 5
		{"ARRAY_INTERSECT(ids, others)", map[string]any{"ids": []any{1, 5, 9}, "others": []any{2, 7}}, false}, // numeric, no common
		{"ARRAY_INTERSECT(tags, vips)", map[string]any{"tags": []string{}, "vips": []string{"a"}}, false},     // empty -> false
		{"ARRAY_INTERSECT(tags, vips)", map[string]any{"tags": []string{"a"}}, false},                         // missing operand -> false
		{"NOT ARRAY_INTERSECT(tags, vips)", map[string]any{"tags": []string{"a"}, "vips": []string{"b"}}, true},
		// combined with the rest of the grammar
		{"ARRAY_INTERSECT(tags, vips) AND age >= 18", map[string]any{"tags": []string{"vip"}, "vips": []string{"vip"}, "age": 20}, true},
	})
}

// TestEnhNegativeNumbers verifies negative numeric literals lex/parse/evaluate in
// every operand position (comparisons, BETWEEN, IN, function args). Previously
// the lexer rejected '-' outright.
func TestEnhNegativeNumbers(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"temperature < -10", map[string]any{"temperature": -15}, true},
		{"temperature < -10", map[string]any{"temperature": -5}, false},
		{"balance >= -100", map[string]any{"balance": -50}, true},
		{"delta = -3", map[string]any{"delta": -3}, true},
		{"lat BETWEEN -90 AND 90", map[string]any{"lat": -33.8}, true},
		{"lat BETWEEN -90 AND 90", map[string]any{"lat": -120}, false},
		{"offset IN (-1, 0, 1)", map[string]any{"offset": -1}, true},
		{"ABS(delta) = 3", map[string]any{"delta": -3}, true},
		{"ROUND(amount, -1) = 120", map[string]any{"amount": 123.4}, true}, // negative decimal places
		{"DATE_ADD(d, -7) = '2026-06-01'", map[string]any{"d": "2026-06-08"}, true},
		{"score > -0.5", map[string]any{"score": 0.0}, true},
	})
}

// TestEnhRoundTrip checks the new syntax survives an emit -> parse -> emit cycle
// unchanged, so Optimize's SQL-keyed dedup and rule export stay stable.
func TestEnhRoundTrip(t *testing.T) {
	rules := []string{
		"ROUND(price, 2) = 3.14",
		"SUBSTRING(name, 1, 2) = '数码'",
		"ARRAY_INTERSECT(tags, vips)",
		"grade BETWEEN 'A' AND 'C'", // desugars to grade >= 'A' AND grade <= 'C'
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
