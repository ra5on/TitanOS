package storage

import (
	"testing"
	"time"
)

func TestCommandTimeouts(t *testing.T) {
	for _, c := range []struct {
		name string
		args []string
		want time.Duration
	}{
		{"umount", []string{"/mnt/tank"}, 2 * time.Minute},
		{"mdadm", []string{"--stop", "/dev/md127"}, 2 * time.Minute},
		{"mdadm", []string{"--create", "/dev/md/tank"}, 10 * time.Minute},
		{"udevadm", []string{"settle"}, 3 * time.Minute},
		{"dmsetup", []string{"suspend", "x"}, 5 * time.Minute},
		{"tune2fs", []string{"-l", "/dev/md127"}, time.Minute},
		// Grows with the size of the volume: no limit.
		{"tune2fs", []string{"-O", "quota,project"}, 0},
		{"mkfs.xfs", []string{"-f"}, 0},
		{"btrfs", []string{"device", "remove"}, 0},
		{"btrfs", []string{"device", "add", "-f"}, 0},
		{"btrfs", []string{"replace", "start", "-B"}, 0},
		{"btrfs", []string{"balance", "start", "--bg"}, 0},
		// Snapshots and queries are bounded: a stalled Btrfs must not
		// hold the actions lock for ever.
		{"btrfs", []string{"subvolume", "delete", "--commit-after", "x"}, 10 * time.Minute},
		{"btrfs", []string{"subvolume", "snapshot", "-r", "a", "b"}, 10 * time.Minute},
		{"btrfs", []string{"subvolume", "show", "x"}, 10 * time.Minute},
		{"btrfs", []string{"filesystem", "show", "--raw"}, 2 * time.Minute},
		{"btrfs", []string{"device", "scan"}, 2 * time.Minute},
		{"btrfs", []string{"replace", "status", "-1"}, 2 * time.Minute},
		{"blkdiscard", nil, 0},
	} {
		if got := commandTimeout(c.name, c.args); got != c.want {
			t.Errorf("%s %v: %v, want %v", c.name, c.args, got, c.want)
		}
	}
	if _, ok := NewManager(nil).runner.(secretRunner); !ok {
		t.Fatal("the real runner must pass secrets on descriptors")
	}
}
