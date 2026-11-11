package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
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
