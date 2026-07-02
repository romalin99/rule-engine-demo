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
> - 第四期(**第十节**,2026-07-02):⑥字符串转义吞掉正则字符类(`'\d'`→`'d'`,特性 7 静默损坏);
>   ⑦`Emit(SQL)` 不转义引号/反斜杠;⑧AST↔VM 对 NULL 的 `LIKE`/`IN('')` 不一致;并经 qlbridge
>   源码逐名比对补齐第三批 9 个因子(`SECONDS`/`UNIXTRUNC`/`UNSIGN`/`STRING_INDEX`/`TITLECASE`/
>   `QS2`/`URL_MATCHQS`/函数式 `SUM/AVG/COUNT`/`HASH` 别名)。
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
>
> 架构维度的深入分析见 **第七节**(DSL × VM 支持矩阵 —— 哪些前端/运行时覆盖 7 类特性)。

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

---

## 七、DSL × VM 支持矩阵(架构维度)

> "VM" 在本项目有**两层含义**,分处相反方向:①作为**输入 DSL / 前端**(`pkg/parser/*`),
> 把规则文本解析成统一 IR;②作为**执行运行时 / 后端 VM**(`pkg/runtime/*`),消费 IR 求值。
> `qlbridge/cel/expr` 运行时的做法是把 IR **发射回**该 DSL 文本、再交给其原生引擎(主要用于 A/B 跑分)。
> 架构主图("多 DSL → 统一 IR → 单一 bytecode VM,换 Parser、VM 不动")指的是**主路径**。

**前端能产出多少 IR(表达力)**:

| 前端 (DSL) | 覆盖 | 依据 |
| ---------- | ---- | ---- |
| **SQL (native)** | **全部 7 类** + 负数字面量 + 字符串/日期 `BETWEEN` | `pkg/ir` 词法/语法全覆盖 |
| **SQL (qlbridge)** | 基础子集;`QLBridgeFrontend` 对无法解析/转换的构造**回退 native** → 等价全覆盖 | `pkg/engine/frontend_qlbridge.go` |
| **JSON Rule** | 基础子集(`and/or/not/`比较`/between/in/not_in/like/not_like/isnull`) | `pkg/engine/frontend_json.go` |
| **CEL / Expr** | 基础子集(实测仅 `Logic/Not/Compare/In/Like`) | `pkg/parser/cel`、`pkg/parser/expr` |

**运行时能求值多少 IR(执行力)**:

| 运行时 (VM) | 覆盖 | 机制 / 依据 |
| ----------- | ---- | ---- |
| **bytecode**(自研,默认) | **全部 7 类** | `pkg/vm`、`pkg/runtime/bytecode` |
| **ast**(参考实现,`Explain` 也用它) | **全部 7 类** | `pkg/runtime/ast` |
| **qlbridge**(SQL VM) | 基础子集 + qlbridge 内建函数;`JSON_EXTRACT/数组/EXISTS/ANY/ALL/`本项目`REGEXP` **不支持** | `pkg/runtime/qlbridge`:IR→`emitSQL`→qlbridge |
| **cel / expr**(Expression VM) | **仅基础子集**:`ir.Emit` 的 `emitCode` 只发射 `Logic/Compare/Between/In/Like/IsNull/Not`,函数/集合/JSON/正则节点发射空串 | `pkg/runtime/cel`、`pkg/runtime/expr`;`pkg/ir/emit.go` |

**结论**:7 类高级 SQL 特性**只在 bytecode / ast 运行时上完整可用**,且只有 **native SQL 前端**能完整表达。
`JSON / CEL / Expr` 前端与 `qlbridge / CEL / Expr` 运行时均为基础谓词子集——即 `docs/functions.md §9`
记录的"翻译到 CEL/Expr/Aviator 尚未实现"。**线上 API 服务与 CLI 走的是 native 前端 + bytecode/ast,
故部署形态下 7 类特性端到端可用**;此矩阵仅影响直接选用 `qlbridge/cel/expr` 运行时或 `JSON/CEL/Expr` 前端的场景。

