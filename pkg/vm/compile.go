package vm

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"tcg-rulex-engine/pkg/ir"
)

// Compile lowers an IR tree into bytecode. It is called once per rule at load
// time; the resulting *Program is then evaluated many times.
func Compile(n ir.Node) (*Program, error) {
	c := &compiler{
		p:        &Program{},
		fieldIdx: map[string]int32{},
	}
	if err := c.emit(n); err != nil {
		return nil, err
	}
	return c.p, nil
}

// CompileString is a convenience for tests: front-end parse + compile.
func CompileString(rule string) (*Program, error) {
	n, err := ir.Parse(rule)
	if err != nil {
		return nil, err
	}
	return Compile(n)
}

type compiler struct {
	p        *Program
	fieldIdx map[string]int32
}

// substrToEnd is the sentinel length used to lower the 2-argument SUBSTRING(s,
// start) into the 3-argument opcode: it is larger than any realistic string, so
// substr clamps the slice to the remaining runes ("from start to the end").
const substrToEnd = float64(1 << 30)

func (c *compiler) add(op OpCode, a int32) {
	c.p.Code = append(c.p.Code, Instr{Op: op, A: a})
}

func (c *compiler) field(name string) int32 {
	if i, ok := c.fieldIdx[name]; ok {
		return i
	}
	i := int32(len(c.p.Fields))
	c.p.Fields = append(c.p.Fields, name)
	c.fieldIdx[name] = i
	return i
}

func (c *compiler) numConst(f float64) int32 {
	i := int32(len(c.p.Nums))
	c.p.Nums = append(c.p.Nums, f)
	return i
}

func (c *compiler) strConst(s string) int32 {
	i := int32(len(c.p.Strs))
	c.p.Strs = append(c.p.Strs, s)
	return i
}

func (c *compiler) setConst(vals []string) int32 {
	m := make(map[string]struct{}, len(vals))
	for _, v := range vals {
		m[v] = struct{}{}
	}
	i := int32(len(c.p.Sets))
	c.p.Sets = append(c.p.Sets, m)
	return i
}

