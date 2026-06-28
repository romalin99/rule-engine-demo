//go:build test

// model_error_test.go — USS 错误类型与可空 JSON 类型编解码（外围业务模块）。
//
// 运行 / Run:  go test -tags test ./internal/client/uss/ -v
// 覆盖：HTTPError/IsHTTPError/IsProfileNotFound、FlexTime/NullString/NullInt32/NullInt
//   的 Marshal/Unmarshal(零值/null/非法格式)。

package uss

import (
	"errors"
	"testing"
	"time"
)

// ── HTTPError ────────────────────────────────────────────────────────────────

func TestHTTPError_Error(t *testing.T) {
	e := HTTPError{body: "not found", code: 404}
	if e.Error() != "not found" {
		t.Errorf("Error(): got %q, want %q", e.Error(), "not found")
	}
}

func TestHTTPError_StatusCode(t *testing.T) {
	e := HTTPError{body: "bad request", code: 400}
	if e.StatusCode() != 400 {
		t.Errorf("StatusCode(): got %d, want 400", e.StatusCode())
	}
}

func TestIsHTTPError_Nil(t *testing.T) {
	if IsHTTPError(nil) {
		t.Error("IsHTTPError(nil) should return false")
	}
}

func TestIsHTTPError_ValueType(t *testing.T) {
	e := HTTPError{body: "err", code: 500}
	if !IsHTTPError(e) {
		t.Error("IsHTTPError(HTTPError value) should return true")
	}
}

func TestIsHTTPError_PointerType(t *testing.T) {
	e := &HTTPError{body: "err", code: 500}
	if !IsHTTPError(e) {
		t.Error("IsHTTPError(*HTTPError) should return true")
	}
}

func TestIsHTTPError_OtherError(t *testing.T) {
	if IsHTTPError(errors.New("generic")) {
		t.Error("IsHTTPError(generic error) should return false")
	}
}

func TestIsProfileNotFound_Nil(t *testing.T) {
	if IsProfileNotFound(nil) {
		t.Error("IsProfileNotFound(nil) should return false")
	}
}

func TestIsProfileNotFound_NotHTTPError(t *testing.T) {
	if IsProfileNotFound(errors.New("random error")) {
		t.Error("IsProfileNotFound(non-HTTP error) should return false")
	}
}

func TestIsProfileNotFound_EmptyBody(t *testing.T) {
	e := HTTPError{body: "", code: 404}
	if IsProfileNotFound(e) {
		t.Error("IsProfileNotFound(empty body) should return false")
	}
}

func TestIsProfileNotFound_ValueType_True(t *testing.T) {
	e := HTTPError{body: `{"errorCode":"uss-ae.profile.data_not_found"}`, code: 404}
	if !IsProfileNotFound(e) {
		t.Error("IsProfileNotFound should return true for uss-ae.profile.data_not_found body")
	}
}

func TestIsProfileNotFound_PointerType_True(t *testing.T) {
	e := &HTTPError{body: `{"errorCode":"profile.data_not_found"}`, code: 404}
	if !IsProfileNotFound(e) {
		t.Error("IsProfileNotFound should return true for profile.data_not_found body")
	}
}

func TestIsProfileNotFound_OtherBody(t *testing.T) {
	e := HTTPError{body: `{"errorCode":"some.other.error"}`, code: 404}
	if IsProfileNotFound(e) {
		t.Error("IsProfileNotFound should return false for unrelated error body")
	}
}

// ── FlexTime ─────────────────────────────────────────────────────────────────

func TestFlexTime_MarshalJSON_Zero(t *testing.T) {
	ft := FlexTime{}
	b, err := ft.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON zero: %v", err)
	}
	if string(b) != "null" {
		t.Errorf("zero FlexTime: got %s, want null", b)
	}
}

func TestFlexTime_MarshalJSON_NonZero(t *testing.T) {
	ft := FlexTime{Time: time.Date(2024, 3, 15, 10, 30, 0, 0, time.UTC)}
	b, err := ft.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON non-zero: %v", err)
	}
	want := `"2024-03-15 10:30:00"`
	if string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}
}

