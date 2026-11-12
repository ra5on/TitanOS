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
// start of titand, the start of every copy run, every operation on the
// VM's disks and the start of the VM itself all run the same undo from
// that record. Only what a record names is ever committed or removed: a VM
// with snapshots or a backing chain of its own, or with a whole disk of
// the NAS, is not touched at all.
//
// The undo trusts only what libvirt says about the running VM. An overlay
// is removed only after the live block chain no longer names it; while it
// cannot be told (a snapshot command that was cut off, a block job still
// running) the record stays and it is tried again. A block job that an
// earlier attempt left in QEMU is waited for and finished, never started
// a second time and never aborted.

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
	"slices"
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
	DiskBusy           = "vm_disk_busy"            // another operation of the VM runs, it is not in the state "running", or it is being stopped
	DiskPassthrough    = "vm_disk_passthrough"     // the VM has a whole disk of the NAS
	DiskBackingChain   = "vm_disk_backing_chain"   // an image builds on another file (snapshots of its own)
	DiskNoSpace        = "vm_disk_no_space"        // no room (any more) for the overlay next to the disk
	DiskRawImage       = "vm_disk_raw_image"       // not qcow2: only pausing would do
	DiskSnapshotFailed = "vm_disk_snapshot_failed" // libvirt refused the snapshot; nothing was changed
	DiskPauseFailed    = "vm_disk_pause_failed"    // libvirt refused to pause; nothing was changed
	DiskPauseTimeout   = "vm_disk_pause_timeout"   // the pause reached its time limit
	DiskUndoFailed     = "vm_disk_undo_failed"     // the overlay or the pause could not be undone yet; it is tried again
	// DiskSnapshotUnproven: a libvirt snapshot would work, but the job did
	// not ask for it. The way is tested with a stand-in for libvirt only
	// (docs/rules/VMS-LXC.md), so it is used only where a job says so.
	DiskSnapshotUnproven = "vm_disk_snapshot_unproven"
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

