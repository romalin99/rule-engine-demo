//go:build test

package mcs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tcg-rulex-engine/internal/client/clienthttp"
)

// ── test client factory ───────────────────────────────────────────────────────

func newTestClient(baseURL string) *Client {
	return &Client{
		httpClient:       &http.Client{},
		baseURL:          baseURL,
		basePath:         "mcs-core/",
		maxRetries:       3,
		retryDelay:       0,
		singleReqTimeout: clienthttp.DefaultSingleReqTimeout,
	}
}

// ── fixtures ──────────────────────────────────────────────────────────────────

// standardReq is a realistic VerifyPlayerInfo request body.
var standardReq = VerifyFinanceHistoryReq{
	VerifyPlayerFinanceInfo: VerifyPlayerFinanceInfo{
		BcNumber:        "1234567890",
		BcHolderName:    "John Doe",
		EwAccount:       "",
		EwHolderName:    "",
		VwAddress:       "",
		VwHolderName:    "",
		IsCaseSensitive: false,
	},
	VerifyPlayerHistoryInfo: VerifyPlayerHistoryInfo{
		LastDepositAmount:          "1000",
		LastDepositAmountRange:     "",
		LastDepositMethod:          "bank_transfer",
		LastDepositTime:            "2026-03-10",
		LastWithdrawAmount:         "500",
		LastWithdrawAmountRange:    "",
		LastWithdrawMethod:         "bank_transfer",
		LastWithdrawTime:           "2026-03-09",
		LastDepositTimeRangeInDay:  1,
		LastWithdrawTimeRangeInDay: 1,
	},
}

var standardHeaders = PlayerHeaders{
	CustomerID:   "100001",
	CustomerName: "devpm01",
	Merchant:     "a71bdtf1",
	CustomerIP:   "192.168.1.100",
}

// 完全匹配响应（所有字段得分 > 0）
const fullMatchRespJSON = `{
	"success": true,
	"value": {
		"verifyPlayerFinanceInfo": {
			"bcNumber":     10,
			"bcHolderName": 10,
			"bcBankCode":   0,
			"bcSubBranch":  0,
			"bcCity":       0,
			"bcProvince":   0,
			"vwAddress":    0,
			"vwHolderName": 0,
			"vwBankCode":   0,
			"ewAccount":    0,
			"ewHolderName": 0,
			"ewBankCode":   0
		},
		"verifyPlayerTransactionHistory": {
			"lastDepositAmount":  10,
			"lastDepositMethod":  10,
			"lastDepositTime":    10,
			"lastWithdrawAmount": 10,
			"lastWithdrawMethod": 10,
			"lastWithdrawTime":   10
		}
	}
}`

// 部分匹配响应
const partialMatchRespJSON = `{
	"success": true,
	"value": {
		"verifyPlayerFinanceInfo": {
			"bcNumber":     10,
			"bcHolderName": 0,
			"bcBankCode":   0,
			"bcSubBranch":  0,
			"bcCity":       0,
			"bcProvince":   0,
			"vwAddress":    0,
			"vwHolderName": 0,
			"vwBankCode":   0,
			"ewAccount":    0,
			"ewHolderName": 0,
			"ewBankCode":   0
		},
		"verifyPlayerTransactionHistory": {
			"lastDepositAmount":  0,
			"lastDepositMethod":  10,
			"lastDepositTime":    0,
			"lastWithdrawAmount": 0,
			"lastWithdrawMethod": 0,
			"lastWithdrawTime":   0
		}
	}
}`

// success=false — 业务失败
const successFalseRespJSON = `{
	"success": false,
	"value": {
		"verifyPlayerFinanceInfo": {},
		"verifyPlayerTransactionHistory": {}
	}
}`

// ── helpers ───────────────────────────────────────────────────────────────────

// capturedRequest holds the server-side view of one inbound request.
type capturedRequest struct {
	method  string
	path    string
	headers http.Header
	body    []byte
}

// newCapturingHandler returns a server that captures every request and responds
// with status+body.
func newCapturingHandler(status int, body string, out *capturedRequest) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if out != nil {
			out.method = r.Method
			out.path = r.URL.Path
			out.headers = r.Header.Clone()
			out.body, _ = io.ReadAll(r.Body)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
}

