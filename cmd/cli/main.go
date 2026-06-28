// Command cli 是规则引擎的命令行入口（multi-tool / 瑞士军刀）：演示、规则导出、
// 离线基准压测，以及功能完整的运维服务器，全部由命令行参数选择子命令。
//
// 它保留了「原始 cmd/api」的全部行为——在 cmd/api 被改造成分层（ucs-fe 风格、读
// TOML 配置）的 HTTP 服务之后，凡是想用 -serve / -rules / -watch 这类 flag 直接
// 起服务、跑 demo 或压测的场景，都应该用本入口 cmd/cli，而不是 cmd/api。
//
// 两个入口的区别：
//
//	cmd/api  分层生产服务，只接受 -f <config.toml>，端口/规则等全部来自 TOML 配置；
//	         带完整 bootstrap（中间件、pprof、优雅关闭）。
//	cmd/cli  flag 驱动的多功能工具，下面的所有子命令都在这里；-serve 起的服务
//	         复用同一套分层 handler 与 Swagger UI（见 internal/router.Serve）。
//
// 本文件只是一层极薄的包装：它把所有逻辑都委托给 internal/cli.RunCLI()，后者负责
// 解析 flag 并分派到对应的子命令（详见 internal/cli/run.go 的文档与示例）。
//
// # 子命令一览（互斥，按优先级 demo > export > serve > 默认压测 选择第一个命中的）
//
//	-demo                运行 data/*.json 里 5 条命名规则的可读性演示后退出
//	-export <dsl>        把规则转换成 sql|aviator|cel|expr 并打印后退出
//	-serve <addr>        在指定地址启动完整运维控制台 / 评分 API（含 Swagger UI）
//	(以上都不给)          执行「生成/加载规则+用户 → 批量匹配 → 打印报告」的基准压测
//
// # 常用示例
//
//	# 1) 命名规则演示（最直观，验证正确性）
//	go run ./cmd/cli -demo
//
//	# 2) 启动完整运维服务（CRUD / 版本管理 / Web 编辑器 / Swagger）
//	go run ./cmd/cli -serve :8080
//	#    浏览器打开 http://localhost:8080/swagger/index.html 查看接口列表
//	#    运维控制台首页：    http://localhost:8080/tcg-rulex-engine/
//
//	# 3) 用真实规则文件起服务，并开启热加载（改文件即时生效）
//	go run ./cmd/cli -serve :8080 -rules data/rules.json -watch
//
//	# 4) SQL 规则 → 其它 DSL（默认读 data/rules.json，可用 -rules 指定）
//	go run ./cmd/cli -export cel
//	go run ./cmd/cli -export aviator -rules data/rules.json
//
//	# 5) 离线基准压测：生成 1 万条规则 × 1 万个用户，32 worker
//	go run ./cmd/cli -gen-rules 10000 -gen-users 10000 -workers 32
//
//	# 6) A/B 对比不同后端 / 前端
//	go run ./cmd/cli -serve :8080 -backend qlbridge        # 用 qlbridge VM
//	go run ./cmd/cli -demo -backend bytecode -frontend native
//
// 各 flag 的完整说明见 internal/cli.RunCLI。
package main

import "tcg-rulex-engine/internal/cli"

// main 仅委托给 cli.RunCLI()：解析 flag 并分派子命令。所有实际逻辑都在
// internal/cli 包中，便于被测试直接调用而无需经过进程入口。
func main() {
	cli.RunCLI()
}