// Limits and intervals; variables for tests.
var (
	// yieldLimit is how long stopping a VM waits for a backup to let go.
	yieldLimit = 2 * time.Minute
	// pauseLimit is the longest a backup keeps a VM paused.
	pauseLimit = 15 * time.Minute
	// diskUndoLimit bounds one attempt to commit an overlay back. A block
	// job that outlasts it runs on in QEMU and is taken up by the next
	// attempt.
	diskUndoLimit = 30 * time.Minute
	// snapshotLimit bounds the snapshot command. It never runs on the
	// context of the backup: cancelling a backup must not kill virsh
	// while libvirt is in the middle of switching the disks.
	snapshotLimit = 3 * time.Minute
	// snapshotSettle is how old a record must be before a look at the VM
	// decides that an overlay it does not use was never taken into use
	// (a snapshot command that was cut off may still be finishing).
	snapshotSettle = 2 * snapshotLimit
	// blockJobPoll is how often a running block job is looked at.
	blockJobPoll = 2 * time.Second
	// spaceWatch is how often the room next to the overlays is looked at
	// while a snapshot is held; below overlayLowWater the copy is stopped
	// and the overlay committed back before the volume fills up.
	spaceWatch      = 5 * time.Second
	overlayLowWater = uint64(1 << 30)
	// planMaxAge is how long PlanDiskBackupCached reuses an answer.
	planMaxAge = 30 * time.Second
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

// Running tells whether a VM runs or starts right now (RAM and pid file).
func (m *Manager) Running(name string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.Disabled && (m.starting[name] || m.pid(name) != 0)
}

// DiskBackupOptions are what a job allows for the disks of a running VM.
type DiskBackupOptions struct {
	// Snapshot allows the libvirt snapshot (qcow2 images).
	Snapshot bool
	// Pause allows pausing the VM where no snapshot is possible or allowed.
	Pause bool
}

// DiskPlan is how the disks of a running VM could be copied right now.
type DiskPlan struct {
	// Way is DiskWaySnapshot, DiskWayPause or "" (none).
	Way string `json:"way"`
	// Agent: the QEMU guest agent answers, the snapshot can quiesce the
	// guest's file systems.
	Agent bool `json:"agent"`
	// Reason is why the snapshot is not used (a Disk… code); set with
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

type cachedPlan struct {
	plan DiskPlan
	at   time.Time
}

// PlanDiskBackupCached is PlanDiskBackup for pages that ask again and
// again: an answer is reused for half a minute, so that every look at the
// job editor does not ask libvirt, the guest agent and the images anew.
func (m *Manager) PlanDiskBackupCached(ctx context.Context, name string, opts DiskBackupOptions) DiskPlan {
	key := fmt.Sprintf("%s\x00%v\x00%v", name, opts.Snapshot, opts.Pause)
	m.mu.Lock()
	cached, ok := m.diskPlans[key]
	m.mu.Unlock()
	if ok && m.clock().Sub(cached.at) < planMaxAge && !m.clock().Before(cached.at) {
		return cached.plan
	}
	plan := m.PlanDiskBackup(ctx, name, opts)
	m.mu.Lock()
	if m.diskPlans == nil {
		m.diskPlans = map[string]cachedPlan{}
	}
	m.diskPlans[key] = cachedPlan{plan: plan, at: m.clock()}
	m.mu.Unlock()
	return plan
}

// PlanDiskBackup tells how the disks of a running VM could be copied
// consistently right now. It changes nothing: it reads the headers of the
// images, the free space and asks the guest agent, all within a quarter of
// a minute.
func (m *Manager) PlanDiskBackup(ctx context.Context, name string, opts DiskBackupOptions) DiskPlan {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
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
		if opts.Pause {
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
	if !opts.Snapshot {
		// It would work, but the job did not ask for it.
		return paused(DiskSnapshotUnproven)
	}
	for dir, bytes := range need {
		free, err := diskFree(dir)
		if err != nil || free < bytes {
			return DiskPlan{Reason: DiskNoSpace, NeedBytes: bytes, FreeBytes: free}
		}
	}
	ask, stop := context.WithTimeout(ctx, 8*time.Second)
	defer stop()
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
	// LowSpace: the room next to the overlays ran low while the snapshot
	// was held. A VM that QEMU paused for a full disk is resumed once the
	// overlay is committed back.
	LowSpace bool `json:"lowSpace,omitempty"`
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
	mu     sync.Mutex
	// stopped is why the copy was told to stop before it was done ("" if
	// it was not): DiskPauseTimeout, DiskNoSpace or DiskBusy.
	stopped string
	// certain: libvirt is not in the middle of taking the snapshot (the
	// command ended by itself). Without it the undo does not decide about
	// an overlay the VM does not use (yet).
	certain bool
	// watching is closed when watchSpace has ended; nil without it.
	watching chan struct{}
}

// Method is the way used and whether the guest's file systems were frozen.
func (b *DiskBackup) Method() (way string, quiesced bool) { return b.Way, b.Quiesced }

// Context ends when the copy must stop: the caller's context ended, or
// the VM was let go early (Stopped).
func (b *DiskBackup) Context() context.Context { return b.ctx }

// Stopped is why the VM was let go before the copy was done: the pause
// reached its limit (DiskPauseTimeout), the room for the overlay ran low
// (DiskNoSpace), the VM is being stopped (DiskBusy). "" otherwise.
func (b *DiskBackup) Stopped() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.stopped
}

// stop tells the copy to stop for a reason; the first reason stays.
func (b *DiskBackup) stop(code string) {
	b.mu.Lock()
	if b.stopped == "" {
		b.stopped = code
	}
	b.mu.Unlock()
	b.cancel()
}

// BeginDiskBackup makes the image files of a running VM stand still: with
// a libvirt snapshot (qcow2, if opts allow it), or by pausing it if opts
// allow that and nothing better works. The caller copies the images and
// calls End, always. A *DiskBackupError says why it did not start; nothing
// is changed then (DiskUndoFailed excepted: the VM needs another look).
func (m *Manager) BeginDiskBackup(ctx context.Context, name string, opts DiskBackupOptions) (*DiskBackup, error) {
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
	plan := m.PlanDiskBackup(ctx, name, opts)
	if plan.Way == "" {
		return fail(plan.Reason, nil)
	}
	state, err := m.runner.Run(ctx, "", "virsh", "--quiet", "domstate", name)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(state), "running") {
		// Paused by its owner, by an I/O error, shutting down: left alone.
		return fail(DiskBusy, err)
	}
	if err := ctx.Err(); err != nil {
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
	b := &DiskBackup{m: m, vm: vm, lock: lock, record: rec, Way: plan.Way, certain: true}
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
		// The overlays are made here, not by libvirt: TitanOS runs QEMU as
		// its own user and libvirt does not change owners
		// (dynamic_ownership = 0), so a file libvirt made as root could not
		// be opened by QEMU. Like the images, they are reachable through
		// the group. -u: the image is in use and is not opened; its size
		// comes from its header.
		for i, disk := range rec.Disks {
			size := vm.Disks[i].SizeBytes
			if out, err := m.runner.Run(ctx, "", "qemu-img", "info", "-U", "--output=json", "-f", "qcow2", disk.Base); err == nil {
				var info struct {
					VirtualSize int64 `json:"virtual-size"`
				}
				if json.Unmarshal([]byte(out), &info) == nil && info.VirtualSize > 0 {
					size = info.VirtualSize
				}
			}
			if size <= 0 {
				return abort(DiskSnapshotFailed, fmt.Errorf("size of %s unknown", disk.Base))
			}
			if _, err := m.runner.Run(context.WithoutCancel(ctx), "", "qemu-img", "create", "-q", "-f", "qcow2", "-F", "qcow2", "-b", disk.Base, "-u", disk.Overlay, fmt.Sprint(size)); err != nil {
				return abort(DiskSnapshotFailed, err)
			}
			if err := m.share(disk.Overlay, 0o660); err != nil {
				return abort(DiskSnapshotFailed, err)
			}
		}
		args := []string{"--quiet", "snapshot-create-as", name, "--disk-only", "--atomic", "--no-metadata", "--reuse-external"}
		for _, disk := range rec.Disks {
			args = append(args, "--diskspec", disk.Target+",snapshot=external,file="+disk.Overlay)
		}
		// Not on the context of the backup: a cancel must not kill virsh
		// while libvirt switches the disks (it would finish the snapshot
		// all the same, and nobody would know). own tells that the command
		// ended by itself, so libvirt is done either way.
		create := func(args []string) (err error, own bool) {
			run, cancel := context.WithTimeout(context.WithoutCancel(ctx), snapshotLimit)
			defer cancel()
			_, err = m.runner.Run(run, "", "virsh", args...)
			return err, run.Err() == nil
		}
		err, own := errors.New("no guest agent"), true
		if plan.Agent {
			if err, own = create(append(slices0(args), "--quiesce")); err == nil {
				b.Quiesced = true
			} else if own {
				slog.Warn("VM snapshot for a backup: the guest did not freeze its file systems; taken without", "vm", name, "err", err)
			}
		}
		if err != nil && own {
			err, own = create(args)
		}
		if !own {
			// Cut off by its time limit: libvirt may still take the snapshot.
			// Nothing is decided now; the record stays for the undo.
			b.certain = false
		}
		if err != nil {
			return abort(DiskSnapshotFailed, err)
		}
		b.watching = make(chan struct{})
		go b.watchSpace()
	case DiskWayPause:
		if _, err := m.runner.Run(context.WithoutCancel(ctx), "", "virsh", "--quiet", "suspend", name); err != nil {
			return abort(DiskPauseFailed, err)
		}
		b.timer = time.AfterFunc(pauseLimit, func() {
			slog.Warn("VM paused for a backup reached the time limit; resumed, its disks are not saved by this run", "vm", name, "limit", pauseLimit)
			b.stop(DiskPauseTimeout)
			_ = b.End() // reported by the caller's own End
		})
	}
	slog.Info("VM disks stand still for a backup", "vm", name, "way", plan.Way, "quiesced", b.Quiesced)
	return b, nil
}

// watchSpace looks at the room next to the overlays while the snapshot is
// held. The guest's writes go there; if they outgrow what was reserved,
// the volume would fill up and QEMU would pause the VM (error_policy
// "stop"). Before that the copy is stopped; End commits the overlay back.
func (b *DiskBackup) watchSpace() {
	defer close(b.watching)
	ticker := time.NewTicker(spaceWatch)
	defer ticker.Stop()
	for {
		select {
		case <-b.ctx.Done():
			return
		case <-ticker.C:
		}
		for _, disk := range b.record.Disks {
			free, err := diskFree(filepath.Dir(disk.Overlay))
			if err != nil || free >= overlayLowWater {
				continue
			}
			slog.Warn("room for the snapshot of a VM runs low; the backup lets go of the VM, its disks are not saved by this run", "vm", b.vm.Name, "freeBytes", free)
			b.mu.Lock()
			b.record.LowSpace = true
			rec := b.record
			b.mu.Unlock()
			if err := b.m.writeDiskBackup(b.vm.Volume, rec); err != nil {
				slog.Warn("record of a VM backup not updated", "vm", b.vm.Name, "err", err)
			}
			b.stop(DiskNoSpace)
			return
		}
	}
}

// slices0 copies a list of arguments, so that appending to it leaves the
// original alone.
func slices0(args []string) []string { return append([]string(nil), args...) }

// End undoes what BeginDiskBackup did: the overlay is committed back and
// removed, or the VM resumed. It is safe to call more than once and after
// the context ended; every later call returns the first result. If the
// undo fails or cannot be decided yet the record stays, and it is tried
// again when titand starts, when the next copy run begins, before any
// operation on the VM's disks and before the VM is started.
func (b *DiskBackup) End() error {
	b.once.Do(func() {
		if b.timer != nil {
			b.timer.Stop()
		}
		b.cancel()
		if b.watching != nil {
			<-b.watching
		}
		ctx, cancel := context.WithTimeout(context.Background(), diskUndoLimit)
		defer cancel()
		b.mu.Lock()
		rec, certain := b.record, b.certain
		b.mu.Unlock()
		b.err = b.m.undoDiskBackup(ctx, b.vm.Volume, rec, certain)
		b.m.mu.Lock()
		delete(b.m.diskBackups, b.vm.UUID)
		// Looked at again soon, not only at the next start.
		delete(b.m.diskBackupChecked, b.vm.Volume)
		b.m.mu.Unlock()
		b.lock.Unlock()
		if b.err != nil {
			slog.Error("VM not back to normal after a backup; tried again shortly", "vm", b.vm.Name, "way", b.Way, "err", b.err)
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
	b.stop(DiskBusy)
	return true
}

// errUndecided: the VM does not use an overlay of the record, and it is too
// early to tell that it never will.
var errUndecided = errors.New("vm: cannot tell yet whether the snapshot was taken; the record stays")

// blockList is the live block chain of a running VM as "virsh domblklist"
// prints it: target and source per line.
func (m *Manager) blockList(ctx context.Context, name string) (map[string]string, error) {
	out, err := m.runner.Run(ctx, "", "virsh", "--quiet", "domblklist", name)
	if err != nil {
		return nil, err
	}
	sources := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		// The source may hold blanks: everything after the target.
		target, source, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok {
			sources[target] = strings.TrimSpace(source)
		}
	}
	return sources, nil
}

// blockJobActive tells whether QEMU runs a block job on a disk, and
// whether it is ready to be finished (an active commit at 100 %).
func (m *Manager) blockJobActive(ctx context.Context, name, target string) (active, ready bool, err error) {
	// --raw: the plain form goes to stderr as a progress line (seen with
	// libvirt 11.3); raw prints type, bandwidth, cur and end to stdout, and
	// nothing at all without a job.
	out, err := m.runner.Run(ctx, "", "virsh", "blockjob", name, target, "--info", "--raw")
	if err != nil {
		return false, false, err
	}
	values := map[string]string{}
	for _, field := range strings.Fields(out) {
		if key, value, ok := strings.Cut(field, "="); ok {
			values[key] = value
		}
	}
	if _, job := values["type"]; !job {
		return false, false, nil
	}
	// An active commit is ready for the pivot once everything is copied.
	return true, values["cur"] != "" && values["cur"] == values["end"], nil
}

// commitLive brings a running VM back from an overlay onto its image: the
// overlay is committed into the image and the guest switched over (pivot).
// A block job an earlier attempt left in QEMU (titand restarted, the time
// limit) is not started again, which QEMU would refuse: it is waited for
// and then finished. It is never aborted. When ctx ends first, the job
// runs on and the next attempt takes it up. The overlay counts as gone
// only when the live block chain no longer names it.
func (m *Manager) commitLive(ctx context.Context, name string, disk diskBackupDisk) error {
	active, ready, err := m.blockJobActive(ctx, name, disk.Target)
	if err != nil {
		return err
	}
	if !active {
		_, err := m.runner.Run(ctx, "", "virsh", "--quiet", "blockcommit", name, disk.Target, "--active", "--wait", "--pivot")
		if err == nil {
			return m.offOverlay(ctx, name, disk)
		}
		// Refused, or cut off: is there a job now?
		look, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		active, ready, _ = m.blockJobActive(look, name, disk.Target)
		cancel()
		if !active {
			// Perhaps it went through all the same.
			if m.offOverlay(look, name, disk) == nil {
				return nil
			}
			return err
		}
	}
	ticker := time.NewTicker(blockJobPoll)
	defer ticker.Stop()
	for !ready {
		select {
		case <-ctx.Done():
			return fmt.Errorf("block job of %s on %s still runs: %w", name, disk.Target, ctx.Err())
		case <-ticker.C:
		}
		if active, ready, err = m.blockJobActive(ctx, name, disk.Target); err != nil {
			return err
		}
		if !active {
			// Ended by itself (a commit that was started with --pivot).
			return m.offOverlay(ctx, name, disk)
		}
	}
	if _, err := m.runner.Run(ctx, "", "virsh", "--quiet", "blockjob", name, disk.Target, "--pivot"); err != nil {
		return err
	}
	return m.offOverlay(ctx, name, disk)
}

// offOverlay confirms from the live block chain that the VM no longer
// uses an overlay.
func (m *Manager) offOverlay(ctx context.Context, name string, disk diskBackupDisk) error {
	sources, err := m.blockList(ctx, name)
	if err != nil {
		return err
	}
	if sources[disk.Target] == disk.Overlay {
		return fmt.Errorf("%s still runs on the overlay of %s", name, disk.Target)
	}
	return nil
}

// undoDiskBackup brings a VM back to normal from its record and removes
// the record. Only what the record names is touched. certain tells that
// libvirt is not in the middle of taking the snapshot; without it, and for
// a young record, an overlay the VM does not use is not decided about. The
// caller holds the operation lock of the VM.
func (m *Manager) undoDiskBackup(ctx context.Context, volume string, rec diskBackupRecord, certain bool) error {
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
		for _, disk := range rec.Disks {
			if !strings.HasPrefix(filepath.Base(disk.Overlay), filepath.Base(disk.Base)+".") || !strings.HasSuffix(disk.Overlay, OverlaySuffix) || filepath.Dir(disk.Overlay) != filepath.Dir(disk.Base) {
				return fmt.Errorf("record of %s names %q, which is no overlay of a backup", rec.VM, disk.Overlay)
			}
		}
		settled := certain || m.clock().Sub(rec.Started) >= snapshotSettle
		for _, disk := range rec.Disks {
			exists := func() bool {
				info, err := os.Lstat(disk.Overlay)
				return err == nil && info.Mode().IsRegular()
			}
			if running {
				// Only libvirt knows what the guest writes to; asked anew for
				// every disk, right before anything is done.
				sources, err := m.blockList(ctx, rec.VM)
				if err != nil {
					return err
				}
				switch {
				case sources[disk.Target] == disk.Overlay:
					if err := m.commitLive(ctx, rec.VM, disk); err != nil {
						return err
					}
				case !settled:
					// Not in use (yet?): a snapshot command that was cut off
					// may still switch the guest onto it. Removing the file
					// or the record now could leave the VM on an overlay
					// nobody knows about.
					return errUndecided
				}
				// Once more before the file goes: never the overlay of a
				// running VM.
				if err := m.offOverlay(ctx, rec.VM, disk); err != nil {
					return err
				}
				if m.pid(rec.VM) == 0 {
					running = false // it stopped meanwhile: the other way
				}
			}
			if !running && exists() {
				// The VM stopped on the overlay (or the NAS went down): what
				// it wrote there goes into the image before it starts again.
				// Committing what is committed already changes nothing.
				if _, err := m.runner.Run(ctx, "", "qemu-img", "commit", "-d", "-f", "qcow2", disk.Overlay); err != nil {
					return err
				}
			}
			if exists() {
				if err := os.Remove(disk.Overlay); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return err
				}
			}
		}
		if rec.LowSpace && m.pid(rec.VM) != 0 {
			// The overlays are gone and with them what filled the volume. If
			// QEMU paused the VM for the full disk meanwhile, it goes on now.
			state, err := m.runner.Run(ctx, "", "virsh", "--quiet", "domstate", rec.VM, "--reason")
			if err != nil {
				return err
			}
			if lower := strings.ToLower(state); strings.HasPrefix(strings.TrimSpace(lower), "paused") && (strings.Contains(lower, "i/o error") || strings.Contains(lower, "ioerror")) {
				if _, err := m.runner.Run(ctx, "", "virsh", "--quiet", "resume", rec.VM); err != nil {
					return err
				}
				slog.Warn("VM resumed: it was paused because the snapshot of a backup filled the volume", "vm", rec.VM)
			}
		}
	}
	return m.removeDiskBackup(volume, rec.UUID)
}

// ErrDiskBackupRecord: the record of an interrupted backup of a VM cannot
// be read or does not fit the VM, and overlays of a backup lie next to its
// disks. TitanOS does not guess: RepairDiskBackup (the VM page, owners and
// administrators) commits them back after checking each one.
var ErrDiskBackupRecord = errors.New("vm: the record of an interrupted backup is unreadable; repair the VM's disks first")

func (m *Manager) diskBackupRecordPath(vm VM) string {
	return filepath.Join(m.mountRoot, vm.Volume, ".titanos", diskBackupFeature, vm.UUID+".json")
}

// overlaysOf lists the overlay files of backups next to the disks of a VM,
// by disk index.
func (m *Manager) overlaysOf(vm VM) map[int][]string {
	found := map[int][]string{}
	for i, disk := range vm.Disks {
		if m.isBlock(disk) {
			continue
		}
		base := m.diskPath(vm, disk)
		entries, _ := os.ReadDir(filepath.Dir(base))
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), filepath.Base(base)+".") && strings.HasSuffix(entry.Name(), OverlaySuffix) && entry.Type().IsRegular() {
				found[i] = append(found[i], filepath.Join(filepath.Dir(base), entry.Name()))
			}
		}
	}
	return found
}

