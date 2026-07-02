package sqlfn

import (
	"strconv"
	"strings"
	"time"
)

// registerDates2 adds the second batch of qlbridge date factors: timezone-aware
// parsing (todatein) and strftime-style formatting (strftime / extract).
func registerDates2() {
	// TODATEIN(tz, x): parse x as a wall-clock time in the IANA timezone tz
	// (e.g. 'Asia/Shanghai'), normalize to UTC, and render as a datetime
	// string. An unknown timezone or unparseable date yields NULL. Requires
	// tzdata on the host (standard on macOS/Linux servers).
	register([]string{"TODATEIN"}, 2, 2, false, func(a []any) any {
		tz, ok1 := str(a[0])
		s, ok2 := str(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			return nil
		}
		for _, layout := range dateLayouts {
			if t, err := time.ParseInLocation(layout, s, loc); err == nil {
				return t.UTC().Format("2006-01-02 15:04:05")
			}
		}
		return nil
	})

	// STRFTIME(x, fmt) / EXTRACT(x, fmt): render a date/datetime with a
	// strftime-style format (qlbridge: strftime / extract, value first).
	// Supported codes: %Y %y %m %d %e %H %I %M %S %p %a %A %b %B %j %w %s %%;
	// unrecognized codes pass through literally.
	register([]string{"STRFTIME", "EXTRACT"}, 2, 2, false, func(a []any) any {
		t, ok1 := parseTime(a[0])
		format, ok2 := str(a[1])
		if !ok1 || !ok2 {
			return nil
		}
		return strftime(t, format)
	})
}

// strftime renders t with a strftime-style format string (common subset).
func strftime(t time.Time, format string) string {
	var sb strings.Builder
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i+1 >= len(format) {
			sb.WriteByte(c)
			continue
		}
		i++
		switch format[i] {
		case 'Y':
			sb.WriteString(strconv.Itoa(t.Year()))
		case 'y':
			sb.WriteString(pad2(t.Year() % 100))
		case 'm':
			sb.WriteString(pad2(int(t.Month())))
		case 'd':
			sb.WriteString(pad2(t.Day()))
		case 'e':
			if t.Day() < 10 {
				sb.WriteByte(' ')
			}
			sb.WriteString(strconv.Itoa(t.Day()))
		case 'H':
			sb.WriteString(pad2(t.Hour()))
		case 'I':
			h := t.Hour() % 12
			if h == 0 {
				h = 12
			}
			sb.WriteString(pad2(h))
		case 'M':
			sb.WriteString(pad2(t.Minute()))
		case 'S':
			sb.WriteString(pad2(t.Second()))
		case 'p':
			if t.Hour() < 12 {
				sb.WriteString("AM")
			} else {
				sb.WriteString("PM")
			}
		case 'a':
			sb.WriteString(t.Format("Mon"))
		case 'A':
			sb.WriteString(t.Format("Monday"))
		case 'b':
			sb.WriteString(t.Format("Jan"))
		case 'B':
			sb.WriteString(t.Format("January"))
		case 'j':
			d := strconv.Itoa(t.YearDay())
			sb.WriteString(strings.Repeat("0", 3-len(d)) + d)
		case 'w':
			sb.WriteString(strconv.Itoa(int(t.Weekday())))
		case 's':
			sb.WriteString(strconv.FormatInt(t.Unix(), 10))
		case '%':
			sb.WriteByte('%')
		default: // unknown code: emit as-is
			sb.WriteByte('%')
			sb.WriteByte(format[i])
		}
	}
	return sb.String()
}

// pad2 renders an int with 2-digit zero padding.
func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}