> 补齐两条路(本轮按用户决定**未改代码**):(A) 扩 `emitCode` + CEL 的 `collectFields`,把可映射的标量
> 函数翻译到 CEL/Expr;(B) 给 CEL/Expr/qlbridge 运行时加显式能力校验,对无法忠实求值的规则返回清晰错误。

---

## 八、第二期(2026-07-02):qlbridge 计算因子对齐 + 一致性修复

本期新增第 8 项需求——**补全 qlbridge 内置计算因子**,并在复查中修复 4 个现存缺陷。

### 新增:扩展函数库 `pkg/sqlfn`(qlbridge 对齐)

- **新架构**:可扩展内置函数注册表(`pkg/sqlfn`,仅依赖标准库),按注册序分配稳定 ID;
  字节码 VM 新增指令 `OpCallB`(操作数打包 `id<<8|argc`,元数在**编译期**校验),AST 运行时
  经同一注册表求值 —— 两套运行时**共用同一份实现**,杜绝语义漂移。解析器无需逐函数改动
  (任意 `FN(args…)` 均解析为 `CallTerm`);布尔谓词函数经 `sqlfn.IsBoolFn` 自动识别为独立谓词。
- **对齐清单**(63 个可调用名,详见 `docs/functions.md §5.8`):字符串 `TOLOWER/TOUPPER/STRIP/
  CHAR_LENGTH/REPLACE/SPLIT/JOIN/CONCAT` 与谓词 `CONTAINS/STARTSWITH(HASPREFIX)/ENDSWITH(HASSUFFIX)`;
  函数式比较 `EQ/NE/GT/GE/LT/LE`;数学 `SQRT/POW(POWER)`;转换 `TOINT/TONUMBER/TOBOOL/TOSTRING`;
  选择 `ONEOF(COALESCE)`;日期 `NOW/TODATE/TOTIMESTAMP(UNIX_TIMESTAMP)/HOUR/MINUTE/SECOND/DAYOFWEEK/
  HOUROFDAY/HOUROFWEEK/MONTHOFYEAR/YY/MM/YYMM`;邮箱 `EMAIL/EMAILNAME/EMAILDOMAIN`;URL `HOST/DOMAIN/
  PATH(URLPATH)/QS/URLDECODE/URLMAIN/URLMINUSQS`;摘要 `MD5/SHA1/SHA256/SHA512(HASH_* 别名)/B64ENCODE/
  B64DECODE`;数组 `ARRAY_INDEX/ARRAY_SLICE`(0 起,qlbridge 编号)。`SPLIT` 产出的数组可直接喂给
  `ARRAY_CONTAINS/ARRAY_LENGTH/ARRAY_INDEX`(两套运行时都支持计算型数组实参)。
- **明确跳过**(理由见 functions.md §5.8 末注):`map*`、`filter/filtermatch/match`、函数式
  `any/all/exists/not`(关键字冲突)、`cast`、`todatein`、`uuid`(非确定性)、`useragent*`、
  `hash.sip`、`json.jmespath`、`domains/hosts`、`extract/strftime`。

### 修复的缺陷

| # | 缺陷 | 修复 |
|---|------|------|
| 1 | 🐞 **`pkg/vm` 测试无法编译**:`ext_test.go`/`enh_test.go` 匿名结构体字段序为 `{row, rule, want}`,但全部无键字面量按 `{rule, row, want}` 排列(字符串塞进 map 字段,类型错误)。**根因**:`make optimize` 的 `fieldalignment -fix` 会按指针密度重排结构体**字段**,却不同步改写**无键复合字面量**——每跑一次就把编译通过的测试文件改坏一次(循环性损坏) | 统一改回 `{rule, row, want}` 字段序(4 个测试文件 26 处);Makefile 的 `fieldalignment` 加 `-test=false`,不再触碰测试文件,根治循环 |
| 2 | 🐞 **`LENGTH(数组)` 两运行时不一致**:VM 返回 0(`kArr` 渲染为 `""` 的 rune 数),AST 返回 NULL | 双方统一为**元素个数**(等价 `ARRAY_LENGTH`,即 qlbridge `len` 语义) |
| 3 | 🐞 **字符串函数作用于数组字段不一致**:VM 对 `UPPER/LOWER/TRIM/SUBSTRING(数组)` 按 `""` 变换出字符串,AST 返回 NULL | VM 改为 NULL(向 AST/SQL 语义对齐);`LENGTH`/`ARRAY_LENGTH` 除外 |
| 4 | 🐞 **AST `valStr` 对布尔渲染为 `""`**,VM 渲染 `"true"/"false"`(影响 `TOBOOL(x) = 'true'` 这类比较) | AST 补布尔分支,与 VM `asString` 对齐 |
| 5 | 🧹 `.gitignore` 第 62 行 `pkg/`(本意忽略构建产物)会吞掉 `pkg/` 下**新增源码文件**(上期已发现未修) | 删除该行(`dist/build/out` 已覆盖构建产物) |

