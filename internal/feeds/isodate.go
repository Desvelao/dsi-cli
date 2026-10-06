// Package feeds builds, signs and verifies RSS feeds from Markdown posts and
// OPML lists from vCards.
package feeds

import (
	"errors"
	"time"
	"unicode/utf8"

	"github.com/Desvelao/dsi-cli/internal/pyutil"
)

var errISO = errors.New("Invalid isoformat string")

// ParseISODate parses an ISO 8601 date or datetime like Python 3.12's
// datetime.fromisoformat (the C implementation, quirks included) and returns
// the instant as a naive time expressed in UTC: values with an offset are
// converted, values without one are taken as UTC. Surrounding whitespace is
// ignored.
func ParseISODate(value string) (time.Time, error) {
	s := pyutil.Strip(value)
	sepAt := findISOSeparator(s)
	if sepAt < 0 {
		return time.Time{}, errISO
	}
	y, mo, d, err := parseISODate(s, sepAt)
	if err != nil {
		return time.Time{}, err
	}
	var h, mi, sec, us, tzOffset, tzUs int
	hasTZ := false
	if len(s) > sepAt {
		// skip the separator, which may be any (possibly multi-byte) character
		_, w := utf8.DecodeRuneInString(s[sepAt:])
		rest := s[sepAt+w:]
		h, mi, sec, us, tzOffset, tzUs, hasTZ, err = parseISOTime(rest)
		if err != nil {
			return time.Time{}, err
		}
	}
	if y < 1 || y > 9999 || mo < 1 || mo > 12 || d < 1 || d > daysIn(y, mo) ||
		h > 23 || mi > 59 || sec > 59 {
		return time.Time{}, errISO
	}
	t := time.Date(y, time.Month(mo), d, h, mi, sec, us*1000, time.UTC)
	if hasTZ {
		if tzOffset <= -86400 || tzOffset >= 86400 {
			return time.Time{}, errors.New("offset must be a timedelta strictly between -timedelta(hours=24) and timedelta(hours=24)")
		}
		t = t.Add(-(time.Duration(tzOffset)*time.Second + time.Duration(tzUs)*time.Microsecond))
		if t.Year() < 1 || t.Year() > 9999 {
			return time.Time{}, errors.New("date value out of range")
		}
	}
	return t, nil
}

