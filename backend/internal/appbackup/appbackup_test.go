package appbackup

import (
	"context"
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/backup"
	"github.com/ra5on/TitanOS/backend/internal/config"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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
