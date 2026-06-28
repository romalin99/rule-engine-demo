# 测试指南 (TESTING.md)

本项目全部测试用例的**作用、运行方式、预期结果与注解**。约 150 个测试函数 / 16 个文件，
分两类：**规则引擎核心**（`pkg/ir`、`pkg/vm`、`pkg/engine`、`pkg/dtable`、`internal/router`、
`internal/benchmark`）与**外围业务模块**（`internal/client/*`、`internal/model/*`）。

> 模块名：`tcg-rulex-engine`，Go `1.26.4`。所有命令在仓库根目录执行。

---

## 0. 前置条件 & 已知问题（先读）

1. **依赖**：CEL / Expr 前端依赖 `github.com/google/cel-go`、`github.com/expr-lang/expr`
   （已在 `go.mod`）。首次运行先 `go mod tidy`。
2. **构建标签**：`internal/client/{mcs,uss,wps}` 的测试带 `//go:build test`，**默认 `go test ./...` 不会执行**，需加 `-tags test`。
3. **已知 stale 测试 / 需修复项**：
   - `pkg/engine/frontend_json_test.go: TestStubFrontends` —— 断言 `CELFrontend/ExprFrontend.Parse` 返回「未实现」错误，但 `pkg/parser/cel`、`pkg/parser/expr` **已是真实实现**，因此该用例**当前会失败**。修复方式二选一：改为断言解析成功，或删除该用例（详见 §3.3 注解）。
   - `internal/client/wps/client_test.go` —— 曾 import 外部模块 `tcg-rulex-engine/internal/client/clienthttp`（同目录 mcs/uss 用的是 `tcg-rulex-engine/...`），导致 `-tags test` 构建失败。**已在本次修正**为本模块路径。
4. **基准路径**：部分基准注释里写的是 `./benchmark/`（相对路径），从根目录请用 `./internal/benchmark/`。

---

## 1. 一键命令

```bash
go mod tidy                                   # 解析 cel/expr 等依赖

# 核心单元测试（不含带 test 标签的 client 测试）
go test ./pkg/... ./internal/router/ -v

# 全量（注意：TestStubFrontends 修复前 pkg/engine 会红，见 §0.3）
go test ./...

# 竞态检测 / 覆盖率
go test -race ./pkg/... ./internal/router/
go test -cover ./pkg/... ./internal/router/
go test -coverprofile=cover.out ./pkg/... ./internal/router/ && go tool cover -html=cover.out

# 外围 client 测试（需 test 标签）
go test -tags test ./internal/client/... -v

# 基准（见 §3.6）
go test -bench=. -benchmem ./internal/benchmark/

# 只跑某个用例 / 某个文件涉及的用例
go test ./pkg/vm/ -run TestOperators -v
go test ./internal/router/ -run TestEvaluateAll -v
```

---

## 2. 测试总览

