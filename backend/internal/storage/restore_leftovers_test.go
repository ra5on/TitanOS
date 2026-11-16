package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// What restores cut off by a stop of titand left goes after the next
// start: staging folders of single files and, below the folder its note
// names, the temporary entries of a folder restore. Nothing else.
func TestLeftoversOfInterruptedRestoresAreRemoved(t *testing.T) {
	m, _ := snapshotFilesFixture(t)
	safe := m.MountPoint("safe")
	data := m.MountPoint("data")
	mk := func(p string, dir bool) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if dir {
			if err := os.MkdirAll(p, 0o700); err != nil {
				t.Fatal(err)
			}
			p = filepath.Join(p, "inside")
		}
		if err := os.WriteFile(p, []byte("half"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	temp := ".titanos-restore-0123456789abcdef01234567.titanos-part"
	stage := filepath.Join(safe, ".titanos", "restores", "00112233445566778899aabb")
	mk(filepath.Join(stage, "photos", "a.jpg"), false)
	mk(filepath.Join(data, "photos", temp), false)
	mk(filepath.Join(data, "photos", "sub", temp), true)
	// Outside the noted folder, a link out of it, other names: kept.
	outside := t.TempDir()
	mk(filepath.Join(outside, temp), false)
	os.Symlink(outside, filepath.Join(data, "photos", "link"))
	kept := []string{
		filepath.Join(data, "other", temp),
		filepath.Join(data, "photos", "real.jpg"),
		filepath.Join(safe, ".titanos", "restores", "notes"),
		filepath.Join(outside, temp),
	}
	for _, p := range kept[:3] {
		mk(p, false)
	}
	root, err := volumedata.Open(m.mountRoot, "data", restoresFeature, m.Mounted())
	if err != nil {
		t.Fatal(err)
	}
	volumedata.WriteJSON(root, "folder-aabbccddeeff001122334455.json", restoreJournal{ID: "aabbccddeeff001122334455", Base: "photos"})
	root.Close()

	// A restore (or an application restore of a backup) running on the
	// target volume: its folder is left alone, tried again later.
	safeRoot, err := volumedata.Open(m.mountRoot, "safe", restoresFeature, m.Mounted())
	if err != nil {
		t.Fatal(err)
	}
	release, err := volumedata.Share(safeRoot)
	if err != nil {
		t.Fatal(err)
	}
	if m.cleanRestores("safe") {
		t.Fatal("swept while a restore runs")
	}
	if _, err := os.Stat(stage); err != nil {
		t.Fatalf("the folder of a running restore was removed: %v", err)
	}
	release()
	safeRoot.Close()

	// While a sweep holds the folder, a restore is refused at once instead
	// of waiting with the storage lock held.
	sweep, err := volumedata.Open(m.mountRoot, "safe", restoresFeature, m.Mounted())
	if err != nil {
		t.Fatal(err)
	}
	held, ok, err := volumedata.TryExclusive(sweep)
	if err != nil || !ok {
		t.Fatalf("exclusive: %v %v", ok, err)
	}
	refused := make(chan error, 1)
	go func() {
		_, err := m.RestoreSnapshotFiles(context.Background(), "data", "manual", SnapshotRestore{Paths: []string{"photos/a.jpg"}, TargetVolume: "safe"})
		refused <- err
	}()
	select {
	case err := <-refused:
		if !errors.Is(err, ErrBusy) {
			t.Fatalf("restore during a sweep: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a restore waits for the sweep while holding the storage lock")
	}
	held()
	sweep.Close()

	for _, volume := range []string{"safe", "data"} {
		if !m.cleanRestores(volume) {
			t.Fatalf("%s not swept", volume)
		}
	}
	for _, p := range []string{stage, filepath.Join(data, "photos", temp), filepath.Join(data, "photos", "sub", temp), filepath.Join(data, ".titanos", "restores", "folder-aabbccddeeff001122334455.json")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s stays: %v", p, err)
		}
	}
	for _, p := range kept {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}
}

// A folder restore keeps its note while it runs, holds the folder against
// the sweep, and removes the note at its end.
func TestFolderRestoreKeepsItsNoteWhileItRuns(t *testing.T) {
	m, _, _ := folderFixture(t)
	ctx := context.Background()
	gate, entered := make(chan struct{}), make(chan struct{})
	var once sync.Once
	m.restoreStep = func(string) {
		once.Do(func() { close(entered) })
		<-gate
	}
	preview, err := m.PreviewSnapshotFolder(ctx, "data", "manual", SnapshotFolderRequest{Path: "photos", Mode: RestoreCopy})
	if err != nil {
		t.Fatal(err)
	}
	job, err := m.StartSnapshotFolderRestore(ctx, "alex", "data", "manual", SnapshotFolderRequest{UUID: preview.UUID, Path: "photos", Mode: RestoreCopy})
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	note := filepath.Join(m.MountPoint("data"), ".titanos", "restores", "folder-"+job.ID+".json")
	if _, err := os.Stat(note); err != nil {
		t.Fatalf("no note while running: %v", err)
	}
	if m.cleanRestores("data") {
		t.Fatal("swept while a folder restore runs")
	}
	close(gate)
	if done := waitRestore(t, m, job.ID); done.State != RestoreDone {
		t.Fatalf("%+v", done)
	}
	if _, err := os.Stat(note); !os.IsNotExist(err) {
		t.Fatalf("note kept after the end: %v", err)
	}
}
