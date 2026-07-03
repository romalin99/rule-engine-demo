# 函数与表达式语法 (Functions & Expression Syntax)

面向**使用方（规则作者）**的完整参考:一条规则能写什么、每个算子/函数怎么用、
边界在哪。规则的底层节点形状见 [ir.md](ir.md);本页所有能力由原生 SQL 语法
(`pkg/ir`) 解析,并由 `bytecode`(默认)与 `ast` 两个运行时求值。

---

## 1. 规则是什么 (What a rule is)

一条规则是一个**布尔谓词表达式**(SQL `WHERE` 的主体),对**一行宽表数据**
`map[string]any` 求值,返回 `true`(命中)/ `false`(不命中)。

- 它**不是**完整 SQL:没有 `SELECT … FROM …`,也没有结尾 `;`。只写谓词本身。
- 空白与换行随意,会被忽略。
- 关键字(`AND`/`OR`/`BETWEEN`/`LIKE`…)与函数名**大小写不敏感**。
- 字符串字面量用单引号 `'…'` 或双引号 `"…"`;字符串内可用 `\` 转义(如 `'it\'s'`)。

```
规则: age >= 18 AND province = '广东'
数据: {"age": 20, "province": "广东"}
结果: 命中 (true)
```

---

## 2. 数据类型 (Types)

| 类型     | 来源(行字段值)                                  |
| -------- | ----------------------------------------------- |
| 数字     | `int` / `int64` / `float64` 等数值类型          |
| 字符串   | `string`                                        |
| 布尔     | `bool`                                          |
| 数组     | `[]string` 或 `[]any`(元素按文本处理)          |
| 对象     | `map[string]any`(已解析 JSON 对象)、`[]map[string]any`(类型化嵌套行集合) |
| NULL     | 字段**缺失**、值为 `nil`,或值模型无法表示的其他 Go 类型 |

**非标量语义**(第十三轮统一):数组与对象是**存在的值**——`tags IS NOT NULL`、
`profile IS NOT NULL` 成立——但它们**不是标量**,任何比较、`IN`、`LIKE`、`REGEXP`
与标量函数作用其上都视同 NULL(不命中/传播 NULL)。要触达其内容,用对应的结构化
算子:数组用 `ARRAY_*` / `ANY` / `ALL`,对象用 `JSON_EXTRACT` / `JMESPATH` /
`MAPKEYS`,嵌套行集合用 `EXISTS` / 子查询。因此
`profile IS NOT NULL AND JSON_EXTRACT(profile, '$.city') = '深圳'` 对
**JSON 字符串**与**已解析对象**两种行形态都成立。

**比较语义**:两侧都是数字时按**数值**比较,否则按**字符串(字典序)**。日期以
ISO 字符串存放时,字典序即时间序(`'2026-01-01' < '2026-02-01'`)。比较的**任意一侧**
都可以是字段、字面量或函数调用,例如 `LENGTH(a) = LENGTH(b)`、`last_login >= CURRENT_DATE`。

**布尔字段**在字符串上下文一律渲染为 `'true'`/`'false'`(两套运行时一致):
`flag = 'true'`、`is_vip = is_active`、`UPPER(flag) = 'TRUE'`、`flag REGEXP '^tr'`、
`flag = ANY(SELECT passed FROM checks)` 均按此语义求值;数学函数(`ABS` 等)对布尔
操作数返回 NULL,`TOINT(flag)` 可显式转 1/0。

**数字字面量**支持整数、小数与**负数**(`-` 紧跟数字即负号,本语法无二元减法),可用于任意
操作数位置:`temperature < -10`、`balance >= -100`、`lat BETWEEN -90 AND 90`、
`offset IN (-1, 0, 1)`、`DATE_ADD(d, -7)`、`ROUND(x, -1)`。

---

## 3. NULL 语义(重要)(NULL semantics)

本引擎使用**两值逻辑**(不是 SQL 的三值逻辑),请务必注意:

- 缺失或 `nil` 的字段视为 **NULL**。
- 函数对 NULL 求值结果仍为 **NULL**(传播):`LOWER(缺失)` → NULL。
- **任意一侧为 NULL 的比较都判为 `false`**(不命中),`=` 和 `<>` 都如此。
- `NOT(...)` 对**布尔结果**取反:因 NULL 比较得 `false`,故 `NOT (field = v)` 在
  field 缺失时为 `true`。

```
规则: risk_level = '高'          数据: {}  → 不命中 (NULL = '高' 判 false)
规则: risk_level <> '高'         数据: {}  → 不命中 (NULL <> '高' 也判 false)
规则: NOT (risk_level = '高')    数据: {}  → 命中   (NOT(false) = true)
```

> **要求字段必须存在**时,显式加 `IS NOT NULL`:
> `risk_level IS NOT NULL AND risk_level <> '高'`(此时 `{}` 不命中)。

---

## 4. 运算符 (Operators)

| 类别 | 形式 |
| ---- | ---- |
| 比较 | `=` `==` `!=` `<>` `>` `>=` `<` `<=` |
| 区间 | `x BETWEEN lo AND hi`(边界可为**字面量、字段或函数**:`age BETWEEN min_age AND max_age`、`d BETWEEN DATE_SUB(CURRENT_DATE, 7) AND CURRENT_DATE`) |
| 集合 | `x IN (a, b, …)` / `x NOT IN (…)`；子查询形式 `x [NOT] IN (SELECT col FROM coll [WHERE …])`（脱糖为 `= ANY` / `!= ALL`，两值逻辑） |
| 模糊 | `x LIKE 'a%'` / `x NOT LIKE '%b%'`;完整 SQL 通配符:`%` 任意位置(含中间)、`_` 恰好一个字符、`\%` `\_` 转义为字面量 |
| 正则 | `x REGEXP '正则'` / `x NOT REGEXP '...'`(Go **RE2** 语法;`RLIKE` 同义;另见 [§5.6](#56-正则匹配-regexp)) |
| 空值 | `x IS NULL` / `x IS NOT NULL` |
| 逻辑 | `AND`、`OR`、`NOT (…)`、括号、任意层嵌套 |

```
规则: gender = '男'                              数据: {"gender":"男"}            → 命中
规则: age BETWEEN 25 AND 40                      数据: {"age":30}                 → 命中
规则: province IN ('广东','江苏')                数据: {"province":"江苏"}        → 命中
规则: income_level NOT IN ('<5k','5k-10k')       数据: {"income_level":"20k-30k"} → 命中
规则: name LIKE '数%'                            数据: {"name":"数码城"}          → 命中  (前缀)
规则: name LIKE '%店'                            数据: {"name":"便利店"}          → 命中  (后缀)
规则: name LIKE '%码%'                           数据: {"name":"数码城"}          → 命中  (包含)
规则: name LIKE '数%城'                          数据: {"name":"数码城"}          → 命中  (中间 %)
规则: code LIKE '5__'                            数据: {"code":"512"}             → 命中  (_ 单字符)
规则: pct  LIKE '50\%'                           数据: {"pct":"50%"}              → 命中  (转义 %)
规则: phone IS NULL                              数据: {}                         → 命中
规则: phone IS NOT NULL                          数据: {"phone":"139..."}         → 命中
规则: (vip_level >= 3 OR order_count >= 30)      数据: {"vip_level":4}            → 命中
规则: age >= 25 AND NOT (province IN ('北京'))   数据: {"age":30,"province":"广东"} → 命中
```

> `<>` 是 `!=` 的 SQL 别名(等价)。`LIKE` 支持**完整 SQL 通配符语法**(第八轮):`%` 匹配任意
> 字符串(可出现在中间)、`_` 匹配恰好一个字符、反斜杠转义(`\%` `\_` `\\`)表示字面量。
> 纯前缀/后缀/包含/等值形状仍走原有的快速字符串指令(零性能回退);其余形状在**加载期**
> 翻译为锚定 RE2 并预编译(文本经 QuoteMeta 转义,行数据不参与模式构造)。CEL/Expr 前端的
> `startsWith/endsWith/contains` 字面量会先做 LIKE 转义,`'50%'` 之类的文本保持字面语义。
>
> `IN` 按**文本**匹配:数字字面量以书写形式参与比较,数字字段按规范渲染(无多余小数位)。
> 因此请规范书写数字——`IN (1, 2)` 能匹配数值 1,而 `IN (1.0)`、`IN (01)` 不能(与 MySQL 的
> 数值强转不同,本引擎不做隐式转换;需要数值语义时可写 `x = 1.0 OR x = 2.5` 或 `x = ANY(1.0, 2.5)`,
> 比较两侧数字时按数值比较)。

---

## 5. 函数 (Functions)

所有函数都可以出现在**比较两侧**,也可以作为 `BETWEEN`/`IN`/`LIKE`/`IS NULL` 的操作数
(见 [§6](#6-函数用作谓词操作数-functions-as-predicate-operands))。

### 5.1 字符串函数 (String)

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `LOWER(x)` / `UPPER(x)` | 字符串 | 转小写 / 大写 |
| `TRIM(x)` | 字符串 | 去除首尾空白 |
| `LENGTH(x)` | 数字 | **字符数(rune)**,中文安全;数组操作数返回**元素个数**(同 `ARRAY_LENGTH`) |
| `SUBSTRING(s, start, len)` / `SUBSTR` | 字符串 | **从 1 开始**、按 rune 截取;越界截断为 `''` |
| `SUBSTRING(s, start)` / `SUBSTR` | 字符串 | 两参形式:从 `start`(1 起)截到**字符串末尾** |

```
规则: LOWER(name) = 'abc'              数据: {"name":"ABC"}          → 命中
规则: UPPER(code) = 'GD'               数据: {"code":"gd"}           → 命中
规则: TRIM(city) = '北京'              数据: {"city":"  北京  "}     → 命中
规则: LENGTH(name) = 3                 数据: {"name":"数码城"}        → 命中  (3 个字符)
规则: SUBSTRING(phone, 1, 3) = '139'   数据: {"phone":"13912345678"} → 命中
规则: SUBSTRING(phone, 8) = '5678'     数据: {"phone":"13912345678"} → 命中  (第 8 位到末尾)
```

### 5.2 数学函数 (Math)

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `ABS(x)` | 数字 | 绝对值 |
| `ROUND(x)` | 数字 | 就近取整(四舍五入,远离零方向) |
| `ROUND(x, d)` | 数字 | 保留 `d` 位小数(`d` 可为负,按十/百位取整);四舍五入、远离零 |
| `CEIL(x)` / `CEILING(x)` | 数字 | 向上取整 |
| `FLOOR(x)` | 数字 | 向下取整 |

> 数学函数要求操作数是数字;非数字或 NULL 一律返回 NULL。

```
规则: ABS(delta) <= 5      数据: {"delta":-3}     → 命中
规则: ROUND(score) = 86    数据: {"score":85.6}   → 命中
规则: ROUND(price, 2) = 3.14   数据: {"price":3.14159} → 命中  (保留 2 位小数)
规则: CEIL(score) = 86     数据: {"score":85.1}   → 命中
规则: FLOOR(score) = 85    数据: {"score":85.9}   → 命中
```

### 5.3 日期函数 (Date)

日期按 **ISO 字符串**处理(字典序=时间序)。可解析的输入格式:
`2006-01-02 15:04:05`、`2006-01-02T15:04:05Z07:00`(RFC3339)、`2006-01-02T15:04:05`、`2006-01-02`,以及斜杠变体 `2006/01/02 15:04:05`、`2006/01/02`(第八轮起与扩展日期函数 `MM/DAYOFWEEK/TODATE/…` 的接受集合一致;`DATE_ADD` 等的输出统一渲染为 ISO 连字符格式)。

> **陷阱**:比较运算符与 `BETWEEN` 对日期是**纯字典序**(不做日期解析)。同一种格式内字典序=时间序,
> 但**混用**连字符与斜杠格式会得到错误顺序(`'2026/06/15' > '2026-12-31'`,因 `/` 的码位大于 `-`)。
> 混合格式数据请先用 `TODATE(x)` 归一化再比较:`TODATE(reg) BETWEEN '2026-01-01' AND '2026-12-31'`。

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `CURRENT_DATE` | 字符串 `YYYY-MM-DD` | **裸关键字,无括号**,取当天;可用于比较**任意一侧**(如 `CURRENT_DATE >= last_login`) |
| `CURRENT_TIMESTAMP` | 字符串 `YYYY-MM-DD HH:MM:SS` | 裸关键字,取当前时刻;可用于比较任意一侧 |
| `YEAR(x)` / `MONTH(x)` / `DAY(x)` | 数字 | 从日期字符串提取年/月/日 |
| `DATEDIFF(a, b)` | 数字 | 整天数 `a − b`(截断) |
| `DATE_ADD(d, n [, unit])` | 字符串 | 日期**加** n 个单位(省略 unit 时按 **DAY**);保留输入精度(日期 / 日期时间) |
| `DATE_SUB(d, n [, unit])` | 字符串 | 日期**减** n 个单位(省略 unit 时按 **DAY**) |

> `unit` 大小写不敏感、可带复数 `s`,取值:`DAY` `WEEK` `MONTH` `YEAR` `HOUR` `MINUTE` `SECOND`。
> 月/年加减按日历进位(如 `DATE_ADD('2026-01-31', 1, 'MONTH')` → `2026-03-03`)。
> 日期参数为 NULL、n 非数字或日期无法解析时返回 **NULL**。

```
规则: register_date <= CURRENT_DATE             数据: {"register_date":"2020-01-01"} → 命中
规则: CURRENT_DATE >= register_date             数据: {"register_date":"2020-01-01"} → 命中  (裸关键字作左操作数)
规则: register_date BETWEEN '2020-01-01' AND '2020-12-31'  数据: {"register_date":"2020-06-15"} → 命中  (日期区间)
规则: YEAR(birthday) = 2000                     数据: {"birthday":"2000-06-15"}      → 命中
规则: MONTH(birthday) = 6                        数据: {"birthday":"2000-06-15"}      → 命中
规则: DAY(birthday) = 15                          数据: {"birthday":"2000-06-15 08:30:00"} → 命中
规则: DATEDIFF('2026-06-28', '2026-06-01') = 27   数据: {}                             → 命中
规则: DATEDIFF(CURRENT_DATE, last_login) <= 30    数据: {"last_login":"2026-06-10"}    → 视当天而定
规则: DATE_ADD('2026-06-01', 30) = '2026-07-01'   数据: {}                             → 命中
规则: DATE_SUB(CURRENT_DATE, 7) <= last_login     数据: {"last_login":"2026-06-26"}    → 视当天而定(近 7 天)
规则: DATE_ADD(start, 1, 'MONTH') >= deadline     数据: {"start":"2026-01-15","deadline":"2026-02-10"} → 命中
```

### 5.4 数组函数 (Array)

数组函数读取行里的**列表字段**(`[]string` 或 `[]any`,元素按文本比较)。
**数组参数必须是字段引用**。

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `ARRAY_LENGTH(field)` | 数字 | 元素个数 |
| `ARRAY_CONTAINS(field, v)` | 布尔 | `v`(字符串或数字)是否为元素 |
| `ARRAY_OVERLAP(field, v1, v2, …)` | 布尔 | 是否含任一**字面量** `vi`(脱糖为 `ARRAY_CONTAINS` 的 `OR`) |
| `ARRAY_INTERSECT(fieldA, fieldB)` | 布尔 | 两个**数组字段**是否有交集(交集非空);元素按文本比较,空/缺失一侧为 `false` |

> `ARRAY_CONTAINS` / `ARRAY_OVERLAP` 本身就是完整布尔谓词;`ARRAY_LENGTH` 返回数字,
> 用于比较式中。

```
规则: ARRAY_LENGTH(tags) >= 2              数据: {"tags":["vip","new","gold"]} → 命中
规则: ARRAY_CONTAINS(tags, 'vip')          数据: {"tags":["vip","new"]}        → 命中
规则: ARRAY_CONTAINS(ids, 5)               数据: {"ids":[1,5,9]}               → 命中  (按文本 "5")
规则: ARRAY_OVERLAP(tags, 'gold', 'vip')   数据: {"tags":["vip"]}              → 命中
规则: ARRAY_INTERSECT(tags, targets)       数据: {"tags":["vip","new"],"targets":["gold","new"]} → 命中  (共有 "new")
```

> `ARRAY_OVERLAP` 的比较值是**字面量列表**;若要判断两个**数组字段**的交集(如用户标签 ∩ 活动目标标签),用 `ARRAY_INTERSECT(a, b)`。

### 5.5 集合判断:EXISTS / ANY / ALL (Set predicates)

引擎对**单行**求值,这里的"集合 / 子查询"指当前行里的**集合字段**:标量数组
(`[]string` / `[]any`),或**嵌套对象数组**(`[]map[string]any` / `[]any` of maps)。

**量词 ANY / ALL**(`SOME` 是 `ANY` 的别名)——把左操作数与一组值逐一比较。
左操作数须是字段或函数调用(与其它谓词一致):

| 形式 | 含义 |
| ---- | ---- |
| `x <op> ANY (v1, v2, …)` | 存在某个 `vi` 使 `x op vi`(脱糖为 `OR`) |
| `x <op> ALL (v1, v2, …)` | 对每个 `vi` 都有 `x op vi`(脱糖为 `AND`) |
| `x <op> ANY (arrField)` | 对数组字段的**任一元素**成立 |
| `x <op> ALL (arrField)` | 对数组字段的**所有元素**成立 |
| `x <op> ANY/ALL (数组函数(…))` | 对**计算数组**的元素成立:`'b' = ANY(SPLIT(csv, ','))`、`x = ANY(MAPKEYS(attrs))`(识别 `SPLIT`/`MAPKEYS`/`MAPVALUES`/`ARRAY_SLICE`/`DOMAINS`/`HOSTS`;标量函数保持"单值列表"的普通比较语义) |

> 空集 / 缺失数组的语义同 SQL:`ANY` 为假、`ALL` 为真。元素在左操作数为数字且元素可解析为数字时按**数值**比较,否则按字符串。

**EXISTS**——判断集合是否存在(满足条件的)元素:

| 形式 | 含义 |
| ---- | ---- |
| `EXISTS (coll)` | 集合字段**非空**(标量或对象数组皆可) |
| `EXISTS (SELECT 1 FROM coll WHERE <pred>)` | 对象数组中**存在**满足 `<pred>` 的嵌套行 |
| `NOT EXISTS (...)` | 取反 |

**子查询量词**——把外层值与嵌套行的投影列逐一比较:

| 形式 | 含义 |
| ---- | ---- |
| `x <op> ANY (SELECT col FROM coll [WHERE pred])` | 存在满足 `pred` 的嵌套行使 `x op row.col` |
| `x <op> ALL (SELECT col FROM coll [WHERE pred])` | 所有满足 `pred` 的嵌套行都有 `x op row.col` |

> 子查询里的 `WHERE` 谓词在**嵌套行**上求值:其标识符指向嵌套行的列。`SELECT` 投影**单列**(供 `ANY/ALL` 比较),或写 `1` / `*`(`EXISTS` 用)。

```
# 标量数组 / 值列表
规则: city = ANY('北京','上海','广州')   数据: {"city":"上海"}                 → 命中
规则: score >= ALL(60, 70, 80)          数据: {"score":85}                    → 命中
规则: target = ANY(tags)                 数据: {"tags":["vip","target"]}       → 命中
规则: floor < ANY(scores)                数据: {"floor":90,"scores":[70,95]}   → 命中 (95>90)

