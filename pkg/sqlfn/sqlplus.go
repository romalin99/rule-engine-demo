// sqlplus.go — round fourteen: the common-SQL function set beyond qlbridge
// parity. The audit brief asks for "the named functions AND the commonly used
// SQL functions" per category; this file closes the gaps against the everyday
// MySQL surface: string utilities (LEFT/RIGHT/LPAD/LOCATE/...), math
// (MOD/SIGN/TRUNCATE/GREATEST/...), calendar extractors and formatting
// (QUARTER/LAST_DAY/DATE_FORMAT/TIMESTAMPDIFF/...), array aggregates
// (ARRAY_MIN/...) and the dynamic regexp trio (REGEXP_SUBSTR/REPLACE/INSTR).
//
// Semantics follow MySQL where MySQL defines them (documented deviations are
// called out inline), and the engine's value model everywhere else: NULL
// propagates, invalid input is NULL (never a panic), booleans render as
// "true"/"false", and every string position is RUNE-based (Chinese-safe),
// matching LENGTH/SUBSTRING. Registered AFTER every earlier batch so existing
// builtin IDs stay stable.
//
// Security posture (see docs/security_review.md §14): REPEAT/LPAD/RPAD and
// REGEXP_REPLACE project their output size BEFORE building and yield NULL
// beyond maxStringOut; the regexp trio compiles through the same capped,
// size-bounded pattern cache as URL_MATCHQS (MaxRegexpPattern / reCacheMax);
// no function here is non-deterministic (no RAND — replay/audit safety).
package sqlfn

