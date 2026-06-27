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
	logic, ok := n.(Logic)
	if !ok {
		return n
	}

	// 1. optimize children, then 2. flatten nested logic of the same operator.
	flat := make([]Node, 0, len(logic.Args))
	for _, a := range logic.Args {
		oa := Optimize(a)
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
		key := Emit(a, SQL)
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
