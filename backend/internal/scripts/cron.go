package scripts

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cron is a parsed five-field cron expression: minute, hour, day of the
// month, month and day of the week (0 or 7 = Sunday). Each field allows
// "*", numbers, ranges (a-b), lists (a,b) and steps (*/n, a-b/n). Names of
// months and weekdays and the @-shortcuts are not supported.
type cron struct {
	minute, hour, dom, month, dow uint64
	// Restricted day fields are joined with OR, as in every cron: "0 3 1 * 1"
	// runs on the first of the month and on every Monday.
	domAny, dowAny bool
}

var errCron = errors.New("invalid cron expression")

func parseCron(expr string) (cron, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return cron{}, fmt.Errorf("%w: five fields expected", errCron)
	}
	var c cron
	var err error
	if c.minute, err = cronField(fields[0], 0, 59); err != nil {
		return cron{}, err
	}
	if c.hour, err = cronField(fields[1], 0, 23); err != nil {
		return cron{}, err
	}
	if c.dom, err = cronField(fields[2], 1, 31); err != nil {
		return cron{}, err
	}
	if c.month, err = cronField(fields[3], 1, 12); err != nil {
		return cron{}, err
	}
	if c.dow, err = cronField(fields[4], 0, 7); err != nil {
		return cron{}, err
	}
	if c.dow&(1<<7) != 0 {
		c.dow |= 1
	}
	c.domAny = fields[2] == "*"
	c.dowAny = fields[4] == "*"
	return c, nil
}

func cronField(field string, low, high int) (uint64, error) {
	var bits uint64
	for part := range strings.SplitSeq(field, ",") {
		step := 1
		if base, s, ok := strings.Cut(part, "/"); ok {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 || n > high {
				return 0, fmt.Errorf("%w: step %q", errCron, part)
			}
			step, part = n, base
		}
		from, to := low, high
		switch {
		case part == "*":
		case strings.Contains(part, "-"):
			a, b, _ := strings.Cut(part, "-")
			x, err1 := strconv.Atoi(a)
			y, err2 := strconv.Atoi(b)
			if err1 != nil || err2 != nil || x > y {
				return 0, fmt.Errorf("%w: range %q", errCron, part)
			}
			from, to = x, y
		default:
			x, err := strconv.Atoi(part)
			if err != nil {
				return 0, fmt.Errorf("%w: value %q", errCron, part)
			}
			from, to = x, x
			if step > 1 {
				to = high
			}
		}
		if from < low || to > high {
			return 0, fmt.Errorf("%w: %q outside %d-%d", errCron, part, low, high)
		}
		for v := from; v <= to; v += step {
			bits |= 1 << v
		}
	}
	return bits, nil
}

// matches tells whether the minute of t (in its own location) is due.
func (c cron) matches(t time.Time) bool {
	if c.minute&(1<<t.Minute()) == 0 || c.hour&(1<<t.Hour()) == 0 || c.month&(1<<int(t.Month())) == 0 {
		return false
	}
	dom := c.dom&(1<<t.Day()) != 0
	dow := c.dow&(1<<int(t.Weekday())) != 0
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dow
	case c.dowAny:
		return dom
	default:
		return dom || dow
	}
}

// next returns the first due minute after t, looking at most a year and a
// day ahead (a 30 February never comes).
func (c cron) next(t time.Time) *time.Time {
	at := t.Truncate(time.Minute).Add(time.Minute)
	for range 366 * 24 * 60 {
		if c.matches(at) {
			return &at
		}
		at = at.Add(time.Minute)
	}
	return nil
}

// Schedule is when a script with the trigger "schedule" runs. Every is
// hourly, daily, weekly, monthly or custom (Cron).
type Schedule struct {
	Every string `json:"every"`
	// Minute of the hour for hourly.
	Minute int `json:"minute,omitempty"`
	// Time (HH:MM) for daily, weekly and monthly.
	Time string `json:"time,omitempty"`
	// Weekday 0 (Sunday) to 6 for weekly.
	Weekday int `json:"weekday,omitempty"`
	// MonthDay 1 to 28 for monthly, so that every month has it.
	MonthDay int `json:"monthDay,omitempty"`
	// Cron is the expression for custom.
	Cron string `json:"cron,omitempty"`
}

// Expression returns the schedule as cron expression.
func (s Schedule) Expression() (string, error) {
	clock := func() (int, int, error) {
		t, err := time.Parse("15:04", s.Time)
		if err != nil || len(s.Time) != 5 {
			return 0, 0, errors.New("invalid time")
		}
		return t.Hour(), t.Minute(), nil
	}
	switch s.Every {
	case "hourly":
		if s.Minute < 0 || s.Minute > 59 {
			return "", errors.New("invalid minute")
		}
		return fmt.Sprintf("%d * * * *", s.Minute), nil
	case "daily":
		h, m, err := clock()
		return fmt.Sprintf("%d %d * * *", m, h), err
	case "weekly":
		h, m, err := clock()
		if err == nil && (s.Weekday < 0 || s.Weekday > 6) {
			err = errors.New("invalid weekday")
		}
		return fmt.Sprintf("%d %d * * %d", m, h, s.Weekday), err
	case "monthly":
		h, m, err := clock()
		if err == nil && (s.MonthDay < 1 || s.MonthDay > 28) {
			err = errors.New("invalid day of the month")
		}
		return fmt.Sprintf("%d %d %d * *", m, h, s.MonthDay), err
	case "custom":
		expr := strings.Join(strings.Fields(s.Cron), " ")
		if len(expr) > 100 {
			return "", errCron
		}
		_, err := parseCron(expr)
		return expr, err
	}
	return "", errors.New("invalid schedule")
}
