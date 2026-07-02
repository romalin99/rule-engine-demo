// depth_test.go — nesting bound for the standalone JSON rule parser (round
// nine). A deeply nested document must fail at parse with a clean error, not
// overflow the goroutine stack in the recursive toIR lowering.
package json_test

import (
	"strings"
	"testing"

	jsonp "tcg-rulex-engine/pkg/parser/json"
)

func TestJSONParserDepthLimit(t *testing.T) {
	p := jsonp.New()

	// 600 nested {"not": …} wrappers: well past the 200-level bound.
	deepNot := strings.Repeat(`{"not":`, 600) + `{"field":"x","op":"=","value":1}` + strings.Repeat("}", 600)
	if _, err := p.Parse(deepNot); err == nil {
		t.Error("expected depth error for 600 nested not-nodes")
	} else if !strings.Contains(err.Error(), "deeply") {
		t.Errorf("unexpected error: %v", err)
	}

	// 600 nested and-arrays hit the same guard.
	deepAnd := strings.Repeat(`{"and":[`, 600) + `{"field":"x","op":"=","value":1}` + strings.Repeat("]}", 600)
	if _, err := p.Parse(deepAnd); err == nil {
		t.Error("expected depth error for 600 nested and-arrays")
	}

	// Deep nesting reached through EXISTS/aggregate WHERE predicates is bounded
	// too (checkDepth follows the same child links toIR recurses through).
	deepWhere := strings.Repeat(`{"exists":{"coll":"c","where":`, 400) +
		`{"field":"x","op":"=","value":1}` + strings.Repeat("}}", 400)
	if _, err := p.Parse(deepWhere); err == nil {
		t.Error("expected depth error for deeply nested EXISTS where")
	}

	// A modestly nested rule still parses fine, well under the bound.
	ok := strings.Repeat(`{"not":`, 50) + `{"field":"x","op":"=","value":1}` + strings.Repeat("}", 50)
	if _, err := p.Parse(ok); err != nil {
		t.Errorf("50-deep JSON rule should parse: %v", err)
	}
}
