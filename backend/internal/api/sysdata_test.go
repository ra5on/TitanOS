package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/sysdata"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

func TestSystemDataIsForAdministrators(t *testing.T) {
	var data *sysdata.Manager
	ts := newTestServerWith(t, func(o *Options) {
		data = sysdata.New(o.Store)
		data.Mounted = o.Storage.Mounted
		o.SysData = data
		o.SystemData = &volumedata.Home{MountRoot: t.TempDir(), Mounted: o.Storage.Mounted, Choose: func() string { return "" }, Override: data.Override}
		o.Activity = activity.New(o.Store)
	})
	if res, _ := ts.request("GET", "/api/v1/system/data", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}
	ts.setup()
	res, body := ts.request("GET", "/api/v1/system/data", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("state: %d %s", res.StatusCode, body)
	}
	var state sysDataState
	if err := json.Unmarshal(body, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Functions) != len(sysdata.Functions) || state.Volume != "" || state.Resolved != "" {
		t.Fatalf("state: %+v", state)
	}
	for i, f := range state.Functions {
		if f.ID != sysdata.Functions[i] || f.Choice != "" || f.Volume != "" || f.Source != "automatic" || f.SizeBytes != nil {
			t.Errorf("function %d: %+v", i, f)
		}
	}
	// Nothing is mounted here: a volume cannot be chosen, and nothing is stored.
	for _, bad := range []map[string]any{
		{"volume": "nowhere"},
		{"functions": map[string]string{"logs": "nowhere"}},
		{"functions": map[string]string{"activity": "nowhere"}},
	} {
		if res, body := ts.request("PUT", "/api/v1/system/data", bad); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_volume" {
			t.Errorf("%v: %d %s", bad, res.StatusCode, body)
		}
	}
	if res, body := ts.request("PUT", "/api/v1/system/data", map[string]any{"functions": map[string]string{"shadow": ""}}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown function: %d %s", res.StatusCode, body)
	}
	if s := data.Settings(); s.Volume != "" || len(s.Overrides) != 0 {
		t.Fatalf("a refused request was stored: %+v", s)
	}
	// Choosing "follow" where nothing was chosen changes nothing.
	changes := ts.store.Changes()
	if res, body := ts.request("PUT", "/api/v1/system/data", map[string]any{"volume": "", "functions": map[string]string{"logs": "", "activity": ""}}); res.StatusCode != http.StatusOK {
		t.Fatalf("no change: %d %s", res.StatusCode, body)
	}
	if ts.store.Changes() != changes {
		t.Fatal("/config written without a change")
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, method := range []string{"GET", "PUT"} {
		if res, _ := ts.do(guest, method, "/api/v1/system/data", map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s as a plain user: %d, want 403", method, res.StatusCode)
		}
	}
}

// A function whose volume is changed while the old one is not mounted:
// the old volume is still noted (from the stored choice, not from what is
// mounted), so its data are moved once it is back.
func TestSystemDataNotesThePreviousVolumeAlsoWhenItIsAway(t *testing.T) {
	store := config.Open(t.TempDir())
	if err := store.WriteJSON("storage/volumes.json", []map[string]any{
		{"name": "alt", "level": "raid1", "filesystem": "ext4", "arrayUUID": "a", "filesystemUUID": "a"},
		{"name": "neu", "level": "raid1", "filesystem": "ext4", "arrayUUID": "b", "filesystemUUID": "b"},
	}); err != nil {
		t.Fatal(err)
	}
	disks := loadedStorage(t, store)
	data := sysdata.New(store)
	data.Mounted = disks.Mounted
	if err := data.Update(func(s *sysdata.Settings) { s.Volume = "alt"; s.Overrides["smart"] = "alt" }); err != nil {
		t.Fatal(err)
	}
	home := &volumedata.Home{MountRoot: t.TempDir(), Mounted: disks.Mounted, Choose: data.Volume, Override: data.Override}
	area := sysDataArea{data: data, home: home, storage: disks}
	state := area.state(httptest.NewRequest("GET", "/api/v1/system/data", nil))
	for _, f := range state.Functions {
		if f.ID == sysdata.Config || f.ID == sysdata.Activity || f.ID == sysdata.Monitoring {
			continue // their managers are not part of this test
		}
		if f.Volume != "" || f.target != "alt" {
			t.Fatalf("%s: shown on %q, belongs on %q; want \"\" and alt (not mounted, but chosen)", f.ID, f.Volume, f.target)
		}
	}
	var s sysdata.Settings
	s.Pending = map[string][]string{}
	notePending(&s, sysdata.Logs, "alt", "neu")
	notePending(&s, sysdata.Smart, "alt", "alt")
	notePending(&s, sysdata.Notify, "", "neu")
	notePending(&s, sysdata.Activity, "alt", "")
	if len(s.Pending) != 1 || !slices.Equal(s.Pending[sysdata.Logs], []string{"alt"}) {
		t.Fatalf("pending %v", s.Pending)
	}
}
