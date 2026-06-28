//go:build test

// client_test.go — USS HTTP 客户端单元测试(httptest mock)。带 build tag `test`。
//
// 运行 / Run:  go test -tags test ./internal/client/uss/ -v
// 覆盖：GetCustomer / GeneratePasswordResetToken / GetCustomerPersonalInfo +
//   NullString/NullInt32/FlexTime 解码。

package uss

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"tcg-rulex-engine/internal/client/clienthttp"
)

// newTestClient creates a Client directly (bypasses the globalOnce singleton)
// and points it at the given base URL with no retry delay so tests run fast.
func newTestClient(baseURL string) *Client {
	return &Client{
		httpClient:       &http.Client{},
		baseURL:          baseURL,
		basePath:         "tcg-uss-ae/",
		maxRetries:       3,
		retryDelay:       0, // no delay in tests
		singleReqTimeout: clienthttp.DefaultSingleReqTimeout,
	}
}

// ── fixtures ────────────────────────────────────────────────────────────────

// customerJSON is a realistic USS response for customerName=a71bdtf1@devpm01.
const customerJSON = `{
  "success": true,
  "value": {
    "customerId": 100001,
    "customerName": "a71bdtf1@devpm01",
    "customerNameExcludeMerchant": "devpm01",
    "password": "abc123hash",
    "paymentPassword": null,
    "activeFlag": 1,
    "email": "devpm01@test.com",
    "systemId": 9999,
    "errorTime": null,
    "merchantId": 5001,
    "merchantCode": "a71bdtf1",
    "hashAlgorithm": 1,
    "loginLanguage": "EN",
    "profile": {
      "createTime": "2025-01-10 09:00:00",
      "updateTime": "2026-03-10 12:00:00",
      "version": 42,
      "customerId": 100001,
      "customerName": "devpm01",
      "merchantCode": "a71bdtf1",
      "nickname": "devtester",
      "type": 2,
      "typeUpdateTime": "2025-01-10 09:00:00",
      "regDate": "2025-01-10 09:00:00",
      "passwdLastModifyDate": null,
      "mobileNo": "6591234567",
      "qqNo": null,
      "lineId": "line001",
      "lineUuid": null,
      "whatsAppId": "6591234567",
      "facebookId": "fb001",
      "twitter": "tw001",
      "viber": "vb001",
      "zalo": "zl001",
      "idNumber": "S0000001A",
      "login": null,
      "lastLoginIp": "10.0.0.1",
      "recommenderId": null,
      "winProbability": null,
      "noActive": null,
      "levelId": null,
      "createSuboFlag": 1,
      "payeeName": "Dev PM01",
      "city": "Singapore",
      "zipcode": "018989",
      "address": "1 Test Street",
      "lastLoginTime": "2026-03-10 08:00:00",
      "lastLogoutTime": null,
      "firstDepositTime": null,
      "updatePayeeNameTime": null,
      "lastWithdrawTime": null,
      "lastDepositTime": null,
      "birthday": "1990-06-15 00:00:00",
      "verificationMode": null,
      "refer": null,
      "icon": null,
      "nickname2": null,
      "wechat": "wc001",
      "telegram": "tg001",
      "countryCode": "SG",
      "idVerification": false,
      "previousLoginTime": "2026-03-09 20:00:00",
      "previousLoginIp": "10.0.0.2",
      "appleId": "ap001",
      "gender": 1,
      "maritalStatus": 1,
      "idType": 1,
      "sourceOfIncome": 2,
      "occupation": 3,
      "twitterId": null,
      "idVerificationStatus": "N",
      "activeFlag": 1,
      "activeFlagUpdateTime": "2026-03-10 12:00:00",
      "email": "devpm01@test.com"
    },
    "merchant": {
      "merchantDesc": "a71bdtf1",
      "customerId": 5000,
      "parentId": 100,
      "groupId": 200,
      "deptId": 300,
      "status": 1,
      "creator": null,
      "deleteFlag": null,
      "type": 2,
      "currencyCode": "SGD",
      "merchantTimeZone": 8
    },
    "idVerificationStatus": "N",
    "customerAdditionalInfo": {
      "createTime": "2025-01-10 09:00:00",
      "updateTime": "2025-01-10 09:00:00",
      "version": 0,
      "customerId": 100001,
      "permanentAddress": "1 Test Street",
      "placeOfBirth": "Singapore",
      "nationality": "Singaporean",
      "region": "SEA",
      "kakao": null,
      "officialAppLoginStatus": null,
      "emailVerification": null,
      "glifeId": null,
      "mayaId": null,
      "telegramId": "tg001",
      "usState": null,
      "googleId": null,
      "appleUid": null,
      "facebookUid": null,
      "google": null
    }
  }
}`

