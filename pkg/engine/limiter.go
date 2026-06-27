package engine

import "github.com/example/rule-engine-demo/pkg/model"

// MaxConcurrentUsers caps how many users may participate in rule judgment at the
// same time (Phase 5 / v0.5.0). It bounds both batch worker fan-out
// (MatchBatchBounded) and concurrent online scoring requests (the HTTP server's
// semaphore). 200 is the v0.5.0 ceiling.
const MaxConcurrentUsers = 200

// Semaphore is a counting semaphore backed by a buffered channel. Acquire blocks
// while MaxConcurrentUsers holders are active, so excess work queues instead of
// over-subscribing the engine.
type Semaphore struct{ slots chan struct{} }

// NewSemaphore returns a semaphore allowing n concurrent holders (n<=0 → 1).
func NewSemaphore(n int) *Semaphore {
	if n <= 0 {
		n = 1
	}
	return &Semaphore{slots: make(chan struct{}, n)}
}

// Acquire takes a slot, blocking until one is free.
func (s *Semaphore) Acquire() { s.slots <- struct{}{} }

// TryAcquire takes a slot without blocking; it returns false when full.
func (s *Semaphore) TryAcquire() bool {
	select {
	case s.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

// Release frees a slot.
func (s *Semaphore) Release() { <-s.slots }

// InFlight reports how many slots are currently held.
func (s *Semaphore) InFlight() int { return len(s.slots) }

// Cap reports the maximum number of concurrent holders.
func (s *Semaphore) Cap() int { return cap(s.slots) }

// MatchBatchBounded scores a batch with concurrency capped at MaxConcurrentUsers,
// guaranteeing no more than 200 users are judged simultaneously regardless of
// batch size. Each worker evaluates one user at a time, so worker count is the
// concurrency bound.
func (e *Engine) MatchBatchBounded(users []model.User) []model.UserResult {
	workers := MaxConcurrentUsers
	if len(users) > 0 && len(users) < workers {
		workers = len(users)
	}
	return e.MatchBatch(users, workers)
}
