// frontend_json_test.go — JSON 前端与 CEL/Expr 前端占位断言。
//
// 运行 / Run:  go test ./pkg/engine/ -run 'JSON|Stub' -v
// 用例 / Cases: TestJSONFrontend(and/between/in/like/>=)、TestJSONFrontendNegation
//   (not_in/not_like/{"not":...}/<>)、TestStubFrontends(⚠ 见下方注解，当前会失败)。

package engine_test

import (
	"strings"
	"testing"

	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/model"
)

// TestJSONFrontendDepthLimit locks in the round-eight fix: a deeply nested
// JSON rule must fail at load with a clean error, not overflow the goroutine
// stack (unrecoverable). The native SQL parser has the same bound; both
// entry-point-reachable frontends are now guarded.
func TestJSONFrontendDepthLimit(t *testing.T) {
	fe := engine.JSONFrontend{}

	// 600 nested {"not": …} wrappers around a leaf: well past the 200 bound.
	deep := strings.Repeat(`{"not":`, 600) + `{"field":"x","op":"=","value":1}` + strings.Repeat("}", 600)
	if _, err := fe.Parse(deep); err == nil {
		t.Error("expected depth error for 600 nested JSON nodes")
	} else if !strings.Contains(err.Error(), "deeply") {
		t.Errorf("unexpected error: %v", err)
	}

	// Nested and-arrays hit the same guard.
	and := strings.Repeat(`{"and":[`, 600) + `{"field":"x","op":"=","value":1}` + strings.Repeat("]}", 600)
	if _, err := fe.Parse(and); err == nil {
		t.Error("expected depth error for 600 nested and-arrays")
	}

	// A modestly nested rule still parses fine (well under the bound).
	ok := strings.Repeat(`{"not":`, 50) + `{"field":"x","op":"=","value":1}` + strings.Repeat("}", 50)
	if _, err := fe.Parse(ok); err != nil {
		t.Errorf("50-deep JSON rule should parse: %v", err)
	}
}

func TestJSONFrontend(t *testing.T) {
	jsonRule := `{
      "and": [
        {"field":"age","op":"between","values":[25,40]},
        {"field":"province","op":"in","values":["广东","江苏","浙江"]},
        {"field":"income_level","op":"in","values":["20k-30k","30k+"]},
        {"field":"favorite_category","op":"like","value":"数%"},
        {"field":"active_score","op":">=","value":85}
      ]
    }`

	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.JSONFrontend{}))
	if loaded, failed := eng.LoadRules([]model.Rule{
		{ID: 100, Name: "json-rule", Enabled: true, Expr: jsonRule},
	}); loaded != 1 || failed != 0 {
		t.Fatalf("loaded=%d failed=%d", loaded, failed)
	}

	hit := model.User{UID: 1, Fields: map[string]any{
		"age": 28, "province": "广东", "income_level": "20k-30k",
		"favorite_category": "数码", "active_score": 91.52,
	}}
	miss := model.User{UID: 2, Fields: map[string]any{
		"age": 28, "province": "上海", "income_level": "20k-30k",
		"favorite_category": "数码", "active_score": 91.52,
	}}

	if got := eng.Match(hit); len(got) != 1 || got[0] != 100 {
		t.Errorf("hit: got %v want [100]", got)
	}
	if got := eng.Match(miss); len(got) != 0 {
		t.Errorf("miss: got %v want []", got)
	}
}

// TestJSONFrontendDateBetween verifies a date-range BETWEEN in a JSON rule
// compiles to the same bytecode as its SQL equivalent (string bounds desugar to
// >= AND <=). Regression for the JSON frontend building a numeric-only Between.
func TestJSONFrontendDateBetween(t *testing.T) {
	jsonRule := `{"field":"reg_date","op":"between","values":["2020-01-01","2020-12-31"]}`
	sqlRule := "reg_date BETWEEN '2020-01-01' AND '2020-12-31'"

	jf := engine.NewWithBackend(engine.NewBytecodeBackend(engine.JSONFrontend{}))
	nf := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
	if l, f := jf.LoadRules([]model.Rule{{ID: 300, Name: "json-between", Enabled: true, Expr: jsonRule}}); l != 1 || f != 0 {
		t.Fatalf("json between load: loaded=%d failed=%d (want 1,0)", l, f)
	}
	if l, f := nf.LoadRules([]model.Rule{{ID: 300, Name: "sql-between", Enabled: true, Expr: sqlRule}}); l != 1 || f != 0 {
		t.Fatalf("sql between load: loaded=%d failed=%d", l, f)
	}
	for _, tc := range []struct {
		val  string
		want bool
	}{
		{"2020-06-15", true},
		{"2020-01-01", true},
		{"2020-12-31", true},
		{"2019-12-31", false},
		{"2021-01-01", false},
	} {
		u := model.User{UID: 1, Fields: map[string]any{"reg_date": tc.val}}
		gj := len(jf.Match(u)) == 1
		gn := len(nf.Match(u)) == 1
		if gj != gn {
			t.Errorf("reg_date=%s: json=%v != native=%v", tc.val, gj, gn)
		}
		if gj != tc.want {
			t.Errorf("reg_date=%s: got %v want %v", tc.val, gj, tc.want)
		}
	}
}

// TestStubFrontends verifies the CEL and Expr front-ends now parse a basic
// expression into IR. (They previously returned a "not implemented" error and
// this test asserted that; both now delegate to the real cel-go / expr-lang
// parsers in pkg/parser/cel and pkg/parser/expr, so a successful parse is the
// correct expectation.)
func TestStubFrontends(t *testing.T) {
	for _, fe := range []engine.Frontend{engine.CELFrontend{}, engine.ExprFrontend{}} {
		if node, err := fe.Parse("age >= 18"); err != nil || node == nil {
			t.Errorf("%s: expected successful parse, got node=%v err=%v", fe.Name(), node, err)
		}
	}
}

// TestJSONFrontendNegation covers the JSON DSL's negation ops: not_in, not_like,
// the {"not": ...} wrapper, and the "<>" alias.
func TestJSONFrontendNegation(t *testing.T) {
	jsonRule := `{
      "and": [
        {"field":"income_level","op":"not_in","values":["<5k","5k-10k"]},
        {"field":"occupation","op":"not_like","value":"%学生%"},
        {"not": {"field":"risk_level","op":"=","value":"高"}},
        {"field":"marital_status","op":"<>","value":"未知"}
      ]
    }`

	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.JSONFrontend{}))
	if loaded, failed := eng.LoadRules([]model.Rule{
		{ID: 200, Name: "json-negation", Enabled: true, Expr: jsonRule},
	}); loaded != 1 || failed != 0 {
		t.Fatalf("loaded=%d failed=%d", loaded, failed)
	}

	hit := model.User{UID: 1, Fields: map[string]any{
		"income_level": "20k-30k", "occupation": "工程师",
		"risk_level": "低", "marital_status": "已婚",
	}}
	miss := model.User{UID: 2, Fields: map[string]any{
		"income_level": "<5k", "occupation": "在校学生",
		"risk_level": "高", "marital_status": "未知",
	}}

	if got := eng.Match(hit); len(got) != 1 || got[0] != 200 {
		t.Errorf("hit: got %v want [200]", got)
	}
	if got := eng.Match(miss); len(got) != 0 {
		t.Errorf("miss: got %v want []", got)
	}
}
