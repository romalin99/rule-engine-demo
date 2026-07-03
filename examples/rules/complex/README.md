# 复杂规则样例（五类输入 DSL，每类一个文件，引擎可直接解析）

对应 `docs/dsl_inputs.md` 的五类输入，本目录给出**复杂**规则样例（比 `examples/rules/`
的基础样例更深的嵌套、更全的算子覆盖），并附样例用户数据。每个文件都**只使用对应前端
实测支持的构造**，可被引擎解析、编译为字节码并求值。

| # | 输入 DSL | 文件 | 前端 | 规则数 | 覆盖能力 |
|---|----------|------|------|--------|----------|
| 1 | SQL(native) | [`native.sql`](native.sql) | `engine.NativeFrontend{}` | 68 | 全部 7 类高级特性：字符串/日期/集合(EXISTS·ANY·ALL·SOME·聚合子查询)/数学/数组/JSON·Map/正则，另有 CAST、扩展布尔内建(CONTAINS/STARTSWITH/…)、邮箱/URL 函数、MATCH 字段名前缀、负数与字符串 BETWEEN、字段对字段、字面量在左、双重否定，以及 4 条多特性深嵌套「旗舰」规则 |
| 2 | SQL(qlbridge) | [`qlbridge.sql`](qlbridge.sql) | `engine.QLBridgeFrontend{}`（`engine.New()` 默认） | 23 | A 区：qlbridge 直译快路径（比较/IN/LIKE/数值 BETWEEN/多层与或）；B 区：qlbridge 可解析、转换器不认 → 自动回退 native（NOT(…)、函数调用）；C 区：五层嵌套复杂组合 |
| 3 | JSON Rule | [`json_rule.json`](json_rule.json) | `engine.JSONFrontend{}` | 16 | 全部叶子算子（`= == != <> > >= < <=`、数值/日期 `between`、`in/not_in`、`like/not_like`、`isnull/isnotnull`、布尔值）+ `and/or/not` 六层深嵌套、双重否定、全算子大满贯 |
| 4 | CEL | [`cel.cel`](cel.cel) | `engine.CELFrontend{}` | 21 | cel-go（https://cel.dev）可解析：`&& \|\| !`、六种比较、`in [列表]`、`startsWith/endsWith/contains`，五层嵌套/长链/德摩根形态 |
| 5 | Expr | [`expr.expr`](expr.expr) | `engine.ExprFrontend{}` | 23 | expr-lang/expr 可解析：`&&/and、\|\|/or、!/not`、六种比较、`in / not in`、中缀 `startsWith/endsWith/contains`、内建 `hasPrefix/hasSuffix`，五层嵌套/长链 |

配套数据：[`users.json`](users.json)（4 个画像各异的样例用户，含 `orders`/`tags_rows`
嵌套行、`profile` JSON 文档、`attrs` Map、数组/负数/布尔等字段，供上述全部规则演示）。

## 运行（CLI，按前端加载对应文件）

```bash
go run ./cmd/cli -frontend native   -rules examples/rules/complex/native.sql    -users examples/rules/complex/users.json
go run ./cmd/cli -frontend qlbridge -rules examples/rules/complex/qlbridge.sql  -users examples/rules/complex/users.json
go run ./cmd/cli -frontend json     -rules examples/rules/complex/json_rule.json -users examples/rules/complex/users.json
go mod tidy   # CEL / Expr 首次需解析依赖
go run ./cmd/cli -frontend cel      -rules examples/rules/complex/cel.cel        -users examples/rules/complex/users.json
go run ./cmd/cli -frontend expr     -rules examples/rules/complex/expr.expr      -users examples/rules/complex/users.json
```

## 解析可用性验证（零编译失败断言）

```bash
go test ./pkg/engine/ -run TestComplexRuleFiles -v
```

`pkg/engine/complex_examples_test.go` 用**对应前端**加载每个文件、逐条编译并对全部
样例用户批量求值：要求 0 条解析/编译失败。

## 能力边界提醒（选型速查）

- **要函数/正则/JSON/集合判断** → 用 `native.sql` 的写法（或 qlbridge 前端，回退后等价）。
- **JSON / CEL / Expr 是基础谓词子集**：比较、逻辑、`in`、前后缀/包含；无函数与正则。
  CEL/Expr 的比较要求**字段在左、字面量在右**；`BETWEEN` 用 `f >= lo && f <= hi` 表达；
  CEL/Expr 样例中**不使用负数字面量**（一元负号超出该子集）。
- 规则行格式（`.sql/.cel/.expr`）：每行一条，`#` 注释，可选 `名称: 表达式` 前缀
  （名称仅限中英文/数字/空格/`_`/`-`，≤60 字节）。
