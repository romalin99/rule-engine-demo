# ─────────────────────────────────────────────────────────────────────────
# SQL(native) 示例规则 —— 自研全功能 SQL 前端 (engine.NativeFrontend)
#
# 前端源码: pkg/ir (自研词法/语法器,零第三方依赖)。能力最全,覆盖 7 类高级特性,
#   并可解析【任意复杂】的嵌套 SQL(见文末「复杂规则」区)。
#
# 运行 / Run:
#   go run ./cmd/cli -frontend native -rules examples/rules/native.sql \
#                    -users examples/rules/users.json
#
# 格式:每行一条规则(DSL = SQL-WHERE);'#' 开头为注释;可选 "名称: 表达式"。
# ─────────────────────────────────────────────────────────────────────────

# —— 基础:比较 / 逻辑 / 括号 / BETWEEN / IN / LIKE / IS NULL ——
成年深广用户: age >= 18 AND city IN ('深圳','广州')
年龄区间: age BETWEEN 25 AND 40
高分或高等级: active_score >= 85 OR vip_level >= 3
排除学生: occupation NOT LIKE '%学生%' AND income_level NOT IN ('<5k','5k-10k')
有手机号: phone IS NOT NULL AND nickname IS NOT NULL
复合优先级: age >= 25 AND NOT (risk_level = '高') AND (vip_level >= 3 OR order_count >= 30)

# —— 1) 字符串函数 ——
小写等值: LOWER(name) = 'vip'
昵称长度: LENGTH(nickname) BETWEEN 2 AND 12
截取前缀: SUBSTRING(city_code, 1, 2) = 'SH'
去空白后判空: TRIM(remark) IS NULL

# —— 2) 日期函数 ——
今天之前登录: last_login <= CURRENT_DATE
注册年份: YEAR(register_date) >= 2024
活跃间隔: DATEDIFF(CURRENT_DATE, last_login) <= 3650
到期日: DATE_ADD(register_date, 7) >= CURRENT_DATE

# —— 3) 集合判断:EXISTS / ANY / ALL / 聚合子查询 ——
有已付订单: EXISTS (SELECT 1 FROM orders WHERE status = '已付')
命中任一标签: '高价值' = ANY (SELECT label FROM tags_rows)
全部订单达标: 100 <= ALL (SELECT amount FROM orders)
累计消费额: (SELECT SUM(amount) FROM orders WHERE status = '已付') >= 500

# —— 4) 数学函数 ——
绝对偏差: ABS(delta) <= 5
四舍五入两位: ROUND(rate, 2) >= 0.85
向上取整: CEIL(score) >= 90
向下取整: FLOOR(score) >= 60

# —— 5) 数组操作:包含 / 交集 / 长度 / 计算数组量词 ——
标签包含: ARRAY_CONTAINS(tags, '高价值')
兴趣有交集: ARRAY_INTERSECT(interests, promo_targets)
标签数量: ARRAY_LENGTH(tags) >= 3
拆分后命中: 'b' = ANY (SPLIT(csv_tags, ','))

# —— 6) JSON 字段访问 ——
JSON城市: JSON_EXTRACT(profile, '$.city') = '深圳'
JSON嵌套邮编: JSON_EXTRACT(profile, '$.addr.zip') = '518000'

# —— 7) 正则匹配 ——
手机号正则: phone REGEXP '^1\d{10}$'
非数字开头: name NOT REGEXP '^[0-9]'
不敏感匹配: REGEXP_LIKE(name, 'vip', 'i')

# ═══ 复杂规则(展示 native 解析任意复杂 SQL 的能力)═══
# —— 深度嵌套布尔:多层括号 + AND/OR/NOT 混合 ——
深嵌套画像: (age BETWEEN 25 AND 40 OR vip_level >= 5) AND NOT (risk_level = '高' OR (marital_status = '未知' AND province NOT IN ('广东','江苏')))
# —— 多字符串函数组合 ——
多函数串接: LOWER(name) LIKE 'v%' AND LENGTH(nickname) BETWEEN 2 AND 12 AND SUBSTRING(city_code, 1, 2) IN ('SH','BJ')
# —— 双聚合子查询联立 ——
双聚合: (SELECT COUNT(*) FROM orders WHERE amount >= 100 AND status = '已付') >= 2 AND (SELECT SUM(amount) FROM orders WHERE status = '已付') >= 500
# —— 数组 + JSON + 数学 同条组合 ——
数组JSON数学: ARRAY_LENGTH(tags) >= 2 AND ARRAY_INTERSECT(interests, promo_targets) AND JSON_EXTRACT(profile, '$.addr.zip') = '518000' AND ROUND(rate, 2) >= 0.85
# —— 量词子查询 + 正则 + 日期 + 负数字面量 ——
量词正则日期: '高价值' = ANY (SELECT label FROM tags_rows) AND phone REGEXP '^1\d{10}$' AND DATEDIFF(CURRENT_DATE, register_date) >= 0 AND temperature > -10
# —— EXISTS 带 WHERE + 函数 + 数组,一条覆盖多类 ——
旗舰综合: age >= 18 AND (vip_level >= 3 OR (SELECT COUNT(*) FROM orders) >= 2) AND LOWER(city_code) IN ('sh','bj','gz') AND NOT (risk_level = '高') AND JSON_EXTRACT(profile, '$.city') = '深圳' AND ARRAY_CONTAINS(tags, '高价值') AND EXISTS (SELECT 1 FROM orders WHERE amount >= 100)
