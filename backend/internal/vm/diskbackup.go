package vm

// Consistent copies of the disks of running VMs for backups, where the
// volume has no snapshots of its own (ext4, XFS):
//
//   - qcow2 images: an external, disk-only libvirt snapshot. The guest
//     writes to an overlay next to each disk while the image itself stands
//     still and is copied; then the overlay is committed back into the
//     image (blockcommit with pivot) and removed.
//   - other images (raw), only if the job allows it: the VM is paused,
//     copied and resumed, with a hard upper time limit.
//
// A VM is never left paused or running on an overlay. What is in flight is
// written to .titanos/vmbackup on the VM's volume before anything is
// started; ending the copy, cancelling it, a failed copy, the time limit, a
// start of titand, the start of every copy run and the start of the VM
// itself all run the same undo from that record. Only what a record names
// is ever committed or removed: a VM with snapshots or a backing chain of
// its own, or with a whole disk of the NAS, is not touched at all.

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const (
	diskBackupFeature = "vmbackup"
	// OverlaySuffix ends the name of the overlay a backup puts next to a
	// disk; backups never copy such a file.
	OverlaySuffix = ".titanos-overlay"
)

// Ways a disk of a running VM can be copied consistently.
const (
	DiskWaySnapshot = "libvirt_snapshot"
	DiskWayPause    = "pause"
)

// Why a way is not available: codes the backup reports and the page
// translates (backup.problem.<code>).
const (
	DiskBusy           = "vm_disk_busy"            // another operation of the VM runs, or it is not in the state "running"
	DiskPassthrough    = "vm_disk_passthrough"     // the VM has a whole disk of the NAS
	DiskBackingChain   = "vm_disk_backing_chain"   // an image builds on another file (snapshots of its own)
	DiskNoSpace        = "vm_disk_no_space"        // no room for the overlay next to the disk
	DiskRawImage       = "vm_disk_raw_image"       // not qcow2: only pausing would do, and the job does not allow it
	DiskSnapshotFailed = "vm_disk_snapshot_failed" // libvirt refused the snapshot; nothing was changed
	DiskPauseFailed    = "vm_disk_pause_failed"    // libvirt refused to pause; nothing was changed
	DiskUndoFailed     = "vm_disk_undo_failed"     // the overlay or the pause could not be undone yet; it is tried again
)

// DiskBackupError is why BeginDiskBackup or End did not work, as a code.
type DiskBackupError struct {
	Code string
	Err  error
}

func (e *DiskBackupError) Error() string {
	if e.Err == nil {
		return e.Code
	}
	return e.Code + ": " + e.Err.Error()
}
func (e *DiskBackupError) Unwrap() error       { return e.Err }
func (e *DiskBackupError) ProblemCode() string { return e.Code }

// pauseLimit is the longest a backup keeps a VM paused; diskUndoLimit bounds
// committing an overlay back. Variables for tests.
var (
	// yieldLimit is how long stopping a VM waits for a backup to let go.
	yieldLimit    = 2 * time.Minute
	pauseLimit    = 15 * time.Minute
	diskUndoLimit = 30 * time.Minute
	// diskFree is the free space of the file system holding dir.
	diskFree = func(dir string) (uint64, error) {
		var st syscall.Statfs_t
		if err := syscall.Statfs(dir, &st); err != nil {
			return 0, err
		}
		return st.Bavail * uint64(st.Bsize), nil
	}
)

// overlayReserve is the room an overlay must find next to a disk of size
// bytes: it takes what the guest writes while the image is copied, at most
// the size of the disk. A tenth of the disk, at least 4 GiB.
func overlayReserve(size int64) uint64 {
	if size <= 0 {
		return 4 << 30
	}
	return uint64(min(size, max(4<<30, size/10)))
}

