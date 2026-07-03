// sqlplus_test.go — round fourteen: the common-SQL function set (strings /
// math / dates / arrays / regexp trio / JSON quartet) and the IN (SELECT ...)
// sub-query form. Every case asserts bytecode == ast AND the expected value;
// calendar/rune/MySQL-semantic expectations were derived independently in
// Python before the implementations were written.
package vm

import (
	"strings"
	"testing"

	"tcg-rulex-engine/pkg/ir"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

func TestSQLPlusStrings(t *testing.T) {
	row := map[string]any{"name": "数码城", "s": "  hi  ", "host": "www.mysql.com", "ab": "abcb"}
	cases := []struct {
		rule string
		want bool
	}{
		{"LTRIM(s) = 'hi  '", true},
		{"RTRIM(s) = '  hi'", true},
		{"LEFT(name, 2) = '数码'", true},   // rune-based
		{"LEFT(name, 9) = '数码城'", true},  // clamped
		{"LEFT(name, -1) = ''", true},
		{"RIGHT(name, 1) = '城'", true},
		{"REVERSE(name) = '城码数'", true},
		{"REPEAT('ab', 3) = 'ababab'", true},
		{"REPEAT('ab', 0) = ''", true},
		{"LPAD('5', 3, '0') = '005'", true},
		{"LPAD('ab', 5, 'xy') = 'xyxab'", true},  // MySQL pad cycling
		{"LPAD('hello', 3, '*') = 'hel'", true},  // truncation to n
		{"RPAD('ab', 5, 'xy') = 'abxyx'", true},
		{"LPAD('a', 4, '') = ''", true},          // empty pad (MySQL)
		{"LOCATE('码', name) = 2", true},          // 1-based rune position
		{"LOCATE('b', ab, 3) = 4", true},         // pos argument
		{"LOCATE('x', ab) = 0", true},            // absent -> 0 (MySQL)
		{"INSTR(name, '城') = 3", true},           // swapped args
		{"SUBSTRING_INDEX(host, '.', 2) = 'www.mysql'", true},
		{"SUBSTRING_INDEX(host, '.', -2) = 'mysql.com'", true},
		{"SUBSTRING_INDEX(host, '|', 1) = host", true}, // delim absent -> whole
		{"SUBSTRING_INDEX(host, '.', 0) = ''", true},
		// overlapping delimiters follow left-to-right non-overlapping (split/
		// MySQL) numbering — caught by the Python differential check
		{"SUBSTRING_INDEX('深深深', '深深', -1) = '深'", true},
		{"SUBSTRING_INDEX(',深b深深深a,', '深深', -1) = '深a,'", true},
		{"INITCAP('hello world') = 'Hello World'", true},
		// NULL propagation
		{"LEFT(missing, 2) IS NULL", true},
		{"LOCATE('a', missing) IS NULL", true},
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, row); got != c.want {
			t.Errorf("%q = %v, want %v", c.rule, got, c.want)
		}
	}
}

func TestSQLPlusMath(t *testing.T) {
	row := map[string]any{"x": 29, "neg": -29, "f": 1.999}
	cases := []struct {
		rule string
		want bool
	}{
		{"MOD(x, 9) = 2", true},
		{"MOD(neg, 9) = -2", true}, // sign of dividend (MySQL)
		{"MOD(x, 0) IS NULL", true},
		{"SIGN(neg) = -1", true},
		{"SIGN(0) = 0", true},
		{"SIGN(x) = 1", true},
		{"TRUNCATE(f, 1) = 1.9", true},
		{"TRUNCATE(-1.999, 1) = -1.9", true}, // toward zero
		{"TRUNCATE(199, -2) = 100", true},
		{"GREATEST(3, 7, 5) = 7", true},
		{"LEAST(3, 7, 5) = 3", true},
		{"GREATEST('a', 'c', 'b') = 'c'", true},
		{"GREATEST(x, missing) IS NULL", true}, // any NULL -> NULL (MySQL)
		{"EXP(0) = 1", true},
		{"LN(1) = 0", true},
		{"LN(0) IS NULL", true},
		{"LOG(2, 8) = 3", true}, // MySQL argument order LOG(base, x)
		{"LOG10(1000) = 3", true},
		{"LOG2(8) = 3", true},
		{"PI() > 3.14 AND PI() < 3.15", true},
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, row); got != c.want {
			t.Errorf("%q = %v, want %v", c.rule, got, c.want)
		}
	}
}

