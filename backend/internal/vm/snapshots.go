package vm

// Snapshots are independent, sparse/reflink copies. A running guest is saved
// before copying any disk or firmware state and resumed only afterwards. There
// are no backing chains to lose during deletion or an OS rollback.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"golang.org/x/sys/unix"
)

var (
	ErrSnapshotBusy        = errors.New("vm: another operation is in progress")
	ErrSnapshotUnsupported = errors.New("vm: snapshot cannot include passthrough devices, host folders or block disks")
	ErrSnapshotRecovery    = errors.New("vm: an interrupted snapshot operation needs recovery")
	ErrSnapshotRuntime     = errors.New("vm: RAM state requires the same QEMU, firmware and host CPU; restore without RAM instead")
	ErrSnapshotExists      = errors.New("vm: remove snapshots before removing the VM")
	snapshotID             = regexp.MustCompile(`^[0-9a-f-]{36}$`)
)

type SnapshotSchedule struct {
	Enabled    bool   `json:"enabled"`
	Time       string `json:"time"`
	Days       []int  `json:"days"` // Sunday = 0
	Keep       int    `json:"keep"`
	Memory     bool   `json:"memory"`
	AllowPause bool   `json:"allowPause"`
}
type SnapshotFile struct {
	Target    string `json:"target"`
	File      string `json:"file"`
	Size      int64  `json:"size"`
	Directory bool   `json:"directory,omitempty"`
}
type Snapshot struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Created    time.Time      `json:"created"`
	Memory     bool           `json:"memory"`
	Automatic  bool           `json:"automatic"`
	Bytes      int64          `json:"bytes"`
	VM         VM             `json:"vm"`
	Runtime    string         `json:"runtime"`
	Files      []SnapshotFile `json:"files"`
	Incomplete bool           `json:"incomplete,omitempty"`
}
type SnapshotRequest struct {
	Name       string `json:"name"`
	Memory     bool   `json:"memory"`
	AllowPause bool   `json:"allowPause"`
}
type SnapshotStatus struct {
	Snapshots []Snapshot       `json:"snapshots"`
	Schedule  SnapshotSchedule `json:"schedule"`
	Job       *Job             `json:"job"`
	Pending   bool             `json:"pending"`
	Reason    string           `json:"reason"`
}
type snapshotOperation struct {
	Kind     string         `json:"kind"`
	Phase    string         `json:"phase"`
	ID       string         `json:"id"`
	Original VM             `json:"original"`
	Memory   bool           `json:"memory"`
	Files    []SnapshotFile `json:"files"`
	Existing []bool         `json:"existing"`
	Runtime  string         `json:"runtime"`
}

// Reservations are shared by all snapshot jobs. Count conservatively until a
// job finishes, including bytes it may already have written. This prevents two
// simultaneous schedules from both claiming the same free volume space.
func (m *Manager) reserveSnapshotSpace(requested map[string]int64) (func(), error) {
	m.snapshotSpaceMu.Lock()
	defer m.snapshotSpaceMu.Unlock()
	needs := map[uint64]uint64{}
	free := map[uint64]uint64{}
	for path, amount := range requested {
		if amount < 0 {
			return nil, ErrNoSpace
		}
		// Btrfs subvolumes have different st_dev values but share the same
		// capacity. Identify the configured volume at /mnt/<name>, including
		// when restoring an imported image below a nested subvolume.
		relative, err := filepath.Rel(m.mountRoot, filepath.Clean(path))
		if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
			return nil, ErrUnknownVolume
		}
		volumeRoot := filepath.Join(m.mountRoot, strings.Split(relative, string(os.PathSeparator))[0])
		info, err := os.Stat(volumeRoot)
		if err != nil {
			return nil, err
		}
		dev := info.Sys().(*syscall.Stat_t).Dev
		available, err := freeBytes(path)
		if err != nil {
			return nil, err
		}
		needs[dev] += uint64(amount)
		capacity := uint64(max(available, 0))
		if previous, exists := free[dev]; exists {
			capacity = min(capacity, previous)
		}
		free[dev] = capacity
	}
	for dev, need := range needs {
		available := free[dev] - min(free[dev], m.snapshotSpace[dev])
		if need > available {
			return nil, ErrNoSpace
		}
	}
	if m.snapshotSpace == nil {
		m.snapshotSpace = map[uint64]uint64{}
	}
	for dev, need := range needs {
		m.snapshotSpace[dev] += need
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			m.snapshotSpaceMu.Lock()
			defer m.snapshotSpaceMu.Unlock()
			for dev, need := range needs {
				m.snapshotSpace[dev] -= need
				if m.snapshotSpace[dev] == 0 {
					delete(m.snapshotSpace, dev)
				}
			}
		})
	}, nil
}

