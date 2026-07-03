# ═════════════════════════════════════════════════════════════════════════
# SQL(native) 复杂规则样例 —— 自研全功能 SQL 前端 (engine.NativeFrontend)
#
# 前端源码: pkg/ir(自研词法/语法器,零第三方依赖)。能力最全:7 类高级特性
#   (字符串/日期/集合/数学/数组/JSON/正则)+ 类型转换 + 扩展内建函数 + 任意深度嵌套。
#
# 运行 / Run:
#   go run ./cmd/cli -frontend native -rules examples/rules/complex/native.sql \
#                    -users examples/rules/complex/users.json
#
# 解析可用性验证 / Verify:
#   go test ./pkg/engine/ -run TestComplexRuleFiles -v
#
# 格式:每行一条规则(DSL = SQL-WHERE);'#' 开头为注释;"名称: 表达式"。
# ═════════════════════════════════════════════════════════════════════════

# ─── A. 基础谓词强化:多路逻辑 / 双重否定 / 负数 / 字符串区间 / 字段对字段 / 字面量在左 ───
# 三路年龄段并联,再与省份集合相交 —— 营销活动的典型分层圈选
多路或与优先级: (age BETWEEN 18 AND 30 OR age BETWEEN 45 AND 60 OR vip_level >= 5) AND province IN ('广东','江苏','浙江','四川')
# NOT NOT 双重否定(语法压力测试,等价于括号内原式)
双重否定: NOT NOT (vip_level >= 1)
# <> 是 != 的 SQL 别名;信用分数值区间
不等与区间: marital_status <> '未知' AND credit_score BETWEEN 650 AND 850
# 负数与小数字面量:下界为负的 BETWEEN、负数比较、浮点比较
负数与小数: temperature BETWEEN -10 AND 39.5 AND delta >= -5 AND rate > 0.5
# 字符串(日期)区间 BETWEEN —— 脱糖为 >= AND <=,ISO 日期按字典序即时间序
注册日期区间: register_date BETWEEN '2024-01-01' AND '2026-12-31'
# 字段对字段比较(右侧不是字面量而是另一字段)
字段对字段: balance > budget OR active_score >= score
# 标准 SQL 允许字面量写在左侧
字面量在左: '深圳' = city OR 'SH' = city_code
# IS NULL / IS NOT NULL 链
空值链: remark IS NULL AND phone IS NOT NULL AND nickname IS NOT NULL

# ─── B. 1) 字符串函数:嵌套调用 / 函数结果作 IN·BETWEEN·LIKE 左操作数 ───
# 函数嵌套:先 TRIM 再 LOWER
嵌套函数: LOWER(TRIM(name)) = 'vip'
# 函数结果做 IN 的左操作数(脱糖为 OR 等值链)
函数左IN: UPPER(SUBSTRING(city_code, 1, 2)) IN ('SH','BJ','GZ','SZ')
# 函数结果做 BETWEEN 左操作数;CONCAT 变长参数
函数左区间: LENGTH(CONCAT(name, '-', nickname)) BETWEEN 3 AND 30
# 替换分隔符后做 LIKE 前缀匹配
替换后匹配: REPLACE(csv_tags, ',', '|') LIKE 'a|%'
# SUBSTRING 双参形态(取到串尾)与三参形态并用
二参截取: SUBSTRING(phone, 8) LIKE '%8' OR SUBSTRING(phone, 1, 3) = '139'
# 扩展函数 STRIP / CHAR_LENGTH(pkg/sqlfn)
清洗与计长: STRIP(nickname) != '' AND CHAR_LENGTH(nickname) BETWEEN 2 AND 12
# TOLOWER / TOUPPER 扩展别名
大小写规整: TOLOWER(city_code) IN ('sh','sz') OR TOUPPER(name) = 'VIP'
# 拼接后整串等值
拼接比较: CONCAT(province, '-', city) = '广东-深圳'

# ─── C. 2) 日期时间:CURRENT_DATE/TIMESTAMP / 年月日拆解 / 带单位加减 / 差值 ───
# YEAR/MONTH 拆解后分别做区间与集合判断
年月拆解: YEAR(register_date) BETWEEN 2023 AND 2026 AND MONTH(register_date) IN (1,2,3,6,11,12)
# 最近 30 天活跃,或"最后登录 + 90 天"仍未过期
活跃窗口: DATEDIFF(CURRENT_DATE, last_login) <= 30 OR DATE_ADD(last_login, 90) >= CURRENT_DATE
# 三参 DATE_ADD/DATE_SUB(单位 YEAR/DAY);函数对函数比较
单位步进: DATE_ADD(register_date, 1, 'YEAR') <= CURRENT_TIMESTAMP AND DATE_SUB(CURRENT_DATE, 180) <= register_date
# 周末最后登录(DAYOFWEEK: 周日=0/周六=6,依实现),或月上旬注册
星期与日: DAYOFWEEK(last_login) IN (0,6) OR DAY(register_date) <= 15
# 无括号 nullary 函数 CURRENT_DATE 做左操作数;TODATE 归一化
当天判断: CURRENT_DATE >= register_date AND TODATE(last_login) <= CURRENT_DATE

