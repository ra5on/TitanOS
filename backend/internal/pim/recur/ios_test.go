package recur

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-ical"
)

var updateIOS = flag.Bool("update-ios", false, "rewrite testdata/ios/expected.txt")

// TestIOSFixtures expands iOS-style files (written by hand after the form
// iOS 26 sends: Apple properties, alarms, VTIMEZONE, folded lines; no
// personal data) over one year and compares with a fixed list.
func TestIOSFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/ios/*.ics")
	if err != nil || len(files) == 0 {
		t.Fatal("no fixtures", err)
	}
	loc := berlin(t)
	from, to := time.Date(2026, 10, 1, 0, 0, 0, 0, loc), time.Date(2027, 10, 1, 0, 0, 0, 0, loc)
	var lines []string
	for _, name := range files {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := Read(b, Options{Local: loc, Name: filepath.Base(name)})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, s := range f.Series {
			if s.Master.Unsupported || s.Master.TZGuess {
				t.Errorf("%s: unsupported %v (%s) guess %v", name, s.Master.Unsupported, s.Master.Reason, s.Master.TZGuess)
			}
			x, err := s.Expand(from, to)
			if err != nil {
				t.Fatal(err)
			}
			for _, o := range x.Occurrences {
				sum, _ := o.Event.Component.Props.Text(ical.PropSummary)
				mark := ""
				if o.Override {
					mark = " (override)"
				}
				lines = append(lines, fmt.Sprintf("%s %s %s %s%s", filepath.Base(name),
					o.Start.In(loc).Format(time.RFC3339), o.End.In(loc).Format(time.RFC3339), sum, mark))
			}
		}
	}
	got := strings.Join(lines, "\n") + "\n"
	if *updateIOS {
		if err := os.WriteFile("testdata/ios/expected.txt", []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile("testdata/ios/expected.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("occurrences differ from testdata/ios/expected.txt:\n%s", got)
	}
}

// TestIOSEditKeepsAppleData cuts the iOS series and checks that alarms,
// Apple properties and the VTIMEZONE travel along.
func TestIOSEditKeepsAppleData(t *testing.T) {
	b, err := os.ReadFile("testdata/ios/weekly-team.ics")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Update(b, "", "20261116T080000Z", ScopeFollowing, setSummary("Neue Runde"), editOpt(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, data := range [][]byte{r.Data, r.New} {
		s := string(data)
		for _, want := range []string{"BEGIN:VTIMEZONE", "TZID:Europe/Berlin", "X-APPLE-CREATOR-IDENTITY:com.apple.mobilecal", "BEGIN:VALARM", "X-WR-ALARMUID:"} {
			if !strings.Contains(s, want) {
				t.Errorf("%q missing", want)
			}
		}
	}
	old := read(t, r.Data)
	if n := len(old.Series[0].Overrides); n != 1 {
		t.Errorf("old series has %d overrides, want the one of 2 November", n)
	}
	n := read(t, r.New)
	if n.Series[0].UID != "new-uid" || len(n.Series[0].Master.ExDates) != 2 {
		t.Errorf("new series: %s, %d EXDATE", n.Series[0].UID, len(n.Series[0].Master.ExDates))
	}
	if d, _ := n.Series[0].Master.Component.Props.Text(ical.PropDescription); !strings.HasPrefix(d, "Wöchentliche Abstimmung") || !strings.HasSuffix(d, "eintragen, danke.") {
		t.Errorf("description after folding: %q", d)
	}
}

// TestAllDayOnDSTDay: an all-day event on the day of the spring change
// lasts 23 hours on the clock and still covers exactly that date.
func TestAllDayOnDSTDay(t *testing.T) {
	b, err := os.ReadFile("testdata/ios/allday-yearly.ics")
	if err != nil {
		t.Fatal(err)
	}
	f := read(t, b)
	x, err := f.Series[0].Expand(day(t, 2026, 3, 1), day(t, 2026, 4, 1))
	if err != nil || len(x.Occurrences) != 1 {
		t.Fatal(x, err)
	}
	o := x.Occurrences[0]
	if o.Start.Format(time.RFC3339) != "2026-03-29T00:00:00+01:00" || o.End.Format(time.RFC3339) != "2026-03-30T00:00:00+02:00" || o.ID() != "20260329" {
		t.Errorf("%v – %v %s", o.Start, o.End, o.ID())
	}
}
