# 安全风险评审:8 类 SQL 特性 (Security Review)

> 评审日期 2026-07-02(第七轮引擎面 + 第八轮 Web/HTTP 面)。视角:**规则文本**
> (来自租户/管理 UI)与**行数据**(来自业务上游)都是不可信外部输入;目标是任何
> 规则/数据组合都不能导致进程崩溃、资源失控或语义偏离。第七轮实施 5 项引擎加固
> (§9),第八轮追加 2 项传输层求值边界遏制并复核控制台回显(§10);此前各轮修复中有
> 2 项本质上也是安全修复(转义注入、正则静默降级),一并归档。

## 0. 信任模型与既有防线

```
   不可信                     信任边界                       受保护资产
┌───────────┐    ┌──────────────────────────────┐    ┌──────────────────┐
│ 规则文本   │──▶ │ 词法/解析(严格报错+深度上限) │──▶ │ 进程存活          │
│ (租户 UI)  │    │ 编译期校验(元数/正则/JMES)  │    │ 内存/CPU 配额     │
├───────────┤    ├──────────────────────────────┤    │ 求值语义正确性    │
│ 行数据     │──▶ │ 值模型(标签联合,无反射)     │    │ (双运行时一致)   │
│ (业务上游) │    │ fail-safe:无效 → false/NULL │    └──────────────────┘
└───────────┘    └──────────────────────────────┘
```

结构性防线(设计即有):**RE2 正则**(线性时间,无回溯灾难);规则**加载期编译**,
失败即拒绝(坏规则进不了热路径);VM **固定深度栈 + 全指令边界检查**(溢出返回 false
不 panic);值模型是**平坦标签联合**(无反射、无动态代码);引擎**不生成 SQL 落库**
(不存在传统 SQL 注入面);求值**无循环/递归构造**,单行成本 O(规则长度 × 行大小)。

## 1. 字符串函数(LOWER/UPPER/TRIM/LENGTH/SUBSTRING + 扩展)

| 风险点 | 状态 |
|--------|------|
| 🔴 **SUBSTRING 整数溢出 → panic → 进程死亡**:`SUBSTRING(s, start, 巨大长度)` 中 `from+count` 回绕为负切片界。`2^63-1024` 是精确 float64,可由规则文本稳定触发;worker goroutine 无 recover,一条恶意规则+一行长字符串即可杀死服务 | ✅ 本轮修复:先夹紧再相加(两运行时);另加求值层 panic 遏制兜底 |
| 🟡 规则文本注入:决策表单元格含 `'` 时经"IR→SQL→重解析"链路可改变规则结构(`x' OR '1'='1` 式) | ✅ 第四轮已修:`Emit(SQL)` 转义 `\` `'`,往返稳定 |
| 🟢 rune 级切片(SUBSTRING/LENGTH)不会切裂 UTF-8;CONCAT/JOIN 输出规模受行数据大小线性约束 | 设计即有 |

## 2. 日期函数(CURRENT_DATE/CURRENT_TIMESTAMP/DATE_ADD/…)

| 风险点 | 状态 |
|--------|------|
| 🟢 解析仅用**白名单布局**(6 种),不引入 dateparse 任意格式推断 → 无解析歧义/性能陷阱 | 设计即有 |
| 🟢 `DATE_ADD(d, 巨大n)`:`time.AddDate` 溢出产生离奇年份字符串但**不 panic**,后续比较自然不命中 | 验证通过,无需改 |
| 🟢 `TODATEIN(tz, x)` 的 tz 来自规则/行数据:Go `time.LoadLocation` **拒绝路径穿越**(`..`、绝对路径报错),未知时区 → NULL | stdlib 保证,已复核 |

## 3. 集合判断(EXISTS/ANY/ALL/聚合子查询)

