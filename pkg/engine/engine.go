// Package engine is a small in-memory rule engine. It compiles rules once into a
// pluggable Backend's AST (cached in a sync.Map) and matches Kafka-style users
// against every rule, single or in concurrent batches.
//
//	rules ─Backend.Compile─▶ AST ─▶ sync.Map(RuleCache)     // parse once
//	user  ─Backend.NewContext─▶ ctx ─Backend.Eval(AST)─▶ matched RuleIDs
//
// After load the AST is never re-parsed; the hot path only evaluates. Swapping
// SQL → CEL → Expr means swapping the Backend; the Engine/business code is
// unchanged.
package engine

import (
	"fmt"

	"github.com/example/rule-engine-demo/pkg/model"
)

// Matcher is the engine interface used by callers (HTTP server, batch jobs, ...).
//
//	type Engine interface {
//	    LoadRules(...)
//	    Match(User) []int64
//	    MatchBatch(...) []UserResult
//	}
type Matcher interface {
	LoadRules(rules []model.Rule) (loaded, failed int)
	Match(u model.User) []int64
	MatchBatch(users []model.User, workers int) []model.UserResult
	RuleCount() int
}

// Engine is the concrete rule engine.
type Engine struct {
	backend Backend
	cache   *RuleCache
}

// compile-time check that *Engine satisfies the Matcher interface.
var _ Matcher = (*Engine)(nil)

// New returns an engine using the default 2026-style pipeline: qlbridge parses
// the rule, the result is lowered to bytecode, and a custom VM evaluates it.
//
//	Rule(SQL) → qlbridge Parser → AST → IR → ByteCode → VM → map[string]any
//
// Swap the front-end (NativeFrontend / a future CEL/Expr frontend) or the whole
// backend via NewWithBackend; nothing else changes.
func New() *Engine {
	return NewWithBackend(NewBytecodeBackend(QLBridgeFrontend{}))
}

// NewWithBackend returns an engine using a custom backend (CEL, Expr, ...).
func NewWithBackend(b Backend) *Engine {
	return &Engine{backend: b, cache: NewRuleCache()}
}

// Backend returns the active backend.
func (e *Engine) Backend() Backend { return e.backend }

// Cache exposes the rule cache (for hot reload / inspection).
func (e *Engine) Cache() *RuleCache { return e.cache }

// RuleCount reports how many compiled rules are active.
func (e *Engine) RuleCount() int { return e.cache.Len() }

// compile turns rules into compiled programs (skipping disabled / un-parseable).
func (e *Engine) compile(rules []model.Rule) (progs []*model.RuleProgram, failed int) {
	progs = make([]*model.RuleProgram, 0, len(rules))
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		ast, err := e.backend.Compile(r.Expr)
		if err != nil {
			failed++
			continue
		}
		progs = append(progs, &model.RuleProgram{
			ID: r.ID, Name: r.Name, Priority: r.Priority, Version: r.Version, Source: r.Expr, AST: ast,
		})
	}
	return progs, failed
}

// LoadRules compiles enabled rules and ADDS them to the cache (merge). Returns
// the number loaded and the number that failed to compile (never fatal).
func (e *Engine) LoadRules(rules []model.Rule) (loaded, failed int) {
	progs, failed := e.compile(rules)
	e.cache.PutAll(progs)
	return len(progs), failed
}

// ReplaceRules atomically swaps the ENTIRE rule set (full hot reload).
func (e *Engine) ReplaceRules(rules []model.Rule) (loaded, failed int) {
	progs, failed := e.compile(rules)
	e.cache.ReplaceAll(progs)
	return len(progs), failed
}

// AddRule compiles and inserts/updates a single rule (incremental hot update).
func (e *Engine) AddRule(r model.Rule) error {
	progs, failed := e.compile([]model.Rule{r})
	if failed > 0 || len(progs) == 0 {
		return fmt.Errorf("engine: rule %d failed to compile", r.ID)
	}
	e.cache.Put(progs[0])
	e.cache.Rebuild()
	return nil
}

// RemoveRule deletes a rule by id (incremental hot update).
func (e *Engine) RemoveRule(id int64) {
	e.cache.Delete(id)
	e.cache.Rebuild()
}

// LoadRulesFromFile loads rules from a JSON file and ADDS them to the cache.
func (e *Engine) LoadRulesFromFile(path string) (loaded, failed int, err error) {
	rules, err := LoadRules(path)
	if err != nil {
		return 0, 0, err
	}
	loaded, failed = e.LoadRules(rules)
	return loaded, failed, nil
}

// ReloadFromFile re-reads a rules file and atomically REPLACES the rule set.
func (e *Engine) ReloadFromFile(path string) (loaded, failed int, err error) {
	rules, err := LoadRules(path)
	if err != nil {
		return 0, 0, err
	}
	loaded, failed = e.ReplaceRules(rules)
	return loaded, failed, nil
}

// Match scores a single user against all cached rules and returns matched rule
// IDs in priority order. The eval context is built once and reused across rules.
func (e *Engine) Match(u model.User) []int64 {
	return e.matchInto(u, e.cache.Snapshot(), nil)
}

// matchInto is the shared match routine; dst is a reusable scratch buffer.
func (e *Engine) matchInto(u model.User, rules []*model.RuleProgram, dst []int64) []int64 {
	ctx := e.backend.NewContext(u.Fields)
	dst = dst[:0]
	for _, p := range rules {
		if e.backend.Eval(ctx, p.AST) {
			dst = append(dst, p.ID)
		}
	}
	return dst
}

// MatchBatch scores a batch of users concurrently and returns per-user results.
func (e *Engine) MatchBatch(users []model.User, workers int) []model.UserResult {
	results, _ := e.RunBatch(users, workers)
	return results
}

// RunBatch is like MatchBatch but also returns aggregate Stats.
func (e *Engine) RunBatch(users []model.User, workers int) ([]model.UserResult, *Stats) {
	return NewPool(workers, e).Run(users, e.cache.Snapshot())
}
