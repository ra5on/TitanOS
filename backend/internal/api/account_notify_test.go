package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type pushSink struct {
	mu      sync.Mutex
	bodies  []string
	click   []string
	actions []string
}

func (p *pushSink) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		p.mu.Lock()
		p.bodies = append(p.bodies, r.Header.Get("Title")+"|"+string(body))
		p.click = append(p.click, r.Header.Get("Click"))
		p.actions = append(p.actions, r.Header.Get("Actions"))
		p.mu.Unlock()
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (p *pushSink) wait(t *testing.T, contains string) {
	t.Helper()
	for range 200 {
		p.mu.Lock()
		all := strings.Join(p.bodies, "\n")
		p.mu.Unlock()
		if strings.Contains(all, contains) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	t.Fatalf("no push with %q in %q", contains, p.bodies)
}

func TestAccountNotificationsAndAgentMessages(t *testing.T) {
	sink := &pushSink{}
	push := sink.start(t)
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		switch n {
		case 1:
			if !strings.Contains(strings.Join(toolNames(body), ","), "notify_me") {
				return []string{`{"role":"assistant","content":"no notify_me tool"}`}
			}
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"notify_me","arguments":{"title":"Disk report","text":"All three disks are healthy."}}}]}`}
		case 2:
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"file_write","arguments":{"root":"share/Docs","path":"report.txt","content":"x","format":"text","reason":"Save the disk report"}}}]}`}
		default:
			return []string{`{"role":"assistant","content":"Report saved."}`}
		}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b"})

	settings := map[string]any{"language": "en", "content": "summary", "origin": "https://nas.local",
		"ntfy":   map[string]any{"enabled": true, "url": push.URL + "/alex", "token": "personal-secret"},
		"events": map[string]bool{"confirmation": true, "done": true, "blocked": true, "agent": true}}
	if res, body := ts.request("PUT", "/api/v1/account/notifications", settings); res.StatusCode != 200 || strings.Contains(string(body), "personal-secret") {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	if _, body := ts.request("GET", "/api/v1/account/notifications", nil); strings.Contains(string(body), "personal-secret") || !strings.Contains(string(body), `"ntfy":true`) {
		t.Fatalf("get: %s", body)
	}

	id := ts.newChat(ts.client)
	if res, body := ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "check my disks and save a report"}); res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	sink.wait(t, "All three disks are healthy.")
	// The task now waits for the approval of file_write; its reason is the summary.
	sink.wait(t, "Approval needed")
	sink.wait(t, "Save the disk report")
	sink.mu.Lock()
	if !strings.Contains(strings.Join(sink.click, " "), "https://nas.local/assistant?chat="+id) {
		t.Fatalf("no link to the chat: %v", sink.click)
	}
	sink.mu.Unlock()

	// The shared bell never carries the content.
	_, bell := ts.request("GET", "/api/v1/notifications", nil)
	if strings.Contains(string(bell), "disks are healthy") || strings.Contains(string(bell), "disk report") {
		t.Fatalf("content in the shared channel: %s", bell)
	}
}

func TestAccountNotificationsNeedLogin(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	res, _ := ts.do(&http.Client{}, "GET", "/api/v1/account/notifications", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without login: %d", res.StatusCode)
	}
}
