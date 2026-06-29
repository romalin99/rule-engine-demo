# 进阶特性 curl 演示 (Advanced features – runnable curl)

演示 **正则 / JSON 字段访问 / 集合谓词(EXISTS·ANY·ALL)/ 聚合子查询**,均由
**原生 SQL 前端**解析,bytecode 与 ast 双运行时一致。

数据文件:

- [`data/rules_advanced.json`](../data/rules_advanced.json) —— 18 条规则,每条对应一个进阶特性。
- [`data/users_advanced.json`](../data/users_advanced.json) —— `uid 1` 构造为**命中全部**;`uid 2` 字段稀疏 / 无订单,演示 NULL 与未命中。

## 1. 起服务(固定原生前端)

```bash
go run ./cmd/api -serve :8080 -rules data/rules_advanced.json
```

> 进阶函数/谓词仅 `native` 前端支持;`-serve` 默认即 native。

## 2. 单条规则测试 `POST /rules/test`

`{"rule","row"}` → `{"matched":bool}`。下面用带引号的 heredoc(`<<'JSON'`)避免 shell 把单引号/`$` 吃掉。

```bash
# 正则:RE2 中缀匹配
curl -s localhost:8080/rules/test -H 'Content-Type: application/json' -d @- <<'JSON'
{"rule":"phone REGEXP '^139[0-9]{8}$'","row":{"phone":"13912345678"}}
JSON
# -> {"matched":true}

# 正则:REGEXP_LIKE 大小写不敏感
curl -s localhost:8080/rules/test -H 'Content-Type: application/json' -d @- <<'JSON'
{"rule":"REGEXP_LIKE(name, 'alice', 'i')","row":{"name":"Alice123"}}
JSON
# -> {"matched":true}

# JSON:从 JSON 字符串字段取嵌套标量
curl -s localhost:8080/rules/test -H 'Content-Type: application/json' -d @- <<'JSON'
{"rule":"JSON_EXTRACT(profile, '$.addr.zip') = '518000'","row":{"profile":"{\"addr\":{\"zip\":\"518000\"}}"}}
JSON
# -> {"matched":true}

# 集合:嵌套对象数组的 EXISTS 子查询
curl -s localhost:8080/rules/test -H 'Content-Type: application/json' -d @- <<'JSON'
{"rule":"EXISTS(SELECT 1 FROM orders WHERE amount > 100)","row":{"orders":[{"amount":120},{"amount":50}]}}
JSON
# -> {"matched":true}

# 集合:ANY/ALL 子查询(投影列)
curl -s localhost:8080/rules/test -H 'Content-Type: application/json' -d @- <<'JSON'
{"rule":"budget >= ALL(SELECT amount FROM orders WHERE status = 'paid')","row":{"budget":500,"orders":[{"amount":120,"status":"paid"},{"amount":900,"status":"refunded"}]}}
JSON
# -> {"matched":true}   (只看已付的 120)

# 聚合:标量聚合子查询(COUNT/SUM/AVG/MIN/MAX)
curl -s localhost:8080/rules/test -H 'Content-Type: application/json' -d @- <<'JSON'
{"rule":"(SELECT COUNT(*) FROM orders WHERE status = 'paid') >= 2","row":{"orders":[{"status":"paid"},{"status":"paid"},{"status":"refunded"}]}}
JSON
# -> {"matched":true}

curl -s localhost:8080/rules/test -H 'Content-Type: application/json' -d @- <<'JSON'
{"rule":"budget > (SELECT SUM(amount) FROM orders)","row":{"budget":500,"orders":[{"amount":120},{"amount":80},{"amount":50}]}}
JSON
# -> {"matched":true}   (500 > 250)
```

## 3. 整行评估全部规则 `POST /evaluate/all`

`{"uid","row"}` → `{"passed":bool, ...}`;`passed=true` 表示该 row 命中所有已加载规则。
下面这条 row 就是 `users_advanced.json` 里的 `uid 1`,命中全部 18 条:

```bash
curl -s localhost:8080/evaluate/all -H 'Content-Type: application/json' -d @- <<'JSON'
{
  "uid": 1,
  "row": {
    "name": "Alice123",
    "phone": "13912345678",
    "budget": 500,
    "profile": "{\"city\":\"深圳\",\"age\":30,\"vip\":true,\"addr\":{\"zip\":\"518000\"}}",
    "tags": ["vip", "gold", "高价值"],
    "scores": [70, 85, 92],
    "orders": [
      {"amount": 120, "status": "paid", "qty": 2},
      {"amount": 80,  "status": "paid", "qty": 1},
      {"amount": 50,  "status": "refunded", "qty": 3}
    ]
  }
}
JSON
# -> {"passed":true, ...}
```

把 `orders` 清空、或把 `profile` 改成 `"{}"`,即可看到聚合/JSON 规则进入 `failed` 列表
(聚合空集:`COUNT`→0,`SUM/AVG/MIN/MAX`→NULL → 比较判 false;JSON 缺键→NULL)。
