package api

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// PLAN 90-4: pinning, topics, "zuletzt genutzt", editing a message and the
// state of the NAS for the start page.

func TestPlan90MemoryPinTopicClear(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	list := func() []agent.Memory {
		_, body := ts.request("GET", "/api/v1/agent/memory", nil)
		var out struct{ Memory []agent.Memory }
		json.Unmarshal(body, &out)
		return out.Memory
	}
	if res, body := ts.request("PUT", "/api/v1/agent/memory/new", map[string]any{"text": "Der Drucker steht im Flur.", "topic": " Büro  und Geräte ", "pinned": true}); res.StatusCode != http.StatusOK {
		t.Fatalf("new: %d %s", res.StatusCode, body)
	}
	ts.request("PUT", "/api/v1/agent/memory/new", map[string]any{"text": "Fotos liegen in Photos."})
	mem := list()
	if len(mem) != 2 || !mem[0].Pinned || mem[0].Topic != "Büro und Geräte" || mem[1].Pinned || mem[1].Topic != "" {
		t.Fatalf("memory = %+v", mem)
	}
	// Pinned and topic change without the text; the date stays.
	at := mem[1].At
	if res, body := ts.request("PUT", "/api/v1/agent/memory/"+mem[1].ID, map[string]any{"pinned": true, "topic": "Dateien"}); res.StatusCode != http.StatusOK {
		t.Fatalf("pin: %d %s", res.StatusCode, body)
	}
	if got := list()[1]; !got.Pinned || got.Topic != "Dateien" || got.Text != "Fotos liegen in Photos." || !got.At.Equal(at) {
		t.Fatalf("after pin = %+v", got)
	}
	ts.request("PUT", "/api/v1/agent/memory/"+mem[1].ID, map[string]any{"pinned": false})
	for _, bad := range []map[string]any{{}, {"topic": string(make([]byte, 61))}, {"text": ""}} {
		if res, _ := ts.request("PUT", "/api/v1/agent/memory/"+mem[1].ID, bad); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%v: %d", bad, res.StatusCode)
		}
	}
	// "Gedächtnis leeren" keeps what is pinned.
	if res, _ := ts.request("DELETE", "/api/v1/agent/memory", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("clear: %d", res.StatusCode)
	}
	if got := list(); len(got) != 1 || got[0].Text != "Der Drucker steht im Flur." {
		t.Fatalf("after clear = %+v", got)
	}
}

func TestPlan90MemoryTouchAndFade(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	old := now.AddDate(0, -8, 0)
	list := []agent.Memory{
		{ID: "a", Text: "Home Assistant läuft als VM HAOS.", At: old},
		{ID: "b", Text: "Der Drucker steht im Flur.", At: old},
		{ID: "c", Text: "Zigbee", At: old},
	}
	if !memoryTouch(list, "", "Wie geht es Home Assistant und Zigbee heute?", now) {
		t.Fatal("nothing touched")
	}
	if list[0].Used == nil || list[1].Used != nil || list[2].Used == nil {
		t.Fatalf("used = %v %v %v", list[0].Used, list[1].Used, list[2].Used)
	}
	// The same day again writes nothing.
	if memoryTouch(list, "", "Home Assistant, Zigbee", now.Add(time.Hour)) {
		t.Fatal("touched twice a day")
	}
	if memoryTouch(list, "", "", now) || memoryTouch(list, "", "Und der Flur?", now) {
		t.Fatal("one common word of a longer memory is no use")
	}
	// A memory of another project was not given to this chat.
	other := []agent.Memory{{ID: "p", Text: "Der Drucker steht im Flur.", Project: "p1", At: old}}
	if memoryTouch(other, "p2", "Wo steht der Drucker, im Flur?", now) || !memoryTouch(other, "p1", "Wo steht der Drucker, im Flur?", now) {
		t.Fatal("only the chat's project counts")
	}
	if got := memoryFresh(list[0]); !got.Equal(now) {
		t.Fatalf("fresh = %v", got)
	}
	if got := memoryFresh(list[1]); !got.Equal(old) {
		t.Fatalf("fresh unused = %v", got)
	}

	// Fading asks only about what is old, unused and not pinned.
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	list[1].Pinned = true
	list = append(list, agent.Memory{ID: "d", Text: "Die USV hängt am Server.", At: old})
	if err := ts.server.agentStore().SaveMemories("alex", list); err != nil {
		t.Fatal(err)
	}
	ts.server.reflectFade("alex", agentInitiative{FadeMonths: 6}, now)
	inbox := ts.server.agentInbox("alex")
	if len(inbox) != 1 || inbox[0].Data["memory"] != "d" {
		t.Fatalf("inbox = %+v", inbox)
	}
}

