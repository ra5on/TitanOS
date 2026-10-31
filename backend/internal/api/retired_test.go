package api

import (
	"os"
	"path/filepath"
	"testing"
)

// The leftovers of the removed assistant go: settings on the stick and the
// folder on each volume; held personal notifications move first.
func TestRetiredAssistantDataRemoved(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	dir := ts.withShare()
	if err := ts.store.WriteJSON(retiredAssistantSettings, map[string]any{"enabled": true, "anthropicKey": "sk-old"}); err != nil {
		t.Fatal(err)
	}
	volume := filepath.Dir(dir)
	old := filepath.Join(volume, ".titanos", "assistant")
	for _, f := range []string{"alex/chats/c1.json", "alex/knowledge/k.json", "alex/notifications/pending.json"} {
		p := filepath.Join(old, f)
		os.MkdirAll(filepath.Dir(p), 0o700)
		os.WriteFile(p, []byte(`[]`), 0o600)
	}
	ts.server.removeRetiredAssistant()
	if _, err := ts.store.Read(retiredAssistantSettings); err == nil {
		t.Fatal("settings still on the stick")
	}
	if _, err := os.Lstat(old); err == nil {
		t.Fatal("old folder still there")
	}
	if _, err := os.Stat(filepath.Join(volume, ".titanos", "notify", "alex", "notifications", "pending.json")); err != nil {
		t.Fatalf("held notifications not moved: %v", err)
	}
	// Nothing left: a second run changes nothing.
	ts.server.removeRetiredAssistant()
}
