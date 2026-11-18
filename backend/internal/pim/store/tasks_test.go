package store

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func vtodo(uid, summary string, extra ...string) []byte {
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//Apple Inc.//iPhone OS 26.1//EN", "BEGIN:VTODO", "UID:" + uid,
		"DTSTAMP:20261118T090000Z", "SUMMARY:" + summary}
	lines = append(lines, extra...)
	lines = append(lines, "END:VTODO", "END:VCALENDAR", "")
	return []byte(strings.Join(lines, "\r\n"))
}

// A reminder list is a kind of its own: the virtual default list next to
// the virtual "privat", written below .titanos/tasks only; writing a list
// leaves "privat" virtual and the calendars' last-collection rule alone.
func TestTasksApartFromCalendars(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	lists, err := s.Collections(Tasks, "alex")
	if err != nil || len(lists) != 1 || lists[0].ID != DefaultTaskList || !lists[0].Virtual || lists[0].Name != DefaultTaskListName ||
		lists[0].Color != DefaultTaskColor || lists[0].CTag != "0-0" {
		t.Fatalf("virtual list: %+v %v", lists, err)
	}
	mustPut(t, s, Tasks, DefaultTaskList, "milch.ics", vtodo("milch", "Milch kaufen", "DUE;VALUE=DATE:20261120"))
	for _, p := range e.tree() {
		if strings.HasPrefix(p, filepath.Join(".titanos", "calendar")) {
			t.Fatalf("a reminder wrote below the calendars: %v", e.tree())
		}
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".titanos", "tasks", "alex", DefaultTaskList, "o", "milch.ics")); err != nil {
		t.Fatal(err)
	}
	cals, err := s.Collections(Calendar, "alex")
	if err != nil || len(cals) != 1 || cals[0].ID != DefaultCalendar || !cals[0].Virtual {
		t.Fatalf("privat after a reminder: %+v %v", cals, err)
	}
	lists, _ = s.Collections(Tasks, "alex")
	if len(lists) != 1 || lists[0].Virtual || lists[0].Count != 1 || lists[0].Name != DefaultTaskListName {
		t.Fatalf("list made real: %+v", lists)
	}
	// The last list stays, and so does the last calendar – each on its own.
	if err := s.DeleteCollection(Tasks, "alex", DefaultTaskList, ""); !errors.Is(err, ErrLastCollection) {
		t.Fatalf("last list: %v", err)
	}
	if _, err := s.CreateCollection(Calendar, "alex", "arbeit", Meta{Name: "Arbeit"}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteCollection(Calendar, "alex", "arbeit", ""); err != nil {
		t.Fatalf("a calendar next to privat (made real with it): %v", err)
	}
	if err := s.DeleteCollection(Calendar, "alex", DefaultCalendar, ""); !errors.Is(err, ErrLastCollection) {
		t.Fatalf("last calendar: %v", err)
	}
}

