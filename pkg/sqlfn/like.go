// like.go — shared SQL LIKE wildcard support (round eight).
//
// The engine historically classified LIKE patterns into four fast shapes
// (equals / prefix / suffix / contains) keyed on leading/trailing '%'. That
// silently mis-evaluated standard SQL patterns that use a '_' single-character
// wildcard or an interior '%' (`'a%b'` was compared as the literal text
// "a%b"). Both runtimes now lower such patterns to an anchored RE2 regexp,
// translated here so the bytecode VM and the AST runtime share one
// implementation and cannot drift.
//
// Only SQL-semantics front-ends opt in (ir.Like/ir.LikeTerm carry a Wildcards
// flag): CEL/Expr front-ends build Like nodes out of startsWith/contains
// calls whose text must stay literal — `startsWith('a_b')` matches "a_b…",
// not "aXb…".
package sqlfn

import (
	"regexp"
	"strings"
)

// MaxRegexpPattern bounds every regular-expression pattern the engine will
// compile — REGEXP/RLIKE/REGEXP_LIKE rule literals, LIKE wildcard
// translations, and URL_MATCHQS patterns (which may come from row data).
// Go's regexp/syntax already rejects pathologically large programs, but an
// explicit input cap keeps the failure mode a clean load-time/eval-time error
// with a bounded cost, independent of stdlib internals. 4 KiB is far beyond
// any real rule pattern.
const MaxRegexpPattern = 4096

// LikeEscape backslash-escapes the LIKE wildcard characters (% _ \) in
// literal text. Front-ends that assemble LIKE patterns out of user literals —
// CEL/Expr startsWith / endsWith / contains — escape the literal with this
// and set Wildcards, so `startsWith('50%')` matches the literal text "50%"
// instead of treating '%' as a wildcard.
func LikeEscape(s string) string {
	if !strings.ContainsAny(s, `%_\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 4)
	for _, r := range s {
		if r == '%' || r == '_' || r == '\\' {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// LikeShape classifies a LIKE pattern into one of the four fast shapes —
// "equals" / "prefix" / "suffix" / "contains" — returning the literal core
// text. Under the legacy grammar (wildcards=false) this mirrors the historical
// leading/trailing-'%' classification and always succeeds ("equals" returns
// the whole pattern as core). Under the wildcard grammar (wildcards=true)
// backslash escapes are resolved into the core, and ok=false is returned when
// the pattern cannot be expressed as a fast shape (an unescaped '_' anywhere,
// or an unescaped '%' that is neither leading nor trailing) — emitters that
// target DSLs without wildcard support (CEL/Expr/Aviator) treat that as
// untranslatable.
func LikeShape(pattern string, wildcards bool) (kind, core string, ok bool) {
	if !wildcards {
		pre := strings.HasPrefix(pattern, "%")
		suf := strings.HasSuffix(pattern, "%")
		trimmed := strings.Trim(pattern, "%")
		switch {
		case pre && suf:
			return "contains", trimmed, true
		case suf:
			return "prefix", trimmed, true
		case pre:
			return "suffix", trimmed, true
		default:
			return "equals", pattern, true
		}
	}
	type tok struct {
		r   rune
		pct bool
		any bool
	}
	rs := []rune(pattern)
	toks := make([]tok, 0, len(rs))
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '\\':
			if i+1 < len(rs) {
				toks = append(toks, tok{r: rs[i+1]})
				i++
			} else {
				toks = append(toks, tok{r: '\\'})
			}
		case '%':
			toks = append(toks, tok{pct: true})
		case '_':
			toks = append(toks, tok{any: true})
		default:
			toks = append(toks, tok{r: rs[i]})
		}
	}
	a, b := 0, len(toks)
	pre, suf := false, false
	for a < b && toks[a].pct {
		a++
		pre = true
	}
	for b > a && toks[b-1].pct {
		b--
		suf = true
	}
	if a == b { // pattern was empty or only '%'s: contains("") ≡ match any non-null
		if pre {
			return "contains", "", true
		}
		return "equals", "", true
	}
	var sb strings.Builder
	for _, t := range toks[a:b] {
		if t.pct || t.any {
			return "", "", false
		}
		sb.WriteRune(t.r)
	}
	switch {
	case pre && suf:
		kind = "contains"
	case suf:
		kind = "prefix"
	case pre:
		kind = "suffix"
	default:
		kind = "equals"
	}
	return kind, sb.String(), true
}

// LikeLegacyWildcard re-encodes a legacy (Wildcards=false) LIKE pattern into
// the wildcard grammar with identical semantics: the legacy shape's core text
// is escaped — so '_' / '\' / interior '%' stay literal — and the structural
// edge-'%' wildcards are re-attached. The SQL/JSON emitters use it so a
// legacy node's emitted rule re-parses (under the SQL front-ends' wildcard
// grammar) to exactly the meaning the node had.
func LikeLegacyWildcard(pattern string) string {
	kind, core, _ := LikeShape(pattern, false)
	switch kind {
	case "contains":
		return "%" + LikeEscape(core) + "%"
	case "prefix":
		return LikeEscape(core) + "%"
	case "suffix":
		return "%" + LikeEscape(core)
	default: // equals: core is the whole pattern
		return LikeEscape(core)
	}
}

// LikeNeedsRegexp reports whether a SQL LIKE pattern uses wildcard features
// beyond the four fast shapes (equals / prefix / suffix / contains): a '_'
// single-character wildcard, a backslash escape (\% \_ \\), or a '%' that is
// neither leading nor trailing. Such patterns are evaluated via LikeRegexp.
func LikeNeedsRegexp(pattern string) bool {
	if strings.ContainsAny(pattern, "_\\") {
		return true
	}
	return strings.Contains(strings.Trim(pattern, "%"), "%")
}

// LikeRegexp translates a SQL LIKE pattern into an anchored RE2 pattern:
// '%' matches any run of characters (including newlines, hence (?s)), '_'
// matches exactly one character, and a backslash escapes the following
// character (MySQL's default ESCAPE '\'), making \% and \_ literal. All other
// text is matched literally (QuoteMeta). A dangling trailing backslash is a
// literal backslash.
func LikeRegexp(pattern string) string {
	var b strings.Builder
	b.WriteString("(?s)^")
	rs := []rune(pattern)
	for i := 0; i < len(rs); i++ {
		switch rs[i] {
		case '\\':
			if i+1 < len(rs) { // escaped character: literal
				b.WriteString(regexp.QuoteMeta(string(rs[i+1])))
				i++
			} else {
				b.WriteString(regexp.QuoteMeta(`\`))
			}
		case '%':
			b.WriteString(".*")
		case '_':
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(string(rs[i])))
		}
	}
	b.WriteString("$")
	return b.String()
}
