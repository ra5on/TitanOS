package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"
	"time"
	"unsafe"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"golang.org/x/sys/unix"
)

// Whole folders come back from a read-only Btrfs snapshot as a background
// job: either merged into their original place or as a new folder next to
// it. Every file, link and new folder is written under a hidden temporary
// name and only renamed into place when it is complete, so nobody sees half
// a file. The snapshot is only ever opened for reading, nothing outside the
// target is touched, and no symlink is ever followed on either side.
// Jobs live in RAM only; titand forgets them with a restart.

// Restore modes and conflict policies (the names the file copy dialogs use).
const (
	RestoreOriginal = "original"
	RestoreCopy     = "copy"

	RestoreSkip    = "skip"
	RestoreReplace = "replace"
	RestoreRename  = "rename"
)

// Folder restore job states and phases.
const (
	RestoreRunning   = "running"
	RestoreDone      = "done"
	RestoreFailed    = "failed"
	RestoreCancelled = "cancelled"

	RestorePhaseCounting = "counting"
	RestorePhaseCopying  = "copying"
)

// maxRestoreJobs is how many finished folder restores stay listed.
const maxRestoreJobs = 20

// restorePreviewTime bounds the counting walk of a preview.
var restorePreviewTime = 2 * time.Minute

// ErrNoRestore means there is no such folder restore job.
var ErrNoRestore = errors.New("storage: no such snapshot restore")

// SnapshotFolderRequest asks for one folder of a snapshot. UUID binds the
// start to the snapshot a preview looked at.
type SnapshotFolderRequest struct {
	UUID     string `json:"uuid,omitempty"`
	Path     string `json:"path"`
	Mode     string `json:"mode"`
	Conflict string `json:"conflict,omitempty"`
}

// SnapshotFolderPreview is what a folder restore would do.
type SnapshotFolderPreview struct {
	UUID     string `json:"uuid"`
	Path     string `json:"path"`
	Mode     string `json:"mode"`
	Conflict string `json:"conflict"`
	// Target is the folder on the volume that receives the files. For a
	// new folder it is the name free right now; the job may pick the next.
	Target    string `json:"target"`
	Files     int64  `json:"files"`
	Folders   int64  `json:"folders"`
	Links     int64  `json:"links"`
	Special   int64  `json:"special"`
	Bytes     int64  `json:"bytes"`
	Conflicts int64  `json:"conflicts"`
	// Partial means the count stopped at the time limit; the numbers are
	// a lower bound.
	Partial bool `json:"partial"`
}

// SnapshotFolderJob is a running or finished folder restore.
type SnapshotFolderJob struct {
	ID         string     `json:"id"`
	User       string     `json:"user"`
	Volume     string     `json:"volume"`
	Snapshot   string     `json:"snapshot"`
	Path       string     `json:"path"`
	Mode       string     `json:"mode"`
	Conflict   string     `json:"conflict"`
	Target     string     `json:"target"`
	State      string     `json:"state"`
	Phase      string     `json:"phase"`
	TotalFiles int64      `json:"totalFiles"`
	TotalBytes int64      `json:"totalBytes"`
	Files      int64      `json:"files"`
	Bytes      int64      `json:"bytes"`
	Copied     int64      `json:"copied"`
	Cloned     int64      `json:"cloned"`
	Skipped    int64      `json:"skipped"`
	Replaced   int64      `json:"replaced"`
	Renamed    int64      `json:"renamed"`
	Special    int64      `json:"special"`
	Error      string     `json:"error,omitempty"`
	Started    time.Time  `json:"started"`
	Finished   *time.Time `json:"finished,omitempty"`

	cancel context.CancelFunc
}

func validRestoreRequest(req *SnapshotFolderRequest) error {
	switch req.Mode {
	case RestoreOriginal:
		if req.Conflict != RestoreSkip && req.Conflict != RestoreReplace && req.Conflict != RestoreRename {
			return fmt.Errorf("%w: choose skip, replace or rename for conflicts", ErrNotPossible)
		}
	case RestoreCopy:
		// A new folder never meets existing files.
		req.Conflict = RestoreRename
	default:
		return fmt.Errorf("%w: unknown restore mode", ErrNotPossible)
	}
	if req.Path == "" || req.Path == "." || !safeSnapshotPath(req.Path) {
		return fmt.Errorf("%w: choose a folder inside the snapshot", ErrNotPossible)
	}
	return nil
}

