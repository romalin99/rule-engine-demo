// like_test.go — round eight: the shared LIKE wildcard translation and the
// string-builder / regexp hardening caps.
//
// Run: go test ./pkg/sqlfn/ -run 'TestLike|TestStringOutCap|TestRegexpPatternCap' -v
package sqlfn

import (
	"regexp"
	"strings"
	"testing"
)

func TestLikeNeedsRegexp(t *testing.T) {
	cases := []struct {
		pattern string
		want    bool
	}{
		{"abc", false},
		{"abc%", false},
		{"%abc", false},
		{"%abc%", false},
		{"%", false},
		{"a%b", true},     // interior %
		{"%a%b%", true},   // interior % between edge wildcards
		{"a_b", true},     // single-char wildcard
		{`50\%`, true},    // escape
		{`a\\b`, true},    // backslash
		{"数%", false},
		{"数_码", true},
	}
	for _, c := range cases {
		if got := LikeNeedsRegexp(c.pattern); got != c.want {
			t.Errorf("LikeNeedsRegexp(%q) = %v, want %v", c.pattern, got, c.want)
		}
	}
}

func TestLikeRegexpTranslation(t *testing.T) {
	cases := []struct {
		pattern string
		match   []string
		miss    []string
	}{
		{"a%b", []string{"ab", "axxb", "a\nb"}, []string{"ba", "aB", "abx"}},
		{"a_c", []string{"abc", "a中c"}, []string{"ac", "abbc"}},
		{`50\%`, []string{"50%"}, []string{"500", "50"}},
		{`a\_b%`, []string{"a_b", "a_bcd"}, []string{"aXb"}},
		{"%.com", []string{"x.com"}, []string{"xacom"}}, // '.' stays literal
		{"a(b)_", []string{"a(b)c"}, []string{"a(bc)"}}, // metachars quoted
	}
	for _, c := range cases {
		re, err := regexp.Compile(LikeRegexp(c.pattern))
		if err != nil {
			t.Fatalf("LikeRegexp(%q) produced invalid regexp: %v", c.pattern, err)
		}
		for _, s := range c.match {
			if !re.MatchString(s) {
				t.Errorf("pattern %q (re %q) should match %q", c.pattern, re, s)
			}
		}
		for _, s := range c.miss {
			if re.MatchString(s) {
				t.Errorf("pattern %q (re %q) should NOT match %q", c.pattern, re, s)
			}
		}
	}
}

func TestLikeEscape(t *testing.T) {
	cases := [][2]string{
		{"plain", "plain"},
		{"50%", `50\%`},
		{"a_b", `a\_b`},
		{`a\b`, `a\\b`},
		{"数%码", `数\%码`},
	}
	for _, c := range cases {
		if got := LikeEscape(c[0]); got != c[1] {
			t.Errorf("LikeEscape(%q) = %q, want %q", c[0], got, c[1])
		}
	}
	// Escape must round-trip: the escaped literal, matched under the wildcard
	// grammar, matches exactly the original text.
	for _, lit := range []string{"50%", "a_b", `a\b`, "x%y_z"} {
		re := regexp.MustCompile(LikeRegexp(LikeEscape(lit)))
		if !re.MatchString(lit) {
			t.Errorf("escaped %q should match itself (re %q)", lit, re)
		}
		if re.MatchString(lit + "!") {
			t.Errorf("escaped %q should be anchored (re %q)", lit, re)
		}
	}
}

// TestStringOutCap: REPLACE/CONCAT/JOIN refuse to build results beyond
// maxStringOut (memory-amplification hardening) and return NULL instead;
// ordinary results are untouched.
func TestStringOutCap(t *testing.T) {
	call := func(name string, args ...any) any {
		b, ok := Lookup(name)
		if !ok {
			t.Fatalf("builtin %s not found", name)
		}
		return b.Fn(args)
	}
	big := strings.Repeat("a", 100_000)

	// REPLACE: 100 KB input, each 'a' grows 20× -> ~2 MB projected -> NULL.
	if got := call("REPLACE", big, "a", strings.Repeat("b", 20)); got != nil {
		t.Errorf("amplifying REPLACE should be NULL, got %d bytes", len(got.(string)))
	}
	if got := call("REPLACE", "banana", "na", "NA"); got != "baNANA" {
		t.Errorf("benign REPLACE broken: %v", got)
	}
	// Shrinking replacements on big inputs stay allowed.
	if got := call("REPLACE", big, "aa", "a"); got == nil {
		t.Errorf("shrinking REPLACE should not be capped")
	}

	// CONCAT: 11 × 100 KB > 1 MiB -> NULL; small concat fine.
	args := make([]any, 11)
	for i := range args {
		args[i] = big
	}
	if got := call("CONCAT", args...); got != nil {
		t.Errorf("oversized CONCAT should be NULL")
	}
	if got := call("CONCAT", "a", "b", "c"); got != "abc" {
		t.Errorf("benign CONCAT broken: %v", got)
	}

	// JOIN: same cap, separator counted.
	if got := call("JOIN", big, big, big, big, big, big, big, big, big, big, big, ","); got != nil {
		t.Errorf("oversized JOIN should be NULL")
	}
	if got := call("JOIN", "a", "b", "-"); got != "a-b" {
		t.Errorf("benign JOIN broken: %v", got)
	}
}

// TestRegexpPatternCap: dynamically compiled patterns (URL_MATCHQS — pattern
// may come from row data) are size-capped before hitting the regexp compiler.
func TestRegexpPatternCap(t *testing.T) {
	if _, err := regexpCached(strings.Repeat("x", MaxRegexpPattern+1)); err == nil {
		t.Errorf("oversized dynamic pattern should be rejected")
	}
	if _, err := regexpCached("^ok[0-9]+$"); err != nil {
		t.Errorf("small dynamic pattern should compile: %v", err)
	}
}
