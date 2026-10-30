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
