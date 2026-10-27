package shares

// Deleting the data of a share together with the share. Removing a share
// normally keeps its folder; on request the folder goes too, as a job in the
// background and only after the share itself is gone from the configuration
// and from Samba and NFS.
//
// Only exactly the folder of the share is deleted: /mnt/<volume>/<name> on a
// mounted TitanOS volume, opened without following links, on the volume's
// own mount. A folder holding another mount or a nested Btrfs subvolume, or
// one that something else on the NAS uses, is refused. The folder is first
// renamed into .titanos/share-delete/<id> on the same volume, next to a small
// note; a deletion broken off by a crash is finished from there once the
// volume is mounted again.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const (
	purgeFeature = "share-delete"
	PreviewTime  = 20 * time.Second
	maxDepth     = 512
	maxListed    = 20
	keepPurges   = 20
	btrfsMagic   = 0x9123683e
	// Every Btrfs subvolume has this inode number at its root.
	subvolumeInode = 256
)

// Kinds of Usage. The first group blocks deleting the data, warnings do not.
const (
	UseVM            = "vm"
	UseDocker        = "docker"
	UseDockerUnknown = "docker-unknown"
	UseBackupSource  = "backup-source"
	UseBackupTarget  = "backup-target"
	UseShare         = "share"
	UseSMB           = "smb-session"
	UseSMBUnknown    = "smb-unknown"
	UseMount         = "mount"
	UseSubvolume     = "nested-subvolume"
	UseSnapshots     = "subvolume-snapshots"
	UseSnapUnknown   = "subvolume-unknown"
	UsePath          = "unsafe-path"
	UseRunning       = "purge-running"
	// Warnings.
	UseVolumeSnapshots = "volume-snapshots"
	UseLimit           = "count-limit"
)

// previewEntries is the most the preview counts; the job itself counts all.
var previewEntries uint64 = 200000

var (
	ErrPurgeBlocked = errors.New("shares: the folder of the share is in use")
	ErrPurgeRunning = errors.New("shares: the data of a share is being deleted")
	ErrPurgeUnsafe  = errors.New("shares: the folder cannot be deleted safely")
	ErrPurgeChanged = errors.New("shares: the share changed since the preview")
)

// Usage is something that uses the folder of a share, or a reason it
// cannot be deleted.
type Usage struct {
	Kind string `json:"kind"`
	Name string `json:"name,omitempty"`
	Path string `json:"path,omitempty"`
}

// BlockedError lists what stands in the way of deleting the data.
type BlockedError struct{ Usages []Usage }

func (e *BlockedError) Error() string { return ErrPurgeBlocked.Error() }
func (e *BlockedError) Unwrap() error { return ErrPurgeBlocked }

// DataPreview tells what deleting a share's data would delete.
type DataPreview struct {
	Share  string `json:"share"`
	Volume string `json:"volume"`
	Path   string `json:"path"`
	// Exists is false if the folder is not there: only the share goes.
	Exists bool   `json:"exists"`
	Files  uint64 `json:"files"`
	Dirs   uint64 `json:"dirs"`
	Bytes  uint64 `json:"bytes"`
	// Complete is false when counting stopped at Limit (entries, time, depth).
	Complete  bool    `json:"complete"`
	Limit     string  `json:"limit,omitempty"`
	Subvolume bool    `json:"subvolume"`
	Blockers  []Usage `json:"blockers"`
	Warnings  []Usage `json:"warnings"`
}

// PurgeJob is the deletion of a share's data.
type PurgeJob struct {
	ID     string `json:"id"`
	Share  string `json:"share"`
	Volume string `json:"volume"`
	Path   string `json:"path"`
	By     string `json:"by,omitempty"`
	// Phase is checking, removing, waiting, deleting, done or failed.
	Phase      string     `json:"phase"`
	Started    time.Time  `json:"started"`
	Finished   *time.Time `json:"finished,omitempty"`
	Files      uint64     `json:"files"`
	Bytes      uint64     `json:"bytes"`
	TotalFiles uint64     `json:"totalFiles"`
	TotalBytes uint64     `json:"totalBytes"`
	Subvolume  bool       `json:"subvolume"`
	// ShareRemoved tells whether the share itself is gone; DataKept that
	// nothing of the folder was deleted.
	ShareRemoved bool    `json:"shareRemoved"`
	DataKept     bool    `json:"dataKept"`
	Resumed      bool    `json:"resumed,omitempty"`
	Error        string  `json:"error,omitempty"`
	Blockers     []Usage `json:"blockers,omitempty"`
}

