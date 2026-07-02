// qlfn_parity_test.go — the third qlbridge-alignment batch (2026-07-02 registry
// diff): SECONDS / UNIXTRUNC / UNSIGN / STRING_INDEX / TITLECASE / QS2 /
// URL_MATCHQS / functional SUM / AVG / COUNT / the HASH alias — plus the lexer
// escape fix (regex character classes must survive string literals), the
// Emit(SQL) quoting fix, and the AST↔VM NULL alignment for LIKE / IN.
//
// Every deterministic case asserts BOTH that the bytecode VM and the AST
// runtime agree AND that the result is correct (shared runAgree helper).
// Expected values were computed independently in Python (epochs, unsigned
// two's-complement readings, SipHash vector).
//
// Run: go test ./pkg/vm/ -run TestParity -v
package vm

import (
	"strings"
	"testing"

	"tcg-rulex-engine/pkg/ir"
)

func TestParitySeconds(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"SECONDS(v) = 630", map[string]any{"v": "M10:30"}, true},
		{"SECONDS(v) = 6030", map[string]any{"v": "100:30"}, true},
		{"SECONDS(v) = 30", map[string]any{"v": "00:30"}, true},
		{"SECONDS(v) = 30", map[string]any{"v": "30"}, true},
		{"SECONDS(v) = 30", map[string]any{"v": 30}, true},
		{"SECONDS(v) = 1435968000", map[string]any{"v": "2015/07/04"}, true},
		{"SECONDS(v) IS NULL", map[string]any{"v": "0:00"}, true}, // qlbridge parity
		{"SECONDS(missing) IS NULL", map[string]any{}, true},
		{"SECONDS(v) > 600 AND SECONDS(v) < 700", map[string]any{"v": "M10:30"}, true},
	})
}

func TestParityUnixTrunc(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// 13-digit epoch string = milliseconds (qlbridge doc examples)
		{"UNIXTRUNC(ts) = '1438445529'", map[string]any{"ts": "1438445529707"}, true},
		{"UNIXTRUNC(ts, 'seconds') = '1438445529.707'", map[string]any{"ts": "1438445529707"}, true},
		{"UNIXTRUNC(ts, 'ms') = '1438445529707'", map[string]any{"ts": "1438445529707"}, true},
		{"UNIXTRUNC(ts, 'secondsmicro') = '1438445529.707123'", map[string]any{"ts": "1438445529707123456"}, true},
		// 10-digit epoch = seconds; numbers work like their rendered digits
		{"UNIXTRUNC(ts) = '1438445529'", map[string]any{"ts": "1438445529"}, true},
		{"UNIXTRUNC(ts) = '1438445529'", map[string]any{"ts": 1438445529}, true},
		// datetime strings go through the shared layouts (UTC)
		{"UNIXTRUNC(ts) = '1438443129'", map[string]any{"ts": "2015-08-01 15:32:09"}, true},
		{"UNIXTRUNC(ts, 's') = '1438443129.000'", map[string]any{"ts": "2015-08-01 15:32:09"}, true},
		{"UNIXTRUNC(ts, 'bogus') IS NULL", map[string]any{"ts": "1438445529707"}, true},
		{"UNIXTRUNC(missing) IS NULL", map[string]any{}, true},
		{"UNIXTRUNC(ts) IS NULL", map[string]any{"ts": "not a date"}, true},
	})
}

func TestParityUnsign(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"UNSIGN(v) = '876'", map[string]any{"v": 876}, true},
		{"UNSIGN(v) = '18446744073709551546'", map[string]any{"v": -70}, true},
		{"UNSIGN(v) = '18446744073709551546'", map[string]any{"v": "-70"}, true},
		{"UNSIGN(v) = '18446711226086221769'", map[string]any{"v": "-32847623329847"}, true}, // qlbridge doc example
		{"UNSIGN(missing) IS NULL", map[string]any{}, true},
		{"UNSIGN(v) IS NULL", map[string]any{"v": "abc"}, true},
	})
}

func TestParityStringIndexTitle(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"STRING_INDEX(s, ',') = 6", map[string]any{"s": "apples,oranges"}, true},
		{"STRING_INDEX(s, 'apples') = 0", map[string]any{"s": "apples,oranges"}, true},
		{"STRING_INDEX(s, 'z') IS NULL", map[string]any{"s": "apples"}, true},
		{"STRING_INDEX(missing, 'a') IS NULL", map[string]any{}, true},

		{"TITLECASE(s) = 'Hello World'", map[string]any{"s": "hello world"}, true},
		{"TITLECASE(s) = 'Foo_bar'", map[string]any{"s": "foo_bar"}, true}, // '_' is not a separator (strings.Title)
		{"TITLECASE(s) = 'Hello WOrld'", map[string]any{"s": "hello wOrld"}, true},
		{"TITLECASE(missing) IS NULL", map[string]any{}, true},
	})
}

