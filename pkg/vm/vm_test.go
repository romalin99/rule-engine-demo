package vm

import (
	"testing"

	"github.com/example/rule-engine-demo/pkg/ir"
	astrt "github.com/example/rule-engine-demo/pkg/runtime/ast"
)

func TestCompileAndEval(t *testing.T) {
	prog, err := CompileString(
		"age BETWEEN 25 AND 40 AND province IN ('广东','江苏','浙江') AND favorite_category LIKE '数%' AND active_score >= 85")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		row  map[string]any
		want bool
	}{
		{"hit", map[string]any{"age": 30, "province": "广东", "favorite_category": "数码", "active_score": 90.0}, true},
		{"age too old", map[string]any{"age": 50, "province": "广东", "favorite_category": "数码", "active_score": 90.0}, false},
		{"wrong province", map[string]any{"age": 30, "province": "北京", "favorite_category": "数码", "active_score": 90.0}, false},
		{"not 数 prefix", map[string]any{"age": 30, "province": "广东", "favorite_category": "图书", "active_score": 90.0}, false},
		{"low score", map[string]any{"age": 30, "province": "广东", "favorite_category": "数码", "active_score": 70.0}, false},
		{"missing field", map[string]any{"age": 30, "province": "广东", "favorite_category": "数码"}, false},
	}
	for _, c := range cases {
		if got := prog.Eval(c.row); got != c.want {
			t.Errorf("%s: got %v want %v", c.name, got, c.want)
		}
	}
}

func TestOperators(t *testing.T) {
	cases := []struct {
		rule string
		row  map[string]any
		want bool
	}{
		{"gender = '男'", map[string]any{"gender": "男"}, true},
		{"gender = '男'", map[string]any{"gender": "女"}, false},
		{"vip_level >= 3", map[string]any{"vip_level": 3}, true},
		{"vip_level >= 3", map[string]any{"vip_level": 2}, false},
		{"credit_score < 600", map[string]any{"credit_score": 580.0}, true},
		{"total_amount > 50000 AND age > 45", map[string]any{"total_amount": 60000.0, "age": 48}, true},
		{"total_amount > 50000 AND age > 45", map[string]any{"total_amount": 60000.0, "age": 30}, false},
		{"a = 1 OR b = 2", map[string]any{"a": 9, "b": 2}, true},
		{"a = 1 OR b = 2", map[string]any{"a": 9, "b": 9}, false},
		{"name LIKE '%店'", map[string]any{"name": "便利店"}, true},
		{"name LIKE '%码%'", map[string]any{"name": "数码城"}, true},
		{"phone IS NULL", map[string]any{"name": "x"}, true},
		{"phone IS NULL", map[string]any{"phone": "139"}, false},
		{"phone IS NOT NULL", map[string]any{"phone": "139"}, true},
		{"phone IS NOT NULL AND age >= 18", map[string]any{"phone": "139", "age": 20}, true},
		// <> (SQL not-equal alias)
		{"x <> 5", map[string]any{"x": 6}, true},
		{"x <> 5", map[string]any{"x": 5}, false},
		{"name <> '高'", map[string]any{"name": "低"}, true},
		// NOT IN
		{"income_level NOT IN ('<5k','5k-10k')", map[string]any{"income_level": "20k-30k"}, true},
		{"income_level NOT IN ('<5k','5k-10k')", map[string]any{"income_level": "<5k"}, false},
		// NOT LIKE
		{"occupation NOT LIKE '%学生%'", map[string]any{"occupation": "工程师"}, true},
		{"occupation NOT LIKE '%学生%'", map[string]any{"occupation": "在校学生"}, false},
		// NOT (group)
		{"NOT (risk_level = '高')", map[string]any{"risk_level": "低"}, true},
		{"NOT (risk_level = '高')", map[string]any{"risk_level": "高"}, false},
		{"NOT (a = 1 OR b = 2)", map[string]any{"a": 9, "b": 9}, true},
		{"NOT (a = 1 OR b = 2)", map[string]any{"a": 1, "b": 9}, false},
		{"age >= 25 AND NOT (province IN ('北京','上海'))", map[string]any{"age": 30, "province": "广东"}, true},
		{"age >= 25 AND NOT (province IN ('北京','上海'))", map[string]any{"age": 30, "province": "北京"}, false},
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
		why string
		row map[string]any
	}{
		{"NOT IN fails", flip(match, "income_level", "<5k")},
		{"NOT LIKE fails", flip(match, "occupation", "在校学生")},
		{"NOT(=高) fails", flip(match, "risk_level", "高")},
		{"<> fails", flip(match, "marital_status", "未知")},
		{"BETWEEN fails", flip(match, "credit_score", 999)},
		{"OR group fails", merge(match, map[string]any{"vip_level": 1, "order_count": 2})},
		{"IS NOT NULL fails", without(match, "last_login_time")},
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
	for k, v := range base {
		out[k] = v
	}
	for k, v := range over {
		out[k] = v
	}
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