// PurgeRequest starts a deletion.
type PurgeRequest struct {
	Name string
	// Path is the folder the preview showed; it must still be the share's.
	Path    string
	By      string
	Mounted func() []string
	// Check returns what else on the NAS uses the path: VMs, apps, backups.
	// It is asked before the share is removed and again right before deleting.
	Check func(ctx context.Context, path string) []Usage
	// Done is told the outcome.
	Done func(PurgeJob)
}

type purgeState struct {
	mu      sync.Mutex
	running *PurgeJob
	results []PurgeJob
	resumed map[string]bool
	// resumedDone is told how a deletion ended that a restart of titand
	// had broken off.
	resumedDone func(PurgeJob)
	// For tests: how Btrfs subvolumes are recognised, how long to wait.
	subvolume func(dir *os.File, st *unix.Stat_t) (bool, error)
	wait      time.Duration
	poll      time.Duration
}

// purgeNote is kept next to the folder while it is being deleted.
type purgeNote struct {
	Share   string    `json:"share"`
	Volume  string    `json:"volume"`
	By      string    `json:"by"`
	Started time.Time `json:"started"`
}

// Overlaps reports whether one path is the other or lies inside it.
func Overlaps(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

// PurgeBusy reports whether the data of a share is being deleted.
func (m *Manager) PurgeBusy() bool {
	m.purge.mu.Lock()
	defer m.purge.mu.Unlock()
	return m.purge.running != nil
}

// PurgeJobs returns the running deletion and the last ones that ended.
func (m *Manager) PurgeJobs() []PurgeJob {
	m.purge.mu.Lock()
	defer m.purge.mu.Unlock()
	out := []PurgeJob{}
	if m.purge.running != nil {
		out = append(out, *m.purge.running)
	}
	for i := len(m.purge.results) - 1; i >= 0; i-- {
		out = append(out, m.purge.results[i])
	}
	return out
}

func (m *Manager) share(name string) (Share, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.shares, func(s Share) bool { return s.Name == name })
	if i < 0 {
		return Share{}, false
	}
	return m.shares[i], true
}

// folder is the share's folder, opened without following links.
type folder struct {
	volume *os.Root
	vol    *os.File
	dir    *os.File // nil if the folder does not exist
	stat   unix.Stat_t
	sub    bool
}

func (f *folder) Close() {
	if f.dir != nil {
		f.dir.Close()
	}
	f.vol.Close()
	f.volume.Close()
}

// openFolder opens exactly mountRoot/volume/name: the volume must be a
// mounted TitanOS volume and the folder a real directory on its mount.
func (m *Manager) openFolder(volume, name string, mounted []string) (*folder, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("%w: not a share name", ErrPurgeUnsafe)
	}
	root, err := volumedata.OpenVolume(m.mountRoot, volume, mounted)
	if err != nil {
		return nil, fmt.Errorf("%w: the volume is not mounted", ErrPurgeUnsafe)
	}
	vol, err := root.Open(".")
	if err != nil {
		root.Close()
		return nil, err
	}
	f := &folder{volume: root, vol: vol}
	fd, err := unix.Openat(int(vol.Fd()), name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return f, nil
	}
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("%w: not a folder (or a link)", ErrPurgeUnsafe)
	}
	f.dir = os.NewFile(uintptr(fd), filepath.Join(m.mountRoot, volume, name))
	if err := unix.Fstat(fd, &f.stat); err != nil {
		f.Close()
		return nil, err
	}
	same, err := sameMount(vol, f.dir)
	if err != nil || !same {
		f.Close()
		return nil, fmt.Errorf("%w: the folder is mounted separately", ErrPurgeUnsafe)
	}
	detect := m.purge.subvolume
	if detect == nil {
		detect = btrfsSubvolume
	}
	if f.sub, err = detect(f.dir, &f.stat); err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