| 风险点 | 状态 |
|--------|------|
| 🟢 子查询只作用于**当前行的集合字段**——无外部数据源、无表连接 → 不存在 SSRF/越权读取面 | 设计即有 |
| 🟡 深嵌套子查询/括号 → 递归解析器栈耗尽(Go 栈溢出**不可恢复**,直接杀进程;约 10MB 括号文本可触发) | ✅ 本轮修复:解析深度上限 200(单点守卫覆盖全语法),超限为**加载期明确报错**;同时使"能解析必能求值"成立(VM 栈 256 > 201) |
| 🟢 嵌套行遍历 O(行内集合大小),空集语义符合 SQL(ANY 假/ALL 真),无放大 | 设计即有 |

## 4. 数学函数(ABS/ROUND/CEIL/FLOOR + SQRT/POW)

| 风险点 | 状态 |
|--------|------|
| 🟢 `ROUND(x, 巨大d)`:`10^d` 溢出/为零已守卫 → NULL;`POW` 非有限结果 → NULL;`SQRT(负)` → NULL;`AVG` 零除守卫 | 既有(第一、二轮) |
| 🟢 NaN/Inf 不进入比较真值(NULL 传播,两运行时一致) | 既有 |
| 🟢 `TOINT`/`UNSIGN` 越界浮点转换有显式范围守卫(避免实现相关行为) | 既有(第四轮) |

## 5. 数组操作(包含/交集/长度/定位)

| 风险点 | 状态 |
|--------|------|
| 🟢 `ARRAY_INDEX`/`ARRAY_SLICE` 负下标、越界全部夹紧或 NULL,无 panic 路径 | 既有,已复核 |
| 🟢 元素统一文本化 → 无类型混淆;交集 O(n+m) 哈希,受行大小约束 | 设计即有 |
| 🟢 标量函数作用于数组 → NULL(两运行时一致,第二轮对齐) | 既有 |

## 6. JSON 字段访问(JSON_EXTRACT/JSON_VALUE/JMESPATH)

| 风险点 | 状态 |
|--------|------|
| 🟢 文档解析走 `encoding/json`(stdlib 嵌套深度上限 10000,防解析炸弹);路径解析器为**手写迭代**,无递归 | 设计即有 |
| 🟢 路径/JMESPath 表达式**强制字符串字面量 + 加载期编译**——行数据无法注入查询语言(对比:若允许字段作路径,攻击者可用行数据探测任意结构) | 设计即有,已复核 |
| 🟢 非标量叶子(对象/数组)→ NULL,布尔规范渲染 `"true"/"false"`,无类型混淆 | 既有 |
| 🟡 JMESPath 投影在大数组上的每行成本 | 受行大小约束;表达式经编译缓存(键为规则字面量,**有界**) |

## 7. 正则匹配(REGEXP/RLIKE/REGEXP_LIKE)

| 风险点 | 状态 |
|--------|------|
| 🟢 **无 ReDoS**:Go RE2 线性时间,无回溯;规则模式**加载期预编译**,坏模式拒绝整条规则;编译体量受 Go regexp 内建限制(重复计数 ≤1000、程序尺寸限制) | 设计即有 |
| 🔴 词法层把 `\d` 脱成 `d` → 正则**静默弱化**(如手机号校验 `^1\d{10}$` 实际放行 `1dxxxxxxxxx`)——校验类规则被绕过属安全问题 | ✅ 第四轮已修:仅 `\' \" \\` 转义,其余保留 |
| 🟡 match_type 未知标志此前**静默忽略**(语义与作者预期漂移) | ✅ 第六轮已修:未知标志编译期报错 |
| 🟡 `URL_MATCHQS` 的模式参数可为**字段**(行数据驱动):① 每唯一值一次编译的 CPU 成本(动态模式固有);② 编译缓存以模式为键 → **无界内存增长** | ✅ 本轮修复②:缓存封顶 1024,超限编译不缓存;建议规则用字面量模式(文档已注) |

## 8. qlbridge 计算因子(80 个对齐名)