// FileDisk is an image file of a VM, for backups.
type FileDisk struct {
	VM        string `json:"vm"`
	Path      string `json:"path"`
	Format    string `json:"format"`
	SizeBytes int64  `json:"sizeBytes"`
	// Running: the VM runs or starts, the file changes all the time.
	Running bool `json:"running"`
}

// FileDisks lists the image files of all VMs with the state of their VM,
// from RAM (and the pid files); no disk is read.
func (m *Manager) FileDisks() []FileDisk {
	m.mu.Lock()
	defer m.mu.Unlock()
	var disks []FileDisk
	for name, vm := range m.vms {
		running := !m.Disabled && (m.starting[name] || m.pid(name) != 0)
		for _, disk := range vm.Disks {
			if !m.isBlock(disk) {
				disks = append(disks, FileDisk{VM: name, Path: filepath.Clean(m.diskPath(vm, disk)), Format: disk.Format, SizeBytes: disk.SizeBytes, Running: running})
			}
		}
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Path < disks[j].Path })
	return disks
}

// DiskPlan is how the disks of a running VM could be copied right now.
type DiskPlan struct {
	// Way is DiskWaySnapshot, DiskWayPause or "" (none).
	Way string `json:"way"`
	// Agent: the QEMU guest agent answers, the snapshot can quiesce the
	// guest's file systems.
	Agent bool `json:"agent"`
	// Reason is why the snapshot is not possible (a Disk… code); set with
	// Way "" and with DiskWayPause.
	Reason string `json:"reason,omitempty"`
	// NeedBytes and FreeBytes are set for DiskNoSpace.
	NeedBytes uint64 `json:"needBytes,omitempty"`
	FreeBytes uint64 `json:"freeBytes,omitempty"`
}

// diskTargets are the device names the guest's disks have in the domain,
// in the order of vm.Disks (as domainXML numbers them).
func diskTargets(vm VM) []string {
	prefix := "sd"
	if vm.Template == "linux" {
		prefix = "vd"
	}
	targets := make([]string, len(vm.Disks))
	for i := range vm.Disks {
		targets[i] = prefix + string(rune('a'+i))
	}
	return targets
}

// PlanDiskBackup tells how the disks of a running VM could be copied
// consistently right now. It changes nothing: it reads the headers of the
// images, the free space and asks the guest agent.
func (m *Manager) PlanDiskBackup(ctx context.Context, name string, allowPause bool) DiskPlan {
	vm, err := m.get(name)
	if err != nil || m.Disabled || m.pid(name) == 0 {
		return DiskPlan{Reason: DiskBusy}
	}
	m.mu.Lock()
	busy := m.jobs[name] != nil || m.starting[name]
	if job := m.snapshotJobs[name]; job != nil && job.State == "running" {
		busy = true
	}
	m.mu.Unlock()
	if busy || m.pendingSnapshot(vm) {
		return DiskPlan{Reason: DiskBusy}
	}
	raw := false
	for _, disk := range vm.Disks {
		if m.isBlock(disk) {
			return DiskPlan{Reason: DiskPassthrough}
		}
		raw = raw || disk.Format != "qcow2"
	}
	if len(vm.Disks) == 0 {
		return DiskPlan{Reason: DiskBusy}
	}
	paused := func(reason string) DiskPlan {
		if allowPause {
			return DiskPlan{Way: DiskWayPause, Reason: reason}
		}
		return DiskPlan{Reason: reason}
	}
	if raw {
		return paused(DiskRawImage)
	}
	need := map[string]uint64{}
	for _, disk := range vm.Disks {
		path := m.diskPath(vm, disk)
		// -U: the running QEMU holds the image; only its header is read.
		out, err := m.runner.Run(ctx, "", "qemu-img", "info", "-U", "--output=json", "-f", "qcow2", path)
		var info struct {
			VirtualSize int64  `json:"virtual-size"`
			Backing     string `json:"backing-filename"`
		}
		var whole any
		if err != nil || json.Unmarshal([]byte(out), &info) != nil || json.Unmarshal([]byte(out), &whole) != nil {
			return DiskPlan{Reason: DiskSnapshotFailed}
		}
		if info.Backing != "" || hasDataFile(whole) {
			// Snapshots or a chain of its own: never touched, also not paused.
			return DiskPlan{Reason: DiskBackingChain}
		}
		size := disk.SizeBytes
		if size <= 0 {
			size = info.VirtualSize
		}
		need[filepath.Dir(path)] += overlayReserve(size)
	}
	for dir, bytes := range need {
		free, err := diskFree(dir)
		if err != nil || free < bytes {
			return DiskPlan{Reason: DiskNoSpace, NeedBytes: bytes, FreeBytes: free}
		}
	}
	ask, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = m.runner.Run(ask, "", "virsh", "--quiet", "qemu-agent-command", name, `{"execute":"guest-ping"}`, "--timeout", "5")
	return DiskPlan{Way: DiskWaySnapshot, Agent: err == nil}
}

