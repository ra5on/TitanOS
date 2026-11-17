package tz

import (
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"
)

var (
	zoneMu    sync.Mutex
	zoneCache = map[string]*time.Location{}
	zoneMiss  = map[string]bool{}
)

// loadCached wraps time.LoadLocation; parsing a zone from the embedded
// archive costs far more than a map lookup, and VTIMEZONE matching loads
// hundreds of candidates.
func loadCached(name string) (*time.Location, error) {
	zoneMu.Lock()
	defer zoneMu.Unlock()
	if l, ok := zoneCache[name]; ok {
		return l, nil
	}
	if zoneMiss[name] {
		return nil, errUnknownZone
	}
	l, err := time.LoadLocation(name)
	if err != nil {
		if len(zoneMiss) < 4096 {
			zoneMiss[name] = true
		}
		return nil, err
	}
	zoneCache[name] = l
	return l, nil
}

type unknownZone struct{}

func (unknownZone) Error() string { return "unknown time zone" }

var errUnknownZone error = unknownZone{}

// transition is one change of UTC offset.
type transition struct {
	at     time.Time // instant
	offset int       // seconds east of UTC after the change
}

// observance is one STANDARD or DAYLIGHT sub-component.
type observance struct {
	standard bool
	start    time.Time // DTSTART wall clock, read in UTC
	from, to int
	rule     *yearlyRule
	rdates   []time.Time // wall clocks, read in UTC
}

// yearlyRule is the small part of RRULE that VTIMEZONEs use: once a year in
// BYMONTH, on the nth weekday (BYDAY=-1SU) or on the first BYDAY weekday
// within BYMONTHDAY (the old US form BYMONTHDAY=8,…,14;BYDAY=SU).
type yearlyRule struct {
	month    time.Month
	nth      int
	weekday  time.Weekday
	hasDay   bool
	monthDay []int
	until    time.Time // instant; zero = none
}

// fromVTimezone resolves an unknown TZID through its VTIMEZONE: the
// X-LIC-LOCATION name, else an IANA zone that matches the offsets and
// transition days in year, else a fixed offset with guess.
func fromVTimezone(tzid string, comp *ical.Component, year int) (*time.Location, bool) {
	if p := comp.Props.Get("X-LIC-LOCATION"); p != nil {
		if l := Known(p.Value); l != nil {
			return l, false
		}
	}
	obs := observances(comp)
	if len(obs) == 0 {
		return nil, true
	}
	want, before, ok := vtzTransitions(obs, year)
	if ok {
		key := matchKey(want, before, year)
		matchMu.Lock()
		name, seen := matchCache[key]
		matchMu.Unlock()
		if !seen {
			name = searchIANA(want, before, year)
			matchMu.Lock()
			if len(matchCache) < 1024 {
				matchCache[key] = name
			}
			matchMu.Unlock()
		}
		if name != "" {
			if l := LoadIANA(name); l != nil {
				return l, false
			}
		}
	}
	// Fixed offset of the last STANDARD (by DTSTART); a zone without
	// STANDARD falls back to its last observance of any kind.
	var last *observance
	for i := range obs {
		o := &obs[i]
		if last == nil || (o.standard && !last.standard) ||
			(o.standard == last.standard && o.start.After(last.start)) {
			last = o
		}
	}
	return time.FixedZone(tzid, last.to), true
}

var (
	matchMu    sync.Mutex
	matchCache = map[string]string{}
)

func matchKey(want []transition, before, year int) string {
	var b strings.Builder
	b.WriteString(strconv.Itoa(year))
	b.WriteByte('|')
	b.WriteString(strconv.Itoa(before))
	for _, t := range want {
		b.WriteByte('|')
		b.WriteString(strconv.FormatInt(t.at.Unix(), 10))
		b.WriteByte('/')
		b.WriteString(strconv.Itoa(t.offset))
	}
	return b.String()
}

// searchIANA finds a zone whose offset at the start of year is before and
// whose transitions in year fall on the same days to the same offsets.
// Zones named for territory "001" are tried first, then all others in
// alphabetical order, so the answer is stable.
func searchIANA(want []transition, before, year int) string {
	primary := make([]string, 0, len(windowsZones))
	for _, z := range windowsZones {
		primary = append(primary, z)
	}
	slices.Sort(primary)
	for _, list := range [][]string{primary, ianaCandidates} {
		for _, name := range list {
			loc := LoadIANA(name)
			if loc == nil {
				continue
			}
			got, off := ianaTransitions(loc, year)
			if off != before || len(got) != len(want) {
				continue
			}
			match := true
			for i := range got {
				d := got[i].at.Sub(want[i].at)
				if got[i].offset != want[i].offset || d >= 24*time.Hour || d <= -24*time.Hour {
					match = false
					break
				}
			}
			if match {
				return name
			}
		}
	}
	return ""
}

