package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// PLAN 58: the volume is chosen when the agent is switched on, changes
// only by moving, and a move copies, checks, switches and cleans up.
func TestAgentVolumeChooseAndMove(t *testing.T) {
	fake := &agentOllama{}
	ts := newTestServer(t)
	ts.setup()
	mnt := t.TempDir()
	if err := ts.store.WriteJSON("storage/volumes.json", []storage.VolumeConfig{
		{Name: "tank", Level: "raid1", Filesystem: "ext4", ArrayUUID: "a", FilesystemUUID: "b"},
		{Name: "fast", Level: "raid1", Filesystem: "btrfs", ArrayUUID: "c", FilesystemUUID: "d"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := ts.disks.Load(); err != nil {
		t.Fatal(err)
	}
	for _, v := range []string{"tank", "fast"} {
		os.MkdirAll(filepath.Join(mnt, v), 0o755)
	}
	ts.disks.UseMounts(mnt, func() map[string]bool { return map[string]bool{mnt + "/tank": true, mnt + "/fast": true} })
	ts.shares.UseMountRoot(mnt)
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond) })

	settings := map[string]any{"enabled": true, "ollama": fake.start(t), "model": "gpt-oss:20b", "policy": agent.Policy{Preset: agent.PresetStandard}}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("switched on without a volume: %d %s", res.StatusCode, body)
	}
	settings["volume"] = "nirgends"
	if res, _ := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusBadRequest {
		t.Fatal("unknown volume accepted")
	}
	settings["volume"] = "tank"
	if res, body := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	settings["volume"] = "fast"
	if res, _ := ts.request("PUT", "/api/v1/agent/settings", settings); res.StatusCode != http.StatusConflict {
		t.Fatal("volume changed without moving")
	}
	_, body := ts.request("GET", "/api/v1/agent/volumes", nil)
	var vols struct {
		Volumes []agentVolumeView
		Current string
	}
	json.Unmarshal(body, &vols)
	if vols.Current != "tank" || len(vols.Volumes) != 2 || !vols.Volumes[1].Current || vols.Volumes[0].FreeBytes == 0 {
		t.Fatalf("volumes: %s", body)
	}

	// Data in both places: a chat (private) and a note (share).
	chat := &agent.Chat{ID: agent.NewID(), Title: "Umzug"}
	chat.Entries = append(chat.Entries, agent.Entry{Role: "user", Content: "Hallo"})
	if err := ts.server.agentStore().SaveChat("alex", chat); err != nil {
		t.Fatal(err)
	}
	if res, body := ts.request("PUT", "/api/v1/agent/notes/file", map[string]any{"path": "Einkauf.md", "content": "- [ ] Milch"}); res.StatusCode != http.StatusOK {
		t.Fatalf("note: %d %s", res.StatusCode, body)
	}
	os.Symlink("/etc", filepath.Join(mnt, "tank", agentShareName, "alex", "raus"))

	if res, _ := ts.request("POST", "/api/v1/agent/volume/move", map[string]any{"to": "tank"}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("moved onto itself")
	}
	if res, body := ts.request("POST", "/api/v1/agent/volume/move", map[string]any{"to": "fast"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("move: %d %s", res.StatusCode, body)
	}
	var job agentMoveJob
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, body := ts.request("GET", "/api/v1/agent/volume/move", nil)
		var out struct{ Move agentMoveJob }
		json.Unmarshal(body, &out)
		job = out.Move
		if job.State == moveDone || job.State == moveFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("move hangs: %+v", job)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if job.State != moveDone || job.Files < 2 || job.Skipped != 1 || job.Cleanup != "" {
		t.Fatalf("job %+v", job)
	}
	if ts.server.agentSettings().Volume != "fast" {
		t.Fatal("settings not switched")
	}
	for _, sh := range ts.shares.List() {
		if sh.Name == agentShareName && sh.Volume != "fast" {
			t.Fatalf("share on %s", sh.Volume)
		}
	}
	for _, gone := range []string{".titanos/agent", agentShareName} {
		if _, err := os.Stat(filepath.Join(mnt, "tank", gone)); !os.IsNotExist(err) {
			t.Fatalf("%s left on the old volume", gone)
		}
	}
	// The old data is kept aside until removed ("Alte Kopie löschen").
	old, _ := filepath.Glob(filepath.Join(mnt, "tank", ".titanos", "agent-old", "*", agentShareName, "alex", "Notizen", "Einkauf.md"))
	if len(old) != 1 {
		t.Fatal("old copy not kept")
	}
	_, body = ts.request("GET", "/api/v1/agent/volumes", nil)
	vols.Volumes = nil
	json.Unmarshal(body, &vols)
	if vols.Current != "fast" || !vols.Volumes[1].OldCopy || vols.Volumes[0].OldCopy {
		t.Fatalf("after the move: %s", body)
	}
	if res, _ := ts.request("POST", "/api/v1/agent/volume/cleanup", map[string]any{"volume": "tank"}); res.StatusCode != http.StatusNoContent {
		t.Fatal("cleanup")
	}
	if _, err := os.Stat(filepath.Join(mnt, "tank", ".titanos", "agent-old")); !os.IsNotExist(err) {
		t.Fatal("old copy still there")
	}
	if _, err := os.Lstat(filepath.Join(mnt, "fast", agentShareName, "alex", "raus")); !os.IsNotExist(err) {
		t.Fatal("link copied")
	}
	got, err := ts.server.agentStore().Chat("alex", chat.ID)
	if err != nil || got.Title != "Umzug" {
		t.Fatalf("chat after the move: %v %+v", err, got)
	}
	_, body = ts.request("GET", "/api/v1/agent/notes/file?path=Einkauf.md", nil)
	var note struct{ Content string }
	json.Unmarshal(body, &note)
	if note.Content != "- [ ] Milch" {
		t.Fatalf("note after the move: %s", body)
	}

	// A running task blocks a move; while moving, nothing starts.
	ts.server.agentRT.mu.Lock()
	if ts.server.agentRT.runs == nil {
		ts.server.agentRT.runs = map[string]*agentRun{}
	}
	ts.server.agentRT.runs["alex/x"] = &agentRun{}
	ts.server.agentRT.mu.Unlock()
	if res, _ := ts.request("POST", "/api/v1/agent/volume/move", map[string]any{"to": "tank"}); res.StatusCode != http.StatusConflict {
		t.Fatal("moved while a task runs")
	}
	ts.server.agentRT.mu.Lock()
	delete(ts.server.agentRT.runs, "alex/x")
	ts.server.agentRT.mu.Unlock()
	ts.server.agentMove.active.Store(true)
	if _, err := ts.server.agentStore().Chats("alex"); err == nil {
		t.Fatal("store open while moving")
	}
	ts.server.agentMove.active.Store(false)
}

