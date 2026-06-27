// Package cel is a Google CEL (github.com/google/cel-go) runtime: it emits the
// IR to a CEL string (ir.Emit(n, ir.CEL)), builds a CEL program, and evaluates
// it against each row. CEL is statically typed, so the referenced fields are
// extracted from the IR and declared as dynamic variables before compiling.
//
// NOTE: requires `github.com/google/cel-go`; run `go mod tidy` locally. The
// cel-go program API (NewEnv/Variable/Compile/Program/Eval) is targeted as of
// v0.18+; adjust if your version differs.
package cel

import (
	"fmt"

	celgo "github.com/google/cel-go/cel"

	"github.com/example/rule-engine-demo/pkg/api"
	"github.com/example/rule-engine-demo/pkg/ir"
)

// Runtime implements api.Runtime over the CEL evaluator.
type Runtime struct{}

// New returns a CEL runtime.
func New() *Runtime { return &Runtime{} }

// Name identifies the runtime.
func (*Runtime) Name() string { return "cel" }

type plan struct{ prog celgo.Program }

// Compile lowers IR to CEL, declares referenced fields as dyn vars, and builds
// a reusable CEL program.
func (*Runtime) Compile(program api.Program) (api.Plan, error) {
	fields := collectFields(program, map[string]struct{}{})
	opts := make([]celgo.EnvOption, 0, len(fields))
	for f := range fields {
		opts = append(opts, celgo.Variable(f, celgo.DynType))
	}
	env, err := celgo.NewEnv(opts...)
	if err != nil {
		return nil, fmt.Errorf("cel runtime env: %w", err)
	}
	ast, iss := env.Compile(ir.Emit(program, ir.CEL))
	if iss != nil && iss.Err() != nil {
		return nil, fmt.Errorf("cel runtime compile: %w", iss.Err())
	}
	prg, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("cel runtime program: %w", err)
	}
	return plan{prog: prg}, nil
}

// Execute evaluates the CEL program against a row.
func (*Runtime) Execute(p api.Plan, row map[string]any) (bool, error) {
	cp, ok := p.(plan)
	if !ok {
		return false, fmt.Errorf("cel runtime: invalid plan type %T", p)
	}
	out, _, err := cp.prog.Eval(row)
	if err != nil {
		return false, err
	}
	b, ok := out.Value().(bool)
	return ok && b, nil
}

// collectFields gathers every field name referenced by the IR tree.
func collectFields(n ir.Node, acc map[string]struct{}) map[string]struct{} {
	switch t := n.(type) {
	case ir.Logic:
		for _, a := range t.Args {
			collectFields(a, acc)
		}
	case ir.Compare:
		acc[t.Field] = struct{}{}
	case ir.Between:
		acc[t.Field] = struct{}{}
	case ir.In:
		acc[t.Field] = struct{}{}
	case ir.Like:
		acc[t.Field] = struct{}{}
	case ir.IsNull:
		acc[t.Field] = struct{}{}
	case ir.Not:
		collectFields(t.Arg, acc)
	}
	return acc
}
