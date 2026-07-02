package sqlfn

import (
	"math"
	"strings"
)

// registerNumbers adds the math builtins (qlbridge: sqrt, pow), the type casts
// (qlbridge: toint, tonumber, tobool; TOSTRING for symmetry) and the
// first-non-NULL selector (qlbridge: oneof; COALESCE is the SQL spelling).
func registerNumbers() {
	// SQRT(x): square root; negative input yields NULL.
	register([]string{"SQRT"}, 1, 1, false, func(a []any) any {
		f, ok := num(a[0])
		if !ok || f < 0 {
			return nil
		}
		return math.Sqrt(f)
	})
	// POW / POWER(x, y): x raised to y; a non-finite result yields NULL.
	register([]string{"POW", "POWER"}, 2, 2, false, func(a []any) any {
		x, ok1 := num(a[0])
		y, ok2 := num(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		r := math.Pow(x, y)
		if math.IsNaN(r) || math.IsInf(r, 0) {
			return nil
		}
		return r
	})

	// TOINT(x): integral number. Numbers truncate toward zero; strings are
	// cleaned of spaces / thousands separators and parsed; booleans map to 1/0.
	register([]string{"TOINT"}, 1, 1, false, func(a []any) any {
		f, ok := castNum(a[0])
		if !ok {
			return nil
		}
		return math.Trunc(f)
	})
	// TONUMBER(x): numeric value of x (same parsing as TOINT, no truncation).
	register([]string{"TONUMBER"}, 1, 1, false, func(a []any) any {
		f, ok := castNum(a[0])
		if !ok {
			return nil
		}
		return f
	})
	// TOBOOL(x): boolean value of x. Strings accept true/t/1/yes/y/on and
	// false/f/0/no/n/off (case-insensitive); numbers are 0=false, else true.
	register([]string{"TOBOOL"}, 1, 1, false, func(a []any) any {
		switch x := a[0].(type) {
		case bool:
			return x
		case float64:
			return x != 0
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true", "t", "1", "yes", "y", "on":
				return true
			case "false", "f", "0", "no", "n", "off":
				return false
			}
		}
		return nil
	})
	// TOSTRING(x): x rendered as text (numbers without trailing zeros,
	// booleans as "true"/"false"). Arrays yield NULL.
	register([]string{"TOSTRING"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return s
	})

	// ONEOF / COALESCE(v1, v2, ...): the first non-NULL argument.
	register([]string{"ONEOF", "COALESCE"}, 1, -1, false, func(a []any) any {
		for _, v := range a {
			if v != nil {
				return v
			}
		}
		return nil
	})
}

// castNum parses a value as a number for the TOINT / TONUMBER casts: numbers
// pass through, booleans map to 1/0, and strings are trimmed, stripped of
// thousands separators (",") and parsed as a decimal.
func castNum(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case string:
		s := strings.ReplaceAll(strings.TrimSpace(x), ",", "")
		if s == "" {
			return 0, false
		}
		return parseFloat(s)
	}
	return 0, false
}