// newRetryHandler returns a server that fails the first failCount requests, then
// succeeds with successBody. It also returns a pointer to the call counter.
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

// ── TestVerifyPlayerInfo_Success* ─────────────────────────────────────────────

func TestVerifyPlayerInfo_FullMatch(t *testing.T) {
	srv := newCapturingHandler(http.StatusOK, fullMatchRespJSON, nil)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		VerifyPlayerInfo(context.Background(), standardHeaders, standardReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Error("Success: got false, want true")
	}

	fi := result.Value.VerifyPlayerFinanceInfo
	if fi.BcNumber != 10 {
		t.Errorf("BcNumber: got %d, want 10", fi.BcNumber)
	}
	if fi.BcHolderName != 10 {
		t.Errorf("BcHolderName: got %d, want 10", fi.BcHolderName)
	}

	hi := result.Value.VerifyPlayerHistoryInfo
	if hi.LastDepositAmount != 10 {
		t.Errorf("LastDepositAmount: got %d, want 10", hi.LastDepositAmount)
	}
	if hi.LastDepositMethod != 10 {
		t.Errorf("LastDepositMethod: got %d, want 10", hi.LastDepositMethod)
	}
	if hi.LastWithdrawAmount != 10 {
		t.Errorf("LastWithdrawAmount: got %d, want 10", hi.LastWithdrawAmount)
	}
}

func TestVerifyPlayerInfo_PartialMatch(t *testing.T) {
	srv := newCapturingHandler(http.StatusOK, partialMatchRespJSON, nil)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		VerifyPlayerInfo(context.Background(), standardHeaders, standardReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	fi := result.Value.VerifyPlayerFinanceInfo
	if fi.BcNumber != 10 {
		t.Errorf("BcNumber: got %d, want 10", fi.BcNumber)
	}
	if fi.BcHolderName != 0 {
		t.Errorf("BcHolderName: got %d, want 0 (no match)", fi.BcHolderName)
	}

	hi := result.Value.VerifyPlayerHistoryInfo
	if hi.LastDepositMethod != 10 {
		t.Errorf("LastDepositMethod: got %d, want 10", hi.LastDepositMethod)
	}
	if hi.LastDepositAmount != 0 {
		t.Errorf("LastDepositAmount: got %d, want 0 (no match)", hi.LastDepositAmount)
	}
}

func TestVerifyPlayerInfo_SuccessFalse(t *testing.T) {
	srv := newCapturingHandler(http.StatusOK, successFalseRespJSON, nil)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		VerifyPlayerInfo(context.Background(), standardHeaders, standardReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Success: got true, want false")
	}
}

// ── TestVerifyPlayerInfo_RequestShape ─────────────────────────────────────────

// 验证请求路径、method、headers、body 均正确
func TestVerifyPlayerInfo_RequestShape(t *testing.T) {
	var rec capturedRequest
	srv := newCapturingHandler(http.StatusOK, fullMatchRespJSON, &rec)
	defer srv.Close()

	_, err := newTestClient(srv.URL).
		VerifyPlayerInfo(context.Background(), standardHeaders, standardReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ── Method ───────────────────────────────────────────────────────────────
	if rec.method != http.MethodPost {
		t.Errorf("method: got %q, want POST", rec.method)
	}

	// ── Path ─────────────────────────────────────────────────────────────────
	wantPath := "/mcs-core/player/verifyPlayerInfo"
	if rec.path != wantPath {
		t.Errorf("path: got %q, want %q", rec.path, wantPath)
	}

	// ── Headers ───────────────────────────────────────────────────────────────
	checks := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
		"Customerid":   "100001", // Go's http.Header canonicalises key casing
		"Customername": "devpm01",
		"Merchant":     "a71bdtf1",
		"Customerip":   "192.168.1.100",
	}
	for hdr, want := range checks {
		if got := rec.headers.Get(hdr); got != want {
			t.Errorf("header %q: got %q, want %q", hdr, got, want)
		}
	}

	// ── Body: JSON keys match model tags ──────────────────────────────────────
	var body map[string]any
	if err := json.Unmarshal(rec.body, &body); err != nil {
		t.Fatalf("request body is not valid JSON: %v", err)
	}
	if _, ok := body["verifyPlayerFinanceInfo"]; !ok {
		t.Error("request body missing key 'verifyPlayerFinanceInfo'")
	}
	if _, ok := body["verifyPlayerTransactionHistory"]; !ok {
		t.Error("request body missing key 'verifyPlayerTransactionHistory'")
	}

	// 验证 finance 子对象中的字段
	fi, _ := body["verifyPlayerFinanceInfo"].(map[string]any)
	if fi["bcNumber"] != "1234567890" {
		t.Errorf("verifyPlayerFinanceInfo.bcNumber: got %v, want %q", fi["bcNumber"], "1234567890")
	}
	if fi["bcHolderName"] != "John Doe" {
		t.Errorf("verifyPlayerFinanceInfo.bcHolderName: got %v, want %q", fi["bcHolderName"], "John Doe")
	}

	// 验证 history 子对象中的字段（JSON key 必须是 verifyPlayerTransactionHistory）
	hi, _ := body["verifyPlayerTransactionHistory"].(map[string]any)
	if hi["lastDepositAmount"] != "1000" {
		t.Errorf("verifyPlayerTransactionHistory.lastDepositAmount: got %v, want %q", hi["lastDepositAmount"], "1000")
	}
}

// ── TestVerifyPlayerInfo_HTTPErrors ───────────────────────────────────────────

func TestVerifyPlayerInfo_Non200(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprint(w, `{"error":"bad gateway"}`)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.VerifyPlayerInfo(context.Background(), standardHeaders, standardReq)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	// readBody prefixes the snippet: "status 502: ..."
	if !strings.Contains(err.Error(), "502") {
		t.Errorf("error should contain status code, got: %v", err)
	}
	if calls != 3 {
		t.Errorf("retry count: got %d, want 3", calls)
	}
}

func TestVerifyPlayerInfo_500InternalError(t *testing.T) {
	srv := newCapturingHandler(http.StatusInternalServerError, `{"message":"internal error"}`, nil)
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.VerifyPlayerInfo(context.Background(), standardHeaders, standardReq)
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should contain 500, got: %v", err)
	}
}