// nullAdditionalInfoJSON is a response where customerAdditionalInfo is explicitly null.
const nullAdditionalInfoJSON = `{
  "success": true,
  "value": {
    "customerId": 100002,
    "customerName": "a71bdtf1@devpm01",
    "customerNameExcludeMerchant": "devpm01",
    "password": "hash",
    "paymentPassword": null,
    "activeFlag": 1,
    "email": null,
    "systemId": 9999,
    "errorTime": null,
    "merchantId": 5001,
    "merchantCode": "a71bdtf1",
    "hashAlgorithm": 1,
    "loginLanguage": null,
    "profile": {
      "createTime": "2025-01-10 09:00:00",
      "updateTime": "2025-01-10 09:00:00",
      "version": 1,
      "customerId": 100002,
      "customerName": "devpm01",
      "merchantCode": "a71bdtf1",
      "nickname": null,
      "type": 2,
      "typeUpdateTime": "2025-01-10 09:00:00",
      "regDate": "2025-01-10 09:00:00",
      "passwdLastModifyDate": null,
      "mobileNo": "6500000000",
      "qqNo": null,
      "lineId": null,
      "lineUuid": null,
      "whatsAppId": null,
      "facebookId": null,
      "twitter": null,
      "viber": null,
      "zalo": null,
      "idNumber": null,
      "login": null,
      "lastLoginIp": null,
      "recommenderId": null,
      "winProbability": null,
      "noActive": null,
      "levelId": null,
      "createSuboFlag": 0,
      "payeeName": null,
      "city": null,
      "zipcode": null,
      "address": null,
      "lastLoginTime": null,
      "lastLogoutTime": null,
      "firstDepositTime": null,
      "updatePayeeNameTime": null,
      "lastWithdrawTime": null,
      "lastDepositTime": null,
      "birthday": null,
      "verificationMode": "0",
      "refer": null,
      "icon": 0,
      "nickname2": null,
      "wechat": null,
      "telegram": null,
      "countryCode": null,
      "idVerification": false,
      "previousLoginTime": null,
      "previousLoginIp": null,
      "appleId": null,
      "gender": null,
      "maritalStatus": null,
      "idType": null,
      "sourceOfIncome": null,
      "occupation": null,
      "twitterId": null,
      "idVerificationStatus": "N",
      "activeFlag": 1,
      "activeFlagUpdateTime": "2025-01-10 09:00:00",
      "email": null
    },
    "merchant": {
      "merchantDesc": "a71bdtf1",
      "customerId": 5000,
      "parentId": 100,
      "groupId": 200,
      "deptId": 300,
      "status": 1,
      "creator": null,
      "deleteFlag": null,
      "type": 2,
      "currencyCode": "SGD",
      "merchantTimeZone": 8
    },
    "idVerificationStatus": "N",
    "customerAdditionalInfo": null
  }
}`

// ── helpers ──────────────────────────────────────────────────────────────────

// newHandler returns an httptest.Server that always responds 200 OK with the
// given body. capturedURL, if non-nil, receives the request URL on first hit.
func newHandler(t *testing.T, body string, capturedURL *string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if capturedURL != nil && *capturedURL == "" {
			*capturedURL = r.URL.String()
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, body)
	}))
}

// newRetryHandler returns an httptest.Server that fails the first (failCount)
// requests, then succeeds with successBody.
func newRetryHandler(failCount int, successBody string) *httptest.Server {
	calls := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= failCount {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, successBody)
	}))
}

// ── TestGetCustomer_Success ──────────────────────────────────────────────────

