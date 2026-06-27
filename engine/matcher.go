package engine

// BatchUser is one user's wide-table record plus its id, for batch matching.
type BatchUser struct {
	UserID int64
	Data   map[string]any
}

// MatchBatchSequential evaluates every user against every rule on a single
// goroutine. Simple and deterministic; use MatchBatch for high throughput.
func (e *Engine) MatchBatchSequential(users []BatchUser) []UserMatches {
	out := make([]UserMatches, len(users))
	for i, u := range users {
		out[i] = UserMatches{UserID: u.UserID, Matches: e.Match(u.Data)}
	}
	return out
}