// restoreSource checks name and folder and returns the snapshot's identity.
// Caller holds actions.
func (m *Manager) restoreSource(ctx context.Context, volume, name string, req *SnapshotFolderRequest) (string, error) {
	if m.GrowthPending() {
		return "", ErrBusy
	}
	if err := validRestoreRequest(req); err != nil {
		return "", err
	}
	root, err := m.openSnapshot(volume, name)
	if err != nil {
		return "", err
	}
	defer root.Close()
	if err := noSnapshotLinks(root, req.Path); err != nil {
		return "", err
	}
	st, err := root.Lstat(req.Path)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return "", fmt.Errorf("%w: choose a folder", ErrNotPossible)
	}
	return m.snapshotIdentity(ctx, volume, name)
}

// PreviewSnapshotFolder counts what a folder restore would bring back and how
// many names already exist at the original place. It writes nothing.
func (m *Manager) PreviewSnapshotFolder(ctx context.Context, volume, name string, req SnapshotFolderRequest) (SnapshotFolderPreview, error) {
	m.actions.Lock()
	uuid, err := m.restoreSource(ctx, volume, name, &req)
	m.actions.Unlock()
	out := SnapshotFolderPreview{UUID: uuid, Path: req.Path, Mode: req.Mode, Conflict: req.Conflict}
	if err != nil {
		return out, err
	}
	r, err := m.openRestore(volume, name, req)
	if err != nil {
		return out, err
	}
	defer r.close()
	limited, cancel := context.WithTimeout(ctx, restorePreviewTime)
	defer cancel()
	var c restoreCount
	err = r.count(limited, r.src(), r.dst(), req.Path, &c)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		out.Partial, err = true, nil
	}
	if err != nil {
		return out, err
	}
	out.Files, out.Folders, out.Links, out.Special, out.Bytes, out.Conflicts = c.files, c.folders, c.links, c.special, c.bytes, c.conflicts
	out.Target = req.Path
	if req.Mode == RestoreCopy {
		target, err := freeName(int(r.liveDirs[len(r.parts)-1].Fd()), copyName(req.Path, name), true)
		if err != nil {
			return out, err
		}
		out.Target = path.Join(path.Dir(req.Path), target)
	}
	return out, nil
}

// StartSnapshotFolderRestore checks the request once more against the
// snapshot a preview saw and starts the job in the background.
func (m *Manager) StartSnapshotFolderRestore(ctx context.Context, user, volume, name string, req SnapshotFolderRequest) (SnapshotFolderJob, error) {
	m.actions.Lock()
	defer m.actions.Unlock()
	if req.UUID == "" {
		return SnapshotFolderJob{}, fmt.Errorf("%w: preview the restore first", ErrNotPossible)
	}
	uuid, err := m.restoreSource(ctx, volume, name, &req)
	if err != nil {
		return SnapshotFolderJob{}, err
	}
	if uuid != req.UUID {
		return SnapshotFolderJob{}, fmt.Errorf("%w: snapshot identity changed; preview again", ErrNotPossible)
	}
	r, err := m.openRestore(volume, name, req)
	if err != nil {
		return SnapshotFolderJob{}, err
	}
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		r.close()
		return SnapshotFolderJob{}, err
	}
	work, cancel := context.WithCancel(context.Background())
	job := &SnapshotFolderJob{
		ID: hex.EncodeToString(raw[:]), User: user, Volume: volume, Snapshot: name, Path: req.Path,
		Mode: req.Mode, Conflict: req.Conflict, Target: req.Path, State: RestoreRunning,
		Phase: RestorePhaseCounting, Started: time.Now(), cancel: cancel,
	}
	m.restoreMu.Lock()
	for _, other := range m.restores {
		if other.State == RestoreRunning {
			m.restoreMu.Unlock()
			cancel()
			r.close()
			return SnapshotFolderJob{}, fmt.Errorf("%w: another snapshot restore is running", ErrBusy)
		}
	}
	m.restores = append(m.restores, job)
	m.trimRestores()
	copied := *job
	m.restoreMu.Unlock()
	r.job, r.m = job, m
	slog.Info("snapshot folder restore started", "id", job.ID, "volume", volume, "snapshot", name, "path", req.Path, "mode", req.Mode, "conflict", req.Conflict, "user", user)
	go m.runRestore(work, r)
	return copied, nil
}

