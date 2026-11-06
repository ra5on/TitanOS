package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// agentTick with a chosen clock: a schedule that is due starts once, a run
// missed while the NAS was off is caught up once, marked "(nachgeholt)".
// Reminders are used, so no model runs. The hours stay below 3 so the
// nightly map (03:00), the reflection (03:30) and the storage forecast
// (from 04:00) do not start.
func TestCovAgentTickStartsOnceAndCatchesUpOnce(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	store := ts.server.agentStore()
	loc := ts.server.agentLocation()
	day := func(d, h, m int) time.Time { return time.Date(2026, 11, d, h, m, 0, 0, loc) }

	list := []agent.Schedule{
		{ID: agent.NewID(), Name: "Pünktlich", Task: "Blumen gießen", Kind: "reminder", Every: "daily", At: "02:00", Enabled: true, Created: day(7, 12, 0)},
		{ID: agent.NewID(), Name: "Verpasst", Task: "Müll", Kind: "reminder", Every: "daily", At: "00:30", Enabled: true, Created: day(1, 12, 0), LastRun: day(8, 0, 30)},
		{ID: agent.NewID(), Name: "Aus", Task: "nie", Kind: "reminder", Every: "daily", At: "02:00", Enabled: false, Created: day(1, 12, 0)},
	}
	if err := store.SaveSchedules("alex", list); err != nil {
		t.Fatal(err)
	}
	// 10.11. (Tuesday): 02:00 is the regular slot of the first, 00:30 of
	// the second was missed (and the one of the 9th lies beyond the window).
	ts.server.agentTick(day(10, 2, 0))
	ts.server.agentTick(day(10, 2, 1))
	ts.server.agentTick(day(10, 2, 15))

	reminders := func() []agent.ChatInfo {
		chats, err := store.Chats("alex")
		if err != nil {
			t.Fatal(err)
		}
		var out []agent.ChatInfo
		for _, c := range chats {
			if c.Origin == "reminder" {
				out = append(out, c)
			}
		}
		return out
	}
	got := reminders()
	if len(got) != 2 {
		t.Fatalf("reminder chats: %+v", got)
	}
	var onTime, late int
	for _, c := range got {
		switch {
		case strings.HasPrefix(c.Title, "Pünktlich") && !strings.Contains(c.Title, "nachgeholt"):
			onTime++
		case strings.HasPrefix(c.Title, "Verpasst") && strings.HasSuffix(c.Title, "(nachgeholt)"):
			late++
		default:
			t.Errorf("unexpected chat %q", c.Title)
		}
	}
	if onTime != 1 || late != 1 {
		t.Fatalf("on time %d, late %d", onTime, late)
	}
	saved, _ := store.Schedules("alex")
	for _, sc := range saved {
		if sc.Enabled && (sc.Chat == "" || !sc.LastOK || sc.LastRun.IsZero()) {
			t.Errorf("%s: result not kept %+v", sc.Name, sc)
		}
		if !sc.Enabled && !sc.LastRun.IsZero() {
			t.Errorf("disabled schedule ran")
		}
	}

	// Next day: each regular slot again, once.
	ts.server.agentTick(day(11, 0, 30))
	ts.server.agentTick(day(11, 0, 31))
	ts.server.agentTick(day(11, 2, 0))
	ts.server.agentTick(day(11, 2, 5))
	if n := len(reminders()); n != 4 {
		t.Fatalf("after the next day: %d reminder chats", n)
	}
	for _, c := range reminders() {
		if strings.Contains(c.Title, "· 11.11.") && strings.Contains(c.Title, "nachgeholt") {
			t.Fatalf("regular run marked late: %q", c.Title)
		}
	}
}

// A switched-off agent plans nothing.
func TestCovAgentTickOffDoesNothing(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	store := ts.server.agentStore()
	loc := ts.server.agentLocation()
	at := time.Date(2026, 11, 10, 2, 0, 0, 0, loc)
	if err := store.SaveSchedules("alex", []agent.Schedule{{ID: agent.NewID(), Name: "x", Task: "y", Kind: "reminder", Every: "daily", At: "02:00", Enabled: true, Created: at.Add(-time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["enabled"] = false
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != 200 {
		t.Fatalf("switch off: %d %s", res.StatusCode, body)
	}
	ts.server.agentTick(at)
	chats, _ := store.Chats("alex")
	if len(chats) != 0 {
		t.Fatalf("switched-off agent started %d chats", len(chats))
	}
}
