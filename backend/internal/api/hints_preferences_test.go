package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Dismissed one-time hints (plan 7a) are kept per account like the other preferences.
func TestHintPreferencesAreStoredPerAccount(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	value := map[string]any{"checksDismissed": []string{"Data"}}
	if res, body := ts.request("PUT", "/api/v1/preferences/hints", value); res.StatusCode != http.StatusNoContent {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	stored, err := newPreferences(ts.store).get("alex", "hints")
	if err != nil {
		t.Fatal(err)
	}
	var hints struct {
		ChecksDismissed []string `json:"checksDismissed"`
	}
	if err := json.Unmarshal(stored, &hints); err != nil || len(hints.ChecksDismissed) != 1 || hints.ChecksDismissed[0] != "Data" {
		t.Fatalf("reload: %s %v", stored, err)
	}
	if res, _ := ts.request("PUT", "/api/v1/preferences/hints", []string{"Data"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a list instead of an object: %d", res.StatusCode)
	}
}
