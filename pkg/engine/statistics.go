package engine

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Stats captures the outcome and performance of a batch match run.
type Stats struct {
	Users       int
	Rules       int
	Workers     int
	Evaluations int64 // total rule evaluations = Users * Rules
	Hits        int64 // total (user,rule) matches
	Elapsed     time.Duration

	// PerRule maps rule ID -> number of users that matched it.
	PerRule map[int64]int64
}

// TPS is throughput in rule-evaluations per second (the engine's core metric).
func (s *Stats) TPS() float64 {
	if s.Elapsed <= 0 {
		return 0
	}
	return float64(s.Evaluations) / s.Elapsed.Seconds()
}

// QPS is throughput in users-scored per second.
func (s *Stats) QPS() float64 {
	if s.Elapsed <= 0 {
		return 0
	}
	return float64(s.Users) / s.Elapsed.Seconds()
}

// HitRate is hits / evaluations.
func (s *Stats) HitRate() float64 {
	if s.Evaluations == 0 {
		return 0
	}
	return float64(s.Hits) / float64(s.Evaluations)
}

// AvgLatencyMs is the average wall-clock time to fully score one user (against
// all rules), in milliseconds.
func (s *Stats) AvgLatencyMs() float64 {
	if s.Users == 0 {
		return 0
	}
	return float64(s.Elapsed.Nanoseconds()) / float64(s.Users) / 1e6
}

type ruleHit struct {
	ID    int64
	Count int64
}

// TopRules returns the n most-hit rules, descending.
func (s *Stats) TopRules(n int) []ruleHit {
	hits := make([]ruleHit, 0, len(s.PerRule))
	for id, c := range s.PerRule {
		hits = append(hits, ruleHit{ID: id, Count: c})
	}
	sort.Slice(hits, func(i, j int) bool {
		if hits[i].Count != hits[j].Count {
			return hits[i].Count > hits[j].Count
		}
		return hits[i].ID < hits[j].ID
	})
	if n > 0 && len(hits) > n {
		hits = hits[:n]
	}
	return hits
}

// Report renders the headline metrics in the required format.
func (s *Stats) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Worker=%d\n", s.Workers)
	fmt.Fprintf(&b, "Total User=%s\n", comma(int64(s.Users)))
	fmt.Fprintf(&b, "Rule=%s\n", comma(int64(s.Rules)))
	fmt.Fprintf(&b, "Hit=%s\n", comma(s.Hits))
	fmt.Fprintf(&b, "Eval=%s\n", comma(s.Evaluations))
	fmt.Fprintf(&b, "Elapsed=%s\n", s.Elapsed.Round(time.Millisecond))
	fmt.Fprintf(&b, "TPS=%s\n", comma(int64(s.TPS())))
	fmt.Fprintf(&b, "QPS=%s\n", comma(int64(s.QPS())))
	fmt.Fprintf(&b, "Latency=%.3fms\n", s.AvgLatencyMs())
	fmt.Fprintf(&b, "HitRate=%.4f%%\n", s.HitRate()*100)
	return b.String()
}

// RuleHits renders per-rule hit counts (sorted by rule ID) for operations:
//
//	Rule1001    Hit 302
//	Rule1002    Hit 1008
//
// nameOf is optional (may be nil); when provided, the rule name is appended.
func (s *Stats) RuleHits(nameOf func(int64) string) string {
	ids := make([]int64, 0, len(s.PerRule))
	for id := range s.PerRule {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	var b strings.Builder
	for _, id := range ids {
		if nameOf != nil {
			if name := nameOf(id); name != "" {
				fmt.Fprintf(&b, "Rule%-8d Hit %-8s %s\n", id, comma(s.PerRule[id]), name)
				continue
			}
		}
		fmt.Fprintf(&b, "Rule%-8d Hit %s\n", id, comma(s.PerRule[id]))
	}
	return b.String()
}

// comma formats an integer with thousands separators: 2300000 -> "2,300,000".
func comma(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	if neg {
		return "-" + string(out)
	}
	return string(out)
}