func btrfsSubvolume(dir *os.File, st *unix.Stat_t) (bool, error) {
	var sfs unix.Statfs_t
	if err := unix.Fstatfs(int(dir.Fd()), &sfs); err != nil {
		return false, err
	}
	return sfs.Type == btrfsMagic && st.Ino == subvolumeInode, nil
}

// sameMount compares the kernel mount of two open files.
func sameMount(a, b *os.File) (bool, error) {
	first, err := mountID(a)
	if err != nil {
		return false, err
	}
	second, err := mountID(b)
	if err != nil {
		return false, err
	}
	return first == second, nil
}

func mountID(f *os.File) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/self/fdinfo/%d", f.Fd()))
	if err != nil {
		return "", err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if fields := strings.Fields(line); len(fields) == 2 && fields[0] == "mnt_id:" {
			return fields[1], nil
		}
	}
	return "", errors.New("mount identity unavailable")
}

// tally is what a count of a folder found.
type tally struct {
	files, dirs, bytes uint64
	limit              string
	foreign            []Usage
}

var errStop = errors.New("count stopped")

// count walks a folder without following links. Directories on another
// device (a nested Btrfs subvolume or a mount) are listed, not entered.
func (t *tally) count(ctx context.Context, dir *os.File, rel string, dev uint64, depth int, max uint64) error {
	if depth > maxDepth {
		t.limit = "depth"
		return errStop
	}
	fd := int(dir.Fd())
	for {
		entries, err := dir.ReadDir(128)
		for _, entry := range entries {
			if ctx.Err() != nil {
				t.limit = "time"
				return errStop
			}
			if max > 0 && t.files+t.dirs >= max {
				t.limit = "entries"
				return errStop
			}
			var st unix.Stat_t
			if err := unix.Fstatat(fd, entry.Name(), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				if errors.Is(err, unix.ENOENT) {
					continue
				}
				return err
			}
			if st.Mode&unix.S_IFMT != unix.S_IFDIR {
				t.files++
				t.bytes += uint64(st.Size)
				continue
			}
			t.dirs++
			name := path.Join(rel, entry.Name())
			if st.Dev != dev {
				if len(t.foreign) < maxListed {
					t.foreign = append(t.foreign, Usage{Kind: UseSubvolume, Path: name})
				}
				continue
			}
			child, err := unix.Openat(fd, entry.Name(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
			if err != nil {
				return err
			}
			sub := os.NewFile(uintptr(child), name)
			err = t.count(ctx, sub, name, dev, depth+1, max)
			sub.Close()
			if err != nil {
				return err
			}
		}
		if len(entries) == 0 || errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// nestedMounts returns the mounts at or below path.
func (m *Manager) nestedMounts(dir string) []Usage {
	var out []Usage
	for point := range m.mounts() {
		if point == dir || strings.HasPrefix(point, dir+"/") {
			out = append(out, Usage{Kind: UseMount, Path: point})
		}
	}
	slices.SortFunc(out, func(a, b Usage) int { return strings.Compare(a.Path, b.Path) })
	return out
}

// subvolumeSnapshots asks Btrfs for snapshots of a subvolume.
func (m *Manager) subvolumeSnapshots(ctx context.Context, dir string) ([]Usage, error) {
	out, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "show", dir)
	if err != nil {
		return nil, err
	}
	return parseSnapshots(out), nil
}

// parseSnapshots reads the "Snapshot(s):" list of `btrfs subvolume show`.
func parseSnapshots(out string) []Usage {
	var list []Usage
	in := false
	for line := range strings.SplitSeq(out, "\n") {
		trimmed := strings.TrimSpace(line)
		if key, value, ok := strings.Cut(trimmed, ":"); ok && !strings.Contains(key, "/") {
			in = key == "Snapshot(s)"
			if in && strings.TrimSpace(value) != "" {
				list = append(list, Usage{Kind: UseSnapshots, Path: strings.TrimSpace(value)})
			}
			continue
		}
		if in && trimmed != "" {
			list = append(list, Usage{Kind: UseSnapshots, Path: trimmed})
		}
	}
	return list
}

// smbSessions returns the open SMB connections to a share.
func (m *Manager) smbSessions(ctx context.Context, share Share) ([]Usage, error) {
	if m.Disabled || !share.SMB {
		return nil, nil
	}
	out, err := m.runner.Run(ctx, "", "smbstatus", "--shares", "--json")
	if err != nil {
		return nil, err
	}
	return parseSessions(out, share.Name)
}

func parseSessions(out, name string) ([]Usage, error) {
	var status struct {
		Tcons map[string]struct {
			Service string `json:"service"`
			Machine string `json:"machine"`
		} `json:"tcons"`
	}
	if err := json.Unmarshal([]byte(out), &status); err != nil {
		return nil, err
	}
	var list []Usage
	for _, tcon := range status.Tcons {
		if sameName(tcon.Service, name) {
			list = append(list, Usage{Kind: UseSMB, Name: tcon.Machine})
		}
	}
	slices.SortFunc(list, func(a, b Usage) int { return strings.Compare(a.Name, b.Name) })
	return list, nil
}

// inspect gathers what the share itself knows stands in the way.
func (m *Manager) inspect(ctx context.Context, share Share, f *folder) (blockers, warnings []Usage) {
	dir := m.Path(share)
	for _, other := range m.List() {
		if other.Name != share.Name && Overlaps(m.Path(other), dir) {
			blockers = append(blockers, Usage{Kind: UseShare, Name: other.Name, Path: m.Path(other)})
		}
	}
	blockers = append(blockers, m.nestedMounts(dir)...)
	sessions, err := m.smbSessions(ctx, share)
	if err != nil {
		blockers = append(blockers, Usage{Kind: UseSMBUnknown})
	}
	blockers = append(blockers, sessions...)
	if f.dir != nil && f.sub {
		snapshots, err := m.subvolumeSnapshots(ctx, dir)
		if err != nil {
			blockers = append(blockers, Usage{Kind: UseSnapUnknown})
		}
		blockers = append(blockers, snapshots...)
	}
	if f.dir != nil && !f.sub {
		// Snapshots of the whole volume keep their copy of the files.
		if entries, err := os.ReadDir(filepath.Join(m.mountRoot, share.Volume, ".titanos", "snapshots")); err == nil && len(entries) > 0 {
			warnings = append(warnings, Usage{Kind: UseVolumeSnapshots, Name: fmt.Sprint(len(entries))})
		}
	}
	return blockers, warnings
}

// PreviewPurge counts the folder of a share and lists what is in the way of
// deleting it. external comes from the other parts of TitanOS.
func (m *Manager) PreviewPurge(ctx context.Context, name string, mounted []string, external func(context.Context, string) []Usage) (DataPreview, error) {
	share, ok := m.share(name)
	if !ok {
		return DataPreview{}, ErrNotFound
	}
	p := DataPreview{Share: share.Name, Volume: share.Volume, Path: m.Path(share), Blockers: []Usage{}, Warnings: []Usage{}}
	if m.PurgeBusy() {
		p.Blockers = append(p.Blockers, Usage{Kind: UseRunning})
	}
	f, err := m.openFolder(share.Volume, share.Name, mounted)
	if err != nil {
		if !errors.Is(err, ErrPurgeUnsafe) {
			return DataPreview{}, err
		}
		p.Blockers = append(p.Blockers, Usage{Kind: UsePath, Path: p.Path, Name: strings.TrimPrefix(err.Error(), ErrPurgeUnsafe.Error()+": ")})
		return p, nil
	}
	defer f.Close()
	p.Exists, p.Subvolume = f.dir != nil, f.sub
	if f.dir != nil {
		count, cancel := context.WithTimeout(ctx, PreviewTime)
		var t tally
		err := t.count(count, f.dir, "", f.stat.Dev, 0, previewEntries)
		cancel()
		if err != nil && !errors.Is(err, errStop) {
			return DataPreview{}, err
		}
		p.Files, p.Dirs, p.Bytes, p.Limit, p.Complete = t.files, t.dirs, t.bytes, t.limit, t.limit == ""
		p.Blockers = append(p.Blockers, t.foreign...)
		if !p.Complete {
			p.Warnings = append(p.Warnings, Usage{Kind: UseLimit, Name: t.limit})
		}
	} else {
		p.Complete = true
	}
	blockers, warnings := m.inspect(ctx, share, f)
	p.Blockers = append(p.Blockers, blockers...)
	p.Warnings = append(p.Warnings, warnings...)
	if external != nil {
		p.Blockers = append(p.Blockers, external(ctx, p.Path)...)
	}
	return p, nil
}

func newPurgeID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// StartPurge removes a share and then deletes its folder in the background.
// What can be checked quickly is checked before anything changes; a
// *BlockedError lists what is in the way.
func (m *Manager) StartPurge(ctx context.Context, req PurgeRequest) (PurgeJob, error) {
	share, ok := m.share(req.Name)
	if !ok {
		return PurgeJob{}, ErrNotFound
	}
	if req.Path != m.Path(share) {
		return PurgeJob{}, ErrPurgeChanged
	}
	m.purge.mu.Lock()
	busy := m.purge.running != nil
	m.purge.mu.Unlock()
	if busy {
		return PurgeJob{}, ErrPurgeRunning
	}
	f, err := m.openFolder(share.Volume, share.Name, req.Mounted())
	if err != nil {
		return PurgeJob{}, err
	}
	blockers, _ := m.inspect(ctx, share, f)
	f.Close()
	if req.Check != nil {
		blockers = append(blockers, req.Check(ctx, m.Path(share))...)
	}
	if len(blockers) > 0 {
		return PurgeJob{}, &BlockedError{blockers}
	}
	job := &PurgeJob{ID: newPurgeID(), Share: share.Name, Volume: share.Volume, Path: m.Path(share), By: req.By, Phase: "checking", Started: time.Now().UTC(), DataKept: true}
	m.purge.mu.Lock()
	if m.purge.running != nil {
		m.purge.mu.Unlock()
		return PurgeJob{}, ErrPurgeRunning
	}
	m.purge.running = job
	snapshot := *job
	m.purge.mu.Unlock()
	go m.runPurge(job, share, req)
	return snapshot, nil
}

func (m *Manager) update(job *PurgeJob, change func(*PurgeJob)) {
	m.purge.mu.Lock()
	change(job)
	m.purge.mu.Unlock()
}

// finish ends a job and keeps its result.
func (m *Manager) finish(job *PurgeJob, err error, done func(PurgeJob)) {
	m.purge.mu.Lock()
	now := time.Now().UTC()
	job.Finished = &now
	if err != nil {
		job.Phase, job.Error = "failed", err.Error()
		var blocked *BlockedError
		if errors.As(err, &blocked) {
			job.Blockers = blocked.Usages
		}
	} else {
		job.Phase = "done"
	}
	result := *job
	m.purge.mu.Unlock()
	if err != nil {
		slog.Error("share data not deleted", "share", result.Share, "volume", result.Volume, "shareRemoved", result.ShareRemoved, "dataKept", result.DataKept, "err", err)
	} else {
		slog.Info("share data deleted", "share", result.Share, "volume", result.Volume, "files", result.Files, "bytes", result.Bytes, "by", result.By)
	}
	// The job stays running until its notification and activity entry are
	// out, so nothing that waits for PurgeBusy misses them.
	if done != nil {
		done(result)
	}
	m.purge.mu.Lock()
	m.purge.results = append(m.purge.results, result)
	if len(m.purge.results) > keepPurges {
		m.purge.results = m.purge.results[len(m.purge.results)-keepPurges:]
	}
	if m.purge.running == job {
		m.purge.running = nil
	}
	m.purge.mu.Unlock()
}

func (m *Manager) runPurge(job *PurgeJob, share Share, req PurgeRequest) {
	ctx := context.Background()
	err := m.purgeShare(ctx, job, share, req)
	m.finish(job, err, req.Done)
}

func (m *Manager) purgeShare(ctx context.Context, job *PurgeJob, share Share, req PurgeRequest) error {
	// Count it all, and make sure nothing foreign is inside, before the
	// share is touched.
	f, err := m.openFolder(share.Volume, share.Name, req.Mounted())
	if err != nil {
		return err
	}
	var t tally
	exists := f.dir != nil
	var dev, ino uint64
	if exists {
		dev, ino = f.stat.Dev, f.stat.Ino
		err = t.count(ctx, f.dir, "", dev, 0, 0)
	}
	sub := f.sub
	f.Close()
	if err != nil {
		return fmt.Errorf("counting the folder: %w", err)
	}
	if len(t.foreign) > 0 {
		return &BlockedError{t.foreign}
	}
	m.update(job, func(j *PurgeJob) {
		j.TotalFiles, j.TotalBytes, j.Subvolume, j.Phase = t.files, t.bytes, sub, "removing"
	})

	if err := m.Delete(share.Name); err != nil {
		return fmt.Errorf("removing the share: %w", err)
	}
	m.mu.Lock()
	gen := m.gen
	m.mu.Unlock()
	m.update(job, func(j *PurgeJob) { j.ShareRemoved, j.Phase = true, "waiting" })
	if err := m.release(ctx, share, gen); err != nil {
		return err
	}
	if !exists {
		m.update(job, func(j *PurgeJob) { j.DataKept = false })
		return nil
	}
	// Again, now that the share is gone: nothing may have started to use it.
	f, err = m.openFolder(share.Volume, share.Name, req.Mounted())
	if err != nil {
		return err
	}
	defer f.Close()
	if f.dir == nil || f.stat.Dev != dev || f.stat.Ino != ino || f.sub != sub {
		return ErrPurgeChanged
	}
	blockers, _ := m.inspect(ctx, share, f)
	if req.Check != nil {
		blockers = append(blockers, req.Check(ctx, m.Path(share))...)
	}
	if len(blockers) > 0 {
		return &BlockedError{blockers}
	}
	private, err := volumedata.Open(m.mountRoot, share.Volume, purgeFeature, req.Mounted())
	if err != nil {
		return err
	}
	defer private.Close()
	note, _ := json.Marshal(purgeNote{Share: share.Name, Volume: share.Volume, By: job.By, Started: job.Started})
	if err := volumedata.Write(private, job.ID+".json", note); err != nil {
		return err
	}
	// From here on the folder is gone from its place; it is deleted below.
	if err := f.volume.Rename(share.Name, filepath.Join(".titanos", purgeFeature, job.ID)); err != nil {
		private.Remove(job.ID + ".json")
		return fmt.Errorf("moving the folder aside: %w", err)
	}
	m.update(job, func(j *PurgeJob) { j.Phase, j.DataKept = "deleting", false })
	return m.deleteStaged(ctx, job, private, share.Volume, dev, ino, sub)
}

// release waits until Samba and NFS no longer offer the removed share.
func (m *Manager) release(ctx context.Context, share Share, gen uint64) error {
	if m.Disabled {
		return nil
	}
	wait, poll := m.purge.wait, m.purge.poll
	if wait == 0 {
		wait, poll = 2*time.Minute, 500*time.Millisecond
	}
	deadline := time.Now().Add(wait)
	m.Kick()
	for {
		m.mu.Lock()
		applied := m.appliedGen >= gen
		m.mu.Unlock()
		if applied {
			break
		}
		if time.Now().After(deadline) {
			return errors.New("the share was removed, but Samba and NFS were not updated in time; the data is kept")
		}
		time.Sleep(poll)
	}
	if share.SMB {
		// Connections to a share no longer in smb.conf are closed.
		if _, err := m.runner.Run(ctx, "", "smbcontrol", "smbd", "close-share", share.Name); err != nil {
			slog.Debug("close SMB share", "share", share.Name, "err", err)
		}
	}
	nfs := filepath.Join(m.nfsRoot, share.Name)
	for {
		sessions, err := m.smbSessions(ctx, share)
		if err == nil && len(sessions) == 0 && !m.mounts()[nfs] {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("the share was removed, but SMB connections or the NFS export are still open; the data is kept")
		}
		m.Kick()
		time.Sleep(poll)
	}
}

// deleteStaged deletes a folder moved to .titanos/share-delete/<id>.
func (m *Manager) deleteStaged(ctx context.Context, job *PurgeJob, private *os.Root, volume string, dev, ino uint64, sub bool) error {
	dirFile, err := private.Open(".")
	if err != nil {
		return err
	}
	defer dirFile.Close()
	pfd := int(dirFile.Fd())
	fd, err := unix.Openat(pfd, job.ID, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, unix.ENOENT) {
			private.Remove(job.ID + ".json")
			return nil
		}
		return err
	}
	staged := os.NewFile(uintptr(fd), job.ID)
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		staged.Close()
		return err
	}
	if dev != 0 && (st.Dev != dev || st.Ino != ino) {
		staged.Close()
		return ErrPurgeChanged
	}
	same, err := sameMount(dirFile, staged)
	if err != nil || !same {
		staged.Close()
		return fmt.Errorf("%w: the folder is mounted separately", ErrPurgeUnsafe)
	}
	full := filepath.Join(m.mountRoot, volume, ".titanos", purgeFeature, job.ID)
	if mounts := m.nestedMounts(full); len(mounts) > 0 {
		staged.Close()
		return &BlockedError{mounts}
	}
	if sub {
		staged.Close()
		if _, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "delete", "--commit-after", full); err != nil {
			return fmt.Errorf("deleting the subvolume: %w", err)
		}
		m.update(job, func(j *PurgeJob) { j.Files, j.Bytes = j.TotalFiles, j.TotalBytes })
	} else {
		err := m.removeTree(ctx, job, staged, st.Dev, 0)
		staged.Close()
		if err != nil {
			return err
		}
		if err := unix.Unlinkat(pfd, job.ID, unix.AT_REMOVEDIR); err != nil {
			return err
		}
	}
	return private.Remove(job.ID + ".json")
}

