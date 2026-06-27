package benchmark

import (
	"fmt"
	"testing"
)

// BenchmarkWorkersScaling shows how batch throughput scales with worker count.
//
//	go test -bench=WorkersScaling -benchmem ./benchmark/
func BenchmarkWorkersScaling(b *testing.B) {
	eng, users := buildEngine(b, 2000, 5000)

	for _, w := range []int{1, 2, 4, 8, 16, 32, 64} {
		w := w
		b.Run(fmt.Sprintf("workers=%d", w), func(b *testing.B) {
			b.ResetTimer()
			var last float64
			for i := 0; i < b.N; i++ {
				_, stats := eng.RunBatch(users, w)
				last = stats.TPS()
			}
			b.ReportMetric(last, "evals/s")
		})
	}
}

// BenchmarkRuleCountScaling shows how single-user latency grows with rule count.
func BenchmarkRuleCountScaling(b *testing.B) {
	for _, n := range []int{100, 1000, 5000, 10000} {
		n := n
		b.Run(fmt.Sprintf("rules=%d", n), func(b *testing.B) {
			eng, users := buildEngine(b, n, 256)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = eng.Match(users[i%len(users)])
			}
		})
	}
}
