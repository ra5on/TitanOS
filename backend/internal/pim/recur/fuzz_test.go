package recur

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

var fuzzSeeds = append([]string{
	"FREQ=WEEKLY;BYDAY=MO,WE",
	"FREQ=MONTHLY;BYDAY=3TU",
	"FREQ=MONTHLY;BYMONTHDAY=-1;COUNT=12",
	"FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=29",
	"FREQ=YEARLY;BYWEEKNO=1,-1;BYDAY=MO",
	"FREQ=YEARLY;BYYEARDAY=-1,100;UNTIL=20300101",
	"FREQ=MONTHLY;BYDAY=MO,TU,WE,TH,FR;BYSETPOS=-2;WKST=SU",
	"FREQ=DAILY;INTERVAL=1000;UNTIL=20991231T235959Z",
	"FREQ=WEEKLY;INTERVAL=2;BYDAY=SU,SA;WKST=SU;COUNT=5000",
	"FREQ=DAILY;BYHOUR=9;BYMINUTE=0;BYSECOND=0",
	"RRULE:FREQ=YEARLY;UNTIL=20261231T000000",
}, brokenRules...)

// FuzzParse: Parse never panics; what it accepts writes back to an equal
// rule; and what also passes Check runs through rrule-go without a panic
// and without hanging (no recover here, so the fuzzer sees one).
func FuzzParse(f *testing.F) {
	for _, s := range fuzzSeeds {
		f.Add(s, int64(0))
		f.Add(s, int64(1798761600)) // 2027-01-01
	}
	f.Fuzz(func(t *testing.T, s string, startUnix int64) {
		r, err := Parse(s)
		if err != nil {
			if !errors.Is(err, ErrSyntax) && !errors.Is(err, ErrUnsupported) {
				t.Fatalf("%q: error %v wraps neither", s, err)
			}
			return
		}
		back, err := Parse(r.String())
		if err != nil {
			t.Fatalf("%q -> %q: %v", s, r.String(), err)
		}
		if !reflect.DeepEqual(r, back) {
			t.Fatalf("%q -> %q: %+v != %+v", s, r.String(), r, back)
		}
		r.Describe("de", time.UTC)
		r.Describe("en", time.UTC)
		r.Simple(time.UTC)

		start := time.Unix(1792670400+startUnix%(200*365*86400), 0).In(time.UTC) // around 2026
		dt := tz.Value{Time: start, Kind: tz.KindUTC}
		if r.Check(dt, start) != nil {
			return
		}
		next, err := newIterator(r.option(start, dt, time.UTC))
		if err != nil {
			return
		}
		for range 50 {
			if _, ok := next(); !ok {
				break
			}
		}
	})
}

// FuzzRead: hostile calendar files never panic the reader, the expansion
// or the editor.
func FuzzRead(f *testing.F) {
	files, _ := filepath.Glob("testdata/ios/*.ics")
	for _, name := range files {
		if b, err := os.ReadFile(name); err == nil {
			f.Add(b)
		}
	}
	f.Add(ics("UID:a\nDTSTART;TZID=Nowhere:20261102T090000\nRRULE:FREQ=DAILY;COUNT=3\nEXDATE;VALUE=DATE:20261103"))
	f.Add(ics("UID:a\nDTSTART;VALUE=DATE:20261102\nRDATE;VALUE=PERIOD:20261110T100000Z/PT1H", "UID:a\nRECURRENCE-ID;RANGE=THISANDFUTURE:20261102\nDTSTART;VALUE=DATE:20261105"))
	loc, _ := time.LoadLocation("Europe/Berlin")
	f.Fuzz(func(t *testing.T, b []byte) {
		file, err := Read(b, Options{Local: loc})
		if err != nil {
			return
		}
		from := time.Date(2026, 10, 1, 0, 0, 0, 0, loc)
		for _, s := range file.Series {
			x, err := s.Expand(from, from.AddDate(0, 0, 60))
			if err != nil {
				t.Fatal(err)
			}
			s.Overlaps(from, time.Time{})
			if len(x.Occurrences) > 0 {
				opt := EditOptions{Local: loc, Now: from, NewUID: "fuzz-new"}
				id := x.Occurrences[len(x.Occurrences)-1].ID()
				_, _ = Update(b, s.UID, id, ScopeFollowing, setSummary("x"), opt)
				_, _ = Delete(b, s.UID, id, ScopeThis, opt)
			}
		}
	})
}
