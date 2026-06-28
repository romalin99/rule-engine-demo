package handler

import (
	"encoding/json"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"tcg-rulex-engine/pkg/model"
	"tcg-rulex-engine/pkg/web"
)

// Editor serves the operations console (single-file HTML rule editor) at GET /.
func (h *RuleHandler) Editor(c fiber.Ctx) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(web.Page)
}

// RuleList: GET /rules/list -> all editable rules.
func (h *RuleHandler) RuleList(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"rules": h.svc.List()})
}

// RuleUpsert: POST /rules -> add/update a rule and take effect immediately.
func (h *RuleHandler) RuleUpsert(c fiber.Ctx) error {
	var r model.Rule
	if err := json.Unmarshal(c.Body(), &r); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad rule json: " + err.Error()})
	}
	if err := h.svc.Upsert(r); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "id": r.ID, "rules": h.svc.RuleCount()})
}

// RuleDelete: DELETE /rules/:id -> remove a rule (live).
func (h *RuleHandler) RuleDelete(c fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad id"})
	}
	h.svc.Remove(id)
	return c.JSON(fiber.Map{"ok": true, "id": id, "rules": h.svc.RuleCount()})
}

// RuleTest: POST /rules/test -> test a draft rule against a sample row.
func (h *RuleHandler) RuleTest(c fiber.Ctx) error {
	var req struct {
		Row  map[string]any `json:"row"`
		Rule string         `json:"rule"`
	}
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad json: " + err.Error()})
	}
	matched, err := h.svc.TestDraft(req.Rule, req.Row)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"matched": matched})
}

// SelfTest: POST /rules/selftest -> prove live add + remove of a complex rule.
// Body is optional ({rule,row}); defaults to the flagship rule + sample user.
func (h *RuleHandler) SelfTest(c fiber.Ctx) error {
	var req struct {
		Row  map[string]any `json:"row"`
		Rule string         `json:"rule"`
	}
	_ = json.Unmarshal(c.Body(), &req) // body optional
	res, err := h.svc.SelfTest(req.Rule, req.Row)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(res)
}

// VersionList: GET /versions -> published snapshots.
func (h *RuleHandler) VersionList(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"versions": h.svc.Versions()})
}

// Publish: POST /versions -> snapshot the current rule set (optional note).
func (h *RuleHandler) Publish(c fiber.Ctx) error {
	var req struct {
		Note string `json:"note"`
	}
	_ = json.Unmarshal(c.Body(), &req)
	v := h.svc.Publish(req.Note)
	return c.JSON(fiber.Map{"version": v.Version, "count": v.Count})
}

// Rollback: POST /versions/:v/rollback -> restore a snapshot (live).
func (h *RuleHandler) Rollback(c fiber.Ctx) error {
	v, err := strconv.Atoi(c.Params("v"))
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad version"})
	}
	if err := h.svc.Rollback(v); err != nil {
		return c.Status(fiber.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
	}
	return c.JSON(fiber.Map{"ok": true, "version": v, "rules": h.svc.RuleCount()})
}
