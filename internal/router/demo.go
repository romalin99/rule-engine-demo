package router

import (
	"fmt"

	"github.com/gofiber/fiber/v3"

	"tcg-rulex-engine/pkg/ir"
	"tcg-rulex-engine/pkg/model"
	astrt "tcg-rulex-engine/pkg/runtime/ast"
)

// flagshipRule is the full-operator-coverage rule used as the default for the
// /evaluate and /rules/selftest endpoints when the caller omits "rule".
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

// selfTestRuleID is a high, reserved ID used only by /rules/selftest; the probe
// adds it then immediately removes it, so it never collides with real rules.
const selfTestRuleID = 990001

// flagshipSampleRow is a wide-table user that satisfies every clause of
// flagshipRule. Used as the default subject for /rules/selftest.
func flagshipSampleRow() map[string]any {
	return map[string]any{
		"age": 32, "province": "江苏", "income_level": "20k-30k",
		"favorite_category": "数码", "occupation": "工程师", "active_score": 90.5,
		"credit_score": 760, "total_amount": 22000.0, "avg_order_amount": 366.67,
		"last_login_time": "2026-06-26 21:15:00", "vip_level": 4, "order_count": 60,
		"risk_level": "低", "register_days": 400, "marital_status": "已婚",
	}
}

// handleEvaluate accepts a wide-table user row, runs it through one rule tree,
// and returns whether it passes plus the predicates that caused any failure.
//
//	POST /evaluate
//	{ "row": { ...wide-table fields... },
//	  "rule": "<SQL, optional — defaults to the flagship rule>",
//	  "rule_id": <int, optional — explain a currently-loaded rule by id> }
//	-> { "passed": bool, "reasons": [{"expr","detail"}...], "rule": "..." }
func (s *Server) handleEvaluate(c fiber.Ctx) error {
	var req struct {
		Rule   string         `json:"rule"`
		RuleID int64          `json:"rule_id"`
		Row    map[string]any `json:"row"`
	}
	if err := c.Bind().Body(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad json: " + err.Error()})
	}

	rule := req.Rule
	if rule == "" && req.RuleID != 0 {
		p, ok := s.eng.Cache().Get(req.RuleID)
		if !ok {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("rule_id %d not loaded", req.RuleID)})
		}
		rule = p.Source
	}
	if rule == "" {
		rule = flagshipRule
	}

	node, err := ir.Parse(rule)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "parse: " + err.Error()})
	}

	passed, reasons := astrt.Explain(node, req.Row)
	if reasons == nil {
		reasons = []astrt.Reason{} // render [] instead of null
	}
	return c.JSON(fiber.Map{
		"passed":  passed,
		"reasons": reasons,
		"rule":    rule,
	})
}

// handleSelfTest proves live (hot) add + delete of a complex rule on the running
// engine: it scores a sample user before adding, after adding the rule (the user
// should now match it), and after removing it (the match should disappear) —
// returning a step-by-step trace and two boolean proofs.
//
//	POST /rules/selftest
//	{ "rule": "<SQL, optional>", "row": { ... , optional } }
//	-> { "live_add_ok": bool, "live_remove_ok": bool, "steps": [...], ... }
func (s *Server) handleSelfTest(c fiber.Ctx) error {
	var req struct {
		Rule string         `json:"rule"`
		Row  map[string]any `json:"row"`
	}
	_ = c.Bind().Body(&req) // body is optional

	rule := req.Rule
	if rule == "" {
		rule = flagshipRule
	}
	row := req.Row
	if row == nil {
		row = flagshipSampleRow()
	}
	if _, err := ir.Parse(rule); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "parse: " + err.Error()})
	}

	user := model.User{Fields: row}

	type step struct {
		Step    string  `json:"step"`
		Rules   int     `json:"rules"`
		Matched []int64 `json:"matched"`
	}
	steps := []step{{"before", s.eng.RuleCount(), s.eng.Match(user)}}

	// 1) live add (rebuilds the cache atomically; takes effect immediately)
	if err := s.mgr.Upsert(model.Rule{ID: selfTestRuleID, Name: "selftest-complex", Enabled: true, Expr: rule}); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "live add failed: " + err.Error()})
	}
	afterAdd := s.eng.Match(user)
	steps = append(steps, step{"after_add", s.eng.RuleCount(), afterAdd})

	// 2) live remove
	s.mgr.Remove(selfTestRuleID)
	afterRemove := s.eng.Match(user)
	steps = append(steps, step{"after_remove", s.eng.RuleCount(), afterRemove})

	return c.JSON(fiber.Map{
		"rule":           rule,
		"sample_user":    row,
		"added_rule_id":  selfTestRuleID,
		"live_add_ok":    contains(afterAdd, selfTestRuleID),     // matched immediately after add
		"live_remove_ok": !contains(afterRemove, selfTestRuleID), // gone immediately after remove
		"steps":          steps,
	})
}

func contains(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}
