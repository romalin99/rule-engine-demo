// qlfn_test.go — the extended builtin library (pkg/sqlfn): the qlbridge-parity
// computable factors. Every deterministic case asserts BOTH that the bytecode
// VM and the tree-walking AST runtime agree AND that the result is correct
// (via the shared runAgree helper). Expected hash / base64 / epoch / weekday
// values were computed independently (Python hashlib / datetime).
//
// Run: go test ./pkg/vm/ -run TestQlfn -v
package vm

import (
	"strings"
	"testing"

	"tcg-rulex-engine/pkg/ir"
)

func TestQlfnStrings(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"TOLOWER(name) = 'abc'", map[string]any{"name": "AbC"}, true},
		{"TOUPPER(name) = 'ABC'", map[string]any{"name": "abc"}, true},
		{"STRIP(city) = '北京'", map[string]any{"city": "  北京  "}, true},
		{"CHAR_LENGTH(name) = 3", map[string]any{"name": "数码城"}, true},
		{"REPLACE(code, '-', '_') = 'AB_12'", map[string]any{"code": "AB-12"}, true},
		{"REPLACE(code, '-') = 'AB12'", map[string]any{"code": "AB-12"}, true}, // 2-arg removes
		{"LENGTH(REPLACE(code, '-', '')) = 4", map[string]any{"code": "AB-12"}, true},
		{"REPLACE(missing, '-') IS NULL", map[string]any{}, true},
		{"TOLOWER(missing) IS NULL", map[string]any{}, true},

		// SPLIT composes with the array opcodes and ARRAY_INDEX
		{"ARRAY_LENGTH(SPLIT(csv, ',')) = 3", map[string]any{"csv": "a,b,c"}, true},
		{"ARRAY_CONTAINS(SPLIT(csv, ','), 'b')", map[string]any{"csv": "a,b,c"}, true},
		{"ARRAY_CONTAINS(SPLIT(csv, ','), 'z')", map[string]any{"csv": "a,b,c"}, false},
		{"ARRAY_INDEX(SPLIT(csv, ','), 1) = 'b'", map[string]any{"csv": "a,b,c"}, true},
		{"SPLIT(csv, '') IS NULL", map[string]any{"csv": "abc"}, true}, // empty separator

		// JOIN: trailing separator; arrays expand; NULLs are skipped
		{"JOIN(a, b, '-') = 'x-y'", map[string]any{"a": "x", "b": "y"}, true},
		{"JOIN(tags, '|') = 'vip|new'", map[string]any{"tags": []string{"vip", "new"}}, true},
		{"JOIN(missing, a, '-') = 'x'", map[string]any{"a": "x"}, true},
		{"JOIN(m1, m2, '-') IS NULL", map[string]any{}, true},

		// CONCAT: MySQL NULL propagation
		{"CONCAT(a, '-', b) = 'x-y'", map[string]any{"a": "x", "b": "y"}, true},
		{"CONCAT(a, missing) IS NULL", map[string]any{"a": "x"}, true},
		{"CONCAT(n) = '5'", map[string]any{"n": 5}, true}, // numbers render as text

		// boolean string predicates (standalone and in logic)
		{"CONTAINS(name, '码')", map[string]any{"name": "数码城"}, true},
		{"CONTAINS(name, 'z')", map[string]any{"name": "数码城"}, false},
		{"NOT CONTAINS(name, 'z')", map[string]any{"name": "数码城"}, true},
		{"CONTAINS(missing, 'a')", map[string]any{}, false}, // NULL -> false
		{"CONTAINS(LOWER(name), 'abc')", map[string]any{"name": "xABCy"}, true},
		{"STARTSWITH(phone, '139')", map[string]any{"phone": "13912345678"}, true},
		{"HASPREFIX(phone, '139')", map[string]any{"phone": "13912345678"}, true},
		{"ENDSWITH(phone, '678')", map[string]any{"phone": "13912345678"}, true},
		{"HASSUFFIX(phone, '679')", map[string]any{"phone": "13912345678"}, false},
		{"STARTSWITH(name, 'a') AND age >= 18", map[string]any{"name": "abc", "age": 20}, true},
		{"STARTSWITH(name, 'a') OR age >= 18", map[string]any{"name": "xyz", "age": 20}, true},
	})
}

