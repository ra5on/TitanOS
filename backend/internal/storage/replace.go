package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

// The assistant for swapping a disk (docs/DISK-SWAP.md) uses AddDisk and
// RemoveDisk for failed and missing disks. For a disk that still works but
// should go (SMART warnings, a larger one), ReplaceDisk keeps the volume
// redundant all along, and IdentifyDisk helps to find the disk in the case.

// ReplaceDisk swaps a working member of an mdadm volume for a new disk: the
// new one joins as a spare and mdadm copies the old one onto it (mdadm
// --replace). The volume keeps its redundancy the whole time; when the copy
// is done mdadm marks the old member faulty and RemoveDisk takes it out.
// Everything on the new disk is destroyed. Btrfs volumes add the new disk
// and remove the old one instead.
func (m *Manager) ReplaceDisk(ctx context.Context, name, oldDisk, newDisk, identity string, erase bool) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthPending() {
		return ErrBusy
	}
	if !erase {
		return ErrNotConfirmed
	}
	volume, ok := m.find(name)
	if !ok {
		return ErrNotFound
	}
	if volume.Filesystem == "btrfs" {
		return fmt.Errorf("%w: on Btrfs add the new disk first and then remove the old one", ErrNotPossible)
	}
	if volume.Level == "raid0" {
		return fmt.Errorf("%w: RAID 0 cannot copy a disk while it runs", ErrNotPossible)
	}
	if oldDisk == newDisk {
		return fmt.Errorf("%w: the new disk is the old one", ErrNotPossible)
	}
	a, running := m.arrayOf(ctx, volume)
	if !running {
		return fmt.Errorf("%w: the volume is not running", ErrNotPossible)
	}
	if a.degraded > 0 {
		return fmt.Errorf("%w: a disk is missing; add the new disk to rebuild instead", ErrNotPossible)
	}
	if a.syncAction != "" && a.syncAction != "idle" {
		return fmt.Errorf("%w: wait until the %s has finished", ErrBusy, a.syncAction)
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return err
	}
	old, found := findDevice(devices, oldDisk)
	if !found {
		return fmt.Errorf("%w: %s not found", ErrDiskUnavailable, oldDisk)
	}
	oldPart := ""
	for _, member := range a.members {
		part := "/dev/" + member.device
		if slices.ContainsFunc(old.partitions(), func(p blockDevice) bool { return p.KName == part }) {
			if state := memberState(member); state != MemberActive {
				return fmt.Errorf("%w: %s is not an active part of the volume (%s)", ErrNotPossible, oldDisk, state)
			}
			oldPart = part
		}
	}
	if oldPart == "" {
		return fmt.Errorf("%w: %s is not part of %s", ErrDiskUnavailable, oldDisk, name)
	}
	slog.Info("replacing disk of volume", "name", name, "old", oldDisk, "new", newDisk)
	newPart, err := m.prepareMember(ctx, name, a, newDisk, identity)
	if err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "mdadm", "--manage", "/dev/"+a.device, "--add", newPart); err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "mdadm", "--manage", "/dev/"+a.device, "--replace", oldPart, "--with", newPart); err != nil {
		// The new disk stays as a spare: nothing is lost, it steps in when
		// a disk fails.
		return fmt.Errorf("%w: mdadm did not start copying: %v", ErrNotPossible, err)
	}
	return nil
}

// identifying is set while IdentifyDisk reads a disk; one at a time.
var identifying atomic.Bool

// identifyRead is how IdentifyDisk reads; tests replace it.
var identifyRead = readForLED

// IdentifyDisk reads from a disk for a while so that its activity light
// flickers and it can be found in the case. Read only; it wakes a sleeping
// disk. It returns at once; the reading runs in the background.
func (m *Manager) IdentifyDisk(ctx context.Context, diskName string, seconds int) error {
	if m.ReadOnly {
		return ErrReadOnly
	}
	if seconds < 5 || seconds > 120 {
		return fmt.Errorf("%w: between 5 and 120 seconds", ErrNotPossible)
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return err
	}
	device, found := findDevice(devices, diskName)
	if !found || device.Type != "disk" {
		return fmt.Errorf("%w: %s not found", ErrDiskUnavailable, diskName)
	}
	if !identifying.CompareAndSwap(false, true) {
		return ErrBusy
	}
	go func() {
		defer identifying.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds)*time.Second)
		defer cancel()
		if err := identifyRead(ctx, device.KName, device.Size); err != nil {
			slog.Warn("disk not read for identification", "disk", diskName, "err", err)
		}
	}()
	return nil
}

// readForLED reads 1 MiB blocks spread over the disk, past the page cache,
// until ctx ends; jumps keep the head busy and the light on.
func readForLED(ctx context.Context, path string, size uint64) error {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECT|unix.O_CLOEXEC, 0)
	if err != nil {
		return &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	const block = 1 << 20
	buffer, free := alignedBuffer(block)
	defer free()
	blocks := size / block
	if blocks == 0 {
		return errors.New("disk too small")
	}
	var step uint64
	for ctx.Err() == nil {
		offset := int64((step * 7919 % blocks) * block)
		if _, err := file.ReadAt(buffer, offset); err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		step++
	}
	return nil
}
