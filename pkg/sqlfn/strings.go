package sqlfn

import (
	"strings"
	"unicode/utf8"
)

// registerStrings adds the string transforms and string predicates
// (qlbridge: tolower, strip, replace, split, join, contains, hasprefix,
// hassuffix; plus the common SQL spellings STARTSWITH / ENDSWITH / CONCAT /
// CHAR_LENGTH and TOUPPER for symmetry).
// maxStringOut caps the output size of the string-building builtins
// (REPLACE / CONCAT / JOIN). Rule text and row data are both untrusted; the
// cap turns would-be memory-amplification (DoS) results into NULL, which SQL
// semantics already propagate safely through every comparison. 1 MiB is far
// beyond any legitimate rule-computed string.
const maxStringOut = 1 << 20

// maxArrayElems caps the element count of computed arrays (SPLIT). A []string
// costs ~16 bytes of header per element on top of the shared backing text, so
// splitting a large row string on a 1-byte separator would amplify a 10 MiB
// row value into ~170 MiB of slice headers per evaluation — per rule × per
// row × per worker. Oversized results are NULL (checked BEFORE allocating).
// 65536 elements is far beyond any legitimate rule-side list.
const maxArrayElems = 1 << 16

func registerStrings() {
	register([]string{"TOLOWER"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return strings.ToLower(s)
	})
	register([]string{"TOUPPER"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return strings.ToUpper(s)
	})
	// STRIP(s): trim surrounding whitespace (qlbridge strip; alias of TRIM).
	register([]string{"STRIP"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return strings.TrimSpace(s)
	})
	// CHAR_LENGTH(s): rune count of a string (arrays are not accepted here —
	// use LENGTH/ARRAY_LENGTH for element counts).
	register([]string{"CHAR_LENGTH"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return float64(utf8.RuneCountInString(s))
	})
	// REPLACE(s, old[, new]): replace every occurrence of old with new (new
	// defaults to "" — i.e. remove old, matching qlbridge's replace). The
	// projected output size is checked BEFORE building: rule text is untrusted
	// in multi-tenant deployments, and REPLACE is the one string builtin whose
	// output can exceed the sum of its inputs (each occurrence of old grows by
	// len(new)-len(old)), so a short hostile rule against a large row string
	// could otherwise force a multi-GB allocation. Oversized results are NULL.
	register([]string{"REPLACE"}, 2, 3, false, func(a []any) any {
		s, ok1 := str(a[0])
		old, ok2 := str(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		repl := ""
		if len(a) == 3 {
			r, ok := str(a[2])
			if !ok {
				return nil
			}
			repl = r
		}
		if old == "" { // avoid Go's insert-between-every-rune behaviour
			return s
		}
		if len(repl) > len(old) {
			if n := strings.Count(s, old); len(s)+n*(len(repl)-len(old)) > maxStringOut {
				return nil
			}
		}
		return strings.ReplaceAll(s, old, repl)
	})
	// SPLIT(s, sep): split s around sep into an array. An empty separator
	// yields NULL (rune-splitting is never what a rule intends), and so does
	// a result beyond maxArrayElems elements (memory-amplification guard —
	// counted before allocating).
	register([]string{"SPLIT"}, 2, 2, false, func(a []any) any {
		s, ok1 := str(a[0])
		sep, ok2 := str(a[1])
		if !ok1 || !ok2 || sep == "" {
			return nil
		}
		if strings.Count(s, sep)+1 > maxArrayElems { // see maxArrayElems
			return nil
		}
		return strings.Split(s, sep)
	})
	// JOIN(v1, v2, ..., sep): join the leading values with the trailing
	// separator. Array arguments contribute their elements; NULL arguments are
	// skipped (qlbridge joins what exists). All-NULL values yield NULL.
	register([]string{"JOIN"}, 2, -1, false, func(a []any) any {
		sep, ok := str(a[len(a)-1])
		if !ok {
			return nil
		}
		var parts []string
		for _, v := range a[:len(a)-1] {
			if v == nil {
				continue
			}
			if xs, ok := arr(v); ok {
				parts = append(parts, xs...)
				continue
			}
			if s, ok := str(v); ok {
				parts = append(parts, s)
			}
		}
		if parts == nil {
			return nil
		}
		total := len(sep) * (len(parts) - 1)
		for _, p := range parts {
			total += len(p)
		}
		if total > maxStringOut { // see maxStringOut
			return nil
		}
		return strings.Join(parts, sep)
	})
	// CONCAT(v1, v2, ...): concatenate rendered values. MySQL semantics: any
	// NULL operand makes the whole result NULL.
	register([]string{"CONCAT"}, 1, -1, false, func(a []any) any {
		var sb strings.Builder
		for _, v := range a {
			s, ok := str(v)
			if !ok {
				return nil
			}
			if sb.Len()+len(s) > maxStringOut { // see maxStringOut
				return nil
			}
			sb.WriteString(s)
		}
		return sb.String()
	})

	// ---- boolean string predicates (complete predicates on their own) ----

	// CONTAINS(s, sub): substring test (qlbridge contains).
	register([]string{"CONTAINS"}, 2, 2, true, func(a []any) any {
		s, ok1 := str(a[0])
		sub, ok2 := str(a[1])
		return ok1 && ok2 && strings.Contains(s, sub)
	})
	// STARTSWITH / HASPREFIX(s, prefix): prefix test (qlbridge hasprefix).
	register([]string{"STARTSWITH", "HASPREFIX"}, 2, 2, true, func(a []any) any {
		s, ok1 := str(a[0])
		p, ok2 := str(a[1])
		return ok1 && ok2 && strings.HasPrefix(s, p)
	})
	// ENDSWITH / HASSUFFIX(s, suffix): suffix test (qlbridge hassuffix).
	register([]string{"ENDSWITH", "HASSUFFIX"}, 2, 2, true, func(a []any) any {
		s, ok1 := str(a[0])
		p, ok2 := str(a[1])
		return ok1 && ok2 && strings.HasSuffix(s, p)
	})
}

// registerCompare adds the function-form comparisons (qlbridge: eq, ne, gt,
// ge, lt, le). They follow the engine's operator semantics exactly: NULL on
// either side is false; two numbers compare numerically, everything else
// compares as rendered text.
func registerCompare() {
	register([]string{"EQ"}, 2, 2, true, func(a []any) any { return cmpFn(a[0], a[1], "=") })
	register([]string{"NE"}, 2, 2, true, func(a []any) any { return cmpFn(a[0], a[1], "!=") })
	register([]string{"GT"}, 2, 2, true, func(a []any) any { return cmpFn(a[0], a[1], ">") })
	register([]string{"GE"}, 2, 2, true, func(a []any) any { return cmpFn(a[0], a[1], ">=") })
	register([]string{"LT"}, 2, 2, true, func(a []any) any { return cmpFn(a[0], a[1], "<") })
	register([]string{"LE"}, 2, 2, true, func(a []any) any { return cmpFn(a[0], a[1], "<=") })
}

// cmpFn mirrors the VM's compare: NULL operands are false for every operator
// (two-value logic, including !=); numeric when both sides are numbers, else
// lexical on the rendered text.
func cmpFn(a, b any, op string) bool {
	if a == nil || b == nil {
		return false
	}
	if fa, ok1 := num(a); ok1 {
		if fb, ok2 := num(b); ok2 {
			switch op {
			case "=":
				return fa == fb
			case "!=":
				return fa != fb
			case ">":
				return fa > fb
			case ">=":
				return fa >= fb
			case "<":
				return fa < fb
			case "<=":
				return fa <= fb
			}
			return false
		}
	}
	sa, ok1 := str(a)
	sb, ok2 := str(b)
	if !ok1 || !ok2 { // arrays are not ordered values
		return false
	}
	switch op {
	case "=":
		return sa == sb
	case "!=":
		return sa != sb
	case ">":
		return sa > sb
	case ">=":
		return sa >= sb
	case "<":
		return sa < sb
	case "<=":
		return sa <= sb
	}
	return false
}
