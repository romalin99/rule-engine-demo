package engine

// Backend is the pluggable rule-language layer.
//
//	rule text ──Compile──▶ AST(any) ─┐
//	user.Fields ──NewContext──▶ ctx ─┴─Eval──▶ bool
//
// Implement this interface to add a new rule language without touching the
// engine, worker pool, statistics or any business code:
//
//	SQL  ─┐
//	CEL  ─┼─▶ Backend ─▶ Program (model.RuleProgram{AST any})
//	Expr ─┘
//
// The compiled AST is opaque (`any`) to everything except the backend that
// produced it. NewContext is called once per user; Eval is called once per
// (user, rule) and must be safe for concurrent use across goroutines.
type Backend interface {
	// Name identifies the backend, e.g. "qlbridge".
	Name() string
	// Compile parses rule text into a reusable AST. Called once at load time.
	Compile(expr string) (any, error)
	// NewContext wraps a user's fields into a backend-specific eval context.
	NewContext(fields map[string]any) any
	// Eval evaluates a compiled AST against a context, returning the boolean
	// result (false on missing field / type error — never panics).
	Eval(ctx any, ast any) bool
}
