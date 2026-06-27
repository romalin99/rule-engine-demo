// Command main is an alternative entry point matching the project layout.
// It is equivalent to `go run .` at the repo root.
//
//	go run ./cmd
package main

import "github.com/example/rule-engine-demo/engine"

func main() {
	engine.RunCLI()
}
