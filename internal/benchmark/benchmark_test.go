// Package benchmark holds Go benchmarks for the rule engine.
//
// Run them with:
//
//	go test -bench=. -benchmem ./internal/benchmark/
//
// BenchmarkRunBatch prints the headline block:
//
//	Rule 10000
//	User 10000
//	Worker 32
//	TPS 2,300,000
//	Latency 0.2ms
package benchmark

import (
	"fmt"
	"testing"

	ds "tcg-rulex-engine/internal/datasource"
	"tcg-rulex-engine/pkg/engine"
	"tcg-rulex-engine/pkg/model"
)

const (
	benchRules = 10000
	benchUsers = 10000
	benchWkrs  = 32
	seed       = 42
)

// buildEngine returns a loaded engine plus a generated user slice.
func buildEngine(tb testing.TB, nRules, nUsers int) (*engine.Engine, []model.User) {
	tb.Helper()
	gen := ds.NewGenerator(seed)
	rules := gen.Rules(nRules)
	users := gen.Users(nUsers)

	eng := engine.New()
	loaded, failed := eng.LoadRules(rules)
	if failed > 0 || loaded == 0 {
		tb.Fatalf("load rules: loaded=%d failed=%d", loaded, failed)
	}
	return eng, users
}

// BenchmarkCompile measures one-time rule parsing/compilation throughput.
func BenchmarkCompile(b *testing.B) {
	gen := ds.NewGenerator(seed)
	rules := gen.Rules(benchRules)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		eng := engine.New()
		if loaded, failed := eng.LoadRules(rules); failed > 0 || loaded == 0 {
			b.Fatalf("compile failed=%d ok=%d", failed, loaded)
		}
	}
}

// BenchmarkMatchUser measures scoring a single user against all rules.
func BenchmarkMatchUser(b *testing.B) {
	eng, users := buildEngine(b, benchRules, 1024)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = eng.Match(users[i%len(users)])
	}
}

// BenchmarkRunBatch measures full batch matching (Users x Rules) with a worker
// pool and prints the headline report.
func BenchmarkRunBatch(b *testing.B) {
	eng, users := buildEngine(b, benchRules, benchUsers)

	var stats *engine.Stats
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, stats = eng.RunBatch(users, benchWkrs)
	}
	b.StopTimer()

	if stats != nil {
		b.ReportMetric(stats.TPS(), "TPS")
		b.ReportMetric(stats.AvgLatencyMs(), "ms/user")
		fmt.Printf("\nRule %d\nUser %d\nWorker %d\nTPS %s\nLatency %.3fms\n",
			stats.Rules, stats.Users, stats.Workers, commafmt(int64(stats.TPS())), stats.AvgLatencyMs())
	}
}

// commafmt formats an integer with thousands separators.
func commafmt(n int64) string {
	s := fmt.Sprintf("%d", n)
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	return string(out)
}
