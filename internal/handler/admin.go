package handler

import (
	"encoding/json"
	"strconv"

	"github.com/gofiber/fiber/v3"

	"tcg-rulex-engine/pkg/model"
	"tcg-rulex-engine/pkg/web"
)

// Editor serves the operations console (single-file HTML rule editor) at GET /.
//
//	@Summary		运维控制台
//	@Description	返回单文件 HTML 规则编辑器（运维控制台）页面。
//	@Tags			console
//	@Produce		html
//	@Success		200	{string}	string	"HTML page"
//	@Router			/ [get]
func (h *RuleHandler) Editor(c fiber.Ctx) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(web.Page)
}

// RuleList handles GET /rules/list -> all editable rules.
//
//	@Summary		规则列表
//	@Description	返回全部可编辑规则。
//	@Tags			rules
//	@Produce		json
//	@Success		200	{object}	router.RuleListResponse
//	@Router			/rules/list [get]
func (h *RuleHandler) RuleList(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"rules": h.svc.List()})
}

// RuleUpsert handles POST /rules -> add/update a rule and take effect immediately.
//
//	@Summary		新增/更新规则
//	@Description	新增或更新一条规则并立即生效（热更新）。
//	@Tags			rules
//	@Accept			json
//	@Produce		json
//	@Param			rule	body		router.RuleInput	true	"规则对象"
//	@Success		200		{object}	router.UpsertResponse
//	@Failure		400		{object}	router.ErrorResponse
//	@Router			/rules [post]
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

// RuleDelete handles DELETE /rules/:id -> remove a rule (live).
//
//	@Summary		删除规则
//	@Description	按 ID 删除一条规则（实时生效）。
//	@Tags			rules
//	@Produce		json
//	@Param			id	path		int	true	"规则 ID"
//	@Success		200	{object}	router.DeleteResponse
//	@Failure		400	{object}	router.ErrorResponse
//	@Router			/rules/{id} [delete]
func (h *RuleHandler) RuleDelete(c fiber.Ctx) error {
	id, err := strconv.ParseInt(c.Params("id"), 10, 64)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "bad id"})
	}
	h.svc.Remove(id)
	return c.JSON(fiber.Map{"ok": true, "id": id, "rules": h.svc.RuleCount()})
}

// RuleTest handles POST /rules/test -> test a draft rule against a sample row.
//
//	@Summary		草稿规则测试
//	@Description	对草稿规则文本针对一条样例 row 进行匹配测试，返回是否命中。
//	@Tags			rules
//	@Accept			json
//	@Produce		json
//	@Param			request	body		router.RuleTestRequest	true	"草稿测试请求"
//	@Success		200		{object}	router.RuleTestResponse
//	@Failure		400		{object}	router.ErrorResponse
//	@Router			/rules/test [post]
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

// SelfTest handles POST /rules/selftest -> prove live add + remove of a complex rule.
// Body is optional ({rule,row}); defaults to the flagship rule + sample user.
//
//	@Summary		自检（热增删验证）
//	@Description	验证一条复杂规则的实时新增与删除。请求体可选（{rule,row}），缺省时使用内置旗舰规则与样例用户。
//	@Tags			rules
//	@Accept			json
//	@Produce		json
//	@Param			request	body		router.SelfTestRequest	false	"自检请求（可选）"
//	@Success		200		{object}	router.SelfTestResponse
//	@Failure		400		{object}	router.ErrorResponse
//	@Router			/rules/selftest [post]
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

// VersionList handles GET /versions -> published snapshots.
//
//	@Summary		版本列表
//	@Description	返回所有已发布的规则集快照。
//	@Tags			versions
//	@Produce		json
//	@Success		200	{object}	router.VersionListResponse
//	@Router			/versions [get]
func (h *RuleHandler) VersionList(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"versions": h.svc.Versions()})
}

// Publish handles POST /versions -> snapshot the current rule set (optional note).
//
//	@Summary		发布版本
//	@Description	将当前规则集快照为一个新版本（备注可选）。
//	@Tags			versions
//	@Accept			json
//	@Produce		json
//	@Param			request	body		router.PublishRequest	false	"发布请求（可选备注）"
//	@Success		200		{object}	router.PublishResponse
//	@Router			/versions [post]
func (h *RuleHandler) Publish(c fiber.Ctx) error {
	var req struct {
		Note string `json:"note"`
	}
	_ = json.Unmarshal(c.Body(), &req)
	v := h.svc.Publish(req.Note)
	return c.JSON(fiber.Map{"version": v.Version, "count": v.Count})
}

// Rollback handles POST /versions/:v/rollback -> restore a snapshot (live).
//
//	@Summary		回滚版本
//	@Description	将规则集回滚到指定版本快照（实时生效）。
//	@Tags			versions
//	@Produce		json
//	@Param			v	path		int	true	"版本号"
//	@Success		200	{object}	router.RollbackResponse
//	@Failure		400	{object}	router.ErrorResponse
//	@Failure		404	{object}	router.ErrorResponse
//	@Router			/versions/{v}/rollback [post]
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
