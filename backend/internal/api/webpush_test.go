package api

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestPushDevices(t *testing.T) {
	ts := newTestServer(t)
	routes := []struct{ method, path string }{{"GET", "/notifications/push"}, {"POST", "/notifications/push"}, {"DELETE", "/notifications/push/x"}, {"POST", "/notifications/push/x/test"}}
	for _, route := range routes {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	ts.setup()
	ts.request("POST", "/api/v1/users", map[string]any{"name": "reader", "password": testPassword, "role": "user"})
	user := ts.newClient()
	ts.do(user, "POST", "/api/v1/login", credentials{Name: "reader", Password: testPassword})
	// PLAN 58: an account with the role "Benutzer" subscribes its own
	// devices (for its agent's messages) and sees only those.
	if res, _ := ts.do(user, "GET", "/api/v1/notifications/push", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("user list: %d", res.StatusCode)
	}
	if res, _ := ts.do(user, "DELETE", "/api/v1/notifications/push/x", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("user remove: %d", res.StatusCode)
	}

	res, body := ts.request("GET", "/api/v1/notifications/push", nil)
	var list struct {
		PublicKey string            `json:"publicKey"`
		Devices   []json.RawMessage `json:"devices"`
	}
	if json.Unmarshal(body, &list); res.StatusCode != 200 || len(list.PublicKey) < 80 || list.Devices == nil {
		t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	key, _ := ecdh.P256().GenerateKey(rand.Reader)
	auth := make([]byte, 16)
	rand.Read(auth)
	b64 := base64.RawURLEncoding
	sub := func(endpoint string) map[string]any {
		return map[string]any{"name": "iPhone", "subscription": map[string]any{"endpoint": endpoint, "keys": map[string]string{"p256dh": b64.EncodeToString(key.PublicKey().Bytes()), "auth": b64.EncodeToString(auth)}}}
	}
	if res, body := ts.request("POST", "/api/v1/notifications/push", sub("https://intranet.example.org/push")); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "invalid_push_subscription") {
		t.Fatalf("foreign endpoint: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/notifications/push", sub("https://web.push.apple.com/QGabc"))
	var device struct{ ID, Name, Service string }
	if json.Unmarshal(body, &device); res.StatusCode != 200 || device.Service != "web.push.apple.com" || strings.Contains(string(body), "QGabc") {
		t.Fatalf("subscribe: %d %s", res.StatusCode, body)
	}
	if _, body := ts.request("GET", "/api/v1/notifications/push", nil); !strings.Contains(string(body), device.ID) || strings.Contains(string(body), "p256dh") {
		t.Fatalf("list after subscribe: %s", body)
	}
	if res, _ := ts.request("DELETE", "/api/v1/notifications/push/"+device.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("remove: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/notifications/push/"+device.ID+"/test", map[string]any{}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("test of a removed device: %d", res.StatusCode)
	}
}
