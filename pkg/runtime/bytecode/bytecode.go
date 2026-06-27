// Package bytecode is the default high-performance runtime: it lowers the
// unified IR to the custom stack bytecode (pkg/vm) once per rule, then evaluates
// that bytecode against each row with zero per-eval allocation.
//
//	Program(IR) ─vm.Compile─▶ *vm.Program ─Eval(map)─▶ bool
package bytecode

import (
	"fmt"

	"github.com/example/rule-engine-demo/pkg/api"
	"github.com/example/rule-engine-demo/pkg/vm"
)

// Runtime implements api.Runtime over the custom bytecode VM.
type Runtime struct{}

// New returns a bytecode runtime.
func New() *Runtime { return &Runtime{} }

// Name identifies the runtime.
func (*Runtime) Name() string { return "bytecode" }

// Compile lowers the IR program into a reusable bytecode plan.
func (*Runtime) Compile(program api.Program) (api.Plan, error) {
	return vm.Compile(program)
}

// Execute evaluates a compiled bytecode plan against a row.
func (*Runtime) Execute(plan api.Plan, row map[string]any) (bool, error) {
	prog, ok := plan.(*vm.Program)
	if !ok {
		return false, fmt.Errorf("bytecode: invalid plan type %T", plan)
	}
	return prog.Eval(row), nil
}