// diskBackupRecord is what is in flight for one VM, on its volume.
type diskBackupRecord struct {
	ID      string           `json:"id"`
	VM      string           `json:"vm"`
	UUID    string           `json:"uuid"`
	Way     string           `json:"way"`
	Started time.Time        `json:"started"`
	Disks   []diskBackupDisk `json:"disks,omitempty"`
}

type diskBackupDisk struct {
	Target  string `json:"target"`
	Base    string `json:"base"`
	Overlay string `json:"overlay"`
}

var diskBackupFile = regexp.MustCompile(`^[0-9a-f-]{36}\.json$`)

func (m *Manager) diskBackupRoot(volume string) (*os.Root, error) {
	return volumedata.Open(m.mountRoot, volume, diskBackupFeature, m.Mounted())
}

func (m *Manager) writeDiskBackup(volume string, rec diskBackupRecord) error {
	root, err := m.diskBackupRoot(volume)
	if err != nil {
		return err
	}
	defer root.Close()
	return volumedata.WriteJSON(root, rec.UUID+".json", rec)
}

func (m *Manager) removeDiskBackup(volume, uuid string) error {
	root, err := m.diskBackupRoot(volume)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := root.Remove(uuid + ".json"); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if dir, err := root.Open("."); err == nil {
		defer dir.Close()
		return dir.Sync()
	}
	return nil
}

// DiskBackup is a running consistent copy of the disks of one VM. The
// images stand still until End.
type DiskBackup struct {
	m      *Manager
	vm     VM
	lock   *sync.Mutex
	record diskBackupRecord
	// Way is DiskWaySnapshot or DiskWayPause.
	Way string
	// Quiesced: the guest agent froze the guest's file systems for the
	// snapshot. Without it the copy is like the disk after a power cut.
	Quiesced bool

	ctx    context.Context
	cancel context.CancelFunc
	timer  *time.Timer
	once   sync.Once
	err    error
	// expired: the pause reached its limit and was ended; the copy is not complete.
	expired bool
	mu      sync.Mutex
}

// Method is the way used and whether the guest's file systems were frozen.
func (b *DiskBackup) Method() (way string, quiesced bool) { return b.Way, b.Quiesced }

// Context ends when the copy must stop: the caller's context ended, or
// the pause reached its limit (Expired).
func (b *DiskBackup) Context() context.Context { return b.ctx }

// Expired tells that the pause was ended by its time limit.
func (b *DiskBackup) Expired() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.expired
}

