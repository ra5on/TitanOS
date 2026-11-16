package api

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/agent"
	"github.com/ra5on/TitanOS/backend/internal/storage"
)

// Review 1.02: the move left the data of "Dokumente" (.titanos/docs: the
// index and the paperwork log with its undo and the open questions) on
// the old volume; after the move the agent started with none. A copy left
// behind on the target by such an earlier move does not stop a move back.
func TestAgentMoveTakesTheDocuments(t *testing.T) {
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
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond) })
	fake := &agentOllama{}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "ollama": fake.start(t), "model": "gpt-oss:20b", "volume": "tank", "policy": agent.Policy{Preset: agent.PresetStandard}}); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	if err := ts.server.agentStore().SaveChat("alex", &agent.Chat{ID: agent.NewID(), Title: "x"}); err != nil {
		t.Fatal(err)
	}
	write := func() {
		root, err := ts.server.docsRoot("alex")
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		if err := root.WriteFile("paper.json", []byte(`{"entries":[]}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func() string {
		root, err := ts.server.docsRoot("alex")
		if err != nil {
			t.Fatal(err)
		}
		defer root.Close()
		data, _ := root.ReadFile("paper.json")
		return string(data)
	}
	move := func(to string) {
		t.Helper()
		if res, body := ts.request("POST", "/api/v1/agent/volume/move", map[string]any{"to": to}); res.StatusCode != http.StatusAccepted {
			t.Fatalf("move to %s: %d %s", to, res.StatusCode, body)
		}
		deadline := time.Now().Add(10 * time.Second)
		for ts.server.agentMoving() && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if j := ts.server.agentMoveView(); j == nil || j.State != moveDone {
			t.Fatalf("job %+v", j)
		}
	}
	// A copy an earlier move (before 1.02) left on fast.
	os.MkdirAll(filepath.Join(mnt, "fast", ".titanos", "docs", "alex"), 0o700)
	os.WriteFile(filepath.Join(mnt, "fast", ".titanos", "docs", "alex", "paper.json"), []byte(`{"old":true}`), 0o600)
	write()
	move("fast")
	if got := read(); got != `{"entries":[]}` {
		t.Fatalf("documents after the move: %q", got)
	}
	if _, err := os.Stat(filepath.Join(mnt, "tank", ".titanos", "docs")); !os.IsNotExist(err) {
		t.Fatal("documents left on the old volume")
	}
	move("tank")
	if got := read(); got != `{"entries":[]}` {
		t.Fatalf("documents after moving back: %q", got)
	}
}

// Documents left on the target by an earlier move never become the
// agent's, also when the old volume has none.
func TestAgentMoveSetsOldDocumentsAside(t *testing.T) {
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
	t.Cleanup(func() { time.Sleep(300 * time.Millisecond) })
	fake := &agentOllama{}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": true, "ollama": fake.start(t), "model": "gpt-oss:20b", "volume": "tank", "policy": agent.Policy{Preset: agent.PresetStandard}}); res.StatusCode != http.StatusOK {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	if err := ts.server.agentStore().SaveChat("alex", &agent.Chat{ID: agent.NewID(), Title: "x"}); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(mnt, "fast", ".titanos", "docs", "alex"), 0o700)
	os.WriteFile(filepath.Join(mnt, "fast", ".titanos", "docs", "alex", "paper.json"), []byte(`{"old":true}`), 0o600)
	if res, body := ts.request("POST", "/api/v1/agent/volume/move", map[string]any{"to": "fast"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("move: %d %s", res.StatusCode, body)
	}
	deadline := time.Now().Add(10 * time.Second)
	for ts.server.agentMoving() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if j := ts.server.agentMoveView(); j == nil || j.State != moveDone {
		t.Fatalf("job %+v", j)
	}
	if _, err := os.Stat(filepath.Join(mnt, "fast", ".titanos", "docs")); !os.IsNotExist(err) {
		t.Fatal("old documents still in use")
	}
	if m, _ := filepath.Glob(filepath.Join(mnt, "fast", ".titanos", "agent-old", "*-left", "docs", "alex", "paper.json")); len(m) != 1 {
		t.Fatal("old documents not kept aside")
	}
}
