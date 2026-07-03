// cel_test.go — round eight: startsWith/endsWith/contains literals are
// LIKE-escaped, so '%'/'_' inside CEL string literals stay literal text
// instead of acting as SQL LIKE wildcards after the lowering.
package cel

import (
	"reflect"
	"testing"

	"tcg-rulex-engine/pkg/ir"
)

func TestCELStringPredicateEscaping(t *testing.T) {
	p := New()
	cases := []struct {
		expr string
		want ir.Node
	}{
		{`name.startsWith('50%')`, ir.Like{Field: "name", Pattern: `50\%%`, Wildcards: true}},
		{`name.endsWith('a_b')`, ir.Like{Field: "name", Pattern: `%a\_b`, Wildcards: true}},
		{`name.contains('x_y')`, ir.Like{Field: "name", Pattern: `%x\_y%`, Wildcards: true}},
		{`name.startsWith('plain')`, ir.Like{Field: "name", Pattern: "plain%", Wildcards: true}},
	}
	for _, c := range cases {
		got, err := p.Parse(c.expr)
		if err != nil {
			t.Fatalf("parse %q: %v", c.expr, err)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q -> %#v, want %#v", c.expr, got, c.want)
		}
	}
}
