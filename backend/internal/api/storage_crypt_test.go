package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// Passwords of encrypted volumes come only from a browser session: the
// assistant (and Home Assistant, which uses the same in-process path) can
// neither reach the routes nor find them in its catalog.
func TestVolumePasswordRoutesNeedABrowserSession(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	// A browser session reaches the storage manager (no such volume here).
	res, body := ts.request("POST", "/api/v1/storage/volumes/nope/unlock", map[string]string{"password": "correct horse battery"})
	if res.StatusCode != http.StatusNotFound || errorCode(t, body) != "volume_not_found" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	user, _ := ts.users.Get("alex")
	for _, route := range []string{"/api/v1/storage/volumes/nope/unlock", "/api/v1/storage/volumes/nope/passwords", "/api/v1/storage/volumes/nope/lock"} {
		code, answer := ts.server.internalAs(context.Background(), user, "POST", route, map[string]string{"password": "correct horse battery", "action": "add"})
		if code != http.StatusForbidden || !strings.Contains(answer, "web_session_required") {
			t.Fatalf("%s in-process: %d %s", route, code, answer)
		}
	}
	code, answer := ts.server.internalAs(context.Background(), user, "GET", "/api/v1/storage/volumes/nope/luks-header", nil)
	if code != http.StatusForbidden {
		t.Fatalf("header backup in-process: %d %s", code, answer)
	}
	// Creating an encrypted volume or adding a disk with a password needs
	// the browser as well.
	code, answer = ts.server.internalAs(context.Background(), user, "POST", "/api/v1/storage/volumes", map[string]any{"name": "safe", "encrypt": true, "password": "correct horse battery"})
	if code != http.StatusForbidden {
		t.Fatalf("encrypted create in-process: %d %s", code, answer)
	}
	for _, route := range apiRouteList() {
		for _, secret := range []string{"/unlock", "/lock", "/passwords", "/luks-header"} {
			if strings.Contains(route, "/storage/") && strings.Contains(route, secret) {
				t.Fatalf("assistant catalog offers %s", route)
			}
		}
	}
}