// TestGetCustomer_Success verifies that the happy-path response for
// customerName=a71bdtf1@devpm01 is correctly deserialised.
func TestGetCustomer_Success(t *testing.T) {
	var capturedURL string
	srv := newHandler(t, customerJSON, &capturedURL)
	defer srv.Close()

	client := newTestClient(srv.URL)
	ctx := context.Background()

	result, err := client.GetCustomer(ctx, "a71bdtf1@devpm01", false)
	if err != nil {
		t.Fatalf("GetCustomer error: %v", err)
	}

	// ── URL query parameters ──────────────────────────────────────────────────
	// Go's fmt.Sprintf does NOT percent-encode the '@', so the raw customerName
	// is placed directly in the query string.
	parsedURL, _ := url.ParseRequestURI("http://dummy" + capturedURL)
	q := parsedURL.Query()

	if got := q.Get("customerName"); got != "a71bdtf1@devpm01" {
		t.Errorf("customerName query param: got %q, want %q", got, "a71bdtf1@devpm01")
	}
	if got := q.Get("force"); got != "false" {
		t.Errorf("force query param: got %q, want %q", got, "false")
	}

	// ── Top-level fields ──────────────────────────────────────────────────────
	if !result.Success {
		t.Error("expected Success=true")
	}
	if result.Value.CustomerID.Val != 100001 {
		t.Errorf("CustomerID: got %d, want 100001", result.Value.CustomerID.Val)
	}
	if result.Value.CustomerName.Val != "a71bdtf1@devpm01" {
		t.Errorf("CustomerName: got %q, want %q", result.Value.CustomerName.Val, "a71bdtf1@devpm01")
	}
	if result.Value.MerchantCode.Val != "a71bdtf1" {
		t.Errorf("MerchantCode: got %q, want %q", result.Value.MerchantCode.Val, "a71bdtf1")
	}

	// ── Profile fields ────────────────────────────────────────────────────────
	p := result.Value.Profile
	if p.MobileNo.Val != "6591234567" {
		t.Errorf("Profile.MobileNo: got %q, want %q", p.MobileNo.Val, "6591234567")
	}
	if p.VerificationMode.Val != "" {
		// verificationMode is null in this fixture
		t.Errorf("Profile.VerificationMode: got %q, want empty (null)", p.VerificationMode.Val)
	}
	if p.Gender.Val != 1 {
		t.Errorf("Profile.Gender: got %d, want 1", p.Gender.Val)
	}
	wantBirthday := "1990-06-15"
	if got := p.Birthday.FormatDate(); got != wantBirthday {
		t.Errorf("Profile.Birthday.FormatDate(): got %q, want %q", got, wantBirthday)
	}

	// ── CustomerAdditionalInfo ────────────────────────────────────────────────
	a := result.Value.CustomerAdditionalInfo
	if a.Nationality.Val != "Singaporean" {
		t.Errorf("AdditionalInfo.Nationality: got %q, want %q", a.Nationality.Val, "Singaporean")
	}
	if a.EmailVerification {
		t.Error("AdditionalInfo.EmailVerification: got true, want false (null → false)")
	}
}

// TestGetCustomer_ForceTrue verifies the force=true query parameter.
func TestGetCustomer_ForceTrue(t *testing.T) {
	var capturedURL string
	srv := newHandler(t, customerJSON, &capturedURL)
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.GetCustomer(context.Background(), "a71bdtf1@devpm01", true)
	if err != nil {
		t.Fatalf("GetCustomer error: %v", err)
	}

	parsed, _ := url.ParseRequestURI("http://dummy" + capturedURL)
	if got := parsed.Query().Get("force"); got != "true" {
		t.Errorf("force query param: got %q, want %q", got, "true")
	}
}

// ── TestGetCustomer_NullAdditionalInfo ───────────────────────────────────────

// TestGetCustomer_NullAdditionalInfo verifies that a response with
// "customerAdditionalInfo": null does NOT crash and returns zero-value fields.
func TestGetCustomer_NullAdditionalInfo(t *testing.T) {
	srv := newHandler(t, nullAdditionalInfoJSON, nil)
	defer srv.Close()

	client := newTestClient(srv.URL)
	result, err := client.GetCustomer(context.Background(), "a71bdtf1@devpm01", false)
	if err != nil {
		t.Fatalf("GetCustomer with null additionalInfo error: %v", err)
	}

	if !result.Success {
		t.Error("expected Success=true")
	}
	// Additional info should be zero value (empty strings, false bools).
	a := result.Value.CustomerAdditionalInfo
	if a.PermanentAddress.Val != "" {
		t.Errorf("expected empty PermanentAddress, got %q", a.PermanentAddress.Val)
	}
	if a.EmailVerification {
		t.Error("expected EmailVerification=false when additionalInfo is null")
	}
}

// TestGetCustomer_NullAdditionalInfo_IconInteger verifies that "icon": 0
// (integer, not null or string) in profile does not cause a deserialisation error.
func TestGetCustomer_NullAdditionalInfo_IconInteger(t *testing.T) {
	srv := newHandler(t, nullAdditionalInfoJSON, nil)
	defer srv.Close()

	client := newTestClient(srv.URL)
	result, err := client.GetCustomer(context.Background(), "a71bdtf1@devpm01", false)
	if err != nil {
		t.Fatalf("GetCustomer with icon=0 error: %v", err)
	}
	// The fixture has `"verificationMode": "0"` — must be decoded correctly.
	if result.Value.Profile.VerificationMode.Val != "0" {
		t.Errorf("VerificationMode: got %q, want %q",
			result.Value.Profile.VerificationMode.Val, "0")
	}
}

// ── TestGetCustomer_HTTP errors ──────────────────────────────────────────────

// TestGetCustomer_Non200 verifies that a non-200 response returns an error
// (and retries up to maxRetries times).
func TestGetCustomer_Non200(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway) // 502
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.retryDelay = 0

	_, err := client.GetCustomer(context.Background(), "a71bdtf1@devpm01", false)
	if err == nil {
		t.Fatal("expected error for non-200 response, got nil")
	}
	if !strings.Contains(err.Error(), "502") && !strings.Contains(err.Error(), "unexpected status") {
		t.Errorf("error should mention status code, got: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 retry attempts, got %d", calls)
	}
}