// trimRestores drops the oldest finished jobs. Caller holds restoreMu.
func (m *Manager) trimRestores() {
	for finished := 0; ; finished = 0 {
		oldest := -1
		for i, job := range m.restores {
			if job.State != RestoreRunning {
				finished++
				if oldest < 0 {
					oldest = i
				}
			}
		}
		if finished <= maxRestoreJobs {
			return
		}
		m.restores = append(m.restores[:oldest], m.restores[oldest+1:]...)
	}
}

// SnapshotRestores returns the folder restores, newest first.
func (m *Manager) SnapshotRestores() []SnapshotFolderJob {
	m.restoreMu.Lock()
	defer m.restoreMu.Unlock()
	out := make([]SnapshotFolderJob, 0, len(m.restores))
	for i := len(m.restores) - 1; i >= 0; i-- {
		out = append(out, *m.restores[i])
	}
	return out
}

// CancelSnapshotRestore stops a running folder restore. Files already in
// place stay; the folder or file being written is removed.
func (m *Manager) CancelSnapshotRestore(id string) error {
	m.restoreMu.Lock()
	defer m.restoreMu.Unlock()
	for _, job := range m.restores {
		if job.ID == id {
			if job.State == RestoreRunning {
				job.cancel()
			}
			return nil
		}
	}
	return ErrNoRestore
}

// ForgetSnapshotRestore removes a finished job from the list.
func (m *Manager) ForgetSnapshotRestore(id string) error {
	m.restoreMu.Lock()
	defer m.restoreMu.Unlock()
	for i, job := range m.restores {
		if job.ID == id {
			if job.State == RestoreRunning {
				return ErrBusy
			}
			m.restores = append(m.restores[:i], m.restores[i+1:]...)
			return nil
		}
	}
	return ErrNoRestore
}

// SnapshotRestoresRunning tells app updates and maintenance to wait.
func (m *Manager) SnapshotRestoresRunning() bool {
	m.restoreMu.Lock()
	defer m.restoreMu.Unlock()
	for _, job := range m.restores {
		if job.State == RestoreRunning {
			return true
		}
	}
	return false
}

// restoreUses reports whether a running restore reads from the volume, or
// from the given snapshot when it is not empty.
func (m *Manager) restoreUses(volume, snapshot string) bool {
	m.restoreMu.Lock()
	defer m.restoreMu.Unlock()
	for _, job := range m.restores {
		if job.State == RestoreRunning && job.Volume == volume && (snapshot == "" || job.Snapshot == snapshot) {
			return true
		}
	}
	return false
}

func (m *Manager) updateRestore(job *SnapshotFolderJob, change func(*SnapshotFolderJob)) {
	m.restoreMu.Lock()
	change(job)
	m.restoreMu.Unlock()
}

func (m *Manager) runRestore(ctx context.Context, r *folderRestore) {
	defer r.close()
	job := r.job
	var c restoreCount
	err := r.count(ctx, r.src(), nil, job.Path, &c)
	if err == nil {
		m.updateRestore(job, func(j *SnapshotFolderJob) {
			j.TotalFiles, j.TotalBytes, j.Phase = c.files+c.links, c.bytes, RestorePhaseCopying
		})
		err = r.restore(ctx)
	}
	now := time.Now()
	m.updateRestore(job, func(j *SnapshotFolderJob) {
		j.Finished = &now
		switch {
		case ctx.Err() != nil:
			j.State = RestoreCancelled
		case err != nil:
			j.State, j.Error = RestoreFailed, err.Error()
		default:
			j.State = RestoreDone
		}
	})
	job.cancel()
	m.restoreMu.Lock()
	done := *job
	m.restoreMu.Unlock()
	slog.Info("snapshot folder restore finished", "id", done.ID, "volume", done.Volume, "snapshot", done.Snapshot, "path", done.Path, "target", done.Target, "state", done.State, "files", done.Copied, "skipped", done.Skipped, "err", done.Error)
	if m.OnSnapshotRestoreDone != nil {
		m.OnSnapshotRestoreDone(done)
	}
}

