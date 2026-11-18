package tasks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

var berlin = func() *time.Location { l, _ := time.LoadLocation("Europe/Berlin"); return l }()

var now = time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)

func newService(t *testing.T) Service {
	t.Helper()
	dir := t.TempDir()
	st := store.New()
	st.Local = func() *time.Location { return berlin }
	st.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		path := filepath.Join(dir, ".titanos", feature)
		if create {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return nil, nil, err
			}
		}
		root, err := os.OpenRoot(path)
		return root, func() {}, err
	}
	return Service{Store: st, Now: func() time.Time { return now }}
}

func ptr[T any](v T) *T { return &v }

func opt[T any](v T) Opt[T] { return Opt[T]{Set: true, Value: &v} }

func unfold(b []byte) string { return strings.ReplaceAll(string(b), "\r\n ", "") }

func has(t *testing.T, data []byte, want ...string) {
	t.Helper()
	s := unfold(data)
	for _, w := range want {
		if !strings.Contains(s, w) {
			t.Errorf("missing %q in\n%s", w, s)
		}
	}
}

func lacks(t *testing.T, data []byte, not ...string) {
	t.Helper()
	s := unfold(data)
	for _, w := range not {
		if strings.Contains(s, w) {
			t.Errorf("unexpected %q in\n%s", w, s)
		}
	}
}

// iPhone is a reminder as iOS writes it, with what TitanOS does not know.
const iPhone = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//Apple Inc.//iPhone OS 26.1//EN\r\nCALSCALE:GREGORIAN\r\n" +
	"BEGIN:VTODO\r\nCREATED:20261118T081500Z\r\nDTSTAMP:20261118T081512Z\r\nLAST-MODIFIED:20261118T081511Z\r\nSEQUENCE:0\r\n" +
	"SUMMARY:Arzt anrufen\r\nUID:9B1F0D3E\r\nDTSTART;TZID=Europe/Berlin:20261120T090000\r\nDUE;TZID=Europe/Berlin:20261120T090000\r\n" +
	"PRIORITY:1\r\nX-APPLE-SORT-ORDER:785139312\r\nX-APPLE-FLAGGED;X-PARAM=1:TRUE\r\n" +
	"BEGIN:VALARM\r\nACTION:DISPLAY\r\nDESCRIPTION:Reminder\r\nTRIGGER;VALUE=DATE-TIME:20261120T080000Z\r\nX-WR-ALARMUID:keep-me\r\nEND:VALARM\r\n" +
	"END:VTODO\r\nEND:VCALENDAR\r\n"

