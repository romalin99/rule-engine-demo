package ir

import (
	"strings"

	"tcg-rulex-engine/pkg/sqlfn"
)

// DSL identifies a target output language.
type DSL string

const (
	SQL      DSL = "sql"
	Aviator  DSL = "aviator"
	CEL      DSL = "cel"
	Expr     DSL = "expr"
	JSONRule DSL = "json" // structured JSON predicate document (see EmitJSON)
)

// AllDSLs is the set of supported targets (SQL / Aviator / CEL / Expr are infix
// expression dialects; JSONRule is the structured-document target handled by
// EmitJSON — see Convert).
var AllDSLs = []DSL{SQL, Aviator, CEL, Expr, JSONRule}

// Emit renders an IR node into the given target DSL. For the infix dialects the
// result is always a string; for JSONRule it delegates to EmitJSON and drops
// the error (unrepresentable nodes yield ""), so callers that need to surface
// that error should use EmitJSON or Convert instead.
//
// A tree beyond MaxNesting yields "" like any other unrepresentable input:
// the emitters recurse, and externally built IR must not be able to exhaust
// the stack (parser-produced trees are bounded at 200 and never hit this).
func Emit(n Node, d DSL) string {
	if TooDeep(n) {
		return ""
	}
	switch d {
	case SQL:
		return emitSQL(n)
	case JSONRule:
		s, _ := EmitJSON(n)
		return s
	default:
		return emitCode(n, d)
	}
}

// Convert parses a rule expression and emits it in the target DSL in one step.
// Unlike Emit it surfaces the JSONRule emitter's error (some IR constructs have
// no JSON-rule form — see EmitJSON).
func Convert(rule string, d DSL) (string, error) {
	n, err := Parse(rule)
	if err != nil {
		return "", err
	}
	if d == JSONRule {
		return EmitJSON(n)
	}
	return Emit(n, d), nil
}

// ---- SQL ------------------------------------------------------------------

func emitSQL(n Node) string {
	switch t := n.(type) {
	case Logic:
		op := " " + t.Op + " "
		parts := make([]string, len(t.Args))
		for i, a := range t.Args {
			parts[i] = wrapSQL(a)
		}
		return strings.Join(parts, op)
	case Compare:
		return t.Field + " " + t.Op + " " + sqlVal(t.Val)
	case Between:
		return t.Field + " BETWEEN " + sqlVal(t.Lo) + " AND " + sqlVal(t.Hi)
	case In:
		if t.Negate {
			return t.Field + " NOT IN (" + sqlValList(t.Vals) + ")"
		}
		return t.Field + " IN (" + sqlValList(t.Vals) + ")"
	case Like:
		pat := sqlStr(likeEmitPattern(t.Pattern, t.Wildcards))
		if t.Negate {
			return t.Field + " NOT LIKE " + pat
		}
		return t.Field + " LIKE " + pat
	case IsNull:
		if t.Negate {
			return t.Field + " IS NOT NULL"
		}
		return t.Field + " IS NULL"
	case Not:
		return "NOT (" + emitSQL(t.Arg) + ")"
	case CompareTerm:
		return emitTermSQL(t.Left) + " " + t.Op + " " + emitTermSQL(t.Right)
	case LikeTerm:
		pat := sqlStr(likeEmitPattern(t.Pattern, t.Wildcards))
		if t.Negate {
			return emitTermSQL(t.Left) + " NOT LIKE " + pat
		}
		return emitTermSQL(t.Left) + " LIKE " + pat
	case IsNullTerm:
		if t.Negate {
			return emitTermSQL(t.Left) + " IS NOT NULL"
		}
		return emitTermSQL(t.Left) + " IS NULL"
	case PredCall:
		parts := make([]string, len(t.Args))
		for i, a := range t.Args {
			parts[i] = emitTermSQL(a)
		}
		return t.Fn + "(" + strings.Join(parts, ", ") + ")"
	case Exists:
		if t.Where != nil {
			return "EXISTS (SELECT 1 FROM " + t.Coll + " WHERE " + emitSQL(t.Where) + ")"
		}
		return "EXISTS (" + t.Coll + ")"
	case QuantArr:
		return emitTermSQL(t.Left) + " " + t.Op + " " + quantKind(t.All) + " (" + emitTermSQL(t.Array) + ")"
	case QuantSub:
		sub := "SELECT " + t.Col + " FROM " + t.Coll
		if t.Where != nil {
			sub += " WHERE " + emitSQL(t.Where)
		}
		return emitTermSQL(t.Left) + " " + t.Op + " " + quantKind(t.All) + " (" + sub + ")"
	case Regexp:
		if t.Negate {
			return emitTermSQL(t.Left) + " NOT REGEXP " + sqlStr(t.Pattern)
		}
		return emitTermSQL(t.Left) + " REGEXP " + sqlStr(t.Pattern)
	}
	return ""
}

