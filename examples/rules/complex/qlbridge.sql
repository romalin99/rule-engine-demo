# ═════════════════════════════════════════════════════════════════════════
# SQL(qlbridge) 复杂规则样例 —— 符合 github.com/araddon/qlbridge 可解析语法
#                              (engine.QLBridgeFrontend,亦即 engine.New() 默认前端)
#
# 库 / Library: github.com/araddon/qlbridge —— 仅作 SQL 解析器,不参与求值。
#   引擎流程: expr.ParseExpression(rule) → qlToIR 转换;qlbridge 解析失败或
#   转换器不支持的节点【透明回退 native】(native 是严格超集),故全部规则最终可求值。
#   前端源码 pkg/engine/frontend_qlbridge.go。
#
# 运行 / Run:
#   go run ./cmd/cli -frontend qlbridge -rules examples/rules/complex/qlbridge.sql \
#                    -users examples/rules/complex/users.json
#
# 解析可用性验证 / Verify:
#   go test ./pkg/engine/ -run TestComplexRuleFiles -v
#
# 词汇约束(与 examples/rules/qlbridge.sql 同一口径,保证两侧都可解析):
#   比较 == = != > >= < <;AND/OR/NOT;IN/NOT IN;LIKE/NOT LIKE;数值 BETWEEN;
#   单/双引号字符串;函数调用仅用已验证集合 {lower, tolower, toupper, length,
#   array_contains, exists, json_extract, datediff, current_date}。
#   刻意不用:中缀 CONTAINS/INTERSECTS、REGEXP、SELECT 子查询、NULL 字面量
#   (它们两侧解析行为不一致,改用等价写法)。
# ═════════════════════════════════════════════════════════════════════════

# ═══ A 区:qlbridge 解析 + qlToIR 直接转换(不走回退的快路径)═══
# 等值两种写法(== 与 =)+ 单双引号混用
等值混写: city == "深圳" OR city = '广州'
# 多路圈选:两层括号 + IN 集合并联
多路圈选: (age >= 18 AND age <= 45) AND (province IN ("广东", "江苏", "浙江") OR city IN ("成都", "杭州"))
# 数值 BETWEEN 与不等
区间与不等: age BETWEEN 25 AND 40 AND risk_level != "高"
# 双数值区间联立
双区间: active_score BETWEEN 60 AND 100 AND credit_score BETWEEN 650 AND 850
# 排除集合与不等排除(NOT LIKE 留给回退区,直译区用 != 等价表达)
排除集合: income_level NOT IN ("<5k", "5k-10k") AND occupation != "在校学生"
# LIKE 前缀/包含
前后缀匹配: favorite_category LIKE "数%" OR nickname LIKE "%VIP%"
# 三层与或嵌套
深层与或: ((vip_level >= 3 AND order_count >= 10) OR (active_score >= 90 AND order_count >= 5)) AND marital_status != "未知"
# 六条件长链(全部直译构造)
长链条件: age >= 18 AND age <= 65 AND active_score >= 60 AND vip_level >= 1 AND order_count >= 1 AND risk_level != "高"
# 小数与负数比较
小数比较: rate > 0.5 AND active_score >= 85.5
# 四层纯嵌套(全部为可直译节点)
四层嵌套: ((age >= 18 AND (city == "深圳" OR (province IN ("广东", "江苏") AND active_score >= 70))) OR vip_level >= 6) AND (risk_level != "高" OR order_count >= 100)

# ═══ B 区:qlbridge 可解析,qlToIR 不直接转换 → 引擎经 native 回退求值 ═══
# 一元 NOT(qlbridge UnaryNode → 回退)
显式取反: NOT (risk_level == "高" OR income_level IN ("<5k"))
# 函数调用(qlbridge FuncNode → 回退;native 按大写函数名编译)
小写等值: lower(name) == "vip"
规整比较: tolower(city_code) IN ("sh", "sz") OR toupper(name) == "VIP"
昵称长度带或: length(nickname) >= 2 AND (length(name) <= 20 OR name == "vip")
数组包含组合: array_contains(tags, "高价值") AND NOT (array_contains(tags, "黑名单"))
存在与计数: exists(orders) AND order_count >= 1
JSON嵌套: json_extract(profile, "$.addr.zip") == "518000"
活跃间隔: datediff(current_date, last_login) <= 3650

# ═══ C 区:复杂规则(深度嵌套 + 函数矩阵,均两侧可解析、引擎可求值)═══
# 深嵌套画像:区间/等级并联,再排除高风险与"未知婚姻且省外"
深嵌套画像: (age BETWEEN 25 AND 40 OR vip_level >= 5) AND NOT (risk_level == "高" OR (marital_status == "未知" AND province NOT IN ("广东", "江苏")))
# 函数矩阵:名称小写 + 活跃/标签择一 + JSON 城市 + 昵称长度区间
函数矩阵: lower(name) == "vip" AND (datediff(current_date, last_login) <= 3650 OR array_contains(tags, "高价值")) AND json_extract(profile, "$.city") == "深圳" AND length(nickname) BETWEEN 2 AND 12
# 圈选排除:城市集合 + 等级/单量分支 + 类目前缀 + 双排除
圈选排除: city IN ("深圳", "广州", "杭州") AND (vip_level >= 3 OR order_count >= 30) AND favorite_category LIKE "数%" AND NOT (income_level IN ("<5k", "5k-10k") OR occupation LIKE "%学生%")
# 存在或新客:订单存在/新客标签择一 + JSON 或城市 + 注册天数非负
存在或新客: (exists(orders) OR array_contains(tags, "新客")) AND (json_extract(profile, "$.city") == "深圳" OR city == "广州") AND datediff(current_date, register_date) >= 0
# 五层混合:函数 + 纯逻辑深嵌套 + 排除集合
五层混合: ((lower(name) == "vip" OR (nickname LIKE "%VIP%" AND length(nickname) <= 12)) AND (city == "深圳" OR (province IN ("广东", "江苏") AND active_score >= 70))) AND NOT (risk_level IN ("高", "中高") OR occupation LIKE "%学生%")