func TestQlfnCompareFns(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"EQ(age, 18)", map[string]any{"age": 18}, true},
		{"EQ(name, 'x')", map[string]any{"name": "x"}, true},
		{"NE(age, 18)", map[string]any{"age": 19}, true},
		{"NE(missing, 1)", map[string]any{}, false}, // NULL -> false, even for NE
		{"GT(score, 90)", map[string]any{"score": 95}, true},
		{"GE(score, 90)", map[string]any{"score": 90}, true},
		{"LT(score, 60)", map[string]any{"score": 50}, true},
		{"LE(score, 60)", map[string]any{"score": 60}, true},
		{"GT(missing, 1)", map[string]any{}, false},
		{"GT(LENGTH(name), 2)", map[string]any{"name": "数码城"}, true},
	})
}

func TestQlfnMathAndCasts(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"SQRT(x) = 3", map[string]any{"x": 9}, true},
		{"SQRT(x) IS NULL", map[string]any{"x": -1}, true},
		{"SQRT(missing) IS NULL", map[string]any{}, true},
		{"POW(n, 10) = 1024", map[string]any{"n": 2}, true},
		{"POWER(n, 2) = 9", map[string]any{"n": 3}, true},

		{"TOINT(s) = 1234", map[string]any{"s": "1,234.9"}, true},
		{"TOINT(f) = 3", map[string]any{"f": 3.9}, true},
		{"TOINT(f) = -3", map[string]any{"f": -3.9}, true}, // truncate toward zero
		{"TOINT(s) IS NULL", map[string]any{"s": "abc"}, true},
		{"TONUMBER(s) = 3.5", map[string]any{"s": "3.5"}, true},
		{"TONUMBER(s) IS NULL", map[string]any{"s": "abc"}, true},
		{"TOSTRING(age) = '18'", map[string]any{"age": 18}, true},

		{"TOBOOL(flag) = 'true'", map[string]any{"flag": "yes"}, true},
		{"TOBOOL(flag) = 'false'", map[string]any{"flag": "off"}, true},
		{"TOBOOL(flag) = 'true'", map[string]any{"flag": true}, true}, // native bool field
		{"TOBOOL(n) = 'true'", map[string]any{"n": 1}, true},
		{"TOBOOL(s) IS NULL", map[string]any{"s": "maybe"}, true},

		{"ONEOF(missing, name) = 'bob'", map[string]any{"name": "bob"}, true},
		{"COALESCE(nick, name) = 'bob'", map[string]any{"name": "bob"}, true},
		{"ONEOF(nick, name) = 'nn'", map[string]any{"nick": "nn", "name": "bob"}, true},
		{"ONEOF(m1, m2) IS NULL", map[string]any{}, true},
	})
}

func TestQlfnDates(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// NOW / UNIX_TIMESTAMP are time-dependent: compare against far-past bounds
		{"NOW() > '2000-01-01'", map[string]any{}, true},
		{"UNIX_TIMESTAMP() > 1000000000", map[string]any{}, true},
		{"DAYOFWEEK() BETWEEN 0 AND 6", map[string]any{}, true},

		{"TODATE(d) = '2026-07-02'", map[string]any{"d": "2026/07/02"}, true},
		{"TODATE(d) = '2026-07-02'", map[string]any{"d": "2026-07-02 10:30:00"}, true},
		{"TODATE('01/02/2006', d) = '2026-06-15'", map[string]any{"d": "06/15/2026"}, true},
		{"TODATE(d) IS NULL", map[string]any{"d": "not a date"}, true},

		// 2026-07-02 00:00:00 (parsed as UTC) = 1782950400
		{"TOTIMESTAMP(d) = 1782950400", map[string]any{"d": "2026-07-02 00:00:00"}, true},
		{"UNIX_TIMESTAMP(d) = 1782950400", map[string]any{"d": "2026-07-02 00:00:00"}, true},
		{"TOTIMESTAMP(missing) IS NULL", map[string]any{}, true},

		{"HOUR(ts) = 15", map[string]any{"ts": "2026-07-02 15:04:05"}, true},
		{"MINUTE(ts) = 4", map[string]any{"ts": "2026-07-02 15:04:05"}, true},
		{"SECOND(ts) = 5", map[string]any{"ts": "2026-07-02 15:04:05"}, true},
		{"HOUR(d) = 0", map[string]any{"d": "2026-07-02"}, true}, // date-only = midnight
		{"HOUR(missing) IS NULL", map[string]any{}, true},

		// 2026-07-02 is a Thursday: Go weekday 4 (0 = Sunday)
		{"DAYOFWEEK(d) = 4", map[string]any{"d": "2026-07-02"}, true},
		{"HOUROFDAY(ts) = 15", map[string]any{"ts": "2026-07-02 15:04:05"}, true},
		{"HOUROFWEEK(ts) = 111", map[string]any{"ts": "2026-07-02 15:04:05"}, true}, // 4*24+15
		{"MONTHOFYEAR(d) = 7", map[string]any{"d": "2026-07-02"}, true},
		{"YY(d) = 26", map[string]any{"d": "2026-07-02"}, true},
		{"MM(d) = 7", map[string]any{"d": "2026-07-02"}, true},
		{"YYMM(d) = '2607'", map[string]any{"d": "2026-07-02"}, true},
	})
}

