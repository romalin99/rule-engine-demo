# Benchmarks

Benchmarks live in `internal/benchmark` and report throughput, latency and
allocations across rule/user/worker scales.

```bash
go test -bench=. -benchmem ./internal/benchmark/
```

## What's measured

| Benchmark                   | Varies                          | Reports                  |
| --------------------------- | ------------------------------- | ------------------------ |
| `BenchmarkCompile`          | rule compilation                | ns/op, allocs/op         |
| `BenchmarkMatchUser`        | single-user match               | ns/op, allocs/op         |
| `BenchmarkRunBatch`         | full batch                      | TPS, ms/user             |
| `BenchmarkWorkersScaling`   | workers 1→64                    | TPS vs worker count      |
| `BenchmarkRuleCountScaling` | rules 100/1000/5000/10000       | ns/op, allocs/op         |
| `BenchmarkBackends`         | bytecode VM vs qlbridge VM      | TPS, ms/user             |

`TPS` is rule-evaluations per second (`users × rules / elapsed`); `QPS` is
users-scored per second. The headline run is `go run  cmd/api/main.go` (10k rules × 10k users,
32 workers) which prints TPS / QPS / latency / hit-rate.

## Suggested matrix

```bash
# rules ∈ {100, 1000, 10000}, users = 10000
go test -bench=RunBatch -benchmem ./internal/benchmark/
# custom VM vs qlbridge on identical rules
go test -bench=Backends ./internal/benchmark/
# worker scaling (CPU utilisation)
go test -bench=WorkersScaling ./internal/benchmark/
```

For CPU/memory profiles add `-cpuprofile cpu.out -memprofile mem.out` and inspect
with `go tool pprof`. Live counters are also exposed at `GET /metrics`
(Prometheus text) when serving.