// setDiskBackupAside takes a record that cannot be used out of the way, so
// that it does not hold the VM back for good; it is kept for a look.
func (m *Manager) setDiskBackupAside(vm VM) error {
	root, err := m.diskBackupRoot(vm.Volume)
	if err != nil {
		return err
	}
	defer root.Close()
	return root.Rename(vm.UUID+".json", fmt.Sprintf("%s.unreadable-%d", vm.UUID, m.clock().Unix()))
}

// finishDiskBackupOf undoes what a backup left for one VM, if anything.
// The caller holds the operation lock of the VM.
//
// A record that cannot be read or belongs to another VM is never acted
// on. If no overlay of a backup lies next to the VM's disks it cannot
// matter any more (at worst the VM is still paused, which its owner sees
// and ends on the VM page): it is set aside and the VM is free. With
// overlays there, the VM is held (ErrDiskBackupRecord) until an owner or
// administrator repairs it (RepairDiskBackup).
func (m *Manager) finishDiskBackupOf(ctx context.Context, vm VM) error {
	if _, err := os.Lstat(m.diskBackupRecordPath(vm)); errors.Is(err, fs.ErrNotExist) {
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
	if err == nil && (rec.UUID != vm.UUID || rec.VM != vm.Name) {
		err = errors.New("it belongs to another VM")
	}
	if err != nil {
		if len(m.overlaysOf(vm)) > 0 {
			slog.Error("record of an interrupted VM backup unusable and overlays lie next to the disks; the VM waits for a repair", "vm", vm.Name, "err", err)
			if m.Notify != nil {
				m.Notify("error", "vm_backup_undo_failed", vm.Name)
			}
			return fmt.Errorf("%w: %v", ErrDiskBackupRecord, err)
		}
		slog.Warn("record of an interrupted VM backup unusable and no overlay left: set aside; if the VM is paused, resume it on the VM page", "vm", vm.Name, "err", err)
		return m.setDiskBackupAside(vm)
	}
	if err := m.undoDiskBackup(ctx, vm.Volume, rec, false); err != nil {
		return err
	}
	slog.Warn("VM brought back to normal after an interrupted backup", "vm", vm.Name, "way", rec.Way)
	return nil
}

// RepairDiskBackup is the way out when the record of an interrupted backup
// is unusable (ErrDiskBackupRecord): asked for by an owner or
// administrator, it commits back the overlays of backups found next to the
// VM's disks, each only after a check, and sets the record aside.
//
// Running VM: the overlay each disk really uses (libvirt's block chain)
// is committed back like after a backup. Stopped VM: a disk may have
// exactly one overlay, and that overlay must name this disk as the image
// it builds on; then it is committed into the image and removed. Anything
// else is refused and nothing is changed for that disk: the files stay
// where they are for someone to look at.
func (m *Manager) RepairDiskBackup(ctx context.Context, name string) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	vm, err := m.get(name)
	if err != nil {
		return err
	}
	m.mu.Lock()
	active := m.diskBackups[vm.UUID]
	m.mu.Unlock()
	if active != nil {
		return ErrSnapshotBusy
	}
	// A record that can be used after all: the normal undo.
	if err := m.finishDiskBackupOf(ctx, vm); err == nil {
		return nil
	} else if !errors.Is(err, ErrDiskBackupRecord) {
		return err
	}
	targets := diskTargets(vm)
	overlays := m.overlaysOf(vm)
	if m.pid(name) != 0 {
		sources, err := m.blockList(ctx, name)
		if err != nil {
			return err
		}
		for i, files := range overlays {
			if source := sources[targets[i]]; slices.Contains(files, source) {
				if err := m.commitLive(ctx, name, diskBackupDisk{Target: targets[i], Base: m.diskPath(vm, vm.Disks[i]), Overlay: source}); err != nil {
					return err
				}
			}
		}
		// What the VM does not use is left for a look, not removed: the
		// record that would say what it is cannot be read.
		for i, files := range overlays {
			for _, file := range files {
				if err := m.offOverlay(ctx, name, diskBackupDisk{Target: targets[i], Overlay: file}); err != nil {
					return err
				}
			}
		}
	} else {
		for i, files := range overlays {
			base := m.diskPath(vm, vm.Disks[i])
			if len(files) != 1 {
				return fmt.Errorf("%w: %d overlays next to %s", ErrDiskBackupRecord, len(files), base)
			}
			out, err := m.runner.Run(ctx, "", "qemu-img", "info", "--output=json", "-f", "qcow2", files[0])
			var info struct {
				Backing string `json:"backing-filename"`
			}
			if err != nil || json.Unmarshal([]byte(out), &info) != nil || info.Backing != base && filepath.Join(filepath.Dir(files[0]), info.Backing) != base {
				return fmt.Errorf("%w: %s does not build on %s", ErrDiskBackupRecord, files[0], base)
			}
		}
		for _, files := range overlays {
			if _, err := m.runner.Run(ctx, "", "qemu-img", "commit", "-d", "-f", "qcow2", files[0]); err != nil {
				return err
			}
			if err := os.Remove(files[0]); err != nil {
				return err
			}
		}
	}
	slog.Warn("disks of a VM repaired after an interrupted backup with an unusable record", "vm", name)
	return m.setDiskBackupAside(vm)
}

