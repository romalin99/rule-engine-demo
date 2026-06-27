package model

// Rule is a single business rule as it would be stored in a database table
// (e.g. a `rule_engine` column holding a SQL-WHERE style expression).
//
// The Expr field uses SQL boolean-expression syntax, for example:
//
//	age BETWEEN 25 AND 40
//	AND province IN ('广东','江苏','浙江')
//	AND income_level IN ('20k-30k','30k+')
//	AND favorite_category LIKE '数%'
//	AND active_score >= 85
//
// It is parsed/compiled once by a Parser and then evaluated many times against
// incoming user wide-table records.
type Rule struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Expr     string `json:"expr"`
	Priority int    `json:"priority"` // higher sorts first
	Enabled  bool   `json:"enabled"`
}
