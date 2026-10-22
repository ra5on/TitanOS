package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/assistant"
)

func TestAgentDirectAndDryRun(t *testing.T) {
	for _, dry := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "dry-run"}[dry], func(t *testing.T) {
			fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
				if n == 1 {
					return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"file_write","arguments":{"root":"share/Docs","path":"agent.txt","content":"written by server","format":"text","reason":"requested"}}}]}`}
				}
				return []string{`{"role":"assistant","content":"finished"}`}
			}}
			ollama := fake.start(t)
			ts := newTestServer(t)
			ts.setup()
			dir := ts.withShare()
			ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "execution": "direct", "ollama": ollama.URL, "model": "qwen3:8b"})
			id := ts.newChat(ts.client)
			res, body := ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "write the file", "dryRun": dry})
			if res.StatusCode != 200 {
				t.Fatalf("%d %s", res.StatusCode, body)
			}
			data, err := os.ReadFile(filepath.Join(dir, "agent.txt"))
			if dry {
				if !os.IsNotExist(err) {
					t.Fatalf("dry run changed file: %s %v", data, err)
				}
			} else if err != nil || string(data) != "written by server" {
				t.Fatalf("not executed: %s %v; %s", data, err, body)
			}
			_, diagnostic := ts.request("GET", "/api/v1/assistant/chats/"+id+"/diagnostics", nil)
			if !json.Valid(diagnostic) || strings.Contains(string(diagnostic), "written by server") || strings.Contains(string(diagnostic), "agent.txt") {
				t.Fatalf("diagnostic leaked content: %s", diagnostic)
			}
		})
	}
}

func TestAgentBrowserDisconnectAndPause(t *testing.T) {
	entered := make(chan struct{})
	proceed := make(chan struct{})
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		if n == 1 {
			close(entered)
			<-proceed
		}
		return []string{`{"role":"assistant","content":"completed in background"}`}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b"})
	id := ts.newChat(ts.client)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", ts.url+"/api/v1/assistant/chats/"+id+"/messages", strings.NewReader(`{"text":"continue after closing browser"}`))
	req.Header.Set("Content-Type", "application/json")
	done := make(chan struct{})
	go func() {
		defer close(done)
		res, _ := ts.client.Do(req)
		if res != nil {
			res.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("model not started")
	}
	cancel()
	close(proceed)
	<-done
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, e := ts.server.chats.Get("alex", id)
		if e == nil && c.Run != nil && c.Run.State == "done" {
			if c.Messages[len(c.Messages)-1].Content != "completed in background" {
				t.Fatalf("bad answer: %+v", c)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("disconnect stopped the job")
}

func TestAgentUnknownOutcomeAndReceiptRecovery(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		return []string{`{"role":"assistant","content":"inspected"}`}
	}}
	ollama := fake.start(t)
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b"})
	id := ts.newChat(ts.client)
	c, _ := ts.server.chats.Get("alex", id)
	c.Messages = []assistant.Entry{{Role: "user", Content: "do work"}, {Role: "tool", Tool: "file_write", Action: &assistant.Action{ID: "1234567890abcdef", Tool: "file_write", State: assistant.ActionRunning}}}
	if err := ts.server.chats.Save("alex", c); err != nil {
		t.Fatal(err)
	}
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/control", map[string]string{"action": "resume"})
	c, _ = ts.server.chats.Get("alex", id)
	if c.Run.State != "blocked" || len(fake.seen) != 0 {
		t.Fatalf("unknown mutation retried: %+v", c)
	}
	base, ok := ts.server.chatsDir()
	if !ok {
		t.Fatal("no persistent storage")
	}
	dir := filepath.Join(base, "alex", "receipts", id)
	os.MkdirAll(dir, 0700)
	receipt, _ := json.Marshal(map[string]string{"ID": "1234567890abcdef", "User": "alex", "Chat": id, "Tool": "file_write", "State": "done", "Result": "durably recorded success"})
	os.WriteFile(filepath.Join(dir, "1234567890abcdef.json"), receipt, 0600)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/control", map[string]string{"action": "resume"})
	c, _ = ts.server.chats.Get("alex", id)
	if c.Run.State != "done" || c.Messages[1].Action.State != "done" {
		t.Fatalf("receipt not recovered: %+v", c)
	}
}

func TestAgentPhotoSearchAndRootRecovery(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	os.WriteFile(filepath.Join(dir, "holiday.HEIC"), []byte("photo"), 0600)
	os.WriteFile(filepath.Join(dir, "note.txt"), []byte("text"), 0600)
	u, _ := ts.users.Get("alex")
	search := ts.server.assistantFilesSearch(u)
	result, err := search(context.Background(), json.RawMessage(`{"root":"share/Docs","extension":"images"}`))
	b, _ := json.Marshal(result)
	if err != nil || !strings.Contains(string(b), "holiday.HEIC") || strings.Contains(string(b), "note.txt") {
		t.Fatalf("photo search: %s %v", b, err)
	}
	_, err = search(context.Background(), json.RawMessage(`{"root":"volume"}`))
	if err == nil || !strings.Contains(err.Error(), "share/Docs") {
		t.Fatalf("no correction for bad root: %v", err)
	}
}

func TestAgentGlobalPauseAndPermissions(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		if n == 1 {
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"file_write","arguments":{"root":"share/Docs","path":"forbidden.txt","content":"no","reason":"requested"}}}]}`}
		}
		return []string{`{"role":"assistant","content":"cannot write"}`}
	}}
	ollama := fake.start(t)
	settings := map[string]any{"enabled": true, "execution": "direct", "ollama": ollama.URL, "model": "qwen3:8b", "areas": map[string]string{"files": "read"}}
	ts.request("PUT", "/api/v1/assistant", settings)
	id := ts.newChat(ts.client)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]string{"text": "write"})
	if _, err := os.Stat(filepath.Join(dir, "forbidden.txt")); !os.IsNotExist(err) {
		t.Fatal("direct bypassed read-only area")
	}
	settings["paused"] = true
	ts.request("PUT", "/api/v1/assistant", settings)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]string{"text": "wait"})
	c, _ := ts.server.chats.Get("alex", id)
	if c.Run.State != "paused" {
		t.Fatalf("pause ignored: %+v", c.Run)
	}
}