# EXISTS:非空 / 嵌套对象子查询
规则: EXISTS(tags)                                          数据: {"tags":["vip"]}             → 命中
规则: EXISTS(SELECT 1 FROM orders WHERE amount > 100)       数据: {"orders":[{"amount":120}]}  → 命中
规则: NOT EXISTS(SELECT 1 FROM orders WHERE status='退款')  数据: {"orders":[{"status":"已付"}]} → 命中

# 子查询量词:外层值 vs 嵌套行投影列(仅看满足 WHERE 的行)
规则: budget >= ALL(SELECT amount FROM orders WHERE status='已付')
  数据: {"budget":500,"orders":[{"amount":120,"status":"已付"},{"amount":900,"status":"退款"}]} → 命中 (仅已付 120)
```

**标量聚合子查询**——`( SELECT 聚合(col|*) FROM 集合字段 [WHERE pred] )` 作为**标量操作数**
参与比较,对嵌套对象数组做聚合:

| 形式 | 含义 |
| ---- | ---- |
| `(SELECT COUNT(*) FROM coll [WHERE p]) <op> n` | 满足 p 的嵌套行数 |
| `(SELECT SUM(col) FROM coll [WHERE p]) <op> n` | col 求和(同理 `AVG`/`MIN`/`MAX`) |
| `x <op> (SELECT SUM(col) FROM coll …)` | 聚合也可作右操作数 |

> `COUNT` 支持 `*`(行数)或 `COUNT(col)`(非空计数);`SUM/MIN/MAX/AVG` 需数值列。
> 空集语义:`COUNT`→0;`SUM/MIN/MAX/AVG`→**NULL**(故比较判 false,可用 `IS NULL` 检测)。

```
数据: {"budget":500,"orders":[{"amount":120,"status":"已付","qty":2},{"amount":80,"status":"已付","qty":1},{"amount":50,"status":"退款","qty":3}]}
规则: (SELECT COUNT(*) FROM orders WHERE status='已付') >= 2   → 命中 (2 单)
规则: (SELECT SUM(amount) FROM orders) >= 200                  → 命中 (250)
规则: (SELECT AVG(amount) FROM orders WHERE status='已付') = 100 → 命中
规则: budget > (SELECT SUM(amount) FROM orders)               → 命中 (500>250)
```

### 5.6 正则匹配 (REGEXP)

`x REGEXP '正则'` 用 **Go RE2** 语法对左操作数(先渲染为字符串)做正则匹配;`RLIKE`
是同义词。函数形式 `REGEXP_LIKE(x, '正则' [, match_type])` 等价,支持 MySQL 的
match_type 标志(折叠为模式前缀 `(?i)`/`(?m)`/`(?s)`):`i` 大小写不敏感、`c` 大小写敏感
(i/c 同时出现时**最右者生效**,MySQL 规则)、`m` 多行锚点、`n` 让 `.` 匹配换行、
`u` 接受但无操作(RE2 本就只认 Unix 行尾);**未知标志在编译期报错**。
左操作数为 NULL 时不匹配。

| 形式 | 含义 |
| ---- | ---- |
| `x REGEXP 'p'` / `x RLIKE 'p'` | x 匹配正则 p |
| `x NOT REGEXP 'p'` | x 不匹配 p |
| `REGEXP_LIKE(x, 'p')` | 同 `x REGEXP 'p'` |
| `REGEXP_LIKE(x, 'p', 'i')` | 大小写不敏感匹配(`'ci'` 则敏感;`'in'` = 不敏感 + `.` 跨行) |

> 字符串字面量中反斜杠仅转义 `\'` `\"` `\\`,其余 `\x` 原样保留——正则可直接写
> `'^1\d{10}$'`,MySQL 风格的 `'^1\\d{10}$'` 同样有效(二者等价)。

