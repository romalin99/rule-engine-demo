// validate.go — round sixteen: structural validation of Programs, closing the
// last documented panic vector (security_review §15.3): a HAND-BUILT Program
// (every field is exported) carrying an out-of-range pool index would panic
// inside Eval's unchecked pool reads — `p.Fields[in.A]`, `p.Strs[in.A]`, … —
// and a hand-built sub-program chain deep enough would exhaust the stack
// through Eval's recursion into Where programs.
//
// Design: zero hot-path tax for compiled programs. Compile marks the program
// valid (plain bool, written once BEFORE the program is published — the
// engine hands programs to workers through synchronized stores, so the flag
// is race-free); Eval's prologue is one predictable branch. A program that
// did not come from Compile is checked lazily on every Eval until its owner
// calls Validate() once (which sets the flag on success). All walks here are
// ITERATIVE — validation of hostile input must not be the thing that
// overflows the stack.
package vm

import (
	"fmt"

	"tcg-rulex-engine/pkg/ir"
	"tcg-rulex-engine/pkg/sqlfn"
)

// Validate checks that every instruction operand indexes its constant pool in
// range (including sub-programs, recursively but iteratively), that regexp
// pool entries are non-nil, and that sub-program nesting stays within
// ir.MaxNesting (Eval recurses into Where programs; unbounded hand-built
// chains would exhaust the stack, which no recover can catch). On success the
// program is marked valid and Eval takes its fast path.
//
// Programs produced by Compile are already valid; call this once, before
// sharing the program across goroutines, when constructing Programs by hand.
func (p *Program) Validate() error {
	if err := p.check(); err != nil {
		return err
	}
	p.ok = true
	return nil
}

// check walks the program (and every nested sub-program) iteratively.
func (p *Program) check() error {
	type item struct {
		p     *Program
		depth int
	}
	work := []item{{p, 1}}
	for len(work) > 0 {
		it := work[len(work)-1]
		work = work[:len(work)-1]
		if it.p == nil {
			return fmt.Errorf("vm: nil sub-program")
		}
		if it.depth > ir.MaxNesting {
			return fmt.Errorf("vm: sub-programs nested too deeply (max %d levels)", ir.MaxNesting)
		}
		if err := it.p.checkOne(); err != nil {
			return err
		}
		for i := range it.p.Subs {
			if w := it.p.Subs[i].Where; w != nil {
				work = append(work, item{w, it.depth + 1})
			}
		}
		for i := range it.p.Aggs {
			if w := it.p.Aggs[i].Where; w != nil {
				work = append(work, item{w, it.depth + 1})
			}
		}
	}
	return nil
}

// checkOne validates one program's instruction operands against its pools.
// The opcode→pool mapping mirrors Eval exactly; opcodes without a pool
// operand need no check (Eval's own sp guards already return false instead
// of panicking on stack under/overflow).
func (p *Program) checkOne() error {
	inRange := func(i int, a int32, n int, pool string) error {
		if a < 0 || int(a) >= n {
			return fmt.Errorf("vm: instruction %d: %s index %d out of range (pool size %d)", i, pool, a, n)
		}
		return nil
	}
	jsonOp := func(i int, a int32, needField bool) error {
		if err := inRange(i, a, len(p.JSONs), "JSON op"); err != nil {
			return err
		}
		if needField {
			return inRange(i, p.JSONs[a].FieldIdx, len(p.Fields), "JSON field")
		}
		return nil
	}
	for i, in := range p.Code {
		var err error
		switch in.Op {
		case OpLoadField, OpMapKeys, OpMapVals:
			err = inRange(i, in.A, len(p.Fields), "field")
		case OpConstNum:
			err = inRange(i, in.A, len(p.Nums), "number const")
		case OpConstStr, OpLikePrefix, OpLikeSuffix, OpLikeContains, OpLikeEq, OpMatchPre:
			err = inRange(i, in.A, len(p.Strs), "string const")
		case OpIn:
			err = inRange(i, in.A, len(p.Sets), "set")
		case OpRegexp:
			if err = inRange(i, in.A, len(p.Regexps), "regexp"); err == nil && p.Regexps[in.A] == nil {
				err = fmt.Errorf("vm: instruction %d: nil regexp in pool slot %d", i, in.A)
			}
		case OpExistsSub, OpQuantSub:
			err = inRange(i, in.A, len(p.Subs), "sub-query")
		case OpAggSub:
			err = inRange(i, in.A, len(p.Aggs), "aggregate")
		case OpJSONField, OpJmesField, OpJSONFnField, OpJSONCntField:
			err = jsonOp(i, in.A, true)
		case OpJSONExpr, OpJmesExpr, OpJSONFnExpr, OpJSONCntExpr:
			err = jsonOp(i, in.A, false)
		case OpCallB:
			id, argc := in.A>>8, int(in.A&0xff)
			b := sqlfn.ByID(id)
			switch {
			case b == nil:
				err = fmt.Errorf("vm: instruction %d: unknown builtin id %d", i, id)
			case !b.ArityOK(argc):
				err = fmt.Errorf("vm: instruction %d: %s expects %s argument(s), got %d", i, b.Name, b.ArityDoc(), argc)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}
