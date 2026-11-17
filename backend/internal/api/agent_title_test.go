package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// 1.07: a chat is named by its whole question, then by the model's short
// title; a name the person gave stays; a failing model leaves the question;
// an old title with " …" gets a title from the model once when opened.
func TestAgentChatTitleFromModel(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	question := "Kannst du bitte einmal nachsehen, wie es meinen Apps, VMs und Containern geht und ob irgendwo Fehler in den Protokollen stehen?"
	start := func(message string) agent.Chat {
		t.Helper()
		res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": message})
		if res.StatusCode != http.StatusAccepted && res.StatusCode != http.StatusCreated {
			t.Fatalf("chat: %d %s", res.StatusCode, body)
		}
		var c agent.Chat
		json.Unmarshal(body, &c)
		if c.Title != strings.Join(strings.Fields(message), " ") {
			t.Fatalf("the first title is not the whole question: %q", c.Title)
		}
		waitChat(t, ts, ts.client, c.ID)
		ts.server.agentRT.work.Wait()
		got, err := ts.server.agentStore().Chat("alex", c.ID)
		if err != nil {
			t.Fatal(err)
		}
		return *got
	}

	fake.title = "„Apps, VMs und Container: Zustand und Fehler.“"
	chat := start(question)
	if chat.Title != "Apps, VMs und Container: Zustand und Fehler" || !chat.TitleMade {
		t.Fatalf("title %q made=%v", chat.Title, chat.TitleMade)
	}
	fake.mu.Lock()
	asked := len(fake.titles)
	if asked != 1 || !strings.Contains(fake.titles[0], "Protokollen") || !strings.Contains(fake.titles[0], "Fertig.") {
		t.Fatalf("title requests %q", fake.titles)
	}
	fake.mu.Unlock()
	// A further answer does not rename it again.
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+chat.ID+"/messages", map[string]any{"message": "Und die Backups?"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("message: %d %s", res.StatusCode, body)
	}
	waitChat(t, ts, ts.client, chat.ID)
	ts.server.agentRT.work.Wait()
	fake.mu.Lock()
	if len(fake.titles) != 1 {
		t.Fatalf("asked again: %d", len(fake.titles))
	}
	fake.mu.Unlock()

	// The model fails: the whole question stays, nothing with "…".
	fake.mu.Lock()
	fake.titleFail = true
	fake.mu.Unlock()
	chat = start(question + " Danke!")
	if chat.Title != question+" Danke!" || chat.TitleMade || strings.Contains(chat.Title, "…") {
		t.Fatalf("after a failure: %q", chat.Title)
	}
	// An answer that is no title (too long) is not taken either.
	fake.mu.Lock()
	fake.titleFail = false
	fake.title = strings.Repeat("sehr ", 20) + "lang"
	fake.mu.Unlock()
	chat = start("Wie voll ist das Volume?")
	if chat.Title != "Wie voll ist das Volume?" {
		t.Fatalf("a long answer became the title: %q", chat.Title)
	}

	// A chat the person named is never renamed; an old " …" title is,
	// once, when the chat is opened.
	fake.mu.Lock()
	fake.title = "Speicher und Volumes"
	fake.mu.Unlock()
	store := ts.server.agentStore()
	old := &agent.Chat{ID: agent.NewID(), Title: "Wie voll ist mein Volume und was belegt den …", Origin: "web", State: agent.StateIdle,
		Entries: []agent.Entry{{ID: "u", Role: "user", Content: "Wie voll ist mein Volume und was belegt den meisten Platz darauf?"}, {ID: "a", Role: "assistant", Content: "Zu 71 % voll."}}}
	named := *old
	named.ID, named.Title, named.TitleOwn = agent.NewID(), "Mein Volume …", true
	for _, c := range []*agent.Chat{old, &named} {
		if err := store.SaveChat("alex", c); err != nil {
			t.Fatal(err)
		}
	}
	res, body := ts.request("GET", "/api/v1/agent/chats/"+old.ID, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"titling":true`) {
		t.Fatalf("open old: %d %s", res.StatusCode, body)
	}
	ts.server.agentRT.work.Wait()
	if c, _ := store.Chat("alex", old.ID); c.Title != "Speicher und Volumes" || !c.TitleMade {
		t.Fatalf("old title: %q", c.Title)
	}
	ts.request("GET", "/api/v1/agent/chats/"+named.ID, nil)
	ts.server.agentRT.work.Wait()
	if c, _ := store.Chat("alex", named.ID); c.Title != "Mein Volume …" {
		t.Fatalf("a named chat was renamed: %q", c.Title)
	}
	// Renaming marks the chat as named by the person.
	if res, body := ts.request("PATCH", "/api/v1/agent/chats/"+chat.ID, map[string]any{"title": "Volume"}); res.StatusCode != http.StatusOK {
		t.Fatalf("rename: %d %s", res.StatusCode, body)
	}
	if c, _ := store.Chat("alex", chat.ID); !c.TitleOwn {
		t.Fatal("a renamed chat is not marked")
	}
}

func TestCleanTitle(t *testing.T) {
	for in, want := range map[string]string{
		"Titel: Backups prüfen.":           "Backups prüfen",
		"\n\n\"Docker-Fehler\"\nmore text": "Docker-Fehler",
		"**Speicher voll?**":               "Speicher voll",
		"Zu lang …":                        "Zu lang",
		"Erst … dann":                      "",
		"":                                 "",
	} {
		if got := cleanTitle(in); got != want {
			t.Errorf("cleanTitle(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("Wort ", 200)
	if got := chatTitle(long); strings.Contains(got, "…") || len([]rune(got)) > chatTitleMax {
		t.Fatalf("long question: %q", got)
	}
}
