// Package qlbridge (parser) compiles SQL-WHERE style rule expressions into
// reusable qlbridge AST nodes.
//
// The returned *CompiledExpr holds an expr.Node which, per qlbridge's design,
// is safe to evaluate concurrently from many goroutines. Compile once, eval
// many times.
package qlbridge

import (
	"fmt"

	"github.com/araddon/qlbridge/expr"
	"github.com/araddon/qlbridge/expr/builtins"
)

func init() {
	// Make qlbridge's built-in functions (lower, contains, todate, ...)
	// available to expressions. Pure comparison/BETWEEN/IN/LIKE rules don't
	// need this, but loading it keeps the engine future-proof and is a no-op
	// on the hot path.
	builtins.LoadAllBuiltins()
}

// CompiledExpr is an opaque, reusable compiled rule expression.
type CompiledExpr struct {
	Source string    // original expression text, for logging/debugging
	Node   expr.Node // parsed qlbridge AST
}

// Parser turns rule text into compiled expressions.
type Parser struct{}

// New returns a qlbridge-backed parser.
func New() *Parser { return &Parser{} }

// Compile parses a SQL-boolean expression and returns a reusable compiled form.
// The result is returned as `any` so the engine stays decoupled from qlbridge
// (pluggable parser/runtime).
func (p *Parser) Compile(expression string) (any, error) {
	node, err := expr.ParseExpression(expression)
	if err != nil {
		return nil, fmt.Errorf("qlbridge: cannot parse rule %q: %w", expression, err)
	}
	return &CompiledExpr{Source: expression, Node: node}, nil
}
