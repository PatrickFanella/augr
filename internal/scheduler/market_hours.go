package scheduler

import (
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

var (
	newYorkLocation    *time.Location
	hasNewYorkLocation bool
)

func init() {
	var err error
	newYorkLocation, err = time.LoadLocation("America/New_York")
	hasNewYorkLocation = err == nil
}

// IsMarketOpen reports whether the given market is open at the provided time.
func IsMarketOpen(t time.Time, marketType domain.MarketType) bool {
	switch normalizeMarketType(marketType) {
	case domain.MarketTypeCrypto, domain.MarketTypePolymarket:
		return true
	case domain.MarketTypeStock:
		return isUSEquityMarketOpen(t)
	default:
		return false
	}
}

func normalizeMarketType(marketType domain.MarketType) domain.MarketType {
	return domain.MarketType(strings.ToLower(strings.TrimSpace(marketType.String())))
}

func isUSEquityMarketOpen(t time.Time) bool {
	if !hasNewYorkLocation {
		return false
	}

	et := t.In(newYorkLocation)
	if et.Weekday() == time.Saturday || et.Weekday() == time.Sunday {
		return false
	}

	if isNYSEHoliday(et) {
		return false
	}

	open := time.Date(et.Year(), et.Month(), et.Day(), 9, 30, 0, 0, newYorkLocation)
	close := time.Date(et.Year(), et.Month(), et.Day(), 16, 0, 0, 0, newYorkLocation)

	return !et.Before(open) && et.Before(close)
}

func isNYSEHoliday(t time.Time) bool {
	year := t.Year()

	holidays := []time.Time{
		observedDate(year, time.January, 1),
		nthWeekdayOfMonth(year, time.January, time.Monday, 3),
		nthWeekdayOfMonth(year, time.February, time.Monday, 3),
		easterSunday(year).AddDate(0, 0, -2),
		lastWeekdayOfMonth(year, time.May, time.Monday),
		observedDate(year, time.June, 19),
		observedDate(year, time.July, 4),
		nthWeekdayOfMonth(year, time.September, time.Monday, 1),
		nthWeekdayOfMonth(year, time.November, time.Thursday, 4),
		observedDate(year, time.December, 25),
	}

	for _, holiday := range holidays {
		if sameDay(t, holiday) {
			return true
		}
	}

	return false
}

func observedDate(year int, month time.Month, day int) time.Time {
	date := time.Date(year, month, day, 0, 0, 0, 0, newYorkLocation)

	switch date.Weekday() {
	case time.Saturday:
		return date.AddDate(0, 0, -1)
	case time.Sunday:
		return date.AddDate(0, 0, 1)
	default:
		return date
	}
}

func nthWeekdayOfMonth(year int, month time.Month, weekday time.Weekday, n int) time.Time {
	date := time.Date(year, month, 1, 0, 0, 0, 0, newYorkLocation)
	for date.Weekday() != weekday {
		date = date.AddDate(0, 0, 1)
	}

	return date.AddDate(0, 0, (n-1)*7)
}

func lastWeekdayOfMonth(year int, month time.Month, weekday time.Weekday) time.Time {
	date := time.Date(year, month+1, 0, 0, 0, 0, 0, newYorkLocation)
	for date.Weekday() != weekday {
		date = date.AddDate(0, 0, -1)
	}

	return date
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()

	return ay == by && am == bm && ad == bd
}

// easterSunday computes Easter Sunday using the Anonymous Gregorian algorithm.
func easterSunday(year int) time.Time {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1

	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, newYorkLocation)
}
