# 多 DSL 输入详解 (Multi-DSL Input Front-ends)

> 对应 README 架构图「多 DSL 输入」一列。四类输入 DSL 各由一个 **Frontend** 解析成
> **同一个 `ir.Node`**,再经 `vm.Compile` 降为字节码、由同一个 VM 求值——**换 Parser,
> VM 不动**。本文逐一详解:机制、能力边界、回退、安全界、选择方式与"同一规则四种写法"。

```
   多 DSL 输入                     统一中间层                 统一执行
┌──────────────┐
│ SQL(qlbridge)│─┐
│ SQL(native)  │ │   Frontend       ir.Node      vm.Compile     ByteCode
│ JSON Rule    │ ├──► (parser) ───►  (IR)  ─────────────────►  (Program)
│ CEL / Expr   │ │                                                 │
└──────────────┘─┘                                                 ▼
                                                        ┌────────────────────┐
   换 Parser，VM 不动                                    │  Bytecode VM        │
                                                        │  map[string]any→bool│
                                                        └────────────────────┘
```

**Frontend 契约**(`pkg/engine/frontend.go`):

```go
type Frontend interface {
    Name() string
    Parse(rule string) (ir.Node, error) // 规则文本 → 统一 IR
}
```

**选择方式**(二选一):

```go
// 代码:构造引擎时注入前端
eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
//                                                     ^^^^^^^^^^^^^^^^^^^^^^^
//   QLBridgeFrontend{} / NativeFrontend{} / JSONFrontend{} / CELFrontend{} / ExprFrontend{}
```

```bash
# CLI:-frontend 选择   (默认 qlbridge)
go run ./cmd/cli -frontend native  -rules data/rules.json
go run ./cmd/cli -frontend json    -rules data/rules_json.json
go run ./cmd/cli -frontend cel      ...
go run ./cmd/cli -frontend expr     ...
```

## 能力总览(先看这张表)

| 输入 DSL | 源码 | 表达力 | 典型用途 |
|----------|------|--------|----------|
| **SQL(native)** | `pkg/ir`(自研词法/语法) | **全部 7 类高级特性**(函数/日期/集合/数组/JSON/正则/字符串)+ 负数/字符串区间 BETWEEN | 线上服务与 CLI 的**权威前端**,能力最全 |
| **SQL(qlbridge)** | `pkg/engine/frontend_qlbridge.go` | 基础子集直译;**无法解析/转换的构造自动回退 native** ⇒ 等价全覆盖 | 兼容既有 qlbridge SQL 习惯;默认前端 |
| **JSON Rule** | `pkg/engine/frontend_json.go` | 基础谓词子集(逻辑/比较/BETWEEN/IN/LIKE/IS NULL) | UI 可视化拖拽、机器生成规则 |
| **CEL / Expr** | `pkg/parser/cel`、`pkg/parser/expr` | 基础子集(逻辑/比较/IN/前后缀包含) | 已有 CEL/Expr 规则资产的接入 |

> **关键**:四者产出**同一 IR**,但**表达力不对等**。native 是全集;qlbridge 通过回退达到等价全集;
> JSON 与 CEL/Expr 是**基础谓词子集**——想用函数/正则/JSON/集合判断,请用 native(或 qlbridge,它会回退到 native)。

---

## 1. SQL(qlbridge) —— 借第三方 SQL 解析器,不达则回退

