# Roadmap

Positioning: **a high-performance Rule Engine for Go** for real-time risk
control, user profiling, audience targeting and recommendation — multi-DSL
input, one IR, one bytecode VM, wide-table batch matching.

## Phase 1 — Engine core ✅

- [x] qlbridge-backed rule engine (qlbridge pinned + API verified)
- [x] Rule cache (`sync.Map`, designed for 100k rules)
- [x] Wide-table users (`map[string]any`, 100–500 fields)
- [x] Batch matching
- [x] Worker pool (channel + N workers)
- [x] Benchmarks (TPS / QPS / latency)
- [x] `BETWEEN`, `IN`, `LIKE`, `AND`/`OR`
- [x] `IS NULL` / `IS NOT NULL` (native frontend + VM)

## Phase 2 — Unified parser front-ends 🔶

- [x] `Frontend` interface — qlbridge is just one implementation
- [x] Native SQL parser front-end (`ir` package)
- [x] JSON Rule front-end
- [x] All front-ends emit the **same IR**
- [ ] Vitess SQL parser front-end
- [ ] CEL front-end (`github.com/google/cel-go`) — stub in `frontend_stub.go`
- [ ] Expr front-end (`github.com/expr-lang/expr`) — stub in `frontend_stub.go`

## Phase 3 — Self-built bytecode VM ✅ / 🔶

- [x] AST/IR → ByteCode compiler (`vm` package)
- [x] Stack VM over `map[string]any` (zero-alloc, concurrency-safe)
- [x] `Backend` abstraction: bytecode VM vs qlbridge VM (A/B benchmark)
- [x] Throughput comparison: bytecode vs qlbridge (`internal/benchmark/compare_test.go`)
- [ ] Comparison vs CEL / expr (needs those front-ends)
- [ ] Short-circuit jumps (`JMP_IF_FALSE`/`JMP_IF_TRUE`)
- [ ] Constant folding / predicate dedup across rules

## Productionization 🔶

- [x] Hot-reload: incremental `AddRule`/`RemoveRule`, atomic `ReplaceRules`,
      file `Watcher` (`-serve -rules f -watch`)
- [x] Decision Table input (`dtable`: rows → IR → SQL → rules)
- [ ] `struct` and Apache Arrow record inputs (columnar batch eval)
- [ ] Prometheus metrics, p50/p90/p99 latency
- [ ] Rule index / predicate sharing to skip evaluating unrelated rules

## Scale

Million-scale is parameterized today:

```bash
go run . -gen-rules 1000000 -gen-users 1000000 -workers 32   # needs RAM
```
