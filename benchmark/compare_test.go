package benchmark

import (
	"testing"

	ds "github.com/example/rule-engine-demo/datasource"
	"github.com/example/rule-engine-demo/engine"
	"github.com/example/rule-engine-demo/model"
)

// BenchmarkBackends compares evaluation throughput of the custom bytecode VM
// against qlbridge's own VM on the same rules + users.
//
//	go test -bench=Backends -benchmem ./benchmark/
func BenchmarkBackends(b *testing.B) {
	gen := ds.NewGenerator(seed)
	rules := gen.Rules(2000)
	users := gen.Users(2000)

	backends := map[string]*engine.Engine{
		"bytecode_qlbridge": engine.NewWithBackend(engine.NewBytecodeBackend(engine.QLBridgeFrontend{})),
		"bytecode_native":   engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{})),
		"qlbridge_vm":       engine.NewWithBackend(engine.NewQLBridgeBackend()),
	}

	for name, eng := range backends {
		if _, failed := eng.LoadRules(rules); failed > 0 {
			b.Fatalf("%s: %d rules failed", name, failed)
		}
	}

	for name, eng := range backends {
		eng := eng
		b.Run(name, func(b *testing.B) {
			var stats *engine.Stats
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, stats = eng.RunBatch(users, 32)
			}
			b.StopTimer()
			if stats != nil {
				b.ReportMetric(stats.TPS(), "TPS")
				b.ReportMetric(stats.AvgLatencyMs(), "ms/user")
			}
		})
	}
}

// sanity: all backends should agree on matches for a fixed user/rule set.
func TestBackendsAgree(t *testing.T) {
	rules := []model.Rule{
		{ID: 1, Enabled: true, Expr: "age BETWEEN 25 AND 40 AND province IN ('广东','江苏')"},
		{ID: 2, Enabled: true, Expr: "active_score >= 90"},
	}
	users := []model.User{
		{UID: 1, Fields: map[string]any{"age": 30, "province": "广东", "active_score": 95.0}},
		{UID: 2, Fields: map[string]any{"age": 50, "province": "广东", "active_score": 80.0}},
	}

	byteEng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.QLBridgeFrontend{}))
	qlEng := engine.NewWithBackend(engine.NewQLBridgeBackend())
	byteEng.LoadRules(rules)
	qlEng.LoadRules(rules)

	for _, u := range users {
		a := byteEng.Match(u)
		c := qlEng.Match(u)
		if len(a) != len(c) {
			t.Errorf("uid %d: bytecode %v != qlbridge %v", u.UID, a, c)
		}
	}
}
