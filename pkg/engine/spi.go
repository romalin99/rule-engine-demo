package engine

import "tcg-rulex-engine/pkg/api"

// This file exposes the canonical Parser / Runtime SPI (steps 2–3) on the engine
// and adapts a Parser+Runtime pair onto the existing Backend machinery, so the
// rule cache, worker pool and statistics run unchanged.
//
//	rule text ─Parser.Parse─▶ Program(IR) ─Runtime.Compile─▶ Plan ─Runtime.Execute(row)─▶ bool
//
// Concrete parsers live in pkg/parser/* and runtimes in pkg/runtime/*; wire any
// pair with NewWithParserRuntime, e.g.:
//
//	import (
//	    qp "tcg-rulex-engine/pkg/parser/qlbridge"
//	    bc "tcg-rulex-engine/pkg/runtime/bytecode"
//	)
//	eng := engine.NewWithParserRuntime(qp.New(), bc.New())

// Parser is the rule-parser SPI (alias of api.Parser). Step 2.
type Parser = api.Parser

// Runtime is the rule-runtime SPI (alias of api.Runtime). Step 3.
type Runtime = api.Runtime

// Program is the unified IR program produced by a Parser. Steps 2 & 4.
type Program = api.Program

// runtimeBackend adapts a (Parser, Runtime) pair to the Backend interface.
type runtimeBackend struct {
	parser  api.Parser
	runtime api.Runtime
}

// NewRuntimeBackend builds a Backend from a parser front-end and a runtime.
func NewRuntimeBackend(p api.Parser, r api.Runtime) Backend {
	return &runtimeBackend{parser: p, runtime: r}
}

// NewWithParserRuntime returns an engine driven by the given Parser + Runtime.
func NewWithParserRuntime(p api.Parser, r api.Runtime) *Engine {
	return NewWithBackend(NewRuntimeBackend(p, r))
}

// Name reports "<runtime>/<parser>", e.g. "bytecode/qlbridge".
func (b *runtimeBackend) Name() string { return b.runtime.Name() + "/" + b.parser.Name() }

// Compile parses rule text to IR and lowers it to a runtime Plan (once, at load).
func (b *runtimeBackend) Compile(exprText string) (any, error) {
	prog, err := b.parser.Parse(exprText)
	if err != nil {
		return nil, err
	}
	return b.runtime.Compile(prog)
}

// NewContext returns the row map directly (the runtimes evaluate over map[string]any).
func (b *runtimeBackend) NewContext(fields map[string]any) any { return fields }

// Eval executes a compiled Plan against the row, mapping errors to false.
func (b *runtimeBackend) Eval(ctx any, plan any) bool {
	row, ok := ctx.(map[string]any)
	if !ok {
		return false
	}
	out, err := b.runtime.Execute(plan, row)
	if err != nil {
		return false
	}
	return out
}
