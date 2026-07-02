package sqlfn

import (
	"strings"

	"github.com/dchest/siphash"
	"github.com/mssola/user_agent"
)

// registerNet2 adds the second batch of qlbridge network factors: the plural
// host/domain collectors, user-agent inspection and SipHash.
func registerNet2() {
	// DOMAINS(v1, v2, ...) / HOSTS(...): collect the base domains / hosts of
	// every URL argument (array arguments expand; NULL / unparseable values
	// are skipped; duplicates removed, order preserved). No result -> NULL.
	register([]string{"DOMAINS"}, 1, -1, false, func(a []any) any {
		return collectURLParts(a, domainOf)
	})
	register([]string{"HOSTS"}, 1, -1, false, func(a []any) any {
		return collectURLParts(a, hostOf)
	})

	// USERAGENT(ua, part): inspect a User-Agent string (qlbridge: useragent).
	// part (case-insensitive): 'bot' / 'mobile' (boolean), 'browser',
	// 'browser_version', 'engine', 'engine_version', 'os', 'platform',
	// 'mozilla', 'localization' (strings; empty -> NULL). Unknown part -> NULL.
	register([]string{"USERAGENT"}, 2, 2, false, func(a []any) any {
		s, ok1 := str(a[0])
		part, ok2 := str(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		ua := user_agent.New(s)
		switch strings.ToLower(strings.TrimSpace(part)) {
		case "bot":
			return ua.Bot()
		case "mobile":
			return ua.Mobile()
		case "mozilla":
			return nonEmpty(ua.Mozilla())
		case "platform":
			return nonEmpty(ua.Platform())
		case "os":
			return nonEmpty(ua.OS())
		case "localization":
			return nonEmpty(ua.Localization())
		case "browser":
			name, _ := ua.Browser()
			return nonEmpty(name)
		case "browser_version":
			_, version := ua.Browser()
			return nonEmpty(version)
		case "engine":
			name, _ := ua.Engine()
			return nonEmpty(name)
		case "engine_version":
			_, version := ua.Engine()
			return nonEmpty(version)
		}
		return nil
	})

	// HASH / HASH_SIP / SIPHASH(x): SipHash-2-4 of the rendered text with the
	// fixed key (k0=0, k1=1), matching qlbridge's hash.sip — which qlbridge also
	// registers under the bare name "hash", mirrored here. The 64-bit result is
	// returned as a decimal string (float64 cannot hold every uint64 exactly).
	register([]string{"HASH_SIP", "SIPHASH", "HASH"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		return formatUint(siphash.Hash(0, 1, []byte(s)))
	})
}

// collectURLParts maps every URL argument (arrays expanded) through part and
// returns the deduplicated, order-preserving results; empty -> nil.
func collectURLParts(a []any, part func(any) (string, bool)) any {
	var out []string
	seen := map[string]struct{}{}
	add := func(v any) {
		if p, ok := part(v); ok {
			if _, dup := seen[p]; !dup {
				seen[p] = struct{}{}
				out = append(out, p)
			}
		}
	}
	for _, v := range a {
		if v == nil {
			continue
		}
		if xs, ok := arr(v); ok {
			for _, e := range xs {
				add(e)
			}
			continue
		}
		add(v)
	}
	if out == nil {
		return nil
	}
	return out
}

// hostOf extracts the lower-cased host of a URL value.
func hostOf(v any) (string, bool) {
	u, _, ok := parseURL(v)
	if !ok {
		return "", false
	}
	return strings.ToLower(u.Hostname()), true
}

// domainOf extracts the naive base domain (last two host labels) of a URL value.
func domainOf(v any) (string, bool) {
	host, ok := hostOf(v)
	if !ok {
		return "", false
	}
	parts := strings.Split(host, ".")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "." + parts[len(parts)-1], true
	}
	return host, true
}

// nonEmpty maps "" to NULL, so absent user-agent parts propagate as NULL.
func nonEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