func TestParityURLFns(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// QS2 is the case-preserving alias of QS (qlbridge qs2)
		{"QS2(u, 'utm_source') = 'google'", map[string]any{"u": "http://www.lytics.io/?utm_source=google"}, true},
		{"QS2(u, 'name') = 'Bob'", map[string]any{"u": "http://x.com/?name=Bob"}, true},
		{"QS2(u, 'absent') IS NULL", map[string]any{"u": "http://x.com/?a=1"}, true},

		// URL_MATCHQS: host+path plus only the matching query parameters
		{"URL_MATCHQS(u) = 'www.lytics.io/blog'",
			map[string]any{"u": "http://www.lytics.io/blog?mc_eid=123&pid=1&utm_content=abc"}, true},
		{"URL_MATCHQS(u, 'utm.*') = 'www.lytics.io/blog?utm_content=abc'",
			map[string]any{"u": "http://www.lytics.io/blog?mc_eid=123&pid=1&utm_content=abc"}, true},
		// kept parameters re-encode in sorted-key order
		{"URL_MATCHQS(u, 'utm.*') = 'x.com/p?utm_a=1&utm_b=2'",
			map[string]any{"u": "https://x.com/p?utm_b=2&utm_a=1&drop=3"}, true},
		{"URL_MATCHQS(u, '^mc_', '^pid$') = 'x.com/?mc_eid=9&pid=1'",
			map[string]any{"u": "http://x.com/?mc_eid=9&pid=1&other=z"}, true},
		{"URL_MATCHQS(missing) IS NULL", map[string]any{}, true},
	})
}

func TestParityFunctionalAggs(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// SUM: variadic; arrays expand leniently; numeric strings parse
		{"SUM(a, b, c) = 6", map[string]any{"a": 1, "b": 2, "c": 3}, true},
		{"SUM(a, b) = 3.5", map[string]any{"a": 1.5, "b": "2"}, true},
		{"SUM(nums) = 6", map[string]any{"nums": []string{"1", "2", "3"}}, true},
		{"SUM(nums) = 3", map[string]any{"nums": []any{1, "x", 2}}, true}, // lenient array skip
		{"SUM(a, missing) = 1", map[string]any{"a": 1}, true},            // NULL args skipped
		{"SUM(a, b) IS NULL", map[string]any{"a": 2, "b": -2}, true},     // qlbridge quirk: total 0 -> NULL
		{"SUM(flag) IS NULL", map[string]any{"flag": true}, true},        // bool poisons (qlbridge default branch)

		// AVG: strict on array elements, lenient on scalar strings
		{"AVG(a, b, c) = 2", map[string]any{"a": 1, "b": 2, "c": 3}, true},
		{"AVG(nums) = 2", map[string]any{"nums": []string{"1", "2", "3"}}, true},
		{"AVG(nums) IS NULL", map[string]any{"nums": []any{1, "x"}}, true}, // bad element fails the average
		{"AVG(s) IS NULL", map[string]any{"s": "hello"}, true},
		{"AVG(missing) IS NULL", map[string]any{}, true},

		// COUNT: occurrence marker (1 or NULL), qlbridge count
		{"COUNT(v) = 1", map[string]any{"v": 5}, true},
		{"COUNT(v) = 1", map[string]any{"v": 0}, true},
		{"COUNT(v) = 1", map[string]any{"v": false}, true},
		{"COUNT(missing) IS NULL", map[string]any{}, true},
		{"COUNT(v) IS NULL", map[string]any{"v": ""}, true},
		{"COUNT(v) IS NULL", map[string]any{"v": []string{}}, true},
		// composes with comparisons like any scalar
		{"COUNT(v) = 1 AND SUM(a, b) > 4", map[string]any{"v": "x", "a": 2, "b": 3}, true},
	})
}

func TestParityHashAlias(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// qlbridge registers hash.sip under the bare name "hash" too; the SipHash
		// vector for key (0,1) over "abc" was verified in the previous round.
		{"HASH(v) = '16397480524846279048'", map[string]any{"v": "abc"}, true},
		{"HASH(v) = HASH_SIP(v)", map[string]any{"v": "whatever"}, true},
		{"HASH(missing) IS NULL", map[string]any{}, true},
	})
}

