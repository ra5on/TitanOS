package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// fakeHAServer answers the few REST calls the assistant makes.
type fakeHAServer struct {
	mu     sync.Mutex
	calls  []string
	notify []string
	events []string
	// The WebSocket of the watcher and its subscriptions.
	conn     *websocket.Conn
	trigger  int
	eventIDs map[string]int
}

// serveWS speaks enough of the WebSocket API for the watcher.
func (f *fakeHAServer) serveWS(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ctx := r.Context()
	send := func(v any) { b, _ := json.Marshal(v); conn.Write(ctx, websocket.MessageText, b) }
	send(map[string]string{"type": "auth_required"})
	if _, _, err := conn.Read(ctx); err != nil {
		return
	}
	send(map[string]string{"type": "auth_ok"})
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg struct {
			ID        int    `json:"id"`
			Type      string `json:"type"`
			EventType string `json:"event_type"`
		}
		json.Unmarshal(data, &msg)
		f.mu.Lock()
		f.conn = conn
		if msg.Type == "subscribe_trigger" {
			f.trigger = msg.ID
		}
		if msg.Type == "subscribe_events" {
			if f.eventIDs == nil {
				f.eventIDs = map[string]int{}
			}
			f.eventIDs[msg.EventType] = msg.ID
		}
		f.mu.Unlock()
		send(map[string]any{"id": msg.ID, "type": "result", "success": true})
	}
}

