// Package qlbridge is a Runtime that evaluates rules on the qlbridge VM (rather
// than the custom bytecode VM). It lets the project A/B-benchmark the two
// engines on identical rules: emit the IR back to SQL, parse it with qlbridge,
// and evaluate that AST against each row.
//
//	Program(IR) ─ir.Emit(SQL)─▶ qlbridge AST ─qlbridge.vm.Eval(row)─▶ bool
package qlbridge

import (
	"fmt"
	"sync"

	qlds "github.com/araddon/qlbridge/datasource"
	"github.com/araddon/qlbridge/expr"
	"github.com/araddon/qlbridge/expr/builtins"
	"github.com/araddon/qlbridge/value"
	qlvm "github.com/araddon/qlbridge/vm"

	"github.com/example/rule-engine-demo/pkg/api"
	"github.com/example/rule-engine-demo/pkg/ir"
)

var builtinsOnce sync.Once

// Runtime implements api.Runtime over the qlbridge VM.
type Runtime struct{}

// New returns a qlbridge runtime with builtins loaded once per process.
func New() *Runtime {
	builtinsOnce.Do(builtins.LoadAllBuiltins)
	return &Runtime{}
}

// Name identifies the runtime.
func (*Runtime) Name() string { return "qlbridge" }

// Compile lowers IR back to SQL and parses it into a reusable qlbridge AST.
func (*Runtime) Compile(program api.Program) (api.Plan, error) {
	node, err := expr.ParseExpression(ir.Emit(program, ir.SQL))
	if err != nil {
		return nil, fmt.Errorf("qlbridge runtime compile: %w", err)
	}
	return node, nil
}

// Execute evaluates the qlbridge AST against a row.
func (*Runtime) Execute(plan api.Plan, row map[string]any) (bool, error) {
	node, ok := plan.(expr.Node)
	if !ok {
		return false, fmt.Errorf("qlbridge runtime: invalid plan type %T", plan)
	}
	val, ok := qlvm.Eval(qlds.NewContextSimpleNative(row), node)
	if !ok || val == nil || val.Nil() || val.Err() {
		return false, nil
	}
	b, ok := value.ValueToBool(val)
	return ok && b, nil
}
