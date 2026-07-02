// qlfn_more_test.go — second qlbridge-parity batch: CAST syntax, the MATCH
// row predicate, MAPKEYS/MAPVALUES, JMESPATH, USERAGENT, HASH_SIP, TODATEIN,
// DOMAINS/HOSTS and STRFTIME/EXTRACT. Deterministic cases assert bytecode ==
// ast == expected via runAgree; SipHash and strftime expectations were
// recomputed independently (SipHash-2-4 verified against the paper's vector).
//
// Run: go test ./pkg/vm/ -run TestQlfn -v
package vm

import (
	"strings"
	"testing"

	"tcg-rulex-engine/pkg/ir"
)

func TestQlfnCast(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"CAST(s AS INT) = 12", map[string]any{"s": "12"}, true},
		{"CAST(f AS INTEGER) = 3", map[string]any{"f": 3.7}, true},
		{"CAST(n AS STRING) = '5'", map[string]any{"n": 5}, true},
		{"CAST(s AS FLOAT) = 3.5", map[string]any{"s": "3.5"}, true},
		{"CAST(b AS BOOL) = 'true'", map[string]any{"b": "yes"}, true},
		{"CAST(d AS DATE) = '2026-07-02'", map[string]any{"d": "2026/07/02"}, true},
		{"CAST(d AS TIMESTAMP) = 1782950400", map[string]any{"d": "2026-07-02 00:00:00"}, true},
		{"CAST(missing AS INT) IS NULL", map[string]any{}, true},
		// nested: CAST inside a function, function inside CAST
		{"LENGTH(CAST(n AS STRING)) = 1", map[string]any{"n": 5}, true},
		{"CAST(TRIM(s) AS INT) = 7", map[string]any{"s": " 7 "}, true},
	})
}

// TestQlfnCastErrors verifies malformed CAST is rejected at parse time.
func TestQlfnCastErrors(t *testing.T) {
	bad := []string{
		"CAST(x AS BLOB) = 1", // unsupported type
		"CAST(x, INT) = 1",    // comma instead of AS
		"CAST(x AS) = 1",      // missing type
		"CAST(x INT) = 1",     // missing AS
	}
	for _, r := range bad {
		if _, err := ir.Parse(r); err == nil {
			t.Errorf("expected parse error for %q, got none", r)
		}
	}
}

func TestQlfnMatch(t *testing.T) {
	row := map[string]any{"price_usd": 10, "price_eur": 12, "name": "x", "nil_pfx_x": nil}
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"MATCH('price_')", row, true},
		{"MATCH('cost_')", row, false},
		{"MATCH('cost_', 'price_')", row, true}, // multiple prefixes = OR
		{"NOT MATCH('z_')", row, true},
		{"MATCH('nil_pfx_')", row, false}, // nil-valued field does not count
		{"MATCH('price_') AND name = 'x'", row, true},
		{"MATCH('price_')", map[string]any{}, false},
	})
}

// TestQlfnMatchErrors verifies MATCH requires non-empty string literals.
func TestQlfnMatchErrors(t *testing.T) {
	for _, r := range []string{"MATCH()", "MATCH('')", "MATCH(field)"} {
		if _, err := CompileString(r); err == nil {
			t.Errorf("expected compile error for %q, got none", r)
		}
	}
}