// TestGetCustomer_InvalidJSON verifies that malformed JSON returns a
// deserialisation error.
func TestGetCustomer_InvalidJSON(t *testing.T) {
	srv := newHandler(t, `{not valid json}`, nil)
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.GetCustomer(context.Background(), "a71bdtf1@devpm01", false)
	if err == nil {
		t.Fatal("expected deserialization error, got nil")
	}
	if !strings.Contains(err.Error(), "deserialization failed") {
		t.Errorf("error should mention deserialization, got: %v", err)
	}
}

// TestGetCustomer_SuccessAfterRetry verifies that GetCustomer succeeds when
// the first two attempts fail and the third succeeds.
func TestGetCustomer_SuccessAfterRetry(t *testing.T) {
	srv := newRetryHandler(2, customerJSON)
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.retryDelay = 0

	result, err := client.GetCustomer(context.Background(), "a71bdtf1@devpm01", false)
	if err != nil {
		t.Fatalf("GetCustomer should succeed after 2 failures: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
}

// TestGetCustomer_AllRetriesExhausted verifies that all retries exhausted
// returns an error when the server is always down.
func TestGetCustomer_AllRetriesExhausted(t *testing.T) {
	// Server that always returns 503.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.retryDelay = 0

	_, err := client.GetCustomer(context.Background(), "a71bdtf1@devpm01", false)
	if err == nil {
		t.Fatal("expected error when all retries exhausted")
	}
	if !strings.Contains(err.Error(), "all 3 attempts failed") {
		t.Errorf("error should mention retries exhausted, got: %v", err)
	}
}

// TestGetCustomer_ContextCancelled verifies that context cancellation stops
// retries immediately.
func TestGetCustomer_ContextCancelled(t *testing.T) {
	// Server that hangs briefly; context will be cancelled first.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, customerJSON)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.singleReqTimeout = 50 * time.Millisecond // timeout before server responds
	client.retryDelay = 0

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	_, err := client.GetCustomer(ctx, "a71bdtf1@devpm01", false)
	if err == nil {
		t.Fatal("expected timeout/cancel error")
	}
}

// ── TestGetCustomer_URLPath ──────────────────────────────────────────────────

// TestGetCustomer_URLPath verifies that the request path and query string
// are formed correctly for customerName=a71bdtf1@devpm01&force=false.
func TestGetCustomer_URLPath(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, customerJSON)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.GetCustomer(context.Background(), "a71bdtf1@devpm01", false)
	if err != nil {
		t.Fatalf("GetCustomer error: %v", err)
	}

	// Path must include the basePath and endpoint.
	if !strings.Contains(capturedPath, "tcg-uss-ae/customer") {
		t.Errorf("path should contain 'tcg-uss-ae/customer', got: %s", capturedPath)
	}
	// customerName=a71bdtf1@devpm01 must appear in the query string.
	// Go uses fmt.Sprintf (no encoding) so @ arrives as-is at the server.
	if !strings.Contains(capturedPath, "customerName=a71bdtf1@devpm01") {
		t.Errorf("query should contain raw customerName, got: %s", capturedPath)
	}
	if !strings.Contains(capturedPath, "force=false") {
		t.Errorf("query should contain force=false, got: %s", capturedPath)
	}
}

// ── TestNullTypes ────────────────────────────────────────────────────────────

// TestNullString_UnmarshalJSON verifies the NullString type handles null,
// empty string, and a valid value correctly.
func TestNullString_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		input     string
		wantVal   string
		wantValid bool
	}{
		{`"hello"`, "hello", true},
		{`null`, "", false},
		{`""`, "", false},
		{`"0"`, "0", true},
	}
	for _, tc := range cases {
		var s NullString
		if err := json.Unmarshal([]byte(tc.input), &s); err != nil {
			t.Errorf("UnmarshalJSON(%s): %v", tc.input, err)
			continue
		}
		if s.Val != tc.wantVal || s.Valid != tc.wantValid {
			t.Errorf("UnmarshalJSON(%s) = {%q, %v}, want {%q, %v}",
				tc.input, s.Val, s.Valid, tc.wantVal, tc.wantValid)
		}
	}
}

// TestNullInt32_UnmarshalJSON verifies the NullInt32 type.
func TestNullInt32_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		input     string
		wantVal   int32
		wantValid bool
	}{
		{`42`, 42, true},
		{`0`, 0, true},
		{`null`, 0, false},
	}
	for _, tc := range cases {
		var n NullInt32
		if err := json.Unmarshal([]byte(tc.input), &n); err != nil {
			t.Errorf("UnmarshalJSON(%s): %v", tc.input, err)
			continue
		}
		if n.Val != tc.wantVal || n.Valid != tc.wantValid {
			t.Errorf("UnmarshalJSON(%s) = {%d, %v}, want {%d, %v}",
				tc.input, n.Val, n.Valid, tc.wantVal, tc.wantValid)
		}
	}
}