// BeginDiskBackup makes the image files of a running VM stand still: with
// a libvirt snapshot (qcow2), or by pausing it if allowPause and nothing
// better works. The caller copies the images and calls End, always.
// A *DiskBackupError says why it did not start; nothing is changed then.
func (m *Manager) BeginDiskBackup(ctx context.Context, name string, allowPause bool) (*DiskBackup, error) {
	if m.Disabled {
		return nil, &DiskBackupError{Code: DiskBusy, Err: ErrDisabled}
	}
	lock, err := m.operation(name)
	if err != nil {
		return nil, &DiskBackupError{Code: DiskBusy, Err: err}
	}
	fail := func(code string, err error) (*DiskBackup, error) {
		lock.Unlock()
		return nil, &DiskBackupError{Code: code, Err: err}
	}
	vm, err := m.get(name)
	if err != nil {
		return fail(DiskBusy, err)
	}
	// What an earlier run left for this VM goes first.
	if err := m.finishDiskBackupOf(ctx, vm); err != nil {
		return fail(DiskUndoFailed, err)
	}
	plan := m.PlanDiskBackup(ctx, name, allowPause)
	if plan.Way == "" {
		return fail(plan.Reason, nil)
	}
	state, err := m.runner.Run(ctx, "", "virsh", "--quiet", "domstate", name)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(state), "running") {
		// Paused by its owner, by an I/O error, shutting down: left alone.
		return fail(DiskBusy, err)
	}
	rec := diskBackupRecord{ID: rand.Text(), VM: name, UUID: vm.UUID, Way: plan.Way, Started: m.clock().UTC()}
	if plan.Way == DiskWaySnapshot {
		targets := diskTargets(vm)
		for i, disk := range vm.Disks {
			base := m.diskPath(vm, disk)
			rec.Disks = append(rec.Disks, diskBackupDisk{Target: targets[i], Base: base, Overlay: base + "." + strings.ToLower(rec.ID[:8]) + OverlaySuffix})
		}
	}
	// On the disk before anything is started.
	if err := m.writeDiskBackup(vm.Volume, rec); err != nil {
		return fail(DiskSnapshotFailed, err)
	}
	b := &DiskBackup{m: m, vm: vm, lock: lock, record: rec, Way: plan.Way}
	m.mu.Lock()
	if m.diskBackups == nil {
		m.diskBackups = map[string]*DiskBackup{}
	}
	b.ctx, b.cancel = context.WithCancel(ctx)
	m.diskBackups[vm.UUID] = b
	m.mu.Unlock()
	abort := func(code string, err error) (*DiskBackup, error) {
		// The same undo as after a copy: whatever libvirt did or did not do.
		if undo := b.End(); undo != nil {
			return nil, &DiskBackupError{Code: DiskUndoFailed, Err: errors.Join(err, undo)}
		}
		return nil, &DiskBackupError{Code: code, Err: err}
	}
	switch plan.Way {
	case DiskWaySnapshot:
		args := []string{"--quiet", "snapshot-create-as", name, "--disk-only", "--atomic", "--no-metadata"}
		for _, disk := range rec.Disks {
			args = append(args, "--diskspec", disk.Target+",snapshot=external,file="+disk.Overlay)
		}
		err := errors.New("no guest agent")
		if plan.Agent {
			if _, err = m.runner.Run(ctx, "", "virsh", append(slices0(args), "--quiesce")...); err == nil {
				b.Quiesced = true
			} else {
				slog.Warn("VM snapshot for a backup: the guest did not freeze its file systems; taken without", "vm", name, "err", err)
			}
		}
		if err != nil {
			if _, err = m.runner.Run(ctx, "", "virsh", args...); err != nil {
				return abort(DiskSnapshotFailed, err)
			}
		}
	case DiskWayPause:
		if _, err := m.runner.Run(ctx, "", "virsh", "--quiet", "suspend", name); err != nil {
			return abort(DiskPauseFailed, err)
		}
		b.timer = time.AfterFunc(pauseLimit, func() {
			b.mu.Lock()
			b.expired = true
			b.mu.Unlock()
			slog.Warn("VM paused for a backup reached the time limit; resumed, its disks are not saved by this run", "vm", name, "limit", pauseLimit)
			_ = b.End() // reported by the caller's own End
		})
	}
	slog.Info("VM disks stand still for a backup", "vm", name, "way", plan.Way, "quiesced", b.Quiesced)
	return b, nil
}

