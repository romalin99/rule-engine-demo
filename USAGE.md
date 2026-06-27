# 使用ガイド / Detailed Usage

本ドキュメントは rule-engine-demo の具体的な使用例をまとめたものです。
アーキテクチャは一貫して **多言語パーサ → 統一 IR → 字節码 VM** です。

```
Rule(SQL/CEL/Expr/JSON) ─Frontend.Parse─▶ ir.Node ─vm.Compile─▶ ByteCode ─VM.Eval(map)─▶ bool
```

---

## 1. ライブラリとして組み込む

```go
eng := engine.New() // 既定: Bytecode VM + qlbridge パーサ

eng.LoadRules([]model.Rule{
    {ID: 1001, Name: "VIP", Enabled: true, Priority: 10,
        Expr: "age BETWEEN 25 AND 40 AND province IN ('广东','江苏') AND active_score >= 85"},
})

u := model.User{UID: 1, Fields: map[string]any{
    "age": 30, "province": "广东", "active_score": 96.0,
}}
ids := eng.Match(u) // -> [1001]
```

実行可能な例: `go run ./examples/quickstart`

---

## 2. パーサ（Frontend）を切り替える — 多言語入力

同じ業務ルールを SQL / CEL / Expr で書いても、同じ IR にコンパイルされ、同じ VM で評価されます。

```go
// CEL
celEng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.CELFrontend{}))
celEng.LoadRules([]model.Rule{{ID: 1, Enabled: true,
    Expr: `age >= 25 && age <= 40 && province in ["广东","江苏"] && active_score >= 85`}})

// Expr
exEng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.ExprFrontend{}))
exEng.LoadRules([]model.Rule{{ID: 2, Enabled: true,
    Expr: `age >= 25 && age <= 40 && province in ["广东","江苏"] && startsWith(favorite_category, "数")`}})
```

実行可能な例: `go run ./examples/cel_expr`

### 各言語のサポート対象（IR にマップできる部分集合）

| 機能 | SQL(qlbridge/native) | CEL | Expr |
| --- | --- | --- | --- |
| AND / OR | `AND` `OR` | `&&` `\|\|` | `&&` `\|\|` |
| 比較 `= != < <= > >=` | ✅ | ✅ | ✅ |
| 範囲 | `BETWEEN lo AND hi` | `x>=lo && x<=hi` | `x>=lo && x<=hi` |
| 集合 | `IN (...)` | `field in [...]` | `field in [...]` |
| 前方/後方/部分一致 | `LIKE '数%'` | `field.startsWith("数")` | `startsWith(field,"数")` |
| NULL 判定 | `IS [NOT] NULL` | `field == null` | `field == nil` |

部分集合外の構文はコンパイル時にエラーになり、誤評価を防ぎます。

---

## 3. 実行系（Backend）を切り替える — A/B 性能比較

```go
bc := engine.NewWithBackend(engine.NewBytecodeBackend(engine.QLBridgeFrontend{})) // 自研VM
ql := engine.NewWithBackend(engine.NewQLBridgeBackend())                          // qlbridge VM
```

CLI: `go run . -backend bytecode -frontend cel` / `-backend qlbridge`

---

## 4. バッチマッチ（Worker Pool）

```go
users := loadUsers()           // 100万件など
results := eng.MatchBatch(users, 32) // 32 worker
// もしくは統計付き:
results, stats := eng.RunBatch(users, 32)
fmt.Print(stats.Report())      // TPS / Latency / hit 分布
```

CLI: `go run . -workers 32 -gen-rules 10000 -gen-users 100000`

---

## 5. HTTP 実時打分サービス（Fiber v3）

```bash
go run . -serve :8080 -gen-rules 1000 -gen-users 0
```

| メソッド | パス | 用途 |
| --- | --- | --- |
| GET | `/healthz` | 稼働確認 |
| GET | `/rules` | ルール件数 |
| POST | `/match` | 単一ユーザ → マッチしたルール |
| POST | `/match/batch` | ユーザ配列 → 各マッチ結果 |

```bash
curl -s localhost:8080/match -d '{"uid":1,"age":30,"province":"广东","active_score":92}'
# {"uid":1,"hits":1,"rule_ids":[...],"rule_names":[...],"took_ms":0.05}
```

---

## 6. 運営プラットフォーム（Web 管理画面・ステップ11）

`-admin` を付けると `/admin` に運営コンソールがマウントされます。ルールの
**編集・テスト・公開・バージョン管理・ロールバック**ができ、公開すると稼働中の
エンジンへホットリロードされます（停止不要）。

```bash
go run . -serve :8080 -admin -gen-rules 50
# ブラウザで http://localhost:8080/admin を開く
```

API（コンソールが内部で使用、直接叩くことも可能）:

```bash
# ルールを1件テスト（公開せず compile + eval）
curl -s localhost:8080/admin/api/test -d \
  '{"expr":"age BETWEEN 25 AND 40 AND active_score>=85","fields":{"age":30,"active_score":92}}'
# {"ok":true,"matched":true}

# 新バージョンを公開（ホットリロード）
curl -s localhost:8080/admin/api/publish -d \
  '{"note":"add VIP rule","rules":[{"id":1,"name":"VIP","enabled":true,"expr":"active_score>=90"}]}'
# {"version":2,"loaded":1,"failed":0}

# バージョン履歴
curl -s localhost:8080/admin/api/versions

# 旧バージョンへロールバック（新バージョンとして再公開）
curl -s localhost:8080/admin/api/rollback -d '{"version":1}'
```

ワークフロー: `編集 → テスト → 公開 → バージョン → ロールバック`。

---

## 7. 多 DSL 相互変換

```go
for _, dsl := range ir.AllDSLs { // sql / aviator / cel / expr
    out, _ := ir.Convert("age BETWEEN 25 AND 40 AND category LIKE '数%'", dsl)
    fmt.Printf("%-8s %s\n", dsl, out)
}
```

CLI: `go run . -export cel -rules data/rules.json`

---

## 8. ベンチマーク

```bash
go test -bench . ./benchmark/
```

ルール数 100/1000/10000、ユーザ数を変えて TPS・Latency・worker スケーリングを計測します。
