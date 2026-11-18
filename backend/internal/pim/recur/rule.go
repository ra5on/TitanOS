// Package recur is the only place in TitanOS that calls rrule-go. It reads
// RRULE, RDATE, EXDATE and RECURRENCE-ID itself (through pim/tz), checks
// every rule against a whitelist before rrule-go sees it, expands only
// within a window and under fixed caps, and recovers from a panic inside
// rrule-go. A rule outside the whitelist is kept byte for byte in the
// store; TitanOS then shows the event once (recurUnsupported).
//
// The package also rewrites a calendar file for the three ways of changing
// a series (this occurrence, this and following, all) — pure functions from
// bytes to bytes.
package recur

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// Freq is the FREQ of a rule. Only the four below are expanded.
type Freq int

const (
	Daily Freq = iota + 1
	Weekly
	Monthly
	Yearly
)

func (f Freq) String() string {
	switch f {
	case Daily:
		return "DAILY"
	case Weekly:
		return "WEEKLY"
	case Monthly:
		return "MONTHLY"
	case Yearly:
		return "YEARLY"
	}
	return ""
}

// WeekdayNum is one BYDAY entry: N is the ordinal (0 = every such day,
// 3 = the third, -1 = the last).
type WeekdayNum struct {
	N   int
	Day time.Weekday
}

func (w WeekdayNum) String() string {
	if w.N == 0 {
		return dayCodes[w.Day]
	}
	return strconv.Itoa(w.N) + dayCodes[w.Day]
}

var dayCodes = [7]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}

func dayCode(s string) (time.Weekday, bool) {
	for i, c := range dayCodes {
		if c == s {
			return time.Weekday(i), true
		}
	}
	return 0, false
}

// Rule is a parsed RRULE value within the whitelist.
type Rule struct {
	Freq     Freq
	Interval int // 1 when not written
	Count    int // 0 = none
	// Until is a date, a UTC time or a floating time (wall clock read in
	// UTC); nil = none. Expansion converts it into the zone of DTSTART.
	Until      *tz.Value
	Wkst       time.Weekday
	WkstSet    bool
	ByDay      []WeekdayNum
	ByMonthDay []int
	ByYearDay  []int
	ByWeekNo   []int
	ByMonth    []int
	BySetPos   []int
	ByHour     []int
	ByMinute   []int
	BySecond   []int
}

var (
	// ErrSyntax marks an RRULE that is not well-formed (RFC 5545 3.3.10).
	ErrSyntax = errors.New("malformed recurrence rule")
	// ErrUnsupported marks a well-formed rule outside what TitanOS expands.
	ErrUnsupported = errors.New("recurrence rule not supported")
)

const (
	maxRuleLength = 2048
	maxListLength = 400
)

func unsupported(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrUnsupported}, args...)...)
}

func syntax(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrSyntax}, args...)...)
}

