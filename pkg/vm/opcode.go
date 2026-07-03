// Package vm is a tiny stack-based bytecode virtual machine for boolean rule
// expressions. It is completely independent of any parser: it consumes an IR
// tree (package ir), compiles it to flat bytecode, and evaluates that bytecode
// against a map[string]any row.
//
//	IR ──Compile──▶ Program(ByteCode) ──Eval(row)──▶ bool
//
// Swapping the front-end parser (qlbridge / SQLParser / CEL / Expr) changes only
// how the IR is produced; this VM stays the same.
package vm

import "regexp"

// OpCode is a single bytecode instruction kind.
type OpCode uint8

const (
	OpLoadField    OpCode = iota // A=field idx:   push row[field]
	OpConstNum                   // A=num idx:     push number const
	OpConstStr                   // A=str idx:     push string const
	OpEq                         // pop b,a:       push a == b
	OpNe                         // pop b,a:       push a != b
	OpGt                         // pop b,a:       push a > b
	OpGe                         // pop b,a:       push a >= b
	OpLt                         // pop b,a:       push a < b
	OpLe                         // pop b,a:       push a <= b
	OpBetween                    // pop hi,lo,x:   push lo <= x <= hi
	OpIn                         // A=set idx:     pop x, push x in set
	OpLikePrefix                 // A=str idx:     pop x, push hasPrefix(x, s)
	OpLikeSuffix                 // A=str idx:     pop x, push hasSuffix(x, s)
	OpLikeContains               // A=str idx:     pop x, push contains(x, s)
	OpLikeEq                     // A=str idx:     pop x, push x == s
	OpIsNull                     // pop x:         push x is null/missing
	OpIsNotNull                  // pop x:         push x is present/non-null
	OpAnd                        // pop b,a:       push a && b
	OpOr                         // pop b,a:       push a || b
	OpNot                        // pop a:         push !a  (NOT / NOT IN / NOT LIKE)

	// Scalar value functions: pop operand(s) and push a computed value (string or
	// number, not a bool). These back SQL function calls; see compile.go (fnOp).
	OpUpper  // pop x:     push UPPER(x)
	OpLower  // pop x:     push LOWER(x)
	OpTrim   // pop x:     push TRIM(x)
	OpLength // pop x:     push LENGTH(x) (rune count)
	OpAbs    // pop x:     push ABS(x)
	OpRound  // pop x:     push ROUND(x) (nearest integer)
	OpRound2 // pop d,x:   push ROUND(x, d) (round to d decimal places)
	OpCeil   // pop x:     push CEIL(x)
	OpFloor  // pop x:     push FLOOR(x)
	OpSubstr // pop s,a,b: push SUBSTRING(s,a,b) (1-indexed)

	// Date functions. Dates are handled as ISO strings (lexical order == chrono
	// order), so no dedicated value kind is needed.
	OpCurrentDate // push CURRENT_DATE (today, "2006-01-02")
	OpCurrentTs   // push CURRENT_TIMESTAMP (now, "2006-01-02 15:04:05")
	OpYear        // pop x: push YEAR(x)
	OpMonth       // pop x: push MONTH(x)
	OpDay         // pop x: push DAY(x)
	OpDateDiff    // pop a,b: push whole days (a - b)

	// Array functions. Arrays reach the stack via OpLoadField -> toValue -> kArr.
	OpArrLen       // pop arr:      push ARRAY_LENGTH(arr)
	OpArrContains  // pop arr,v:    push v in arr
	OpArrIntersect // pop arr2,arr1: push (arr1 ∩ arr2 is non-empty)

	// Date arithmetic (function-style DATE_ADD / DATE_SUB). Operands are popped
	// from the stack; the result is an ISO date/datetime string. The 2-arg forms
	// shift by whole days; the 3-arg forms take a trailing unit literal
	// (DAY/WEEK/MONTH/YEAR/HOUR/MINUTE/SECOND, case-insensitive, plural ok).
	OpDateAdd2 // pop n,date:      push date + n days
	OpDateSub2 // pop n,date:      push date - n days
	OpDateAdd3 // pop unit,n,date: push date + n units
	OpDateSub3 // pop unit,n,date: push date - n units

	// Set predicates: ANY/ALL quantifiers and EXISTS sub-queries.
	OpQuantArr  // A=op<<1|all: pop arr,left -> push (left op ANY/ALL of elems)
	OpExistsSub // A=sub idx:   push EXISTS over a collection (Program.Subs[A])
	OpQuantSub  // A=sub idx:   pop left -> push (left op ANY/ALL of projected col)

	// JSON field access: JSON_EXTRACT(doc, '$.path') -> scalar value (or NULL).
	OpJSONField // A=json idx: push JSON scalar from a field doc (Program.JSONs[A])
	OpJSONExpr  // A=json idx: pop doc-string -> push JSON scalar

	// Regular-expression match (RE2). The pattern is pre-compiled at load time.
	OpRegexp // A=regex idx: pop x -> push regex match (Program.Regexps[A])

	// Scalar aggregate sub-query: (SELECT AGG(col) FROM coll [WHERE pred]).
	OpAggSub // A=agg idx: push the aggregate scalar (Program.Aggs[A])

	// Extended builtin call — the qlbridge-parity function library (pkg/sqlfn:
	// CONTAINS / SPLIT / SQRT / TOINT / NOW / EMAIL / HOST / MD5 / ...). The
	// operand packs the builtin ID and the call-site argument count:
	// A = id<<8 | argc (argc <= 255, validated at compile time). Eval pops argc
	// values, calls the builtin, and pushes its result (NULL -> undef; the
	// boolean predicates push a bool).
	OpCallB

	// Row-level and raw-field factors (second qlbridge-parity batch). These
	// need the raw row (not stack values), so each carries a pool index.
	OpMatchPre // A=str idx:  push true when some row field named with the prefix is non-null (qlbridge match)
	OpMapKeys  // A=field idx: push sorted keys of a map/JSON-object field as an array (qlbridge mapkeys)
	OpMapVals  // A=field idx: push values (ordered by sorted key) of a map/JSON-object field (qlbridge mapvalues)

	// JMESPath over a JSON document (qlbridge json.jmespath): like
	// OpJSONField/OpJSONExpr but with a full JMESPath expression, evaluated by
	// pkg/sqlfn (expression pre-compiled and cached at rule load).
	OpJmesField // A=json idx: push JMESPath result from a raw field doc (Program.JSONs[A])
	OpJmesExpr  // A=json idx: pop doc-string -> push JMESPath result

	// Common-SQL JSON functions (round fourteen): JSON_LENGTH / JSON_TYPE /
	// JSON_VALID (JSONOp.Fn selects which) and JSON_CONTAINS. They share
	// JSON_EXTRACT's raw-document model — a field document is read raw (so a
	// pre-parsed object, the kOpaque row shape, works identically to its JSON
	// string form); a computed document is rendered from the stack. Semantics
	// live in pkg/sqlfn (JSONLength / JSONTypeOf / JSONValid /
	// JSONDeepContains), shared verbatim with the AST runtime.
	OpJSONFnField // A=json idx: push JSONs[A].Fn over a raw field doc
	OpJSONFnExpr  // A=json idx: pop doc-string -> push JSONs[A].Fn over it
	OpJSONCntField // A=json idx: pop candidate -> push JSON_CONTAINS(field doc, candidate)
	OpJSONCntExpr  // A=json idx: pop candidate, pop doc-string -> push JSON_CONTAINS
)

