package nvr

import (
	"testing"
	"time"
)

func TestOnEventNotifiesOnlyWhenArmedAndNotTooOften(t *testing.T) {
	s := newTestStore(t)
	m := &Manager{Store: s}
	var got []string
	m.Notify = func(sev, kind, subject string) { got = append(got, kind+":"+subject) }
	cam := Camera{ID: "cam-1", Name: "Hof"}
	start := Event{Camera: "cam-1", Kind: "motion", Start: time.Now()}

	m.onEvent(cam, start) // not armed
	if len(got) != 0 {
		t.Fatalf("notified while disarmed: %v", got)
	}
	st := s.Settings()
	st.Arming = Arming{Mode: "on"}
	if err := s.SetSettings(st); err != nil {
		t.Fatal(err)
	}
	m.onEvent(cam, start)
	m.onEvent(cam, start) // within five minutes
	end := start
	end.End = time.Now()
	m.onEvent(cam, end) // the end of an event is no news
	if len(got) != 1 || got[0] != "nvr_motion:Hof" {
		t.Fatalf("notifications: %v", got)
	}
	m.onEvent(Camera{ID: "cam-2", Name: "Garten"}, start) // another camera has its own limit
	if len(got) != 2 {
		t.Fatalf("notifications: %v", got)
	}
}
