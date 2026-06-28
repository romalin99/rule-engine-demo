# Runtimes

A runtime compiles a `Program` (IR) into a reusable plan once, then executes
that plan against each row. The contract lives in `pkg/api`, re-exported as
`runtime.Runtime`:

```go
type Runtime interface {
    Name() string
    Compile(program Program) (Plan, error)          // once, at load
    Execute(plan Plan, row map[string]any) (bool, error) // hot path
}
```

`Compile` is called once per rule (its result is cached); `Execute` is the hot
path and must be concurrency-safe.

## Implementations

| Package                | Name       | Engine                         | Status   |
| ---------------------- | ---------- | ------------------------------ | -------- |
| `pkg/runtime/bytecode` | `bytecode` | custom stack VM (`pkg/vm`)     | complete |
| `pkg/runtime/ast`      | `ast`      | IR tree-walk interpreter       | complete |
| `pkg/runtime/qlbridge` | `qlbridge` | qlbridge VM (IR→SQL→eval)      | complete |
| `pkg/runtime/cel`      | `cel`      | cel-go VM                      | stub     |
| `pkg/runtime/expr`     | `expr`     | expr-lang VM                   | stub     |

`bytecode` is the default and fastest (zero-allocation eval). `ast` is a simpler
reference/baseline. `qlbridge` exists to A/B-benchmark the custom VM against
qlbridge on identical rules. The `bytecode` runtime runs `ir.Optimize` before
lowering; see [bytecode.md](bytecode.md) and [ir.md](ir.md).

## HTTP surface (`engine.Serve`, Fiber v3)

```
GET  /healthz                      liveness + rule count
GET  /metrics                      Prometheus text exposition
POST /match            /match/batch  scoring
GET  /                             web rule editor (operations console)
GET  /rules/list   POST /rules   DELETE /rules/:id   POST /rules/test
GET  /versions     POST /versions      POST /versions/:v/rollback
```

Edits via `/rules` recompile the single changed rule and atomically rebuild the
match snapshot — live, no restart. See the operations Manager in
`pkg/engine/manager.go`.
