package engine

import (
	"fmt"

	"github.com/example/rule-engine-demo/pkg/ir"
)

// This file holds extension points for future parser front-ends. Each one only
// needs to turn rule text into the shared ir.Node; the IR → ByteCode → VM
// pipeline and all business code stay unchanged.
//
// To implement one, replace the stub body with a real converter:
//   - CEL:  github.com/google/cel-go  → walk the checked AST → ir.Node
//   - Expr: github.com/expr-lang/expr → walk the compiled tree → ir.Node
//
// Then register it:
//
//	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.CELFrontend{}))

// errNotImplemented is returned by stub frontends.
func errNotImplemented(name string) error {
	return fmt.Errorf("%s frontend not implemented yet (roadmap phase 2); "+
		"implement Parse() to convert its AST into ir.Node", name)
}

// CELFrontend is a placeholder for a Google CEL front-end.
type CELFrontend struct{}

// Name identifies the frontend.
func (CELFrontend) Name() string { return "cel" }

// Parse is not yet implemented.
func (CELFrontend) Parse(rule string) (ir.Node, error) { return nil, errNotImplemented("cel") }

// ExprFrontend is a placeholder for an expr-lang front-end.
type ExprFrontend struct{}

// Name identifies the frontend.
func (ExprFrontend) Name() string { return "expr" }

// Parse is not yet implemented.
func (ExprFrontend) Parse(rule string) (ir.Node, error) { return nil, errNotImplemented("expr") }
