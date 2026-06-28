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

// handleEvaluate godoc
//
//	@ID				evaluate
//	@Summary		单规则评估并解释
//	@Description	将一个宽表用户行在单条规则树上求值，返回是否通过 passed，以及导致未通过的谓词原因列表 reasons（passed=true 时为空）。
//	@Description	- rule：可选，SQL 规则文本；省略时使用内置 flagship 规则
//	@Description	- rule_id：可选，对已加载的规则按 ID 取其表达式进行解释（仅当 rule 为空时生效）
//	@Description	- row：宽表用户字段
//	@Tags			Scoring
//	@Accept			json
//	@Produce		json
//	@Param			body	body		EvaluateRequest		true	"评估请求"
//	@Success		200		{object}	EvaluateResponse	"评估结果（passed + reasons）"
//	@Failure		400		{object}	ErrorResponse		"JSON 解析或规则解析失败"
//	@Failure		404		{object}	ErrorResponse		"rule_id 未加载"
//	@Router			/evaluate [post]
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

// handleSelfTest godoc
//
//	@ID				rule_selftest
//	@Summary		规则热增删自检
//	@Description	对运行中的引擎证明复杂规则的热增 / 热删：评分 → 热加规则（应命中）→ 热删规则（应消失），返回逐步轨迹与两个布尔证明。rule 与 row 均可省略（使用内置 flagship 规则与样例）。
//	@Tags			Rules
//	@Accept			json
//	@Produce		json
//	@Param			body	body		SelfTestRequest		false	"可选：自定义规则与样例行"
//	@Success		200		{object}	SelfTestResponse	"自检结果与轨迹"
//	@Failure		400		{object}	ErrorResponse		"规则解析或热加失败"
//	@Router			/rules/selftest [post]
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
