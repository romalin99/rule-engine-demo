package router

import (
	"encoding/json"
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

// handleEvaluateAll godoc
//
//	@ID				evaluate_all
//	@Summary		全规则评估（必须全部通过）
//	@Description	接收一个宽表用户行，对引擎当前已加载的【全部规则】逐条求值。语义为"必须全部命中才算通过"：仅当用户命中所有规则时 passed=true。
//	@Description	未通过时返回 failed_rule_ids（未命中规则的 ID）与 failed（每条未命中规则的完整 SQL 及未通过谓词原因 reasons）。
//	@Description	请求体：{"uid": 可选, "row": {宽表字段...}}。规则集来源于 -serve -rules data/rules_60.json（60 条示例规则）。
//	@Tags			Scoring
//	@Accept			json
//	@Produce		json
//	@Param			body	body		EvaluateAllRequest	true	"宽表用户（{\"row\": {...}}）"
//	@Success		200		{object}	EvaluateAllResponse	"评估结果（passed + 未通过规则 + 原因）"
//	@Failure		400		{object}	ErrorResponse		"JSON 解析失败或缺少 row"
//	@Router			/evaluate/all [post]
//
// handleEvaluateAll runs one wide-table user against EVERY loaded rule and reports
// whether it passes ALL of them (gate / allowlist semantics). For each rule the
// user does not satisfy it returns that rule's ID, name, full SQL text and the
// failing predicates (value-aware reasons), reusing the same Explain as /evaluate.
func (s *Server) handleEvaluateAll(c fiber.Ctx) error {
	var req struct {
		UID int64          `json:"uid"`
		Row map[string]any `json:"row"`
	}
	// Decode the raw body as JSON directly (like /match/batch) so the endpoint
	// works regardless of the request Content-Type — `curl -d` defaults to
	// application/x-www-form-urlencoded, which the struct binder would ignore
	// (leaving row empty). This way the header is optional.
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad json: " + err.Error()})
	}
	if len(req.Row) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing 'row' (wide-table user fields)"})
	}
	return c.JSON(evaluateAll(s.eng.Cache().Snapshot(), req.UID, req.Row))
}

// evaluateAll runs one wide-table row against EVERY rule in snapshot using gate
// semantics: Passed is true only when the row satisfies ALL rules (and there is
// at least one rule). For each unsatisfied rule it records the rule ID, name,
// full SQL text and the value-aware failing predicates (reasons). It is a pure
// function (no transport), so it is unit-tested directly in evaluate_all_test.go.
func evaluateAll(snapshot []*model.RuleProgram, uid int64, row map[string]any) EvaluateAllResponse {
	res := EvaluateAllResponse{
		UID:           uid,
		TotalRules:    len(snapshot),
		PassedRuleIDs: make([]int64, 0, len(snapshot)),
		FailedRuleIDs: make([]int64, 0),
		Failed:        make([]FailedRule, 0),
	}
	for _, p := range snapshot {
		node, err := ir.Parse(p.Source)
		if err != nil {
			// A cached rule whose text cannot be re-parsed natively counts as not passed.
			res.FailedRuleIDs = append(res.FailedRuleIDs, p.ID)
			res.Failed = append(res.Failed, FailedRule{
				RuleID:  p.ID,
				Name:    p.Name,
				Rule:    p.Source,
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
			reasons = []astrt.Reason{} // render [] instead of null
		}
		res.FailedRuleIDs = append(res.FailedRuleIDs, p.ID)
		res.Failed = append(res.Failed, FailedRule{RuleID: p.ID, Name: p.Name, Rule: p.Source, Reasons: reasons})
	}
	res.PassedCount = len(res.PassedRuleIDs)
	res.FailedCount = len(res.FailedRuleIDs)
	res.Passed = res.TotalRules > 0 && res.FailedCount == 0 // must match ALL loaded rules
	return res
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