| 风险点 | 状态 |
|--------|------|
| 🟡 **qlbridge 解析器是第三方无维护代码**,对畸形文本的 panic 会穿透默认引擎 `engine.New()` | ✅ 本轮修复:`safeQLParse` panic 隔离 → 转为错误 → 走 native 回退(native 有严格报错+深度上限) |
| 🟢 `MD5/SHA1` 仅作指纹因子(源码已 `nolint:gosec` 注明**非安全用途**);`HASH_SIP` 固定密钥 (0,1) —— **不可当 MAC/鉴权用**(文档已注) | 既有标注 |
| 🟢 `uuid()` 拒绝实现(非确定性会破坏规则可重放/审计);`B64DECODE` 输出仅参与文本比较,无执行面 | 设计决策 |
| 🟢 `USERAGENT` 第三方解析为纯字符串处理;`EMAIL` 走 `net/mail` 严格解析 | 已复核 |
| 🟢 全部因子**total**:不 panic、不返回错误,坏输入 → NULL/false;元数**编译期**校验 | 设计即有 |

## 9. 本轮(第七轮)加固清单

| # | 加固 | 文件 |
|---|------|------|
| 1 | 解析深度上限 `maxParseDepth=200`(单点守卫全部嵌套构造;附:能解析 ⇒ VM 栈必够) | `pkg/ir/parser.go` |
| 2 | `SUBSTRING` 溢出夹紧(先 clamp 再相加) | `pkg/vm/vm.go`、`pkg/runtime/ast/ast.go` |
| 3 | `URL_MATCHQS` 模式缓存封顶(1024,LoadOrStore + 原子计数) | `pkg/sqlfn/parity.go` |
| 4 | qlbridge 解析 panic 隔离(→错误→native 回退) | `pkg/engine/frontend_qlbridge.go` |
| 5 | 求值 panic 遏制(按用户 recover,fail-safe 不命中,`EvalPanics()` 计数可观测;此前 worker goroutine panic = **整进程死亡**) | `pkg/engine/engine.go` |

回归:`pkg/ir/security_test.go`(深度上限/1MiB 恶意括号/平铺 5000 项不受影响)、
`pkg/vm/qlfn_parity_test.go: TestParitySubstrOverflow`(修复前该用例确定性 panic)、
`pkg/engine/security_test.go`(panic 遏制 + 隔离)、`pkg/sqlfn/parity_test.go`(缓存上界)。

## 10. Web / HTTP 面(第八轮追加)

评审传输层——规则文本经 `/rules/test`、`/evaluate` 从网络直达求值,以及运营控制台
(`pkg/web/editor.go`)对规则文本的回显。

| 面 | 风险点 | 状态 |
|----|--------|------|
| `/rules/test`(Manager.Test) | 🔴 **直接 `Compile`+`Eval` 未经 matchInto 遏制**;CLI `Serve`/`NewApp` 路径**不挂 recover 中间件** → 编译/求值 panic 打崩连接 goroutine(第七轮遗漏的第二个求值边界) | ✅ 本轮修复:`Manager.Test` 自带 panic 遏制 → 返回错误(与传输无关,覆盖 CLI/生产/未来调用方) |
| `/evaluate`、`/evaluate/all`(Explain) | 🟡 **第三个求值边界**:未信任规则文本/行经 `astrt.Explain` 直接遍历;`EvaluateAll` 循环中单行崩溃应跳过该规则而非中止整个 gate | ✅ 本轮修复:`safeExplain` 逐次遏制,panic 记为"未通过 + 原因" |
| 运营控制台回显(name/expr/note) | **存储型 XSS?** —— 规则名/表达式经列表接口回显 | ✅ **复核为安全**:`esc()` 转义 `&<>`,且全部值落在**元素文本内容**(非属性/`<script>`),该语境下转义 `<>&` 即足以阻断标记注入;`id`/`version` 为服务端 `int64` 数值。无需改动,已记录以防回归 |
| 传输层配额 | body 大小、请求速率 | 既有:Fiber 默认 BodyLimit 4MB;分组级限流 800 req/s + 读端点 500 req/s(`routes.go`) |
| 生产 vs CLI | `cmd/api` 挂 `middleware.Recover()`;`NewApp`/`Serve` 不挂 | HTTP 层遏制仅生产有——故上面三处求值边界**均下沉到库内自我遏制**,不依赖中间件 |