func (f *fakeHAServer) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/websocket" && r.Header.Get("Authorization") != "Bearer ha-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/websocket" {
			f.serveWS(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/events/"):
			f.mu.Lock()
			f.events = append(f.events, r.URL.Path+" "+string(body))
			f.mu.Unlock()
			w.Write([]byte(`{"message":"Event fired."}`))
		case r.URL.Path == "/api/config":
			w.Write([]byte(`{"version":"2026.10.1","location_name":"Home"}`))
		case r.URL.Path == "/api/states":
			w.Write([]byte(`[{"entity_id":"light.kitchen","state":"off","attributes":{"friendly_name":"Kitchen"}},{"entity_id":"lock.door","state":"locked","attributes":{}},{"entity_id":"switch.pump","state":"on","attributes":{}}]`))
		case r.URL.Path == "/api/template":
			w.Write([]byte("light.kitchen\tKitchen\n"))
		case r.URL.Path == "/api/services" && r.Method == http.MethodGet:
			w.Write([]byte(`[{"domain":"notify","services":{"mobile_app_phone":{}}}]`))
		case strings.HasPrefix(r.URL.Path, "/api/services/notify/"):
			f.mu.Lock()
			f.notify = append(f.notify, string(body))
			f.mu.Unlock()
			w.Write([]byte(`[]`))
		case strings.HasPrefix(r.URL.Path, "/api/services/"):
			f.mu.Lock()
			f.calls = append(f.calls, r.URL.Path+" "+string(body))
			f.mu.Unlock()
			w.Write([]byte(`[{"entity_id":"light.kitchen","state":"on","attributes":{}}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Home Assistant over MCP: only released entities, dangerous domains only
// when the tool is set to "all" and with confirm=true.
func TestHomeAssistantSettingsAndMCPCall(t *testing.T) {
	fakeHA := &fakeHAServer{}
	ha := fakeHA.start(t)
	ts := newTestServer(t)
	ts.setup()

	// The connection: the token stays on the NAS.
	res, body := ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"enabled": true, "url": ha.URL, "token": "ha-token", "domains": []string{"light"}, "entities": []string{"lock.door"}})
	if res.StatusCode != 200 || strings.Contains(string(body), "ha-token") || !strings.Contains(string(body), `"tokenSet":true`) {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/homeassistant/check", nil); res.StatusCode != 200 || !strings.Contains(string(body), "2026.10.1") {
		t.Fatalf("check: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/homeassistant/notify-services", nil); res.StatusCode != 200 || !strings.Contains(string(body), "notify.mobile_app_phone") {
		t.Fatalf("services: %d %s", res.StatusCode, body)
	}
	// Not released: refused by the route itself.
	if res, _ := ts.request("POST", "/api/v1/homeassistant/call", map[string]any{"domain": "switch", "service": "turn_off", "entities": []string{"switch.pump"}}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("unreleased call: %d", res.StatusCode)
	}

	ts.mcpOn()
	token := ts.mcpToken("alex", "mcp", "system:read", "system:write")
	if text, isErr, _ := ts.mcpCall(token, "ha_entities", map[string]any{}); isErr || strings.Contains(text, "switch.pump") || !strings.Contains(text, "light.kitchen") {
		t.Fatalf("entities: %v %s", isErr, text)
	}
	if text, isErr, _ := ts.mcpCall(token, "ha_call", map[string]any{"domain": "switch", "service": "turn_off", "entities": []string{"switch.pump"}}); !isErr {
		t.Fatalf("unreleased entity switched: %s", text)
	}
	if text, isErr, _ := ts.mcpCall(token, "ha_call", map[string]any{"domain": "light", "service": "turn_on", "entities": []string{"light.kitchen"}, "data": map[string]any{"brightness_pct": 30}}); isErr {
		t.Fatalf("light: %s", text)
	}
	fakeHA.mu.Lock()
	defer fakeHA.mu.Unlock()
	if len(fakeHA.calls) != 1 || !strings.HasPrefix(fakeHA.calls[0], "/api/services/light/turn_on") || !strings.Contains(fakeHA.calls[0], `"brightness_pct":30`) {
		t.Fatalf("calls %v", fakeHA.calls)
	}
}

func TestHomeAssistantNotifyChannel(t *testing.T) {
	fakeHA := &fakeHAServer{}
	ha := fakeHA.start(t)
	ts := newTestServer(t)
	ts.setup()
	ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"enabled": true, "url": ha.URL, "token": "ha-token"})
	settings := map[string]any{"language": "en", "content": "summary", "origin": "https://nas.local",
		"homeAssistant": map[string]any{"enabled": true, "service": "notify.mobile_app_phone"},
		"events":        map[string]bool{"storage": true}}
	if res, body := ts.request("PUT", "/api/v1/account/notifications", settings); res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/notifications/test", map[string]string{"channel": "homeassistant"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("test: %d %s", res.StatusCode, body)
	}
	fakeHA.mu.Lock()
	defer fakeHA.mu.Unlock()
	if len(fakeHA.notify) != 1 || !strings.Contains(fakeHA.notify[0], `"title"`) {
		t.Fatalf("notify %v", fakeHA.notify)
	}
}

func TestHomeAssistantCommandsActAsTheChosenAccount(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ctx := context.Background()
	// Without a chosen account nothing happens.
	if err := ts.server.HACommand(ctx, "system", "power", "reboot"); err == nil {
		t.Fatal("command without account")
	}
	if res, _ := ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"account": "nobody"}); res.StatusCode != 400 {
		t.Fatalf("unknown account accepted: %d", res.StatusCode)
	}
	if res, body := ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"account": "alex"}); res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// Busy: refused and reported.
	ts.update.Working = func() bool { return true }
	if err := ts.server.HACommand(ctx, "system", "power", "poweroff"); err == nil {
		t.Fatal("power off while busy")
	}
	if !strings.Contains(fmt.Sprint(ts.server.opts.Notify.Messages()), "ha_power_refused") {
		t.Fatal("refusal not reported")
	}
	ts.update.Working = func() bool { return false }
	if err := ts.server.HACommand(ctx, "system", "power", "reboot"); err != nil {
		t.Fatal(err)
	}
	select {
	case action := <-ts.power.actions:
		if action != "reboot" {
			t.Fatal(action)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no reboot")
	}
	if err := ts.server.HACommand(ctx, "system", "shell", "rm"); err == nil {
		t.Fatal("unknown command")
	}
	// The snapshot works without any hardware.
	if snap := ts.server.HASnapshot(ctx); snap.System == nil || snap.Version != "1.2.3" {
		t.Fatalf("%+v", snap)
	}
}
