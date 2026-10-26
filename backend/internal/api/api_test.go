package api

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/backuptarget"
	"github.com/ra5on/TitanOS/backend/internal/catalog"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/configbackup"
	"github.com/ra5on/TitanOS/backend/internal/diagnose"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/files"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/network"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/sshd"
	"github.com/ra5on/TitanOS/backend/internal/storage"
	"github.com/ra5on/TitanOS/backend/internal/system"
	"github.com/ra5on/TitanOS/backend/internal/tailscale"
	"github.com/ra5on/TitanOS/backend/internal/terminal"
	"github.com/ra5on/TitanOS/backend/internal/update"
	"github.com/ra5on/TitanOS/backend/internal/ups"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

var testUI = fstest.MapFS{
	"index.html":                  {Data: []byte("<!doctype html><title>index</title>")},
	"favicon.svg":                 {Data: []byte("<svg/>")},
	"_app/immutable/entry/app.js": {Data: []byte("console.log(1)")},
}

const testPassword = "a long test password"

type fakePower struct{ actions chan string }

func (p fakePower) Reboot() error   { p.actions <- "reboot"; return nil }
func (p fakePower) Poweroff() error { p.actions <- "poweroff"; return nil }

type testServer struct {
	server *Server
	t      *testing.T
	url    string
	store  *config.Store
	users  *auth.Users
	power  fakePower
	client *http.Client
	update *update.Manager
	disks  *storage.Manager
	shares *shares.Manager
	home   *homeassistant.Manager
}

func newTestServer(t *testing.T) *testServer {
	t.Helper()
	store := config.Open(t.TempDir())
	users := auth.NewUsers(store)
	if err := users.Load(); err != nil {
		t.Fatal(err)
	}
	power := fakePower{actions: make(chan string, 1)}
	updater := disabledUpdate(store)
	disks := loadedStorage(t, store)
	shareManager := loadedShares(t, store)
	home := homeassistant.NewManager(store)
	home.Load()
	notifier := loadedNotify(store)
	notifier.SetHomeAssistant(home)
	handler := New(Options{
		UI: testUI, Store: store, Users: users, Monitor: system.NewMonitor(context.Background()), Energy: disabledEnergy(store), MemoryPolicy: disabledMemory(store), PowerButton: disabledPowerButton(store), Tailscale: disabledTailscale(store),
		Storage: disks, Shares: shareManager, Files: files.NewManager(0), Docker: disabledDocker(store), Catalog: loadedCatalog(t, store), BackupTarget: loadedTarget(store), Terminal: noShell(store), VMs: disabledVMs(store), Hardware: hardware.NewManager(), Plugins: disabledPlugins(store), Network: disabledNetwork(store), Notify: notifier, HomeAssistant: home, ConfigBackup: disabledBackup(store), Diagnose: diagnose.NewManager(store, "1.2.3"), SSH: disabledSSH(store), Update: updater, Backup: disabledBackups(store), UPS: disabledUPS(store), Settings: disabledSettings(store), Power: power, Version: "1.2.3",
	})
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	ts := &testServer{server: handler, t: t, url: srv.URL, store: store, users: users, power: power, update: updater, disks: disks, shares: shareManager, home: home}
	ts.client = ts.newClient()
	return ts
}

func disabledTailscale(store *config.Store) *tailscale.Manager {
	m := tailscale.NewManager(store)
	m.Disabled = true
	m.Load()
	return m
}

func disabledPowerButton(store *config.Store) *system.PowerButtonManager {
	m := system.NewPowerButtonManager(store)
	m.Disabled = true
	m.Load()
	return m
}

func disabledEnergy(store *config.Store) *system.EnergyManager {
	m := system.NewEnergyManager(store)
	m.Disabled = true
	m.Load()
	return m
}

func TestEnergyAndNotificationDismissalPermissions(t *testing.T) {
	ts := newTestServer(t)
	for _, route := range []struct{ method, path string }{{"GET", "/backups/progress"}, {"GET", "/system/memory"}, {"PUT", "/system/memory"}, {"GET", "/plugins/tailscale"}, {"PUT", "/plugins/tailscale"}, {"POST", "/plugins/tailscale/control/connect"}, {"GET", "/system/power-button"}, {"PUT", "/system/power-button"}, {"GET", "/system/energy"}, {"PUT", "/system/energy"}, {"POST", "/notifications/dismiss"}} {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", route.path, res.StatusCode)
		}
	}
	ts.setup()
	for _, path := range []string{"/system/memory", "/system/power-button", "/plugins/tailscale"} {
		if res, body := ts.request("GET", "/api/v1"+path, nil); res.StatusCode != 200 || !strings.Contains(string(body), `"readOnly":true`) {
			t.Fatalf("owner status %s: %d %s", path, res.StatusCode, body)
		}
		if res, _ := ts.request("PUT", "/api/v1"+path, map[string]string{"action": "ignore"}); res.StatusCode != 409 {
			t.Fatalf("development mode allowed changes: %s %d", path, res.StatusCode)
		}
	}
	if res, body := ts.request("GET", "/api/v1/system/energy", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"readOnly":true`) {
		t.Fatalf("energy: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/system/energy", map[string]string{"governor": "powersave"}); res.StatusCode != http.StatusConflict {
		t.Fatal("development mode allowed energy changes")
	}
	if res, _ := ts.request("POST", "/api/v1/notifications/dismiss", map[string]any{"ids": []int{1}}); res.StatusCode != http.StatusOK {
		t.Fatalf("owner dismissal: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/notifications/dismiss", map[string]any{"ids": []int{-1}}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("invalid message ID accepted")
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, route := range []struct{ method, path string }{{"GET", "/system/memory"}, {"PUT", "/system/memory"}, {"GET", "/plugins/tailscale"}, {"PUT", "/plugins/tailscale"}, {"POST", "/plugins/tailscale/control/connect"}, {"GET", "/system/power-button"}, {"PUT", "/system/power-button"}, {"GET", "/system/energy"}, {"PUT", "/system/energy"}, {"POST", "/notifications/dismiss"}} {
		if res, _ := ts.do(guest, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Fatalf("user %s: %d", route.path, res.StatusCode)
		}
	}
}

// disabledDocker returns a Docker manager that never touches the system.
func disabledDocker(store *config.Store) *docker.Manager {
	m := docker.NewManager(store, "")
	m.Disabled = true
	m.Load()
	return m
}

// disabledUPS returns a UPS manager that never touches the system.
func disabledUPS(store *config.Store) *ups.Manager {
	m := ups.NewManager(store)
	m.Disabled = true
	m.Load()
	return m
}

// disabledBackups returns a backup manager that runs nothing.
func disabledBackups(store *config.Store) *backup.Manager {
	m := backup.NewManager(store)
	m.Disabled = true
	m.Sources = func() []backup.Source { return []backup.Source{{Name: "media", Volume: "tank"}} }
	m.Load()
	return m
}

// disabledUpdate returns an updater that never touches a stick.
func disabledUpdate(store *config.Store) *update.Manager {
	m := update.NewManager(store, "1.2.3", "")
	m.Disabled = true
	m.Load()
	return m
}

// disabledSSH returns an SSH manager that never touches the system.
func disabledSSH(store *config.Store) *sshd.Manager {
	m := sshd.NewManager(store, 100)
	m.Disabled = true
	m.Load()
	return m
}

// disabledBackup returns a backup manager that never touches the system.
func disabledBackup(store *config.Store) *configbackup.Manager {
	m := configbackup.NewManager(store, "1.2.3")
	m.Disabled = true
	m.Load()
	return m
}

// loadedNotify returns a notifier without any channel.
func loadedNotify(store *config.Store) *notify.Manager {
	m := notify.NewManager(store)
	// Test servers listen on loopback, which only administrators may reach.
	m.Confined = func(string) bool { return false }
	m.Load()
	return m
}

// disabledNetwork returns a network manager that never touches the system.
func disabledNetwork(store *config.Store) *network.Manager {
	m := network.NewManager(store)
	m.Disabled = true
	m.Load(context.Background())
	return m
}

// disabledSettings returns system settings that never touch the system.
func disabledSettings(store *config.Store) *system.Settings {
	s := system.NewSettings(store)
	s.Disabled = true
	s.Load()
	return s
}

// disabledPlugins returns a plugin manager that never touches the system.
func disabledPlugins(store *config.Store) *plugins.Manager {
	m := plugins.NewManager(store)
	m.Disabled = true
	m.Load()
	return m
}

// disabledVMs returns a VM manager that never touches the system.
func disabledVMs(store *config.Store) *vm.Manager {
	m := vm.NewManager(store, 0)
	m.Disabled = true
	m.Load()
	return m
}

// noShell returns a terminal manager that never starts a shell.
func noShell(store *config.Store) *terminal.Manager {
	m := terminal.NewManager(store)
	m.Disabled = true
	m.Load()
	return m
}

// loadedCatalog returns a catalog whose official source is a local server
// with one app.
func loadedCatalog(t *testing.T, store *config.Store) *catalog.Manager {
	t.Helper()
	var buf bytes.Buffer
	zipped := gzip.NewWriter(&buf)
	archive := tar.NewWriter(zipped)
	compose := "services:\n  web:\n    image: traefik/whoami\n"
	archive.WriteHeader(&tar.Header{Name: "apps-HEAD/whoami/compose.yaml", Mode: 0o644, Size: int64(len(compose)), Typeflag: tar.TypeReg})
	archive.Write([]byte(compose))
	archive.Close()
	zipped.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(buf.Bytes()) }))
	t.Cleanup(srv.Close)
	m := catalog.NewManager(store)
	m.SetOfficialURL(srv.URL + "/apps")
	m.Load()
	m.Refresh(context.Background())
	return m
}

// loadedTarget returns a backup target that serves nothing (development mode).
func loadedTarget(store *config.Store) *backuptarget.Manager {
	m := backuptarget.NewManager(store)
	m.Disabled = true
	m.Load()
	return m
}

// loadedShares returns a share manager that never touches the system.
func loadedShares(t *testing.T, store *config.Store) *shares.Manager {
	t.Helper()
	m := shares.NewManager(store)
	m.Disabled = true
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m
}

func loadedStorage(t *testing.T, store *config.Store) *storage.Manager {
	t.Helper()
	m := storage.NewManager(store)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m
}

// newClient returns a client with its own cookies: a separate browser.
func (ts *testServer) newClient() *http.Client {
	jar, _ := cookiejar.New(nil)
	return &http.Client{Jar: jar}
}

func (ts *testServer) do(client *http.Client, method, path string, body any) (*http.Response, []byte) {
	ts.t.Helper()
	var reader io.Reader
	switch b := body.(type) {
	case nil:
	case []byte:
		reader = bytes.NewReader(b)
	default:
		data, _ := json.Marshal(b)
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, ts.url+path, reader)
	if err != nil {
		ts.t.Fatal(err)
	}
	res, err := client.Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(res.Body)
	return res, data
}

func (ts *testServer) request(method, path string, body any) (*http.Response, []byte) {
	ts.t.Helper()
	return ts.do(ts.client, method, path, body)
}

func (ts *testServer) setup() {
	ts.t.Helper()
	res, body := ts.request("POST", "/api/v1/setup", credentials{Name: "alex", Password: testPassword})
	if res.StatusCode != http.StatusCreated {
		ts.t.Fatalf("setup: %d %s", res.StatusCode, body)
	}
}

func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var res map[string]apiError
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("error body is not JSON: %s", body)
	}
	return res["error"].Code
}

func session(t *testing.T, body []byte) sessionResponse {
	t.Helper()
	var res sessionResponse
	if err := json.Unmarshal(body, &res); err != nil {
		t.Fatalf("session body: %s", body)
	}
	return res
}

func TestSetupLoginLogout(t *testing.T) {
	ts := newTestServer(t)

	_, body := ts.request("GET", "/api/v1/session", nil)
	if s := session(t, body); !s.SetupRequired || !s.ConfigAvailable || s.User != nil || s.Version != "" {
		t.Fatalf("fresh system: %+v", s)
	}

	// Nothing but the public endpoints works before login.
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("system without login: %d", res.StatusCode)
	}

	for _, tc := range []struct{ name, password, code string }{
		{"Alex Example", testPassword, "invalid_name"},
		{"root", testPassword, "reserved_name"},
		{"alex", "short", "password_too_short"},
	} {
		res, body := ts.request("POST", "/api/v1/setup", credentials{Name: tc.name, Password: tc.password})
		if res.StatusCode != http.StatusBadRequest || errorCode(t, body) != tc.code {
			t.Errorf("setup %q: %d %s, want %s", tc.name, res.StatusCode, body, tc.code)
		}
	}

	ts.setup()
	_, body = ts.request("GET", "/api/v1/session", nil)
	if s := session(t, body); s.SetupRequired || s.User == nil || s.User.Name != "alex" || s.User.Role != auth.RoleOwner || s.Version != "1.2.3" {
		t.Fatalf("after setup: %+v", s)
	}

	// Setup is closed for good once the owner exists.
	other := ts.newClient()
	res, body := ts.do(other, "POST", "/api/v1/setup", credentials{Name: "eve", Password: testPassword})
	if res.StatusCode != http.StatusConflict || errorCode(t, body) != "setup_done" {
		t.Errorf("second setup: %d %s", res.StatusCode, body)
	}

	if res, _ := ts.request("POST", "/api/v1/logout", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("logout: %d", res.StatusCode)
	}
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("system after logout: %d", res.StatusCode)
	}

	res, body = ts.request("POST", "/api/v1/login", credentials{Name: "alex", Password: "the wrong password"})
	if res.StatusCode != http.StatusUnauthorized || errorCode(t, body) != "invalid_credentials" {
		t.Errorf("wrong password: %d %s", res.StatusCode, body)
	}
	// Names are not case sensitive at the login form.
	res, body = ts.request("POST", "/api/v1/login", credentials{Name: " Alex ", Password: testPassword})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", res.StatusCode, body)
	}
	cookie := res.Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Value == "" {
		t.Errorf("session cookie = %+v", cookie)
	}
	if res, body := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"version":"1.2.3"`) {
		t.Errorf("system after login: %d %s", res.StatusCode, body)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	attacker := ts.newClient()

	for i := range 5 {
		res, _ := ts.do(attacker, "POST", "/api/v1/login", credentials{Name: "alex", Password: "guess number one"})
		if res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d", i+1, res.StatusCode)
		}
	}
	res, body := ts.do(attacker, "POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword})
	if res.StatusCode != http.StatusTooManyRequests || errorCode(t, body) != "rate_limited" {
		t.Fatalf("after 5 failures even the right password must wait: %d %s", res.StatusCode, body)
	}
	var parsed map[string]apiError
	json.Unmarshal(body, &parsed)
	if parsed["error"].RetryAfter <= 0 {
		t.Errorf("retryAfter = %d", parsed["error"].RetryAfter)
	}
}