// TestFlexTime_UnmarshalJSON verifies FlexTime handles the USS datetime format.
func TestFlexTime_UnmarshalJSON(t *testing.T) {
	cases := []struct {
		input    string
		wantDate string
		wantZero bool
	}{
		{input: `"2025-01-10 09:00:00"`, wantZero: false, wantDate: "2025-01-10"},
		{input: `null`, wantZero: true, wantDate: ""},
		{input: `""`, wantZero: true, wantDate: ""},
	}
	for _, tc := range cases {
		var ft FlexTime
		if err := json.Unmarshal([]byte(tc.input), &ft); err != nil {
			t.Errorf("FlexTime.UnmarshalJSON(%s): %v", tc.input, err)
			continue
		}
		if ft.IsZero() != tc.wantZero {
			t.Errorf("FlexTime.IsZero() for %s: got %v, want %v", tc.input, ft.IsZero(), tc.wantZero)
		}
		if got := ft.FormatDate(); got != tc.wantDate {
			t.Errorf("FlexTime.FormatDate() for %s: got %q, want %q", tc.input, got, tc.wantDate)
		}
	}
}

// ════════════════════════════════════════════════════════════════════════════
// GeneratePasswordResetToken tests
// ════════════════════════════════════════════════════════════════════════════

// ── fixtures ─────────────────────────────────────────────────────────────────

// successTokenResp: 正常响应，包含一次性密码 token
const successTokenResp = `{"success":true,"value":"OTP-TOKEN-ABC123"}`

// successTokenEmptyValue: success=true 但 value 为空字符串（业务不认可）
const successTokenEmptyValue = `{"success":true,"value":""}`

// failTokenResp: USS 返回 success=false
const failTokenResp = `{"success":false,"value":""}`

// ── capturedPutRequest holds server-side view of one PUT request ──────────────

type capturedPutRequest struct {
	method string
	path   string
	body   []byte
}

func newPutCapture(status int, respBody string, out *capturedPutRequest) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if out != nil && out.path == "" {
			out.method = r.Method
			out.path = r.URL.Path
			b := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(b)
			out.body = b
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, respBody)
	}))
}

// newPutRetryHandler 前 failCount 次返回 503，之后成功。
func newPutRetryHandler(failCount int, successBody string) (*httptest.Server, *int) {
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

// ── TestGeneratePasswordResetToken_Success ────────────────────────────────────

// TestGeneratePasswordResetToken_Success 验证正常响应解析 token 正确。
func TestGeneratePasswordResetToken_Success(t *testing.T) {
	srv := newHandler(t, successTokenResp, nil)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		GeneratePasswordResetToken(context.Background(), "devpm01", "a71bdtf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("Success: got false, want true")
	}
	if result.Value != "OTP-TOKEN-ABC123" {
		t.Errorf("Value: got %q, want %q", result.Value, "OTP-TOKEN-ABC123")
	}
}

// TestGeneratePasswordResetToken_SuccessFalse 验证 success=false 时不报 error，
// 由业务层检查 Success 字段。
func TestGeneratePasswordResetToken_SuccessFalse(t *testing.T) {
	srv := newHandler(t, failTokenResp, nil)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		GeneratePasswordResetToken(context.Background(), "devpm01", "a71bdtf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("Success: got true, want false")
	}
	if result.Value != "" {
		t.Errorf("Value: got %q, want empty", result.Value)
	}
}

// TestGeneratePasswordResetToken_EmptyToken 验证 value="" 时仍能正常解析。
func TestGeneratePasswordResetToken_EmptyToken(t *testing.T) {
	srv := newHandler(t, successTokenEmptyValue, nil)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		GeneratePasswordResetToken(context.Background(), "devpm01", "a71bdtf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Value != "" {
		t.Errorf("Value: got %q, want empty", result.Value)
	}
}

// ── TestGeneratePasswordResetToken_RequestShape ───────────────────────────────

// TestGeneratePasswordResetToken_RequestShape 验证：
//   - Method = PUT
//   - Path   = /tcg-uss-ae/password/reset-generate
//   - Body   = {"customerName":"devpm01","merchantCode":"a71bdtf1"}
func TestGeneratePasswordResetToken_RequestShape(t *testing.T) {
	var rec capturedPutRequest
	srv := newPutCapture(http.StatusOK, successTokenResp, &rec)
	defer srv.Close()

	_, err := newTestClient(srv.URL).
		GeneratePasswordResetToken(context.Background(), "devpm01", "a71bdtf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// ── Method ────────────────────────────────────────────────────────────────
	if rec.method != http.MethodPut {
		t.Errorf("method: got %q, want PUT", rec.method)
	}

	// ── Path ──────────────────────────────────────────────────────────────────
	wantPath := "/tcg-uss-ae/password/reset-generate"
	if rec.path != wantPath {
		t.Errorf("path: got %q, want %q", rec.path, wantPath)
	}

	// ── Body JSON ─────────────────────────────────────────────────────────────
	var body map[string]string
	if err := json.Unmarshal(rec.body, &body); err != nil {
		t.Fatalf("request body is not valid JSON: %v (raw: %s)", err, rec.body)
	}
	if body["customerName"] != "devpm01" {
		t.Errorf("body.customerName: got %q, want %q", body["customerName"], "devpm01")
	}
	if body["merchantCode"] != "a71bdtf1" {
		t.Errorf("body.merchantCode: got %q, want %q", body["merchantCode"], "a71bdtf1")
	}
}

// TestGeneratePasswordResetToken_URLPath 使用完整路径断言（RequestURI）。
func TestGeneratePasswordResetToken_URLPath(t *testing.T) {
	var capturedURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, successTokenResp)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).
		GeneratePasswordResetToken(context.Background(), "devpm01", "a71bdtf1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(capturedURI, "tcg-uss-ae/password/reset-generate") {
		t.Errorf("URI should contain 'tcg-uss-ae/password/reset-generate', got: %s", capturedURI)
	}
}

