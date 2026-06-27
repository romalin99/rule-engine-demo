// Demonstrates the CEL and Expr parser front-ends. Each parses a different rule
// language into the SAME IR, which the SAME bytecode VM evaluates — proving the
// "many parsers, one runtime" design.
//
//	go run ./examples/cel_expr
package main

import (
	"fmt"

	"github.com/example/rule-engine-demo/engine"
	"github.com/example/rule-engine-demo/model"
)

func main() {
	user := model.User{UID: 1, Fields: map[string]any{
		"age":               30,
		"province":          "广东",
		"active_score":      96.0,
		"favorite_category": "数码周边",
	}}

	// Same business intent, expressed in three languages:
	//   age in [25,40] AND province in {广东,江苏} AND active_score >= 85
	//   AND favorite_category starts with "数"
	cases := []struct {
		name string
		fe   engine.Frontend
		expr string
	}{
		{"SQL  (qlbridge)", engine.QLBridgeFrontend{},
			"age BETWEEN 25 AND 40 AND province IN ('广东','江苏') AND active_score >= 85 AND favorite_category LIKE '数%'"},
		{"CEL  (cel-go)", engine.CELFrontend{},
			`age >= 25 && age <= 40 && province in ["广东","江苏"] && active_score >= 85 && favorite_category.startsWith("数")`},
		{"Expr (expr-lang)", engine.ExprFrontend{},
			`age >= 25 && age <= 40 && province in ["广东","江苏"] && active_score >= 85 && startsWith(favorite_category, "数")`},
	}

	for i, c := range cases {
		eng := engine.NewWithBackend(engine.NewBytecodeBackend(c.fe))
		loaded, failed := eng.LoadRules([]model.Rule{
			{ID: int64(i + 1), Name: c.name, Enabled: true, Expr: c.expr},
		})
		fmt.Printf("%-18s backend=%-18s loaded=%d failed=%d matched=%v\n",
			c.name, eng.Backend().Name(), loaded, failed, eng.Match(user))
	}
}