func TestVerifyPlayerInfo_InvalidJSON(t *testing.T) {
	srv := newCapturingHandler(http.StatusOK, `{not valid json}`, nil)
	defer srv.Close()

	_, err := newTestClient(srv.URL).
		VerifyPlayerInfo(context.Background(), standardHeaders, standardReq)
	if err == nil {
		t.Fatal("expected deserialization error")
	}
	if !strings.Contains(err.Error(), "unmarshal failed") {
		t.Errorf("error should mention unmarshal, got: %v", err)
	}
}

// ── TestVerifyPlayerInfo_Retry ────────────────────────────────────────────────

// 前 2 次失败，第 3 次成功
func TestVerifyPlayerInfo_SuccessAfterRetry(t *testing.T) {
	srv, calls := newRetryHandler(2, fullMatchRespJSON)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		VerifyPlayerInfo(context.Background(), standardHeaders, standardReq)
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

// 3 次全部失败
func TestVerifyPlayerInfo_AllRetriesExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.VerifyPlayerInfo(context.Background(), standardHeaders, standardReq)
	if err == nil {
		t.Fatal("expected error when all retries exhausted")
	}
	if !strings.Contains(err.Error(), "all 3 attempts failed") {
		t.Errorf("error should mention retries exhausted, got: %v", err)
	}
}

// 每次 retry body 都能被完整重读（bytes.NewReader 保证）
func TestVerifyPlayerInfo_RetryBodyReplayable(t *testing.T) {
	attempts := 0
	var receivedBodies [][]byte

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		b, _ := io.ReadAll(r.Body)
		receivedBodies = append(receivedBodies, b)

		if attempts < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, fullMatchRespJSON)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).
		VerifyPlayerInfo(context.Background(), standardHeaders, standardReq)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(receivedBodies) != 3 {
		t.Fatalf("expected 3 attempts, got %d", len(receivedBodies))
	}
	// 每次 retry 收到的 body 必须完全相同
	for i, body := range receivedBodies[1:] {
		if string(body) != string(receivedBodies[0]) {
			t.Errorf("body mismatch on attempt %d: got %s, want %s",
				i+2, body, receivedBodies[0])
		}
	}
}

