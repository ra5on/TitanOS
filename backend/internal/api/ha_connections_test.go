package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
)

// Two Home Assistant servers (PLAN §45): every route and tool works on the
// connection it names, the first one stays where older versions read it,
// and buttons act for the account of the connection that pressed them.
func TestHomeAssistantSeveralConnections(t *testing.T) {
	first, second := &fakeHAServer{}, &fakeHAServer{}
	ha1, ha2 := first.start(t), second.start(t)
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("PUT", "/api/v1/homeassistant/settings", map[string]any{"enabled": true, "url": ha1.URL, "token": "ha-token", "domains": []string{"light"}, "account": "alex"}); res.StatusCode != 200 {
		t.Fatalf("first: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("POST", "/api/v1/homeassistant/connections", map[string]string{"name": "Ferienwohnung"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add: %d %s", res.StatusCode, body)
	}
	var added struct {
		ID          string             `json:"id"`
		Connections []haConnectionInfo `json:"connections"`
	}
	json.Unmarshal(body, &added)
	id := added.ID
	if len(id) != 8 || len(added.Connections) != 2 || added.Connections[1].Name != "Ferienwohnung" || added.Connections[0].ID != homeassistant.PrimaryID {
		t.Fatalf("added %s", body)
	}
	q := "?connection=" + id
	if res, body := ts.request("PUT", "/api/v1/homeassistant/settings"+q, map[string]any{"name": "Ferienwohnung", "enabled": true, "url": ha2.URL, "token": "ha-token", "domains": []string{"light"}}); res.StatusCode != 200 || !strings.Contains(string(body), ha2.URL) {
		t.Fatalf("second: %d %s", res.StatusCode, body)
	}
	if _, body := ts.request("GET", "/api/v1/homeassistant/settings", nil); !strings.Contains(string(body), ha1.URL) || strings.Contains(string(body), ha2.URL) {
		t.Fatalf("first changed: %s", body)
	}
	if res, _ := ts.request("GET", "/api/v1/homeassistant/settings?connection=nothere", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown connection: %d", res.StatusCode)
	}
	// The MQTT identities differ, so both can use one broker.
	m1, m2 := ts.server.haConnection(""), ts.server.haConnection(id)
	s1, _ := m1.Settings()
	s2, _ := m2.Settings()
	if s1.MQTT.ID == "" || s1.MQTT.ID == s2.MQTT.ID {
		t.Fatalf("MQTT IDs %q %q", s1.MQTT.ID, s2.MQTT.ID)
	}

	// A call goes only to the server of its connection.
	if res, body := ts.request("POST", "/api/v1/homeassistant/call"+q, map[string]any{"domain": "light", "service": "turn_on", "entities": []string{"light.kitchen"}}); res.StatusCode != 200 {
		t.Fatalf("call: %d %s", res.StatusCode, body)
	}
	ts.mcpOn()
	token := ts.mcpToken("alex", "mcp", "system:read", "system:write")
	if text, isErr, _ := ts.mcpCall(token, "ha_call", map[string]any{"connection": id, "domain": "light", "service": "turn_off", "entities": []string{"light.kitchen"}}); isErr {
		t.Fatalf("tool on the second: %s", text)
	}
	if text, isErr, _ := ts.mcpCall(token, "ha_entities", map[string]any{"connection": "nothere"}); !isErr {
		t.Fatalf("unknown connection in a tool: %s", text)
	}
	if text, isErr, _ := ts.mcpCall(token, "ha_entities", map[string]any{}); isErr || !strings.Contains(text, "light.kitchen") {
		t.Fatalf("tool without connection: %v %s", isErr, text)
	}
	first.mu.Lock()
	second.mu.Lock()
	if len(first.calls) != 0 || len(second.calls) != 2 {
		t.Fatalf("calls first %v second %v", first.calls, second.calls)
	}
	first.mu.Unlock()
	second.mu.Unlock()

	// Buttons of the second act for its account: it has none.
	if err := ts.server.HACommand(homeassistant.WithConnection(context.Background(), m2), "system", "updates", "check"); err == nil {
		t.Fatal("a button of the second used the account of the first")
	}

	// Messages to a phone through the second connection.
	settings := map[string]any{"language": "en", "content": "summary", "origin": "https://nas.local",
		"homeAssistant": map[string]any{"enabled": true, "service": "notify.mobile_app_phone", "connection": id},
		"events":        map[string]bool{"storage": true}}
	if res, body := ts.request("PUT", "/api/v1/account/notifications", settings); res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/notifications/test", map[string]string{"channel": "homeassistant"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("test: %d %s", res.StatusCode, body)
	}
	first.mu.Lock()
	second.mu.Lock()
	if len(first.notify) != 0 || len(second.notify) != 1 {
		t.Fatalf("notify first %v second %v", first.notify, second.notify)
	}
	first.mu.Unlock()
	second.mu.Unlock()

	// Storage is kept apart: the first in storage.json as before.
	if err := ts.server.saveHAStorage(id, &haStorage{Account: "hafewo"}); err != nil {
		t.Fatal(err)
	}
	if setup, _ := ts.server.haStorage(""); setup != nil {
		t.Fatalf("storage of the second seen as the first's: %+v", setup)
	}
	if _, err := ts.store.Read(haStorageFileOf(id)); err != nil {
		t.Fatal(err)
	}

	// The first cannot be removed; the second goes with its files.
	if res, _ := ts.request("DELETE", "/api/v1/homeassistant/connections/main", nil); res.StatusCode != http.StatusConflict {
		t.Fatalf("first removed: %d", res.StatusCode)
	}
	if res, body := ts.request("DELETE", "/api/v1/homeassistant/connections/"+id, nil); res.StatusCode != 200 || strings.Contains(string(body), id) {
		t.Fatalf("remove: %d %s", res.StatusCode, body)
	}
	if _, err := ts.store.Read(haStorageFileOf(id)); err == nil {
		t.Fatal("storage of the removed connection kept")
	}
	if _, err := ts.store.Read("homeassistant/connections/" + id + ".json"); err == nil {
		t.Fatal("settings of the removed connection kept")
	}
	var old homeassistant.Settings
	if err := ts.store.ReadJSON("homeassistant/settings.json", &old); err != nil || old.URL != ha1.URL {
		t.Fatalf("settings.json: %+v %v", old, err)
	}
}