// One component per collection; an id is a calendar or a list.
func TestTasksComponentsAndIDs(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	if _, err := s.Put(Tasks, "alex", DefaultTaskList, "e.ics", ics("e", "Termin", "20261110T080000Z"), PutOptions{}); !errors.Is(err, ErrUnsupportedComponent) {
		t.Fatalf("an event in a list: %v", err)
	}
	if _, err := s.Put(Calendar, "alex", DefaultCalendar, "t.ics", vtodo("t", "x"), PutOptions{}); !errors.Is(err, ErrUnsupportedComponent) {
		t.Fatalf("a reminder in a calendar: %v", err)
	}
	mixed := strings.Replace(string(vtodo("m", "x")), "END:VCALENDAR", "BEGIN:VEVENT\r\nUID:m\r\nDTSTAMP:20261101T000000Z\r\nDTSTART:20261110T080000Z\r\nEND:VEVENT\r\nEND:VCALENDAR", 1)
	if _, err := s.Put(Tasks, "alex", DefaultTaskList, "m.ics", []byte(mixed), PutOptions{}); !errors.Is(err, ErrUnsupportedComponent) {
		t.Fatalf("mixed: %v", err)
	}
	two := strings.Replace(string(vtodo("a", "x")), "END:VCALENDAR", "BEGIN:VTODO\r\nUID:b\r\nDTSTAMP:20261101T000000Z\r\nEND:VTODO\r\nEND:VCALENDAR", 1)
	if _, err := s.Put(Tasks, "alex", DefaultTaskList, "two.ics", []byte(two), PutOptions{}); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("two UIDs: %v", err)
	}
	// The virtual default of the other kind holds its id, too.
	if _, err := s.CreateCollection(Calendar, "alex", DefaultTaskList, Meta{Name: "x"}); !errors.Is(err, ErrExists) {
		t.Fatalf("calendar with the list's id: %v", err)
	}
	if _, err := s.CreateCollection(Tasks, "alex", DefaultCalendar, Meta{Name: "x"}); !errors.Is(err, ErrExists) {
		t.Fatalf("list with privat's id: %v", err)
	}
	if _, err := s.CreateCollection(Tasks, "alex", "einkauf", Meta{Name: "Einkauf"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCollection(Calendar, "alex", "einkauf", Meta{Name: "Einkauf"}); !errors.Is(err, ErrExists) {
		t.Fatalf("calendar with a list's id: %v", err)
	}
	// The first own list made the virtual one real (as privat with the
	// first calendar): a device that saw it keeps it.
	lists, _ := s.Collections(Tasks, "alex")
	if ids := []string{lists[0].ID, lists[1].ID}; len(lists) != 2 || !slices.Contains(ids, DefaultTaskList) || lists[0].Virtual || lists[1].Virtual {
		t.Fatalf("lists: %+v", lists)
	}
	for id, want := range map[string]Kind{DefaultCalendar: Calendar, "einkauf": Tasks, DefaultTaskList: Tasks} {
		if k, err := s.CalDAVKind("alex", id); err != nil || k != want {
			t.Errorf("CalDAVKind %s: %s %v", id, k, err)
		}
	}
	if _, err := s.CalDAVKind("alex", "nichts"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown id: %v", err)
	}
	if !s.Holds(Tasks, "alex", "einkauf") || s.Holds(Calendar, "alex", "einkauf") {
		t.Error("Holds")
	}
	e.present.Store(false)
	if _, err := s.CalDAVKind("alex", "nichts"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("without a volume: %v", err)
	}
}

// The index keeps what lists and filters need of a reminder.
func TestTaskIndex(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	berlin, _ := time.LoadLocation("Europe/Berlin")
	s.Local = func() *time.Location { return berlin }
	mustPut(t, s, Tasks, DefaultTaskList, "a.ics", vtodo("a", "Arzt anrufen",
		"DTSTART;TZID=Europe/Berlin:20261120T090000", "DUE;TZID=Europe/Berlin:20261120T090000",
		"PRIORITY:1", "STATUS:NEEDS-ACTION", "RELATED-TO;RELTYPE=PARENT:p-1", "X-APPLE-SORT-ORDER:123",
		"BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER;VALUE=DATE-TIME:20261120T080000Z", "END:VALARM"))
	mustPut(t, s, Tasks, DefaultTaskList, "b.ics", vtodo("b", "Müll", "DUE;VALUE=DATE:20261121", "DTSTART;VALUE=DATE:20261121",
		"RRULE:FREQ=WEEKLY", "STATUS:COMPLETED", "COMPLETED:20261118T100000Z", "PERCENT-COMPLETE:100"))
	mustPut(t, s, Tasks, DefaultTaskList, "c.ics", vtodo("c", "ohne Datum", "RRULE:FREQ=DAILY", "PRIORITY:42"))
	a, _ := s.Stat(Tasks, "alex", DefaultTaskList, "a.ics")
	b, _ := s.Stat(Tasks, "alex", DefaultTaskList, "b.ics")
	c, _ := s.Stat(Tasks, "alex", DefaultTaskList, "c.ics")
	if a.Task == nil || a.Summary != "Arzt anrufen" || !a.Task.Due.Equal(time.Date(2026, 11, 20, 8, 0, 0, 0, time.UTC)) || a.Task.DueTZ != "Europe/Berlin" ||
		a.Task.Priority != 1 || a.Task.Parent != "p-1" || !a.Task.Alarm || a.Task.Done() || a.Task.Recurs {
		t.Fatalf("a: %+v %+v", a.Entry, a.Task)
	}
	if b.Task == nil || !b.Task.DueAllDay || !b.Task.Due.Equal(time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)) || !b.Task.Done() || !b.Task.Recurs || b.Task.Percent != 100 ||
		!b.Task.DueAt(berlin).Equal(time.Date(2026, 11, 21, 0, 0, 0, 0, berlin)) {
		t.Fatalf("b: %+v", b.Task)
	}
	if c.Task == nil || c.Task.Recurs || !c.Task.RecurUnsupported || c.Task.Priority != 0 || !c.Task.Due.IsZero() {
		t.Fatalf("c: %+v", c.Task)
	}
	// The index survives a restart (state.json), and a list never shows
	// up as a calendar.
	s2 := e.store()
	if b2, err := s2.Stat(Tasks, "alex", DefaultTaskList, "b.ics"); err != nil || b2.Task == nil || !b2.Task.Done() {
		t.Fatalf("after a restart: %+v %v", b2, err)
	}
	if n, _, err := s2.Counts("alex"); err != nil || n != 0 {
		t.Fatalf("reminders counted as events: %d %v", n, err)
	}
}

// Deleting an account takes its lists along; the mark of kept reminders
// keeps a missing folder from showing the empty default.
func TestTasksForgetAndMark(t *testing.T) {
	v := newVolumes(t, "disk1", "disk2")
	s := v.store()
	mustPut(t, s, Tasks, DefaultTaskList, "a.ics", vtodo("a", "x"))
	if m := v.mark("alex"); m.Tasks.IsZero() || !m.Calendar.IsZero() {
		t.Fatalf("mark: %+v", m)
	}
	v.current.Store("disk2")
	if _, err := s.Collections(Tasks, "alex"); !errors.Is(err, ErrElsewhere) {
		t.Fatalf("folder elsewhere: %v", err)
	}
	if cals, err := s.Collections(Calendar, "alex"); err != nil || len(cals) != 1 || !cals[0].Virtual {
		t.Fatalf("calendars keep their own mark: %+v %v", cals, err)
	}
	v.current.Store("disk1")
	if err := s.ForgetUser("alex"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v.dir("disk1", ".titanos/tasks/alex")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("tasks of a deleted account: %v", err)
	}
}