// ianaTransitions lists the offset changes of loc within year and the
// offset in force at its start.
func ianaTransitions(loc *time.Location, year int) ([]transition, int) {
	t := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC).In(loc)
	_, before := t.Zone()
	end := time.Date(year+1, 1, 1, 0, 0, 0, 0, time.UTC)
	var out []transition
	for range 16 {
		_, next := t.ZoneBounds()
		if next.IsZero() || !next.Before(end) {
			break
		}
		_, off := next.Zone()
		out = append(out, transition{at: next.UTC(), offset: off})
		t = next
	}
	return out, before
}

// vtzTransitions lists the onsets of the observances within year (sorted)
// and the offset in force at its start. ok is false when the VTIMEZONE does
// not say enough to tell.
func vtzTransitions(obs []observance, year int) (out []transition, before int, ok bool) {
	type onset struct {
		wall time.Time
		o    *observance
	}
	var all []onset
	// Onsets in year-1 too, to know the offset in force on 1 January.
	for y := year - 1; y <= year; y++ {
		for i := range obs {
			o := &obs[i]
			for _, w := range o.onsets(y) {
				all = append(all, onset{w, o})
			}
		}
	}
	slices.SortFunc(all, func(a, b onset) int { return a.wall.Compare(b.wall) })
	if len(all) == 0 {
		return nil, 0, false
	}
	start := time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC)
	before = all[0].o.from
	for _, on := range all {
		at := on.wall.Add(-time.Duration(on.o.from) * time.Second)
		if at.Before(start) {
			before = on.o.to
			continue
		}
		if on.o.from == on.o.to {
			continue
		}
		out = append(out, transition{at: at, offset: on.o.to})
	}
	return out, before, true
}

// onsets returns the wall clocks (read in UTC) at which o begins in year.
func (o *observance) onsets(year int) []time.Time {
	var out []time.Time
	if o.rule == nil {
		if o.start.Year() == year {
			out = append(out, o.start)
		}
	} else if year >= o.start.Year() {
		if w, ok := o.rule.on(year, o.start); ok {
			at := w.Add(-time.Duration(o.from) * time.Second)
			if o.rule.until.IsZero() || !at.After(o.rule.until) {
				out = append(out, w)
			}
		}
	}
	for _, r := range o.rdates {
		if r.Year() == year {
			out = append(out, r)
		}
	}
	return out
}

func (r *yearlyRule) on(year int, start time.Time) (time.Time, bool) {
	h, mi, s := start.Clock()
	at := func(day int) time.Time { return time.Date(year, r.month, day, h, mi, s, 0, time.UTC) }
	days := daysIn(year, r.month)
	switch {
	case r.hasDay && len(r.monthDay) > 0:
		for _, md := range r.monthDay {
			if md < 0 {
				md = days + md + 1
			}
			if md >= 1 && md <= days && at(md).Weekday() == r.weekday {
				return at(md), true
			}
		}
		return time.Time{}, false
	case r.hasDay && r.nth != 0:
		if r.nth > 0 {
			first := at(1)
			d := 1 + (int(r.weekday)-int(first.Weekday())+7)%7 + 7*(r.nth-1)
			if d > days {
				return time.Time{}, false
			}
			return at(d), true
		}
		lastDay := at(days)
		d := days - (int(lastDay.Weekday())-int(r.weekday)+7)%7 + 7*(r.nth+1)
		if d < 1 {
			return time.Time{}, false
		}
		return at(d), true
	case len(r.monthDay) == 1 && !r.hasDay:
		md := r.monthDay[0]
		if md < 0 {
			md = days + md + 1
		}
		if md < 1 || md > days {
			return time.Time{}, false
		}
		return at(md), true
	case !r.hasDay && len(r.monthDay) == 0:
		if start.Month() != r.month || start.Day() > days {
			return time.Time{}, false
		}
		return at(start.Day()), true
	}
	return time.Time{}, false
}