func (c *compiler) emit(n ir.Node) error {
	switch t := n.(type) {
	case ir.Logic:
		if len(t.Args) == 0 {
			return fmt.Errorf("vm: empty logic node")
		}
		if err := c.emit(t.Args[0]); err != nil {
			return err
		}
		op := OpAnd
		if t.Op == "OR" {
			op = OpOr
		}
		for _, a := range t.Args[1:] {
			if err := c.emit(a); err != nil {
				return err
			}
			c.add(op, 0)
		}
		return nil

	case ir.Compare:
		c.add(OpLoadField, c.field(t.Field))
		if err := c.pushVal(t.Val); err != nil {
			return err
		}
		op, err := cmpOp(t.Op)
		if err != nil {
			return err
		}
		c.add(op, 0)
		return nil

	case ir.Between:
		c.add(OpLoadField, c.field(t.Field))
		lo, err := numOf(t.Lo)
		if err != nil {
			return fmt.Errorf("vm: BETWEEN lo: %w", err)
		}
		hi, err := numOf(t.Hi)
		if err != nil {
			return fmt.Errorf("vm: BETWEEN hi: %w", err)
		}
		c.add(OpConstNum, c.numConst(lo))
		c.add(OpConstNum, c.numConst(hi))
		c.add(OpBetween, 0)
		return nil

	case ir.In:
		c.add(OpLoadField, c.field(t.Field))
		vals := make([]string, len(t.Vals))
		for i, v := range t.Vals {
			vals[i] = valStr(v)
		}
		c.add(OpIn, c.setConst(vals))
		if t.Negate { // NOT IN
			c.add(OpNot, 0)
		}
		return nil

	case ir.Like:
		c.add(OpLoadField, c.field(t.Field))
		kind, core := likeParts(t.Pattern)
		si := c.strConst(core)
		switch kind {
		case "prefix":
			c.add(OpLikePrefix, si)
		case "suffix":
			c.add(OpLikeSuffix, si)
		case "contains":
			c.add(OpLikeContains, si)
		default:
			c.add(OpLikeEq, si)
		}
		if t.Negate { // NOT LIKE
			c.add(OpNot, 0)
		}
		return nil

	case ir.IsNull:
		c.add(OpLoadField, c.field(t.Field))
		if t.Negate {
			c.add(OpIsNotNull, 0)
		} else {
			c.add(OpIsNull, 0)
		}
		return nil

	case ir.Not:
		if err := c.emit(t.Arg); err != nil {
			return err
		}
		c.add(OpNot, 0)
		return nil

	case ir.CompareTerm:
		if err := c.emitTerm(t.Left); err != nil {
			return err
		}
		if err := c.emitTerm(t.Right); err != nil {
			return err
		}
		op, err := cmpOp(t.Op)
		if err != nil {
			return err
		}
		c.add(op, 0)
		return nil

	case ir.LikeTerm:
		if err := c.emitTerm(t.Left); err != nil {
			return err
		}
		kind, core := likeParts(t.Pattern)
		si := c.strConst(core)
		switch kind {
		case "prefix":
			c.add(OpLikePrefix, si)
		case "suffix":
			c.add(OpLikeSuffix, si)
		case "contains":
			c.add(OpLikeContains, si)
		default:
			c.add(OpLikeEq, si)
		}
		if t.Negate {
			c.add(OpNot, 0)
		}
		return nil

	case ir.IsNullTerm:
		if err := c.emitTerm(t.Left); err != nil {
			return err
		}
		if t.Negate {
			c.add(OpIsNotNull, 0)
		} else {
			c.add(OpIsNull, 0)
		}
		return nil

	case ir.PredCall:
		return c.emitPred(t)

	case ir.QuantArr:
		// left op ANY/ALL (arrayField): push left, push the array, then quantify.
		if err := c.emitTerm(t.Left); err != nil {
			return err
		}
		if err := c.emitTerm(t.Array); err != nil {
			return err
		}
		op, err := cmpOp(t.Op)
		if err != nil {
			return err
		}
		a := int32(op) << 1
		if t.All {
			a |= 1
		}
		c.add(OpQuantArr, a)
		return nil

	case ir.Exists:
		sub := SubProg{Coll: t.Coll}
		if t.Where != nil {
			w, err := Compile(t.Where)
			if err != nil {
				return fmt.Errorf("vm: EXISTS sub-query: %w", err)
			}
			sub.Where = w
		}
		c.add(OpExistsSub, c.addSub(sub))
		return nil

	case ir.QuantSub:
		if err := c.emitTerm(t.Left); err != nil {
			return err
		}
		op, err := cmpOp(t.Op)
		if err != nil {
			return err
		}
		sub := SubProg{Coll: t.Coll, Col: t.Col, Op: op, All: t.All}
		if t.Where != nil {
			w, err := Compile(t.Where)
			if err != nil {
				return fmt.Errorf("vm: ANY/ALL sub-query: %w", err)
			}
			sub.Where = w
		}
		c.add(OpQuantSub, c.addSub(sub))
		return nil

	case ir.Regexp:
		if err := c.emitTerm(t.Left); err != nil {
			return err
		}
		idx, err := c.regexp(t.Pattern)
		if err != nil {
			return err
		}
		c.add(OpRegexp, idx)
		if t.Negate { // NOT REGEXP
			c.add(OpNot, 0)
		}
		return nil

	default:
		return fmt.Errorf("vm: unsupported IR node %T", n)
	}
}

// addSub appends a compiled sub-query and returns its index in Program.Subs.
func (c *compiler) addSub(s SubProg) int32 {
	i := int32(len(c.p.Subs))
	c.p.Subs = append(c.p.Subs, s)
	return i
}

// addJSON appends a JSON accessor and returns its index in Program.JSONs.
func (c *compiler) addJSON(j JSONOp) int32 {
	i := int32(len(c.p.JSONs))
	c.p.JSONs = append(c.p.JSONs, j)
	return i
}

// emitAgg lowers a scalar aggregate sub-query, compiling its optional WHERE into
// a sub-program evaluated against each nested row.
func (c *compiler) emitAgg(x ir.AggSub) error {
	a := AggOp{Fn: x.Fn, Col: x.Col, Coll: x.Coll}
	if x.Where != nil {
		w, err := Compile(x.Where)
		if err != nil {
			return fmt.Errorf("vm: aggregate sub-query: %w", err)
		}
		a.Where = w
	}
	i := int32(len(c.p.Aggs))
	c.p.Aggs = append(c.p.Aggs, a)
	c.add(OpAggSub, i)
	return nil
}

