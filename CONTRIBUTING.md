# Contributing

Thanks for your interest! This project is a high-performance, multi-DSL rule
engine for Go. Contributions of all sizes are welcome.

## Development

```bash
go test ./...          # unit tests (engine, vm, ir)
go vet ./...
gofmt -s -w .
make bench             # benchmarks
```

## Adding a new rule language (front-end)

The engine is intentionally split so a new DSL only needs a parser:

```
Rule text ──Frontend.Parse──▶ ir.Node ──vm.Compile──▶ ByteCode ──VM──▶ bool
```

1. Implement `engine.Frontend`:

   ```go
   type MyFrontend struct{}
   func (MyFrontend) Name() string { return "mine" }
   func (MyFrontend) Parse(rule string) (ir.Node, error) { /* parse → ir.Node */ }
   ```

2. Return one of the IR nodes in package `ir` (`Logic`, `Compare`, `Between`,
   `In`, `Like`, `IsNull`). The VM and all business code stay unchanged.
3. Add a test that cross-checks your front-end against an existing one
   (see `engine.TestFrontendsAgree`).

## Adding a VM opcode

Add the constant in `pkg/vm/opcode.go`, emit it in `pkg/vm/compile.go`, and handle it in
the `pkg/vm/vm.go` interpreter loop. Keep the interpreter allocation-free.

## Guidelines

- Keep the hot path (VM `Eval`) free of heap allocations and reflection.
- Every new operator/feature needs a unit test.
- Run `go vet` and `gofmt -s` before opening a PR.
