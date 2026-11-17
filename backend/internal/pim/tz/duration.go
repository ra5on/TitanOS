package tz

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Duration is an iCalendar DURATION (RFC 5545 3.3.6). Weeks and days are
// nominal (a day across a DST change is 23 or 25 hours), hours, minutes and
// seconds exact.
type Duration struct {
	Negative bool
	Days     int // weeks count as 7 days
	Exact    time.Duration
}

// maxDurationDays keeps a duration within reach of time.Time arithmetic
// and of any sensible event (about 27 years).
const maxDurationDays = 10000

// ParseDuration reads a value such as "PT1H30M", "P1D", "-P2W" or
// "P1DT12H".
func ParseDuration(s string) (Duration, error) {
	var d Duration
	in := strings.TrimSpace(s)
	switch {
	case strings.HasPrefix(in, "-"):
		d.Negative = true
		in = in[1:]
	case strings.HasPrefix(in, "+"):
		in = in[1:]
	}
	if !strings.HasPrefix(in, "P") || len(in) < 3 {
		return Duration{}, fmt.Errorf("%w: duration %q", ErrInvalid, s)
	}
	in = in[1:]
	inTime, any := false, false
	seen := map[byte]bool{}
	for in != "" {
		if in[0] == 'T' {
			if inTime {
				return Duration{}, fmt.Errorf("%w: duration %q", ErrInvalid, s)
			}
			inTime = true
			in = in[1:]
			continue
		}
		i := 0
		for i < len(in) && in[i] >= '0' && in[i] <= '9' {
			i++
		}
		if i == 0 || i > 9 || i == len(in) {
			return Duration{}, fmt.Errorf("%w: duration %q", ErrInvalid, s)
		}
		n, _ := strconv.Atoi(in[:i])
		unit := in[i]
		in = in[i+1:]
		if seen[unit] {
			return Duration{}, fmt.Errorf("%w: duration %q", ErrInvalid, s)
		}
		seen[unit] = true
		switch {
		case !inTime && unit == 'W':
			d.Days += 7 * n
		case !inTime && unit == 'D':
			d.Days += n
		case inTime && unit == 'H':
			d.Exact += time.Duration(n) * time.Hour
		case inTime && unit == 'M':
			d.Exact += time.Duration(n) * time.Minute
		case inTime && unit == 'S':
			d.Exact += time.Duration(n) * time.Second
		default:
			return Duration{}, fmt.Errorf("%w: duration %q", ErrInvalid, s)
		}
		any = true
	}
	if !any || d.Days > maxDurationDays || d.Exact > maxDurationDays*24*time.Hour {
		return Duration{}, fmt.Errorf("%w: duration %q", ErrInvalid, s)
	}
	return d, nil
}

// AddTo returns t moved by d: days on the wall clock of t's zone, then the
// exact part.
func (d Duration) AddTo(t time.Time) time.Time {
	if d.Negative {
		return t.Add(-d.Exact).AddDate(0, 0, -d.Days)
	}
	return t.AddDate(0, 0, d.Days).Add(d.Exact)
}

// IsZero reports whether d is empty.
func (d Duration) IsZero() bool { return d.Days == 0 && d.Exact == 0 }

// String formats d in RFC 5545 form.
func (d Duration) String() string {
	var b strings.Builder
	if d.Negative {
		b.WriteByte('-')
	}
	b.WriteByte('P')
	if d.Days%7 == 0 && d.Days > 0 && d.Exact == 0 {
		fmt.Fprintf(&b, "%dW", d.Days/7)
		return b.String()
	}
	if d.Days > 0 {
		fmt.Fprintf(&b, "%dD", d.Days)
	}
	if d.Exact > 0 || d.Days == 0 {
		b.WriteByte('T')
		sec := int64(d.Exact / time.Second)
		h, m, s := sec/3600, sec/60%60, sec%60
		if h > 0 {
			fmt.Fprintf(&b, "%dH", h)
		}
		if m > 0 {
			fmt.Fprintf(&b, "%dM", m)
		}
		if s > 0 || (h == 0 && m == 0) {
			fmt.Fprintf(&b, "%dS", s)
		}
	}
	return b.String()
}
