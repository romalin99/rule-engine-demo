# rule-engine-demo — High Performance Rule Engine for Go

> 多 DSL 输入 → 统一 IR → 统一字节码 VM，面向**实时风控 / 用户画像 / 营销圈选 / 推荐**
> 的高性能规则引擎。Multi-DSL in, one IR, one bytecode VM, wide-table batch matching.

![go](https://img.shields.io/badge/go-1.24-00ADD8) ![license](https://img.shields.io/badge/license-MIT-green) ![status](https://img.shields.io/badge/status-WIP-orange)

它不是“Aviator 的 Go 版”，而是一个**面向宽表实时计算**的规则引擎框架：规则可以来自
SQL / 表达式 / CEL / JSON，统一编译成字节码，由一个自研 VM 对 `map[string]any` 批量求值。

---

## ✨ Features

| 能力 | 状态 |
|------|------|
| DSL 输入：SQL(qlbridge)、SQL(自研)、JSON Rule | ✅ |
| DSL 输入：CEL、Expr | 🔶 扩展点已留（`frontend_stub.go`） |
| 算子：`= != > >= < <=`、`BETWEEN`、`IN`、`LIKE`、`AND/OR`、`IS [NOT] NULL` | ✅ |
| 统一 IR（`ir` 包） | ✅ |
| 自研字节码 VM（`vm` 包，零分配、并发安全） | ✅ |
| 可插拔后端：自研 VM ↔ qlbridge VM（可对比吞吐） | ✅ |
| 规则缓存 `sync.Map`（10 万规则） | ✅ |
| 宽表用户 `map[string]any`（100–500 字段，无反射） | ✅ |
| Worker Pool 批量匹配 + TPS/QPS/Latency | ✅ |
| 多 DSL 互转（SQL↔Aviator↔CEL↔Expr） | ✅ |
| 实时打分 HTTP 服务（Fiber v3） | ✅ |
| 热更新规则（增量 Add/Remove + 原子 Replace + 文件 Watcher） | ✅ |
| Decision Table 输入（`dtable`：行→IR→SQL→规则） | ✅ 基础版 |
| 后端 A/B 基准（自研 VM vs qlbridge VM） | ✅ |
| struct / Arrow 列式输入 | ⏳ Roadmap |

完整规划见 [ROADMAP.md](ROADMAP.md)。

---

## 🏗 Architecture

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

核心思想：**parser 只负责解析，求值永远走同一个字节码 VM**。qlbridge 在这里只是
front-end 之一，不参与业务求值。

---

## 🚀 Quick start

```bash
go mod tidy
go run .                 # 生成 1万规则 + 1万用户，32 worker，打印 TPS/QPS/Latency
go test ./...            # 单元测试（engine / vm / ir）
make bench               # 基准
```

`go run .` 输出（数值为真实测量）：

```
CPU=10 GOMAXPROCS=10
Backend=bytecode/qlbridge
加载10000条规则...
Rule Cache Ready. (compiled=10000 failed=0 in 120ms)
加载10000个用户...
开始匹配...
Worker=32
Total User=10,000
Rule=10,000
Hit=356,128
Eval=100,000,000
TPS=23,800,000
QPS=2,380
Latency=0.420ms
HitRate=0.3561%
```

---

## 📦 Library usage

```go
eng := engine.New() // 默认：自研字节码 VM + qlbridge 解析

eng.LoadRules([]model.Rule{
    {ID: 1001, Name: "VIP", Enabled: true,
     Expr: "age BETWEEN 25 AND 40 AND province IN ('广东','江苏') AND active_score >= 85"},
})

u := model.User{UID: 1, Fields: map[string]any{ // 宽表数据，来自 Kafka 等
    "age": 30, "province": "广东", "active_score": 96.0,
}}

ids := eng.Match(u)               // -> [1001]   命中的 RuleID
res := eng.MatchBatch(users, 32)  // 批量并发
```

可运行示例：`go run ./examples/quickstart`。

### 换一种 Parser，VM 完全不动

```go
engine.NewWithBackend(engine.NewBytecodeBackend(engine.NativeFrontend{})) // 自研 SQL parser
engine.NewWithBackend(engine.NewBytecodeBackend(engine.JSONFrontend{}))   // JSON Rule
// 以后实现 CELFrontend{} / ExprFrontend{}（见 frontend_stub.go）即可
```

JSON Rule 示例（见 `data/rules_json.json`）：

```json
{ "and": [
  {"field":"age","op":"between","values":[25,40]},
  {"field":"province","op":"in","values":["广东","江苏"]},
  {"field":"favorite_category","op":"like","value":"数%"},
  {"field":"active_score","op":">=","value":85}
]}
```

---

## 🔁 多 DSL 互转

`ir` 包可把一条规则发射成 SQL / Aviator / CEL / Expr（`go run . -export <dsl>`）：

```
SQL    : age BETWEEN 25 AND 40 AND favorite_category LIKE '数%'
CEL    : (age >= 25 && age <= 40) && favorite_category.startsWith('数')
Expr   : (age >= 25 && age <= 40) && hasPrefix(favorite_category, "数")
Aviator: (age >= 25 && age <= 40) && string.startsWith(favorite_category, '数')
```

---

## 🧰 CLI

```bash
go run . -workers 32 -gen-rules 10000 -gen-users 10000   # 基准
go run . -rules data/rules.json -users data/users.json   # 从文件加载
go run . -demo                                           # 命名 5 规则可读演示
go run . -serve :8080                                    # 实时打分 HTTP 服务
go run . -export cel -rules data/rules.json              # 规则转 DSL
go run . -frontend native                                # 换自研 SQL parser
go run . -backend qlbridge                               # 对照：qlbridge VM
go run . -serve :8080 -rules data/rules.json -watch      # 实时服务 + 规则热更新
go run . -gen-rules 1000000 -gen-users 1000000           # 百万级（需足够内存）
```

热更新 API（库内）：`AddRule` / `RemoveRule` 增量更新，`ReplaceRules` / `ReloadFromFile`
原子全量替换；`engine.Watcher` 轮询规则文件变更自动热加载。

后端 A/B 基准：`go test -bench=Backends ./internal/benchmark/`（自研 VM vs qlbridge VM）。

Decision Table（`dtable` 包）把决策表行编译成普通规则：

```go
tbl, _ := dtable.Load("data/decision_table.json")
rules, _ := tbl.Rules()      // 每行 → 一条 SQL 规则
eng.LoadRules(rules)
```

实时打分：

```bash
curl -s localhost:8080/match -d '{"uid":1,"age":28,"province":"广东","active_score":91.5,"favorite_category":"数码"}'
```

---

## 🧱 Project layout

```
.
├── main.go / cmd/api/main.go    # 入口（go run . / go run ./cmd/api）
├── pkg/                         # 公共库（可被外部项目 import）
│   ├── api/                     # SPI：Parser / Runtime 接口 + Program(=ir.Node)
│   ├── engine/                  # 引擎装配、缓存、worker pool、统计、HTTP、运营 API
│   │   ├── engine.go  backend.go    # Engine/Matcher 接口 + Backend 抽象
│   │   ├── spi.go                   # Parser+Runtime → Backend 适配（NewWithParserRuntime）
│   │   ├── manager.go               # 运营层：增/删/测试/发布/版本/回滚
│   │   ├── server.go                # Fiber v3：打分 API + 运营 API + Web 编辑器
│   │   ├── cache.go workerpool.go statistics.go loader.go reload.go
│   ├── parser/                  # 插件化 Parser（统一输出 ir.Node）
│   │   ├── qlbridge/ native/ json/  # 无额外依赖，开箱即用
│   │   └── expr/ cel/ vitess/        # 需外部依赖（go mod tidy）；vitess 为占位
│   ├── runtime/                 # 插件化 Runtime（执行 ir.Node）
│   │   ├── bytecode/ ast/            # 完整实现（字节码 VM / 树遍历）
│   │   └── cel/ expr/                # 占位（原生 VM 对照，待补依赖）
│   ├── vm/                      # 字节码 VM（求值核心，换 parser 不用动）
│   │   ├── opcode.go value.go compile.go vm.go
│   ├── ir/                      # IR 类型 + 自研 SQL parser + 多 DSL 发射
│   ├── model/                   # Rule / RuleProgram / User{UID,Fields} / Result
│   ├── web/                     # 运营控制台（单文件 HTML 规则编辑器）
│   └── dtable/                  # Decision Table 输入：行 → IR → SQL → 规则
├── internal/                    # 项目私有（不对外暴露）
│   ├── cli/                     # CLI / demo 编排（RunCLI / Run / Serve / Export / Demo）
│   ├── datasource/              # UserStore + 确定性数据生成
│   └── benchmark/               # go test -bench（TPS/QPS/Latency、worker 扩展性）
├── examples/quickstart/         # 库用法示例
└── data/                        # rules.json / users.json / rules_json.json / functions.json
```

---

## 🔌 插件化 Parser / Runtime（SPI）

统一接口在 `pkg/api`：`Parser.Parse(rule) → Program(=ir.Node)`，`Runtime.Compile/Execute`。
任意 Parser 与 Runtime 自由组合，引擎/缓存/worker pool 不变：

```go
import (
    "github.com/example/rule-engine-demo/pkg/engine"
    qp "github.com/example/rule-engine-demo/pkg/parser/qlbridge"
    bc "github.com/example/rule-engine-demo/pkg/runtime/bytecode"
)
eng := engine.NewWithParserRuntime(qp.New(), bc.New()) // 解析↔执行 自由替换
```

可用 Parser：`qlbridge` `native` `json`（开箱即用）、`expr` `cel`（需 `go mod tidy`）、
`vitess`（占位）。可用 Runtime：`bytecode` `ast`（完整）、`cel` `expr`（占位）。

## 🖥 运营控制台（热更新 + 版本/回滚）

`go run . -serve :8080` 后打开 <http://localhost:8080/>：在线编辑、**测试**、**发布（实时生效）**、
**快照版本**、**回滚**。对应 HTTP API：

```
GET  /rules/list            POST /rules            DELETE /rules/:id     # 增删查（实时生效）
POST /rules/test            # {"rule":"...","row":{...}} → {"matched":bool}
POST /versions              POST /versions/:v/rollback                    # 快照 / 回滚
```

> `expr` / `cel` 解析器与 `expr` / `cel` runtime 依赖外部库（`github.com/expr-lang/expr`、
> `github.com/google/cel-go`），已在 `go.mod` 声明 —— 首次使用请在本地执行 `go mod tidy`。

## 🗺 Roadmap

见 [ROADMAP.md](ROADMAP.md)：Vitess parser、CEL/Expr 前端、字节码短路跳转与跨规则谓词共享、
Decision Table、Arrow 列式批量求值等。

## 🤝 Contributing

见 [CONTRIBUTING.md](CONTRIBUTING.md)。新增一种规则语言只需实现 `engine.Frontend`。

## 📄 License

[MIT](LICENSE)
