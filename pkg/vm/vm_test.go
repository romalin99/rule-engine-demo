package vm

import "testing"

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
