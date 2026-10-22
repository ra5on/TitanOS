package assistant

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAutomationDSTAndCalendar(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	tr := Trigger{Kind: "daily", Time: "02:30", Timezone: "Europe/Berlin"}
	for _, tc := range []struct{ at, want string }{{"2026-03-28T23:00:00+01:00", "2026-03-30T02:30:00+02:00"}, {"2026-10-25T02:45:00+02:00", "2026-10-26T02:30:00+01:00"}} {
		now, _ := time.Parse(time.RFC3339, tc.at)
		got := tr.NextAfter(now).In(loc).Format(time.RFC3339)
		// On the autumn day Go may pick either instance. Require at most one per
		// local date, even when the first instance has already passed.
		if got != tc.want {
			t.Fatalf("%s: %s != %s", tc.at, got, tc.want)
		}
	}
	tr.Kind = "weekly"
	tr.Weekday = 1
	tr.Time = "18:00"
	now, _ := time.Parse(time.RFC3339, "2026-10-23T12:00:00Z")
	if got := tr.NextAfter(now).In(loc).Format(time.RFC3339); got != "2026-10-26T18:00:00+01:00" {
		t.Fatal(got)
	}
}
func TestAutomationStoreIsolationAndServerState(t *testing.T) {
	base := t.TempDir()
	available := true
	s := NewAutomations(func() (string, bool) { return base, available })
	now := time.Now()
	a, err := s.Put("alex", Automation{Name: "Report", Prompt: "Read volumes", Enabled: true, Trigger: Trigger{Kind: "interval", Minutes: 60}, Runs: []AutomationRun{{Chat: "forged"}}, Pending: true, EventReady: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Runs) != 0 || a.Pending || a.EventReady || a.Revision != 1 {
		t.Fatalf("accepted server fields: %+v", a)
	}
	other, err := s.List("bob")
	if err != nil || len(other) != 0 {
		t.Fatal(other, err)
	}
	restarted := NewAutomations(func() (string, bool) { return base, available })
	list, err := restarted.List("alex")
	if err != nil || len(list) != 1 || list[0].ID != a.ID {
		t.Fatal(list, err)
	}
	a.Name = "Updated"
	updated, err := restarted.Put("alex", a, now)
	if err != nil || updated.Revision != 2 {
		t.Fatal(updated, err)
	}
	if _, err = restarted.Put("alex", a, now); err == nil {
		t.Fatal("stale edit accepted")
	}
	before, _ := os.ReadFile(filepath.Join(base, "alex", "automations", "definitions.json"))
	available = false
	if _, err = restarted.Put("alex", updated, now); err == nil {
		t.Fatal("RAM fallback")
	}
	after, _ := os.ReadFile(filepath.Join(base, "alex", "automations", "definitions.json"))
	if string(before) != string(after) {
		t.Fatal("modified while unavailable")
	}
}
func TestChatBodyKeepsLargeInteger(t *testing.T) {
	s := NewChats(func() (string, bool) { return "", false })
	c, _ := s.New("alex", "precision")
	c.Messages = []Entry{{Role: "tool", Action: &Action{Body: map[string]any{"size": json.Number("9007199254740993")}}}}
	if err := s.Save("alex", c); err != nil {
		t.Fatal(err)
	}
	loaded, err := s.Get("alex", c.ID)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(loaded.Messages[0].Action.Body)
	if string(b) != `{"size":9007199254740993}` {
		t.Fatal(string(b))
	}
}
