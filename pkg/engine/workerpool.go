package engine

import (
	"runtime"
	"sync"
	"time"

	"tcg-rulex-engine/pkg/model"
)

// Pool runs batch matching with the classic channel + N-workers pattern:
//
//	users ─▶ jobs channel ─▶ [ worker_1 ... worker_N ] ─▶ results
//
//	func worker() {
//	    for job := range ch {
//	        match(job.user)
//	    }
//	}
//
// Each worker writes into a distinct results slot (no locking) and keeps its own
// hit counters, merged once at the end. Compiled ASTs are shared read-only.
type Pool struct {
	workers int
	eng     *Engine
}

// NewPool returns a pool. workers <= 0 defaults to GOMAXPROCS.
func NewPool(workers int, eng *Engine) *Pool {
	return &Pool{workers: workers, eng: eng}
}

type job struct {
	idx  int
	user model.User
}

// Run scores every user against every rule and returns per-user results plus
// aggregate statistics.
func (p *Pool) Run(users []model.User, rules []*model.RuleProgram) ([]model.UserResult, *Stats) {
	n := len(users)
	workers := p.workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if n > 0 && workers > n {
		workers = n
	}

	results := make([]model.UserResult, n)
	stats := &Stats{
		Users:       n,
		Rules:       len(rules),
		Workers:     workers,
		Evaluations: int64(n) * int64(len(rules)),
		PerRule:     make(map[int64]int64),
	}
	if n == 0 {
		return results, stats
	}

	jobs := make(chan job, workers*2)
	partials := make([]struct {
		hits    int64
		perRule map[int64]int64
	}, workers)

	var wg sync.WaitGroup
	start := time.Now()

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			buf := make([]int64, 0, 16) // reused matched-id scratch
			local := make(map[int64]int64)
			var hits int64
			for jb := range jobs {
				buf = p.eng.matchInto(jb.user, rules, buf)
				ids := make([]int64, len(buf))
				copy(ids, buf)
				results[jb.idx] = model.UserResult{UID: jb.user.UID, RuleIDs: ids}
				hits += int64(len(ids))
				for _, id := range ids {
					local[id]++
				}
			}
			partials[w].hits = hits
			partials[w].perRule = local
		}(w)
	}

	for i := range users {
		jobs <- job{idx: i, user: users[i]}
	}
	close(jobs)
	wg.Wait()
	stats.Elapsed = time.Since(start)

	for _, pt := range partials {
		stats.Hits += pt.hits
		for id, c := range pt.perRule {
			stats.PerRule[id] += c
		}
	}
	return results, stats
}