// TestParityRegexEscapes locks in the lexer fix: backslash escapes inside
// string literals keep every \x sequence except \' \" \\ verbatim, so regex
// character classes reach the RE2 compiler intact. Before the fix,
// '^1\d{10}$' lexed to '^1d{10}$' and silently matched the letter 'd'.
func TestParityRegexEscapes(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{`phone REGEXP '^1\d{10}$'`, map[string]any{"phone": "13912345678"}, true},
		{`phone REGEXP '^1\d{10}$'`, map[string]any{"phone": "1d912345678"}, false}, // the old corrupted reading
		{`name REGEXP '^\w+$'`, map[string]any{"name": "abc_123"}, true},
		{`price REGEXP '^\d+\.\d{2}$'`, map[string]any{"price": "12.50"}, true},
		{`price REGEXP '^\d+\.\d{2}$'`, map[string]any{"price": "12x50"}, false},
		// doubled backslashes (MySQL style) still work: '\\d' lexes to '\d'
		{`phone REGEXP '^1\\d{10}$'`, map[string]any{"phone": "13912345678"}, true},
		{`REGEXP_LIKE(price, '^\d+$')`, map[string]any{"price": "12345"}, true},
		// escaped quote still unescapes
		{`name = 'O\'Brien'`, map[string]any{"name": "O'Brien"}, true},
	})
}

// TestParityRegexpLikeFlags covers the full MySQL match_type set for
// REGEXP_LIKE (round six): i / c (rightmost of the pair wins), m (multi-line
// anchors), n ('.' matches newline, RE2 's'), u (accepted, no-op). Expected
// matches were cross-checked against Python re (same semantics as RE2 for
// these flags).
func TestParityRegexpLikeFlags(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"REGEXP_LIKE(name, '^alice$', 'i')", map[string]any{"name": "ALICE"}, true},
		{"REGEXP_LIKE(name, '^alice$', 'ic')", map[string]any{"name": "ALICE"}, false}, // rightmost 'c' wins
		{"REGEXP_LIKE(name, '^alice$', 'ci')", map[string]any{"name": "ALICE"}, true},  // rightmost 'i' wins
		{"REGEXP_LIKE(s, '^a.b$', 'n')", map[string]any{"s": "a\nb"}, true},            // '.' spans the newline
		{"REGEXP_LIKE(s, '^a.b$')", map[string]any{"s": "a\nb"}, false},
		{"REGEXP_LIKE(s, '^b$', 'm')", map[string]any{"s": "a\nb"}, true}, // multi-line anchor
		{"REGEXP_LIKE(s, '^b$')", map[string]any{"s": "a\nb"}, false},
		{"REGEXP_LIKE(s, '^B$', 'im')", map[string]any{"s": "a\nb"}, true},
		{"REGEXP_LIKE(s, '^b$', 'mu')", map[string]any{"s": "a\nb"}, true}, // 'u' is a no-op
	})
	// An unknown flag must fail at rule load, mirroring MySQL's error.
	if _, err := CompileString("REGEXP_LIKE(name, 'x', 'z')"); err == nil {
		t.Error("expected compile error for match_type 'z'")
	} else if !strings.Contains(err.Error(), "match_type") {
		t.Errorf("unexpected error text: %v", err)
	}
}

// TestParityQuantComputedArrays covers quantifiers over ARRAY-RETURNING
// function calls (round six): `x = ANY(SPLIT(csv, ','))` and friends. A
// scalar-returning call inside ANY keeps the plain-comparison desugar.
func TestParityQuantComputedArrays(t *testing.T) {
	attrs := map[string]any{"vip": 1, "x": 2}
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"tag = ANY(SPLIT(csv, ','))", map[string]any{"tag": "b", "csv": "a,b,c"}, true},
		{"tag = ANY(SPLIT(csv, ','))", map[string]any{"tag": "z", "csv": "a,b,c"}, false},
		{"tag != ALL(SPLIT(csv, ','))", map[string]any{"tag": "z", "csv": "a,b,c"}, true},
		// numeric comparison against parsed elements
		{"n <= ALL(SPLIT(csv, ','))", map[string]any{"n": 1, "csv": "3,2,5"}, true},
		{"n <= ALL(SPLIT(csv, ','))", map[string]any{"n": 3, "csv": "3,2,5"}, false},
		// MAPKEYS / MAPVALUES / ARRAY_SLICE / HOSTS as quantifier sources
		{"k = ANY(MAPKEYS(attrs))", map[string]any{"k": "vip", "attrs": attrs}, true},
		{"v = ANY(MAPVALUES(attrs))", map[string]any{"v": "2", "attrs": attrs}, true},
		{"tag = ANY(ARRAY_SLICE(tags, 0, 2))", map[string]any{"tag": "b", "tags": []string{"a", "b", "c"}}, true},
		{"tag = ANY(ARRAY_SLICE(tags, 0, 2))", map[string]any{"tag": "c", "tags": []string{"a", "b", "c"}}, false},
		{"h = ANY(HOSTS(u1, u2))", map[string]any{"h": "x.com", "u1": "http://x.com/a", "u2": "https://y.org"}, true},
		// SQL empty-set semantics for a missing computed array
		{"tag = ANY(SPLIT(missing, ','))", map[string]any{"tag": "a"}, false},
		{"tag = ALL(SPLIT(missing, ','))", map[string]any{"tag": "a"}, true},
		// scalar-returning call: still a plain comparison, not a quantifier
		{"low = ANY(LOWER(name))", map[string]any{"low": "abc", "name": "ABC"}, true},
	})
}

