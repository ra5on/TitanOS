package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const growthFile = "storage/growth.json"

type GrowthPointer struct {
	ID             string `json:"id"`
	Volume         string `json:"volume"`
	RecoveryVolume string `json:"recoveryVolume"`
}
type Growth struct {
	CurrentBytes uint64 `json:"currentBytes,omitempty"`
	PlannedBytes uint64 `json:"plannedBytes,omitempty"`
	GrowthPointer
	ArrayUUID     string    `json:"arrayUUID"`
	DiskSerial    string    `json:"diskSerial"`
	OriginalDisks int       `json:"originalDisks"`
	TargetDisks   int       `json:"targetDisks"`
	Phase         string    `json:"phase"`
	Error         string    `json:"error"`
	Started       time.Time `json:"started"`
}
type GrowthRequest struct {
	Disk           string `json:"disk"`
	Identity       string `json:"identity"`
	RecoveryVolume string `json:"recoveryVolume"`
	Erase          bool   `json:"erase"`
	Confirm        string `json:"confirm"`
}

func (m *Manager) loadGrowth() error {
	var p *GrowthPointer
	if err := m.store.ReadJSON(growthFile, &p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if p != nil && (!regexp.MustCompile(`^[0-9a-f]{24}$`).MatchString(p.ID) || !naming.ValidVolume(p.Volume) || !naming.ValidVolume(p.RecoveryVolume) || p.Volume == p.RecoveryVolume) {
		return errors.New("invalid growth recovery pointer")
	}
	m.mu.Lock()
	m.growth = p
	m.mu.Unlock()
	return nil
}
func (m *Manager) GrowthPending() bool { m.mu.RLock(); defer m.mu.RUnlock(); return m.growth != nil }
func (m *Manager) growthRoot(p GrowthPointer) (*os.Root, error) {
	return volumedata.Open(m.mountRoot, p.RecoveryVolume, "growth", m.Mounted())
}
func (m *Manager) GrowthStatus() (Growth, bool, error) {
	m.mu.RLock()
	p := m.growth
	m.mu.RUnlock()
	if p == nil {
		return Growth{}, false, nil
	}
	root, err := m.growthRoot(*p)
	if err != nil {
		return Growth{GrowthPointer: *p, Phase: "recovery-volume-missing"}, true, err
	}
	defer root.Close()
	var g Growth
	err = volumedata.ReadJSON(root, p.ID+".json", &g, 65536)
	return g, true, err
}
func (m *Manager) saveGrowth(root *os.Root, g *Growth, phase string) error {
	g.Phase = phase
	g.Error = ""
	return volumedata.WriteJSON(root, g.ID+".json", g)
}
func (m *Manager) GrowthPreview(ctx context.Context, name string, req GrowthRequest) (Growth, error) {
	v, ok := m.find(name)
	if !ok {
		return Growth{}, ErrNotFound
	}
	if m.ReadOnly {
		return Growth{}, ErrReadOnly
	}
	if m.GrowthPending() {
		return Growth{}, ErrBusy
	}
	if !slices.Contains([]string{"raid5", "raid6"}, v.Level) || !slices.Contains([]string{"ext4", "xfs"}, v.Filesystem) {
		return Growth{}, fmt.Errorf("%w: expansion adds one member to RAID5/6 with ext4/XFS", ErrNotPossible)
	}
	// The cache device has the size of the array it was set up with.
	if v.Cache != nil {
		return Growth{}, ErrCacheConfigured
	}
	a, ok := m.arrayOf(ctx, v)
	if !ok || a.degraded != 0 || a.syncAction != "idle" || !slices.Contains(m.Mounted(), name) {
		return Growth{}, ErrBusy
	}
	status, err := m.Status(ctx)
	if err != nil {
		return Growth{}, err
	}
	disk, err := m.usable(status, req.Disk)
	if err != nil {
		return Growth{}, err
	}
	if disk.Identity != req.Identity || disk.Serial == "" {
		return Growth{}, ErrDiskUnavailable
	}
	serials := 0
	for _, d := range status.Disks {
		if d.Serial == disk.Serial {
			serials++
		}
	}
	if serials != 1 {
		return Growth{}, ErrDiskUnavailable
	}
	var recovery *Volume
	for i := range status.Volumes {
		if status.Volumes[i].Name == req.RecoveryVolume {
			recovery = &status.Volumes[i]
		}
	}
	if recovery == nil || recovery.Name == name || !recovery.Mounted || recovery.State != VolumeOK {
		return Growth{}, errors.New("choose a healthy recovery volume on separate physical disks")
	}
	for _, d := range status.Disks {
		if d.Volume == req.RecoveryVolume && (d.Name == disk.Name || d.Volume == name) {
			return Growth{}, ErrNotPossible
		}
	}
	// Resolve physical membership from the discovered status, never from filesystem names alone.
	var sourceMembers []string
	for _, d := range status.Disks {
		if d.Volume == name {
			sourceMembers = append(sourceMembers, d.Name)
		}
	}
	for _, d := range recovery.Members {
		if slices.Contains(sourceMembers, d.Disk) || d.Disk == disk.Name {
			return Growth{}, ErrNotPossible
		}
	}
	size := m.growthPartitionMiB(a, status, name)
	if disk.SizeBytes <= reserveBytes+mebibyte || size == 0 || memberSizeMiB(disk.SizeBytes) < size {
		return Growth{}, ErrDiskTooSmall
	}
	devices, err := m.blockDevices(ctx)
	if err != nil {
		return Growth{}, err
	}
	if _, err := m.oldArrays(devices, []string{disk.Name}); err != nil {
		return Growth{}, err
	}
	var space syscall.Statfs_t
	if err := syscall.Statfs(m.MountPoint(req.RecoveryVolume), &space); err != nil {
		return Growth{}, err
	}
	if space.Bavail*uint64(space.Bsize) < 256<<20 {
		return Growth{}, errors.New("recovery volume needs 256 MiB free")
	}
	var current uint64
	for _, item := range status.Volumes {
		if item.Name == name {
			current = item.SizeBytes
		}
	}
	parity := 1
	if v.Level == "raid6" {
		parity = 2
	}
	planned := uint64(0)
	if a.disks > parity {
		planned = current / uint64(a.disks-parity) * uint64(a.disks+1-parity)
	}
	return Growth{GrowthPointer: GrowthPointer{Volume: name, RecoveryVolume: req.RecoveryVolume}, CurrentBytes: current, PlannedBytes: planned, ArrayUUID: v.ArrayUUID, DiskSerial: disk.Serial, OriginalDisks: a.disks, TargetDisks: a.disks + 1, Phase: "preview", Started: time.Now().UTC()}, nil
}

// growthPartitionMiB is the size of the partition a new member gets: the
// largest member partition (rounded up to MiB), or, if the kernel does not
// tell, what TitanOS would have created on the largest member disk.
func (m *Manager) growthPartitionMiB(a array, status Status, name string) uint64 {
	if sectors := m.memberSectors(a); sectors > 0 {
		return (sectors*512 + mebibyte - 1) / mebibyte
	}
	var size uint64
	for _, d := range status.Disks {
		if d.Volume == name {
			size = max(size, memberSizeMiB(d.SizeBytes))
		}
	}
	return size
}
func (m *Manager) StartGrowth(ctx context.Context, name string, req GrowthRequest) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if !req.Erase || req.Confirm != name {
		return ErrNotConfirmed
	}
	g, err := m.GrowthPreview(ctx, name, req)
	if err != nil {
		return err
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return err
	}
	g.ID = hex.EncodeToString(id[:])
	root, err := m.growthRoot(g.GrowthPointer)
	if err != nil {
		return err
	}
	if err := m.saveGrowth(root, &g, "preparing"); err != nil {
		root.Close()
		return err
	}
	if err := m.store.WriteJSON(growthFile, g.GrowthPointer); err != nil {
		root.Close()
		return err
	}
	m.mu.Lock()
	m.growth = &g.GrowthPointer
	m.growthActive = true
	m.mu.Unlock()
	go m.grow(context.WithoutCancel(ctx), root, g, req.Disk, false)
	return nil
}
func (m *Manager) grow(ctx context.Context, root *os.Root, g Growth, diskName string, resume bool) {
	defer root.Close()
	defer func() { m.mu.Lock(); m.growthActive = false; m.mu.Unlock() }()
	result := func() error {
		v, ok := m.find(g.Volume)
		if !ok || v.ArrayUUID != g.ArrayUUID {
			return ErrNotFound
		}
		a, ok := m.arrayOf(ctx, v)
		if !ok {
			return errors.New("array unavailable; recover it using the preserved backup file")
		}
		backup := filepath.Join(m.mountRoot, g.RecoveryVolume, ".titanos", "growth", g.ID+".backup")
		if !resume {
			devices, err := m.blockDevices(ctx)
			if err != nil {
				return err
			}
			status := m.status(ctx, devices, m.arrays(ctx))
			disk, err := m.usable(status, diskName)
			if err != nil || disk.Serial != g.DiskSerial {
				return ErrDiskUnavailable
			}
			// The same partition size the preview checked: as large as the
			// largest existing member partition, not the largest member disk.
			size := m.growthPartitionMiB(a, status, g.Volume)
			if size == 0 || memberSizeMiB(disk.SizeBytes) < size {
				return ErrDiskTooSmall
			}
			devices, err = m.releaseOldArrays(ctx, devices, []string{diskName})
			if err != nil {
				return err
			}
			device, ok := findDevice(devices, diskName)
			if !ok {
				return ErrDiskUnavailable
			}
			part, err := m.prepareDisk(ctx, device, g.Volume, size)
			if err != nil {
				return err
			}
			if err := m.saveGrowth(root, &g, "adding"); err != nil {
				return err
			}
			if _, err := m.runner.Run(ctx, "", "mdadm", "--manage", "/dev/"+a.device, "--add", part); err != nil {
				return err
			}
			if err := m.saveGrowth(root, &g, "starting"); err != nil {
				return err
			}
			if _, err := m.runner.Run(ctx, "", "mdadm", "--grow", "/dev/"+a.device, "--raid-devices="+strconv.Itoa(g.TargetDisks), "--backup-file="+backup); err != nil {
				return err
			}
		} else if a.syncAction == "frozen" {
			if _, err := m.runner.Run(ctx, "", "mdadm", "--grow", "/dev/"+a.device, "--continue", "--backup-file="+backup); err != nil {
				return err
			}
		} else if a.disks == g.OriginalDisks {
			// An acknowledged interrupted setup may continue only if the new member is
			// already present as a spare. Never repeat partitioning or --add after a crash.
			status, err := m.Status(ctx)
			if err != nil {
				return err
			}
			present := false
			for _, d := range status.Disks {
				if d.Volume == g.Volume && d.Serial == g.DiskSerial && d.MemberState == MemberSpare {
					present = true
				}
			}
			if !present {
				return errors.New("new member is not a verified spare; inspect the disks before recovery")
			}
			if err := m.saveGrowth(root, &g, "starting"); err != nil {
				return err
			}
			if _, err := m.runner.Run(ctx, "", "mdadm", "--grow", "/dev/"+a.device, "--raid-devices="+strconv.Itoa(g.TargetDisks), "--backup-file="+backup); err != nil {
				return err
			}
		}
		if err := m.saveGrowth(root, &g, "reshaping"); err != nil {
			return err
		}
		for {
			if !slices.Contains(m.Mounted(), g.RecoveryVolume) {
				return volumedata.ErrUnavailable
			}
			a, ok = m.arrayOf(ctx, v)
			if !ok || a.degraded > 0 {
				return errors.New("array disappeared or became degraded")
			}
			if a.syncAction == "idle" && a.disks == g.TargetDisks {
				break
			}
			if a.syncAction != "reshape" && a.syncAction != "recover" && a.syncAction != "resync" {
				return fmt.Errorf("reshape has not completed: %s (%d members)", a.syncAction, a.disks)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(5 * time.Second):
			}
		}
		if err := m.saveGrowth(root, &g, "filesystem"); err != nil {
			return err
		}
		if !slices.Contains(m.Mounted(), g.Volume) {
			if err := m.mount(ctx, v); err != nil {
				return err
			}
		}
		fsDevice := "/dev/" + a.device
		if v.Encryption != nil {
			// LUKS grows with the array first (no password needed: the
			// key is in the dm-crypt table, opened with --disable-keyring).
			name := cryptName(v, "")
			if _, err := m.runner.Run(ctx, "", "cryptsetup", "resize", name); err != nil {
				return err
			}
			fsDevice = m.mapperPath(name)
		}
		if v.Filesystem == "ext4" {
			_, err := m.runner.Run(ctx, "", "resize2fs", fsDevice)
			if err != nil {
				return err
			}
		} else {
			if _, err := m.runner.Run(ctx, "", "xfs_growfs", m.MountPoint(v.Name)); err != nil {
				return err
			}
		}
		if err := m.saveGrowth(root, &g, "done"); err != nil {
			return err
		}
		if err := m.store.WriteJSON(growthFile, nil); err != nil {
			return err
		}
		m.mu.Lock()
		m.growth = nil
		m.mu.Unlock()
		return nil
	}()
	if result != nil {
		g.Error = result.Error()
		_ = volumedata.WriteJSON(root, g.ID+".json", g)
	}
}
func (m *Manager) RecoverGrowth(ctx context.Context, confirm string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	g, ok, err := m.GrowthStatus()
	if err != nil {
		return err
	}
	if !ok || confirm != g.Volume {
		return ErrNotConfirmed
	}
	m.mu.Lock()
	active := m.growthActive
	m.mu.Unlock()
	if active {
		return ErrBusy
	}
	v, ok := m.find(g.Volume)
	if !ok || v.ArrayUUID != g.ArrayUUID {
		return ErrNotFound
	}
	root, err := m.growthRoot(g.GrowthPointer)
	if err != nil {
		return err
	}
	if _, ok := m.arrayOf(ctx, v); !ok {
		backup := filepath.Join(m.mountRoot, g.RecoveryVolume, ".titanos", "growth", g.ID+".backup")
		if st, err := root.Lstat(g.ID + ".backup"); err != nil || !st.Mode().IsRegular() {
			root.Close()
			return errors.New("original reshape backup is unavailable; refusing assembly")
		}
		if _, err := m.runner.Run(ctx, "", "mdadm", "--assemble", "--uuid="+g.ArrayUUID, "--backup-file="+backup, "--freeze-reshape", "/dev/md/"+g.Volume); err != nil {
			root.Close()
			return err
		}
	}
	m.mu.Lock()
	m.growthActive = true
	m.mu.Unlock()
	go m.grow(context.WithoutCancel(ctx), root, g, "", true)
	return nil
}