> 模式在加载期**预编译**一次(RE2,线性时间、无回溯灾难)。左操作数可为字段或函数调用,
> 如 `LOWER(name) REGEXP '^a'`、`JSON_EXTRACT(p, '$.city') REGEXP '深'`。

```
规则: phone REGEXP '^139[0-9]{8}$'    数据: {"phone":"13912345678"} → 命中
规则: name NOT REGEXP '[0-9]'         数据: {"name":"Alice"}        → 命中
规则: REGEXP_LIKE(name, 'alice', 'i') 数据: {"name":"Alice123"}     → 命中
```

### 5.7 JSON 字段访问 (JSON_EXTRACT)

`JSON_EXTRACT(doc, '$.path')`(`JSON_VALUE` 同义)从 JSON 文档取出**标量叶子值**。
`doc` 可为 **JSON 字符串字段**(自动解析)或行里**已解析的对象**(`map[string]any`)。
路径以 `$` 开头,支持 `.key`、`[下标]`、`["含点的键"]`,如 `$.a.b[0]`。

| 取出的 JSON 类型 | 返回 |
| ---------------- | ---- |
| 字符串 | 字符串(**不带引号**,故可直接 `= '深圳'`) |
| 数字 | 数字 |
| 布尔 | 字符串 `'true'` / `'false'` |
| null / 不存在 / 对象 / 数组 | NULL |