func TestPreferences(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()

	_, body := ts.request("GET", "/api/v1/preferences", nil)
	if strings.TrimSpace(string(body)) != `{"appearance":null,"assistant":null,"bell":null,"dashboard":null,"docker":null,"files":null,"shares":null,"sound":null,"vnc":null}` {
		t.Errorf("fresh preferences = %s", body)
	}

	if res, body := ts.request("PUT", "/api/v1/preferences/appearance", map[string]any{"theme": "hell"}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("put: %d %s", res.StatusCode, body)
	}
	_, body = ts.request("GET", "/api/v1/preferences", nil)
	if !strings.Contains(string(body), `"appearance":{"theme":"hell"}`) {
		t.Errorf("preferences = %s", body)
	}
	if stored, err := ts.store.Read("users/alex/appearance.json"); err != nil || !strings.Contains(string(stored), "hell") {
		t.Errorf("not written to the store: %s %v", stored, err)
	}

	// File manager settings belong to the authenticated account and survive a
	// new preference cache (as after restarting titand), not just a browser.
	files := map[string]any{"view": "large", "sort": "modified", "descending": true,
		"favorites": []map[string]string{{"root": "share/Backup", "path": "MeineFotos"}}}
	if res, body := ts.request("PUT", "/api/v1/preferences/files", files); res.StatusCode != http.StatusNoContent {
		t.Fatalf("put files: %d %s", res.StatusCode, body)
	}
	reloaded := newPreferences(ts.store)
	stored, err := reloaded.get("alex", "files")
	if err != nil || !strings.Contains(string(stored), `"view":"large"`) || !strings.Contains(string(stored), "MeineFotos") {
		t.Fatalf("files preferences not persisted: %s %v", stored, err)
	}
	other, err := reloaded.get("other", "files")
	if err != nil || other != nil {
		t.Fatalf("preferences leaked to another account: %s %v", other, err)
	}
	_, body = ts.request("GET", "/api/v1/preferences", nil)
	if !strings.Contains(string(body), `"files":{"descending":true`) {
		t.Fatalf("files missing from response: %s", body)
	}

	if res, _ := ts.request("PUT", "/api/v1/preferences/appearance", []byte(`["not", "an", "object"]`)); res.StatusCode != http.StatusBadRequest {
		t.Errorf("array accepted: %d", res.StatusCode)
	}
	if res, _ := ts.request("PUT", "/api/v1/preferences/account", map[string]any{"role": "owner"}); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown kind accepted: %d", res.StatusCode)
	}
	if res, _ := ts.request("PUT", "/api/v1/preferences/dashboard", []byte(`{"x":"`+strings.Repeat("a", maxDashboardPreferences)+`"}`)); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversized body: %d", res.StatusCode)
	}
	if res, _ := ts.request("PUT", "/api/v1/preferences/appearance", []byte(`{"x":"`+strings.Repeat("a", maxJSONBody)+`"}`)); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("appearance bound changed: %d", res.StatusCode)
	}
	large := []byte(`{"version":5,"tileOptions":{"system":{"title":"` + strings.Repeat("a", maxJSONBody) + `"}}}`)
	if res, _ := ts.request("PUT", "/api/v1/preferences/dashboard", large); res.StatusCode != http.StatusNoContent {
		t.Fatalf("large dashboard rejected: %d", res.StatusCode)
	}
	_, largeStored := ts.request("GET", "/api/v1/preferences", nil)
	if !bytes.Contains(largeStored, large) {
		t.Fatal("large dashboard not retained")
	}
}

