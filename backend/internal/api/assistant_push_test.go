package api

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
)

// waitAgentsIdle waits until no assistant task runs, so that none still
// writes into the test's folders while they are removed.
func waitAgentsIdle(t *testing.T, ts *testServer) {
	t.Helper()
	for range 1000 {
		ts.server.agentMu.Lock()
		running := len(ts.server.agentJobs)
		ts.server.agentMu.Unlock()
		if running == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("assistant tasks still running")
}

var pushURL = regexp.MustCompile(`https://nas\.local(/api/v1/assistant/push/[0-9a-f]{64})/approve`)

func TestApproveFromNotification(t *testing.T) {
	sink := &pushSink{}
	push := sink.start(t)
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		if n == 1 {
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"file_write","arguments":{"root":"share/Docs","path":"from-phone.txt","content":"approved on the phone","format":"text","reason":"Save the note"}}}]}`}
		}
		return []string{`{"role":"assistant","content":"Saved."}`}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b", "pushApproval": true})
	ts.request("PUT", "/api/v1/account/notifications", map[string]any{"language": "en", "content": "summary",
		"ntfy": map[string]any{"enabled": true, "url": push.URL + "/alex"}, "events": map[string]bool{"confirmation": true}})
	// The browser's address becomes the origin of links; here it is set directly.
	p, _, _ := ts.server.opts.Notify.PersonalSettings("alex")
	p.Origin = "https://nas.local"
	ts.server.opts.Notify.SetPersonal("alex", p)

	id := ts.newChat(ts.client)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "save a note"})
	sink.wait(t, "Approval needed")
	sink.mu.Lock()
	match := pushURL.FindStringSubmatch(strings.Join(sink.actions, " "))
	sink.mu.Unlock()
	if match == nil {
		t.Fatalf("no approve button: %v", sink.actions)
	}
	anonymous := &http.Client{}
	if res, _ := ts.do(anonymous, "POST", match[1]+"/maybe", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown decision: %d", res.StatusCode)
	}
	// Without login, with the button's token.
	if res, body := ts.do(anonymous, "POST", match[1]+"/approve", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("approve: %d %s", res.StatusCode, body)
	}
	for range 300 {
		if data, err := os.ReadFile(filepath.Join(dir, "from-phone.txt")); err == nil && string(data) == "approved on the phone" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "from-phone.txt")); string(data) != "approved on the phone" {
		t.Fatal("not executed after approval")
	}
	if res, _ := ts.do(anonymous, "POST", match[1]+"/approve", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("token used twice: %d", res.StatusCode)
	}
	waitAgentsIdle(t, ts)
	expired := ts.server.push.add(pushToken{user: "alex", chat: id, action: "x", expires: time.Now().Add(-time.Second)})
	if res, _ := ts.do(anonymous, "POST", "/api/v1/assistant/push/"+expired+"/approve", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("expired token: %d", res.StatusCode)
	}
}

func TestPushButtonsOnlyWhenSwitchedOnAndNeverForDestroy(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	u, _ := ts.users.Get("alex")
	chat := &assistant.Chat{ID: "c1"}
	a := &assistant.Action{ID: "a1", Level: assistant.LevelDanger}
	if got := ts.server.pushActions(u, chat, a, "https://nas.local", "de"); got != nil {
		t.Fatal("buttons while switched off")
	}
	settings := ts.server.opts.Assistant.Settings()
	settings.PushApproval = true
	ts.server.opts.Assistant.SetSettings(settings)
	if got := ts.server.pushActions(u, chat, a, "https://nas.local", "de"); len(got) != 2 || got[0].Label != "Freigeben" || !strings.HasPrefix(got[1].HAAction, "TITANOS_REJECT_") {
		t.Fatalf("%+v", got)
	}
	a.Level = assistant.LevelDestroy
	if got := ts.server.pushActions(u, chat, a, "https://nas.local", "de"); got != nil {
		t.Fatal("buttons for a destructive action")
	}
}

func TestHomeAssistantAssistAndAppButtons(t *testing.T) {
	fakeHA := &fakeHAServer{}
	ha := fakeHA.start(t)
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		if strings.Contains(lastContent(body), "delete") {
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"file_write","arguments":{"root":"share/Docs","path":"x.txt","content":"x","format":"text","reason":"Write x"}}}]}`}
		}
		return []string{`{"role":"assistant","content":"Das **NAS** ist zu 42 % belegt."}`}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ts.home.Run(ctx)
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b", "pushApproval": true})
	ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"enabled": true, "url": ha.URL, "token": "ha-token", "account": "alex", "assist": true})

	fakeHA.fire(t, "titanos_ask", map[string]string{"id": "q1", "text": "Wie voll ist das NAS?"})
	answer := func(want string) {
		t.Helper()
		for range 500 {
			fakeHA.mu.Lock()
			all := strings.Join(fakeHA.events, "\n")
			fakeHA.mu.Unlock()
			if strings.Contains(all, want) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("no answer with %q: %v", want, fakeHA.events)
	}
	answer(`"text":"Das NAS ist zu 42 % belegt."`)
	answer(`/api/events/titanos_answer`)

	// A change needs the approval; the answer says so, and the button of the
	// Home Assistant app rejects it.
	fakeHA.fire(t, "titanos_ask", map[string]string{"id": "q2", "text": "delete something"})
	answer(`I need your approval in TitanOS for this. Write x`)
	list, _ := ts.server.chats.List("alex", "")
	var waiting *assistant.Chat
	for _, summary := range list {
		if c, err := ts.server.chats.Get("alex", summary.ID); err == nil && assistant.Pending(c) != nil {
			waiting = c
		}
	}
	if waiting == nil {
		t.Fatal("no waiting chat")
	}
	u, _ := ts.users.Get("alex")
	buttons := ts.server.pushActions(u, waiting, assistant.Pending(waiting), "", "en")
	fakeHA.fire(t, "mobile_app_notification_action", map[string]string{"action": buttons[1].HAAction})
	for range 300 {
		if c, _ := ts.server.chats.Get("alex", waiting.ID); assistant.Pending(c) == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if c, _ := ts.server.chats.Get("alex", waiting.ID); assistant.Pending(c) != nil {
		t.Fatal("still waiting after reject")
	}
	waitAgentsIdle(t, ts)
	if _, err := os.Stat(filepath.Join(dir, "x.txt")); err == nil {
		t.Fatal("rejected action was executed")
	}
}