import (
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

func registerSQLPlus() {
	registerStrings2()
	registerMath2()
	registerDates3()
	registerArrays2()
	registerRegexpFns()
}

// ---- strings ----------------------------------------------------------------

func registerStrings2() {
	// LTRIM / RTRIM(s): trim leading / trailing whitespace (Unicode, matching
	// the engine's TRIM; MySQL trims only ' ' — superset, documented).
	register([]string{"LTRIM"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return strings.TrimLeftFunc(s, unicode.IsSpace)
	})
	register([]string{"RTRIM"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return strings.TrimRightFunc(s, unicode.IsSpace)
	})
	// LEFT / RIGHT(s, n): the first / last n RUNES; n <= 0 yields "".
	register([]string{"LEFT"}, 2, 2, false, func(a []any) any {
		s, ok1 := str(a[0])
		n, ok2 := num(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		rs := []rune(s)
		k := clampLen(int(n), len(rs))
		return string(rs[:k])
	})
	register([]string{"RIGHT"}, 2, 2, false, func(a []any) any {
		s, ok1 := str(a[0])
		n, ok2 := num(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		rs := []rune(s)
		k := clampLen(int(n), len(rs))
		return string(rs[len(rs)-k:])
	})
	// REVERSE(s): the runes of s in reverse order.
	register([]string{"REVERSE"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		rs := []rune(s)
		for i, j := 0, len(rs)-1; i < j; i, j = i+1, j-1 {
			rs[i], rs[j] = rs[j], rs[i]
		}
		return string(rs)
	})
	// REPEAT(s, n): s repeated n times; n <= 0 yields "". The projected size
	// is checked BEFORE building (memory-amplification guard, like REPLACE).
	register([]string{"REPEAT"}, 2, 2, false, func(a []any) any {
		s, ok1 := str(a[0])
		n, ok2 := num(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		k := int(n)
		if k <= 0 {
			return ""
		}
		if len(s) > 0 && k > maxStringOut/len(s) { // see maxStringOut
			return nil
		}
		return strings.Repeat(s, k)
	})
	// LPAD / RPAD(s, n, pad): s padded with pad to EXACTLY n runes (longer
	// input is truncated to its first n runes, MySQL semantics). An empty pad
	// with n beyond the input yields "" (MySQL); n < 0 or beyond the output
	// cap yields NULL.
	register([]string{"LPAD"}, 3, 3, false, func(a []any) any { return padFn(a, true) })
	register([]string{"RPAD"}, 3, 3, false, func(a []any) any { return padFn(a, false) })
	// LOCATE(sub, s[, pos]): the 1-based RUNE position of the first occurrence
	// of sub at or after position pos (default 1); 0 when absent (MySQL —
	// unlike STRING_INDEX, which is qlbridge's 0-based BYTE offset with NULL).
	register([]string{"LOCATE"}, 2, 3, false, func(a []any) any {
		sub, ok1 := str(a[0])
		s, ok2 := str(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		pos := 1
		if len(a) == 3 {
			f, ok := num(a[2])
			if !ok {
				return nil
			}
			pos = int(f)
		}
		return locateRune(s, sub, pos)
	})
	// INSTR(s, sub): LOCATE with MySQL's swapped argument order.
	register([]string{"INSTR"}, 2, 2, false, func(a []any) any {
		s, ok1 := str(a[0])
		sub, ok2 := str(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		return locateRune(s, sub, 1)
	})
	// SUBSTRING_INDEX(s, delim, count): everything before the count-th
	// occurrence of delim (count > 0), or after the |count|-th occurrence from
	// the end (count < 0). Fewer occurrences than |count| yields the whole
	// string; count = 0 or an empty delim yields "" (MySQL). Scan-based — no
	// element-slice allocation, so no SPLIT-style amplification surface.
	register([]string{"SUBSTRING_INDEX"}, 3, 3, false, func(a []any) any {
		s, ok1 := str(a[0])
		delim, ok2 := str(a[1])
		cf, ok3 := num(a[2])
		if !ok1 || !ok2 || !ok3 {
			return nil
		}
		count := int(cf)
		if count == 0 || delim == "" {
			return ""
		}
		if count > 0 {
			off := 0
			for i := 0; i < count; i++ {
				j := strings.Index(s[off:], delim)
				if j < 0 {
					return s
				}
				if i == count-1 {
					return s[:off+j]
				}
				off += j + len(delim)
			}
		}
		// Negative count: keep everything after the |count|-th occurrence
		// from the END — counted over the same LEFT-to-right NON-OVERLAPPING
		// occurrences as the positive branch (MySQL/split semantics; a
		// right-to-left LastIndex scan would disagree on overlapping
		// delimiters like '深深' inside '深深深'). Two O(n) passes, no
		// allocation.
		total := strings.Count(s, delim)
		if total < -count {
			return s
		}
		off := 0
		for i := 0; i < total+count; i++ { // advance past total-|count| occurrences
			off += strings.Index(s[off:], delim) + len(delim)
		}
		return s[off+strings.Index(s[off:], delim)+len(delim):]
	})
	// INITCAP(s): first letter of every word upper-cased (Oracle/Postgres
	// spelling of TITLECASE; same word rules).
	register([]string{"INITCAP"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return titleCase(s)
	})
}

// padFn implements LPAD / RPAD (rune-exact target length).
func padFn(a []any, left bool) any {
	s, ok1 := str(a[0])
	nf, ok2 := num(a[1])
	pad, ok3 := str(a[2])
	if !ok1 || !ok2 || !ok3 {
		return nil
	}
	n := int(nf)
	if n < 0 || n > maxStringOut { // see maxStringOut
		return nil
	}
	rs := []rune(s)
	if len(rs) >= n {
		return string(rs[:n])
	}
	if pad == "" {
		return "" // MySQL: cannot reach the target length -> empty string
	}
	pr := []rune(pad)
	fill := make([]rune, 0, n-len(rs))
	for len(fill) < n-len(rs) {
		fill = append(fill, pr[len(fill)%len(pr)])
	}
	if left {
		return string(fill) + s
	}
	return s + string(fill)
}

// locateRune returns the 1-based rune position of sub in s at or after the
// 1-based rune position pos, or 0 when absent / pos out of range (MySQL).
func locateRune(s, sub string, pos int) any {
	if pos < 1 {
		return float64(0)
	}
	rs := []rune(s)
	if pos > len(rs)+1 {
		return float64(0)
	}
	tail := string(rs[pos-1:])
	i := strings.Index(tail, sub)
	if i < 0 {
		return float64(0)
	}
	return float64(pos + utf8.RuneCountInString(tail[:i]))
}

// clampLen clamps n into [0, max].
func clampLen(n, max int) int {
	if n < 0 {
		return 0
	}
	if n > max {
		return max
	}
	return n
}

// ---- math ---------------------------------------------------------------

func registerMath2() {
	// MOD(a, b): remainder with the sign of the dividend (MySQL / Go
	// math.Mod); MOD(x, 0) is NULL.
	register([]string{"MOD"}, 2, 2, false, func(a []any) any {
		x, ok1 := num(a[0])
		y, ok2 := num(a[1])
		if !ok1 || !ok2 || y == 0 {
			return nil
		}
		r := math.Mod(x, y)
		if math.IsNaN(r) {
			return nil
		}
		return r
	})
	// SIGN(x): -1, 0 or 1.
	register([]string{"SIGN"}, 1, 1, false, func(a []any) any {
		x, ok := num(a[0])
		if !ok || math.IsNaN(x) {
			return nil
		}
		switch {
		case x > 0:
			return float64(1)
		case x < 0:
			return float64(-1)
		default:
			return float64(0)
		}
	})
	// TRUNCATE(x, d): x truncated toward zero to d decimal places (d may be
	// negative: TRUNCATE(199, -2) = 100). Non-finite intermediates are NULL.
	register([]string{"TRUNCATE"}, 2, 2, false, func(a []any) any {
		x, ok1 := num(a[0])
		d, ok2 := num(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		pow := math.Pow(10, math.Trunc(d))
		if pow == 0 || math.IsInf(pow, 0) {
			return nil
		}
		r := math.Trunc(x*pow) / pow
		if math.IsNaN(r) || math.IsInf(r, 0) {
			return nil
		}
		return r
	})
	// GREATEST / LEAST(v1, v2, ...): the largest / smallest argument. NULL if
	// ANY argument is NULL (MySQL). All-numeric arguments compare numerically;
	// otherwise every argument is rendered as text and compared lexically
	// (the engine's comparison rule); an array argument is NULL.
	register([]string{"GREATEST"}, 2, -1, false, func(a []any) any { return extremum(a, true) })
	register([]string{"LEAST"}, 2, -1, false, func(a []any) any { return extremum(a, false) })
	// EXP(x): e^x; overflow is NULL.
	register([]string{"EXP"}, 1, 1, false, func(a []any) any {
		x, ok := num(a[0])
		if !ok {
			return nil
		}
		return finiteOrNil(math.Exp(x))
	})
	// LN(x): natural log; x <= 0 is NULL. LOG(x) = LN(x); LOG(b, x) = log_b(x)
	// (MySQL argument order), with b <= 0 or b = 1 NULL. LOG10 / LOG2 likewise.
	register([]string{"LN"}, 1, 1, false, func(a []any) any { return logOrNil(a[0], math.Log) })
	register([]string{"LOG"}, 1, 2, false, func(a []any) any {
		if len(a) == 1 {
			return logOrNil(a[0], math.Log)
		}
		b, ok1 := num(a[0])
		x, ok2 := num(a[1])
		if !ok1 || !ok2 || b <= 0 || b == 1 || x <= 0 {
			return nil
		}
		return finiteOrNil(math.Log(x) / math.Log(b))
	})
	register([]string{"LOG10"}, 1, 1, false, func(a []any) any { return logOrNil(a[0], math.Log10) })
	register([]string{"LOG2"}, 1, 1, false, func(a []any) any { return logOrNil(a[0], math.Log2) })
	// PI(): 3.141592653589793.
	register([]string{"PI"}, 0, 0, false, func([]any) any { return math.Pi })
}

func finiteOrNil(r float64) any {
	if math.IsNaN(r) || math.IsInf(r, 0) {
		return nil
	}
	return r
}

func logOrNil(v any, fn func(float64) float64) any {
	x, ok := num(v)
	if !ok || x <= 0 {
		return nil
	}
	return finiteOrNil(fn(x))
}

// extremum implements GREATEST / LEAST.
func extremum(a []any, greatest bool) any {
	nums := make([]float64, 0, len(a))
	allNum := true
	for _, v := range a {
		if v == nil {
			return nil // MySQL: any NULL argument -> NULL
		}
		if f, ok := num(v); ok && allNum {
			nums = append(nums, f)
		} else {
			allNum = false
		}
	}
	if allNum {
		best := nums[0]
		for _, f := range nums[1:] {
			if greatest && f > best || !greatest && f < best {
				best = f
			}
		}
		return best
	}
	best, ok := str(a[0])
	if !ok {
		return nil // arrays/objects are not ordered values
	}
	for _, v := range a[1:] {
		s, ok := str(v)
		if !ok {
			return nil
		}
		if greatest && s > best || !greatest && s < best {
			best = s
		}
	}
	return best
}

// ---- dates ----------------------------------------------------------------

func registerDates3() {
	// The cohort extractors accept zero arguments ("now") or one date/datetime
	// argument, like DAYOFWEEK / MM / YY.
	register([]string{"QUARTER"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return float64((int(t.Month())-1)/3 + 1)
	})
	// WEEKOFYEAR: ISO-8601 week number 1-53 (MySQL WEEKOFYEAR = WEEK(d, 3)).
	// The mode-dependent WEEK() is deliberately not provided.
	register([]string{"WEEKOFYEAR"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		_, wk := t.ISOWeek()
		return float64(wk)
	})
	register([]string{"DAYOFYEAR"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return float64(t.YearDay())
	})
	register([]string{"DAYOFMONTH"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return float64(t.Day())
	})
	register([]string{"MONTHNAME"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return t.Month().String() // English, matching MySQL ('July')
	})
	register([]string{"DAYNAME"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return t.Weekday().String() // English, matching MySQL ('Friday')
	})
	// LAST_DAY(x): the last day of x's month, as a date string.
	register([]string{"LAST_DAY"}, 1, 1, false, func(a []any) any {
		t, ok := parseTime(a[0])
		if !ok {
			return nil
		}
		return time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
	})
	// DATE(x) / TIME(x): the date / clock part of a datetime.
	register([]string{"DATE"}, 1, 1, false, func(a []any) any {
		t, ok := parseTime(a[0])
		if !ok {
			return nil
		}
		return t.Format("2006-01-02")
	})
	register([]string{"TIME"}, 1, 1, false, func(a []any) any {
		t, ok := parseTime(a[0])
		if !ok {
			return nil
		}
		return t.Format("15:04:05")
	})
	// DATE_FORMAT(x, fmt): MySQL %-code formatting (NOT strftime: MySQL's %i
	// is minutes and %M is the month name — use STRFTIME for strftime codes).
	register([]string{"DATE_FORMAT"}, 2, 2, false, func(a []any) any {
		t, ok1 := parseTime(a[0])
		format, ok2 := str(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		return dateFormatMySQL(t, format)
	})
	// TIMESTAMPDIFF(unit, from, to): complete units from `from` to `to`
	// (negative when to < from; truncated toward zero, MySQL). unit is
	// SECOND/MINUTE/HOUR/DAY/WEEK/MONTH/QUARTER/YEAR — the parser accepts the
	// bare MySQL spelling TIMESTAMPDIFF(MINUTE, a, b) and quotes it.
	register([]string{"TIMESTAMPDIFF"}, 3, 3, false, func(a []any) any {
		unit, ok := str(a[0])
		if !ok {
			return nil
		}
		from, ok1 := parseTime(a[1])
		to, ok2 := parseTime(a[2])
		if !ok1 || !ok2 {
			return nil
		}
		return timestampDiff(normTsUnit(unit), from, to)
	})
}

// normTsUnit normalizes a TIMESTAMPDIFF unit (upper-case, plural 's' dropped).
func normTsUnit(u string) string {
	return strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(u)), "S")
}

// timestampDiff counts complete units between two instants (MySQL semantics).
func timestampDiff(unit string, from, to time.Time) any {
	switch unit {
	case "SECOND":
		return float64(int64(to.Sub(from) / time.Second))
	case "MINUTE":
		return float64(int64(to.Sub(from) / time.Minute))
	case "HOUR":
		return float64(int64(to.Sub(from) / time.Hour))
	case "DAY":
		return float64(int64(to.Sub(from) / (24 * time.Hour)))
	case "WEEK":
		return float64(int64(to.Sub(from) / (7 * 24 * time.Hour)))
	case "MONTH", "QUARTER", "YEAR":
		months := monthsBetween(from, to)
		switch unit {
		case "QUARTER":
			return float64(months / 3)
		case "YEAR":
			return float64(months / 12)
		}
		return float64(months)
	}
	return nil // unknown unit
}

// monthsBetween counts months from `from` to `to` with MySQL TIMESTAMPDIFF
// semantics (negative when to < from): the raw month delta, decremented when
// `to`'s day-of-month + clock has not yet reached `from`'s (and the mirror
// for negative deltas). MySQL compares the day/time tuple directly — an
// AddDate anchor would drift on month-end normalization (Jul 31 + 7 months
// "is" Feb 31 → Go rolls to Mar 3), giving 6 where MySQL says 7 for
// '2026-07-31' → '2027-03-01'.
func monthsBetween(from, to time.Time) int {
	months := (to.Year()-from.Year())*12 + int(to.Month()) - int(from.Month())
	fa, ta := monthAnchor(from), monthAnchor(to)
	if months > 0 && ta < fa {
		months--
	}
	if months < 0 && ta > fa {
		months++
	}
	return months
}

// monthAnchor encodes a time's position inside its month (day + clock) as a
// single comparable scalar.
func monthAnchor(t time.Time) int64 {
	return int64(t.Day())*86400 + int64(t.Hour())*3600 + int64(t.Minute())*60 + int64(t.Second())
}

// dateFormatMySQL renders t with MySQL DATE_FORMAT % codes (common subset).
// An unrecognized %x emits the bare character, like MySQL.
func dateFormatMySQL(t time.Time, format string) string {
	var sb strings.Builder
	rs := []rune(format)
	for i := 0; i < len(rs); i++ {
		if rs[i] != '%' || i+1 >= len(rs) {
			sb.WriteRune(rs[i])
			continue
		}
		i++
		switch rs[i] {
		case 'Y':
			sb.WriteString(t.Format("2006"))
		case 'y':
			sb.WriteString(t.Format("06"))
		case 'm':
			sb.WriteString(pad2(int(t.Month())))
		case 'c':
			sb.WriteString(t.Format("1"))
		case 'd':
			sb.WriteString(pad2(t.Day()))
		case 'e':
			sb.WriteString(t.Format("2"))
		case 'H':
			sb.WriteString(pad2(t.Hour()))
		case 'k':
			sb.WriteString(strconv.Itoa(t.Hour())) // 24h, no zero padding
		case 'h', 'I':
			sb.WriteString(t.Format("03"))
		case 'l':
			sb.WriteString(t.Format("3"))
		case 'i':
			sb.WriteString(pad2(t.Minute())) // MySQL: %i = MINUTES
		case 's', 'S':
			sb.WriteString(pad2(t.Second()))
		case 'p':
			sb.WriteString(t.Format("PM"))
		case 'a':
			sb.WriteString(t.Format("Mon"))
		case 'W':
			sb.WriteString(t.Weekday().String())
		case 'b':
			sb.WriteString(t.Format("Jan"))
		case 'M':
			sb.WriteString(t.Month().String()) // MySQL: %M = month NAME
		case 'j':
			sb.WriteString(strftime(t, "%j")) // 3-digit day of year
		case 'w':
			sb.WriteString(strftime(t, "%w"))
		case 'r':
			sb.WriteString(t.Format("03:04:05 PM"))
		case 'T':
			sb.WriteString(t.Format("15:04:05"))
		case '%':
			sb.WriteRune('%')
		default: // MySQL drops the '%' and keeps the character
			sb.WriteRune(rs[i])
		}
	}
	return sb.String()
}

// ---- arrays -----------------------------------------------------------------

func registerArrays2() {
	// ARRAY_MIN / ARRAY_MAX(arr): the smallest / largest NUMERIC element
	// (non-numeric elements are skipped; none numeric -> NULL).
	register([]string{"ARRAY_MIN"}, 1, 1, false, func(a []any) any { return arrExtremum(a[0], false) })
	register([]string{"ARRAY_MAX"}, 1, 1, false, func(a []any) any { return arrExtremum(a[0], true) })
	// ARRAY_DISTINCT(arr): the array with duplicate elements removed
	// (order-preserving, text equality).
	register([]string{"ARRAY_DISTINCT"}, 1, 1, false, func(a []any) any {
		xs, ok := arr(a[0])
		if !ok {
			return nil
		}
		seen := make(map[string]struct{}, len(xs))
		out := make([]string, 0, len(xs))
		for _, e := range xs {
			if _, dup := seen[e]; dup {
				continue
			}
			seen[e] = struct{}{}
			out = append(out, e)
		}
		return out
	})
	// ARRAY_POSITION(arr, v): the 1-based position of the first element equal
	// to v (text equality, like ARRAY_CONTAINS); absent -> NULL.
	register([]string{"ARRAY_POSITION"}, 2, 2, false, func(a []any) any {
		xs, ok := arr(a[0])
		if !ok {
			return nil
		}
		v, ok := str(a[1])
		if !ok {
			return nil
		}
		for i, e := range xs {
			if e == v {
				return float64(i + 1)
			}
		}
		return nil
	})
}

func arrExtremum(v any, greatest bool) any {
	xs, ok := arr(v)
	if !ok {
		return nil
	}
	best, found := 0.0, false
	for _, e := range xs {
		f, ok := parseFloat(e)
		if !ok || math.IsNaN(f) {
			continue
		}
		if !found || (greatest && f > best) || (!greatest && f < best) {
			best, found = f, true
		}
	}
	if !found {
		return nil
	}
	return best
}

// ---- regexp -----------------------------------------------------------------

// The regexp trio accepts its pattern as any term (typically a rule literal;
// occasionally row-driven) and therefore compiles through regexpCached — the
// same capped (reCacheMax) and size-bounded (MaxRegexpPattern) cache that
// hardens URL_MATCHQS. RE2 has no catastrophic backtracking, so matching is
// linear in the subject.

func registerRegexpFns() {
	// REGEXP_SUBSTR(s, pat[, pos[, occurrence]]): the occurrence-th match of
	// pat in s starting at 1-based rune position pos; no match -> NULL.
	register([]string{"REGEXP_SUBSTR"}, 2, 4, false, func(a []any) any {
		s, re, off, occ, ok := regexpArgs(a)
		if !ok {
			return nil
		}
		lo, hi, found := findOccurrence(re, s, off, occ)
		if !found {
			return nil
		}
		return s[lo:hi]
	})
	// REGEXP_INSTR(s, pat[, pos[, occurrence]]): the 1-based RUNE position of
	// that match; 0 when there is none (MySQL).
	register([]string{"REGEXP_INSTR"}, 2, 4, false, func(a []any) any {
		s, re, off, occ, ok := regexpArgs(a)
		if !ok {
			return nil
		}
		lo, _, found := findOccurrence(re, s, off, occ)
		if !found {
			return float64(0)
		}
		return float64(utf8.RuneCountInString(s[:lo]) + 1)
	})
	// REGEXP_REPLACE(s, pat, repl): every match replaced (Go/ICU `$1` group
	// references). The output is size-projected BEFORE building and checked
	// after group expansion; beyond maxStringOut -> NULL.
	register([]string{"REGEXP_REPLACE"}, 3, 3, false, func(a []any) any {
		s, ok1 := str(a[0])
		pat, ok2 := str(a[1])
		repl, ok3 := str(a[2])
		if !ok1 || !ok2 || !ok3 {
			return nil
		}
		re, err := regexpCached(pat)
		if err != nil {
			return nil
		}
		// Projection: count matches (O(n), allocation-free) and bound the
		// pre-expansion output; $-group expansion is re-checked after.
		n, off := 0, 0
		for off <= len(s) {
			loc := re.FindStringIndex(s[off:])
			if loc == nil {
				break
			}
			n++
			if loc[1] == 0 { // empty match: step one rune to guarantee progress
				_, w := utf8.DecodeRuneInString(s[off+loc[0]:])
				off += loc[0] + maxInt(w, 1)
			} else {
				off += loc[1]
			}
		}
		if len(s)+n*len(repl) > maxStringOut { // see maxStringOut
			return nil
		}
		out := re.ReplaceAllString(s, repl)
		if len(out) > maxStringOut { // $1 expansion can exceed the projection
			return nil
		}
		return out
	})
}

// regexpArgs unpacks (s, pat[, pos[, occurrence]]) for SUBSTR/INSTR: the
// compiled pattern, the BYTE offset of the 1-based rune position pos, and the
// occurrence ordinal. ok=false on NULL/invalid arguments, a bad pattern, or
// pos/occurrence < 1.
func regexpArgs(a []any) (s string, re matcher, off, occ int, ok bool) {
	s, ok1 := str(a[0])
	pat, ok2 := str(a[1])
	if !ok1 || !ok2 {
		return "", nil, 0, 0, false
	}
	pos, occ := 1, 1
	if len(a) >= 3 {
		f, ok := num(a[2])
		if !ok {
			return "", nil, 0, 0, false
		}
		pos = int(f)
	}
	if len(a) == 4 {
		f, ok := num(a[3])
		if !ok {
			return "", nil, 0, 0, false
		}
		occ = int(f)
	}
	if pos < 1 || occ < 1 {
		return "", nil, 0, 0, false
	}
	rs := []rune(s)
	if pos > len(rs)+1 {
		return "", nil, 0, 0, false
	}
	compiled, err := regexpCached(pat)
	if err != nil {
		return "", nil, 0, 0, false
	}
	return s, compiled, len(string(rs[:pos-1])), occ, true
}

// matcher is the regexp surface the trio needs (satisfied by *regexp.Regexp).
type matcher interface {
	FindStringIndex(s string) []int
}

// findOccurrence locates the occ-th match of re in s at or after byte offset
// off, returning its byte bounds. Empty matches advance one rune so the scan
// always terminates.
func findOccurrence(re matcher, s string, off, occ int) (lo, hi int, ok bool) {
	for i := 0; i < occ; i++ {
		if off > len(s) {
			return 0, 0, false
		}
		loc := re.FindStringIndex(s[off:])
		if loc == nil {
			return 0, 0, false
		}
		lo, hi = off+loc[0], off+loc[1]
		if i == occ-1 {
			return lo, hi, true
		}
		if hi == lo { // empty match: force progress
			_, w := utf8.DecodeRuneInString(s[lo:])
			off = lo + maxInt(w, 1)
		} else {
			off = hi
		}
	}
	return 0, 0, false
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
