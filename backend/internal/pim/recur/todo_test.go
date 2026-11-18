package recur

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

func todoFile(lines ...string) []byte {
	all := append([]string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//t//EN", "BEGIN:VTODO", "UID:t", "DTSTAMP:20261118T090000Z"}, lines...)
	all = append(all, "END:VTODO", "END:VCALENDAR", "")
	return []byte(strings.Join(all, "\r\n"))
}

func TestTodoNext(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	for _, c := range []struct {
		name  string
		lines []string
		want  string // OccurrenceID, "" = no next
	}{
		{"weekly date", []string{"DTSTART;VALUE=DATE:20261121", "DUE;VALUE=DATE:20261121", "RRULE:FREQ=WEEKLY"}, "20261128"},
		{"due only", []string{"DUE;TZID=Europe/Berlin:20261120T090000", "RRULE:FREQ=MONTHLY"}, "20261220T080000Z"},
		{"over the change of time", []string{"DTSTART;TZID=Europe/Berlin:20261024T090000", "RRULE:FREQ=DAILY"}, "20261025T080000Z"},
		{"count used up", []string{"DTSTART:20261120T090000Z", "RRULE:FREQ=DAILY;COUNT=1"}, ""},
		{"until passed", []string{"DTSTART:20261120T090000Z", "RRULE:FREQ=DAILY;UNTIL=20261120T235959Z"}, ""},
		{"exdate skipped", []string{"DTSTART:20261120T090000Z", "RRULE:FREQ=DAILY", "EXDATE:20261121T090000Z"}, "20261122T090000Z"},
		{"yearly", []string{"DUE;VALUE=DATE:20240229", "RRULE:FREQ=YEARLY;BYMONTH=2;BYMONTHDAY=29"}, "20280229"},
		{"no dates", []string{"RRULE:FREQ=DAILY"}, ""},
		{"no rule", []string{"DUE:20261120T090000Z"}, ""},
	} {
		f, err := ReadTodos(todoFile(c.lines...), Options{Local: berlin})
		if err != nil || len(f.Todos) != 1 {
			t.Fatalf("%s: %v", c.name, err)
		}
		next, ok := f.Todos[0].Next()
		got := ""
		if ok {
			got = OccurrenceID(next)
		}
		if got != c.want {
			t.Errorf("%s: next %q, want %q", c.name, got, c.want)
		}
		if ok && f.Todos[0].Anchor() != nil && next.Kind != f.Todos[0].Anchor().Kind {
			t.Errorf("%s: next in another form %v", c.name, next.Kind)
		}
	}
	f, _ := ReadTodos(todoFile("RRULE:FREQ=DAILY"), Options{})
	if !f.Todos[0].Unsupported || f.Todos[0].Recurring() {
		t.Error("a rule without dates is unsupported")
	}
}

func TestReadTodosLimits(t *testing.T) {
	for name, data := range map[string][]byte{
		"no UID":      []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:x\r\nBEGIN:VTODO\r\nDTSTAMP:20261118T090000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"),
		"two DUE":     todoFile("DUE:20261120T090000Z", "DUE:20261121T090000Z"),
		"bad DUE":     todoFile("DUE:morgen"),
		"DUE+DUR":     todoFile("DUE:20261120T090000Z", "DURATION:PT1H"),
		"deep":        []byte(strings.Repeat("BEGIN:X\r\n", 20)),
		"not at all":  []byte("hello"),
		"two VCALS":   append(todoFile(), todoFile()...),
		"big zone":    []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:x\r\nBEGIN:VTIMEZONE\r\nTZID:x\r\nX-A:" + strings.Repeat("a", 70<<10) + "\r\nEND:VTIMEZONE\r\nBEGIN:VTODO\r\nUID:t\r\nDTSTAMP:20261118T090000Z\r\nEND:VTODO\r\nEND:VCALENDAR\r\n"),
		"many params": todoFile("X-P;" + strings.Repeat("A=b;", 3000) + "A=b:x"),
	} {
		if _, err := ReadTodos(data, Options{MaxZone: MaxZoneBytes}); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrLimit) {
			t.Errorf("%s: %v", name, err)
		}
	}
	f, err := ReadTodos(todoFile("DTSTART;VALUE=DATE:20261121", "COMPLETED:20261118T100000Z", "DUE;TZID=W. Europe Standard Time:20261121T100000"), Options{})
	if err != nil || f.Todos[0].Completed == nil || f.Todos[0].Due == nil || f.Todos[0].Due.Kind != tz.KindZoned || f.Todos[0].Due.Time.Location().String() != "Europe/Berlin" {
		t.Fatalf("windows zone: %+v %v", f, err)
	}
}
