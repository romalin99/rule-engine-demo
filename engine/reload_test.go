package engine_test

import (
	"testing"

	"github.com/example/rule-engine-demo/engine"
	"github.com/example/rule-engine-demo/model"
)

func TestHotReload(t *testing.T) {
	eng := engineWith(engine.NativeFrontend{})
	eng.LoadRules([]model.Rule{
		{ID: 1, Name: "a", Enabled: true, Expr: "age >= 18"},
		{ID: 2, Name: "b", Enabled: true, Expr: "age >= 60"},
	})
	if eng.RuleCount() != 2 {
		t.Fatalf("count=%d want 2", eng.RuleCount())
	}

	adult := model.User{UID: 1, Fields: map[string]any{"age": 30}}
	if got := eng.Match(adult); len(got) != 1 || got[0] != 1 {
		t.Fatalf("match=%v want [1]", got)
	}

	// incremental add
	if err := eng.AddRule(model.Rule{ID: 3, Name: "c", Enabled: true, Expr: "age >= 25"}); err != nil {
		t.Fatal(err)
	}
	if eng.RuleCount() != 3 {
		t.Fatalf("count=%d want 3 after add", eng.RuleCount())
	}
	if got := eng.Match(adult); len(got) != 2 { // rules 1 and 3
		t.Fatalf("match=%v want 2 hits after add", got)
	}

	// incremental remove
	eng.RemoveRule(1)
	if eng.RuleCount() != 2 {
		t.Fatalf("count=%d want 2 after remove", eng.RuleCount())
	}

	// full replace
	loaded, failed := eng.ReplaceRules([]model.Rule{
		{ID: 99, Name: "only", Enabled: true, Expr: "age >= 0"},
	})
	if loaded != 1 || failed != 0 {
		t.Fatalf("replace loaded=%d failed=%d", loaded, failed)
	}
	if eng.RuleCount() != 1 {
		t.Fatalf("count=%d want 1 after replace", eng.RuleCount())
	}
	if got := eng.Match(adult); len(got) != 1 || got[0] != 99 {
		t.Fatalf("match=%v want [99] after replace", got)
	}

	// bad rule fails to add, doesn't change set
	if err := eng.AddRule(model.Rule{ID: 100, Enabled: true, Expr: "age >>>"}); err == nil {
		t.Fatal("expected compile error for bad rule")
	}
	if eng.RuleCount() != 1 {
		t.Fatalf("count=%d want 1 after failed add", eng.RuleCount())
	}
}
