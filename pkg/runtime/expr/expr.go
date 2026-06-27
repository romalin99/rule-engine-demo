// Package expr is a placeholder for an expr-lang native runtime: instead of the
// bytecode VM, it would emit the IR back to an Expr string (ir.Emit(n, ir.Expr)),
// compile it with github.com/expr-lang/expr, and evaluate the resulting program
// against each row. It exists so the runtime/ directory mirrors the parser/ set
// and to enable an A/B benchmark (bytecode vs expr-vm) once wired.
//
// Status: stub (dependency-free). To complete it:
//
//	import "github.com/expr-lang/expr"
//	src := ir.Emit(program, ir.Expr)
//	prog, _ := expr.Compile(src, expr.AllowUndefinedVariables())   // in Compile
//	out, _ := expr.Run(prog, row)                                  // in Execute
//	return out.(bool), nil
package expr

import (
	"fmt"

	"github.com/example/rule-engine-demo/pkg/api"
)

// Runtime implements api.Runtime via the Expr VM (not yet implemented).
type Runtime struct{}

// New returns an Expr runtime stub.
func New() *Runtime { return &Runtime{} }

// Name identifies the runtime.
func (*Runtime) Name() string { return "expr" }

// Compile is not implemented yet; see the package doc for the intended approach.
func (*Runtime) Compile(program api.Program) (api.Plan, error) {
	return nil, fmt.Errorf("expr runtime not implemented yet: emit IR via ir.Emit(program, ir.Expr) and compile with expr.Compile")
}

// Execute is not implemented yet.
func (*Runtime) Execute(plan api.Plan, row map[string]any) (bool, error) {
	return false, fmt.Errorf("expr runtime not implemented yet")
}