// JSONOp.Fn selectors for OpJSONFnField / OpJSONFnExpr.
const (
	jsonFnLen uint8 = iota // JSON_LENGTH
	jsonFnType             // JSON_TYPE
	jsonFnValid            // JSON_VALID (no path)
)

// AggOp describes one scalar aggregate sub-query. Fn is COUNT/SUM/MIN/MAX/AVG;
// Col is "" for COUNT(*); Coll names the collection field on the outer row;
// Where (optional) filters nested rows before aggregation.
type AggOp struct {
	Where *Program
	Fn    string
	Col   string
	Coll  string
}

// JSONOp describes one JSON accessor call. Path is the `$.a.b[0]` accessor
// ("" when the function takes no path); FieldIdx indexes Program.Fields when
// the document is a row field (so the raw value — a JSON string or an
// already-parsed object — is read directly), or -1 when the document is
// produced on the stack; Fn selects the function for OpJSONFnField/Expr.
type JSONOp struct {
	Path     string
	FieldIdx int32
	Fn       uint8
}

// SubProg is a compiled sub-query attached to a Program (referenced by index
// from OpExistsSub / OpQuantSub — the opcode selects which behaviour applies).
// Coll names the collection field on the outer row; Where, when non-nil, is a
// sub-program evaluated against each nested row; Col is the projected column for
// quantifier sub-queries; Op/All carry the comparison used by OpQuantSub.
type SubProg struct {
	Where *Program
	Coll  string
	Col   string
	Op    OpCode
	All   bool
}

// Instr is one instruction: an opcode plus a single operand (an index into one
// of the Program's constant pools, unused for stack-only ops).
type Instr struct {
	Op OpCode
	A  int32
}

// Program is compiled bytecode plus its constant pools.
type Program struct {
	Source  string
	Code    []Instr
	Nums    []float64
	Strs    []string
	Fields  []string
	Sets    []map[string]struct{}
	Subs    []SubProg        // compiled sub-queries referenced by OpExistsSub/OpQuantSub
	JSONs   []JSONOp         // JSON_EXTRACT accessors referenced by OpJSONField/OpJSONExpr
	Regexps []*regexp.Regexp // pre-compiled patterns referenced by OpRegexp
	Aggs    []AggOp          // aggregate sub-queries referenced by OpAggSub

	// ok records that the program passed structural validation (set by
	// Compile, or by an explicit Validate call on a hand-built program,
	// BEFORE the program is shared — so Eval's read is race-free). Eval
	// re-checks unvalidated programs on every call instead of risking an
	// out-of-range pool read, which would panic. See validate.go.
	ok bool
}

// Len reports the number of instructions.
func (p *Program) Len() int { return len(p.Code) }
