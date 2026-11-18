package tz

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

// A generated VTIMEZONE, read back under an unknown name (and without
// X-LIC-LOCATION), gives the offsets of the zone it was made from.
func TestVTimezoneReadsBack(t *testing.T) {
	for _, name := range []string{"Europe/Berlin", "America/New_York", "Australia/Sydney", "Asia/Tokyo", "Asia/Kolkata", "Europe/London", "America/Santiago"} {
		t.Run(name, func(t *testing.T) {
			loc := LoadIANA(name)
			comp := VTimezone("Fake/Zone", loc, 2026, 2028)
			comp.Props.Del("X-LIC-LOCATION")
			cal := ical.NewCalendar()
			cal.Props.SetText(ical.PropVersion, "2.0")
			cal.Props.SetText(ical.PropProductID, "-//t//EN")
			cal.Children = append(cal.Children, comp)
			var buf bytes.Buffer
			if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
				t.Fatal(err)
			}
			back, err := ical.NewDecoder(&buf).Decode()
			if err != nil {
				t.Fatal(err)
			}
			r := NewResolver(time.UTC, back)
			for year := 2026; year <= 2028; year++ {
				got, guess := r.Location("Fake/Zone", year)
				// A zone without changes resolves as its fixed offset
				// (marked as guess); the offsets must match all the same.
				if guess && name != "Asia/Tokyo" && name != "Asia/Kolkata" {
					t.Errorf("%d: guessed", year)
				}
				for day := 0; day < 366; day += 5 {
					at := time.Date(year, 1, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, day)
					_, want := at.In(loc).Zone()
					_, off := at.In(got).Zone()
					if want != off {
						t.Fatalf("%s: offset %d, want %d (resolved as %s)", at.Format(time.DateOnly), off, want, got)
					}
				}
			}
		})
	}
}

func TestVTimezoneShape(t *testing.T) {
	c := VTimezone("Europe/Berlin", LoadIANA("Europe/Berlin"), 2026, 2036)
	var rules []string
	for _, o := range c.Children {
		if p := o.Props.Get(ical.PropRecurrenceRule); p != nil {
			rules = append(rules, o.Name+" "+o.Props.Get(ical.PropDateTimeStart).Value+" "+p.Value)
		}
	}
	want := []string{
		"DAYLIGHT 20260329T020000 FREQ=YEARLY;BYMONTH=3;BYDAY=-1SU",
		"STANDARD 20261025T030000 FREQ=YEARLY;BYMONTH=10;BYDAY=-1SU",
	}
	if strings.Join(rules, "|") != strings.Join(want, "|") {
		t.Fatalf("rules %q", rules)
	}
	tokyo := VTimezone("Asia/Tokyo", LoadIANA("Asia/Tokyo"), 2026, 2030)
	if len(tokyo.Children) != 1 || tokyo.Children[0].Props.Get(ical.PropTimezoneOffsetTo).Value != "+0900" {
		t.Fatalf("tokyo %+v", tokyo.Children)
	}
	if formatOffset(-(3*3600+30*60)) != "-0330" || formatOffset(3600+31*60+28) != "+013128" {
		t.Fatal("offset format")
	}
}
