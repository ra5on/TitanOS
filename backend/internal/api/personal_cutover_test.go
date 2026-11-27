package api

import (
	"bytes"
	"net/http"
	"testing"
)

func TestPersonalCutoverLeavesLegacyDormant(t *testing.T) {
	fixture := []byte(`{"enabled":true,"name":"legacy-fixture","schedules":[{"id":"old-only"}]}`)
	ts := testServerRoutes(t, func(o *Options) {
		if err := o.Store.Write("agent.json", fixture); err != nil {
			t.Fatal(err)
		}
		o.PersonalDataDir = t.TempDir()
	}, false)
	ts.setup()
	if ts.server.opts.Notify.Observer != nil || (ts.server.opts.Cluster != nil && ts.server.opts.Cluster.Agent != nil) {
		t.Fatal("legacy handler remains registered")
	}
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/agent/settings"}, {"POST", "/api/v1/agent/schedules/old-only/run"}, {"GET", "/api/v1/agent/chats"}, {"GET", assistPrefix + "/api/tags"}} {
		res, _ := ts.request(route.method, route.path, nil)
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d", route.path, res.StatusCode)
		}
	}
	if res, _ := ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"account": "alex"}); res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	if err := ts.server.HACommand(t.Context(), "agent", "old-only", "run"); err == nil || err.Error() != "unknown command" {
		t.Fatalf("unexpected HA agent dispatch: %v", err)
	}
	if tasks := ts.server.HASnapshot(t.Context()).AgentTasks; len(tasks) != 0 {
		t.Fatal("old tasks advertised to MQTT")
	}
	if res, _ := ts.request("GET", "/api/v1/personal-agent/activity", nil); res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	got, err := ts.store.Read("agent.json")
	if err != nil || !bytes.Equal(got, fixture) {
		t.Fatal("upgrade touched legacy data")
	}
}
