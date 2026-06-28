# Architecture

The engine turns rules in many languages into one intermediate representation,
optimizes it, lowers it to bytecode, and evaluates that bytecode against
wide-table users at high throughput.

```
多 DSL 输入            统一中间层              统一执行
SQL(qlbridge) ─┐
SQL(native)   ─┤  Parser     ir.Node    ir.Optimize     vm.Compile      ByteCode
JSON Rule     ─┼─► (parse) ──► (IR) ───► (optimize) ───► (lower) ─────► (Program)
Expr / CEL    ─┘                                                            │
                                                                            ▼
                                                              ┌────────────────────┐
   换 Parser，Runtime 不动                                     │  Runtime (bytecode) │
                                                              │  map[string]any→bool│
                                                              └────────────────────┘
```

## Layers

- **Parser** (`pkg/parser/*`, SPI in `pkg/api`) — rule text → `Program` (= `ir.Node`).
  qlbridge / native / json are built in; expr / cel / vitess are pluggable.
- **IR** (`pkg/ir`) — one tree shape for every language. See [ir.md](ir.md).
- **Optimizer** (`pkg/ir/optimize.go`) — semantics-preserving rewrites before lowering.
- **Runtime** (`pkg/runtime/*`, SPI in `pkg/api`) — executes a `Program`. `bytecode`
  (custom VM) and `ast` (tree-walk) are complete; `qlbridge` evaluates on the
  qlbridge VM for A/B comparison.
- **Engine** (`pkg/engine`) — assembly, rule cache, worker pool, statistics, HTTP,
  hot reload, and the operations Manager.

## Decoupling

The parser and the runtime never depend on each other or on the engine — they
meet only at `pkg/api` (`Parser`, `Runtime`, `Program`). Swap a parser and the
runtime is untouched; swap a runtime and parsers are untouched:

```go
eng := engine.NewWithParserRuntime(qlbridge.New(), bytecode.New())
```

## Request paths

- **Batch** — `MatchBatch([]User, workers)` fans users across a worker pool; rules
  are compiled once and shared read-only. See [benchmark.md](benchmark.md).
- **Online** — `engine.Serve` (Fiber v3) exposes `/match`, `/match/batch`, the rule
  management API, `/metrics`, and the web console. See [runtime.md](runtime.md).
