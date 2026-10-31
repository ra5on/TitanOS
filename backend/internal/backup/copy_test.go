package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/usb"
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
	// A link or FIFO in the source is left out with a warning; the rest
	// is still copied.
	os.Symlink("/etc/passwd", filepath.Join(source, "link"))
	if err := syscall.Mkfifo(filepath.Join(source, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(source, "new.txt"), []byte("new"), 0600)
	if err := m.copyUSB(context.Background(), job); err != nil {
		t.Fatal("a link stopped the whole copy:", err)
	}
	for _, name := range []string{"link", "pipe"} {
		if _, err := os.Lstat(filepath.Join(target, "backups/copy/docs", name)); !os.IsNotExist(err) {
			t.Fatalf("%s copied", name)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(target, "backups/copy/docs/new.txt")); string(b) != "new" {
		t.Fatal("rest not copied")
	}
	state := m.states[job.ID]
	if state.Skipped != 2 || !strings.Contains(state.Warning, "docs/link") || !strings.Contains(state.Warning, "docs/pipe") {
		t.Fatalf("not warned: %+v", state)
	}
	var saved State
	if data, err := os.ReadFile(m.stateFile(job)); err != nil || json.Unmarshal(data, &saved) != nil || saved.Skipped != 2 {
		t.Fatalf("state not saved: %s %v", data, err)
	}
	os.Remove(filepath.Join(source, "link"))
	os.Remove(filepath.Join(source, "pipe"))
	if err := m.copyUSB(context.Background(), job); err != nil || m.states[job.ID].Warning != "" || m.states[job.ID].Skipped != 0 {
		t.Fatalf("warning stayed: %v %+v", err, m.states[job.ID])
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

func TestCopySkipsProblemsAndKeepsGoing(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	m := NewManager(config.Open(t.TempDir()))
	m.Sources = func() []Source { return []Source{{Name: "docs", Volume: "tank", Path: source}} }
	os.WriteFile(filepath.Join(source, "good"), []byte("good"), 0o600)
	os.WriteFile(filepath.Join(source, "locked"), []byte("secret"), 0o000)
	job := Job{ID: "copy", Sources: []string{"docs", "gone"}, Folders: map[string][]string{"docs": {".", "missing"}}, Destination: Destination{Type: "usb-copy", Folder: "backups/copy"}}
	m.states["copy"] = &State{Running: true}
	root, _ := os.OpenRoot(target)
	defer root.Close()
	if err := m.copyTo(context.Background(), job, root); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(filepath.Join(target, "backups/copy/docs/good")); string(data) != "good" {
		t.Fatal("good file not copied")
	}
	state := m.states["copy"]
	if os.Geteuid() != 0 && state.Skipped != 3 {
		t.Errorf("skipped %d: %s", state.Skipped, state.Warning)
	}
	for _, want := range []string{"shares not available: gone", "folders not available: docs/missing"} {
		if !strings.Contains(state.Warning, want) {
			t.Errorf("warning %q lacks %q", state.Warning, want)
		}
	}
	if state.Percent != 100 || state.BytesTotal == 0 {
		t.Errorf("progress %+v", state)
	}

	// Nothing of the job there at all: that is an error, not an empty copy.
	job.Sources = []string{"gone"}
	if err := m.copyTo(context.Background(), job, root); err == nil {
		t.Fatal("copy without any share succeeded")
	}
}

func TestCopyErrorsFatalOrSkipped(t *testing.T) {
	for _, err := range []error{syscall.ENOSPC, syscall.EROFS, syscall.EIO, context.Canceled} {
		if !fatalTarget(fmt.Errorf("write: %w", err)) {
			t.Errorf("%v not fatal", err)
		}
	}
	for _, err := range []error{syscall.EFBIG, syscall.EINVAL, errSourceChanged} {
		if fatalTarget(err) {
			t.Errorf("%v fatal", err)
		}
	}
	if !strings.Contains(problemOf(syscall.EFBIG), "4 GB") {
		t.Error("FAT32 limit not named")
	}
	var p copyProblems
	for i := range 7 {
		p.add("names", fmt.Sprint(i))
	}
	if p.total() != 7 || p.String() != "7 names: 0, 1, 2, 3, 4, …" {
		t.Errorf("summary %q", p.String())
	}
}

func TestUSBCopyReportsOncePerSeriesAndCatchesUpOnPlug(t *testing.T) {
	mnt := t.TempDir()
	source := filepath.Join(mnt, "tank", "docs")
	os.MkdirAll(source, 0o755)
	os.WriteFile(filepath.Join(source, "a"), []byte("a"), 0o600)
	target := t.TempDir()
	m := NewManager(config.Open(t.TempDir()))
	m.mountRoot = mnt
	m.Sources = func() []Source { return []Source{{Name: "docs", Volume: "tank", Path: source}} }
	plugged := false
	m.WithUSB = func(ctx context.Context, id string, work func(*os.Root) error) error {
		if !plugged {
			return usb.ErrNotFound
		}
		root, _ := os.OpenRoot(target)
		defer root.Close()
		return work(root)
	}
	results := make(chan string, 10)
	m.OnResult = func(job string, err error) { results <- fmt.Sprint(err) }
	m.OnRecovered = func(job string) { results <- "recovered" }
	m.jobs = []Job{{ID: "stick", Enabled: true, Sources: []string{"docs"}, Destination: Destination{Type: "usb-copy", DeviceID: "ABCD-1234", Folder: "b"}}}
	wait := func() State {
		for range 500 {
			if s := m.state(m.jobs[0]); !s.Running {
				return s
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("job did not end")
		return State{}
	}
	for range 3 {
		if err := m.Start("stick"); err != nil {
			t.Fatal(err)
		}
		if s := wait(); s.LastErrorCode != "usb_not_found" {
			t.Fatalf("code %q", s.LastErrorCode)
		}
	}
	if len(results) != 1 {
		t.Fatalf("%d messages for one series of failures", len(results))
	}
	<-results
	// Another drive changes nothing; this one catches up at once.
	m.Plugged("OTHER-1")
	plugged = true
	m.Plugged("ABCD-1234")
	time.Sleep(20 * time.Millisecond)
	if s := wait(); s.LastOK == nil || s.FailingSince != nil {
		t.Fatalf("not caught up: %+v", s)
	}
	if got := []string{<-results, <-results}; got[0] != "<nil>" || got[1] != "recovered" {
		t.Fatalf("messages %v", got)
	}
}
