package engine

import (
	"tcg-rulex-engine/pkg/ir"
	"tcg-rulex-engine/pkg/vm"
)

// BytecodeBackend is the 2026-style backend: a pluggable Frontend parses rules
// into IR, which is compiled to bytecode and executed by the custom VM. qlbridge
// (or any other parser) only ever sits in the Frontend; the VM is the single,
// stable evaluation engine.
//
//	Rule ─Frontend.Parse─▶ IR ─vm.Compile─▶ ByteCode ─VM.Eval(map)─▶ bool
type BytecodeBackend struct {
	fe Frontend
}

// NewBytecodeBackend builds a bytecode backend with the given parser front-end.
func NewBytecodeBackend(fe Frontend) *BytecodeBackend {
	return &BytecodeBackend{fe: fe}
}

// Name reports "bytecode/<frontend>".
func (b *BytecodeBackend) Name() string { return "bytecode/" + b.fe.Name() }

// Compile parses (front-end) then compiles to bytecode.
func (b *BytecodeBackend) Compile(exprText string) (any, error) {
	node, err := b.fe.Parse(exprText)
	if err != nil {
		return nil, err
	}
	return vm.Compile(ir.Optimize(node))
}

// NewContext returns the row map directly — the VM evaluates against
// map[string]any with no per-rule context construction.
func (b *BytecodeBackend) NewContext(fields map[string]any) any { return fields }

// Eval runs the compiled bytecode program against the row.
func (b *BytecodeBackend) Eval(ctx any, ast any) bool {
	prog, ok := ast.(*vm.Program)
	if !ok {
		return false
	}
	row, ok := ctx.(map[string]any)
	if !ok {
		return false
	}
	return prog.Eval(row)
}