func TestQlfnMapFns(t *testing.T) {
	attrs := map[string]any{"b": 1, "a": 2, "c": true}
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// keys are sorted for determinism
		{"ARRAY_INDEX(MAPKEYS(attrs), 0) = 'a'", map[string]any{"attrs": attrs}, true},
		{"ARRAY_LENGTH(MAPKEYS(attrs)) = 3", map[string]any{"attrs": attrs}, true},
		{"ARRAY_CONTAINS(MAPKEYS(attrs), 'b')", map[string]any{"attrs": attrs}, true},
		{"ARRAY_CONTAINS(MAPKEYS(attrs), 'z')", map[string]any{"attrs": attrs}, false},
		// values ordered by sorted key: a->2, b->1, c->true
		{"ARRAY_INDEX(MAPVALUES(attrs), 0) = '2'", map[string]any{"attrs": attrs}, true},
		{"ARRAY_INDEX(MAPVALUES(attrs), 2) = 'true'", map[string]any{"attrs": attrs}, true},
		// JSON-object string documents work too
		{"ARRAY_CONTAINS(MAPKEYS(doc), 'city')", map[string]any{"doc": `{"city":"深圳","age":30}`}, true},
		{"MAPKEYS(missing) IS NULL", map[string]any{}, true},
		{"MAPKEYS(s) IS NULL", map[string]any{"s": "not json"}, true},
	})
}

func TestQlfnJmes(t *testing.T) {
	row := map[string]any{
		"profile": `{"city":"深圳","n":3,"vip":true,"tags":["a","b"],"addr":{"zip":"518000"}}`,
		"orders":  `[{"amount":120,"status":"paid"},{"amount":50,"status":"refunded"}]`,
		"parsed":  map[string]any{"city": "北京", "n": 5},
	}
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"JMESPATH(profile, 'city') = '深圳'", row, true},
		{"JSON_JMESPATH(profile, 'city') = '深圳'", row, true}, // alias
		{"JMESPATH(profile, 'n') = 3", row, true},
		{"JMESPATH(profile, 'vip') = 'true'", row, true}, // bool renders "true"
		{"JMESPATH(profile, 'addr.zip') = '518000'", row, true},
		{"JMESPATH(profile, 'tags[0]') = 'a'", row, true},
		{"ARRAY_CONTAINS(JMESPATH(profile, 'tags'), 'b')", row, true}, // array result composes
		{"ARRAY_LENGTH(JMESPATH(profile, 'tags')) = 2", row, true},
		{"JMESPATH(profile, 'missing') IS NULL", row, true},
		{"JMESPATH(profile, 'addr') IS NULL", row, true}, // object leaf -> NULL
		// the JMESPath language: filters and projections over an array doc
		{"JMESPATH(orders, '[0].amount') = 120", row, true},
		{"ARRAY_CONTAINS(JMESPATH(orders, \"[?status=='paid'].amount\"), '120')", row, true},
		{"JMESPATH(orders, 'length(@)') = 2", row, true},
		// pre-parsed object field
		{"JMESPATH(parsed, 'city') = '北京'", row, true},
		{"JMESPATH(missing, 'a') IS NULL", row, true},
	})
	// a bad expression fails at compile time
	if _, err := CompileString("JMESPATH(profile, '[invalid') = 1"); err == nil {
		t.Error("expected compile error for a bad JMESPath expression")
	}
}

func TestQlfnUserAgent(t *testing.T) {
	chrome := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	iphone := "Mozilla/5.0 (iPhone; CPU iPhone OS 14_6 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/14.1.1 Mobile/15E148 Safari/604.1"
	bot := "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)"
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"USERAGENT(ua, 'browser') = 'Chrome'", map[string]any{"ua": chrome}, true},
		{"USERAGENT(ua, 'engine') = 'AppleWebKit'", map[string]any{"ua": chrome}, true},
		{"USERAGENT(ua, 'mobile') = 'false'", map[string]any{"ua": chrome}, true},
		{"USERAGENT(ua, 'bot') = 'false'", map[string]any{"ua": chrome}, true},
		{"USERAGENT(ua, 'mobile') = 'true'", map[string]any{"ua": iphone}, true},
		{"USERAGENT(ua, 'bot') = 'true'", map[string]any{"ua": bot}, true},
		{"USERAGENT(ua, 'nosuchpart') IS NULL", map[string]any{"ua": chrome}, true},
		{"USERAGENT(missing, 'browser') IS NULL", map[string]any{}, true},
	})
}

