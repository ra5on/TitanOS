package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/cluster"
	"github.com/ra5on/TitanOS/backend/internal/pim/devices"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// newDevice makes a device password through the page of the client's
// account.
func (ts *testServer) newDevice(client *http.Client, name string) devices.Created {
	ts.t.Helper()
	res, body := ts.do(client, "POST", "/api/v1/account/devices", map[string]string{"name": name, "password": testPassword})
	var created devices.Created
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &created) != nil || created.Password == "" {
		ts.t.Fatalf("new device %s: %d %s", name, res.StatusCode, body)
	}
	return created
}

// calAuth runs the checker of /caldav/ for Basic credentials.
func (ts *testServer) calAuth(user, password string) (devices.Identity, error) {
	req := httptest.NewRequest("PROPFIND", "/caldav/", nil)
	req.RemoteAddr = "192.0.2.10:5000"
	req.SetBasicAuth(user, password)
	return ts.server.devices.Authenticate(req)
}

func (ts *testServer) deviceList(client *http.Client, path string) []devices.Device {
	ts.t.Helper()
	res, body := ts.do(client, "GET", path, nil)
	var list struct {
		Devices []devices.Device
		Max     int
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &list) != nil || list.Max != devices.Max {
		ts.t.Fatalf("list %s: %d %s", path, res.StatusCode, body)
	}
	return list.Devices
}

