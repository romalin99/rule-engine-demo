# Functions & expression syntax

A complete reference for **rule authors**: what a rule may contain, how each
operator/function behaves, and where the boundaries are. For the underlying node
shapes see [ir.md](ir.md). Everything here is parsed by the native SQL syntax
(`pkg/ir`) and evaluated by the `bytecode` (default) and `ast` runtimes.

> 中文版见 [functions.md](functions.md)。

---

## 1. What a rule is

A rule is a boolean **predicate expression** (the body of a SQL `WHERE`)
evaluated against **one flat row** (`map[string]any`); it returns `true` (match)
or `false`.

- It is **not** a full SQL statement — no `SELECT … FROM …`, no trailing `;`.
- Whitespace and newlines are ignored.
- Keywords (`AND`/`OR`/`BETWEEN`/`LIKE`…) and function names are **case-insensitive**.
- String literals use single `'…'` or double `"…"` quotes; `\` escapes inside (`'it\'s'`).

```
rule: age >= 18 AND province = '广东'
row:  {"age": 20, "province": "广东"}
=>    match (true)
```

---

## 2. Types

| Type    | Source (row field value)                          |
| ------- | ------------------------------------------------- |
| number  | `int` / `int64` / `float64` … numeric types       |
| string  | `string`                                          |
| bool    | `bool`                                            |
| array   | `[]string` or `[]any` (elements treated as text)  |
| NULL    | field **absent**, or value is `nil`               |

**Comparison** is numeric when both operands are numeric, otherwise lexical
(string). For ISO date strings, lexical order equals chronological order
(`'2026-01-01' < '2026-02-01'`). **Either side** of a comparison may be a field,
a literal, or a function call — e.g. `LENGTH(a) = LENGTH(b)`,
`last_login >= CURRENT_DATE`.

---

## 3. NULL semantics (important)

This engine uses **two-valued logic** (not SQL's three-valued logic):

- An absent or `nil` field is **NULL**.
- A function of NULL is **NULL** (propagation): `LOWER(missing)` → NULL.
- **Any comparison with a NULL operand is `false`** — both `=` and `<>`.
- `NOT(...)` negates the **boolean result**: since a NULL comparison is `false`,
  `NOT (field = v)` is `true` when the field is missing.

```
rule: risk_level = '高'          row: {}  => no match  (NULL = '高' is false)
rule: risk_level <> '高'         row: {}  => no match  (NULL <> '高' is also false)
rule: NOT (risk_level = '高')    row: {}  => match      (NOT(false) = true)
```

> To **require a field to be present**, add `IS NOT NULL` explicitly:
> `risk_level IS NOT NULL AND risk_level <> '高'` (now `{}` does not match).

---

## 4. Operators

| Category   | Forms                                                  |
| ---------- | ------------------------------------------------------ |
| Comparison | `=` `==` `!=` `<>` `>` `>=` `<` `<=`                    |
| Range      | `x BETWEEN lo AND hi`                                  |
| Set        | `x IN (a, b, …)` / `x NOT IN (…)`                       |
| Pattern    | `x LIKE 'a%'` / `x NOT LIKE '%b%'` (only `%`; no `_`)   |
| Null       | `x IS NULL` / `x IS NOT NULL`                          |
| Logical    | `AND`, `OR`, `NOT (…)`, parentheses, arbitrary nesting |

```
rule: gender = '男'                              row: {"gender":"男"}             => match
rule: age BETWEEN 25 AND 40                      row: {"age":30}                  => match
rule: province IN ('广东','江苏')                row: {"province":"江苏"}         => match
rule: income_level NOT IN ('<5k','5k-10k')       row: {"income_level":"20k-30k"}  => match
rule: name LIKE '数%'                            row: {"name":"数码城"}           => match (prefix)
rule: name LIKE '%店'                            row: {"name":"便利店"}           => match (suffix)
rule: name LIKE '%码%'                           row: {"name":"数码城"}           => match (contains)
rule: phone IS NULL                              row: {}                          => match
rule: phone IS NOT NULL                          row: {"phone":"139..."}          => match
rule: (vip_level >= 3 OR order_count >= 30)      row: {"vip_level":4}             => match
rule: age >= 25 AND NOT (province IN ('北京'))   row: {"age":30,"province":"广东"} => match
```

> `<>` is the SQL alias of `!=`. `LIKE` recognizes only `%` (prefix/suffix/contains);
> `_` is treated as a literal character.

---

## 5. Functions

Every function may appear on **either side of a comparison** and as the operand
of `BETWEEN` / `IN` / `LIKE` / `IS NULL` (see [§6](#6-functions-as-predicate-operands)).

### 5.1 String

| Function                              | Returns | Notes                                          |
| ------------------------------------- | ------- | ---------------------------------------------- |
| `LOWER(x)` / `UPPER(x)`               | string  | lower / upper case                             |
| `TRIM(x)`                             | string  | trims leading/trailing whitespace              |
| `LENGTH(x)`                           | number  | **rune count** (Unicode-safe)                  |
| `SUBSTRING(s, start, len)` / `SUBSTR` | string  | **1-indexed**, rune-based; out-of-range → `''` |

```
rule: LOWER(name) = 'abc'              row: {"name":"ABC"}           => match
rule: UPPER(code) = 'GD'               row: {"code":"gd"}            => match
rule: TRIM(city) = '北京'              row: {"city":"  北京  "}      => match
rule: LENGTH(name) = 3                 row: {"name":"数码城"}         => match (3 runes)
rule: SUBSTRING(phone, 1, 3) = '139'   row: {"phone":"13912345678"}  => match
```

### 5.2 Math

| Function                 | Returns | Notes                                              |
| ------------------------ | ------- | -------------------------------------------------- |
| `ABS(x)`                 | number  |                                                    |
| `ROUND(x)`               | number  | nearest integer (half away from zero); single-arg  |
| `CEIL(x)` / `CEILING(x)` | number  | round up                                           |
| `FLOOR(x)`               | number  | round down                                         |

> Math functions require a numeric operand; a non-numeric or NULL operand → NULL.

```
rule: ABS(delta) <= 5      row: {"delta":-3}     => match
rule: ROUND(score) = 86    row: {"score":85.6}   => match
rule: CEIL(score) = 86     row: {"score":85.1}   => match
rule: FLOOR(score) = 85    row: {"score":85.9}   => match
```

### 5.3 Date

Dates are handled as **ISO strings** (lexical order = chronological order).
Accepted input layouts: `2006-01-02 15:04:05`, `2006-01-02T15:04:05Z07:00`
(RFC3339), `2006-01-02T15:04:05`, `2006-01-02`.

| Function                            | Returns                       | Notes                          |
| ----------------------------------- | ----------------------------- | ------------------------------ |
| `CURRENT_DATE`                      | string `YYYY-MM-DD`           | **bare keyword, no parens**    |
| `CURRENT_TIMESTAMP`                 | string `YYYY-MM-DD HH:MM:SS`  | bare keyword                   |
| `YEAR(x)` / `MONTH(x)` / `DAY(x)`   | number                        | extracted from a date string   |
| `DATEDIFF(a, b)`                    | number                        | whole days `a − b` (truncated) |

```
rule: register_date <= CURRENT_DATE             row: {"register_date":"2020-01-01"}     => match
rule: YEAR(birthday) = 2000                     row: {"birthday":"2000-06-15"}          => match
rule: MONTH(birthday) = 6                        row: {"birthday":"2000-06-15"}          => match
rule: DAY(birthday) = 15                          row: {"birthday":"2000-06-15 08:30:00"} => match
rule: DATEDIFF('2026-06-28', '2026-06-01') = 27   row: {}                                 => match
rule: DATEDIFF(CURRENT_DATE, last_login) <= 30    row: {"last_login":"2026-06-10"}        => depends on today
```

### 5.4 Array

Array functions read a **list field** (`[]string` or `[]any`, elements compared
as text). **The array argument must be a field reference.**

| Function                          | Returns | Notes                                                  |
| --------------------------------- | ------- | ------------------------------------------------------ |
| `ARRAY_LENGTH(field)`             | number  | element count                                          |
| `ARRAY_CONTAINS(field, v)`        | bool    | `v` (string or number) is an element                   |
| `ARRAY_OVERLAP(field, v1, v2, …)` | bool    | any `vi` is an element (desugars to `OR` of `ARRAY_CONTAINS`) |

> `ARRAY_CONTAINS` / `ARRAY_OVERLAP` are complete boolean predicates;
> `ARRAY_LENGTH` returns a number for use inside a comparison.

```
rule: ARRAY_LENGTH(tags) >= 2              row: {"tags":["vip","new","gold"]} => match
rule: ARRAY_CONTAINS(tags, 'vip')          row: {"tags":["vip","new"]}        => match
rule: ARRAY_CONTAINS(ids, 5)               row: {"ids":[1,5,9]}               => match (text "5")
rule: ARRAY_OVERLAP(tags, 'gold', 'vip')   row: {"tags":["vip"]}              => match
```

---

## 6. Functions as predicate operands

Functions are not limited to comparisons; they may be the left operand of
`BETWEEN` / `IN` / `LIKE` / `IS NULL`:

```
rule: LENGTH(name) BETWEEN 2 AND 4         row: {"name":"abc"}        => match
rule: LOWER(city) IN ('bj','sh')           row: {"city":"BJ"}         => match
rule: LOWER(city) NOT IN ('bj','sh')       row: {"city":"GZ"}         => match
rule: LOWER(name) LIKE 'a%'                row: {"name":"ABC"}        => match
rule: UPPER(name) NOT LIKE '%X'            row: {"name":"abc"}        => match
rule: TRIM(note) IS NULL                   row: {}                    => match
rule: TRIM(note) IS NOT NULL               row: {"note":"  x  "}      => match
```

---

## 7. Worked example

The flagship rule (covering most operators) with one matching row:

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

The row above **matches** (every clause holds). Changing `age` to `21` breaks
`age BETWEEN 25 AND 40`, so the whole rule **fails** (`AND` short-circuits). A
function-based variant:

```sql
LENGTH(nickname) BETWEEN 2 AND 12
AND LOWER(city_code) IN ('bj','sh','gz')
AND DATEDIFF(CURRENT_DATE, last_login) <= 30
AND ARRAY_CONTAINS(tags, '高价值')
```

---

## 8. Runnable examples

Ready-to-run rule and row files live in `data/`:

- [`data/rules_functions.json`](../data/rules_functions.json) — 24 rules, one per
  function / predicate form (the `name`/`comment` fields say which).
- [`data/users_functions.json`](../data/users_functions.json) — `uid 1` is crafted
  to match **all 24** rules; `uid 2` is sparse, to show NULL / partial behaviour.

Functions are parsed by the **native** front-end (not qlbridge), so pass
`-frontend native`:

```bash
# Batch match: load the rules, run them over the demo users
go run ./cmd/api -rules data/rules_functions.json -users data/users_functions.json -frontend native