// All lifecycle mutations of a VM take this lock, including asynchronous work.
func (m *Manager) operation(name string) (*sync.Mutex, error) {
	value, _ := m.operations.LoadOrStore(name, &sync.Mutex{})
	lock := value.(*sync.Mutex)
	if !lock.TryLock() {
		return nil, ErrSnapshotBusy
	}
	return lock, nil
}
func (m *Manager) snapshotRoot(v VM) string {
	return filepath.Join(m.mountRoot, v.Volume, ".titanos", "vm-snapshots", v.UUID)
}
func (m *Manager) pendingSnapshot(v VM) bool {
	_, err := os.Lstat(filepath.Join(m.snapshotRoot(v), "operation.json"))
	return err == nil || !errors.Is(err, fs.ErrNotExist)
}
func (m *Manager) snapshotReason(v VM) string {
	if v.ISO != "" {
		return "iso"
	}
	if len(v.PCI) > 0 || len(v.USB) > 0 {
		return "passthrough"
	}
	if len(v.Folders) > 0 {
		return "folders"
	}
	if len(v.Disks) == 0 {
		return "disks"
	}
	for _, d := range v.Disks {
		if m.isBlock(d) {
			return "block"
		}
		if d.Source != "" {
			return "preparing"
		}
	}
	return ""
}