func TestDeviceCreateNeedsPasswordAndCode(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	// A session alone makes none.
	if res, body := ts.request("POST", "/api/v1/account/devices", map[string]string{"name": "iPhone"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Fatalf("without password: %d %s", res.StatusCode, body)
	}
	// A bad name costs no attempt of the password.
	for _, name := range []string{"", strings.Repeat("x", 65), "a\u200bb"} {
		if res, body := ts.request("POST", "/api/v1/account/devices", map[string]string{"name": name, "password": "wrong"}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_device_name" {
			t.Fatalf("name %q: %d %s", name, res.StatusCode, body)
		}
	}
	res, body := ts.request("POST", "/api/v1/account/devices", map[string]string{"name": "iPhone von Alex", "password": testPassword})
	var created devices.Created
	json.Unmarshal(body, &created)
	if res.StatusCode != http.StatusOK || res.Header.Get("Cache-Control") != "no-store" || created.User != "alex" || len(created.Password) != 34 {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	// Shown once: the list and the stick hold no password and no hash.
	_, listed := ts.request("GET", "/api/v1/account/devices", nil)
	stored, _ := os.ReadFile(filepath.Join(ts.store.Dir(), "users", "alex", "devices.json"))
	if strings.Contains(string(listed), created.Password) || strings.Contains(string(listed), "sha256") || strings.Contains(string(stored), created.Password) {
		t.Fatalf("listed %s / stored %s", listed, stored)
	}
	if res, body := ts.request("POST", "/api/v1/account/devices", map[string]string{"name": "iphone von alex", "password": testPassword}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "device_name_taken" {
		t.Fatalf("same name: %d %s", res.StatusCode, body)
	}

	// With a second factor the code is needed as well.
	_, body = ts.request("POST", "/api/v1/account/2fa/begin", map[string]string{"password": testPassword})
	var begun struct{ Secret string }
	json.Unmarshal(body, &begun)
	if res, body := ts.request("POST", "/api/v1/account/2fa/enable", map[string]string{"code": auth.CodeForTest(begun.Secret, 0)}); res.StatusCode != http.StatusOK {
		t.Fatalf("2fa: %d %s", res.StatusCode, body)
	}
	// Switching it on oneself keeps the device (S-25: devices.Rebind).
	if _, err := ts.calAuth("alex", created.Password); err != nil {
		t.Fatalf("device after switching the second factor on: %v", err)
	}
	if res, body := ts.request("POST", "/api/v1/account/devices", map[string]string{"name": "Mac", "password": testPassword}); res.StatusCode != http.StatusForbidden {
		t.Fatalf("without code: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/devices", map[string]string{"name": "Mac", "password": testPassword, "code": auth.CodeForTest(begun.Secret, 1)}); res.StatusCode != http.StatusOK {
		t.Fatalf("with code: %d %s", res.StatusCode, body)
	}

	// A token never makes one.
	token := ts.mcpToken("alex", "system:read", "system:write")
	req, _ := http.NewRequest("POST", ts.url+"/api/v1/account/devices", strings.NewReader(`{"name":"x","password":"`+testPassword+`"}`))
	req.Header.Set("Authorization", "Bearer "+token)
	tres, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	tres.Body.Close()
	if tres.StatusCode != http.StatusForbidden && tres.StatusCode != http.StatusUnauthorized {
		t.Fatalf("token: %d", tres.StatusCode)
	}
	if n := len(ts.deviceList(ts.client, "/api/v1/account/devices")); n != 2 {
		t.Fatalf("%d devices", n)
	}
}

func TestDeviceLimitAndRevoke(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	first := ts.newDevice(ts.client, "iPhone")
	second := ts.newDevice(ts.client, "Mac")
	if id, err := ts.calAuth("alex", first.Password); err != nil || id.Device != first.ID {
		t.Fatalf("device: %+v %v", id, err)
	}
	// Revoking holds at once; the other device of the account stays.
	if res, _ := ts.request("DELETE", "/api/v1/account/devices/"+first.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke: %d", res.StatusCode)
	}
	for range 30 {
		if _, err := ts.calAuth("alex", first.Password); !errors.Is(err, devices.ErrRevoked) {
			t.Fatalf("revoked: %v", err)
		}
	}
	if _, err := ts.calAuth("alex", second.Password); err != nil {
		t.Fatalf("second device after the revoked one asked 30 times: %v", err)
	}
	if res, _ := ts.request("DELETE", "/api/v1/account/devices/"+first.ID, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("revoke again: %d", res.StatusCode)
	}
	// The 21st device: 409, before the password is asked.
	owner, _ := ts.users.Get("alex")
	for i := range devices.Max - 1 {
		if _, err := ts.server.devices.Create(owner, fmt.Sprintf("Gerät %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if res, body := ts.request("POST", "/api/v1/account/devices", map[string]string{"name": "zu viel", "password": "wrong"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "device_limit" {
		t.Fatalf("21st: %d %s", res.StatusCode, body)
	}
}

// Only device passwords open the calendar: not the account password, the
// WebDAV password or an API token.
func TestDeviceCheckerTakesNothingElse(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.newDevice(ts.client, "iPhone")
	_, body := ts.request("POST", "/api/v1/account/webdav", map[string]any{"password": testPassword})
	var dav struct{ Password string }
	json.Unmarshal(body, &dav)
	token := ts.mcpToken("alex", "system:read")
	for what, password := range map[string]string{"account password": testPassword, "WebDAV password": dav.Password, "API token": token} {
		if password == "" {
			t.Fatalf("no %s", what)
		}
		ts.server.limiter = auth.NewLimiter()
		if _, err := ts.calAuth("alex", password); !errors.Is(err, devices.ErrDenied) {
			t.Errorf("%s: %v", what, err)
		}
	}
	req := httptest.NewRequest("PROPFIND", "/caldav/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if _, err := ts.server.devices.Authenticate(req); !errors.Is(err, devices.ErrNoCredentials) {
		t.Errorf("Bearer: %v", err)
	}
}

// A device asks with every request: the stick is neither read nor
// written for it.
func TestDeviceRequestsLeaveTheStick(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	device := ts.newDevice(ts.client, "iPhone")
	if _, err := ts.calAuth("alex", device.Password); err != nil {
		t.Fatal(err)
	}
	writes := ts.store.Writes()
	// Taken away behind titand's back: the answer comes from RAM.
	if err := os.Remove(filepath.Join(ts.store.Dir(), "users", "alex", "devices.json")); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if _, err := ts.calAuth("alex", device.Password); err != nil {
			t.Fatalf("the stick was read again: %v", err)
		}
		ts.calAuth("alex", "wrong")
		ts.server.limiter = auth.NewLimiter()
	}
	if ts.store.Writes() != writes {
		t.Fatalf("%d writes for requests", ts.store.Writes()-writes)
	}
}

// Three devices, then a new account password: the old ones keep asking,
// and the device made afterwards gets through at once (cal-user:alex is
// not locked by them).
func TestDevicePasswordChangeDoesNotLockTheNewDevice(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	var old []devices.Created
	for i := range 3 {
		old = append(old, ts.newDevice(ts.client, fmt.Sprintf("Gerät %d", i)))
	}
	newPassword := testPassword + " new"
	if res, body := ts.request("POST", "/api/v1/users/alex/password", map[string]string{"password": newPassword, "current": testPassword}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("password: %d %s", res.StatusCode, body)
	}
	for range 30 {
		for _, d := range old {
			if _, err := ts.calAuth("alex", d.Password); !errors.Is(err, devices.ErrRevoked) {
				t.Fatalf("old device: %v", err)
			}
		}
	}
	res, body := ts.request("POST", "/api/v1/account/devices", map[string]string{"name": "neues iPhone", "password": newPassword})
	var fresh devices.Created
	if json.Unmarshal(body, &fresh); res.StatusCode != http.StatusOK {
		t.Fatalf("new device: %d %s", res.StatusCode, body)
	}
	if _, err := ts.calAuth("alex", fresh.Password); err != nil {
		t.Fatalf("new device after 90 refusals: %v", err)
	}
}

// One test per row of TECHNIK 3.3.
func TestDeviceDeletionRules(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	bea := ts.guest("bea")
	device := func() devices.Created { return ts.newDevice(bea, "iPhone") }
	revoked := func(row string, d devices.Created) {
		t.Helper()
		if _, err := ts.calAuth("bea", d.Password); !errors.Is(err, devices.ErrRevoked) {
			t.Fatalf("%s: %v", row, err)
		}
		if n := len(ts.deviceList(ts.client, "/api/v1/users/bea/devices")); n != 0 {
			t.Fatalf("%s: %d devices listed", row, n)
		}
	}
	file := filepath.Join(ts.store.Dir(), "users", "bea", "devices.json")

	// Own password changed.
	d := device()
	if res, _ := ts.do(bea, "POST", "/api/v1/users/bea/password", map[string]string{"password": testPassword, "current": testPassword}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("own password: %d", res.StatusCode)
	}
	revoked("own password", d)
	// Password set by an administrator.
	d = device()
	if res, _ := ts.request("POST", "/api/v1/users/bea/password", map[string]string{"password": testPassword}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("admin password: %d", res.StatusCode)
	}
	// bea's session ended with it.
	bea = ts.newClient()
	ts.do(bea, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword})
	revoked("password by admin", d)
	// Second factor reset.
	d = device()
	if res, body := ts.request("PUT", "/api/v1/users/bea", map[string]any{"role": "user", "resetTwoFactor": true}); res.StatusCode != http.StatusOK {
		t.Fatalf("reset: %d %s", res.StatusCode, body)
	}
	bea = ts.newClient()
	ts.do(bea, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword})
	revoked("2FA reset", d)
	// Password and TOTP changed through the cluster.
	for _, change := range []string{"password", "totp"} {
		d = device()
		before, _ := ts.users.Get("bea")
		after := before
		if change == "password" {
			after.PasswordHash += "x"
		} else {
			after.TOTPSecret = "JBSWY3DPEHPK3PXP"
		}
		ts.server.clusterArea().clusterApplied(before, true, after, false)
		revoked("cluster "+change, d)
		bea = ts.newClient()
		ts.do(bea, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword})
	}
	// Locked: refused, the device stays.
	d = device()
	if res, _ := ts.request("POST", "/api/v1/users/bea/lock", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("lock: %d", res.StatusCode)
	}
	if _, err := ts.calAuth("bea", d.Password); !errors.Is(err, devices.ErrDenied) {
		t.Fatalf("locked: %v", err)
	}
	ts.request("POST", "/api/v1/users/bea/unlock", nil)
	ts.server.limiter = auth.NewLimiter()
	if _, err := ts.calAuth("bea", d.Password); err != nil {
		t.Fatalf("unlocked: %v", err)
	}
	// Deleted: the file goes, RAM forgets; made anew: nothing comes back.
	if res, _ := ts.request("DELETE", "/api/v1/users/bea", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if _, err := os.Stat(file); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("devices.json after delete: %v", err)
	}
	if _, err := ts.calAuth("bea", d.Password); !errors.Is(err, devices.ErrDenied) {
		t.Fatalf("deleted: %v", err)
	}
	ts.request("POST", "/api/v1/users", map[string]any{"name": "bea", "password": testPassword, "role": "user"})
	if _, err := ts.calAuth("bea", d.Password); !errors.Is(err, devices.ErrDenied) {
		t.Fatalf("made anew: %v", err)
	}
	if n := len(ts.deviceList(ts.client, "/api/v1/users/bea/devices")); n != 0 {
		t.Fatalf("made anew: %d devices", n)
	}
	// Deleted and made anew through the cluster.
	bea = ts.newClient()
	ts.do(bea, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword})
	d = device()
	before, _ := ts.users.Get("bea")
	if err := ts.users.Delete("bea"); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.users.Create("bea", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	after, _ := ts.users.Get("bea")
	ts.server.clusterArea().clusterApplied(before, true, after, false)
	ts.server.limiter = auth.NewLimiter()
	if _, err := ts.calAuth("bea", d.Password); !errors.Is(err, devices.ErrDenied) {
		t.Fatalf("made anew elsewhere: %v", err)
	}
	bea = ts.newClient()
	ts.do(bea, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword})
	d = device()
	before, _ = ts.users.Get("bea")
	if err := ts.users.Delete("bea"); err != nil {
		t.Fatal(err)
	}
	ts.server.clusterArea().clusterApplied(before, true, auth.User{}, true)
	if _, err := ts.calAuth("bea", d.Password); !errors.Is(err, devices.ErrDenied) {
		t.Fatalf("deleted elsewhere: %v", err)
	}
}

// Owners and administrators see and revoke the devices of the accounts
// they manage; nobody else, and nobody makes one for another account.
func TestDeviceRightsOfOthers(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ownerDevice := ts.newDevice(ts.client, "iPhone")
	bob := ts.guest("bob")
	bobDevice := ts.newDevice(bob, "iPhone von Bob")
	if _, err := ts.users.Create("carol", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	carol := ts.newClient()
	ts.do(carol, "POST", "/api/v1/login", credentials{Name: "carol", Password: testPassword})

	// A user: only the own devices.
	if res, _ := ts.do(bob, "GET", "/api/v1/users/alex/devices", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("bob lists alex's: %d", res.StatusCode)
	}
	if res, _ := ts.do(bob, "DELETE", "/api/v1/users/alex/devices/"+ownerDevice.ID, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("bob revokes alex's: %d", res.StatusCode)
	}
	if res, _ := ts.do(bob, "DELETE", "/api/v1/account/devices/"+ownerDevice.ID, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("bob revokes alex's as his own: %d", res.StatusCode)
	}
	// An administrator: the user's, not the owner's.
	if res, _ := ts.do(carol, "GET", "/api/v1/users/alex/devices", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("admin lists the owner's: %d", res.StatusCode)
	}
	if res, _ := ts.do(carol, "DELETE", "/api/v1/users/alex/devices/"+ownerDevice.ID, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("admin revokes the owner's: %d", res.StatusCode)
	}
	if list := ts.deviceList(carol, "/api/v1/users/bob/devices"); len(list) != 1 || list[0].ID != bobDevice.ID {
		t.Fatalf("admin lists bob's: %+v", list)
	}
	if res, _ := ts.do(carol, "DELETE", "/api/v1/users/bob/devices/"+bobDevice.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("admin revokes bob's: %d", res.StatusCode)
	}
	if _, err := ts.calAuth("bob", bobDevice.Password); !errors.Is(err, devices.ErrRevoked) {
		t.Fatalf("bob's revoked device: %v", err)
	}
	if res, _ := ts.do(carol, "DELETE", "/api/v1/users/nobody/devices/x", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown account: %d", res.StatusCode)
	}
	// No route makes a device for another account.
	for _, pattern := range ts.server.patterns {
		if strings.HasPrefix(pattern, "POST ") && strings.Contains(pattern, "/users/") && strings.HasSuffix(pattern, "/devices") {
			t.Errorf("route %s", pattern)
		}
	}
	if _, err := ts.calAuth("alex", ownerDevice.Password); err != nil {
		t.Fatalf("owner's device: %v", err)
	}
}

// deviceRoutes are the routes of the device passwords.
func deviceRoutes(ts *testServer) [][2]string {
	var out [][2]string
	for _, pattern := range ts.server.patterns {
		method, path, ok := strings.Cut(pattern, " ")
		if !ok || !(strings.HasPrefix(path, "/api/v1/account/devices") || strings.HasPrefix(path, "/api/v1/users/{name}/devices")) {
			continue
		}
		path = strings.NewReplacer("{name}", "alex", "{id}", "d_x").Replace(path)
		out = append(out, [2]string{method, path})
	}
	return out
}

// Every route of the device passwords: a web session of this node only –
// never in-process (MCP, the agent), never a token, never through
// another node, never in publicRoutes.
func TestDeviceRouteRights(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	routes := deviceRoutes(ts)
	if len(routes) != 6 {
		t.Fatalf("routes: %v", routes)
	}
	for _, r := range publicRoutes {
		if strings.Contains(r.Path, "/account/devices") || strings.HasPrefix(r.Path, "/users/{name}/devices") {
			t.Errorf("public route %s %s", r.Method, r.Path)
		}
	}
	owner, _ := ts.users.Get("alex")
	token := ts.mcpToken("alex", "system:read", "system:write", "files:read", "files:write")
	for _, rt := range routes {
		code, body := ts.server.internalAs(context.Background(), owner, rt[0], rt[1], map[string]any{})
		if code != http.StatusForbidden || !strings.Contains(body, "browser_session_required") {
			t.Errorf("%s %s in-process: %d %s", rt[0], rt[1], code, body)
		}
		req, _ := http.NewRequest(rt[0], ts.url+rt[1], strings.NewReader("{}"))
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s with a token: %d", rt[0], rt[1], res.StatusCode)
		}
		creq := httptest.NewRequest(rt[0], rt[1], strings.NewReader("{}"))
		creq = creq.WithContext(cluster.WithIdentity(creq.Context(), cluster.Identity{Node: "n2", User: "alex"}))
		rec := httptest.NewRecorder()
		ts.server.ServeHTTP(rec, creq)
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "cluster_local_only") {
			t.Errorf("%s %s through the cluster: %d %s", rt[0], rt[1], rec.Code, rec.Body)
		}
		// The web session reaches the handler.
		if res, body := ts.request(rt[0], rt[1], map[string]any{}); strings.Contains(string(body), "browser_session_required") || res.StatusCode == http.StatusNotFound && !strings.Contains(string(body), "device_not_found") {
			t.Errorf("%s %s from the web UI: %d %s", rt[0], rt[1], res.StatusCode, body)
		}
	}
}