func TestQlfnEmailAndURL(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"EMAIL(e) = 'bob@example.com'", map[string]any{"e": " Bob <BOB@Example.COM> "}, true},
		{"EMAILNAME(e) = 'bob'", map[string]any{"e": "BOB@Example.com"}, true},
		{"EMAILDOMAIN(e) = 'example.com'", map[string]any{"e": "BOB@Example.com"}, true},
		{"EMAIL(e) IS NULL", map[string]any{"e": "not-an-email"}, true},
		{"EMAIL(missing) IS NULL", map[string]any{}, true},

		{"HOST(u) = 'www.example.com'", map[string]any{"u": "https://www.Example.com:8080/a/b?x=1&y=2"}, true},
		{"DOMAIN(u) = 'example.com'", map[string]any{"u": "https://www.Example.com:8080/a/b?x=1"}, true},
		{"URLPATH(u) = '/a/b'", map[string]any{"u": "https://www.example.com/a/b?x=1"}, true},
		{"PATH(u) = '/a/b'", map[string]any{"u": "https://www.example.com/a/b?x=1"}, true},
		{"QS(u, 'x') = '1'", map[string]any{"u": "https://www.example.com/a/b?x=1&y=2"}, true},
		{"QS(u, 'z') IS NULL", map[string]any{"u": "https://www.example.com/a/b?x=1"}, true},
		{"HOST(u) = 'example.com'", map[string]any{"u": "example.com/path"}, true}, // scheme-less
		{"URLDECODE(s) = 'a b'", map[string]any{"s": "a%20b"}, true},
		{"URLMAIN(u) = 'https://e.com/a/b'", map[string]any{"u": "https://e.com/a/b?x=1&y=2"}, true},
		{"URLMINUSQS(u, 'a') = 'http://e.com/p?b=2'", map[string]any{"u": "http://e.com/p?a=1&b=2"}, true},
		{"HOST(u) IS NULL", map[string]any{"u": "   "}, true},
	})
}

func TestQlfnHashesAndEncoding(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"MD5(s) = '900150983cd24fb0d6963f7d28e17f72'", map[string]any{"s": "abc"}, true},
		{"HASH_MD5(s) = '900150983cd24fb0d6963f7d28e17f72'", map[string]any{"s": "abc"}, true},
		{"SHA1(s) = 'a9993e364706816aba3e25717850c26c9cd0d89d'", map[string]any{"s": "abc"}, true},
		{"SHA256(s) = 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'", map[string]any{"s": "abc"}, true},
		{"SHA512(s) = 'ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f'", map[string]any{"s": "abc"}, true},
		{"MD5(missing) IS NULL", map[string]any{}, true},
		{"B64ENCODE(s) = 'aGVsbG8='", map[string]any{"s": "hello"}, true},
		{"B64DECODE(b) = 'hello'", map[string]any{"b": "aGVsbG8="}, true},
		{"B64DECODE(bad) IS NULL", map[string]any{"bad": "!!!"}, true},
	})
}