> **追加(第八轮末,panic 面专项复扫)**:对全仓 panic 源做穷举——显式 `panic()` 仅 4 处且均在
> 启动期(logger/metrics)或 DB 事务故意重抛(`tx.go`),不在求值路径;求值热路径的类型断言**全部**
> `,ok` 保护、两处浮点除法均有 `pow==0/Inf` 前置守卫、VM 栈 push/pop **全边界检查**、两处 `make`
> 长度非负(`argc`∈0-255、`end-start` 有 `start>end` 守卫);`regexp.MustCompile` **零使用**(全用
> `Compile`+错误处理)。**发现并修复 1 处**:JSON 规则前端 `JSONFrontend.toIR` 递归**无深度界**
> (`-frontend json` CLI 可达),深嵌套 JSON 可栈溢出——已加 `maxJSONDepth=200`,与 native 对齐。
> 另:`pkg/parser/json`、`pkg/dtable` 亦有无界递归 `toIR`,但**无任何服务/CLI 入口引用**(仅测试
> 可达),属库直调风险,已记录待按需加界。
>
> **第七轮改动复查**:①解析深度计数器每次 `Parse` 新建 parser、从 0 起 → `AddRule`/
> qlbridge 回退重解析无跨调用累积;②`parseSubSelect`/`parseAggSub` 的 WHERE 递归都经
> `parseUnary`(已守卫),函数深嵌套经 `parseTerm`(本轮前补的第二守卫点)——**无绕过守卫的递归路径**;
> ③`reCacheN` 并发下可能少量 overshoot,已注明无害且测试留 +8 余量;④`matchInto` 具名返回 `out=dst`
> 在 panic 时返回已收集匹配,语义为 fail-safe(已文档化)。

## 11. 协程与编译边界(第九轮:穷尽剩余 panic 逃逸面)

对全部 `go` 协程与编译路径逐一核对——协程内 panic 无法在别处 recover,逃逸即杀进程。

| 协程 / 路径 | 状态 |
|------------|------|
| kafka 分区 worker(`consumer.go:239`) | ✅ 既有内联 recover |
| kafka 关闭等待(`consumer.go:452`) | ✅ 仅 `WaitGroup.Wait`+`close`,无 panic 面 |
| redis / oracle 连接池监控 | ✅ 既有 `defer gos.Recover()` |
| 批量匹配 worker(`workerpool.go:73`) | ✅ 经 `matchInto` 的 recover(第七轮) |
| **文件热重载 watcher(`cli/run.go:215` → `Watcher.Run`)** | ✅ 本轮:`checkOnce` 经 `reloadSafely` 遏制 → 坏文件报错并保留现有规则集,循环存活;`Run` 自身仅 ticker/select 无 panic 面 |
| **规则编译收口(`Engine.compile`)** | ✅ 本轮:抽出 `compileOne` 逐条 recover——覆盖 `LoadRules`/`ReplaceRules`/`AddRule`(=`POST /rules` 上报)/`ReloadFromFile` 全部编译路径,编译 panic → "编译失败" 而非崩溃 |

**JSON 深度界补全**:第八轮补了 `engine.JSONFrontend`(入口可达);本轮补齐**独立** `pkg/parser/json`
解析器(含 EXISTS/ANY/ALL/agg 子查询递归)——`Parse` 入口加**有界深度预检**(在上限处即返回,
预检自身不溢出),零改动 6 个递归方法。二者深度界均为 200,与 native 对齐。

**更正第八轮一处不准确记录**:`pkg/dtable` 经复核为**扁平结构**(`Row.toIR` 遍历平铺 `[]Cond`,
`Cond.toIR` 只产叶子节点,无自递归),**无栈深度风险**,无需加界。此前"dtable 无界递归"的表述有误。