// ── TestGeneratePasswordResetToken_HTTPErrors ─────────────────────────────────

// TestGeneratePasswordResetToken_Non200 验证 502 响应触发 3 次重试并返回错误。
func TestGeneratePasswordResetToken_Non200(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.GeneratePasswordResetToken(context.Background(), "devpm01", "a71bdtf1")
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if !strings.Contains(err.Error(), "502") && !strings.Contains(err.Error(), "unexpected status") {
		t.Errorf("error should mention status code, got: %v", err)
	}
	if calls != 3 {
		t.Errorf("retry count: got %d, want 3", calls)
	}
}

// TestGeneratePasswordResetToken_InvalidJSON 验证服务端返回非法 JSON 时报错。
func TestGeneratePasswordResetToken_InvalidJSON(t *testing.T) {
	srv := newHandler(t, `{not valid json}`, nil)
	defer srv.Close()

	_, err := newTestClient(srv.URL).
		GeneratePasswordResetToken(context.Background(), "devpm01", "a71bdtf1")
	if err == nil {
		t.Fatal("expected deserialization error")
	}
	if !strings.Contains(err.Error(), "deserialization failed") {
		t.Errorf("error should mention deserialization, got: %v", err)
	}
}

// ── TestGeneratePasswordResetToken_Retry ──────────────────────────────────────

// TestGeneratePasswordResetToken_SuccessAfterRetry 前 2 次失败、第 3 次成功。
func TestGeneratePasswordResetToken_SuccessAfterRetry(t *testing.T) {
	srv, calls := newPutRetryHandler(2, successTokenResp)
	defer srv.Close()

	result, err := newTestClient(srv.URL).
		GeneratePasswordResetToken(context.Background(), "devpm01", "a71bdtf1")
	if err != nil {
		t.Fatalf("should succeed after 2 failures: %v", err)
	}
	if !result.Success {
		t.Error("Success: want true")
	}
	if result.Value != "OTP-TOKEN-ABC123" {
		t.Errorf("Value: got %q, want OTP-TOKEN-ABC123", result.Value)
	}
	if *calls != 3 {
		t.Errorf("total calls: got %d, want 3", *calls)
	}
}

// TestGeneratePasswordResetToken_AllRetriesExhausted 3 次全部失败。
func TestGeneratePasswordResetToken_AllRetriesExhausted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	_, err := newTestClient(srv.URL).
		GeneratePasswordResetToken(context.Background(), "devpm01", "a71bdtf1")
	if err == nil {
		t.Fatal("expected error when all retries exhausted")
	}
	if !strings.Contains(err.Error(), "all 3 attempts failed") {
		t.Errorf("error should mention retries exhausted, got: %v", err)
	}
}

// ── TestGeneratePasswordResetToken_ContextCancelled ───────────────────────────

func TestGeneratePasswordResetToken_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, successTokenResp)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.singleReqTimeout = 50 * time.Millisecond
	client.retryDelay = 0

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()

	_, err := client.GeneratePasswordResetToken(ctx, "devpm01", "a71bdtf1")
	if err == nil {
		t.Fatal("expected timeout/cancel error")
	}
}

// ── TestGeneratePasswordResetToken_DifferentMerchants ─────────────────────────

// ── TestNewClient / Config lifecycle ─────────────────────────────────────────

