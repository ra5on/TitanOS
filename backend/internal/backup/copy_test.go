package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestUSBCopyRetainsDeletedAndReplacesChanged(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	m := NewManager(config.Open(t.TempDir()))
	m.mountRoot = t.TempDir() // state files must stay inside the test, even when run as root
	m.Sources = func() []Source { return []Source{{Name: "docs", Volume: "tank", Path: source}} }
	m.WithUSB = func(ctx context.Context, id string, work func(*os.Root) error) error {
		if id != "ABCD-1234" {
			return errors.New("wrong disk")
		}
		root, err := os.OpenRoot(target)
		if err != nil {
			return err
		}
		defer root.Close()
		return work(root)
	}
	job := Job{ID: "copy", Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", DeviceID: "ABCD-1234", Folder: "backups/copy"}, Patterns: []string{"*.tmp"}}
	os.WriteFile(filepath.Join(source, "file.txt"), []byte("first"), 0600)
	os.WriteFile(filepath.Join(source, "keep.txt"), []byte("retained"), 0600)
	os.WriteFile(filepath.Join(source, "skip.tmp"), []byte("skip"), 0600)
	if err := m.copyUSB(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(source, "keep.txt"))
	os.WriteFile(filepath.Join(source, "file.txt"), []byte("changed"), 0600)
	if err := m.copyUSB(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"file.txt": "changed", "keep.txt": "retained"} {
		b, err := os.ReadFile(filepath.Join(target, "backups/copy/docs", name))
		if err != nil || string(b) != want {
			t.Fatalf("%s: %s %v", name, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(target, "backups/copy/docs/skip.tmp")); !os.IsNotExist(err) {
		t.Fatal("excluded file copied")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.copyUSB(ctx, job); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(target, "backups/copy/docs/file.txt"))
	if string(b) != "changed" {
		t.Fatal("cancel damaged prior copy")
	}
	os.Symlink(t.TempDir(), filepath.Join(target, "escape"))
	job.Destination.Folder = "escape"
	if err := m.copyUSB(context.Background(), job); err == nil {
		t.Fatal("destination symlink accepted")
	}
	job.Destination.Folder = "backups/copy"
	os.Symlink("/etc/passwd", filepath.Join(source, "link"))
	if err := m.copyUSB(context.Background(), job); err == nil {
		t.Fatal("source symlink accepted")
	}
	m.WithUSB = func(context.Context, string, func(*os.Root) error) error { return errors.New("missing USB") }
	if err := m.copyUSB(context.Background(), job); err == nil {
		t.Fatal("missing device accepted")
	}
}

func TestUSBCopyValidation(t *testing.T) {
	shares := []Source{{Name: "docs", Volume: "tank"}}
	job := Job{ID: "copy", Sources: []string{"docs"}, Every: "daily", Time: "03:00", Destination: Destination{Type: "usb-copy", DeviceID: "ABCD-1234", Folder: "backup"}}
	if err := check(job, shares, []string{"tank"}); err != nil {
		t.Fatal(err)
	}
	for _, folder := range []string{"../outside", "/root", "."} {
		bad := job
		bad.Destination.Folder = folder
		if check(bad, shares, nil) == nil {
			t.Fatal(folder)
		}
	}
	job.Extras = []string{"config"}
	if check(job, shares, nil) == nil {
		t.Fatal("plaintext config extra accepted")
	}
}

// SMB and USB jobs share copyTo. Every user-chosen path component must survive
// job persistence and the actual copy, including the default folder's job name.
func TestCopyPreservesNamesAcrossPersistence(t *testing.T) {
	for _, kind := range []string{"smb-copy", "usb-copy"} {
		t.Run(kind, func(t *testing.T) {
			source, target := t.TempDir(), t.TempDir()
			if err := os.Mkdir(filepath.Join(source, "MeineFotos"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(source, "MeineFotos", "Urlaub.JPG"), []byte("photo"), 0600); err != nil {
				t.Fatal(err)
			}
			store := config.Open(t.TempDir())
			m := NewManager(store)
			m.mountRoot = t.TempDir()
			m.Sources = func() []Source { return []Source{{Name: "Daten", Volume: "Backup", Path: source}} }
			if err := m.Load(); err != nil {
				t.Fatal(err)
			}
			m.smbTargets = []SMBTarget{{ID: "ExternesNAS"}}
			job := Job{ID: "Backup", Sources: []string{"Daten"}, Time: "02:00", Destination: Destination{Type: kind, TargetID: "ExternesNAS", DeviceID: "ABCD-1234"}}
			if err := m.Create(job, []string{"Backup"}); err != nil {
				t.Fatal(err)
			}
			duplicate := job
			duplicate.ID = "backup"
			if err := m.Create(duplicate, []string{"Backup"}); !errors.Is(err, ErrExists) {
				t.Fatalf("case duplicate: %v", err)
			}
			if err := m.Load(); err != nil {
				t.Fatal(err)
			}
			saved, ok := m.find("Backup")
			if !ok || saved.Destination.Folder != "backups/Backup" || saved.Sources[0] != "Daten" || saved.Destination.TargetID != "ExternesNAS" {
				t.Fatalf("name changed: %+v", saved)
			}
			root, err := os.OpenRoot(target)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err := m.copyTo(context.Background(), saved, root); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(target, "backups", "Backup", "Daten", "MeineFotos", "Urlaub.JPG"))
			if err != nil || string(data) != "photo" {
				t.Fatalf("copy: %q %v", data, err)
			}
			if _, err := os.Stat(filepath.Join(target, "backups", "backup")); !os.IsNotExist(err) {
				t.Fatalf("lowercase destination created: %v", err)
			}
		})
	}
}

func TestUSBProgressOnlyUsesCachedStates(t *testing.T) {
	// No store, source callback or mount root: this endpoint must only read RAM.
	manager := &Manager{jobs: []Job{{ID: "Backup", Destination: Destination{Type: "usb-copy"}}, {ID: "Other", Destination: Destination{Type: "local"}}, {ID: "Unloaded", Destination: Destination{Type: "usb-copy"}}}, states: map[string]*State{"Backup": {Running: true, Percent: 25, BytesDone: 10, BytesTotal: 40}, "Other": {Running: true}}}
	states := manager.USBProgress()
	if len(states) != 1 || !states["Backup"].Running || states["Backup"].Percent != 25 {
		t.Fatalf("states=%+v", states)
	}
	state := states["Backup"]
	state.Percent = 100
	states["Backup"] = state
	if manager.USBProgress()["Backup"].Percent != 25 {
		t.Fatal("caller changed cached state")
	}
}