// regexp compiles a pattern once at load time and returns its pool index.
func (c *compiler) regexp(pattern string) (int32, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return 0, fmt.Errorf("vm: bad regexp %q: %w", pattern, err)
	}
	i := int32(len(c.p.Regexps))
	c.p.Regexps = append(c.p.Regexps, re)
	return i, nil
}

// emitJSON lowers JSON_EXTRACT / JSON_VALUE(doc, '$.path'). The path must be a
// string literal. When the document is a field, the raw row value (a JSON string
// or an already-parsed object) is read directly via OpJSONField; otherwise the
// document expression is evaluated to a string and consumed by OpJSONExpr.
func (c *compiler) emitJSON(x ir.CallTerm) error {
	if len(x.Args) != 2 {
		return fmt.Errorf("vm: %s expects 2 arguments, got %d", x.Fn, len(x.Args))
	}
	lit, ok := x.Args[1].(ir.LitTerm)
	if !ok || !lit.Val.IsString {
		return fmt.Errorf("vm: %s path must be a string literal", x.Fn)
	}
	if ft, ok := x.Args[0].(ir.FieldTerm); ok {
		c.add(OpJSONField, c.addJSON(JSONOp{Path: lit.Val.Str, FieldIdx: c.field(ft.Name)}))
		return nil
	}
	if err := c.emitTerm(x.Args[0]); err != nil {
		return err
	}
	c.add(OpJSONExpr, c.addJSON(JSONOp{Path: lit.Val.Str, FieldIdx: -1}))
	return nil
}

// emitPred lowers a boolean-valued function call (ARRAY_CONTAINS / ARRAY_INTERSECT).
func (c *compiler) emitPred(t ir.PredCall) error {
	switch t.Fn {
	case "ARRAY_CONTAINS":
		if len(t.Args) != 2 {
			return fmt.Errorf("vm: ARRAY_CONTAINS expects 2 arguments, got %d", len(t.Args))
		}
		if err := c.emitTerm(t.Args[0]); err != nil {
			return err
		}
		if err := c.emitTerm(t.Args[1]); err != nil {
			return err
		}
		c.add(OpArrContains, 0)
		return nil
	case "ARRAY_INTERSECT":
		// ARRAY_INTERSECT(a, b): true when two array fields share at least one
		// element (set intersection is non-empty). Both operands are arrays.
		if len(t.Args) != 2 {
			return fmt.Errorf("vm: ARRAY_INTERSECT expects 2 arguments, got %d", len(t.Args))
		}
		if err := c.emitTerm(t.Args[0]); err != nil {
			return err
		}
		if err := c.emitTerm(t.Args[1]); err != nil {
			return err
		}
		c.add(OpArrIntersect, 0)
		return nil
	default:
		return fmt.Errorf("vm: unsupported predicate function %q", t.Fn)
	}
}

// emitTerm lowers a scalar operand (field, literal, or function call) so it
// leaves exactly one value on the VM stack.
func (c *compiler) emitTerm(t ir.Term) error {
	switch x := t.(type) {
	case ir.FieldTerm:
		c.add(OpLoadField, c.field(x.Name))
		return nil
	case ir.LitTerm:
		return c.pushVal(x.Val)
	case ir.AggSub:
		return c.emitAgg(x)
	case ir.CallTerm:
		// JSON_EXTRACT needs the raw document (string or parsed object), so it is
		// lowered specially rather than through the generic value-stack path.
		if x.Fn == "JSON_EXTRACT" || x.Fn == "JSON_VALUE" {
			return c.emitJSON(x)
		}
		// SUBSTRING(s, start) (2-arg) means "from start to the end of the string".
		// Lower it to the 3-arg opcode with a sentinel length that substr clamps
		// to the remaining runes, so no dedicated 2-arg opcode is needed.
		if (x.Fn == "SUBSTRING" || x.Fn == "SUBSTR") && len(x.Args) == 2 {
			if err := c.emitTerm(x.Args[0]); err != nil {
				return err
			}
			if err := c.emitTerm(x.Args[1]); err != nil {
				return err
			}
			c.add(OpConstNum, c.numConst(substrToEnd))
			c.add(OpSubstr, 0)
			return nil
		}
		for _, a := range x.Args {
			if err := c.emitTerm(a); err != nil {
				return err
			}
		}
		op, err := fnOp(x.Fn, len(x.Args))
		if err != nil {
			return err
		}
		c.add(op, 0)
		return nil
	default:
		return fmt.Errorf("vm: unsupported term %T", t)
	}
}