func TestBuildAndOpen(t *testing.T) {
	data, err := Build(Input{Title: ptr("Müll rausbringen"), Notes: ptr("gelbe Tonne\nauch Papier"), Due: opt("2026-11-23"),
		Priority: ptr(5), Repeat: opt(recur.Simple{Freq: "weekly", Interval: 1}), Parent: opt("p-1"),
		Alarms: opt([]string{"2026-11-23T07:30"})}, "u-1", now, berlin)
	if err != nil {
		t.Fatal(err)
	}
	has(t, data, "PRODID:"+ProdID, "UID:u-1", "SUMMARY:Müll rausbringen", "DESCRIPTION:gelbe Tonne\\nauch Papier",
		"DUE;VALUE=DATE:20261123", "DTSTART;VALUE=DATE:20261123", "PRIORITY:5", "STATUS:NEEDS-ACTION", "RELATED-TO:p-1",
		"RRULE:FREQ=WEEKLY", "TRIGGER;VALUE=DATE-TIME:20261123T063000Z", "DTSTAMP:20261118T100000Z")
	task, err := Open("l", "u-1.ics", "e", data, berlin)
	if err != nil {
		t.Fatal(err)
	}
	if task.Title != "Müll rausbringen" || task.Due != "2026-11-23" || !task.DueAllDay || task.Notes != "gelbe Tonne\nauch Papier" ||
		task.Priority != 5 || task.Parent != "p-1" || !task.Recurring || task.Rule == nil || task.Rule.Freq != "weekly" ||
		len(task.Alarms) != 1 || task.Alarms[0] != "2026-11-23T07:30:00+01:00" || task.Done {
		t.Fatalf("%+v", task)
	}
	// A time in the NAS zone gets its VTIMEZONE; one named another zone.
	data, err = Build(Input{Title: ptr("Anruf"), Due: opt("2026-11-20T09:00")}, "u-2", now, berlin)
	if err != nil {
		t.Fatal(err)
	}
	has(t, data, "DUE;TZID=Europe/Berlin:20261120T090000", "BEGIN:VTIMEZONE", "TZID:Europe/Berlin")
	data, _ = Build(Input{Title: ptr("Call"), Due: opt("2026-11-20T09:00"), Timezone: "America/New_York", Start: Opt[string]{Set: true}}, "u-3", now, berlin)
	has(t, data, "DUE;TZID=America/New_York:20261120T090000")
	lacks(t, data, "DTSTART;")
	for name, in := range map[string]Input{
		"no title":    {Title: ptr("  ")},
		"bad date":    {Title: ptr("x"), Due: opt("2026-13-40")},
		"bad zone":    {Title: ptr("x"), Due: opt("2026-11-20T09:00"), Timezone: "Mars/Olympus"},
		"priority":    {Title: ptr("x"), Priority: ptr(11)},
		"repeat":      {Title: ptr("x"), Repeat: opt(recur.Simple{Freq: "weekly", Interval: 1})},
		"control":     {Title: ptr("a\x01b")},
		"alarms":      {Title: ptr("x"), Alarms: opt(make([]string, 21))},
		"year":        {Title: ptr("x"), Due: opt("3000-01-01")},
		"long parent": {Title: ptr("x"), Parent: opt(strings.Repeat("p", 300))},
	} {
		if _, err := Build(in, "u", now, berlin); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Editing changes only what is given; X-APPLE properties, parameters and
// an alarm's own properties stay. Moving DUE moves DTSTART (equal to it)
// and the alarm at the due time along.
func TestEditKeepsWhatItDoesNotKnow(t *testing.T) {
	out, changed, err := Edit([]byte(iPhone), Input{Title: ptr("Arzt anrufen")}, now, berlin)
	if err != nil || changed || string(out) != iPhone {
		t.Fatalf("unchanged input: %v %v", changed, err)
	}
	out, changed, err = Edit([]byte(iPhone), Input{Title: ptr("Arzt anrufen (Rezept)"), Due: opt("2026-11-21T10:00")}, now, berlin)
	if err != nil || !changed {
		t.Fatal(err)
	}
	has(t, out, "SUMMARY:Arzt anrufen (Rezept)", "X-APPLE-SORT-ORDER:785139312", "X-APPLE-FLAGGED;X-PARAM=1:TRUE", "X-WR-ALARMUID:keep-me",
		"DUE;TZID=Europe/Berlin:20261121T100000", "DTSTART;TZID=Europe/Berlin:20261121T100000", "TRIGGER;VALUE=DATE-TIME:20261121T090000Z",
		"SEQUENCE:1", "LAST-MODIFIED:20261118T100000Z", "PRIORITY:1", "CREATED:20261118T081500Z")
	// Fields removed: due (and start with it), priority, alarms.
	out, _, err = Edit([]byte(iPhone), Input{Due: Opt[string]{Set: true}, Priority: ptr(0), Alarms: Opt[[]string]{Set: true}}, now, berlin)
	if err != nil {
		t.Fatal(err)
	}
	lacks(t, out, "DUE", "DTSTART", "PRIORITY", "VALARM")
	has(t, out, "X-APPLE-SORT-ORDER")
	// A rule needs a date.
	if _, _, err := Edit(out, Input{Repeat: opt(recur.Simple{Freq: "daily", Interval: 1})}, now, berlin); !errors.Is(err, ErrInvalid) {
		t.Fatalf("rule without a date: %v", err)
	}
	// An alarm iOS relates to the start is kept when its time stays.
	rel := strings.Replace(iPhone, "TRIGGER;VALUE=DATE-TIME:20261120T080000Z", "TRIGGER:-PT15M", 1)
	task, _ := Open("l", "a.ics", "e", []byte(rel), berlin)
	if len(task.Alarms) != 1 || task.Alarms[0] != "2026-11-20T08:45:00+01:00" {
		t.Fatalf("relative alarm: %+v", task.Alarms)
	}
	out, _, _ = Edit([]byte(rel), Input{Alarms: opt([]string{"2026-11-20T08:45:00+01:00", "2026-11-19T20:00"})}, now, berlin)
	has(t, out, "TRIGGER:-PT15M", "X-WR-ALARMUID:keep-me", "TRIGGER;VALUE=DATE-TIME:20261119T190000Z")
}

func TestSetDone(t *testing.T) {
	c, err := SetDone([]byte(iPhone), true, now, berlin)
	if err != nil || c.Copy != nil {
		t.Fatal(err)
	}
	has(t, c.Data, "STATUS:COMPLETED", "COMPLETED:20261118T100000Z", "PERCENT-COMPLETE:100", "X-APPLE-SORT-ORDER", "SEQUENCE:1")
	if again, _ := SetDone(c.Data, true, now, berlin); again.Data != nil {
		t.Fatal("done twice wrote")
	}
	back, _ := SetDone(c.Data, false, now, berlin)
	has(t, back.Data, "STATUS:NEEDS-ACTION")
	lacks(t, back.Data, "COMPLETED:", "PERCENT-COMPLETE")

	// A series: the occurrence becomes a done copy, the series moves on
	// (DTSTART, DUE, the absolute alarm; COUNT one less).
	series := strings.Replace(iPhone, "PRIORITY:1\r\n", "PRIORITY:1\r\nRRULE:FREQ=WEEKLY;COUNT=3\r\n", 1)
	c, err = SetDone([]byte(series), true, now, berlin)
	if err != nil || c.Copy == nil || c.CopyUID == "" {
		t.Fatalf("series: %+v %v", c, err)
	}
	has(t, c.Data, "DTSTART;TZID=Europe/Berlin:20261127T090000", "DUE;TZID=Europe/Berlin:20261127T090000",
		"TRIGGER;VALUE=DATE-TIME:20261127T080000Z", "COUNT=2", "UID:9B1F0D3E", "X-APPLE-SORT-ORDER")
	lacks(t, c.Data, "STATUS:COMPLETED", "COMPLETED:")
	has(t, c.Copy, "UID:"+c.CopyUID, "STATUS:COMPLETED", "DUE;TZID=Europe/Berlin:20261120T090000", "SUMMARY:Arzt anrufen", "BEGIN:VTIMEZONE")
	lacks(t, c.Copy, "RRULE:FREQ=WEEKLY", "VALARM", "UID:9B1F0D3E")
	// The last occurrence: done as a whole.
	last := strings.Replace(iPhone, "PRIORITY:1\r\n", "PRIORITY:1\r\nRRULE:FREQ=WEEKLY;COUNT=1\r\n", 1)
	c, _ = SetDone([]byte(last), true, now, berlin)
	if c.Copy != nil {
		t.Fatal("last occurrence copied")
	}
	has(t, c.Data, "STATUS:COMPLETED", "RRULE:FREQ=WEEKLY;COUNT=1")
	// A weekly series on dates (Monday) moves a week on.
	dates := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:x\r\nBEGIN:VTODO\r\nUID:m\r\nDTSTAMP:20261118T000000Z\r\nSUMMARY:Müll\r\n" +
		"DTSTART;VALUE=DATE:20261123\r\nDUE;VALUE=DATE:20261123\r\nRRULE:FREQ=WEEKLY;BYDAY=MO\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"
	c, _ = SetDone([]byte(dates), true, now, berlin)
	has(t, c.Data, "DTSTART;VALUE=DATE:20261130", "DUE;VALUE=DATE:20261130")
}

func TestServiceFlow(t *testing.T) {
	s := newService(t)
	lists, err := s.Lists("alex", "en")
	if err != nil || len(lists) != 1 || !lists[0].Virtual || lists[0].Name != "Reminders" || lists[0].ID != store.DefaultTaskList {
		t.Fatalf("lists: %+v %v", lists, err)
	}
	// The first reminder made in English names the default list so.
	a, err := s.Create("alex", Input{Title: ptr("Milk"), Due: opt("2026-11-20"), Lang: "en"})
	if err != nil {
		t.Fatal(err)
	}
	lists, _ = s.Lists("alex", "de")
	if len(lists) != 1 || lists[0].Virtual || lists[0].Name != "Reminders" || lists[0].Count != 1 || lists[0].Open != 1 {
		t.Fatalf("after the first: %+v", lists)
	}
	shop, err := s.CreateList("alex", ListInput{Name: ptr("Einkauf")})
	if err != nil || shop.ID != "einkauf" || shop.Order != 1 {
		t.Fatalf("list: %+v %v", shop, err)
	}
	if _, err := s.Store.CreateCollection(store.Calendar, "alex", "arbeit", store.Meta{Name: "Arbeit"}); err != nil {
		t.Fatal(err)
	}
	// A list named like a calendar gets the next id.
	if l, err := s.CreateList("alex", ListInput{Name: ptr("Arbeit")}); err != nil || l.ID != "arbeit-2" {
		t.Fatalf("id next to a calendar: %+v %v", l, err)
	}
	b, err := s.Create("alex", Input{List: "einkauf", Title: ptr("Brot"), Due: opt("2026-11-19T18:00"), Priority: ptr(1)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("alex", Input{List: "einkauf", Title: ptr("Käse")}); err != nil {
		t.Fatal(err)
	}
	open, err := s.Tasks("alex", Filter{})
	if err != nil || len(open) != 3 || open[0].Title != "Brot" || open[1].Title != "Milk" || open[2].Title != "Käse" || open[0].Due != "2026-11-19T18:00:00+01:00" {
		t.Fatalf("open: %+v %v", open, err)
	}
	if due, _ := s.Tasks("alex", Filter{DueBefore: time.Date(2026, 11, 20, 0, 0, 0, 0, berlin)}); len(due) != 1 || due[0].Title != "Brot" {
		t.Fatalf("due before: %+v", due)
	}
	if due, _ := s.Due("alex", time.Date(2026, 11, 20, 0, 0, 0, 0, berlin), time.Date(2026, 11, 21, 0, 0, 0, 0, berlin)); len(due) != 1 || due[0].Title != "Milk" {
		t.Fatalf("due window: %+v", due)
	}
	if _, err := s.Due("alex", now, now.AddDate(2, 0, 0)); !errors.Is(err, recur.ErrWindow) {
		t.Fatalf("window: %v", err)
	}
	// Ticked off with a stale ETag: conflict; with the right one: done.
	if _, err := s.SetDone("alex", "einkauf", b.ID, "stale", true); !errors.Is(err, store.ErrPrecondition) {
		t.Fatalf("stale: %v", err)
	}
	d, err := s.SetDone("alex", "einkauf", b.ID, `"`+b.ETag+`"`, true)
	if err != nil || !d.Task.Done || d.Copy != nil || d.Task.ETag == b.ETag {
		t.Fatalf("done: %+v %v", d, err)
	}
	if done, _ := s.Tasks("alex", Filter{Status: "done"}); len(done) != 1 || done[0].ID != b.ID {
		t.Fatalf("done list: %+v", done)
	}
	if all, _ := s.Tasks("alex", Filter{Status: "all", List: "einkauf"}); len(all) != 2 || all[1].ID != b.ID {
		t.Fatalf("all: %+v", all)
	}
	// Update with a move to another list.
	got, _ := s.Get("alex", store.DefaultTaskList, a.ID)
	moved, changed, err := s.Update("alex", store.DefaultTaskList, a.ID, got.ETag, Input{List: "einkauf", Notes: ptr("1 l")})
	if err != nil || !changed || moved.List != "einkauf" || moved.Notes != "1 l" {
		t.Fatalf("move: %+v %v", moved, err)
	}
	if _, err := s.Get("alex", store.DefaultTaskList, a.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("still in the old list: %v", err)
	}
	same, changed, err := s.Update("alex", "einkauf", a.ID, moved.ETag, Input{Notes: ptr("1 l")})
	if err != nil || changed || same.ETag != moved.ETag {
		t.Fatalf("unchanged: %v %v", changed, err)
	}
	if err := s.Delete("alex", "einkauf", a.ID, "x"); !errors.Is(err, store.ErrPrecondition) {
		t.Fatalf("delete stale: %v", err)
	}
	if err := s.Delete("alex", "einkauf", a.ID, moved.ETag); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteList("alex", "einkauf"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("alex", "einkauf", b.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("list deleted with its reminders: %v", err)
	}
	if l, err := s.UpdateList("alex", "arbeit-2", ListInput{Name: ptr("Büro"), Color: ptr("#2563eb")}); err != nil || l.Name != "Büro" || l.Color != "#2563eb" {
		t.Fatalf("update list: %+v %v", l, err)
	}
	if err := s.DeleteList("alex", "arbeit-2"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteList("alex", store.DefaultTaskList); !errors.Is(err, store.ErrLastCollection) {
		t.Fatalf("last list: %v", err)
	}
	if err := (Service{}).DeleteList("alex", "x"); !errors.Is(err, store.ErrUnavailable) {
		t.Fatalf("no store: %v", err)
	}
}

// A series ticked off through the service: the series moves on, the copy
// is a reminder of its own in the same list.
func TestServiceSeries(t *testing.T) {
	s := newService(t)
	a, err := s.Create("alex", Input{Title: ptr("Pflanzen gießen"), Due: opt("2026-11-18"), Repeat: opt(recur.Simple{Freq: "daily", Interval: 2})})
	if err != nil || !a.Recurring {
		t.Fatalf("%+v %v", a, err)
	}
	d, err := s.SetDone("alex", a.List, a.ID, a.ETag, true)
	if err != nil || d.Copy == nil || d.Task.Done || d.Task.Due != "2026-11-20" || !d.Copy.Done || d.Copy.Due != "2026-11-18" || d.Copy.Recurring {
		t.Fatalf("%+v %+v %v", d.Task, d.Copy, err)
	}
	all, _ := s.Tasks("alex", Filter{Status: "all"})
	if len(all) != 2 {
		t.Fatalf("all: %+v", all)
	}
}
