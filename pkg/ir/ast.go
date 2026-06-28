package ir

// Node is an IR expression node.
type Node interface{ node() }

// Value is a literal operand: either a string or a numeric literal (kept as raw
// text so the original formatting is preserved on emit).
type Value struct {
	Str      string
	Num      string
	IsString bool
}

// Logic is an AND/OR of two or more operands.
type Logic struct {
	Op   string // "AND" or "OR"
	Args []Node
}

// Compare is `field <op> value`, op in = == != >= <= > <.
type Compare struct {
	Field string
	Op    string
	Val   Value
}

// Between is `field BETWEEN lo AND hi`.
type Between struct {
	Field  string
	Lo, Hi Value
}

// In is `field IN (v1, v2, ...)` (Negate=false) or `field NOT IN (...)`
// (Negate=true).
type In struct {
	Field  string
	Vals   []Value
	Negate bool
}

// Like is `field LIKE 'pattern'` (Negate=false) or `field NOT LIKE 'pattern'`
// (Negate=true).
type Like struct {
	Field   string
	Pattern string
	Negate  bool
}

// IsNull is `field IS NULL` (Negate=false) or `field IS NOT NULL` (Negate=true).
// A field counts as NULL when it is absent from the row or holds a nil value.
type IsNull struct {
	Field  string
	Negate bool
}

// Not negates any boolean sub-expression: `NOT (expr)`. It is the general
// negation node; `NOT IN` / `NOT LIKE` are represented as In/Like with
// Negate=true (cheaper to emit and evaluate), while `NOT (a OR b)` and
// `NOT (field = v)` use this node.
type Not struct {
	Arg Node
}

func (Logic) node()   {}
func (Compare) node() {}
func (Between) node() {}
func (In) node()      {}
func (Like) node()    {}
func (IsNull) node()  {}
func (Not) node()     {}
