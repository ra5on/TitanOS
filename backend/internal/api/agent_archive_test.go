package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 72: a chat goes to the Archiv and back; archived chats are listed
// with the flag (the page shows them under Archiv) and survive trimming.
func TestAgentChatArchive(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	store := ts.server.agentStore()
	now := time.Now()
	chat := &agent.Chat{ID: agent.NewID(), Title: "Steuer", Created: now, Updated: now, State: agent.StateIdle}
	if err := store.SaveChat("alex", chat); err != nil {
		t.Fatal(err)
	}
	res, body := ts.request("PATCH", "/api/v1/agent/chats/"+chat.ID, map[string]any{"archived": true})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("archive: %d %s", res.StatusCode, body)
	}
	_, body = ts.request("GET", "/api/v1/agent/chats", nil)
	var list struct{ Chats []agent.ChatInfo }
	json.Unmarshal(body, &list)
	if len(list.Chats) != 1 || !list.Chats[0].Archived {
		t.Fatalf("list: %s", body)
	}
	if res, _ := ts.request("PATCH", "/api/v1/agent/chats/"+chat.ID, map[string]any{"archived": false}); res.StatusCode != http.StatusOK {
		t.Fatal(res.StatusCode)
	}
	if c, _ := store.Chat("alex", chat.ID); c.Archived {
		t.Fatal("not taken back")
	}
}

// PLAN 72: a planned task chooses for itself whether it may wake a
// sleeping disk; empty keeps the account's choice, other chats never use it.
func TestScheduleWakeChoice(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	for _, bad := range []string{"always", "WAKE"} {
		if res, _ := ts.request("PUT", "/api/v1/agent/schedules/new", map[string]any{"name": "Fotos", "task": "Fotos sortieren", "every": "daily", "at": "03:00", "enabled": true, "wake": bad}); res.StatusCode != http.StatusBadRequest {
			t.Errorf("wake %q: %d", bad, res.StatusCode)
		}
	}
	res, body := ts.request("PUT", "/api/v1/agent/schedules/new", map[string]any{"name": "Fotos", "task": "Fotos sortieren", "every": "daily", "at": "03:00", "enabled": true, "wake": "never"})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	list, _ := ts.server.agentStore().Schedules("alex")
	if len(list) != 1 || list[0].Wake != "never" {
		t.Fatalf("%+v", list)
	}
	id := list[0].ID
	if w := ts.server.scheduleWake("alex", &agent.Chat{Origin: "schedule", Schedule: id}); w != "never" {
		t.Fatalf("schedule chat: %q", w)
	}
	for _, c := range []*agent.Chat{nil, {Origin: "web", Schedule: id}, {Origin: "schedule"}, {Origin: "schedule", Schedule: "gone"}} {
		if w := ts.server.scheduleWake("alex", c); w != "" {
			t.Errorf("%+v: %q", c, w)
		}
	}
	// A run of the task carries it in its chat.
	chatID, err := ts.server.agentRunSchedule(mustUser(t, ts, "alex"), id, time.Now(), false)
	if err != nil {
		t.Fatal(err)
	}
	// The run goes on in the background; it must end before the test
	// removes its folders.
	for until := time.Now().Add(10 * time.Second); ts.server.agentRunning("alex", chatID) && time.Now().Before(until); {
		time.Sleep(20 * time.Millisecond)
	}
	if ts.server.agentRunning("alex", chatID) {
		t.Fatal("the run did not end")
	}
	if c, _ := ts.server.agentStore().Chat("alex", chatID); c.Schedule != id {
		t.Fatalf("chat of the run: %q", c.Schedule)
	}
}

// PLAN 76 (F-11-4): a planned run that cannot start says so in its chat and
// is not left as "last run: ok".
func TestScheduleThatCannotStartIsRecorded(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	res, body := ts.request("PUT", "/api/v1/agent/schedules/new", map[string]any{"name": "Fotos", "task": "Fotos sortieren", "every": "daily", "at": "03:00", "enabled": true})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	store := ts.server.agentStore()
	list, _ := store.Schedules("alex")
	id := list[0].ID
	if err := store.UpdateSchedule("alex", id, func(sc *agent.Schedule) { sc.LastOK = true }); err != nil {
		t.Fatal(err)
	}
	// The agent is switched off while the task is due.
	ts.server.agentSettings()
	ts.server.agentConf.mu.Lock()
	ts.server.agentConf.settings.Enabled = false
	ts.server.agentConf.mu.Unlock()
	chatID, err := ts.server.agentRunSchedule(mustUser(t, ts, "alex"), id, time.Now(), false)
	if err == nil {
		t.Fatal("a run with the agent off started")
	}
	list, _ = store.Schedules("alex")
	chat, cerr := store.Chat("alex", chatID)
	if cerr != nil || list[0].LastOK || chat.State != agent.StateError || len(chat.Entries) != 2 || !strings.Contains(chat.Entries[1].Content, "ausgeschaltet") {
		t.Fatalf("schedule %+v\nchat %+v %v", list[0], chat, cerr)
	}
}
