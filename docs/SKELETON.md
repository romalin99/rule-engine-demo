# 项目骨架 (SKELETON.md)

参照 **tcg-rulex-engine** 的项目骨架重构 **tcg-rulex-engine** 的工程骨架：采用相同的分层架构
与 `application` 容器式启动（config → infra → service → handler → observability），
并保留 DB 层（infra / repository / tx）。规则引擎作为头部组件接入 service 层。

> 引擎内部架构见 [ARCHITECTURE.md](ARCHITECTURE.md)；本文件讲**工程骨架 / 启动装配**。

---

## 1. 分层依赖（与 ucs-fe 一致，单向无环）

```
                cmd/api  (application 容器: 启动/优雅关闭/pprof/中间件链)
                   │
        ┌──────────┼───────────────┐
        ▼          ▼               ▼
   middleware   router          observability (FlightRecorder)
                   │
                   ▼
              handler (HTTP 层: 仅解码→委托→编码)
                   │
                   ▼
              service (业务编排: 规则装配/匹配/评估)
                   │
                   ▼
              infra.ComManager (依赖容器)
                ├── engine        (规则引擎: 原生前端 + 字节码 VM)
                ├── repository    (数据层: 规则来源 file/DB)
                ├── DBX *sqlx.DB  (Oracle, 默认不接, WireOracle 启用)
                ├── Rc  *redis    (可选)
                └── Uss/Mcs/Wps   (外部 HTTP 客户端)
```

`config` 不依赖任何分层；`infra/service/handler` 都不依赖 `router`；
`router/bootstrap.go` 仅向上聚合 handler。已静态校验无 import 环。

---

## 2. 目录骨架（本次新增 ✚ / 重写 ✎）

```
cmd/
  api/main.go         ✎ ucs-fe 式 application 容器 (分层 HTTP 服务)
  cli/main.go         ✚ 原 cmd/api 的引擎 CLI / demo / bench / 全功能 ops 服务
internal/
  infra/              ✚ ComManager(engine+Manager+DB+redis+clients+repo) + tx.go(WithTx/Tx)
  repository/         ✚ RuleRepository: FileRuleRepository + DBRuleRepository(骨架)
  service/            ✎ RuleService 全功能: Load/Match/Batch/Evaluate/EvaluateAll +
                         List/Upsert/Remove/Test/SelfTest/Publish/Versions/Rollback
  handler/            ✎ RuleHandler 全部端点: handler.go scoring.go admin.go latency.go
  observability/      ✚ FlightRecorder (SIGUSR1/2 dump trace) — 移植自 ucs-fe
  router/routes.go    ✎ RegisterHandlers(全端点) + NewApp + Serve(CLI 复用)
  config/ middleware/ client/ model/ apperror/ masking/ topics/ types/   既有, 复用
pkg/
  conv/ math/ memstatus/   ✚ 移植自 ucs-fe (memstatus 被 main 使用; helper 已删—含 mongo)
  engine/ ir/ vm/ parser/ runtime/ dtable/ ...                           引擎既有
config/ dev.toml sit.toml prod.toml   ✚/✎ 移植自 ucs-fe
scripts/sql/                          ✚ 迁移脚本目录
.golangci.yaml  .mockery.yaml         ✚ 移植自 ucs-fe
```

> 全功能 ops 服务（规则 CRUD / 版本 / Web 编辑器 / swagger / `/match` / `/match/batch` /
> `/evaluate` / `/evaluate/all` / `/rules/selftest` / `/metrics` 等）已**全部迁移**到分层栈
> （handler → service → infra → engine）。`cmd/api` 与 CLI `go run ./cmd/cli -serve :8080`
> 都经同一 `router.RegisterHandlers` 暴露完整端点；区别仅在 `cmd/api` 额外带 ucs-fe 式
> 中间件链 / pprof / 优雅关闭。旧的 `router.Server/NewServer/demo.go/bootstrap.go` 已删除。

---

## 3. 启动流程（cmd/api/main.go）

1. `cfg.Init(env)` 读取 `config/<env>.toml`（ENV=dev|sit|prod）。
2. `newApplication`：`InitLog` → `metrics.Init` → `Telemetry.InitTracer` →
   `infra.NewComManager` → `service.NewRuleService.LoadRules` → `handler.NewScoring`
   → `observability.NewFlightRecorder` → `memstatus.MemStats` 协程。
3. 可选 pprof server（`pprof.enabled`）。
4. `fiber.New`（sonic 编解码、超时、`ServerHeader`、`ErrorHandler`）。
5. 中间件链：`cors` → `Recover` → (可选)`EnableOtelTrace` → `BehaviorLogger`。
6. `router.RegisterHandlers` 挂路由。
7. `gracefulShutdown`：SIGINT/SIGTERM → 在 `shutdownTimeout` 内按序关闭
   Fiber → pprof → FlightRecorder → memstats → goroutine 池 → infra → config → flush 日志。

---

## 4. 运行

```bash
go mod tidy                          # 解析新依赖 (见 §6)
ENV=dev go run ./cmd/api             # 分层 HTTP 服务 (端口见 config/dev.toml)
curl -s localhost:18080/ping         # -> {"ping":"pong"}
curl -s localhost:18080/evaluate/all -d '{"uid":1,"row":{...}}'

go run ./cmd/cli -serve :8080        # 旧全功能 ops 服务 (CRUD/版本/Web/swagger)
go run ./cmd/cli -demo               # 引擎 demo / -export / -gen-rules 基准
```

数据层：默认 `repository.FileRuleRepository`（`data/rules.json`）。接 Oracle 时
`com.WireOracle(cfg)` 切到 `DBRuleRepository`（其 `Load` 为骨架，待补 SQL）。

---

## 5. 与 ucs-fe 的差异

| 维度       | ucs-fe                                               | rulex（本骨架）                           |
| ---------- | ---------------------------------------------------- | ----------------------------------------- |
| 头部组件   | Oracle 业务(玩家校验/时长/条款)                      | 规则引擎(engine)                          |
| repository | 6 个业务表仓库                                       | RuleRepository(file/DB 骨架)              |
| service    | 多业务 service + cron + consumer                     | RuleService(装配/匹配/评估)               |
| handler    | 玩家校验/条款/时长                                   | Scoring(match/evaluate_all) + ping/health |
| DB         | 强依赖 Oracle                                        | 默认不接，`WireOracle` 可选               |
| 其余       | infra/tx/observability/middleware/config/pkg helpers | 同构对齐                                  |

---

## 6. 注意事项 / 待办

- **已删除所有 mongo 相关**：`pkg/helper`（bson Decimal128）整包删除、`config` 的
  `mongo.*` viper 默认与 `OracleConnectInfo.MongodbConnectStringer` 字段、
  `apperror.ModuleMongo` 均已移除。因此**无需** mongo-driver / shopspring-decimal 依赖。
- **全功能 ops 已迁移**：所有端点经 `handler → service → infra → engine`；`cmd/api`
  与 CLI `-serve` 共用 `router.RegisterHandlers`。旧 `router.Server/NewServer/demo.go/bootstrap.go` 已删。
- `internal/repository.DBRuleRepository.Load` 与 `infra.ComManager.WireOracle` 是 DB
  层骨架，接入真实 rules 表后补全。
- `cmd/api` 中间件用了 `github.com/gofiber/fiber/v3/middleware/cors`（fiber v3 自带）。
- **本环境无 Go 工具链，未编译验证**；请本地 `go build ./... && go vet ./... && go test ./...` 收尾。

```

```