// A move a restart broke off leaves a marked copy on the target, which the
// next move clears; a share KI-Agent on another volume stops the move.
func TestAgentMoveAfterBrokenMove(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	mnt := t.TempDir()
	ts.store.WriteJSON("storage/volumes.json", []storage.VolumeConfig{
		{Name: "tank", Level: "raid1", Filesystem: "ext4", ArrayUUID: "a", FilesystemUUID: "b"},
		{Name: "fast", Level: "raid1", Filesystem: "btrfs", ArrayUUID: "c", FilesystemUUID: "d"},
	})
	ts.disks.Load()
	for _, v := range []string{"tank", "fast"} {
		os.MkdirAll(filepath.Join(mnt, v), 0o755)
	}
	ts.disks.UseMounts(mnt, func() map[string]bool { return map[string]bool{mnt + "/tank": true, mnt + "/fast": true} })
	ts.shares.UseMountRoot(mnt)
	fake := &agentOllama{}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "ollama": fake.start(t), "model": "gpt-oss:20b", "volume": "tank", "policy": agent.Policy{Preset: agent.PresetStandard}}); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	if err := ts.server.agentStore().SaveChat("alex", &agent.Chat{ID: agent.NewID(), Title: "x"}); err != nil {
		t.Fatal(err)
	}
	// Half a copy from before a restart.
	os.MkdirAll(filepath.Join(mnt, "fast", ".titanos", "agent", "alex"), 0o700)
	os.WriteFile(filepath.Join(mnt, "fast", ".titanos", "agent", "alex", "halb.json"), []byte("{}"), 0o600)
	if res, _ := ts.request("POST", "/api/v1/agent/volume/move", map[string]any{"to": "fast"}); res.StatusCode != http.StatusConflict {
		t.Fatal("moved onto foreign data")
	}
	os.WriteFile(filepath.Join(mnt, "fast", moveMarker), []byte("tank\n"), 0o600)
	if res, body := ts.request("POST", "/api/v1/agent/volume/move", map[string]any{"to": "fast"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("move after a broken one: %d %s", res.StatusCode, body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for ts.server.agentMoving() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if j := ts.server.agentMoveView(); j == nil || j.State != moveDone {
		t.Fatalf("job %+v", j)
	}
	if _, err := os.Stat(filepath.Join(mnt, "fast", ".titanos", "agent", "alex", "halb.json")); !os.IsNotExist(err) {
		t.Fatal("half copy kept")
	}
	if _, err := os.Stat(filepath.Join(mnt, "fast", moveMarker)); !os.IsNotExist(err) {
		t.Fatal("marker kept")
	}
}