**是什么**:把规则当作 SQL-WHERE 文本,先交给 [qlbridge](https://github.com/araddon/qlbridge)
的 `expr.ParseExpression` 解析成它的 AST,再由 `qlToIR` 转成本项目 IR。**qlbridge 只当解析器,
不参与求值**。

**机制**(`pkg/engine/frontend_qlbridge.go`):

```
规则(SQL) ─qlbridge.ParseExpression─▶ qlbridge AST ─qlToIR─▶ ir.Node
                    │ 解析失败                  │ 遇不支持的节点
                    └──────────┬───────────────┘
                               ▼
                         native ir.Parse(rule)   ← 透明回退(native 是严格超集)
```

`qlToIR` **直接支持**的节点:`AND`/`OR`(BooleanNode/BinaryNode)、比较
`= == != > >= < <=`、`IN (...)`、`LIKE '...'`、**数值** `BETWEEN`。遇到函数、`IS NULL`、
`NOT (...)`、`EXISTS`/`ANY`/`ALL`、`REGEXP`、`JSON_EXTRACT`、数组谓词等——qlbridge 要么解析报错、
要么转换器不认——**一律透明回退到 native `ir.Parse`**(native 能解析全部,见下一节)。

**安全**:qlbridge 是第三方无维护代码,其解析器对畸形输入可能 panic。`Parse` 用 `safeQLParse`
**隔离 panic → 转为错误 → 走 native 回退**,不会打崩进程。

**用途 / 选择**:引擎默认前端(`engine.New()` 用它)。适合"已有一堆 qlbridge 风格 SQL、又想用到
本引擎的高级特性"的场景——基础规则走 qlbridge 快路径,高级规则自动落到 native。

```go
eng := engine.New() // 等价于 NewWithBackend(NewBytecodeBackend(QLBridgeFrontend{}))
_ = eng.AddRule(model.Rule{ID: 1, Expr: "age >= 18 AND city IN ('深圳','广州')", Enabled: true})
// 高级规则也能加:qlbridge 认不了 LOWER(...),自动回退 native 解析
_ = eng.AddRule(model.Rule{ID: 2, Expr: "LOWER(name) = 'vip'", Enabled: true})
```

---

## 2. SQL(native) —— 自研全功能 SQL 解析器(能力最全)

**是什么**:项目自带的 SQL-WHERE 词法器 + 递归下降语法器(`pkg/ir/lexer.go`、`parser.go`),
零第三方依赖。它是**能力最全的前端**,也是线上服务与 CLI 实际使用的权威前端。

**机制**(`pkg/engine/frontend.go`):

```go
func (NativeFrontend) Parse(rule string) (ir.Node, error) { return ir.Parse(rule) }
```

**覆盖 7 类高级特性 + 更多**:

| 类别 | 示例 |
|------|------|
| 字符串函数 | `LOWER/UPPER/TRIM/LENGTH/SUBSTRING` 及扩展 `REPLACE/SPLIT/CONCAT/...` |
| 日期 | `CURRENT_DATE`、`DATE_ADD(d,7)`、`YEAR(x)`、`DATEDIFF(a,b)`、`>=`/`<=` |
| 集合判断 | `EXISTS(coll)`、`x = ANY(SELECT ... )`、`x < ALL(arr)`、聚合子查询 |
| 数学 | `ABS/ROUND(x,2)/CEIL/FLOOR/SQRT/POW` |
| 数组 | `ARRAY_CONTAINS/ARRAY_INTERSECT/ARRAY_LENGTH`、`ANY(SPLIT(csv,','))` |
| JSON | `JSON_EXTRACT(profile,'$.city') = '深圳'` |
| 正则 | `phone REGEXP '^1\d{10}$'`、`REGEXP_LIKE(x,'p','i')` |
| 其它 | 负数字面量、字符串/日期区间 `BETWEEN`、`IS [NOT] NULL`、`NOT(...)`、括号优先级 |

**安全**:解析深度上限 `maxParseDepth = 200`(逻辑嵌套与函数嵌套共用),超限**加载期明确报错**,
杜绝深嵌套文本导致的栈溢出(Go 栈溢出不可恢复);字符串字面量仅转义 `\' \" \\`,正则字符类
(`\d \w`)原样保留。

**用途 / 选择**:需要任何高级特性时的首选。`internal/infra`(线上服务)、CLI `-frontend native` 用它。

```go
eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{}))
_ = eng.AddRule(model.Rule{ID: 1, Enabled: true,
    Expr: "age BETWEEN 25 AND 40 AND JSON_EXTRACT(profile,'$.city') = '深圳' AND phone REGEXP '^1\\d{10}$'"})
```

---

## 3. JSON Rule —— 机器友好、UI 可拖拽的结构化规则

**是什么**:把规则表达为**结构化 JSON 文档**(而非 SQL 文本),便于前端可视化搭建、程序化生成、
存库校验。编译成与 SQL 规则**完全相同的字节码**。

**机制**(`pkg/engine/frontend_json.go`):递归下降解析 `{"and"|"or"|"not"|<叶子>}`。

**语法**:

```jsonc
{"and": [ <node>, ... ]}                 // 与
{"or":  [ <node>, ... ]}                 // 或
{"not": <node>}                          // 非
{"field":"age","op":"between","values":[25,40]}
{"field":"city","op":"in","values":["深圳","广州"]}      // op 也可为 "not_in"
{"field":"name","op":"like","value":"数%"}               // op 也可为 "not_like"
{"field":"score","op":">=","value":85}
{"field":"phone","op":"isnull"}                          // 或 "isnotnull"
```

**支持的叶子 `op`**:`= == != <> > >= < <=`、`between`、`in`/`not_in`、`like`/`not_like`、
`isnull`/`isnotnull`。字符串/日期区间 `between` 会脱糖为 `>= AND <=`,与 SQL 语义对齐。

**不支持**(基础谓词子集):函数、`REGEXP`、`JSON_EXTRACT`、数组谓词、`EXISTS`/`ANY`/`ALL`。
需要这些请用 native SQL。

**安全**:嵌套深度上限 `maxJSONDepth = 200`,深嵌套 JSON 文档加载期报错(防栈溢出)。

**用途 / 选择**:规则由 UI 生成或存于配置系统。CLI `-frontend json`。

```go
eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.JSONFrontend{}))
_ = eng.AddRule(model.Rule{ID: 1, Enabled: true, Expr: `
{"and":[
  {"field":"age","op":">=","value":18},
  {"field":"city","op":"in","values":["深圳","广州"]}
]}`})
```

> **反向生成**:`ir.EmitJSON(node)` 可把 IR 反向导出为该 JSON 文档,实现 `SQL → JSON` 或
> `JSON → IR → JSON` 往返(仅覆盖上述基础子集;函数/正则等无 JSON 形式会报错)。见 CLI `-export json`。

---

## 4. CEL / Expr —— 接入已有表达式资产(基础子集)

**是什么**:两个基于成熟表达式库的前端——**CEL**(Google [cel-go](https://github.com/google/cel-go))
与 **Expr**([expr-lang/expr](https://github.com/expr-lang/expr))。它们把各自库的 AST 走成本项目 IR,
让用 CEL/Expr 写的既有规则直接接入。

**机制**(`pkg/engine/frontend_stub.go` 适配 → `pkg/parser/cel`、`pkg/parser/expr`):

```go
func (CELFrontend) Parse(rule string) (ir.Node, error)  { return celparser.New().Parse(rule) }
func (ExprFrontend) Parse(rule string) (ir.Node, error) { return exprparser.New().Parse(rule) }
```

**支持的形状(基础子集,两者一致)**:

| 写法 | → IR |
|------|------|
| `a && b` / `a \|\| b` | `Logic{AND/OR}` |
| `!expr` | `Not` |
| `field == v`(及 `!= < <= > >=`) | `Compare` |
| `field in [v1, v2, ...]` | `In` |
| `field.startsWith("x")` / `endsWith` / `contains`(CEL)、`hasPrefix(field,"x")` / `hasSuffix` / `contains`(Expr) | `Like`(`x%` / `%x` / `%x%`) |
| `field >= lo && field <= hi` | 自然表达 BETWEEN |

**不支持**:函数、正则、JSON、集合判断等高级特性(超出这两个库前端当前的转换范围)。属 best-effort,
建议对照所锁定的库版本验证。

**安全 / 依赖**:CEL/Expr 库版本已在 `go.sum` 锁定;本地首次启用后跑 `go mod tidy` 解析依赖。

**用途 / 选择**:已有 CEL/Expr 规则库需要迁移/共用时。CLI `-frontend cel` / `-frontend expr`。

```go
eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.CELFrontend{}))
_ = eng.AddRule(model.Rule{ID: 1, Enabled: true, Expr: `age >= 18 && city in ['深圳','广州']`})
```

---

## 同一条规则,四种输入写法(都编译成同一 IR、匹配结果一致)

逻辑:**`age ≥ 18` 且 `city ∈ {深圳, 广州}`**

| 输入 DSL | 规则文本 |
|----------|----------|
| SQL(native) | `age >= 18 AND city IN ('深圳','广州')` |
| SQL(qlbridge) | `age >= 18 AND city IN ('深圳','广州')`(同上,qlbridge 直译此基础规则) |
| JSON Rule | `{"and":[{"field":"age","op":">=","value":18},{"field":"city","op":"in","values":["深圳","广州"]}]}` |
| CEL | `age >= 18 && city in ['深圳','广州']` |
| Expr | `age >= 18 && city in ['深圳','广州']` |

```go
// 无论用哪个前端,对同一行数据匹配结果相同
row := model.User{UID: 1, Fields: map[string]any{"age": 25, "city": "深圳"}}
// eng.Match(row) => [1]   (命中)
```

**能力差异示例**:规则 `LOWER(name) = 'vip'`

| 输入 DSL | 结果 |
|----------|------|
| SQL(native) | ✅ 解析成功(函数由 native 支持) |
| SQL(qlbridge) | ✅ 成功——qlbridge 认不了函数,**透明回退 native** |
| JSON Rule | ❌ 无法表达(基础子集无函数) |
| CEL / Expr | ❌ 无法表达(当前子集无函数) |

---

## 可运行示例文件(每类一个,引擎直接解析)

`examples/rules/` 下按 DSL 各放一个规则文件,配套 `users.json`。规则文件加载走
`engine.LoadRulesAuto`(按扩展名分发):`.json` = 规则集数组;`.sql/.cel/.expr` = **每行一条**
规则表达式(`#` 注释、空行忽略、可选 `名称: 表达式`)。

| 文件 | 前端 | 内容 |
|------|------|------|
| [`examples/rules/native.sql`](../examples/rules/native.sql) | native | 7 类高级特性逐条演示(函数/日期/集合/数学/数组/JSON/正则)+ 基础谓词 |
| [`examples/rules/qlbridge.sql`](../examples/rules/qlbridge.sql) | qlbridge | 基础子集(qlbridge 直译)+ 回退演示区(函数/正则/JSON 等自动回退 native) |
| [`examples/rules/json_rule.json`](../examples/rules/json_rule.json) | json | 12 条结构化 JSON 规则(逻辑/比较/BETWEEN/IN/LIKE/IS NULL/嵌套 NOT) |
| [`examples/rules/cel.cel`](../examples/rules/cel.cel) | cel | CEL 基础子集(逻辑/比较/IN/startsWith·endsWith·contains) |
| [`examples/rules/expr.expr`](../examples/rules/expr.expr) | expr | Expr 基础子集(逻辑/比较/IN/hasPrefix·hasSuffix·contains) |

```bash
# 每个文件用【对应前端】加载并对样例用户批量匹配
go run ./cmd/cli -frontend native   -rules examples/rules/native.sql    -users examples/rules/users.json
go run ./cmd/cli -frontend qlbridge -rules examples/rules/qlbridge.sql  -users examples/rules/users.json
go run ./cmd/cli -frontend json     -rules examples/rules/json_rule.json -users examples/rules/users.json
go mod tidy   # CEL/Expr 首次需解析依赖
go run ./cmd/cli -frontend cel      -rules examples/rules/cel.cel        -users examples/rules/users.json
go run ./cmd/cli -frontend expr     -rules examples/rules/expr.expr      -users examples/rules/users.json
```

> 解析/编译可用性由 `pkg/engine/examples_test.go` 断言(每个文件用对应前端加载,零编译失败):
> `go test ./pkg/engine/ -run TestExampleRuleFiles -v`。

## 小结

- **四种输入 → 同一 `ir.Node` → 同一 VM**,是"换 Parser、VM 不动"的落点。
- 要**全部高级特性**(函数/日期/集合/数组/JSON/正则):用 **SQL(native)**,或 **SQL(qlbridge)**(自动回退到 native)。
- 要**机器生成 / UI 可视化**:用 **JSON Rule**(基础谓词子集)。
- 要**接入已有 CEL/Expr 资产**:用 **CEL / Expr**(基础子集)。
- 线上服务与 CLI 默认走 native 或 qlbridge(→native),故部署形态下 7 类特性端到端可用;JSON、CEL/Expr
  为基础谓词子集,按数据源/团队习惯选用。

> 相关文档:解析器契约与注册见 [parser.md](parser.md);函数与算子全表见 [functions.md](functions.md);
> 输出/生成侧(`ir.Emit`:SQL/Aviator/CEL/Expr/JSON)见 §5.8 与 `-export`。