# ─── D. 3) 集合判断:EXISTS / NOT EXISTS / ANY·ALL·SOME / 聚合子查询 ───
# 子查询 WHERE 内含嵌套 OR —— 有一笔 ≥200 的已付或待发货订单
子查询复合条件: EXISTS (SELECT 1 FROM orders WHERE amount >= 200 AND (status = '已付' OR status = '待发货'))
# 反存在:没有大额退款
反存在: NOT EXISTS (SELECT 1 FROM orders WHERE status = '退款' AND amount > 1000)
# 字面量 = ANY(子查询投影列)
标签行量词: '高价值' = ANY (SELECT label FROM tags_rows)
# 全称量词:每笔已付订单都 ≥ 50
全称量词: 50 <= ALL (SELECT amount FROM orders WHERE status = '已付')
# SOME 是 ANY 的标准别名
SOME别名: 100 <= SOME (SELECT amount FROM orders)
# 字段与子查询列逐一比较(订单数大于所有单笔件数)
数量对比: order_count > ALL (SELECT qty FROM orders)
# 聚合子查询作 BETWEEN 左操作数
聚合区间: (SELECT AVG(amount) FROM orders WHERE status = '已付') BETWEEN 100 AND 500
# 聚合子查询作比较右操作数
聚合右侧: budget >= (SELECT SUM(amount) FROM orders WHERE status = '已付')
# 空集聚合语义:MIN 于空集为 NULL;COUNT 恒有值
聚合判空: (SELECT MIN(amount) FROM orders WHERE status = '退款') IS NULL OR (SELECT COUNT(*) FROM orders) >= 1
# 两个聚合子查询联立
双聚合联立: (SELECT COUNT(*) FROM orders WHERE amount >= 100) >= 2 AND (SELECT MAX(amount) FROM orders) <= 5000

# ─── E. 4) 数学函数:ABS / ROUND(1|2参) / CEIL / FLOOR / SQRT / POW ───
绝对值与开方: ABS(delta) <= 5 AND SQRT(score) >= 7.5
精度控制: ROUND(rate, 2) BETWEEN 0.5 AND 0.99 AND ROUND(active_score) <= 100
幂与取整: POW(vip_level, 2) >= 9 OR (CEIL(score) = 100 AND FLOOR(active_score) >= 60)
# ROUND 第二参为负 = 舍入到十/百位
负位数舍入: ROUND(balance, -2) >= 0

# ─── F. 5) 数组操作:包含 / 任一命中 / 交集 / 长度 / 索引 / 切片 / 拆分 / 连接 ───
含黑白名单: ARRAY_CONTAINS(tags, '高价值') AND NOT ARRAY_CONTAINS(tags, '黑名单')
# ARRAY_OVERLAP(arr, v1, v2, ...) 命中任一即真(脱糖为 OR 包含链)
任一命中: ARRAY_OVERLAP(interests, '数码', '运动', '美妆')
交集与长度: ARRAY_INTERSECT(interests, promo_targets) AND ARRAY_LENGTH(tags) BETWEEN 1 AND 10
# 按下标取元素再比较
索引取值: ARRAY_INDEX(tags, 1) = '高价值' OR ARRAY_INDEX(tags, 2) = '新客'
# 量词遍历计算数组:切片
切片量词: '新客' = ANY (ARRAY_SLICE(tags, 1, 2))
# 数组连接为串后模糊匹配
连接匹配: JOIN(interests, ',') LIKE '%数码%'
# 量词遍历计算数组:CSV 拆分
拆分量词: 'b' = ANY (SPLIT(csv_tags, ',')) AND 'zzz' != ANY (SPLIT(csv_tags, ','))

# ─── G. 6) JSON / Map 字段访问:JSON_EXTRACT / JSON_VALUE / JMESPATH / MAPKEYS·MAPVALUES ───
JSON等值集合: JSON_EXTRACT(profile, '$.city') IN ('深圳','广州')
JSON嵌套前缀: JSON_EXTRACT(profile, '$.addr.zip') LIKE '518%'
JSON判空: JSON_VALUE(profile, '$.vip') IS NULL OR JSON_EXTRACT(profile, '$.city') IS NOT NULL
# JMESPath 表达式在加载期预编译校验
JMES路径: JMESPATH(profile, 'addr.zip') = '518000'
# Map 字段的键/值集合配合量词
Map键量词: 'city' = ANY (MAPKEYS(attrs))
Map值量词: '深圳' = ANY (MAPVALUES(attrs))