| 文件                                      | 包            |         用例数 | 作用                                          | 运行                                            |
| ----------------------------------------- | ------------- | -------------: | --------------------------------------------- | ----------------------------------------------- |
| `pkg/ir/ir_test.go`                       | `ir`          |              8 | SQL→IR 解析、多 DSL 发射、错误处理            | `go test ./pkg/ir/`                             |
| `pkg/vm/vm_test.go`                       | `vm`          |              5 | 字节码编译/求值、全算子、AST↔VM 一致、Explain | `go test ./pkg/vm/`                             |
| `pkg/engine/engine_test.go`               | `engine_test` |              4 | 加载/匹配、前端一致、批量=单条、Matcher 接口  | `go test ./pkg/engine/`                         |
| `pkg/engine/frontend_json_test.go`        | `engine_test` |              3 | JSON 前端正/负算子、CEL/Expr 占位断言(⚠stale) | `go test ./pkg/engine/`                         |
| `pkg/engine/reload_test.go`               | `engine_test` |              1 | 热更新 增/删/替换/坏规则                      | `go test ./pkg/engine/`                         |
| `pkg/dtable/table_test.go`                | `dtable_test` |              1 | 决策表 行→IR→SQL→规则→匹配                    | `go test ./pkg/dtable/`                         |
| `internal/router/evaluate_all_test.go`    | `router`      |              5 | `/evaluate/all` 全规则门禁 + HTTP             | `go test ./internal/router/ -run EvaluateAll`   |
| `internal/benchmark/benchmark_test.go`    | `benchmark`   |      3 (bench) | 编译/单匹配/批量吞吐                          | `go test -bench=. ./internal/benchmark/`        |
| `internal/benchmark/compare_test.go`      | `benchmark`   | 1 bench+1 test | 自研 VM vs qlbridge VM 吞吐 & 一致            | `go test -bench=Backends ./internal/benchmark/` |
| `internal/benchmark/cpu_test.go`          | `benchmark`   |      2 (bench) | worker / 规则数 扩展性                        | `go test -bench=Scaling ./internal/benchmark/`  |
| `internal/client/mcs/client_test.go`      | `mcs`         |             32 | MCS HTTP 客户端（需 `-tags test`）            | `go test -tags test ./internal/client/mcs/`     |
| `internal/client/uss/client_test.go`      | `uss`         |             32 | USS HTTP 客户端（需 `-tags test`）            | `go test -tags test ./internal/client/uss/`     |
| `internal/client/uss/model_error_test.go` | `uss`         |             28 | USS 错误/可空类型编解码                       | `go test -tags test ./internal/client/uss/`     |
| `internal/client/wps/client_test.go`      | `wps`         |             17 | WPS HTTP 客户端（需 `-tags test`）            | `go test -tags test ./internal/client/wps/`     |
| `internal/model/merchant_rule_test.go`    | `model_test`  |             16 | 商户问卷规则/翻译解析                         | `go test ./internal/model/`                     |
| `internal/model/topic_time_usage_test.go` | `model_test`  |              1 | 时长间隔单位归一化                            | `go test ./internal/model/`                     |

---

## 3. 规则引擎核心测试（详解）

### 3.1 `pkg/ir` —— SQL→IR 解析与多 DSL 发射

运行：`go test ./pkg/ir/ -v`

- **TestConvertMultiDSL** — 一条规则同时转 SQL/CEL/Expr/Aviator，断言四种输出**精确**匹配（如 `BETWEEN`→CEL `(age>=25 && age<=40)`、`LIKE '数%'`→`startsWith('数')`）。
- **TestEqualityMapping** — `=` 在 CEL 中映射为 `==`，`>=` 原样保留。
- **TestOrParens** — `(a=1 OR b=2) AND c>=3` 转 Expr 后保留括号与 `||`/`&&` 优先级。
- **TestLikeVariants** — `LIKE '%数'`/`'%数%'`/`'数'`/`'数%'` 分别映射 CEL `endsWith`/`contains`/`==`、Aviator `startsWith`。
- **TestIsNull** — `IS [NOT] NULL` → SQL 原样、CEL `has()`/`!has()`、Expr `==nil`、Aviator `!=nil`。
- **TestParseErrors** — 5 个非法表达式（缺值 `age >`、缺 hi `BETWEEN 1`、空 `IN ()`、悬空 `AND`、非法字符 `@bad`）必须返回解析错误。
- **TestNegationConvert** — `<>`/`NOT IN`/`NOT LIKE`/`NOT(...)` 到各 DSL 的取反发射。
- **TestFullCoverageRoundTrip** — 旗舰全算子规则：SQL 发射含全部子句、四种 DSL 均非空、且 SQL 发射**幂等**（emit→parse→emit 不变）。

### 3.2 `pkg/vm` —— 字节码 VM（求值核心）

运行：`go test ./pkg/vm/ -v`

- **TestCompileAndEval** — 旗舰子集编译后对 6 行求值：命中 / 年龄超限 / 省份错 / 非「数」前缀 / 低分 / 缺字段。
- **TestOperators** — 27 条**单算子**用例：`=`、`>=`、`<`、`AND`、`OR`、`LIKE %x%`、`IS [NOT] NULL`、`<>`、`NOT IN`、`NOT LIKE`、`NOT(...)`、嵌套 `age>=25 AND NOT(province IN(...))`。
- **TestFullCoverageRule** — 旗舰全算子规则：1 个全命中行 + 7 个「各破坏一条子句」的未命中行（NOT IN / NOT LIKE / NOT(=高) / `<>` / BETWEEN / OR 组 / IS NOT NULL）。
- **TestASTMatchesBytecode** — **关键回归**：同一规则/行下，树遍历 runtime(`pkg/runtime/ast`) 与字节码 VM 结果必须逐一相等（覆盖全部取反算子）。
- **TestExplain** — `/evaluate`、`/evaluate/all` 背后的解释器：命中行 `reasons` 为空；未命中行返回含具体失败谓词的 `reasons`（断言含 `age BETWEEN`、`income_level NOT IN`、`occupation NOT LIKE`、`last_login_time IS NOT NULL`、`NOT (risk_level` 等）。

