# SQL 特性支持审计与修补报告 (SQL Feature Audit & Patch Report)

> 针对企业级定位,审计 7 类 SQL 特性在 **native SQL 前端 → 统一 IR → 字节码 VM / AST 运行时**
> 全链路的支持情况,并修补发现的缺陷与缺口。审计日期:2026-07-01。

## 结论速览

7 类特性此前**均已由 native SQL 前端实现**(并在 `bytecode` 与 `ast` 两套运行时交叉校验)。
经**三轮深入分析**(核心求值 → 前端装配/词法 → 多输入路径与端到端服务层),共:修复 **3 个正确性
bug**、补齐 **4 项功能**、统一 **3 处输入路径**(qlbridge / JSON 规则 / 决策表)的 `BETWEEN` 行为、
消除 **1 处默认引擎可用性陷阱**,并顺带修复 **1 个与本主题无关、但会导致 `go test` 变红的陈旧用例**。

> - 第二轮(**第四节**):①负数字面量此前无法解析(词法层);②默认引擎 `engine.New()` 用的
>   qlbridge 前端无法解析这些函数/谓词——现透明回退到 native。
> - 第三轮(**第五节**):③JSON 规则前端的日期区间 `BETWEEN` 与 SQL 不一致;④决策表/服务层/示例
>   数据端到端核对;⑤修复陈旧的 `TestStubFrontends`。
>
> **端到端确认**:线上 API 服务(`internal/infra`)、CLI、`/rules/test`、`/evaluate` 全部走 **native**
> 前端或 `ir.Parse`,7 类特性在部署形态下均可用;决策表经"行→IR→SQL→native 重解析"链路也已覆盖。

| # | 特性 | 审计前 | 本次动作 | 审计后 |
|---|------|--------|----------|--------|
| 1 | 字符串 `LOWER/UPPER/TRIM/LENGTH/SUBSTRING` | ✅ 已支持(`SUBSTRING` 仅 3 参) | ➕ 补 `SUBSTRING(s, start)` 两参形式 | ✅ 完整 |
| 2 | 日期 `CURRENT_DATE/CURRENT_TIMESTAMP`、加减、`YEAR/MONTH/DAY`、`>= <=` | ⚠️ 基本支持,但裸关键字**作左操作数被当字段** | 🐞 修复 `CURRENT_DATE/TIMESTAMP` 左操作数;➕ 补字符串/日期区间 `BETWEEN` | ✅ 完整 |
| 3 | 集合判断 `EXISTS / ANY / ALL`(子查询) | ✅ 已支持(行内子查询、量词、标量聚合子查询) | 无需改动 | ✅ 完整 |
| 4 | 数学 `ABS/ROUND/CEIL/FLOOR` | ✅ 已支持(`ROUND` 仅取整) | ➕ 补 `ROUND(x, d)` 保留小数(含负数位) | ✅ 完整 |
| 5 | 数组:包含 / 交集 / 长度 | ⚠️ 包含✅ 长度✅ 与**字面量**重叠✅;**两数组字段交集**✗ | ➕ 新增 `ARRAY_INTERSECT(a, b)` | ✅ 完整 |
| 6 | JSON 字段访问 `json_extract(profile,'$.city')='深圳'` | ✅ 已支持(字符串自动解析 / 已解析对象;`$.a.b[0]` 路径) | 无需改动 | ✅ 完整 |
| 7 | 正则 `REGEXP / RLIKE / REGEXP_LIKE` | ✅ 已支持(Go RE2,加载期预编译,`i` 标志) | 无需改动 | ✅ 完整 |

> 说明:上述函数/集合谓词由 **native SQL 前端**解析(非 qlbridge),服务端与批量入口已固定使用
> native 前端;CEL/Expr/Aviator 前端不解析这些函数(既有设计,见 `docs/functions.md §9`)。

---

## 一、修复的 Bug

### 1. `CURRENT_DATE` / `CURRENT_TIMESTAMP` 作为比较**左操作数**被误当作字段

- **现象**:`CURRENT_DATE >= last_login`、`CURRENT_TIMESTAMP > x` 恒不命中。
- **根因**:`pkg/ir/parser.go` 中,只有 `parseTerm`(用于比较右侧/函数实参)特判了这两个
  裸关键字;`parsePredicate`(比较左侧入口)把 `CURRENT_DATE` 当成了名为 `"CURRENT_DATE"`
  的字段 → 求值时字段缺失 → NULL → 比较判 false。两套运行时表现一致地**错误**。
