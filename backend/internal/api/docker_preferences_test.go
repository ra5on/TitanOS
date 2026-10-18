package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestDockerPreferencesPersistAndRemainAccountScoped(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	layout := map[string]any{"version": 1, "view": "minimal", "order": []string{"app:photos"}, "widths": map[string]int{"app:photos": 3}}
	if res, body := ts.request("PUT", "/api/v1/preferences/docker", layout); res.StatusCode != http.StatusNoContent {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	// A fresh preference cache models reading the stored layout after a restart.
	value, err := newPreferences(ts.store).get("alex", "docker")
	if err != nil || !strings.Contains(string(value), `"minimal"`) {
		t.Fatalf("persistence: %s %v", value, err)
	}
	if _, err := ts.users.Create("other", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	other := ts.newClient()
	ts.do(other, "POST", "/api/v1/login", credentials{Name: "other", Password: testPassword})
	_, body := ts.do(other, "GET", "/api/v1/preferences", nil)
	var prefs map[string]json.RawMessage
	if err := json.Unmarshal(body, &prefs); err != nil {
		t.Fatal(err)
	}
	if string(prefs["docker"]) != "null" {
		t.Fatalf("another account received layout: %s", body)
	}
	if res, _ := ts.do(ts.newClient(), "PUT", "/api/v1/preferences/docker", layout); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous save: %d", res.StatusCode)
	}
	if res, _ := ts.request("PUT", "/api/v1/preferences/docker", []byte(`[]`)); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("array save: %d", res.StatusCode)
	}
}