func TestBackground(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()

	if res, _ := ts.request("GET", "/api/v1/background", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("no image yet: %d", res.StatusCode)
	}
	if res, body := ts.request("PUT", "/api/v1/background", []byte("<script>alert(1)</script>")); res.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("HTML accepted as image: %d %s", res.StatusCode, body)
	}

	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")
	if res, body := ts.request("PUT", "/api/v1/background", png); res.StatusCode != http.StatusNoContent {
		t.Fatalf("upload: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("GET", "/api/v1/background", nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "image/png" || !bytes.Equal(body, png) {
		t.Errorf("download: %d %q", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if res, _ := ts.request("DELETE", "/api/v1/background", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", res.StatusCode)
	}
	if res, _ := ts.request("GET", "/api/v1/background", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("after delete: %d", res.StatusCode)
	}
}

func TestPower(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()

	if res, _ := ts.request("POST", "/api/v1/system/power", map[string]string{"action": "format-everything"}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown action: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/system/power", map[string]string{"action": "reboot"}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("reboot: %d", res.StatusCode)
	}
	if got := <-ts.power.actions; got != "reboot" {
		t.Errorf("carried out %q", got)
	}

	// A plain user may not restart the NAS.
	if _, err := ts.users.Create("gast", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	guest := ts.newClient()
	if res, body := ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("login: %d %s", res.StatusCode, body)
	}
	res, body := ts.do(guest, "POST", "/api/v1/system/power", map[string]string{"action": "poweroff"})
	if res.StatusCode != http.StatusForbidden || errorCode(t, body) != "forbidden" {
		t.Errorf("user powering off: %d %s", res.StatusCode, body)
	}
}

func TestCrossSiteRequestsAreRejected(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()

	req, _ := http.NewRequest("POST", ts.url+"/api/v1/system/power", strings.NewReader(`{"action":"poweroff"}`))
	req.Header.Set("Origin", "https://evil.example")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	res, err := ts.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site request: %d, want 403", res.StatusCode)
	}
	select {
	case action := <-ts.power.actions:
		t.Errorf("cross-site request carried out %q", action)
	default:
	}
}

func TestConfigUnavailable(t *testing.T) {
	store := config.Open("/proc/titanos-no-such-dir")
	users := auth.NewUsers(store)
	srv := httptest.NewServer(New(Options{UI: testUI, Store: store, Users: users, Monitor: system.NewMonitor(context.Background()), Storage: storage.NewManager(store), Shares: shares.NewManager(store), Files: files.NewManager(0), Docker: disabledDocker(store), Catalog: loadedCatalog(t, store), BackupTarget: loadedTarget(store), Terminal: noShell(store), VMs: disabledVMs(store), Hardware: hardware.NewManager(), Plugins: disabledPlugins(store), Network: disabledNetwork(store), Notify: loadedNotify(store), ConfigBackup: disabledBackup(store), Diagnose: diagnose.NewManager(store, "1.2.3"), SSH: disabledSSH(store), Update: disabledUpdate(store), Backup: disabledBackups(store), UPS: disabledUPS(store), Settings: disabledSettings(store), Power: fakePower{}}))
	defer srv.Close()
	ts := &testServer{t: t, url: srv.URL, client: http.DefaultClient}

	_, body := ts.request("GET", "/api/v1/session", nil)
	if s := session(t, body); s.ConfigAvailable || s.SetupRequired {
		t.Errorf("without stick: %+v, want neither config nor setup", s)
	}
	res, body := ts.request("POST", "/api/v1/setup", credentials{Name: "alex", Password: testPassword})
	if res.StatusCode != http.StatusServiceUnavailable || errorCode(t, body) != "config_unavailable" {
		t.Errorf("setup without stick: %d %s", res.StatusCode, body)
	}
}

func TestHealthAndUnknownAPIPath(t *testing.T) {
	ts := newTestServer(t)
	if res, body := ts.request("GET", "/api/v1/health", nil); res.StatusCode != http.StatusOK || strings.Contains(string(body), "1.2.3") {
		t.Errorf("health: %d %s (must not tell the version)", res.StatusCode, body)
	}
	res, body := ts.request("GET", "/api/v1/does-not-exist", nil)
	if res.StatusCode != http.StatusNotFound || errorCode(t, body) != "not_found" {
		t.Errorf("unknown path: %d %s", res.StatusCode, body)
	}
}

func TestUI(t *testing.T) {
	ts := newTestServer(t)
	tests := []struct {
		name      string
		target    string
		wantBody  string
		wantCache string
	}{
		{"root", "/", "<title>index</title>", "no-cache"},
		{"client-side route", "/storage/volumes", "<title>index</title>", "no-cache"},
		{"directory", "/_app/immutable/", "<title>index</title>", "no-cache"},
		{"file", "/favicon.svg", "<svg/>", "no-cache"},
		{"hashed file", "/_app/immutable/entry/app.js", "console.log(1)", "public, max-age=31536000, immutable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res, body := ts.request("GET", tt.target, nil)
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.StatusCode)
			}
			if !strings.Contains(string(body), tt.wantBody) {
				t.Errorf("body = %q, want it to contain %q", body, tt.wantBody)
			}
			if cc := res.Header.Get("Cache-Control"); cc != tt.wantCache {
				t.Errorf("Cache-Control = %q, want %q", cc, tt.wantCache)
			}
			if res.Header.Get("X-Frame-Options") != "DENY" {
				t.Error("X-Frame-Options missing")
			}
		})
	}
	if res, _ := ts.request("POST", "/", nil); res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST /: %d, want 405", res.StatusCode)
	}
}

