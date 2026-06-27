// Package expr is an expr-lang (github.com/expr-lang/expr) runtime: it emits the
// IR back to an Expr string (ir.Emit(n, ir.Expr)), compiles it once, and runs
// the compiled program against each row. It lets the project A/B-benchmark the
// custom bytecode VM against the expr-lang VM on identical rules.
//
// NOTE: requires `github.com/expr-lang/expr`; run `go mod tidy` locally.
package expr

import (
	"fmt"

	exprlang "github.com/expr-lang/expr"
	exprvm "github.com/expr-lang/expr/vm"

	"github.com/example/rule-engine-demo/pkg/api"
	"github.com/example/rule-engine-demo/pkg/ir"
)

// Runtime implements api.Runtime over the expr-lang VM.
type Runtime struct{}

// New returns an Expr runtime.
func New() *Runtime { return &Runtime{} }

// Name identifies the runtime.
func (*Runtime) Name() string { return "expr" }

// Compile lowers IR to an Expr source string and compiles it to a program.
// AllowUndefinedVariables tolerates rows that omit a referenced field (it reads
// as nil, mirroring the bytecode VM's "missing field" semantics).
func (*Runtime) Compile(program api.Program) (api.Plan, error) {
	src := ir.Emit(program, ir.Expr)
	prog, err := exprlang.Compile(src, exprlang.AllowUndefinedVariables(), exprlang.AsBool())
	if err != nil {
		return nil, fmt.Errorf("expr runtime compile %q: %w", src, err)
	}
	return prog, nil
}

// Execute runs the compiled Expr program against a row.
func (*Runtime) Execute(plan api.Plan, row map[string]any) (bool, error) {
	prog, ok := plan.(*exprvm.Program)
	if !ok {
		return false, fmt.Errorf("expr runtime: invalid plan type %T", plan)
	}
	out, err := exprlang.Run(prog, row)
	if err != nil {
		return false, err
	}
	b, _ := out.(bool)
	return b, nil
}
