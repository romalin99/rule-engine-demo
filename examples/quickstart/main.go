// Quickstart shows how to embed the rule engine as a library.
//
//	go run ./examples/quickstart
package main

import (
	"fmt"

	"github.com/example/rule-engine-demo/engine"
	"github.com/example/rule-engine-demo/ir"
	"github.com/example/rule-engine-demo/model"
)

func main() {
	// 1. Build an engine (default: bytecode VM + qlbridge parser front-end).
	eng := engine.New()

	// 2. Load rules (as if fetched from a DB).
	eng.LoadRules([]model.Rule{
		{ID: 1001, Name: "VIP", Enabled: true, Priority: 10,
			Expr: "age BETWEEN 25 AND 40 AND province IN ('广东','江苏') AND active_score >= 85"},
		{ID: 1002, Name: "高活跃", Enabled: true, Priority: 5,
			Expr: "active_score >= 95"},
	})

	// 3. Score a wide-table user (e.g. arriving from Kafka).
	u := model.User{UID: 1, Fields: map[string]any{
		"age": 30, "province": "广东", "active_score": 96.0,
	}}
	fmt.Println("matched rule IDs:", eng.Match(u)) // -> [1001 1002]

	// 4. Same rule, other DSLs — one IR, many targets.
	for _, dsl := range ir.AllDSLs {
		out, _ := ir.Convert("age BETWEEN 25 AND 40 AND favorite_category LIKE '数%'", dsl)
		fmt.Printf("%-8s %s\n", dsl, out)
	}
}