// ── TestVerifyPlayerInfo_ContextCancelled ────────────────────────────────────

func TestVerifyPlayerInfo_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, fullMatchRespJSON)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.singleReqTimeout = 50 * time.Millisecond
	client.retryDelay = 0

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	_, err := client.VerifyPlayerInfo(ctx, standardHeaders, standardReq)
	if err == nil {
		t.Fatal("expected timeout/cancel error")
	}
}

// ── TestPlayerHeaders_Apply ───────────────────────────────────────────────────

func TestPlayerHeaders_Apply(t *testing.T) {
	req, _ := http.NewRequest(http.MethodPost, "http://example.com", nil)
	h := PlayerHeaders{
		CustomerID:   "42",
		CustomerName: "testuser",
		Merchant:     "dfstar",
		CustomerIP:   "10.0.0.1",
	}
	h.apply(req)

	checks := map[string]string{
		"Content-Type": "application/json",
		"Accept":       "application/json",
		"Customerid":   "42",
		"Customername": "testuser",
		"Merchant":     "dfstar",
		"Customerip":   "10.0.0.1",
	}
	for hdr, want := range checks {
		if got := req.Header.Get(hdr); got != want {
			t.Errorf("header %q: got %q, want %q", hdr, got, want)
		}
	}
}

// ── TestVerifyFinanceHistoryResp_String ───────────────────────────────────────

func TestVerifyFinanceHistoryResp_String(t *testing.T) {
	r := &VerifyFinanceHistoryResp{
		Success: true,
		Value: VerifyFinanceHistoryResult{
			VerifyPlayerFinanceInfo: VerifyPlayerFinanceInfoResult{BcNumber: 10},
		},
	}
	s := r.String()
	if !strings.Contains(s, "bcNumber") {
		t.Errorf("String() should contain 'bcNumber', got: %s", s)
	}

	var nilResp *VerifyFinanceHistoryResp
	if nilResp.String() != "<nil>" {
		t.Errorf("nil.String() should return '<nil>', got: %s", nilResp.String())
	}
}

// ── TestReadBody_Snippet ──────────────────────────────────────────────────────

// 验证 readBody 在非 200 时截取前 512 字节作为错误摘要
func TestReadBody_Snippet(t *testing.T) {
	// 构造一个 400 响应，body 含有可识别内容
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = fmt.Fprint(w, `{"errorCode":"invalid_param","message":"bad request"}`)
	}))
	defer srv.Close()

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("http.Get error: %v", err)
	}

	_, readErr := readBody(resp)
	if readErr == nil {
		t.Fatal("expected error for non-200 response")
	}
	// 错误信息应包含状态码
	if !strings.Contains(readErr.Error(), "400") {
		t.Errorf("error should contain status 400, got: %v", readErr)
	}
	// 错误信息应包含 body 摘要
	if !strings.Contains(readErr.Error(), "invalid_param") {
		t.Errorf("error should contain body snippet, got: %v", readErr)
	}
}

// ── doWithRetry edge cases ────────────────────────────────────────────────────

// failReader always returns an error so readBody's io.ReadAll path fails.
type failReader struct{}

func (f failReader) Read(_ []byte) (int, error) { return 0, fmt.Errorf("injected read error") }

func TestReadBody_ReadBodyFails(t *testing.T) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(failReader{}),
	}
	_, err := readBody(resp)
	if err == nil {
		t.Error("expected error when body read fails")
	}
}

// TestDoWithRetry_BuildFailed covers the early-return when the request builder errors.
func TestDoWithRetry_BuildFailed(t *testing.T) {
	c := &Client{
		httpClient:       &http.Client{},
		maxRetries:       2,
		singleReqTimeout: clienthttp.DefaultSingleReqTimeout,
	}
	_, err := c.doWithRetry(context.Background(), func(_ context.Context) (*http.Request, error) {
		return nil, fmt.Errorf("bad url")
	})
	if err == nil {
		t.Error("expected build error to propagate")
	}
}

// ── TestNewClient / Config lifecycle ─────────────────────────────────────────

