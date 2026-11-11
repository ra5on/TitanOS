package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

// The job editor asks which VM disks a copy would meet. Owners and
// administrators only, like the other backup routes; codes and values, no
// sentences; nothing is changed by asking.
func TestBackupVMDisksPreview(t *testing.T) {
	var store *config.Store
	ts := newTestServerWith(t, func(o *Options) {
		store = o.Store
		// A VM with a disk in its folder on the volume "tank".
		if err := store.WriteJSON("vms/ha.json", map[string]any{
			"name": "ha", "uuid": "11111111-2222-3333-4444-555555555555", "volume": "tank", "template": "linux", "memoryMiB": 2048, "vcpus": 2,
			"disks": []map[string]any{{"file": "vdisk1.qcow2", "format": "qcow2", "sizeBytes": 34359738368, "volume": "tank"}},
		}); err != nil {
			t.Fatal(err)
		}
		machines := vm.NewManager(store, 0)
		machines.Disabled = true
		if err := machines.Load(); err != nil {
			t.Fatal(err)
		}
		o.VMs = machines
		jobs := backup.NewManager(store)
		jobs.Disabled = true
		jobs.Sources = func() []backup.Source {
			return []backup.Source{{Name: "machines", Volume: "tank", Path: "/mnt/tank/vms"}, {Name: "media", Volume: "tank", Path: "/mnt/tank/media"}}
		}
		jobs.Load()
		o.Backup = jobs
	})
	if res, _ := ts.request("GET", "/api/v1/backups/vm-disks?source=machines", nil); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}
	ts.setup()
	var answer struct {
		Choice       *backup.VMDisks        `json:"choice"`
		ChoiceNeeded bool                   `json:"choiceNeeded"`
		Disks        []backup.VMDiskPreview `json:"disks"`
	}
	ask := func(query string) {
		t.Helper()
		res, body := ts.request("GET", "/api/v1/backups/vm-disks?"+query, nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("%s: %d %s", query, res.StatusCode, body)
		}
		answer.Choice, answer.Disks = nil, nil
		if err := json.Unmarshal(body, &answer); err != nil {
			t.Fatalf("%s: %v in %s", query, err, body)
		}
	}
	ask("source=machines")
	if len(answer.Disks) != 1 || answer.Choice != nil || answer.ChoiceNeeded {
		t.Fatalf("%+v", answer)
	}
	want := backup.VMDiskPreview{VM: "ha", Path: "machines/ha/vdisk1.qcow2", File: filepath.Join("/mnt/tank/vms/ha/vdisk1.qcow2"), Format: "qcow2", SizeBytes: 34359738368, State: "stopped", Way: backup.VMDiskWayCopy}
	if answer.Disks[0] != want {
		t.Fatalf("got %+v, want %+v", answer.Disks[0], want)
	}
	// Not among the sources: not listed. An empty list is a list.
	ask("source=media")
	if answer.Disks == nil || len(answer.Disks) != 0 {
		t.Fatalf("%+v", answer.Disks)
	}
	// "What if" for a setting that is not stored yet.
	ask("source=machines&mode=consistent&raw=pause")
	if answer.Choice == nil || answer.Choice.Mode != "consistent" || answer.Choice.Raw != "pause" {
		t.Fatalf("%+v", answer.Choice)
	}
	for _, bad := range []string{"mode=always", "mode=skip&raw=pause", "mode=consistent&raw=stop"} {
		if res, body := ts.request("GET", "/api/v1/backups/vm-disks?source=machines&"+bad, nil); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_backup_job" {
			t.Errorf("%s: %d %s", bad, res.StatusCode, body)
		}
	}
	if res, body := ts.request("GET", "/api/v1/backups/vm-disks?job=gone", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "backup_job_not_found" {
		t.Errorf("unknown job: %d %s", res.StatusCode, body)
	}
	// A stored job with its setting.
	job := map[string]any{"id": "kopie", "sources": []string{"machines"}, "enabled": true, "every": "daily", "time": "02:30",
		"destination": map[string]any{"type": "usb-copy", "deviceID": "ABCD-1234", "folder": "backup"}, "vmDisks": map[string]any{"mode": "skip"}}
	if res, body := ts.request("POST", "/api/v1/backups", job); res.StatusCode != http.StatusOK {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	writes := store.Writes()
	ask("job=kopie")
	if answer.Choice == nil || answer.Choice.Mode != "skip" || len(answer.Disks) != 1 {
		t.Fatalf("%+v", answer)
	}
	if store.Writes() != writes {
		t.Fatal("asking wrote to /config")
	}
	// The setting comes back with the job, and a wrong one is refused.
	if res, body := ts.request("GET", "/api/v1/backups", nil); res.StatusCode != http.StatusOK || !json.Valid(body) || !strings.Contains(string(body), `"vmDisks":{"mode":"skip"}`) {
		t.Fatalf("job without its setting: %s", body)
	}
	job["id"], job["vmDisks"] = "zwei", map[string]any{"mode": "skip", "raw": "pause"}
	job["destination"] = map[string]any{"type": "usb-copy", "deviceID": "ABCD-1234", "folder": "backup2"}
	if res, body := ts.request("POST", "/api/v1/backups", job); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("wrong setting accepted: %d %s", res.StatusCode, body)
	}
	// Plain users may not ask.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, _ := ts.do(guest, "GET", "/api/v1/backups/vm-disks?source=machines", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("plain user: %d", res.StatusCode)
	}
}