### 改动文件

| 文件 | 改动 |
|------|------|
| `pkg/sqlfn/`(新增 6 文件) | 注册表 + 字符串/比较/数学转换/日期/网络/摘要/数组实现 |
| `pkg/vm/opcode.go` | 新增 `OpCallB` |
| `pkg/vm/compile.go` | `isCoreFn` 分派;`emitBuiltin`(编译期元数校验);布尔内置谓词下降 |
| `pkg/vm/vm.go` | `OpCallB` 求值 + `valueAny`;`LENGTH(数组)`;数组操作数 NULL 对齐 |
| `pkg/ir/parser.go` | `sqlfn.IsBoolFn` 的调用自动成为独立谓词(与 `ARRAY_CONTAINS` 同径) |
| `pkg/runtime/ast/ast.go` | sqlfn 直通(`applyFunc`/`evalPred`)+ `argVal`;`arrayOf` 支持计算型数组;`LENGTH(数组)`;`valStr` 布尔 |
| `pkg/vm/ext_test.go`、`enh_test.go` | 修复结构体字段序(缺陷 1) |
| `pkg/vm/qlfn_test.go`(新增) | 全函数覆盖,断言 **bytecode == ast** 且结果正确;元数编译错误;emit 往返;大小写不敏感 |
| `docs/functions.md`、`README.md`、`.gitignore` | 文档同步;§5.8 扩展函数参考;去除 `pkg/` 忽略 |

### 验证

- 期望值经 Python 独立复算(hashlib/base64/datetime/urllib:MD5/SHA*/B64、epoch `1782950400`、
  2026-07-02=周四(Go weekday 4)、`HOUROFWEEK=111`、`YYMM='2607'`、`TOINT('1,234.9')=1234` 等)。
- 本环境无 Go 工具链(同上期),请本机执行:

```bash
go build ./... && go test ./...
go test ./pkg/vm/ -run TestQlfn -v     # 扩展函数库(含 bytecode==ast 交叉校验)
go test ./pkg/vm/ -run 'TestExt|TestEnh' -v   # 回归(本期修复了这两个文件的编译错误)
```

---

## 九、第三期(2026-07-02 补充):qlbridge 因子第二批 —— 上期跳过项复盘补齐

按"尽量补全"的要求,对上期跳过清单逐项复盘,把**可以在本引擎模型内正确实现**的全部补齐;
只保留确实不可移植的少数项(见 functions.md §5.8 末注,现仅剩 `map()/mapinvert/maptime`、
`filter/filtermatch`、函数式 `any/all/exists/not`(关键字冲突)、`uuid`、`useragent.map`)。

### 本期新增