// TestNewClient_CreatesSingleton verifies that NewClient initialises the
// package-level singleton on the first call and returns the same pointer on
// subsequent calls.
func TestNewClient_CreatesSingleton(t *testing.T) {
	c1 := NewClient("http://mcs.test", "mcs-core/")
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
	cfg := &Config{Host: "http://mcs.test", BasPath: "mcs-core/"}
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

// TestDoWithRetry_CtxCancelledDuringRetryDelay reliably covers the ctx.Done() branch
// in the retry-delay select by pre-cancelling the context and using a non-zero retryDelay.
func TestDoWithRetry_CtxCancelledDuringRetryDelay(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.retryDelay = 500 * time.Millisecond // long enough so ctx.Done() always wins

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel — ctx.Done() is immediately ready

	_, err := c.VerifyPlayerInfo(ctx, standardHeaders, standardReq)
	if err == nil {
		t.Error("expected context-cancelled error")
	}
}

// ── TestGetRegisterIP ─────────────────────────────────────────────────────────
//
// GetRegisterIP 的契约：
//   - 成功时返回 result.Value.RegisterIP
//   - 任意失败路径（HTTP error / 非 200 / JSON 解析失败 / success=false / 超时）
//     一律返回 ""（不向上抛错）
//
// 因此所有失败场景都断言返回值为空字符串，而不是 err != nil。

// 成功响应固定 fixture
const getRegisterIPSuccessRespJSON = `{
	"success": true,
	"value": {
		"registerIp": "10.123.130.128"
	}
}`

// 业务失败响应（success=false，例如 customer_not_exist）
const getRegisterIPNotExistRespJSON = `{
	"success": false,
	"message": "customer_not_exist",
	"errorCode": "mcsfe.register.customer_not_exist"
}`

// ── 成功场景 ─────────────────────────────────────────────────────────────────

// 成功：返回正确的 registerIp。
func TestGetRegisterIP_Success(t *testing.T) {
	srv := newCapturingHandler(http.StatusOK, getRegisterIPSuccessRespJSON, nil)
	defer srv.Close()

	ip := newTestClient(srv.URL).GetRegisterIP(context.Background(), 123456)
	if ip != "10.123.130.128" {
		t.Errorf("registerIp: got %q, want %q", ip, "10.123.130.128")
	}
}

// 成功：验证请求 method、path、query、header 都正确。
func TestGetRegisterIP_RequestShape(t *testing.T) {
	var rec capturedRequest
	srv := newCapturingHandler(http.StatusOK, getRegisterIPSuccessRespJSON, &rec)
	defer srv.Close()

	_ = newTestClient(srv.URL).GetRegisterIP(context.Background(), 987654)

	if rec.method != http.MethodGet {
		t.Errorf("method: got %q, want GET", rec.method)
	}
	wantPath := "/mcs-core/register/getRegisterIp"
	if rec.path != wantPath {
		t.Errorf("path: got %q, want %q", rec.path, wantPath)
	}
	if got := rec.headers.Get("Accept"); got != "application/json" {
		t.Errorf("Accept header: got %q, want %q", got, "application/json")
	}
}

// 成功：customerId 作为 query string 拼接（验证 URL 构造）。
func TestGetRegisterIP_CustomerIDInQuery(t *testing.T) {
	gotQuery := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, getRegisterIPSuccessRespJSON)
	}))
	defer srv.Close()

	_ = newTestClient(srv.URL).GetRegisterIP(context.Background(), 42)
	if gotQuery != "customerId=42" {
		t.Errorf("query: got %q, want %q", gotQuery, "customerId=42")
	}
}

// 成功：前 2 次失败后第 3 次成功，仍能拿到 IP。
func TestGetRegisterIP_SuccessAfterRetry(t *testing.T) {
	srv, calls := newRetryHandler(2, getRegisterIPSuccessRespJSON)
	defer srv.Close()

	ip := newTestClient(srv.URL).GetRegisterIP(context.Background(), 1)
	if ip != "10.123.130.128" {
		t.Errorf("registerIp: got %q, want %q", ip, "10.123.130.128")
	}
	if *calls != 3 {
		t.Errorf("total calls: got %d, want 3", *calls)
	}
}

