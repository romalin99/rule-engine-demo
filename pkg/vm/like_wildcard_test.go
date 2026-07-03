// like_wildcard_test.go — round eight: full SQL LIKE wildcard grammar.
//
// Before this round both runtimes classified LIKE patterns into four fast
// shapes keyed on leading/trailing '%', silently mis-evaluating standard SQL
// patterns: '_' was a literal underscore and an interior '%' made the whole
// pattern a literal-equality test. These tests pin the fixed semantics —
// '_' one character, '%' anywhere, backslash escapes — plus the unchanged
// fast shapes, the legacy literal mode (Wildcards=false, for externally built
// IR), the widened slash date layouts, and the regexp-pattern size cap.
// Every case asserts bytecode VM == AST runtime.
//
// Run: go test ./pkg/vm/ -run TestLikeWildcard -v
package vm

import (
	"strings"
	"testing"

	"tcg-rulex-engine/pkg/ir"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

func TestLikeWildcardGrammar(t *testing.T) {
	cases := []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// interior '%' — previously a literal-equality test
		{"name LIKE 'a%b'", map[string]any{"name": "ab"}, true},
		{"name LIKE 'a%b'", map[string]any{"name": "axxxb"}, true},
		{"name LIKE 'a%b'", map[string]any{"name": "ba"}, false},
		{"name LIKE 'a%b'", map[string]any{"name": "aB"}, false},
		{"name LIKE 'a%b%c'", map[string]any{"name": "a1b2c"}, true},
		{"name LIKE 'a%b%c'", map[string]any{"name": "acb"}, false},

		// '_' single-character wildcard — previously a literal underscore
		{"name LIKE 'a_c'", map[string]any{"name": "abc"}, true},
		{"name LIKE 'a_c'", map[string]any{"name": "a中c"}, true}, // one rune
		{"name LIKE 'a_c'", map[string]any{"name": "ac"}, false},
		{"name LIKE 'a_c'", map[string]any{"name": "abbc"}, false},
		{"code LIKE '5__'", map[string]any{"code": "512"}, true},
		{"code LIKE '5__'", map[string]any{"code": "51"}, false},
		{"code LIKE '5__'", map[string]any{"code": "5123"}, false},
		{"name LIKE '%数_码%'", map[string]any{"name": "A数X码B"}, true},
		{"name LIKE '%数_码%'", map[string]any{"name": "数码"}, false},

		// backslash escapes: \% and \_ are literal text
		{`pct LIKE '50\%'`, map[string]any{"pct": "50%"}, true},
		{`pct LIKE '50\%'`, map[string]any{"pct": "500"}, false},
		{`pct LIKE '100\%_OFF'`, map[string]any{"pct": "100%xOFF"}, true},
		{`pct LIKE '100\%_OFF'`, map[string]any{"pct": "100%OFF"}, false},
		{`name LIKE 'a\_b%'`, map[string]any{"name": "a_bc"}, true},
		{`name LIKE 'a\_b%'`, map[string]any{"name": "aXbc"}, false},

		// NOT LIKE flips; NULL keeps the engine's two-valued behaviour
		{"name NOT LIKE 'a%b'", map[string]any{"name": "ab"}, false},
		{"name NOT LIKE 'a%b'", map[string]any{"name": "zz"}, true},
		{"name LIKE 'a_c'", map[string]any{}, false},
		{"name NOT LIKE 'a_c'", map[string]any{}, true},

		// the four fast shapes are untouched
		{"city LIKE '深%'", map[string]any{"city": "深圳"}, true},
		{"city LIKE '%圳'", map[string]any{"city": "深圳"}, true},
		{"city LIKE '%圳%'", map[string]any{"city": "深圳南"}, true},
		{"city LIKE '深圳'", map[string]any{"city": "深圳"}, true},
		{"city LIKE '深圳'", map[string]any{"city": "深圳市"}, false},

		// function-aware left operand (LikeTerm) takes the same grammar
		{"LOWER(name) LIKE 'a_c%'", map[string]any{"name": "ABCD"}, true},
		{"LOWER(name) LIKE 'a_c%'", map[string]any{"name": "AXBC"}, false},

		// slash date layouts accepted by the core date opcodes (round eight)
		{"YEAR('2026/06/15') = 2026", map[string]any{}, true},
		{"DATEDIFF('2026/07/02', '2026-07-01') = 1", map[string]any{}, true},
		{"DATE_ADD('2026/06/15', 1, 'DAY') = '2026-06-16'", map[string]any{}, true},
		{"reg BETWEEN '2026/01/01' AND '2026/12/31'", map[string]any{"reg": "2026/06/15"}, true},
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
			t.Errorf("AST/VM disagree on %q row=%v: bytecode=%v ast=%v", c.rule, c.row, bc, a)
		}
		if bc != c.want {
			t.Errorf("wrong result for %q row=%v: got %v want %v", c.rule, c.row, bc, c.want)
		}
	}
}

