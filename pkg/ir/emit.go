package ir

import "strings"

// DSL identifies a target output language.
type DSL string

const (
	SQL     DSL = "sql"
	Aviator DSL = "aviator"
	CEL     DSL = "cel"
	Expr    DSL = "expr"
)

// AllDSLs is the set of supported targets.
var AllDSLs = []DSL{SQL, Aviator, CEL, Expr}

// Emit renders an IR node into the given target DSL.
func Emit(n Node, d DSL) string {
	if d == SQL {
		return emitSQL(n)
	}
	return emitCode(n, d)
}

// Convert parses a rule expression and emits it in the target DSL in one step.
func Convert(rule string, d DSL) (string, error) {
	n, err := Parse(rule)
	if err != nil {
		return "", err
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
		if t.Negate {
			return t.Field + " NOT LIKE '" + t.Pattern + "'"
		}
		return t.Field + " LIKE '" + t.Pattern + "'"
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
		if t.Negate {
			return emitTermSQL(t.Left) + " NOT LIKE '" + t.Pattern + "'"
		}
		return emitTermSQL(t.Left) + " LIKE '" + t.Pattern + "'"
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
			return emitTermSQL(t.Left) + " NOT REGEXP '" + t.Pattern + "'"
		}
		return emitTermSQL(t.Left) + " REGEXP '" + t.Pattern + "'"
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

func sqlVal(v Value) string {
	if v.IsString {
		return "'" + v.Str + "'"
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
	if t.Negate { // NOT LIKE -> logical-not of the match
		return "!(" + pos + ")"
	}
	return pos
}

func emitLikePositive(t Like, d DSL) string {
	kind, core := likeParts(t.Pattern)
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

// likeParts classifies a LIKE pattern by % placement and returns the core text.
func likeParts(pattern string) (kind, core string) {
	pre := strings.HasPrefix(pattern, "%")
	suf := strings.HasSuffix(pattern, "%")
	core = strings.Trim(pattern, "%")
	switch {
	case pre && suf:
		return "contains", core
	case suf:
		return "prefix", core // "abc%"  -> startsWith abc
	case pre:
		return "suffix", core // "%abc"  -> endsWith abc
	default:
		return "equals", core
	}
}

// quote wraps a string in the dialect's quote character, escaping any occurrence
// of that character inside the value.
func quote(s string, d DSL) string {
	q := "'"
	if d == Expr {
		q = "\""
	}
	return q + strings.ReplaceAll(s, q, "\\"+q) + q
}
