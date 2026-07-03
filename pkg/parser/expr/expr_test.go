// expr_test.go — round eight: infix startsWith/endsWith/contains and the
// hasPrefix/hasSuffix builtins LIKE-escape their literals, so '%'/'_' inside
// Expr string literals stay literal text after the lowering.
package expr

import (
	"reflect"
	"testing"

	"tcg-rulex-engine/pkg/ir"
)

func TestExprStringPredicateEscaping(t *testing.T) {
	p := New()
	cases := []struct {
		expr string
		want ir.Node
	}{
		{`name startsWith '50%'`, ir.Like{Field: "name", Pattern: `50\%%`, Wildcards: true}},
		{`name endsWith 'a_b'`, ir.Like{Field: "name", Pattern: `%a\_b`, Wildcards: true}},
		{`name contains '_'`, ir.Like{Field: "name", Pattern: `%\_%`, Wildcards: true}},
		{`hasPrefix(name, '50%')`, ir.Like{Field: "name", Pattern: `50\%%`, Wildcards: true}},
		{`hasSuffix(name, 'a_b')`, ir.Like{Field: "name", Pattern: `%a\_b`, Wildcards: true}},
		{`name contains 'plain'`, ir.Like{Field: "name", Pattern: "%plain%", Wildcards: true}},
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
