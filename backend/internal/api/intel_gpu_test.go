package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

func TestIntelGucRoutes(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, _ := ts.request("GET", "/api/v1/hardware/intel-gpu", nil); res.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("without the setting: %d", res.StatusCode)
	}
	// The area takes its manager when the routes are registered: a second
	// server that was built with the setting.
	ts = newTestServerWith(t, func(o *Options) {
		guc := hardware.NewIntelGuc(o.Store, hardware.NewManager(), nil, true)
		if err := guc.Load(); err != nil {
			t.Fatal(err)
		}
		o.IntelGuc = guc
	})
	ts.setup()

	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, method := range []string{"GET", "PUT"} {
		if res, _ := ts.do(guest, method, "/api/v1/hardware/intel-gpu", map[string]string{"mode": "huc"}); res.StatusCode != http.StatusForbidden {
			t.Errorf("guest %s: %d", method, res.StatusCode)
		}
	}

	res, body := ts.request("GET", "/api/v1/hardware/intel-gpu", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"mode":"auto"`) || !strings.Contains(string(body), `"writable":false`) {
		t.Errorf("status: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/hardware/intel-gpu", map[string]string{"mode": "fast"}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("unknown mode: %d %s", res.StatusCode, body)
	}
	// Development mode never writes the boot entries.
	if res, body := ts.request("PUT", "/api/v1/hardware/intel-gpu", map[string]string{"mode": "huc"}); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "intel_guc_unavailable") {
		t.Errorf("development mode: %d %s", res.StatusCode, body)
	}
	var stored hardware.IntelGucSettings
	if err := ts.store.ReadJSON("system/intel-gpu.json", &stored); err == nil {
		t.Errorf("setting stored: %+v", stored)
	}
}

func TestUASQuirkRoutes(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, _ := ts.request("GET", "/api/v1/hardware/uas-quirks", nil); res.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("without the setting: %d", res.StatusCode)
	}
	ts = newTestServerWith(t, func(o *Options) {
		quirks := hardware.NewUASQuirks(o.Store, nil, true)
		if err := quirks.Load(); err != nil {
			t.Fatal(err)
		}
		o.UASQuirks = quirks
	})
	ts.setup()
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, method := range []string{"GET", "PUT"} {
		if res, _ := ts.do(guest, method, "/api/v1/hardware/uas-quirks", map[string]any{"device": "152d:0578", "on": true}); res.StatusCode != http.StatusForbidden {
			t.Errorf("guest %s: %d", method, res.StatusCode)
		}
	}
	res, body := ts.request("GET", "/api/v1/hardware/uas-quirks", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"devices":[]`) || !strings.Contains(string(body), `"writable":false`) {
		t.Errorf("status: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/hardware/uas-quirks", map[string]any{"device": "152d:0578:u; reboot", "on": true}); res.StatusCode != http.StatusBadRequest {
		t.Errorf("bad device: %d %s", res.StatusCode, body)
	}
	// Development mode never writes the boot entries.
	if res, body := ts.request("PUT", "/api/v1/hardware/uas-quirks", map[string]any{"device": "152d:0578", "on": true}); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "uas_quirks_unavailable") {
		t.Errorf("development mode: %d %s", res.StatusCode, body)
	}
}
