package calweb

import (
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
)

// 1.13: what the week sends when an event is dragged – start and end as
// RFC 3339 instants (the view's zone, here Berlin) with timezone "" – for
// a series in another zone (New York) and for an all-day series, with
// each scope. The event keeps its own zone; the instant is what counts.
func TestDraggedSeries(t *testing.T) {
	ny := ics("BEGIN:VEVENT", "UID:ny", "DTSTAMP:20261101T100000Z",
		"DTSTART;TZID=America/New_York:20261102T090000", "DTEND;TZID=America/New_York:20261102T100000",
		"RRULE:FREQ=WEEKLY;COUNT=6", "SUMMARY:Call", "END:VEVENT")
	// The occurrence of 16 November (09:00 New York = 15:00 Berlin) moved
	// to 16:30–17:30 Berlin, i.e. 10:30–11:30 New York.
	in := Input{Start: "2026-11-16T15:30:00Z", End: "2026-11-16T16:30:00Z"}
	opt := EditOptions{Local: berlin, Now: now, NewUID: "ny-2"}

	res, changed, err := Edit(ny, "20261116T140000Z", recur.ScopeThis, in, opt)
	if err != nil || !changed {
		t.Fatalf("this: %v %v", changed, err)
	}
	has(t, res.Data, "RECURRENCE-ID;TZID=America/New_York:20261116T090000",
		"DTSTART;TZID=America/New_York:20261116T103000", "DTEND;TZID=America/New_York:20261116T113000")

	res, _, err = Edit(ny, "20261116T140000Z", recur.ScopeFollowing, in, opt)
	if err != nil || res.New == nil {
		t.Fatalf("following: %v", err)
	}
	has(t, res.Data, "RRULE:FREQ=WEEKLY;UNTIL=20261116T135959Z")
	has(t, res.New, "UID:ny-2", "DTSTART;TZID=America/New_York:20261116T103000",
		"DTEND;TZID=America/New_York:20261116T113000", "RRULE:FREQ=WEEKLY;COUNT=4")

	res, _, err = Edit(ny, "20261116T140000Z", recur.ScopeAll, in, opt)
	if err != nil {
		t.Fatalf("all: %v", err)
	}
	// The whole series moves by the same 90 minutes, from its first day.
	has(t, res.Data, "DTSTART;TZID=America/New_York:20261102T103000", "DTEND;TZID=America/New_York:20261102T113000",
		"RRULE:FREQ=WEEKLY;COUNT=6")

	// An all-day series: one occurrence a day later, then this and following.
	day := ics("BEGIN:VEVENT", "UID:d", "DTSTAMP:20261101T100000Z", "DTSTART;VALUE=DATE:20261102",
		"DTEND;VALUE=DATE:20261103", "RRULE:FREQ=WEEKLY;UNTIL=20261214", "SUMMARY:Müll",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:PT9H", "END:VALARM", "END:VEVENT")
	move := Input{Start: "2026-11-17", End: "2026-11-18", AllDay: true}
	res, _, err = Edit(day, "20261116", recur.ScopeThis, move, EditOptions{Local: berlin, Now: now})
	if err != nil {
		t.Fatalf("all-day this: %v", err)
	}
	has(t, res.Data, "RECURRENCE-ID;VALUE=DATE:20261116", "DTSTART;VALUE=DATE:20261117", "DTEND;VALUE=DATE:20261118")
	res, _, err = Edit(day, "20261116", recur.ScopeFollowing, move, EditOptions{Local: berlin, Now: now, NewUID: "d-2"})
	if err != nil || res.New == nil {
		t.Fatalf("all-day following: %v", err)
	}
	has(t, res.Data, "RRULE:FREQ=WEEKLY;UNTIL=20261115")
	has(t, res.New, "DTSTART;VALUE=DATE:20261117", "DTEND;VALUE=DATE:20261118", "TRIGGER:PT9H")
	if !strings.Contains(string(res.New), "UNTIL=2026121") {
		t.Errorf("the new series keeps its end: %s", res.New)
	}
}
