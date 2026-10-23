package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestHAUndoRejectsInterveningChangesAndReplay(t *testing.T) {
	var mu sync.Mutex
	state := "off"
	changed := time.Now().UTC()
	calls := 0
	ha := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case r.URL.Path == "/api/states/switch.test":
			json.NewEncoder(w).Encode(map[string]any{"entity_id": "switch.test", "state": state, "last_changed": changed})
		case strings.HasPrefix(r.URL.Path, "/api/services/switch/"):
			calls++
			if strings.HasSuffix(r.URL.Path, "turn_on") {
				state = "on"
			} else {
				state = "off"
			}
			changed = changed.Add(time.Second)
			w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer ha.Close()
	ts := newTestServer(t)
	ts.setup()
	ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"enabled": true, "url": ha.URL, "token": "test", "entities": []string{"switch.test"}})
	call := map[string]any{"domain": "switch", "service": "turn_on", "entities": []string{"switch.test"}}
	res, body := ts.request("POST", "/api/v1/homeassistant/call", call)
	if res.StatusCode != 200 {
		t.Fatal(string(body))
	}
	var receipt struct{ UndoReceipt string }
	json.Unmarshal(body, &receipt)
	if receipt.UndoReceipt == "" {
		t.Fatal("missing undo", string(body))
	}
	mu.Lock()
	changed = changed.Add(time.Second)
	mu.Unlock()
	if res, _ = ts.request("POST", "/api/v1/homeassistant/undo", map[string]string{"receipt": receipt.UndoReceipt}); res.StatusCode != 409 {
		t.Fatal("accepted changed switch", res.StatusCode)
	}
	mu.Lock()
	state = "off"
	mu.Unlock()
	_, body = ts.request("POST", "/api/v1/homeassistant/call", call)
	json.Unmarshal(body, &receipt)
	if res, body = ts.request("POST", "/api/v1/homeassistant/undo", map[string]string{"receipt": receipt.UndoReceipt}); res.StatusCode != 200 {
		t.Fatal(string(body))
	}
	if res, _ = ts.request("POST", "/api/v1/homeassistant/undo", map[string]string{"receipt": receipt.UndoReceipt}); res.StatusCode < 400 {
		t.Fatal("replayed undo")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 3 || state != "off" {
		t.Fatal(calls, state)
	}
}