// With the first connection empty, the only one in use is the default and
// the others can still be chosen for messages.
func TestHomeAssistantFirstEmpty(t *testing.T) {
	second := &fakeHAServer{}
	ha2 := second.start(t)
	ts := newTestServer(t)
	ts.setup()
	res, body := ts.request("POST", "/api/v1/homeassistant/connections", map[string]string{"name": "Haus"})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("add: %d %s", res.StatusCode, body)
	}
	var added struct {
		ID string `json:"id"`
	}
	json.Unmarshal(body, &added)
	if res, body := ts.request("PUT", "/api/v1/homeassistant/settings?connection="+added.ID, map[string]any{"name": "Haus", "enabled": true, "url": ha2.URL, "token": "ha-token", "domains": []string{"light"}}); res.StatusCode != 200 {
		t.Fatalf("second: %d %s", res.StatusCode, body)
	}
	ts.mcpOn()
	token := ts.mcpToken("alex", "mcp", "system:read", "system:write")
	if text, isErr, _ := ts.mcpCall(token, "ha_entities", map[string]any{}); isErr || !strings.Contains(text, "light.kitchen") {
		t.Fatalf("default with the first empty: %v %s", isErr, text)
	}
	res, body = ts.request("GET", "/api/v1/homeassistant/notify-services", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), added.ID) {
		t.Fatalf("choices with the first empty: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/homeassistant/connection-names", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"name":"Haus"`) || strings.Contains(string(body), ha2.URL) {
		t.Fatalf("names: %d %s", res.StatusCode, body)
	}
}