// TestLikeWildcardLegacyLiteral pins the zero-value (Wildcards=false) mode:
// externally built ir.Like nodes keep the historical four-shape literal
// classification, where '_' and '\' have no special meaning.
func TestLikeWildcardLegacyLiteral(t *testing.T) {
	rt := astrt.New()
	cases := []struct {
		node ir.Node
		row  map[string]any
		want bool
	}{
		{ir.Like{Field: "name", Pattern: "a_b%"}, map[string]any{"name": "a_bXY"}, true},
		{ir.Like{Field: "name", Pattern: "a_b%"}, map[string]any{"name": "aXbYY"}, false},
		// legacy mode has no escapes: `50\%` classifies as prefix `50\`
		{ir.Like{Field: "name", Pattern: `50\%`}, map[string]any{"name": `50\x`}, true},
		{ir.Like{Field: "name", Pattern: `50\%`}, map[string]any{"name": "50%"}, false},
		{ir.Like{Field: "name", Pattern: "%a_c%"}, map[string]any{"name": "xxa_cyy"}, true},
		{ir.Like{Field: "name", Pattern: "%a_c%"}, map[string]any{"name": "xxaXcyy"}, false},
	}
	for _, c := range cases {
		prog, err := Compile(c.node)
		if err != nil {
			t.Fatalf("compile %+v: %v", c.node, err)
		}
		plan, err := rt.Compile(c.node)
		if err != nil {
			t.Fatalf("ast compile %+v: %v", c.node, err)
		}
		bc := prog.Eval(c.row)
		a, err := rt.Execute(plan, c.row)
		if err != nil {
			t.Fatalf("ast execute %+v: %v", c.node, err)
		}
		if bc != a || bc != c.want {
			t.Errorf("legacy literal %+v row=%v: bytecode=%v ast=%v want=%v", c.node, c.row, bc, a, c.want)
		}
	}
}

// TestLikeWildcardEmitRoundTrip (round eleven): a legacy (Wildcards=false)
// node and its emitted-SQL reparse must agree on every row — the emitter
// re-encodes legacy patterns ('_' / '\' / interior '%' escaped) because the
// SQL front-end re-parses emitted text under the full wildcard grammar.
func TestLikeWildcardEmitRoundTrip(t *testing.T) {
	nodes := []ir.Node{
		ir.Like{Field: "name", Pattern: "a_b%"},          // legacy '_' literal
		ir.Like{Field: "name", Pattern: "a%b"},           // legacy interior '%' -> equality
		ir.Like{Field: "name", Pattern: "%a_c%"},         // legacy contains with '_'
		ir.Like{Field: "name", Pattern: `50\%`},          // legacy prefix '50\'
		ir.Like{Field: "name", Pattern: "a_b%", Negate: true},
	}
	rows := []map[string]any{
		{"name": "a_bXY"}, {"name": "aXbYY"}, {"name": "a%b"}, {"name": "ab"},
		{"name": "xxa_cyy"}, {"name": "xxaXcyy"}, {"name": `50\x`}, {"name": "50%"},
		{"name": ""}, {},
	}
	rt := astrt.New()
	for _, n := range nodes {
		sql := ir.Emit(n, ir.SQL)
		reparsed, err := ir.Parse(sql)
		if err != nil {
			t.Fatalf("re-parse %q: %v", sql, err)
		}
		p1, err := Compile(n)
		if err != nil {
			t.Fatalf("compile direct %#v: %v", n, err)
		}
		p2, err := Compile(reparsed)
		if err != nil {
			t.Fatalf("compile reparsed %q: %v", sql, err)
		}
		a1, err := rt.Compile(n)
		if err != nil {
			t.Fatalf("ast compile direct: %v", err)
		}
		a2, err := rt.Compile(reparsed)
		if err != nil {
			t.Fatalf("ast compile reparsed: %v", err)
		}
		for _, row := range rows {
			d, r := p1.Eval(row), p2.Eval(row)
			if d != r {
				t.Errorf("VM: direct %#v != reparse %q on %v (%v vs %v)", n, sql, row, d, r)
			}
			ad, err1 := rt.Execute(a1, row)
			ar, err2 := rt.Execute(a2, row)
			if err1 != nil || err2 != nil {
				t.Fatalf("ast execute: %v / %v", err1, err2)
			}
			if ad != ar || ad != d {
				t.Errorf("AST: direct=%v reparse=%v vm=%v for %#v on %v", ad, ar, d, n, row)
			}
		}
	}
}

// TestLikeWildcardPatternCap: regexp-lowered patterns obey the shared size cap
// (sqlfn.MaxRegexpPattern) — an oversized hostile pattern is a load-time error,
// for LIKE and REGEXP alike, not a memory sink.
func TestLikeWildcardPatternCap(t *testing.T) {
	long := strings.Repeat("a", 5000)
	for _, rule := range []string{
		"name LIKE '" + long + "_'",   // '_' forces the regexp lowering
		"name REGEXP '" + long + "'", // plain REGEXP takes the same cap
	} {
		node, err := ir.Parse(rule)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if _, err := Compile(node); err == nil {
			t.Errorf("want compile error for oversized pattern (len %d), got nil", 5000)
		}
	}
	// Boundary sanity: a comfortably-sized wildcard pattern still compiles.
	node, err := ir.Parse("name LIKE '" + strings.Repeat("b", 100) + "_'")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, err := Compile(node); err != nil {
		t.Errorf("small wildcard pattern should compile, got %v", err)
	}
}
