package router

// Complete test cases for POST /evaluate/all (engine-wide, "must match ALL"
// gate). Covers the pure evaluator (evaluateAll) and the HTTP endpoint over
// Fiber v3:
//
//	1. passes only when the user matches every loaded rule
//	2. on failure: returns failed_rule_ids + each rule's full SQL + value-aware reasons
//	3. the flagship full-operator rule: passes for the sample row, fails loudly otherwise
//	4. an empty rule set is NOT a pass (a gate needs at least one rule)
//	5. HTTP end-to-end: 200 pass / 200 fail-with-details / 400 missing row
//
// Run: go test ./internal/router/ -run EvaluateAll -v

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/model"
)

// newEngineWithRules builds a native-frontend bytecode engine and loads rules,
// failing the test if any rule does not compile.
func newEngineWithRules(t *testing.T, rules []model.Rule) *engine.Engine {
	t.Helper()
	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
	loaded, failed := eng.LoadRules(rules)
	if failed != 0 || loaded != len(rules) {
		t.Fatalf("LoadRules: loaded=%d failed=%d, want %d/0", loaded, failed, len(rules))
	}
	return eng
}

// coherentRules is a small rule set a single user can satisfy in full, so the
// "must match ALL" gate can actually return passed=true.
func coherentRules() []model.Rule {
	return []model.Rule{
		{ID: 1, Name: "成年区间", Enabled: true, Expr: "age BETWEEN 25 AND 40"},
		{ID: 2, Name: "目标省份", Enabled: true, Expr: "province IN ('广东','江苏','浙江')"},
		{ID: 3, Name: "活跃且低风险", Enabled: true, Expr: "active_score >= 85 AND NOT (risk_level = '高')"},
	}
}

func findFailed(resp EvaluateAllResponse, id int64) (FailedRule, bool) {
	for _, f := range resp.Failed {
		if f.RuleID == id {
			return f, true
		}
	}
	return FailedRule{}, false
}

// ── 1) passes only when the user matches ALL rules ───────────────────────────
func TestEvaluateAll_PassesWhenAllRulesMatch(t *testing.T) {
	eng := newEngineWithRules(t, coherentRules())
	row := map[string]any{"age": 30, "province": "广东", "active_score": 90.0, "risk_level": "低"}

	res := evaluateAll(eng.Cache().Snapshot(), 1, row)

	if !res.Passed {
		t.Fatalf("expected passed=true, got false; failed=%+v", res.Failed)
	}
	if res.TotalRules != 3 || res.PassedCount != 3 || res.FailedCount != 0 {
		t.Fatalf("counts total/passed/failed = %d/%d/%d, want 3/3/0",
			res.TotalRules, res.PassedCount, res.FailedCount)
	}
	if len(res.FailedRuleIDs) != 0 || len(res.Failed) != 0 {
		t.Fatalf("expected no failed rules, got ids=%v failed=%d", res.FailedRuleIDs, len(res.Failed))
	}
}

// ── 2) on failure: failed_rule_ids + full SQL + value-aware reasons ───────────
func TestEvaluateAll_FailsWithRuleIDsAndReasons(t *testing.T) {
	eng := newEngineWithRules(t, coherentRules())
	row := map[string]any{"age": 20, "province": "北京", "active_score": 70.0, "risk_level": "高"}

	res := evaluateAll(eng.Cache().Snapshot(), 7, row)

	if res.Passed {
		t.Fatal("expected passed=false when not every rule matches")
	}
	if res.UID != 7 {
		t.Fatalf("uid echoed wrong: got %d want 7", res.UID)
	}
	if res.FailedCount != 3 || len(res.FailedRuleIDs) != 3 {
		t.Fatalf("expected 3 failed rules, got count=%d ids=%v", res.FailedCount, res.FailedRuleIDs)
	}

	// rule 1: age out of range — full SQL returned + reason mentions the value.
	f1, ok := findFailed(res, 1)
	if !ok {
		t.Fatal("rule 1 not present in failed[]")
	}
	if f1.Rule != "age BETWEEN 25 AND 40" {
		t.Fatalf("rule 1 full SQL wrong: %q", f1.Rule)
	}
	if len(f1.Reasons) == 0 || !strings.Contains(f1.Reasons[0].Detail, "age=20") {
		t.Fatalf("rule 1 reason should mention age=20, got %+v", f1.Reasons)
	}

	// rule 3: risk_level='高' satisfies NOT(...), so the rule fails with a reason.
	f3, ok := findFailed(res, 3)
	if !ok || len(f3.Reasons) == 0 {
		t.Fatalf("rule 3 should fail with reasons, got ok=%v reasons=%+v", ok, f3.Reasons)
	}
}

