package middleware

import (
	"github.com/gofiber/fiber/v3"

	"tcg-rulex-engine/internal/types/resp"
	"tcg-rulex-engine/pkg/logs"
)

// Recover catches panics, logs them as errors, and returns a 500 response.
func Recover() fiber.Handler {
	return func(c fiber.Ctx) (err error) {
		defer func() {
			if r := recover(); r != nil {
				logs.Err(c, "[PANIC] %s %s => %v", c.Method(), c.Path(), r)
				_ = c.Status(fiber.StatusInternalServerError).JSON(resp.BaseResponse{
					Success:   false,
					ErrorCode: "ucs-fe.non.internal_error",
					Message:   "Internal server error",
				})
			}
		}()
		return c.Next()
	}
}