func TestAgentModeChangeRevokesAutomaticPendingAction(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		return []string{`{"role":"assistant","content":"finished"}`}
	}}
	ollama := fake.start(t)
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "execution": "confirm", "ollama": ollama.URL, "model": "qwen3:8b"})
	id := ts.newChat(ts.client)
	c, _ := ts.server.chats.Get("alex", id)
	c.Messages = []assistant.Entry{{Role: "user", Content: "write"}, {Role: "tool", Action: &assistant.Action{ID: "1234567890abcdef", Tool: "file_write", Key: "file.write", State: assistant.ActionPending, Auto: true, Level: assistant.LevelNormal}}}
	ts.server.chats.Save("alex", c)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/control", map[string]string{"action": "resume"})
	c, _ = ts.server.chats.Get("alex", id)
	if c.Run.State != "confirmation" || c.Messages[1].Action.Auto || len(fake.seen) != 0 {
		t.Fatalf("stale auto permission: %+v", c)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.txt")); !os.IsNotExist(err) {
		t.Fatal("executed without approval")
	}
}

func TestAgentSchedulerResumesSavedJob(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		return []string{`{"role":"assistant","content":"resumed checkpoint"}`}
	}}
	ollama := fake.start(t)
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b"})
	id := ts.newChat(ts.client)
	c, _ := ts.server.chats.Get("alex", id)
	c.Model = "qwen3:8b"
	c.Messages = []assistant.Entry{{Role: "user", Content: "finish after restart"}}
	c.Run = &assistant.Run{State: "waiting", Wake: time.Now().Add(-time.Second)}
	ts.server.chats.Save("alex", c)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); ts.server.RunAssistant(ctx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, _ = ts.server.chats.Get("alex", id)
		if c.Run.State == "done" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("checkpoint not resumed: %+v", c.Run)
}

func TestAgentSteeringExpiresUnexecutedProposal(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		return []string{`{"role":"assistant","content":"Changed instructions received"}`}
	}}
	ollama := fake.start(t)
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "execution": "direct", "ollama": ollama.URL, "model": "qwen3:8b"})
	id := ts.newChat(ts.client)
	c, _ := ts.server.chats.Get("alex", id)
	c.Messages = []assistant.Entry{{Role: "user", Content: "do work"}, {Role: "tool", Action: &assistant.Action{ID: "1234567890abcdef", Tool: "file_write", Key: "file.write", State: assistant.ActionPending, Auto: true}}}
	ts.server.chats.Save("alex", c)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/control", map[string]string{"action": "steer", "text": "Do not write anything; explain instead."})
	c, _ = ts.server.chats.Get("alex", id)
	if c.Run.State != "done" || c.Messages[1].Action.State != assistant.ActionExpired {
		t.Fatalf("stale proposal remained executable: %+v", c)
	}
}