> 结论:全仓 `go` 协程与规则编译路径的 panic **均已遏制或确认无面**。规则**求值**(第七/八轮三个
> 边界)+ 规则**编译**(本轮收口)+ 规则**解析**(深度界,native/两个 JSON 解析器全覆盖)三段全绿。
> 新增回归:`pkg/parser/json/depth_test.go`、`TestSecurityCompilePanicContainment`。

## 12. LIKE 通配符与字符串/正则加固(第十轮,2026-07-03)

第十轮以功能审计带出三处加固(功能修复本身见 sql_feature_audit.md 第十一节):

| # | 风险点 | 状态 |
|---|--------|------|
| 1 | 🟡 **LIKE 语义缺口是"静默弱化"类风险**:`_` 与中间 `%` 此前按字面处理,`code LIKE '5__'` 之类的**校验/圈选规则永不按作者意图命中**(方向同第四轮的 `\d` 脱转义)。修复采用**加载期**把通配形状翻译为锚定 RE2:字面文本全部 `QuoteMeta`,模式仅由**规则字面量**构成(行数据不参与),RE2 线性匹配无 ReDoS;纯前缀/后缀/包含/等值仍走快速指令 | ✅ 本轮修复(`sqlfn.LikeRegexp`,VM 降为 `OpRegexp`,AST 同一翻译) |
| 2 | 🟡 **CEL/Expr 字面量含 `%`/`_` 的语义混叠**:`startsWith('50%')` 拼出的 LIKE 模式会把 `%` 当通配符 | ✅ 本轮修复:前端先 `LikeEscape` 再拼模式(`\%` `\_` `\\`),字面语义保真;`ir.Like` 零值(Wildcards=false)保留四形状字面分类,外部构造的 IR 行为不变 |
| 3 | 🟡 **REPLACE 内存放大 DoS**:规则文本不可信,`REPLACE(大字段, 'a', '20 字节')` 对 100KB 行数据投影 ~2MB、更长替换文本可达 GB 级单次分配(进程 OOM)。`CONCAT`/`JOIN` 为线性拼接,一并封顶防组合放大 | ✅ 本轮修复:**构建前**按 `strings.Count` 投影输出尺寸,超 `maxStringOut`(1 MiB)→ NULL(SQL 语义自然传播);良性用法不受影响 |
| 4 | 🟡 **正则模式尺寸无显式上界**:规则模式(REGEXP/LIKE 翻译)与 `URL_MATCHQS` 的行数据驱动模式,尺寸仅受 Go regexp 内部程序上限约束,失败形态依赖 stdlib 细节 | ✅ 本轮修复:统一上界 `sqlfn.MaxRegexpPattern`(4 KiB)——VM 加载期、AST 惰性编译、sqlfn 动态编译三处同界,超限为清晰错误 |

回归:`pkg/vm/like_wildcard_test.go`(通配语法 + 旧字面模式 + 尺寸上界 + bytecode==ast)、
`pkg/sqlfn/like_test.go`(翻译/转义/往返 + REPLACE·CONCAT·JOIN 封顶 + 动态模式上界)、
`pkg/parser/cel/cel_test.go`、`pkg/parser/expr/expr_test.go`(字面量转义)。

## 13. 非标量操作数统一 + SPLIT 放大防护(第十三轮,2026-07-03)

第十三轮以对抗视角专查**值模型边界**(数组/对象/异型 Go 值进入标量谓词路径)与
**计算数组的内存核算**,修复 2 类正确性漂移、加固 1 处放大向量:

