# Roadmap

Phased plan; see the repo-root [ROADMAP.md](../ROADMAP.md) for the original
checklist. Status reflects the current tree.

## Phase 1 — Basic engine (v0.1.0) ✅

Engine/Matcher interface, Rule/User models, rule & JSON loaders, `sync.Map` rule
cache, logger (`pkg/logs`), config (`config/*.toml`), worker-pool batch match,
and benchmarks.

## Phase 2 — Multi-parser (v0.2.0) ✅ / 🔶

Unified `Parser` SPI with qlbridge / native / json built in (✅) and expr / cel
parsers plus a vitess stub (🔶, require `go mod tidy`). All emit the same IR.

## Phase 3 — Unified IR + optimizer (v0.3.0) ✅ / 🔶

`pkg/ir` is the single tree shape; `ir.Optimize` flattens/collapses/dedups before
bytecode lowering (✅). Planned: constant folding, cross-rule predicate sharing,
short-circuit jumps in the VM (🔶).

## Phase 4 — Production (v0.4.0) 🔶

Done: REST API (Fiber v3), hot reload, rule version + rollback, web rule
playground/editor, `/metrics` (Prometheus text), local data loading.

Planned (require external deps / integration work):

- Rule Debugger (step-through IR/bytecode with per-node trace)
- Native OpenTelemetry tracing + richer Prometheus metrics (`client_golang`)
- gRPC API (alongside REST)
- Streaming sources: Kafka, Flink, Redis Streams
- CEL / Expr **native** runtimes (currently stubs)

## Phase 5 — Scale (v0.5.0) ✅

Bounded concurrency for rule judgment: `engine.MaxConcurrentUsers = 200` with a
counting `Semaphore` (`pkg/engine/limiter.go`). The online server queues excess
requests so at most 200 users are judged concurrently, and
`Engine.MatchBatchBounded` caps batch worker fan-out to the same ceiling.
`/metrics` exposes `rule_engine_inflight` and `rule_engine_max_concurrency`.
Million-scale batch remains parameterized via `-gen-rules`/`-gen-users`.

## Docs & book

`docs/` covers architecture, parser, runtime, ir, bytecode, benchmark. A future
`book/` (mdBook `book.toml`) can publish these as an online site.
