package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	recoverer "github.com/gofiber/fiber/v3/middleware/recover"

	"github.com/example/rule-engine-demo/model"
)

// Server exposes the engine over HTTP for real-time, online scoring: send one
// user (or a batch) and get back the rules they matched. The rule cache is
// shared, so requests never re-parse.
//
// The transport is built on Fiber v3 (github.com/gofiber/fiber/v3), which runs
// on fasthttp for lower per-request allocation than net/http.
type Server struct {
	eng   *Engine
	store *RuleStore // optional: when set, the /admin operations console is mounted
}

// NewServer wraps an engine in a Fiber-backed handler.
func NewServer(eng *Engine) *Server { return &Server{eng: eng} }

// NewServerWithAdmin is like NewServer but also mounts the operations console
// (rule editor, test, publish, versioning, rollback) backed by store.
func NewServerWithAdmin(eng *Engine, store *RuleStore) *Server {
	return &Server{eng: eng, store: store}
}

// matchResponse is returned by /match for a single user.
type matchResponse struct {
	UID     int64    `json:"uid"`
	Hits    int      `json:"hits"`
	RuleIDs []int64  `json:"rule_ids"`
	Names   []string `json:"rule_names"`
	TookMs  float64  `json:"took_ms"`
}

// App builds a configured *fiber.App with all routes and middleware registered.
// It is exported so callers (and tests via app.Test) can use the handler
// without binding to a network address.
func (s *Server) App() *fiber.App {
	app := fiber.New(fiber.Config{
		AppName:      "rule-engine-demo",
		ErrorHandler: jsonErrorHandler,
	})

	app.Use(recoverer.New()) // turn panics into 500 JSON instead of killing the worker
	app.Use(logger.New())    // per-request access log

	app.Get("/healthz", s.handleHealth)
	app.Get("/rules", s.handleRules)
	app.Post("/match", s.handleMatch)       // single user
	app.Post("/match/batch", s.handleBatch) // array of users

	if s.store != nil {
		registerAdmin(app, s.store) // /admin operations console + /admin/api/*
	}
	return app
}

// jsonErrorHandler renders every error (including *fiber.Error) as a JSON body
// so clients always get a consistent {"error": ...} shape.
func jsonErrorHandler(c fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	var fe *fiber.Error
	if errors.As(err, &fe) {
		code = fe.Code
	}
	return c.Status(code).JSON(fiber.Map{"error": err.Error()})
}

func (s *Server) handleHealth(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"ok": true, "rules": s.eng.RuleCount()})
}

func (s *Server) handleRules(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"rules": s.eng.RuleCount()})
}

func (s *Server) handleMatch(c fiber.Ctx) error {
	// model.User has a custom UnmarshalJSON (flat Kafka-style object), so decode
	// the raw body directly rather than via BodyParser to preserve that logic.
	var u model.User
	if err := json.Unmarshal(c.Body(), &u); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "bad user json: "+err.Error())
	}
	return c.JSON(s.scoreOne(u))
}

func (s *Server) handleBatch(c fiber.Ctx) error {
	var users []model.User
	if err := json.Unmarshal(c.Body(), &users); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "bad users json: "+err.Error())
	}
	out := make([]matchResponse, len(users))
	for i := range users {
		if users[i].UID == 0 {
			users[i].UID = int64(i + 1)
		}
		out[i] = s.scoreOne(users[i])
	}
	return c.JSON(out)
}

func (s *Server) scoreOne(u model.User) matchResponse {
	t0 := time.Now()
	ids := s.eng.Match(u)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if p, ok := s.eng.Cache().Get(id); ok {
			names = append(names, p.Name)
		}
	}
	return matchResponse{
		UID:     u.UID,
		Hits:    len(ids),
		RuleIDs: ids,
		Names:   names,
		TookMs:  float64(time.Since(t0).Microseconds()) / 1000.0,
	}
}

// Serve loads rules into a new engine and serves HTTP until interrupted.
func Serve(addr string, eng *Engine) error {
	return ServeWithStore(addr, eng, nil)
}

// ServeWithStore serves the scoring API and, when store is non-nil, the /admin
// operations console (rule editor, test, publish, versioning, rollback).
func ServeWithStore(addr string, eng *Engine, store *RuleStore) error {
	srv := NewServerWithAdmin(eng, store)
	fmt.Printf("rule-engine serving on %s (rules=%d)\n", addr, eng.RuleCount())
	fmt.Println("  POST /match        single user  -> matched rules")
	fmt.Println("  POST /match/batch  []user       -> matched rules")
	fmt.Println("  GET  /rules        rule count")
	if store != nil {
		fmt.Printf("  GET  /admin        operations console (test/publish/rollback)\n")
	}
	return srv.App().Listen(addr)
}
