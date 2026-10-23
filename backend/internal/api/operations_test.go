package api

import (
	"github.com/ra5on/TitanOS/backend/internal/appbackup"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"github.com/ra5on/TitanOS/backend/internal/monitoring"
	"net/http"
	"testing"
)

func TestOperationsRoutesEnforceAdministrator(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.users.Create("other", testPassword, auth.RoleUser)
	client := ts.newClient()
	ts.do(client, "POST", "/api/v1/login", credentials{Name: "other", Password: testPassword})
	routes := [][2]string{{"GET", "/backups/coverage"}, {"GET", "/monitoring/export?hours=24&format=csv"}, {"GET", "/monitoring/trends"}, {"POST", "/backups/job/applications/preview"}, {"POST", "/backups/job/applications/restore"}, {"POST", "/storage/volumes/data/snapshots/before/files/preview"}, {"GET", "/operations"}, {"GET", "/maintenance"}, {"PUT", "/maintenance"}, {"POST", "/maintenance/job/run"}, {"GET", "/monitoring"}, {"PUT", "/monitoring"}, {"GET", "/monitoring/history?hours=24"}, {"GET", "/storage/import"}, {"POST", "/storage/import/preview"}, {"POST", "/storage/import"}, {"GET", "/storage/growth"}, {"POST", "/storage/growth/recover"}, {"POST", "/storage/volumes/data/growth"}, {"GET", "/storage/volumes/data/usage"}, {"GET", "/storage/volumes/data/snapshots/before/files"}, {"POST", "/storage/volumes/data/snapshots/before/files/restore"}, {"POST", "/backups/job/probe"}, {"GET", "/backups/application-recovery"}, {"POST", "/backups/application-recovery"}, {"POST", "/vms/guest/clone"}, {"GET", "/vm-clones/data"}, {"POST", "/vm-clones/data/id"}}
	for _, route := range routes {
		res, body := ts.do(client, route[0], "/api/v1"+route[1], map[string]any{})
		if res.StatusCode != http.StatusForbidden {
			t.Fatalf("%v: %d %s", route, res.StatusCode, body)
		}
	}
}

func TestApplicationRecoveryFencesLifecycleAfterReload(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	op := appbackup.Operation{ID: "0123456789abcdef01234567", Volume: "data", Job: "daily"}
	if err := ts.store.WriteJSON("backup/application-pending.json", op); err != nil {
		t.Fatal(err)
	}
	coordinator := &appbackup.Manager{Store: ts.store}
	if err := coordinator.Load(); err != nil {
		t.Fatal(err)
	}
	ts.server.opts.ApplicationBackups = coordinator
	for _, path := range []string{"/docker/containers/id/start", "/vms/guest/start"} {
		res, body := ts.request("POST", "/api/v1"+path, map[string]any{})
		if res.StatusCode != http.StatusLocked {
			t.Fatal(path, res.StatusCode, string(body))
		}
	}
	res, _ := ts.request("GET", "/api/v1/backups/application-recovery", nil)
	if res.StatusCode != http.StatusOK {
		t.Fatal("recovery remains accessible", res.StatusCode)
	}
	if err := ts.store.WriteJSON("backup/application-pending.json", map[string]string{"id": "invalid"}); err != nil {
		t.Fatal(err)
	}
	broken := &appbackup.Manager{Store: ts.store}
	if err := broken.Load(); err == nil {
		t.Fatal("invalid recovery pointer accepted")
	}
	ts.server.opts.ApplicationBackups = broken
	res, _ = ts.request("GET", "/api/v1/backups/application-recovery", nil)
	if res.StatusCode != http.StatusConflict {
		t.Fatal("unreadable recovery state was hidden", res.StatusCode)
	}
}
func TestOperationsUnavailableAndLifecycleGate(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	for _, path := range []string{"/maintenance", "/monitoring", "/backups/application-recovery"} {
		res, _ := ts.request("GET", "/api/v1"+path, nil)
		if res.StatusCode != 503 {
			t.Fatal(path, res.StatusCode)
		}
	}
	ts.server.opts.Maintenance = maintenance.New(ts.store)
	ts.server.opts.Monitoring = monitoring.New(ts.store)
	for _, path := range []string{"/maintenance", "/monitoring"} {
		res, body := ts.request("GET", "/api/v1"+path, nil)
		if res.StatusCode != 200 {
			t.Fatal(path, res.StatusCode, string(body))
		}
	}
	coordinator := &appbackup.Manager{}
	ts.server.opts.ApplicationBackups = coordinator
	coordinator.Gate.Lock()
	defer coordinator.Gate.Unlock()
	for _, path := range []string{"/docker/containers/id/start", "/vms/guest/start"} {
		res, _ := ts.request("POST", "/api/v1"+path, map[string]any{})
		if res.StatusCode != 423 {
			t.Fatal(path, res.StatusCode)
		}
	}
}
