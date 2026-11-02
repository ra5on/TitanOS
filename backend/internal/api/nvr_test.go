package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

func newNVRTestServer(t *testing.T) *testServer {
	return newTestServerWith(t, func(o *Options) {
		o.NVR = nvr.NewManager(o.Store, func(string) (string, bool) { return "", false })
	})
}

func TestNVRServiceSwitchAndCameras(t *testing.T) {
	ts := newNVRTestServer(t)
	ts.setup()
	_, body := ts.request("GET", "/api/v1/services/state", nil)
	var state map[string]bool
	json.Unmarshal(body, &state)
	if state["nvr"] {
		t.Fatalf("NVR on by default: %s", body)
	}
	if res, b := ts.request("PUT", "/api/v1/services/nvr", map[string]any{"enabled": true}); res.StatusCode != 200 {
		t.Fatalf("switch on: %d %s", res.StatusCode, b)
	}
	if !ts.server.servicesOn()["nvr"] {
		t.Fatal("NVR still off")
	}

	cam := map[string]any{"name": "Hof", "host": "192.168.1.5", "user": "view", "password": "geheim-123", "mainUrl": "rtsp://192.168.1.5:554/main", "record": true}
	res, b := ts.request("POST", "/api/v1/nvr/cameras", cam)
	if res.StatusCode != 201 {
		t.Fatalf("add: %d %s", res.StatusCode, b)
	}
	if strings.Contains(string(b), "geheim-123") {
		t.Fatalf("password in the answer: %s", b)
	}
	_, b = ts.request("GET", "/api/v1/nvr", nil)
	if strings.Contains(string(b), "geheim-123") || !strings.Contains(string(b), `"Hof"`) {
		t.Fatalf("state: %s", b)
	}

	// Addresses outside the home network are refused.
	for _, bad := range []string{"rtsp://8.8.8.8/x", "rtsp://127.0.0.1/x", "rtsp://169.254.169.254/x"} {
		cam["mainUrl"] = bad
		if res, b := ts.request("POST", "/api/v1/nvr/cameras", cam); res.StatusCode != 400 {
			t.Fatalf("%s accepted: %d %s", bad, res.StatusCode, b)
		}
	}
	if res, _ := ts.request("POST", "/api/v1/nvr/discover", map[string]any{"host": "8.8.8.8", "user": "u", "password": "p"}); res.StatusCode != 400 {
		t.Fatalf("search outside the home network: %d", res.StatusCode)
	}
	if res, _ := ts.request("GET", "/api/v1/nvr/cameras/nope/snapshot", nil); res.StatusCode != 404 {
		t.Fatalf("unknown camera: %d", res.StatusCode)
	}
}

func TestNVRIsForAdministratorsOnly(t *testing.T) {
	ts := newNVRTestServer(t)
	ts.setup()
	res, _ := ts.do(ts.newClient(), "GET", "/api/v1/nvr", nil)
	if res.StatusCode != 401 {
		t.Fatalf("without login: %d", res.StatusCode)
	}
}

func TestNVRViewsAreSavedAndChecked(t *testing.T) {
	ts := newNVRTestServer(t)
	ts.setup()
	res, b := ts.request("POST", "/api/v1/nvr/cameras", map[string]any{"name": "Hof", "host": "192.168.1.5", "user": "v", "mainUrl": "rtsp://192.168.1.5/main"})
	if res.StatusCode != 201 {
		t.Fatalf("add: %d %s", res.StatusCode, b)
	}
	var state struct {
		Cameras []struct{ ID string } `json:"cameras"`
	}
	json.Unmarshal(b, &state)
	id := state.Cameras[0].ID
	ok := map[string]any{"views": []map[string]any{{"name": "Haus", "tiles": []map[string]any{{"camera": id, "x": 0, "y": 0, "w": 3, "h": 2}}}}}
	if res, b := ts.request("PUT", "/api/v1/nvr/views", ok); res.StatusCode != 200 || !strings.Contains(string(b), `"Haus"`) {
		t.Fatalf("save: %d %s", res.StatusCode, b)
	}
	bad := map[string]any{"views": []map[string]any{{"name": "x", "tiles": []map[string]any{{"camera": id, "x": 5, "y": 0, "w": 3, "h": 1}}}}}
	if res, _ := ts.request("PUT", "/api/v1/nvr/views", bad); res.StatusCode != 400 {
		t.Fatalf("tile outside the grid: %d", res.StatusCode)
	}
}