// Parse reads an RRULE value such as "FREQ=MONTHLY;BYDAY=3TU". It checks
// everything that does not depend on DTSTART (Rule.Check does the rest).
// The error wraps ErrSyntax or ErrUnsupported.
func Parse(s string) (*Rule, error) {
	if len(s) > maxRuleLength {
		return nil, unsupported("rule longer than %d bytes", maxRuleLength)
	}
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "RRULE:")
	if s == "" {
		return nil, syntax("empty")
	}
	r := &Rule{Interval: 1}
	seen := map[string]bool{}
	var unsupportedErr error
	for _, part := range strings.Split(s, ";") {
		key, value, ok := strings.Cut(part, "=")
		key = strings.ToUpper(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		if !ok || key == "" || value == "" {
			return nil, syntax("part %q", part)
		}
		if seen[key] {
			return nil, syntax("%s twice", key)
		}
		seen[key] = true
		var err error
		switch key {
		case "FREQ":
			switch strings.ToUpper(value) {
			case "DAILY":
				r.Freq = Daily
			case "WEEKLY":
				r.Freq = Weekly
			case "MONTHLY":
				r.Freq = Monthly
			case "YEARLY":
				r.Freq = Yearly
			case "SECONDLY", "MINUTELY", "HOURLY":
				unsupportedErr = unsupported("FREQ=%s", strings.ToUpper(value))
			default:
				return nil, syntax("FREQ=%s", value)
			}
		case "INTERVAL":
			r.Interval, err = atoi(value)
			if err == nil && (r.Interval < 1 || r.Interval > 1000) {
				// INTERVAL=0 would become 1 inside rrule-go (#77).
				unsupportedErr = unsupported("INTERVAL=%d", r.Interval)
			}
		case "COUNT":
			r.Count, err = atoi(value)
			if err == nil && (r.Count < 1 || r.Count > MaxOccurrences) {
				unsupportedErr = unsupported("COUNT=%d", r.Count)
			}
		case "UNTIL":
			var v tz.Value
			v, err = parseUntil(value)
			r.Until = &v
		case "WKST":
			r.Wkst, ok = dayCode(strings.ToUpper(value))
			r.WkstSet = true
			if !ok {
				err = syntax("WKST=%s", value)
			}
		case "BYDAY":
			r.ByDay, err = parseByDay(value)
		case "BYMONTHDAY":
			r.ByMonthDay, err = intList(value, 1, 31, true)
		case "BYYEARDAY":
			r.ByYearDay, err = intList(value, 1, 366, true)
		case "BYWEEKNO":
			r.ByWeekNo, err = intList(value, 1, 53, true)
		case "BYMONTH":
			r.ByMonth, err = intList(value, 1, 12, false)
		case "BYSETPOS":
			r.BySetPos, err = intList(value, 1, 366, true)
		case "BYHOUR":
			r.ByHour, err = intList(value, 0, 23, false)
		case "BYMINUTE":
			r.ByMinute, err = intList(value, 0, 59, false)
		case "BYSECOND":
			r.BySecond, err = intList(value, 0, 60, false)
		case "BYEASTER":
			// rrule-go panics on offsets outside the year (#76); not
			// RFC 5545 anyway.
			unsupportedErr = unsupported("BYEASTER")
		default:
			if strings.HasPrefix(key, "X-") {
				unsupportedErr = unsupported("%s", key)
			} else {
				return nil, syntax("unknown part %s", key)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	if r.Freq == 0 && unsupportedErr == nil {
		return nil, syntax("FREQ missing")
	}
	if r.Count > 0 && r.Until != nil {
		return nil, syntax("COUNT and UNTIL together")
	}
	if unsupportedErr != nil {
		return nil, unsupportedErr
	}
	if err := r.checkStatic(); err != nil {
		return nil, err
	}
	return r, nil
}

// checkStatic applies the parts of the whitelist that need no DTSTART.
func (r *Rule) checkStatic() error {
	for _, d := range r.ByDay {
		limit := 0
		switch r.Freq {
		case Monthly:
			limit = 5
		case Yearly:
			limit = 53
			if len(r.ByMonth) > 0 {
				limit = 5
			}
		}
		if d.N > limit || d.N < -limit {
			// rrule-go panics on BYDAY ordinals outside the period (#75).
			return unsupported("BYDAY=%s with FREQ=%s", d, r.Freq)
		}
		if d.N != 0 && r.Freq == Yearly && len(r.ByWeekNo) > 0 {
			return unsupported("BYDAY ordinal with BYWEEKNO")
		}
	}
	if len(r.ByMonthDay) > 0 && r.Freq == Weekly {
		return unsupported("BYMONTHDAY with FREQ=WEEKLY")
	}
	if len(r.ByYearDay) > 0 && r.Freq != Yearly {
		return unsupported("BYYEARDAY with FREQ=%s", r.Freq)
	}
	if len(r.ByWeekNo) > 0 && r.Freq != Yearly {
		return unsupported("BYWEEKNO with FREQ=%s", r.Freq)
	}
	if len(r.ByWeekNo) > 0 && (len(r.ByMonth) > 0 || len(r.ByMonthDay) > 0 || len(r.ByYearDay) > 0) {
		return unsupported("BYWEEKNO with BYMONTH, BYMONTHDAY or BYYEARDAY")
	}
	if len(r.BySetPos) > 0 && len(r.ByDay)+len(r.ByMonthDay)+len(r.ByYearDay)+len(r.ByWeekNo)+len(r.ByMonth) == 0 {
		return unsupported("BYSETPOS without another BY part")
	}
	return nil
}

func atoi(s string) (int, error) {
	if len(s) > 9 {
		return 0, syntax("number %q", s)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, syntax("number %q", s)
	}
	return n, nil
}

func intList(s string, lo, hi int, signed bool) ([]int, error) {
	parts := strings.Split(s, ",")
	if len(parts) > maxListLength {
		return nil, unsupported("list longer than %d", maxListLength)
	}
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, err
		}
		a := n
		if signed && a < 0 {
			a = -a
		}
		if (!signed && n < 0) || a < lo || a > hi {
			return nil, unsupported("value %d out of range", n)
		}
		out = append(out, n)
	}
	return out, nil
}

func parseByDay(s string) ([]WeekdayNum, error) {
	parts := strings.Split(s, ",")
	if len(parts) > maxListLength {
		return nil, unsupported("BYDAY longer than %d", maxListLength)
	}
	out := make([]WeekdayNum, 0, len(parts))
	for _, p := range parts {
		p = strings.ToUpper(strings.TrimSpace(p))
		if len(p) < 2 {
			return nil, syntax("BYDAY %q", p)
		}
		wd, ok := dayCode(p[len(p)-2:])
		if !ok {
			return nil, syntax("BYDAY %q", p)
		}
		n := 0
		if num := p[:len(p)-2]; num != "" {
			v, err := atoi(strings.TrimPrefix(num, "+"))
			if err != nil || v == 0 {
				return nil, syntax("BYDAY %q", p)
			}
			if v > 53 || v < -53 {
				return nil, unsupported("BYDAY %q", p)
			}
			n = v
		}
		out = append(out, WeekdayNum{N: n, Day: wd})
	}
	return out, nil
}

// parseUntil reads UNTIL: a date, a UTC date-time or a floating one. A
// floating value keeps its wall clock in UTC until expansion places it.
func parseUntil(s string) (tz.Value, error) {
	switch {
	case len(s) == 8 && digits(s):
		t, err := time.ParseInLocation("20060102", s, time.UTC)
		if err == nil {
			return tz.Value{Time: t, Kind: tz.KindDate}, nil
		}
	case len(s) == 16 && s[15] == 'Z':
		t, err := time.ParseInLocation("20060102T150405Z", s, time.UTC)
		if err == nil && digits(s[:8]) && digits(s[9:15]) {
			return tz.Value{Time: t, Kind: tz.KindUTC}, nil
		}
	case len(s) == 15:
		t, err := time.ParseInLocation("20060102T150405", s, time.UTC)
		if err == nil && digits(s[:8]) && digits(s[9:]) {
			return tz.Value{Time: t, Kind: tz.KindFloating}, nil
		}
	}
	return tz.Value{}, syntax("UNTIL=%s", s)
}

func digits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}

