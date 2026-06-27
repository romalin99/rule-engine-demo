package model

import "encoding/json"

// Rule is a business rule exactly as it would be stored in a database table or
// rules.json file. The expression uses SQL-WHERE boolean syntax, evaluated by
// the qlbridge runtime.
//
// Example:
//
//	{
//	  "id": 1001,
//	  "name": "VIP用户",
//	  "rule": "age BETWEEN 25 AND 40 AND province IN ('广东','江苏','浙江') AND active_score>=85"
//	}
type Rule struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Expr     string `json:"rule"`
	Priority int    `json:"priority"` // higher sorts first
	Version  int    `json:"version"`  // monotonic per-rule revision (0 = unset)
	Enabled  bool   `json:"enabled"`
}

// UnmarshalJSON accepts either "rule" or "expr" as the expression key so the
// engine is tolerant of both common schemas. When "enabled" is omitted it
// defaults to true (a rule present in the file is active unless disabled).
func (r *Rule) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Rule     string `json:"rule"`
		Expr     string `json:"expr"`
		Priority int    `json:"priority"`
		Version  int    `json:"version"`
		Enabled  *bool  `json:"enabled"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	r.ID = raw.ID
	r.Name = raw.Name
	if raw.Rule != "" {
		r.Expr = raw.Rule
	} else {
		r.Expr = raw.Expr
	}
	r.Priority = raw.Priority
	r.Version = raw.Version
	r.Enabled = raw.Enabled == nil || *raw.Enabled // default true
	return nil
}

// MarshalJSON writes the canonical schema (using the "expr" key).
func (r Rule) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID       int64  `json:"id"`
		Name     string `json:"name"`
		Expr     string `json:"expr"`
		Priority int    `json:"priority"`
		Version  int    `json:"version"`
		Enabled  bool   `json:"enabled"`
	}{ID: r.ID, Name: r.Name, Expr: r.Expr, Priority: r.Priority, Version: r.Version, Enabled: r.Enabled})
}

// RuleProgram is a compiled, cached rule.
//
// AST is intentionally typed as `any` (it actually holds a qlbridge expr.Node)
// so this package stays decoupled from the evaluation backend — the cache and
// model layers never import qlbridge directly. The engine compiles each Rule
// once at startup; afterwards the hot path only evaluates the cached AST and
// never re-parses.
type RuleProgram struct {
	ID       int64
	Name     string
	Priority int    // copied from Rule; controls match/sort order (higher first)
	Version  int    // copied from Rule; per-rule revision for hot-update tracking
	Source   string // original expression text (for logging / debugging)
	AST      any    // compiled expr.Node
}