func TestFlexTime_UnmarshalJSON_InvalidFormat(t *testing.T) {
	var ft FlexTime
	if err := ft.UnmarshalJSON([]byte(`"not-a-date"`)); err == nil {
		t.Error("expected error for invalid date format")
	}
}

// ── NullString ───────────────────────────────────────────────────────────────

func TestNullString_MarshalJSON_Valid(t *testing.T) {
	ns := NullString{Val: "hello", Valid: true}
	b, err := ns.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != `"hello"` {
		t.Errorf("got %s, want \"hello\"", b)
	}
}

func TestNullString_MarshalJSON_Invalid(t *testing.T) {
	ns := NullString{Val: "", Valid: false}
	b, err := ns.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != "null" {
		t.Errorf("got %s, want null", b)
	}
}

func TestNullString_String_Valid(t *testing.T) {
	ns := NullString{Val: "world", Valid: true}
	if ns.String() != "world" {
		t.Errorf("String(): got %q, want %q", ns.String(), "world")
	}
}

func TestNullString_String_Invalid(t *testing.T) {
	ns := NullString{Val: "", Valid: false}
	if ns.String() != "" {
		t.Errorf("String() on invalid: got %q, want empty", ns.String())
	}
}

// ── NullInt32 ────────────────────────────────────────────────────────────────

func TestNullInt32_UnmarshalJSON_Null(t *testing.T) {
	var n NullInt32
	if err := n.UnmarshalJSON([]byte("null")); err != nil {
		t.Fatalf("UnmarshalJSON null: %v", err)
	}
	if n.Valid {
		t.Error("Valid should be false for null")
	}
	if n.Val != 0 {
		t.Errorf("Val should be 0, got %d", n.Val)
	}
}

func TestNullInt32_UnmarshalJSON_Number(t *testing.T) {
	var n NullInt32
	if err := n.UnmarshalJSON([]byte("42")); err != nil {
		t.Fatalf("UnmarshalJSON number: %v", err)
	}
	if !n.Valid {
		t.Error("Valid should be true for number")
	}
	if n.Val != 42 {
		t.Errorf("Val: got %d, want 42", n.Val)
	}
}

func TestNullInt32_UnmarshalJSON_Invalid(t *testing.T) {
	var n NullInt32
	if err := n.UnmarshalJSON([]byte(`"notanumber"`)); err == nil {
		t.Error("expected error for non-numeric string")
	}
}

func TestNullInt32_MarshalJSON_Valid(t *testing.T) {
	n := NullInt32{Val: 7, Valid: true}
	b, err := n.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != "7" {
		t.Errorf("got %s, want 7", b)
	}
}

func TestNullInt32_MarshalJSON_Invalid(t *testing.T) {
	n := NullInt32{Val: 0, Valid: false}
	b, err := n.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != "null" {
		t.Errorf("got %s, want null", b)
	}
}

// ── NullInt ──────────────────────────────────────────────────────────────────

func TestNullInt_UnmarshalJSON_Null(t *testing.T) {
	var n NullInt
	if err := n.UnmarshalJSON([]byte("null")); err != nil {
		t.Fatalf("UnmarshalJSON null: %v", err)
	}
	if n.Valid {
		t.Error("Valid should be false for null")
	}
}

func TestNullInt_UnmarshalJSON_Number(t *testing.T) {
	var n NullInt
	if err := n.UnmarshalJSON([]byte("9999")); err != nil {
		t.Fatalf("UnmarshalJSON number: %v", err)
	}
	if !n.Valid || n.Val != 9999 {
		t.Errorf("got Valid=%v Val=%d, want Valid=true Val=9999", n.Valid, n.Val)
	}
}

func TestNullInt_MarshalJSON_Valid(t *testing.T) {
	n := NullInt{Val: 123, Valid: true}
	b, err := n.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != "123" {
		t.Errorf("got %s, want 123", b)
	}
}

func TestNullInt_MarshalJSON_Invalid(t *testing.T) {
	n := NullInt{Val: 0, Valid: false}
	b, err := n.MarshalJSON()
	if err != nil {
		t.Fatalf("MarshalJSON: %v", err)
	}
	if string(b) != "null" {
		t.Errorf("got %s, want null", b)
	}
}