// fnOp maps a SQL function name + argument count to its opcode. DATE_ADD /
// DATE_SUB are variadic (2 args = days, 3 args = value + unit), so they are
// resolved by arity here before the fixed-arity table.
func fnOp(fn string, argc int) (OpCode, error) {
	switch fn {
	case "DATE_ADD":
		switch argc {
		case 2:
			return OpDateAdd2, nil
		case 3:
			return OpDateAdd3, nil
		}
		return 0, fmt.Errorf("vm: DATE_ADD expects 2 or 3 arguments, got %d", argc)
	case "DATE_SUB":
		switch argc {
		case 2:
			return OpDateSub2, nil
		case 3:
			return OpDateSub3, nil
		}
		return 0, fmt.Errorf("vm: DATE_SUB expects 2 or 3 arguments, got %d", argc)
	case "ROUND":
		switch argc {
		case 1:
			return OpRound, nil // ROUND(x) -> nearest integer
		case 2:
			return OpRound2, nil // ROUND(x, d) -> round to d decimal places
		}
		return 0, fmt.Errorf("vm: ROUND expects 1 or 2 arguments, got %d", argc)
	}
	op, want, ok := lookupFn(fn)
	if !ok {
		return 0, fmt.Errorf("vm: unsupported function %q", fn)
	}
	if argc != want {
		return 0, fmt.Errorf("vm: %s expects %d argument(s), got %d", fn, want, argc)
	}
	return op, nil
}

// lookupFn returns the opcode and expected argument count for a function name.
func lookupFn(fn string) (op OpCode, argc int, ok bool) {
	switch fn {
	case "UPPER":
		return OpUpper, 1, true
	case "LOWER":
		return OpLower, 1, true
	case "TRIM":
		return OpTrim, 1, true
	case "LENGTH", "LEN":
		return OpLength, 1, true
	case "ABS":
		return OpAbs, 1, true
	case "CEIL", "CEILING":
		return OpCeil, 1, true
	case "FLOOR":
		return OpFloor, 1, true
	case "SUBSTRING", "SUBSTR":
		return OpSubstr, 3, true
	case "CURRENT_DATE":
		return OpCurrentDate, 0, true
	case "CURRENT_TIMESTAMP":
		return OpCurrentTs, 0, true
	case "YEAR":
		return OpYear, 1, true
	case "MONTH":
		return OpMonth, 1, true
	case "DAY":
		return OpDay, 1, true
	case "DATEDIFF":
		return OpDateDiff, 2, true
	case "ARRAY_LENGTH":
		return OpArrLen, 1, true
	}
	return 0, 0, false
}

// pushVal emits a constant push for an IR value (string or number).
func (c *compiler) pushVal(v ir.Value) error {
	if v.IsString {
		c.add(OpConstStr, c.strConst(v.Str))
		return nil
	}
	f, err := strconv.ParseFloat(v.Num, 64)
	if err != nil {
		return fmt.Errorf("vm: bad number %q: %w", v.Num, err)
	}
	c.add(OpConstNum, c.numConst(f))
	return nil
}

func numOf(v ir.Value) (float64, error) {
	if v.IsString {
		return 0, fmt.Errorf("expected number, got string %q", v.Str)
	}
	return strconv.ParseFloat(v.Num, 64)
}

func valStr(v ir.Value) string {
	if v.IsString {
		return v.Str
	}
	return v.Num
}

func cmpOp(op string) (OpCode, error) {
	switch op {
	case "=", "==":
		return OpEq, nil
	case "!=":
		return OpNe, nil
	case ">":
		return OpGt, nil
	case ">=":
		return OpGe, nil
	case "<":
		return OpLt, nil
	case "<=":
		return OpLe, nil
	}
	return 0, fmt.Errorf("vm: unsupported operator %q", op)
}

// likeParts classifies a LIKE pattern by '%' placement and returns the core text.
func likeParts(pattern string) (kind, core string) {
	pre := strings.HasPrefix(pattern, "%")
	suf := strings.HasSuffix(pattern, "%")
	core = strings.Trim(pattern, "%")
	switch {
	case pre && suf:
		return "contains", core
	case suf:
		return "prefix", core // "abc%" -> hasPrefix abc
	case pre:
		return "suffix", core // "%abc" -> hasSuffix abc
	default:
		return "equals", core
	}
}
