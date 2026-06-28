// Package handler is the HTTP layer (mirrors tcg-rulex-engine's internal/handler).
// RuleHandler is the single entry point: handlers decode the request, delegate to
// RuleService, and encode the response. They never touch the engine, manager,
// repositories or datastores directly.
package handler

import (
	"fmt"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"tcg-rulex-engine/internal/service"
	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/model"
)

// RuleHandler serves the full rule-engine ops API over the service layer.
// It owns the concurrency bound (≤ engine.MaxConcurrentUsers) and the online
// /match latency recorder used by /metrics.
type RuleHandler struct {
	svc *service.RuleService
	sem *engine.Semaphore
	lat *latencyRecorder
}

// NewRuleHandler wires the handler to the rule service.
func NewRuleHandler(svc *service.RuleService) *RuleHandler {
	return &RuleHandler{
		svc: svc,
		sem: engine.NewSemaphore(engine.MaxConcurrentUsers),
		lat: newLatencyRecorder(4096),
	}
}

// matchResponse is the per-user scoring result.
type matchResponse struct {
	RuleIDs []int64  `json:"rule_ids"`
	Names   []string `json:"rule_names"`
	UID     int64    `json:"uid"`
	Hits    int      `json:"hits"`
	TookMs  float64  `json:"took_ms"`
}

// Ping is a lightweight liveness probe: GET /ping -> {"ping":"pong"}.
//
//	@Summary		存活探针
//	@Description	轻量存活探针，返回 {"ping":"pong"}。
//	@Tags			probes
//	@Produce		json
//	@Success		200	{object}	router.PingResponse
//	@Router			/ping [get]
func (h *RuleHandler) Ping(c fiber.Ctx) error { return c.JSON(fiber.Map{"ping": "pong"}) }

// Health handles GET /healthz -> {"ok":true,"rules":N}.
//
//	@Summary		健康检查
//	@Description	返回服务健康状态与当前已加载规则数。
//	@Tags			probes
//	@Produce		json
//	@Success		200	{object}	router.HealthResponse
//	@Router			/healthz [get]
func (h *RuleHandler) Health(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"ok": true, "rules": h.svc.RuleCount()})
}

// RuleCount handles GET /rules -> {"rules":N}.
//
//	@Summary		规则数量
//	@Description	返回当前已加载（生效）规则的数量。
//	@Tags			scoring
//	@Produce		json
//	@Success		200	{object}	router.RuleCountResponse
//	@Router			/rules [get]
func (h *RuleHandler) RuleCount(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"rules": h.svc.RuleCount()})
}

// scoreOne matches one user and resolves matched rule names.
func (h *RuleHandler) scoreOne(u model.User) matchResponse {
	t0 := time.Now()
	ids := h.svc.Match(u)
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		if n, ok := h.svc.RuleName(id); ok {
			names = append(names, n)
		}
	}
	return matchResponse{
		UID:     u.UID,
		Hits:    len(ids),
		RuleIDs: ids,
		Names:   names,
		TookMs:  float64(time.Since(t0).Microseconds()) / 1000.0,
	}
}

// Metrics exposes engine metrics in Prometheus text exposition format at /metrics
// (dependency-free): rule count, version count, in-flight users, concurrency cap,
// and recent /match latency percentiles.
//
//	@Summary		Prometheus 指标
//	@Description	以 Prometheus 文本曝光格式返回引擎指标：规则数、版本数、在飞用户数、并发上限与最近 /match 延迟分位数。
//	@Tags			probes
//	@Produce		plain
//	@Success		200	{string}	string	"Prometheus exposition text"
//	@Router			/metrics [get]
func (h *RuleHandler) Metrics(c fiber.Ctx) error {
	var b strings.Builder
	b.WriteString("# HELP rule_engine_rules Number of active compiled rules.\n# TYPE rule_engine_rules gauge\n")
	fmt.Fprintf(&b, "rule_engine_rules %d\n", h.svc.RuleCount())
	b.WriteString("# HELP rule_engine_versions Number of published rule-set versions.\n# TYPE rule_engine_versions gauge\n")
	fmt.Fprintf(&b, "rule_engine_versions %d\n", h.svc.VersionCount())
	b.WriteString("# HELP rule_engine_inflight Users currently being judged.\n# TYPE rule_engine_inflight gauge\n")
	fmt.Fprintf(&b, "rule_engine_inflight %d\n", h.sem.InFlight())
	b.WriteString("# HELP rule_engine_max_concurrency Max concurrent users.\n# TYPE rule_engine_max_concurrency gauge\n")
	fmt.Fprintf(&b, "rule_engine_max_concurrency %d\n", h.sem.Cap())
	p50, p90, p99, n := h.lat.Percentiles()
	b.WriteString("# HELP rule_engine_match_latency_ms Online /match latency percentiles (recent window).\n# TYPE rule_engine_match_latency_ms gauge\n")
	fmt.Fprintf(&b, "rule_engine_match_latency_ms{quantile=\"0.5\"} %.3f\n", p50)
	fmt.Fprintf(&b, "rule_engine_match_latency_ms{quantile=\"0.9\"} %.3f\n", p90)
	fmt.Fprintf(&b, "rule_engine_match_latency_ms{quantile=\"0.99\"} %.3f\n", p99)
	fmt.Fprintf(&b, "rule_engine_match_latency_samples %d\n", n)
	c.Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	return c.SendString(b.String())
}
