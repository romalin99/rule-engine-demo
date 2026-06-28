//go:build test

// client_test.go — WPS HTTP 客户端单元测试(httptest mock)。带 build tag `test`。
//
// 运行 / Run:  go test -tags test ./internal/client/wps/ -v
// 覆盖：GetResetPasswordStatus(邮箱/短信开关组合、非200、404、非法JSON、重试、ctx) + HTTPError。
//
// 注：原 import 误写为外部模块 tcg-rulex-engine/...，已修正为本模块路径(与 mcs/uss 一致)。

package wps

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tcg-rulex-engine/internal/client/clienthttp"
)

// newTestClient creates a Client directly, bypassing the globalOnce singleton,
// with no retry delay so tests run fast.
func newTestClient(baseURL string) *Client {
	return &Client{
		httpClient:       &http.Client{},
		baseURL:          baseURL,
		basePath:         "wps-core/",
		maxRetries:       3,
		retryDelay:       0,
		singleReqTimeout: clienthttp.DefaultSingleReqTimeout,
	}
}

// ── fixtures ─────────────────────────────────────────────────────────────────

const (
	// 邮箱和短信均开启
	bothEnabledJSON = `{
		"success": true,
		"value": {
			"isEmailResetEnabled": true,
			"isSmsResetEnabled":   true
		}
	}`

	// 仅邮箱开启
	emailOnlyJSON = `{
		"success": true,
		"value": {
			"isEmailResetEnabled": true,
			"isSmsResetEnabled":   false
		}
	}`

	// 仅短信开启
	smsOnlyJSON = `{
		"success": true,
		"value": {
			"isEmailResetEnabled": false,
			"isSmsResetEnabled":   true
		}
	}`

	// 全部关闭
	bothDisabledJSON = `{
		"success": true,
		"value": {
			"isEmailResetEnabled": false,
			"isSmsResetEnabled":   false
		}
	}`

	// success=false — 业务层返回失败
	successFalseJSON = `{
		"success": false,
		"value": {
			"isEmailResetEnabled": false,
			"isSmsResetEnabled":   false
		}
	}`
)

// ── helpers ───────────────────────────────────────────────────────────────────

type requestRecord struct {
	path     string
	merchant string
}

// newHandler 返回一个 httptest.Server，记录首次请求后固定返回 status+body。
func newHandler(status int, body string, rec *requestRecord) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rec != nil && rec.path == "" {
			rec.path = r.URL.Path
			rec.merchant = r.Header.Get("Merchant")
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
}

// newRetryHandler 返回前 failCount 次 503，之后返回 200+successBody。
func newRetryHandler(failCount int, successBody string) (*httptest.Server, *int) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= failCount {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, successBody)
	}))
	return srv, &calls
}

// ── TestGetResetPasswordStatus_Success* ──────────────────────────────────────

func TestGetResetPasswordStatus_BothEnabled(t *testing.T) {
	var rec requestRecord
	srv := newHandler(http.StatusOK, bothEnabledJSON, &rec)
	defer srv.Close()

	client := newTestClient(srv.URL)
	result, err := client.GetResetPasswordStatus(context.Background(), "dfstar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("Success: got false, want true")
	}
	if !result.Value.IsEmailResetEnabled {
		t.Error("IsEmailResetEnabled: got false, want true")
	}
	if !result.Value.IsSmsResetEnabled {
		t.Error("IsSmsResetEnabled: got false, want true")
	}
}

func TestGetResetPasswordStatus_EmailOnly(t *testing.T) {
	srv := newHandler(http.StatusOK, emailOnlyJSON, nil)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		GetResetPasswordStatus(context.Background(), "gi8viet2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Value.IsEmailResetEnabled {
		t.Error("IsEmailResetEnabled: want true")
	}
	if result.Value.IsSmsResetEnabled {
		t.Error("IsSmsResetEnabled: want false")
	}
}

func TestGetResetPasswordStatus_SmsOnly(t *testing.T) {
	srv := newHandler(http.StatusOK, smsOnlyJSON, nil)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		GetResetPasswordStatus(context.Background(), "gi8viet2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value.IsEmailResetEnabled {
		t.Error("IsEmailResetEnabled: want false")
	}
	if !result.Value.IsSmsResetEnabled {
		t.Error("IsSmsResetEnabled: want true")
	}
}

func TestGetResetPasswordStatus_BothDisabled(t *testing.T) {
	srv := newHandler(http.StatusOK, bothDisabledJSON, nil)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		GetResetPasswordStatus(context.Background(), "demo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value.IsEmailResetEnabled || result.Value.IsSmsResetEnabled {
		t.Error("both flags should be false")
	}
}

// ── TestGetResetPasswordStatus_Request ───────────────────────────────────────

// 验证请求路径和 Merchant header 正确传递
func TestGetResetPasswordStatus_RequestShape(t *testing.T) {
	var rec requestRecord
	srv := newHandler(http.StatusOK, bothEnabledJSON, &rec)
	defer srv.Close()

	_, err := newTestClient(srv.URL).
		GetResetPasswordStatus(context.Background(), "dfstar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 路径必须包含 basePath + endpoint
	wantPath := "/wps-core/members/reset-password-status"
	if rec.path != wantPath {
		t.Errorf("path: got %q, want %q", rec.path, wantPath)
	}
	// Merchant header 必须与传入的 merchantCode 一致
	if rec.merchant != "dfstar" {
		t.Errorf("Merchant header: got %q, want %q", rec.merchant, "dfstar")
	}
}

// ── TestGetResetPasswordStatus_SuccessFalse ───────────────────────────────────

// success=false 时不返回 error（反序列化正常），由业务层检查 Success 字段
func TestGetResetPasswordStatus_SuccessFalse(t *testing.T) {
	srv := newHandler(http.StatusOK, successFalseJSON, nil)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		GetResetPasswordStatus(context.Background(), "dfstar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Success: got true, want false")
	}
}