// These requests are refused before any disk is looked at, so the test is
// safe on any machine.
func TestStorageRefusals(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()

	for _, tc := range []struct {
		method, path string
		body         any
		status       int
		code         string
	}{
		{"POST", "/api/v1/storage/volumes", map[string]any{"name": "Bad Name", "level": "raid1", "filesystem": "ext4"}, http.StatusBadRequest, "invalid_volume_name"},
		{"POST", "/api/v1/storage/volumes", map[string]any{"name": "data", "level": "raid9", "filesystem": "ext4"}, http.StatusBadRequest, "invalid_level"},
		{"POST", "/api/v1/storage/volumes", map[string]any{"name": "data", "level": "raid1", "filesystem": "ext4", "disks": []string{"sdx", "sdy"}}, http.StatusBadRequest, "not_confirmed"},
		{"DELETE", "/api/v1/storage/volumes/data", map[string]any{"confirm": "data"}, http.StatusNotFound, "volume_not_found"},
		{"POST", "/api/v1/storage/volumes/data/disks", map[string]any{"disk": "sdx", "erase": true}, http.StatusNotFound, "volume_not_found"},
		{"POST", "/api/v1/storage/volumes/data/check", map[string]any{"action": "format"}, http.StatusBadRequest, "bad_request"},
	} {
		res, body := ts.request(tc.method, tc.path, tc.body)
		if res.StatusCode != tc.status || errorCode(t, body) != tc.code {
			t.Errorf("%s %s: %d %s, want %d %s", tc.method, tc.path, res.StatusCode, body, tc.status, tc.code)
		}
	}

	// A plain user may look but not touch.
	if _, err := ts.users.Create("gast", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	res, body := ts.do(guest, "POST", "/api/v1/storage/volumes", map[string]any{"name": "data"})
	if res.StatusCode != http.StatusForbidden || errorCode(t, body) != "forbidden" {
		t.Errorf("user creating a volume: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(ts.newClient(), "GET", "/api/v1/storage", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("storage without login: %d", res.StatusCode)
	}
}

func TestUserManagement(t *testing.T) {
	ts := newTestServer(t)
	ts.setup() // owner "alex"

	create := func(client *http.Client, name string, role auth.Role) (*http.Response, []byte) {
		return ts.do(client, "POST", "/api/v1/users", map[string]any{"name": name, "password": testPassword, "role": role})
	}
	if res, body := create(ts.client, "bea", auth.RoleAdmin); res.StatusCode != http.StatusCreated {
		t.Fatalf("create admin: %d %s", res.StatusCode, body)
	}
	if res, body := create(ts.client, "bea", auth.RoleUser); res.StatusCode != http.StatusConflict || errorCode(t, body) != "user_exists" {
		t.Errorf("duplicate: %d %s", res.StatusCode, body)
	}
	if res, _ := create(ts.client, "gast", auth.RoleUser); res.StatusCode != http.StatusCreated {
		t.Fatal("create user")
	}

	admin, guest := ts.newClient(), ts.newClient()
	ts.do(admin, "POST", "/api/v1/login", credentials{Name: "bea", Password: testPassword})
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})

	// An administrator manages users, but not owners.
	if res, body := create(admin, "chef", auth.RoleOwner); res.StatusCode != http.StatusForbidden {
		t.Errorf("admin creating an owner: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(admin, "PUT", "/api/v1/users/alex", map[string]any{"role": "user"}); res.StatusCode != http.StatusForbidden {
		t.Errorf("admin demoting the owner: %d", res.StatusCode)
	}
	if res, _ := ts.do(admin, "POST", "/api/v1/users/alex/password", map[string]any{"password": "a new password for alex"}); res.StatusCode != http.StatusForbidden {
		t.Errorf("admin resetting the owner's password: %d", res.StatusCode)
	}
	if res, _ := ts.do(admin, "DELETE", "/api/v1/users/alex", nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("admin deleting the owner: %d", res.StatusCode)
	}
	// A plain user manages nobody and cannot list accounts.
	if res, _ := ts.do(guest, "GET", "/api/v1/users", nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("user listing accounts: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "POST", "/api/v1/users/bea/password", map[string]any{"password": "hijacked password"}); res.StatusCode != http.StatusForbidden {
		t.Errorf("user resetting another password: %d", res.StatusCode)
	}

	// Own password: needs the current one.
	if res, body := ts.do(guest, "POST", "/api/v1/users/gast/password", map[string]any{"password": "my new password!", "current": "wrong guess here"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Errorf("own password with wrong current: %d %s", res.StatusCode, body)
	}
	other := ts.newClient() // gast on a second device
	ts.do(other, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, body := ts.do(guest, "POST", "/api/v1/users/gast/password", map[string]any{"password": "my new password!", "current": testPassword}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("own password: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK {
		t.Errorf("the device that changed the password was logged out: %d", res.StatusCode)
	}
	if res, _ := ts.do(other, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("the other device is still logged in with the old password: %d", res.StatusCode)
	}

	// The owner cannot delete themselves or stop being the last owner.
	if res, body := ts.request("DELETE", "/api/v1/users/alex", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "cannot_delete_self" {
		t.Errorf("deleting oneself: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/users/alex", map[string]any{"role": "admin"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "last_owner" {
		t.Errorf("last owner demoting themselves: %d %s", res.StatusCode, body)
	}

	// Deleting an account ends its sessions.
	if res, _ := ts.request("DELETE", "/api/v1/users/gast", nil); res.StatusCode != http.StatusNoContent {
		t.Fatal("delete user")
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("deleted account is still logged in: %d", res.StatusCode)
	}
	_, body := ts.request("GET", "/api/v1/users", nil)
	if !strings.Contains(string(body), `"name":"bea"`) || strings.Contains(string(body), "gast") || strings.Contains(string(body), "argon2") || strings.Contains(string(body), "smbHash") {
		t.Errorf("account list: %s", body)
	}
}

func TestShareAccessInTheAPI(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.users.Create("gast", testPassword, auth.RoleUser)
	// No volume exists on the test machine: creating a share is refused …
	res, body := ts.request("POST", "/api/v1/shares", map[string]any{"name": "fotos", "volume": "data", "smb": true})
	if res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "unknown_volume" {
		t.Errorf("share on a missing volume: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/shares", map[string]any{"name": "Fotos!", "volume": "data"}); errorCode(t, body) != "invalid_share_name" {
		t.Errorf("bad name: %d %s", res.StatusCode, body)
	}
	// … and a plain user cannot create one at all.
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, _ := ts.do(guest, "POST", "/api/v1/shares", map[string]any{"name": "fotos", "volume": "data"}); res.StatusCode != http.StatusForbidden {
		t.Errorf("user creating a share: %d", res.StatusCode)
	}
	if res, body := ts.do(guest, "GET", "/api/v1/shares", nil); res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("user listing shares: %d %s", res.StatusCode, body)
	}
}

// Without shares there is nowhere to go: every root is refused the same way.
func TestFilesNeedARoot(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	// An administrator sees the stick, and nothing else yet.
	if res, body := ts.request("GET", "/api/v1/files/roots", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"id":"stick"`) || strings.Contains(string(body), "share/") {
		t.Errorf("roots: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("GET", "/api/v1/files/list?root=stick", nil)
	if res.StatusCode != http.StatusOK || strings.Contains(string(body), `"users"`) {
		t.Errorf("stick listing: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/files/list?root=stick&path=users", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("secrets of the stick: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/files/text", map[string]string{"root": "stick", "path": "nicht-da.json", "content": "{}", "version": "test"}); res.StatusCode != http.StatusNotFound {
		t.Errorf("saving a file that is not there: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/usb", nil); res.StatusCode != http.StatusOK || strings.TrimSpace(string(body)) != "[]" {
		t.Errorf("usb: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("GET", "/api/v1/files/jobs", nil); res.StatusCode != http.StatusOK {
		t.Errorf("jobs: %d", res.StatusCode)
	}
	for _, target := range []string{
		"/api/v1/files/list?root=share/fotos",
		"/api/v1/files/list?root=volume/data",
		"/api/v1/files/list?root=../../etc",
		"/api/v1/files/download?root=share/x&path=/etc/passwd",
		"/api/v1/files/zip?root=share/x&path=a",
		"/api/v1/files/trash?root=share/x&path=admin",
	} {
		if res, body := ts.request("GET", target, nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "file_not_found" {
			t.Errorf("%s: %d %s", target, res.StatusCode, body)
		}
	}
	// The stick has no recycle bin.
	if res, body := ts.request("GET", "/api/v1/files/trash?root=stick", nil); res.StatusCode != http.StatusBadRequest {
		t.Errorf("recycle bin of the stick: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/files/trash/restore", map[string]any{"root": "share/x", "paths": []string{"admin/a"}}); res.StatusCode != http.StatusNotFound {
		t.Errorf("restore without a root: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/files/uploads", map[string]any{"root": "share/x", "name": "a", "size": 1}); res.StatusCode != http.StatusNotFound {
		t.Errorf("upload without a root: %d", res.StatusCode)
	}
	if res, _ := ts.do(ts.newClient(), "GET", "/api/v1/files/roots", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("roots without login: %d", res.StatusCode)
	}
}

func TestDockerIsForAdministrators(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	res, body := ts.request("GET", "/api/v1/docker", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"running":false`) {
		t.Errorf("docker status: %d %s", res.StatusCode, body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{
		{"GET", "/api/v1/docker"},
		{"POST", "/api/v1/docker/containers/aaaaaaaaaaaa/start"},
		{"POST", "/api/v1/docker/apps"},
		{"GET", "/api/v1/docker/containers/aaaaaaaaaaaa/logs"},
	} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestVMsAreForAdministrators(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	res, body := ts.request("GET", "/api/v1/vms", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"vms":[]`) || !strings.Contains(string(body), `"cpus":`) {
		t.Errorf("VM list: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/vms/nope/start", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("start in development mode: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/vms/nope/screen", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "vm_not_running" {
		t.Errorf("screen of a VM that does not run: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/vms/nope/xml", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "vm_not_found" {
		t.Errorf("description of an unknown VM: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/vms/host", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"cpus":[{"id":0`) || !strings.Contains(string(body), `"images":[]`) {
		t.Errorf("host: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/hardware", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"usedBy":{}`) {
		t.Errorf("hardware: %d %s", res.StatusCode, body)
	}
	// A device that is not there cannot be given to a VM.
	spec := map[string]any{"name": "x", "template": "linux", "memoryMiB": 1024, "vcpus": 1, "pci": []string{"0000:ff:1f.7"}}
	if res, body := ts.request("POST", "/api/v1/vms", spec); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "device_unavailable" {
		t.Errorf("VM with a device that does not exist: %d %s", res.StatusCode, body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{
		{"GET", "/api/v1/vms"},
		{"GET", "/api/v1/vms/host"},
		{"GET", "/api/v1/hardware"},
		{"GET", "/api/v1/vms/x/serial"},
		{"GET", "/api/v1/vms/x/snapshots"},
		{"POST", "/api/v1/vms/x/snapshots"},
		{"POST", "/api/v1/vms/x/snapshots/test/restore"},
		{"DELETE", "/api/v1/vms/x/snapshots/test"},
		{"PUT", "/api/v1/vms/x/snapshot-schedule"},
		{"POST", "/api/v1/vms/x/snapshot-recovery"},
		{"POST", "/api/v1/vms/x/grow"},
		{"POST", "/api/v1/vms"},
		{"GET", "/api/v1/vms/media"},
		{"POST", "/api/v1/vms/x/start"},
		{"DELETE", "/api/v1/vms/x"},
		{"GET", "/api/v1/vms/x/screen"},
	} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestTwoFactorLogin(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	res, body := ts.request("POST", "/api/v1/account/2fa/begin", nil)
	var begun struct{ Secret, URI string }
	if json.Unmarshal(body, &begun) != nil || res.StatusCode != http.StatusOK || begun.Secret == "" {
		t.Fatalf("begin: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/2fa/enable", map[string]string{"code": "000000"}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_code" {
		t.Errorf("enable with a wrong code: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/account/2fa/enable", map[string]string{"code": auth.CodeForTest(begun.Secret, 0)})
	var enabled struct{ RecoveryCodes []string }
	if json.Unmarshal(body, &enabled) != nil || res.StatusCode != http.StatusOK || len(enabled.RecoveryCodes) != 8 {
		t.Fatalf("enable: %d %s", res.StatusCode, body)
	}
	if _, body := ts.request("GET", "/api/v1/session", nil); !strings.Contains(string(body), `"twoFactor":true`) {
		t.Errorf("session: %s", body)
	}

	// A new login: the password alone is not enough any more.
	other := ts.newClient()
	login := func(code string) (*http.Response, []byte) {
		return ts.do(other, "POST", "/api/v1/login", map[string]string{"name": "alex", "password": testPassword, "code": code})
	}
	if res, body := login(""); res.StatusCode != http.StatusUnauthorized || errorCode(t, body) != "second_factor_required" {
		t.Errorf("login without a code: %d %s", res.StatusCode, body)
	}
	if res, body := login("000000"); res.StatusCode != http.StatusUnauthorized || errorCode(t, body) != "invalid_code" {
		t.Errorf("login with a wrong code: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(other, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("a session exists without the second factor: %d", res.StatusCode)
	}
	// With a wrong password nothing tells that a second factor exists.
	if res, body := ts.do(other, "POST", "/api/v1/login", map[string]string{"name": "alex", "password": "wrong password"}); errorCode(t, body) != "invalid_credentials" {
		t.Errorf("wrong password: %d %s", res.StatusCode, body)
	}
	if res, body := login(auth.CodeForTest(begun.Secret, 1)); res.StatusCode != http.StatusOK {
		t.Fatalf("login with a code: %d %s", res.StatusCode, body)
	}
	if res, body := login(enabled.RecoveryCodes[0]); res.StatusCode != http.StatusOK {
		t.Errorf("login with a recovery code: %d %s", res.StatusCode, body)
	}
	if res, body := login(enabled.RecoveryCodes[0]); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("a recovery code worked twice: %d %s", res.StatusCode, body)
	}

	// The list of devices, and ending one of them.
	res, body = ts.request("GET", "/api/v1/account/sessions", nil)
	var devices struct {
		Sessions []struct {
			ID      string
			Current bool
		}
	}
	if json.Unmarshal(body, &devices) != nil || len(devices.Sessions) != 3 {
		t.Fatalf("sessions: %d %s", res.StatusCode, body)
	}
	for _, device := range devices.Sessions {
		if !device.Current {
			if res, _ := ts.request("DELETE", "/api/v1/account/sessions/"+device.ID, nil); res.StatusCode != http.StatusOK {
				t.Errorf("end a session: %d", res.StatusCode)
			}
		}
	}
	if res, _ := ts.do(other, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("the ended device is still logged in: %d", res.StatusCode)
	}

	// Sensitive confirmations take the code as well; switching it off takes both.
	if res, body := ts.request("POST", "/api/v1/session/confirm", map[string]string{"password": testPassword}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "invalid_code" {
		t.Errorf("confirm without a code: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/2fa/disable", map[string]string{"password": testPassword, "code": "000000"}); res.StatusCode != http.StatusForbidden {
		t.Errorf("switch off with a wrong code: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/2fa/disable", map[string]string{"password": testPassword, "code": enabled.RecoveryCodes[1]}); res.StatusCode != http.StatusNoContent {
		t.Errorf("switch off: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(ts.newClient(), "POST", "/api/v1/login", map[string]string{"name": "alex", "password": testPassword}); res.StatusCode != http.StatusOK {
		t.Errorf("login after switching it off: %d", res.StatusCode)
	}
}

func TestNetworkIsForAdministrators(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	res, body := ts.request("GET", "/api/v1/network", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"dhcp":true`) || !strings.Contains(string(body), `"pending":null`) || !strings.Contains(string(body), `"bondModes":[`) {
		t.Errorf("network: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/network", map[string]any{"hostname": "nas", "dhcp": true}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("change in development mode: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/network/bridge", map[string]any{}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("bridge changed network in development: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/network/confirm", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "network_not_pending" {
		t.Errorf("confirm without a change: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/system/timezone", map[string]string{"timezone": "Europe/Atlantis"}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_timezone" {
		t.Errorf("unknown time zone: %d %s", res.StatusCode, body)
	}
	if _, body := ts.request("GET", "/api/v1/system", nil); !strings.Contains(string(body), `"networkPendingSeconds":0`) {
		t.Errorf("system info: %s", body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{{"GET", "/api/v1/network"}, {"PUT", "/api/v1/network"}, {"POST", "/api/v1/network/bridge"}, {"POST", "/api/v1/network/confirm"}, {"POST", "/api/v1/network/revert"}, {"PUT", "/api/v1/system/timezone"}} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestNotificationsKeepTheirSecrets(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	settings := map[string]any{"language": "de", "minSeverity": "warning",
		"ntfy": map[string]any{"enabled": true, "url": "http://127.0.0.1:1/topic", "token": "tk_secret"}}
	res, body := ts.request("PUT", "/api/v1/notifications/settings", settings)
	if res.StatusCode != http.StatusOK || strings.Contains(string(body), "tk_secret") || !strings.Contains(string(body), `"ntfy":true`) {
		t.Errorf("settings: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/notifications/settings", map[string]any{"language": "xx", "minSeverity": "warning"}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_notify_settings" {
		t.Errorf("unknown language: %d %s", res.StatusCode, body)
	}
	// Nothing listens there: the test says so instead of claiming success.
	if res, body := ts.request("POST", "/api/v1/notifications/test", map[string]string{"channel": "ntfy"}); res.StatusCode != http.StatusBadGateway || errorCode(t, body) != "notify_test_failed" {
		t.Errorf("test without a server: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/notifications", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"messages":[]`) {
		t.Errorf("messages: %d %s", res.StatusCode, body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{{"GET", "/api/v1/notifications"}, {"GET", "/api/v1/notifications/settings"}, {"PUT", "/api/v1/notifications/settings"}, {"POST", "/api/v1/notifications/test"}} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestRestoreOnlyOnANewStick(t *testing.T) {
	ts := newTestServer(t)
	// Before the owner exists the disks may be searched for backups.
	if res, body := ts.request("GET", "/api/v1/setup/backups", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"backups":[]`) {
		t.Errorf("backups on a new stick: %d %s", res.StatusCode, body)
	}
	ts.setup()
	for _, tc := range [][2]string{{"GET", "/api/v1/setup/backups"}, {"POST", "/api/v1/setup/restore"}, {"POST", "/api/v1/setup/restore/upload"}} {
		if res, body := ts.request(tc[0], tc[1], map[string]string{"device": "md127", "name": "x"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "setup_done" {
			t.Errorf("%s %s after the setup: %d %s", tc[0], tc[1], res.StatusCode, body)
		}
	}

	res, body := ts.request("GET", "/api/v1/config-backup", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"enabled":true`) || !strings.Contains(string(body), `"keep":14`) {
		t.Errorf("state: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/config-backup", map[string]any{"enabled": true, "volume": "nowhere", "keep": 14}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_backup_settings" {
		t.Errorf("unknown volume: %d %s", res.StatusCode, body)
	}
	// A backup holds every secret: not without the password once more.
	if res, body := ts.request("GET", "/api/v1/config-backup/download?volume=data&name=config-20261013T120000Z-000000000000.tar.gz", nil); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "confirm_required" {
		t.Errorf("download without confirmation: %d %s", res.StatusCode, body)
	}
	ts.request("POST", "/api/v1/session/confirm", map[string]string{"password": testPassword})
	if res, body := ts.request("GET", "/api/v1/config-backup/download?volume=data&name=config-20261013T120000Z-000000000000.tar.gz", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "backup_not_found" {
		t.Errorf("download of a backup that is not there: %d %s", res.StatusCode, body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{{"GET", "/api/v1/config-backup"}, {"PUT", "/api/v1/config-backup"}, {"POST", "/api/v1/config-backup/run"}, {"GET", "/api/v1/config-backup/download"}} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestDiagnosticsAreForAdministrators(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	for _, query := range []string{"source=nonsense", "priority=9", "priority=x", "after=a%20--file%3D/etc/shadow"} {
		if res, body := ts.request("GET", "/api/v1/logs?"+query, nil); res.StatusCode != http.StatusBadRequest {
			t.Errorf("logs?%s: %d %s", query, res.StatusCode, body)
		}
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, path := range []string{"/api/v1/logs", "/api/v1/diagnostics/bundle"} {
		if res, _ := ts.do(guest, "GET", path, nil); res.StatusCode != http.StatusForbidden {
			t.Errorf("GET %s as a plain user: %d, want 403", path, res.StatusCode)
		}
	}
}

func TestSSHKeysAndAccess(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	const key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGaa0YUn9eEcPL0FzQ0qzL9dXYrmsBrrmIjXzyEGPBaN test@laptop"
	res, body := ts.request("POST", "/api/v1/account/ssh-keys", map[string]string{"key": key})
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"type":"ssh-ed25519"`) || !strings.Contains(string(body), `"comment":"test@laptop"`) || !strings.Contains(string(body), `"mayLogin":true`) || !strings.Contains(string(body), `"sshEnabled":false`) {
		t.Fatalf("add: %d %s", res.StatusCode, body)
	}
	// The key itself is not repeated.
	if strings.Contains(string(body), "AAAAC3NzaC1lZDI1NTE5") {
		t.Errorf("the answer repeats the key: %s", body)
	}
	if res, body := ts.request("POST", "/api/v1/account/ssh-keys", map[string]string{"key": key}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "ssh_key_exists" {
		t.Errorf("twice: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/ssh-keys", map[string]string{"key": `command="id" ` + key}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_ssh_key" {
		t.Errorf("with options: %d %s", res.StatusCode, body)
	}
	var listed struct{ Keys []struct{ ID string } }
	json.Unmarshal(body[:0], &listed)
	_, body = ts.request("GET", "/api/v1/account/ssh-keys", nil)
	json.Unmarshal(body, &listed)
	if res, body := ts.request("DELETE", "/api/v1/account/ssh-keys/"+listed.Keys[0].ID, nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"keys":[]`) {
		t.Errorf("remove: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("DELETE", "/api/v1/account/ssh-keys/"+listed.Keys[0].ID, nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "ssh_key_not_found" {
		t.Errorf("remove again: %d %s", res.StatusCode, body)
	}

	if res, body := ts.request("GET", "/api/v1/system/ssh", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"enabled":false`) {
		t.Errorf("status: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/system/ssh", map[string]bool{"enabled": true}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("switching on in development mode: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/system/certificate", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("certificate without TLS: %d %s", res.StatusCode, body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{{"GET", "/api/v1/system/ssh"}, {"PUT", "/api/v1/system/ssh"}, {"GET", "/api/v1/system/certificate"}, {"PUT", "/api/v1/system/certificate"}, {"DELETE", "/api/v1/system/certificate"}} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
	// A plain user may keep keys, but is told that they open nothing.
	if res, body := ts.do(guest, "GET", "/api/v1/account/ssh-keys", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"mayLogin":false`) {
		t.Errorf("keys of a plain user: %d %s", res.StatusCode, body)
	}
}

func TestUpdatesAreForAdministrators(t *testing.T) {
	ts := newTestServer(t)
	if res, _ := ts.request("GET", "/api/v1/update/progress", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("progress without a login: %d", res.StatusCode)
	}
	ts.setup()
	if res, body := ts.request("GET", "/api/v1/update/progress", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"progress":`) || strings.Contains(string(body), `"slots":`) {
		t.Errorf("progress: %d %s", res.StatusCode, body)
	}
	res, body := ts.request("GET", "/api/v1/update", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"version":"1.2.3"`) || !strings.Contains(string(body), `"channel":"stable"`) || !strings.Contains(string(body), `"canVerify":false`) {
		t.Errorf("status: %d %s", res.StatusCode, body)
	}
	// This build holds no key: nothing can be checked, so nothing is installed.
	if res, body := ts.request("POST", "/api/v1/update/check", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "update_no_key" {
		t.Errorf("check without a key: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/update/install", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("install in development mode: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/update/settings", map[string]any{"channel": "nightly", "installTime": "03:30"}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_update_settings" {
		t.Errorf("unknown channel: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/update/settings", map[string]any{"channel": "beta", "installTime": "04:15", "autoInstall": true, "source": "https://evil.example"}); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"channel":"stable"`) || strings.Contains(string(body), "evil") {
		t.Errorf("settings: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/health", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"ready":`) {
		t.Errorf("health: %d %s", res.StatusCode, body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{{"GET", "/api/v1/update"}, {"GET", "/api/v1/update/progress"}, {"PUT", "/api/v1/update/settings"}, {"POST", "/api/v1/update/check"}, {"POST", "/api/v1/update/install"}, {"POST", "/api/v1/update/rollback"}} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestAbout(t *testing.T) {
	ts := newTestServer(t)
	if res, _ := ts.request("GET", "/api/v1/about", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("without a login: %d", res.StatusCode)
	}
	ts.setup()
	if res, body := ts.request("GET", "/api/v1/about", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"version":"1.2.3"`) || !strings.Contains(string(body), `"thirdParty":`) {
		t.Errorf("about: %d %s", res.StatusCode, body)
	}
}

func TestBackupTargetSettings(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("GET", "/api/v1/backups/target", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"enabled":false`) || !strings.Contains(string(body), `"sources":[]`) {
		t.Errorf("target: %d %s", res.StatusCode, body)
	}
	// In development mode nothing is served or paired.
	if res, body := ts.request("PUT", "/api/v1/backups/target", map[string]any{"enabled": true, "volume": "tank"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("switching on: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/backups/target/sources", map[string]string{"name": "keller"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("pairing: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("DELETE", "/api/v1/backups/target/sources/keller", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "backup_source_not_found" {
		t.Errorf("removing an unknown source: %d %s", res.StatusCode, body)
	}
	guest := ts.newClient()
	ts.users.Create("gast", testPassword, auth.RoleUser)
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, _ := ts.do(guest, "GET", "/api/v1/backups/target", nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("target as a plain user: %d", res.StatusCode)
	}
}

func TestBackupsKeepTheirSecrets(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	job := map[string]any{"id": "cloud", "sources": []string{"media"}, "enabled": true, "every": "daily", "time": "02:30",
		"destination": map[string]any{"type": "s3", "endpoint": "https://s3.example.com", "bucket": "nas-backup", "accessKey": "AKIA1", "secretKey": "s3-secret-value"}}
	res, body := ts.request("POST", "/api/v1/backups", job)
	if res.StatusCode != http.StatusOK || strings.Contains(string(body), "s3-secret-value") || !strings.Contains(string(body), `"secretKey":true`) || strings.Contains(string(body), `"password":"`) {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/backups/folders?share=gone&dir=", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "backup_job_not_found" {
		t.Errorf("folders of an unknown share: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/backups", job); res.StatusCode != http.StatusConflict || errorCode(t, body) != "backup_job_exists" {
		t.Errorf("twice: %d %s", res.StatusCode, body)
	}
	job["sources"] = []string{"gone"}
	if res, body := ts.request("PUT", "/api/v1/backups/cloud", job); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_backup_job" {
		t.Errorf("unknown share: %d %s", res.StatusCode, body)
	}
	// The password of the backup only after the account's password.
	if res, body := ts.request("GET", "/api/v1/backups/cloud/password", nil); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "confirm_required" {
		t.Errorf("password without confirmation: %d %s", res.StatusCode, body)
	}
	ts.request("POST", "/api/v1/session/confirm", map[string]string{"password": testPassword})
	if res, body := ts.request("GET", "/api/v1/backups/cloud/password", nil); res.StatusCode != http.StatusOK || len(body) < 40 {
		t.Errorf("password: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/backups/cloud/run", nil); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("run in development mode: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("DELETE", "/api/v1/backups/cloud", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"jobs":[]`) {
		t.Errorf("delete: %d %s", res.StatusCode, body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{{"GET", "/api/v1/backups"}, {"POST", "/api/v1/backups"}, {"POST", "/api/v1/backups/smb/discover"}, {"PUT", "/api/v1/backups/smb"}, {"DELETE", "/api/v1/backups/smb/test"}, {"POST", "/api/v1/backups/ssh-key"}, {"GET", "/api/v1/backups/x/snapshots"}, {"GET", "/api/v1/backups/x/password"}, {"POST", "/api/v1/backups/x/restore"}, {"POST", "/api/v1/backups/x/run"}} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestUPSIsForAdministrators(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("GET", "/api/v1/ups", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"mode":"off"`) || !strings.Contains(string(body), `"usbhid-ups"`) {
		t.Errorf("status: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/ups", map[string]any{"mode": "local"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("change in development mode: %d %s", res.StatusCode, body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{{"GET", "/api/v1/ups"}, {"PUT", "/api/v1/ups"}, {"POST", "/api/v1/ups/scan"}} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestPluginsAreForAdministrators(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("GET", "/api/v1/plugins", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"plugins":[]`) {
		t.Errorf("plugins: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/plugins", map[string]string{"path": "/mnt/x/y.plugin.json"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("install in development mode: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/plugins/catalog", map[string]bool{}); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"state":"development"`) {
		t.Errorf("catalog in development build: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/plugins", map[string]string{"name": "nvidia", "volume": "data"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Errorf("download in development mode: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/plugins", map[string]string{"name": "nvidia", "path": "/mnt/x/y.plugin.json"}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("ambiguous install: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/plugins/x/explode", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("unknown action: %d", res.StatusCode)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{{"GET", "/api/v1/plugins"}, {"POST", "/api/v1/plugins"}, {"POST", "/api/v1/plugins/catalog"}, {"POST", "/api/v1/plugins/nvidia/updates"}, {"POST", "/api/v1/plugins/x/enable"}, {"DELETE", "/api/v1/plugins/x"}} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestAppStore(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	res, body := ts.request("GET", "/api/v1/apps", nil)
	var store struct {
		Sources []struct {
			ID      string
			Builtin bool
			Count   int
		}
		Apps []struct{ ID, Source, Name string }
	}
	if err := json.Unmarshal(body, &store); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("apps: %d %s", res.StatusCode, body)
	}
	if len(store.Sources) != 1 || !store.Sources[0].Builtin || len(store.Apps) == 0 || store.Sources[0].Count != len(store.Apps) {
		t.Fatalf("store = %+v", store)
	}
	// The compose file of a template never shows up in the list.
	if strings.Contains(string(body), "services:") {
		t.Error("the list carries compose files")
	}

	app := store.Apps[0]
	res, body = ts.request("GET", "/api/v1/apps/sources/"+app.Source+"/apps/"+app.ID, nil)
	var tpl struct {
		Compose, Timezone, Appdata string
		UID, GID                   int
		MissingDevices             []string
	}
	if err := json.Unmarshal(body, &tpl); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("template: %d %s", res.StatusCode, body)
	}
	if !strings.Contains(tpl.Compose, "services:") || tpl.Timezone == "" || tpl.UID < 2000 || tpl.GID != 100 || tpl.MissingDevices == nil {
		t.Errorf("template = %+v", tpl)
	}
	if res, body := ts.request("GET", "/api/v1/apps/sources/titanos/apps/nope", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "source_not_found" {
		t.Errorf("unknown template: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/apps/sources", map[string]string{"url": "file:///etc/passwd"}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_source_url" {
		t.Errorf("source that is no web address: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("DELETE", "/api/v1/apps/sources/titanos", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("removing the built-in catalog: %d", res.StatusCode)
	}

	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, tc := range [][2]string{
		{"GET", "/api/v1/apps"},
		{"POST", "/api/v1/apps/refresh"},
		{"POST", "/api/v1/apps/sources"},
		{"GET", "/api/v1/apps/sources/titanos/apps/" + app.ID},
	} {
		if res, _ := ts.do(guest, tc[0], tc[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s %s as a plain user: %d, want 403", tc[0], tc[1], res.StatusCode)
		}
	}
}

func TestVMConsoleConfirmationAndDirectAdminShells(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	console := "/api/v1/vms/test/serial"
	dockerConsole := "/api/v1/docker/containers/aaaaaaaaaaaa1111/console"
	// Without the prompt switched off, consoles ask for the password again.
	var confirmed struct{ Confirmed bool }
	_, body0 := ts.request("GET", "/api/v1/session/confirmed", nil)
	if json.Unmarshal(body0, &confirmed); confirmed.Confirmed {
		t.Error("confirmed without entering the password")
	}
	if res, _ := ts.request("PUT", "/api/v1/terminal", map[string]bool{"confirm": false}); res.StatusCode != http.StatusOK {
		t.Errorf("switching the prompt off: %d", res.StatusCode)
	}
	_, body0 = ts.request("GET", "/api/v1/session/confirmed", nil)
	if json.Unmarshal(body0, &confirmed); !confirmed.Confirmed {
		t.Error("the prompt is off but the session is not confirmed")
	}
	ts.request("PUT", "/api/v1/terminal", map[string]bool{"confirm": true})
	// The NAS terminal accepts the existing authenticated admin session.
	if res, body := ts.request("GET", "/api/v1/terminal/shell", nil); res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusOK {
		t.Errorf("NAS shell before confirmation: %d %s", res.StatusCode, body)
	}
	ts.users.Create("terminal-guest", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "terminal-guest", Password: testPassword})
	if res, _ := ts.do(guest, "GET", "/api/v1/terminal/shell", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("guest shell: %d", res.StatusCode)
	}

	// Docker uses the existing login even when confirmation is enabled.
	if res, body := ts.request("GET", dockerConsole, nil); res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusOK {
		t.Errorf("Docker console before confirmation: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(guest, "GET", dockerConsole, nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("guest Docker console: %d", res.StatusCode)
	}
	if res, _ := ts.do(ts.newClient(), "GET", dockerConsole, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous Docker console: %d", res.StatusCode)
	}
	// VM consoles use the existing login too, without asking again.
	if res, body := ts.request("GET", console, nil); res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusOK {
		t.Errorf("console with the login alone: %d %s", res.StatusCode, body)
	}
	if _, body := ts.request("GET", "/api/v1/session/confirmed", nil); !strings.Contains(string(body), "false") {
		t.Errorf("confirmed = %s", body)
	}
	if res, body := ts.request("POST", "/api/v1/session/confirm", map[string]string{"password": "not the password"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Errorf("wrong password: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("POST", "/api/v1/session/confirm", map[string]string{"password": testPassword}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("confirm: %d", res.StatusCode)
	}
	if _, body := ts.request("GET", "/api/v1/session/confirmed", nil); !strings.Contains(string(body), "true") {
		t.Errorf("confirmed = %s", body)
	}
	// Confirmed now, but this plain request is no WebSocket: it gets past
	// the check and is refused by the upgrade instead.
	if res, body := ts.request("GET", console, nil); res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusOK {
		t.Errorf("console after confirmation: %d %s", res.StatusCode, body)
	}
	// The shell on the NAS is behind the same check.
	if res, body := ts.do(ts.newClient(), "GET", "/api/v1/terminal/shell", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("shell without login: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/terminal/shell", nil); res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusOK {
		t.Errorf("shell after confirmation: %d %s", res.StatusCode, body)
	}
	// Switched off in the settings, not even a confirmed session gets one.
	if res, body := ts.request("PUT", "/api/v1/terminal", map[string]bool{"enabled": false}); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"enabled":false`) {
		t.Errorf("switching terminals off: %d %s", res.StatusCode, body)
	}
	for _, path := range []string{console, dockerConsole, "/api/v1/terminal/shell"} {
		if res, body := ts.request("GET", path, nil); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "terminal_off" {
			t.Errorf("%s while terminals are off: %d %s", path, res.StatusCode, body)
		}
	}
	ts.request("PUT", "/api/v1/terminal", map[string]bool{"enabled": true})
	// Any session of an administrator opens consoles.
	other := ts.newClient()
	ts.do(other, "POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword})
	if res, body := ts.do(other, "GET", console, nil); res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusOK {
		t.Errorf("another session of the same account: %d %s", res.StatusCode, body)
	}
}

func TestSnapshotsAndHardwareControlsRequireAdministrator(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, route := range [][2]string{{"GET", "/api/v1/storage/volumes/data/snapshots"}, {"POST", "/api/v1/storage/volumes/data/snapshots"}, {"GET", "/api/v1/hardware/container-devices"}, {"GET", "/api/v1/hardware/nvidia"}, {"PUT", "/api/v1/hardware/nvidia/0000:01:00.0"}} {
		if res, _ := ts.do(guest, route[0], route[1], map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%v: %d", route, res.StatusCode)
		}
	}
}

func disabledMemory(store *config.Store) *system.MemoryPolicy {
	m := system.NewMemoryPolicy(store, func() system.Memory { return system.Memory{TotalBytes: 8 << 30, AvailableBytes: 4 << 30} })
	m.Disabled = true
	m.Load()
	return m
}

func TestTextEditorReadsAndSavesLargeJSONWithVersion(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if err := ts.store.WriteJSON("editor.json", map[string]string{"text": "original"}); err != nil {
		t.Fatal(err)
	}
	res, body := ts.request("GET", "/api/v1/files/text?root=stick&path=editor.json", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatal(res.StatusCode, string(body))
	}
	var original files.TextFile
	if err := json.Unmarshal(body, &original); err != nil {
		t.Fatal(err)
	}
	content := `{"text":"` + strings.Repeat("x", 80000) + `"}`
	payload := map[string]string{"root": "stick", "path": "editor.json", "content": content, "version": original.Version}
	if res, body = ts.request("PUT", "/api/v1/files/text", payload); res.StatusCode != http.StatusOK {
		t.Fatal(res.StatusCode, string(body))
	}
	if res, body = ts.request("PUT", "/api/v1/files/text", payload); res.StatusCode != http.StatusConflict || errorCode(t, body) != "text_conflict" {
		t.Fatal(res.StatusCode, string(body))
	}
	payload["version"] = ""
	if res, _ = ts.request("PUT", "/api/v1/files/text", payload); res.StatusCode != http.StatusConflict {
		t.Fatal("missing version", res.StatusCode)
	}
	for _, target := range []string{"/api/v1/files/text?root=stick&path=users", "/api/v1/files/text?root=volume/missing&path=x.json"} {
		if res, _ = ts.request("GET", target, nil); res.StatusCode != http.StatusNotFound {
			t.Fatal(target, res.StatusCode)
		}
	}
}

func TestTextEditorDoesNotExposeAdminRootsToUsers(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("POST", "/api/v1/users", map[string]any{"name": "reader", "password": testPassword, "role": "user"}); res.StatusCode != http.StatusCreated {
		t.Fatal(res.StatusCode, string(body))
	}
	client := ts.newClient()
	ts.do(client, "POST", "/api/v1/login", credentials{Name: "reader", Password: testPassword})
	if res, body := ts.do(client, "GET", "/api/v1/files/text?root=stick&path=editor.json", nil); res.StatusCode != http.StatusNotFound {
		t.Fatal(res.StatusCode, string(body))
	}
	if res, body := ts.do(client, "PUT", "/api/v1/files/text", map[string]string{"root": "stick", "path": "editor.json", "content": "{}", "version": "test"}); res.StatusCode != http.StatusNotFound {
		t.Fatal(res.StatusCode, string(body))
	}
}

func TestUpdateCheckOnlyAfterSuccessfulAdminLogin(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	// Explicit loopback source keeps the test independent of GitHub and keys
	// installed on the host. A missing key or failed local fetch is recorded.
	ts.update.Disabled = false
	if err := ts.update.SetSettings(update.Settings{Channel: update.ChannelStable, InstallTime: "03:30", Source: "http://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.users.Create("gast", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	ts.request("POST", "/api/v1/login", map[string]string{"name": "alex", "password": "wrong"})
	ts.request("POST", "/api/v1/login", map[string]string{"name": "gast", "password": testPassword})
	if ts.update.Status().LastCheck != nil {
		t.Fatal("failed or ordinary-user login triggered update check")
	}
	if res, body := ts.request("POST", "/api/v1/login", map[string]string{"name": "alex", "password": testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("login failed: %d %s", res.StatusCode, body)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if ts.update.Status().LastCheck != nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("successful owner login did not start update check")
}

func TestUSBBackupProgressRequiresAdministrator(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("GET", "/api/v1/backups/progress", nil); res.StatusCode != 200 || strings.TrimSpace(string(body)) != "{}" {
		t.Fatalf("owner: %d %s", res.StatusCode, body)
	}
	if _, err := ts.users.Create("guest", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	ts.request("POST", "/api/v1/login", map[string]string{"name": "guest", "password": testPassword})
	if res, _ := ts.request("GET", "/api/v1/backups/progress", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("user: %d", res.StatusCode)
	}
}

func TestFileInspectionHonorsRootPermissions(t *testing.T) {
	ts := newTestServer(t)
	request := map[string]any{"root": "stick", "paths": []string{"users"}}
	if res, _ := ts.request("POST", "/api/v1/files/inspect", request); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}
	ts.setup()
	if res, _ := ts.request("POST", "/api/v1/files/inspect", request); res.StatusCode == http.StatusOK {
		t.Fatal("inspection exposed account directory")
	}
	ts.users.Create("guest", testPassword, auth.RoleUser)
	ts.request("POST", "/api/v1/login", map[string]string{"name": "guest", "password": testPassword})
	if res, _ := ts.request("POST", "/api/v1/files/inspect", request); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest accessed stick: %d", res.StatusCode)
	}
}

func TestFilePropertiesAndTrashPreviewKeepRootAuthorization(t *testing.T) {
	ts := newTestServer(t)
	for _, endpoint := range []string{"/api/v1/files/properties?root=stick&path=users&checksum=sha256", "/api/v1/files/trash?root=stick&flat=1"} {
		if res, _ := ts.request("GET", endpoint, nil); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", endpoint, res.StatusCode)
		}
	}
	if res, _ := ts.request("POST", "/api/v1/files/jobs/missing/retry", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous retry: %d", res.StatusCode)
	}
	ts.setup()
	if res, _ := ts.request("GET", "/api/v1/files/properties?root=stick&path=users&checksum=sha256", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("secret properties: %d", res.StatusCode)
	}
	ts.users.Create("guest", testPassword, auth.RoleUser)
	ts.request("POST", "/api/v1/login", map[string]string{"name": "guest", "password": testPassword})
	for _, endpoint := range []string{"/api/v1/files/properties?root=stick&path=users", "/api/v1/files/trash?root=stick&flat=1"} {
		if res, _ := ts.request("GET", endpoint, nil); res.StatusCode != http.StatusNotFound {
			t.Fatalf("guest %s: %d", endpoint, res.StatusCode)
		}
	}
	if res, _ := ts.request("POST", "/api/v1/files/trash/preview", map[string]any{"root": "stick", "paths": []string{"owner/config.json"}}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("guest restore preview: %d", res.StatusCode)
	}
}

func TestAdvancedFileToolsRequireSessionAndRootAccess(t *testing.T) {
	ts := newTestServer(t)
	paths := []string{"duplicates", "usage", "search", "compare", "rename-preview", "rename-batch", "archive", "extract"}
	request := map[string]any{"root": "stick", "path": "users", "toRoot": "stick", "toDir": "", "name": "out", "names": []string{"users"}, "digits": 1}
	for _, action := range paths {
		if res, _ := ts.request("POST", "/api/v1/files/"+action, request); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", action, res.StatusCode)
		}
	}
	ts.setup()
	ts.users.Create("guest", testPassword, auth.RoleUser)
	ts.request("POST", "/api/v1/login", map[string]string{"name": "guest", "password": testPassword})
	for _, action := range paths {
		if res, _ := ts.request("POST", "/api/v1/files/"+action, request); res.StatusCode != http.StatusNotFound {
			t.Fatalf("guest %s: %d", action, res.StatusCode)
		}
	}
	if res, _ := ts.request("POST", "/api/v1/files/undo/unknown", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown undo: %d", res.StatusCode)
	}
}

func TestFileJobControlsRequireOwnJob(t *testing.T) {
	ts := newTestServer(t)
	req := map[string]any{"paused": false, "priority": 1, "rate": 1048576}
	if res, _ := ts.request("POST", "/api/v1/files/jobs/unknown/control", req); res.StatusCode != http.StatusUnauthorized {
		t.Fatal(res.StatusCode)
	}
	ts.setup()
	if res, _ := ts.request("POST", "/api/v1/files/jobs/unknown/control", req); res.StatusCode != http.StatusNotFound {
		t.Fatal(res.StatusCode)
	}
}

func TestZipDownloadNameAndEmptyDirectory(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if err := os.Mkdir(filepath.Join(ts.store.Dir(), "Leer"), 0700); err != nil {
		t.Fatal(err)
	}
	q := url.Values{"root": {"stick"}, "path": {"Leer"}, "name": {"Meine-Sicherung.ZIP"}}
	res, body := ts.request("GET", "/api/v1/files/zip?"+q.Encode(), nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	_, params, err := mime.ParseMediaType(res.Header.Get("Content-Disposition"))
	if err != nil || params["filename"] != "Meine-Sicherung.ZIP" {
		t.Fatalf("%v %v", params, err)
	}
	z, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(z.File) != 1 || z.File[0].Name != "Leer/" {
		t.Fatalf("%v %v", z, err)
	}
	for _, name := range []string{"../other.zip", "bad\\name.zip", "bad\nname.zip", "bad\x00name.zip", ".zip", "without-extension"} {
		q.Set("name", name)
		if res, _ := ts.request("GET", "/api/v1/files/zip?"+q.Encode(), nil); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%q: %d", name, res.StatusCode)
		}
	}
}

func TestPreviousTextAccessAndRestoreVersions(t *testing.T) {
	ts := newTestServer(t)
	endpoint := "/api/v1/files/text/previous?root=stick&path=note.txt"
	if res, _ := ts.request("GET", endpoint, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatal(res.StatusCode)
	}
	ts.setup()
	os.WriteFile(filepath.Join(ts.store.Dir(), "note.txt"), []byte("current"), 0600)
	os.WriteFile(filepath.Join(ts.store.Dir(), "note.txt.prev"), []byte("previous"), 0600)
	res, body := ts.request("GET", endpoint, nil)
	if res.StatusCode != http.StatusOK || !bytes.Contains(body, []byte("previous")) {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/files/text", map[string]string{"root": "stick", "path": "note.txt", "content": "previous", "version": "stale"}); res.StatusCode != http.StatusConflict {
		t.Fatal(res.StatusCode)
	}
	ts.users.Create("guest", testPassword, auth.RoleUser)
	ts.request("POST", "/api/v1/login", map[string]string{"name": "guest", "password": testPassword})
	if res, _ := ts.request("GET", endpoint, nil); res.StatusCode != http.StatusNotFound {
		t.Fatal(res.StatusCode)
	}
}

func TestFileMediaStreamingIsAuthorizedTypedAndSupportsRanges(t *testing.T) {
	ts := newTestServer(t)
	endpoint := "/api/v1/files/download?root=stick&path=Clip.MP4&media=1"
	if res, _ := ts.request("GET", endpoint, nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatal(res.StatusCode)
	}
	ts.setup()
	if err := os.WriteFile(filepath.Join(ts.store.Dir(), "Clip.MP4"), []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", ts.url+endpoint, nil)
	req.Header.Set("Range", "bytes=2-5")
	res, err := ts.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusPartialContent || string(body) != "2345" {
		t.Fatalf("%d %q %v", res.StatusCode, body, err)
	}
	if res.Header.Get("Content-Type") != "video/mp4" || res.Header.Get("X-Content-Type-Options") != "nosniff" || res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal(res.Header)
	}
	if !strings.HasPrefix(res.Header.Get("Content-Disposition"), "inline;") {
		t.Fatal(res.Header)
	}
	if err := os.WriteFile(filepath.Join(ts.store.Dir(), "unsafe.html"), []byte("<script>alert(1)</script>"), 0600); err != nil {
		t.Fatal(err)
	}
	if res, _ := ts.request("GET", "/api/v1/files/download?root=stick&path=unsafe.html&media=1", nil); res.StatusCode != http.StatusBadRequest {
		t.Fatal(res.StatusCode)
	}
	if res, _ := ts.request("GET", "/api/v1/files/download?root=stick&path=unsafe.html", nil); !strings.HasPrefix(res.Header.Get("Content-Disposition"), "attachment;") {
		t.Fatal(res.Header)
	}
}

// withShare mounts a volume "tank" in a temporary folder and creates the
// share Docs on it, readable and writable for the owner alex.
func (ts *testServer) withShare() string {
	ts.t.Helper()
	mnt := ts.t.TempDir()
	if err := ts.store.WriteJSON("storage/volumes.json", []storage.VolumeConfig{{Name: "tank", Level: "raid1", Filesystem: "ext4", ArrayUUID: "a", FilesystemUUID: "b"}}); err != nil {
		ts.t.Fatal(err)
	}
	if err := ts.disks.Load(); err != nil {
		ts.t.Fatal(err)
	}
	ts.disks.UseMounts(mnt, func() map[string]bool { return map[string]bool{mnt + "/tank": true} })
	ts.shares.UseMountRoot(mnt)
	dir := filepath.Join(mnt, "tank", "Docs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		ts.t.Fatal(err)
	}
	if res, body := ts.request("POST", "/api/v1/shares", map[string]any{"name": "Docs", "volume": "tank", "smb": true}); res.StatusCode != http.StatusCreated {
		ts.t.Fatalf("share: %d %s", res.StatusCode, body)
	}
	return dir
}

func TestTransferRemovalDoesNotRequireIDConfirmation(t *testing.T) {
	ts := newTestServer(t)
	if res, _ := ts.request("DELETE", "/api/v1/files/transfers", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatal(res.StatusCode)
	}
	ts.setup()
	base := t.TempDir()
	src := files.Root{ID: "test/source", Dir: filepath.Join(base, "source"), Writable: true}
	dst := files.Root{ID: "test/target", Dir: filepath.Join(base, "target"), Writable: true}
	os.Mkdir(src.Dir, 0700)
	os.Mkdir(dst.Dir, 0700)
	os.WriteFile(filepath.Join(src.Dir, "a.txt"), []byte("keep"), 0600)
	create := func(user string) string {
		j, err := ts.server.opts.Files.StartCopy(src, []string{"a.txt"}, dst, ".", false, files.ConflictRename, user, os.Getuid())
		if err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			for _, view := range ts.server.opts.Files.Jobs(user) {
				if view.ID == j.ID && view.State == files.JobDone {
					return j.ID
				}
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatal("copy did not finish")
		return ""
	}
	own := create("alex")
	foreign := create("sam")
	if res, _ := ts.request("DELETE", "/api/v1/files/transfers/"+foreign, nil); res.StatusCode != http.StatusNotFound {
		t.Fatal("foreign deletion", res.StatusCode)
	}
	if res, body := ts.request("DELETE", "/api/v1/files/transfers/"+own, nil); res.StatusCode != http.StatusNoContent {
		t.Fatal(res.StatusCode, string(body))
	}
	create("alex")
	create("alex")
	if res, body := ts.request("DELETE", "/api/v1/files/transfers", nil); res.StatusCode != http.StatusNoContent {
		t.Fatal(res.StatusCode, string(body))
	}
	if jobs := ts.server.opts.Files.Jobs("alex"); len(jobs) != 0 {
		t.Fatal(jobs)
	}
	if jobs := ts.server.opts.Files.Jobs("sam"); len(jobs) != 1 {
		t.Fatal("foreign jobs removed")
	}
	if b, err := os.ReadFile(filepath.Join(dst.Dir, "a.txt")); err != nil || string(b) != "keep" {
		t.Fatal("copied data changed", err)
	}
}

// "Only after the rebuild" waits, can be cancelled, and runs once the
// volumes are quiet.
func TestPowerAfterRebuild(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	var busy atomic.Bool
	busy.Store(true)
	ts.server.rebuilding = busy.Load
	defer func(old time.Duration) { powerPollInterval = old }(powerPollInterval)
	powerPollInterval = 10 * time.Millisecond

	status := func() map[string]any {
		t.Helper()
		var got map[string]any
		res, body := ts.request("GET", "/api/v1/system/power", nil)
		if res.StatusCode != http.StatusOK || json.Unmarshal([]byte(body), &got) != nil {
			t.Fatalf("status: %d %s", res.StatusCode, body)
		}
		return got
	}
	if res, _ := ts.request("POST", "/api/v1/system/power", map[string]any{"action": "poweroff", "afterRebuild": true}); res.StatusCode != http.StatusAccepted {
		t.Fatalf("waiting shutdown: %d", res.StatusCode)
	}
	if got := status(); got["pending"] == nil || got["rebuilding"] != true {
		t.Fatalf("status while waiting: %v", got)
	}
	time.Sleep(50 * time.Millisecond)
	select {
	case got := <-ts.power.actions:
		t.Fatalf("%s during the rebuild", got)
	default:
	}
	// Cancelled: nothing happens even when the rebuild ends.
	ts.request("DELETE", "/api/v1/system/power", nil)
	busy.Store(false)
	time.Sleep(50 * time.Millisecond)
	select {
	case got := <-ts.power.actions:
		t.Fatalf("cancelled %s still ran", got)
	default:
	}
	// Waiting again, then the rebuild ends.
	busy.Store(true)
	ts.request("POST", "/api/v1/system/power", map[string]any{"action": "reboot", "afterRebuild": true})
	busy.Store(false)
	select {
	case got := <-ts.power.actions:
		if got != "reboot" {
			t.Fatalf("ran %s", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the restart did not follow the rebuild")
	}
	if got := status(); got["pending"] != nil {
		t.Fatalf("still pending: %v", got)
	}
}