// TestParityNullLikeIn locks in the AST↔VM alignment for NULL fields against
// LIKE / IN: SQL two-value semantics — NULL never matches a pattern and is
// never IN a list (the AST used to render NULL as "" and match '%' or '').
func TestParityNullLikeIn(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"missing LIKE '%'", map[string]any{}, false},
		{"missing LIKE '%x%'", map[string]any{}, false},
		{"missing NOT LIKE '%'", map[string]any{}, true},
		{"missing IN ('')", map[string]any{}, false},
		{"missing IN ('a', '')", map[string]any{}, false},
		{"missing NOT IN ('a')", map[string]any{}, true},
		{"nilfield LIKE '%'", map[string]any{"nilfield": nil}, false},
		{"nilfield IN ('')", map[string]any{"nilfield": nil}, false},
		{"present LIKE '%'", map[string]any{"present": ""}, true}, // a real empty string still matches
		{"present IN ('')", map[string]any{"present": ""}, true},
	})
}

// TestParityEmitRoundTrip verifies Emit(SQL) escaping: values and patterns
// containing quotes or backslashes must survive parse → emit → parse → emit
// (the decision-table path persists rules as emitted SQL).
func TestParityEmitRoundTrip(t *testing.T) {
	rules := []string{
		`name = 'O\'Brien'`,
		`path = 'C:\\temp'`,
		`phone REGEXP '^1\d{10}$'`,
		`name LIKE '%\d%'`,
		`SECONDS(v) = 630`,
		`UNIXTRUNC(ts, 'seconds') = '1438445529.707'`,
		`SUM(a, b) > 4`,
		`URL_MATCHQS(u, 'utm.*') = 'x.com/p'`,
		`TITLECASE(s) = 'Hello World'`,
		`tag = ANY(SPLIT(csv, ','))`,
		`k = ANY(MAPKEYS(attrs))`,
		`REGEXP_LIKE(s, '^a.b$', 'in')`, // emits as s REGEXP '(?is)^a.b$'
	}
	for _, rule := range rules {
		n1, err := ir.Parse(rule)
		if err != nil {
			t.Fatalf("parse %q: %v", rule, err)
		}
		sql1 := ir.Emit(n1, ir.SQL)
		n2, err := ir.Parse(sql1)
		if err != nil {
			t.Fatalf("re-parse emitted %q (from %q): %v", sql1, rule, err)
		}
		sql2 := ir.Emit(n2, ir.SQL)
		if sql1 != sql2 {
			t.Errorf("emit not stable for %q:\n first=%q\n second=%q", rule, sql1, sql2)
		}
	}
}

// TestParityCompileErrors verifies compile-time arity validation for the new
// builtins (wrong argument counts must fail rule load, not evaluate to NULL).
func TestParityCompileErrors(t *testing.T) {
	for _, rule := range []string{
		"SECONDS() IS NULL",
		"SECONDS(a, b) IS NULL",
		"UNIXTRUNC(a, b, c) IS NULL",
		"UNSIGN() IS NULL",
		"STRING_INDEX(s) IS NULL",
		"TITLECASE(a, b) IS NULL",
		"COUNT(a, b) IS NULL",
		"SUM() IS NULL",
	} {
		if _, err := CompileString(rule); err == nil {
			t.Errorf("expected compile error for %q", rule)
		} else if !strings.Contains(err.Error(), "argument") {
			t.Errorf("unexpected error text for %q: %v", rule, err)
		}
	}
}

