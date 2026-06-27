package ir

// Node is an IR expression node.
type Node interface{ node() }

// Value is a literal operand: either a string or a numeric literal (kept as raw
// text so the original formatting is preserved on emit).
type Value struct {
	IsString bool
	Str      string // when IsString
	Num      string // when !IsString (raw number text)
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

// In is `field IN (v1, v2, ...)`.
type In struct {
	Field string
	Vals  []Value
}

// Like is `field LIKE 'pattern'`.
type Like struct {
	Field   string
	Pattern string
}

// IsNull is `field IS NULL` (Negate=false) or `field IS NOT NULL` (Negate=true).
// A field counts as NULL when it is absent from the row or holds a nil value.
type IsNull struct {
	Field  string
	Negate bool
}

func (Logic) node()   {}
func (Compare) node() {}
func (Between) node()  {}
func (In) node()       {}
func (Like) node()     {}
func (IsNull) node()   {}
