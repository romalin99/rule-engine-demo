// Package engine wires together a (pluggable) Parser and Evaluator with a rule
// cache to match user wide-table records against a set of business rules in
// real time.
//
//	rules.json ──load──▶ Parser.Compile ──▶ RuleCache ──▶ Evaluator.Eval ──▶ matches
//
// The engine is deliberately decoupled from any specific DSL: swap in a CEL or
// Expr backed Parser/Evaluator without touching this package.
package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	"github.com/example/rule-engine-demo/model"
)

// Parser compiles a textual rule into a reusable, evaluable form.
type Parser interface {
	Compile(expression string) (any, error)
}

// Evaluator evaluates a compiled rule against one user's record.
type Evaluator interface {
	Eval(compiled any, user map[string]any) (bool, error)
}

// MatchResult is a single rule a user satisfied.
type MatchResult struct {
	RuleID   int64
	RuleName string
	Priority int
}

// UserMatches groups all rules a single user matched (used in batch mode).
type UserMatches struct {
	UserID  int64
	Matches []MatchResult
}

// Engine holds the compiled rule cache plus the parser/evaluator backends.
type Engine struct {
	parser Parser
	eval   Evaluator
	cache  *RuleCache
}

// New builds an engine from a parser and evaluator backend.
func New(p Parser, e Evaluator) *Engine {
	return &Engine{parser: p, eval: e, cache: NewRuleCache()}
}

// LoadRulesFromFile reads rules from a JSON file (as a DB stand-in) and compiles
// them into the cache.
func (e *Engine) LoadRulesFromFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("engine: read rules file: %w", err)
	}
	var rules []model.Rule
	if err := json.Unmarshal(raw, &rules); err != nil {
		return fmt.Errorf("engine: parse rules file: %w", err)
	}
	return e.LoadRules(rules)
}

// LoadRules compiles the given rules and (atomically) replaces the cache.
// A rule that fails to compile is reported but does not abort the others.
func (e *Engine) LoadRules(rules []model.Rule) error {
	compiled := make([]*CompiledRule, 0, len(rules))
	var firstErr error
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		node, err := e.parser.Compile(r.Expr)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			fmt.Printf("⚠️  skipping rule %d (%s): %v\n", r.ID, r.Name, err)
			continue
		}
		compiled = append(compiled, &CompiledRule{Rule: r, Compiled: node})
	}
	// Highest priority first.
	sort.SliceStable(compiled, func(i, j int) bool {
		return compiled[i].Rule.Priority > compiled[j].Rule.Priority
	})
	e.cache.Replace(compiled)
	return firstErr
}

// Match returns every rule the given user satisfies, highest priority first.
func (e *Engine) Match(user map[string]any) []MatchResult {
	rules := e.cache.Snapshot()
	results := make([]MatchResult, 0, 4)
	for _, cr := range rules {
		ok, err := e.eval.Eval(cr.Compiled, user)
		if err != nil {
			fmt.Printf("⚠️  rule %d (%s) eval error: %v\n", cr.Rule.ID, cr.Rule.Name, err)
			continue
		}
		if ok {
			results = append(results, MatchResult{
				RuleID:   cr.Rule.ID,
				RuleName: cr.Rule.Name,
				Priority: cr.Rule.Priority,
			})
		}
	}
	return results
}

// RuleCount reports how many compiled rules are currently active.
func (e *Engine) RuleCount() int { return len(e.cache.Snapshot()) }
