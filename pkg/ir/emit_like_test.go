// emit_like_test.go — round eleven: LIKE emission preserves semantics across
// the Wildcards divide.
//
//   - Legacy nodes (Wildcards=false, externally built IR / pre-round-8
//     exports) are re-encoded on emit — '_' / '\' / interior '%' escaped —
//     because the SQL and JSON front-ends re-parse emitted rules under the
//     full wildcard grammar. Without this, `startsWith('a_b')`-style nodes
//     silently changed meaning across an emit → parse round trip.
//   - Wildcard nodes emit verbatim (and still re-parse to Wildcards=true).
//   - The CEL/Expr/Aviator emitters resolve wildcard escapes back into
//     literals (`50\%%` → startsWith('50%')) and emit nothing for patterns
//     those DSLs cannot express ('_' / interior '%').
//
// Run: go test ./pkg/ir/ -run TestEmitLike -v
package ir

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEmitLikeSQLLegacyReencoding(t *testing.T) {
	cases := []struct {
		node Node
		want string
	}{
		// legacy '_' must come back escaped (sqlStr doubles the backslash)
		{Like{Field: "name", Pattern: "a_b%"}, `name LIKE 'a\\_b%'`},
		// legacy interior '%' was literal equality — re-encoded as \%
		{Like{Field: "name", Pattern: "a%b"}, `name LIKE 'a\\%b'`},
		// legacy contains with inner '_' keeps structural edge wildcards
		{Like{Field: "name", Pattern: "%a_c%"}, `name LIKE '%a\\_c%'`},
		// plain legacy patterns are unchanged
		{Like{Field: "name", Pattern: "abc%"}, `name LIKE 'abc%'`},
		{Like{Field: "name", Pattern: "%abc", Negate: true}, `name NOT LIKE '%abc'`},
		// wildcard nodes emit verbatim (backslash doubled by sqlStr only)
		{Like{Field: "name", Pattern: `a\_b%`, Wildcards: true}, `name LIKE 'a\\_b%'`},
		{Like{Field: "name", Pattern: "a_b%", Wildcards: true}, `name LIKE 'a_b%'`},
		{LikeTerm{Left: CallTerm{Fn: "LOWER", Args: []Term{FieldTerm{Name: "name"}}}, Pattern: "a_c"},
			`LOWER(name) LIKE 'a\\_c'`},
	}
	for _, c := range cases {
		if got := Emit(c.node, SQL); got != c.want {
			t.Errorf("Emit(%#v, SQL) = %q, want %q", c.node, got, c.want)
		}
	}
}

// TestEmitLikeSQLRoundTripStable: emitted legacy patterns re-parse to
// Wildcards=true nodes whose emitted SQL is stable (fixed point after one hop).
func TestEmitLikeSQLRoundTripStable(t *testing.T) {
	for _, n := range []Node{
		Like{Field: "name", Pattern: "a_b%"},
		Like{Field: "name", Pattern: "a%b"},
		Like{Field: "name", Pattern: "%a_c%", Negate: true},
		Like{Field: "name", Pattern: `50\%`}, // legacy: prefix '50\'
	} {
		s1 := Emit(n, SQL)
		p1, err := Parse(s1)
		if err != nil {
			t.Fatalf("re-parse %q: %v", s1, err)
		}
		l1, ok := p1.(Like)
		if !ok || !l1.Wildcards {
			t.Fatalf("%q should re-parse to a Wildcards Like, got %#v", s1, p1)
		}
		if s2 := Emit(p1, SQL); s1 != s2 {
			t.Errorf("round trip not stable: %q -> %q", s1, s2)
		}
	}
}

func TestEmitLikeJSONLegacyReencoding(t *testing.T) {
	out, err := EmitJSON(Like{Field: "name", Pattern: "a_b%"})
	if err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	var doc struct {
		Field string `json:"field"`
		Op    string `json:"op"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("bad JSON %q: %v", out, err)
	}
	if doc.Value != `a\_b%` || doc.Op != "like" || doc.Field != "name" {
		t.Errorf("legacy JSON emit = %+v (raw %q), want value `a\\_b%%`", doc, out)
	}
	// Wildcard nodes pass through verbatim.
	out2, err := EmitJSON(Like{Field: "name", Pattern: "a_b%", Wildcards: true})
	if err != nil {
		t.Fatalf("EmitJSON: %v", err)
	}
	if !strings.Contains(out2, `a_b%`) || strings.Contains(out2, `\\_`) {
		t.Errorf("wildcard JSON emit should be verbatim, got %q", out2)
	}
}

func TestEmitLikeCodeTargets(t *testing.T) {
	cases := []struct {
		node Node
		dsl  DSL
		want string
	}{
		// wildcard escapes resolve back into literal text
		{Like{Field: "name", Pattern: `50\%%`, Wildcards: true}, CEL, `name.startsWith('50%')`},
		{Like{Field: "name", Pattern: `%a\_b`, Wildcards: true}, CEL, `name.endsWith('a_b')`},
		{Like{Field: "name", Pattern: `%x\_y%`, Wildcards: true}, Expr, `name contains "x_y"`},
		// untranslatable wildcard shapes emit nothing (like other unsupported nodes)
		{Like{Field: "name", Pattern: "a_b", Wildcards: true}, CEL, ""},
		{Like{Field: "name", Pattern: "a%b%c", Wildcards: true}, CEL, ""},
		{Like{Field: "name", Pattern: "a_b", Negate: true, Wildcards: true}, CEL, ""},
		// legacy nodes keep their historical literal translation
		{Like{Field: "name", Pattern: "a_b%"}, CEL, `name.startsWith('a_b')`},
	}
	for _, c := range cases {
		if got := Emit(c.node, c.dsl); got != c.want {
			t.Errorf("Emit(%#v, %s) = %q, want %q", c.node, c.dsl, got, c.want)
		}
	}
}
