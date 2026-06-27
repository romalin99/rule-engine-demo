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

		case OpLoadField:
			if sp >= stackMax {
				return false
			}
			if raw, ok := row[p.Fields[in.A]]; ok {
				st[sp] = toValue(raw)
			} else {
				st[sp] = undef
			}
			sp++

		case OpConstNum:
			if sp >= stackMax {
				return false
			}
			st[sp] = numV(p.Nums[in.A])
			sp++

		case OpConstStr:
			if sp >= stackMax {
				return false
			}
			st[sp] = strV(p.Strs[in.A])
			sp++

		case OpEq, OpNe, OpGt, OpGe, OpLt, OpLe:
			if sp < 2 {
				return false
			}
			b := st[sp-1]
			a := st[sp-2]
			sp -= 2
			st[sp] = boolV(compare(a, b, in.Op))
			sp++

		case OpBetween:
			if sp < 3 {
				return false
			}
			hi := st[sp-1]
			lo := st[sp-2]
			x := st[sp-3]
			sp -= 3
			res := x.k == kNum && lo.k == kNum && hi.k == kNum && lo.n <= x.n && x.n <= hi.n
			st[sp] = boolV(res)
			sp++

		case OpIn:
			if sp < 1 {
				return false
			}
			x := st[sp-1]
			sp--
			_, ok := p.Sets[in.A][x.asString()]
			st[sp] = boolV(ok && x.k != kUndef)
			sp++

		case OpLikePrefix:
			if sp < 1 {
				return false
			}
			x := st[sp-1]
			sp--
			st[sp] = boolV(x.k != kUndef && strings.HasPrefix(x.asString(), p.Strs[in.A]))
			sp++

		case OpLikeSuffix:
			if sp < 1 {
				return false
			}
			x := st[sp-1]
			sp--
			st[sp] = boolV(x.k != kUndef && strings.HasSuffix(x.asString(), p.Strs[in.A]))
			sp++

		case OpLikeContains:
			if sp < 1 {
				return false
			}
			x := st[sp-1]
			sp--
			st[sp] = boolV(x.k != kUndef && strings.Contains(x.asString(), p.Strs[in.A]))
			sp++

		case OpLikeEq:
			if sp < 1 {
				return false
			}
			x := st[sp-1]
			sp--
			st[sp] = boolV(x.k != kUndef && x.asString() == p.Strs[in.A])
			sp++

		case OpIsNull:
			if sp < 1 {
				return false
			}
			x := st[sp-1]
			sp--
			st[sp] = boolV(x.k == kUndef)
			sp++

		case OpIsNotNull:
			if sp < 1 {
				return false
			}
			x := st[sp-1]
			sp--
			st[sp] = boolV(x.k != kUndef)
			sp++

		case OpAnd:
			if sp < 2 {
				return false
			}
			b := st[sp-1]
			a := st[sp-2]
			sp -= 2
			st[sp] = boolV(a.b && b.b)
			sp++

		case OpOr:
			if sp < 2 {
				return false
			}
			b := st[sp-1]
			a := st[sp-2]
			sp -= 2
			st[sp] = boolV(a.b || b.b)
			sp++

		default:
			return false
		}
	}

	if sp != 1 || st[0].k != kBool {
		return false
	}
	return st[0].b
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
