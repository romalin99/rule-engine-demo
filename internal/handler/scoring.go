package handler

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"

	"tcg-rulex-engine/pkg/model"
)

// Match handles POST /match: a flat wide-table user -> matched rule IDs + names.
// Bounded by the concurrency semaphore; records latency for /metrics.
func (h *RuleHandler) Match(c fiber.Ctx) error {
	var u model.User
	if err := json.Unmarshal(c.Body(), &u); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad user json: " + err.Error()})
	}
	h.sem.Acquire()
	defer h.sem.Release()
	t0 := time.Now()
	resp := h.scoreOne(u)
	h.lat.Record(time.Since(t0))
	return c.JSON(resp)
}

// MatchBatch handles POST /match/batch: a JSON array of users -> per-user results.
func (h *RuleHandler) MatchBatch(c fiber.Ctx) error {
	var users []model.User
	if err := json.Unmarshal(c.Body(), &users); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad users json: " + err.Error()})
	}
	h.sem.Acquire()
	defer h.sem.Release()
	results := h.svc.MatchBatchBounded(users)
	out := make([]matchResponse, len(users))
	for i := range users {
		uid := users[i].UID
		if uid == 0 {
			uid = int64(i + 1)
		}
		ids := results[i].RuleIDs
		names := make([]string, 0, len(ids))
		for _, id := range ids {
			if n, ok := h.svc.RuleName(id); ok {
				names = append(names, n)
			}
		}
		out[i] = matchResponse{UID: uid, Hits: len(ids), RuleIDs: ids, Names: names}
	}
	return c.JSON(out)
}

// Evaluate handles POST /evaluate: pass/fail + reasons against one rule tree.
// Body: {"rule":"<SQL>", "rule_id":N, "row":{...}} — rule wins, else rule_id,
// else the built-in flagship rule.
func (h *RuleHandler) Evaluate(c fiber.Ctx) error {
	var req struct {
		Row    map[string]any `json:"row"`
		Rule   string         `json:"rule"`
		RuleID int64          `json:"rule_id"`
	}
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad json: " + err.Error()})
	}
	if req.Rule == "" && req.RuleID != 0 {
		if _, ok := h.svc.RuleName(req.RuleID); !ok {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": fmt.Sprintf("rule_id %d not loaded", req.RuleID)})
		}
	}
	passed, reasons, ruleText, err := h.svc.Evaluate(req.Rule, req.RuleID, req.Row)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"passed": passed, "reasons": reasons, "rule": ruleText})
}

// EvaluateAll handles POST /evaluate/all: {"uid":..,"row":{...}} evaluated against
// ALL loaded rules (gate semantics). Returns passed + failed rules + reasons.
func (h *RuleHandler) EvaluateAll(c fiber.Ctx) error {
	var req struct {
		Row map[string]any `json:"row"`
		UID int64          `json:"uid"`
	}
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad json: " + err.Error()})
	}
	if len(req.Row) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "missing 'row' (wide-table user fields)"})
	}
	return c.JSON(h.svc.EvaluateAll(req.UID, req.Row))
}