func TestSQLPlusDates(t *testing.T) {
	row := map[string]any{"d": "2026-07-03 09:05:07", "feb": "2026-02-10", "a": "2026-01-31", "b": "2026-02-28"}
	cases := []struct {
		rule string
		want bool
	}{
		{"QUARTER(d) = 3", true},
		{"WEEKOFYEAR(d) = 27", true}, // ISO week (Python-verified)
		{"WEEKOFYEAR('2026-01-01') = 1", true},
		{"DAYOFYEAR(d) = 184", true},
		{"DAYOFMONTH(d) = 3", true},
		{"MONTHNAME(d) = 'July'", true},
		{"DAYNAME(d) = 'Friday'", true},
		{"LAST_DAY(feb) = '2026-02-28'", true}, // 2026 is not a leap year
		{"DATE(d) = '2026-07-03'", true},
		{"TIME(d) = '09:05:07'", true},
		// DATE_FORMAT: MySQL codes (%i = minutes, %M = month name, %k unpadded)
		{"DATE_FORMAT(d, '%Y-%m-%d %H:%i:%s') = '2026-07-03 09:05:07'", true},
		{"DATE_FORMAT(d, '%W %M %e, %Y') = 'Friday July 3, 2026'", true},
		{"DATE_FORMAT(d, '%r') = '09:05:07 AM'", true},
		{"DATE_FORMAT(d, '%k') = '9'", true},
		{"DATE_FORMAT(d, '100%%') = '100%'", true},
		// TIMESTAMPDIFF: bare MySQL unit spelling and calendar-true months
		{"TIMESTAMPDIFF(DAY, a, b) = 28", true},
		{"TIMESTAMPDIFF(MONTH, a, b) = 0", true},  // Jan 31 -> Feb 28: partial
		{"TIMESTAMPDIFF(MONTH, a, '2026-03-31') = 2", true},
		{"TIMESTAMPDIFF(MONTH, '2026-03-15', '2026-01-15') = -2", true},
		{"TIMESTAMPDIFF(MONTH, '2026-01-15', '2026-03-14') = 1", true},
		// month-end normalization trap: MySQL compares day+clock tuples, so
		// Jul 31 -> Mar 1 is 7 (an AddDate anchor would roll Feb 31 -> Mar 3
		// and answer 6) — caught by the Python differential check
		{"TIMESTAMPDIFF(MONTH, '2026-07-31', '2027-03-01') = 7", true},
		{"TIMESTAMPDIFF(MONTH, '2027-03-01', '2026-07-31') = -7", true},
		{"TIMESTAMPDIFF(YEAR, '2020-06-15', d) = 6", true},
		{"TIMESTAMPDIFF(QUARTER, '2026-01-01', d) = 2", true},
		{"TIMESTAMPDIFF(HOUR, '2026-07-03 00:00:00', d) = 9", true},
		{"TIMESTAMPDIFF('MINUTE', '2026-07-03 09:00:00', d) = 5", true}, // quoted unit
		{"TIMESTAMPDIFF(WEEK, '2026-06-01', d) = 4", true},
		{"TIMESTAMPDIFF(DAY, d, CURRENT_DATE) >= 0", true},
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, row); got != c.want {
			t.Errorf("%q = %v, want %v", c.rule, got, c.want)
		}
		// every date rule must emit -> reparse -> evaluate identically
		node, err := ir.Parse(c.rule)
		if err != nil {
			t.Fatalf("parse %q: %v", c.rule, err)
		}
		if got := evalBoth(t, ir.Emit(node, ir.SQL), row); got != c.want {
			t.Errorf("round trip of %q = %v, want %v", c.rule, got, c.want)
		}
	}
	// bad unit is a load-time error (bare spelling), not a silent NULL
	if _, err := ir.Parse("TIMESTAMPDIFF(FORTNIGHT, a, b) = 1"); err == nil {
		t.Errorf("TIMESTAMPDIFF(FORTNIGHT, ...) should be a parse error")
	}
}

