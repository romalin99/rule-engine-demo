package sqlfn

// registerArrays adds positional array access (qlbridge: array.index,
// array.slice — spelled ARRAY_INDEX / ARRAY_SLICE here). Membership,
// intersection and length live in the core opcodes (ARRAY_CONTAINS /
// ARRAY_INTERSECT / ARRAY_LENGTH).
func registerArrays() {
	// ARRAY_INDEX(arr, i): the i-th element (0-based, qlbridge numbering);
	// out-of-range yields NULL. Elements are text, like every array value.
	register([]string{"ARRAY_INDEX"}, 2, 2, false, func(a []any) any {
		xs, ok1 := arr(a[0])
		f, ok2 := num(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		i := int(f)
		if i < 0 || i >= len(xs) {
			return nil
		}
		return xs[i]
	})
	// ARRAY_SLICE(arr, start[, end]): the half-open sub-array [start, end)
	// (0-based; end defaults to the array length). Negative indexes count from
	// the end; bounds are clamped, and an inverted range yields an empty array.
	register([]string{"ARRAY_SLICE"}, 2, 3, false, func(a []any) any {
		xs, ok1 := arr(a[0])
		sf, ok2 := num(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		start := normIndex(int(sf), len(xs))
		end := len(xs)
		if len(a) == 3 {
			ef, ok := num(a[2])
			if !ok {
				return nil
			}
			end = normIndex(int(ef), len(xs))
		}
		if start > end {
			return []string{}
		}
		out := make([]string, end-start)
		copy(out, xs[start:end])
		return out
	})
}

// normIndex resolves a possibly-negative slice index against a length and
// clamps it into [0, n].
func normIndex(i, n int) int {
	if i < 0 {
		i += n
	}
	if i < 0 {
		return 0
	}
	if i > n {
		return n
	}
	return i
}