// TestNewClient_CreatesSingleton verifies that NewClient initialises the
// package-level singleton on the first call and returns the same pointer on
// subsequent calls.
func TestNewClient_CreatesSingleton(t *testing.T) {
	c1 := NewClient("http://uss.test", "tcg-uss-ae/")
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
	cfg := &Config{Host: "http://uss.test", BasPath: "tcg-uss-ae/"}
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

// TestGeneratePasswordResetToken_DifferentMerchants 验证不同 merchantCode 都能
// 正确放入请求 body，不存在全局状态污染。
func TestGeneratePasswordResetToken_DifferentMerchants(t *testing.T) {
	merchants := []struct {
		customerName string
		merchantCode string
	}{
		{"user001", "dfstar"},
		{"user002", "gi8viet2"},
		{"user003@special", "a71bdtf1"},
	}

	for _, tc := range merchants {
		t.Run(tc.merchantCode, func(t *testing.T) {
			var rec capturedPutRequest
			srv := newPutCapture(http.StatusOK, successTokenResp, &rec)
			defer srv.Close()

			_, err := newTestClient(srv.URL).
				GeneratePasswordResetToken(context.Background(), tc.customerName, tc.merchantCode)
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", tc.merchantCode, err)
			}

			var body map[string]string
			if err := json.Unmarshal(rec.body, &body); err != nil {
				t.Fatalf("invalid JSON body: %v", err)
			}
			if body["customerName"] != tc.customerName {
				t.Errorf("customerName: got %q, want %q", body["customerName"], tc.customerName)
			}
			if body["merchantCode"] != tc.merchantCode {
				t.Errorf("merchantCode: got %q, want %q", body["merchantCode"], tc.merchantCode)
			}
		})
	}
}

// ════════════════════════════════════════════════════════════════════════════
// GetCustomerPersonalInfo tests
// ════════════════════════════════════════════════════════════════════════════

const personalInfoJSON = `{
  "success": true,
  "value": {
    "customerId": 359039,
    "email": "lena359039@123.com",
    "password": "478a8ca1da392621f978af16ce04de83",
    "paymentPassword": null,
    "merchantCode": "gi8viet",
    "customerName": "lena4843002",
    "type": 2,
    "recommenderId": null,
    "nickname": "nickname002",
    "payeeName": "PAYEE002",
    "mobileNo": "1111111002",
    "countryCode": "EN",
    "qqNo": "002",
    "wechat": "wechat002",
    "lineId": "lineId002",
    "facebookId": "facebookId002",
    "whatsAppId": "whatsAppId002",
    "idNumber": "002",
    "zalo": "zalo002",
    "birthday": "2000-01-01 00:00:00",
    "telegram": "telegram002",
    "gender": 1,
    "maritalStatus": 1,
    "idType": 1,
    "idVerificationStatus": "N",
    "idVerification": false,
    "sourceOfIncome": 1,
    "occupation": 1,
    "city": "city002",
    "zipcode": "002",
    "address": "address002",
    "twitter": "twitter002",
    "viber": "viber002",
    "appleId": "002",
    "passwordLastModifyDate": null,
    "permanentAddress": "permanentAddress002",
    "placeOfBirth": "start",
    "nationality": "1",
    "region": "region002",
    "kakao": "kakao002",
    "isOfficialAppLogin": null,
    "isEmailVerification": false,
    "glifeId": null,
    "mayaId": null,
    "usState": 11,
    "googleId": "ek::V3TnVB6odAt",
    "facebookUid": null,
    "google": "ek::V3TnVBHhHyAFk",
    "appleUid": null,
    "stateValue": null,
    "recommenders": null,
    "isMobileVerified": false
  }
}`

func TestGetCustomerPersonalInfo_Success(t *testing.T) {
	var capturedURL string
	srv := newHandler(t, personalInfoJSON, &capturedURL)
	defer srv.Close()

	client := newTestClient(srv.URL)
	result, err := client.GetCustomerPersonalInfo(context.Background(), 359039)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// URL should contain customerId=359039
	if !strings.Contains(capturedURL, "customerId=359039") {
		t.Errorf("URL should contain customerId=359039, got: %s", capturedURL)
	}

	if result.CustomerID.Val != 359039 {
		t.Errorf("CustomerID: got %d, want 359039", result.CustomerID.Val)
	}
	if result.Email.Val != "lena359039@123.com" {
		t.Errorf("Email: got %q, want %q", result.Email.Val, "lena359039@123.com")
	}
	if result.MerchantCode.Val != "gi8viet" {
		t.Errorf("MerchantCode: got %q, want %q", result.MerchantCode.Val, "gi8viet")
	}
	if result.Kakao.Val != "kakao002" {
		t.Errorf("Kakao: got %q, want %q", result.Kakao.Val, "kakao002")
	}
	if result.Nickname.Val != "nickname002" {
		t.Errorf("Nickname: got %q, want %q", result.Nickname.Val, "nickname002")
	}
	if result.Gender.Val != 1 {
		t.Errorf("Gender: got %d, want 1", result.Gender.Val)
	}
	if result.UsState.Val != 11 {
		t.Errorf("UsState: got %d, want 11", result.UsState.Val)
	}
	wantBirthday := "2000-01-01"
	if got := result.Birthday.FormatDate(); got != wantBirthday {
		t.Errorf("Birthday: got %q, want %q", got, wantBirthday)
	}
	if result.PlaceOfBirth.Val != "start" {
		t.Errorf("PlaceOfBirth: got %q, want %q", result.PlaceOfBirth.Val, "start")
	}
	if result.Region.Val != "region002" {
		t.Errorf("Region: got %q, want %q", result.Region.Val, "region002")
	}
}