// quantKind renders the ANY/ALL keyword for a quantifier node.
func quantKind(all bool) string {
	if all {
		return "ALL"
	}
	return "ANY"
}

// emitTermSQL renders a scalar operand (field, literal, or function call) as SQL.
func emitTermSQL(t Term) string {
	switch x := t.(type) {
	case FieldTerm:
		return x.Name
	case LitTerm:
		return sqlVal(x.Val)
	case CallTerm:
		parts := make([]string, len(x.Args))
		for i, a := range x.Args {
			parts[i] = emitTermSQL(a)
		}
		return x.Fn + "(" + strings.Join(parts, ", ") + ")"
	case AggSub:
		arg := x.Col
		if arg == "" {
			arg = "*"
		}
		sub := "SELECT " + x.Fn + "(" + arg + ") FROM " + x.Coll
		if x.Where != nil {
			sub += " WHERE " + emitSQL(x.Where)
		}
		return "(" + sub + ")"
	}
	return ""
}

func wrapSQL(n Node) string {
	if _, ok := n.(Logic); ok {
		return "(" + emitSQL(n) + ")"
	}
	return emitSQL(n)
}

// sqlStr renders a string value as a single-quoted SQL literal, escaping
// backslashes and single quotes so the output re-parses to the same value
// (the lexer unescapes exactly \' \" \\). Without this, a value containing a
// quote (O'Brien) or a trailing backslash would break the emit→parse round
// trip that the decision-table path (row → IR → SQL → reparse) relies on.
func sqlStr(s string) string {
	if !strings.ContainsAny(s, `\'`) {
		return "'" + s + "'"
	}
	var sb strings.Builder
	sb.Grow(len(s) + 2)
	sb.WriteByte('\'')
	for _, r := range s {
		if r == '\\' || r == '\'' {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	sb.WriteByte('\'')
	return sb.String()
}

func sqlVal(v Value) string {
	if v.IsString {
		return sqlStr(v.Str)
	}
	return v.Num
}

func sqlValList(vs []Value) string {
	parts := make([]string, len(vs))
	for i, v := range vs {
		parts[i] = sqlVal(v)
	}
	return strings.Join(parts, ", ")
}

// ---- Aviator / CEL / Expr -------------------------------------------------

func emitCode(n Node, d DSL) string {
	switch t := n.(type) {
	case Logic:
		sep := " && "
		if t.Op == "OR" {
			sep = " || "
		}
		parts := make([]string, len(t.Args))
		for i, a := range t.Args {
			parts[i] = wrapCode(a, d)
		}
		return strings.Join(parts, sep)
	case Compare:
		return t.Field + " " + mapOp(t.Op) + " " + codeVal(t.Val, d)
	case Between:
		return "(" + t.Field + " >= " + codeVal(t.Lo, d) + " && " +
			t.Field + " <= " + codeVal(t.Hi, d) + ")"
	case In:
		return emitIn(t, d)
	case Like:
		return emitLike(t, d)
	case IsNull:
		return emitIsNull(t, d)
	case Not:
		return "!(" + emitCode(t.Arg, d) + ")"
	}
	return ""
}

// emitIsNull renders IS [NOT] NULL for code DSLs.
func emitIsNull(t IsNull, d DSL) string {
	switch d {
	case CEL:
		if t.Negate {
			return "has(" + t.Field + ")"
		}
		return "!has(" + t.Field + ")"
	default: // Aviator / Expr
		if t.Negate {
			return t.Field + " != nil"
		}
		return t.Field + " == nil"
	}
}

func wrapCode(n Node, d DSL) string {
	if _, ok := n.(Logic); ok {
		return "(" + emitCode(n, d) + ")"
	}
	return emitCode(n, d)
}

// mapOp converts SQL '=' to the code-style '=='; other operators are unchanged.
func mapOp(op string) string {
	if op == "=" {
		return "=="
	}
	return op
}

func codeVal(v Value, d DSL) string {
	if v.IsString {
		return quote(v.Str, d)
	}
	return v.Num
}

func emitIn(t In, d DSL) string {
	var pos string
	switch d {
	case CEL, Expr:
		parts := make([]string, len(t.Vals))
		for i, v := range t.Vals {
			parts[i] = codeVal(v, d)
		}
		pos = t.Field + " in [" + strings.Join(parts, ", ") + "]"
	default: // Aviator: expand to OR of equals
		parts := make([]string, len(t.Vals))
		for i, v := range t.Vals {
			parts[i] = t.Field + " == " + codeVal(v, d)
		}
		pos = "(" + strings.Join(parts, " || ") + ")"
	}
	if t.Negate { // NOT IN -> logical-not of the membership test
		return "!(" + pos + ")"
	}
	return pos
}

func emitLike(t Like, d DSL) string {
	pos := emitLikePositive(t, d)
	if pos == "" { // untranslatable wildcard pattern: like other unsupported nodes
		return ""
	}
	if t.Negate { // NOT LIKE -> logical-not of the match
		return "!(" + pos + ")"
	}
	return pos
}

func emitLikePositive(t Like, d DSL) string {
	// LikeShape resolves wildcard-grammar escapes into the literal core (so
	// a CEL-built `50\%%` emits back as startsWith('50%')) and reports
	// untranslatable patterns ('_' / interior '%'), which have no
	// startsWith/endsWith/contains equivalent in these DSLs.
	kind, core, ok := sqlfn.LikeShape(t.Pattern, t.Wildcards)
	if !ok {
		return ""
	}
	q := quote(core, d)
	switch d {
	case CEL:
		switch kind {
		case "prefix":
			return t.Field + ".startsWith(" + q + ")"
		case "suffix":
			return t.Field + ".endsWith(" + q + ")"
		case "contains":
			return t.Field + ".contains(" + q + ")"
		default:
			return t.Field + " == " + q
		}
	case Expr:
		switch kind {
		case "prefix":
			return "hasPrefix(" + t.Field + ", " + q + ")"
		case "suffix":
			return "hasSuffix(" + t.Field + ", " + q + ")"
		case "contains":
			return t.Field + " contains " + q
		default:
			return t.Field + " == " + q
		}
	default: // Aviator
		switch kind {
		case "prefix":
			return "string.startsWith(" + t.Field + ", " + q + ")"
		case "suffix":
			return "string.endsWith(" + t.Field + ", " + q + ")"
		case "contains":
			return "string.contains(" + t.Field + ", " + q + ")"
		default:
			return t.Field + " == " + q
		}
	}
}

// likeEmitPattern renders a Like node's pattern for SQL/JSON emission. The SQL
// front-ends re-parse emitted rules under the full wildcard grammar
// (Wildcards=true), so a legacy node (Wildcards=false, four literal shapes)
// must have its pattern re-encoded — escaping '_' / '\' / interior '%' — to
// keep its exact meaning across the emit → parse round trip. Wildcard nodes
// emit verbatim. (The historical likeParts classifier lives on as
// sqlfn.LikeShape, shared by both emit directions and both runtimes.)
func likeEmitPattern(pattern string, wildcards bool) string {
	if wildcards {
		return pattern
	}
	return sqlfn.LikeLegacyWildcard(pattern)
}

// quote wraps a string in the dialect's quote character, escaping backslashes
// and any occurrence of that character inside the value. Backslashes must be
// escaped FIRST — CEL/Expr/Aviator string literals all treat '\' as an escape
// introducer, so emitting a value like `a\b` verbatim would parse as the
// (different, or invalid) escape sequence `\b` on the target engine.
func quote(s string, d DSL) string {
	q := "'"
	if d == Expr {
		q = "\""
	}
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return q + strings.ReplaceAll(s, q, "\\"+q) + q
}