// slices0 copies a list of arguments, so that appending to it leaves the
// original alone.
func slices0(args []string) []string { return append([]string(nil), args...) }

// End undoes what BeginDiskBackup did: the overlay is committed back and
// removed, or the VM resumed. It is safe to call more than once and after
// the context ended; every later call returns the first result. If the
// undo fails the record stays, and it is tried again when titand starts,
// when the next copy run begins and before the VM is started.
func (b *DiskBackup) End() error {
	b.once.Do(func() {
		if b.timer != nil {
			b.timer.Stop()
		}
		b.cancel()
		ctx, cancel := context.WithTimeout(context.Background(), diskUndoLimit)
		defer cancel()
		b.err = b.m.undoDiskBackup(ctx, b.vm.Volume, b.record)
		b.m.mu.Lock()
		delete(b.m.diskBackups, b.vm.UUID)
		b.m.mu.Unlock()
		b.lock.Unlock()
		if b.err != nil {
			slog.Error("VM not back to normal after a backup; tried again at the next start or backup", "vm", b.vm.Name, "way", b.Way, "err", b.err)
			if b.m.Notify != nil {
				b.m.Notify("error", "vm_backup_undo_failed", b.vm.Name)
			}
			b.err = &DiskBackupError{Code: DiskUndoFailed, Err: b.err}
		}
	})
	return b.err
}

// yieldDiskBackup tells a backup that holds the disks of a VM to let go,
// because the VM is to be shut down or switched off: the backup stops
// copying this VM's disks and undoes what it did. It reports whether a
// backup held the VM; the caller then waits for the operation lock.
func (m *Manager) yieldDiskBackup(name string) bool {
	vm, err := m.get(name)
	if err != nil {
		return false
	}
	m.mu.Lock()
	b := m.diskBackups[vm.UUID]
	m.mu.Unlock()
	if b == nil {
		return false
	}
	slog.Info("backup lets go of a VM that is to be stopped", "vm", name)
	b.cancel()
	return true
}