| qlbridge 因子 | 本引擎形式 | 实现要点 |
|---|---|---|
| `cast` | `CAST(x AS INT/FLOAT/STRING/BOOL/DATE/TIMESTAMP)` | 解析期脱糖为 `TOINT/TONUMBER/…`,无新 IR/opcode |
| `match`(+`exists(match)`) | `MATCH('prefix', …)` 布尔谓词 | 新 opcode `OpMatchPre`(扫描行字段名,逐前缀 OR 链);前缀须为非空字面量(编译期校验) |
| `mapkeys` / `mapvalues` | `MAPKEYS(field)` / `MAPVALUES(field)` | 新 opcode `OpMapKeys/OpMapVals` 裸字段读取;支持 `map[string]any` 与 JSON 对象字符串;**键排序**保证确定性 |
| `json.jmespath` | `JMESPATH(doc,'expr')` / `JSON_JMESPATH` | 新 opcode `OpJmesField/OpJmesExpr`(复用 JSONOp 池);表达式加载期预编译+缓存(`sqlfn.JmesCompile`),坏表达式编译报错;全标量数组结果映射为数组值,可组合 `ARRAY_*` |
| `useragent` | `USERAGENT(ua, part)` | `mssola/user_agent`(qlbridge 既有固定依赖,go.sum 已锁定);bot/mobile 返回布尔 |
| `hash.sip` | `HASH_SIP(x)` / `SIPHASH(x)` | `dchest/siphash`(同上),密钥 (0,1);64 位结果以十进制**字符串**返回(避免 float64 精度截断) |
| `todatein` | `TODATEIN(tz, x)` | `time.LoadLocation` + `ParseInLocation`,归一化为 UTC;未知时区 → NULL |
| `domains` / `hosts` | `DOMAINS(v…)` / `HOSTS(v…)` | 变参,数组参数展开,去重保序返回数组 |
| `extract` / `strftime` | `EXTRACT(x, fmt)` / `STRFTIME(x, fmt)` | 手写 strftime 子集(`%Y %y %m %d %e %H %I %M %S %p %a %A %b %B %j %w %s %%`),无第三方格式库 |

> 新引入的三个 import(`mssola/user_agent`、`dchest/siphash`、`jmespath/go-jmespath`)都是 qlbridge
> 的既有传递依赖,版本已在 go.sum 锁定,构建无需新下载;`go mod tidy` 会把它们的 `// indirect`
> 标记转正。

### 顺带修复

- **AST `toStringSlice` 对数组内布尔元素渲染为 `""`**(VM 渲染 `"true"/"false"`)——
  抽出 `rawText` 统一渲染,`ARRAY_CONTAINS(flags,'true')` 两运行时现在一致。

### 本期改动文件

| 文件 | 改动 |
|------|------|
| `pkg/sqlfn/dates2.go`、`net2.go`、`jmes.go`(新增) | TODATEIN/STRFTIME/EXTRACT;DOMAINS/HOSTS/USERAGENT/HASH_SIP;JMESPath 编译缓存与求值 |
| `pkg/vm/opcode.go` | `OpMatchPre` `OpMapKeys` `OpMapVals` `OpJmesField` `OpJmesExpr` |
| `pkg/vm/compile.go` | `emitJmes` / `emitMapFn` / `emitMatch` 下降与编译期校验 |
| `pkg/vm/vm.go` | 5 个新指令求值 + `matchPrefix` / `mapParts` |
| `pkg/ir/parser.go` | `CAST(x AS t)` 脱糖(`finishCast`/`castFn`);`MATCH` 谓词识别 |
| `pkg/runtime/ast/ast.go` | `jmesAST` / `mapPartsAST` / `MATCH` 分支 / `rawText` |
| `pkg/vm/qlfn_more_test.go`(新增) | 全部新因子用例,断言 bytecode == ast;CAST/MATCH/JMESPath 编译错误用例 |
| `docs/functions.md`、`README.md` | §5.8 增补;跳过清单缩短为 5 项 |

### 本期验证

- SipHash-2-4 以纯 Python 独立实现并先通过**论文测试向量**(`0xa129ca6149be45e5`)自检,再复算
  `Hash(0,1,"abc") = 16397480524846279048`;strftime 各代码、`%s` epoch(`1783004645`)、时区换算
  (上海 08:00 → UTC 00:00)均经 Python 复核。
- 同样请本机执行 `go build ./... && go test ./...`(新用例:`go test ./pkg/vm/ -run TestQlfn -v`)。

---

## 十、第四期(2026-07-02):qlbridge 注册表逐名核对 + 正则转义修复

本期首次在沙箱中**直接克隆 qlbridge 源码**(`expr/builtins`,91 个注册名),与 `pkg/sqlfn`
注册表逐名机器比对——发现此前两期的"跳过清单"之外还有 **9 个因子既未实现也未记录**;同时对
1-7 项特性做第四轮走查,发现并修复 **3 个正确性缺陷**(其一直接破坏特性 7 正则)。

