package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ra5on/TitanOS/backend/internal/homeassistant"
	"github.com/ra5on/TitanOS/backend/internal/shares"
)

// fakeSupervisor is Home Assistant OS as far as storage goes: network
// storage through supervisor/api and the backups through backup/info.
type fakeSupervisor struct {
	mu      sync.Mutex
	mounts  map[string]map[string]any
	backups []map[string]any
	refuse  string // adding or changing is refused with this message
}

func (f *fakeSupervisor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	ctx := r.Context()
	send := func(v any) { b, _ := json.Marshal(v); conn.Write(ctx, websocket.MessageText, b) }
	send(map[string]string{"type": "auth_required"})
	conn.Read(ctx)
	send(map[string]string{"type": "auth_ok"})
	ok := func(id any, result any) {
		send(map[string]any{"id": id, "type": "result", "success": true, "result": result})
	}
	fail := func(id any, code, message string) {
		send(map[string]any{"id": id, "type": "result", "success": false, "error": map[string]string{"code": code, "message": message}})
	}
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg map[string]any
		json.Unmarshal(data, &msg)
		f.mu.Lock()
		if f.mounts == nil {
			f.mounts = map[string]map[string]any{}
		}
		method, _ := msg["method"].(string)
		endpoint, _ := msg["endpoint"].(string)
		name := strings.TrimPrefix(endpoint, "/mounts/")
		switch {
		case msg["type"] == "backup/info":
			ok(msg["id"], map[string]any{"backups": f.backups})
		case msg["type"] != "supervisor/api":
			fail(msg["id"], "unknown_command", "?")
		case method == "get" && endpoint == "/mounts":
			list := []map[string]any{}
			for _, m := range f.mounts {
				list = append(list, m)
			}
			ok(msg["id"], map[string]any{"mounts": list})
		case (method == "post" || method == "put") && f.refuse != "":
			fail(msg["id"], "home_assistant_error", f.refuse)
		case method == "post":
			data := msg["data"].(map[string]any)
			f.mounts[data["name"].(string)] = data
			ok(msg["id"], map[string]any{})
		case method == "put" && f.mounts[name] != nil:
			f.mounts[name] = msg["data"].(map[string]any)
			ok(msg["id"], map[string]any{})
		case method == "delete" && f.mounts[name] != nil:
			delete(f.mounts, name)
			ok(msg["id"], map[string]any{})
		default:
			fail(msg["id"], "home_assistant_error", "no such mount")
		}
		f.mu.Unlock()
	}
}

func (f *fakeSupervisor) mount(name string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.mounts[name]
}

type haAnswer struct {
	Setup    haStorage
	Entry    haEntry
	Password string
	Results  []haResult
}

