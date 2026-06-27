// Command allops demonstrates the engine's full SQL operator coverage on one
// flagship rule: parse → unified IR → (bytecode VM + tree-walking runtime) →
// multi-DSL export. It uses the native SQL front-end, which lowers the whole
// operator set to IR — including NOT IN / NOT LIKE / <> and NOT (...).
//
//	go run ./examples/allops
package main

import (
	"fmt"
	"strings"

	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/ir"
	"tcg-rulex-engine/pkg/model"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
	"tcg-rulex-engine/pkg/vm"
)

// flagship covers: = <> > >= < <= BETWEEN IN NOT IN LIKE NOT LIKE
// IS NOT NULL AND OR NOT and parenthesised precedence.
const flagship = "age BETWEEN 25 AND 40 " +
	"AND province IN ('广东','江苏','浙江') " +
	"AND income_level NOT IN ('<5k','5k-10k') " +
	"AND favorite_category LIKE '数%' " +
	"AND occupation NOT LIKE '%学生%' " +
	"AND active_score >= 85 " +
	"AND credit_score BETWEEN 700 AND 850 " +
	"AND total_amount > 5000 " +
	"AND avg_order_amount <= 1000 " +
	"AND last_login_time IS NOT NULL " +
	"AND (vip_level >= 3 OR order_count >= 30) " +
	"AND NOT (risk_level = '高') " +
	"AND register_days >= 180 " +
	"AND marital_status <> '未知'"

func main() {
	// 1. Parse SQL → unified IR (then run the safe optimizer).
	node, err := ir.Parse(flagship)
	if err != nil {
		panic(err)
	}
	node = ir.Optimize(node)
	fmt.Println("=== IR (operator coverage) ===")
	printIR(node, 0)

	// 2. Two wide-table rows: one matches every clause, one fails the negations.
	match := map[string]any{
		"age": 32, "province": "江苏", "income_level": "20k-30k",
		"favorite_category": "数码", "occupation": "工程师", "active_score": 90.5,
		"credit_score": 760, "total_amount": 22000.0, "avg_order_amount": 366.67,
		"last_login_time": "2026-06-26 21:15:00", "vip_level": 4, "order_count": 60,
		"risk_level": "低", "register_days": 400, "marital_status": "已婚",
	}
	miss := map[string]any{ // student, low income, high risk, missing last_login_time
		"age": 21, "province": "江苏", "income_level": "<5k",
		"favorite_category": "图书", "occupation": "在校学生", "active_score": 70.0,
		"credit_score": 660, "total_amount": 1000.0, "avg_order_amount": 1500.0,
		"vip_level": 1, "order_count": 3, "risk_level": "高",
		"register_days": 30, "marital_status": "未知",
	}

	// 3. Bytecode VM and 4. tree-walking runtime should always agree.
	prog, err := vm.Compile(node)
	if err != nil {
		panic(err)
	}
	rt := astrt.New()
	plan, err := rt.Compile(node)
	if err != nil {
		panic(err)
	}
	astMatch, _ := rt.Execute(plan, match)
	astMiss, _ := rt.Execute(plan, miss)

	fmt.Println("\n=== Eval (bytecode VM  vs  tree-walking runtime) ===")
	fmt.Printf("match user : bytecode=%v  ast=%v\n", prog.Eval(match), astMatch)
	fmt.Printf("miss  user : bytecode=%v  ast=%v\n", prog.Eval(miss), astMiss)

	// 5. Same rule, four target DSLs — one IR, many emitters.
	fmt.Println("\n=== Multi-DSL export (one IR → 4 targets) ===")
	for _, dsl := range ir.AllDSLs {
		fmt.Printf("--- %s ---\n%s\n", dsl, ir.Emit(node, dsl))
	}

	// 6. End-to-end through the engine (native front-end + bytecode VM).
	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
	eng.LoadRules([]model.Rule{{ID: 6, Name: "full-coverage", Enabled: true, Expr: flagship}})
	fmt.Println("\n=== Engine.Match ===")
	fmt.Println("match user -> rule IDs:", eng.Match(model.User{UID: 9, Fields: match}))
	fmt.Println("miss  user -> rule IDs:", eng.Match(model.User{UID: 8, Fields: miss}))
}

// printIR walks the IR and prints one line per node, tagging the SQL operator it
// represents — a quick visual of everything the rule exercises.
func printIR(n ir.Node, depth int) {
	pad := strings.Repeat("  ", depth)
	switch t := n.(type) {
	case ir.Logic:
		fmt.Printf("%s%s\n", pad, t.Op)
		for _, a := range t.Args {
			printIR(a, depth+1)
		}
	case ir.Not:
		fmt.Printf("%sNOT\n", pad)
		printIR(t.Arg, depth+1)
	case ir.Compare:
		fmt.Printf("%sCompare      %s %s %s\n", pad, t.Field, t.Op, valText(t.Val))
	case ir.Between:
		fmt.Printf("%sBETWEEN      %s in [%s, %s]\n", pad, t.Field, valText(t.Lo), valText(t.Hi))
	case ir.In:
		op := "IN"
		if t.Negate {
			op = "NOT IN"
		}
		fmt.Printf("%s%-12s %s (%d values)\n", pad, op, t.Field, len(t.Vals))
	case ir.Like:
		op := "LIKE"
		if t.Negate {
			op = "NOT LIKE"
		}
		fmt.Printf("%s%-12s %s '%s'\n", pad, op, t.Field, t.Pattern)
	case ir.IsNull:
		op := "IS NULL"
		if t.Negate {
			op = "IS NOT NULL"
		}
		fmt.Printf("%s%-12s %s\n", pad, op, t.Field)
	default:
		fmt.Printf("%s<unknown %T>\n", pad, n)
	}
}

func valText(v ir.Value) string {
	if v.IsString {
		return "'" + v.Str + "'"
	}
	return v.Num
}
