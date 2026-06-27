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
- [x] CEL front-end (`pkg/parser/cel`, `github.com/google/cel-go`) — wired via `engine.CELFrontend`
- [x] Expr front-end (`pkg/parser/expr`, `github.com/expr-lang/expr`) — wired via `engine.ExprFrontend`
- [ ] Vitess SQL parser front-end — `pkg/parser/vitess` is a documented stub (needs `vitess.io/vitess` sqlparser)

## Phase 3 — Self-built bytecode VM ✅ / 🔶

- [x] AST/IR → ByteCode compiler (`vm` package)
- [x] Stack VM over `map[string]any` (zero-alloc, concurrency-safe)
- [x] `Backend` abstraction: bytecode VM vs qlbridge VM (A/B benchmark)
- [x] Throughput comparison: bytecode vs qlbridge (`internal/benchmark/compare_test.go`)
- [x] CEL / Expr runtimes (`pkg/runtime/cel`, `pkg/runtime/expr`) — enables bytecode-vs-cel/expr A/B
- [x] Predicate dedup + logic flattening within a rule (`ir.Optimize`)
- [ ] Short-circuit jumps (`JMP_IF_FALSE`/`JMP_IF_TRUE`) — deferred: rewrites the postfix VM core; not safe to land without a compile/test loop
- [ ] Constant folding / predicate sharing **across** rules (rule index) — deferred: needs careful correctness work (OR / IS NULL)

## Productionization 🔶

- [x] Hot-reload: incremental `AddRule`/`RemoveRule`, atomic `ReplaceRules`,
      file `Watcher` (`-serve -rules f -watch`)
- [x] Decision Table input (`dtable`: rows → IR → SQL → rules)
- [x] `struct` record input (`engine.UserFromStruct` / `UsersFromStructs`, reflection-free JSON mapping)
- [x] Prometheus metrics (`/metrics`) + p50/p90/p99 online match latency
- [x] Bounded concurrency: ≤200 users judged at once (`engine.MaxConcurrentUsers`, Phase 5)
- [ ] Apache Arrow record input (columnar batch eval) — deferred: needs `apache/arrow` dependency
- [ ] Rule index / predicate sharing to skip evaluating unrelated rules — deferred (see Phase 3)

## Scale

Million-scale is parameterized today:

```bash
go run . -gen-rules 1000000 -gen-users 1000000 -workers 32   # needs RAM
```