- **修复**:`parsePredicate` 在识别到左操作数是 `CURRENT_DATE`/`CURRENT_TIMESTAMP`(且其后
  不是 `(`)时,构造 `CallTerm` 并走 `parseTermPredicate`,与右侧行为对齐。
- **影响面**:纯解析层修复,两套运行时同时受益;不改变任何既有规则的解析结果(此前这样写等价于
  引用一个不存在的字段,没有合法语义)。

---

## 二、补齐的功能缺口

### 2. 字符串/日期区间 `BETWEEN`(如 `register_date BETWEEN '2020-01-01' AND '2020-12-31'`)

- **审计前**:遗留 `Between` 节点仅支持**数值**边界。字符串边界在字节码 VM 上**编译报错**,在 AST
  运行时上**静默返回 false**——两套运行时不一致且都不可用。
- **补齐**:`parsePredicate` 在 `BETWEEN` 边界含字符串时,脱糖为
  `field >= lo AND field <= hi`(两套运行时都按字典序比较字符串,对 ISO 日期即时间序)。
  数值区间仍走原 `Between` 快路径,零性能回退。

### 3. `ROUND(x, d)` —— 保留 d 位小数

- **审计前**:`ROUND` 仅单参取整;两参**编译报错**(`docs` 曾标注"未实现")。
- **补齐**:新增 `OpRound2`。`ROUND(x, d)` = `round(x·10^d)/10^d`,四舍五入远离零;`d` 可为负
  (按十/百位取整)。字节码 VM 与 AST 运行时同实现。

### 4. `SUBSTRING(s, start)` —— 两参形式(截到末尾)

- **审计前**:仅 `SUBSTRING(s, start, len)` 三参。
- **补齐**:两参形式表示"从 `start`(1 起)到字符串末尾"。字节码侧脱糖为三参 + 末尾哨兵长度
  (由既有 `substr` 越界裁剪逻辑收敛),AST 侧直接取到末尾;两条路径结果逐一比对一致。

### 5. `ARRAY_INTERSECT(a, b)` —— 两个**数组字段**求交集(对应需求中的"交集")

- **审计前**:`ARRAY_OVERLAP(field, v1, v2, …)` 只能判断数组与**字面量列表**是否有交集;两个数组
  **字段**之间的交集无法表达(`ARRAY_CONTAINS(a, bField)` 会把整个数组当文本比较,失效)。
- **补齐**:新增布尔谓词 `ARRAY_INTERSECT(a, b)`,判定两个数组字段交集是否非空;元素按文本比较
  (兼容 `[]string` / `[]any`、数字数组),任一侧为空/缺失则为 false。新增 `OpArrIntersect`,两套
  运行时同实现。

---

## 三、改动的文件

| 文件 | 改动 |
|------|------|
| `pkg/ir/parser.go` | 修复 `CURRENT_DATE/TIMESTAMP` 左操作数;字符串/日期 `BETWEEN` 脱糖;识别 `ARRAY_INTERSECT` 为布尔谓词 |
| `pkg/vm/opcode.go` | 新增 `OpRound2`、`OpArrIntersect` |
| `pkg/vm/compile.go` | `ROUND` 按元数分派;`SUBSTRING` 两参脱糖;`ARRAY_INTERSECT` 下降;`ROUND` 从固定元数表移出 |
| `pkg/vm/vm.go` | `OpRound2`/`OpArrIntersect` 求值 + `round2`/`arrIntersect` 辅助函数 |
| `pkg/runtime/ast/ast.go` | `ROUND` 两参、`SUBSTRING` 两参、`ARRAY_INTERSECT`(+ `sliceIntersect`) |
| `pkg/vm/enh_test.go`(新增) | 覆盖以上全部场景,并断言 **bytecode == ast** 且结果正确;含 emit→parse→emit 往返稳定性 |
| `docs/functions.md`、`README.md` | 文档同步更新,移除过时的"未实现"说明 |

所有新增能力都遵循既有模式(新增 opcode + Eval 分支 + 辅助函数;`PredCall` 复用),且未改动任何
比较类 opcode 的取值,`QuantArr` 的 `op<<1` 编码不受影响。

---

## 四、深入分析(二轮)新增修补

### 6. 🐞 负数字面量此前无法解析(词法层)

