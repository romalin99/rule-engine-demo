# Parsers

A parser turns one rule's text into the unified IR. The contract lives in
`pkg/api` and is re-exported as `parser.Parser`:

```go
type Parser interface {
    Name() string
    Parse(rule string) (Program, error) // Program == ir.Node
}
```

Every parser emits the **same** `ir.Node`, so the optimizer, bytecode compiler
and runtimes are identical regardless of source language.

## Built-in (no extra dependencies)

| Package              | Name         | Input                                            |
| -------------------- | ------------ | ------------------------------------------------ |
| `pkg/parser/qlbridge`| `qlbridge`   | SQL-WHERE via the qlbridge parser                |
| `pkg/parser/native`  | `native-sql` | SQL-WHERE via the project's own lexer/parser     |
| `pkg/parser/json`    | `json`       | JSON rule document (UI-friendly)                 |

## Pluggable (require `go mod tidy`)

| Package             | Name     | Library                          | Status     |
| ------------------- | -------- | -------------------------------- | ---------- |
| `pkg/parser/expr`   | `expr`   | `github.com/expr-lang/expr`      | best-effort|
| `pkg/parser/cel`    | `cel`    | `github.com/google/cel-go`       | best-effort|
| `pkg/parser/vitess` | `vitess` | `vitess.io/vitess/.../sqlparser` | stub       |

The `expr` and `cel` parsers walk the library AST into `ir.Node`; verify against
your pinned library version. `vitess` is a documented stub (see its package doc).

## Registry

`parser.Register(name, ctor)` / `parser.New(name)` provide string-keyed
selection (e.g. from config). Dependency-heavy parsers are registered explicitly
by the caller so their dependencies stay opt-in.

## Adding a parser

Implement `api.Parser`, converting your AST into `ir.Node` (see
`pkg/parser/qlbridge` as the reference). Nothing else in the pipeline changes.