// ── TestGetResetPasswordStatus_HTTPErrors ────────────────────────────────────

func TestGetResetPasswordStatus_Non200(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway) // 502
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.retryDelay = 0

	_, err := client.GetResetPasswordStatus(context.Background(), "dfstar")
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	// 应触发 3 次重试
	if calls != 3 {
		t.Errorf("retry count: got %d, want 3", calls)
	}
	// 错误必须可被 IsHTTPError 识别（readBody 返回 *HTTPError）
	if !IsHTTPError(err) && !strings.Contains(err.Error(), "502") {
		t.Errorf("error chain should contain HTTPError or 502, got: %v", err)
	}
}

func TestGetResetPasswordStatus_404NotFound(t *testing.T) {
	srv := newHandler(http.StatusNotFound, `{"message":"not found"}`, nil)
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.retryDelay = 0

	_, err := client.GetResetPasswordStatus(context.Background(), "unknown")
	if err == nil {
		t.Fatal("expected error for 404 response")
	}
}

func TestGetResetPasswordStatus_InvalidJSON(t *testing.T) {
	srv := newHandler(http.StatusOK, `{bad json}`, nil)
	defer srv.Close()

	_, err := newTestClient(srv.URL).
		GetResetPasswordStatus(context.Background(), "dfstar")
	if err == nil {
		t.Fatal("expected deserialization error")
	}
	if !strings.Contains(err.Error(), "deserialization failed") {
		t.Errorf("error should mention deserialization, got: %v", err)
	}
}

// ── TestGetResetPasswordStatus_Retry ─────────────────────────────────────────

// 前 2 次失败，第 3 次成功 → 最终成功
func TestGetResetPasswordStatus_SuccessAfterRetry(t *testing.T) {
	srv, calls := newRetryHandler(2, bothEnabledJSON)
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.retryDelay = 0

	result, err := client.GetResetPasswordStatus(context.Background(), "dfstar")
	if err != nil {
		t.Fatalf("should succeed after 2 failures: %v", err)
	}
	if !result.Success {
		t.Error("Success: want true")
	}
	if *calls != 3 {
		t.Errorf("total calls: got %d, want 3", *calls)
	}
}

// 3 次全部失败 → all N attempts failed
func TestGetResetPasswordStatus_AllRetriesExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.retryDelay = 0

	_, err := client.GetResetPasswordStatus(context.Background(), "dfstar")
	if err == nil {
		t.Fatal("expected error when all retries exhausted")
	}
	if !strings.Contains(err.Error(), "all 3 attempts failed") {
		t.Errorf("error should mention retries exhausted, got: %v", err)
	}
}

// ── TestGetResetPasswordStatus_ContextCancelled ──────────────────────────────

func TestGetResetPasswordStatus_ContextCancelled(t *testing.T) {
	// 服务端故意延迟，context 先超时
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, bothEnabledJSON)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.singleReqTimeout = 50 * time.Millisecond
	client.retryDelay = 0

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	_, err := client.GetResetPasswordStatus(ctx, "dfstar")
	if err == nil {
		t.Fatal("expected timeout/cancel error")
	}
}

// ── TestNewClient / Config lifecycle ─────────────────────────────────────────

// TestNewClient_CreatesSingleton verifies that NewClient initialises the
// package-level singleton on the first call and returns the same pointer on
// subsequent calls.
func TestNewClient_CreatesSingleton(t *testing.T) {
	c1 := NewClient("http://wps.test", "wps-core/")
	if c1 == nil {
		t.Fatal("NewClient returned nil")
	}
	c2 := NewClient("http://other.test", "other/")
	if c1 != c2 {
		t.Error("NewClient should return the same singleton on repeated calls")
	}
}

// TestConfig_InitClose verifies that Config.Init delegates to NewClient and
// Config.Close delegates to Client.Close without panicking.
func TestConfig_InitClose(t *testing.T) {
	cfg := &Config{Host: "http://wps.test", BasPath: "wps-core/"}
	cl := cfg.Init()
	if cl == nil {
		t.Fatal("Config.Init returned nil")
	}
	cfg.Close(cl)
}

// TestClient_Close_Idempotent verifies that Close can be called multiple times
// without panicking (sync.Once behaviour).
func TestClient_Close_Idempotent(t *testing.T) {
	c := newTestClient("http://localhost")
	c.Close()
	c.Close()
}

// ── TestIsHTTPError ───────────────────────────────────────────────────────────

func TestIsHTTPError(t *testing.T) {
	cases := []struct {
		err  error
		name string
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "HTTPError value", err: HTTPError{body: "err", code: 502}, want: true},
		{name: "HTTPError pointer", err: &HTTPError{body: "err", code: 404}, want: true},
		{name: "plain error", err: fmt.Errorf("some error"), want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsHTTPError(tc.err); got != tc.want {
				t.Errorf("IsHTTPError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestHTTPError_StatusCode(t *testing.T) {
	e := HTTPError{body: "bad gateway", code: 502}
	if e.StatusCode() != 502 {
		t.Errorf("StatusCode(): got %d, want 502", e.StatusCode())
	}
	if e.Error() != "bad gateway" {
		t.Errorf("Error(): got %q, want %q", e.Error(), "bad gateway")
	}
}
