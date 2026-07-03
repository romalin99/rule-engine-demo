// depth.go — round sixteen: an ITERATIVE nesting-depth measure for IR trees.
//
// Stack exhaustion is the one panic Go cannot recover, and every recursive
// consumer of externally constructed IR (the AST runtime's walk, Emit,
// Optimize, the bytecode compiler) is exposed to it. The parser bounds TEXT
// rules at 200 levels, but library users can build ir.Node values directly —
// so each public consumer gates on NestingDepth first. The measure itself is
// a worklist traversal (no recursion), safe on ANY input depth.
package ir

// MaxNesting bounds IR nesting for every consumer of externally constructed
// IR: the bytecode compiler counts levels while lowering, and the AST
// runtime, Emit, EmitJSON and Optimize gate on NestingDepth up front. Text
// rules are parser-bounded at 200 levels and stay far inside it.
const MaxNesting = 500

// NestingDepth reports the maximum nesting depth of an IR tree, counting
// every Node and Term level along a path (a bare leaf is depth 1). The
// traversal is iterative, so it is safe to call on hostile, arbitrarily deep
// trees — that is the point: consumers check the depth BEFORE recursing.
func NestingDepth(n Node) int {
	if n == nil {
		return 0
	}
	type frame struct {
		v any // Node or Term
		d int
	}
	stack := []frame{{n, 1}}
	max := 0
	push := func(s []frame, d int, vs ...any) []frame {
		for _, v := range vs {
			if v != nil {
				s = append(s, frame{v, d})
			}
		}
		return s
	}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if f.d > max {
			max = f.d
		}
		d := f.d + 1
		switch t := f.v.(type) {
		case Logic:
			for _, a := range t.Args {
				stack = push(stack, d, a)
			}
		case Not:
			stack = push(stack, d, t.Arg)
		case CompareTerm:
			stack = push(stack, d, t.Left, t.Right)
		case LikeTerm:
			stack = push(stack, d, t.Left)
		case IsNullTerm:
			stack = push(stack, d, t.Left)
		case PredCall:
			for _, a := range t.Args {
				stack = push(stack, d, a)
			}
		case QuantArr:
			stack = push(stack, d, t.Left, t.Array)
		case Exists:
			if t.Where != nil {
				stack = push(stack, d, t.Where)
			}
		case QuantSub:
			stack = push(stack, d, t.Left)
			if t.Where != nil {
				stack = push(stack, d, t.Where)
			}
		case Regexp:
			stack = push(stack, d, t.Left)
		case CallTerm:
			for _, a := range t.Args {
				stack = push(stack, d, a)
			}
		case AggSub:
			if t.Where != nil {
				stack = push(stack, d, t.Where)
			}
		}
		// Compare / Between / In / Like / IsNull / FieldTerm / LitTerm: leaves.
	}
	return max
}

// TooDeep reports whether n exceeds MaxNesting — the shared gate used by the
// AST runtime, the emitters and the optimizer before their recursive walks.
func TooDeep(n Node) bool { return NestingDepth(n) > MaxNesting }
