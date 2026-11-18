package recur

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/teambition/rrule-go"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

func berlin(t testing.TB) *time.Location {
	t.Helper()
	l, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// ics wraps VEVENT lines (one string per line) into a calendar.
func ics(events ...string) []byte {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//TitanOS//Test//EN\r\n")
	for _, e := range events {
		b.WriteString("BEGIN:VEVENT\r\n")
		for _, l := range strings.Split(strings.TrimSpace(e), "\n") {
			b.WriteString(strings.TrimSpace(l) + "\r\n")
		}
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return []byte(b.String())
}

func read(t testing.TB, b []byte) *File {
	t.Helper()
	f, err := Read(b, Options{Local: berlin(t), Account: "anna", Name: "test.ics"})
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func day(t testing.TB, y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, berlin(t))
}

// starts expands the only series of b and formats each start in Berlin.
func starts(t testing.TB, b []byte, from, to time.Time) []string {
	t.Helper()
	f := read(t, b)
	if len(f.Series) != 1 {
		t.Fatalf("%d series", len(f.Series))
	}
	x, err := f.Series[0].Expand(from, to)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, o := range x.Occurrences {
		out = append(out, o.Start.In(berlin(t)).Format("2006-01-02 15:04"))
	}
	return out
}

func equal(t testing.TB, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("got  %v\nwant %v", got, want)
	}
}

func TestDSTKeepsWallClock(t *testing.T) {
	b := ics(`UID:dst
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20260327T090000
DTEND;TZID=Europe/Berlin:20260327T100000
RRULE:FREQ=DAILY;COUNT=4`)
	equal(t, starts(t, b, day(t, 2026, 3, 1), day(t, 2026, 4, 1)),
		"2026-03-27 09:00", "2026-03-28 09:00", "2026-03-29 09:00", "2026-03-30 09:00")
	f := read(t, b)
	x, _ := f.Series[0].Expand(day(t, 2026, 3, 1), day(t, 2026, 4, 1))
	if x.Occurrences[1].Start.UTC().Hour() != 8 || x.Occurrences[2].Start.UTC().Hour() != 7 {
		t.Errorf("UTC hours: %v %v", x.Occurrences[1].Start.UTC(), x.Occurrences[2].Start.UTC())
	}
	if d := x.Occurrences[2].End.Sub(x.Occurrences[2].Start); d != time.Hour {
		t.Errorf("duration on the change day: %v", d)
	}

	autumn := ics(`UID:dst2
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20261013T090000
DURATION:PT1H
RRULE:FREQ=WEEKLY;BYDAY=TU`)
	equal(t, starts(t, autumn, day(t, 2026, 10, 1), day(t, 2026, 11, 4)),
		"2026-10-13 09:00", "2026-10-20 09:00", "2026-10-27 09:00", "2026-11-03 09:00")
}

func TestAllDayAcrossDST(t *testing.T) {
	b := ics(`UID:allday
DTSTAMP:20260101T000000Z
DTSTART;VALUE=DATE:20260323
DTEND;VALUE=DATE:20260324
RRULE:FREQ=WEEKLY`)
	f := read(t, b)
	x, err := f.Series[0].Expand(day(t, 2026, 3, 20), day(t, 2026, 4, 7))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range x.Occurrences {
		if !o.AllDay {
			t.Error("not all-day")
		}
		got = append(got, o.Start.Format("2006-01-02T15:04Z07:00")+"/"+o.End.Format("2006-01-02T15:04Z07:00"))
	}
	equal(t, got,
		"2026-03-23T00:00+01:00/2026-03-24T00:00+01:00",
		"2026-03-30T00:00+02:00/2026-03-31T00:00+02:00",
		"2026-04-06T00:00+02:00/2026-04-07T00:00+02:00")
	if id := x.Occurrences[1].ID(); id != "20260330" {
		t.Errorf("id %s", id)
	}
	// Two-day event over the change keeps two dates.
	two := ics(`UID:two
DTSTAMP:20260101T000000Z
DTSTART;VALUE=DATE:20260328
DTEND;VALUE=DATE:20260330`)
	f = read(t, two)
	x, _ = f.Series[0].Expand(day(t, 2026, 3, 1), day(t, 2026, 4, 1))
	if len(x.Occurrences) != 1 || x.Occurrences[0].End.Format("01-02 15:04") != "03-30 00:00" {
		t.Errorf("two-day: %+v", x.Occurrences)
	}
}

func TestMonthlyAndYearlyRules(t *testing.T) {
	cases := []struct {
		start, rule string
		from, to    time.Time
		want        []string
	}{
		{"20260120T190000", "FREQ=MONTHLY;BYDAY=3TU", day(t, 2026, 1, 1), day(t, 2027, 1, 1), []string{
			"2026-01-20", "2026-02-17", "2026-03-17", "2026-04-21", "2026-05-19", "2026-06-16",
			"2026-07-21", "2026-08-18", "2026-09-15", "2026-10-20", "2026-11-17", "2026-12-15"}},
		{"20260131T080000", "FREQ=MONTHLY;BYMONTHDAY=-1;COUNT=5", day(t, 2026, 1, 1), day(t, 2027, 1, 1), []string{
			"2026-01-31", "2026-02-28", "2026-03-31", "2026-04-30", "2026-05-31"}},
		{"20260130T080000", "FREQ=MONTHLY;BYDAY=-1FR;UNTIL=20260501T000000Z", day(t, 2026, 1, 1), day(t, 2027, 1, 1), []string{
			"2026-01-30", "2026-02-27", "2026-03-27", "2026-04-24"}},
		{"20240229T120000", "FREQ=YEARLY", day(t, 2027, 6, 1), day(t, 2028, 6, 1), []string{"2028-02-29"}},
		{"20240229T120000", "FREQ=YEARLY", day(t, 2025, 1, 1), day(t, 2026, 1, 1), nil},
	}
	for _, c := range cases {
		b := ics(fmt.Sprintf("UID:m\nDTSTAMP:20260101T000000Z\nDTSTART;TZID=Europe/Berlin:%s\nRRULE:%s", c.start, c.rule))
		var dates []string
		for _, s := range starts(t, b, c.from, c.to) {
			dates = append(dates, s[:10])
		}
		if strings.Join(dates, ",") != strings.Join(c.want, ",") {
			t.Errorf("%s: got %v, want %v", c.rule, dates, c.want)
		}
	}
}

func TestExdates(t *testing.T) {
	b := ics(`UID:ex
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20261103T090000
RRULE:FREQ=WEEKLY;COUNT=6
EXDATE;TZID=Europe/Berlin:20261110T090000,20261117T090000
EXDATE:20261201T080000Z`)
	equal(t, starts(t, b, day(t, 2026, 11, 1), day(t, 2027, 1, 1)),
		"2026-11-03 09:00", "2026-11-24 09:00", "2026-12-08 09:00")

	dates := ics(`UID:exd
DTSTAMP:20260101T000000Z
DTSTART;VALUE=DATE:20261102
RRULE:FREQ=DAILY;COUNT=5
EXDATE;VALUE=DATE:20261103,20261105`)
	equal(t, starts(t, dates, day(t, 2026, 11, 1), day(t, 2026, 12, 1)),
		"2026-11-02 00:00", "2026-11-04 00:00", "2026-11-06 00:00")

	// A date EXDATE on a timed series removes that day.
	mixed := ics(`UID:exm
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20261102T090000
RRULE:FREQ=DAILY;COUNT=3
EXDATE;VALUE=DATE:20261103`)
	equal(t, starts(t, mixed, day(t, 2026, 11, 1), day(t, 2026, 12, 1)),
		"2026-11-02 09:00", "2026-11-04 09:00")
}

func TestRDates(t *testing.T) {
	b := ics(`UID:rd
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20261102T090000
DTEND;TZID=Europe/Berlin:20261102T100000
RRULE:FREQ=WEEKLY;COUNT=2
RDATE;TZID=Europe/Berlin:20261105T140000,20261102T090000
RDATE;VALUE=PERIOD:20261120T100000Z/PT3H`)
	f := read(t, b)
	x, err := f.Series[0].Expand(day(t, 2026, 11, 1), day(t, 2026, 12, 1))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range x.Occurrences {
		got = append(got, o.Start.In(berlin(t)).Format("01-02 15:04")+"-"+o.End.In(berlin(t)).Format("15:04"))
	}
	equal(t, got, "11-02 09:00-10:00", "11-05 14:00-15:00", "11-09 09:00-10:00", "11-20 11:00-14:00")
}

func TestOverrides(t *testing.T) {
	master := `UID:ov
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20261102T090000
DTEND;TZID=Europe/Berlin:20261102T093000
SUMMARY:Weekly
RRULE:FREQ=WEEKLY;COUNT=4`
	moved := `UID:ov
DTSTAMP:20260101T000000Z
RECURRENCE-ID;TZID=Europe/Berlin:20261109T090000
DTSTART;TZID=Europe/Berlin:20261109T110000
DTEND;TZID=Europe/Berlin:20261109T120000
SUMMARY:Moved later`
	otherDay := `UID:ov
DTSTAMP:20260101T000000Z
RECURRENCE-ID:20261116T080000Z
DTSTART;TZID=Europe/Berlin:20261119T090000
DTEND;TZID=Europe/Berlin:20261119T093000
SUMMARY:Other day`
	outside := `UID:ov
DTSTAMP:20260101T000000Z
RECURRENCE-ID;TZID=Europe/Berlin:20261104T090000
DTSTART;TZID=Europe/Berlin:20261104T090000
DTEND;TZID=Europe/Berlin:20261104T093000
SUMMARY:Not in the series`
	f := read(t, ics(master, moved, otherDay, outside))
	x, err := f.Series[0].Expand(day(t, 2026, 11, 1), day(t, 2026, 12, 1))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, o := range x.Occurrences {
		s, _ := o.Event.Component.Props.Text("SUMMARY")
		got = append(got, fmt.Sprintf("%s %s override=%v orphan=%v id=%s", o.Start.In(berlin(t)).Format("01-02 15:04"), s, o.Override, o.Orphan, o.ID()))
	}
	equal(t, got,
		"11-02 09:00 Weekly override=false orphan=false id=20261102T080000Z",
		"11-04 09:00 Not in the series override=true orphan=true id=20261104T080000Z",
		"11-09 11:00 Moved later override=true orphan=false id=20261109T080000Z",
		"11-19 09:00 Other day override=true orphan=false id=20261116T080000Z",
		"11-23 09:00 Weekly override=false orphan=false id=20261123T080000Z")

	// The window only holds the original slot of a moved occurrence: it
	// is gone there; the window of its new slot holds it.
	equal(t, starts(t, ics(master, otherDay), day(t, 2026, 11, 16), day(t, 2026, 11, 17)))
	equal(t, starts(t, ics(master, otherDay), day(t, 2026, 11, 19), day(t, 2026, 11, 20)), "2026-11-19 09:00")
}

func TestThisAndFutureRange(t *testing.T) {
	b := ics(`UID:tf
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20261102T090000
DTEND;TZID=Europe/Berlin:20261102T093000
RRULE:FREQ=WEEKLY;COUNT=4`, `UID:tf
DTSTAMP:20260101T000000Z
RECURRENCE-ID;RANGE=THISANDFUTURE;TZID=Europe/Berlin:20261116T090000
DTSTART;TZID=Europe/Berlin:20261116T100000
DTEND;TZID=Europe/Berlin:20261116T110000`)
	f := read(t, b)
	x, _ := f.Series[0].Expand(day(t, 2026, 11, 1), day(t, 2026, 12, 1))
	var got []string
	for _, o := range x.Occurrences {
		got = append(got, o.Start.In(berlin(t)).Format("01-02 15:04")+"-"+o.End.In(berlin(t)).Format("15:04"))
	}
	equal(t, got, "11-02 09:00-09:30", "11-09 09:00-09:30", "11-16 10:00-11:00", "11-23 10:00-11:00")
}

func TestZonesInSeries(t *testing.T) {
	win := ics(`UID:win
DTSTAMP:20260101T000000Z
DTSTART;TZID=W. Europe Standard Time:20261023T090000
RRULE:FREQ=DAILY;COUNT=3`)
	equal(t, starts(t, win, day(t, 2026, 10, 1), day(t, 2026, 11, 1)),
		"2026-10-23 09:00", "2026-10-24 09:00", "2026-10-25 09:00")

	vtz := []byte(strings.ReplaceAll(`BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//Example//Exchange//EN
BEGIN:VTIMEZONE
TZID:Customized Time Zone
BEGIN:STANDARD
DTSTART:16010101T030000
TZOFFSETFROM:+0200
TZOFFSETTO:+0100
RRULE:FREQ=YEARLY;BYDAY=-1SU;BYMONTH=10
END:STANDARD
BEGIN:DAYLIGHT
DTSTART:16010101T020000
TZOFFSETFROM:+0100
TZOFFSETTO:+0200
RRULE:FREQ=YEARLY;BYDAY=-1SU;BYMONTH=3
END:DAYLIGHT
END:VTIMEZONE
BEGIN:VEVENT
UID:vtz
DTSTAMP:20260101T000000Z
DTSTART;TZID=Customized Time Zone:20261023T090000
RRULE:FREQ=DAILY;COUNT=3
END:VEVENT
END:VCALENDAR
`, "\n", "\r\n"))
	f := read(t, vtz)
	if f.Series[0].Master.TZGuess {
		t.Error("matched zone marked as guess")
	}
	equal(t, starts(t, vtz, day(t, 2026, 10, 1), day(t, 2026, 11, 1)),
		"2026-10-23 09:00", "2026-10-24 09:00", "2026-10-25 09:00")

	floating := ics(`UID:fl
DTSTAMP:20260101T000000Z
DTSTART:20260328T090000
RRULE:FREQ=DAILY;COUNT=2`)
	equal(t, starts(t, floating, day(t, 2026, 3, 1), day(t, 2026, 4, 1)), "2026-03-28 09:00", "2026-03-29 09:00")

	utc := ics(`UID:utc
DTSTAMP:20260101T000000Z
DTSTART:20260328T080000Z
RRULE:FREQ=DAILY;COUNT=2`)
	equal(t, starts(t, utc, day(t, 2026, 3, 1), day(t, 2026, 4, 1)), "2026-03-28 09:00", "2026-03-29 10:00")
}

func TestUntilForms(t *testing.T) {
	cases := map[string][]string{
		// UTC UNTIL on a zoned series, exactly the last start.
		"DTSTART;TZID=Europe/Berlin:20261102T090000\nRRULE:FREQ=DAILY;UNTIL=20261104T080000Z": {"11-02", "11-03", "11-04"},
		// Floating UNTIL (not RFC, but written) read in the zone of DTSTART.
		"DTSTART;TZID=Europe/Berlin:20261102T090000\nRRULE:FREQ=DAILY;UNTIL=20261103T090000": {"11-02", "11-03"},
		// Date UNTIL on a timed series includes that day.
		"DTSTART;TZID=Europe/Berlin:20261102T090000\nRRULE:FREQ=DAILY;UNTIL=20261103": {"11-02", "11-03"},
		// Date series with date UNTIL.
		"DTSTART;VALUE=DATE:20261102\nRRULE:FREQ=DAILY;UNTIL=20261104": {"11-02", "11-03", "11-04"},
		// Date series with a UTC UNTIL at local midnight (Google).
		"DTSTART;VALUE=DATE:20261102\nRRULE:FREQ=DAILY;UNTIL=20261103T230000Z": {"11-02", "11-03", "11-04"},
	}
	for ev, want := range cases {
		var got []string
		for _, s := range starts(t, ics("UID:u\nDTSTAMP:20260101T000000Z\n"+ev), day(t, 2026, 11, 1), day(t, 2026, 12, 1)) {
			got = append(got, s[5:10])
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%q: got %v want %v", ev, got, want)
		}
	}
}

// brokenRules are the rules of rrule-go issues #72, #75, #76, #77, the two
// panics found in the trial, and rules that never match (rrule-go scans
// them to the year 9999).
var brokenRules = []string{
	"FREQ=MONTHLY;COUNT=2;BYDAY=1MO,8MO",
	"FREQ=MONTHLY;COUNT=2;BYDAY=-8MO",
	"FREQ=YEARLY;COUNT=2;BYEASTER=400",
	"FREQ=YEARLY;COUNT=3;BYEASTER=-400",
	"FREQ=DAILY;INTERVAL=0",
	"FREQ=HOURLY;INTERVAL=2;BYHOUR=1",
	"FREQ=MINUTELY;INTERVAL=2;BYMINUTE=1",
	"FREQ=SECONDLY;INTERVAL=2;BYSECOND=1",
	"FREQ=YEARLY;INTERVAL=100000000",
	"FREQ=DAILY;BYHOUR=10",
	"FREQ=DAILY;BYMONTH=2;BYMONTHDAY=30",
	"FREQ=WEEKLY;BYMONTH=4;BYDAY=MO;BYSETPOS=6",
	"FREQ=YEARLY;INTERVAL=4;BYMONTH=2;BYMONTHDAY=29",
	"FREQ=MONTHLY;BYDAY=5MO;BYMONTHDAY=1",
	"FREQ=MONTHLY;BYMONTHDAY=1;BYSETPOS=2",
	"FREQ=YEARLY;BYYEARDAY=1;BYMONTH=6",
	"FREQ=DAILY;COUNT=2;UNTIL=20270101T000000Z",
	"FREQ=FORTNIGHTLY",
	"FREQ=DAILY;X-NAME=1",
	"FREQ=WEEKLY;BYMONTHDAY=3",
	"FREQ=DAILY;COUNT=5001",
	"INTERVAL=2",
	"",
}

func TestBrokenRulesShowOnce(t *testing.T) {
	for _, rule := range brokenRules {
		b := ics("UID:b\nDTSTAMP:20260101T000000Z\nDTSTART;TZID=Europe/Berlin:20270101T090000\nRRULE:" + rule)
		if rule == "" {
			b = ics("UID:b\nDTSTAMP:20260101T000000Z\nDTSTART;TZID=Europe/Berlin:20270101T090000\nRRULE:FREQ")
		}
		start := time.Now()
		f := read(t, b)
		m := f.Series[0].Master
		if !m.Unsupported || m.Rule != nil {
			t.Errorf("%q: not unsupported", rule)
			continue
		}
		x, err := f.Series[0].Expand(day(t, 2026, 12, 1), day(t, 2027, 12, 1))
		if err != nil || !x.Unsupported || len(x.Occurrences) != 1 {
			t.Errorf("%q: %+v %v", rule, x, err)
		}
		if d := time.Since(start); d > 200*time.Millisecond {
			t.Errorf("%q took %v", rule, d)
		}
	}
	// The same rules handed to rrule-go directly, under its own recover,
	// prove that the whitelist is what keeps them away.
	for _, rule := range []string{"FREQ=MONTHLY;COUNT=2;BYDAY=1MO,8MO", "FREQ=YEARLY;COUNT=2;BYEASTER=400"} {
		panicked := func() (p bool) {
			defer func() { p = recover() != nil }()
			r, err := rrule.StrToRRule("DTSTART:19971202T090000Z\nRRULE:" + rule)
			if err == nil {
				r.All()
			}
			return false
		}()
		if !panicked {
			t.Logf("%s no longer panics in rrule-go", rule)
		}
	}
}

func TestRuleCheckImpossibleIsFast(t *testing.T) {
	for _, rule := range []string{
		"FREQ=DAILY;BYMONTH=2;BYMONTHDAY=30",
		"FREQ=MONTHLY;BYDAY=5MO;BYMONTHDAY=1",
		"FREQ=YEARLY;INTERVAL=4;BYMONTH=2;BYMONTHDAY=29",
		"FREQ=MONTHLY;BYMONTHDAY=1;BYSETPOS=2",
		"FREQ=YEARLY;INTERVAL=2;BYWEEKNO=53",
	} {
		r, err := Parse(rule)
		if err != nil {
			t.Fatal(rule, err)
		}
		start := time.Date(2027, 1, 1, 9, 0, 0, 0, time.UTC)
		begin := time.Now()
		err = r.Check(dtValue(start), start)
		if rule == "FREQ=YEARLY;INTERVAL=2;BYWEEKNO=53" {
			// 2027+2n reaches 2032? No: 2026 and 2032 have 53 ISO weeks,
			// 2027+2n gives odd years only, e.g. 2037 has 53.
			if err != nil {
				t.Errorf("%s: %v", rule, err)
			}
		} else if !errors.Is(err, ErrUnsupported) {
			t.Errorf("%s: %v", rule, err)
		}
		if d := time.Since(begin); d > 50*time.Millisecond {
			t.Errorf("%s: check took %v", rule, d)
		}
	}
	// Valid rules pass.
	for _, rule := range []string{
		"FREQ=YEARLY;INTERVAL=4;BYMONTH=2;BYMONTHDAY=29",
		"FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-1",
		"FREQ=YEARLY;BYMONTH=5;BYDAY=2SU",
		"FREQ=YEARLY;BYYEARDAY=366",
		"FREQ=YEARLY;BYWEEKNO=20;BYDAY=MO",
		"FREQ=DAILY;BYDAY=MO,TU,WE,TH,FR",
		"FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE;WKST=SU",
	} {
		r, err := Parse(rule)
		if err != nil {
			t.Fatal(rule, err)
		}
		start := time.Date(2028, 2, 29, 9, 0, 0, 0, time.UTC)
		if err := r.Check(dtValue(start), start); err != nil {
			t.Errorf("%s: %v", rule, err)
		}
	}
}

func TestPanicInExpansionIsContained(t *testing.T) {
	old := newIterator
	defer func() { newIterator = old }()
	newIterator = func(rrule.ROption) (func() (time.Time, bool), error) {
		return func() (time.Time, bool) { panic("index out of range") }, nil
	}
	f := read(t, ics(`UID:p
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20261102T090000
RRULE:FREQ=DAILY`))
	x, err := f.Series[0].Expand(day(t, 2026, 11, 1), day(t, 2026, 12, 1))
	if err != nil {
		t.Fatal(err)
	}
	if !x.Failed || !x.Unsupported || len(x.Occurrences) != 1 {
		t.Errorf("%+v", x)
	}
	if !f.Series[0].Overlaps(day(t, 2026, 11, 1), time.Time{}) {
		t.Error("overlap after a panic")
	}
}

func TestOverlapSemantics(t *testing.T) {
	endless := read(t, ics(`UID:e
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20261102T090000
DTEND;TZID=Europe/Berlin:20261102T100000
RRULE:FREQ=WEEKLY`)).Series[0]
	if !endless.Overlaps(day(t, 2150, 1, 1), time.Time{}) {
		t.Error("endless series must match an open range far ahead")
	}
	if !endless.Overlaps(time.Time{}, time.Time{}) {
		t.Error("fully open range")
	}
	// 09:00-10:00: a range ending at 09:00 does not overlap, one starting
	// 09:59 does.
	at := time.Date(2026, 11, 9, 9, 0, 0, 0, berlin(t))
	if endless.Overlaps(at.Add(-time.Hour), at) {
		t.Error("range ending at the start overlaps")
	}
	if !endless.Overlaps(at.Add(59*time.Minute), at.Add(2*time.Hour)) {
		t.Error("range in the occurrence does not overlap")
	}
	ended := read(t, ics(`UID:e
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:20261102T090000
RRULE:FREQ=WEEKLY;UNTIL=20261201T000000Z`)).Series[0]
	if ended.Overlaps(day(t, 2027, 1, 1), time.Time{}) {
		t.Error("ended series matches a later open range")
	}
	// Zero length: matches when the start is in [from, to).
	if !ended.Overlaps(at, at.Add(time.Minute)) || ended.Overlaps(at.Add(-time.Minute), at) {
		t.Error("zero-length occurrence")
	}
}

func TestWindowAndCaps(t *testing.T) {
	f := read(t, ics(`UID:w
DTSTAMP:20260101T000000Z
DTSTART;TZID=Europe/Berlin:19000101T090000
RRULE:FREQ=DAILY`))
	if _, err := f.Series[0].Expand(day(t, 2026, 1, 1), day(t, 2027, 2, 6)); !errors.Is(err, ErrWindow) {
		t.Errorf("401 days: %v", err)
	}
	if _, err := f.Series[0].Expand(day(t, 2026, 1, 1), day(t, 2026, 1, 1)); !errors.Is(err, ErrWindow) {
		t.Errorf("empty window: %v", err)
	}
	// A daily series from 1900 reaches 2150 within the step cap (rrule-go
	// itself ends a rule without end about 292 years after DTSTART).
	x, err := f.Series[0].Expand(day(t, 2150, 1, 1), day(t, 2150, 1, 3))
	if err != nil || len(x.Occurrences) != 2 || x.Truncated {
		t.Errorf("2150: %d %v %v", len(x.Occurrences), x.Truncated, err)
	}
	old, oldCap := maxSteps, occurrenceCap
	defer func() { maxSteps, occurrenceCap = old, oldCap }()
	maxSteps = 1000
	x, _ = f.Series[0].Expand(day(t, 2026, 1, 1), day(t, 2026, 2, 1))
	if !x.Truncated || len(x.Occurrences) != 0 {
		t.Errorf("step cap: %d %v", len(x.Occurrences), x.Truncated)
	}
	maxSteps, occurrenceCap = old, 10
	x, _ = f.Series[0].Expand(day(t, 2026, 1, 1), day(t, 2026, 2, 1))
	if !x.Truncated || len(x.Occurrences) != 10 {
		t.Errorf("occurrence cap: %d %v", len(x.Occurrences), x.Truncated)
	}
}

func TestReadLimits(t *testing.T) {
	many := make([]string, MaxEvents+1)
	for i := range many {
		many[i] = fmt.Sprintf("UID:%d\nDTSTAMP:20260101T000000Z\nDTSTART:20261102T090000Z", i)
	}
	if _, err := Read(ics(many...), Options{}); !errors.Is(err, ErrLimit) {
		t.Errorf("too many events: %v", err)
	}
	if _, err := Read(ics(many[:MaxEvents]...), Options{}); err != nil {
		t.Errorf("%d events: %v", MaxEvents, err)
	}
	deep := "BEGIN:VCALENDAR\r\n" + strings.Repeat("BEGIN:X\r\n", 20) + strings.Repeat("END:X\r\n", 20) + "END:VCALENDAR\r\n"
	if _, err := Read([]byte(deep), Options{}); !errors.Is(err, ErrLimit) {
		t.Errorf("deep: %v", err)
	}
	// BEGIN hidden in a folded line still counts.
	folded := "BEGIN:VCALENDAR\r\n" + strings.Repeat("BEG\r\n IN:X\r\n", 20) + "END:VCALENDAR\r\n"
	if _, err := Read([]byte(folded), Options{}); !errors.Is(err, ErrLimit) {
		t.Errorf("folded deep: %v", err)
	}
	param := ics("UID:p\nDTSTAMP:20260101T000000Z\nDTSTART:20261102T090000Z\nX-A;X-P=" + strings.Repeat("a", 9000) + ":v")
	if _, err := Read(param, Options{}); !errors.Is(err, ErrLimit) {
		t.Errorf("long param: %v", err)
	}
	ex := make([]string, MaxDates+1)
	for i := range ex {
		ex[i] = time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC).AddDate(0, 0, i).Format("20060102T150405Z")
	}
	exd := ics("UID:x\nDTSTAMP:20260101T000000Z\nDTSTART:20260101T090000Z\nRRULE:FREQ=DAILY\nEXDATE:" + strings.Join(ex[:600], ",") + "\nEXDATE:" + strings.Join(ex[600:], ","))
	if _, err := Read(exd, Options{}); !errors.Is(err, ErrLimit) {
		t.Errorf("exdates: %v", err)
	}
	if _, err := Read(make([]byte, MaxSize+1), Options{}); !errors.Is(err, ErrLimit) {
		t.Errorf("size: %v", err)
	}
	for name, b := range map[string]string{
		"no uid":     "DTSTAMP:20260101T000000Z\nDTSTART:20261102T090000Z",
		"no dtstart": "UID:a\nDTSTAMP:20260101T000000Z",
		"bad exdate": "UID:a\nDTSTAMP:20260101T000000Z\nDTSTART:20261102T090000Z\nEXDATE:2026",
		"end+dur":    "UID:a\nDTSTAMP:20260101T000000Z\nDTSTART:20261102T090000Z\nDTEND:20261102T100000Z\nDURATION:PT1H",
	} {
		if _, err := Read(ics(b), Options{}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := Read(ics("UID:a\nDTSTART:20261102T090000Z", "UID:a\nDTSTART:20261103T090000Z"), Options{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("two masters: %v", err)
	}
	if _, err := Read([]byte("garbage"), Options{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("garbage: %v", err)
	}
}

// paddedZone is a VTIMEZONE "Pad/Zone" of about n bytes (X-PAD lines).
func paddedZone(n int) string {
	var b strings.Builder
	b.WriteString("BEGIN:VTIMEZONE\r\nTZID:Pad/Zone\r\nBEGIN:STANDARD\r\nDTSTART:16010101T000000\r\nTZOFFSETFROM:+0100\r\nTZOFFSETTO:+0100\r\nEND:STANDARD\r\n")
	for b.Len()+68+16 <= n { // a pad line, the END line
		b.WriteString("X-PAD:" + strings.Repeat("x", 60) + "\r\n")
	}
	b.WriteString("END:VTIMEZONE\r\n")
	return b.String()
}

// One VTIMEZONE may have MaxZoneBytes when MaxZone asks for it (the
// checks of a write); a stored file is read without the bound (S-21).
func TestReadZoneLimit(t *testing.T) {
	file := func(zone string) []byte {
		return []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\n" + zone +
			"BEGIN:VEVENT\r\nUID:z\r\nDTSTAMP:20260101T000000Z\r\nDTSTART;TZID=Pad/Zone:20261102T090000\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	}
	small, big := paddedZone(MaxZoneBytes-100), paddedZone(MaxZoneBytes+100)
	if len(small) > MaxZoneBytes || len(big) <= MaxZoneBytes {
		t.Fatalf("zones of %d and %d bytes", len(small), len(big))
	}
	if _, err := Read(file(small), Options{MaxZone: MaxZoneBytes}); err != nil {
		t.Errorf("zone of %d bytes: %v", len(small), err)
	}
	if _, err := Read(file(big), Options{MaxZone: MaxZoneBytes}); !errors.Is(err, ErrLimit) {
		t.Errorf("zone of %d bytes: %v", len(big), err)
	}
	// Folded and with LF only, the zone is counted as written.
	lf := strings.ReplaceAll(strings.ReplaceAll(big, "X-PAD:xxxxx", "X-PAD:xx\r\n xxx"), "\r\n", "\n")
	if _, err := Read(file(lf), Options{MaxZone: MaxZoneBytes}); !errors.Is(err, ErrLimit) {
		t.Errorf("folded zone: %v", err)
	}
	if _, err := Read(file(big), Options{}); err != nil {
		t.Errorf("stored file with a big zone: %v", err)
	}
}

func dtValue(t time.Time) tz.Value { return tz.Value{Time: t, Kind: tz.KindUTC} }

func TestPrescanSeesParameterisedBegin(t *testing.T) {
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:x\r\n")
	for i := range MaxEvents + 1 {
		fmt.Fprintf(&b, "BEGIN;X=1:VEVENT\r\nUID:%d\r\nDTSTART:20261102T090000Z\r\nEND;X=1:VEVENT\r\n", i)
	}
	b.WriteString("END:VCALENDAR\r\n")
	if _, err := Read([]byte(b.String()), Options{}); !errors.Is(err, ErrLimit) {
		t.Errorf("BEGIN;X=1:VEVENT: %v", err)
	}
	deep := "BEGIN:VCALENDAR\r\n" + strings.Repeat("BEGIN;X=\"a:b\":X\r\n", 20) + strings.Repeat("END:X\r\n", 20) + "END:VCALENDAR\r\n"
	if _, err := Read([]byte(deep), Options{}); !errors.Is(err, ErrLimit) {
		t.Errorf("nested BEGIN;X=…: %v", err)
	}
	lower := "BEGIN:VCALENDAR\r\n" + strings.Repeat("begin:x\r\n", 20) + strings.Repeat("end:x\r\n", 20) + "END:VCALENDAR\r\n"
	if _, err := Read([]byte(lower), Options{}); !errors.Is(err, ErrLimit) {
		t.Errorf("lower-case begin: %v", err)
	}
}

// Many overrides of one long series: orphan detection walks the master
// once, not once per override.
func TestManyOverridesStayCheap(t *testing.T) {
	events := []string{"UID:big\nDTSTAMP:20260101T000000Z\nDTSTART;TZID=Europe/Berlin:19500101T090000\nRRULE:FREQ=DAILY"}
	for i := range MaxEvents - 1 {
		rid := time.Date(2190, 1, 1, 9, 0, 0, 0, berlin(t)).AddDate(0, 0, i)
		events = append(events, fmt.Sprintf("UID:big\nDTSTAMP:20260101T000000Z\nRECURRENCE-ID;TZID=Europe/Berlin:%s\nDTSTART;TZID=Europe/Berlin:20261110T120000\nDURATION:PT1H",
			rid.Format("20060102T150405")))
	}
	f := read(t, ics(events...))
	start := time.Now()
	x, err := f.Series[0].Expand(day(t, 2026, 11, 1), day(t, 2026, 12, 1))
	if err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("expansion took %v", d)
	}
	overrides := 0
	for _, o := range x.Occurrences {
		if o.Override {
			overrides++
			if o.Orphan {
				t.Fatal("override of a real occurrence marked orphan")
			}
		}
	}
	if overrides != MaxEvents-1 {
		t.Errorf("%d overrides shown", overrides)
	}
}