- **现象**:`temperature < -10`、`balance > -100`、`DATE_ADD(d, -7)`、`ROUND(x, -1)`、
  `lat BETWEEN -90 AND 90`、`offset IN (-1, 0, 1)` 等**任何在规则文本里写负数**的规则,都会在
  词法阶段报 `unexpected character '-'`。此前的用例只在**行数据**里放负数(如 `delta:-3`),从未在
  规则文本里出现负号,故一直未暴露。
- **根因**:`pkg/ir/lexer.go` 只把 `= ! > <` 当运算符、数字必须以数位开头,`-` 落到 default 分支报错。
- **修复**:当 `-` **紧跟数字/小数点**时按负数字面量处理(本语法无二元减法,`-` 只能是符号,零歧义)。
  影响全部操作数位置(比较、`BETWEEN`、`IN`、函数实参),两套运行时一致。

### 7. 🐞 默认引擎 `engine.New()` 无法解析这些函数/谓词(前端可用性陷阱)

- **现象**:`engine.New()` 默认使用 **qlbridge 前端**,而该前端的 AST→IR 转换器**只支持**
  `AND/OR`、比较、`IN`、`LIKE`、数值 `BETWEEN`——**不支持**函数、`IS [NOT] NULL`、`NOT(…)`、
  `EXISTS/ANY/ALL`、`REGEXP`、`JSON_EXTRACT`、数组谓词。用库的开发者直接
  `engine.New().AddRule("LOWER(name)='x'")` 会得到"编译失败"。
  (注:线上 API 服务与 CLI 走的是 `internal/infra/manager.go` / `internal/cli` 里显式选择的
  **native 前端**,端到端可用;受影响的只是库默认构造器。)
- **修复**:让 `QLBridgeFrontend.Parse` 在 qlbridge **无法解析**或转换器**遇到不支持的节点**时,
  **透明回退到 native `ir.Parse`**(native 是严格超集)。同时把 qlbridge 的 `BETWEEN` 字符串边界
  按 native 方式脱糖,保证两前端对日期区间语义一致。这样默认引擎也能解析全部 7 类特性,且对既有
  基础规则解析结果不变(`TestFrontendsAgree` 仍成立)。

### 本轮追加改动的文件

| 文件 | 改动 |
|------|------|
| `pkg/ir/lexer.go` | 负数字面量词法支持 |
| `pkg/engine/frontend_qlbridge.go` | qlbridge → native 透明回退;字符串/日期 `BETWEEN` 脱糖对齐 |
| `pkg/vm/enh_test.go` | 追加 `TestEnhNegativeNumbers`(11 例) |
| `pkg/engine/engine_test.go` | 追加 `TestQLBridgeFallbackToNative`(9 条高级规则,native == qlbridge) |

---

## 五、深入分析(三轮):多输入路径一致性 + 端到端核对

引擎宣称"多 DSL 输入 → 同一字节码"。第三轮沿着**每条输入路径**核对上述 7 类特性是否一致可用。

### 8. 🐞 JSON 规则前端的日期区间 `BETWEEN` 与 SQL 不一致

- **现象**:`frontend_json.go` 直接构造 `ir.Between`(不经 SQL 文本回环),字符串/日期边界会命中
  同一处"数值-only Between"缺陷——即 `{"field":"reg_date","op":"between","values":["2020-01-01",
  "2020-12-31"]}` 在 JSON 前端下**无法命中**,而等价 SQL 规则可以。违背该前端"编译成与 SQL 相同
  字节码"的自述约定。
- **修复**:在 `JSONFrontend` 的 `between` 分支同样对字符串边界脱糖为 `>= AND <=`,与 native/qlbridge
  对齐。新增 `TestJSONFrontendDateBetween` 断言 JSON 前端与 native 前端结果逐一相同。

### 9. ✅ 决策表 / 服务层 / 示例数据 端到端核对(无需改动)

- **决策表(`dtable`)**:每行 `行 → IR → ir.Emit(SQL) → model.Rule.Expr`,即以 **SQL 文本**落库,加载时
  由 native 前端**重新解析**。故日期区间 `BETWEEN` 经 SQL 回环后由第二节的 native 修复自动覆盖,无需
  为 dtable 单独改动。
- **服务层**:`RuleTest`→`Manager.Test`、`Evaluate`/`EvaluateAll`→`ir.Parse`+`astrt.Explain`,均走
  native / AST;`internal/infra` 装配的引擎用 `NativeFrontend`。7 类特性经 REST 端点端到端可用。