# Per-rule pass/fail for one row (server pins the native front-end)
go run ./cmd/api -serve :8080 -rules data/rules_functions.json
curl -s localhost:8080/evaluate/all -H 'Content-Type: application/json' -d '{
  "row": {"name":"ABC","code":"gd","city":"  北京  ","city_code":"BJ",
          "phone":"13912345678","delta":-3,"score":85.6,"register_date":"2020-01-01",
          "birthday":"2000-06-15","last_login":"2026-06-10","note":"hello",
          "tags":["vip","新客","高价值"],"ids":[1,5,9]}
}'   # -> {"passed":true, ...}  (uid 1's fields hit every rule)

# Try a single function rule directly
curl -s localhost:8080/rules/test -H 'Content-Type: application/json' \
  -d '{"rule":"LOWER(name) = '\''abc'\''","row":{"name":"ABC"}}'   # -> {"matched":true}
```

---

## 9. Not supported / boundaries

- **Arithmetic operators** (`+ - * /`) and `CASE WHEN … THEN … ELSE … END`. Use
  `DATE_ADD` / `DATE_SUB` for date arithmetic.
- **Cross-table JOINs and external/relational subqueries**: the "table" behind
  `EXISTS` / `ANY` / `ALL` can only be a **collection field on the current row**
  (a scalar array or a nested-object array); it cannot reference an outside data
  source or join tables.
- `LIKE` supports only `%`; the single-character `_` wildcard is literal (use
  `REGEXP` / `RLIKE` / `REGEXP_LIKE`, RE2 syntax, for full patterns).
- Regex is **RE2**: no backreferences `\1` or lookaround `(?=...)`.
- `JSON_EXTRACT` / `JSON_VALUE` return scalar leaves only (objects/arrays → NULL);
  the path must be a string literal. The document may be a JSON string or an
  already-decoded object on the row.
- Inside string literals a backslash escapes only `\'` `\"` `\\`; every other
  `\x` stays verbatim, so regex classes (`\d` `\w`) work either raw or doubled.
- A full `SELECT … FROM … WHERE …;` wrapper — pass only the predicate expression
  (in-row subqueries excepted; see §5.5 of the Chinese reference).
- Functions and set predicates are wired through the native SQL parser and emit
  to **SQL** only (used by `Optimize`); translating them to CEL / Expr / Aviator
  is not yet implemented.

---

## 10. Runtimes

Functions evaluate identically on the default **bytecode** VM and the **ast**
tree-walker; the cross-runtime test `TestASTMatchesBytecodeFunctions`
(`pkg/vm/vm_test.go`) pins the two to agree on every (rule, row) pair.