// folderRestore holds the pinned descriptors of one restore.
type folderRestore struct {
	m   *Manager
	job *SnapshotFolderJob
	req SnapshotFolderRequest
	// snapshot is the snapshot name, for the name of a new folder.
	snapshot string
	parts    []string
	// srcDirs[i] is the snapshot folder of parts[:i]; liveDirs[i] the live
	// folder of parts[:i] as far as it exists (len(liveDirs)-1 is the
	// deepest existing level).
	srcDirs, liveDirs []*os.File
	mount             uint64
}

func (r *folderRestore) close() {
	for _, f := range append(r.srcDirs, r.liveDirs...) {
		f.Close()
	}
	r.srcDirs, r.liveDirs = nil, nil
}

// src is the snapshot folder to restore.
func (r *folderRestore) src() *os.File { return r.srcDirs[len(r.parts)] }

// dst is the existing live folder at the original place, or nil.
func (r *folderRestore) dst() *os.File {
	if len(r.liveDirs) == len(r.parts)+1 {
		return r.liveDirs[len(r.parts)]
	}
	return nil
}

func openDirAt(dir *os.File, name string) (*os.File, error) {
	fd, err := unix.Openat(int(dir.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), name), nil
}

func mountOf(f *os.File) (uint64, error) {
	var stx unix.Statx_t
	if err := unix.Statx(int(f.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &stx); err != nil {
		return 0, err
	}
	if stx.Mask&unix.STATX_MNT_ID == 0 {
		return 0, errors.New("mount identity unavailable")
	}
	return stx.Mnt_id, nil
}

// openRestore pins the snapshot folders and the live folders on the way to
// the target. Path components that are symlinks or other mounts are refused.
func (m *Manager) openRestore(volume, name string, req SnapshotFolderRequest) (_ *folderRestore, result error) {
	r := &folderRestore{req: req, snapshot: name, parts: strings.Split(req.Path, "/")}
	defer func() {
		if result != nil {
			r.close()
		}
	}()
	snapRoot, err := m.openSnapshot(volume, name)
	if err != nil {
		return nil, err
	}
	snapDir, err := snapRoot.Open(".")
	snapRoot.Close()
	if err != nil {
		return nil, err
	}
	r.srcDirs = append(r.srcDirs, snapDir)
	volRoot, err := volumedata.OpenVolume(m.mountRoot, volume, m.Mounted())
	if err != nil {
		return nil, err
	}
	live, err := volRoot.Open(".")
	volRoot.Close()
	if err != nil {
		return nil, err
	}
	r.liveDirs = append(r.liveDirs, live)
	if r.mount, err = mountOf(live); err != nil {
		return nil, err
	}
	for _, part := range r.parts {
		next, err := openDirAt(r.srcDirs[len(r.srcDirs)-1], part)
		if err != nil {
			return nil, err
		}
		r.srcDirs = append(r.srcDirs, next)
	}
	for _, part := range r.parts {
		next, err := r.liveDir(r.liveDirs[len(r.liveDirs)-1], part)
		if errors.Is(err, os.ErrNotExist) {
			break
		}
		if err != nil {
			return nil, err
		}
		r.liveDirs = append(r.liveDirs, next)
	}
	if req.Mode == RestoreCopy && len(r.liveDirs) < len(r.parts) {
		return nil, fmt.Errorf("%w: the parent folder no longer exists; restore to the original place", ErrNotPossible)
	}
	return r, nil
}

// liveDir opens an existing live folder by name, refusing symlinks, files
// and other mounts.
func (r *folderRestore) liveDir(parent *os.File, name string) (*os.File, error) {
	var st unix.Stat_t
	if err := unix.Fstatat(int(parent.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		if errors.Is(err, unix.ENOENT) {
			return nil, os.ErrNotExist
		}
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR {
		return nil, fmt.Errorf("%w: %s on the volume is not a folder", ErrNotPossible, name)
	}
	dir, err := openDirAt(parent, name)
	if err != nil {
		return nil, err
	}
	if id, err := mountOf(dir); err != nil || id != r.mount {
		dir.Close()
		return nil, fmt.Errorf("%w: %s is another mount", ErrNotPossible, name)
	}
	return dir, nil
}

type restoreCount struct {
	files, folders, links, special, bytes, conflicts int64
}

// readNames reads the names of a folder in batches.
func readNames(dir *os.File, each func(name string) error) error {
	fd, err := unix.Dup(int(dir.Fd()))
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), dir.Name())
	defer f.Close()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	for {
		names, err := f.Readdirnames(1024)
		sort.Strings(names)
		for _, name := range names {
			if err := each(name); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) || (err == nil && len(names) == 0) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// count walks a snapshot folder; with a live folder it also counts the names
// that would conflict at the original place.
func (r *folderRestore) count(ctx context.Context, src, dst *os.File, rel string, c *restoreCount) error {
	return readNames(src, func(name string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		child := path.Join(rel, name)
		if !safeSnapshotPath(child) {
			return nil
		}
		var st unix.Stat_t
		if err := unix.Fstatat(int(src.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		var live unix.Stat_t
		exists := false
		if dst != nil && r.req.Mode == RestoreOriginal {
			err := unix.Fstatat(int(dst.Fd()), name, &live, unix.AT_SYMLINK_NOFOLLOW)
			if err != nil && !errors.Is(err, unix.ENOENT) {
				return err
			}
			exists = err == nil
		}
		switch st.Mode & unix.S_IFMT {
		case unix.S_IFDIR:
			c.folders++
			if exists && live.Mode&unix.S_IFMT != unix.S_IFDIR {
				c.conflicts++
			}
			sub, err := openDirAt(src, name)
			if err != nil {
				return err
			}
			defer sub.Close()
			var subDst *os.File
			if exists && live.Mode&unix.S_IFMT == unix.S_IFDIR {
				if d, err := r.liveDir(dst, name); err == nil {
					subDst = d
					defer d.Close()
				}
			}
			return r.count(ctx, sub, subDst, child, c)
		case unix.S_IFREG:
			c.files++
			c.bytes += st.Size
		case unix.S_IFLNK:
			c.links++
		default:
			c.special++
			return nil
		}
		if exists {
			c.conflicts++
		}
		return nil
	})
}

func restoreTemp() string {
	var raw [12]byte
	rand.Read(raw[:])
	return ".titanos-restore-" + hex.EncodeToString(raw[:]) + ".titanos-part"
}

// copyName is the name of a new folder next to the original.
func copyName(folder, snapshot string) string {
	return path.Base(folder) + " (" + snapshot + ")"
}

// keepBothName is "name (n).ext" for files and "name (n)" for folders.
func keepBothName(name string, n int, dir bool) string {
	ext := ""
	if !dir {
		if ext = path.Ext(name); ext == name {
			ext = ""
		}
	}
	return strings.TrimSuffix(name, ext) + fmt.Sprintf(" (%d)", n) + ext
}

// freeName returns name or the first free "keep both" variant of it.
func freeName(dir int, name string, isDir bool) (string, error) {
	for n := 1; n < 10000; n++ {
		candidate := name
		if n > 1 {
			candidate = keepBothName(name, n, isDir)
		}
		if len(candidate) > 255 {
			return "", fmt.Errorf("%w: name too long", ErrNotPossible)
		}
		var st unix.Stat_t
		err := unix.Fstatat(dir, candidate, &st, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", ErrExists
}

// removeAllAt removes a temporary tree this restore created, never following links.
func removeAllAt(dir int, name string) error {
	err := unix.Unlinkat(dir, name, 0)
	if err == nil || errors.Is(err, unix.ENOENT) {
		return nil
	}
	fd, err := unix.Openat(dir, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), name)
	names, _ := f.Readdirnames(-1)
	for _, n := range names {
		if err := removeAllAt(int(f.Fd()), n); err != nil {
			f.Close()
			return err
		}
	}
	f.Close()
	return unix.Unlinkat(dir, name, unix.AT_REMOVEDIR)
}

func (r *folderRestore) progress(change func(*SnapshotFolderJob)) {
	if r.m != nil && r.job != nil {
		r.m.updateRestore(r.job, change)
	}
}

// restore carries out the job on the pinned descriptors.
func (r *folderRestore) restore(ctx context.Context) error {
	n := len(r.parts)
	switch {
	case r.req.Mode == RestoreCopy:
		target, err := r.newFolder(ctx, r.src(), r.liveDirs[n-1], copyName(r.req.Path, r.snapshot), r.req.Path, nil, true)
		if err != nil {
			return err
		}
		r.progress(func(j *SnapshotFolderJob) { j.Target = path.Join(path.Dir(r.req.Path), target) })
		return nil
	case r.dst() != nil:
		return r.merge(ctx, r.src(), r.dst(), r.req.Path)
	default:
		// The folder, perhaps also its parents, is gone: build the missing
		// chain under a temporary name and publish its top at once.
		k := len(r.liveDirs) - 1
		_, err := r.newFolder(ctx, r.srcDirs[k+1], r.liveDirs[k], r.parts[k], strings.Join(r.parts[:k+1], "/"), r.srcDirs[k+2:], false)
		return err
	}
}

// merge restores the contents of a snapshot folder into an existing live
// folder. The live folder's own metadata stays as it is.
func (r *folderRestore) merge(ctx context.Context, src, dst *os.File, rel string) error {
	return readNames(src, func(name string) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		child := path.Join(rel, name)
		if !safeSnapshotPath(child) {
			return nil
		}
		var st unix.Stat_t
		if err := unix.Fstatat(int(src.Fd()), name, &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
			return err
		}
		kind := st.Mode & unix.S_IFMT
		if kind != unix.S_IFDIR && kind != unix.S_IFREG && kind != unix.S_IFLNK {
			r.progress(func(j *SnapshotFolderJob) { j.Special++ })
			return nil
		}
		var live unix.Stat_t
		err := unix.Fstatat(int(dst.Fd()), name, &live, unix.AT_SYMLINK_NOFOLLOW)
		if errors.Is(err, unix.ENOENT) {
			return r.create(ctx, src, dst, name, name, child, &st, false)
		}
		if err != nil {
			return err
		}
		liveDir := live.Mode&unix.S_IFMT == unix.S_IFDIR
		if kind == unix.S_IFDIR && liveDir {
			sub, err := openDirAt(src, name)
			if err != nil {
				return err
			}
			defer sub.Close()
			subDst, err := r.liveDir(dst, name)
			if err != nil {
				return err
			}
			defer subDst.Close()
			return r.merge(ctx, sub, subDst, child)
		}
		policy := r.req.Conflict
		// Replace only swaps a file or link for another; a folder is never
		// deleted to make room, nor replaced by a file. Both are kept.
		if policy == RestoreReplace && (kind == unix.S_IFDIR || liveDir) {
			policy = RestoreRename
		}
		switch policy {
		case RestoreSkip:
			return r.skip(ctx, src, name, child, &st)
		case RestoreReplace:
			return r.create(ctx, src, dst, name, name, child, &st, true)
		default:
			target, err := freeName(int(dst.Fd()), name, kind == unix.S_IFDIR)
			if err != nil {
				return err
			}
			r.progress(func(j *SnapshotFolderJob) { j.Renamed++ })
			return r.create(ctx, src, dst, name, target, child, &st, false)
		}
	})
}

// skip counts what a conflict leaves out, so that progress still reaches the end.
func (r *folderRestore) skip(ctx context.Context, src *os.File, name, rel string, st *unix.Stat_t) error {
	var c restoreCount
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		sub, err := openDirAt(src, name)
		if err != nil {
			return err
		}
		err = r.count(ctx, sub, nil, rel, &c)
		sub.Close()
		if err != nil {
			return err
		}
	case unix.S_IFREG:
		c.files, c.bytes = 1, st.Size
	default:
		c.links = 1
	}
	r.progress(func(j *SnapshotFolderJob) {
		j.Skipped += c.files + c.links
		j.Files += c.files + c.links
		j.Bytes += c.bytes
	})
	return nil
}

// create writes one entry under a temporary name and publishes it as target.
// replace swaps it for an existing file or link in one step.
func (r *folderRestore) create(ctx context.Context, src, dst *os.File, name, target, rel string, st *unix.Stat_t, replace bool) (err error) {
	switch st.Mode & unix.S_IFMT {
	case unix.S_IFDIR:
		sub, err := openDirAt(src, name)
		if err != nil {
			return err
		}
		defer sub.Close()
		_, err = r.newFolder(ctx, sub, dst, target, rel, nil, false)
		return err
	case unix.S_IFREG:
		err = r.copyFile(ctx, src, dst, name, target, st, replace)
	default:
		err = r.copyLink(src, dst, name, target, st, replace)
	}
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("%s: %w", rel, err)
	}
	return err
}

// publish renames a finished temporary entry into place.
func publish(dir int, temp, target string, replace bool) error {
	if replace {
		return unix.Renameat(dir, temp, dir, target)
	}
	return unix.Renameat2(dir, temp, dir, target, unix.RENAME_NOREPLACE)
}

// newFolder builds a folder under a temporary name in dst and publishes it.
// chain are snapshot folders to create inside, one per level, ending with
// the folder whose contents are restored. pickFree chooses the next free
// "keep both" name when target appeared meanwhile.
func (r *folderRestore) newFolder(ctx context.Context, src, dst *os.File, target, rel string, chain []*os.File, pickFree bool) (string, error) {
	temp := restoreTemp()
	dir := int(dst.Fd())
	if err := unix.Mkdirat(dir, temp, 0700); err != nil {
		return "", fmt.Errorf("%s: %w", rel, err)
	}
	done := false
	defer func() {
		if !done {
			removeAllAt(dir, temp)
		}
	}()
	made, err := openDirAt(dst, temp)
	if err != nil {
		return "", err
	}
	defer made.Close()
	if len(chain) > 0 {
		// Only the next folder of the missing chain is created here.
		next := chain[0]
		if _, err := r.newFolder(ctx, next, made, path.Base(next.Name()), path.Join(rel, path.Base(next.Name())), chain[1:], false); err != nil {
			return "", err
		}
	} else if err := r.merge(ctx, src, made, rel); err != nil {
		return "", err
	}
	if err := applyMeta(src, made); err != nil {
		return "", err
	}
	if err := made.Sync(); err != nil {
		return "", err
	}
	if pickFree {
		for {
			free, err := freeName(dir, target, true)
			if err != nil {
				return "", err
			}
			err = publish(dir, temp, free, false)
			if errors.Is(err, unix.EEXIST) {
				continue
			}
			if err != nil {
				return "", err
			}
			target = free
			break
		}
	} else if err := publish(dir, temp, target, false); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return "", fmt.Errorf("%w: %s appeared on the volume meanwhile", ErrExists, path.Join(path.Dir(rel), target))
		}
		return "", err
	}
	done = true
	return target, dst.Sync()
}

// applyMeta copies owner, mode, extended attributes (and with them ACLs) and
// times from a snapshot entry to its restored copy.
func applyMeta(src, dst *os.File) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(src.Fd()), &st); err != nil {
		return err
	}
	if err := unix.Fchown(int(dst.Fd()), int(st.Uid), int(st.Gid)); err != nil && !(errors.Is(err, unix.EPERM) && os.Geteuid() != 0) {
		return err
	}
	if err := copyXattrs(int(src.Fd()), int(dst.Fd())); err != nil {
		return err
	}
	// After chown, which clears set-user-ID bits.
	if err := unix.Fchmod(int(dst.Fd()), st.Mode&07777); err != nil {
		return err
	}
	return futimens(int(dst.Fd()), st.Atim, st.Mtim)
}

