package recur

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

func TestDescribe(t *testing.T) {
	loc := berlin(t)
	cases := []struct{ rule, de, en string }{
		{"FREQ=MONTHLY;BYDAY=3TU", "jeden Monat am dritten Dienstag", "every month on the third Tuesday"},
		{"FREQ=WEEKLY;BYDAY=MO,WE", "jede Woche am Montag und Mittwoch", "every week on Monday and Wednesday"},
		{"FREQ=WEEKLY;BYDAY=MO,WE,FR;COUNT=10", "jede Woche am Montag, Mittwoch und Freitag, endet nach 10 Terminen", "every week on Monday, Wednesday and Friday, ends after 10 occurrences"},
		{"FREQ=DAILY;UNTIL=20261231T225959Z", "jeden Tag, bis 31.12.2026", "every day, until December 31, 2026"},
		{"FREQ=DAILY;BYDAY=MO,TU,WE,TH,FR", "jeden Werktag", "every weekday"},
		{"FREQ=WEEKLY;INTERVAL=2", "alle 2 Wochen", "every 2 weeks"},
		{"FREQ=MONTHLY;BYMONTHDAY=-1", "jeden Monat am letzten Tag", "every month on the last day"},
		{"FREQ=MONTHLY;BYMONTHDAY=1,15", "jeden Monat am 1. und 15.", "every month on the 1st and 15th"},
		{"FREQ=MONTHLY;BYDAY=-1FR", "jeden Monat am letzten Freitag", "every month on the last Friday"},
		{"FREQ=YEARLY;BYMONTH=5;BYDAY=2SU", "jedes Jahr im Mai am zweiten Sonntag", "every year in May on the second Sunday"},
		{"FREQ=YEARLY;COUNT=1", "jedes Jahr, endet nach 1 Termin", "every year, ends after 1 occurrence"},
		{"FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1", "jeden Monat am Montag, Dienstag, Mittwoch, Donnerstag und Freitag (davon jeweils der letzte)", "every month on Monday, Tuesday, Wednesday, Thursday and Friday (the last of them)"},
		{"FREQ=YEARLY;BYWEEKNO=20;BYDAY=MO", "jedes Jahr in Kalenderwoche 20 am Montag", "every year in week 20 on Monday"},
		{"FREQ=YEARLY;BYYEARDAY=100", "jedes Jahr am 100. Tag des Jahres", "every year on day 100 of the year"},
	}
	for _, c := range cases {
		r, err := Parse(c.rule)
		if err != nil {
			t.Fatal(c.rule, err)
		}
		if got := r.Describe("de", loc); got != c.de {
			t.Errorf("%s de: %q, want %q", c.rule, got, c.de)
		}
		if got := r.Describe("en", loc); got != c.en {
			t.Errorf("%s en: %q, want %q", c.rule, got, c.en)
		}
	}
}

func TestSimpleForm(t *testing.T) {
	loc := berlin(t)
	cases := []struct {
		rule   string
		custom bool
		json   string
	}{
		{"FREQ=MONTHLY;BYDAY=3TU", false, `{"freq":"monthly","interval":1,"byDay":["3TU"]}`},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;UNTIL=20261231T225959Z", false, `{"freq":"weekly","interval":2,"byDay":["MO","WE"],"until":"2026-12-31"}`},
		{"FREQ=MONTHLY;BYMONTHDAY=-1;COUNT=6", false, `{"freq":"monthly","interval":1,"byMonthDay":[-1],"count":6}`},
		{"FREQ=YEARLY;BYMONTH=5;BYDAY=2SU", true, `{"freq":"yearly","interval":1,"byDay":["2SU"],"custom":true}`},
		{"FREQ=MONTHLY;BYDAY=MO,TU;BYSETPOS=1", true, ""},
		{"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;WKST=SU", true, ""},
		{"FREQ=WEEKLY;BYDAY=MO;WKST=SU", false, ""},
		{"FREQ=MONTHLY;BYDAY=MO", true, ""},
		{"FREQ=DAILY;BYDAY=MO,TU,WE,TH,FR", false, ""},
	}
	for _, c := range cases {
		r, err := Parse(c.rule)
		if err != nil {
			t.Fatal(c.rule, err)
		}
		s := r.Simple(loc)
		if s.Custom != c.custom {
			t.Errorf("%s: custom %v", c.rule, s.Custom)
		}
		if c.json != "" {
			b, _ := json.Marshal(s)
			if string(b) != c.json {
				t.Errorf("%s: %s, want %s", c.rule, b, c.json)
			}
		}
	}
}

func TestSimpleToRule(t *testing.T) {
	loc := berlin(t)
	zoned := tz.Value{Time: time.Date(2026, 11, 17, 19, 0, 0, 0, loc), Kind: tz.KindZoned, TZID: "Europe/Berlin"}
	date := tz.Date(2026, 11, 17)
	floating := tz.Value{Time: time.Date(2026, 11, 17, 19, 0, 0, 0, loc), Kind: tz.KindFloating}
	cases := []struct {
		s     Simple
		start tz.Value
		want  string
	}{
		{Simple{Freq: "monthly", Interval: 1, ByDay: []string{"3TU"}}, zoned, "FREQ=MONTHLY;BYDAY=3TU"},
		{Simple{Freq: "weekly", Interval: 2, ByDay: []string{"MO", "WE"}, Until: "2026-12-31"}, zoned, "FREQ=WEEKLY;UNTIL=20261231T225959Z;INTERVAL=2;BYDAY=MO,WE"},
		{Simple{Freq: "daily", Until: "2026-12-31"}, date, "FREQ=DAILY;UNTIL=20261231"},
		{Simple{Freq: "daily", Until: "2026-12-31"}, floating, "FREQ=DAILY;UNTIL=20261231T235959"},
		{Simple{Freq: "yearly", Count: 3}, date, "FREQ=YEARLY;COUNT=3"},
	}
	for _, c := range cases {
		r, err := c.s.Rule(c.start)
		if err != nil {
			t.Fatal(c.s, err)
		}
		if r.String() != c.want {
			t.Errorf("%+v: %s, want %s", c.s, r, c.want)
		}
		// Round trip through the simple form.
		back := r.Simple(c.start.Time.Location())
		if back.Until != c.s.Until || back.Count != c.s.Count || back.Custom {
			t.Errorf("%+v: back %+v", c.s, back)
		}
	}
	for _, bad := range []Simple{
		{Freq: "hourly"},
		{Freq: "daily", Interval: 1001},
		{Freq: "weekly", ByDay: []string{"3TU"}},
		{Freq: "weekly", ByMonthDay: []int{3}},
		{Freq: "monthly", ByDay: []string{"9TU"}},
		{Freq: "daily", Count: 2, Until: "2026-12-31"},
		{Freq: "daily", Until: "31.12.2026"},
		{Freq: "monthly", Custom: true},
	} {
		if _, err := bad.Rule(zoned); !errors.Is(err, ErrNotSimple) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
}