// Reject symlinks in every component, including parent directories. All paths
// are generated from the VM configuration, never supplied as restore targets.
func (m *Manager) snapshotPath(path string, missing bool) error {
	path = filepath.Clean(path)
	rel, err := filepath.Rel(m.mountRoot, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return ErrBadFolder
	}
	p := m.mountRoot
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		p = filepath.Join(p, part)
		st, err := os.Lstat(p)
		if errors.Is(err, fs.ErrNotExist) && missing {
			return nil
		}
		if err != nil {
			return err
		}
		if st.Mode()&fs.ModeSymlink != 0 {
			return ErrBadFolder
		}
	}
	return nil
}
func (m *Manager) snapshotFiles(v VM) ([]SnapshotFile, error) {
	var files []SnapshotFile
	add := func(path string) error {
		if err := m.snapshotPath(path, false); err != nil {
			return err
		}
		st, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return ErrBadImage
		}
		files = append(files, SnapshotFile{Target: path, File: fmt.Sprintf("file-%03d", len(files)), Size: st.Size()})
		return nil
	}
	for _, d := range v.Disks {
		if err := add(m.diskPath(v, d)); err != nil {
			return nil, err
		}
	}
	dir := m.dir(v.Volume, v.folder())
	if _, err := os.Lstat(filepath.Join(dir, "nvram.fd")); err == nil {
		if err = add(filepath.Join(dir, "nvram.fd")); err != nil {
			return nil, err
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if v.TPM {
		path := filepath.Join(dir, "tpm")
		size, _, err := snapshotTreeSize(path)
		if err != nil {
			return nil, err
		}
		files = append(files, SnapshotFile{Target: path, File: fmt.Sprintf("file-%03d", len(files)), Size: size, Directory: true})
	}

	return files, nil
}
func (m *Manager) snapshotRuntime(ctx context.Context, v VM) (string, error) {
	q, err := m.runner.Run(ctx, "", emulator, "--version")
	if err != nil {
		return "", err
	}
	fw, err := os.ReadFile(firmware(v).code)
	if err != nil {
		return "", err
	}
	cpu, err := os.ReadFile(filepath.Join(m.procDir, "cpuinfo"))
	if err != nil {
		return "", err
	}
	var identity strings.Builder
	for _, line := range strings.Split(string(cpu), "\n") {
		if strings.HasPrefix(line, "model name") || strings.HasPrefix(line, "flags") {
			identity.WriteString(line)
		}
	}
	hash := sha256.New()
	hash.Write([]byte(q))
	hash.Write(fw)
	hash.Write([]byte(identity.String()))
	return hex.EncodeToString(hash.Sum(nil)), nil
}
func (m *Manager) SnapshotStatus(name string, mounted []string) (SnapshotStatus, error) {
	v, err := m.get(name)
	if err != nil {
		return SnapshotStatus{}, err
	}
	out := SnapshotStatus{Snapshots: []Snapshot{}, Schedule: v.SnapshotSchedule, Reason: m.snapshotReason(v)}
	m.mu.Lock()
	if j := m.snapshotJobs[name]; j != nil {
		copy := *j
		out.Job = &copy
	}
	m.mu.Unlock()
	if !m.available(v, mounted) {
		out.Reason = "volume"
		return out, nil
	}
	if err = m.snapshotPath(m.snapshotRoot(v), true); err != nil {
		return out, err
	}
	out.Pending = m.pendingSnapshot(v)
	entries, err := os.ReadDir(m.snapshotRoot(v))
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	for _, entry := range entries {
		if !entry.IsDir() || !snapshotID.MatchString(entry.Name()) {
			continue
		}
		snap, err := m.readSnapshot(v, entry.Name())
		if err != nil {
			snap = Snapshot{ID: entry.Name(), Name: entry.Name(), Incomplete: true}
		}
		out.Snapshots = append(out.Snapshots, snap)
	}
	sort.Slice(out.Snapshots, func(i, j int) bool { return out.Snapshots[i].Created.After(out.Snapshots[j].Created) })
	return out, nil
}
func (m *Manager) readSnapshot(v VM, id string) (Snapshot, error) {
	var s Snapshot
	if !snapshotID.MatchString(id) {
		return s, ErrNotFound
	}
	dir := filepath.Join(m.snapshotRoot(v), id)
	if err := m.snapshotPath(dir, false); err != nil {
		return s, err
	}
	if err := config.Open(dir).ReadJSON("snapshot.json", &s); err != nil {
		return s, err
	}
	if s.ID != id || s.VM.UUID != v.UUID || s.VM.Name != v.Name || s.VM.Volume != v.Volume || s.VM.folder() != v.folder() || len(s.Files) < len(s.VM.Disks) || len(s.Files) > 1000 {
		return s, ErrInvalid
	}
	if m.snapshotReason(s.VM) != "" {
		return s, ErrSnapshotUnsupported
	}
	// Disk paths must still belong to this VM. Changing RAM/CPU is reversible;
	// changing storage ownership is not silently undone by an old snapshot.
	if len(s.VM.Disks) != len(v.Disks) {
		return s, ErrBadImage
	}
	for i, d := range s.VM.Disks {
		if m.diskPath(s.VM, d) != m.diskPath(v, v.Disks[i]) {
			return s, ErrBadImage
		}
	}
	seen := map[string]bool{}
	for i, f := range s.Files {
		allowed := i < len(s.VM.Disks) && f.Target == m.diskPath(s.VM, s.VM.Disks[i])
		dir := m.dir(v.Volume, v.folder())
		allowed = allowed || f.Target == filepath.Join(dir, "nvram.fd") || (s.VM.TPM && f.Directory && f.Target == filepath.Join(dir, "tpm"))
		if f.Directory && f.Target != filepath.Join(dir, "tpm") {
			return s, ErrInvalid
		}
		if !allowed || seen[f.Target] || f.File != fmt.Sprintf("file-%03d", i) || f.Size < 0 {
			return s, ErrInvalid
		}
		seen[f.Target] = true
		if err := m.snapshotPath(f.Target, true); err != nil {
			return s, err
		}
	}
	return s, nil
}
func (m *Manager) snapshotJob(name string, work func() error, lock *sync.Mutex) {
	m.mu.Lock()
	m.snapshotJobs[name] = &Job{State: "running"}
	m.mu.Unlock()
	go func() {
		defer lock.Unlock()
		err := work()
		if err != nil {
			slog.Error("VM snapshot operation failed", "vm", name, "err", err)
		} else {
			slog.Info("VM snapshot operation completed", "vm", name)
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if err != nil {
			m.snapshotJobs[name] = &Job{State: "failed", Text: err.Error()}
		} else {
			delete(m.snapshotJobs, name)
		}
	}()
}
func (m *Manager) CreateSnapshot(ctx context.Context, name string, req SnapshotRequest, mounted []string) error {
	return m.createSnapshot(ctx, name, req, mounted, false)
}
func (m *Manager) createSnapshot(ctx context.Context, name string, req SnapshotRequest, mounted []string, automatic bool) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	release := true
	defer func() {
		if release {
			lock.Unlock()
		}
	}()
	v, err := m.get(name)
	if err != nil {
		return err
	}
	if !m.available(v, mounted) {
		return ErrUnknownVolume
	}
	if m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	if m.snapshotReason(v) != "" {
		return ErrSnapshotUnsupported
	}
	if req.Name == "" || len(req.Name) > 160 || !utf8.ValidString(req.Name) || strings.ContainsAny(req.Name, "\x00\r\n") {
		return ErrInvalid
	}
	running := m.pid(name) != 0
	if running && (!req.Memory || !req.AllowPause) {
		return ErrNotConfirmed
	}
	m.mu.Lock()
	j := m.jobs[name]
	m.mu.Unlock()
	if j != nil {
		return ErrPreparing
	}
	if err := m.snapshotPath(m.dir(v.Volume, v.folder()), false); err != nil {
		return err
	}
	nvram := filepath.Join(m.dir(v.Volume, v.folder()), "nvram.fd")
	if _, e := os.Lstat(nvram); errors.Is(e, fs.ErrNotExist) {
		if e = copyFile(firmware(v).vars, nvram); e != nil {
			return e
		}
		if e = m.share(nvram, 0o660); e != nil {
			return e
		}
	}
	if v.TPM {
		if err := m.prepareTPM(m.dir(v.Volume, v.folder())); err != nil {
			return err
		}
	}
	files, err := m.snapshotFiles(v)
	if err != nil {
		return err
	}
	// Conservatively reserve full allocated disk space plus the entire guest RAM.
	var need int64 = 256 << 20
	for _, f := range files {
		_, allocated, err := snapshotTreeSize(f.Target)
		if err != nil {
			return err
		}
		need += allocated
	}
	if running {
		need += int64(v.MemoryMiB) << 20
	}
	if err = m.snapshotPath(m.snapshotRoot(v), true); err != nil {
		return err
	}
	releaseSpace, err := m.reserveSnapshotSpace(map[string]int64{filepath.Join(m.mountRoot, v.Volume): need})
	if err != nil {
		return err
	}
	ctx = context.WithoutCancel(ctx)
	release = false
	m.snapshotJob(name, func() error {
		defer releaseSpace()
		return m.captureSnapshot(ctx, v, req, automatic, running)
	}, lock)
	return nil
}
func (m *Manager) captureSnapshot(ctx context.Context, v VM, req SnapshotRequest, automatic, running bool) (result error) {
	root := m.snapshotRoot(v)
	id := newUUID()
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	store := config.Open(root)
	op := snapshotOperation{Kind: "create", ID: id, Original: v, Memory: running, Phase: "saving"}
	if err := store.WriteJSON("operation.json", op); err != nil {
		return err
	}
	snap := Snapshot{ID: id, Name: req.Name, Created: time.Now(), Memory: running, Automatic: automatic, VM: v}
	if running {
		var err error
		snap.Runtime, err = m.snapshotRuntime(ctx, v)
		if err != nil {
			store.Remove("operation.json")
			os.RemoveAll(dir)
			return err
		}
		op.Runtime = snap.Runtime
		if err = store.WriteJSON("operation.json", op); err != nil {
			return err
		}
		if _, err = m.runner.Run(ctx, "", "virsh", "--quiet", "save", v.Name, filepath.Join(dir, "memory.save"), "--bypass-cache", "--running"); err != nil {
			if m.pid(v.Name) != 0 {
				store.Remove("operation.json")
				os.RemoveAll(dir)
			}
			return err
		}
	}
	// On an ordinary copy error the original guest can resume. If titand is
	// killed, the journal prevents autostart and the UI offers explicit recovery.
	defer func() {
		if running {
			if err := m.resumeSnapshot(ctx, store, &op, dir); err != nil {
				result = errors.Join(result, err)
				return
			}
		}
		if err := store.Remove("operation.json"); err != nil {
			result = errors.Join(result, err)
		}
		if result != nil {
			if !m.pendingSnapshot(v) {
				os.RemoveAll(dir)
			}
		} else if automatic {
			result = m.pruneSnapshots(v)
		}
	}()
	for _, disk := range v.Disks {
		if _, err := m.imageInfo(ctx, m.diskPath(v, disk)); err != nil {
			return err
		}
	}
	op.Phase = "copying"
	if err := store.WriteJSON("operation.json", op); err != nil {
		return err
	}
	files, err := m.snapshotFiles(v)
	if err != nil {
		return err
	}
	snap.Files = files
	for _, f := range files {
		if err = copySnapshotTree(f.Target, filepath.Join(dir, f.File)); err != nil {
			return err
		}
	}
	if running {
		if err = syncSnapshotFile(filepath.Join(dir, "memory.save")); err != nil {
			return err
		}
	}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if st, e := entry.Info(); e == nil {
			_, allocated, _ := snapshotTreeSize(filepath.Join(dir, entry.Name()))
			snap.Bytes += allocated
			_ = st
		}
	}
	if err = config.Open(dir).WriteJSON("snapshot.json", snap); err != nil {
		return err
	}
	return nil
}
func (m *Manager) resumeSnapshot(ctx context.Context, store *config.Store, op *snapshotOperation, dir string) error {
	op.Phase = "resume-started"
	if err := store.WriteJSON("operation.json", op); err != nil {
		return err
	}
	if err := os.MkdirAll(m.runDir, 0o770); err != nil {
		return err
	}
	if err := os.Chown(m.runDir, -1, m.runGid); err != nil {
		return err
	}
	if _, err := m.runner.Run(ctx, "", "virsh", "--quiet", "restore", filepath.Join(dir, "memory.save"), "--bypass-cache", "--running"); err != nil {
		return err
	}
	m.reserve(ctx)
	return nil
}

