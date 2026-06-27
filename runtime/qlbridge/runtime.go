// Package qlbridge (runtime) evaluates compiled rule expressions against a
// single user's wide-table record.
package qlbridge

import (
	"fmt"

	"github.com/araddon/qlbridge/datasource"
	"github.com/araddon/qlbridge/value"
	"github.com/araddon/qlbridge/vm"

	qlparser "github.com/example/rule-engine-demo/parser/qlbridge"
)

// Evaluator runs compiled qlbridge expressions.
type Evaluator struct{}

// New returns a qlbridge-backed evaluator.
func New() *Evaluator { return &Evaluator{} }

// Eval evaluates a previously-compiled expression against a user record.
//
// `user` is the user's wide-table row as column -> value (int / float64 /
// string / bool ...). Keys must match the identifiers used in the rule
// (age, province, total_amount, ...).
//
// Returns true only when the expression evaluates to a real boolean true.
// A missing field, type error, or non-boolean result yields (false, nil) so a
// single bad rule never blocks the others.
func (e *Evaluator) Eval(compiled any, user map[string]any) (bool, error) {
	ce, ok := compiled.(*qlparser.CompiledExpr)
	if !ok {
		return false, fmt.Errorf("qlbridge: unexpected compiled type %T", compiled)
	}

	// Wrap the native Go map as an evaluation context. Creating this is cheap;
	// the compiled AST (ce.Node) is the expensive part and is reused.
	ctx := datasource.NewContextSimpleNative(user)

	val, ok := vm.Eval(ctx, ce.Node)
	if !ok || val == nil || val.Nil() || val.Err() {
		return false, nil
	}

	b, ok := value.ValueToBool(val)
	if !ok {
		return false, nil
	}
	return b, nil
}
