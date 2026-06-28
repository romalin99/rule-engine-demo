# Intermediate Representation (IR)

The IR (`pkg/ir`) is the single tree shape every parser emits and every runtime
consumes. It is the heart of the project: rule languages come and go, the IR and
the VM stay.

## Node types

```go
type Node interface{ node() }
```

| Node      | Meaning                                  |
| --------- | ---------------------------------------- |
| `Logic`   | `AND` / `OR` of N children               |
| `Compare` | `field <op> value` (`= != > >= < <=`)    |
| `Between` | `field BETWEEN lo AND hi`                |
| `In`      | `field IN (v1, v2, ...)`                 |
| `Like`    | `field LIKE 'pat%'`                      |
| `IsNull`  | `field IS [NOT] NULL`                    |

`Value` keeps a literal as either a string or raw numeric text, so original
formatting survives a round-trip through `Emit`.

## Emit (IR → DSL)

`ir.Emit(node, dsl)` renders an IR tree back to SQL / Aviator / CEL / Expr, and
`ir.Convert(ruleText, dsl)` parses then emits in one step. This powers multi-DSL
export and the `qlbridge` runtime (IR → SQL → qlbridge).

## Optimize (Phase 3)

`ir.Optimize(node)` applies semantics-preserving rewrites before lowering:

- **flatten** nested same-operator logic: `AND(a, AND(b, c)) → AND(a, b, c)`
- **collapse** single-child logic: `AND(a) → a`
- **dedup** identical predicates: `AND(a, b, a) → AND(a, b)`

These rely only on the associativity/idempotence of boolean AND/OR. The
`bytecode` runtime and the default engine backend call `Optimize` automatically.

Planned: constant folding and cross-rule predicate sharing (see [roadmap.md](roadmap.md)).
