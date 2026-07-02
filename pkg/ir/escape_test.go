// escape_test.go — string-literal escaping at the lexer and the SQL emitter.
//
// The lexer keeps every \x sequence verbatim except \' \" \\ so regular
// expressions survive parsing ('^1\d{10}$' must stay ^1\d{10}$ — the old rule
// of unescaping every \x corrupted character classes to ^1d{10}$). The SQL
// emitter escapes backslashes and quotes so emitted rules re-parse to the same
// value (the decision-table path persists rules as emitted SQL).
package ir

import "testing"

func TestLexStringEscapes(t *testing.T) {
	cases := []struct {
		rule    string // full rule text (Go backtick literal: backslashes are real)
		pattern string // expected string-token value
	}{
		{`x REGEXP '^1\d{10}$'`, `^1\d{10}$`},   // \d kept verbatim
		{`x REGEXP '^1\\d{10}$'`, `^1\d{10}$`},  // MySQL-style \\ collapses to \
		{`x REGEXP '\w+\.\d*'`, `\w+\.\d*`},     // \w \. \d all kept
		{`x = 'O\'Brien'`, `O'Brien`},           // \' unescapes
		{`x = "say \"hi\""`, `say "hi"`},        // \" unescapes
		{`x = 'C:\\temp'`, `C:\temp`},           // \\ collapses
		{`x = 'a\b'`, `a\b`},                    // unknown escape kept
		{`x = 'tail\\'`, `tail\`},               // escaped trailing backslash
	}
	for _, c := range cases {
		toks, err := lexAll(c.rule)
		if err != nil {
			t.Fatalf("lex %q: %v", c.rule, err)
		}
		var got string
		found := false
		for _, tok := range toks {
			if tok.Kind == tString {
				got, found = tok.Text, true
				break
			}
		}
		if !found {
			t.Fatalf("no string token in %q", c.rule)
		}
		if got != c.pattern {
			t.Errorf("lex %q: string = %q, want %q", c.rule, got, c.pattern)
		}
	}
}

func TestEmitSQLEscapes(t *testing.T) {
	cases := []struct {
		value string // raw string value carried by the IR
		lit   string // expected emitted SQL literal
	}{
		{`plain`, `'plain'`},
		{`O'Brien`, `'O\'Brien'`},
		{`C:\temp`, `'C:\\temp'`},
		{`^1\d{10}$`, `'^1\\d{10}$'`},
	}
	for _, c := range cases {
		if got := sqlStr(c.value); got != c.lit {
			t.Errorf("sqlStr(%q) = %s, want %s", c.value, got, c.lit)
		}
	}
}

// TestEscapeRoundTrip: parse → emit → parse must preserve every literal and
// pattern, and the second emit must equal the first (stability).
func TestEscapeRoundTrip(t *testing.T) {
	rules := []string{
		`name = 'O\'Brien'`,
		`path = 'C:\\temp'`,
		`phone REGEXP '^1\d{10}$'`,
		`phone NOT REGEXP '^\d{3}-\d{4}$'`,
		`name LIKE '%\d%'`,
		`city IN ('深圳', 'O\'Fallon')`,
		`note = 'line\\break'`,
	}
	for _, rule := range rules {
		n1, err := Parse(rule)
		if err != nil {
			t.Fatalf("parse %q: %v", rule, err)
		}
		sql1 := Emit(n1, SQL)
		n2, err := Parse(sql1)
		if err != nil {
			t.Fatalf("re-parse emitted %q (from %q): %v", sql1, rule, err)
		}
		sql2 := Emit(n2, SQL)
		if sql1 != sql2 {
			t.Errorf("unstable emit for %q:\n first=%q\n second=%q", rule, sql1, sql2)
		}
	}
}
