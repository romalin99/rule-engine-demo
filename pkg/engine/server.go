package engine

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/example/rule-engine-demo/pkg/model"
	"github.com/example/rule-engine-demo/pkg/web"
	"github.com/gofiber/fiber/v3"
)

// Server exposes the engine over HTTP for real-time, online scoring: send one
// user (or a batch) and get back the rules they matched. The rule cache is
// shared, so requests never re-parse. The transport is built on Fiber v3.
//
// It also hosts the operations console (step 11): a Manager-backed rule API
// (add / remove / list / test / publish / rollback) and a web editor at "/".
type Server struct {
	eng *Engine
	mgr *Manager
	sem *Semaphore // caps concurrent users in rule judgment (Phase 5: ≤200)
}

// NewServer wraps an engine in a Fiber-backed HTTP handler. Online scoring is
// bounded to MaxConcurrentUsers concurrent requests.
func NewServer(eng *Engine) *Server {
	return &Server{eng: eng, mgr: NewManager(eng), sem: NewSemaphore(MaxConcurrentUsers)}
}

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
	// Scoring
	app.Get("/healthz", s.handleHealth)
	app.Get("/rules", s.handleRules)         // rule count
	app.Get("/metrics", s.handleMetrics)     // Prometheus text exposition
	app.Post("/match", s.handleMatch)        // single user
	app.Post("/match/batch", s.handleBatch)  // array of users
	// Operations console (step 11)
	app.Get("/", s.handleEditor)             // web rule editor
	app.Get("/rules/list", s.handleRuleList) // list editable rules
	app.Post("/rules", s.handleRuleUpsert)   // add/update a rule (live)
	app.Delete("/rules/:id", s.handleRuleDelete)
	app.Post("/rules/test", s.handleRuleTest)        // test draft vs sample row
	app.Get("/versions", s.handleVersionList)        // published snapshots
	app.Post("/versions", s.handlePublish)           // snapshot current set
	app.Post("/versions/:v/rollback", s.handleRollback)
	return app
}

// --- Operations console handlers (step 11) ------------------------------------

func (s *Server) handleEditor(c fiber.Ctx) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(web.Page)
}

func (s *Server) handleRuleList(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"rules": s.mgr.List()})
}

func (s *Server) handleRuleUpsert(c fiber.Ctx) error {
	var r model.Rule
	if err := c.Bind().Body(&r); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad rule json: " + err.Error()})
	}
	if err := s.mgr.Upsert(r); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "id": r.ID, "rules": s.eng.RuleCount()})
}

func (s *Server) handleRuleDelete(c fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad id"})
	}
	s.mgr.Remove(id)
	return c.JSON(fiber.Map{"ok": true, "id": id, "rules": s.eng.RuleCount()})
}

func (s *Server) handleRuleTest(c fiber.Ctx) error {
	var req struct {
		Rule string         `json:"rule"`
		Row  map[string]any `json:"row"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad json: " + err.Error()})
	}
	matched, err := s.mgr.Test(req.Rule, req.Row)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"matched": matched})
}

func (s *Server) handleVersionList(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"versions": s.mgr.Versions()})
}

func (s *Server) handlePublish(c fiber.Ctx) error {
	var req struct {
		Note string `json:"note"`
	}
	_ = c.Bind().Body(&req)
	v := s.mgr.Publish(req.Note)
	return c.JSON(fiber.Map{"version": v.Version, "count": v.Count})
}

func (s *Server) handleRollback(c fiber.Ctx) error {
	v, err := strconv.Atoi(c.Params("v"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad version"})
	}
	if err := s.mgr.Rollback(v); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "version": v, "rules": s.eng.RuleCount()})
}

func (s *Server) handleHealth(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"ok": true, "rules": s.eng.RuleCount()})
}

func (s *Server) handleRules(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"rules": s.eng.RuleCount()})
}

// handleMetrics exposes engine metrics in Prometheus text exposition format
// (dependency-free) so a Prometheus server can scrape /metrics directly.
func (s *Server) handleMetrics(c fiber.Ctx) error {
	var b strings.Builder
	b.WriteString("# HELP rule_engine_rules Number of active compiled rules.\n")
	b.WriteString("# TYPE rule_engine_rules gauge\n")
	fmt.Fprintf(&b, "rule_engine_rules %d\n", s.eng.RuleCount())
	b.WriteString("# HELP rule_engine_versions Number of published rule-set versions.\n")
	b.WriteString("# TYPE rule_engine_versions gauge\n")
	fmt.Fprintf(&b, "rule_engine_versions %d\n", len(s.mgr.Versions()))
	b.WriteString("# HELP rule_engine_inflight Users currently being judged.\n")
	b.WriteString("# TYPE rule_engine_inflight gauge\n")
	fmt.Fprintf(&b, "rule_engine_inflight %d\n", s.sem.InFlight())
	b.WriteString("# HELP rule_engine_max_concurrency Max concurrent users (Phase 5 cap).\n")
	b.WriteString("# TYPE rule_engine_max_concurrency gauge\n")
	fmt.Fprintf(&b, "rule_engine_max_concurrency %d\n", s.sem.Cap())
	c.Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	return c.SendString(b.String())
}

func (s *Server) handleMatch(c fiber.Ctx) error {
	var u model.User
	if err := c.Bind().Body(&u); err != nil {
		return c.Status(fiber.StatusBadRequest).
			JSON(fiber.Map{"error": "bad user json: " + err.Error()})
	}
	// Phase 5: at most MaxConcurrentUsers users judged concurrently; excess queues.
	s.sem.Acquire()
	defer s.sem.Release()
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
	// Phase 5: bound concurrency to MaxConcurrentUsers across the batch.
	s.sem.Acquire()
	defer s.sem.Release()
	results := s.eng.MatchBatchBounded(users)
	out := make([]matchResponse, len(users))
	for i := range users {
		uid := users[i].UID
		if uid == 0 {
			uid = int64(i + 1)
		}
		ids := results[i].RuleIDs
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			if p, ok := s.eng.Cache().Get(id); ok {
				names = append(names, p.Name)
			}
		}
		out[i] = matchResponse{UID: uid, Hits: len(ids), RuleIDs: ids, Names: names}
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
	fmt.Println("  GET  /              web rule editor (operations console)")
	fmt.Println("  POST /match        single user  -> matched rules")
	fmt.Println("  POST /match/batch  []user       -> matched rules")
	fmt.Println("  GET  /rules/list   list rules    POST /rules add/update    DELETE /rules/:id")
	fmt.Println("  POST /rules/test   test a draft rule against a sample row")
	fmt.Println("  POST /versions     publish snapshot   POST /versions/:v/rollback")
	return srv.App().Listen(addr)
}
