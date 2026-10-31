package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/retention"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

func TestAuditRetentionPreservesRecreatedManualSnapshot(t *testing.T) {
	m, _ := snapshotFilesFixture(t)
	m.runner = scheduleSnapshotRunner{m.runner.(*fakeRunner)}
	ctx := context.Background()
	if err := m.ScheduledSnapshot(ctx, "data", "audit", 1, 0, nil); err != nil {
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
	if err := m.ScheduledSnapshot(ctx, "data", "audit", 1, 0, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("MANUAL replacement snapshot %s was deleted by automatic retention: %v", old, err)
	}
}

func TestScheduledSnapshotsKeptByDayWeekMonth(t *testing.T) {
	m, _ := snapshotFilesFixture(t)
	m.runner = scheduleSnapshotRunner{m.runner.(*fakeRunner)}
	ctx := context.Background()
	root, err := volumedata.Open(m.mountRoot, "data", "snapplans", m.Mounted())
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	// Three older snapshots: two days ago twice, and five weeks ago.
	var copies []scheduledSnapshot
	for _, at := range []time.Time{time.Now().AddDate(0, 0, -2).Add(-time.Hour), time.Now().AddDate(0, 0, -2), time.Now().AddDate(0, 0, -35)} {
		name := "auto-plan-" + at.UTC().Format("20060102T150405") + "-000000000"
		if err := m.SnapshotAction(ctx, "data", name, "create", ""); err != nil {
			t.Fatal(err)
		}
		uuid, err := m.snapshotIdentity(ctx, "data", name)
		if err != nil {
			t.Fatal(err)
		}
		copies = append(copies, scheduledSnapshot{Name: name, UUID: uuid})
	}
	volumedata.WriteJSON(root, "plan.v2.json", copies)
	if err := m.ScheduledSnapshot(ctx, "data", "plan", 1, 0, &retention.Keep{Daily: 2}); err != nil {
		t.Fatal(err)
	}
	var left []scheduledSnapshot
	volumedata.ReadJSON(root, "plan.v2.json", &left, 128<<10)
	// Today's new one and the newest of two days ago stay.
	if len(left) != 2 || left[0].Name != copies[1].Name {
		t.Fatalf("left %v", left)
	}
	dir, _ := m.snapshotDir("data")
	for _, gone := range []string{copies[0].Name, copies[2].Name} {
		if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
			t.Fatalf("%s not removed", gone)
		}
	}
}
