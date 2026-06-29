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

// ---- Function-call operands (extensible scalar expressions) ---------------

// Term is a scalar-valued operand: a field reference, a literal, or a function
// call. Terms are the foundation for SQL function support. Compare/Between/In/
// Like keep using bare fields + literals for backward compatibility, while
// CompareTerm is the function-aware comparison where either side may be a call.
type Term interface{ term() }

// FieldTerm references a row field by name, e.g. `age`.
type FieldTerm struct{ Name string }

// LitTerm is a literal operand (number or string).
type LitTerm struct{ Val Value }

// CallTerm is a function application, e.g. LOWER(name) or SUBSTRING(name, 1, 3).
// Fn is the upper-cased function name; Args are its operands.
type CallTerm struct {
	Fn   string
	Args []Term
}

// CompareTerm is `<term> <op> <term>`, op in = == != >= <= > <. Unlike Compare
// (field vs literal), either side may be a function call, which enables
// predicates such as `LOWER(name) = 'abc'` or `ABS(balance) >= 100`.
type CompareTerm struct {
	Left  Term
	Op    string
	Right Term
}

// LikeTerm is `<term> LIKE 'pattern'` (Negate = NOT LIKE): the function-aware
// form of Like, e.g. `LOWER(name) LIKE 'a%'`.
type LikeTerm struct {
	Left    Term
	Pattern string
	Negate  bool
}

// IsNullTerm is `<term> IS NULL` (Negate = IS NOT NULL): the function-aware form
// of IsNull, e.g. `TRIM(name) IS NULL`.
type IsNullTerm struct {
	Left   Term
	Negate bool
}

// PredCall is a boolean-valued function call used directly as a predicate (it
// yields a bool, not a scalar), e.g. `ARRAY_CONTAINS(tags, '高价值')`.
type PredCall struct {
	Fn   string
	Args []Term
}

// Regexp is `<term> REGEXP 'pattern'` (Negate = NOT REGEXP / the function form
// REGEXP_LIKE). Pattern is a Go RE2 regular expression; a leading `(?i)` (added
// by the REGEXP_LIKE 'i' match-type) makes it case-insensitive. The left side
// may be a field or a function call, e.g. `LOWER(name) REGEXP '^a'`.
type Regexp struct {
	Left    Term
	Pattern string
	Negate  bool
}

// AggSub is a scalar aggregate sub-query operand:
// `( SELECT <Fn>(<Col>|*) FROM <Coll> [WHERE <Where>] )`. It evaluates to a
// single number over the nested-row collection Coll (rows filtered by the
// optional Where). Fn is COUNT/SUM/MIN/MAX/AVG; Col is "" for COUNT(*). As a
// Term it can sit on either side of a comparison, e.g.
// `(SELECT COUNT(*) FROM orders WHERE amount > 100) > 3`.
type AggSub struct {
	Fn    string
	Col   string
	Coll  string
	Where Node
}

func (FieldTerm) term() {}
func (LitTerm) term()   {}
func (CallTerm) term()  {}
func (AggSub) term()    {}

func (CompareTerm) node() {}
func (LikeTerm) node()    {}
func (IsNullTerm) node()  {}
func (PredCall) node()    {}

// ---- Set predicates: EXISTS / ANY / ALL (quantifiers & sub-queries) --------
//
// The engine evaluates one flat row (map[string]any), so a "table" is a
// collection field on that row: either an array of scalars (`[]string`/`[]any`)
// or an array of nested rows (`[]map[string]any` / `[]any` of maps). EXISTS and
// the ANY/ALL quantifiers range over such a collection.

// Exists is `EXISTS ( coll )` or `EXISTS ( SELECT … FROM coll WHERE pred )`.
// Coll names a collection field on the current row. When Where is nil it is a
// plain non-empty test; when Where is non-nil it is the sub-query form and the
// predicate is evaluated against each nested row (its identifiers resolve to the
// nested row's fields). `NOT EXISTS(...)` is represented with a wrapping Not.
type Exists struct {
	Coll  string
	Where Node // nil = non-empty test; non-nil = ∃ nested row satisfying it
}

// QuantArr is `<left> <op> ANY|ALL ( arrayField )`: compare Left against every
// element of an array field. All=false is ANY/SOME (∃), All=true is ALL (∀).
// An empty/absent array makes ANY false and ALL true (SQL semantics).
type QuantArr struct {
	Left  Term
	Op    string
	All   bool
	Array Term // a FieldTerm referencing the array field
}

// QuantSub is `<left> <op> ANY|ALL ( SELECT col FROM coll [WHERE pred] )`:
// compare Left against the projected Col of each nested row of collection Coll
// that satisfies the optional sub-query predicate Where. All=false is ANY (∃),
// All=true is ALL (∀); an empty result set makes ANY false and ALL true.
type QuantSub struct {
	Left  Term
	Op    string
	All   bool
	Col   string
	Coll  string
	Where Node // nil = no WHERE (all nested rows considered)
}

func (Exists) node()   {}
func (QuantArr) node() {}
func (QuantSub) node() {}
func (Regexp) node()   {}