func TestQlfnArrays(t *testing.T) {
	tags := []string{"a", "b", "c"}
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"ARRAY_INDEX(tags, 0) = 'a'", map[string]any{"tags": tags}, true},
		{"ARRAY_INDEX(tags, 2) = 'c'", map[string]any{"tags": tags}, true},
		{"ARRAY_INDEX(tags, 5) IS NULL", map[string]any{"tags": tags}, true},
		{"ARRAY_INDEX(tags, -1) IS NULL", map[string]any{"tags": tags}, true},
		{"ARRAY_INDEX(ids, 1) = 5", map[string]any{"ids": []any{1, 5, 9}}, true}, // numeric arrays are text

		{"ARRAY_LENGTH(ARRAY_SLICE(tags, 1)) = 2", map[string]any{"tags": tags}, true},
		{"ARRAY_LENGTH(ARRAY_SLICE(tags, 0, 2)) = 2", map[string]any{"tags": tags}, true},
		{"ARRAY_INDEX(ARRAY_SLICE(tags, 1), 0) = 'b'", map[string]any{"tags": tags}, true},
		{"ARRAY_LENGTH(ARRAY_SLICE(tags, -2)) = 2", map[string]any{"tags": tags}, true},   // negative = from end
		{"ARRAY_LENGTH(ARRAY_SLICE(tags, 2, 1)) = 0", map[string]any{"tags": tags}, true}, // inverted -> empty
		{"ARRAY_SLICE(missing, 0) IS NULL", map[string]any{}, true},

		// LENGTH of an array = element count on BOTH runtimes (bug fix: the VM
		// used to answer 0 — rune count of "" — while the AST answered NULL)
		{"LENGTH(tags) = 3", map[string]any{"tags": tags}, true},
		{"LEN(tags) = 3", map[string]any{"tags": tags}, true},
		{"LENGTH(SPLIT(csv, ',')) = 2", map[string]any{"csv": "x,y"}, true},

		// string functions on an array operand are NULL on BOTH runtimes
		// (bug fix: the VM used to transform the empty string instead)
		{"UPPER(tags) IS NULL", map[string]any{"tags": tags}, true},
		{"SUBSTRING(tags, 1, 2) IS NULL", map[string]any{"tags": tags}, true},
	})
}

// TestQlfnArityErrors verifies argument counts are rejected at compile time.
func TestQlfnArityErrors(t *testing.T) {
	bad := []string{
		"SQRT() = 1",
		"SQRT(a, b) = 1",
		"NOW(x) > '2000-01-01'",
		"QS(u) = '1'",
		"CONTAINS(a)",
		"TODATE(a, b, c) = '2026-01-01'",
		"NOSUCHFN(a) = 1",
	}
	for _, r := range bad {
		if _, err := CompileString(r); err == nil {
			t.Errorf("expected compile error for %q, got none", r)
		}
	}
}

// TestQlfnRoundTrip checks the extended-builtin syntax survives an
// emit -> parse -> emit cycle unchanged (Optimize dedup / rule export).
func TestQlfnRoundTrip(t *testing.T) {
	rules := []string{
		"CONTAINS(name, '码')",
		"STARTSWITH(name, 'a') AND ENDSWITH(name, 'z')",
		"TOINT(s) = 1234",
		"ARRAY_INDEX(SPLIT(csv, ','), 1) = 'b'",
		"NOW() >= '2000-01-01'",
		"EMAILDOMAIN(email) = 'example.com'",
		"GT(LENGTH(name), 2)",
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

// TestQlfnCaseInsensitive verifies the extended builtins parse in lower case
// too (function names are case-insensitive, like the core functions).
func TestQlfnCaseInsensitive(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"contains(name, '码')", map[string]any{"name": "数码城"}, true},
		{"toint(s) = 42", map[string]any{"s": "42"}, true},
		{"md5(s) = '900150983cd24fb0d6963f7d28e17f72'", map[string]any{"s": "abc"}, true},
	})
}

// TestQlfnEmitSQL spot-checks the SQL emitted for extended calls (generic
// FN(args) rendering, PredCall included).
func TestQlfnEmitSQL(t *testing.T) {
	n, err := ir.Parse("contains(name, 'x') AND toint(s) = 1")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	got := ir.Emit(n, ir.SQL)
	for _, want := range []string{"CONTAINS(name, 'x')", "TOINT(s) = 1"} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted SQL %q does not contain %q", got, want)
		}
	}
}
