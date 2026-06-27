package engine_test

import (
	"testing"

	"github.com/example/rule-engine-demo/engine"
	"github.com/example/rule-engine-demo/model"
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
