package api

import (
	"context"
	"github.com/ra5on/TitanOS/backend/internal/maintenance"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditMaintenanceFencesAllHeavyEntrypoints(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.disks.ReadOnly = true
	m := maintenance.New(ts.store)
	m.MountRoot = t.TempDir()
	if err := os.Mkdir(filepath.Join(m.MountRoot, "data"), 0700); err != nil {
		t.Fatal(err)
	}
	m.Mounted = func() []string { return []string{"data"} }
	s := maintenance.Settings{Volume: "data", Timezone: "UTC", WindowFrom: "00:00", WindowTo: "00:00", Jobs: []maintenance.Job{{ID: "held", Name: "Held maintenance", Kind: "raid-check", Target: "data", Every: "daily", Time: "03:00", Missed: "window"}}}
	if err := m.Set(s); err != nil {
		t.Fatal(err)
	}
	m.Execute = func(ctx context.Context, j maintenance.Job) error { <-ctx.Done(); return ctx.Err() }
	if err := m.Start("held"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = m.Stop("held")
		deadline := time.Now().Add(3 * time.Second)
		for m.Busy() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		if m.Busy() {
			t.Error("maintenance fixture did not stop")
		}
	}()
	ts.server.opts.Maintenance = m
	for _, path := range []string{"/storage/tests", "/storage/volumes", "/vms", "/vms/test/snapshots", "/vms/test/grow", "/files/jobs/unknown/retry", "/backups/unknown/run", "/backups/unknown/check", "/backups/unknown/probe", "/backups/unknown/applications/restore", "/docker/apps", "/plugins"} {
		t.Run(path, func(t *testing.T) {
			res, body := ts.request("POST", "/api/v1"+path, map[string]any{})
			if res.StatusCode != http.StatusLocked {
				t.Errorf("heavy request bypassed active maintenance: got %d (%s), expected 423", res.StatusCode, body)
			}
		})
	}
}
