package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBtrfsCreateSafetyAndNativeMirror(t *testing.T) {
	for _, req := range []CreateRequest{
		{Name: "mirror", Level: "raid5", Filesystem: "btrfs", Disks: []string{"sde", "sdf"}, Erase: []string{"sde", "sdf"}},
		{Name: "mirror", Level: "raid1", Filesystem: "btrfs", Disks: []string{"sde", "sdf"}},
		{Name: "mirror", Level: "raid1", Filesystem: "btrfs", Disks: []string{"sde", "sdz"}, Erase: []string{"sde", "sdz"}},
	} {
		m, runner, _ := newTestManager(t)
		if err := m.Create(context.Background(), req); err == nil {
			t.Fatal("accepted unsafe mirror", req)
		}
		if len(runner.commands) != 0 {
			t.Fatal("unsafe request changed disks", runner.commands)
		}
	}
	m, runner, _ := newTestManager(t)
	req := CreateRequest{Name: "mirror", Level: "raid1", Filesystem: "btrfs", Disks: []string{"sde", "sdf"}, Erase: []string{"sde", "sdf"}}
	if err := m.Create(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	commands := strings.Join(runner.commands, "\n")
	if !strings.Contains(commands, "mkfs.btrfs -f -L mirror -d raid1 -m raid1 /dev/sde1 /dev/sdf1") || strings.Contains(commands, "mdadm --create") {
		t.Fatal(commands)
	}
	if !strings.Contains(commands, "subvolid=5,degraded") {
		t.Fatal("missing Btrfs mount options", commands)
	}
	if cfg, ok := m.find("mirror"); !ok || cfg.ArrayUUID != "" || cfg.FilesystemUUID == "" {
		t.Fatalf("not persisted by filesystem UUID: %+v", cfg)
	}
}

func TestBtrfsSnapshotSafety(t *testing.T) {
	m, runner, _ := newTestManager(t)
	m.volumes = []VolumeConfig{{Name: "mirror", Filesystem: "btrfs", FilesystemUUID: "fs"}}
	point := m.MountPoint("mirror")
	os.MkdirAll(point, 0o755)
	m.mounts = func() map[string]bool { return map[string]bool{point: true} }
	ctx := context.Background()
	for _, name := range []string{"../escape", "a/b", "-option", ""} {
		if err := m.SnapshotAction(ctx, "mirror", name, "create", ""); !errors.Is(err, ErrInvalidName) {
			t.Fatalf("%q: %v", name, err)
		}
	}
	if err := m.SnapshotAction(ctx, "mirror", "before", "create", ""); err != nil {
		t.Fatal(err)
	}
	if got := runner.commands[len(runner.commands)-1]; !strings.Contains(got, "subvolume snapshot -r "+point) {
		t.Fatal(got)
	}
	snapshot := filepath.Join(point, ".titanos/snapshots/before")
	os.Mkdir(snapshot, 0o700)
	if err := m.SnapshotAction(ctx, "mirror", "before", "create", ""); !errors.Is(err, ErrExists) {
		t.Fatal("overwrote snapshot", err)
	}
	if err := m.SnapshotAction(ctx, "mirror", "before", "delete", ""); !errors.Is(err, ErrNotConfirmed) {
		t.Fatal("unconfirmed deletion", err)
	}
	if err := m.SnapshotAction(ctx, "mirror", "before", "restore", ""); err != nil {
		t.Fatal(err)
	}
	if got := runner.commands[len(runner.commands)-1]; !strings.HasSuffix(got, "/restored/before") {
		t.Fatal("restored over live volume", got)
	}
	os.Mkdir(filepath.Join(point, "restored/before"), 0o700)
	if err := m.SnapshotAction(ctx, "mirror", "before", "restore", ""); !errors.Is(err, ErrExists) {
		t.Fatal("overwrote restore", err)
	}
	m.ReadOnly = true
	if err := m.SnapshotAction(ctx, "mirror", "before", "delete", "before"); !errors.Is(err, ErrReadOnly) {
		t.Fatal("dev mode changed volume", err)
	}
	if _, err := m.Snapshots("mirror"); !errors.Is(err, ErrReadOnly) {
		t.Fatal("dev mode read changed volume", err)
	}
}

func TestBtrfsSnapshotsRejectSymlinkAndUnmountedVolume(t *testing.T) {
	m, runner, _ := newTestManager(t)
	m.volumes = []VolumeConfig{{Name: "mirror", Filesystem: "btrfs"}}
	point := m.MountPoint("mirror")
	os.MkdirAll(point, 0o755)
	if err := m.SnapshotAction(context.Background(), "mirror", "safe", "create", ""); !errors.Is(err, ErrNotPossible) {
		t.Fatal(err)
	}
	m.mounts = func() map[string]bool { return map[string]bool{point: true} }
	os.Symlink(t.TempDir(), filepath.Join(point, ".titanos"))
	if err := m.SnapshotAction(context.Background(), "mirror", "safe", "create", ""); err == nil {
		t.Fatal("followed symlink")
	}
	if len(runner.commands) != 0 {
		t.Fatal(runner.commands)
	}
}

func TestBtrfsMembershipAndProgress(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.volumes = []VolumeConfig{{Name: "mirror", Filesystem: "btrfs", FilesystemUUID: "same", Level: "raid1"}}
	devices := []blockDevice{{Name: "/dev/sdx", Type: "disk", Size: 8 << 30, Children: []blockDevice{{Name: "/dev/sdx1", FSType: "btrfs", UUID: "same", Size: 7 << 30}}}}
	status := m.status(devices, nil)
	if status.Disks[0].Role != RoleMember || status.Volumes[0].State != VolumeDegraded {
		t.Fatalf("missing member not detected: %+v", status)
	}
	if err := m.deleteBtrfs(context.Background(), m.volumes[0]); !errors.Is(err, ErrNotPossible) {
		t.Fatal("forgot incomplete mirror", err)
	}
	if state := parseBtrfsProgress("Status: running\nuncorrectable_errors: 2", ""); state.State != VolumeChecking || state.Errors != 2 {
		t.Fatal(state)
	}
	if state := parseBtrfsProgress("Status: finished", "32.5% done, 0 write errs"); state.State != VolumeRebuilding || state.Sync.Percent != 32.5 {
		t.Fatal(state)
	}
}

func TestBtrfsMissingDeviceOutput(t *testing.T) {
	output := "Label: 'mirror'  uuid: fe7d9deb-4876-480e-a6c1-5ef3b16c4630\n\tTotal devices 2 FS bytes used 5685248\n\tdevid    1 size 2012217344 used 880672768 path /dev/vda1\n\tdevid    2 size 0 used 0 path  MISSING\n"
	matches := btrfsDeviceLine.FindAllStringSubmatch(output, -1)
	if len(matches) != 2 || matches[1][1] != "2" || !strings.EqualFold(strings.TrimSpace(matches[1][3]), "missing") {
		t.Fatal(matches)
	}
}