// ── 失败场景 ─────────────────────────────────────────────────────────────────

// 业务失败：success=false，应返回空字符串。
func TestGetRegisterIP_BusinessFailure(t *testing.T) {
	srv := newCapturingHandler(http.StatusOK, getRegisterIPNotExistRespJSON, nil)
	defer srv.Close()

	ip := newTestClient(srv.URL).GetRegisterIP(context.Background(), 999)
	if ip != "" {
		t.Errorf("registerIp on business failure: got %q, want \"\"", ip)
	}
}

// HTTP 5xx：所有重试都失败，应返回空字符串，且确实触发了 3 次重试。
func TestGetRegisterIP_HTTPError(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprint(w, `{"error":"bad gateway"}`)
	}))
	defer srv.Close()

	ip := newTestClient(srv.URL).GetRegisterIP(context.Background(), 1)
	if ip != "" {
		t.Errorf("registerIp on HTTP 502: got %q, want \"\"", ip)
	}
	if calls != 3 {
		t.Errorf("retry count: got %d, want 3", calls)
	}
}

// HTTP 500：返回空字符串。
func TestGetRegisterIP_500InternalError(t *testing.T) {
	srv := newCapturingHandler(http.StatusInternalServerError, `{"message":"internal error"}`, nil)
	defer srv.Close()

	ip := newTestClient(srv.URL).GetRegisterIP(context.Background(), 1)
	if ip != "" {
		t.Errorf("registerIp on HTTP 500: got %q, want \"\"", ip)
	}
}

// 响应体不是合法 JSON：应返回空字符串（unmarshal 失败被吞掉）。
func TestGetRegisterIP_InvalidJSON(t *testing.T) {
	srv := newCapturingHandler(http.StatusOK, `{not valid json}`, nil)
	defer srv.Close()

	ip := newTestClient(srv.URL).GetRegisterIP(context.Background(), 1)
	if ip != "" {
		t.Errorf("registerIp on invalid JSON: got %q, want \"\"", ip)
	}
}

// 所有 3 次重试都失败：返回空字符串。
func TestGetRegisterIP_AllRetriesExhausted(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ip := newTestClient(srv.URL).GetRegisterIP(context.Background(), 1)
	if ip != "" {
		t.Errorf("registerIp when all retries fail: got %q, want \"\"", ip)
	}
	if calls != 3 {
		t.Errorf("total calls: got %d, want 3", calls)
	}
}

// ── 超时场景 ─────────────────────────────────────────────────────────────────

// 单次请求超时：服务端故意慢响应（超过 singleReqTimeout），3 次重试全部超时，
// 应返回空字符串。
func TestGetRegisterIP_SingleReqTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, getRegisterIPSuccessRespJSON)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.singleReqTimeout = 30 * time.Millisecond // 每次请求都会超时
	c.retryDelay = 0

	start := time.Now()
	ip := c.GetRegisterIP(context.Background(), 1)
	elapsed := time.Since(start)

	if ip != "" {
		t.Errorf("registerIp on single-req timeout: got %q, want \"\"", ip)
	}
	// 至少应跑完一次重试（不严格断言重试次数，避免依赖时序）
	if elapsed < 30*time.Millisecond {
		t.Errorf("elapsed=%v should be at least one singleReqTimeout window", elapsed)
	}
}

// 整体上下文超时：调用方传入的 ctx 提前超时，应返回空字符串。
func TestGetRegisterIP_ContextTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, getRegisterIPSuccessRespJSON)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.singleReqTimeout = 50 * time.Millisecond
	c.retryDelay = 0

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	ip := c.GetRegisterIP(ctx, 1)
	if ip != "" {
		t.Errorf("registerIp on ctx timeout: got %q, want \"\"", ip)
	}
}

// 上下文已取消：立即返回空字符串。
func TestGetRegisterIP_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(srv.URL)
	c.retryDelay = 500 * time.Millisecond // 让 ctx.Done() 一定先就绪

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 预先取消

	ip := c.GetRegisterIP(ctx, 1)
	if ip != "" {
		t.Errorf("registerIp on cancelled ctx: got %q, want \"\"", ip)
	}
}
