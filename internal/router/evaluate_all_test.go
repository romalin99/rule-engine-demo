package router

// HTTP tests for the layered rule-engine API assembled by router.NewApp
// (handler → service → infra → engine). EVERY route — including probes
// (/healthz, /ping, /metrics) — is served under BasePath (/tcg-rulex-engine).
//
// Run: go test ./internal/router/ -v

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/model"
)

// newTestEngine builds a native-frontend bytecode engine and loads rules.
func newTestEngine(t *testing.T, rules []model.Rule) *engine.Engine {
	t.Helper()
	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
	loaded, failed := eng.LoadRules(rules)
	if failed != 0 || loaded != len(rules) {
		t.Fatalf("LoadRules: loaded=%d failed=%d, want %d/0", loaded, failed, len(rules))
	}
	return eng
}

// coherentRules is a small rule set one user can satisfy in full, so the
// "must match ALL" gate can return passed=true.
func coherentRules() []model.Rule {
	return []model.Rule{
		{ID: 1, Name: "成年区间", Enabled: true, Expr: "age BETWEEN 25 AND 40"},
		{ID: 2, Name: "目标省份", Enabled: true, Expr: "province IN ('广东','江苏','浙江')"},
		{ID: 3, Name: "活跃且低风险", Enabled: true, Expr: "active_score >= 85 AND NOT (risk_level = '高')"},
	}
}

// evalAllResp is a partial view of service.EvaluateAllResult for assertions.
type evalAllResp struct {
	Failed []struct {
		Rule    string `json:"rule"`
		Reasons []struct {
			Expr   string `json:"expr"`
			Detail string `json:"detail"`
		} `json:"reasons"`
		RuleID int64 `json:"rule_id"`
	} `json:"failed"`
	TotalRules  int  `json:"total_rules"`
	PassedCount int  `json:"passed_count"`
	FailedCount int  `json:"failed_count"`
	Passed      bool `json:"passed"`
}

// ── /evaluate/all gate under the BasePath group (pass / fail / 400) ───────────
func TestEvaluateAllHTTP(t *testing.T) {
	app := NewApp(newTestEngine(t, coherentRules()))

	post := func(body string) (*http.Response, []byte) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, BasePath+"/evaluate/all", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		b, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return resp, b
	}

	// pass: matches all rules
	resp, body := post(`{"uid":1,"row":{"age":30,"province":"广东","active_score":90,"risk_level":"低"}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	var ok evalAllResp
	if err := json.Unmarshal(body, &ok); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if !ok.Passed || ok.FailedCount != 0 {
		t.Fatalf("expected passed, got %+v", ok)
	}

	// fail: returns failed rule IDs + full SQL + reasons
	resp, body = post(`{"uid":2,"row":{"age":20,"province":"北京","active_score":50,"risk_level":"高"}}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var bad evalAllResp
	if err := json.Unmarshal(body, &bad); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, body)
	}
	if bad.Passed || bad.FailedCount == 0 || len(bad.Failed) == 0 {
		t.Fatalf("expected fail with reasons, got %+v", bad)
	}
	if bad.Failed[0].Rule == "" || len(bad.Failed[0].Reasons) == 0 {
		t.Fatalf("failed[0] should carry full SQL + reasons, got %+v", bad.Failed[0])
	}

	// missing row -> 400
	resp, _ = post(`{"uid":3}`)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing row should be 400, got %d", resp.StatusCode)
	}
}

// ── the full migrated ops surface is wired (all under BasePath group) ─────────
func TestLayeredOpsRoutesWired(t *testing.T) {
	app := NewApp(newTestEngine(t, coherentRules()))

	getCode := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test %s: %v", path, err)
		}
		_ = resp.Body.Close()
		return resp.StatusCode
	}

	// every GET route — probes included — now lives under BasePath (group at root)
	for _, p := range []string{
		BasePath + "/healthz", BasePath + "/ping", BasePath + "/metrics",
		BasePath + "/", BasePath + "/rules", BasePath + "/rules/list", BasePath + "/versions",
	} {
		if code := getCode(p); code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", p, code)
		}
	}

	// POST {BasePath}/match (flat user)
	req := httptest.NewRequest(http.MethodPost, BasePath+"/match",
		bytes.NewReader([]byte(`{"uid":1,"age":30,"province":"广东","active_score":90,"risk_level":"低"}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST %s/match = %d, want 200", BasePath, resp.StatusCode)
	}
	_ = resp.Body.Close()

	// POST {BasePath}/rules/selftest (proves live add+remove through the service layer)
	req = httptest.NewRequest(http.MethodPost, BasePath+"/rules/selftest", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	resp, err = app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("POST %s/rules/selftest = %d, want 200", BasePath, resp.StatusCode)
	}
	_ = resp.Body.Close()
}
