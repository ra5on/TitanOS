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
	mounts  []map[string]any
	backups []map[string]any
	refuse  string // a mount is refused with this message
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
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var msg map[string]any
		json.Unmarshal(data, &msg)
		f.mu.Lock()
		switch {
		case msg["type"] == "backup/info":
			send(map[string]any{"id": msg["id"], "type": "result", "success": true, "result": map[string]any{"backups": f.backups}})
		case msg["type"] == "supervisor/api" && msg["method"] == "post" && f.refuse != "":
			send(map[string]any{"id": msg["id"], "type": "result", "success": false, "error": map[string]string{"code": "home_assistant_error", "message": f.refuse}})
		case msg["type"] == "supervisor/api" && msg["method"] == "post":
			f.mounts = append(f.mounts, msg["data"].(map[string]any))
			send(map[string]any{"id": msg["id"], "type": "result", "success": true, "result": map[string]any{}})
		default:
			send(map[string]any{"id": msg["id"], "type": "result", "success": false, "error": map[string]string{"code": "unknown_command", "message": "?"}})
		}
		f.mu.Unlock()
	}
}

func TestStorageForHomeAssistant(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	fake := &fakeSupervisor{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	setup := map[string]any{"volume": "tank", "share": "homeassistant", "account": "homeassistant", "usage": "backup", "register": true}
	// Entering it in Home Assistant needs the connection.
	if res, body := ts.request("POST", "/api/v1/homeassistant/storage", setup); res.StatusCode != http.StatusConflict || errorCode(t, body) != "ha_not_connected" {
		t.Fatalf("without connection: %d %s", res.StatusCode, body)
	}
	if err := ts.home.SetSettings(homeassistant.Settings{Enabled: true, URL: srv.URL, Token: "token"}); err != nil {
		t.Fatal(err)
	}
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

	res, body := ts.request("POST", "/api/v1/homeassistant/storage", setup)
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", res.StatusCode, body)
	}
	var made struct {
		Setup         haStorage
		Password      string
		Registered    bool
		RegisterError string
	}
	json.Unmarshal(body, &made)
	if !made.Registered || len(made.Password) < 20 || made.Setup.Mount != "titanos_homeassistant" || made.Setup.MaxAgeDays != 3 {
		t.Fatalf("setup %s", body)
	}
	// The share is for this account only, and Home Assistant got its values.
	share := findShare(ts, "homeassistant")
	if share.Access["homeassistant"] != "rw" || len(share.Access) != 1 || share.Guest || !share.SMB {
		t.Fatalf("share %+v", share)
	}
	fake.mu.Lock()
	mount := fake.mounts[0]
	fake.mu.Unlock()
	if mount["type"] != "cifs" || mount["share"] != "homeassistant" || mount["username"] != "homeassistant" || mount["password"] != made.Password || mount["usage"] != "backup" || mount["server"] == "" {
		t.Fatalf("mount %v", mount)
	}
	// The password is shown once: neither the status nor /config has it.
	_, status := ts.request("GET", "/api/v1/homeassistant/storage", nil)
	stored, _ := ts.store.Read(haStorageFile)
	if strings.Contains(string(status), made.Password) || strings.Contains(string(stored), made.Password) {
		t.Fatal("password kept")
	}
	if res, body := ts.request("POST", "/api/v1/homeassistant/storage", setup); res.StatusCode != http.StatusConflict || errorCode(t, body) != "ha_storage_exists" {
		t.Fatalf("second setup: %d %s", res.StatusCode, body)
	}

	// Entering it again gives a new password; a refusal comes back as text.
	fake.mu.Lock()
	fake.refuse = "Could not mount titanos_homeassistant: Permission denied"
	fake.mu.Unlock()
	res, body = ts.request("POST", "/api/v1/homeassistant/storage/register", nil)
	var again struct {
		Password      string
		Registered    bool
		RegisterError string
	}
	json.Unmarshal(body, &again)
	if res.StatusCode != http.StatusOK || again.Registered || !strings.Contains(again.RegisterError, "Permission denied") || again.Password == made.Password {
		t.Fatalf("register again: %d %s", res.StatusCode, body)
	}

	// Backups: none yet is fine at first, overdue after the days set.
	now := time.Now()
	setupNow, _ := ts.server.haStorage()
	ts.server.checkHABackups(t.Context(), setupNow, now)
	if ts.server.haStore.backup.Overdue {
		t.Fatal("overdue right after the setup")
	}
	ts.server.checkHABackups(t.Context(), setupNow, now.Add(4*24*time.Hour))
	if !ts.server.haStore.backup.Overdue || ts.server.haStore.backup.Error != "" {
		t.Fatalf("not overdue: %+v", ts.server.haStore.backup)
	}
	if !hasNotice(ts, "backup_ha_overdue") {
		t.Fatal("no notice")
	}
	fake.mu.Lock()
	fake.backups = []map[string]any{{"backup_id": "a", "name": "Täglich", "date": now.Add(4*24*time.Hour - time.Hour), "agents": map[string]any{"hassio.titanos_homeassistant": map[string]any{}}}}
	fake.mu.Unlock()
	ts.server.checkHABackups(t.Context(), setupNow, now.Add(4*24*time.Hour))
	if b := ts.server.haStore.backup; b.Overdue || b.Name != "Täglich" {
		t.Fatalf("after a backup: %+v", b)
	}

	// Watching can be switched off and the setup forgotten; share stays.
	if res, body := ts.request("PUT", "/api/v1/homeassistant/storage", map[string]any{"maxAgeDays": 0}); res.StatusCode != http.StatusOK {
		t.Fatalf("off: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/homeassistant/storage", map[string]any{"maxAgeDays": 400}); res.StatusCode != http.StatusBadRequest {
		t.Fatal("400 days accepted")
	}
	if res, _ := ts.request("DELETE", "/api/v1/homeassistant/storage", nil); res.StatusCode != http.StatusOK {
		t.Fatal("forget")
	}
	if findShare(ts, "homeassistant").Name == "" {
		t.Fatal("share removed with the setup")
	}
	if _, err := ts.store.Read(haStorageFile); err == nil {
		t.Fatal("setup still stored")
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

// A share that exists, and a folder in it, read only: the account joins
// those who may open the share, and Home Assistant gets share/folder.
func TestStorageForHomeAssistantInAnExistingFolder(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	docs := ts.withShare()
	os.MkdirAll(filepath.Join(docs, "Musik", "Rock"), 0o755)
	os.WriteFile(filepath.Join(docs, "notiz.txt"), []byte("x"), 0o644)
	os.Symlink("/etc", filepath.Join(docs, "link"))
	fake := &fakeSupervisor{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	ts.home.SetSettings(homeassistant.Settings{Enabled: true, URL: srv.URL, Token: "token"})

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

	res, body := ts.request("POST", "/api/v1/homeassistant/storage", map[string]any{
		"share": "Docs", "existing": true, "folder": "/Musik/Rock/", "account": "hass", "usage": "media", "readOnly": true, "register": true,
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("setup: %d %s", res.StatusCode, body)
	}
	var made struct {
		Setup      haStorage
		Registered bool
	}
	json.Unmarshal(body, &made)
	if !made.Registered || made.Setup.Folder != "Musik/Rock" || made.Setup.Mount != "titanos_docs_musik_rock" || !made.Setup.Existing || !made.Setup.ReadOnly {
		t.Fatalf("setup %s", body)
	}
	share := findShare(ts, "Docs")
	if share.Access["alex"] != "rw" || share.Access["hass"] != "ro" {
		t.Fatalf("access %v", share.Access)
	}
	fake.mu.Lock()
	mount := fake.mounts[0]
	fake.mu.Unlock()
	if mount["share"] != "Docs/Musik/Rock" || mount["read_only"] != true || mount["usage"] != "media" {
		t.Fatalf("mount %v", mount)
	}
}
