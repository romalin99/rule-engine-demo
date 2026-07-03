# rule-engine-demo — High Performance Rule Engine for Go

> 多 DSL 输入 → 统一 IR → 统一字节码 VM，面向**实时风控 / 用户画像 / 营销圈选 / 推荐**
> 的高性能规则引擎。Multi-DSL in, one IR, one bytecode VM, wide-table batch matching.

![go](https://img.shields.io/badge/go-1.24-00ADD8) ![license](https://img.shields.io/badge/license-MIT-green) ![status](https://img.shields.io/badge/status-WIP-orange)

它不是“Aviator 的 Go 版”，而是一个**面向宽表实时计算**的规则引擎框架：规则可以来自
SQL / 表达式 / CEL / JSON，统一编译成字节码，由一个自研 VM 对 `map[string]any` 批量求值。

---

## ✨ Features

| 能力                                                                                                                     | 状态                                |
| ------------------------------------------------------------------------------------------------------------------------ | ----------------------------------- |
| DSL 输入：SQL(qlbridge)、SQL(自研)、JSON Rule                                                                            | ✅                                  |
| DSL 输入：CEL、Expr                                                                                                      | 🔶 扩展点已留（`frontend_stub.go`） |
| 算子：`= <> != > >= < <=`、`BETWEEN`（数值 + 字符串/日期区间 + 字段/函数边界）、`IN`/`NOT IN`、`LIKE`/`NOT LIKE`、`REGEXP`/`NOT REGEXP`、`AND/OR`、`NOT (…)`、`IS [NOT] NULL`、括号优先级 | ✅ 原生/JSON 前端全覆盖             |
| 函数：字符串 `LOWER/UPPER/TRIM/LENGTH/SUBSTRING`（2 或 3 参）、数学 `ABS/ROUND`（`ROUND(x)` 取整或 `ROUND(x,d)` 保留小数）`/CEIL/FLOOR`、日期 `CURRENT_DATE/CURRENT_TIMESTAMP`（可作比较任意一侧）`/YEAR/MONTH/DAY/DATEDIFF/DATE_ADD/DATE_SUB`、数组 `ARRAY_LENGTH/ARRAY_CONTAINS/ARRAY_OVERLAP/ARRAY_INTERSECT/ARRAY_MIN/ARRAY_MAX/ARRAY_DISTINCT/ARRAY_POSITION` | ✅ 原生前端 + bytecode/ast（见 [docs/functions.md](docs/functions.md)） |
| 集合判断：`EXISTS`、`ANY`/`ALL`（量词 + 行内子查询）、`IN (SELECT col FROM coll)`；标量聚合子查询 `(SELECT COUNT/SUM/MIN/MAX/AVG(...) FROM 集合字段 WHERE …)` | ✅ 原生前端 + bytecode/ast          |
| JSON 字段访问：`JSON_EXTRACT`/`JSON_VALUE`（`$.a.b[0]` 路径，字符串或已解析对象）                                        | ✅ 原生前端 + bytecode/ast          |
| 正则匹配：`REGEXP`/`RLIKE`/`REGEXP_LIKE`（Go RE2，加载期预编译）                                                         | ✅ 原生前端 + bytecode/ast          |
| 统一 IR（`ir` 包）                                                                                                       | ✅                                  |
| 自研字节码 VM（`vm` 包，零分配、并发安全）                                                                               | ✅                                  |
| 可插拔后端：自研 VM ↔ qlbridge VM（可对比吞吐）                                                                          | ✅                                  |
| 规则缓存 `sync.Map`（10 万规则）                                                                                         | ✅                                  |
| 宽表用户 `map[string]any`（100–500 字段，无反射）                                                                        | ✅                                  |
| Worker Pool 批量匹配 + TPS/QPS/Latency                                                                                   | ✅                                  |
| 多 DSL 互转（SQL↔Aviator↔CEL↔Expr）                                                                                      | ✅                                  |
| 实时打分 HTTP 服务（Fiber v3）                                                                                           | ✅                                  |
| 热更新规则（增量 Add/Remove + 原子 Replace + 文件 Watcher）                                                              | ✅                                  |
| Decision Table 输入（`dtable`：行→IR→SQL→规则）                                                                          | ✅ 基础版                           |
| 后端 A/B 基准（自研 VM vs qlbridge VM）                                                                                  | ✅                                  |
| struct / Arrow 列式输入                                                                                                  | ⏳ Roadmap                          |

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

> ⚠️ **前端 / 运行时能力并不对等**:7 类高级 SQL 特性(字符串/日期/数学/集合/数组/JSON/正则)
> 目前**只在 `bytecode` 与 `ast` 运行时上完整可用**,且只有 **native SQL 前端**能完整表达;
> `JSON / CEL / Expr` 前端与 `qlbridge / CEL / Expr` 运行时为基础谓词子集。完整支持矩阵见
> [docs/architecture.md](docs/architecture.md#支持矩阵-support-matrix) 与
> [docs/sql_feature_audit.md](docs/sql_feature_audit.md)。

---

## 🚀 Quick start

```bash
go mod tidy
go run  cmd/api/main.go                 # 生成 1万规则 + 1万用户，32 worker，打印 TPS/QPS/Latency
go test ./...            # 单元测试（engine / vm / ir）
make bench               # 基准
```

`go run  cmd/api/main.go` 输出（数值为真实测量）：

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
{
  "and": [
    { "field": "age", "op": "between", "values": [25, 40] },
    { "field": "province", "op": "in", "values": ["广东", "江苏"] },
    { "field": "favorite_category", "op": "like", "value": "数%" },
    { "field": "active_score", "op": ">=", "value": 85 }
  ]
}
```

JSON DSL 也支持取反算子 `not_in` / `not_like` 与 `{"not": <node>}` 包裹，
完整覆盖示例见 `data/rules_json_full.json`。

进阶谓词同样接入了 JSON 前端（与原生 SQL 前端等价，详见 `pkg/parser/json` 包注释）：

```json
{ "and": [
  { "field": "name", "op": "regexp", "value": "^A", "flags": "i" },
  { "field": "profile", "json": "$.city", "op": "=", "value": "深圳" },
  { "field": "score", "op": "<", "any": { "array": "scores" } },
  { "exists": { "coll": "orders", "where": { "field": "amount", "op": ">", "value": 100 } } },
  { "agg": { "fn": "SUM", "col": "amount", "from": "orders" }, "op": ">=", "value": 200 }
] }
```

即:`regexp`/`not_regexp`、`json`(JSON_EXTRACT 路径修饰)、`any`/`all`(数组/值列表/子查询量词)、
`exists`(非空或子查询)、`agg`(标量聚合子查询)。

---

## 🔁 多 DSL 互转

`ir` 包可把一条规则发射成 SQL / Aviator / CEL / Expr（`go run . -export <dsl>`）：

```
SQL    : age BETWEEN 25 AND 40 AND favorite_category LIKE '数%'
CEL    : (age >= 25 && age <= 40) && favorite_category.startsWith('数')
Expr   : (age >= 25 && age <= 40) && hasPrefix(favorite_category, "数")
Aviator: (age >= 25 && age <= 40) && string.startsWith(favorite_category, '数')
```

取反算子同样可发射到各 DSL，例如 `income_level NOT IN ('<5k','5k-10k') AND NOT (risk_level = '高')`：

```
SQL    : income_level NOT IN ('<5k', '5k-10k') AND NOT (risk_level = '高')
CEL    : !(income_level in ['<5k', '5k-10k']) && !(risk_level == '高')
Expr   : !(income_level in ["<5k", "5k-10k"]) && !(risk_level == "高")
Aviator: !((income_level == '<5k' || income_level == '5k-10k')) && !(risk_level == '高')
```

### 全算子覆盖 Demo

`data/rules.json` 的规则 #6 用一条规则覆盖了所有算子
（`=`/`<>`/`>`/`>=`/`<`/`<=`/`BETWEEN`/`IN`/`NOT IN`/`LIKE`/`NOT LIKE`/`AND`/`OR`/`NOT`/括号）。
它使用 `NOT IN` / `NOT LIKE` / `NOT (…)`，目前仅原生 SQL 与 JSON 前端可解析，
因此 `-demo` 已固定走原生前端：

```bash
go run  cmd/api/main.go -demo                 # data/rules.json（含规则 #6）对 data/users.json 实时匹配
go run ./examples/allops       # 解析→IR→字节码/树遍历双执行→四种 DSL 互转
go run  cmd/api/main.go -rules data/rules.json -users data/users.json -frontend native
```

> qlbridge 前端尚未映射取反 AST（`NOT IN`/`NOT LIKE`/`NOT(...)` 会被记为 `failed`，
> 非致命）；需要取反的规则请用 `-frontend native`。详见
> [ROADMAP.md](ROADMAP.md) 与 `pkg/parser/qlbridge` 包注释。

---

## 🧮 函数与表达式语法

规则在基础算子之外,原生 SQL 前端还支持**字符串 / 数学 / 日期 / 数组**函数,可用于
比较两侧以及 `BETWEEN`/`IN`/`LIKE`/`IS NULL` 的操作数。**完整参考与逐个用例见
[docs/functions.md](docs/functions.md)。**

| 类别   | 函数                                                              |
| ------ | ----------------------------------------------------------------- |
| 字符串 | `LOWER` `UPPER` `TRIM` `LENGTH` `SUBSTRING`/`SUBSTR`；扩展:`TOLOWER` `TOUPPER` `STRIP` `CHAR_LENGTH` `REPLACE` `SPLIT` `JOIN` `CONCAT` `STRING_INDEX` `TITLECASE`、谓词 `CONTAINS` `STARTSWITH`/`HASPREFIX` `ENDSWITH`/`HASSUFFIX` |
| 数学   | `ABS` `ROUND` `CEIL`/`CEILING` `FLOOR`；扩展:`SQRT` `POW`/`POWER`、转换 `TOINT` `TONUMBER` `TOBOOL` `TOSTRING` `UNSIGN`、`ONEOF`/`COALESCE`、函数式比较 `EQ/NE/GT/GE/LT/LE`、函数式聚合 `SUM/AVG/COUNT` |
| 日期   | `CURRENT_DATE` `CURRENT_TIMESTAMP` `YEAR` `MONTH` `DAY` `DATEDIFF` `DATE_ADD` `DATE_SUB`；扩展:`NOW()` `TODATE` `TOTIMESTAMP`/`UNIX_TIMESTAMP` `HOUR` `MINUTE` `SECOND` `DAYOFWEEK` `HOUROFDAY` `HOUROFWEEK` `MONTHOFYEAR` `YY` `MM` `YYMM` `SECONDS` `UNIXTRUNC` |
| 数组   | `ARRAY_LENGTH` `ARRAY_CONTAINS` `ARRAY_OVERLAP` `ARRAY_INTERSECT`；扩展:`ARRAY_INDEX` `ARRAY_SLICE`（可与 `SPLIT` 组合） |
| 集合   | `EXISTS` `ANY`/`SOME` `ALL`（量词 + 行内子查询 + **计算数组**：`tag = ANY(SPLIT(csv,','))`）；标量聚合 `COUNT/SUM/MIN/MAX/AVG` |
| JSON   | `JSON_EXTRACT`/`JSON_VALUE`（`$.a.b[0]` 路径，取标量）             |
| 正则   | `x REGEXP 'p'`/`RLIKE`、`REGEXP_LIKE(x,'p'[,match_type])`（RE2；标志 `i/c/m/n/u`，最右 i/c 生效） |
| 网络   | 扩展:`EMAIL` `EMAILNAME` `EMAILDOMAIN`、`HOST` `DOMAIN` `PATH`/`URLPATH` `QS`/`QS2` `URLDECODE` `URLMAIN` `URLMINUSQS` `URL_MATCHQS` `DOMAINS` `HOSTS`、`USERAGENT` |
| 摘要   | 扩展:`MD5` `SHA1` `SHA256` `SHA512`（及 `HASH_*` 别名）、`HASH`/`HASH_SIP`/`SIPHASH`、`B64ENCODE` `B64DECODE` |
| 其他   | 扩展:`CAST(x AS 类型)`、`MATCH('前缀')` 行级字段匹配、`MAPKEYS`/`MAPVALUES`、`JMESPATH`（完整 JMESPath 查询）、`TODATEIN`（时区）、`STRFTIME`/`EXTRACT` |

> “扩展”指与 [qlbridge](https://github.com/araddon/qlbridge) 内置计算因子对齐的函数库
> （`pkg/sqlfn`，bytecode 与 ast 两套运行时同实现），见 [docs/functions.md §5.8](docs/functions.md)。
>
> 安全：规则文本与行数据均按不可信输入处理——解析深度上限、求值 panic 遏制（`EvalPanics()`
> 可观测）、RE2 无回溯正则、缓存有界、fail-safe 语义。逐特性风险矩阵见
> [docs/security_review.md](docs/security_review.md)。

```sql
LOWER(name) = 'abc'                         -- 字符串
ABS(delta) <= 5                             -- 数学
LENGTH(nickname) BETWEEN 2 AND 12           -- 函数用作 BETWEEN 操作数
UPPER(city_code) IN ('BJ','SH','GZ')        -- 函数用作 IN 操作数
YEAR(birthday) = 2000                       -- 日期：提取年份
DATEDIFF(CURRENT_DATE, last_login) <= 30    -- 日期：整天差
DATE_SUB(CURRENT_DATE, 30) <= last_login    -- 日期：加减（近 30 天）
ARRAY_CONTAINS(tags, '高价值')               -- 数组：包含
ARRAY_LENGTH(tags) >= 2                     -- 数组：长度
score = ANY(scores)                         -- 集合：量词（数组/列表）
EXISTS(SELECT 1 FROM orders WHERE amount > 100)  -- 集合：行内子查询
budget >= ALL(SELECT amount FROM orders WHERE status = '已付')
(SELECT COUNT(*) FROM orders WHERE amount > 100) >= 2  -- 集合：标量聚合子查询
budget > (SELECT SUM(amount) FROM orders)    -- 聚合作右操作数
JSON_EXTRACT(profile, '$.city') = '深圳'     -- JSON：字段访问
phone REGEXP '^139[0-9]{8}$'                -- 正则：RE2 匹配
```

要点（详见文档）：

- 日期按 **ISO 字符串**处理（字典序＝时间序）；数组字段为 `[]string` / `[]any`，数组参数须为字段。
- **两值逻辑**：任意一侧为 NULL 的比较判 `false`；`NOT (field = v)` 在 field 缺失时为 `true`
  —— 要求字段存在请显式 `field IS NOT NULL`。
- 暂不支持：算术运算符 `+ - * /`（日期加减改用 `DATE_ADD`/`DATE_SUB`）、`CASE WHEN`、跨表 JOIN / 外部数据源子查询；`LIKE` 仅 `%`（需完整模式用 `REGEXP`）；`ROUND` 仅单参。`EXISTS`/`ANY`/`ALL` 仅作用于**行内集合字段**；`JSON_EXTRACT` 仅取标量叶子；正则为 **RE2**（无反向引用/环视）。
- 双运行时（`bytecode` / `ast`）语义一致，有交叉校验测试保证。

**可运行示例**：`data/rules_functions.json` + `data/users_functions.json`
（`go run ./cmd/api -rules data/rules_functions.json -users data/users_functions.json -frontend native`）。
完整参考另有英文版 [docs/functions_en.md](docs/functions_en.md)。

---

## 🧰 CLI

```bash
go run  cmd/api/main.go -workers 32 -gen-rules 10000 -gen-users 10000   # 基准
go run  cmd/api/main.go -rules data/rules.json -users data/users.json   # 从文件加载
go run  cmd/api/main.go -demo                                           # 命名 5 规则可读演示
go run  cmd/api/main.go -serve :8080                                    # 实时打分 HTTP 服务
go run  cmd/api/main.go -export cel -rules data/rules.json              # 规则转 DSL
go run  cmd/api/main.go -frontend native                                # 换自研 SQL parser
go run  cmd/api/main.go -backend qlbridge                               # 对照：qlbridge VM
go run  cmd/api/main.go -serve :8080 -rules data/rules.json -watch      # 实时服务 + 规则热更新
go run  cmd/api/main.go -serve :8080 -rules data/rules_60.json          # 60 条示例规则（/evaluate/all 演示）
go run  cmd/api/main.go -gen-rules 1000000 -gen-users 1000000           # 百万级（需足够内存）
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
│   ├── engine/                  # 引擎装配、缓存、worker pool、统计、运营 API
│   │   ├── engine.go  backend.go    # Engine/Matcher 接口 + Backend 抽象
│   │   ├── spi.go                   # Parser+Runtime → Backend 适配（NewWithParserRuntime）
│   │   ├── manager.go               # 运营层：增/删/测试/发布/版本/回滚
│   │   ├── cache.go workerpool.go statistics.go loader.go reload.go limiter.go
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
│   ├── router/                  # Fiber v3 HTTP 路由：打分 API + 运营 API + Web 编辑器（routes.go）
│   ├── datasource/              # UserStore + 确定性数据生成
│   └── benchmark/               # go test -bench（TPS/QPS/Latency、worker 扩展性）
├── examples/quickstart/         # 库用法示例
├── examples/allops/             # 全算子覆盖示例（解析→IR→VM/AST→多 DSL）
└── data/                        # rules.json / users.json / rules_json*.json / functions.json
```

---

## 🔌 插件化 Parser / Runtime（SPI）

统一接口在 `pkg/api`：`Parser.Parse(rule) → Program(=ir.Node)`，`Runtime.Compile/Execute`。
任意 Parser 与 Runtime 自由组合，引擎/缓存/worker pool 不变：

```go
import (
    "tcg-rulex-engine/pkg/engine"
    qp "tcg-rulex-engine/pkg/parser/qlbridge"
    bc "tcg-rulex-engine/pkg/runtime/bytecode"
)
eng := engine.NewWithParserRuntime(qp.New(), bc.New()) // 解析↔执行 自由替换
```

可用 Parser：`qlbridge` `native` `json`（开箱即用）、`expr` `cel`（需 `go mod tidy`）、
`vitess`（占位）。可用 Runtime：`bytecode` `ast`（完整）、`cel` `expr`（占位）。

## 🖥 运营控制台（热更新 + 版本/回滚）

`go run cmd/api/main.go -serve :8080` 后打开 <http://localhost:8080/>：在线编辑、**测试**、**发布（实时生效）**、
**快照版本**、**回滚**。对应 HTTP API：

```
GET  /rules/list            POST /rules            DELETE /rules/:id     # 增删查（实时生效）
POST /rules/test            # {"rule":"...","row":{...}} → {"matched":bool}
POST /rules/selftest        # 复杂规则“加→命中→删”实时生效自检（见下）
POST /evaluate              # 宽表用户 → {passed, reasons[]}（单条规则树，见下）
POST /evaluate/all          # 宽表用户 → 对【全部规则】求值 {passed, failed_rule_ids, failed[]}（见下）
POST /versions              POST /versions/:v/rollback                    # 快照 / 回滚
```

> `-serve` 默认走原生 SQL 前端，支持全部算子（含 `=`/`<>`/`>`/`>=`/`<`/`<=`/`BETWEEN`/`IN`/`NOT IN`/`LIKE`/`NOT LIKE`/`IS NULL`/`IS NOT NULL`/`AND`/`OR`/`NOT``NOT(...)`/`括号优先级`/`逻辑运算`/`多层逻辑组合`/`其它等`），
> 因此上述实时增删与 `/evaluate` 可直接使用旗舰规则。

### POST /evaluate — 是否通过规则树(规则引擎) + 未通过原因

传入宽表用户数据，返回是否通过某条规则树；未通过时逐条给出失败原因（含实际取值）。
缺省评估全算子旗舰规则，也可传 `rule`（SQL）或已加载规则的 `rule_id`：

```bash
curl -s localhost:8080/evaluate -H 'Content-Type: application/json' -d '{
  "row": {"age":21,"province":"江苏","income_level":"<5k","favorite_category":"图书",
          "occupation":"在校学生","active_score":70,"credit_score":660,
          "risk_level":"高","marital_status":"未知"}
}'
# -> {"passed":false,"reasons":[
#      {"expr":"age BETWEEN 25 AND 40","detail":"age=21 is outside [25, 40]"},
#      {"expr":"income_level NOT IN ('<5k', '5k-10k')","detail":"income_level=<5k is in the excluded set"},
#      {"expr":"occupation NOT LIKE '%学生%'","detail":"occupation=在校学生 matches the excluded pattern '%学生%'"},
#      {"expr":"last_login_time IS NOT NULL","detail":"last_login_time is missing or null (IS NOT NULL required)"},
#      {"expr":"NOT (risk_level = '高')","detail":"the negated condition was satisfied"}, ... ],
#     "rule":"age BETWEEN 25 AND 40 AND ..."}
```

通过时 `passed=true` 且 `reasons` 为空数组。

### POST /evaluate/all — 对【全部已加载规则】评估（必须全部命中才通过）

传入一个宽表用户，对引擎当前加载的**所有规则**逐条求值。语义为「必须全部命中才算通过」：
仅当用户命中全部规则时 `passed=true`；未通过时返回未命中规则的 ID 列表 `failed_rule_ids`，
以及每条未命中规则的**完整 SQL** 与失败谓词原因 `failed[].reasons`。配套 60 条示例规则
（40 条复杂，含旗舰全算子规则）见 `data/rules_60.json`：

```bash
go run cmd/api/main.go -serve :8080 -rules data/rules_60.json    # 加载 60 条规则
curl -s localhost:8080/evaluate/all -H 'Content-Type: application/json' -d '{
  "row": {"age":21,"province":"江苏","income_level":"<5k","favorite_category":"图书",
          "occupation":"在校学生","active_score":70,"credit_score":660,"total_amount":1200,
          "avg_order_amount":1500,"risk_level":"高","marital_status":"未知","register_days":30}
}'
# -> {"passed":false,"total_rules":60,"passed_count":0,"failed_count":60,
#     "failed_rule_ids":[1001,1002, ... ],
#     "failed":[{"rule_id":1001,"name":"全算子覆盖-精准圈选(旗舰)",
#                "rule":"age BETWEEN 25 AND 40 AND ... AND marital_status <> '未知'",
#                "reasons":[{"expr":"age BETWEEN 25 AND 40","detail":"age=21 is outside [25, 40]"}, ... ]}, ... ]}
```

命中全部规则时 `passed=true` 且 `failed` 为空数组。

### POST /rules/selftest — 复杂规则热更新自检

验证“添加一条复杂规则 + 删除规则”实时生效：对样本用户先打分、加规则后再打分（应命中）、
删规则后再打分（命中消失），返回每一步轨迹与两个布尔证明。`rule` / `row` 可选（缺省用旗舰规则与样本用户）：

```bash
curl -s localhost:8080/rules/selftest -d '{}'
# -> {"live_add_ok":true,"live_remove_ok":true,"added_rule_id":990001,
#     "steps":[{"step":"before","rules":N,"matched":[...]},
#              {"step":"after_add","rules":N+1,"matched":[...,990001]},
#              {"step":"after_remove","rules":N,"matched":[...]}], ... }
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
