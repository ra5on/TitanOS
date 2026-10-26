package api

import (
	"io"
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestBearerScopesRevocationAndCurrentRole(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	owner, _ := ts.users.Get("alex")
	token, plain, err := ts.server.tokens.Create(owner, "monitor", []string{"system:read", "docker:read"})
	if err != nil {
		t.Fatal(err)
	}
	call := func(method, path string) int {
		t.Helper()
		req, _ := http.NewRequest(method, ts.url+path, nil)
		req.Header.Set("Authorization", "Bearer "+plain)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		return resp.StatusCode
	}
	if code := call("GET", "/api/v1/system"); code != 200 {
		t.Fatal(code)
	}
	for _, v := range []struct{ method, path string }{{"GET", "/api/v1/account/tokens"}, {"GET", "/api/v1/users"}, {"GET", "/api/v1/terminal/shell"}, {"POST", "/api/v1/network/confirm"}, {"POST", "/api/v1/docker/containers/abc/start"}, {"GET", "/api/v1/files/list"}} {
		if code := call(v.method, v.path); code != 403 {
			t.Fatalf("%s %s: %d", v.method, v.path, code)
		}
	}
	if _, err := ts.users.Create("second", testPassword, auth.RoleOwner); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.users.SetRole("alex", auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	if code := call("GET", "/api/v1/docker"); code != 403 {
		t.Fatal("demoted token retained admin rights", code)
	}
	if err := ts.server.tokens.Revoke("second", token.ID); err == nil {
		t.Fatal("another account revoked token")
	}
	if err := ts.server.tokens.Revoke("alex", token.ID); err != nil {
		t.Fatal(err)
	}
	if code := call("GET", "/api/v1/system"); code != 401 {
		t.Fatal("revoked token works", code)
	}
}

func TestPublicContractNeverAllowsCredentialsOrUnknownActions(t *testing.T) {
	for _, p := range []string{"/api/v1/docker/containers/id/exec", "/api/v1/docker/containers/id/remove", "/api/v1/vms/a/console", "/api/v1/vms/a/snapshot", "/api/v1/account/ssh-keys", "/api/v1/setup", "/api/v1/updates/install", "/api/v1/assistant", "/api/v1/network"} {
		if publicScope("POST", p) != "" {
			t.Fatal("unexpected public mutation", p)
		}
	}
}
