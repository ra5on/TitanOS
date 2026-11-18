package recur

import (
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// Year range of DTSTART that is expanded.
const (
	minYear = 1900
	maxYear = 2200
)

// feasibleYears is how far ahead a rule must produce its first day. The
// Gregorian calendar repeats every 400 years, so a rule without a match in
// that span has none at all, apart from INTERVAL phases longer than that.
const feasibleYears = 400

// Check applies the parts of the whitelist that depend on DTSTART: its
// year, BYHOUR/BYMINUTE/BYSECOND only with the value of DTSTART itself,
// and a rule that can produce no occurrence at all. rrule-go scans to the
// year 9999 for such a rule, whatever UNTIL says; that is too slow to let
// a client trigger it at will. start is DTSTART in the zone of expansion.
func (r *Rule) Check(dtstart tz.Value, start time.Time) error {
	if y := start.Year(); y < minYear || y > maxYear {
		return unsupported("DTSTART in the year %d", y)
	}
	only := func(name string, list []int, want int) error {
		if len(list) == 0 {
			return nil
		}
		if dtstart.IsDate() || len(list) != 1 || list[0] != want {
			return unsupported("%s other than the time of DTSTART", name)
		}
		return nil
	}
	if err := only("BYHOUR", r.ByHour, start.Hour()); err != nil {
		return err
	}
	if err := only("BYMINUTE", r.ByMinute, start.Minute()); err != nil {
		return err
	}
	if err := only("BYSECOND", r.BySecond, start.Second()); err != nil {
		return err
	}
	if !r.feasible(start) {
		return unsupported("rule produces no occurrence within %d years", feasibleYears)
	}
	return nil
}

// dayFilter is the rule with the defaults rrule-go applies when no day
// part is given (a yearly rule falls on DTSTART's month and day, a monthly
// one on its day, a weekly one on its weekday).
type dayFilter struct {
	r          *Rule
	byMonth    []int
	byMonthDay []int
	byDay      []WeekdayNum
}

func (r *Rule) filter(start time.Time) dayFilter {
	f := dayFilter{r: r, byMonth: r.ByMonth, byMonthDay: r.ByMonthDay, byDay: r.ByDay}
	if len(r.ByWeekNo) == 0 && len(r.ByYearDay) == 0 && len(r.ByMonthDay) == 0 && len(r.ByDay) == 0 {
		switch r.Freq {
		case Yearly:
			if len(f.byMonth) == 0 {
				f.byMonth = []int{int(start.Month())}
			}
			f.byMonthDay = []int{start.Day()}
		case Monthly:
			f.byMonthDay = []int{start.Day()}
		case Weekly:
			f.byDay = []WeekdayNum{{Day: start.Weekday()}}
		}
	}
	return f
}

// civil is one day of the walk, stepped without time.Time arithmetic.
type civil struct {
	y           int
	m           time.Month
	d, wd, yday int
	mdays, ylen int
}

func civilOf(t time.Time) civil {
	y, m, d := t.Date()
	c := civil{y: y, m: m, d: d, wd: int(t.Weekday()), yday: t.YearDay(), mdays: daysIn(y, m), ylen: 365}
	if isLeap(y) {
		c.ylen = 366
	}
	return c
}

func (c *civil) next() {
	c.d++
	c.yday++
	c.wd = (c.wd + 1) % 7
	if c.d <= c.mdays {
		return
	}
	c.d = 1
	c.m++
	if c.m > 12 {
		c.m = 1
		c.y++
		c.yday = 1
		c.ylen = 365
		if isLeap(c.y) {
			c.ylen = 366
		}
	}
	c.mdays = daysIn(c.y, c.m)
}

func (f *dayFilter) match(c *civil) bool {
	r := f.r
	if len(f.byMonth) > 0 && !slices.Contains(f.byMonth, int(c.m)) {
		return false
	}
	if len(f.byMonthDay) > 0 && !slices.Contains(f.byMonthDay, c.d) && !slices.Contains(f.byMonthDay, c.d-c.mdays-1) {
		return false
	}
	if len(r.ByYearDay) > 0 && !slices.Contains(r.ByYearDay, c.yday) && !slices.Contains(r.ByYearDay, c.yday-c.ylen-1) {
		return false
	}
	if len(r.ByWeekNo) > 0 {
		w, n := weekNo(time.Date(c.y, c.m, c.d, 0, 0, 0, 0, time.UTC), r.Wkst)
		if !slices.Contains(r.ByWeekNo, w) && !slices.Contains(r.ByWeekNo, w-n-1) {
			return false
		}
	}
	if len(f.byDay) > 0 {
		ok := false
		inMonth := r.Freq == Monthly || (r.Freq == Yearly && len(r.ByMonth) > 0)
		for _, bd := range f.byDay {
			if int(bd.Day) != c.wd {
				continue
			}
			if bd.N == 0 {
				ok = true
				break
			}
			var pos, neg int
			if inMonth {
				pos, neg = (c.d-1)/7+1, -((c.mdays-c.d)/7 + 1)
			} else {
				pos, neg = (c.yday-1)/7+1, -((c.ylen-c.yday)/7 + 1)
			}
			if bd.N == pos || bd.N == neg {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return true
}

// datesPossible checks BYMONTH, BYMONTHDAY and BYYEARDAY alone over a
// leap and a common year: it rules out most impossible rules at once.
func (f *dayFilter) datesPossible() (common, leap bool) {
	plain := dayFilter{r: &Rule{Freq: f.r.Freq, ByYearDay: f.r.ByYearDay}, byMonth: f.byMonth, byMonthDay: f.byMonthDay}
	for _, y := range []int{2026, 2028} {
		c := civilOf(time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC))
		for c.y == y {
			if plain.match(&c) {
				if y == 2028 {
					leap = true
				} else {
					common = true
				}
				break
			}
			c.next()
		}
	}
	return common, leap
}

// feasible reports whether the rule yields at least one day on or after
// DTSTART within feasibleYears, honouring INTERVAL and BYSETPOS. It walks
// only the periods INTERVAL reaches; a match is usually found in the first
// one, the full walk (about 146 000 days, a few milliseconds) only for
// broken rules.
func (r *Rule) feasible(start time.Time) bool {
	f := r.filter(start)
	if common, leap := f.datesPossible(); !common && !leap {
		return false
	}
	first := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	horizon := first.AddDate(feasibleYears, 0, 0)
	k := r.Interval
	if k < 1 {
		k = 1
	}
	// period returns the first day of the nth period INTERVAL reaches and
	// its length in days.
	var period func(n int) (time.Time, int)
	switch r.Freq {
	case Daily:
		period = func(n int) (time.Time, int) { return first.AddDate(0, 0, n*k), 1 }
	case Weekly:
		back := (int(first.Weekday()) - int(r.Wkst) + 7) % 7
		ws := first.AddDate(0, 0, -back)
		period = func(n int) (time.Time, int) { return ws.AddDate(0, 0, 7*n*k), 7 }
	case Monthly:
		ms := time.Date(first.Year(), first.Month(), 1, 0, 0, 0, 0, time.UTC)
		period = func(n int) (time.Time, int) {
			s := ms.AddDate(0, n*k, 0)
			return s, daysIn(s.Year(), s.Month())
		}
	case Yearly:
		period = func(n int) (time.Time, int) {
			y := first.Year() + n*k
			if isLeap(y) {
				return time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC), 366
			}
			return time.Date(y, 1, 1, 0, 0, 0, 0, time.UTC), 365
		}
	default:
		return false
	}
	for n := 0; ; n++ {
		s, length := period(n)
		if s.After(horizon) {
			return false
		}
		c := civilOf(s)
		// Days before DTSTART count for BYSETPOS but cannot be the match.
		skip := 0
		if s.Before(first) {
			skip = int(first.Sub(s) / (24 * time.Hour))
		}
		var count, after int
		for i := 0; i < length; i++ {
			if f.match(&c) {
				count++
				if i >= skip {
					after++
				}
			}
			c.next()
		}
		if after == 0 {
			continue
		}
		if len(r.BySetPos) == 0 {
			return true
		}
		for _, p := range r.BySetPos {
			if p < 0 {
				p = -p
			}
			if p <= count {
				return true
			}
		}
	}
}

// weekNo returns the week number of d for weeks starting on wkst (week 1
// is the first with at least four days in the year, RFC 5545 3.3.10) and
// the number of weeks of that week-year.
func weekNo(d time.Time, wkst time.Weekday) (week, weeks int) {
	ws := d.AddDate(0, 0, -((int(d.Weekday()) - int(wkst) + 7) % 7))
	fourth := ws.AddDate(0, 0, 3)
	week = (fourth.YearDay()-1)/7 + 1
	dec28 := time.Date(fourth.Year(), 12, 28, 0, 0, 0, 0, time.UTC)
	ls := dec28.AddDate(0, 0, -((int(dec28.Weekday()) - int(wkst) + 7) % 7))
	weeks = (ls.AddDate(0, 0, 3).YearDay()-1)/7 + 1
	return week, weeks
}

func daysIn(year int, m time.Month) int {
	return time.Date(year, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func isLeap(y int) bool { return y%4 == 0 && (y%100 != 0 || y%400 == 0) }
