package api

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// Review 1.02: pins of notes were read, changed and written without the
// store's lock; two pins at once (two devices) lost one of them.
func TestAgentNotePinsAtOnceAllStay(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	ts.withShare()
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond); ts.server.agentSyncShare() })
	const n = 16
	for i := range n {
		if res, body := ts.request("PUT", "/api/v1/agent/notes/file", map[string]any{"path": fmt.Sprintf("n%d.md", i), "content": "x"}); res.StatusCode != http.StatusOK {
			t.Fatalf("save: %d %s", res.StatusCode, body)
		}
	}
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			ts.request("PUT", "/api/v1/agent/notes/pin", map[string]any{"path": fmt.Sprintf("n%d.md", i), "pinned": true})
		})
	}
	wg.Wait()
	meta, err := ts.server.agentStore().NotesMeta("alex")
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Pinned) != n {
		t.Fatalf("%d of %d pins kept: %v", len(meta.Pinned), n, meta.Pinned)
	}
}

// Review 1.02: moving a folder of notes into another one could put its
// notes deeper than four folders; the list leaves those out, so the notes
// were gone from the page (still on the disk).
func TestAgentNoteMoveKeepsNotesInSight(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{Preset: agent.PresetStandard})
	ts.withShare()
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond); ts.server.agentSyncShare() })
	if res, body := ts.request("PUT", "/api/v1/agent/notes/file", map[string]any{"path": "a/b/c/d/x.md", "content": "x"}); res.StatusCode != http.StatusOK {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/agent/notes/folders", map[string]any{"path": "z"}); res.StatusCode != http.StatusNoContent {
		t.Fatal("folder")
	}
	for _, to := range []string{"z/a", "p/q/r/s/t"} {
		if res, _ := ts.request("POST", "/api/v1/agent/notes/move", map[string]any{"from": "a", "to": to}); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("moved to %s: %d", to, res.StatusCode)
		}
	}
	// Within the depth it moves.
	if res, body := ts.request("POST", "/api/v1/agent/notes/move", map[string]any{"from": "a/b", "to": "z/b"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("move: %d %s", res.StatusCode, body)
	}
	_, body := ts.request("GET", "/api/v1/agent/notes", nil)
	if !strings.Contains(string(body), `"z/b/c/d/x.md"`) {
		t.Fatalf("note not listed: %s", body)
	}
}
