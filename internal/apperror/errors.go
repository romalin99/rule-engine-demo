package apperror

import (
	"fmt"
	"strings"
)

// AppError is the internal domain error carrying Module, ErrorCode, and an
// optional root cause. It implements the error interface and supports
// errors.Is / errors.As via Unwrap.
type AppError struct {
	Cause     error     `json:"-"`
	Module    Module    `json:"module"`
	ErrorCode ErrorCode `json:"errorCode"`
	Message   string    `json:"message"`
}

// New constructs an AppError with module, code, description, and root cause.
func New(module Module, code ErrorCode, desc string, cause error) *AppError {
	return &AppError{Module: module, ErrorCode: code, Message: buildMessage(desc), Cause: cause}
}

// Newf constructs an AppError with a formatted description.
func Newf(module Module, code ErrorCode, cause error, format string, args ...any) *AppError {
	return New(module, code, fmt.Sprintf(format, args...), cause)
}

// NewSimple constructs an AppError without a module or root cause.
func NewSimple(code ErrorCode, desc string) *AppError {
	return New(ModuleNone, code, desc, nil)
}

// Wrap wraps an existing error into an AppError, preserving the original
// message as the description.
func Wrap(code ErrorCode, cause error) *AppError {
	return &AppError{Module: ModuleNone, ErrorCode: code, Message: cause.Error(), Cause: cause}
}

func (e *AppError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s:%s %s (%v)", e.Module, e.ErrorCode, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s:%s %s", e.Module, e.ErrorCode, e.Message)
}

// Unwrap enables errors.Is / errors.As chain traversal.
func (e *AppError) Unwrap() error { return e.Cause }

// Is reports whether target is an *AppError carrying the same ErrorCode.
// This lets handlers compare sentinel AppErrors via errors.Is without
// requiring pointer identity — any AppError with a matching ErrorCode
// (including freshly constructed ones via NewSimple/New) will match.
// ModuleNone on the target acts as a wildcard for the module dimension.
func (e *AppError) Is(target error) bool {
	t, ok := target.(*AppError)
	if !ok {
		return false
	}
	if e.ErrorCode != t.ErrorCode {
		return false
	}
	if t.Module == ModuleNone {
		return true
	}
	return e.Module == t.Module
}

// buildMessage returns a clean human-readable description.
// ErrorCode is intentionally excluded here; it appears in Error() for logging.
func buildMessage(desc string) string {
	if strings.TrimSpace(desc) == "" {
		return "unknown error"
	}
	return desc
}