// removeTree empties a directory without following links or leaving its
// device. dir is open; its entries are deleted, not dir itself.
func (m *Manager) removeTree(ctx context.Context, job *PurgeJob, dir *os.File, dev uint64, depth int) error {
	if depth > maxDepth {
		return errors.New("the folder is nested too deeply")
	}
	fd := int(dir.Fd())
	for {
		if _, err := dir.Seek(0, io.SeekStart); err != nil {
			return err
		}
		entries, err := dir.ReadDir(128)
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
		if len(entries) == 0 {
			return nil
		}
		for _, entry := range entries {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			var st unix.Stat_t
			if err := unix.Fstatat(fd, entry.Name(), &st, unix.AT_SYMLINK_NOFOLLOW); err != nil {
				if errors.Is(err, unix.ENOENT) {
					continue
				}
				return err
			}
			if st.Mode&unix.S_IFMT == unix.S_IFDIR {
				if st.Dev != dev {
					return fmt.Errorf("%w: %s is another subvolume or mount", ErrPurgeUnsafe, entry.Name())
				}
				child, err := unix.Openat(fd, entry.Name(), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
				if err != nil {
					return err
				}
				sub := os.NewFile(uintptr(child), entry.Name())
				err = m.removeTree(ctx, job, sub, dev, depth+1)
				sub.Close()
				if err != nil {
					return err
				}
				if err := unix.Unlinkat(fd, entry.Name(), unix.AT_REMOVEDIR); err != nil && !errors.Is(err, unix.ENOENT) {
					return err
				}
				continue
			}
			if err := unix.Unlinkat(fd, entry.Name(), 0); err != nil && !errors.Is(err, unix.ENOENT) {
				return err
			}
			m.update(job, func(j *PurgeJob) { j.Files++; j.Bytes += uint64(st.Size) })
		}
	}
}

// resumePurges finishes deletions a crash broke off, once per volume.
func (m *Manager) resumePurges(mounted []string) {
	for _, volume := range mounted {
		m.purge.mu.Lock()
		done := m.purge.resumed[volume]
		busy := m.purge.running != nil
		m.purge.mu.Unlock()
		if done || busy {
			continue
		}
		if _, err := os.Lstat(filepath.Join(m.mountRoot, volume, ".titanos", purgeFeature)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				m.markResumed(volume)
			}
			continue
		}
		if m.resumeOne(volume, mounted) {
			continue // more may follow on the next round
		}
		m.markResumed(volume)
	}
}