// ── 3) flagship full-operator rule: passes for the sample, fails loudly ───────
func TestEvaluateAll_FlagshipRule(t *testing.T) {
	eng := newEngineWithRules(t, []model.Rule{
		{ID: 6, Name: "flagship", Enabled: true, Expr: flagshipRule},
	})
	snap := eng.Cache().Snapshot()

	// 3a. the built-in sample row satisfies every clause -> passed.
	pass := evaluateAll(snap, 1, flagshipSampleRow())
	if !pass.Passed {
		t.Fatalf("flagship sample row should pass; failed=%+v", pass.Failed)
	}

	// 3b. a deliberately bad row trips many clauses (age, NOT IN, NOT LIKE,
	//     IS NOT NULL, NOT(...), comparisons, etc.).
	bad := map[string]any{
		"age": 21, "province": "江苏", "income_level": "<5k", "favorite_category": "图书",
		"occupation": "在校学生", "active_score": 70.0, "credit_score": 660,
		"risk_level": "高", "marital_status": "未知", "register_days": 30,
		// last_login_time intentionally omitted -> IS NOT NULL fails
	}
	res := evaluateAll(snap, 1, bad)
	if res.Passed {
		t.Fatal("flagship should fail for the bad row")
	}
	f, ok := findFailed(res, 6)
	if !ok {
		t.Fatal("flagship rule 6 missing from failed[]")
	}
	if f.Rule != flagshipRule {
		t.Fatalf("flagship full SQL not echoed back: %q", f.Rule)
	}
	if len(f.Reasons) < 3 {
		t.Fatalf("expected several failing predicates, got %d: %+v", len(f.Reasons), f.Reasons)
	}
}

// ── 4) an empty rule set is NOT a pass (gate needs >=1 rule) ──────────────────
func TestEvaluateAll_EmptyRuleSetIsNotPassed(t *testing.T) {
	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
	res := evaluateAll(eng.Cache().Snapshot(), 1, map[string]any{"age": 30})
	if res.Passed {
		t.Fatal("empty rule set must not report passed=true")
	}
	if res.TotalRules != 0 || res.FailedCount != 0 {
		t.Fatalf("empty set: total=%d failed=%d, want 0/0", res.TotalRules, res.FailedCount)
	}
}

// ── 5) HTTP end-to-end over Fiber v3: POST /evaluate/all ──────────────────────
func TestEvaluateAllHTTP(t *testing.T) {
	app := NewServer(newEngineWithRules(t, coherentRules())).App()

	post := func(body string) (*http.Response, []byte) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/evaluate/all", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, b
	}

	// 5a. a user who matches all rules -> 200 passed=true.
	resp, body := post(`{"uid":1,"row":{"age":30,"province":"广东","active_score":90,"risk_level":"低"}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var okResp EvaluateAllResponse
	if err := json.Unmarshal(body, &okResp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if !okResp.Passed || okResp.FailedCount != 0 {
		t.Fatalf("expected passed=true, got %+v", okResp)
	}

	// 5b. a user who misses rules -> 200 passed=false with ids + SQL + reasons.
	resp, body = post(`{"uid":2,"row":{"age":20,"province":"北京","active_score":50,"risk_level":"高"}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var badResp EvaluateAllResponse
	if err := json.Unmarshal(body, &badResp); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if badResp.Passed || badResp.FailedCount == 0 || len(badResp.Failed) == 0 {
		t.Fatalf("expected passed=false with failures, got %+v", badResp)
	}
	if len(badResp.FailedRuleIDs) == 0 {
		t.Fatal("expected failed_rule_ids to be populated")
	}
	if badResp.Failed[0].Rule == "" || len(badResp.Failed[0].Reasons) == 0 {
		t.Fatalf("failed[0] should carry full SQL + reasons, got %+v", badResp.Failed[0])
	}

	// 5c. missing "row" -> 400.
	resp, _ = post(`{"uid":3}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing row should be 400, got %d", resp.StatusCode)
	}
}
