package engine

import (
	"fmt"
	"strings"
	"sync"

	qlds "github.com/araddon/qlbridge/datasource"
	"github.com/araddon/qlbridge/expr"
	"github.com/araddon/qlbridge/expr/builtins"
	"github.com/araddon/qlbridge/value"
	"github.com/araddon/qlbridge/vm"
)

// QLBridgeBackend implements Backend using the qlbridge SQL expression engine.
type QLBridgeBackend struct{}

// NewQLBridgeBackend returns a qlbridge backend with all functions registered.
func NewQLBridgeBackend() *QLBridgeBackend {
	loadBuiltins()
	return &QLBridgeBackend{}
}

// Name identifies the backend.
func (*QLBridgeBackend) Name() string { return "qlbridge" }

// Compile parses SQL-WHERE expression text into a reusable qlbridge AST.
func (*QLBridgeBackend) Compile(exprText string) (any, error) {
	node, err := expr.ParseExpression(exprText)
	if err != nil {
		return nil, fmt.Errorf("qlbridge parse %q: %w", exprText, err)
	}
	return node, nil
}

// NewContext wraps a user's fields into a qlbridge evaluation context. Build it
// once per user and reuse it across all rules — boxing the native values is the
// main per-user cost; AST evaluation itself is cheap.
func (*QLBridgeBackend) NewContext(fields map[string]any) any {
	return qlds.NewContextSimpleNative(fields)
}

// Eval evaluates a compiled AST against a prepared context.
func (*QLBridgeBackend) Eval(ctx any, ast any) bool {
	ec, ok := ctx.(expr.EvalContext)
	if !ok {
		return false
	}
	node, ok := ast.(expr.Node)
	if !ok {
		return false
	}
	val, ok := vm.Eval(ec, node)
	if !ok || val == nil || val.Nil() || val.Err() {
		return false
	}
	b, ok := value.ValueToBool(val)
	return ok && b
}

// ---------------------------------------------------------------------------
// builtins + custom functions (registered once per process)
// ---------------------------------------------------------------------------

var builtinsOnce sync.Once

func loadBuiltins() {
	builtinsOnce.Do(func() {
		builtins.LoadAllBuiltins()
		expr.FuncAdd("has_prefix", &hasPrefix{})
		expr.FuncAdd("in_set", &inSet{})
	})
}

// hasPrefix(field, prefix) -> bool. e.g. has_prefix(favorite_category, "数")
type hasPrefix struct{}

func (hasPrefix) Type() value.ValueType { return value.BoolType }
func (hasPrefix) Validate(n *expr.FuncNode) (expr.EvaluatorFunc, error) {
	if len(n.Args) != 2 {
		return nil, fmt.Errorf("has_prefix(field, prefix) expects 2 args, got %d", len(n.Args))
	}
	return func(_ expr.EvalContext, args []value.Value) (value.Value, bool) {
		if anyNil(args) {
			return value.BoolValueFalse, true
		}
		return value.NewBoolValue(strings.HasPrefix(args[0].ToString(), args[1].ToString())), true
	}, nil
}

// in_set(field, "a", "b", ...) -> bool. Function-style alternative to IN.
type inSet struct{}

func (inSet) Type() value.ValueType { return value.BoolType }
func (inSet) Validate(n *expr.FuncNode) (expr.EvaluatorFunc, error) {
	if len(n.Args) < 2 {
		return nil, fmt.Errorf("in_set(field, v1, ...) expects >=2 args, got %d", len(n.Args))
	}
	return func(_ expr.EvalContext, args []value.Value) (value.Value, bool) {
		if args[0] == nil || args[0].Nil() || args[0].Err() {
			return value.BoolValueFalse, true
		}
		needle := args[0].ToString()
		for _, a := range args[1:] {
			if a != nil && a.ToString() == needle {
				return value.BoolValueTrue, true
			}
		}
		return value.BoolValueFalse, true
	}, nil
}

func anyNil(args []value.Value) bool {
	for _, a := range args {
		if a == nil || a.Nil() || a.Err() {
			return true
		}
	}
	return false
}
