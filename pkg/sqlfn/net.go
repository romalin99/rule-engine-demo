package sqlfn

import (
	"net/mail"
	"net/url"
	"strings"
)

// registerNet adds the email and URL accessors (qlbridge: email, emailname,
// emaildomain, host, domain, path, qs, urldecode, urlmain, urlminusqs).
func registerNet() {
	// EMAIL(x): the normalized (lower-cased, display-name stripped) address,
	// or NULL when x does not parse as an email.
	register([]string{"EMAIL"}, 1, 1, false, func(a []any) any {
		addr, ok := emailAddr(a[0])
		if !ok {
			return nil
		}
		return addr
	})
	// EMAILNAME(x): the local part before '@' ("bob" of "bob@example.com").
	register([]string{"EMAILNAME"}, 1, 1, false, func(a []any) any {
		addr, ok := emailAddr(a[0])
		if !ok {
			return nil
		}
		return addr[:strings.LastIndex(addr, "@")]
	})
	// EMAILDOMAIN(x): the domain after '@' ("example.com").
	register([]string{"EMAILDOMAIN"}, 1, 1, false, func(a []any) any {
		addr, ok := emailAddr(a[0])
		if !ok {
			return nil
		}
		return addr[strings.LastIndex(addr, "@")+1:]
	})

	// HOST(x): the lower-cased host of a URL, port stripped. A scheme-less
	// input ("www.x.com/p") is accepted.
	register([]string{"HOST"}, 1, 1, false, func(a []any) any {
		u, _, ok := parseURL(a[0])
		if !ok {
			return nil
		}
		return strings.ToLower(u.Hostname())
	})
	// DOMAIN(x): the base domain — the last two labels of the host
	// ("www.google.com" -> "google.com"). Naive like qlbridge: no public-suffix
	// list, so "a.co.uk" yields "co.uk".
	register([]string{"DOMAIN"}, 1, 1, false, func(a []any) any {
		u, _, ok := parseURL(a[0])
		if !ok {
			return nil
		}
		host := strings.ToLower(u.Hostname())
		parts := strings.Split(host, ".")
		if len(parts) >= 2 {
			return parts[len(parts)-2] + "." + parts[len(parts)-1]
		}
		return host
	})
	// PATH / URLPATH(x): the path component ("/a/b" of "http://h/a/b?x=1").
	register([]string{"PATH", "URLPATH"}, 1, 1, false, func(a []any) any {
		u, _, ok := parseURL(a[0])
		if !ok {
			return nil
		}
		return u.Path
	})
	// QS / QS2 / QSL(x, key): the first value of a query-string parameter, NULL
	// when the parameter is absent. The implementation follows qlbridge's qs2 —
	// the URL keeps its case (qlbridge's deprecated qs/qsl lower-cased the whole
	// URL, corrupting mixed-case parameter values); the QS2/QSL names are
	// registered so rules ported verbatim from qlbridge parse unchanged.
	register([]string{"QS", "QS2", "QSL"}, 2, 2, false, func(a []any) any {
		u, _, ok := parseURL(a[0])
		if !ok {
			return nil
		}
		key, ok := str(a[1])
		if !ok {
			return nil
		}
		vs, present := u.Query()[key]
		if !present || len(vs) == 0 {
			return nil
		}
		return vs[0]
	})
	// URLDECODE(x): percent-decoding ('%20' and '+' become spaces).
	register([]string{"URLDECODE"}, 1, 1, false, func(a []any) any {
		s, ok := str(a[0])
		if !ok {
			return nil
		}
		d, err := url.QueryUnescape(s)
		if err != nil {
			return nil
		}
		return d
	})
	// URLMAIN(x): the URL without query string or fragment. The scheme is kept
	// only when the input had one.
	register([]string{"URLMAIN"}, 1, 1, false, func(a []any) any {
		u, hadScheme, ok := parseURL(a[0])
		if !ok {
			return nil
		}
		return urlBase(u, hadScheme)
	})
	// URLMINUSQS(x, key): the URL with one query-string parameter removed.
	// Remaining parameters are re-encoded in sorted-key order.
	register([]string{"URLMINUSQS"}, 2, 2, false, func(a []any) any {
		u, hadScheme, ok := parseURL(a[0])
		if !ok {
			return nil
		}
		key, ok := str(a[1])
		if !ok {
			return nil
		}
		q := u.Query()
		q.Del(key)
		base := urlBase(u, hadScheme)
		if enc := q.Encode(); enc != "" {
			return base + "?" + enc
		}
		return base
	})
}

// emailAddr normalizes an email value: trim, lower-case, strip any display
// name ("Bob <bob@x.com>" -> "bob@x.com"). ok is false when the input is not
// a parseable address with non-empty local and domain parts.
func emailAddr(v any) (string, bool) {
	s, ok := str(v)
	if !ok {
		return "", false
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", false
	}
	a, err := mail.ParseAddress(s)
	if err != nil {
		return "", false
	}
	addr := a.Address
	at := strings.LastIndex(addr, "@")
	if at <= 0 || at == len(addr)-1 {
		return "", false
	}
	return addr, true
}

// parseURL parses a URL value, accepting scheme-less inputs by assuming http.
// hadScheme reports whether the input carried its own scheme, so rebuilders
// (URLMAIN / URLMINUSQS) can preserve the original shape.
func parseURL(v any) (u *url.URL, hadScheme, ok bool) {
	s, sok := str(v)
	if !sok {
		return nil, false, false
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, false, false
	}
	hadScheme = strings.Contains(s, "://")
	if !hadScheme {
		s = "http://" + s
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return nil, false, false
	}
	return u, hadScheme, true
}

// urlBase renders scheme://host/path (or host/path for scheme-less inputs).
func urlBase(u *url.URL, hadScheme bool) string {
	if hadScheme {
		return u.Scheme + "://" + u.Host + u.Path
	}
	return u.Host + u.Path
}
