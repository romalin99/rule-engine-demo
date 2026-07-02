package engine

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"tcg-rulex-engine/pkg/model"
)

// Manager is the operations layer (step 11): it holds the authoritative,
// editable rule set, applies live edits to the engine's hot cache, can test a
// draft rule against a sample row, and keeps published snapshots for rollback.
//
// Edits take effect immediately (the engine recompiles the single changed rule
// and atomically rebuilds its snapshot); no restart, no file reload required.
type Manager struct {
	eng      *Engine
	rules    map[int64]model.Rule
	versions []Version
	mu       sync.Mutex
}

// Version is a published, restorable snapshot of the entire rule set.
type Version struct {
	Time    time.Time    `json:"time"`
	Note    string       `json:"note"`
	Rules   []model.Rule `json:"rules"`
	Version int          `json:"version"`
	Count   int          `json:"count"`
}

// NewManager wraps an engine and seeds the editable set from its current cache.
func NewManager(eng *Engine) *Manager {
	m := &Manager{eng: eng, rules: make(map[int64]model.Rule)}
	for _, p := range eng.Cache().Snapshot() {
		m.rules[p.ID] = model.Rule{
			ID: p.ID, Name: p.Name, Expr: p.Source, Priority: p.Priority, Version: p.Version, Enabled: true,
		}
	}
	return m
}

// List returns the current rules sorted by ID.
func (m *Manager) List() []model.Rule {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]model.Rule, 0, len(m.rules))
	for _, r := range m.rules {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Upsert adds or updates one rule and applies it to the live engine immediately.
// A disabled rule is removed from the hot cache but kept in the editable set.
func (m *Manager) Upsert(r model.Rule) error {
	if r.ID == 0 {
		return fmt.Errorf("rule id is required")
	}
	// Auto-increment the per-rule version on each edit unless the caller pinned one.
	m.mu.Lock()
	if prev, ok := m.rules[r.ID]; ok && r.Version <= prev.Version {
		r.Version = prev.Version + 1
	} else if r.Version == 0 {
		r.Version = 1
	}
	m.mu.Unlock()
	if r.Enabled {
		if err := m.eng.AddRule(r); err != nil {
			return err
		}
	} else {
		m.eng.RemoveRule(r.ID)
	}
	m.mu.Lock()
	m.rules[r.ID] = r
	m.mu.Unlock()
	return nil
}

// Remove deletes a rule from the live engine and the editable set.
func (m *Manager) Remove(id int64) {
	m.eng.RemoveRule(id)
	m.mu.Lock()
	delete(m.rules, id)
	m.mu.Unlock()
}

// Test compiles a draft expression and evaluates it against a sample row without
// touching the live rule set — the "Test Rule" button in the editor.
//
// Both the expression and the row are untrusted external input (the /rules/test
// HTTP endpoint feeds them straight in). Compile and Eval are panic-contained
// here rather than relying on an HTTP-layer recover: the CLI `Serve`/`NewApp`
// paths mount no recover middleware, and Test is a distinct eval boundary from
// the batch matchInto path — so a compile/eval panic on hostile input is turned
// into a clean error instead of taking the process (or the connection
// goroutine) down. A recovered panic reports as a normal draft error.
func (m *Manager) Test(exprText string, row map[string]any) (matched bool, err error) {
	defer func() {
		if r := recover(); r != nil {
			matched, err = false, fmt.Errorf("rule test failed (recovered): %v", r)
		}
	}()
	plan, err := m.eng.Backend().Compile(exprText)
	if err != nil {
		return false, err
	}
	ctx := m.eng.Backend().NewContext(row)
	return m.eng.Backend().Eval(ctx, plan), nil
}

// Publish snapshots the current rule set as a new, restorable version.
func (m *Manager) Publish(note string) Version {
	rules := m.List()
	m.mu.Lock()
	defer m.mu.Unlock()
	v := Version{
		Version: len(m.versions) + 1,
		Time:    time.Now(),
		Note:    note,
		Count:   len(rules),
		Rules:   rules,
	}
	m.versions = append(m.versions, v)
	return v
}

// Versions returns the published snapshots, oldest first.
func (m *Manager) Versions() []Version {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Version, len(m.versions))
	copy(out, m.versions)
	return out
}

// Rollback atomically restores a published version into the live engine.
func (m *Manager) Rollback(version int) error {
	m.mu.Lock()
	var rules []model.Rule
	for i := range m.versions {
		if m.versions[i].Version == version {
			rules = make([]model.Rule, len(m.versions[i].Rules))
			copy(rules, m.versions[i].Rules)
			break
		}
	}
	m.mu.Unlock()
	if rules == nil {
		return fmt.Errorf("version %d not found", version)
	}
	m.eng.ReplaceRules(rules)
	m.mu.Lock()
	m.rules = make(map[int64]model.Rule, len(rules))
	for _, r := range rules {
		m.rules[r.ID] = r
	}
	m.mu.Unlock()
	return nil
}
