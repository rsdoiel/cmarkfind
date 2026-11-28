package index

import (
	"testing"
	"time"
)

func dayOf(y int, m time.Month, d int) int {
	return int(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Unix() / 86400)
}

func TestParseDayReadsTheISOFormsAtDayPrecision(t *testing.T) {
	for in, want := range map[string][2]int{
		"1970-01-01":                {0, 0},
		"2026-11-27":                {dayOf(2026, 11, 27), dayOf(2026, 11, 27)},
		"2026-11":                   {dayOf(2026, 11, 1), dayOf(2026, 11, 30)},
		"2024-02":                   {dayOf(2024, 2, 1), dayOf(2024, 2, 29)}, // a leap year
		"2026":                      {dayOf(2026, 1, 1), dayOf(2026, 12, 31)},
		"2026-11-27T23:30:00-08:00": {dayOf(2026, 11, 27), dayOf(2026, 11, 27)}, // the date as written, its zone not applied
		"2026-11-27T00:00:00Z":      {dayOf(2026, 11, 27), dayOf(2026, 11, 27)},
		"2026-11-27 09:15":          {dayOf(2026, 11, 27), dayOf(2026, 11, 27)},
		" 2026-11-27 ":              {dayOf(2026, 11, 27), dayOf(2026, 11, 27)},
		"1969-12-31":                {-1, -1},
	} {
		first, last, ok := ParseDay(in)
		if !ok || first != want[0] || last != want[1] {
			t.Errorf("%q: %d %d %v, want %v", in, first, last, ok, want)
		}
	}
}

func TestParseDayRefusesWhatIsNotADate(t *testing.T) {
	for _, in := range []string{
		"", "June 2026", "2026-13", "2026-00", "2026-02-30", "2025-02-29", "26", "20261127", "2026/11/27",
		"2026-11-27 and more", "not a date", "0000", "12026", "2026-1-5", "-2026", "2026T10:00", "2026-11T10:00",
	} {
		if first, last, ok := ParseDay(in); ok {
			t.Errorf("%q read as %d %d", in, first, last)
		}
	}
}