| # | 风险点 | 状态 |
|---|--------|------|
| 1 | 🟠 **kArr 以 `""` 参与 VM 标量比较——规则语义漂移 + 双运行时分歧**:`tags = other_tags` 在字节码 VM 对**任意**两个数组字段恒真(kArr 渲染 "" 相等),`name = tags` 在 name 为空串时也真,`tags REGEXP '^'`、`tags LIKE '%'`、`tags IN ('')` 同理;AST 运行时(termVal 置 nil)全部判 false。**打分(VM)与解释(/evaluate,AST)结论相反**,且圈选规则可被空串/数组行数据意外命中——属"静默弱化"类。差分模糊器此前的标量比较模板从不抽取数组字段,故 12 轮未见 | ✅ 本轮修复:统一为"**非标量即 NULL**"——VM `compare`/`IN`/`LIKE`/`REGEXP` 增加 `scalarKind` 守卫,AST 以 `scalarRaw` 镜像;数组语义仅经 `ARRAY_*`/量词/子查询触达。模糊器新增第 24 类模板(数组×标量谓词)+ 行生成器混入 `[]string`/解析对象/类型化集合 |
| 2 | 🟠 **已解析 JSON 对象 / 类型化集合装载为 kUndef**:`map[string]any` 行字段在 VM 判 `IS NULL` 为**真**(AST 为假)——`profile IS NOT NULL AND JSON_EXTRACT(profile,'$.city')='深圳'` 这类防御性写法在解析态行数据上**永不命中**(特性 6 的实际使用陷阱);`[]map[string]any` 同理 | ✅ 本轮修复:新增 `kOpaque` 值类("存在但非标量"),`IS [NOT] NULL` 视为存在,其余标量路径视同 NULL;AST 以 `presentRaw` 镜像同一分类;无法表示的异型 Go 值(如 `time.Time`)双方一致判 NULL |
| 3 | 🟡 **SPLIT 无元素数上界——内存放大 DoS**:`[]string` 每元素 ~16 B 头部,`SPLIT(大字段, ',')` 可把 10 MiB 行值放大为 ~170 MiB 瞬时分配,× 规则 × 行 × worker;`maxStringOut`(第十轮)只覆盖字符串构建类,不覆盖数组构建 | ✅ 本轮修复:`maxArrayElems`(65536)——**分配前**以 `strings.Count` 计数,超限 → NULL;`DOMAINS`/`HOSTS` 的数组实参来自行字段或 SPLIT(已封顶),无独立放大面 |
| 4 | 🟢 复核无恙,顺带对齐:AST `litNum` 曾把字符串界的外部构造 `Between` 按数值求值(VM 拒编译)——对齐为"字符串不是数字"(仅影响库用户直构 IR);`quote()` 发射 CEL/Expr/Aviator 字面量补反斜杠转义(先 `\\` 后引号),消除含 `\` 值的目标语言转义混叠 | ✅ 一并修复 |

回归:`pkg/vm/array_scalar_test.go`(非标量×全谓词矩阵、kOpaque 存在性、BETWEEN
项边界、SPLIT 上界、外部 IR 钉死,全部断言 **bytecode == ast**);模糊器模板与行
生成器扩展后,该缺陷类纳入每次 `TestDifferentialFuzz` 的常规覆盖。

## 14. 未实施建议(Roadmap)

- 🔑 **控制台变更端点无鉴权**(`POST /rules`、`DELETE /rules/:id`、`POST /versions/:v/rollback`):
  企业部署下这是**首要加固项**。刻意不在库内硬编码鉴权(会给出虚假安全感,且租户/RBAC 模型需按部署
  定制)——正确做法是在 `cmd/api` 装配阶段对**变更分组**挂鉴权中间件(mTLS / OIDC / 网关签名),
  读/打分端点与变更端点分权。此为部署职责,已在此明确标注。
- **规则文本长度上限**(loader/API 层;深度上限已封死崩溃向量,长度仅关乎常规内存核算)。
- **每租户规则配额 + 变更审计日志**(谁在何时改了哪条规则)。
- **求值超时**——刻意不做:语言无循环/递归构造,单行求值最坏为 O(规则×行) 的微秒级直线代码,
  超时机制反而引入取消通道的常态开销。
- **`EvalPanics` 接入 Prometheus**(现为进程内计数器,接入 `pkg/metrics` 仅一行导出);非零值应告警。