// A device password opens nothing but /caldav/ and /carddav/: not the
// shares over WebDAV, not the API, not MCP, not Assist (TECHNIK 3.4.6).
func TestDevicePasswordOpensNothingElse(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	ts.request("PUT", "/api/v1/system/webdav", map[string]any{"enabled": true})
	device := ts.newDevice(ts.client, "iPhone")
	for _, c := range []struct{ method, path, auth string }{
		{"PROPFIND", "/dav/", "basic"},
		{"GET", "/api/v1/system", "basic"},
		{"GET", "/api/v1/system", "bearer"},
		{"POST", "/mcp", "basic"},
		{"POST", "/mcp", "bearer"},
		{"GET", "/agent-ollama/api/tags", "basic"},
		{"GET", "/agent-ollama/api/tags", "bearer"},
	} {
		req, _ := http.NewRequest(c.method, ts.url+c.path, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		if c.auth == "basic" {
			req.SetBasicAuth("alex", device.Password)
		} else {
			req.Header.Set("Authorization", "Bearer "+device.Password)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode < 400 {
			t.Errorf("%s %s with the device password (%s): %d", c.method, c.path, c.auth, res.StatusCode)
		}
	}
}

// Deleting an account or making one anew removes its calendars and
// contacts from the volume (pimForgetUser → pim/store ForgetUser).
func TestDeviceAccountDeleteForgetsCalendarData(t *testing.T) {
	dir := t.TempDir()
	pim := pimstore.New()
	pim.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		path := filepath.Join(dir, ".titanos", feature)
		if create {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return nil, nil, err
			}
		}
		root, err := os.OpenRoot(path)
		return root, func() {}, err
	}
	ts := newTestServerWith(t, func(o *Options) {
		o.PIM = pim
		pim.Account = func(name string) (time.Time, bool) {
			u, ok := o.Users.Get(name)
			return u.Created, ok
		}
	})
	ts.setup()
	ts.guest("bea")
	event := []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:a\r\nDTSTAMP:20261101T000000Z\r\nDTSTART:20261110T080000Z\r\nDURATION:PT1H\r\nSUMMARY:A\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n")
	if _, err := pim.Put(pimstore.Calendar, "bea", pimstore.DefaultCalendar, "a.ics", event, pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, ".titanos", "calendar", "bea")
	if _, err := os.Stat(folder); err != nil {
		t.Fatal(err)
	}
	if res, _ := ts.request("DELETE", "/api/v1/users/bea", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if _, err := os.Stat(folder); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("calendar of the deleted account: %v", err)
	}
}

