package engine

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/example/rule-engine-demo/pkg/model"
	"github.com/gofiber/fiber/v3"
)

// Server exposes the engine over HTTP for real-time, online scoring: send one
// user (or a batch) and get back the rules they matched. The rule cache is
// shared, so requests never re-parse. The transport is built on Fiber v3.
type Server struct {
	eng *Engine
}

// NewServer wraps an engine in a Fiber-backed HTTP handler.
func NewServer(eng *Engine) *Server { return &Server{eng: eng} }

// matchResponse is returned by /match for a single user.
type matchResponse struct {
	UID     int64    `json:"uid"`
	Hits    int      `json:"hits"`
	RuleIDs []int64  `json:"rule_ids"`
	Names   []string `json:"rule_names"`
	TookMs  float64  `json:"took_ms"`
}

// App builds a fresh Fiber app with all routes registered.
func (s *Server) App() *fiber.App {
	app := fiber.New()
	app.Get("/healthz", s.handleHealth)
	app.Get("/rules", s.handleRules)
	app.Post("/match", s.handleMatch)        // single user
	app.Post("/match/batch", s.handleBatch)  // array of users
	return app
}

func (s *Server) handleHealth(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"ok": true, "rules": s.eng.RuleCount()})
}

func (s *Server) handleRules(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"rules": s.eng.RuleCount()})
}

func (s *Server) handleMatch(c fiber.Ctx) error {
	var u model.User
	if err := c.Bind().Body(&u); err != nil {
		return c.Status(fiber.StatusBadRequest).
			JSON(fiber.Map{"error": "bad user json: " + err.Error()})
	}
	return c.JSON(s.scoreOne(u))
}

func (s *Server) handleBatch(c fiber.Ctx) error {
	// The batch body is a top-level JSON array, so decode it directly (Fiber's
	// struct binder targets objects); each element uses model.User.UnmarshalJSON.
	var users []model.User
	if err := json.Unmarshal(c.Body(), &users); err != nil {
		return c.Status(fiber.StatusBadRequest).
			JSON(fiber.Map{"error": "bad users json: " + err.Error()})
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

// Serve loads rules into a new engine and serves HTTP (Fiber v3) until interrupted.
func Serve(addr string, eng *Engine) error {
	srv := NewServer(eng)
	fmt.Printf("rule-engine serving on %s (rules=%d)\n", addr, eng.RuleCount())
	fmt.Println("  POST /match        single user  -> matched rules")
	fmt.Println("  POST /match/batch  []user       -> matched rules")
	fmt.Println("  GET  /rules        rule count")
	return srv.App().Listen(addr)
}
