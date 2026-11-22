package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

// 1.19: the Android app keeps its own desktop apart from the web's
// ("eigene Einrichtung für Android"); writing one leaves the other alone.
func TestAndroidDesktopIsStoredApartFromTheWebDesktop(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	web := map[string]any{"version": 1, "sort": "name", "dock": map[string]any{"position": "left"}}
	if res, body := ts.request("PUT", "/api/v1/preferences/desktop", web); res.StatusCode != http.StatusNoContent {
		t.Fatalf("web desktop: %d %s", res.StatusCode, body)
	}
	android := map[string]any{"version": 1, "sort": "free", "iconSize": "l"}
	if res, body := ts.request("PUT", "/api/v1/preferences/androiddesktop", android); res.StatusCode != http.StatusNoContent {
		t.Fatalf("android desktop: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("GET", "/api/v1/preferences", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("read: %d %s", res.StatusCode, body)
	}
	var all map[string]map[string]any
	if err := json.Unmarshal(body, &all); err != nil {
		t.Fatal(err)
	}
	if all["desktop"]["sort"] != "name" || all["androiddesktop"]["sort"] != "free" || all["androiddesktop"]["iconSize"] != "l" {
		t.Fatalf("mixed up: desktop %v android %v", all["desktop"], all["androiddesktop"])
	}
	if res, _ := ts.request("PUT", "/api/v1/preferences/androiddesktop", []string{"free"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("a list instead of an object: %d", res.StatusCode)
	}
}