func TestSQLPlusArrays(t *testing.T) {
	row := map[string]any{
		"tags": []any{"b", "a", "b", "c"},
		"nums": []any{3, 1, 7, 5},
		"mix":  []any{"x", 2, "y"},
	}
	cases := []struct {
		rule string
		want bool
	}{
		{"ARRAY_MIN(nums) = 1", true},
		{"ARRAY_MAX(nums) = 7", true},
		{"ARRAY_MIN(mix) = 2", true},   // non-numeric elements skipped
		{"ARRAY_MIN(tags) IS NULL", true},
		{"ARRAY_LENGTH(ARRAY_DISTINCT(tags)) = 3", true},
		{"ARRAY_POSITION(tags, 'a') = 2", true}, // 1-based
		{"ARRAY_POSITION(tags, 'z') IS NULL", true},
		{"'c' = ANY (ARRAY_DISTINCT(tags))", true}, // quantifier source
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, row); got != c.want {
			t.Errorf("%q = %v, want %v", c.rule, got, c.want)
		}
	}
}

func TestSQLPlusRegexpFns(t *testing.T) {
	row := map[string]any{"s": "abc123def456", "cn": "深圳abc123", "p": "a1b2"}
	cases := []struct {
		rule string
		want bool
	}{
		{"REGEXP_SUBSTR(s, '[0-9]+') = '123'", true},
		{"REGEXP_SUBSTR(s, '[0-9]+', 1, 2) = '456'", true}, // occurrence
		{"REGEXP_SUBSTR(s, '[0-9]+', 7) = '456'", true},    // start position
		{"REGEXP_SUBSTR(s, 'zzz') IS NULL", true},
		{"REGEXP_INSTR(s, '[0-9]') = 4", true},
		{"REGEXP_INSTR(cn, '[0-9]') = 6", true}, // RUNE position, Chinese-safe
		{"REGEXP_INSTR(s, 'zzz') = 0", true},    // no match -> 0 (MySQL)
		{"REGEXP_REPLACE(p, '[0-9]', '#') = 'a#b#'", true},
		{"REGEXP_REPLACE(p, '(a)(1)', '$2$1') = '1ab2'", true}, // group refs
		{"REGEXP_SUBSTR(s, '[0-9]+', 0) IS NULL", true},        // pos < 1
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, row); got != c.want {
			t.Errorf("%q = %v, want %v", c.rule, got, c.want)
		}
	}
}