### 3.3 `pkg/engine` —— 引擎装配 / 前端 / 热更新

运行：`go test ./pkg/engine/ -v`

`engine_test.go`

- **TestLoadAndMatch** — 原生前端+VM，6 条规则（含 1 条禁用）→ 加载 5 条；3 个样例用户命中固定答案 `{uid1:[1,2,4], uid2:[5], uid6:[3]}`。
- **TestFrontendsAgree** — qlbridge 前端（qlbridge AST→IR）与原生前端对同一规则/用户**命中完全一致**，证明 VM 与 parser 解耦。
- **TestBatchEqualsSingle** — `RunBatch` 批量结果与逐个 `Match` 一致；并校验统计 `users=3 rules=5 evals=15`。
- **TestMatcherInterface** — 默认引擎（bytecode + qlbridge 前端）经 `engine.Matcher` 接口加载并计数。

`frontend_json_test.go`

- **TestJSONFrontend** — JSON Rule（`and`/`between`/`in`/`like`/`>=`）加载并正确命中/未命中。
- **TestJSONFrontendNegation** — JSON DSL 取反算子 `not_in`/`not_like`/`{"not":...}`/`<>` 命中/未命中。
- **TestStubFrontends** — ⚠ **stale**：断言 `CELFrontend/ExprFrontend.Parse` 返回错误，但二者已委托给真实的 `pkg/parser/cel`、`pkg/parser/expr`（cel-go / expr-lang），`Parse("age >= 18")` 会**成功**返回 IR，故该断言**当前会失败**。修复：改为断言成功（`if err != nil { t.Fatalf(...) }` 且 `node != nil`）或移除本用例。

`reload_test.go`

- **TestHotReload** — 热更新闭环：加载 2 条 → `AddRule(3)` 后命中 2 条 → `RemoveRule(1)` → `ReplaceRules(99)` 原子全量替换 → 坏规则 `AddRule("age >>>")` 失败且不改变规则集。

### 3.4 `pkg/dtable` —— 决策表

运行：`go test ./pkg/dtable/ -v`

- **TestDecisionTableCompilesAndMatches** — 2 行决策表（`between`/`in`/`like`、`isnull`）→ 编译成 2 条 SQL 规则（ID 从 `IDBase=7000` 起）→ 引擎加载 → 用户命中两条（7000、7001），验证「行→IR→SQL→规则→匹配」全链路。

### 3.5 `internal/router` —— `/evaluate/all`（全规则门禁 + HTTP）

运行：`go test ./internal/router/ -run TestEvaluateAll -v`

- **TestEvaluateAll_PassesWhenAllRulesMatch** — 协同规则集下，满足全部规则 → `passed=true`，计数 3/3/0。
- **TestEvaluateAll_FailsWithRuleIDsAndReasons** — 不满足时 `passed=false`，校验 `failed_rule_ids`、回显完整 SQL、原因含实际值（`age=20`）。
- **TestEvaluateAll_FlagshipRule** — 旗舰全算子规则：样例行全过；坏数据触发多条谓词失败。
- **TestEvaluateAll_EmptyRuleSetIsNotPassed** — 空规则集不算通过（门禁需 ≥1 条规则）。
- **TestEvaluateAllHTTP** — Fiber v3 `app.Test` 端到端：200 通过 / 200 未通过带明细 / 缺 `row` → 400。

> 配套数据：`data/rules_30.json`（协同门禁，可全过）、`data/rules_60.json`（冲突集，门禁恒 false，适合 `/match/batch` 圈选）、`data/users_pass_10000.json`（1 万条满足旗舰的用户）。

### 3.6 `internal/benchmark` —— 基准

运行：`go test -bench=. -benchmem ./internal/benchmark/`（注意路径是 `./internal/benchmark/`）

