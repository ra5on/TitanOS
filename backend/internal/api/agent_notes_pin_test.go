package api

import (
	"fmt"
	"net/http"
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