// String writes the rule back in a fixed order. Parse(r.String()) gives
// an equal rule.
func (r *Rule) String() string {
	var parts []string
	parts = append(parts, "FREQ="+r.Freq.String())
	if r.Until != nil {
		parts = append(parts, "UNTIL="+r.Until.Format())
	}
	if r.Count > 0 {
		parts = append(parts, "COUNT="+strconv.Itoa(r.Count))
	}
	if r.Interval > 1 {
		parts = append(parts, "INTERVAL="+strconv.Itoa(r.Interval))
	}
	ints := func(name string, v []int) {
		if len(v) == 0 {
			return
		}
		s := make([]string, len(v))
		for i, n := range v {
			s[i] = strconv.Itoa(n)
		}
		parts = append(parts, name+"="+strings.Join(s, ","))
	}
	ints("BYSECOND", r.BySecond)
	ints("BYMINUTE", r.ByMinute)
	ints("BYHOUR", r.ByHour)
	if len(r.ByDay) > 0 {
		s := make([]string, len(r.ByDay))
		for i, d := range r.ByDay {
			s[i] = d.String()
		}
		parts = append(parts, "BYDAY="+strings.Join(s, ","))
	}
	ints("BYMONTHDAY", r.ByMonthDay)
	ints("BYYEARDAY", r.ByYearDay)
	ints("BYWEEKNO", r.ByWeekNo)
	ints("BYMONTH", r.ByMonth)
	ints("BYSETPOS", r.BySetPos)
	if r.WkstSet {
		parts = append(parts, "WKST="+dayCodes[r.Wkst])
	}
	return strings.Join(parts, ";")
}

// Clone returns a deep copy.
func (r *Rule) Clone() *Rule {
	c := *r
	if r.Until != nil {
		u := *r.Until
		c.Until = &u
	}
	c.ByDay = slices.Clone(r.ByDay)
	c.ByMonthDay = slices.Clone(r.ByMonthDay)
	c.ByYearDay = slices.Clone(r.ByYearDay)
	c.ByWeekNo = slices.Clone(r.ByWeekNo)
	c.ByMonth = slices.Clone(r.ByMonth)
	c.BySetPos = slices.Clone(r.BySetPos)
	c.ByHour = slices.Clone(r.ByHour)
	c.ByMinute = slices.Clone(r.ByMinute)
	c.BySecond = slices.Clone(r.BySecond)
	return &c
}
