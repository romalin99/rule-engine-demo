// emit_json_test.go — EmitJSON is the inverse of engine.JSONFrontend. These
// tests assert (1) exact JSON output for representative rules, (2) that
// SQL → IR → JSON → IR round-trips to the same emitted SQL, and (3) that
// constructs with no JSON form return an error rather than a lossy document.
//
// Run: go test ./pkg/ir/ -run TestEmitJSON -v
package ir

import (
	"strings"
	"testing"
)

func TestEmitJSONShapes(t *testing.T) {
	cases := []struct{ rule, want string }{
		{"age >= 85", `{"field":"age","op":">=","value":85}`},
		{"city = '深圳'", `{"field":"city","op":"=","value":"深圳"}`},
		{"age BETWEEN 25 AND 40", `{"field":"age","op":"between","values":[25,40]}`},
		{"city IN ('深圳','广州')", `{"field":"city","op":"in","values":["深圳","广州"]}`},
		{"city NOT IN ('北京')", `{"field":"city","op":"not_in","values":["北京"]}`},
		{"name LIKE '数%'", `{"field":"name","op":"like","value":"数%"}`},
		{"name NOT LIKE '%学生%'", `{"field":"name","op":"not_like","value":"%学生%"}`},
		{"phone IS NULL", `{"field":"phone","op":"isnull"}`},
		{"phone IS NOT NULL", `{"field":"phone","op":"isnotnull"}`},
		{"score = 3.5", `{"field":"score","op":"=","value":3.5}`},
	}
	for _, c := range cases {
		n, err := Parse(c.rule)
		if err != nil {
			t.Fatalf("parse %q: %v", c.rule, err)
		}
		got, err := EmitJSON(n)
		if err != nil {
			t.Fatalf("EmitJSON %q: %v", c.rule, err)
		}
		if got != c.want {
			t.Errorf("EmitJSON(%q)\n got=%s\nwant=%s", c.rule, got, c.want)
		}
	}
}

func TestEmitJSONNestedLogic(t *testing.T) {
	n, err := Parse("age >= 18 AND (city = '深圳' OR vip = 1)")
	if err != nil {
		t.Fatal(err)
	}
	got, err := EmitJSON(n)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"and":[{"field":"age","op":">=","value":18},` +
		`{"or":[{"field":"city","op":"=","value":"深圳"},{"field":"vip","op":"=","value":1}]}]}`
	if got != want {
		t.Errorf("nested logic\n got=%s\nwant=%s", got, want)
	}
}

// The SQL → IR → JSON → IR round-trip against the real engine.JSONFrontend is
// in pkg/engine (TestEmitJSONRoundTripViaFrontend): the JSON front-end lives in
// package engine, which imports ir, so the cross-component check cannot live
// here without an import cycle.

func TestEmitJSONUnsupported(t *testing.T) {
	// Constructs the JSON grammar cannot represent must error, not emit "".
	for _, rule := range []string{
		"LOWER(name) = 'x'",             // function call
		"name REGEXP '^a'",              // regexp
		"JSON_EXTRACT(p, '$.city') = 'x'", // json access
		"ARRAY_CONTAINS(tags, 'vip')",   // array predicate
		"EXISTS (orders)",               // set predicate
		"age = ANY(SELECT v FROM xs)",   // quantifier sub-query
	} {
		n, err := Parse(rule)
		if err != nil {
			t.Fatalf("parse %q: %v", rule, err)
		}
		if _, err := EmitJSON(n); err == nil {
			t.Errorf("expected EmitJSON error for %q", rule)
		} else if !strings.Contains(err.Error(), "JSON-rule form") {
			t.Errorf("unexpected error for %q: %v", rule, err)
		}
	}
}