func (m *Manager) GrowthActive() bool { m.mu.RLock(); defer m.mu.RUnlock(); return m.growthActive }

// CancelGrowth only releases an unstarted preparation. A reshape can never be
// cancelled by deleting its recovery journal or pretending the old geometry won.
func (m *Manager) CancelGrowth(ctx context.Context, confirm string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if m.ReadOnly {
		return ErrReadOnly
	}
	if m.GrowthActive() {
		return ErrBusy
	}
	g, ok, err := m.GrowthStatus()
	if err != nil {
		return err
	}
	if !ok || confirm != g.Volume {
		return ErrNotConfirmed
	}
	if !slices.Contains([]string{"preparing", "adding", "starting", "aborting"}, g.Phase) {
		return ErrNotPossible
	}
	v, ok := m.find(g.Volume)
	if !ok || v.ArrayUUID != g.ArrayUUID {
		return ErrNotFound
	}
	a, ok := m.arrayOf(ctx, v)
	if !ok || a.disks != g.OriginalDisks || a.degraded != 0 || a.syncAction != "idle" {
		return ErrBusy
	}
	position, err := os.ReadFile(filepath.Join(m.sysBlock, a.device, "md", "reshape_position"))
	if err != nil || strings.TrimSpace(string(position)) != "none" {
		return ErrBusy
	}
	status, err := m.Status(ctx)
	if err != nil {
		return err
	}
	newName := ""
	for _, d := range status.Disks {
		if d.Serial == g.DiskSerial && d.Volume == g.Volume {
			if d.MemberState != MemberSpare {
				return ErrNotPossible
			}
			newName = d.Name
		}
	}
	root, err := m.growthRoot(g.GrowthPointer)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := m.saveGrowth(root, &g, "aborting"); err != nil {
		return err
	}
	if newName != "" {
		devices, err := m.blockDevices(ctx)
		if err != nil {
			return err
		}
		device, ok := findDevice(devices, newName)
		if !ok {
			return ErrDiskUnavailable
		}
		part := ""
		device.walk(func(node blockDevice) {
			for _, member := range a.members {
				if filepath.Base(node.Name) == member.device && memberState(member) == MemberSpare {
					part = node.Name
				}
			}
		})
		if part == "" {
			return ErrDiskUnavailable
		}
		if _, err := m.runner.Run(ctx, "", "mdadm", "--manage", "/dev/"+a.device, "--remove", part); err != nil {
			return err
		}
	}
	if err := m.saveGrowth(root, &g, "cancelled"); err != nil {
		return err
	}
	if err := m.store.WriteJSON(growthFile, nil); err != nil {
		return err
	}
	m.mu.Lock()
	m.growth = nil
	m.mu.Unlock()
	return nil
}
