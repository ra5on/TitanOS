package backup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/usb"
)

func TestFullTargetDropsOldestOnlyWhenAllowed(t *testing.T) {
	e := newEnv(t)
	job := s3Job()
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	write := func(lines string) { os.WriteFile(filepath.Join(e.dir, "versions"), []byte(lines), 0o644) }
	versions := "v1 2026-10-01T03:00:00Z\nv2 2026-10-02T03:00:00Z\nv3 2026-10-03T03:00:00Z\nv4 2026-10-04T03:00:00Z\n"
	write(versions)
	os.WriteFile(filepath.Join(e.dir, "full"), []byte("1\n"), 0o644)

	// Default: stop and report, nothing removed.
	e.m.Start("cloud")
	if got := e.result(t); !strings.Contains(got, "not enough space") {
		t.Fatalf("full target: %s", got)
	}
	if code := e.state("cloud").LastErrorCode; code != "backup_no_space" {
		t.Fatalf("code %q", code)
	}
	if data, _ := os.ReadFile(filepath.Join(e.dir, "versions")); string(data) != versions {
		t.Fatalf("removed without leave: %s", data)
	}

	// With leave: the oldest go, one by one, until the run fits.
	job.Keep = Keep{Daily: 7, WhenFull: "oldest"}
	if err := e.m.Update("cloud", job, volumes); err != nil {
		t.Fatal(err)
	}
	freed := 0
	e.m.OnFreed = func(_ string, n int) { freed = n }
	e.m.Start("cloud")
	if got := e.result(t); got != "cloud: ok" {
		t.Fatalf("with leave: %s", got)
	}
	if data, _ := os.ReadFile(filepath.Join(e.dir, "versions")); string(data) != "v4 2026-10-04T03:00:00Z\n" || freed != 3 {
		t.Fatalf("left %q, freed %d", data, freed)
	}
	if w := e.state("cloud").Warning; !strings.Contains(w, "removed the oldest 3") {
		t.Fatalf("warning %q", w)
	}
	// The newest is never removed: a target too small for even one more
	// run fails.
	os.WriteFile(filepath.Join(e.dir, "full"), []byte("0\n"), 0o644)
	e.m.Start("cloud")
	if got := e.result(t); !strings.Contains(got, "not enough space") {
		t.Fatalf("too small: %s", got)
	}
	if data, _ := os.ReadFile(filepath.Join(e.dir, "versions")); string(data) != "v4 2026-10-04T03:00:00Z\n" {
		t.Fatalf("newest removed: %q", data)
	}
}

func TestChosenVersionsAreForgottenByID(t *testing.T) {
	e := newEnv(t)
	job := s3Job()
	job.Keep = Keep{Daily: 1, DailyAt: "03:00"}
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	// Days and times are those of the NAS time zone.
	e.m.Timezone = func() string { return "Europe/Berlin" }
	berlin, _ := time.LoadLocation("Europe/Berlin")
	local := func(day, hour int) string {
		return time.Date(2026, 10, day, hour, 0, 0, 0, berlin).UTC().Format(time.RFC3339)
	}
	os.WriteFile(filepath.Join(e.dir, "versions"), []byte(fmt.Sprintf("a %s\nb %s\nc %s\n", local(13, 3), local(14, 3), local(14, 11))), 0o644)
	e.m.Start("cloud")
	if got := e.result(t); got != "cloud: ok" {
		t.Fatal(got)
	}
	if !strings.Contains(e.calls(), "forget --prune a |") {
		t.Fatalf("calls: %s", e.calls())
	}
	if strings.Contains(e.calls(), "--keep-daily") {
		t.Fatal("restic's own keep rules used as well")
	}
}

func TestEncryptedVersionsOnUSBHoldTheDrive(t *testing.T) {
	e := newEnv(t)
	drive := t.TempDir()
	holding := false
	e.m.WithUSB = func(ctx context.Context, id string, work func(*os.Root) error) error {
		if id != "ABCD-1234" {
			return errors.New("wrong drive")
		}
		root, err := os.OpenRoot(drive)
		if err != nil {
			return err
		}
		defer root.Close()
		holding = true
		defer func() { holding = false }()
		return work(root)
	}
	job := Job{ID: "usb", Sources: []string{"media"}, Enabled: true, Every: "daily", Time: "02:00", Destination: Destination{Type: "usb", DeviceID: "ABCD-1234", DeviceName: "Backup-HDD", Folder: "titanos"}}
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	e.m.Start("usb")
	if got := e.result(t); got != "usb: ok" {
		t.Fatal(got)
	}
	if !strings.Contains(e.calls(), "repo="+filepath.Join(drive, "titanos")+" ") {
		t.Fatalf("calls: %s", e.calls())
	}
	if holding {
		t.Fatal("drive still held after the run")
	}
	// Listing versions holds it only while it looks.
	if _, err := e.m.Snapshots(context.Background(), "usb"); err != nil || holding {
		t.Fatalf("snapshots: %v, held %v", err, holding)
	}
	e.m.WithUSB = func(context.Context, string, func(*os.Root) error) error { return usb.ErrNotFound }
	if _, err := e.m.Snapshots(context.Background(), "usb"); !errors.Is(err, usb.ErrNotFound) {
		t.Fatalf("missing drive: %v", err)
	}
}

func TestReadableCopyToAnotherVolume(t *testing.T) {
	e := newEnv(t)
	os.WriteFile(filepath.Join(e.m.mountRoot, "tank/media/a.txt"), []byte("a"), 0o600)
	mounts := filepath.Join(t.TempDir(), "mounts")
	os.WriteFile(mounts, []byte("/dev/md1 "+filepath.Join(e.m.mountRoot, "cold")+" ext4 rw 0 0\n"), 0o644)
	e.m.mountsFile = mounts
	job := Job{ID: "kopie", Sources: []string{"media"}, Every: "daily", Time: "02:00", Destination: Destination{Type: "local-copy", Volume: "cold", Folder: "kopie"}}
	if err := e.m.Create(job, volumes); err != nil {
		t.Fatal(err)
	}
	e.m.Start("kopie")
	if got := e.result(t); got != "kopie: ok" {
		t.Fatal(got)
	}
	if data, err := os.ReadFile(filepath.Join(e.m.mountRoot, "cold/kopie/media/a.txt")); err != nil || string(data) != "a" {
		t.Fatalf("copy: %q %v", data, err)
	}
	job.Destination.Volume = "tank"
	job.ID = "same"
	if err := e.m.Create(job, volumes); err == nil {
		t.Fatal("copy onto the volume of its shares accepted")
	}
}
