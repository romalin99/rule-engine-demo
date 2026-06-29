// ext_json_test.go — the JSON front-end's extended predicates (regexp,
// JSON_EXTRACT, EXISTS/ANY/ALL, aggregate sub-queries) must compile to IR that
// is semantically identical to the equivalent native-SQL rule. For each case we
// evaluate the JSON rule and the SQL rule on the same rows, with both the
// bytecode VM and the AST runtime, and assert all four results agree.
//
// Run: go test ./pkg/parser/json/ -run TestJSONFrontendExtended -v

package json_test

import (
	"testing"

	"tcg-rulex-engine/pkg/ir"
	jsonp "tcg-rulex-engine/pkg/parser/json"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
	"tcg-rulex-engine/pkg/vm"
)

func TestJSONFrontendExtended(t *testing.T) {
	rows := []map[string]any{
		{
			"name": "Alice123", "floor": 50, "n": 30, "budget": 500,
			"profile": `{"city":"深圳","age":30}`,
			"tags":    []string{"vip", "gold"},
			"scores":  []any{70, 85, 92},
			"orders": []any{
				map[string]any{"amount": 120, "status": "paid"},
				map[string]any{"amount": 80, "status": "paid"},
				map[string]any{"amount": 50, "status": "refunded"},
			},
		},
		{ // sparse: missing/empty collections, different scalars
			"name": "bob", "floor": 100, "n": 5, "budget": 10,
			"profile": `{}`,
			"tags":    []string{},
			"scores":  []any{},
			"orders":  []any{},
		},
		{}, // everything missing
	}

	cases := []struct {
		name string
		json string
		sql  string
	}{
		{"regexp", `{"field":"name","op":"regexp","value":"^A"}`, "name REGEXP '^A'"},
		{"regexp-i", `{"field":"name","op":"regexp","value":"alice","flags":"i"}`, "REGEXP_LIKE(name, 'alice', 'i')"},
		{"not-regexp", `{"not":{"field":"name","op":"regexp","value":"^[0-9]"}}`, "NOT (name REGEXP '^[0-9]')"},
		{"json-str", `{"field":"profile","json":"$.city","op":"=","value":"深圳"}`, "JSON_EXTRACT(profile, '$.city') = '深圳'"},
		{"json-num", `{"field":"profile","json":"$.age","op":">=","value":18}`, "JSON_EXTRACT(profile, '$.age') >= 18"},
		{"exists-nonempty", `{"exists":{"coll":"tags"}}`, "EXISTS(tags)"},
		{"exists-subquery", `{"exists":{"coll":"orders","where":{"field":"amount","op":">","value":100}}}`, "EXISTS(SELECT 1 FROM orders WHERE amount > 100)"},
		{"any-array", `{"field":"floor","op":"<","any":{"array":"scores"}}`, "floor < ANY(scores)"},
		{"all-values", `{"field":"n","op":">=","all":{"values":[10,20,30]}}`, "n >= ALL(10, 20, 30)"},
		{"all-subquery", `{"field":"budget","op":">=","all":{"select":"amount","coll":"orders","where":{"field":"status","op":"=","value":"paid"}}}`, "budget >= ALL(SELECT amount FROM orders WHERE status = 'paid')"},
		{"agg-count", `{"agg":{"fn":"COUNT","col":"*","from":"orders","where":{"field":"status","op":"=","value":"paid"}},"op":">=","value":2}`, "(SELECT COUNT(*) FROM orders WHERE status = 'paid') >= 2"},
		{"agg-sum", `{"agg":{"fn":"SUM","col":"amount","from":"orders"},"op":">=","value":200}`, "(SELECT SUM(amount) FROM orders) >= 200"},
		{"json-regexp", `{"field":"profile","json":"$.city","op":"regexp","value":"深"}`, "JSON_EXTRACT(profile, '$.city') REGEXP '深'"},
	}

	rt := astrt.New()
	for _, c := range cases {
		jnode, err := jsonp.New().Parse(c.json)
		if err != nil {
			t.Fatalf("[%s] json parse: %v", c.name, err)
		}
		snode, err := ir.Parse(c.sql)
		if err != nil {
			t.Fatalf("[%s] sql parse: %v", c.name, err)
		}
		jprog, err := vm.Compile(jnode)
		if err != nil {
			t.Fatalf("[%s] json compile: %v", c.name, err)
		}
		sprog, err := vm.Compile(snode)
		if err != nil {
			t.Fatalf("[%s] sql compile: %v", c.name, err)
		}
		jplan, _ := rt.Compile(jnode)
		splan, _ := rt.Compile(snode)

		for i, row := range rows {
			jvm := jprog.Eval(row)
			svm := sprog.Eval(row)
			jast, _ := rt.Execute(jplan, row)
			sast, _ := rt.Execute(splan, row)
			if jvm != svm || jvm != jast || jvm != sast {
				t.Errorf("[%s] row %d disagree: json(vm=%v ast=%v) sql(vm=%v ast=%v)",
					c.name, i, jvm, jast, svm, sast)
			}
		}
	}
}