// diskOperation is operation for everything that works on the disks of a
// VM (grow, clone, snapshots, delete, application backups): what a backup
// left behind is undone first, and the operation is refused if that does
// not work. The image alone is not the whole disk while an overlay holds
// the guest's last writes.
func (m *Manager) diskOperation(ctx context.Context, name string) (*sync.Mutex, error) {
	lock, err := m.operation(name)
	if err != nil {
		return nil, err
	}
	if vm, err := m.get(name); err == nil && m.Mounted != nil && containsString(m.Mounted(), vm.Volume) {
		if err := m.finishDiskBackupOf(ctx, vm); err != nil {
			lock.Unlock()
			if errors.Is(err, ErrDiskBackupRecord) {
				return nil, err
			}
			return nil, fmt.Errorf("%w: %v", ErrSnapshotRecovery, err)
		}
	}
	return lock, nil
}

// RecoverDiskBackups undoes what interrupted backups left behind: a VM
// still paused is resumed, an overlay is committed back and removed. It
// looks at the volumes of the VMs only, once per start unless force is
// set (the start of a copy run) or the last look did not finish (then
// again with every round of the VM loop). VMs with another operation
// running are left for the next time.
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
					switch {
					case errors.Is(err, ErrDiskBackupRecord):
						// Waits for a person; asking again every few seconds
						// would not help.
						errs = append(errs, fmt.Errorf("%s: %w", vm.Name, err))
					case err != nil:
						failed = true
						errs = append(errs, fmt.Errorf("%s: %w", vm.Name, err))
						if !errors.Is(err, errUndecided) {
							slog.Error("VM not brought back to normal after an interrupted backup", "vm", vm.Name, "err", err)
						}
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
	return slices.Contains(list, value)
}
