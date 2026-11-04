package scripts

import (
	"testing"
	"time"
)

func TestCronParsing(t *testing.T) {
	for _, bad := range []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8", "5-1 * * * *", "*/0 * * * *", "a * * * *", "* * * * * *"} {
		if _, err := parseCron(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	at := func(s string) time.Time {
		v, err := time.Parse("2006-01-02 15:04", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	cases := []struct {
		expr string
		when string
		want bool
	}{
		{"* * * * *", "2026-11-04 10:17", true},
		{"*/15 * * * *", "2026-11-04 10:45", true},
		{"*/15 * * * *", "2026-11-04 10:46", false},
		{"5/20 * * * *", "2026-11-04 10:25", true},
		{"0 3 * * *", "2026-11-04 03:00", true},
		{"0 3 * * *", "2026-11-04 04:00", false},
		{"30 2 * * 1-5", "2026-11-04 02:30", true}, // Wednesday
		{"30 2 * * 0", "2026-11-08 02:30", true},   // Sunday
		{"30 2 * * 7", "2026-11-08 02:30", true},
		{"0 0 1,15 * *", "2026-11-15 00:00", true},
		{"0 0 * 2 *", "2026-11-01 00:00", false},
		// Both day fields restricted: either one is enough.
		{"0 3 1 * 1", "2026-11-02 03:00", true}, // a Monday, not the first
		{"0 3 1 * 1", "2026-11-01 03:00", true}, // the first, a Sunday
		{"0 3 1 * 1", "2026-11-03 03:00", false},
	}
	for _, c := range cases {
		parsed, err := parseCron(c.expr)
		if err != nil {
			t.Fatalf("%q: %v", c.expr, err)
		}
		if got := parsed.matches(at(c.when)); got != c.want {
			t.Errorf("%q at %s = %v", c.expr, c.when, got)
		}
	}
	parsed, _ := parseCron("0 3 * * *")
	if next := parsed.next(at("2026-11-04 03:00")); next == nil || !next.Equal(at("2026-11-05 03:00")) {
		t.Errorf("next = %v", next)
	}
	never, _ := parseCron("0 0 30 2 *")
	if next := never.next(at("2026-11-04 03:00")); next != nil {
		t.Errorf("30 February = %v", next)
	}
}

func TestScheduleExpressions(t *testing.T) {
	cases := map[string]Schedule{
		"7 * * * *":     {Every: "hourly", Minute: 7},
		"15 3 * * *":    {Every: "daily", Time: "03:15"},
		"0 22 * * 5":    {Every: "weekly", Time: "22:00", Weekday: 5},
		"30 1 28 * *":   {Every: "monthly", Time: "01:30", MonthDay: 28},
		"*/5 * * * 1-5": {Every: "custom", Cron: "  */5 *  * * 1-5 "},
	}
	for want, s := range cases {
		if got, err := s.Expression(); err != nil || got != want {
			t.Errorf("%+v = %q, %v", s, got, err)
		}
	}
	for _, bad := range []Schedule{{Every: "hourly", Minute: 60}, {Every: "daily", Time: "3:15"}, {Every: "weekly", Time: "03:00", Weekday: 7}, {Every: "monthly", Time: "03:00", MonthDay: 31}, {Every: "custom", Cron: "x"}, {Every: "yearly"}} {
		if _, err := bad.Expression(); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
}

func TestCronLastDayOfTheMonth(t *testing.T) {
	c, err := parseCron("0 3 L * *")
	if err != nil {
		t.Fatal(err)
	}
	for when, want := range map[string]bool{"2026-02-28 03:00": true, "2028-02-28 03:00": false, "2028-02-29 03:00": true, "2026-11-30 03:00": true, "2026-12-30 03:00": false, "2026-12-31 03:00": true} {
		v, _ := time.Parse("2006-01-02 15:04", when)
		if c.matches(v) != want {
			t.Errorf("%s = %v", when, !want)
		}
	}
	both, err := parseCron("0 3 1,L * *")
	if err != nil {
		t.Fatal(err)
	}
	first, _ := time.Parse("2006-01-02 15:04", "2026-11-01 03:00")
	last, _ := time.Parse("2006-01-02 15:04", "2026-11-30 03:00")
	if !both.matches(first) || !both.matches(last) {
		t.Error("1,L")
	}
	for _, bad := range []string{"0 3 LL * *", "0 3 L-2 * *", "0 3 * * L", "0 3 , * *", "0 3 L, * *"} {
		if _, err := parseCron(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
