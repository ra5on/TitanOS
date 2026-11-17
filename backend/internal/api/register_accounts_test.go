package api

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// Z-11 (1.03): tokens, passwords, 2FA, passkeys, SSH keys and the MCP
// settings are for a person at a web session. A request titand makes in
// the name of an account (MCP, the agent, Home Assistant) is refused before
// the handler, whatever it would check itself.
func TestAccountSecretsNeedTheWebSession(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	owner, _ := ts.users.Get("alex")
	routes := []struct{ method, path string }{
		{"GET", "/api/v1/account/tokens"},
		{"POST", "/api/v1/account/tokens"},
		{"DELETE", "/api/v1/account/tokens/x"},
		{"POST", "/api/v1/users/alex/password"},
		{"POST", "/api/v1/account/2fa/begin"},
		{"POST", "/api/v1/account/2fa/enable"},
		{"POST", "/api/v1/account/2fa/disable"},
		{"POST", "/api/v1/account/2fa/recovery"},
		{"GET", "/api/v1/account/passkeys"},
		{"POST", "/api/v1/account/passkeys/begin"},
		{"POST", "/api/v1/account/passkeys"},
		{"DELETE", "/api/v1/account/passkeys/x"},
		{"POST", "/api/v1/account/passkeys/confirm"},
		{"GET", "/api/v1/account/ssh-keys"},
		{"POST", "/api/v1/account/ssh-keys"},
		{"DELETE", "/api/v1/account/ssh-keys/x"},
		{"PUT", "/api/v1/system/mcp"},
	}
	for _, rt := range routes {
		code, body := ts.server.internalAs(context.Background(), owner, rt.method, rt.path, map[string]any{})
		if code != http.StatusForbidden || !strings.Contains(body, "browser_session_required") {
			t.Errorf("%s %s in-process: %d %s", rt.method, rt.path, code, body)
		}
		// The web UI still reaches the handler and its own checks.
		if res, body := ts.request(rt.method, rt.path, map[string]any{}); strings.Contains(string(body), "browser_session_required") {
			t.Errorf("%s %s from the web UI: %d %s", rt.method, rt.path, res.StatusCode, body)
		}
	}
	// Resetting a second factor over PUT /users/{name} is refused too; a
	// change of the role (the catalog's account tool) still goes through.
	if _, body := ts.request("POST", "/api/v1/users", map[string]any{"name": "bob", "password": "secret-password-1", "role": "user"}); !strings.Contains(string(body), "bob") {
		t.Fatalf("create bob: %s", body)
	}
	code, body := ts.server.internalAs(context.Background(), owner, "PUT", "/api/v1/users/bob", map[string]any{"role": "user", "resetTwoFactor": true})
	if code != http.StatusForbidden || !strings.Contains(body, "browser_session_required") {
		t.Errorf("2FA reset in-process: %d %s", code, body)
	}
	code, body = ts.server.internalAs(context.Background(), owner, "PUT", "/api/v1/users/bob", map[string]any{"role": "admin"})
	if code != http.StatusOK {
		t.Errorf("role change in-process: %d %s", code, body)
	}
}
