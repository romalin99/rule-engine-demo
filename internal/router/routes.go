// Package router wires the layered rule-engine HTTP API (handler → service →
// infra → engine) onto Fiber v3. Every request is served under a single
// base-path Group (mirroring tcg-rulex-engine); NewApp/Serve assemble the app for the
// CLI server. The engine knows nothing about transport/routing.
package router

import (
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/limiter"

	"tcg-rulex-engine/internal/handler"
	"tcg-rulex-engine/internal/infra"
	"tcg-rulex-engine/internal/service"
	"tcg-rulex-engine/pkg/engine"
)

// BasePath is the API group prefix, mirroring tcg-rulex-engine's "/tcg-rulex-engine".
// ALL endpoints — including the liveness/observability probes — live under this
// group, so the group sits at the root of every request.
const BasePath = "/tcg-rulex-engine"

// RegisterHandlers mounts the complete rule-engine ops API under the BasePath
// group (rate-limited):
//   - probes / scrape: /healthz, /ping, /metrics
//   - scoring:         /match, /match/batch, /evaluate, /evaluate/all, /rules
//   - operations:      web editor (/), rules CRUD, draft test, self-test, versions
func RegisterHandlers(app *fiber.App, h *handler.RuleHandler) {
	// Group-level global rate limiter (≤800 req/s across the whole group),
	// mirroring tcg-rulex-engine's routes.go.
	groupLimiter := limiter.New(limiter.Config{
		Max:          800,
		Expiration:   time.Second,
		KeyGenerator: func(fiber.Ctx) string { return "global" },
	})
	// Per-endpoint limiter (keyed by path) for read-heavy GET endpoints.
	readLimiter := limiter.New(limiter.Config{
		Max:          500,
		Expiration:   time.Second,
		KeyGenerator: func(c fiber.Ctx) string { return c.Path() },
	})

	// The group is the root of every request.
	api := app.Group(BasePath, groupLimiter)

	// probes / observability
	api.Get("/healthz", h.Health)
	api.Get("/ping", h.Ping)
	api.Get("/metrics", h.Metrics)

	// scoring
	api.Post("/match", h.Match)
	api.Post("/match/batch", h.MatchBatch)
	api.Post("/evaluate", h.Evaluate)
	api.Post("/evaluate/all", h.EvaluateAll)
	api.Get("/rules", readLimiter, h.RuleCount)

	// operations console
	api.Get("/", h.Editor)
	api.Get("/rules/list", readLimiter, h.RuleList)
	api.Post("/rules", h.RuleUpsert)
	api.Delete("/rules/:id", h.RuleDelete)
	api.Post("/rules/test", h.RuleTest)
	api.Post("/rules/selftest", h.SelfTest)
	api.Get("/versions", readLimiter, h.VersionList)
	api.Post("/versions", h.Publish)
	api.Post("/versions/:v/rollback", h.Rollback)
}

// BuildHandler assembles the layered handler from an engine
// (infra → service → handler). Used by NewApp and tests.
func BuildHandler(eng *engine.Engine) *handler.RuleHandler {
	com := infra.NewComManagerWithEngine(eng)
	svc := service.NewRuleService(com)
	return handler.NewRuleHandler(svc)
}

// NewApp builds a Fiber app with the full layered API mounted (no global
// middleware / graceful shutdown — those are added by the cmd/api bootstrap).
// Used by Serve and HTTP tests.
func NewApp(eng *engine.Engine) *fiber.App {
	app := fiber.New()
	RegisterHandlers(app, BuildHandler(eng))
	return app
}

// Serve builds the layered app, mounts Swagger, and serves until interrupted.
// Kept for the CLI (`go run ./cmd/cli -serve`); the production entry point with
// the full ucs-fe-style bootstrap (middleware, pprof, graceful shutdown) is cmd/api.
func Serve(addr string, eng *engine.Engine) error {
	app := NewApp(eng)
	RegisterSwagger(app, portFromAddr(addr))

	b := BasePath
	fmt.Printf("rule-engine serving on %s (rules=%d)\n", addr, eng.RuleCount())
	fmt.Printf("  all endpoints are under %s :\n", b)
	fmt.Printf("  GET  %s/            web rule editor (operations console)\n", b)
	fmt.Printf("  POST %s/match  %s/match/batch  %s/evaluate  %s/evaluate/all\n", b, b, b, b)
	fmt.Printf("  GET  %s/rules  %s/rules/list   POST %s/rules   DELETE %s/rules/:id\n", b, b, b, b)
	fmt.Printf("  POST %s/rules/test  %s/rules/selftest\n", b, b)
	fmt.Printf("  GET  %s/versions   POST %s/versions   POST %s/versions/:v/rollback\n", b, b, b)
	fmt.Printf("  GET  %s/ping  %s/healthz  %s/metrics\n", b, b, b)
	fmt.Println("  GET  /swagger/*     (Swagger UI at root; spec BasePath = " + b + ")")
	return app.Listen(addr)
}
