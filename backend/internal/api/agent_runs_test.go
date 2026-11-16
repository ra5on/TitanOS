package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// blockingOllama is a fake Ollama for the runs of 1.01: every chat request
// answers the replies in order; one with hold streams its first piece and
// waits until release is closed.
type blockingOllama struct {
	mu      sync.Mutex
	replies []string
	// hold: the next answer that is plain text streams "Hal" and waits.
	hold    bool
	release chan struct{}
	asked   int
}

func (f *blockingOllama) start(t *testing.T) string {
	f.release = make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/version":
			w.Write([]byte(`{"version":"0.40.14"}`))
		case "/api/show":
			w.Write([]byte(`{"capabilities":["tools"]}`))
		case "/api/ps":
			w.Write([]byte(`{"models":[{"name":"m","context_length":32768,"size_vram":1}]}`))
		case "/api/chat":
			var body struct{ Messages []any }
			json.NewDecoder(r.Body).Decode(&body)
			if len(body.Messages) == 0 { // preload (PLAN 59a)
				w.Write([]byte(`{"done":true,"done_reason":"load"}`))
				return
			}
			f.mu.Lock()
			f.asked++
			reply := ""
			if len(f.replies) > 0 {
				reply, f.replies = f.replies[0], f.replies[1:]
			}
			hold := f.hold && reply == ""
			f.mu.Unlock()
			if reply != "" {
				w.Write([]byte(`{"message":` + reply + `,"done":false}` + "\n" + `{"message":{"role":"assistant","content":""},"done":true}` + "\n"))
				return
			}
			if hold {
				w.Write([]byte(`{"message":{"role":"assistant","content":"Hal"},"done":false}` + "\n"))
				w.(http.Flusher).Flush()
				select {
				case <-f.release:
				case <-r.Context().Done():
					return
				}
			}
			w.Write([]byte(`{"message":{"role":"assistant","content":"lo"},"done":false}` + "\n" + `{"message":{"role":"assistant","content":""},"done":true}` + "\n"))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// runsServer is a test server with the agent on the blocking fake; the
// waiting answers are released before the runs are waited for.
func runsServer(t *testing.T, fake *blockingOllama, extra map[string]any) *testServer {
	t.Helper()
	ts := newTestServer(t)
	ts.setup()
	dir := t.TempDir()
	ts.server.agentOpen = func() (*os.Root, error) { return os.OpenRoot(dir) }
	t.Cleanup(ts.server.agentRT.work.Wait)
	settings := map[string]any{"enabled": true, "ollama": fake.start(t), "model": "m", "policy": map[string]any{}}
	for k, v := range extra {
		settings[k] = v
	}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	ts.server.shareSyncs.Wait()
	// Registered after the wait above, so it runs first.
	t.Cleanup(func() {
		select {
		case <-fake.release:
		default:
			close(fake.release)
		}
	})
	return ts
}

func startChat(t *testing.T, ts *testServer, message string) (agent.Chat, *http.Response, []byte) {
	t.Helper()
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": message})
	var chat agent.Chat
	json.Unmarshal(body, &chat)
	return chat, res, body
}

// waitFor polls until ok or fails after a few seconds.
func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func runView(ts *testServer, user, chat string) (content string, entries int, ok bool) {
	ts.server.agentRT.mu.Lock()
	defer ts.server.agentRT.mu.Unlock()
	run := ts.server.agentRT.runs[runKey(user, chat)]
	if run == nil {
		return "", 0, false
	}
	return run.view.content.String(), len(run.view.entries), true
}

// A page that comes while a run works gets what it missed first (1.01):
// the lines the run wrote, the plan and the text that streamed in.
func TestAgentSnapshotAfterMissedEvents(t *testing.T) {
	fake := &blockingOllama{hold: true, replies: []string{toolCall("plan_update", map[string]any{"steps": []map[string]any{{"text": "Erst lesen"}}})}}
	ts := runsServer(t, fake, nil)
	chat, res, body := startChat(t, ts, "Hallo")
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	// Nobody follows yet: the step, its line and "Hal" go by unseen.
	waitFor(t, "the answer streams", func() bool {
		content, entries, _ := runView(ts, "alex", chat.ID)
		return content == "Hal" && entries >= 2
	})
	req, _ := http.NewRequest("GET", ts.url+"/api/v1/agent/chats/"+chat.ID+"/events", nil)
	stream, err := ts.client.Do(req)
	if err != nil || stream.StatusCode != http.StatusOK {
		t.Fatalf("stream: %v %v", err, stream)
	}
	defer stream.Body.Close()
	read := bufio.NewReader(stream.Body)
	block := func() (string, string) {
		var name, data string
		for {
			line, err := read.ReadString('\n')
			if err != nil {
				t.Fatalf("stream ended: %v", err)
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case line == "" && name != "":
				return name, data
			case strings.HasPrefix(line, "event: "):
				name = line[7:]
			case strings.HasPrefix(line, "data: "):
				data = line[6:]
			}
		}
	}
	name, data := block()
	if name != "snapshot" {
		t.Fatalf("first event %q: %s", name, data)
	}
	var snap struct {
		Type     string
		Entries  []agent.Entry
		Content  string
		State    string
		Plan     []agent.PlanStep
		Thinking string
	}
	if err := json.Unmarshal([]byte(data), &snap); err != nil {
		t.Fatal(err)
	}
	if snap.Type != "snapshot" || snap.Content != "Hal" || snap.State != agent.StateRunning {
		t.Fatalf("snapshot: %s", data)
	}
	var called, stepped bool
	for _, e := range snap.Entries {
		called = called || (e.Role == "assistant" && len(e.Calls) == 1)
		stepped = stepped || (e.Role == "tool" && e.Tool == "plan_update")
	}
	if !called || !stepped || len(snap.Plan) != 1 || snap.Plan[0].Text != "Erst lesen" {
		t.Fatalf("snapshot lacks the run so far: %s", data)
	}
	// The user's message was there before the run; it is not sent again.
	for _, e := range snap.Entries {
		if e.Role == "user" {
			t.Fatalf("snapshot repeats the question: %s", data)
		}
	}
	// Older pages still get their state line, then the events go on.
	if name, data := block(); name != "state" || !strings.Contains(data, `"running"`) {
		t.Fatalf("second event %q: %s", name, data)
	}
	close(fake.release)
	rest := ""
	for !strings.Contains(rest, "event: end") {
		line, err := read.ReadString('\n')
		rest += line
		if err != nil {
			break
		}
	}
	if !strings.Contains(rest, `"text":"lo"`) || !strings.Contains(rest, `"content":"Hallo"`) {
		t.Fatalf("the run did not go on after the snapshot: %s", rest)
	}
	if done := waitChat(t, ts, ts.client, chat.ID); done.Entries[len(done.Entries)-1].Content != "Hallo" {
		t.Fatalf("answer: %+v", done.Entries[len(done.Entries)-1])
	}
}

// A page too slow for the events gets a new snapshot, never a gap.
func TestAgentSlowPageGetsASnapshot(t *testing.T) {
	old := agentSubBuffer
	agentSubBuffer = 2
	t.Cleanup(func() { agentSubBuffer = old })
	ts := newTestServer(t)
	ts.setup()
	id := agent.NewID()
	run, err := ts.server.agentClaim("alex", id, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ts.server.agentRelease(run)
	sub, first, ok := ts.server.agentSubscribe(run)
	if !ok || first.State != agent.StateRunning || len(first.Entries) != 0 {
		t.Fatalf("first snapshot: %+v", first)
	}
	emit := func(e agent.Event) {
		ts.server.agentRT.mu.Lock()
		run.emitLocked(e)
		ts.server.agentRT.mu.Unlock()
	}
	emit(agent.Event{Type: "entry", Entry: &agent.Entry{ID: "e1", Role: "assistant", Content: "Erst"}})
	for _, piece := range []string{"Ein", " langer", " Satz", " ohne", " Lücke"} {
		emit(agent.Event{Type: "content", Text: piece})
	}
	select {
	case <-sub.wake:
	default:
		t.Fatal("a full channel did not ask for a snapshot")
	}
	snap, ok := ts.server.agentResync(run, sub)
	if !ok || snap.Content != "Ein langer Satz ohne Lücke" || len(snap.Entries) != 1 || snap.Entries[0].ID != "e1" {
		t.Fatalf("resync: %+v", snap)
	}
	if len(sub.ch) != 0 {
		t.Fatalf("older events stayed behind the snapshot: %d", len(sub.ch))
	}
	emit(agent.Event{Type: "content", Text: "."})
	if e := <-sub.ch; e.Text != "." {
		t.Fatalf("after the snapshot: %+v", e)
	}
	// "reset" and an answer clear the text; a tool line clears the step.
	emit(agent.Event{Type: "step", Step: &agent.Entry{ID: "s1", Role: "tool", Tool: "files_list"}})
	emit(agent.Event{Type: "reset"})
	ts.server.agentRT.mu.Lock()
	snap = run.snapshotLocked()
	ts.server.agentRT.mu.Unlock()
	if snap.Content != "" || snap.Step == nil || snap.Step.Tool != "files_list" {
		t.Fatalf("after reset: %+v", snap)
	}
	emit(agent.Event{Type: "entry", Entry: &agent.Entry{ID: "s1", Role: "tool", Tool: "files_list"}})
	ts.server.agentRT.mu.Lock()
	snap = run.snapshotLocked()
	ts.server.agentRT.mu.Unlock()
	if snap.Step != nil || len(snap.Entries) != 2 {
		t.Fatalf("after the step's line: %+v", snap)
	}
}

// The limit of runs at once (owner 16 Nov 2026): 1 from the factory, 2 and
// more as set; a refused message changes nothing; tasks of the NAS do not
// count and still start.
func TestAgentRunLimit(t *testing.T) {
	fake := &blockingOllama{hold: true}
	ts := runsServer(t, fake, nil)
	a, res, body := startChat(t, ts, "Erster")
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("first: %d %s", res.StatusCode, body)
	}
	_, res, body = startChat(t, ts, "Zweiter")
	if res.StatusCode != http.StatusConflict || !strings.Contains(string(body), `"agent_limit"`) {
		t.Fatalf("second chat at limit 1: %d %s", res.StatusCode, body)
	}
	// No chat was made for the refused message.
	_, body = ts.request("GET", "/api/v1/agent/chats", nil)
	if strings.Contains(string(body), "Zweiter") {
		t.Fatalf("refused chat was saved: %s", body)
	}
	// An existing chat keeps its entries when its message is refused.
	res, body = ts.request("POST", "/api/v1/agent/chats", map[string]any{"draft": true})
	var idle agent.Chat
	json.Unmarshal(body, &idle)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("draft: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+idle.ID+"/messages", map[string]any{"message": "Noch was"}); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "agent_limit") {
		t.Fatalf("message at limit: %d %s", res.StatusCode, body)
	}
	if kept, _ := ts.server.agentStore().Chat("alex", idle.ID); len(kept.Entries) != 0 || ts.server.agentRunning("alex", idle.ID) {
		t.Fatalf("refused message changed the chat: %+v", kept.Entries)
	}
	// The running chat itself still has one run only.
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+a.ID+"/messages", map[string]any{"message": "Und?"}); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "agent_busy") {
		t.Fatalf("second run of one chat: %d %s", res.StatusCode, body)
	}
	// A planned task (or a look into a notification) starts all the same.
	alex := mustUser(t, ts, "alex")
	now := time.Now()
	task := &agent.Chat{ID: agent.NewID(), Title: "Aufgabe", Created: now, Updated: now, Origin: "schedule", State: agent.StateIdle,
		Entries: []agent.Entry{{ID: agent.NewID(), At: now, Role: "user", Content: "Prüfe"}}}
	if err := ts.server.agentStore().SaveChat("alex", task); err != nil {
		t.Fatal(err)
	}
	if err := ts.server.agentStart(alex, task, func(rn *agent.Runner, ctx context.Context) error { return rn.Run(ctx, task) }); err != nil {
		t.Fatalf("planned task at the limit: %v", err)
	}
	// The status names the limit; the list of runs has both.
	if _, body := ts.request("GET", "/api/v1/agent/status", nil); !strings.Contains(string(body), `"maxRuns":1`) {
		t.Fatalf("status: %s", body)
	}
	if _, body := ts.request("GET", "/api/v1/agent/runs", nil); !strings.Contains(string(body), a.ID) || !strings.Contains(string(body), task.ID) || !strings.Contains(string(body), `"max":1`) {
		t.Fatalf("runs: %s", body)
	}
	close(fake.release)
	waitChat(t, ts, ts.client, a.ID)
	waitChat(t, ts, ts.client, task.ID)
	// Once the first is done, the next one may run.
	if _, res, body := startChat(t, ts, "Dritter"); res.StatusCode != http.StatusCreated {
		t.Fatalf("after the first: %d %s", res.StatusCode, body)
	}
}

