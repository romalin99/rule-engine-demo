// table_test.go — 决策表（Decision Table）单元测试。
//
// 运行 / Run:  go test ./pkg/dtable/ -v
// 用例 / Cases: TestDecisionTableCompilesAndMatches —— 2 行决策表(between/in/like/isnull)
//   → 编译成 2 条 SQL 规则(ID 从 IDBase 起)→ 引擎加载 → 用户命中两条，验证
//   「行→IR→SQL→规则→匹配」全链路。

package dtable_test

import (
	"testing"

	"tcg-rulex-engine/pkg/dtable"
	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/model"
)

func TestDecisionTableCompilesAndMatches(t *testing.T) {
	tbl := dtable.Table{
		Name:   "seg",
		IDBase: 7000,
		Rows: []dtable.Row{
			{Name: "广东数码", Priority: 10, When: []dtable.Cond{
				{Field: "age", Op: "between", Values: []any{25.0, 40.0}},
				{Field: "province", Op: "in", Values: []any{"广东", "江苏"}},
				{Field: "favorite_category", Op: "like", Value: "数%"},
			}},
			{Name: "缺手机", Priority: 1, When: []dtable.Cond{
				{Field: "phone", Op: "isnull"},
			}},
		},
	}

	rules, err := tbl.Rules()
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 2 {
		t.Fatalf("rules=%d want 2", len(rules))
	}
	// row → SQL text
	if rules[0].Expr == "" || rules[0].ID != 7000 {
		t.Fatalf("bad rule0: %+v", rules[0])
	}

	// Compile via the engine and verify matching behaviour end-to-end.
	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
	if loaded, failed := eng.LoadRules(rules); loaded != 2 || failed != 0 {
		t.Fatalf("loaded=%d failed=%d (exprs: %q | %q)", loaded, failed, rules[0].Expr, rules[1].Expr)
	}

	u := model.User{UID: 1, Fields: map[string]any{
		"age": 30, "province": "广东", "favorite_category": "数码",
	}}
	got := eng.Match(u)
	// matches row0 (广东数码) and row1 (phone is null) => ids 7000, 7001
	if len(got) != 2 {
		t.Errorf("got %v want 2 matches (rule SQL: %q ; %q)", got, rules[0].Expr, rules[1].Expr)
	}
}
