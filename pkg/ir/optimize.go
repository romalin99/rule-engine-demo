package ir

// Optimize rewrites an IR tree into an equivalent, cheaper-to-evaluate form
// before it is lowered to bytecode (Phase 3: IR → Optimizer → Bytecode).
//
// All transforms are strictly semantics-preserving and rely only on the
// associativity / idempotence of boolean AND and OR:
//
//   - flatten:  AND(a, AND(b, c))  → AND(a, b, c)      (same for OR)
//   - collapse: AND(a)             → a                  (single child)
//   - dedup:    AND(a, b, a)       → AND(a, b)          (identical predicates)
//
// Leaf predicates (Compare / Between / In / Like / IsNull) are returned
// unchanged. The function is pure and safe to call on any node.
func Optimize(n Node) Node {
	// Returning the input unchanged is always semantics-preserving, so a tree
	// beyond MaxNesting is simply not optimized — Optimize recurses (and its
	// dedup calls Emit), and externally built IR must not be able to exhaust
	// the stack. Parser-produced trees are bounded at 200 and never hit this.
	// The gate runs ONCE here; the recursion below goes through the ungated
	// optimize (re-gating every level would be quadratic).
	if TooDeep(n) {
		return n
	}
	return optimize(n)
}

func optimize(n Node) Node {
	// Optimize the operand of a NOT, but keep the NOT itself (negation is not
	// distributed — that would not be semantics-preserving for IS NULL / OR).
	if not, ok := n.(Not); ok {
		return Not{Arg: optimize(not.Arg)}
	}
	logic, ok := n.(Logic)
	if !ok {
		return n
	}

	// 1. optimize children, then 2. flatten nested logic of the same operator.
	flat := make([]Node, 0, len(logic.Args))
	for _, a := range logic.Args {
		oa := optimize(a)
		if child, ok := oa.(Logic); ok && child.Op == logic.Op {
			flat = append(flat, child.Args...)
		} else {
			flat = append(flat, oa)
		}
	}

	// 3. drop structurally-identical duplicates (order-preserving).
	seen := make(map[string]struct{}, len(flat))
	uniq := make([]Node, 0, len(flat))
	for _, a := range flat {
		key := emitSQL(a) // internal: depth was gated once at Optimize entry
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		uniq = append(uniq, a)
	}

	if len(uniq) == 1 {
		return uniq[0]
	}
	return Logic{Op: logic.Op, Args: uniq}
}