// TestParityBoolFieldTerms locks in the round-five fix: the AST runtime's
// termVal used to nil BOOLEAN fields on every term-based path while the VM
// kept them (rendering "true"/"false"), so two-field comparisons, scalar
// functions, REGEXP and quantifiers over bool fields disagreed across
// runtimes. Booleans now flow through both runtimes identically.
func TestParityBoolFieldTerms(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// two-field comparison (CompareTerm) over booleans
		{"is_vip = is_active", map[string]any{"is_vip": true, "is_active": true}, true},
		{"is_vip = is_active", map[string]any{"is_vip": true, "is_active": false}, false},
		{"is_vip != is_active", map[string]any{"is_vip": true, "is_active": false}, true},
		// bool field against a function result / string literal
		{"LOWER(name) = flag", map[string]any{"name": "TRUE", "flag": true}, true},
		{"UPPER(flag) = 'TRUE'", map[string]any{"flag": true}, true},
		{"LENGTH(flag) = 4", map[string]any{"flag": true}, true},  // "true"
		{"LENGTH(flag) = 5", map[string]any{"flag": false}, true}, // "false"
		// bool field as the REGEXP operand
		{"flag REGEXP '^tr'", map[string]any{"flag": true}, true},
		{"flag NOT REGEXP '^tr'", map[string]any{"flag": false}, true},
		// quantifiers: bool left against an array / a projected bool column
		{"flag = ANY(opts)", map[string]any{"flag": true, "opts": []string{"true", "x"}}, true},
		{"flag = ANY(opts)", map[string]any{"flag": false, "opts": []string{"true", "x"}}, false},
		{"ok = ANY(SELECT passed FROM checks)", map[string]any{
			"ok": true,
			"checks": []any{
				map[string]any{"passed": false},
				map[string]any{"passed": true},
			},
		}, true},
		{"ok = ALL(SELECT passed FROM checks)", map[string]any{
			"ok": true,
			"checks": []any{
				map[string]any{"passed": false},
				map[string]any{"passed": true},
			},
		}, false},
	})
}

// TestParityJSONExprDoc locks in the companion round-five fix: a COMPUTED
// (non-field) JSON_EXTRACT document is rendered to text before parsing on
// both runtimes (the VM always consumed the stack value via asString; the
// AST used to NULL numeric/boolean expression results).
func TestParityJSONExprDoc(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"JSON_EXTRACT(LENGTH(s), '$') = 3", map[string]any{"s": "abc"}, true},
		{"JSON_EXTRACT(TOBOOL(v), '$') = 'true'", map[string]any{"v": "yes"}, true},
		{"JSON_EXTRACT(CONCAT(a, b), '$.k') = 1", map[string]any{"a": `{"k":`, "b": "1}"}, true},
		{"JSON_EXTRACT(missing, '$.k') IS NULL", map[string]any{}, true},
	})
}

// TestParitySubstrOverflow locks in the round-seven hardening: a huge
// SUBSTRING length literal used to overflow from+count into a negative slice
// bound — a panic inside Eval, which (before panic containment) would kill a
// whole worker process. 9223372036854774784 = 2^63-1024 is exactly float64-
// representable, so the int conversion is well-defined and the pre-fix code
// deterministically panicked here.
func TestParitySubstrOverflow(t *testing.T) {
	long := strings.Repeat("a", 2000)
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"LENGTH(SUBSTRING(s, 1500, 9223372036854774784)) = 501", map[string]any{"s": long}, true},
		{"SUBSTRING(s, 1, 9223372036854774784) = s", map[string]any{"s": "abc"}, true},
		{"SUBSTRING(s, 2000, 9223372036854774784) = 'a'", map[string]any{"s": long}, true},
		// negative / oversized starts stay clamped as before
		{"SUBSTRING(s, -5, 9223372036854774784) = s", map[string]any{"s": "abc"}, true},
		{"SUBSTRING(s, 4000, 9223372036854774784) = ''", map[string]any{"s": long}, true},
	})
}

// TestParityAggSubqueryUnaffected re-checks that the functional COUNT/SUM/AVG
// registrations do not disturb the aggregate sub-query grammar, which parses
// the same names inside `(SELECT ... FROM ...)`.
func TestParityAggSubqueryUnaffected(t *testing.T) {
	row := map[string]any{
		"orders": []any{
			map[string]any{"amount": 120},
			map[string]any{"amount": 80},
		},
		"a": 1, "b": 2,
	}
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"(SELECT COUNT(*) FROM orders) = 2", row, true},
		{"(SELECT SUM(amount) FROM orders) = 200", row, true},
		{"(SELECT AVG(amount) FROM orders) = 100", row, true},
		{"(SELECT SUM(amount) FROM orders WHERE amount > 100) = 120", row, true},
		// functional and sub-query forms coexist in one rule
		{"SUM(a, b) = 3 AND (SELECT COUNT(*) FROM orders) = 2", row, true},
	})
}
