// vm_test.go — 字节码 VM（求值核心）单元测试。
//
// 运行 / Run:  go test ./pkg/vm/ -v
// 用例 / Cases: TestCompileAndEval(编译+多行求值)、TestOperators(27 条单算子)、
//   TestFullCoverageRule(旗舰规则: 1 命中 + 7 个各破坏一子句)、
//   TestASTMatchesBytecode(AST 树遍历与字节码 VM 结果必须一致)、
//   TestExplain(命中无 reasons / 未命中给出失败谓词)。

package vm

import (
	"maps"
	"strings"
	"testing"

	"tcg-rulex-engine/pkg/ir"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

func TestCompileAndEval(t *testing.T) {
	prog, err := CompileString(
		"age BETWEEN 25 AND 40 AND province IN ('广东','江苏','浙江') AND favorite_category LIKE '数%' AND active_score >= 85")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		row  map[string]any
		name string
		want bool
	}{
		{name: "hit", row: map[string]any{"age": 30, "province": "广东", "favorite_category": "数码", "active_score": 90.0}, want: true},
		{name: "age too old", row: map[string]any{"age": 50, "province": "广东", "favorite_category": "数码", "active_score": 90.0}, want: false},
		{name: "wrong province", row: map[string]any{"age": 30, "province": "北京", "favorite_category": "数码", "active_score": 90.0}, want: false},
		{name: "not 数 prefix", row: map[string]any{"age": 30, "province": "广东", "favorite_category": "图书", "active_score": 90.0}, want: false},
		{name: "low score", row: map[string]any{"age": 30, "province": "广东", "favorite_category": "数码", "active_score": 70.0}, want: false},
		{name: "missing field", row: map[string]any{"age": 30, "province": "广东", "favorite_category": "数码"}, want: false},
	}
	for _, c := range cases {
		if got := prog.Eval(c.row); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestOperators(t *testing.T) {
	cases := []struct {
		row  map[string]any
		rule string
		want bool
	}{
		{rule: "gender = '男'", row: map[string]any{"gender": "男"}, want: true},
		{rule: "gender = '男'", row: map[string]any{"gender": "女"}, want: false},
		{rule: "vip_level >= 3", row: map[string]any{"vip_level": 3}, want: true},
		{rule: "vip_level >= 3", row: map[string]any{"vip_level": 2}, want: false},
		{rule: "credit_score < 600", row: map[string]any{"credit_score": 580.0}, want: true},
		{rule: "total_amount > 50000 AND age > 45", row: map[string]any{"total_amount": 60000.0, "age": 48}, want: true},
		{rule: "total_amount > 50000 AND age > 45", row: map[string]any{"total_amount": 60000.0, "age": 30}, want: false},
		{rule: "a = 1 OR b = 2", row: map[string]any{"a": 9, "b": 2}, want: true},
		{rule: "a = 1 OR b = 2", row: map[string]any{"a": 9, "b": 9}, want: false},
		{rule: "name LIKE '%店'", row: map[string]any{"name": "便利店"}, want: true},
		{rule: "name LIKE '%码%'", row: map[string]any{"name": "数码城"}, want: true},
		{rule: "phone IS NULL", row: map[string]any{"name": "x"}, want: true},
		{rule: "phone IS NULL", row: map[string]any{"phone": "139"}, want: false},
		{rule: "phone IS NOT NULL", row: map[string]any{"phone": "139"}, want: true},
		{rule: "phone IS NOT NULL AND age >= 18", row: map[string]any{"phone": "139", "age": 20}, want: true},
		// <> (SQL not-equal alias)
		{rule: "x <> 5", row: map[string]any{"x": 6}, want: true},
		{rule: "x <> 5", row: map[string]any{"x": 5}, want: false},
		{rule: "name <> '高'", row: map[string]any{"name": "低"}, want: true},
		// NOT IN
		{rule: "income_level NOT IN ('<5k','5k-10k')", row: map[string]any{"income_level": "20k-30k"}, want: true},
		{rule: "income_level NOT IN ('<5k','5k-10k')", row: map[string]any{"income_level": "<5k"}, want: false},
		// NOT LIKE
		{rule: "occupation NOT LIKE '%学生%'", row: map[string]any{"occupation": "工程师"}, want: true},
		{rule: "occupation NOT LIKE '%学生%'", row: map[string]any{"occupation": "在校学生"}, want: false},
		// NOT (group)
		{rule: "NOT (risk_level = '高')", row: map[string]any{"risk_level": "低"}, want: true},
		{rule: "NOT (risk_level = '高')", row: map[string]any{"risk_level": "高"}, want: false},
		{rule: "NOT (a = 1 OR b = 2)", row: map[string]any{"a": 9, "b": 9}, want: true},
		{rule: "NOT (a = 1 OR b = 2)", row: map[string]any{"a": 1, "b": 9}, want: false},
		{rule: "age >= 25 AND NOT (province IN ('北京','上海'))", row: map[string]any{"age": 30, "province": "广东"}, want: true},
		{rule: "age >= 25 AND NOT (province IN ('北京','上海'))", row: map[string]any{"age": 30, "province": "北京"}, want: false},
	}
	for _, c := range cases {
		prog, err := CompileString(c.rule)
		if err != nil {
			t.Fatalf("%s: %v", c.rule, err)
		}
		if got := prog.Eval(c.row); got != c.want {
			t.Errorf("%q on %v: got %v want %v", c.rule, c.row, got, c.want)
		}
	}
}

// TestFullCoverageRule compiles the flagship rule (every operator at once) and
// checks a row that matches all clauses plus rows that each break one negation.
func TestFullCoverageRule(t *testing.T) {
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

	prog, err := CompileString(flagship)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	match := map[string]any{
		"age": 32, "province": "江苏", "income_level": "20k-30k",
		"favorite_category": "数码", "occupation": "工程师", "active_score": 90.5,
		"credit_score": 760, "total_amount": 22000.0, "avg_order_amount": 366.67,
		"last_login_time": "2026-06-26 21:15:00", "vip_level": 4, "order_count": 60,
		"risk_level": "低", "register_days": 400, "marital_status": "已婚",
	}
	if !prog.Eval(match) {
		t.Errorf("flagship: match row should hit")
	}

	// Each entry breaks exactly one clause and must flip the result to miss.
	misses := []struct {
		row map[string]any
		why string
	}{
		{why: "NOT IN fails", row: flip(match, "income_level", "<5k")},
		{why: "NOT LIKE fails", row: flip(match, "occupation", "在校学生")},
		{why: "NOT(=高) fails", row: flip(match, "risk_level", "高")},
		{why: "<> fails", row: flip(match, "marital_status", "未知")},
		{why: "BETWEEN fails", row: flip(match, "credit_score", 999)},
		{why: "OR group fails", row: merge(match, map[string]any{"vip_level": 1, "order_count": 2})},
		{why: "IS NOT NULL fails", row: without(match, "last_login_time")},
	}
	for _, m := range misses {
		if prog.Eval(m.row) {
			t.Errorf("flagship: row should miss (%s)", m.why)
		}
	}
}

// flip returns a shallow copy of m with one key overridden.
func flip(m map[string]any, k string, v any) map[string]any {
	return merge(m, map[string]any{k: v})
}

// without returns a shallow copy of m with key k removed.
func without(m map[string]any, k string) map[string]any {
	out := make(map[string]any, len(m))
	for key, val := range m {
		if key != k {
			out[key] = val
		}
	}
	return out
}

// merge returns a shallow copy of base with over's keys applied on top.
func merge(base, over map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(over))
	maps.Copy(out, base)
	maps.Copy(out, over)
	return out
}

// TestASTMatchesBytecode pins the tree-walking runtime to the bytecode VM: for
// every rule/row pair the two evaluators must agree. This covers the negation
// operators (NOT IN / NOT LIKE / <> / NOT (...)). Rows keep all referenced
// fields present, since missing-field comparison semantics are orthogonal here.
func TestASTMatchesBytecode(t *testing.T) {
	rules := []string{
		"income_level NOT IN ('<5k','5k-10k')",
		"occupation NOT LIKE '%学生%'",
		"NOT (risk_level = '高')",
		"marital_status <> '未知'",
		"age BETWEEN 25 AND 40 AND NOT (province IN ('北京','上海'))",
		"age BETWEEN 25 AND 40 " +
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
			"AND marital_status <> '未知'",
	}
	rows := []map[string]any{
		{ // matches the flagship
			"age": 32, "province": "江苏", "income_level": "20k-30k",
			"favorite_category": "数码", "occupation": "工程师", "active_score": 90.5,
			"credit_score": 760, "total_amount": 22000.0, "avg_order_amount": 366.67,
			"last_login_time": "2026-06-26 21:15:00", "vip_level": 4, "order_count": 60,
			"risk_level": "低", "register_days": 400, "marital_status": "已婚",
		},
		{ // breaks every negation
			"age": 21, "province": "北京", "income_level": "<5k",
			"favorite_category": "图书", "occupation": "在校学生", "active_score": 70.0,
			"credit_score": 660, "total_amount": 1000.0, "avg_order_amount": 1500.0,
			"last_login_time": "x", "vip_level": 1, "order_count": 2,
			"risk_level": "高", "register_days": 30, "marital_status": "未知",
		},
	}

	rt := astrt.New()
	for _, r := range rules {
		node, err := ir.Parse(r)
		if err != nil {
			t.Fatalf("parse %q: %v", r, err)
		}
		prog, err := Compile(node)
		if err != nil {
			t.Fatalf("compile %q: %v", r, err)
		}
		plan, err := rt.Compile(node)
		if err != nil {
			t.Fatalf("ast compile %q: %v", r, err)
		}
		for _, row := range rows {
			bc := prog.Eval(row)
			a, err := rt.Execute(plan, row)
			if err != nil {
				t.Fatalf("ast execute %q: %v", r, err)
			}
			if bc != a {
				t.Errorf("disagree on %q\n row=%v\n bytecode=%v ast=%v", r, row, bc, a)
			}
		}
	}
}

// TestExplain checks the explainable evaluator backing POST /evaluate: a passing
// row yields no reasons; a failing row reports the specific failing predicates.
func TestExplain(t *testing.T) {
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

	node, err := ir.Parse(flagship)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	match := map[string]any{
		"age": 32, "province": "江苏", "income_level": "20k-30k",
		"favorite_category": "数码", "occupation": "工程师", "active_score": 90.5,
		"credit_score": 760, "total_amount": 22000.0, "avg_order_amount": 366.67,
		"last_login_time": "2026-06-26 21:15:00", "vip_level": 4, "order_count": 60,
		"risk_level": "低", "register_days": 400, "marital_status": "已婚",
	}
	if passed, reasons := astrt.Explain(node, match); !passed || len(reasons) != 0 {
		t.Errorf("match row: passed=%v reasons=%v; want passed=true, no reasons", passed, reasons)
	}

	miss := map[string]any{ // breaks several clauses incl. the negations
		"age": 21, "province": "江苏", "income_level": "<5k",
		"favorite_category": "图书", "occupation": "在校学生", "active_score": 70.0,
		"credit_score": 660, "total_amount": 1000.0, "avg_order_amount": 1500.0,
		"vip_level": 1, "order_count": 3, "risk_level": "高",
		"register_days": 30, "marital_status": "未知",
	}
	passed, reasons := astrt.Explain(node, miss)
	if passed || len(reasons) == 0 {
		t.Fatalf("miss row: passed=%v reasons=%d; want passed=false with reasons", passed, len(reasons))
	}
	var joined strings.Builder
	for _, r := range reasons {
		joined.WriteString(r.Expr + " — " + r.Detail + "\n")
	}
	for _, want := range []string{"age BETWEEN", "income_level NOT IN", "occupation NOT LIKE", "last_login_time IS NOT NULL", "NOT (risk_level"} {
		if !strings.Contains(joined.String(), want) {
			t.Errorf("reasons missing %q; got:\n%s", want, joined.String())
		}
	}
}