// Sentinel *AppError values returned by service-layer flows (player
// verification, help-center / service-terms). Each carries a typed
// ErrorCode so handlers can map them to HTTP status codes via errors.Is
// (matching is delegated to AppError.Is, which compares by ErrorCode).
//
// Service code wraps these with fmt.Errorf("%w: extra context", ErrXxx)
// when extra context is useful; the unwrap chain preserves the sentinel,
// keeping errors.Is matching intact.
var (
	// ErrMerchantNotFound — merchant rule lookup returned no row.
	ErrMerchantNotFound = NewSimple(MerchantNotFound, "merchant not found")

	// ErrUssCustomerFetchFailed — USS GetCustomer call failed or returned nil.
	ErrUssCustomerFetchFailed = NewSimple(UssCustomerFetchFailed, "uss customer fetch failed")

	// ErrUssPlayerCountOfMerchantFailed — USS GetRegisteredPlayerCount failed
	// during InitAgreeTerms.
	ErrUssPlayerCountOfMerchantFailed = NewSimple(UssPlayerCountOfMerchantFailed, "uss player count of merchant failed")

	// ErrUssCustomerFetchPersonalInfoFailed — USS GetCustomerPersonalInfo failed.
	ErrUssCustomerFetchPersonalInfoFailed = NewSimple(UssCustomerFetchPersonalInfoFailed, "uss customer fetch personal information failed")

	// ErrUssPasswordResetFailed — USS GeneratePasswordResetToken failed or returned success=false.
	ErrUssPasswordResetFailed = NewSimple(UssPasswordResetFailed, "uss password reset failed")

	// ErrMcsVerifyPlayerInfoFailed — MCS VerifyPlayerInfo upstream call failed.
	ErrMcsVerifyPlayerInfoFailed = NewSimple(McsVerifyPlayerInfoFailed, "mcs verify player info failed")

	// ErrParseJSONFailed — merchant rule QUESTIONS CLOB or other JSON parse failed.
	ErrParseJSONFailed = NewSimple(ParseJSONFailed, "parse json failed")

	// ErrQuestionLimitExceeded — IP_RETRY_LIMIT or ACCOUNT_RETRY_LIMIT exhausted for today.
	ErrQuestionLimitExceeded = NewSimple(QuestionLimitExceeded, "question limit exceeded")

	// ErrRedisNotFound — required Redis instance unavailable.
	ErrRedisNotFound = NewSimple(RedisNotFound, "redis instance not found")

	// ErrWpsEmailSmsFailed — WPS GetResetPasswordStatus failed.
	ErrWpsEmailSmsFailed = NewSimple(WpsEmailSmsFailed, "wps email sms api failed")

	// ErrPhoneAlreadyBound — phone is already bound (business-normal state,
	// handler returns 200 + success:true).
	ErrPhoneAlreadyBound = NewSimple(PhoneAlreadyBound, "phone already bound")

	// ErrEmailAlreadyBound — email is already bound (business-normal state,
	// handler returns 200 + success:true).
	ErrEmailAlreadyBound = NewSimple(EmailAlreadyBound, "email already bound")

	// ErrHelpCenterRequestParam — help-center request payload invalid
	// (missing merchant/customer, malformed agreement items, etc.).
	// Mapped by the handler to a 200 business-error response.
	ErrHelpCenterRequestParam = NewSimple(HelpCenterRequestParam, "help-center request param invalid")

	// ErrHelpCenterPersistFailed — persisting SERVICE_TERMS_VERSION /
	// INIT_AGREE_TERMS failed due to a DB or infrastructure error.
	// Mapped by the handler to HTTP 500.
	ErrHelpCenterPersistFailed = NewSimple(HelpCenterPersistFailed, "help-center persist failed")

	// ErrTimeUsageRequestParam — time-usage request param invalid
	// (missing/invalid customerID or merchantCode, or USS ownership mismatch:
	// the merchant returned by USS GetCustomerByID does not match the request).
	// Mapped by the handler to a 200 business-error response.
	ErrTimeUsageRequestParam = NewSimple(TimeUsageRequestParam, "time-usage request param invalid")

	// ErrTimeUsageQueryFailed — querying TIMELIMIT_USAGE_ACCUMULATION failed due
	// to a DB or infrastructure error. Mapped by the handler to HTTP 500.
	ErrTimeUsageQueryFailed = NewSimple(TimeUsageQueryFailed, "time-usage query failed")
)

// HTTPError is the HTTP response-layer error. It implements the error interface
// and is returned by handlers to produce a structured JSON error body.
type HTTPError struct {
	ErrorCode string `json:"errorCode"`
	Message   string `json:"message"`
	Success   bool   `json:"success"`
}

// NewHTTPError constructs an HTTPError from raw string code and message.
func NewHTTPError(errorCode, message string) *HTTPError {
	return &HTTPError{Success: false, ErrorCode: errorCode, Message: message}
}

// NewHTTPErrorFromCode constructs an HTTPError from a typed ErrorCode,
// avoiding manual string conversion at the call site.
func NewHTTPErrorFromCode(code ErrorCode, message string) *HTTPError {
	return &HTTPError{Success: false, ErrorCode: string(code), Message: message}
}

func (e *HTTPError) Error() string { return e.Message }