`benchmark_test.go`

- **BenchmarkCompile** — 1 万规则一次性解析/编译吞吐。
- **BenchmarkMatchUser** — 单用户对 1 万规则评分（`-benchmem` 看分配）。
- **BenchmarkRunBatch** — 1 万用户 × 1 万规则、32 worker，打印 `TPS / Latency` 头部块。

`compare_test.go`

- **BenchmarkBackends** — 三后端吞吐对比：`bytecode_native`、`bytecode_qlbridge`、`qlbridge_vm`（同规则/用户）。运行 `go test -bench=Backends ./internal/benchmark/`。
- **TestBackendsAgree** — 自研 VM 与 qlbridge VM 对固定规则/用户命中一致（基准的正确性前置）。
  > 注：目标里「与 CEL、Expr 完整 Benchmark 对比」尚未纳入此处（当前仅 bytecode vs qlbridge）。

`cpu_test.go`

- **BenchmarkWorkersScaling** — worker=1..64 的批量吞吐扩展性。
- **BenchmarkRuleCountScaling** — 规则数 100..10000 下单用户匹配延迟（`-benchmem`）。

---

## 4. 外围业务模块测试（简述）

> 与规则引擎无关，源自客服/风控等业务；`client/*` 均带 `//go:build test`，需 `-tags test`。

### 4.1 `internal/client/mcs`（32 用例，`-tags test`）

玩家信息校验 `VerifyPlayerInfo` 与注册 IP `GetRegisterIP` 的 HTTP 客户端，用 `httptest` mock 覆盖：
全/部分匹配、`success=false`、请求体形状、非 200、500、非法 JSON、重试成功 / 重试耗尽、请求体可重放、`context` 取消/超时、请求头、单例创建、`Close` 幂等。
运行：`go test -tags test ./internal/client/mcs/ -v`

### 4.2 `internal/client/uss`（client 32 + model_error 28）

- `client_test.go`：`GetCustomer`（含 `force`、空 additionalInfo、URL 路径、重试、ctx）、`GeneratePasswordResetToken`（空 token、不同商户、各错误路径）、`GetCustomerPersonalInfo`，以及 `NullString`/`NullInt32`/`FlexTime` 解码。
- `model_error_test.go`：`HTTPError`/`IsHTTPError`/`IsProfileNotFound` 判定，及 `FlexTime`/`NullString`/`NullInt32`/`NullInt` 的 `Marshal/Unmarshal`（含零值、null、非法格式）。
  运行：`go test -tags test ./internal/client/uss/ -v`

### 4.3 `internal/client/wps`（17 用例，`-tags test`）

`GetResetPasswordStatus`：邮箱/短信开关四种组合、请求形状、`success=false`、非 200、404、非法 JSON、重试、ctx 取消；及 `HTTPError`/`IsHTTPError`。

> 运行：`go test -tags test ./internal/client/wps/ -v`

### 4.4 `internal/model`（merchant_rule 16 + topic_time_usage 1）

- `merchant_rule_test.go`：问卷 `ParseQuestions`、有效问题过滤与 `GetValidQuestionInfos`、序列化 round-trip、`ParseFieldTranslationsMap`、`GetTranslationsByLanguage`（精确匹配 / 回退 EN / 空 / 非法 JSON）。
- `topic_time_usage_test.go`：`IntervalSeconds` 单位归一化（各单位拼写、大小写不敏感、首尾空白、零间隔、未知单位跳过）。
  运行：`go test ./internal/model/ -v`

---

## 5. 覆盖率 & CI 建议

```bash
# 核心包覆盖率
go test -coverprofile=cover.out ./pkg/... ./internal/router/ ./internal/model/
go tool cover -func=cover.out | tail -1      # 总覆盖率
go tool cover -html=cover.out -o cover.html  # 浏览器查看

# CI 推荐分两条流水线
go test -race ./pkg/... ./internal/router/ ./internal/model/   # 引擎核心 + 竞态
go test -tags test ./internal/client/...                       # 外围 client
```

修掉 §0.3 的 `TestStubFrontends` 后，`go test ./...` 即可整库通过。基准建议单独跑、不进单测门禁：
`go test -bench=. -benchmem -run '^$' ./internal/benchmark/`。
