package engine

import (
	"sort"
	"sync"

	"github.com/example/rule-engine-demo/pkg/model"
)

// RuleCache is the compiled-rule store.
//
// Design (as required):
//
//	rules.json → Read → Parse → AST → sync.Map (id → *RuleProgram)
//
// After startup the AST is NEVER re-parsed; the hot path only reads it.
//
// sync.Map is the source of truth and supports concurrent hot-reload (Put/Delete
// while workers are matching). For the matching loop we additionally keep an
// immutable snapshot slice so workers iterate a plain []*RuleProgram (much
// faster and allocation-free than ranging a sync.Map every time).
type RuleCache struct {
	m sync.Map // int64 -> *model.RuleProgram

	mu       sync.RWMutex
	snapshot []*model.RuleProgram
}

// NewRuleCache returns an empty cache.
func NewRuleCache() *RuleCache { return &RuleCache{} }

// Put inserts/updates a single compiled rule. Call Rebuild() afterwards (or use
// PutAll) to refresh the matching snapshot.
func (c *RuleCache) Put(p *model.RuleProgram) {
	c.m.Store(p.ID, p)
}

// PutAll inserts many programs and rebuilds the snapshot once.
func (c *RuleCache) PutAll(progs []*model.RuleProgram) {
	for _, p := range progs {
		c.m.Store(p.ID, p)
	}
	c.Rebuild()
}

// ReplaceAll atomically swaps the entire rule set (full hot reload): it clears
// the map, inserts the new programs, and rebuilds the snapshot. Readers using a
// previously obtained Snapshot keep working on the old set until Rebuild swaps
// in the new one.
func (c *RuleCache) ReplaceAll(progs []*model.RuleProgram) {
	c.m.Clear()
	c.PutAll(progs)
}

// Delete removes a rule by id (hot-reload). Rebuild() to refresh the snapshot.
func (c *RuleCache) Delete(id int64) { c.m.Delete(id) }

// Get returns the compiled program for an id.
func (c *RuleCache) Get(id int64) (*model.RuleProgram, bool) {
	v, ok := c.m.Load(id)
	if !ok {
		return nil, false
	}
	return v.(*model.RuleProgram), true
}

// Rebuild materializes the immutable snapshot from the sync.Map. The snapshot is
// sorted by descending Priority then ascending ID for deterministic output.
func (c *RuleCache) Rebuild() {
	var progs []*model.RuleProgram
	c.m.Range(func(_, v any) bool {
		progs = append(progs, v.(*model.RuleProgram))
		return true
	})
	sort.SliceStable(progs, func(i, j int) bool {
		if progs[i].Priority != progs[j].Priority {
			return progs[i].Priority > progs[j].Priority
		}
		return progs[i].ID < progs[j].ID
	})
	c.mu.Lock()
	c.snapshot = progs
	c.mu.Unlock()
}

// Snapshot returns the immutable rule slice used by the matching loop. The slice
// is replaced wholesale on Rebuild, never mutated in place, so the returned
// reference is safe to read from many goroutines without copying.
func (c *RuleCache) Snapshot() []*model.RuleProgram {
	c.mu.RLock()
	s := c.snapshot
	c.mu.RUnlock()
	return s
}

// Len reports the number of cached rules (from the snapshot).
func (c *RuleCache) Len() int {
	c.mu.RLock()
	n := len(c.snapshot)
	c.mu.RUnlock()
	return n
}