func TestPlan90ReplaceMessage(t *testing.T) {
	fake := &agentOllama{replies: []string{`{"role":"assistant","content":"Erste Antwort."}`}}
	ts := agentServer(t, fake, agent.Policy{})
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Wie geht es dem NAS?", "replace": "x"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("new: %d %s", res.StatusCode, body)
	}
	var chat agent.Chat
	json.Unmarshal(body, &chat)
	chat = waitChat(t, ts, ts.client, chat.ID)
	first := chat.Entries[0]
	fake.replies = []string{`{"role":"assistant","content":"Zweite Antwort."}`}
	ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/messages", map[string]any{"message": "Und die Platten?"})
	chat = waitChat(t, ts, ts.client, chat.ID)
	if len(chat.Entries) != 4 {
		t.Fatalf("entries = %d", len(chat.Entries))
	}
	second := chat.Entries[2]

	// Neu erzeugen: the last question again, its old answer is gone.
	fake.replies = []string{`{"role":"assistant","content":"Neue Antwort."}`}
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/messages", map[string]any{"replace": second.ID}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("regenerate: %d %s", res.StatusCode, body)
	}
	chat = waitChat(t, ts, ts.client, chat.ID)
	if len(chat.Entries) != 4 || chat.Entries[2].Content != "Und die Platten?" || chat.Entries[3].Content != "Neue Antwort." || chat.Entries[1].Content != "Erste Antwort." {
		t.Fatalf("after regenerate = %+v", chat.Entries)
	}

	// Bearbeiten: the first message changes, everything after it goes.
	fake.replies = []string{`{"role":"assistant","content":"Ganz neu."}`}
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/messages", map[string]any{"message": "Wie geht es den Sicherungen?", "replace": first.ID}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("edit: %d %s", res.StatusCode, body)
	}
	chat = waitChat(t, ts, ts.client, chat.ID)
	if len(chat.Entries) != 2 || chat.Entries[0].Content != "Wie geht es den Sicherungen?" || chat.Entries[1].Content != "Ganz neu." {
		t.Fatalf("after edit = %+v", chat.Entries)
	}

	// Only a message of the person in this chat can be replaced.
	for _, id := range []string{chat.Entries[1].ID, first.ID, "nope"} {
		if res, _ := ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/messages", map[string]any{"message": "x", "replace": id}); res.StatusCode != http.StatusNotFound {
			t.Fatalf("replace %s: %d", id, res.StatusCode)
		}
	}
	if res, _ := ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/messages", map[string]any{"message": ""}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty message: %d", res.StatusCode)
	}
}

func TestPlan90Home(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	res, body := ts.request("GET", "/api/v1/agent/home", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("home: %d %s", res.StatusCode, body)
	}
	var out struct {
		State *struct {
			At      time.Time
			Volumes []agentHomeVolume
		}
	}
	json.Unmarshal(body, &out)
	if out.State == nil || out.State.At.IsZero() || out.State.Volumes == nil {
		t.Fatalf("owner state = %s", body)
	}
	// A plain account gets no state of the NAS.
	guest := covGuest(t, ts)
	res, body = ts.do(guest, "GET", "/api/v1/agent/home", nil)
	if res.StatusCode != http.StatusOK || string(body) != `{"state":null}`+"\n" {
		t.Fatalf("guest: %d %q", res.StatusCode, body)
	}
}
