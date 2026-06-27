// Command rule-engine-demo is a mini in-memory rule engine.
//
// Run it directly:
//
//	go run .
//
// It generates 10,000 rules and 10,000 users, compiles every rule to a cached
// qlbridge AST, then matches all users against all rules with a worker pool and
// prints throughput (TPS/QPS) and hit statistics.
//
// Useful flags:
//
//	go run . -workers 32 -gen-rules 10000 -gen-users 10000
//	go run . -rules data/rules.json -users data/users.json   # load from files
//	go run . -demo                                            # named 5-rule demo
package main

import "github.com/example/rule-engine-demo/internal/cli"

func main() {
	cli.RunCLI()
}
