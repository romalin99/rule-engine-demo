package middleware

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"

	"tcg-rulex-engine/internal/apperror"
	"tcg-rulex-engine/internal/types/resp"
	"tcg-rulex-engine/pkg/logs"
)

// ignoreErrorCodes contains error codes that are logged at Info level instead of Warn.
var ignoreErrorCodes = map[string]struct{}{
	apperror.SysErr.String(): {},
}

func isIgnoreErrorCode(code string) bool {
	_, ok := ignoreErrorCodes[code]
	return ok
}

func ErrorHandler(c fiber.Ctx, err error) error {
	switch e := err.(type) {

	case *apperror.AppError:
		logs.Err(c.Context(), "%v module=%s cause=%v", e.ErrorCode, e.Module, e.Cause)

		errorCode := "ucs-fe." + e.Module.String() + "." + strings.ToLower(e.ErrorCode.String())
		return c.Status(http.StatusInternalServerError).JSON(resp.BaseResponse{
			Success:   false,
			ErrorCode: errorCode,
			Message:   e.Message,
		})

	case *apperror.HTTPError:
		if isIgnoreErrorCode(e.ErrorCode) {
			logs.Info(c, "%v", e.Message)
		} else {
			logs.Warn(c, "%v", e.Message)
		}

		return c.Status(http.StatusInternalServerError).JSON(resp.BaseResponse{
			Success:   false,
			ErrorCode: e.ErrorCode,
			Message:   e.Message,
		})

	default:
		message := "unknown"
		if err != nil {
			message = err.Error()
		}
		return c.Status(http.StatusInternalServerError).JSON(resp.BaseResponse{
			Success:   false,
			ErrorCode: "ucs-fe.non.unknown_err",
			Message:   message,
		})
	}
}
