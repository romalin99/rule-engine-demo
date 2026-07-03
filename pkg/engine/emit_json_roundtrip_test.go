// emit_json_roundtrip_test.go — the cross-component inverse check: a rule taken
// SQL → IR → JSON (ir.EmitJSON) → IR (engine.JSONFrontend) must re-parse to IR
// that emits the same SQL. This proves EmitJSON and the JSON front-end are true
// inverses on the shared predicate subset. It lives in package engine_test
// because it needs both ir and the engine's JSONFrontend.
//
// Run: go test ./pkg/engine/ -run TestEmitJSONRoundTripViaFrontend -v
package engine_test

import (
	"testing"

	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/ir"
)

func TestEmitJSONRoundTripViaFrontend(t *testing.T) {
	rules := []string{
		"age >= 85",
		"city = '深圳'",
		"age BETWEEN 25 AND 40",
		"city IN ('深圳','广州','杭州')",
		"income NOT IN ('<5k')",
		"name LIKE '数%'",
		"note NOT LIKE '%test%'",
		"phone IS NOT NULL",
		"age >= 18 AND (city = '深圳' OR vip = 1) AND note IS NULL",
		"score = 3.5 OR score = 4",
	}
	fe := engine.JSONFrontend{}
	for _, rule := range rules {
		n1, err := ir.Parse(rule)
		if err != nil {
			t.Fatalf("parse %q: %v", rule, err)
		}
		wantSQL := ir.Emit(n1, ir.SQL)

		doc, err := ir.EmitJSON(n1) // IR → JSON document
		if err != nil {
			t.Fatalf("EmitJSON %q: %v", rule, err)
		}
		n2, err := fe.Parse(doc) // JSON document → IR (the real front-end)
		if err != nil {
			t.Fatalf("JSONFrontend.Parse(%s): %v", doc, err)
		}
		if gotSQL := ir.Emit(n2, ir.SQL); gotSQL != wantSQL {
			t.Errorf("round-trip mismatch for %q\n  via JSON: %s\n  got  SQL: %s\n  want SQL: %s", rule, doc, gotSQL, wantSQL)
		}
	}
}
