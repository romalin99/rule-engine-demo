package engine

import (
	"sync"

	"github.com/example/rule-engine-demo/model"
)

// CompiledRule pairs a rule with its compiled (parser-specific) expression.
// Compiled is the opaque value returned by Parser.Compile and consumed by
// Evaluator.Eval.
type CompiledRule struct {
	Rule     model.Rule
	Compiled any
}

// RuleCache holds the active set of compiled rules. It is safe for concurrent
// reads (Match / batch workers) while LoadRules swaps the set via Replace.
type RuleCache struct {
	mu    sync.RWMutex
	rules []*CompiledRule
}

// NewRuleCache returns an empty cache.
func NewRuleCache() *RuleCache { return &RuleCache{} }

// Replace atomically swaps in a new compiled rule set (e.g. after a hot reload).
func (c *RuleCache) Replace(rules []*CompiledRule) {
	c.mu.Lock()
	c.rules = rules
	c.mu.Unlock()
}

// Snapshot returns the current rule slice. The slice is only ever replaced
// (never mutated in place), so the returned reference is safe to read
// concurrently without copying.
func (c *RuleCache) Snapshot() []*CompiledRule {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.rules
}
