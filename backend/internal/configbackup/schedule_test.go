package configbackup

import (
	"testing"
	"time"
)

func TestSchedule(t *testing.T) {
	zone, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, zone)
	s := Settings{Enabled: true, Schedule: "daily", Time: "03:00"}
	next := nextScheduled(s, now)
	if next == nil || next.Day() != 19 || next.Hour() != 3 {
		t.Fatal(next)
	}
	s.Schedule = "weekly"
	s.Weekday = 0
	next = nextScheduled(s, now)
	if next == nil || next.Day() != 25 || next.Hour() != 3 {
		t.Fatal(next)
	}
	s.Enabled = false
	if nextScheduled(s, now) != nil {
		t.Fatal("disabled")
	}
	for _, clock := range []string{"25:00", "3:00", "12:70", ""} {
		s.Enabled = true
		s.Time = clock
		if checkSchedule(s) == nil {
			t.Fatal(clock)
		}
	}
	s.Time = "03:00"
	s.Schedule = "yearly"
	if checkSchedule(s) == nil {
		t.Fatal("unknown schedule")
	}
}
