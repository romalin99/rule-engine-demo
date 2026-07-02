// security_test.go — hardening regressions (round seven): evaluation panic
// containment and qlbridge parser panic isolation. Rules and row data are
// external input; nothing they contain may take the process down.
//
// Run: go test ./pkg/engine/ -run TestSecurity -v
package engine

import (
	"strings"
	"testing"

	"tcg-rulex-engine/pkg/model"
)

// panicBackend evaluates one designated rule with a panic, everything else
// normally — simulating an engine bug or hostile construct at eval time.
type panicBackend struct{}

func (panicBackend) Name() string { return "panic-stub" }

func (panicBackend) Compile(expr string) (any, error) {
	if expr == "compile-boom" {
		panic("simulated compile panic")
	}
	return expr, nil
}

func (panicBackend) NewContext(fields map[string]any) any { return fields }
func (panicBackend) Eval(_ any, ast any) bool {
	if ast == "boom" {
		panic("simulated evaluation panic")
	}
	return true
}

// TestSecurityMatchPanicContainment: a panicking rule must not crash the
// batch — the user keeps the matches collected before the panic, later users
// still match, and EvalPanics counts the event.
func TestSecurityMatchPanicContainment(t *testing.T) {
	e := NewWithBackend(panicBackend{})
	loaded, failed := e.LoadRules([]model.Rule{
		{ID: 1, Expr: "ok-1", Enabled: true},
		{ID: 2, Expr: "boom", Enabled: true},
		{ID: 3, Expr: "ok-3", Enabled: true},
	})
	if failed != 0 || loaded != 3 {
		t.Fatalf("load: loaded=%d failed=%d", loaded, failed)
	}

	users := []model.User{
		{UID: 1, Fields: map[string]any{"a": 1}},
		{UID: 2, Fields: map[string]any{"a": 2}},
	}
	results := e.MatchBatch(users, 2) // must not crash the process
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if got := e.EvalPanics(); got != 2 { // one recovered panic per user
		t.Errorf("EvalPanics = %d, want 2", got)
	}
	// Every user still holds the rules matched BEFORE the panicking rule; the
	// panicking rule itself and later rules are failed safe for that user.
	for _, r := range results {
		for _, id := range r.RuleIDs {
			if id == 2 {
				t.Errorf("uid %d matched the panicking rule", r.UID)
			}
		}
	}
	// Single-user path shares the same containment.
	_ = e.Match(users[0])
	if got := e.EvalPanics(); got != 3 {
		t.Errorf("EvalPanics after Match = %d, want 3", got)
	}
}

// TestSecurityManagerTestPanicContainment: the draft-test path (/rules/test)
// compiles+evaluates untrusted rule text and rows directly, bypassing the
// batch matchInto containment and — on the CLI Serve/NewApp path — running
// with no HTTP-layer recover. A compile/eval panic must surface as an error,
// not crash the connection goroutine or process.
func TestSecurityManagerTestPanicContainment(t *testing.T) {
	m := NewManager(NewWithBackend(panicBackend{}))

	// A rule whose evaluation panics -> clean error, no crash.
	matched, err := m.Test("boom", map[string]any{"a": 1})
	if err == nil {
		t.Error("expected error from panicking draft eval")
	}
	if matched {
		t.Error("panicking draft must not report matched=true")
	}
	if !strings.Contains(err.Error(), "recovered") {
		t.Errorf("error should note recovery: %v", err)
	}

	// A normal rule still works after a recovered panic (state intact).
	if _, err := m.Test("ok", map[string]any{"a": 1}); err != nil {
		t.Errorf("normal draft test after recovery: %v", err)
	}
}

// TestSecurityCompilePanicContainment: a panic while COMPILING untrusted rule
// text (POST /rules upsert, file-watcher reload) must be contained at the
// single compileOne choke point — the rule fails to load, the rest load
// normally, and the process/goroutine survives. Round nine.
func TestSecurityCompilePanicContainment(t *testing.T) {
	e := NewWithBackend(panicBackend{})

	// A batch where one rule panics at compile: it is counted failed, the
	// others load, and LoadRules does not crash.
	loaded, failed := e.LoadRules([]model.Rule{
		{ID: 1, Expr: "ok-1", Enabled: true},
		{ID: 2, Expr: "compile-boom", Enabled: true},
		{ID: 3, Expr: "ok-3", Enabled: true},
	})
	if loaded != 2 || failed != 1 {
		t.Fatalf("LoadRules with a compile-panicking rule: loaded=%d failed=%d (want 2/1)", loaded, failed)
	}

	// AddRule (the POST /rules path) surfaces the contained panic as an error.
	if err := e.AddRule(model.Rule{ID: 9, Expr: "compile-boom", Enabled: true}); err == nil {
		t.Error("AddRule of a compile-panicking rule should return an error, not crash")
	}
}

// TestSecurityQLBridgePanicIsolation: hostile rule text must never let a
// third-party parser panic escape Parse — it either parses (via fallback) or
// returns an error.
func TestSecurityQLBridgePanicIsolation(t *testing.T) {
	fe := QLBridgeFrontend{}
	hostile := []string{
		"", " ", "((((", "))))", "= = =", "'unterminated",
		"a AND", "NOT", "IN (", "x BETWEEN AND",
		strings.Repeat("(", 500),
		strings.Repeat("NOT ", 300) + "x = 1",
	}
	for _, rule := range hostile {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Parse(%.40q) let a panic escape: %v", rule, r)
				}
			}()
			_, _ = fe.Parse(rule) // error or IR are both fine; a panic is not
		}()
	}
}
