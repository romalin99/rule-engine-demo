// Package cel is a placeholder for a Google CEL native runtime: instead of the
// bytecode VM, it would emit the IR back to a CEL string (ir.Emit(n, ir.CEL)),
// compile it with github.com/google/cel-go, and evaluate the program against
// each row. It exists so the runtime/ directory mirrors the parser/ set and to
// enable an A/B benchmark (bytecode vs cel-vm) once wired.
//
// Status: stub (dependency-free). Completing it requires declaring the row's
// variables in the CEL env (CEL is statically typed), e.g. declare each field as
// cel.Variable(name, cel.DynType), then:
//
//	prog, _ := env.Program(ast)              // in Compile
//	out, _, _ := prog.Eval(row)              // in Execute
//	return out.Value().(bool), nil
package cel

import (
	"fmt"

	"github.com/example/rule-engine-demo/pkg/api"
)

// Runtime implements api.Runtime via the CEL VM (not yet implemented).
type Runtime struct{}

// New returns a CEL runtime stub.
func New() *Runtime { return &Runtime{} }

// Name identifies the runtime.
func (*Runtime) Name() string { return "cel" }

// Compile is not implemented yet; see the package doc for the intended approach.
func (*Runtime) Compile(program api.Program) (api.Plan, error) {
	return nil, fmt.Errorf("cel runtime not implemented yet: emit IR via ir.Emit(program, ir.CEL), declare row vars, and build a cel.Program")
}

// Execute is not implemented yet.
func (*Runtime) Execute(plan api.Plan, row map[string]any) (bool, error) {
	return false, fmt.Errorf("cel runtime not implemented yet")
}