> 仅取**标量**:对象/数组叶子返回 NULL,要更深请写更长的路径(`$.addr.zip`)。路径须为字符串字面量。

```
数据: {"profile":"{\"city\":\"深圳\",\"age\":30,\"addr\":{\"zip\":\"518000\"}}"}
规则: JSON_EXTRACT(profile, '$.city') = '深圳'        → 命中
规则: JSON_EXTRACT(profile, '$.age') >= 18            → 命中
规则: JSON_EXTRACT(profile, '$.addr.zip') = '518000'  → 命中
规则: JSON_EXTRACT(profile, '$.missing') IS NULL      → 命中
```

### 5.8 扩展函数库:qlbridge 计算因子对齐 (Extended builtins)

以下函数补齐 [qlbridge](https://github.com/araddon/qlbridge) 内置计算因子(`pkg/sqlfn`,
两套运行时同实现)。**NULL 传播**:任一必需参数为 NULL 时结果为 NULL;布尔谓词函数遇 NULL 判 `false`。
函数名大小写不敏感;qlbridge 中带点的名字(`hash.md5`)改为下划线/别名形式(`HASH_MD5`/`MD5`)。

**字符串**(qlbridge: `tolower/strip/replace/split/join/contains/hasprefix/hassuffix`):

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `TOLOWER(x)` / `TOUPPER(x)` | 字符串 | `LOWER`/`UPPER` 的 qlbridge 别名 |
| `STRIP(x)` | 字符串 | 去首尾空白(同 `TRIM`) |
| `CHAR_LENGTH(x)` | 数字 | 字符数(rune;仅字符串) |
| `REPLACE(s, old [, new])` | 字符串 | 全量替换;省略 `new` 即删除 `old` |
| `SPLIT(s, sep)` | 数组 | 按 `sep` 切分;可与 `ARRAY_*` 组合,如 `ARRAY_CONTAINS(SPLIT(csv,','),'x')`;空分隔符 → NULL;超过 65536 个元素 → NULL(内存放大防护,见 security_review §13) |
| `JOIN(v1, …, sep)` | 字符串 | 末参为分隔符;数组参数展开其元素;NULL 参数跳过 |
| `CONCAT(v1, v2, …)` | 字符串 | 拼接(MySQL 语义:任一参数 NULL → NULL) |
| `CONTAINS(s, sub)` | **布尔** | 子串判断,可独立作谓词 |
| `STARTSWITH` / `HASPREFIX(s, p)` | **布尔** | 前缀判断 |
| `ENDSWITH` / `HASSUFFIX(s, p)` | **布尔** | 后缀判断 |