// copySnapshotFile preserves holes, ownership and modes, fsyncs before success,
// and drops only its own copied file pages. Reflinks avoid copying where possible.
func copySnapshotFile(from, to string) error {
	in, err := os.OpenFile(from, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return ErrBadImage
	}
	out, err := os.OpenFile(to, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	good := false
	defer func() {
		out.Close()
		if !good {
			os.Remove(to)
		}
	}()
	// Btrfs VM images are NOCOW. Reflink requires matching NOCOW flags on
	// the destination; set it while the destination is still empty.
	const noCOW = 0x00800000
	if flags, e := unix.IoctlGetInt(int(in.Fd()), unix.FS_IOC_GETFLAGS); e == nil && flags&noCOW != 0 {
		if flags, e := unix.IoctlGetInt(int(out.Fd()), unix.FS_IOC_GETFLAGS); e == nil {
			_ = unix.IoctlSetPointerInt(int(out.Fd()), unix.FS_IOC_SETFLAGS, flags|noCOW)
		}
	}
	if err = unix.IoctlFileClone(int(out.Fd()), int(in.Fd())); err != nil {
		if err = out.Truncate(st.Size()); err != nil {
			return err
		}
		for pos := int64(0); pos < st.Size(); {
			data, e := unix.Seek(int(in.Fd()), pos, unix.SEEK_DATA)
			if errors.Is(e, unix.ENXIO) {
				break
			}
			if e != nil {
				data = pos
			}
			hole, e := unix.Seek(int(in.Fd()), data, unix.SEEK_HOLE)
			if e != nil {
				hole = st.Size()
			}
			hole = min(hole, st.Size())
			if hole <= data {
				return io.ErrNoProgress
			}
			in.Seek(data, io.SeekStart)
			out.Seek(data, io.SeekStart)
			for n := data; n < hole; {
				size := min(int64(8<<20), hole-n)
				if _, err = io.CopyN(out, in, size); err != nil {
					return err
				}
				if err = out.Sync(); err != nil {
					return err
				}
				unix.Fadvise(int(out.Fd()), n, size, unix.FADV_DONTNEED)
				unix.Fadvise(int(in.Fd()), n, size, unix.FADV_DONTNEED)
				n += size
			}
			pos = hole
		}
	}
	if stat, ok := st.Sys().(*syscall.Stat_t); ok {
		if err = out.Chown(int(stat.Uid), int(stat.Gid)); err != nil {
			return err
		}
	}
	if err = out.Chmod(st.Mode().Perm()); err != nil {
		return err
	}
	if err = out.Sync(); err != nil {
		return err
	}
	good = true
	return out.Close()
}
func syncSnapshotFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}
func syncSnapshotDir(path string) error { return syncSnapshotFile(path) }