- **示例数据**:`data/rules_functions.json`(24 条函数规则)、`data/rules_advanced.json`(REGEXP /
  JSON / EXISTS / ANY/ALL / 聚合子查询 / 数组)均使用受支持语法,可正常解析求值。

### 10. 🧹 修复陈旧失败用例 `TestStubFrontends`(与本主题无关,但阻塞 `go test`)

- **现象**:该用例断言 `CELFrontend/ExprFrontend.Parse` 返回"未实现"错误,但二者早已委托真实
  cel-go / expr-lang 解析器,`Parse("age >= 18")` 会成功——**用例在 HEAD 上即为失败态**,会让
  `go test ./pkg/engine/` 变红、掩盖本次新增用例的结果。
- **修复**:按用例注释给出的方案改为断言解析成功。属顺手清理,不影响 7 类特性。

### 本轮追加改动的文件

| 文件 | 改动 |
|------|------|
| `pkg/engine/frontend_json.go` | JSON 规则 `BETWEEN` 字符串边界脱糖,与 SQL 对齐 |
| `pkg/engine/frontend_json_test.go` | 新增 `TestJSONFrontendDateBetween`;修复陈旧的 `TestStubFrontends` |

---

## 六、验证方式与说明

> **实测更新**:用户在本机执行 `go test ./...` 后回报两处失败,均已修正:
> - `TestEnhRound2` 一个用例**期望值写错**(我误以为 `ROUND(10.499,2)=10.49`,实际
>   `10.499` 的第 3 位小数进位到 `10.50=10.5`,故规则为 `true`)。**引擎实现正确,是测试数据错误**;
>   已改为一对无歧义用例(`10.499→10.5` 命中、`10.494→10.49` 不命中)。此错源于我的 Python 对照
>   当初**未覆盖这一具体取值**——已补入并全绿。
> - `TestStubFrontends` 是与本主题无关的陈旧用例,已按第十节修复;用户该次运行早于此修改,重跑即过。
>
> **重要**:本轮修改在受限沙箱内完成,该环境**无 Go 工具链且外网(go.dev / 各镜像 / GitHub release
> 资源)均被网关拦截**,故**无法在此环境执行 `go build` / `go test`**。为最大化可信度,采取了:

1. **全链路 + 多路径静态走查**:对每类特性通读 `lexer → parser → compile → vm` 与 `ast` 两条求值
   路径;并沿 **4 条输入路径**(native SQL / qlbridge / JSON 规则 / 决策表)+ **服务层**(handler →
   service → engine)+ 前端装配(`engine.New` / `internal/infra` / `internal/cli`)逐一核对。
2. **算法级 Python 对照**:将新增/改动的叶子算法(`ROUND(x,d)` 浮点行为、`SUBSTRING` 越界裁剪的
   VM/AST 两路径、`ARRAY_INTERSECT` 集合语义、字符串/日期 `BETWEEN` 字典序、`CURRENT_DATE` 左操作数、
   负数字面量在各操作数位置、高级规则与 JSON `BETWEEN` 的命中)逐条移植到 Python,**三轮全部复算通过**,
   并确认 `ROUND` 结果与字面量浮点相等、`SUBSTRING` 两参在 VM/AST 两路径结果逐一相同。
3. **回归面排查**:确认无既有测试使用新语法(纯增量);无按 opcode 数值索引/序列化的代码(中间插入
   opcode 安全);负数词法仅在 `-` 紧跟数字时触发,不影响引号内 `-`、标识符或既有规则;qlbridge 回退
   只在其解析/转换失败时触发,基础规则解析结果不变(`TestFrontendsAgree` 仍成立)。
4. **括号/花括号平衡校验**:全部改动文件相对可编译基线(HEAD)的括号增量为 0。

**请在本机执行以下命令做最终确认**(应全绿):

```bash
go build ./...
go test ./...     # 含新增 enh_test.go、engine_test.go 用例
```

新增用例可单独运行:

```bash
go test ./pkg/vm/     -run TestEnh                       -v   # 字符串/日期/数学/数组/负数 + bytecode==ast
go test ./pkg/engine/ -run 'QLBridge|JSONFrontendDate'   -v   # 前端回退 + JSON 日期区间一致性
```

> 注:仓库 `.gitignore` 第 62 行的 `pkg/`(本意应忽略构建产物)会**忽略 `pkg/` 下的新增文件**,
> 故新测试文件 `pkg/vm/enh_test.go` 需 `git add -f` 才能纳入版本控制(已跟踪的源文件不受影响,
> `go test` 也不受影响)。
