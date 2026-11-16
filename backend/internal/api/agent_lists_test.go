package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/agent"
)

// Projects, skills and planned tasks added from several pages at the same
// time all arrive (1.01: each request read the list, added and wrote it
// back, and the last one won).
func TestAgentListsFromManyPagesAtOnce(t *testing.T) {
	ts := agentServer(t, &agentOllama{}, agent.Policy{})
	send := func(method, path string, body any) error {
		data, _ := json.Marshal(body)
		req, err := http.NewRequest(method, ts.url+path, bytes.NewReader(data))
		if err != nil {
			return err
		}
		res, err := ts.client.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		if res.StatusCode >= 300 {
			text, _ := io.ReadAll(res.Body)
			return fmt.Errorf("%s %s: %d %s", method, path, res.StatusCode, text)
		}
		return nil
	}
	const n = 12
	var wg sync.WaitGroup
	errs := make(chan error, 3*n)
	for i := 0; i < n; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			errs <- send("POST", "/api/v1/agent/projects", map[string]any{"name": fmt.Sprintf("Projekt %d", i)})
		}()
		go func() {
			defer wg.Done()
			errs <- send("PUT", "/api/v1/agent/skills/new", map[string]any{"name": fmt.Sprintf("Skill %d", i), "steps": "Schritt"})
		}()
		go func() {
			defer wg.Done()
			errs <- send("PUT", "/api/v1/agent/schedules/new", map[string]any{"name": fmt.Sprintf("Auftrag %d", i), "task": "Sieh nach", "every": "daily", "at": "08:00", "enabled": true})
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	store := ts.server.agentStore()
	projects, _ := store.Projects("alex")
	skills, _ := store.Skills("alex")
	schedules, _ := store.Schedules("alex")
	if len(projects) != n || len(skills) != n || len(schedules) != n {
		t.Fatalf("projects %d skills %d schedules %d, want %d each", len(projects), len(skills), len(schedules), n)
	}
	// A task saved from the page keeps a run the NAS stamped meanwhile.
	id := schedules[0].ID
	if err := store.UpdateSchedule("alex", id, func(sc *agent.Schedule) { sc.Chat = "stamped" }); err != nil {
		t.Fatal(err)
	}
	if err := send("PUT", "/api/v1/agent/schedules/"+id, map[string]any{"name": "Neu", "task": "Sieh nach", "every": "daily", "at": "08:00", "enabled": true}); err != nil {
		t.Fatal(err)
	}
	after, _ := store.Schedules("alex")
	for _, sc := range after {
		if sc.ID == id && (sc.Chat != "stamped" || sc.Name != "Neu") {
			t.Fatalf("saved task %+v", sc)
		}
	}
}
