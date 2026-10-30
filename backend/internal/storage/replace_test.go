package storage

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func replaceFixture(t *testing.T) (*Manager, *treeRunner) {
	members := []blockDevice{tdisk("sda", "A", raidPart("sda1")), tdisk("sdb", "B", raidPart("sdb1")), tdisk("sdc", "C", raidPart("sdc1"))}
	m, runner := treeManager(t, append(slices.Clone(members), tdisk("sdx", "X")))
	m.volumes = []VolumeConfig{{Name: "data", Level: "raid5", Filesystem: "ext4", ArrayUUID: runningArrayUUID}}
	runningArray(t, m, "1000000", "sda1", "sdb1", "sdc1")
	return m, runner
}

func TestReplaceWorkingDiskKeepsRedundancy(t *testing.T) {
	ctx := context.Background()
	m, runner := replaceFixture(t)
	if err := m.ReplaceDisk(ctx, "data", "sdb", "sdx", idOf(m, "sdx"), false); !errors.Is(err, ErrNotConfirmed) {
		t.Fatal(err)
	}
	if err := m.ReplaceDisk(ctx, "data", "sdb", "sdb", idOf(m, "sdb"), true); !errors.Is(err, ErrNotPossible) {
		t.Fatal("old disk as new disk")
	}
	if err := m.ReplaceDisk(ctx, "data", "sdx", "sdb", idOf(m, "sdb"), true); !errors.Is(err, ErrDiskUnavailable) {
		t.Fatalf("not a member: %v", err)
	}
	if err := m.ReplaceDisk(ctx, "data", "sdb", "sdx", idOf(m, "sdx"), true); err != nil {
		t.Fatal(err)
	}
	add := slices.Index(runner.commands, "mdadm --manage /dev/md127 --add /dev/sdx1")
	replace := slices.Index(runner.commands, "mdadm --manage /dev/md127 --replace /dev/sdb1 --with /dev/sdx1")
	if add < 0 || replace < add {
		t.Fatalf("commands %v", runner.commands)
	}
	for _, c := range runner.commands {
		if c == "wipefs --all --force /dev/sdb" || c == "wipefs --all --force /dev/sdb1" {
			t.Fatal("old disk wiped before the copy")
		}
	}
}

func TestReplaceRefusesWhatCannotCopy(t *testing.T) {
	ctx := context.Background()
	m, runner := replaceFixture(t)
	md := filepath.Join(m.sysBlock, "md127", "md")
	writeSys(t, filepath.Join(md, "degraded"), "1")
	if err := m.ReplaceDisk(ctx, "data", "sdb", "sdx", idOf(m, "sdx"), true); !errors.Is(err, ErrNotPossible) {
		t.Fatalf("degraded: %v", err)
	}
	writeSys(t, filepath.Join(md, "degraded"), "0")
	writeSys(t, filepath.Join(md, "sync_action"), "check")
	if err := m.ReplaceDisk(ctx, "data", "sdb", "sdx", idOf(m, "sdx"), true); !errors.Is(err, ErrBusy) {
		t.Fatalf("checking: %v", err)
	}
	writeSys(t, filepath.Join(md, "sync_action"), "idle")
	writeSys(t, filepath.Join(md, "dev-sdb1", "state"), "spare")
	if err := m.ReplaceDisk(ctx, "data", "sdb", "sdx", idOf(m, "sdx"), true); !errors.Is(err, ErrNotPossible) {
		t.Fatalf("spare: %v", err)
	}
	m.volumes[0].Filesystem = "btrfs"
	if err := m.ReplaceDisk(ctx, "data", "sdb", "sdx", idOf(m, "sdx"), true); !errors.Is(err, ErrNotPossible) {
		t.Fatalf("btrfs: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("commands ran: %v", runner.commands)
	}
	m.ReadOnly = true
	if err := m.ReplaceDisk(ctx, "data", "sdb", "sdx", idOf(m, "sdx"), true); !errors.Is(err, ErrReadOnly) {
		t.Fatal("development mode")
	}
}

func TestIdentifyDiskReadsInTheBackground(t *testing.T) {
	m, _ := replaceFixture(t)
	read := make(chan string, 1)
	identifyRead = func(ctx context.Context, path string, size uint64) error {
		read <- path
		<-ctx.Done()
		return nil
	}
	t.Cleanup(func() { identifyRead = readForLED })
	ctx := context.Background()
	if err := m.IdentifyDisk(ctx, "sdb", 2); !errors.Is(err, ErrNotPossible) {
		t.Fatal("too short accepted")
	}
	if err := m.IdentifyDisk(ctx, "sdq", 10); !errors.Is(err, ErrDiskUnavailable) {
		t.Fatal("unknown disk")
	}
	if err := m.IdentifyDisk(ctx, "sdb", 5); err != nil {
		t.Fatal(err)
	}
	select {
	case path := <-read:
		if path != "/dev/sdb" {
			t.Fatal(path)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("not read")
	}
	if err := m.IdentifyDisk(ctx, "sdc", 5); !errors.Is(err, ErrBusy) {
		t.Fatal("two at once")
	}
}
