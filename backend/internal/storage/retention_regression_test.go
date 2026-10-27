package storage

import (
	"context"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"os"
	"path/filepath"
	"testing"
)

func TestAuditRetentionPreservesRecreatedManualSnapshot(t *testing.T) {
	m, _ := snapshotFilesFixture(t)
	m.runner = scheduleSnapshotRunner{m.runner.(*fakeRunner)}
	ctx := context.Background()
	if err := m.ScheduledSnapshot(ctx, "data", "audit", 1, 0); err != nil {
		t.Fatal(err)
	}
	root, err := volumedata.Open(m.mountRoot, "data", "snapplans", m.Mounted())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	var names []scheduledSnapshot
	if err := volumedata.ReadJSON(root, "audit.v2.json", &names, 128<<10); err != nil || len(names) != 1 {
		t.Fatal(names, err)
	}
	old := names[0].Name
	if err := m.SnapshotAction(ctx, "data", old, "delete", old); err != nil {
		t.Fatal(err)
	}
	if err := m.SnapshotAction(ctx, "data", old, "create", ""); err != nil {
		t.Fatal(err)
	}
	dir, _ := m.snapshotDir("data")
	marker := filepath.Join(dir, old, "MANUAL-COPY")
	if err := os.WriteFile(marker, []byte("must survive automatic retention"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.ScheduledSnapshot(ctx, "data", "audit", 1, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("MANUAL replacement snapshot %s was deleted by automatic retention: %v", old, err)
	}
}
