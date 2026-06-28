// Command cli is the rule-engine command line: demos, exports, benchmarks and the
// feature-complete operations server. It preserves the original cmd/api behaviour
// after cmd/api was converted to the layered ucs-fe-style HTTP service.
//
//	go run ./cmd/cli -demo                  # named demo rules
//	go run ./cmd/cli -serve :8080           # full ops console (CRUD / versions / web)
//	go run ./cmd/cli -export cel            # SQL → CEL/Expr/Aviator
//	go run ./cmd/cli -gen-rules 10000 -gen-users 10000   # benchmark run
package main

import "tcg-rulex-engine/internal/cli"

func main() {
	cli.RunCLI()
}