func TestSQLPlusJSON(t *testing.T) {
	doc := `{"city":"深圳","tags":["vip","new"],"addr":{"zip":"518000","geo":[114,22]},"n":3,"ok":true}`
	rows := map[string]map[string]any{
		"string ": {"profile": doc, "num": 7},
		"decoded": {"profile": map[string]any{
			"city": "深圳", "tags": []any{"vip", "new"},
			"addr": map[string]any{"zip": "518000", "geo": []any{114.0, 22.0}},
			"n":    3.0, "ok": true,
		}, "num": 7},
	}
	cases := []struct {
		rule string
		want bool
	}{
		{"JSON_LENGTH(profile) = 5", true},
		{"JSON_LENGTH(profile, '$.tags') = 2", true},
		{"JSON_LENGTH(profile, '$.city') = 1", true}, // scalar -> 1 (MySQL)
		{"JSON_LENGTH(profile, '$.nope') IS NULL", true},
		{"JSON_TYPE(profile) = 'OBJECT'", true},
		{"JSON_TYPE(profile, '$.tags') = 'ARRAY'", true},
		{"JSON_TYPE(profile, '$.city') = 'STRING'", true},
		{"JSON_TYPE(profile, '$.n') = 'NUMBER'", true},
		{"JSON_TYPE(profile, '$.ok') = 'BOOLEAN'", true},
		{"JSON_VALID(profile)", true},
		{"NOT (JSON_VALID(num))", true}, // a number field is not a JSON doc
		{"JSON_CONTAINS(profile, '深圳', '$.city')", true},   // bare string candidate
		{"JSON_CONTAINS(profile, '\"深圳\"', '$.city')", true}, // MySQL quoted form
		{"JSON_CONTAINS(profile, 'vip', '$.tags')", true},   // scalar in array
		{"JSON_CONTAINS(profile, '[\"vip\",\"new\"]', '$.tags')", true},
		{"JSON_CONTAINS(profile, '{\"zip\":\"518000\"}', '$.addr')", true}, // object subset
		{"JSON_CONTAINS(profile, '999', '$.addr.geo')", false},
		{"JSON_CONTAINS(profile, '114', '$.addr.geo')", true}, // numeric equality
		{"NOT (JSON_CONTAINS(profile, 'gold', '$.tags'))", true},
	}
	for name, row := range rows {
		for _, c := range cases {
			if got := evalBoth(t, c.rule, row); got != c.want {
				t.Errorf("[%s] %q = %v, want %v", name, c.rule, got, c.want)
			}
		}
	}
	// [[1]] does not contain 1 (MySQL top-level rule); [1,2] contains [1,2]
	nested := map[string]any{"j": `[[1]]`, "flat": `[1,2,3]`}
	for rule, want := range map[string]bool{
		"JSON_CONTAINS(j, '1')":         false,
		"JSON_CONTAINS(flat, '1')":      true,
		"JSON_CONTAINS(flat, '[1,2]')":  true,
		"JSON_CONTAINS(flat, '[1,9]')":  false,
		"JSON_LENGTH(j) = 1":            true,
		"JSON_TYPE(flat) = 'ARRAY'":     true,
		"JSON_VALID(missing)":           false,
	} {
		if got := evalBoth(t, rule, nested); got != want {
			t.Errorf("%q = %v, want %v", rule, got, want)
		}
	}
	// typed nested-row collections are JSON array documents too
	typed := map[string]any{"orders": []map[string]any{{"amount": 100.0}, {"amount": 50.0}}}
	for rule, want := range map[string]bool{
		"JSON_LENGTH(orders) = 2":                      true,
		"JSON_TYPE(orders) = 'ARRAY'":                  true,
		"JSON_VALID(orders)":                           true,
		"JSON_CONTAINS(orders, '{\"amount\":100}')":    true,
		"JSON_CONTAINS(orders, '{\"amount\":999}')":    false,
		"JSON_EXTRACT(orders, '$[0].amount') = 100":    true, // jsonRoot upgrade
	} {
		if got := evalBoth(t, rule, typed); got != want {
			t.Errorf("%q = %v, want %v", rule, got, want)
		}
	}
	// path arguments must be literals (load-time error, not silent NULL)
	for _, bad := range []string{
		"JSON_LENGTH(profile, city) = 1",
		"JSON_CONTAINS(profile, 'x', city)",
	} {
		node, err := ir.Parse(bad)
		if err != nil {
			t.Fatalf("parse %q: %v", bad, err)
		}
		if _, err := Compile(node); err == nil {
			t.Errorf("Compile(%q) should fail (non-literal path)", bad)
		}
	}
}

