package model

// MatchResult is a single (user, rule) hit.
type MatchResult struct {
	UID      int64 `json:"uid"`
	RuleID   int64 `json:"rule_id"`
	RuleName string `json:"rule_name"`
}

// UserResult collects all rule IDs a single user matched.
type UserResult struct {
	UID     int64   `json:"uid"`
	RuleIDs []int64 `json:"rule_ids"`
}

// HitCount is the number of rules this user matched.
func (r *UserResult) HitCount() int { return len(r.RuleIDs) }
