package engine_test

import (
	"testing"

	"github.com/example/rule-engine-demo/pkg/engine"
	"github.com/example/rule-engine-demo/pkg/model"
)

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

func TestStubFrontends(t *testing.T) {
	for _, fe := range []engine.Frontend{engine.CELFrontend{}, engine.ExprFrontend{}} {
		if _, err := fe.Parse("age >= 18"); err == nil {
			t.Errorf("%s: expected not-implemented error", fe.Name())
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
