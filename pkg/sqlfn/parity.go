// parity.go — the third qlbridge-alignment batch: the factors found missing by
// the 2026-07-02 registry diff against github.com/araddon/qlbridge (expr/builtins):
// seconds, unixtrunc, unsign, string.index, string.titlecase, url.matchqs and
// the functional aggregates sum / avg / count. Semantics mirror qlbridge
// (documented quirks included) so rules ported from qlbridge keep their meaning.
package sqlfn

import (
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
)

// registerParity adds the third-batch qlbridge factors. Appended last so every
// previously registered builtin keeps its ID within a process.
func registerParity() {
	// SECONDS(x): a value coerced to seconds (qlbridge seconds). A date/datetime
	// string becomes Unix epoch seconds; "MM:SS" (optionally "M"-prefixed, e.g.
	// "M10:30" or "100:30") becomes minutes*60+seconds; a number — or a numeric
	// string — passes through. "0:00" yields NULL (qlbridge parity).
	register([]string{"SECONDS"}, 1, 1, false, func(a []any) any {
		if f, ok := num(a[0]); ok {
			return f
		}
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		if t, ok := parseTime(s); ok {
			return float64(t.Unix())
		}
		ms := strings.TrimPrefix(s, "M")
		if parts := strings.Split(ms, ":"); len(parts) == 2 {
			minutes, _ := parseFloat(parts[0])
			seconds, _ := parseFloat(parts[1])
			if minutes > 0 || seconds > 0 {
				return 60*minutes + seconds
			}
			return nil // "0:00" — qlbridge returns not-ok
		}
		if f, ok := parseFloat(strings.TrimSpace(s)); ok {
			return f
		}
		return nil
	})

	// UNIXTRUNC(x[, precision]): a Unix timestamp rendered as a STRING (qlbridge
	// unixtrunc, BigQuery-style DATE_TRUNC). x is a date/datetime string, an
	// epoch number, or an all-digit epoch string whose unit is inferred from its
	// length (10=s, 13=ms, 16=µs, 19=ns — dateparse's rule). One argument
	// truncates to whole seconds ("1438445529"); the two-argument precisions are
	// 's'/'seconds' -> "1438445529.707", 'ms'/'milliseconds' -> "1438445529707",
	// 'sm'/'secondsmicro' -> "1438445529.707123". Unknown precision -> NULL.
	register([]string{"UNIXTRUNC"}, 1, 2, false, func(a []any) any {
		micros, ok := epochMicros(a[0])
		if !ok {
			return nil
		}
		if len(a) == 1 {
			return strconv.FormatInt(micros/1e6, 10)
		}
		precision, ok := str(a[1])
		if !ok {
			return nil
		}
		millis := micros / 1e3
		switch strings.ToLower(strings.TrimSpace(precision)) {
		case "s", "seconds":
			return strconv.FormatInt(millis/1e3, 10) + "." + pad3(abs64(millis%1e3))
		case "ms", "milliseconds":
			return strconv.FormatInt(millis, 10)
		case "sm", "secondsmicro":
			return strconv.FormatInt(micros/1e6, 10) + "." + pad6(abs64(micros%1e6))
		}
		return nil
	})

	// UNSIGN(x): the two's-complement unsigned reading of an integer, as a
	// decimal STRING — uint64 does not fit float64 exactly (qlbridge unsign):
	// UNSIGN(-70) = '18446744073709551546', UNSIGN(876) = '876'.
	register([]string{"UNSIGN"}, 1, 1, false, func(a []any) any {
		f, ok := castNum(a[0])
		if !ok || math.IsNaN(f) || f >= math.MaxInt64 || f <= math.MinInt64 {
			return nil
		}
		return formatUint(uint64(int64(f)))
	})

	// STRING_INDEX(s, sub): the 0-based byte offset of the first occurrence of
	// sub in s (qlbridge string.index); NULL when sub is absent.
	register([]string{"STRING_INDEX"}, 2, 2, false, func(a []any) any {
		s, ok1 := str(a[0])
		sub, ok2 := str(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		if i := strings.Index(s, sub); i >= 0 {
			return float64(i)
		}
		return nil
	})

	// TITLECASE(x): the first letter of every word upper-cased (qlbridge
	// string.titlecase / strings.Title): 'hello world' -> 'Hello World'. Any
	// non-letter starts a new word; other letters are left unchanged.
	register([]string{"TITLECASE"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return titleCase(s)
	})

	// URL_MATCHQS(url[, re, ...]): the url reduced to host+path, keeping only
	// the query-string parameters whose NAME matches one of the regular
	// expressions (qlbridge url.matchqs). With no patterns every parameter is
	// dropped; kept parameters re-encode in sorted-key order. The url must be
	// absolute for the host to be recognized (qlbridge does not assume a
	// scheme here); an unparseable url or pattern yields NULL.
	register([]string{"URL_MATCHQS"}, 1, -1, false, func(a []any) any {
		raw, ok := str(a[0])
		if !ok || strings.TrimSpace(raw) == "" {
			return nil
		}
		u, err := url.Parse(strings.TrimSpace(raw))
		if err != nil {
			return nil
		}
		if len(a) == 1 {
			return u.Host + u.Path
		}
		include := make([]*regexp.Regexp, 0, len(a)-1)
		for _, p := range a[1:] {
			ps, ok := str(p)
			if !ok || ps == "" {
				return nil
			}
			re, err := regexpCached(ps)
			if err != nil {
				return nil
			}
			include = append(include, re)
		}
		kept := url.Values{}
		for k, vs := range u.Query() {
			for _, re := range include {
				if re.MatchString(k) {
					kept[k] = vs
					break
				}
			}
		}
		if enc := kept.Encode(); enc != "" {
			return u.Host + u.Path + "?" + enc
		}
		return u.Host + u.Path
	})

	// SUM(v, ...): the numeric sum of the arguments (qlbridge sum). Array
	// arguments contribute their parseable elements; NULL arguments and
	// non-numeric strings are skipped; a boolean argument poisons the whole sum
	// (NULL). qlbridge quirk kept for parity: a total of exactly 0 is NULL, so
	// ported `sum(...) == 0` rules stay never-matching.
	register([]string{"SUM"}, 1, -1, false, func(a []any) any {
		total := float64(0)
		for _, v := range a {
			switch x := v.(type) {
			case nil:
				// skip
			case float64:
				if !math.IsNaN(x) {
					total += x
				}
			case string:
				if f, ok := parseFloat(x); ok && !math.IsNaN(f) {
					total += f
				}
			case []string:
				for _, e := range x {
					if f, ok := parseFloat(e); ok && !math.IsNaN(f) {
						total += f
					}
				}
			default: // bool (qlbridge: non-numeric value poisons the sum)
				return nil
			}
		}
		if total == 0 {
			return nil
		}
		return total
	})

	// AVG(v, ...): the arithmetic mean of the numeric arguments (qlbridge avg).
	// Array arguments are strict — one unparseable element makes the result
	// NULL; scalar strings that do not parse are skipped; NULL and boolean
	// arguments are skipped. No numeric contributions -> NULL.
	register([]string{"AVG"}, 1, -1, false, func(a []any) any {
		total, count := float64(0), 0
		for _, v := range a {
			switch x := v.(type) {
			case float64:
				total += x
				count++
			case string:
				if f, ok := parseFloat(x); ok {
					total += f
					count++
				}
			case []string:
				for _, e := range x {
					f, ok := parseFloat(e)
					if !ok || math.IsNaN(f) {
						return nil // qlbridge: a bad element fails the average
					}
					total += f
					count++
				}
			}
		}
		if count == 0 {
			return nil
		}
		return total / float64(count)
	})

	// COUNT(x): 1 when x is a non-empty value, NULL otherwise (qlbridge count —
	// an occurrence marker, not a table aggregate). NULL, '' and an empty array
	// yield NULL. For collection sizes use (SELECT COUNT(*) FROM coll) or
	// ARRAY_LENGTH.
	register([]string{"COUNT"}, 1, 1, false, func(a []any) any {
		switch x := a[0].(type) {
		case nil:
			return nil
		case string:
			if x == "" {
				return nil
			}
		case []string:
			if len(x) == 0 {
				return nil
			}
		}
		return float64(1)
	})
}

// reCache memoizes URL_MATCHQS parameter patterns. Unlike the JMESPath cache
// (whose keys are compile-time literals and therefore bounded by the rule
// set), URL_MATCHQS accepts any term as a pattern — a rule may pass a FIELD,
// which makes the key space row-driven and attacker-influenceable. The cache
// is therefore capped: beyond reCacheMax distinct patterns, compilation still
// happens (CPU cost is inherent to dynamic patterns) but nothing more is
// stored, so hostile rows cannot grow memory without bound.
var (
	reCache  sync.Map
	reCacheN atomic.Int64
)

// reCacheMax bounds the number of memoized URL_MATCHQS patterns.
const reCacheMax = 1024

// regexpCached compiles (and caches, up to reCacheMax) an RE2 pattern.
func regexpCached(pattern string) (*regexp.Regexp, error) {
	if v, ok := reCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	if reCacheN.Load() < reCacheMax { // may overshoot by a few racers: harmless
		if _, loaded := reCache.LoadOrStore(pattern, re); !loaded {
			reCacheN.Add(1)
		}
	}
	return re, nil
}

// epochMicros resolves a UNIXTRUNC operand to Unix microseconds. Numbers and
// all-digit strings infer their unit from digit count (10=s, 13=ms, 16=µs,
// 19=ns); anything else must parse with the supported date/datetime layouts.
func epochMicros(v any) (int64, bool) {
	if f, ok := num(v); ok {
		return digitsToMicros(strconv.FormatFloat(f, 'f', -1, 64))
	}
	s, ok := str(v)
	if !ok {
		return 0, false
	}
	s = strings.TrimSpace(s)
	if isDigits(s) {
		return digitsToMicros(s)
	}
	if t, ok := parseTime(s); ok {
		return t.UnixMicro(), true
	}
	return 0, false
}

// digitsToMicros interprets an all-digit epoch string by its length.
func digitsToMicros(s string) (int64, bool) {
	digits := strings.TrimPrefix(s, "-")
	if !isDigits(digits) {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	switch l := len(digits); {
	case l == 16: // microseconds
		return n, true
	case l == 13: // milliseconds
		return n * 1e3, true
	case l == 19: // nanoseconds
		return n / 1e3, true
	case l <= 12: // seconds (10-digit epochs and any short value)
		return n * 1e6, true
	default: // 14/15/17/18-digit strings are not a recognized epoch unit
		return 0, false
	}
}

// isDigits reports whether s is one or more ASCII digits (a leading '-' is
// handled by the caller).
func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// abs64 returns the absolute value of a 64-bit integer remainder.
func abs64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// pad3 / pad6 zero-pad a non-negative remainder to fixed width.
func pad3(n int64) string {
	s := strconv.FormatInt(n, 10)
	return strings.Repeat("0", 3-len(s)) + s
}

func pad6(n int64) string {
	s := strconv.FormatInt(n, 10)
	return strings.Repeat("0", 6-len(s)) + s
}

// titleCase upper-cases the first letter of every word, replicating
// strings.Title (which qlbridge uses) without the deprecated API: a word
// starts after a separator, and — like strings.Title — ASCII alphanumerics,
// '_', and Unicode letters/digits are NOT separators ("foo_bar" -> "Foo_bar").
func titleCase(s string) string {
	prev := ' '
	return strings.Map(func(r rune) rune {
		if isWordSeparator(prev) {
			prev = r
			return unicode.ToTitle(r)
		}
		prev = r
		return r
	}, s)
}

// isWordSeparator mirrors strings.Title's isSeparator.
func isWordSeparator(r rune) bool {
	if r <= 0x7F {
		switch {
		case '0' <= r && r <= '9', 'a' <= r && r <= 'z', 'A' <= r && r <= 'Z', r == '_':
			return false
		}
		return true
	}
	if unicode.IsLetter(r) || unicode.IsDigit(r) {
		return false
	}
	return unicode.IsSpace(r)
}