// With a limit of 2 or more, as many chats run side by side; one more is
// refused. 5 is no valid setting.
func TestAgentRunsSideBySide(t *testing.T) {
	for _, limit := range []int{2, 4} {
		t.Run(strings.Repeat("n", limit), func(t *testing.T) {
			fake := &blockingOllama{hold: true}
			ts := runsServer(t, fake, map[string]any{"maxRuns": limit})
			ids := []string{}
			for n := range limit {
				chat, res, body := startChat(t, ts, "Chat "+string(rune('A'+n)))
				if res.StatusCode != http.StatusCreated {
					t.Fatalf("chat %d of %d: %d %s", n+1, limit, res.StatusCode, body)
				}
				ids = append(ids, chat.ID)
			}
			// All of them stream at the same moment.
			waitFor(t, "every chat streams", func() bool {
				for _, id := range ids {
					if content, _, _ := runView(ts, "alex", id); content != "Hal" {
						return false
					}
				}
				return true
			})
			if _, res, body := startChat(t, ts, "Einer zu viel"); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "agent_limit") {
				t.Fatalf("over the limit of %d: %d %s", limit, res.StatusCode, body)
			}
			// The limit is kept in the settings.
			_, body := ts.request("GET", "/api/v1/agent/settings", nil)
			var settings map[string]any
			json.Unmarshal(body, &settings)
			if settings["maxRuns"] != float64(limit) {
				t.Fatalf("saved limit: %v", settings["maxRuns"])
			}
			close(fake.release)
			for _, id := range ids {
				if chat := waitChat(t, ts, ts.client, id); chat.Entries[len(chat.Entries)-1].Content != "Hallo" {
					t.Fatalf("answer of %s: %+v", id, chat.Entries[len(chat.Entries)-1])
				}
			}
		})
	}
	fake := &blockingOllama{}
	ts := runsServer(t, fake, nil)
	_, body := ts.request("GET", "/api/v1/agent/settings", nil)
	var settings map[string]any
	json.Unmarshal(body, &settings)
	if _, ok := settings["maxRuns"]; ok {
		t.Fatalf("the factory setting is written: %s", body)
	}
	for _, bad := range []int{-1, 5} {
		settings["maxRuns"] = bad
		if res, _ := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusBadRequest {
			t.Errorf("maxRuns %d accepted: %d", bad, res.StatusCode)
		}
	}
	if (agentSettings{}).maxRuns() != 1 || (agentSettings{MaxRuns: 3}).maxRuns() != 3 {
		t.Fatal("maxRuns")
	}
}

