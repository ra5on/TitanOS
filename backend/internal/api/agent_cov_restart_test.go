package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// After a restart of titand: chats that were running are marked
// interrupted (never claimed to go on), a step that waited for the person
// keeps waiting and can still be approved.
func TestCovAgentInterruptedOnStart(t *testing.T) {
	fake := &agentOllama{}
	ts := agentServer(t, fake, agent.Policy{Preset: agent.PresetStandard})
	dir := ts.withShare()
	os.WriteFile(filepath.Join(dir, "liste.md"), []byte("alt"), 0o644)

	// A chat that really waits for approval: replacing a file asks.
	fake.replies = []string{
		toolCall("file_write", map[string]any{"root": "share/Docs", "path": "liste.md", "content": "neu", "replace": true}),
		`{"role":"assistant","content":"Ersetzt."}`,
	}
	res, body := ts.request("POST", "/api/v1/agent/chats", map[string]any{"message": "Ersetze die Liste"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	var started agent.Chat
	json.Unmarshal(body, &started)
	waiting := waitChat(t, ts, ts.client, started.ID)
	if waiting.State != agent.StateWaiting || waiting.Pending == nil {
		t.Fatalf("not waiting: %s %+v", waiting.State, waiting.Pending)
	}

	// Chats left "running" on the volume by a titand that stopped.
	store := ts.server.agentStore()
	now := time.Now()
	running := &agent.Chat{ID: agent.NewID(), Title: "lief", Created: now, Updated: now, State: agent.StateRunning, Origin: "web",
		Entries: []agent.Entry{{ID: agent.NewID(), At: now, Role: "user", Content: "Mach was"}}}
	pendingID := agent.NewID()
	runningPending := &agent.Chat{ID: agent.NewID(), Title: "lief mit Schritt", Created: now, Updated: now, State: agent.StateRunning, Origin: "schedule",
		Pending: &agent.Pending{ID: pendingID, Tool: "file_write", Args: json.RawMessage(`{}`), Summary: "x", At: now},
		Entries: []agent.Entry{{ID: agent.NewID(), At: now, Role: "user", Content: "Mach was anderes"}}}
	idle := &agent.Chat{ID: agent.NewID(), Title: "fertig", Created: now, Updated: now, State: agent.StateIdle,
		Entries: []agent.Entry{{ID: agent.NewID(), At: now, Role: "user", Content: "x"}}}
	for _, c := range []*agent.Chat{running, runningPending, idle} {
		if err := store.SaveChat("alex", c); err != nil {
			t.Fatal(err)
		}
	}

	ts.server.AgentInterrupted(context.Background())
	// Twice, as after two restarts in a row: no second note.
	ts.server.AgentInterrupted(context.Background())

	// Read back from the volume with a fresh store, as the next titand does.
	fresh := &agent.Store{Open: store.Open}
	notes := func(c *agent.Chat) int {
		n := 0
		for _, e := range c.Entries {
			if e.Role == "note" && e.Note == "interrupted" {
				n++
			}
		}
		return n
	}
	for _, id := range []string{running.ID, runningPending.ID} {
		c, err := fresh.Chat("alex", id)
		if err != nil {
			t.Fatal(err)
		}
		if c.State != agent.StateInterrupted || notes(c) != 1 {
			t.Fatalf("%s: state %s, %d notes", c.Title, c.State, notes(c))
		}
	}
	c, _ := fresh.Chat("alex", runningPending.ID)
	if c.Pending == nil || c.Pending.ID != pendingID {
		t.Fatalf("pending step of an interrupted chat lost: %+v", c.Pending)
	}
	list, _ := fresh.Chats("alex")
	for _, info := range list {
		if info.ID == running.ID && info.State != agent.StateInterrupted {
			t.Fatalf("list still shows %s", info.State)
		}
	}
	c, _ = fresh.Chat("alex", idle.ID)
	if c.State != agent.StateIdle || notes(c) != 0 {
		t.Fatalf("idle chat touched: %s", c.State)
	}
	c, _ = fresh.Chat("alex", waiting.ID)
	if c.State != agent.StateWaiting || c.Pending == nil || c.Pending.ID != waiting.Pending.ID || notes(c) != 0 {
		t.Fatalf("waiting chat changed: %s %+v", c.State, c.Pending)
	}

	// The approval still works after the restart and runs exactly that step.
	if res, body := ts.request("POST", "/api/v1/agent/chats/"+waiting.ID+"/decide", map[string]any{"id": waiting.Pending.ID, "approve": true}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("approve: %d %s", res.StatusCode, body)
	}
	done := waitChat(t, ts, ts.client, waiting.ID)
	if done.State != agent.StateIdle || done.Pending != nil {
		t.Fatalf("after approval: %s %+v", done.State, done.Pending)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "liste.md")); string(data) != "neu" {
		t.Fatalf("not replaced: %q", data)
	}
}
