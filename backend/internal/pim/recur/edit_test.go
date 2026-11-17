package recur

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

var editNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

func editOpt(t testing.TB) EditOptions {
	return EditOptions{Local: berlin(t), Now: editNow, NewUID: "new-uid"}
}

// weekly: Mondays 09:00-09:30 Berlin, six times from 2 November 2026, as
// iOS writes it (alarm, Apple properties).
func weekly(rule string, extra ...string) []byte {
	ev := `UID:series
DTSTAMP:20261101T100000Z
CREATED:20261101T100000Z
SEQUENCE:2
SUMMARY:Weekly
X-APPLE-TRAVEL-ADVISORY-BEHAVIOR:AUTOMATIC
DTSTART;TZID=Europe/Berlin:20261102T090000
DTEND;TZID=Europe/Berlin:20261102T093000
RRULE:` + rule
	events := append([]string{ev}, extra...)
	b := string(ics(events...))
	alarm := "BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Reminder\r\nTRIGGER:-PT15M\r\nX-WR-ALARMUID:A1\r\nEND:VALARM\r\n"
	return []byte(strings.Replace(b, "RRULE:"+rule+"\r\nEND:VEVENT", "RRULE:"+rule+"\r\n"+alarm+"END:VEVENT", 1))
}

// list expands every series of b in November and December 2026.
func list(t testing.TB, b []byte) []string {
	t.Helper()
	f := read(t, b)
	var out []string
	for _, s := range f.Series {
		x, err := s.Expand(day(t, 2026, 10, 1), day(t, 2027, 1, 1))
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range x.Occurrences {
			sum, _ := o.Event.Component.Props.Text(ical.PropSummary)
			out = append(out, fmt.Sprintf("%s %s", o.Start.In(berlin(t)).Format("01-02 15:04"), sum))
		}
	}
	return out
}

func setSummary(s string) Change {
	return func(c *ical.Component) error {
		c.Props.SetText(ical.PropSummary, s)
		return nil
	}
}

// moveBy shifts DTSTART and DTEND of the component by d.
func moveBy(t testing.TB, d time.Duration) Change {
	return func(c *ical.Component) error {
		r := tz.NewResolver(berlin(t), nil)
		for _, name := range []string{ical.PropDateTimeStart, ical.PropDateTimeEnd} {
			p := c.Props.Get(name)
			if p == nil {
				continue
			}
			v, err := r.Parse(p)
			if err != nil {
				return err
			}
			v.Time = v.Time.Add(d)
			v.SetProp(p)
		}
		return nil
	}
}

func component(t testing.TB, b []byte, uid string, rid string) *ical.Component {
	t.Helper()
	f := read(t, b)
	for _, s := range f.Series {
		if s.UID != uid {
			continue
		}
		if rid == "" {
			return s.Master.Component
		}
		for _, o := range s.Overrides {
			if OccurrenceID(*o.RecurrenceID) == rid {
				return o.Component
			}
		}
	}
	t.Fatalf("no component %s %s", uid, rid)
	return nil
}

func propValue(c *ical.Component, name string) string {
	var vs []string
	for _, p := range c.Props.Values(name) {
		s := p.Value
		if id := p.Params.Get("TZID"); id != "" {
			s = "TZID=" + id + ":" + s
		}
		if v := p.Params.Get("VALUE"); v != "" {
			s = "VALUE=" + v + ":" + s
		}
		vs = append(vs, s)
	}
	return strings.Join(vs, " ")
}