func daysIn(y, m int) int {
	return time.Date(y, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// at returns the byte at i, or 0 past the end (the C code reads a NUL terminator).
func at(s string, i int) byte {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// parseDigits reads exactly n ASCII digits from s at pos.
func parseDigits(s string, pos, n int) (val, next int, ok bool) {
	for i := 0; i < n; i++ {
		c := at(s, pos+i)
		if !isDigit(c) {
			return 0, 0, false
		}
		val = val*10 + int(c-'0')
	}
	return val, pos + n, true
}

// findISOSeparator returns the index of the date/time separator (-1 if unknown).
func findISOSeparator(s string) int {
	n := len(s)
	if n == 7 {
		return 7
	}
	if at(s, 4) == '-' {
		if at(s, 5) == 'W' {
			if n < 8 {
				return -1
			}
			if n > 8 && at(s, 8) == '-' {
				return 10
			}
			return 8
		}
		return 10
	}
	if at(s, 4) == 'W' {
		idx := 7
		for ; idx < n; idx++ {
			if !isDigit(s[idx]) {
				break
			}
		}
		if idx < 9 {
			return idx
		}
		if idx%2 == 0 {
			return 7
		}
		return 8
	}
	return 8
}

func parseISODate(s string, length int) (y, m, d int, err error) {
	var p int
	var ok bool
	if y, p, ok = parseDigits(s, 0, 4); !ok {
		return 0, 0, 0, errISO
	}
	usesSep := at(s, p) == '-'
	if usesSep {
		p++
	}
	if at(s, p) == 'W' {
		p++
		week, np, ok := parseDigits(s, p, 2)
		if !ok {
			return 0, 0, 0, errISO
		}
		p = np
		day := 1
		if p < length {
			if usesSep {
				c := at(s, p)
				p++
				if c != '-' {
					return 0, 0, 0, errISO
				}
			}
			if day, _, ok = parseDigits(s, p, 1); !ok {
				return 0, 0, 0, errISO
			}
		}
		return isoToYMD(y, week, day)
	}
	if m, p, ok = parseDigits(s, p, 2); !ok {
		return 0, 0, 0, errISO
	}
	if usesSep {
		c := at(s, p)
		p++
		if c != '-' {
			return 0, 0, 0, errISO
		}
	}
	if d, _, ok = parseDigits(s, p, 2); !ok {
		return 0, 0, 0, errISO
	}
	return y, m, d, nil
}

func isoToYMD(isoYear, week, day int) (int, int, int, error) {
	if week <= 0 || week >= 53 {
		outOfRange := true
		if week == 53 {
			first := weekday(isoYear, 1, 1)
			if first == 3 || (first == 2 && isLeap(isoYear)) {
				outOfRange = false
			}
		}
		if outOfRange {
			return 0, 0, 0, errISO
		}
	}
	if day <= 0 || day >= 8 {
		return 0, 0, 0, errISO
	}
	if isoYear < 1 || isoYear > 9999 {
		return 0, 0, 0, errISO
	}
	jan4 := time.Date(isoYear, 1, 4, 0, 0, 0, 0, time.UTC)
	week1Monday := jan4.AddDate(0, 0, -weekday(isoYear, 1, 4))
	t := week1Monday.AddDate(0, 0, (week-1)*7+day-1)
	return t.Year(), int(t.Month()), t.Day(), nil
}

// weekday is Monday=0 .. Sunday=6.
func weekday(y, m, d int) int {
	return (int(time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC).Weekday()) + 6) % 7
}

func isLeap(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }

// parseHHMMSSFF parses [HH[:?MM[:?SS]][.ffffff]] from s[start:end]. The result
// is 0 (ok), 1 (ok, but a character remains past the end) or an error.
func parseHHMMSSFF(s string, start, end int) (h, m, sec, us, rest int, err error) {
	vals := []*int{&h, &m, &sec}
	p := start
	hasSep := true
	for i := 0; i < 3; i++ {
		v, np, ok := parseDigits(s, p, 2)
		if !ok {
			return 0, 0, 0, 0, 0, errISO
		}
		*vals[i] = v
		p = np
		c := at(s, p)
		p++
		if i == 0 {
			hasSep = c == ':'
		}
		switch {
		case p >= end:
			if c != 0 {
				rest = 1
			}
			return h, m, sec, us, rest, nil
		case hasSep && c == ':':
			continue
		case c == '.' || c == ',':
			goto fraction
		case !hasSep:
			p--
		default:
			return 0, 0, 0, 0, 0, errISO // malformed time separator
		}
	}
fraction:
	remains := end - p
	toParse := remains
	if remains >= 6 {
		toParse = 6
	}
	v, np, ok := parseDigits(s, p, toParse)
	if !ok {
		return 0, 0, 0, 0, 0, errISO
	}
	us = v
	if toParse < 6 && toParse > 0 {
		us *= []int{100000, 10000, 1000, 100, 10}[toParse-1]
	}
	p = np
	for isDigit(at(s, p)) {
		p++
	}
	if at(s, p) != 0 {
		rest = 1
	}
	return h, m, sec, us, rest, nil
}

func parseISOTime(s string) (h, mi, sec, us, tzOffset, tzUs int, hasTZ bool, err error) {
	end := len(s)
	tzPos := 0
	for tzPos < end {
		if c := s[tzPos]; c == 'Z' || c == '+' || c == '-' {
			break
		}
		tzPos++
	}
	h, mi, sec, us, rest, err := parseHHMMSSFF(s, 0, tzPos)
	if err != nil {
		return
	}
	if tzPos == end {
		if rest == 1 {
			err = errISO
		}
		return
	}
	if s[tzPos] == 'Z' {
		if tzPos+1 != end {
			err = errISO
			return
		}
		return h, mi, sec, us, 0, 0, true, nil
	}
	sign := 1
	if s[tzPos] == '-' {
		sign = -1
	}
	tzh, tzm, tzs, tzus, trest, terr := parseHHMMSSFF(s, tzPos+1, end)
	if terr != nil || trest != 0 {
		err = errISO
		return
	}
	return h, mi, sec, us, sign * (tzh*3600 + tzm*60 + tzs), sign * tzus, true, nil
}