// futimens sets the times of an open file (utimensat with a NULL path).
func futimens(fd int, atime, mtime unix.Timespec) error {
	times := [2]unix.Timespec{atime, mtime}
	_, _, errno := unix.Syscall6(unix.SYS_UTIMENSAT, uintptr(fd), 0, uintptr(unsafe.Pointer(&times[0])), 0, 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}

func copyXattrs(src, dst int) error {
	size, err := unix.Flistxattr(src, nil)
	if errors.Is(err, unix.ENOTSUP) || size == 0 {
		return nil
	}
	if err != nil {
		return err
	}
	list := make([]byte, size)
	if size, err = unix.Flistxattr(src, list); err != nil {
		return err
	}
	for _, name := range strings.Split(string(list[:size]), "\x00") {
		if name == "" {
			continue
		}
		n, err := unix.Fgetxattr(src, name, nil)
		if err != nil {
			return err
		}
		value := make([]byte, n)
		if n, err = unix.Fgetxattr(src, name, value); err != nil {
			return err
		}
		if err := unix.Fsetxattr(dst, name, value[:n], 0); err != nil {
			// Unprivileged tests cannot set trusted/security names; titand runs as root.
			if errors.Is(err, unix.ENOTSUP) || (errors.Is(err, unix.EPERM) && os.Geteuid() != 0) {
				continue
			}
			return fmt.Errorf("extended attribute %s: %w", name, err)
		}
	}
	return nil
}

// copyFile clones a file (reflink) or copies it when the filesystem cannot.
func (r *folderRestore) copyFile(ctx context.Context, srcDir, dstDir *os.File, name, target string, st *unix.Stat_t, replace bool) error {
	if r.m != nil && r.m.restoreStep != nil {
		r.m.restoreStep(name)
	}
	fd, err := unix.Openat(int(srcDir.Fd()), name, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	in := os.NewFile(uintptr(fd), name)
	defer in.Close()
	var now unix.Stat_t
	if err := unix.Fstat(fd, &now); err != nil {
		return err
	}
	if now.Mode&unix.S_IFMT != unix.S_IFREG || now.Ino != st.Ino || now.Dev != st.Dev {
		return fmt.Errorf("snapshot file %s changed", name)
	}
	temp := restoreTemp()
	dir := int(dstDir.Fd())
	ofd, err := unix.Openat(dir, temp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0600)
	if err != nil {
		return err
	}
	out := os.NewFile(uintptr(ofd), temp)
	done := false
	defer func() {
		out.Close()
		if !done {
			unix.Unlinkat(dir, temp, 0)
		}
	}()
	cloned := unix.IoctlFileClone(ofd, fd) == nil
	if cloned {
		r.progress(func(j *SnapshotFolderJob) { j.Bytes += now.Size })
	} else {
		var copied int64
		for copied < now.Size {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			n, err := io.CopyN(out, in, min(8<<20, now.Size-copied))
			copied += n
			r.progress(func(j *SnapshotFolderJob) { j.Bytes += n })
			if err != nil {
				return err
			}
		}
	}
	if err := applyMeta(in, out); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	if err := publish(dir, temp, target, replace); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("%w: %s appeared on the volume meanwhile", ErrExists, target)
		}
		return err
	}
	done = true
	r.progress(func(j *SnapshotFolderJob) {
		j.Files++
		j.Copied++
		if cloned {
			j.Cloned++
		}
		if replace {
			j.Replaced++
		}
	})
	return nil
}