func TestInSubquery(t *testing.T) {
	row := map[string]any{
		"status": "已付",
		"orders": []any{
			map[string]any{"status": "已付", "amount": 100},
			map[string]any{"status": "退款", "amount": 50},
		},
	}
	empty := map[string]any{"status": "已付"}
	cases := []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"status IN (SELECT status FROM orders)", row, true},
		{"'退款' IN (SELECT status FROM orders)", row, true},
		{"'作废' IN (SELECT status FROM orders)", row, false},
		{"status NOT IN (SELECT status FROM orders)", row, false},
		{"'作废' NOT IN (SELECT status FROM orders)", row, true},
		{"100 IN (SELECT amount FROM orders WHERE status = '已付')", row, true},
		{"50 IN (SELECT amount FROM orders WHERE status = '已付')", row, false},
		{"LOWER(status) IN (SELECT status FROM orders)", row, true}, // term left
		// empty collection: IN -> false, NOT IN -> vacuously true (!= ALL)
		{"status IN (SELECT status FROM orders)", empty, false},
		{"status NOT IN (SELECT status FROM orders)", empty, true},
		// missing left operand never matches; NOT IN over NULL stays two-valued
		{"missing IN (SELECT status FROM orders)", row, false},
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, c.row); got != c.want {
			t.Errorf("%q = %v, want %v", c.rule, got, c.want)
		}
		node, err := ir.Parse(c.rule)
		if err != nil {
			t.Fatalf("parse %q: %v", c.rule, err)
		}
		if got := evalBoth(t, ir.Emit(node, ir.SQL), c.row); got != c.want {
			t.Errorf("round trip of %q = %v, want %v", c.rule, got, c.want)
		}
	}
	// projection is mandatory: IN (SELECT * FROM coll) is not membership
	if _, err := ir.Parse("status IN (SELECT * FROM orders)"); err == nil {
		t.Errorf("IN (SELECT * ...) should be a parse error")
	}
}

// TestSQLPlusOutputCaps pins the memory-amplification guards of the new
// string builders (see docs/security_review.md §14).
func TestSQLPlusOutputCaps(t *testing.T) {
	row := map[string]any{
		"big":  strings.Repeat("a", 600_000),
		"tiny": "ab",
	}
	cases := []struct {
		rule string
		want bool
	}{
		{"REPEAT(big, 3) IS NULL", true},               // 1.8 MB projected -> NULL
		{"LENGTH(REPEAT(tiny, 3)) = 6", true},          // benign use unaffected
		{"LPAD(tiny, 2000000, 'x') IS NULL", true},     // n beyond cap -> NULL
		{"REGEXP_REPLACE(big, 'a', '##') IS NULL", true}, // 1.8 MB projected -> NULL
		{"REGEXP_REPLACE(tiny, 'a', '##') = '##b'", true},
	}
	for _, c := range cases {
		if got := evalBoth(t, c.rule, row); got != c.want {
			t.Errorf("%q = %v, want %v", c.rule, got, c.want)
		}
	}
}

// TestSQLPlusIDStability guards the append-only builtin registry: the round-
// fourteen batch must not shift any earlier builtin's OpCallB ID (programs
// compiled before/after in one process must agree).
func TestSQLPlusIDStability(t *testing.T) {
	row := map[string]any{"csv": "a,b", "u": "https://x.cn/p?q=1"}
	for rule, want := range map[string]bool{
		"ARRAY_LENGTH(SPLIT(csv, ',')) = 2": true, // early-batch builtins still wired
		"CONTAINS(csv, 'a')":                true,
		"HOST(u) = 'x.cn'":                  true,
		"TOINT('12') = 12":                  true,
	} {
		if got := evalBoth(t, rule, row); got != want {
			t.Errorf("%q = %v, want %v", rule, got, want)
		}
	}
	if _, err := astrt.New().Compile(nil); err == nil {
		t.Errorf("ast Compile(nil) should error") // unchanged contract
	}
}
