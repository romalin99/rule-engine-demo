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
| NULL     | 字段**缺失**,或值为 `nil`                       |

**比较语义**:两侧都是数字时按**数值**比较,否则按**字符串(字典序)**。日期以
ISO 字符串存放时,字典序即时间序(`'2026-01-01' < '2026-02-01'`)。比较的**任意一侧**
都可以是字段、字面量或函数调用,例如 `LENGTH(a) = LENGTH(b)`、`last_login >= CURRENT_DATE`。

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
| 区间 | `x BETWEEN lo AND hi` |
| 集合 | `x IN (a, b, …)` / `x NOT IN (…)` |
| 模糊 | `x LIKE 'a%'` / `x NOT LIKE '%b%'`(仅 `%`;不支持 `_`) |
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
规则: phone IS NULL                              数据: {}                         → 命中
规则: phone IS NOT NULL                          数据: {"phone":"139..."}         → 命中
规则: (vip_level >= 3 OR order_count >= 30)      数据: {"vip_level":4}            → 命中
规则: age >= 25 AND NOT (province IN ('北京'))   数据: {"age":30,"province":"广东"} → 命中
```

> `<>` 是 `!=` 的 SQL 别名(等价)。`LIKE` 仅识别 `%`(前缀/后缀/包含),`_` 按普通字符处理。

---

## 5. 函数 (Functions)

所有函数都可以出现在**比较两侧**,也可以作为 `BETWEEN`/`IN`/`LIKE`/`IS NULL` 的操作数
(见 [§6](#6-函数用作谓词操作数-functions-as-predicate-operands))。

### 5.1 字符串函数 (String)

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `LOWER(x)` / `UPPER(x)` | 字符串 | 转小写 / 大写 |
| `TRIM(x)` | 字符串 | 去除首尾空白 |
| `LENGTH(x)` | 数字 | **字符数(rune)**,中文安全 |
| `SUBSTRING(s, start, len)` / `SUBSTR` | 字符串 | **从 1 开始**、按 rune 截取;越界截断为 `''` |

```
规则: LOWER(name) = 'abc'              数据: {"name":"ABC"}          → 命中
规则: UPPER(code) = 'GD'               数据: {"code":"gd"}           → 命中
规则: TRIM(city) = '北京'              数据: {"city":"  北京  "}     → 命中
规则: LENGTH(name) = 3                 数据: {"name":"数码城"}        → 命中  (3 个字符)
规则: SUBSTRING(phone, 1, 3) = '139'   数据: {"phone":"13912345678"} → 命中
```

### 5.2 数学函数 (Math)

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `ABS(x)` | 数字 | 绝对值 |
| `ROUND(x)` | 数字 | 就近取整(四舍五入,远离零方向);仅单参 |
| `CEIL(x)` / `CEILING(x)` | 数字 | 向上取整 |
| `FLOOR(x)` | 数字 | 向下取整 |

> 数学函数要求操作数是数字;非数字或 NULL 一律返回 NULL。

```
规则: ABS(delta) <= 5      数据: {"delta":-3}     → 命中
规则: ROUND(score) = 86    数据: {"score":85.6}   → 命中
规则: CEIL(score) = 86     数据: {"score":85.1}   → 命中
规则: FLOOR(score) = 85    数据: {"score":85.9}   → 命中
```

### 5.3 日期函数 (Date)

日期按 **ISO 字符串**处理(字典序=时间序)。可解析的输入格式:
`2006-01-02 15:04:05`、`2006-01-02T15:04:05Z07:00`(RFC3339)、`2006-01-02T15:04:05`、`2006-01-02`。

| 函数 | 返回 | 说明 |
| ---- | ---- | ---- |
| `CURRENT_DATE` | 字符串 `YYYY-MM-DD` | **裸关键字,无括号**,取当天 |
| `CURRENT_TIMESTAMP` | 字符串 `YYYY-MM-DD HH:MM:SS` | 裸关键字,取当前时刻 |
| `YEAR(x)` / `MONTH(x)` / `DAY(x)` | 数字 | 从日期字符串提取年/月/日 |
| `DATEDIFF(a, b)` | 数字 | 整天数 `a − b`(截断) |
| `DATE_ADD(d, n [, unit])` | 字符串 | 日期**加** n 个单位(省略 unit 时按 **DAY**);保留输入精度(日期 / 日期时间) |
| `DATE_SUB(d, n [, unit])` | 字符串 | 日期**减** n 个单位(省略 unit 时按 **DAY**) |

> `unit` 大小写不敏感、可带复数 `s`,取值:`DAY` `WEEK` `MONTH` `YEAR` `HOUR` `MINUTE` `SECOND`。
> 月/年加减按日历进位(如 `DATE_ADD('2026-01-31', 1, 'MONTH')` → `2026-03-03`)。
> 日期参数为 NULL、n 非数字或日期无法解析时返回 **NULL**。

```
规则: register_date <= CURRENT_DATE             数据: {"register_date":"2020-01-01"} → 命中
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
| `ARRAY_OVERLAP(field, v1, v2, …)` | 布尔 | 是否含任一 `vi`(脱糖为 `ARRAY_CONTAINS` 的 `OR`) |

> `ARRAY_CONTAINS` / `ARRAY_OVERLAP` 本身就是完整布尔谓词;`ARRAY_LENGTH` 返回数字,
> 用于比较式中。

```
规则: ARRAY_LENGTH(tags) >= 2              数据: {"tags":["vip","new","gold"]} → 命中
规则: ARRAY_CONTAINS(tags, 'vip')          数据: {"tags":["vip","new"]}        → 命中
规则: ARRAY_CONTAINS(ids, 5)               数据: {"ids":[1,5,9]}               → 命中  (按文本 "5")
规则: ARRAY_OVERLAP(tags, 'gold', 'vip')   数据: {"tags":["vip"]}              → 命中
```

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
是同义词。函数形式 `REGEXP_LIKE(x, '正则' [, 'i'])` 等价,`i` 标志做大小写不敏感
(等价于模式前加 `(?i)`)。左操作数为 NULL 时不匹配。

| 形式 | 含义 |
| ---- | ---- |
| `x REGEXP 'p'` / `x RLIKE 'p'` | x 匹配正则 p |
| `x NOT REGEXP 'p'` | x 不匹配 p |
| `REGEXP_LIKE(x, 'p')` | 同 `x REGEXP 'p'` |
| `REGEXP_LIKE(x, 'p', 'i')` | 大小写不敏感匹配 |

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

---

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
- `ROUND(x, d)` 双参未实现,仅 `ROUND(x)` 取整。
- 完整 `SELECT … FROM … WHERE …;` 外壳——只传谓词表达式本身(行内子查询除外,见 §5.5)。
- 上述函数与集合谓词目前仅由**原生 SQL 解析器**解析,并只发射 **SQL**(供 `Optimize`
  去重使用);把它们翻译成 CEL / Expr / Aviator 尚未实现。

---

## 10. 运行时与一致性 (Runtimes)

函数在默认的 **bytecode** 字节码 VM 与 **ast** 树遍历两套运行时上行为完全一致;
交叉校验测试 `TestASTMatchesBytecodeFunctions`(`pkg/vm/vm_test.go`)对每个
(规则, 数据) 组合断言两者结果相等。
