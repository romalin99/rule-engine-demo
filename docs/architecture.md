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
  (custom VM) and `ast` (tree-walk) are complete (all 7 feature categories);
  `qlbridge` / `cel` / `expr` re-emit the IR to their own DSL and run their native
  engine (A/B comparison) and cover only the basic subset — see the Support matrix below.
- **Engine** (`pkg/engine`) — assembly, rule cache, worker pool, statistics, HTTP,
  hot reload, and the operations Manager.

## "VM" 的两层含义 (Two senses of "VM")

"VM" 在本项目里出现在**两个方向相反的位置**,容易混淆:

- 作为**输入 DSL / 前端**:`CEL`、`Expr`、`SQL`、`JSON` 只是**解析器**,把规则文本
  变成统一 IR(`规则文本 → ir.Node`,`pkg/parser/*`)。
- 作为**执行运行时 / 后端 VM**:`bytecode`、`ast`、`qlbridge`、`cel`、`expr` 消费 IR
  求值(`ir.Node → bool`,`pkg/runtime/*`)。`qlbridge/cel/expr` 运行时是把 IR **发射回**
  该 DSL 文本、再交给其原生引擎执行(主要用于 A/B 跑分)。

上面的架构图画的是**主路径**:任意前端 → 统一 IR → **单一 bytecode VM**("换 Parser,
Runtime 不动")。`qlbridge/cel/expr` 作为备选运行时不在该主图内。

## 支持矩阵 (Support matrix)

**前端能产出多少 IR(各 DSL 的表达力)**:

| 前端 (DSL) | 覆盖 |
| ---------- | ---- |
| **SQL (native)** | **全部**:比较、`BETWEEN`(数值 + 字符串/日期)、`IN`、`LIKE`、`REGEXP`、`IS [NOT] NULL`、`NOT`、负数字面量、标量函数、`EXISTS/ANY/ALL`、`JSON_EXTRACT`、数组谓词 |
| **SQL (qlbridge)** | 自身转换器只做基础子集;引擎 `QLBridgeFrontend` 对无法解析/转换的构造**回退 native** → 等价全覆盖 |
| **JSON Rule** | 基础子集(`and/or/not/`比较`/between/in/not_in/like/not_like/isnull`) |
| **CEL / Expr** | 基础子集(实测仅 `Logic / Not / Compare / In / Like`) |

**运行时能求值多少 IR(各 VM 的执行力)**:

| 运行时 (VM) | 覆盖 | 机制 |
| ----------- | ---- | ---- |
| **bytecode**(自研,默认) | **全部 7 类特性** | IR → 字节码 → 栈式 VM |
| **ast**(参考实现) | **全部 7 类特性** | 直接遍历 IR |
| **qlbridge**(SQL VM) | 基础子集 + qlbridge 内建函数;`JSON_EXTRACT / 数组 / EXISTS / ANY/ALL /` 本项目 `REGEXP` 语义**不支持** | IR → `emitSQL` → qlbridge 解析 → qlbridge VM |
| **cel / expr**(Expression VM) | **仅基础子集**(`emitCode` 只发射 `Logic/Compare/Between/In/Like/IsNull/Not`) | IR → `emitCode(CEL/Expr)` → 原生引擎 |

> **结论**:7 类高级 SQL 特性(字符串/日期/数学/集合/数组/JSON/正则)目前**只在
> `bytecode` 与 `ast` 运行时上完整可用**,且只有 **native SQL 前端**能完整表达;
> `JSON / CEL / Expr` 前端与 `qlbridge / CEL / Expr` 运行时均为基础子集(见
> [sql_feature_audit.md](sql_feature_audit.md) §七)。线上 API / CLI 走的正是
> native 前端 + bytecode/ast,故部署形态下 7 类特性端到端可用。

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