func (m *Manager) markResumed(volume string) {
	m.purge.mu.Lock()
	if m.purge.resumed == nil {
		m.purge.resumed = map[string]bool{}
	}
	m.purge.resumed[volume] = true
	m.purge.mu.Unlock()
}

// resumeOne starts the next broken-off deletion on a volume; false if
// there is none left.
func (m *Manager) resumeOne(volume string, mounted []string) bool {
	private, err := volumedata.Open(m.mountRoot, volume, purgeFeature, mounted)
	if err != nil {
		slog.Warn("unfinished share deletions not readable", "volume", volume, "err", err)
		return false
	}
	entries, err := fs.ReadDir(private.FS(), ".")
	if err != nil {
		private.Close()
		return false
	}
	for _, entry := range entries {
		id, isNote := strings.CutSuffix(entry.Name(), ".json")
		if !isNote {
			continue
		}
		var note purgeNote
		if err := volumedata.ReadJSON(private, entry.Name(), &note, 4096); err != nil {
			slog.Warn("unreadable share deletion note kept", "volume", volume, "note", entry.Name(), "err", err)
			continue
		}
		info, err := private.Lstat(id)
		if errors.Is(err, fs.ErrNotExist) {
			private.Remove(entry.Name())
			continue
		}
		if err != nil || !info.IsDir() {
			continue
		}
		job := &PurgeJob{ID: id, Share: note.Share, Volume: volume, Path: filepath.Join(m.mountRoot, volume, note.Share), By: note.By, Phase: "deleting", Started: note.Started, ShareRemoved: true, Resumed: true}
		m.purge.mu.Lock()
		if m.purge.running != nil {
			m.purge.mu.Unlock()
			private.Close()
			return true
		}
		m.purge.running = job
		m.purge.mu.Unlock()
		go func() {
			defer private.Close()
			err := m.resumeStaged(job, private, volume)
			m.purge.mu.Lock()
			done := m.purge.resumedDone
			m.purge.mu.Unlock()
			m.finish(job, err, done)
		}()
		return true
	}
	private.Close()
	return false
}

func (m *Manager) resumeStaged(job *PurgeJob, private *os.Root, volume string) error {
	dir, err := private.Open(job.ID)
	if err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstat(int(dir.Fd()), &st); err != nil {
		dir.Close()
		return err
	}
	detect := m.purge.subvolume
	if detect == nil {
		detect = btrfsSubvolume
	}
	sub, err := detect(dir, &st)
	dir.Close()
	if err != nil {
		return err
	}
	m.update(job, func(j *PurgeJob) { j.Subvolume = sub })
	return m.deleteStaged(context.Background(), job, private, volume, st.Dev, st.Ino, sub)
}

// markApplied notes that the system matches the configuration of gen.
func (m *Manager) markApplied(gen uint64) {
	m.mu.Lock()
	if gen > m.appliedGen {
		m.appliedGen = gen
	}
	m.mu.Unlock()
}

// OnPurgeResumed sets who is told how a resumed deletion ended.
func (m *Manager) OnPurgeResumed(done func(PurgeJob)) {
	m.purge.mu.Lock()
	m.purge.resumedDone = done
	m.purge.mu.Unlock()
}
