package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

func TestLockAccounts(t *testing.T) {
	ts := newTestServer(t)
	ts.setup() // owner "alex"
	for name, role := range map[string]auth.Role{"bea": auth.RoleAdmin, "gast": auth.RoleUser, "chris": auth.RoleOwner} {
		if res, body := ts.request("POST", "/api/v1/users", map[string]any{"name": name, "password": testPassword, "role": role}); res.StatusCode != http.StatusCreated {
			t.Fatalf("create %s: %d %s", name, res.StatusCode, body)
		}
	}
	login := func(name, password string) (*http.Client, *http.Response, []byte) {
		client := ts.newClient()
		res, body := ts.do(client, "POST", "/api/v1/login", credentials{Name: name, Password: password})
		return client, res, body
	}
	admin, _, _ := login("bea", testPassword)
	guest, _, _ := login("gast", testPassword)
	guestUser, _ := ts.users.Get("gast")
	_, plain, err := ts.server.tokens.Create(guestUser, "script", []string{"system:read"})
	if err != nil {
		t.Fatal(err)
	}
	bearer := func() int {
		t.Helper()
		req, _ := http.NewRequest("GET", ts.url+"/api/v1/system", nil)
		req.Header.Set("Authorization", "Bearer "+plain)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		io.Copy(io.Discard, res.Body)
		return res.StatusCode
	}
	if code := bearer(); code != http.StatusOK {
		t.Fatalf("token before the lock: %d", code)
	}

	// Who may lock whom.
	if res, _ := ts.do(guest, "POST", "/api/v1/users/bea/lock", nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("user locking an admin: %d", res.StatusCode)
	}
	if res, _ := ts.do(admin, "POST", "/api/v1/users/alex/lock", nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("admin locking an owner: %d", res.StatusCode)
	}
	if res, body := ts.do(admin, "POST", "/api/v1/users/bea/lock", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "cannot_lock_self" {
		t.Errorf("admin locking themselves: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/users/alex/lock", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "cannot_lock_self" {
		t.Errorf("owner locking themselves: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/users/niemand/lock", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("lock a missing account: %d", res.StatusCode)
	}

	// An administrator locks a user: sessions and tokens end at once.
	res, body := ts.do(admin, "POST", "/api/v1/users/gast/lock", nil)
	var locked accountResponse
	if json.Unmarshal(body, &locked) != nil || res.StatusCode != http.StatusOK || !locked.Disabled || locked.DisabledBy != "bea" || locked.DisabledAt == nil {
		t.Fatalf("lock: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("locked account still logged in: %d", res.StatusCode)
	}
	if code := bearer(); code != http.StatusUnauthorized {
		t.Errorf("token of a locked account: %d", code)
	}
	// The login says "locked" only with the right password.
	if _, res, body := login("gast", "a wrong password!"); res.StatusCode != http.StatusUnauthorized || errorCode(t, body) != "invalid_credentials" {
		t.Errorf("locked account, wrong password: %d %s", res.StatusCode, body)
	}
	if client, res, body := login("gast", testPassword); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "account_disabled" {
		t.Errorf("locked account, right password: %d %s", res.StatusCode, body)
	} else if res, _ := ts.do(client, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("refused login still gave a session: %d", res.StatusCode)
	}
	// Data, shares and rights stay: the account is still listed with its role.
	_, body = ts.request("GET", "/api/v1/users", nil)
	var list []accountResponse
	json.Unmarshal(body, &list)
	for _, account := range list {
		if account.Name == "gast" && (!account.Disabled || account.Role != auth.RoleUser) || account.Name != "gast" && account.Disabled {
			t.Errorf("list entry %+v", account)
		}
	}

	// The last owner that is not locked stays.
	if res, _ := ts.request("POST", "/api/v1/users/chris/lock", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("lock the second owner: %d", res.StatusCode)
	}
	if _, res, body := login("chris", testPassword); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "account_disabled" {
		t.Errorf("locked owner login: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/users/alex", map[string]any{"role": "admin"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "last_active_owner" {
		t.Errorf("demote the only owner that is not locked: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(admin, "POST", "/api/v1/users/chris/unlock", nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("admin unlocking an owner: %d", res.StatusCode)
	}

	// Unlocking lets the account in again; the token works again too (a new
	// password would end it, as before).
	if res, body := ts.do(admin, "POST", "/api/v1/users/gast/unlock", nil); res.StatusCode != http.StatusOK || !json.Valid(body) {
		t.Fatalf("unlock: %d %s", res.StatusCode, body)
	}
	if _, res, body := login("gast", testPassword); res.StatusCode != http.StatusOK {
		t.Errorf("login after unlock: %d %s", res.StatusCode, body)
	}
	if code := bearer(); code != http.StatusOK {
		t.Errorf("token after unlock: %d", code)
	}
	if user, _ := ts.users.Get("gast"); user.Disabled || user.DisabledBy != "" {
		t.Errorf("after unlock: %+v", user)
	}
}

// An open console notices a lock even where no session ends (API token or
// assistant identity).
func TestConsoleEndsWithTheLock(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.request("POST", "/api/v1/users", map[string]any{"name": "bea", "password": testPassword, "role": auth.RoleAdmin})
	bea, _ := ts.users.Get("bea")
	r := httptest.NewRequest("GET", "/api/v1/terminal/shell", nil)
	r = r.WithContext(context.WithValue(r.Context(), apiTokenIdentity{}, "bea"))
	if !ts.server.consoleAllowed(r, bea) {
		t.Fatal("console of an active administrator refused")
	}
	if _, err := ts.users.SetDisabled("bea", true, "alex"); err != nil {
		t.Fatal(err)
	}
	if ts.server.consoleAllowed(r, bea) {
		t.Error("console of a locked account still allowed")
	}
	if _, ok := ts.server.activeUser("bea"); ok {
		t.Error("activeUser returned a locked account")
	}
}

func TestLockIsInTheActivityLog(t *testing.T) {
	ts := newTestServer(t)
	log := activity.New(ts.store)
	ts.server.opts.Activity = log
	ts.setup()
	ts.request("POST", "/api/v1/users", map[string]any{"name": "gast", "password": testPassword, "role": auth.RoleUser})
	ts.request("POST", "/api/v1/users/gast/lock", nil)
	ts.do(ts.newClient(), "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	ts.request("POST", "/api/v1/users/gast/unlock", nil)
	rows := activityEntries(t, log)
	for _, action := range []string{"POST /api/v1/users/{name}/lock", "POST /api/v1/users/{name}/unlock"} {
		if e, ok := findActivity(rows, action); !ok || e.Target != "gast" || e.User != "alex" || !e.OK {
			t.Errorf("%s: %+v %v", action, e, ok)
		}
	}
	if e, ok := findActivity(rows, "login_disabled"); !ok || e.User != "gast" || e.OK {
		t.Errorf("refused login: %+v %v", e, ok)
	}
}
