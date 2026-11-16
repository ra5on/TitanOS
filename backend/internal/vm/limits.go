package vm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Time limits for libvirt (decided for 1.03): a single virsh command that
// asks or switches something (state, start, stop, pause, the block chain,
// a pivot) gets virshLimit. A hanging libvirt then no longer holds the
// operation lock of a VM, the loop of the VMs (schedules, autostart, I/O
// error check, return of devices) or a request for good. Committing an
// overlay back (blockcommit --wait) may take long for a large overlay and
// keeps the limit of an undo attempt (diskUndoLimit): the block job runs on
// in QEMU and the next attempt takes it up. Saving and restoring the
// memory of a VM (snapshots) are not bounded here: they write and read as
// much as the VM has RAM.
var virshLimit = 2 * time.Minute

// ErrLibvirtTimeout: libvirt did not answer a command within its limit.
var ErrLibvirtTimeout = errors.New("vm: libvirt did not answer in time")

// virsh runs one virsh command with virshLimit.
func (m *Manager) virsh(ctx context.Context, args ...string) (string, error) {
	return m.virshWithin(ctx, virshLimit, args...)
}

// virshWithin runs one virsh command with its own time limit. A command
// cut off by that limit (not by ctx) is reported as ErrLibvirtTimeout,
// together with what the command said.
func (m *Manager) virshWithin(ctx context.Context, limit time.Duration, args ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	out, err := m.runner.Run(cctx, "", "virsh", args...)
	if err != nil && errors.Is(cctx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
		return out, fmt.Errorf("%w (virsh %s, limit %s): %w", ErrLibvirtTimeout, strings.Join(args, " "), limit, err)
	}
	return out, err
}
