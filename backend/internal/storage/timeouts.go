package storage

import (
	"context"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// Storage actions hold the actions lock and come from requests whose
// cancellation is ignored on purpose (a half-done action is worse). A
// command hanging on a broken disk (umount or mdadm --stop in D state)
// would then keep every later action waiting for ever. Each command gets a
// time limit fitting what it does; commands whose duration grows with the
// size of the volume (mkfs, resize, btrfs device work, discard) get none.
// run.Exec gives up on a process that does not end after being killed.

// commandTimeout is the time limit of a command, 0 for none.
func commandTimeout(name string, args []string) time.Duration {
	first := ""
	if len(args) > 0 {
		first = args[0]
	}
	switch name {
	case "blkid", "lsblk", "hd-idle":
		return time.Minute
	case "umount", "wipefs", "sfdisk", "partx", "blockdev", "smartctl":
		return 2 * time.Minute
	case "udevadm":
		return 3 * time.Minute
	case "dmsetup", "cryptsetup":
		// suspend waits for I/O in flight; argon2 takes seconds.
		return 5 * time.Minute
	case "mount":
		// Replaying a large journal can take a while.
		return 10 * time.Minute
	case "mdadm":
		if first == "--grow" || first == "--create" {
			return 10 * time.Minute
		}
		return 2 * time.Minute
	case "tune2fs":
		if first == "-l" {
			return time.Minute
		}
		return 0 // switching quotas on counts every inode
	}
	return 0
}

// timeoutRunner applies commandTimeout to every command it runs.
type timeoutRunner struct {
	run.Exec
}

func withCommandTimeout(ctx context.Context, name string, args []string) (context.Context, context.CancelFunc) {
	if limit := commandTimeout(name, args); limit > 0 {
		return context.WithTimeout(ctx, limit)
	}
	return ctx, func() {}
}

func (r timeoutRunner) Run(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	ctx, cancel := withCommandTimeout(ctx, name, args)
	defer cancel()
	return r.Exec.Run(ctx, stdin, name, args...)
}

func (r timeoutRunner) RunSecret(ctx context.Context, stdin []byte, extra [][]byte, name string, args ...string) (string, error) {
	ctx, cancel := withCommandTimeout(ctx, name, args)
	defer cancel()
	return r.Exec.RunSecret(ctx, stdin, extra, name, args...)
}
