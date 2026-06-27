package engine

import (
	"github.com/example/rule-engine-demo/pkg/ir"
	celparser "github.com/example/rule-engine-demo/pkg/parser/cel"
	exprparser "github.com/example/rule-engine-demo/pkg/parser/expr"
)

// CEL and Expr front-ends. The actual rule-text → ir.Node conversion lives in
// the pluggable parser packages (pkg/parser/cel, pkg/parser/expr); these thin
// engine adapters let the CLI / NewBytecodeBackend select them by value:
//
//	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.CELFrontend{}))
//	eng := engine.NewWithBackend(engine.NewBytecodeBackend(engine.ExprFrontend{}))

// CELFrontend parses Google CEL expressions (github.com/google/cel-go) into IR.
type CELFrontend struct{}

// Name identifies the frontend.
func (CELFrontend) Name() string { return "cel" }

// Parse converts a CEL expression into the shared IR.
func (CELFrontend) Parse(rule string) (ir.Node, error) { return celparser.New().Parse(rule) }

// ExprFrontend parses expr-lang expressions (github.com/expr-lang/expr) into IR.
type ExprFrontend struct{}

// Name identifies the frontend.
func (ExprFrontend) Name() string { return "expr" }

// Parse converts an Expr expression into the shared IR.
func (ExprFrontend) Parse(rule string) (ir.Node, error) { return exprparser.New().Parse(rule) }
