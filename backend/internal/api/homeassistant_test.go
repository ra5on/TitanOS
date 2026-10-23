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

// change sends a state change once the watcher subscribed.
func (f *fakeHAServer) change(t *testing.T, entity, from, to string) {
	t.Helper()
	for range 300 {
		f.mu.Lock()
		conn, id := f.conn, f.trigger
		f.mu.Unlock()
		if conn != nil && id > 0 {
			b, _ := json.Marshal(map[string]any{"id": id, "type": "event", "event": map[string]any{"variables": map[string]any{"trigger": map[string]any{
				"entity_id": entity, "from_state": map[string]any{"state": from}, "to_state": map[string]any{"state": to}}}}})
			conn.Write(context.Background(), websocket.MessageText, b)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("watcher did not subscribe")
}

// fire sends a subscribed event once the watcher subscribed to its type.
func (f *fakeHAServer) fire(t *testing.T, eventType string, data any) {
	t.Helper()
	for range 300 {
		f.mu.Lock()
		conn, id := f.conn, f.eventIDs[eventType]
		f.mu.Unlock()
		if conn != nil && id > 0 {
			b, _ := json.Marshal(map[string]any{"id": id, "type": "event", "event": map[string]any{"event_type": eventType, "data": data}})
			conn.Write(context.Background(), websocket.MessageText, b)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("watcher did not subscribe to %s", eventType)
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

func TestHomeAssistantSettingsAndAgentCall(t *testing.T) {
	fakeHA := &fakeHAServer{}
	ha := fakeHA.start(t)
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		switch n {
		case 1:
			if !strings.Contains(strings.Join(toolNames(body), ","), "ha_call") {
				return []string{`{"role":"assistant","content":"no ha_call"}`}
			}
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"ha_entities","arguments":{}}}]}`}
		case 2:
			if strings.Contains(lastContent(body), "switch.pump") || !strings.Contains(lastContent(body), "light.kitchen") {
				return []string{`{"role":"assistant","content":"wrong entities"}`}
			}
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"ha_call","arguments":{"domain":"switch","service":"turn_off","entities":["switch.pump"],"reason":"r"}}}]}`}
		case 3:
			return []string{`{"role":"assistant","content":"","tool_calls":[{"function":{"name":"ha_call","arguments":{"domain":"light","service":"turn_on","entities":["light.kitchen"],"data":{"brightness_pct":30},"reason":"Licht an"}}}]}`}
		default:
			return []string{`{"role":"assistant","content":"Licht ist an."}`}
		}
	}}
	ollama := fake.start(t)
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

	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "execution": "direct", "ollama": ollama.URL, "model": "qwen3:8b", "areas": map[string]string{"home": "write"}})
	id := ts.newChat(ts.client)
	if res, body := ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "Mach das Licht in der Küche an"}); res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	chat := ts.chat(ts.client, id)
	fakeHA.mu.Lock()
	defer fakeHA.mu.Unlock()
	if len(fakeHA.calls) != 1 || !strings.HasPrefix(fakeHA.calls[0], "/api/services/light/turn_on") || !strings.Contains(fakeHA.calls[0], `"brightness_pct":30`) {
		t.Fatalf("calls %v; chat %+v", fakeHA.calls, chat.Chat.Messages)
	}
}

func TestHomeAssistantAreaOffByDefault(t *testing.T) {
	fakeHA := &fakeHAServer{}
	ha := fakeHA.start(t)
	var offered string
	fake := &fakeChatOllama{next: func(n int, body map[string]any) []string {
		offered = strings.Join(toolNames(body), ",")
		return []string{`{"role":"assistant","content":"ok"}`}
	}}
	ollama := fake.start(t)
	ts := newTestServer(t)
	ts.setup()
	ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"enabled": true, "url": ha.URL, "token": "ha-token", "domains": []string{"light"}})
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b"})
	id := ts.newChat(ts.client)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "hi"})
	if strings.Contains(offered, "ha_") {
		t.Fatalf("Home Assistant tools without switching the area on: %s", offered)
	}
	ts.request("PUT", "/api/v1/assistant", map[string]any{"enabled": true, "ollama": ollama.URL, "model": "qwen3:8b", "areas": map[string]string{"home": "read"}})
	id = ts.newChat(ts.client)
	ts.request("POST", "/api/v1/assistant/chats/"+id+"/messages", map[string]any{"text": "hi"})
	if !strings.Contains(offered, "ha_entities") || strings.Contains(offered, "ha_call") {
		t.Fatalf("read access: %s", offered)
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
		"events":        map[string]bool{"agent": true}}
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

func TestHomeAssistantStateStartsAutomation(t *testing.T) {
	fakeHA := &fakeHAServer{}
	ha := fakeHA.start(t)
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ts.home.Run(ctx)
	ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"enabled": true, "url": ha.URL, "token": "ha-token", "entities": []string{"lock.door"}})
	settings := ts.server.opts.Assistant.Settings()
	settings.Enabled = true
	settings.Areas = map[string]string{"home": "read"}
	ts.server.opts.Assistant.SetSettings(settings)
	u, _ := ts.users.Get("alex")

	// Only released entities can be watched.
	now := time.Now()
	if res, body := ts.request("POST", "/api/v1/assistant/automations", map[string]any{"name": "Pump", "prompt": "x", "enabled": true, "trigger": map[string]any{"kind": "event", "event": "homeassistant", "target": "switch.pump"}}); res.StatusCode < 400 {
		t.Fatalf("unreleased entity accepted: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", "/api/v1/assistant/automations", map[string]any{"name": "Door", "prompt": "Tell me who opened the door", "enabled": true,
		"trigger": map[string]any{"kind": "event", "event": "homeassistant", "target": "lock.door", "to": "unlocked"}})
	if res.StatusCode >= 300 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	ts.server.watchHomeAssistant(ts.users.List())
	ts.server.scanAutomations(ctx, u, now) // baseline
	fakeHA.change(t, "lock.door", "locked", "jammed")
	time.Sleep(50 * time.Millisecond)
	ts.server.scanAutomations(ctx, u, now.Add(time.Minute))
	if list, _ := ts.server.automations.List("alex"); len(list[0].Runs) != 0 {
		t.Fatalf("other state started a run: %+v", list[0])
	}
	// A short change in between two looks still counts.
	fakeHA.change(t, "lock.door", "jammed", "unlocked")
	fakeHA.change(t, "lock.door", "unlocked", "locked")
	time.Sleep(50 * time.Millisecond)
	ts.server.scanAutomations(ctx, u, now.Add(2*time.Minute))
	list, _ := ts.server.automations.List("alex")
	if len(list[0].Runs) != 1 {
		t.Fatalf("no run: %+v", list[0])
	}
	ts.server.scanAutomations(ctx, u, now.Add(4*time.Minute))
	if list, _ := ts.server.automations.List("alex"); len(list[0].Runs) != 1 {
		t.Fatal("same change started twice")
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
