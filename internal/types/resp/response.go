package resp

import (
	"tcg-rulex-engine/internal/model"
)

type (
	EmptyResp   struct{}
	SuccessResp struct {
		Success bool `json:"success"`
	}
)

type ApiBaseMessageResp struct {
	Value     any    `json:"value,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
	Message   string `json:"message,omitempty"`
	Success   bool   `json:"success"`
}

type LoginWithInfoResp struct {
	Message string `json:"message,omitempty"`
	Payload []byte `json:"payload,omitempty"`
	Success bool   `json:"success"`
}

type BaseResponse struct {
	Value     any    `json:"value,omitempty"`
	Message   string `json:"message,omitempty"`
	ErrorCode string `json:"errorCode,omitempty"`
	Success   bool   `json:"success"`
}

// BaseResponseT represents a generic response with data.
type BaseResponseT[T any] struct {
	Value   T    `json:"value"`
	Success bool `json:"success"`
}

type MerchantRuleResponse struct {
	MerchantCode string               `json:"merchantCode" example:"gi8viet2"`
	Questions    []model.QuestionInfo `json:"questions"`
}

// CommonResponse is the unified response structure.
type CommonResponse struct {
	Data    any    `json:"data"`
	Message string `json:"message" example:"success"`
	Code    int    `json:"code"    example:"0"`
}

// ErrResponse is the unified error response structure.
type ErrResponse struct {
	ErrorCode string `json:"errorCode"`
	Message   string `json:"message"`
	Success   bool   `json:"success" example:"false"`
}

// NewErrResponse constructs a unified error response.
func NewErrResponse(errorCode, message string) ErrResponse {
	return ErrResponse{
		Success:   false,
		ErrorCode: errorCode,
		Message:   message,
	}
}

func Success(data any) *BaseResponseT[*CommonResponse] {
	return &BaseResponseT[*CommonResponse]{
		Success: true,
		Value: &CommonResponse{
			Code:    0,
			Message: "success",
			Data:    data,
		},
	}
}

func Fail(code int, msg string) *BaseResponseT[*CommonResponse] {
	return &BaseResponseT[*CommonResponse]{
		Success: false,
		Value: &CommonResponse{
			Code:    code,
			Message: msg,
			Data:    nil,
		},
	}
}

// ErrMissingParam is the 400 missing parameter response.
type ErrMissingParam struct {
	Code    string `json:"code"    example:"MISSING_PARAM"`
	Message string `json:"message" example:"merchantCode is required"`
}

// ErrNotFound is the 404 resource not found response.
type ErrNotFound struct {
	Code    string `json:"code"    example:"NOT_FOUND"`
	Message string `json:"message" example:"Merchant rule not found"`
}

// ErrInternalServer is the 500 internal server error response.
type ErrInternalServer struct {
	Code    string `json:"code"    example:"INTERNAL_ERROR"`
	Message string `json:"message" example:"Internal server error"`
}

func NewErrInternalServer(msg string) ErrInternalServer {
	return ErrInternalServer{
		Code:    "INTERNAL_ERROR",
		Message: msg,
	}
}

func NewErrMissingParam(field string) ErrMissingParam {
	return ErrMissingParam{
		Code:    "MISSING_PARAM",
		Message: field + " is required",
	}
}

func NewErrNotFound(resource string) ErrNotFound {
	return ErrNotFound{
		Code:    "NOT_FOUND",
		Message: resource + " not found",
	}
}
