package api

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"testing"
)

// An agent.json that cannot be read is never written over with what is
// left in RAM (empty settings): policy, features and shared nodes would be
// lost, and a stricter preset would fall back to the standard one.
func TestAgentSettingsUnreadableAreNotOverwritten(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	broken := []byte(`{"enabled":true,"policy":{"preset":"read"`)
	if err := ts.store.Write(agentSettingsFile, broken); err != nil {
		t.Fatal(err)
	}
	if res, body := ts.request("PUT", "/api/v1/agent/features", map[string]any{}); res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("features: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/agent/settings", map[string]any{"enabled": false}); res.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	if err := ts.server.agentSetVolume("tank"); err == nil {
		t.Fatal("volume written over an unreadable agent.json")
	}
	data, err := ts.store.Read(agentSettingsFile)
	if err != nil || string(data) != string(broken) {
		t.Fatalf("agent.json changed: %q %v", data, err)
	}
}

// Keeping the volume chosen at first use is one stick write. agentVolume
// runs from minute ticks: when that write fails, it is not tried again on
// every tick (no periodic stick writes).
func TestAgentVolumeWriteNotRetriedEveryTick(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.server.agentSettings() // loaded: no agent.json yet
	dir := ts.store.Dir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if v, ok := ts.server.agentVolume([]string{"tank"}); !ok || v != "tank" {
		t.Fatalf("volume %q %v", v, ok)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// The next tick: the stick would take the write now, but it is not
	// tried again before the next start.
	if v, ok := ts.server.agentVolume([]string{"tank"}); !ok || v != "tank" {
		t.Fatalf("volume %q %v", v, ok)
	}
	if _, err := ts.store.Read(agentSettingsFile); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("agent.json written on a later tick: %v", err)
	}
}