func TestGetCustomerPersonalInfo_NullFields(t *testing.T) {
	nullFieldsJSON := `{
		"success": true,
		"value": {
			"customerId": 100,
			"email": null,
			"password": null,
			"paymentPassword": null,
			"merchantCode": "test",
			"customerName": "user001",
			"type": 2,
			"recommenderId": null,
			"nickname": null,
			"payeeName": null,
			"mobileNo": null,
			"countryCode": null,
			"qqNo": null,
			"wechat": null,
			"lineId": null,
			"facebookId": null,
			"whatsAppId": null,
			"idNumber": null,
			"zalo": null,
			"birthday": null,
			"telegram": null,
			"gender": null,
			"maritalStatus": null,
			"idType": null,
			"idVerificationStatus": null,
			"idVerification": null,
			"sourceOfIncome": null,
			"occupation": null,
			"city": null,
			"zipcode": null,
			"address": null,
			"twitter": null,
			"viber": null,
			"appleId": null,
			"passwordLastModifyDate": null,
			"permanentAddress": null,
			"placeOfBirth": null,
			"nationality": null,
			"region": null,
			"kakao": null,
			"isOfficialAppLogin": null,
			"isEmailVerification": null,
			"glifeId": null,
			"mayaId": null,
			"usState": null,
			"googleId": null,
			"facebookUid": null,
			"google": null,
			"appleUid": null,
			"stateValue": null,
			"recommenders": null,
			"isMobileVerified": null
		}
	}`

	srv := newHandler(t, nullFieldsJSON, nil)
	defer srv.Close()

	client := newTestClient(srv.URL)
	result, err := client.GetCustomerPersonalInfo(context.Background(), 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Email.Valid {
		t.Error("Email.Valid: want false (null)")
	}
	if result.Kakao.Valid {
		t.Error("Kakao.Valid: want false (null)")
	}
	if result.Birthday.FormatDate() != "" {
		t.Errorf("Birthday: got %q, want empty (null)", result.Birthday.FormatDate())
	}
}

func TestGetCustomerPersonalInfo_Non200(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.retryDelay = 0

	_, err := client.GetCustomerPersonalInfo(context.Background(), 999)
	if err == nil {
		t.Fatal("expected error for non-200 response")
	}
	if calls != 3 {
		t.Errorf("retry count: got %d, want 3", calls)
	}
}

func TestGetCustomerPersonalInfo_InvalidJSON(t *testing.T) {
	srv := newHandler(t, `{not valid json}`, nil)
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.GetCustomerPersonalInfo(context.Background(), 100)
	if err == nil {
		t.Fatal("expected deserialization error")
	}
	if !strings.Contains(err.Error(), "deserialization failed") {
		t.Errorf("error should mention deserialization, got: %v", err)
	}
}

func TestGetCustomerPersonalInfo_SuccessAfterRetry(t *testing.T) {
	srv := newRetryHandler(2, personalInfoJSON)
	defer srv.Close()

	client := newTestClient(srv.URL)
	client.retryDelay = 0

	result, err := client.GetCustomerPersonalInfo(context.Background(), 359039)
	if err != nil {
		t.Fatalf("should succeed after 2 failures: %v", err)
	}
	if result.CustomerID.Val != 359039 {
		t.Errorf("CustomerID: got %d, want 359039", result.CustomerID.Val)
	}
}

func TestGetCustomerPersonalInfo_URLPath(t *testing.T) {
	var capturedURI string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURI = r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, personalInfoJSON)
	}))
	defer srv.Close()

	client := newTestClient(srv.URL)
	_, err := client.GetCustomerPersonalInfo(context.Background(), 359039)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(capturedURI, "tcg-uss-ae/customer/personal-info") {
		t.Errorf("URI should contain 'tcg-uss-ae/customer/personal-info', got: %s", capturedURI)
	}
	if !strings.Contains(capturedURI, "customerId=359039") {
		t.Errorf("URI should contain 'customerId=359039', got: %s", capturedURI)
	}
}
