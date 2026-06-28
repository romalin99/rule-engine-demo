package model

// MatchResult is a single (user, rule) hit.
type MatchResult struct {
	RuleName string `json:"rule_name"`
	UID      int64  `json:"uid"`
	RuleID   int64  `json:"rule_id"`
}

// UserResult collects all rule IDs a single user matched.
type UserResult struct {
	RuleIDs []int64 `json:"rule_ids"`
	UID     int64   `json:"uid"`
}

// HitCount is the number of rules this user matched.
func (r *UserResult) HitCount() int { return len(r.RuleIDs) }
