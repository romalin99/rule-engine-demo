package vm

import (
	"fmt"
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

	default:
		return fmt.Errorf("vm: unsupported IR node %T", n)
	}
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
