// Command rule-engine-demo is a mini in-memory rule engine.
//
// Run it directly:
//
// go run  cmd/api/main.go
//
// It generates 10,000 rules and 10,000 users, compiles every rule to a cached
// qlbridge AST, then matches all users against all rules with a worker pool and
// prints throughput (TPS/QPS) and hit statistics.
//
// Useful flags:
//
//	go run  cmd/api/main.go -workers 32 -gen-rules 10000 -gen-users 10000
//	go run  cmd/api/main.go -rules data/rules.json -users data/users.json   # load from files
//	go run  cmd/api/main.go -demo                                            # named 5-rule demo
package main

import "tcg-rulex-engine/internal/cli"

// @title			AIRuleX 规则引擎 API
// @version		2.0
// @description	AIRuleX 实时规则引擎 HTTP 接口：在线评分（/match、/match/batch、/evaluate）、规则热更新（/rules*）与版本管理（/versions*）。请求/响应示例取自 data/users.json 与 data/rules.json。
// @host			localhost:8080
// @BasePath		/
// @schemes		http
func main() {
	cli.RunCLI()
}
