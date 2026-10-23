package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestShareViewPreferencesAreDurableAndAccountScoped(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	value := map[string]any{"volume": "Data", "protocol": "smb", "sort": "name", "compact": true}
	if res, body := ts.request("PUT", "/api/v1/preferences/shares", value); res.StatusCode != http.StatusNoContent {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	stored, err := newPreferences(ts.store).get("alex", "shares")
	if err != nil || !strings.Contains(string(stored), `"Data"`) {
		t.Fatalf("reload: %s %v", stored, err)
	}
	if _, err := ts.users.Create("other", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	other := ts.newClient()
	ts.do(other, "POST", "/api/v1/login", credentials{Name: "other", Password: testPassword})
	_, body := ts.do(other, "GET", "/api/v1/preferences", nil)
	var result map[string]json.RawMessage
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatal(err)
	}
	if string(result["shares"]) != "null" {
		t.Fatalf("another account received shares: %s", body)
	}
	if res, _ := ts.do(ts.newClient(), "PUT", "/api/v1/preferences/shares", value); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous save: %d", res.StatusCode)
	}
}