// TECHNIK 5.7 and 3.3, step 12: what T-Kalender and Kontakte keep of an
// account goes with it – deleted here, deleted by another node, made anew
// elsewhere (other creation time) – and a new account of the same name
// inherits nothing: no devices.json, no mark, no folder. The removal runs
// before the answer (as T-Mail's): a later one, in the background, would
// remove by name and could take the first entries of a new account of the
// same name with it.
func TestDeviceAccountDeleteTakesCalendarAlong(t *testing.T) {
	ts, pim, dir, _ := pimServer(t)
	folder := func(kind pimstore.Kind, name string) string {
		return filepath.Join(dir, "disk1", ".titanos", string(kind), name)
	}
	exists := func(path string) bool {
		_, err := os.Stat(path)
		return err == nil
	}
	keep := func(name string) {
		t.Helper()
		if _, err := pim.Put(pimstore.Calendar, name, pimstore.DefaultCalendar, "a.ics", pimEvent("a-"+name), pimstore.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := pim.Put(pimstore.Contacts, name, pimstore.DefaultBook, "c.vcf", pimCard("c-"+name), pimstore.PutOptions{}); err != nil {
			t.Fatal(err)
		}
		if !exists(folder(pimstore.Calendar, name)) || !exists(folder(pimstore.Contacts, name)) {
			t.Fatalf("%s: folders not written", name)
		}
		if !exists(filepath.Join(ts.store.Dir(), "users", name, "pim.json")) {
			t.Fatalf("%s: no mark", name)
		}
	}
	gone := func(step, name string) {
		t.Helper()
		for _, kind := range []pimstore.Kind{pimstore.Calendar, pimstore.Contacts} {
			if exists(folder(kind, name)) {
				t.Errorf("%s: %s folder of %s still there", step, kind, name)
			}
		}
		if exists(filepath.Join(ts.store.Dir(), "users", name, "pim.json")) {
			t.Errorf("%s: pim.json of %s still there", step, name)
		}
	}
	fresh := func(step, name string) {
		t.Helper()
		cols, err := pim.Collections(pimstore.Calendar, name)
		if err != nil || len(cols) != 1 || !cols[0].Virtual || cols[0].Count != 0 {
			t.Errorf("%s: calendars of the new %s: %+v %v", step, name, cols, err)
		}
		if events, contacts, err := pim.Counts(name); err != nil || events != 0 || contacts != 0 {
			t.Errorf("%s: counts of the new %s: %d %d %v", step, name, events, contacts, err)
		}
	}

	// Deleted here.
	ts.guest("bea")
	keep("bea")
	ts.newDevice(ts.client, "iPhone") // the owner's; bea's own below
	if res, _ := ts.request("DELETE", "/api/v1/users/bea", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	gone("deleted here", "bea")
	// Made anew with the same name: inherits nothing, and what it keeps
	// stays (nothing of the deletion runs later).
	if res, body := ts.request("POST", "/api/v1/users", map[string]any{"name": "bea", "password": testPassword, "role": "user"}); res.StatusCode != http.StatusCreated {
		t.Fatalf("made anew: %d %s", res.StatusCode, body)
	}
	fresh("made anew", "bea")
	if n := len(ts.deviceList(ts.client, "/api/v1/users/bea/devices")); n != 0 {
		t.Fatalf("made anew: %d devices", n)
	}
	if _, err := pim.Put(pimstore.Calendar, "bea", pimstore.DefaultCalendar, "new.ics", pimEvent("new"), pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	ts.server.jobs.Wait()
	if !exists(filepath.Join(folder(pimstore.Calendar, "bea"), pimstore.DefaultCalendar, "o", "new.ics")) {
		t.Fatal("the new account's first event went with the old account's deletion")
	}

	// Deleted by another node (the cluster's record of the deletion).
	ts.guest("cleo")
	keep("cleo")
	before, _ := ts.users.Get("cleo")
	if err := ts.users.Delete("cleo"); err != nil {
		t.Fatal(err)
	}
	ts.server.clusterArea().clusterApplied(before, true, auth.User{}, true)
	gone("deleted elsewhere", "cleo")

	// Made anew elsewhere: same name, another creation time.
	ts.guest("dina")
	keep("dina")
	before, _ = ts.users.Get("dina")
	if err := ts.users.Delete("dina"); err != nil {
		t.Fatal(err)
	}
	// In the same second: auth gives the new account the next one.
	if _, err := ts.users.Create("dina", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	after, _ := ts.users.Get("dina")
	if after.Created.Equal(before.Created) {
		t.Fatal("same creation time")
	}
	ts.server.clusterArea().clusterApplied(before, true, after, false)
	gone("made anew elsewhere", "dina")
	fresh("made anew elsewhere", "dina")

	// Changed elsewhere without a new creation time (a role): stays.
	ts.guest("emil")
	keep("emil")
	before, _ = ts.users.Get("emil")
	after = before
	after.Role = auth.RoleAdmin
	ts.server.clusterArea().clusterApplied(before, true, after, false)
	if !exists(folder(pimstore.Calendar, "emil")) {
		t.Fatal("a role changed elsewhere removed the calendars")
	}
}

// Deleted while the data volume is missing: the folder stays where it is,
// pim.json goes with users/<name>, and a new account of the same name sees
// none of it – the dialog said "nicht zu zählen", not 0 – and its first
// write replaces the old folder.
func TestDeviceAccountDeleteWithoutTheVolume(t *testing.T) {
	ts, pim, dir, setVolume := pimServer(t)
	ts.guest("bea")
	if _, err := pim.Put(pimstore.Calendar, "bea", pimstore.DefaultCalendar, "old.ics", pimEvent("old"), pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := pim.Put(pimstore.Contacts, "bea", pimstore.DefaultBook, "old.vcf", pimCard("old"), pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "disk1", ".titanos", "calendar", "bea", pimstore.DefaultCalendar, "o", "old.ics")
	mark := filepath.Join(ts.store.Dir(), "users", "bea", "pim.json")
	for _, p := range []string{old, mark} {
		if _, err := os.Stat(p); err != nil {
			t.Fatal(err)
		}
	}

	setVolume("")
	var data agentAccountData
	_, body := ts.request("GET", "/api/v1/users/bea/agent-data", nil)
	if err := json.Unmarshal(body, &data); err != nil || !data.CalendarUnknown || data.CalendarEvents != nil || data.Contacts != nil {
		t.Fatalf("agent-data without the volume: %s", body)
	}
	if strings.Contains(string(body), "calendarEvents") || strings.Contains(string(body), `"contacts"`) {
		t.Fatalf("a count without the volume: %s", body)
	}
	if res, _ := ts.request("DELETE", "/api/v1/users/bea", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("the folder on the missing volume: %v", err)
	}
	if _, err := os.Stat(mark); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("pim.json after the deletion: %v", err)
	}
	// Made anew while the volume is still missing – in the same second as
	// the deletion: auth gives it the next second, so owner.json tells the
	// two apart. 503, never an empty list.
	if res, body := ts.request("POST", "/api/v1/users", map[string]any{"name": "bea", "password": testPassword, "role": "user"}); res.StatusCode != http.StatusCreated {
		t.Fatalf("made anew: %d %s", res.StatusCode, body)
	}
	if _, err := pim.Collections(pimstore.Calendar, "bea"); !errors.Is(err, pimstore.ErrUnavailable) {
		t.Fatalf("without the volume: %v", err)
	}

	// The volume back: the old folder is not the new account's.
	setVolume("disk1")
	cols, err := pim.Collections(pimstore.Calendar, "bea")
	if err != nil || len(cols) != 1 || !cols[0].Virtual || cols[0].Count != 0 {
		t.Fatalf("calendars of the new bea: %+v %v", cols, err)
	}
	if _, err := pim.Get(pimstore.Calendar, "bea", pimstore.DefaultCalendar, "old.ics"); !errors.Is(err, pimstore.ErrNotFound) {
		t.Fatalf("the old event for the new account: %v", err)
	}
	data = agentAccountData{}
	_, body = ts.request("GET", "/api/v1/users/bea/agent-data", nil)
	if err := json.Unmarshal(body, &data); err != nil || data.CalendarUnknown || data.CalendarEvents == nil || *data.CalendarEvents != 0 || data.Contacts == nil || *data.Contacts != 0 {
		t.Fatalf("agent-data of the new bea: %s", body)
	}
	// Its first write replaces the old folder.
	if _, err := pim.Put(pimstore.Calendar, "bea", pimstore.DefaultCalendar, "new.ics", pimEvent("new"), pimstore.PutOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("the old event after the new account's first write: %v", err)
	}
	if events, _, err := pim.Counts("bea"); err != nil || events != 1 {
		t.Fatalf("counts after the first write: %d %v", events, err)
	}
}
