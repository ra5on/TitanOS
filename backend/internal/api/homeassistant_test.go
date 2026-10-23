package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeHAServer answers the few REST calls the assistant makes.
type fakeHAServer struct {
	mu     sync.Mutex
	calls  []string
	notify []string
}

func (f *fakeHAServer) start(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ha-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		switch {
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
