# rule-engine-demo

实时规则引擎 Demo：把存在数据库里的 SQL-WHERE 风格规则（如 `rule_engine` 字段），
用 [qlbridge](https://github.com/araddon/qlbridge) 编译一次、对来批的用户宽表数据反复
求值，判断每个用户命中了哪些规则。

```
rules.json ──load──▶ Parser.Compile ──▶ RuleCache ──▶ Evaluator.Eval ──▶ matches
   (DB)              (qlbridge AST)      (并发安全)      (qlbridge VM)
```

## 运行

```bash
go mod tidy            # 拉取 qlbridge 及其依赖
go run ./cmd/rule-engine-demo
# 或
go run ./cmd/rule-engine-demo -rules data/rules.json -workers 8
```

## 规则语法（qlbridge）

规则就是 SQL 的 WHERE 条件，支持 `=`/`==`、`>=`、`BETWEEN ... AND ...`、
`IN (...)`、`LIKE`（`%` 通配，内部转成 `*`）、`AND`/`OR`、单引号字符串等：

```sql
age BETWEEN 25 AND 40
AND province IN ('广东','江苏','浙江')
AND income_level IN ('20k-30k','30k+')
AND favorite_category LIKE '数%'
AND active_score >= 85
```

字段名必须和用户宽表的列名一致（`age`、`province`、`total_amount` ...）。

## 结构

| 包 | 职责 |
|----|----|
| `model` | `Rule`：规则定义（id / name / expr / priority / enabled） |
| `parser/qlbridge` | `Compile(expr)`：用 qlbridge 解析成可复用 AST |
| `runtime/qlbridge` | `Eval(compiled, user)`：在 qlbridge VM 上对用户求值 |
| `engine` | 规则缓存、单条匹配、批量并发匹配（worker pool）、热加载 |
| `cmd/rule-engine-demo` | 装配 + 样例用户 + 打印命中结果 |

`engine` 只依赖 `Parser` / `Evaluator` 两个接口，可无缝替换为 CEL、Expr 等其它 DSL。

## 关键点

- **编译一次，求值多次**：AST 缓存在 `RuleCache`，qlbridge 的 AST 可并发安全求值。
- **批量并发**：`Engine.MatchBatch` 用 worker pool 对一批用户并行打分，规则 AST 只读共享。
- **热加载**：`RuleCache.Replace` 原子替换规则集，读侧无需停顿。
- **容错**：单条规则解析/求值失败只跳过该规则，不影响其它规则。
