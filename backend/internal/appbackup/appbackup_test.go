package appbackup

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/vm"
)

func TestPendingRecoveryAndDisabledNeverRunApplicationCommands(t *testing.T) {
	store := config.Open(t.TempDir())
	op := Operation{ID: "0123456789abcdef01234567", Volume: "data", Job: "daily"}
	if err := store.WriteJSON(pointerFile, op); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Store: store}
	if !m.Busy() {
		t.Fatal("unread recovery state must fence mutations")
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	loaded, active := m.Status()
	if active || loaded == nil || loaded.ID != op.ID {
		t.Fatal(loaded, active)
	}
	if !m.Busy() {
		t.Fatal("pending recovery must fence mutations")
	}
	if _, _, err := m.Prepare(context.Background(), backup.Job{}, "data", nil); err == nil {
		t.Fatal("pending backup ignored")
	}
	m.Disabled = true
	if _, _, err := m.Prepare(context.Background(), backup.Job{}, "data", nil); !errors.Is(err, backup.ErrDisabled) {
		t.Fatal(err)
	}
}
func TestExportBoundStopsAndNamesAreFixed(t *testing.T) {
	cancelled := false
	w := boundedWriter{writer: os.Stdout, left: 1, cancel: func() { cancelled = true }}
	if _, err := w.Write([]byte("too long")); err == nil || !cancelled {
		t.Fatal("export overflow accepted")
	}
	if err := dumpPostgres(context.Background(), "--unsafe", "postgres", filepath.Join(t.TempDir(), "dump")); err == nil {
		t.Fatal("unsafe identity accepted")
	}
}

func TestPostgresExportUsesImageSuperuser(t *testing.T) {
	id := strings.Repeat("a", 64)
	args, err := postgresDumpArgs(id, "immich")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "--user", "postgres", id, "pg_dumpall", "--no-password", "--username=immich"}
	if !slices.Equal(args, want) {
		t.Fatal(args)
	}
	for _, bad := range []string{"", "a\nb", strings.Repeat("x", 64)} {
		if _, err := postgresDumpArgs(id, bad); err == nil {
			t.Fatalf("user %q accepted", bad)
		}
	}
}

// A pointer restored from a stick backup that was taken during a run names an
// operation whose journal is gone. It must not fence application changes for
// good: it can be forgotten, but nothing is restarted blindly.
func TestPointerWithoutJournalCanBeForgotten(t *testing.T) {
	store := config.Open(t.TempDir())
	op := Operation{ID: "0123456789abcdef01234567", Volume: "data", Job: "daily"}
	if err := store.WriteJSON(pointerFile, op); err != nil {
		t.Fatal(err)
	}
	mnt := t.TempDir()
	os.MkdirAll(filepath.Join(mnt, "data"), 0o755)
	m := &Manager{Store: store, MountRoot: mnt, Mounted: func() []string { return []string{"data"} }}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Review(); !errors.Is(err, ErrJournalMissing) {
		t.Fatalf("review: %v", err)
	}
	if err := m.Recover(context.Background(), op.ID, true); !errors.Is(err, ErrJournalMissing) {
		t.Fatalf("restart without a journal: %v", err)
	}
	if !m.Busy() {
		t.Fatal("a refused recovery released the fence")
	}
	if err := m.Recover(context.Background(), op.ID, false); err != nil {
		t.Fatal(err)
	}
	if m.Busy() {
		t.Fatal("the forgotten pointer still fences application changes")
	}
	var stored *Operation
	if err := store.ReadJSON(pointerFile, &stored); err != nil || stored != nil {
		t.Fatalf("pointer on the stick: %v %v", stored, err)
	}
}