**函数式比较**(qlbridge: `eq/ne/gt/ge/lt/le`;语义与运算符完全一致,NULL → false):
`EQ(a,b)` `NE(a,b)` `GT(a,b)` `GE(a,b)` `LT(a,b)` `LE(a,b)`,如 `GT(LENGTH(name), 2)`。
比较(无论运算符还是函数形式)**不定义在数组操作数上**——判数组请用 `ARRAY_*` 谓词。

**数学与类型转换**(qlbridge: `sqrt/pow/toint/tonumber/tobool/oneof`):

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `SQRT(x)` | 数字 | 平方根;负数 → NULL |
| `POW(x, y)` / `POWER` | 数字 | 幂;结果非有限 → NULL |
| `TOINT(x)` | 数字 | 取整(向零截断);字符串先去空格与千分位逗号;布尔 → 1/0 |
| `TONUMBER(x)` | 数字 | 同上,不截断 |
| `TOBOOL(x)` | 布尔值 | `true/t/1/yes/y/on` 与 `false/f/0/no/n/off`(不区分大小写);数字 0=false;比较时渲染为 `'true'`/`'false'` |
| `TOSTRING(x)` | 字符串 | 渲染为文本 |
| `ONEOF(v1, v2, …)` / `COALESCE` | 任意 | 第一个非 NULL 参数 |

**日期时间**(qlbridge: `now/todate/totimestamp/dayofweek/hourofday/hourofweek/monthofyear/yy/mm/yymm`;
`[x]` 表示参数可省,省略取当前时刻):

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `NOW()` | 字符串 | 当前时刻(带括号形式的 `CURRENT_TIMESTAMP`) |
| `TODATE(x)` / `TODATE(layout, x)` | 字符串 | 归一化为 `YYYY-MM-DD`;两参形式用 Go 参考布局解析(qlbridge 参数序) |
| `TOTIMESTAMP(x)` | 数字 | Unix 秒;`UNIX_TIMESTAMP([x])` 同义(可无参) |
| `HOUR(x)` / `MINUTE(x)` / `SECOND(x)` | 数字 | 时/分/秒(纯日期视为 0 点) |
| `DAYOFWEEK([x])` | 数字 | **0=周日 … 6=周六**(Go/qlbridge 编号,注意与 MySQL 的 1 起不同) |
| `HOUROFDAY([x])` | 数字 | 0-23 |
| `HOUROFWEEK([x])` | 数字 | 0-167(周日起 `weekday*24+hour`) |
| `MONTHOFYEAR([x])` / `MM([x])` | 数字 | 1-12 |
| `YY([x])` | 数字 | 两位年(2026 → 26) |
| `YYMM([x])` | 字符串 | 年月批次,如 `'2607'` |

**邮箱与 URL**(qlbridge: `email/emailname/emaildomain/host/domain/path/qs/urldecode/urlmain/urlminusqs`):

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `EMAIL(x)` | 字符串 | 规范化地址(小写、去显示名);非法 → NULL |
| `EMAILNAME(x)` / `EMAILDOMAIN(x)` | 字符串 | `@` 前的本地部分 / `@` 后的域名 |
| `HOST(x)` | 字符串 | URL 主机(小写、去端口);无 scheme 的输入自动按 `http://` 解析 |
| `DOMAIN(x)` | 字符串 | 基础域名(主机最后两级标签,朴素实现、无公共后缀表) |
| `PATH(x)` / `URLPATH(x)` | 字符串 | 路径部分 |
| `QS(x, key)` | 字符串 | 查询串参数值;参数不存在 → NULL |
| `URLDECODE(x)` | 字符串 | 百分号解码(`+` 视为空格) |
| `URLMAIN(x)` | 字符串 | 去掉查询串与锚点的 URL |
| `URLMINUSQS(x, key)` | 字符串 | 删除指定查询参数后的 URL(其余参数按键排序重编码) |

**哈希与编码**(qlbridge: `hash.md5/hash.sha1/hash.sha256/hash.sha512/encoding.b64encode/encoding.b64decode`):
`MD5(x)`/`HASH_MD5`、`SHA1`/`HASH_SHA1`、`SHA256`/`HASH_SHA256`、`SHA512`/`HASH_SHA512`(小写十六进制),
`B64ENCODE(x)`、`B64DECODE(x)`(标准 base64;解码失败 → NULL)。

