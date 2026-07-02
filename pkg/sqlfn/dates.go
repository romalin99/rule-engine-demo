package sqlfn

import "time"

// dateLayouts are tried in order when parsing date / datetime strings. The
// first four mirror the VM's list; the slash variants extend TODATE-style
// normalization to common non-ISO inputs.
var dateLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02",
	"2006/01/02 15:04:05",
	"2006/01/02",
}

// parseTime parses a date/datetime value with the supported layouts.
func parseTime(v any) (time.Time, bool) {
	s, ok := str(v)
	if !ok {
		return time.Time{}, false
	}
	for _, layout := range dateLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// timeArg resolves the optional date argument of the *OF* extractors:
// zero arguments mean "now", one argument is parsed as a date/datetime.
func timeArg(a []any) (time.Time, bool) {
	if len(a) == 0 {
		return time.Now(), true
	}
	return parseTime(a[0])
}

// registerDates adds the extra date/time builtins (qlbridge: now, todate,
// totimestamp, dayofweek, hourofday, hourofweek, monthofyear, yy, mm, yymm;
// plus the SQL-standard HOUR / MINUTE / SECOND extractors and UNIX_TIMESTAMP).
// Dates stay ISO strings, matching the core date functions.
func registerDates() {
	// NOW(): the current timestamp, "2006-01-02 15:04:05" — the parenthesized
	// spelling of the bare CURRENT_TIMESTAMP keyword.
	register([]string{"NOW"}, 0, 0, false, func([]any) any {
		return time.Now().Format("2006-01-02 15:04:05")
	})
	// TODATE(x) normalizes any parseable date/datetime to "YYYY-MM-DD".
	// TODATE(layout, x) parses x with an explicit Go reference layout
	// (qlbridge argument order), e.g. TODATE('01/02/2006', '06/15/2026').
	register([]string{"TODATE"}, 1, 2, false, func(a []any) any {
		if len(a) == 2 {
			layout, ok1 := str(a[0])
			s, ok2 := str(a[1])
			if !ok1 || !ok2 {
				return nil
			}
			t, err := time.Parse(layout, s)
			if err != nil {
				return nil
			}
			return t.Format("2006-01-02")
		}
		t, ok := parseTime(a[0])
		if !ok {
			return nil
		}
		return t.Format("2006-01-02")
	})
	// TOTIMESTAMP(x): Unix epoch seconds of a date/datetime string.
	register([]string{"TOTIMESTAMP"}, 1, 1, false, func(a []any) any {
		t, ok := parseTime(a[0])
		if !ok {
			return nil
		}
		return float64(t.Unix())
	})
	// UNIX_TIMESTAMP([x]): epoch seconds of x, or of now when called bare.
	register([]string{"UNIX_TIMESTAMP"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return float64(t.Unix())
	})

	// HOUR / MINUTE / SECOND(x): clock components of a datetime string
	// (a date-only input reads as midnight: 0/0/0).
	register([]string{"HOUR"}, 1, 1, false, func(a []any) any {
		t, ok := parseTime(a[0])
		if !ok {
			return nil
		}
		return float64(t.Hour())
	})
	register([]string{"MINUTE"}, 1, 1, false, func(a []any) any {
		t, ok := parseTime(a[0])
		if !ok {
			return nil
		}
		return float64(t.Minute())
	})
	register([]string{"SECOND"}, 1, 1, false, func(a []any) any {
		t, ok := parseTime(a[0])
		if !ok {
			return nil
		}
		return float64(t.Second())
	})

	// The qlbridge cohort extractors: every one accepts zero arguments
	// (meaning "now") or one date/datetime argument.
	//
	// DAYOFWEEK: 0=Sunday .. 6=Saturday (Go/qlbridge numbering — note MySQL's
	// DAYOFWEEK is 1-based).
	register([]string{"DAYOFWEEK"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return float64(int(t.Weekday()))
	})
	// HOUROFDAY: 0-23.
	register([]string{"HOUROFDAY"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return float64(t.Hour())
	})
	// HOUROFWEEK: 0-167 (weekday*24 + hour, week starting Sunday).
	register([]string{"HOUROFWEEK"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return float64(int(t.Weekday())*24 + t.Hour())
	})
	// MONTHOFYEAR: 1-12 (the optional-argument twin of MONTH(x)).
	register([]string{"MONTHOFYEAR"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return float64(int(t.Month()))
	})
	// YY: two-digit year (2026 -> 26).
	register([]string{"YY"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return float64(t.Year() % 100)
	})
	// MM: month number 1-12.
	register([]string{"MM"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return float64(int(t.Month()))
	})
	// YYMM: the "0601" cohort string (July 2026 -> "2607").
	register([]string{"YYMM"}, 0, 1, false, func(a []any) any {
		t, ok := timeArg(a)
		if !ok {
			return nil
		}
		return t.Format("0601")
	})
}
