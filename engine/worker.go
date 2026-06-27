package engine

import (
	"runtime"
	"sync"
)

// MatchBatch evaluates a batch of users concurrently using a worker pool, and
// returns one UserMatches per input user (order preserved).
//
// This models the real scenario: a batch of users arrives, each carrying its
// wide-table row, and every user is run against all cached rules in parallel.
// The compiled rule AST is shared read-only across workers (qlbridge evaluation
// is concurrency-safe), so no per-worker compilation is needed.
//
// workers <= 0 defaults to GOMAXPROCS.
func (e *Engine) MatchBatch(users []BatchUser, workers int) []UserMatches {
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if len(users) < workers {
		workers = len(users)
	}

	out := make([]UserMatches, len(users))
	if len(users) == 0 {
		return out
	}

	jobs := make(chan int, len(users))
	var wg sync.WaitGroup

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				u := users[i]
				out[i] = UserMatches{
					UserID:  u.UserID,
					Matches: e.Match(u.Data), // writes to a distinct index → no lock needed
				}
			}
		}()
	}

	for i := range users {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}
