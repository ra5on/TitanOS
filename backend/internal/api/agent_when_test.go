package api

import (
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// "Stündlich" runs at the full hour of the NAS's time zone, once an hour,
// also where the zone is half an hour or 45 minutes off UTC and across the
// changes of summer time (1.01: in Asia/Kolkata it ran at :30). The minute
// tick is simulated over two days for each zone.
func TestAgentHourlyFullHourInEveryZone(t *testing.T) {
	cases := []struct {
		zone  string
		start string // local, the day before a change of summer time
	}{
		{"Asia/Kolkata", "2026-11-15 00:00"},
		{"Asia/Kathmandu", "2026-11-15 00:00"},
		{"Europe/Berlin", "2026-03-28 12:00"},
		{"Europe/Berlin", "2026-10-24 12:00"},
		{"America/St_Johns", "2026-11-01 00:00"},
		{"Australia/Lord_Howe", "2026-04-04 12:00"},
		{"Australia/Lord_Howe", "2026-10-03 12:00"},
	}
	for _, c := range cases {
		loc, err := time.LoadLocation(c.zone)
		if err != nil {
			t.Skipf("no zone data for %s: %v", c.zone, err)
		}
		start, _ := time.ParseInLocation("2006-01-02 15:04", c.start, loc)
		sc := agent.Schedule{Enabled: true, Every: "hourly", Created: start.Add(-time.Minute)}
		var runs []time.Time
		for now := start; now.Before(start.Add(48 * time.Hour)); now = now.Add(time.Minute) {
			if agentDue(sc, now) {
				runs = append(runs, now)
				sc.LastRun = now
			}
		}
		if len(runs) < 47 || len(runs) > 48 {
			t.Errorf("%s from %s: %d runs in 48 hours", c.zone, c.start, len(runs))
		}
		for i, r := range runs {
			if r.In(loc).Minute() != 0 {
				t.Errorf("%s: run at %s, not at the full hour", c.zone, r.In(loc).Format("2006-01-02 15:04 MST"))
				break
			}
			if i > 0 && r.Sub(runs[i-1]) < 59*time.Minute {
				t.Errorf("%s: two runs %s and %s", c.zone, runs[i-1].In(loc).Format("15:04 MST"), r.In(loc).Format("15:04 MST"))
				break
			}
		}
	}
	// The next run shown on the page is the next full hour of the zone.
	loc, _ := time.LoadLocation("Asia/Kolkata")
	after := time.Date(2026, 11, 16, 10, 10, 0, 0, loc)
	next, ok := agentNext(agent.Schedule{Every: "hourly"}, after, loc)
	if !ok || !next.Equal(time.Date(2026, 11, 16, 11, 0, 0, 0, loc)) {
		t.Fatalf("next after 10:10 in Kolkata: %s", next.In(loc))
	}
}