func haConnected(t *testing.T, ts *testServer) *fakeSupervisor {
	t.Helper()
	fake := &fakeSupervisor{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	if err := ts.home.SetSettings(homeassistant.Settings{Enabled: true, URL: srv.URL, Token: "token"}); err != nil {
		t.Fatal(err)
	}
	return fake
}

func TestStorageForHomeAssistant(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	// Lists are lists, also without anything in them (the page reads them).
	if _, body := ts.request("GET", "/api/v1/homeassistant/storage", nil); !strings.Contains(string(body), `"volumes":[]`) || !strings.Contains(string(body), `"shares":[]`) || !strings.Contains(string(body), `"entries":[]`) {
		t.Fatalf("empty lists: %s", body)
	}
	docs := ts.withShare()

	first := map[string]any{"volume": "tank", "share": "homeassistant", "account": "homeassistant", "usage": "backup", "register": true}
	// Entering it in Home Assistant needs the connection.
	if res, body := ts.request("POST", "/api/v1/homeassistant/storage", first); res.StatusCode != http.StatusConflict || errorCode(t, body) != "ha_not_connected" {
		t.Fatalf("without connection: %d %s", res.StatusCode, body)
	}
	fake := haConnected(t, ts)
	// An account of that name exists: nothing is touched.
	if res, body := ts.request("POST", "/api/v1/homeassistant/storage", map[string]any{"volume": "tank", "share": "ha", "account": "alex", "usage": "backup"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "ha_storage_account_exists" {
		t.Fatalf("existing account: %d %s", res.StatusCode, body)
	}
	// A share that cannot be made leaves no account behind.
	if res, _ := ts.request("POST", "/api/v1/homeassistant/storage", map[string]any{"volume": "nowhere", "share": "ha", "account": "hass", "usage": "backup"}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad volume: %d", res.StatusCode)
	}
	if _, ok := ts.users.Get("hass"); ok {
		t.Fatal("account left behind")
	}

	res, body := ts.request("POST", "/api/v1/homeassistant/storage", first)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("first: %d %s", res.StatusCode, body)
	}
	var made haAnswer
	json.Unmarshal(body, &made)
	if len(made.Results) != 1 || !made.Results[0].OK || len(made.Password) < 20 || made.Entry.Mount != "titanos_homeassistant" || made.Entry.MaxAgeDays != 3 {
		t.Fatalf("first %s", body)
	}
	share := findShare(ts, "homeassistant")
	if share.Access["homeassistant"] != "rw" || len(share.Access) != 1 || share.Guest || !share.SMB {
		t.Fatalf("share %+v", share)
	}
	if m := fake.mount("titanos_homeassistant"); m["type"] != "cifs" || m["share"] != "homeassistant" || m["username"] != "homeassistant" || m["password"] != made.Password || m["usage"] != "backup" || m["server"] == "" {
		t.Fatalf("mount %v", m)
	}
	// The password is shown once: neither the status nor /config has it.
	_, status := ts.request("GET", "/api/v1/homeassistant/storage", nil)
	stored, _ := ts.store.Read(haStorageFile)
	if strings.Contains(string(status), made.Password) || strings.Contains(string(stored), made.Password) {
		t.Fatal("password kept")
	}
	if res, body := ts.request("POST", "/api/v1/homeassistant/storage", first); res.StatusCode != http.StatusConflict || errorCode(t, body) != "ha_storage_entry_exists" {
		t.Fatalf("same share twice: %d %s", res.StatusCode, body)
	}

	// A second share for media, same account. Entered directly, the account
	// gets a new password and the first entry in Home Assistant gets it too.
	os.MkdirAll(filepath.Join(docs, "Musik"), 0o755)
	res, body = ts.request("POST", "/api/v1/homeassistant/storage", map[string]any{"share": "Docs", "existing": true, "folder": "Musik", "account": "ignored", "usage": "media", "readOnly": true, "register": true})
	var second haAnswer
	json.Unmarshal(body, &second)
	if res.StatusCode != http.StatusCreated || second.Setup.Account != "homeassistant" || len(second.Setup.Entries) != 2 || second.Password == "" || second.Password == made.Password || len(second.Results) != 2 {
		t.Fatalf("second: %d %s", res.StatusCode, body)
	}
	if _, ok := ts.users.Get("ignored"); ok {
		t.Fatal("a second account was made")
	}
	if m := fake.mount("titanos_homeassistant"); m["password"] != second.Password {
		t.Fatalf("first entry kept the old password: %v", m)
	}
	if m := fake.mount("titanos_docs_musik"); m["share"] != "Docs/Musik" || m["read_only"] != true || m["password"] != second.Password {
		t.Fatalf("second mount %v", m)
	}
	if share := findShare(ts, "Docs"); share.Access["alex"] != "rw" || share.Access["homeassistant"] != "ro" {
		t.Fatalf("access %v", share.Access)
	}

	// Entering everything again: a new password everywhere; a refusal is
	// reported per entry.
	fake.mu.Lock()
	fake.refuse = "Could not mount: Permission denied"
	fake.mu.Unlock()
	res, body = ts.request("POST", "/api/v1/homeassistant/storage/register", nil)
	var again haAnswer
	json.Unmarshal(body, &again)
	if res.StatusCode != http.StatusOK || len(again.Results) != 2 || again.Results[0].OK || !strings.Contains(again.Results[0].Error, "Permission denied") || again.Password == second.Password {
		t.Fatalf("register again: %d %s", res.StatusCode, body)
	}
	fake.mu.Lock()
	fake.refuse = ""
	fake.mu.Unlock()

	// Backups: none yet is fine at first, overdue after the days set.
	now := time.Now()
	setup, _ := ts.server.haStorage()
	ts.server.checkHABackups(t.Context(), setup, now, false)
	backup := func() haBackupState { return *ts.server.haStore.backups["titanos_homeassistant"] }
	if backup().Overdue || ts.server.haStore.backups["titanos_docs_musik"] != nil {
		t.Fatalf("right after the setup: %+v", ts.server.haStore.backups)
	}
	ts.server.checkHABackups(t.Context(), setup, now.Add(4*24*time.Hour), false)
	if !backup().Overdue || !hasNotice(ts, "backup_ha_overdue") {
		t.Fatalf("not overdue: %+v", backup())
	}
	fake.mu.Lock()
	fake.backups = []map[string]any{{"backup_id": "a", "name": "Täglich", "date": now.Add(4*24*time.Hour - time.Hour), "agents": map[string]any{"hassio.titanos_homeassistant": map[string]any{}}}}
	fake.mu.Unlock()
	ts.server.checkHABackups(t.Context(), setup, now.Add(4*24*time.Hour), false)
	if b := backup(); b.Overdue || b.Name != "Täglich" {
		t.Fatalf("after a backup: %+v", b)
	}
	if res, body := ts.request("PUT", "/api/v1/homeassistant/storage/titanos_homeassistant", map[string]any{"maxAgeDays": 7}); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"maxAgeDays":7`) {
		t.Fatalf("days: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/homeassistant/storage/titanos_docs_musik", map[string]any{"maxAgeDays": 3}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("watching media accepted")
	}

	// Removing the media entry: out of Home Assistant, no more access to
	// Docs for the account; the folder and everyone else's access stay.
	if res, body := ts.request("DELETE", "/api/v1/homeassistant/storage/titanos_docs_musik", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"removedError":""`) {
		t.Fatalf("remove: %d %s", res.StatusCode, body)
	}
	if fake.mount("titanos_docs_musik") != nil {
		t.Fatal("still in Home Assistant")
	}
	if share := findShare(ts, "Docs"); share.Access["homeassistant"] != "" || share.Access["alex"] != "rw" {
		t.Fatalf("access after removing: %v", share.Access)
	}
	if _, err := os.Stat(filepath.Join(docs, "Musik")); err != nil {
		t.Fatal("folder gone")
	}
	// The last entry of a share only the account could open: the person
	// removing it gets the access, so the backups stay reachable.
	if res, _ := ts.request("DELETE", "/api/v1/homeassistant/storage/titanos_homeassistant", nil); res.StatusCode != http.StatusOK {
		t.Fatal("remove last")
	}
	if share := findShare(ts, "homeassistant"); share.Name == "" || share.Access["alex"] != "rw" || share.Access["homeassistant"] != "" {
		t.Fatalf("share after the last entry: %+v", share)
	}
	// The account stays and is used again by the next entry.
	if res, body := ts.request("POST", "/api/v1/homeassistant/storage", map[string]any{"share": "homeassistant", "existing": true, "usage": "share"}); res.StatusCode != http.StatusCreated || !strings.Contains(string(body), `"account":"homeassistant"`) || strings.Contains(string(body), `"password"`) {
		t.Fatalf("again without entering: %d %s", res.StatusCode, body)
	}
}

// A share that exists, and a folder in it: nothing out of the share, no
// links, no hidden folders, and backups always write.
func TestStorageForHomeAssistantChecksTheFolder(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	docs := ts.withShare()
	os.MkdirAll(filepath.Join(docs, "Musik", "Rock"), 0o755)
	os.WriteFile(filepath.Join(docs, "notiz.txt"), []byte("x"), 0o644)
	os.Symlink("/etc", filepath.Join(docs, "link"))
	haConnected(t, ts)
	for name, req := range map[string]map[string]any{
		"out of the share":      {"share": "Docs", "existing": true, "folder": "../tank", "usage": "media"},
		"missing folder":        {"share": "Docs", "existing": true, "folder": "Videos", "usage": "media"},
		"a file":                {"share": "Docs", "existing": true, "folder": "notiz.txt", "usage": "media"},
		"a link":                {"share": "Docs", "existing": true, "folder": "link", "usage": "media"},
		"hidden":                {"share": "Docs", "existing": true, "folder": ".recycle", "usage": "media"},
		"folder of a new share": {"share": "neu", "volume": "tank", "folder": "Musik", "usage": "media"},
		"backups read only":     {"share": "Docs", "existing": true, "usage": "backup", "readOnly": true},
		"no such share":         {"share": "Gibtsnicht", "existing": true, "usage": "media"},
	} {
		req["account"] = "hass"
		if res, body := ts.request("POST", "/api/v1/homeassistant/storage", req); res.StatusCode < 400 {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
		if _, ok := ts.users.Get("hass"); ok {
			t.Fatalf("%s: account left behind", name)
		}
	}
	res, body := ts.request("POST", "/api/v1/homeassistant/storage", map[string]any{"share": "Docs", "existing": true, "folder": "/Musik/Rock/", "account": "hass", "usage": "media"})
	var made haAnswer
	json.Unmarshal(body, &made)
	if res.StatusCode != http.StatusCreated || made.Entry.Folder != "Musik/Rock" || made.Entry.Mount != "titanos_docs_musik_rock" {
		t.Fatalf("setup: %d %s", res.StatusCode, body)
	}
}

func findShare(ts *testServer, name string) shares.Share {
	for _, share := range ts.shares.List() {
		if share.Name == name {
			return share
		}
	}
	return shares.Share{}
}

func hasNotice(ts *testServer, kind string) bool {
	_, body := ts.request("GET", "/api/v1/notifications", nil)
	return strings.Contains(string(body), kind)
}

func TestHAQuotaTakesDecimalBytesFirst(t *testing.T) {
	if got := haQuota(5_000_000_000, 0); got != 5_000_000_000 {
		t.Fatalf("bytes: %d", got)
	}
	if got := haQuota(0, 2); got != 2<<30 {
		t.Fatalf("older clients in GiB: %d", got)
	}
	if got := haQuota(3_000_000_000, 7); got != 3_000_000_000 {
		t.Fatalf("bytes win: %d", got)
	}
	if got := haQuota(0, 0); got != 0 {
		t.Fatalf("no limit: %d", got)
	}
}