// copyLink recreates a symbolic link with its owner and times. The link is
// copied as it is and never followed.
func (r *folderRestore) copyLink(srcDir, dstDir *os.File, name, target string, st *unix.Stat_t, replace bool) error {
	buf := make([]byte, 4096)
	n, err := unix.Readlinkat(int(srcDir.Fd()), name, buf)
	if err != nil {
		return err
	}
	if n >= len(buf) {
		return fmt.Errorf("%w: link %s is too long", ErrNotPossible, name)
	}
	temp := restoreTemp()
	dir := int(dstDir.Fd())
	if err := unix.Symlinkat(string(buf[:n]), dir, temp); err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			unix.Unlinkat(dir, temp, 0)
		}
	}()
	if err := unix.Fchownat(dir, temp, int(st.Uid), int(st.Gid), unix.AT_SYMLINK_NOFOLLOW); err != nil && !(errors.Is(err, unix.EPERM) && os.Geteuid() != 0) {
		return err
	}
	if err := unix.UtimesNanoAt(dir, temp, []unix.Timespec{st.Atim, st.Mtim}, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return err
	}
	if err := publish(dir, temp, target, replace); err != nil {
		if errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("%w: %s appeared on the volume meanwhile", ErrExists, target)
		}
		return err
	}
	done = true
	r.progress(func(j *SnapshotFolderJob) {
		j.Files++
		j.Copied++
		if replace {
			j.Replaced++
		}
	})
	return nil
}
