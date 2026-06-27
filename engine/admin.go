package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/gofiber/fiber/v3"

	"github.com/example/rule-engine-demo/model"
)

// admin.go implements the operations platform (roadmap step 11): a small web
// console for editing, testing, publishing, versioning and rolling back rules,
// backed by an in-memory versioned store that hot-reloads the live engine.
//
//	Web UI ─▶ /admin/api/test     compile+eval one rule against a sample user
//	       ─▶ /admin/api/publish  new version  → Engine.ReplaceRules (hot reload)
//	       ─▶ /admin/api/versions list history
//	       ─▶ /admin/api/rollback re-activate an older version (hot reload)
//
// The store keeps full history so any prior version can be re-published.

// RuleVersion is one immutable published snapshot of the whole rule set.
type RuleVersion struct {
	Version   int          `json:"version"`
	Note      string       `json:"note"`
	CreatedAt time.Time    `json:"created_at"`
	Active    bool         `json:"active"`
	Rules     []model.Rule `json:"rules"`
}

// RuleStore is a thread-safe, versioned rule store wired to a live Engine.
// Publishing or rolling back atomically swaps the engine's rule cache, so online
// scoring never stops and users never re-parse.
type RuleStore struct {
	mu       sync.RWMutex
	eng      *Engine
	versions []*RuleVersion
	active   int // index into versions of the currently active snapshot
	nextVer  int
}

// NewRuleStore creates a store seeded with an initial rule set (published as
// version 1) and loads it into the engine.
func NewRuleStore(eng *Engine, initial []model.Rule) *RuleStore {
	s := &RuleStore{eng: eng, nextVer: 1}
	_, _, _ = s.Publish(initial, "initial")
	return s
}

// Publish stores rules as a new active version and hot-reloads the engine.
func (s *RuleStore) Publish(rules []model.Rule, note string) (version, loaded, failed int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	loaded, failed = s.eng.ReplaceRules(rules)
	for _, v := range s.versions {
		v.Active = false
	}
	ver := &RuleVersion{
		Version:   s.nextVer,
		Note:      note,
		CreatedAt: time.Now(),
		Active:    true,
		Rules:     append([]model.Rule(nil), rules...),
	}
	s.versions = append(s.versions, ver)
	s.active = len(s.versions) - 1
	s.nextVer++
	return ver.Version, loaded, failed
}

// Rollback re-publishes the rules of an existing version as a NEW version, so
// history stays linear and auditable. Returns an error if the version is unknown.
func (s *RuleStore) Rollback(version int) (newVersion, loaded, failed int, err error) {
	s.mu.RLock()
	var target *RuleVersion
	for _, v := range s.versions {
		if v.Version == version {
			target = v
			break
		}
	}
	s.mu.RUnlock()
	if target == nil {
		return 0, 0, 0, fmt.Errorf("version %d not found", version)
	}
	rules := append([]model.Rule(nil), target.Rules...)
	nv, l, f := s.Publish(rules, fmt.Sprintf("rollback to v%d", version))
	return nv, l, f, nil
}

// Current returns the active version (nil if the store is empty).
func (s *RuleStore) Current() *RuleVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.versions) == 0 {
		return nil
	}
	return s.versions[s.active]
}

// Versions returns the version history, newest first.
func (s *RuleStore) Versions() []*RuleVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := append([]*RuleVersion(nil), s.versions...)
	sort.Slice(out, func(i, j int) bool { return out[i].Version > out[j].Version })
	return out
}

// TestExpr compiles a single rule expression with the active backend and
// evaluates it against a sample user's fields — without publishing anything.
func (e *Engine) TestExpr(expr string, fields map[string]any) (bool, error) {
	ast, err := e.backend.Compile(expr)
	if err != nil {
		return false, err
	}
	return e.backend.Eval(e.backend.NewContext(fields), ast), nil
}

// registerAdmin mounts the operations console on the Fiber app.
func registerAdmin(app *fiber.App, store *RuleStore) {
	app.Get("/admin", func(c fiber.Ctx) error {
		return c.Type("html").SendString(adminHTML)
	})

	app.Get("/admin/api/rules", func(c fiber.Ctx) error {
		cur := store.Current()
		if cur == nil {
			return c.JSON(fiber.Map{"version": 0, "rules": []model.Rule{}})
		}
		return c.JSON(fiber.Map{"version": cur.Version, "rules": cur.Rules})
	})

	app.Get("/admin/api/versions", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{"versions": store.Versions()})
	})

	app.Post("/admin/api/test", func(c fiber.Ctx) error {
		var req struct {
			Expr   string         `json:"expr"`
			Fields map[string]any `json:"fields"`
		}
		if err := json.Unmarshal(c.Body(), &req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "bad json: "+err.Error())
		}
		matched, err := store.eng.TestExpr(req.Expr, req.Fields)
		if err != nil {
			return c.JSON(fiber.Map{"ok": false, "error": err.Error()})
		}
		return c.JSON(fiber.Map{"ok": true, "matched": matched})
	})

	app.Post("/admin/api/publish", func(c fiber.Ctx) error {
		var req struct {
			Rules []model.Rule `json:"rules"`
			Note  string       `json:"note"`
		}
		if err := json.Unmarshal(c.Body(), &req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "bad json: "+err.Error())
		}
		ver, loaded, failed := store.Publish(req.Rules, req.Note)
		return c.JSON(fiber.Map{"version": ver, "loaded": loaded, "failed": failed})
	})

	app.Post("/admin/api/rollback", func(c fiber.Ctx) error {
		var req struct {
			Version int `json:"version"`
		}
		if err := json.Unmarshal(c.Body(), &req); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "bad json: "+err.Error())
		}
		nv, loaded, failed, err := store.Rollback(req.Version)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return c.JSON(fiber.Map{"version": nv, "loaded": loaded, "failed": failed})
	})
}
