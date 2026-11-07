package api

import (
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// The second factor of one's own account is set up, renewed, switched off
// and reset only with the password (and code) of the account, not with the
// session alone (owner decision 7.11.2026). An administrator resetting
// another account keeps the plain flow.
func TestOwnTwoFactorNeedsThePassword(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	for _, body := range []any{map[string]string{}, map[string]string{"password": "wrong password"}} {
		if res, out := ts.request("POST", "/api/v1/account/2fa/begin", body); res.StatusCode != http.StatusForbidden || errorCode(t, out) != "wrong_password" {
			t.Fatalf("begin without the password: %d %s", res.StatusCode, out)
		}
	}
	// The failed attempts above are not counted again here; a fresh
	// limiter keeps the test independent of the limit.
	ts.server.limiter = auth.NewLimiter()
	secret, _, err := ts.users.BeginTwoFactor("alex", "TitanOS test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ts.users.EnableTwoFactor("alex", auth.CodeForTest(secret, 0)); err != nil {
		t.Fatal(err)
	}
	// Resetting one's own second factor through the account list.
	for _, body := range []map[string]any{
		{"role": "owner", "resetTwoFactor": true},
		{"role": "owner", "resetTwoFactor": true, "password": testPassword, "code": "000000"},
	} {
		if res, out := ts.request("PUT", "/api/v1/users/alex", body); res.StatusCode != http.StatusForbidden {
			t.Fatalf("own reset %v: %d %s", body, res.StatusCode, out)
		}
		if user, _ := ts.users.Get("alex"); !user.TwoFactor() {
			t.Fatal("own second factor reset without proof")
		}
	}
	if res, out := ts.request("PUT", "/api/v1/users/alex", map[string]any{"role": "owner", "resetTwoFactor": true, "password": testPassword, "code": auth.CodeForTest(secret, 1)}); res.StatusCode != http.StatusOK {
		t.Fatalf("own reset with proof: %d %s", res.StatusCode, out)
	}
	if user, _ := ts.users.Get("alex"); user.TwoFactor() {
		t.Fatal("own second factor not reset")
	}
	// The reset ended every session of the account; after logging in
	// again, setting it up with the password works.
	if res, out := ts.request("POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", res.StatusCode, out)
	}
	if res, out := ts.request("POST", "/api/v1/account/2fa/begin", map[string]string{"password": testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("begin with the password: %d %s", res.StatusCode, out)
	}
}
