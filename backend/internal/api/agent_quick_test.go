package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

func TestAgentQuickFindAndPlan(t *testing.T) {
	var all []quickEntity
	if err := json.Unmarshal([]byte(`[
		{"id":"light.wohnzimmer_decke","name":"Deckenlicht","area":"Wohnzimmer","state":"off","policy":{}},
		{"id":"light.stehlampe","name":"Stehlampe","area":"Wohnzimmer","state":"on","policy":{"aliases":["Leselampe"]}},
		{"id":"light.kueche","name":"Küche Licht","area":"Küche","state":"off","policy":{}},
		{"id":"switch.kaffee","name":"Kaffeemaschine","area":"Küche","state":"off","policy":{}},
		{"id":"switch.kaffee2","name":"Kaffeemaschine","area":"Büro","state":"off","policy":{}},
		{"id":"media_player.tv","name":"Fernseher","area":"Wohnzimmer","state":"playing","policy":{}},
		{"id":"cover.markise","name":"Markise","area":"Terrasse","state":"closed","policy":{}},
		{"id":"lock.haustuer","name":"Haustür","area":"Flur","state":"locked","policy":{}},
		{"id":"light.nur_lesen","name":"Flurlicht","area":"Flur","state":"on","policy":{"readOnly":true}}
	]`), &all); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		say, tool, args, answer string
	}{
		{"Stehlampe an", "ha_call", `"service":"turn_on"`, "Stehlampe ist an."},
		{"Schalte die Leselampe aus", "ha_call", `"entities":["light.stehlampe"]`, "Stehlampe ist aus."},
		{"Küchenlicht an", "", "", ""}, // "Küche Licht" is not "Küchenlicht": the model decides
		{"Küche Licht an", "ha_call", `"entities":["light.kueche"]`, "Küche Licht ist an."},
		{"Schalte das Licht im Wohnzimmer aus", "ha_call", `"entities":["light.wohnzimmer_decke","light.stehlampe"]`, "2 Lichter in Wohnzimmer sind aus."},
		{"Stehlampe auf 40 Prozent", "ha_call", `"brightness_pct":40`, "Stehlampe steht auf 40 %."},
		{"Wohnzimmer Deckenlicht an", "ha_call", `"entities":["light.wohnzimmer_decke"]`, "Deckenlicht ist an."},
		{"Kaffeemaschine an", "", "", ""}, // two of that name
		{"Küche Kaffeemaschine an", "ha_call", `"entities":["switch.kaffee"]`, "Kaffeemaschine ist an."},
		{"Öffne die Markise", "ha_call", `"service":"open_cover"`, "Markise fährt auf."},
		{"Haustür auf", "", "", ""}, // locks are not quick
		{"Öffne die Haustür", "", "", ""},
		{"Flurlicht aus", "", "", ""}, // read-only device
		{"Gartenlicht an", "", "", ""},
		{"turn off the Stehlampe", "ha_call", `"service":"turn_off"`, "Stehlampe is off."},
	} {
		in := agent.ParseQuick(c.say)
		if in == nil {
			if c.tool != "" {
				t.Errorf("%q: not read", c.say)
			}
			continue
		}
		step, ok := quickPlan(in, quickFind(in, all))
		if c.tool == "" {
			if ok {
				t.Errorf("%q: planned %s %s, want nothing", c.say, step.Tool, step.Args)
			}
			continue
		}
		if !ok || step.Tool != c.tool || !strings.Contains(string(step.Args), c.args) {
			t.Errorf("%q: ok %v %s %s, want %s with %s", c.say, ok, step.Tool, step.Args, c.tool, c.args)
			continue
		}
		if got := step.Say(""); got != c.answer {
			t.Errorf("%q: says %q, want %q", c.say, got, c.answer)
		}
	}
	// A question reads the state and says it in words.
	in := agent.ParseQuick("Ist der Fernseher an?")
	step, ok := quickPlan(in, quickFind(in, all))
	if !ok || step.Tool != "ha_state" || !strings.Contains(string(step.Args), "media_player.tv") {
		t.Fatalf("state: %v %+v", ok, step)
	}
	if got := step.Say(`{"entity_id":"media_player.tv","state":"playing"}`); got != "Fernseher läuft." {
		t.Errorf("state says %q", got)
	}
	if got := step.Say(`{"state":"buffering"}`); got != "" {
		t.Errorf("unknown state says %q, want the model", got)
	}
	// Reminders and timers.
	r := quickReminder(agent.ParseQuick("Erinnere mich in 10 Minuten an die Pizza im Ofen"))
	if r.Tool != "schedule_create" || !strings.Contains(string(r.Args), `"in_minutes":10`) || !strings.Contains(string(r.Args), `"kind":"reminder"`) || !strings.Contains(r.Say(""), "die Pizza im Ofen") {
		t.Errorf("reminder: %s %s %q", r.Tool, r.Args, r.Say(""))
	}
	if tm := quickReminder(agent.ParseQuick("Timer 90 Minuten")); !strings.Contains(tm.Say(""), "1 Stunde 30 Minuten") {
		t.Errorf("timer says %q", tm.Say(""))
	}
}

// A whole chat: "Timer 5 Minuten" is answered without one call to the
// model and plans the reminder; with the quick commands switched off the
// same message goes to the model.
func TestAgentQuickInChat(t *testing.T) {
	fake := &agentOllama{replies: []string{`{"role":"assistant","content":"Vom Modell."}`}}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	ask := func() agent.Chat {
		t.Helper()
		res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Timer 5 Minuten"})
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("chat: %d %s", res.StatusCode, body)
		}
		var chat agent.Chat
		json.Unmarshal(body, &chat)
		return waitChat(t, ts, ts.client, chat.ID)
	}
	chats := func() int {
		fake.mu.Lock()
		defer fake.mu.Unlock()
		n := 0
		for _, g := range fake.got {
			if _, ok := g["messages"]; ok {
				n++
			}
		}
		return n
	}
	before := chats()
	chat := ask()
	last := chat.Entries[len(chat.Entries)-1]
	if chats() != before || last.Role != "assistant" || !strings.HasPrefix(last.Content, "Timer gestellt: 5 Minuten") {
		t.Fatalf("quick: model asked %d times, entries %+v", chats()-before, chat.Entries)
	}
	if len(chat.Entries) < 3 || chat.Entries[1].Tool != "schedule_create" || !chat.Entries[1].OK {
		t.Fatalf("step: %+v", chat.Entries)
	}
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	settings["quickOff"] = true
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != 200 {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	chat = ask()
	if last := chat.Entries[len(chat.Entries)-1]; last.Content != "Vom Modell." {
		t.Fatalf("switched off: %+v", chat.Entries)
	}
}