**数组定位**(qlbridge: `array.index/array.slice`;判含/交集/长度见 [§5.4](#54-数组函数-array)):

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `ARRAY_INDEX(arr, i)` | 字符串 | 第 `i` 个元素(**0 起**,qlbridge 编号);越界/负下标 → NULL |
| `ARRAY_SLICE(arr, start [, end])` | 数组 | 半开区间 `[start, end)`(0 起);负下标从末尾数;越界收敛;`end` 省略取到末尾 |

**CAST 语法**(qlbridge: `cast`):`CAST(x AS 类型)` 是语法糖,解析期脱糖为对应转换函数——
`INT/INTEGER/BIGINT/SMALLINT → TOINT`、`FLOAT/DOUBLE/DECIMAL/NUMERIC/NUMBER/REAL → TONUMBER`、
`STRING/CHAR/VARCHAR/TEXT → TOSTRING`、`BOOL/BOOLEAN → TOBOOL`、`DATE → TODATE`、
`TIMESTAMP/DATETIME → TOTIMESTAMP`(Unix 秒)。导出的 SQL 为脱糖形式(如 `TOINT(x)`)。

**行级匹配**(qlbridge: `match` + `exists(match(...))`):

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `MATCH('prefix' [, 'prefix2', …])` | **布尔** | 当前行存在**字段名以任一前缀开头**且值非 NULL 的字段;前缀须为非空字符串字面量 |

**Map 字段**(qlbridge: `mapkeys/mapvalues`;操作数须为**字段引用**,可为 `map[string]any` 或 JSON 对象字符串):

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `MAPKEYS(field)` | 数组 | 键名,**按字典序排序**(保证求值确定性) |
| `MAPVALUES(field)` | 数组 | 值(按排序后的键序渲染为文本) |

**JMESPath**(qlbridge: `json.jmespath`;表达式须为字符串字面量,加载期预编译,语法错误在编译时报出):

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `JMESPATH(doc, 'expr')` / `JSON_JMESPATH` | 标量或数组 | 完整 [JMESPath](https://jmespath.org) 查询:过滤 `[?a==\`1\`]`、投影、`length(@)` 等;`doc` 同 `JSON_EXTRACT`(JSON 字符串或已解析对象字段);标量直返,**全标量数组返回数组**(可喂给 `ARRAY_*`),对象/混合数组 → NULL |

**User-Agent**(qlbridge: `useragent`):`USERAGENT(ua, part)`,`part` 取
`bot`/`mobile`(布尔)、`browser`/`browser_version`/`engine`/`engine_version`/`os`/`platform`/`mozilla`/`localization`(字符串,缺失 → NULL)。

**SipHash 与时区**(qlbridge: `hash.sip/todatein`):

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `HASH_SIP(x)` / `SIPHASH(x)` | 字符串 | SipHash-2-4(固定密钥 k0=0,k1=1),64 位结果以**十进制字符串**返回(float64 存不下全部 uint64) |
| `TODATEIN(tz, x)` | 字符串 | 把 `x` 按 IANA 时区 `tz` 的墙钟时间解析,**归一化为 UTC** 的 `YYYY-MM-DD HH:MM:SS`;未知时区/无法解析 → NULL(依赖宿主 tzdata) |

**批量域名/主机**(qlbridge: `domains/hosts`):`DOMAINS(v1, …)`、`HOSTS(v1, …)`——对每个 URL 参数
(数组参数展开)提取基础域名/主机,去重保序返回数组;无有效结果 → NULL。

**strftime 格式化**(qlbridge: `strftime/extract`):`STRFTIME(x, fmt)`、`EXTRACT(x, fmt)`(同义)。
支持 `%Y %y %m %d %e %H %I %M %S %p %a %A %b %B %j %w %s %%`,未识别的代码原样输出。
如 `EXTRACT(ts, '%H') = '15'`、`STRFTIME(d, '%Y-%m') = '2026-07'`(返回**字符串**)。

**第三批对齐因子**(2026-07-02 与 qlbridge 注册表逐名比对后补齐;qlbridge:
`seconds/unixtrunc/unsign/string.index/string.titlecase/qs2/url.matchqs/sum/avg/count/hash`):

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `SECONDS(x)` | 数字 | 值折算为秒:日期/时间串 → Unix 秒(`'2015/07/04'`→1435968000);`'MM:SS'`(可带 `M` 前缀:`'M10:30'`→630、`'100:30'`→6030)→ 分×60+秒;数字/数字串直返;`'0:00'` → NULL(qlbridge 行为) |
| `UNIXTRUNC(x [, p])` | **字符串** | Unix 时间戳截断(qlbridge/BigQuery 风格)。`x` 为日期串、epoch 数字或全数字 epoch 串(按位数辨单位:10=秒,13=毫秒,16=微秒,19=纳秒)。单参 → 整秒 `'1438445529'`;`p`=`'s'/'seconds'` → `'1438445529.707'`,`'ms'/'milliseconds'` → `'1438445529707'`,`'sm'/'secondsmicro'` → `'1438445529.707123'`;未知 `p` → NULL |
| `UNSIGN(x)` | **字符串** | 整数按二补码读作无符号:`UNSIGN(-70)='18446744073709551546'`(uint64 超出 float64 精度,故返回十进制字符串,与 `HASH_SIP` 同理) |
| `STRING_INDEX(s, sub)` | 数字 | `sub` 在 `s` 中首次出现的 **0 起**字节偏移;不存在 → NULL(qlbridge `string.index`) |
| `TITLECASE(x)` | 字符串 | 每个词首字母大写(`strings.Title` 语义:字母/数字/下划线**不是**分词符,`'foo_bar'→'Foo_bar'`) |
| `QS2(x, key)` / `QSL` | 字符串 | `QS` 的别名——本实现本就是 qlbridge `qs2` 的**保大小写**语义(qlbridge 旧版 `qs`/`qsl` 会把整个 URL 转小写,损坏混合大小写的参数值;此处按现代语义统一) |
| `URL_MATCHQS(url [, re…])` | 字符串 | URL 化简为 `host+path`,仅保留**参数名**匹配任一正则的查询参数(按键排序重编码);不带正则则丢弃全部参数;URL/正则非法 → NULL。沿 qlbridge:此因子**不补 scheme**,无 scheme 输入的 host 视为空 |
| `SUM(v, …)` | 数字 | 变参求和:数组参数展开(宽松跳过不可解析元素)、数字串参与、NULL 跳过;布尔参数使整体 NULL;**合计恰为 0 → NULL**(qlbridge 行为,保证移植的 `sum(...) = 0` 规则语义不变) |
| `AVG(v, …)` | 数字 | 变参均值:数组元素**严格**(任一坏元素 → NULL),标量字符串宽松跳过;无有效贡献 → NULL |
| `COUNT(x)` | 数字 | 出现标记(qlbridge `count`,非表聚合):非空值 → 1;NULL / `''` / 空数组 → NULL。集合计数请用 `(SELECT COUNT(*) FROM coll)` 或 `ARRAY_LENGTH` |
| `HASH(x)` | 字符串 | `HASH_SIP` 的别名(qlbridge 把 `hash.sip` 同时注册为裸名 `hash`) |

> `SUM`/`AVG`/`COUNT` 的**函数形式**与**聚合子查询** `(SELECT SUM(col) FROM coll WHERE …)` 互不干扰:
> 后者只在 `(SELECT …)` 上下文内解析,二者可共存于同一条规则。

```
规则: MATCH('price_')                                数据: {"price_usd":10}          → 命中
规则: ARRAY_CONTAINS(MAPKEYS(attrs), 'vip_flag')     数据: {"attrs":{"vip_flag":1}}  → 命中
规则: JMESPATH(orders, "[?status=='paid'].amount") IS NOT NULL
      数据: {"orders":"[{\"amount\":120,\"status\":\"paid\"}]"}                      → 命中
规则: USERAGENT(ua, 'mobile') = 'true'               数据: {"ua":"...iPhone..."}     → 命中
规则: CAST(amount_str AS INT) >= 1000                数据: {"amount_str":"1,234"}    → 命中
规则: TODATEIN('Asia/Shanghai', t) >= '2026-07-02 00:00:00'  数据: {"t":"2026-07-02 08:00:00"} → 命中
```

```
规则: CONTAINS(name, '码')                          数据: {"name":"数码城"}            → 命中
规则: STARTSWITH(phone, '139') AND vip_level >= 3   数据: {"phone":"13912345678","vip_level":4} → 命中
规则: ARRAY_CONTAINS(SPLIT(csv, ','), 'b')          数据: {"csv":"a,b,c"}              → 命中
规则: TOINT(amount_str) >= 1000                     数据: {"amount_str":"1,234"}       → 命中
规则: EMAILDOMAIN(email) = 'example.com'            数据: {"email":"Bob@Example.com"}  → 命中
规则: DAYOFWEEK(reg_date) = 4                       数据: {"reg_date":"2026-07-02"}    → 命中 (周四)
规则: MD5(device_id) = '900150983cd24fb0d6963f7d28e17f72'  数据: {"device_id":"abc"}   → 命中
```

> **仍未对齐的 qlbridge 因子及原因**(经 2026-07-02 对 qlbridge 注册表 91 个可调用名逐一比对,
> 其余已全部补齐):`map(k,v)/mapinvert/maptime`(构造/返回 map,引擎值栈无 map 类型;键值**读取**
> 已由 `MAPKEYS`/`MAPVALUES` 覆盖)、`filter/filtermatch`(查询整形/字段投影,布尔用途已由 `MATCH`
> 覆盖)、函数式 `any/all/exists/not`(与本语法的 `ANY`/`ALL`/`EXISTS`/`NOT` 关键字冲突,语义已由
> 关键字覆盖)、`uuid`(随机值,规则求值需确定性)、`useragent.map`(返回 map;单项读取已由
> `USERAGENT(ua, part)` 覆盖)。

---

### 5.9 SQL 常用函数补充（第十四轮） (Common-SQL additions)

按"点名函数 + **等 SQL 常用函数**"的口径补齐的 45 个可调用名(语义对齐 MySQL,
偏差已注明;全部同时可用于两套运行时,字符串位置一律按 **rune** 计,中文安全)。

**字符串**:

| 函数 | 说明 |
|------|------|
| `LTRIM(s)` / `RTRIM(s)` | 去左/右空白(Unicode 空白,MySQL 仅去空格——超集) |
| `LEFT(s, n)` / `RIGHT(s, n)` | 前/后 n 个字符;n≤0 → `''` |
| `REVERSE(s)` | 字符反转 |
| `REPEAT(s, n)` | 重复 n 次;n≤0 → `''`;超出 1 MiB 投影 → NULL |
| `LPAD/RPAD(s, n, pad)` | 补齐到**恰好** n 字符(超长截断;pad 为空且需补 → `''`,MySQL);n<0 或超上界 → NULL |
| `LOCATE(sub, s[, pos])` / `INSTR(s, sub)` | 1 起 rune 位置,不存在 → **0**(MySQL;注意 `STRING_INDEX` 是 qlbridge 的 0 起字节偏移、不存在 → NULL) |
| `SUBSTRING_INDEX(s, d, count)` | 第 count 个分隔符之前(负数从右计);从左**非重叠**计数(split 语义);无分隔符 → 整串 |
| `INITCAP(s)` | `TITLECASE` 的 Oracle/PG 拼写 |

**数学**:

| 函数 | 说明 |
|------|------|
| `MOD(a, b)` | 余数,符号随被除数;`MOD(x, 0)` → NULL |
| `SIGN(x)` | -1 / 0 / 1 |
| `TRUNCATE(x, d)` | 向零截断到 d 位小数(d 可为负) |
| `GREATEST(...)` / `LEAST(...)` | 最大/最小;**任一参数为 NULL → NULL**(MySQL);全数值按数值,否则按渲染文本字典序 |
| `EXP/LN/LOG/LOG10/LOG2` | `LOG(x)`=自然对数,`LOG(b, x)`=以 b 为底(MySQL 参数序);非法定义域 → NULL |
| `PI()` | π |

**日期**:

| 函数 | 说明 |
|------|------|
| `QUARTER/WEEKOFYEAR/DAYOFYEAR/DAYOFMONTH/MONTHNAME/DAYNAME([x])` | 0 参=当前时刻;`WEEKOFYEAR` 为 ISO 周(= MySQL WEEKOFYEAR);月/星期名为英文 |
| `LAST_DAY(x)` | 当月最后一天(日期串) |
| `DATE(x)` / `TIME(x)` | 日期部分 / 时刻部分 |
| `DATE_FORMAT(x, fmt)` | **MySQL % 代码**(`%i`=分钟、`%M`=月名、`%k` 不补零等;strftime 代码请用 `STRFTIME`);未知代码输出裸字符 |
| `TIMESTAMPDIFF(unit, from, to)` | 完整单位数(向零截断,可为负);unit 支持**裸写**(`TIMESTAMPDIFF(MINUTE, a, b)`,解析期校验)或字符串;MONTH/QUARTER/YEAR 按 MySQL 日号+时刻比较(`'2026-07-31'→'2027-03-01'` 为 7) |

**数组**:

| 函数 | 说明 |
|------|------|
| `ARRAY_MIN/ARRAY_MAX(arr)` | 数值元素的最小/最大(非数值元素跳过;无数值 → NULL) |
| `ARRAY_DISTINCT(arr)` | 去重保序;可作量词源(`x = ANY(ARRAY_DISTINCT(tags))`) |
| `ARRAY_POSITION(arr, v)` | 1 起首个等值位置;不存在 → NULL |

**正则**(模式经与 `URL_MATCHQS` 相同的封顶缓存编译:4 KiB 尺寸上界 + 1024 条缓存上界;RE2 线性匹配无回溯灾难):

| 函数 | 说明 |
|------|------|
| `REGEXP_SUBSTR(s, pat[, pos[, occ]])` | 第 occ 个匹配文本;无匹配 → NULL;pos/occ 为 1 起,非法 → NULL |
| `REGEXP_INSTR(s, pat[, pos[, occ]])` | 匹配起点的 1 起 **rune** 位置;无匹配 → **0**(MySQL) |
| `REGEXP_REPLACE(s, pat, repl)` | 全部替换,`$1` 组引用;输出投影超 1 MiB → NULL |

**JSON**(与 `JSON_EXTRACT` 同为核心下降:文档字段**按原始值直读**,JSON 字符串与已解析对象/类型化集合两种行形态结果一致;路径参数必须是字符串字面量,坏路径在**加载期**报错):

| 函数 | 说明 |
|------|------|
| `JSON_LENGTH(doc[, path])` | 对象=键数、数组=元素数、标量=1;路径缺失/坏文档 → NULL |
| `JSON_TYPE(doc[, path])` | `'OBJECT'/'ARRAY'/'STRING'/'NUMBER'/'BOOLEAN'`(本引擎数字单一类,不分 INTEGER/DOUBLE) |
| `JSON_VALID(x)` | 完整谓词:x 是可解析 JSON 字符串(含标量)或已解析对象/数组 → true;NULL/数字字段 → false |
| `JSON_CONTAINS(doc, cand[, path])` | 完整谓词,MySQL 包含语义(对象子集、数组成员、`[[1]]` 不含 `1`、数字按数值);cand 先按 JSON 解析,解析失败按字符串标量(即 `'深圳'` 与 `'"深圳"'` 皆可——比 MySQL 宽容,已注明) |

**集合**:`x [NOT] IN (SELECT col FROM coll [WHERE …])` 子查询成员测试(见 §4/§5.5)。

## 6. 函数用作谓词操作数 (Functions as predicate operands)

函数不仅能用在 `=`/`>` 这类比较里,也能直接作为 `BETWEEN`/`IN`/`LIKE`/`IS NULL` 的左操作数:

```
规则: LENGTH(name) BETWEEN 2 AND 4         数据: {"name":"abc"}        → 命中
规则: LOWER(city) IN ('bj','sh')           数据: {"city":"BJ"}         → 命中
规则: LOWER(city) NOT IN ('bj','sh')       数据: {"city":"GZ"}         → 命中
规则: LOWER(name) LIKE 'a%'                数据: {"name":"ABC"}        → 命中
规则: UPPER(name) NOT LIKE '%X'            数据: {"name":"abc"}        → 命中
规则: TRIM(note) IS NULL                   数据: {}                    → 命中
规则: TRIM(note) IS NOT NULL               数据: {"note":"  x  "}      → 命中
```

---

## 7. 综合示例 (Worked example)

旗舰规则(覆盖大部分算子)与一行命中数据:

```sql
age BETWEEN 25 AND 40
AND province IN ('广东','江苏','浙江')
AND income_level NOT IN ('<5k','5k-10k')
AND favorite_category LIKE '数%'
AND occupation NOT LIKE '%学生%'
AND active_score >= 85
AND last_login_time IS NOT NULL
AND (vip_level >= 3 OR order_count >= 30)
AND NOT (risk_level = '高')
AND marital_status <> '未知'
```

```json
{
  "age": 32, "province": "江苏", "income_level": "20k-30k",
  "favorite_category": "数码", "occupation": "工程师", "active_score": 90.5,
  "last_login_time": "2026-06-26 21:15:00", "vip_level": 4, "order_count": 60,
  "risk_level": "低", "marital_status": "已婚"
}
```

上行**命中**(每个子句都成立)。若把 `age` 改成 `21`,则 `age BETWEEN 25 AND 40`
不成立 → 整条规则**不命中**(`AND` 短路)。结合函数的版本:

```sql
LENGTH(nickname) BETWEEN 2 AND 12
AND LOWER(city_code) IN ('bj','sh','gz')
AND DATEDIFF(CURRENT_DATE, last_login) <= 30
AND ARRAY_CONTAINS(tags, '高价值')
```

---

## 8. 运行示例 (Runnable examples)

`data/` 下有可直接运行的规则与数据文件:

- [`data/rules_functions.json`](../data/rules_functions.json) —— 24 条规则,每条对应一个
  函数 / 谓词形式(`name`、`comment` 标明用途)。
- [`data/users_functions.json`](../data/users_functions.json) —— `uid 1` 精心构造为
  **命中全部 24 条**;`uid 2` 字段稀疏,演示 NULL / 部分命中。

函数由**原生(native)前端**解析(非 qlbridge),所以加 `-frontend native`:

```bash
# 批量匹配:加载规则,对演示用户求值
go run ./cmd/api -rules data/rules_functions.json -users data/users_functions.json -frontend native

# 单行逐条命中/未命中(服务端固定 native 前端)
go run ./cmd/api -serve :8080 -rules data/rules_functions.json
curl -s localhost:8080/evaluate/all -H 'Content-Type: application/json' -d '{
  "row": {"name":"ABC","code":"gd","city":"  北京  ","city_code":"BJ",
          "phone":"13912345678","delta":-3,"score":85.6,"register_date":"2020-01-01",
          "birthday":"2000-06-15","last_login":"2026-06-10","note":"hello",
          "tags":["vip","新客","高价值"],"ids":[1,5,9]}
}'   # -> {"passed":true, ...}(uid 1 的字段命中每一条规则)

# 直接测试单条函数规则
curl -s localhost:8080/rules/test -H 'Content-Type: application/json' \
  -d '{"rule":"LOWER(name) = '\''abc'\''","row":{"name":"ABC"}}'   # -> {"matched":true}
```

---

## 9. 暂不支持 / 边界 (Not supported / boundaries)

- **算术运算符**(`+ - * /`)与 `CASE WHEN … THEN … ELSE … END`。日期加减请用
  `DATE_ADD` / `DATE_SUB`(见 [§5.3](#53-日期函数-date))。
- **跨表 JOIN 与真正的关系子查询**:`EXISTS` / `ANY` / `ALL` 的"表"只能是**当前行内**的
  集合字段(标量数组或嵌套对象数组),不能引用外部数据源或做表连接(见 [§5.5](#55-集合判断exists--any--all-set-predicates))。
- `LIKE` 仅支持 `%`;单字符通配 `_` 按普通字符处理。需要完整模式时用 `REGEXP`。
- 正则为 **RE2**:不支持反向引用 `\1`、环视 `(?=...)` 等 PCRE 特性。
- `JSON_EXTRACT` 仅返回**标量叶子**(对象/数组 → NULL);路径须为字符串字面量。
- 完整 `SELECT … FROM … WHERE …;` 外壳——只传谓词表达式本身(行内子查询除外,见 §5.5)。
- 上述函数与集合谓词目前仅由**原生 SQL 解析器**解析,并只发射 **SQL**(供 `Optimize`
  去重使用);把它们翻译成 CEL / Expr / Aviator 尚未实现。

---

## 10. 运行时与一致性 (Runtimes)

函数在默认的 **bytecode** 字节码 VM 与 **ast** 树遍历两套运行时上行为完全一致;
交叉校验测试 `TestASTMatchesBytecodeFunctions`(`pkg/vm/vm_test.go`)对每个
(规则, 数据) 组合断言两者结果相等。