func (m *Manager) RestoreSnapshot(ctx context.Context, name, id, confirm string, memory bool, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	release := true
	defer func() {
		if release {
			lock.Unlock()
		}
	}()
	v, err := m.get(name)
	if err != nil {
		return err
	}
	if confirm != v.Name && confirm != v.Label() {
		return ErrNotConfirmed
	}
	if m.pid(name) != 0 {
		return ErrRunning
	}
	if !m.available(v, mounted) {
		return ErrUnknownVolume
	}
	if m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	s, err := m.readSnapshot(v, id)
	if err != nil {
		return err
	}
	if memory && !s.Memory {
		return ErrInvalid
	}
	if memory {
		runtime, err := m.snapshotRuntime(ctx, s.VM)
		if err != nil {
			return err
		}
		if runtime != s.Runtime {
			return ErrSnapshotRuntime
		}
	}
	needs := map[uint64]int64{}
	paths := map[uint64]string{}
	for _, f := range s.Files {
		src := filepath.Join(m.snapshotRoot(v), id, f.File)
		if err = m.snapshotPath(src, false); err != nil {
			return err
		}
		size, allocated, e := snapshotTreeSize(src)
		if e != nil {
			return e
		}
		st, e := os.Stat(src)
		if e != nil {
			return e
		}
		if st.IsDir() != f.Directory || size != f.Size {
			return ErrBadImage
		}
		if other := m.usedBy(f.Target, v.Name); other != "" {
			return ErrBadImage
		}
		parent := filepath.Dir(f.Target)
		if err = m.snapshotPath(parent, false); err != nil {
			return err
		}
		parentInfo, e := os.Stat(parent)
		if e != nil {
			return e
		}
		dev := parentInfo.Sys().(*syscall.Stat_t).Dev
		needs[dev] += allocated
		paths[dev] = parent
	}
	if memory {
		p := filepath.Join(m.snapshotRoot(v), id, "memory.save")
		if err = m.snapshotPath(p, false); err != nil {
			return err
		}
		if _, err = m.runner.Run(ctx, "", "virsh", "--quiet", "save-image-dumpxml", p); err != nil {
			return err
		}
	}
	budget := map[string]int64{}
	for dev, amount := range needs {
		budget[paths[dev]] = amount + (256 << 20)
	}
	releaseSpace, err := m.reserveSnapshotSpace(budget)
	if err != nil {
		return err
	}
	release = false
	ctx = context.WithoutCancel(ctx)
	m.snapshotJob(name, func() error {
		defer releaseSpace()
		return m.restoreSnapshot(ctx, v, s, memory)
	}, lock)
	return nil
}
func snapshotStaging(f SnapshotFile, id, suffix string) string {
	return f.Target + ".titanos-snapshot-" + id + suffix
}
func (m *Manager) restoreSnapshot(ctx context.Context, v VM, s Snapshot, memory bool) (result error) {
	root := m.snapshotRoot(v)
	store := config.Open(root)
	op := snapshotOperation{Kind: "restore", Phase: "staging", ID: s.ID, Original: v, Files: s.Files, Memory: memory, Runtime: s.Runtime}
	for _, f := range op.Files {
		_, err := os.Lstat(f.Target)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		op.Existing = append(op.Existing, err == nil)
		for _, suffix := range []string{".new", ".old"} {
			if _, err = os.Lstat(snapshotStaging(f, op.ID, suffix)); !errors.Is(err, fs.ErrNotExist) {
				return ErrSnapshotRecovery
			}
		}
	}
	if err := store.WriteJSON("operation.json", op); err != nil {
		return err
	}
	defer func() {
		if result != nil && op.Phase != "committed" && op.Phase != "resume-started" {
			result = errors.Join(result, m.rollbackSnapshot(v, &op))
		}
	}()
	for _, f := range op.Files {
		if err := copySnapshotTree(filepath.Join(root, s.ID, f.File), snapshotStaging(f, s.ID, ".new")); err != nil {
			return err
		}
	}
	op.Phase = "switching"
	if err := store.WriteJSON("operation.json", op); err != nil {
		return err
	}
	for i, f := range op.Files {
		if op.Existing[i] {
			if err := os.Rename(f.Target, snapshotStaging(f, s.ID, ".old")); err != nil {
				return err
			}
			if err := syncSnapshotDir(filepath.Dir(f.Target)); err != nil {
				return err
			}
		}
		if err := os.Rename(snapshotStaging(f, s.ID, ".new"), f.Target); err != nil {
			return err
		}
		if err := syncSnapshotDir(filepath.Dir(f.Target)); err != nil {
			return err
		}
	}
	restored := s.VM
	restored.SnapshotSchedule = v.SnapshotSchedule
	if err := m.store.WriteJSON(configDir+"/"+v.Name+".json", restored); err != nil {
		return err
	}
	m.mu.Lock()
	m.vms[v.Name] = restored
	m.mu.Unlock()
	op.Phase = "committed"
	if err := store.WriteJSON("operation.json", op); err != nil {
		op.Phase = "switching"
		return err
	}
	if memory {
		if err := m.resumeSnapshot(ctx, store, &op, filepath.Join(root, s.ID)); err != nil {
			return err
		}
	}
	for _, f := range op.Files {
		if err := os.RemoveAll(snapshotStaging(f, s.ID, ".old")); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := syncSnapshotDir(filepath.Dir(f.Target)); err != nil {
			return err
		}
	}
	return store.Remove("operation.json")
}
func (m *Manager) rollbackSnapshot(v VM, op *snapshotOperation) error {
	if m.pid(v.Name) != 0 {
		return ErrRunning
	}
	for i, f := range op.Files {
		old, next := snapshotStaging(f, op.ID, ".old"), snapshotStaging(f, op.ID, ".new")
		if _, err := os.Lstat(old); err == nil {
			if err = os.RemoveAll(f.Target); err != nil {
				return err
			}
			if err = os.Rename(old, f.Target); err != nil {
				return err
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		} else if !op.Existing[i] && op.Phase == "switching" {
			if err = os.RemoveAll(f.Target); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		if err := os.RemoveAll(next); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		if err := syncSnapshotDir(filepath.Dir(f.Target)); err != nil {
			return err
		}
	}
	if err := m.store.WriteJSON(configDir+"/"+v.Name+".json", op.Original); err != nil {
		return err
	}
	m.mu.Lock()
	m.vms[v.Name] = op.Original
	m.mu.Unlock()
	return config.Open(m.snapshotRoot(v)).Remove("operation.json")
}

// Memory is never replayed after a resume may already have executed. The owner
// can explicitly discard RAM in that ambiguous case and cold-start later.
func (m *Manager) RecoverSnapshot(ctx context.Context, name, confirm string, discardMemory bool, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	v, err := m.get(name)
	if err != nil {
		return err
	}
	if confirm != v.Name && confirm != v.Label() {
		return ErrNotConfirmed
	}
	if !m.available(v, mounted) {
		return ErrUnknownVolume
	}
	root := m.snapshotRoot(v)
	if err = m.snapshotPath(root, false); err != nil {
		return err
	}
	store := config.Open(root)
	var op snapshotOperation
	if err = store.ReadJSON("operation.json", &op); err != nil {
		return err
	}
	if !snapshotID.MatchString(op.ID) || op.Original.UUID != v.UUID || op.Original.Name != v.Name {
		return ErrInvalid
	}
	dir := filepath.Join(root, op.ID)
	if op.Kind == "restore" {
		s, err := m.readSnapshot(v, op.ID)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(s.Files, op.Files) || len(op.Existing) != len(op.Files) {
			return ErrInvalid
		}
		if op.Phase == "staging" || op.Phase == "switching" {
			return m.rollbackSnapshot(v, &op)
		}
		if op.Phase != "committed" && op.Phase != "resume-started" {
			return ErrInvalid
		}
	} else if op.Kind != "create" {
		return ErrInvalid
	}
	if op.Memory && m.pid(v.Name) == 0 && !discardMemory {
		if op.Phase == "resume-started" {
			return ErrSnapshotRecovery
		}
		runtime, e := m.snapshotRuntime(ctx, op.Original)
		if e != nil {
			return e
		}
		if runtime != op.Runtime {
			return ErrSnapshotRuntime
		}
		p := filepath.Join(dir, "memory.save")
		if err = m.snapshotPath(p, false); err != nil {
			return err
		}
		if _, err = m.runner.Run(ctx, "", "virsh", "--quiet", "save-image-dumpxml", p); err != nil {
			return err
		}
		if err = m.resumeSnapshot(ctx, store, &op, dir); err != nil {
			return err
		}
	}
	if op.Kind == "restore" {
		for _, f := range op.Files {
			if err = os.RemoveAll(snapshotStaging(f, op.ID, ".old")); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
	}
	if err = store.Remove("operation.json"); err != nil {
		return err
	}
	if op.Kind == "create" {
		if _, e := os.Stat(filepath.Join(dir, "snapshot.json")); errors.Is(e, fs.ErrNotExist) {
			os.RemoveAll(dir)
		}
	}
	m.mu.Lock()
	delete(m.snapshotJobs, name)
	m.mu.Unlock()
	return nil
}
func (m *Manager) DeleteSnapshot(name, id, confirm string, mounted []string) error {
	if m.Disabled {
		return ErrDisabled
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	v, err := m.get(name)
	if err != nil {
		return err
	}
	if !m.available(v, mounted) {
		return ErrUnknownVolume
	}
	if m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	s, err := m.readSnapshot(v, id)
	if err != nil {
		if !snapshotID.MatchString(id) {
			return ErrNotFound
		}
		dir := filepath.Join(m.snapshotRoot(v), id)
		if e := m.snapshotPath(dir, false); e != nil {
			return e
		}
		st, e := os.Lstat(dir)
		if e != nil {
			return e
		}
		if !st.IsDir() {
			return ErrInvalid
		}
		s.Name = id
	}
	if confirm != s.Name {
		return ErrNotConfirmed
	}
	return config.Open(m.snapshotRoot(v)).RemoveAll(id)
}
func validSnapshotSchedule(s SnapshotSchedule) bool {
	if !s.Enabled {
		return true
	}
	if _, e := time.Parse("15:04", s.Time); e != nil {
		return false
	}
	if len(s.Days) == 0 || len(s.Days) > 7 || s.Keep < 1 || s.Keep > 100 || s.Memory && !s.AllowPause {
		return false
	}
	seen := map[int]bool{}
	for _, d := range s.Days {
		if d < 0 || d > 6 || seen[d] {
			return false
		}
		seen[d] = true
	}
	return true
}
func (m *Manager) SetSnapshotSchedule(name string, s SnapshotSchedule) error {
	if m.Disabled {
		return ErrDisabled
	}
	if !validSnapshotSchedule(s) {
		return ErrInvalid
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	v, err := m.get(name)
	if err != nil {
		return err
	}
	if s.Enabled && m.snapshotReason(v) != "" {
		return ErrSnapshotUnsupported
	}
	if m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	v.SnapshotSchedule = s
	if err = m.store.WriteJSON(configDir+"/"+name+".json", v); err != nil {
		return err
	}
	m.mu.Lock()
	m.vms[name] = v
	m.mu.Unlock()
	return nil
}
func (m *Manager) runSnapshotSchedules(ctx context.Context, mounted []string, now time.Time) {
	// Go caches time.Local. Use the current NAS setting explicitly so changing
	// the time zone does not require a daemon restart for these schedules.
	if m.Timezone != nil {
		location, err := time.LoadLocation(m.Timezone())
		if err != nil {
			return
		}
		now = now.In(location)
	}
	for _, status := range m.List(mounted) {
		s := status.SnapshotSchedule
		if !status.Available || !s.Enabled || !validSnapshotSchedule(s) || now.Format("15:04") != s.Time || !slices.Contains(s.Days, int(now.Weekday())) {
			continue
		}
		v := status.VM
		root := m.snapshotRoot(v)
		if m.snapshotPath(root, true) != nil {
			continue
		}
		store := config.Open(root)
		key := now.Format("2006-01-02") + "T" + s.Time
		var last struct {
			Key string `json:"key"`
		}
		err := store.ReadJSON("last-scheduled.json", &last)
		if err != nil && !errors.Is(err, fs.ErrNotExist) || last.Key == key {
			continue
		}
		last.Key = key
		if store.WriteJSON("last-scheduled.json", last) != nil {
			continue
		}
		err = m.createSnapshot(ctx, v.Name, SnapshotRequest{Name: now.Format("2006-01-02 15:04"), Memory: s.Memory, AllowPause: s.AllowPause}, mounted, true)
		if err != nil {
			slog.Error("scheduled VM snapshot failed", "vm", v.Name, "err", err)
			m.mu.Lock()
			if job := m.snapshotJobs[v.Name]; job == nil || job.State != "running" {
				m.snapshotJobs[v.Name] = &Job{State: "failed", Text: err.Error()}
			}
			m.mu.Unlock()
		}
	}
}
func (m *Manager) pruneSnapshots(v VM) error {
	keep := v.SnapshotSchedule.Keep
	if keep < 1 {
		return nil
	}
	status, err := m.SnapshotStatus(v.Name, v.volumes())
	if err != nil {
		return err
	}
	count := 0
	for _, s := range status.Snapshots {
		if !s.Automatic {
			continue
		}
		count++
		if count > keep {
			if err = config.Open(m.snapshotRoot(v)).RemoveAll(s.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func snapshotTreeSize(path string) (size, allocated int64, err error) {
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() && !st.IsDir() {
			return ErrBadImage
		}
		if !st.IsDir() {
			size += st.Size()
		}
		allocated += st.Sys().(*syscall.Stat_t).Blocks * 512
		return nil
	})
	return
}
func copySnapshotTree(from, to string) error {
	st, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return copySnapshotFile(from, to)
	}
	if err = os.Mkdir(to, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err = copySnapshotTree(filepath.Join(from, entry.Name()), filepath.Join(to, entry.Name())); err != nil {
			return err
		}
	}
	stat := st.Sys().(*syscall.Stat_t)
	if err = os.Chown(to, int(stat.Uid), int(stat.Gid)); err != nil {
		return err
	}
	if err = os.Chmod(to, st.Mode().Perm()); err != nil {
		return err
	}
	return syncSnapshotDir(to)
}

// ProtectFile prevents the file manager from bypassing VM lifecycle checks by
// renaming an image (or its parent), replacing it with an upload, or deleting
// firmware while a snapshot/guest owns it. VM removal releases these paths.
func (m *Manager) ProtectFile(path string) bool {
	path = filepath.Clean(path)
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	} else if real, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(real, filepath.Base(path))
	}
	overlaps := func(other string) bool {
		return path == other || strings.HasPrefix(path, other+"/") || strings.HasPrefix(other, path+"/")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, v := range m.vms {
		for _, volume := range v.volumes() {
			if overlaps(m.dir(volume, v.folder())) {
				return true
			}
		}
		for _, disk := range v.Disks {
			if !m.isBlock(disk) && overlaps(m.diskPath(v, disk)) {
				return true
			}
		}
	}
	return false
}