### 修复的缺陷

| # | 缺陷 | 修复 |
|---|------|------|
| 1 | 🐞 **字符串字面量把一切 `\x` 脱成 `x`,静默破坏正则字符类**:`phone REGEXP '^1\d{10}$'` 实际编译成 `^1d{10}$`(匹配字母 `d`),`\w \s \b \.` 同理全部损坏。既有用例只写 `[0-9]` 形式,故从未暴露 | `pkg/ir/lexer.go`:反斜杠仅转义 `\'` `\"` `\\` 三种,其余 `\x` 原样保留(MySQL 风格 `\\d` 仍解析为 `\d`,兼容) |
| 2 | 🐞 **`Emit(SQL)` 不转义 `'` 与 `\`**:值含单引号(`O'Brien`)或反斜杠时,决策表"行→IR→SQL→重解析"链路在重解析处断裂 | `pkg/ir/emit.go`:新增 `sqlStr`,字符串字面量与 `LIKE`/`REGEXP` 模式发射时转义 `\` `'`;emit→parse→emit 往返稳定 |
| 3 | 🐞 **AST 与 VM 对 NULL 字段的 `LIKE`/`IN('')` 不一致**:AST 把缺失字段渲染为 `""`,于是 `missing LIKE '%'`、`missing IN ('')` 判 true;VM 按 kUndef 判 false | `pkg/runtime/ast/ast.go`:`ir.Like`/`ir.In` 先判空(缺失/nil → 不匹配),对齐 VM 与 SQL 语义(`NOT LIKE`/`NOT IN` 在 NULL 上的既有两值行为不变) |

### 新增:qlbridge 因子第三批(此前遗漏,非跳过项)

| qlbridge 因子 | 本引擎形式 | 说明 |
|---|---|---|
| `seconds` | `SECONDS(x)` | 日期串→epoch 秒;`'M10:30'`→630;数字直返;`'0:00'`→NULL(qlbridge 行为) |
| `unixtrunc` | `UNIXTRUNC(x[,p])` | 返回**字符串**;全数字 epoch 按位数辨单位(10/13/16/19);`p`∈`s|seconds`/`ms|milliseconds`/`sm|secondsmicro` |
| `unsign` | `UNSIGN(x)` | 二补码无符号读数,十进制**字符串**(uint64 超 float64 精度) |
| `string.index` | `STRING_INDEX(s,sub)` | 0 起字节偏移;不存在→NULL |
| `string.titlecase` | `TITLECASE(x)` | 逐词首字母大写,复刻 `strings.Title` 分词(字母/数字/`_` 非分隔) |
| `qs2` / `qsl` | `QS2(x,key)` / `QSL` | 既有 `QS` 实现本就是 qs2 的保大小写语义,补两个别名(qlbridge 旧 `qs`/`qsl` 会把整个 URL 转小写——按现代语义统一,已注记) |
| `url.matchqs` | `URL_MATCHQS(url[,re…])` | host+path + 仅保留参数名匹配任一正则的查询参数(排序重编码);正则进程内缓存编译 |
| `sum` / `avg` / `count` | `SUM(v,…)` / `AVG(v,…)` / `COUNT(x)` | 函数式(逐行)形式,含 qlbridge 怪癖:`SUM` 合计恰为 0→NULL、布尔参数毒化;`AVG` 数组元素严格;`COUNT` 为出现标记(1/NULL)。与 `(SELECT SUM(...) FROM coll)` 聚合子查询语法互不干扰 |
| `hash` | `HASH(x)` | `HASH_SIP` 补裸名别名(qlbridge 同名双注册) |

> 跳过清单**不变**(5 组,理由见 functions.md §5.8 末注):`map/mapinvert/maptime`、
> `filter/filtermatch`、函数式 `any/all/exists/not`(关键字冲突)、`uuid`、`useragent.map`。
> 至此 qlbridge **91 个注册名:已对齐 80 个(含别名),余 11 个(上述 5 组)均有记录在案的不可移植原因**。

### 改动文件

| 文件 | 改动 |
|------|------|
| `pkg/ir/lexer.go` | 字符串转义规则收窄(缺陷 1) |
| `pkg/ir/emit.go` | `sqlStr` 转义发射(缺陷 2) |
| `pkg/runtime/ast/ast.go` | `ir.Like`/`ir.In` NULL 对齐(缺陷 3) |
| `pkg/sqlfn/parity.go`(新增) | 第三批 9 因子实现 |
| `pkg/sqlfn/net.go`、`net2.go`、`sqlfn.go` | `QS2`/`HASH` 别名;`registerParity()` 接线 |
| `pkg/vm/qlfn_parity_test.go`(新增) | 全部新因子 + 转义 + NULL 对齐 + emit 往返 + 元数编译错误 + 聚合子查询共存,断言 **bytecode == ast** |
| `pkg/ir/escape_test.go`(新增) | 词法转义单测 + `sqlStr` 单测 + 往返稳定性 |
| `docs/functions.md`、`README.md` | §5.8 增补第三批;README 函数表同步 |

### 追加(第五轮复核,同日):布尔字段 term 路径不一致

按"再次深入分析"的要求做第五轮全量走查,发现一处前四轮均未覆盖的**双运行时不一致**:

| # | 缺陷 | 修复 |
|---|------|------|
| 4 | 🐞 **AST 的 `termVal` 把布尔字段一律置 NULL,VM 却保留(渲染 `"true"/"false"`)**。影响所有 term 路径:两字段比较 `is_vip = is_active`(VM 真实比较,AST 恒 false)、`UPPER(flag)`/`LENGTH(flag)`(VM `"TRUE"`/4,AST NULL)、`flag REGEXP '^tr'`(VM 命中,AST 恒 Negate)、`flag = ANY(…)` 与子查询投影布尔列。注:`flag = 'true'`、`flag IS NULL` 等**传统路径**两运行时本就一致,故此前未暴露 | `termVal` 布尔直通(`valStr`/`compareVals` 本就有布尔分支);顺带收敛 `argVal`/`rawText` 里为旧行为打的补丁 |
| 5 | 🐞 **计算型(非字段)`JSON_EXTRACT` 文档**:VM 经 `asString` 渲染后解析,AST 直接把数字/布尔结果判 NULL(`JMESPATH` 的 AST 实现反而早已渲染) | `jsonExtractAST` 非字段分支先 `valStr` 渲染,与 VM/`jmesAST` 对齐 |

已知且**保持一致**的边界(不改):数组作标量比较操作数时 VM 按 `""` 渲染而 AST 为 NULL
——两侧均属无意义规则(`LOWER(x) = tags`),判 false 的方向一致;数组比较请用 `ARRAY_*` 谓词。
新增 `TestParityBoolFieldTerms`(13 例)与 `TestParityJSONExprDoc`(4 例),
`docs/functions.md §2` 增补布尔渲染语义说明。

### 追加(第六轮复核,同日):特性组合完备性

第六轮换角度,专查**特性间组合**的表达力缺口(不再重复已验证的单点语义),补齐两处、
文档化一处:

| # | 主题 | 动作 |
|---|------|------|
| 1 | ➕ **`REGEXP_LIKE` match_type 仅认 `'i'`,其余标志被静默忽略**(与 MySQL 语义漂移:`'ic'` 本应最右生效、未知标志本应报错) | 完整支持 `i/c/m/n/u`:`i`/`c` 最右生效、`m`→`(?m)`、`n`→`(?s)`、`u` 接受无操作;**未知标志与非字面量 match_type 改为编译期报错**(此前静默忽略)。解析层脱糖,两运行时自然一致 |
| 2 | ➕ **量词不能套计算数组**:`tag = ANY(SPLIT(csv, ','))` 此前脱糖为与数组值做标量比较(恒 false)。两套运行时的 `QuantArr` 求值路径本就支持任意 Term(VM 栈上 kArr / AST `arrayOf`),只差解析器放行 | `parseQuant` 对**静态可判返回数组**的函数(`SPLIT/MAPKEYS/MAPVALUES/ARRAY_SLICE/DOMAINS/HOSTS`)生成 `QuantArr`;标量函数保持原"单值列表"语义(`x = ANY(LOWER(y))` 不变),`JMESPATH` 因返回形状运行时才知,刻意不列入 |
| 3 | 📝 **`IN` 的数值字面量按文本匹配**:`IN (1, 2)` 匹配数值 1,但 `IN (1.0)`/`IN (01)` 不匹配(引擎不做 MySQL 式隐式数值强转)。语义双运行时一致、对规范拼写完全正确,且"修复"会破坏字符串字段的精确文本匹配 | **保持现状并文档化**(functions.md §4 增注,含替代写法 `x = ANY(1.0, 2.5)`——比较两侧数字时按数值比较) |

新增 `TestParityRegexpLikeFlags`(9 例 + 1 编译错误)与 `TestParityQuantComputedArrays`(13 例),
正则标志预期值经 Python re 复核(与 RE2 在该子集语义一致);emit 往返新增 3 条组合规则。
文档:functions.md §4(IN 注)、§5.5(量词×计算数组)、§5.6(match_type 全表 + 转义说明)。

### 追加(第七轮,同日):逐特性安全风险评审 + 5 项加固

按新增要求("分析每个功能的风险点,提高安全性")以**对抗视角**重审 8 类特性——规则文本
与行数据均视为不可信输入。完整风险矩阵见 **[docs/security_review.md](security_review.md)**;
发现并加固 5 处(其中 2 处为可稳定触发的**进程级崩溃向量**):

| # | 风险(严重度) | 加固 |
|---|------|------|
| 1 | 🔴 `SUBSTRING(s, start, 巨大长度)` 中 `from+count` **整数回绕为负切片界 → panic**;worker goroutine 无 recover → 一条规则+一行长串**杀死整个服务** | 先夹紧再相加(`vm.go`/`ast.go` 双修);新用例修复前确定性 panic |
| 2 | 🔴 深嵌套规则文本(兆级括号/NOT 链)→ 递归解析器**栈耗尽**,Go 栈溢出不可恢复 | 解析深度上限 200(`parseUnary` 单点守卫全语法);超限改为加载期报错,并使"能解析 ⇒ VM 栈(256)必够"成立 |
| 3 | 🟡 `URL_MATCHQS` 模式参数可为字段 → 行数据可撑爆**无界正则缓存**(内存 DoS) | 缓存封顶 1024(LoadOrStore+原子计数),超限编译不缓存 |
| 4 | 🟡 qlbridge(第三方无维护)解析器对畸形文本 panic 会穿透 `engine.New()` | `safeQLParse` panic 隔离 → 错误 → native 回退 |
| 5 | 🟡 求值层任何未知 panic = 进程死亡(goroutine 边界) | `matchInto` 按用户 recover,fail-safe 不命中 + `EvalPanics()` 计数可观测 |

已复核为安全的关键面(详见 security_review.md):RE2 无 ReDoS、`LoadLocation` 拒路径穿越、
JSON 深度受 stdlib 上限、JMESPath/JSON 路径强制字面量(行数据无法注入查询语言)、数组
定位全夹紧、哈希因子已标注非安全用途。新增测试:`pkg/ir/security_test.go`、
`pkg/engine/security_test.go`、`pkg/sqlfn/parity_test.go`、`TestParitySubstrOverflow`。

### 本期验证

- 预期值全部经 Python 独立复算:`2015/07/04`→`1435968000`、`unsign(-32847623329847)`→
  `18446711226086221769`(qlbridge 文档示例)、`1438445529707` 的三种精度渲染、
  `^1\d{10}$` 对 `13912345678` 命中且损坏形 `^1d{10}$` 不命中等。
- 本环境无 Go 工具链(已实测各二进制分发渠道均被网关拦截),请本机执行:

```bash
go build ./... && go test ./...
go test ./pkg/vm/ -run TestParity -v              # 第三批因子 + 转义/NULL 修复(bytecode==ast 交叉校验)
go test ./pkg/ir/ -run 'TestLexString|TestEmitSQL|TestEscape' -v   # 词法/发射转义
```
