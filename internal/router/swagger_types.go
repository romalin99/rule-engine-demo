package router

// This file declares request/response DTOs used ONLY for Swagger (OpenAPI)
// schema generation. The live handlers bind dynamic maps / fiber.Map for
// flexibility; these typed mirrors give `swag` concrete shapes to render.
//
// Field examples are taken from the project's real data samples:
//   - data/users.json  (wide-table user rows)
//   - data/rules.json  (rule objects)

import (
	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/model"
	"tcg-rulex-engine/pkg/runtime/ast"
)

// ── Request DTOs ─────────────────────────────────────────────────────────────

// UserRow is a flat wide-table user record (the body of /match and the "row"
// field of /evaluate, /rules/test, /rules/selftest). The engine accepts ANY
// columns; the fields below are the common ones from data/users.json. Extra
// keys are allowed and evaluated as-is.
type UserRow struct {
	FavoriteCategory string  `json:"favorite_category" example:"数码"`
	Username         string  `json:"username" example:"user001"`
	Gender           string  `json:"gender" example:"男"`
	RiskLevel        string  `json:"risk_level" example:"低"`
	Province         string  `json:"province" example:"广东"`
	City             string  `json:"city" example:"深圳"`
	Occupation       string  `json:"occupation" example:"程序员"`
	IncomeLevel      string  `json:"income_level" example:"20k-30k"`
	DeviceType       string  `json:"device_type" example:"iPhone"`
	VipLevel         int     `json:"vip_level" example:"3"`
	LoginDays30d     int     `json:"login_days_30d" example:"28"`
	OrderCount       int     `json:"order_count" example:"132"`
	TotalAmount      float64 `json:"total_amount" example:"35628.56"`
	AvgOrderAmount   float64 `json:"avg_order_amount" example:"269.91"`
	RegisterDays     int     `json:"register_days" example:"620"`
	UID              int64   `json:"uid" example:"1"`
	ActiveScore      float64 `json:"active_score" example:"91.52"`
	CreditScore      int     `json:"credit_score" example:"765"`
	Age              int     `json:"age" example:"28"`
}

// RuleInput is the body of POST /rules. NOTE the expression key is "rule"
// (mirrors model.Rule's JSON tag). Mirrors data/rules.json items.
type RuleInput struct {
	Name     string `json:"name" example:"电商精准营销-广东高价值男性"`
	Rule     string `json:"rule" example:"gender = '男' AND age BETWEEN 25 AND 35 AND province = '广东' AND vip_level >= 3 AND total_amount >= 10000"`
	ID       int64  `json:"id" example:"1001"`
	Priority int    `json:"priority" example:"10"`
	Version  int    `json:"version" example:"0"`
	Enabled  bool   `json:"enabled" example:"true"`
}

// EvaluateRequest is the body of POST /evaluate.
type EvaluateRequest struct {
	Rule   string  `json:"rule" example:"age BETWEEN 25 AND 40 AND province IN ('广东','江苏','浙江') AND active_score >= 85"`
	Row    UserRow `json:"row"`
	RuleID int64   `json:"rule_id" example:"0"`
}

// EvaluateAllRequest is the body of POST /evaluate/all: a wide-table user row
// evaluated against EVERY loaded rule (gate semantics).
type EvaluateAllRequest struct {
	Row UserRow `json:"row"`
	UID int64   `json:"uid" example:"1"`
}

// RuleTestRequest is the body of POST /rules/test.
type RuleTestRequest struct {
	Rule string  `json:"rule" example:"age >= 30 AND vip_level >= 2 AND total_amount > 5000"`
	Row  UserRow `json:"row"`
}

// SelfTestRequest is the (optional) body of POST /rules/selftest. Both fields
// may be omitted; the engine then uses its built-in flagship rule and sample.
type SelfTestRequest struct {
	Rule string  `json:"rule" example:"age BETWEEN 25 AND 40 AND vip_level >= 3 AND active_score >= 85"`
	Row  UserRow `json:"row"`
}

// PublishRequest is the (optional) body of POST /versions.
type PublishRequest struct {
	Note string `json:"note" example:"上线 618 大促规则集"`
}

// ── Response DTOs ────────────────────────────────────────────────────────────

// MatchResponse is the per-user scoring result (mirrors the internal
// matchResponse). Returned by /match and, as an array, by /match/batch.
type MatchResponse struct {
	RuleIDs []int64  `json:"rule_ids"`
	Names   []string `json:"rule_names"`
	UID     int64    `json:"uid" example:"1"`
	Hits    int      `json:"hits" example:"2"`
	TookMs  float64  `json:"took_ms" example:"0.123"`
}

