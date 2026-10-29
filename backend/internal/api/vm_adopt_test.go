package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

func TestVMAdoptRoutesNeedAnAdministrator(t *testing.T) {
	ts := newTestServer(t)
	for _, method := range []string{"GET", "POST"} {
		if res, _ := ts.request(method, "/api/v1/vms/adopt", map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a login: %d", method, res.StatusCode)
		}
	}
	ts.setup()
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	for _, method := range []string{"GET", "POST"} {
		if res, _ := ts.do(guest, method, "/api/v1/vms/adopt", map[string]any{"volume": "tank", "directory": "office"}); res.StatusCode != http.StatusForbidden {
			t.Errorf("%s as a plain user: %d, want 403", method, res.StatusCode)
		}
	}
	// Nothing mounted: nothing to take over.
	res, body := ts.request("GET", "/api/v1/vms/adopt", nil)
	if res.StatusCode != http.StatusOK || string(body) != "{\"candidates\":[]}\n" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	// Development mode changes nothing.
	if res, body := ts.request("POST", "/api/v1/vms/adopt", map[string]any{"volume": "tank", "directory": "office"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "read_only" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
}

func TestVMAdoptFromTheVolume(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	docs := ts.withShare()
	mnt := filepath.Dir(filepath.Dir(docs))
	machines := ts.server.opts.VMs
	machines.Disabled = false
	machines.UseMountRoot(mnt)
	iso := filepath.Join(docs, "install.iso")
	folder := filepath.Join(mnt, "tank", "vms", "office")
	if err := os.WriteFile(iso, []byte("iso"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(folder, 0o770); err != nil {
		t.Fatal(err)
	}
	stored := vm.VolumeCopy{Format: 1, VM: vm.VM{Name: "office", Template: "linux", UUID: "4f5c3c8e-6f0e-4b5e-9d55-1f0d3a3b7c11", MAC: "52:54:00:12:34:56",
		MemoryMiB: 1024, VCPUs: 1, Volume: "tank", ISO: iso, Network: "user", Autostart: true, USB: []string{"046d:c52b"}}}
	data, _ := json.Marshal(stored)
	if err := os.WriteFile(filepath.Join(folder, "vm.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	res, body := ts.request("GET", "/api/v1/vms/adopt", nil)
	var list struct {
		Candidates []vm.AdoptCandidate `json:"candidates"`
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &list) != nil || len(list.Candidates) != 1 || list.Candidates[0].Name != "office" || list.Candidates[0].Directory != "office" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/vms/adopt", map[string]any{"volume": "tank", "directory": "nothing"}); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "vm_copy_not_found" {
		t.Fatalf("no copy: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/vms/adopt", map[string]any{"volume": "tank", "directory": "../x"}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_folder" {
		t.Fatalf("bad folder: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/vms/adopt", map[string]any{"volume": "tank", "directory": "office"})
	var result vm.AdoptResult
	if res.StatusCode != http.StatusCreated || json.Unmarshal(body, &result) != nil || result.Name != "office" || len(result.Changes) != 2 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var saved vm.VM
	if err := ts.store.ReadJSON("vms/office.json", &saved); err != nil || saved.UUID != stored.VM.UUID || saved.Autostart || len(saved.USB) != 0 {
		t.Fatalf("%v %+v", err, saved)
	}
	// Taken over: no longer offered, and not a second time.
	if res, body := ts.request("GET", "/api/v1/vms/adopt", nil); string(body) != "{\"candidates\":[]}\n" {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/vms/adopt", map[string]any{"volume": "tank", "directory": "office"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "vm_exists" {
		t.Fatalf("twice: %d %s", res.StatusCode, body)
	}
}