func daysIn(year int, m time.Month) int {
	return time.Date(year, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func observances(comp *ical.Component) []observance {
	var out []observance
	for _, c := range comp.Children {
		if c.Name != ical.CompTimezoneStandard && c.Name != ical.CompTimezoneDaylight {
			continue
		}
		o := observance{standard: c.Name == ical.CompTimezoneStandard}
		var ok1, ok2 bool
		if p := c.Props.Get(ical.PropTimezoneOffsetFrom); p != nil {
			o.from, ok1 = parseOffset(p.Value)
		}
		if p := c.Props.Get(ical.PropTimezoneOffsetTo); p != nil {
			o.to, ok2 = parseOffset(p.Value)
		}
		p := c.Props.Get(ical.PropDateTimeStart)
		if !ok1 || !ok2 || p == nil {
			continue
		}
		start, err := time.ParseInLocation(localLayout, strings.TrimSpace(p.Value), time.UTC)
		if err != nil {
			continue
		}
		o.start = start
		if rp := c.Props.Get(ical.PropRecurrenceRule); rp != nil {
			o.rule = parseYearly(rp.Value)
			if o.rule == nil {
				// A rule this reader does not know: matching would be
				// a guess, so the fixed offset decides.
				continue
			}
		}
		for _, rp := range c.Props.Values(ical.PropRecurrenceDates) {
			for _, s := range strings.Split(rp.Value, ",") {
				if t, err := time.ParseInLocation(localLayout, strings.TrimSpace(s), time.UTC); err == nil {
					o.rdates = append(o.rdates, t)
				}
				if len(o.rdates) >= 64 {
					break
				}
			}
		}
		out = append(out, o)
		if len(out) >= 64 {
			break
		}
	}
	return out
}

func parseYearly(s string) *yearlyRule {
	r := &yearlyRule{}
	freq := false
	for _, part := range strings.Split(strings.TrimSpace(s), ";") {
		k, v, _ := strings.Cut(part, "=")
		switch strings.ToUpper(k) {
		case "FREQ":
			if strings.ToUpper(v) != "YEARLY" {
				return nil
			}
			freq = true
		case "INTERVAL":
			if v != "1" {
				return nil
			}
		case "BYMONTH":
			m, err := strconv.Atoi(v)
			if err != nil || m < 1 || m > 12 {
				return nil
			}
			r.month = time.Month(m)
		case "BYDAY":
			n, wd, ok := parseByDay(v)
			if !ok {
				return nil
			}
			r.nth, r.weekday, r.hasDay = n, wd, true
		case "BYMONTHDAY":
			for _, d := range strings.Split(v, ",") {
				n, err := strconv.Atoi(d)
				if err != nil || n == 0 || n < -31 || n > 31 {
					return nil
				}
				r.monthDay = append(r.monthDay, n)
			}
		case "UNTIL":
			t, err := time.ParseInLocation(utcLayout, v, time.UTC)
			if err != nil {
				return nil
			}
			r.until = t
		case "WKST", "COUNT":
			// WKST does not matter for a yearly nth weekday. COUNT is
			// ignored: the zones in the wild that use it describe
			// past years only.
		default:
			return nil
		}
	}
	if !freq || r.month == 0 {
		return nil
	}
	return r
}

var weekdays = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// parseByDay reads one BYDAY entry such as "-1SU", "2SU" or "SU".
func parseByDay(s string) (int, time.Weekday, bool) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if len(s) < 2 {
		return 0, 0, false
	}
	wd, ok := weekdays[s[len(s)-2:]]
	if !ok {
		return 0, 0, false
	}
	n := 0
	if num := s[:len(s)-2]; num != "" {
		v, err := strconv.Atoi(num)
		if err != nil || v == 0 || v < -5 || v > 5 {
			return 0, 0, false
		}
		n = v
	}
	return n, wd, true
}

// parseOffset reads a UTC offset such as "+0100", "-0500" or "+053000".
func parseOffset(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 5 && len(s) != 7 {
		return 0, false
	}
	sign := 1
	switch s[0] {
	case '+':
	case '-':
		sign = -1
	default:
		return 0, false
	}
	if !digits(s[1:]) {
		return 0, false
	}
	h, _ := strconv.Atoi(s[1:3])
	m, _ := strconv.Atoi(s[3:5])
	sec := 0
	if len(s) == 7 {
		sec, _ = strconv.Atoi(s[5:7])
	}
	if h > 23 || m > 59 || sec > 59 {
		return 0, false
	}
	return sign * (h*3600 + m*60 + sec), true
}
