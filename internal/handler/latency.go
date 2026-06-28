package handler

import (
	"sort"
	"sync"
	"time"
)

// latencyRecorder keeps a bounded ring of recent /match latencies so the handler
// can report p50 / p90 / p99 at /metrics with no external dependency. It is
// concurrency-safe; recording is O(1) and percentile computation is O(n log n)
// over the (capped) window.
type latencyRecorder struct {
	buf  []time.Duration
	idx  int
	mu   sync.Mutex
	full bool
}

func newLatencyRecorder(capacity int) *latencyRecorder {
	if capacity <= 0 {
		capacity = 4096
	}
	return &latencyRecorder{buf: make([]time.Duration, capacity)}
}

// Record adds one observation to the ring.
func (r *latencyRecorder) Record(d time.Duration) {
	r.mu.Lock()
	r.buf[r.idx] = d
	r.idx++
	if r.idx == len(r.buf) {
		r.idx = 0
		r.full = true
	}
	r.mu.Unlock()
}

// Percentiles returns p50, p90, p99 in milliseconds over the current window and
// the sample count. With no samples it returns zeros.
func (r *latencyRecorder) Percentiles() (p50, p90, p99 float64, n int) {
	r.mu.Lock()
	size := r.idx
	if r.full {
		size = len(r.buf)
	}
	s := make([]time.Duration, size)
	copy(s, r.buf[:size])
	r.mu.Unlock()

	if size == 0 {
		return 0, 0, 0, 0
	}
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	q := func(p float64) float64 {
		i := int(p*float64(size-1) + 0.5)
		if i < 0 {
			i = 0
		}
		if i >= size {
			i = size - 1
		}
		return float64(s[i].Microseconds()) / 1000.0
	}
	return q(0.50), q(0.90), q(0.99), size
}