// HealthResponse is returned by GET /healthz.
type HealthResponse struct {
	OK    bool `json:"ok" example:"true"`
	Rules int  `json:"rules" example:"10"`
}

// PingResponse is returned by GET /ping.
type PingResponse struct {
	Ping string `json:"ping" example:"pong"`
}

// RuleCountResponse is returned by GET /rules.
type RuleCountResponse struct {
	Rules int `json:"rules" example:"10"`
}

// RuleListResponse is returned by GET /rules/list.
type RuleListResponse struct {
	Rules []model.Rule `json:"rules"`
}

// UpsertResponse is returned by POST /rules.
type UpsertResponse struct {
	OK    bool  `json:"ok" example:"true"`
	ID    int64 `json:"id" example:"1001"`
	Rules int   `json:"rules" example:"11"`
}

// DeleteResponse is returned by DELETE /rules/{id}.
type DeleteResponse struct {
	OK    bool  `json:"ok" example:"true"`
	ID    int64 `json:"id" example:"1001"`
	Rules int   `json:"rules" example:"10"`
}

// RuleTestResponse is returned by POST /rules/test.
type RuleTestResponse struct {
	Matched bool `json:"matched" example:"true"`
}

// EvaluateResponse is returned by POST /evaluate. reasons is empty when passed.
type EvaluateResponse struct {
	Rule    string       `json:"rule" example:"age BETWEEN 25 AND 40 AND active_score >= 85"`
	Reasons []ast.Reason `json:"reasons"`
	Passed  bool         `json:"passed" example:"false"`
}

// FailedRule is one rule the user did not satisfy (element of EvaluateAllResponse.Failed).
type FailedRule struct {
	Name    string       `json:"name" example:"全算子覆盖-精准圈选(旗舰)"`
	Rule    string       `json:"rule" example:"age BETWEEN 25 AND 40 AND ... AND marital_status <> '未知'"`
	Reasons []ast.Reason `json:"reasons"`
	RuleID  int64        `json:"rule_id" example:"1001"`
}

// EvaluateAllResponse is returned by POST /evaluate/all. passed is true only when
// the user matches EVERY loaded rule; otherwise failed lists each unsatisfied rule
// with its ID, full SQL text and the failing predicates (reasons).
type EvaluateAllResponse struct {
	PassedRuleIDs []int64      `json:"passed_rule_ids"`
	FailedRuleIDs []int64      `json:"failed_rule_ids"`
	Failed        []FailedRule `json:"failed"`
	UID           int64        `json:"uid" example:"1"`
	TotalRules    int          `json:"total_rules" example:"60"`
	PassedCount   int          `json:"passed_count" example:"2"`
	FailedCount   int          `json:"failed_count" example:"58"`
	Passed        bool         `json:"passed" example:"false"`
}

// PublishResponse is returned by POST /versions.
type PublishResponse struct {
	Version int `json:"version" example:"1"`
	Count   int `json:"count" example:"10"`
}

// VersionListResponse is returned by GET /versions.
type VersionListResponse struct {
	Versions []engine.Version `json:"versions"`
}

// RollbackResponse is returned by POST /versions/{v}/rollback.
type RollbackResponse struct {
	OK      bool `json:"ok" example:"true"`
	Version int  `json:"version" example:"1"`
	Rules   int  `json:"rules" example:"10"`
}

// SelfTestStep is one step in the /rules/selftest trace.
type SelfTestStep struct {
	Step    string  `json:"step" example:"after_add"`
	Matched []int64 `json:"matched"`
	Rules   int     `json:"rules" example:"11"`
}

// SelfTestResponse is returned by POST /rules/selftest.
type SelfTestResponse struct {
	SampleUser   map[string]any `json:"sample_user"`
	Rule         string         `json:"rule"`
	Steps        []SelfTestStep `json:"steps"`
	AddedRuleID  int64          `json:"added_rule_id" example:"990001"`
	LiveAddOK    bool           `json:"live_add_ok" example:"true"`
	LiveRemoveOK bool           `json:"live_remove_ok" example:"true"`
}

// ErrorResponse is the uniform error body ({"error": "..."}) returned by all
// handlers on failure.
type ErrorResponse struct {
	Error string `json:"error" example:"bad rule json: unexpected end of JSON input"`
}
