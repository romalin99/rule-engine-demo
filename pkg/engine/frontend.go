package engine

import "github.com/example/rule-engine-demo/pkg/ir"

// Frontend is a pluggable rule parser. It turns rule text into the shared IR.
// The VM/back-end never depends on which Frontend produced the IR, so you can
// swap parsers (qlbridge / native SQL / CEL / Expr) without touching the VM:
//
//	Rule ──Frontend.Parse──▶ ir.Node ──vm.Compile──▶ ByteCode ──VM──▶ bool
type Frontend interface {
	Name() string
	Parse(rule string) (ir.Node, error)
}

// NativeFrontend uses the project's own SQL-subset parser (package ir). It has
// no external dependency and is the default front-end.
type NativeFrontend struct{}

// Name identifies the frontend.
func (NativeFrontend) Name() string { return "native-sql" }

// Parse parses rule text into IR.
func (NativeFrontend) Parse(rule string) (ir.Node, error) { return ir.Parse(rule) }