// runManager is a manager over a mounted volume "data" with no containers
// and no VMs.
func runManager(t *testing.T, store *config.Store, mnt string) *Manager {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(mnt, "data"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manager{Store: store, VMs: vm.NewManager(store, 0), MountRoot: mnt, Mounted: func() []string { return []string{"data"} }}
	m.inventory = func(context.Context, []backup.Application, []string) (*docker.BackupLease, []Member, []string, error) {
		return nil, nil, nil, nil
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m
}

// A complete application backup must not write to the stick (PLAN 79-12):
// the pointer to the run lies next to its journal on the volume.
func TestRunNeverWritesTheStick(t *testing.T) {
	store, mnt := config.Open(t.TempDir()), t.TempDir()
	m := runManager(t, store, mnt)
	before := store.Writes()
	paths, finish, err := m.Prepare(context.Background(), backup.Job{ID: "daily"}, "data", nil)
	if err != nil {
		t.Fatal(err)
	}
	pointer := filepath.Join(mnt, "data", ".titanos", "appbackups", volumePointer)
	var onVolume Operation
	data, err := os.ReadFile(pointer)
	if err != nil || json.Unmarshal(data, &onVolume) != nil || onVolume.Volume != "data" || onVolume.Job != "daily" || !operationID.MatchString(onVolume.ID) {
		t.Fatalf("pointer on the volume: %s %v", data, err)
	}
	if len(paths) != 1 || !strings.HasSuffix(paths[0], onVolume.ID+"/operation.json") || !m.Busy() {
		t.Fatalf("running: %v busy=%v", paths, m.Busy())
	}
	if _, _, err := m.Prepare(context.Background(), backup.Job{ID: "other"}, "data", nil); err == nil {
		t.Fatal("a second run started while one is underway")
	}
	m.Discover() // the watcher passing by must not take the running job for a cut-off one
	if err := finish(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pointer); !os.IsNotExist(err) {
		t.Fatalf("pointer left on the volume: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Dir(pointer)); len(entries) != 0 {
		t.Fatalf("journal left: %v", entries)
	}
	if m.Busy() {
		t.Fatal("still busy after the run")
	}
	if writes := store.Writes() - before; writes != 0 {
		t.Fatalf("/config was written %d times by an application backup", writes)
	}
	if onStick(store) {
		t.Fatal("pointer on the stick")
	}
}

// A run that was cut off is found on its volume after the restart, fences
// application changes and is noted on the stick then, so that an older
// titand (after a rollback) sees it as well. Clearing it removes both.
func TestCutOffRunIsFoundOnTheVolume(t *testing.T) {
	store, mnt := config.Open(t.TempDir()), t.TempDir()
	first := runManager(t, store, mnt)
	if _, _, err := first.Prepare(context.Background(), backup.Job{ID: "daily"}, "data", nil); err != nil {
		t.Fatal(err)
	}
	// titand goes down here. The next one does not know the volumes yet.
	settled := false
	m := &Manager{Store: store, VMs: vm.NewManager(store, 0), MountRoot: mnt, Mounted: func() []string { return []string{"data"} }, Settled: func() bool { return settled }}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	m.Discover()
	if op, _ := m.Status(); op != nil || !m.Busy() {
		t.Fatalf("before the volumes are known nothing is pending, but changes are fenced: %v busy=%v", op, m.Busy())
	}
	if _, _, err := m.Prepare(context.Background(), backup.Job{ID: "daily"}, "data", nil); err == nil {
		t.Fatal("a run started before the volumes were looked at")
	}
	settled = true
	before := store.Writes()
	m.Discover()
	op, active := m.Status()
	if op == nil || active || op.Volume != "data" || op.Job != "daily" || !m.Busy() {
		t.Fatalf("cut-off run not found: %v %v", op, active)
	}
	// What an older titand reads at its start.
	var old *Operation
	if err := store.ReadJSON(pointerFile, &old); err != nil || old == nil || old.ID != op.ID || old.Volume != "data" || old.Job != "daily" {
		t.Fatalf("pointer for older versions: %v %v", old, err)
	}
	m.Discover()
	if writes := store.Writes() - before; writes != 1 {
		t.Fatalf("the stick was written %d times for one cut-off run", writes)
	}
	// And a new titand started again reads it from the stick as before.
	again := &Manager{Store: store}
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if loaded, _ := again.Status(); loaded == nil || loaded.ID != op.ID {
		t.Fatalf("after another start: %v", loaded)
	}
	if reviewed, _, err := m.Review(); err != nil || reviewed.ID != op.ID {
		t.Fatalf("review: %v %v", reviewed, err)
	}
	if err := m.Recover(context.Background(), op.ID, false); err != nil {
		t.Fatal(err)
	}
	if m.Busy() || onStick(store) {
		t.Fatalf("after the recovery: busy=%v, pointer on the stick=%v", m.Busy(), onStick(store))
	}
	if entries, _ := os.ReadDir(filepath.Join(mnt, "data", ".titanos", "appbackups")); len(entries) != 0 {
		t.Fatalf("left on the volume: %v", entries)
	}
}

// Looking for a pointer creates nothing on a volume that has none, and a
// pointer that names another volume or is damaged is not taken.
func TestDiscoverLeavesVolumesAlone(t *testing.T) {
	store, mnt := config.Open(t.TempDir()), t.TempDir()
	m := runManager(t, store, mnt)
	m.Discover()
	if entries, _ := os.ReadDir(filepath.Join(mnt, "data")); len(entries) != 0 || m.Busy() {
		t.Fatalf("looking created %v, busy=%v", entries, m.Busy())
	}
	dir := filepath.Join(mnt, "data", ".titanos", "appbackups")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{`{"id":"0123456789abcdef01234567","volume":"other","job":"daily"}`, `{"id":"../x","volume":"data","job":"daily"}`, `nonsense`} {
		if err := os.WriteFile(filepath.Join(dir, volumePointer), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		m.mu.Lock()
		m.looked = nil
		m.mu.Unlock()
		m.Discover()
		if op, _ := m.Status(); op != nil || store.Writes() != 0 {
			t.Fatalf("%s taken: %v", content, op)
		}
	}
}

// onStick tells whether the stick names a pending run ("null" is none).
func onStick(store *config.Store) bool {
	var op *Operation
	return store.ReadJSON(pointerFile, &op) == nil && op != nil
}