// undoDiskBackup brings a VM back to normal from its record and removes
// the record. Only what the record names is touched. The caller holds the
// operation lock of the VM.
func (m *Manager) undoDiskBackup(ctx context.Context, volume string, rec diskBackupRecord) error {
	running := m.pid(rec.VM) != 0
	switch rec.Way {
	case DiskWayPause:
		if running {
			state, err := m.runner.Run(ctx, "", "virsh", "--quiet", "domstate", rec.VM)
			if err != nil {
				return err
			}
			if strings.HasPrefix(strings.TrimSpace(state), "paused") {
				if _, err := m.runner.Run(ctx, "", "virsh", "--quiet", "resume", rec.VM); err != nil {
					return err
				}
			}
		}
	case DiskWaySnapshot:
		list := ""
		if running {
			var err error
			if list, err = m.runner.Run(ctx, "", "virsh", "--quiet", "domblklist", rec.VM); err != nil {
				return err
			}
		}
		for _, disk := range rec.Disks {
			if !strings.HasPrefix(filepath.Base(disk.Overlay), filepath.Base(disk.Base)+".") || !strings.HasSuffix(disk.Overlay, OverlaySuffix) || filepath.Dir(disk.Overlay) != filepath.Dir(disk.Base) {
				return fmt.Errorf("record of %s names %q, which is no overlay of a backup", rec.VM, disk.Overlay)
			}
			info, statErr := os.Lstat(disk.Overlay)
			exists := statErr == nil && info.Mode().IsRegular()
			switch {
			case running && strings.Contains(list, disk.Overlay):
				// The guest writes to the overlay: back into the image, and
				// the guest back onto the image.
				if _, err := m.runner.Run(ctx, "", "virsh", "--quiet", "blockcommit", rec.VM, disk.Target, "--active", "--wait", "--pivot"); err != nil {
					return err
				}
			case !running && exists:
				// The VM stopped on the overlay (or the NAS went down): what
				// it wrote there goes into the image before it starts again.
				// Committing what is committed already changes nothing.
				if _, err := m.runner.Run(ctx, "", "qemu-img", "commit", "-d", "-f", "qcow2", disk.Overlay); err != nil {
					return err
				}
			}
			if exists {
				if err := os.Remove(disk.Overlay); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
		}
	}
	return m.removeDiskBackup(volume, rec.UUID)
}

// finishDiskBackupOf undoes what a backup left for one VM, if anything.
// The caller holds the operation lock of the VM.
func (m *Manager) finishDiskBackupOf(ctx context.Context, vm VM) error {
	file := filepath.Join(m.mountRoot, vm.Volume, ".titanos", diskBackupFeature, vm.UUID+".json")
	if _, err := os.Lstat(file); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	m.mu.Lock()
	active := m.diskBackups[vm.UUID]
	m.mu.Unlock()
	if active != nil {
		return ErrSnapshotBusy
	}
	root, err := m.diskBackupRoot(vm.Volume)
	if err != nil {
		return err
	}
	var rec diskBackupRecord
	err = volumedata.ReadJSON(root, vm.UUID+".json", &rec, 1<<20)
	root.Close()
	if err != nil {
		return fmt.Errorf("record of an unfinished backup of %s: %w", vm.Name, err)
	}
	if rec.UUID != vm.UUID || rec.VM != vm.Name {
		return fmt.Errorf("record of an unfinished backup does not belong to %s", vm.Name)
	}
	if err := m.undoDiskBackup(ctx, vm.Volume, rec); err != nil {
		return err
	}
	slog.Warn("VM brought back to normal after an interrupted backup", "vm", vm.Name, "way", rec.Way)
	return nil
}

// RecoverDiskBackups undoes what interrupted backups left behind: a VM
// still paused is resumed, an overlay is committed back and removed. It
// looks at the volumes of the VMs only, once per start unless force is
// set (the start of a copy run) or the last look failed. VMs with another
// operation running are left for the next time.
func (m *Manager) RecoverDiskBackups(ctx context.Context, force bool) error {
	if m.Disabled || m.Mounted == nil {
		return nil
	}
	mounted := m.Mounted()
	m.mu.Lock()
	byVolume := map[string][]VM{}
	for _, vm := range m.vms {
		byVolume[vm.Volume] = append(byVolume[vm.Volume], vm)
	}
	m.mu.Unlock()
	var errs []error
	for volume, vms := range byVolume {
		m.mu.Lock()
		checked := m.diskBackupChecked[volume]
		m.mu.Unlock()
		if !containsString(mounted, volume) || checked && !force {
			continue
		}
		entries, err := os.ReadDir(filepath.Join(m.mountRoot, volume, ".titanos", diskBackupFeature))
		failed := false
		if err == nil {
			for _, entry := range entries {
				if !diskBackupFile.MatchString(entry.Name()) {
					continue
				}
				uuid := strings.TrimSuffix(entry.Name(), ".json")
				for _, vm := range vms {
					if vm.UUID != uuid {
						continue
					}
					lock, err := m.operation(vm.Name)
					if err != nil {
						failed = true // busy (its own backup runs, say): next time
						continue
					}
					err = m.finishDiskBackupOf(ctx, vm)
					lock.Unlock()
					if err != nil {
						failed = true
						errs = append(errs, fmt.Errorf("%s: %w", vm.Name, err))
						slog.Error("VM not brought back to normal after an interrupted backup", "vm", vm.Name, "err", err)
					}
				}
			}
		}
		m.mu.Lock()
		if m.diskBackupChecked == nil {
			m.diskBackupChecked = map[string]bool{}
		}
		m.diskBackupChecked[volume] = !failed
		m.mu.Unlock()
	}
	return errors.Join(errs...)
}

func containsString(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
