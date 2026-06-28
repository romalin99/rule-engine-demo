package vm

import "strings"

// stackMax bounds the evaluation stack depth. Boolean rule expressions are
// shallow once compiled to postfix, so this is generous; on overflow Eval
// returns false rather than panicking.
const stackMax = 256

// Eval runs the program against a row and returns its boolean result.
//
// The stack is a fixed-size local array, so Eval allocates nothing and is safe
// for concurrent use from many goroutines (each call has its own stack). This is
// what lets the worker pool share one *Program across all workers.
func (p *Program) Eval(row map[string]any) bool {
	var st [stackMax]Value
	sp := 0

	for i := 0; i < len(p.Code); i++ {
		in := p.Code[i]
		switch in.Op {

		case OpLoadField, OpConstNum, OpConstStr: // push one value
			if sp >= stackMax {
				return false
			}
			st[sp] = p.load(in, row)
			sp++

		case OpBetween: // pop hi,lo,x -> push lo <= x <= hi
			if sp < 3 {
				return false
			}
			x, lo, hi := st[sp-3], st[sp-2], st[sp-1]
			sp -= 2
			st[sp-1] = boolV(x.k == kNum && lo.k == kNum && hi.k == kNum && lo.n <= x.n && x.n <= hi.n)

		case OpEq, OpNe, OpGt, OpGe, OpLt, OpLe, OpAnd, OpOr: // pop b,a -> push result
			if sp < 2 {
				return false
			}
			a, b := st[sp-2], st[sp-1]
			sp--
			st[sp-1] = boolV(binaryOp(in.Op, a, b))

		case OpIn, OpLikePrefix, OpLikeSuffix, OpLikeContains, OpLikeEq, OpIsNull, OpIsNotNull, OpNot: // pop x -> push result
			if sp < 1 {
				return false
			}
			st[sp-1] = boolV(p.unaryOp(in, st[sp-1]))

		default:
			return false
		}
	}

	if sp != 1 || st[0].k != kBool {
		return false
	}
	return st[0].b
}

// load produces the value pushed by a load/const opcode (OpLoadField,
// OpConstNum, OpConstStr). A missing field yields undef.
func (p *Program) load(in Instr, row map[string]any) Value {
	switch in.Op {
	case OpConstNum:
		return numV(p.Nums[in.A])
	case OpConstStr:
		return strV(p.Strs[in.A])
	default: // OpLoadField
		if raw, ok := row[p.Fields[in.A]]; ok {
			return toValue(raw)
		}
		return undef
	}
}

// binaryOp evaluates a two-operand opcode: the boolean combinators AND/OR, with
// every comparison delegated to compare.
func binaryOp(op OpCode, a, b Value) bool {
	switch op {
	case OpAnd:
		return a.b && b.b
	case OpOr:
		return a.b || b.b
	default:
		return compare(a, b, op)
	}
}

// unaryOp evaluates a single-operand opcode (set membership, the LIKE variants,
// the IS [NOT] NULL pair, and NOT) against the top-of-stack value x.
func (p *Program) unaryOp(in Instr, x Value) bool {
	switch in.Op {
	case OpIn:
		_, ok := p.Sets[in.A][x.asString()]
		return ok && x.k != kUndef
	case OpLikePrefix:
		return x.k != kUndef && strings.HasPrefix(x.asString(), p.Strs[in.A])
	case OpLikeSuffix:
		return x.k != kUndef && strings.HasSuffix(x.asString(), p.Strs[in.A])
	case OpLikeContains:
		return x.k != kUndef && strings.Contains(x.asString(), p.Strs[in.A])
	case OpLikeEq:
		return x.k != kUndef && x.asString() == p.Strs[in.A]
	case OpIsNull:
		return x.k == kUndef
	case OpIsNotNull:
		return x.k != kUndef
	case OpNot:
		// Negate a boolean result; anything non-boolean is treated as false
		// (so !non-bool stays false rather than silently becoming true).
		return x.k == kBool && !x.b
	default:
		return false
	}
}

// compare evaluates an ordering/equality opcode on two values.
func compare(a, b Value, op OpCode) bool {
	if a.k == kUndef || b.k == kUndef {
		return false
	}
	switch op {
	case OpEq:
		return eq(a, b)
	case OpNe:
		return !eq(a, b)
	}
	// Ordering: prefer numeric comparison; fall back to lexical.
	if a.k == kNum && b.k == kNum {
		switch op {
		case OpGt:
			return a.n > b.n
		case OpGe:
			return a.n >= b.n
		case OpLt:
			return a.n < b.n
		case OpLe:
			return a.n <= b.n
		}
	}
	as, bs := a.asString(), b.asString()
	switch op {
	case OpGt:
		return as > bs
	case OpGe:
		return as >= bs
	case OpLt:
		return as < bs
	case OpLe:
		return as <= bs
	}
	return false
}