func TestQlfnSipAndNets(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// SipHash-2-4 with key (0,1); expectation recomputed independently
		// (implementation verified against the SipHash paper's test vector).
		{"HASH_SIP(s) = '16397480524846279048'", map[string]any{"s": "abc"}, true},
		{"SIPHASH(s) = HASH_SIP(s)", map[string]any{"s": "whatever"}, true},
		{"HASH_SIP(missing) IS NULL", map[string]any{}, true},

		// DOMAINS / HOSTS collect over scalars and arrays, dedupe, keep order
		{
			"ARRAY_CONTAINS(DOMAINS(urls), 'example.com')",
			map[string]any{"urls": []string{"https://a.example.com/x", "https://b.other.org"}},
			true,
		},
		{
			"ARRAY_LENGTH(DOMAINS(urls)) = 2",
			map[string]any{"urls": []string{"https://a.example.com/x", "https://b.example.com", "https://b.other.org"}},
			true,
		},
		{
			"ARRAY_INDEX(HOSTS(u1, u2), 1) = 'b.org'",
			map[string]any{"u1": "https://a.com/x", "u2": "http://B.org/y"},
			true,
		},
		{"DOMAINS(missing) IS NULL", map[string]any{}, true},
	})
}

func TestQlfnStrftime(t *testing.T) {
	row := map[string]any{"ts": "2026-07-02 15:04:05", "d": "2026-07-02"}
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"STRFTIME(ts, '%Y-%m-%d %H:%M') = '2026-07-02 15:04'", row, true},
		{"EXTRACT(ts, '%H') = '15'", row, true}, // qlbridge extract = strftime
		{"EXTRACT(d, '%w') = '4'", row, true},   // Thursday, 0=Sunday
		{"STRFTIME(ts, '%j') = '183'", row, true},
		{"STRFTIME(ts, '%I %p') = '03 PM'", row, true},
		{"STRFTIME(ts, '%s') = '1783004645'", row, true}, // 2026-07-02 15:04:05 UTC
		{"STRFTIME(ts, '100%%') = '100%'", row, true},
		{"STRFTIME(missing, '%Y') IS NULL", row, true},
	})
}

func TestQlfnTodateIn(t *testing.T) {
	runAgree(t, []struct {
		rule string
		row  map[string]any
		want bool
	}{
		// 08:00 wall time in Asia/Shanghai (UTC+8, no DST) = 00:00 UTC
		{
			"TODATEIN('Asia/Shanghai', t) = '2026-07-02 00:00:00'",
			map[string]any{"t": "2026-07-02 08:00:00"},
			true,
		},
		{
			"TODATEIN('UTC', t) = '2026-07-02 08:00:00'",
			map[string]any{"t": "2026-07-02 08:00:00"},
			true,
		},
		{"TODATEIN('No/Such_Zone', t) IS NULL", map[string]any{"t": "2026-07-02 08:00:00"}, true},
		{"TODATEIN('UTC', missing) IS NULL", map[string]any{}, true},
	})
}

// TestQlfnMoreRoundTrip checks the new syntax survives emit -> parse -> emit.
func TestQlfnMoreRoundTrip(t *testing.T) {
	rules := []string{
		"MATCH('price_')",
		"MATCH('a_', 'b_') AND age >= 18",
		"ARRAY_CONTAINS(MAPKEYS(attrs), 'vip')",
		"JMESPATH(profile, 'city') = '深圳'",
		"HASH_SIP(device_id) = '123'",
		"STRFTIME(ts, '%Y-%m') = '2026-07'",
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
	// CAST desugars to its conversion function and stays stable from there
	n, err := ir.Parse("CAST(s AS INT) = 12")
	if err != nil {
		t.Fatalf("parse CAST: %v", err)
	}
	got := ir.Emit(n, ir.SQL)
	if !strings.Contains(got, "TOINT(s)") {
		t.Errorf("CAST should emit as TOINT(...), got %q", got)
	}
}