# ─── H. 7) 正则匹配:REGEXP / NOT REGEXP / RLIKE / REGEXP_LIKE(flags) / 函数结果正则 ───
手机严格校验: phone REGEXP '^1[3-9]\d{9}$'
邮箱域排除: email NOT REGEXP '@(test|fake)\.'
RLIKE别名: nickname RLIKE '^[A-Z]{1,3}'
# 第三参 match_type:i 忽略大小写(亦支持 c/m/n/u)
忽略大小写: REGEXP_LIKE(city_code, '^(sh|bj|gz|sz)$', 'i')
# 左操作数为函数结果的正则
函数结果正则: LOWER(email) REGEXP '^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}$'

# ─── I. 类型转换:CAST(x AS type) 与 TO* 家族 ───
CAST整型浮点: CAST(age AS INT) >= 18 AND CAST(rate AS FLOAT) > 0.5
CAST转串集合: CAST(vip_level AS STRING) IN ('3','4','5','6')
转换函数: TOINT(score) BETWEEN 60 AND 100 AND TOSTRING(order_count) LIKE '%0'
日期转换: TODATE(register_date) <= CURRENT_DATE

# ─── J. 扩展布尔内建 / 邮箱·URL 函数(pkg/sqlfn)───
# CONTAINS/STARTSWITH/ENDSWITH 可独立成谓词
布尔内建: CONTAINS(nickname, 'VIP') AND STARTSWITH(city_code, 'S') AND NOT ENDSWITH(email, '.test')
前后缀别名: HASPREFIX(phone, '139') OR HASSUFFIX(email, '.com')
比较内建: GT(active_score, 80) AND LE(order_count, 100000)
邮箱拆解: EMAILDOMAIN(email) = 'example.com' OR EMAILNAME(email) = 'norman'
站点主机: HOST(homepage) LIKE '%.example.com' OR DOMAIN(homepage) = 'example.com'
# COALESCE/ONEOF 取第一个非空值
合并空值: COALESCE(remark, nickname, name) != ''
# MATCH('前缀', ...):行级字段名前缀探测
字段名前缀: MATCH('tag_', 'ext_')

# ═══ K. 旗舰组合:多类特性深度嵌套于同一条规则 ═══
# 人群圈选:年龄/等级 + 风险排除 + JSON 城市 + 消费聚合 + 手机正则 + 标签
旗舰全域圈选: (age BETWEEN 25 AND 40 OR (vip_level >= 5 AND NOT (risk_level IN ('高','中高')))) AND (JSON_EXTRACT(profile, '$.city') = '深圳' OR city IN ('广州','杭州')) AND (SELECT SUM(amount) FROM orders WHERE status = '已付') >= 500 AND phone REGEXP '^1[3-9]\d{9}$' AND ARRAY_CONTAINS(tags, '高价值')
# 召回排除:无大额退款且非高风险 + 沉默/名称前缀择一 + 昵称与城市码规整
旗舰召回排除: NOT (EXISTS (SELECT 1 FROM orders WHERE status = '退款' AND amount >= 500) OR risk_level = '高') AND (DATEDIFF(CURRENT_DATE, last_login) BETWEEN 30 AND 3650 OR LOWER(TRIM(name)) LIKE 'v%') AND (LENGTH(nickname) BETWEEN 2 AND 12 AND UPPER(SUBSTRING(city_code, 1, 2)) IN ('SH','BJ','GZ','SZ'))
# 量词矩阵:子查询量词 + 拆分量词 + Map 键量词 + JMES + 双重否定
旗舰量词矩阵: ('高价值' = ANY (SELECT label FROM tags_rows) OR 'b' = ANY (SPLIT(csv_tags, ','))) AND 100 <= ALL (SELECT amount FROM orders WHERE status = '已付') AND ('city' = ANY (MAPKEYS(attrs)) OR JMESPATH(profile, 'addr.zip') = '518000') AND NOT NOT (vip_level >= 1)
# 函数长链:字符串三层嵌套 + 数学三层嵌套 + CAST 套数组长度 + 日期函数对比
旗舰函数长链: LOWER(REPLACE(CONCAT(province, '-', city), '广东', 'gd')) LIKE 'gd-%' AND ROUND(SQRT(POW(vip_level, 2)), 0) >= 3 AND CAST(ARRAY_LENGTH(tags) AS INT) BETWEEN 1 AND 99 AND DATE_ADD(register_date, 30) <= DATE_ADD(CURRENT_DATE, 1, 'DAY')
