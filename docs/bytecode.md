w# Bytecode VM

The default runtime lowers the (optimized) IR to a compact stack bytecode
(`pkg/vm`) and evaluates it against `map[string]any` with no per-eval allocation.

```
Program(IR) ─ir.Optimize─▶ IR' ─vm.Compile─▶ *vm.Program ─Eval(row)─▶ bool
```

## Why bytecode

Tree-walking an AST chases pointers and boxes values on every node. Compiling to
a flat instruction stream once, then running a tight stack loop, is markedly
faster and GC-friendly — the hot path touches only a fixed-size local stack.

## Shape

- `pkg/vm/opcode.go` — instruction set (LOAD field, PUSH const, compare, BETWEEN,
  IN over a constant set, LIKE, logical combinators, RETURN).
- `pkg/vm/value.go` — a tagged union (`kUndef/kNum/kStr/kBool`); no interfaces,
  no reflection. Missing fields are `kUndef` and compare false rather than panic.
- `pkg/vm/compile.go` — `Compile(ir.Node) (*Program, error)` lowers IR to postfix.
- `pkg/vm/vm.go` — `(*Program).Eval(row)` runs the program on a local stack and is
  safe for concurrent use (no shared mutable state).

## Semantics

Numeric comparison when both operands are numeric, otherwise string comparison;
`LIKE` supports leading/trailing `%`; a field is NULL when absent or nil. The
`ast` runtime mirrors these rules so the two engines agree.

## Roadmap

Short-circuit jumps (`JMP_IF_FALSE`/`JMP_IF_TRUE`), constant folding, and
predicate dedup across rules — see [roadmap.md](roadmap.md).
