package index

import (
	"regexp"
	"strconv"
	"time"
)

var (
	dateRE = regexp.MustCompile(`^(\d{4})(?:-(\d{2})(?:-(\d{2})(?:[T ]\d{2}:\d{2}(?::\d{2}(?:\.\d+)?)?(?:Z|[+-]\d{2}:?\d{2})?)?)?)?$`)
)

// dayNumber is the number of days from 1970-01-01 to the given day.
func dayNumber(y int, m time.Month, d int) int {
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

/** ParseDay reads a date in the forms a page's front matter and a query use and
 * returns the first and last day number of the period it names. A day number
 * counts days from 1970-01-01 (before it, negative). The forms are `YYYY`,
 * `YYYY-MM` and `YYYY-MM-DD`, and a day with a time after it (`T` or a space,
 * hours and minutes, optional seconds and zone): the date is taken as written
 * and the time and zone are dropped, so a post dated 2026-11-27 late in a zone
 * behind UTC stays the 27th. A year alone is the whole year and a month the
 * whole month; a day is first and last alike. Anything else, or a date that
 * does not exist (2026-02-30), is not a date.
 *
 * Parameters:
 *   s (string) — the text, surrounding space ignored
 *
 * Returns:
 *   int  — first day number of the period
 *   int  — last day number of the period
 *   bool — false when s is not a date
 *
 * Example:
 *   first, last, ok := index.ParseDay("2026-06") // the days of June 2026
 */
func ParseDay(s string) (first, last int, ok bool) {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	m := dateRE.FindStringSubmatch(s)
	if m == nil {
		return 0, 0, false
	}
	y, _ := strconv.Atoi(m[1])
	if y < 1 {
		return 0, 0, false
	}
	if m[2] == "" {
		return dayNumber(y, 1, 1), dayNumber(y, 12, 31), true
	}
	mo, _ := strconv.Atoi(m[2])
	if mo < 1 || mo > 12 {
		return 0, 0, false
	}
	if m[3] == "" {
		return dayNumber(y, time.Month(mo), 1), dayNumber(y, time.Month(mo)+1, 0), true
	}
	d, _ := strconv.Atoi(m[3])
	t := time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.UTC)
	if t.Year() != y || int(t.Month()) != mo || t.Day() != d {
		return 0, 0, false
	}
	n := dayNumber(y, time.Month(mo), d)
	return n, n, true
}
