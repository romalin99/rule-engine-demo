// Package api defines the rule-engine SPI (service-provider interface): the
// Parser and Runtime contracts that every pluggable front-end / back-end
// implements, plus the Program type that flows between them.
//
// This is the seam described by steps 2–4 of the design:
//
//	rule text ──Parser.Parse──▶ Program (== ir.Node, the unified IR)
//	Program ──Runtime.Compile──▶ Plan ──Runtime.Execute(row)──▶ bool
//
// Parsers and runtimes live in their own packages (pkg/parser/*, pkg/runtime/*)
// and depend only on this package and pkg/ir, never on the engine. That keeps
// the engine, worker pool and business code independent of any concrete rule
// language or evaluator.
package api

import "tcg-rulex-engine/pkg/ir"

// Program is a parsed rule in the project's unified IR. A Parser produces it and
// a Runtime consumes it. It is exactly ir.Node (step 4: the IR is the program),
// so every parser — SQL, JSON, Expr, CEL, Vitess — emits the same shape.
type Program = ir.Node

// Plan is a Runtime-specific compiled artifact (e.g. bytecode, or a wrapped
// tree). It is opaque to everything except the Runtime that produced it.
type Plan = any

// Parser turns rule text into a Program (unified IR). Implementations live under
// pkg/parser/* and must be safe for concurrent use.
type Parser interface {
	// Name identifies the parser, e.g. "qlbridge", "json", "expr", "cel".
	Name() string
	// Parse converts one rule's text into the unified IR.
	Parse(rule string) (Program, error)
}

// Runtime executes Programs against a row (map[string]any). Compile is called
// once per rule at load time and returns a reusable Plan; Execute is the hot
// path, called once per (rule, row) and must be concurrency-safe.
//
// This matches step 3's Runtime.Execute(program, row) (bool, error): Plan is the
// compiled form of the program, so the hot path never re-parses or re-compiles.
type Runtime interface {
	// Name identifies the runtime, e.g. "bytecode", "ast", "cel", "expr".
	Name() string
	// Compile lowers a Program into a reusable Plan (called once at load).
	Compile(program Program) (Plan, error)
	// Execute evaluates a compiled Plan against one row.
	Execute(plan Plan, row map[string]any) (bool, error)
}