func TestThisOnly(t *testing.T) {
	b := weekly("FREQ=WEEKLY;COUNT=6")
	r, err := Update(b, "", "20261116T080000Z", ScopeThis, setSummary("Changed once"), editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	equal(t, list(t, r.Data), "11-02 09:00 Weekly", "11-09 09:00 Weekly", "11-16 09:00 Changed once",
		"11-23 09:00 Weekly", "11-30 09:00 Weekly", "12-07 09:00 Weekly")
	o := component(t, r.Data, "series", "20261116T080000Z")
	if got := propValue(o, "RECURRENCE-ID"); got != "TZID=Europe/Berlin:20261116T090000" {
		t.Errorf("RECURRENCE-ID %s", got)
	}
	if propValue(o, "SEQUENCE") != "3" || propValue(o, "DTSTAMP") != "20261117T120000Z" || propValue(o, "LAST-MODIFIED") != "20261117T120000Z" {
		t.Errorf("stamps: %s %s %s", propValue(o, "SEQUENCE"), propValue(o, "DTSTAMP"), propValue(o, "LAST-MODIFIED"))
	}
	if propValue(o, "RRULE") != "" || propValue(o, "DTEND") != "TZID=Europe/Berlin:20261116T093000" {
		t.Errorf("override keeps %s / %s", propValue(o, "RRULE"), propValue(o, "DTEND"))
	}
	if propValue(o, "X-APPLE-TRAVEL-ADVISORY-BEHAVIOR") != "AUTOMATIC" || len(o.Children) != 1 || o.Children[0].Name != "VALARM" {
		t.Error("override lost Apple properties or the alarm")
	}
	m := component(t, r.Data, "series", "")
	if propValue(m, "SEQUENCE") != "2" || len(m.Children) != 1 {
		t.Error("master changed")
	}

	// Again on the changed occurrence: the override changes, no second one.
	r2, err := Update(r.Data, "series", "20261116T080000Z", ScopeThis, setSummary("Changed twice"), editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	f := read(t, r2.Data)
	if n := len(f.Series[0].Overrides); n != 1 {
		t.Fatalf("%d overrides", n)
	}
	if s, _ := f.Series[0].Overrides[0].Component.Props.Text("SUMMARY"); s != "Changed twice" || propValue(f.Series[0].Overrides[0].Component, "SEQUENCE") != "4" {
		t.Errorf("override %q seq %s", s, propValue(f.Series[0].Overrides[0].Component, "SEQUENCE"))
	}

	// Delete that occurrence: override gone, EXDATE in DTSTART's form.
	r3, err := Delete(r2.Data, "series", "20261116T080000Z", ScopeThis, editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	equal(t, list(t, r3.Data), "11-02 09:00 Weekly", "11-09 09:00 Weekly", "11-23 09:00 Weekly", "11-30 09:00 Weekly", "12-07 09:00 Weekly")
	m = component(t, r3.Data, "series", "")
	if got := propValue(m, "EXDATE"); got != "TZID=Europe/Berlin:20261116T090000" {
		t.Errorf("EXDATE %s", got)
	}
	if propValue(m, "SEQUENCE") != "3" {
		t.Errorf("master sequence %s", propValue(m, "SEQUENCE"))
	}
	if len(read(t, r3.Data).Series[0].Overrides) != 0 {
		t.Error("override left")
	}

	// A date series gets a date EXDATE.
	d := ics(`UID:d
DTSTAMP:20261101T100000Z
DTSTART;VALUE=DATE:20261102
DTEND;VALUE=DATE:20261103
RRULE:FREQ=DAILY;COUNT=3`)
	r4, err := Delete(d, "", "20261103", ScopeThis, editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := propValue(component(t, r4.Data, "d", ""), "EXDATE"); got != "VALUE=DATE:20261103" {
		t.Errorf("date EXDATE %s", got)
	}
}

func TestThisAndFollowingAtCount(t *testing.T) {
	moved := `UID:series
DTSTAMP:20261101T100000Z
RECURRENCE-ID;TZID=Europe/Berlin:20261123T090000
DTSTART;TZID=Europe/Berlin:20261123T120000
DTEND;TZID=Europe/Berlin:20261123T123000
SUMMARY:Moved`
	before := `UID:series
DTSTAMP:20261101T100000Z
RECURRENCE-ID;TZID=Europe/Berlin:20261109T090000
DTSTART;TZID=Europe/Berlin:20261109T110000
DTEND;TZID=Europe/Berlin:20261109T113000
SUMMARY:Early move`
	b := weekly("FREQ=WEEKLY;COUNT=6", moved, before)
	b = []byte(strings.Replace(string(b), "RRULE:FREQ=WEEKLY;COUNT=6\r\n", "RRULE:FREQ=WEEKLY;COUNT=6\r\nEXDATE;TZID=Europe/Berlin:20261102T090000,20261130T090000\r\n", 1))

	r, err := Update(b, "series", "20261116T080000Z", ScopeFollowing, setSummary("New title"), editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.Scope != ScopeFollowing || r.NewUID != "new-uid" || r.DroppedExceptions != 0 {
		t.Errorf("result %+v", r)
	}
	old := component(t, r.Data, "series", "")
	if got := propValue(old, "RRULE"); got != "FREQ=WEEKLY;UNTIL=20261116T075959Z" {
		t.Errorf("old RRULE %s", got)
	}
	if got := propValue(old, "EXDATE"); got != "TZID=Europe/Berlin:20261102T090000" {
		t.Errorf("old EXDATE %s", got)
	}
	equal(t, list(t, r.Data), "11-09 11:00 Early move")
	n := component(t, r.New, "new-uid", "")
	// Six instances, two before the cut (one of them excluded): four left.
	if got := propValue(n, "RRULE"); got != "FREQ=WEEKLY;COUNT=4" {
		t.Errorf("new RRULE %s", got)
	}
	if propValue(n, "DTSTART") != "TZID=Europe/Berlin:20261116T090000" || propValue(n, "DTEND") != "TZID=Europe/Berlin:20261116T093000" {
		t.Errorf("new start %s end %s", propValue(n, "DTSTART"), propValue(n, "DTEND"))
	}
	if propValue(n, "EXDATE") != "TZID=Europe/Berlin:20261130T090000" || propValue(n, "SEQUENCE") != "" || propValue(n, "CREATED") != "20261117T120000Z" {
		t.Errorf("new EXDATE %s seq %s created %s", propValue(n, "EXDATE"), propValue(n, "SEQUENCE"), propValue(n, "CREATED"))
	}
	if len(n.Children) != 1 || propValue(n, "X-APPLE-TRAVEL-ADVISORY-BEHAVIOR") != "AUTOMATIC" {
		t.Error("new series lost alarm or Apple properties")
	}
	equal(t, list(t, r.New), "11-16 09:00 New title", "11-23 12:00 Moved", "12-07 09:00 New title")
	if !strings.Contains(string(r.New), "BEGIN:VTIMEZONE") && strings.Contains(string(b), "BEGIN:VTIMEZONE") {
		t.Error("VTIMEZONE not carried")
	}

	// Moving the time drops the exceptions after the cut and says so.
	r2, err := Update(b, "series", "20261116T080000Z", ScopeFollowing, moveBy(t, time.Hour), editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	if r2.DroppedExceptions != 2 {
		t.Errorf("dropped %d, want 2 (override 23.11., EXDATE 30.11.)", r2.DroppedExceptions)
	}
	equal(t, list(t, r2.New), "11-16 10:00 Weekly", "11-23 10:00 Weekly", "11-30 10:00 Weekly", "12-07 10:00 Weekly")

	// Delete this and following.
	r3, err := Delete(b, "series", "20261116T080000Z", ScopeFollowing, editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	if r3.New != nil {
		t.Error("delete made a new series")
	}
	equal(t, list(t, r3.Data), "11-09 11:00 Early move")
}

func TestThisAndFollowingEdges(t *testing.T) {
	b := weekly("FREQ=WEEKLY;UNTIL=20261207T080000Z")
	// At the first occurrence it is the whole series.
	r, err := Update(b, "", "20261102T080000Z", ScopeFollowing, setSummary("All"), editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.Scope != ScopeAll || r.New != nil {
		t.Errorf("first occurrence: %+v", r.Scope)
	}
	equal(t, list(t, r.Data), "11-02 09:00 All", "11-09 09:00 All", "11-16 09:00 All", "11-23 09:00 All", "11-30 09:00 All", "12-07 09:00 All")
	if propValue(component(t, r.Data, "series", ""), "SEQUENCE") != "3" {
		t.Error("sequence not raised")
	}

	// At the last occurrence: the old series ends before it, the new one
	// keeps UNTIL and has one occurrence.
	r, err = Update(b, "", "20261207T080000Z", ScopeFollowing, setSummary("Last"), editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	equal(t, list(t, r.Data), "11-02 09:00 Weekly", "11-09 09:00 Weekly", "11-16 09:00 Weekly", "11-23 09:00 Weekly", "11-30 09:00 Weekly")
	equal(t, list(t, r.New), "12-07 09:00 Last")
	if got := propValue(component(t, r.New, "new-uid", ""), "RRULE"); got != "FREQ=WEEKLY;UNTIL=20261207T080000Z" {
		t.Errorf("new RRULE %s", got)
	}

	// COUNT at the last occurrence: COUNT=1.
	r, err = Update(weekly("FREQ=WEEKLY;COUNT=6"), "", "20261207T080000Z", ScopeFollowing, setSummary("Last"), editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := propValue(component(t, r.New, "new-uid", ""), "RRULE"); got != "FREQ=WEEKLY;COUNT=1" {
		t.Errorf("new RRULE %s", got)
	}

	// A date series ends the day before; a floating one without Z.
	d := ics(`UID:d
DTSTAMP:20261101T100000Z
DTSTART;VALUE=DATE:20261102
RRULE:FREQ=DAILY`)
	r, err = Delete(d, "", "20261105", ScopeFollowing, editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := propValue(component(t, r.Data, "d", ""), "RRULE"); got != "FREQ=DAILY;UNTIL=20261104" {
		t.Errorf("date cut %s", got)
	}
	fl := ics(`UID:f
DTSTAMP:20261101T100000Z
DTSTART:20261102T090000
RRULE:FREQ=DAILY`)
	r, err = Delete(fl, "", "20261105T080000Z", ScopeFollowing, editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	if got := propValue(component(t, r.Data, "f", ""), "RRULE"); got != "FREQ=DAILY;UNTIL=20261105T085959" {
		t.Errorf("floating cut %s", got)
	}
	equal(t, list(t, r.Data)[len(list(t, r.Data))-1:], "11-04 09:00 ")
}

func TestWholeSeries(t *testing.T) {
	// From 19 October across the DST change of 25 October: the EXDATE and
	// the override after the change still meet their occurrences when the
	// series moves from 09:00 to 10:00.
	b := ics(`UID:s
DTSTAMP:20261001T100000Z
SUMMARY:Series
DTSTART;TZID=Europe/Berlin:20261019T090000
DTEND;TZID=Europe/Berlin:20261019T093000
RRULE:FREQ=WEEKLY;UNTIL=20261109T080000Z
EXDATE;TZID=Europe/Berlin:20261102T090000`, `UID:s
DTSTAMP:20261001T100000Z
SUMMARY:Override
RECURRENCE-ID:20261026T080000Z
DTSTART;TZID=Europe/Berlin:20261026T150000
DTEND;TZID=Europe/Berlin:20261026T153000`)
	equal(t, list(t, b), "10-19 09:00 Series", "10-26 15:00 Override", "11-09 09:00 Series")
	r, err := Update(b, "", "", ScopeAll, moveBy(t, time.Hour), editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	equal(t, list(t, r.Data), "10-19 10:00 Series", "10-26 15:00 Override", "11-09 10:00 Series")
	m := component(t, r.Data, "s", "")
	if got := propValue(m, "EXDATE"); got != "TZID=Europe/Berlin:20261102T100000" {
		t.Errorf("EXDATE %s", got)
	}
	if got := propValue(m, "RRULE"); got != "FREQ=WEEKLY;UNTIL=20261109T090000Z" {
		t.Errorf("RRULE %s", got)
	}
	if got := propValue(component(t, r.Data, "s", "20261026T090000Z"), "RECURRENCE-ID"); got != "20261026T090000Z" {
		t.Errorf("RECURRENCE-ID %s", got)
	}

	// Timed to all-day drops what no longer has a meaning.
	r, err = Update(b, "", "", ScopeAll, func(c *ical.Component) error {
		tz.Date(2026, 10, 19).SetProp(c.Props.Get(ical.PropDateTimeStart))
		tz.Date(2026, 10, 20).SetProp(c.Props.Get(ical.PropDateTimeEnd))
		return nil
	}, editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.DroppedExceptions != 2 {
		t.Errorf("dropped %d", r.DroppedExceptions)
	}
	if got := propValue(component(t, r.Data, "s", ""), "RRULE"); got != "FREQ=WEEKLY;UNTIL=20261109" {
		t.Errorf("RRULE %s", got)
	}

	r, err = Delete(b, "", "", ScopeAll, editOpt(t))
	if err != nil || !r.Delete || r.Data != nil {
		t.Errorf("delete all: %+v %v", r, err)
	}
}

func TestEditErrors(t *testing.T) {
	b := weekly("FREQ=WEEKLY;COUNT=6")
	if _, err := Update(b, "", "20261117T080000Z", ScopeThis, setSummary("x"), editOpt(t)); !errors.Is(err, ErrNoOccurrence) {
		t.Errorf("not an occurrence: %v", err)
	}
	if _, err := Delete(b, "", "", ScopeThis, editOpt(t)); !errors.Is(err, ErrNoOccurrence) {
		t.Errorf("this without occurrence: %v", err)
	}
	if _, err := Update(b, "other", "", ScopeAll, setSummary("x"), editOpt(t)); !errors.Is(err, ErrNoSeries) {
		t.Errorf("other uid: %v", err)
	}
	opt := editOpt(t)
	opt.NewUID = ""
	if _, err := Update(b, "", "20261116T080000Z", ScopeFollowing, setSummary("x"), opt); err == nil {
		t.Error("following without new UID")
	}
	boom := errors.New("boom")
	if _, err := Update(b, "", "", ScopeAll, func(*ical.Component) error { return boom }, editOpt(t)); !errors.Is(err, boom) {
		t.Errorf("change error: %v", err)
	}
	// A single event knows only the whole.
	single := ics("UID:one\nDTSTAMP:20261101T100000Z\nDTSTART:20261102T090000Z\nSUMMARY:Once")
	r, err := Update(single, "", "20261102T090000Z", ScopeThis, setSummary("Still once"), editOpt(t))
	if err != nil || r.Scope != ScopeAll {
		t.Fatalf("single: %+v %v", r, err)
	}
	equal(t, list(t, r.Data), "11-02 10:00 Still once")
}
