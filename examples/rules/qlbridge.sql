# ─────────────────────────────────────────────────────────────────────────
# SQL(qlbridge) 示例规则 —— 符合 github.com/araddon/qlbridge 可解析语法
#                          (engine.QLBridgeFrontend,亦即 engine.New() 默认前端)
#
# 库 / Library: github.com/araddon/qlbridge —— 仅作 SQL 解析器,不参与求值。
#   引擎流程: expr.ParseExpression(rule) → qlToIR 转换;转换器不支持的节点【回退 native】。
#   前端源码 pkg/engine/frontend_qlbridge.go。
#
# 运行 / Run:
#   go run ./cmd/cli -frontend qlbridge -rules examples/rules/qlbridge.sql \
#                    -users examples/rules/users.json
#
# 本文件每行都【经 qlbridge 词法/语法可解析】(依据 qlbridge lex/token.go 的关键字表:
#   and or not in like between is null contains intersects exists,以及 == = != > >= < <=、
#   函数调用、单/双引号字符串),且都能被引擎最终解析(基础谓词由 qlToIR 直接转换,
#   函数等经 native 回退求值)。
#
# 说明:qlbridge 独有的中缀 CONTAINS / INTERSECTS、以及 REGEXP、SELECT 子查询 EXISTS
#   在此【刻意不用】—— 它们要么 native 回退不认(中缀 CONTAINS/INTERSECTS、REGEXP),
#   要么非 qlbridge 表达式语法(SELECT 子查询)。改用两侧都可解析的等价写法。
# ─────────────────────────────────────────────────────────────────────────

# ═══ A 区:qlbridge 解析 + qlToIR 直接转换成 IR(不走回退,纯 qlbridge→IR 路径)═══
#   —— 比较 / AND / OR / IN / LIKE / 数值 BETWEEN(qlbridge 惯用 == 与双引号)——
成年深广: age >= 18 AND city == "深圳"
等值单引号: city = '广州'
不等: risk_level != "高"
年龄区间: age BETWEEN 25 AND 40
分数区间: active_score BETWEEN 60 AND 100
省份集合: province IN ("广东", "江苏", "浙江")
排除收入档: income_level NOT IN ("<5k", "5k-10k")
类目前缀: favorite_category LIKE "数%"
手机前缀: phone LIKE "139%"
高分或高等级: active_score >= 85 OR vip_level >= 3
高分高单: active_score >= 85 AND order_count >= 30

# ═══ B 区:qlbridge 可解析,但 qlToIR 不直接转换 → 引擎经 native 回退求值 ═══
#   —— NOT(一元)与函数调用:均为合法 qlbridge 表达式,native 亦支持,故回退后可求值 ——
显式取反: NOT (risk_level == "高")
小写等值: lower(name) == "vip"
昵称长度: length(nickname) >= 2
数组包含: array_contains(tags, "高价值")
存在订单: exists(orders)
JSON城市: json_extract(profile, "$.city") == "深圳"
活跃间隔: datediff(current_date, last_login) <= 3650

# ═══ C 区:复杂规则(深度嵌套 + 函数,均 qlbridge 可解析、引擎可求值)═══
#   —— 所用构造(嵌套括号 / AND·OR·NOT / == = != / IN / LIKE / 数值 BETWEEN / 函数调用)
#      两侧(qlbridge 与 native)都可解析;刻意不含中缀 CONTAINS·INTERSECTS·REGEXP·NULL 字面量 ——
深嵌套画像: (age BETWEEN 25 AND 40 OR vip_level >= 5) AND NOT (risk_level == "高" OR (marital_status == "未知" AND province NOT IN ("广东", "江苏")))
函数与分支: lower(name) == "vip" AND (datediff(current_date, last_login) <= 3650 OR array_contains(tags, "高价值")) AND active_score >= 85
多条件圈选: city IN ("深圳", "广州", "杭州") AND (vip_level >= 3 OR order_count >= 30) AND favorite_category LIKE "数%" AND NOT (income_level IN ("<5k", "5k-10k"))
JSON函数区间: json_extract(profile, "$.city") == "深圳" AND length(nickname) BETWEEN 2 AND 12 AND (exists(orders) OR order_count >= 1)
