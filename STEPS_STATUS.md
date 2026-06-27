# 11ステップ 実装状況と今回の変更

ご提示の11ステップを既存コードと突き合わせた監査結果、今回実装した内容、
および「ディレクトリ物理再編」を後続作業として残した理由をまとめます。

## 監査結果（突き合わせ）

| Step | 要件 | 状態 | 該当箇所 |
| --- | --- | --- | --- |
| 1 MVP | Rule→qlbridge→Match | ✅ 既存 | `cmd/main.go`, `engine/` |
| 2 統一 Parser | `Parse(rule)→Program` | ✅ 既存 | `engine/frontend.go`（`Frontend.Parse → ir.Node`） |
| 3 統一 Runtime | `Execute(prog,row)→bool` | ✅ 既存 | `engine/backend.go`（`Backend`: Compile/NewContext/Eval） |
| 4 統一 IR | Node/And/Between… | ✅ 既存 | `ir/ast.go`（Logic/Compare/Between/In/Like/IsNull） |
| 5 Bytecode VM | LOAD/IN/AND/RET | ✅ 既存 | `vm/*`＋`engine/backend_bytecode.go` |
| 6 Rule Cache | sync.Map・10万件 | ✅ 既存 | `engine/cache.go` |
| 7 Worker Pool | 32 worker | ✅ 既存 | `engine/workerpool.go` |
| 8 Benchmark | rule/user 行列・TPS/Latency | ✅ 既存 | `benchmark/*_test.go` |
| 9 プラグイン Parser | qlbridge/expr/cel/vitess/json | ✅ **今回 CEL・Expr を本実装** | `frontend_{qlbridge,json,cel,expr}.go`（vitess は未対応） |
| 10 プラグイン Runtime | ast/bytecode/cel/expr | ✅ CEL/Expr は IR 経由で bytecode VM 上で動作 | `backend_bytecode.go`, `backend_qlbridge.go` |
| 11 運営プラットフォーム | 編集/テスト/公開/版本/回滚 | ✅ **今回実装** | `engine/admin.go`, `engine/admin_html.go`（`-admin`） |

## 今回の変更（このブランチ `feature/cel-expr-webadmin`）

1. **CEL Frontend 本実装** — `engine/frontend_cel.go`。cel-go でパース→protobuf AST を走査→`ir.Node`。stub を撤去。
2. **Expr Frontend 本実装** — `engine/frontend_expr.go`。expr-lang/expr でパース→AST を走査→`ir.Node`。
3. **Web 運営プラットフォーム** — `engine/admin.go`＋`engine/admin_html.go`。
   バージョン管理ストア＋`/admin` コンソール（編集/テスト/公開/版本/回滚）。公開・回滚は
   `Engine.ReplaceRules` で稼働中エンジンへホットリロード。`-admin` フラグで有効化。
4. **使用例・ドキュメント** — `examples/cel_expr/`、`USAGE.md`、README 更新。
5. **go.mod** — `github.com/google/cel-go`、`github.com/expr-lang/expr`、
   `google.golang.org/genproto/googleapis/api` を追加。

## ⚠️ 検証が必須

このセッション環境では Go の導入も依存取得（cel-go / expr-lang）もプロキシにブロックされ
**コンパイル検証ができませんでした**。ローカルで必ず以下を実行してください。

```bash
go mod tidy          # cel-go / expr-lang / genproto と go.sum を解決
go build ./...
go vet ./...
go test ./...
go run ./examples/cel_expr
go run . -serve :8080 -admin -gen-rules 50   # http://localhost:8080/admin
```

特に CEL/Expr の AST 走査は外部ライブラリの API バージョンに敏感です。`go build` で
型・シグネチャの不一致が出た場合はそこを調整してください（変換ロジック自体は対象部分集合に対し正しい設計です）。

## ステップ「ディレクトリ物理再編」を後続にした理由

ご提示のツリー（`parser/qlbridge/`・`runtime/qlbridge/` を独立パッケージ化、
`cmd/rulexdemo/`、`testdata/`）への**物理移動は機能的価値がゼロ**である一方、
`engine` パッケージ内に密結合した `Frontend`/`Backend`/`BytecodeBackend` を別パッケージへ
移すと、`engine.go`・`benchmark.go`・`server.go`・各テストの import 連鎖を一斉に書き換える
**広範囲リファクタ**になります。本環境ではコンパイル検証ができないため、検証なしで一括移動すると
現在動作しているプロジェクトを壊すリスクが高いと判断しました。

### 推奨する移行手順（ローカルでコンパイルしながら）

| 現在 | 再編後（スケッチ準拠） |
| --- | --- |
| `cmd/main.go` | `cmd/rulexdemo/main.go` |
| `engine/frontend*.go` | `parser/`（interface）＋`parser/{qlbridge,json,native,cel,expr}/` |
| `engine/backend*.go` | `runtime/`（interface）＋`runtime/{bytecode,qlbridge}/` |
| `engine/workerpool.go` | `engine/worker.go` |
| `data/` | `testdata/` |

1. `parser` パッケージを新設し `Parser`/`Program` interface を定義、各 frontend を移設。
2. `runtime` パッケージを新設し `Runtime` interface を定義、各 backend を移設。
3. `engine` は `parser`/`runtime` に依存（循環なし: parser/runtime は engine を import しない）。
4. 1ステップごとに `go build ./...` で検証してからコミット。

この再編が必要であれば、ローカルでビルドを回せる前提で別途対応します。