// Two chats wait for approval at once: the answer in one leaves the step of
// the other as it is.
func TestAgentApprovalStaysInItsChat(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	dir := ts.withShare()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("alt a"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("alt b"), 0o644)
	start := func(file string) agent.Chat {
		fake.mu.Lock()
		fake.replies = append(fake.replies, toolCall("file_write", map[string]any{"root": "share/Docs", "path": file, "content": "neu", "replace": true}))
		fake.mu.Unlock()
		res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Ersetze " + file})
		if res.StatusCode != http.StatusCreated {
			t.Fatalf("start: %d %s", res.StatusCode, body)
		}
		var chat agent.Chat
		json.Unmarshal(body, &chat)
		chat = waitChat(t, ts, ts.client, chat.ID)
		if chat.State != agent.StateWaiting || chat.Pending == nil {
			t.Fatalf("%s does not wait: %s", file, chat.State)
		}
		return chat
	}
	a, b := start("a.md"), start("b.md")
	// The step of B cannot be answered in A.
	if res, _ := ts.request("POST", "/api/v1/agent/chats/"+a.ID+"/decide", map[string]any{"id": b.Pending.ID, "approve": true}); res.StatusCode != http.StatusConflict {
		t.Fatalf("B's step through A: %d", res.StatusCode)
	}
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+a.ID+"/decide", map[string]any{"id": a.Pending.ID, "approve": true}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("approve A: %d %s", res.StatusCode, body)
	}
	if done := waitChat(t, ts, ts.client, a.ID); done.State != agent.StateIdle {
		t.Fatalf("A after approval: %s", done.State)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "a.md")); string(data) != "neu" {
		t.Fatalf("A not written: %q", data)
	}
	still, _ := ts.server.agentStore().Chat("alex", b.ID)
	if still.State != agent.StateWaiting || still.Pending == nil || still.Pending.ID != b.Pending.ID {
		t.Fatalf("B changed with A: %s %+v", still.State, still.Pending)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "b.md")); string(data) != "alt b" {
		t.Fatalf("B written with A: %q", data)
	}
	if res, _ := ts.request("POST", "/api/v1/agent/chats/"+b.ID+"/decide", map[string]any{"id": b.Pending.ID, "approve": false}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("decline B: %d", res.StatusCode)
	}
	declined := waitChat(t, ts, ts.client, b.ID)
	found := false
	for _, e := range declined.Entries {
		found = found || e.Note == "declined"
	}
	if !found {
		t.Fatalf("B lacks its no: %+v", declined.Entries)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "b.md")); string(data) != "alt b" {
		t.Fatalf("B written after its no: %q", data)
	}
}
