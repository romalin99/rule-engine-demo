// Package service is the business layer (mirrors tcg-rulex-engine's internal/service).
// It orchestrates the rule engine: loading rules from a repository into the
// engine cache, online scoring, gate evaluation, and the operations surface
// (hot reload / draft test / self-test / version publish & rollback).
package service

import (
	"context"
	"fmt"

	"tcg-rulex-engine/internal/infra"
	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/ir"
	"tcg-rulex-engine/pkg/model"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

// flagshipRule is the full-operator-coverage rule used as the default for
// Evaluate and SelfTest when the caller omits a rule.
const flagshipRule = "age BETWEEN 25 AND 40 " +
	"AND province IN ('广东','江苏','浙江') " +
	"AND income_level NOT IN ('<5k','5k-10k') " +
	"AND favorite_category LIKE '数%' " +
	"AND occupation NOT LIKE '%学生%' " +
	"AND active_score >= 85 " +
	"AND credit_score BETWEEN 700 AND 850 " +
	"AND total_amount > 5000 " +
	"AND avg_order_amount <= 1000 " +
	"AND last_login_time IS NOT NULL " +
	"AND (vip_level >= 3 OR order_count >= 30) " +
	"AND NOT (risk_level = '高') " +
	"AND register_days >= 180 " +
	"AND marital_status <> '未知'"

// selfTestRuleID is a high, reserved ID used only by SelfTest; it is added then
// immediately removed, so it never collides with real rules.
const selfTestRuleID = 990001

// flagshipSampleRow satisfies every clause of flagshipRule (default SelfTest subject).
func flagshipSampleRow() map[string]any {
	return map[string]any{
		"age": 32, "province": "江苏", "income_level": "20k-30k",
		"favorite_category": "数码", "occupation": "工程师", "active_score": 90.5,
		"credit_score": 760, "total_amount": 22000.0, "avg_order_amount": 366.67,
		"last_login_time": "2026-06-26 21:15:00", "vip_level": 4, "order_count": 60,
		"risk_level": "低", "register_days": 400, "marital_status": "已婚",
	}
}

// RuleService is the business layer over the rule engine + manager.
type RuleService struct {
	com *infra.ComManager
}

// NewRuleService wires the rule service to the infra container.
func NewRuleService(com *infra.ComManager) *RuleService { return &RuleService{com: com} }

// ── Loading & basic scoring ───────────────────────────────────────────────────

// LoadRules pulls rule definitions from the repository and compiles them into the
// engine cache. Safe to call again for a full reload.
func (s *RuleService) LoadRules(ctx context.Context) (loaded, failed int, err error) {
	rules, err := s.com.RuleRepository.Load(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("rule repository load: %w", err)
	}
	loaded, failed = s.com.Engine.LoadRules(rules)
	return loaded, failed, nil
}

// RuleCount returns the number of compiled rules currently in the engine.
func (s *RuleService) RuleCount() int { return s.com.Engine.RuleCount() }

// Match returns the IDs of rules a single wide-table user matches.
func (s *RuleService) Match(u model.User) []int64 { return s.com.Engine.Match(u) }

// MatchBatchBounded scores many users with the engine's bounded worker pool.
func (s *RuleService) MatchBatchBounded(users []model.User) []model.UserResult {
	return s.com.Engine.MatchBatchBounded(users)
}

// RuleName returns the display name of a compiled rule by ID.
func (s *RuleService) RuleName(id int64) (string, bool) {
	p, ok := s.com.Engine.Cache().Get(id)
	if !ok {
		return "", false
	}
	return p.Name, true
}

// ── Operations (hot reload / versions) — delegate to engine.Manager ────────────

// List returns the current editable rule set.
func (s *RuleService) List() []model.Rule { return s.com.Manager.List() }

// Upsert adds or updates a rule and rebuilds the cache atomically (live).
func (s *RuleService) Upsert(r model.Rule) error { return s.com.Manager.Upsert(r) }

// Remove deletes a rule by ID (live).
func (s *RuleService) Remove(id int64) { s.com.Manager.Remove(id) }

// TestDraft tests a draft rule against one sample row without persisting it.
func (s *RuleService) TestDraft(rule string, row map[string]any) (bool, error) {
	return s.com.Manager.Test(rule, row)
}

// Publish snapshots the current rule set as a new version.
func (s *RuleService) Publish(note string) engine.Version { return s.com.Manager.Publish(note) }

// Versions returns the published version snapshots.
func (s *RuleService) Versions() []engine.Version { return s.com.Manager.Versions() }

// VersionCount returns the number of published snapshots (for metrics).
func (s *RuleService) VersionCount() int { return len(s.com.Manager.Versions()) }

// Rollback restores a published version snapshot (live).
func (s *RuleService) Rollback(version int) error { return s.com.Manager.Rollback(version) }

// ── Evaluate (single rule tree, explainable) ───────────────────────────────────

// Evaluate runs one rule tree against a row and returns whether it passes plus
// the failing predicates. rule wins; else rule_id's source; else the flagship.
func (s *RuleService) Evaluate(rule string, ruleID int64, row map[string]any) (passed bool, reasons []astrt.Reason, ruleText string, err error) {
	ruleText = rule
	if ruleText == "" && ruleID != 0 {
		p, ok := s.com.Engine.Cache().Get(ruleID)
		if !ok {
			return false, nil, "", fmt.Errorf("rule_id %d not loaded", ruleID)
		}
		ruleText = p.Source
	}
	if ruleText == "" {
		ruleText = flagshipRule
	}
	node, perr := ir.Parse(ruleText)
	if perr != nil {
		return false, nil, ruleText, fmt.Errorf("parse: %w", perr)
	}
	passed, reasons = astrt.Explain(node, row)
	if reasons == nil {
		reasons = []astrt.Reason{}
	}
	return passed, reasons, ruleText, nil
}

// ── Evaluate ALL loaded rules (gate semantics: must match every rule) ──────────

// FailedRule is one rule a user did not satisfy, with value-aware reasons.
type FailedRule struct {
	Name    string         `json:"name"`
	Rule    string         `json:"rule"`
	Reasons []astrt.Reason `json:"reasons"`
	RuleID  int64          `json:"rule_id"`
}

// EvaluateAllResult is the gate-evaluation result (must match ALL loaded rules).
type EvaluateAllResult struct {
	PassedRuleIDs []int64      `json:"passed_rule_ids"`
	FailedRuleIDs []int64      `json:"failed_rule_ids"`
	Failed        []FailedRule `json:"failed"`
	UID           int64        `json:"uid"`
	TotalRules    int          `json:"total_rules"`
	PassedCount   int          `json:"passed_count"`
	FailedCount   int          `json:"failed_count"`
	Passed        bool         `json:"passed"`
}

// EvaluateAll runs a wide-table row against EVERY loaded rule. Passed is true only
// when the row satisfies all rules; otherwise it lists each unsatisfied rule with
// its ID, name, full SQL and failing predicates.
func (s *RuleService) EvaluateAll(uid int64, row map[string]any) EvaluateAllResult {
	snapshot := s.com.Engine.Cache().Snapshot()
	res := EvaluateAllResult{
		UID:           uid,
		TotalRules:    len(snapshot),
		PassedRuleIDs: make([]int64, 0, len(snapshot)),
		FailedRuleIDs: make([]int64, 0),
		Failed:        make([]FailedRule, 0),
	}
	for _, p := range snapshot {
		node, err := ir.Parse(p.Source)
		if err != nil {
			res.FailedRuleIDs = append(res.FailedRuleIDs, p.ID)
			res.Failed = append(res.Failed, FailedRule{
				RuleID: p.ID, Name: p.Name, Rule: p.Source,
				Reasons: []astrt.Reason{{Expr: p.Source, Detail: "rule text could not be parsed: " + err.Error()}},
			})
			continue
		}
		ok, reasons := astrt.Explain(node, row)
		if ok {
			res.PassedRuleIDs = append(res.PassedRuleIDs, p.ID)
			continue
		}
		if reasons == nil {
			reasons = []astrt.Reason{}
		}
		res.FailedRuleIDs = append(res.FailedRuleIDs, p.ID)
		res.Failed = append(res.Failed, FailedRule{RuleID: p.ID, Name: p.Name, Rule: p.Source, Reasons: reasons})
	}
	res.PassedCount = len(res.PassedRuleIDs)
	res.FailedCount = len(res.FailedRuleIDs)
	res.Passed = res.TotalRules > 0 && res.FailedCount == 0
	return res
}

// ── Self-test (prove live add + remove takes effect) ───────────────────────────

// SelfTestStep is one step in the self-test trace.
type SelfTestStep struct {
	Step    string  `json:"step"`
	Matched []int64 `json:"matched"`
	Rules   int     `json:"rules"`
}

// SelfTestResult proves a complex rule hot add/remove took effect immediately.
type SelfTestResult struct {
	SampleUser   map[string]any `json:"sample_user"`
	Rule         string         `json:"rule"`
	Steps        []SelfTestStep `json:"steps"`
	AddedRuleID  int64          `json:"added_rule_id"`
	LiveAddOK    bool           `json:"live_add_ok"`
	LiveRemoveOK bool           `json:"live_remove_ok"`
}

// SelfTest scores a sample user, hot-adds a complex rule (should match), then
// hot-removes it (match disappears), returning the trace and two boolean proofs.
func (s *RuleService) SelfTest(rule string, row map[string]any) (SelfTestResult, error) {
	if rule == "" {
		rule = flagshipRule
	}
	if row == nil {
		row = flagshipSampleRow()
	}
	if _, err := ir.Parse(rule); err != nil {
		return SelfTestResult{}, fmt.Errorf("parse: %w", err)
	}

	user := model.User{Fields: row}
	steps := []SelfTestStep{{Step: "before", Rules: s.com.Engine.RuleCount(), Matched: s.com.Engine.Match(user)}}

	if err := s.com.Manager.Upsert(model.Rule{ID: selfTestRuleID, Name: "selftest-complex", Enabled: true, Expr: rule}); err != nil {
		return SelfTestResult{}, fmt.Errorf("live add failed: %w", err)
	}
	afterAdd := s.com.Engine.Match(user)
	steps = append(steps, SelfTestStep{Step: "after_add", Rules: s.com.Engine.RuleCount(), Matched: afterAdd})

	s.com.Manager.Remove(selfTestRuleID)
	afterRemove := s.com.Engine.Match(user)
	steps = append(steps, SelfTestStep{Step: "after_remove", Rules: s.com.Engine.RuleCount(), Matched: afterRemove})

	return SelfTestResult{
		Rule:         rule,
		SampleUser:   row,
		AddedRuleID:  selfTestRuleID,
		LiveAddOK:    contains(afterAdd, selfTestRuleID),
		LiveRemoveOK: !contains(afterRemove, selfTestRuleID),
		Steps:        steps,
	}, nil
}

func contains(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
