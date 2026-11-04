package files

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Old JSON checkpoints are imported with their historical format bound.
// New plans put entries in the volume database, never in this descriptor.
const legacyTransferPlanLimit = 128 << 20

var ErrJobBusy = errors.New("files: the transfer is still updating its status")

type transfer struct {
	Format              int `json:"format,omitempty"`
	store               *transferStore
	initialized         bool
	forgotten           bool
	logMu               sync.Mutex
	Job                 Job                   `json:"job"`
	User                string                `json:"user"`
	Volume              string                `json:"volume"`
	Boot                string                `json:"boot"`
	SourceDir           string                `json:"sourceDir"`
	DestinationDir      string                `json:"destinationDir"`
	SourceIdentity      string                `json:"sourceIdentity"`
	DestinationIdentity string                `json:"destinationIdentity"`
	Conflict            string                `json:"conflict"`
	Choices             map[string]CopyChoice `json:"choices"`
	Skipped             map[string]bool       `json:"skipped"`
	Completed           map[string]bool       `json:"completed"`
	CompletedTop        map[string]bool       `json:"completedTop"`
	Targets             map[string]string     `json:"targets"`
	Sources             map[string]string     `json:"sources"`
	Destinations        map[string]string     `json:"destinations"`
	Part                string                `json:"part"`
	PartIdentity        string                `json:"partIdentity"`
	Pending             string                `json:"pending"`
}

func transferAccount(user string) string {
	sum := sha256.Sum256([]byte(user))
	return hex.EncodeToString(sum[:16])
}
func transferIdentity(dir string) (string, error) {
	st, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok || !st.IsDir() {
		return "", ErrInvalid
	}
	return fmt.Sprintf("%d:%d", sys.Dev, sys.Ino), nil
}
func (m *Manager) transferRoot(volume, user string) (*os.Root, error) {
	root, err := volumedata.Open(m.MountRoot, volume, "transfers", m.Mounted())
	if err != nil {
		return nil, err
	}
	defer root.Close()
	account := transferAccount(user)
	if err := root.Mkdir(account, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, err
	}
	st, err := root.Lstat(account)
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0077 != 0 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return nil, ErrProtected
	}
	return root.OpenRoot(account)
}

// prepareTransfer makes the durable plan of a job whose target is on a data
// volume and fills it with one walk of the selection. Other jobs get the walk
// alone. transferMu is held only while the plan is created, not while the
// tree is walked, so the job list stays responsive.
func (m *Manager) prepareTransfer(job *Job, src, dst Root, walk func(add func(string, transferEntry) error) error) error {
	if m.Mounted == nil || dst.Volume == "" {
		return walk(func(string, transferEntry) error { return nil })
	}
	source, err := transferIdentity(src.Dir)
	if err != nil {
		return err
	}
	destination, err := transferIdentity(dst.Dir)
	if err != nil {
		return err
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	t := &transfer{User: job.user, Volume: dst.Volume, Boot: strings.TrimSpace(string(boot)), SourceDir: src.Dir, DestinationDir: dst.Dir, SourceIdentity: source, DestinationIdentity: destination, Conflict: job.conflict, Choices: job.choices}
	m.transferMu.Lock()
	root, err := m.transferRoot(t.Volume, t.User)
	if err != nil {
		m.transferMu.Unlock()
		return err
	}
	err = pruneTransfers(root)
	root.Close()
	if err != nil {
		m.transferMu.Unlock()
		return err
	}
	t.Format = 2
	t.store = &transferStore{manager: m, volume: t.Volume, user: t.User, id: job.ID}
	m.jobs.mu.Lock()
	job.durable = t
	job.Durable = true
	m.jobs.mu.Unlock()
	err = t.store.create(func() error { return m.saveTransfer(job, "") })
	m.transferMu.Unlock()
	if err == nil {
		err = t.store.fill(job, walk)
	}
	if err != nil {
		if t.store.created {
			if root, e := m.transferRoot(t.Volume, t.User); e == nil {
				root.Remove(job.ID + ".json")
				root.Remove(job.ID + ".db")
				_ = syncTransferParent(root, job.ID+".json")
				root.Close()
			}
		}
		m.jobs.mu.Lock()
		job.durable = nil
		job.Durable = false
		m.jobs.mu.Unlock()
		return mapError(err)
	}
	return nil
}
func (m *Manager) saveTransfer(job *Job, pending string) error {
	t := job.durable
	if t == nil {
		return nil
	}
	if t.initialized {
		return m.appendTransfer(job, transferEvent{Pending: &pending})
	}
	m.jobs.mu.Lock()
	t.Pending = pending
	t.Job = snapshotLocked(job)
	if t.Format == 2 {
		t.Job.Operation = "checkpoint-v2"
		t.Job.Blocked = true
		t.Job.Retryable = false
	}
	m.jobs.mu.Unlock()
	root, err := m.transferRoot(t.Volume, t.User)
	if err != nil {
		return err
	}
	defer root.Close()
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	if err := volumedata.Write(root, job.ID+".json", data); err != nil {
		return err
	}
	t.initialized = true
	return nil
}

// finishTransfer records an entry that is at its target now. source is the
// version of the source file that was copied ("" for a rename).
func (m *Manager) finishTransfer(job *Job, dr *os.Root, rel, final, source string) error {
	if job.durable == nil {
		return nil
	}
	if err := syncTransferParent(dr, final); err != nil {
		return err
	}
	info, err := dr.Lstat(final)
	if err != nil {
		return err
	}
	version := ""
	if info.Mode().IsRegular() {
		version = entryVersion(info)
	}
	complete := info.Mode().IsRegular()
	if !job.paged() {
		if version != "" {
			job.durable.Destinations[final] = version
		}
		if source != "" {
			job.durable.Sources[rel] = source
		}
		complete = job.completed[rel] || complete
	}
	empty := ""
	return m.appendTransfer(job, transferEvent{Pending: &empty, Part: &transferPart{}, Rel: rel, Target: final, Version: version, Complete: complete, Source: source})
}
func (m *Manager) loadTransfers(user string) {
	if m.Mounted == nil {
		return
	}
	m.transferMu.Lock()
	defer m.transferMu.Unlock()
	if m.transferLoaded == nil {
		m.transferLoaded = map[string]bool{}
	}
	for _, volume := range m.Mounted() {
		key := user + "/" + volume
		if m.transferLoaded[key] {
			continue
		}
		root, err := m.transferRoot(volume, user)
		if err != nil {
			continue
		}
		dir, err := root.Open(".")
		if err != nil {
			root.Close()
			continue
		}
		list, _ := dir.ReadDir(401)
		dir.Close()
		m.transferLoaded[key] = true
		for _, e := range list {
			if !regexp.MustCompile(`^[0-9a-f]{16}\.json$`).MatchString(e.Name()) {
				continue
			}
			id := strings.TrimSuffix(e.Name(), ".json")
			m.jobs.mu.Lock()
			known := false
			for _, j := range m.jobs.list {
				if j.user == user && j.ID == id {
					known = true
				}
			}
			m.jobs.mu.Unlock()
			if known {
				continue
			}
			var t transfer
			readErr := volumedata.ReadJSON(root, e.Name(), &t, legacyTransferPlanLimit)
			if readErr == nil && t.Format == 2 {
				t.store = &transferStore{manager: m, volume: volume, user: user, id: id}
				var state transferStoreState
				state, readErr = t.store.summary()
				if readErr == nil {
					state.Progress.apply(&t.Job)
					t.Job.Operation = ""
					t.Job.Blocked = false
					t.Pending = state.Pending
					t.Part = state.Part.Path
					t.PartIdentity = state.Part.Identity
				}
			} else if readErr == nil && t.Format == 0 {
				readErr = readTransferEvents(root, id, &t)
			} else if readErr == nil {
				readErr = ErrInvalid
			}
			if err := readErr; err != nil {
				m.jobs.mu.Lock()
				m.jobs.list = append(m.jobs.list, &Job{ID: id, user: user, State: JobFailed, Operation: "checkpoint-error", Blocked: true, Durable: true, durable: &transfer{Volume: volume, User: user}, Error: "Could not read saved transfer: " + err.Error(), From: volume, Names: []string{}, c: &counters{}})
				m.jobs.mu.Unlock()
				continue
			}
			if t.User != user || t.Volume != volume || t.Job.ID != id {
				continue
			}
			t.initialized = true
			j := t.Job
			j.user = user
			j.sourceDir = t.SourceDir
			j.destinationDir = t.DestinationDir
			j.choices = t.Choices
			j.conflict = t.Conflict
			j.completed = t.Completed
			j.completedTop = t.CompletedTop
			j.skipped = map[string]bool{}
			j.planned = true
			j.targets = t.Targets
			j.completedSource = map[string]fs.FileInfo{}
			j.undoEligible = map[string]bool{}
			j.durable = &t
			j.c = &counters{}
			j.c.copied.Store(j.Copied)
			j.c.committed.Store(j.Copied)
			j.c.count.Store(j.Done)
			j.Durable = true
			if j.State == JobRunning {
				j.State = JobFailed
				j.Error = "Transfer interrupted; recheck current access and resume incomplete files."
				j.ErrorCode = "interrupted"
			}
			// After a restart of the NAS the saved state cannot be trusted
			// for a retry; the job offers copying the missing files instead.
			if boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id"); err == nil && strings.TrimSpace(string(boot)) != t.Boot {
				j.Rebooted = true
			}
			if t.Pending != "" {
				j.Blocked = true
				j.Retryable = false
				j.Error = "Publication outcome is unknown: " + t.Pending + ". Inspect source and destination before starting a new transfer."
				j.ErrorCode = "unknown_outcome"
			}
			m.jobs.mu.Lock()
			m.jobs.list = append(m.jobs.list, &j)
			m.jobs.mu.Unlock()
		}
		root.Close()
	}
}
func (m *Manager) verifyTransfer(job *Job, src, dst Root, dr *os.Root) error {
	t := job.durable
	if t == nil {
		return nil
	}
	if job.Blocked || t.Pending != "" {
		return errors.New("unknown publication cannot be replayed")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(boot)) != t.Boot {
		return ErrRebooted
	}
	source, err := transferIdentity(src.Dir)
	if err != nil {
		return err
	}
	destination, err := transferIdentity(dst.Dir)
	if err != nil {
		return err
	}
	if source != t.SourceIdentity || destination != t.DestinationIdentity {
		return ErrTextConflict
	}
	// Destinations are checked entry by entry where it matters: before a
	// move removes a source (destinationIntact). A target file someone
	// edited since does not stop the remaining files.
	if t.Part != "" {
		if !copyPartName.MatchString(path.Base(t.Part)) || m.protected(dst, t.Part) {
			return ErrProtected
		}
		if info, err := dr.Lstat(t.Part); err == nil {
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || !info.Mode().IsRegular() || fmt.Sprintf("%d:%d", stat.Dev, stat.Ino) != t.PartIdentity || stat.Uid != uint32(os.Geteuid()) {
				return ErrTextConflict
			}
			if err := dr.Remove(t.Part); err != nil {
				return err
			}
			if err := syncTransferParent(dr, t.Part); err != nil {
				return err
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// ForgetTransfer removes only a terminal job's bookkeeping, never user files.
func (m *Manager) ForgetTransfer(user, id string) error {
	m.loadTransfers(user)
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	for i, j := range m.jobs.list {
		if j.user != user || j.ID != id {
			continue
		}
		if j.State == JobRunning || j.finishing {
			return ErrJobBusy
		}
		if err := m.removeTransfer(j); err != nil {
			return err
		}
		m.jobs.list = append(m.jobs.list[:i], m.jobs.list[i+1:]...)
		return nil
	}
	return ErrNoJob
}

// ClearTransfers snapshots all terminal jobs under the same lock as starts and
// retries. Running/finishing jobs and other accounts are never removed.
func (m *Manager) ClearTransfers(user string) error {
	m.loadTransfers(user)
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	var first error
	for i := 0; i < len(m.jobs.list); {
		j := m.jobs.list[i]
		if j.user != user || j.State == JobRunning || j.finishing {
			i++
			continue
		}
		if err := m.removeTransfer(j); err != nil {
			if first == nil {
				first = err
			}
			i++
			continue
		}
		m.jobs.list = append(m.jobs.list[:i], m.jobs.list[i+1:]...)
	}
	return first
}
func (m *Manager) removeTransfer(j *Job) error {
	// A failed download's part file is titand's, not a user file.
	m.discardDownload(j.download)
	if j.durable == nil {
		return nil
	}
	// appendTransfer takes logMu before jobs.mu. Never wait for it while
	// holding jobs.mu; let the caller retry a still-finishing status write.
	if !j.durable.logMu.TryLock() {
		return ErrJobBusy
	}
	defer j.durable.logMu.Unlock()
	j.closeStore()
	root, err := m.transferRoot(j.durable.Volume, j.user)
	if err != nil {
		return err
	}
	defer root.Close()
	// Remove and sync the immutable plan first. A crash during cleanup must
	// never leave a replayable plan whose publication journal was deleted.
	if err := root.Remove(j.ID + ".json"); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	j.durable.forgotten = true
	if err := syncTransferParent(root, j.ID+".json"); err != nil {
		return err
	}
	for _, suffix := range []string{".events", ".status.json", ".db"} {
		if err := root.Remove(j.ID + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return syncTransferParent(root, j.ID+".json")
}

// copyPartName is the name of a copy's part file (12 hex digits before 4.11.2026).
var copyPartName = regexp.MustCompile(`^\.titanos-copy-([0-9a-f]{12}|[0-9a-f]{24})\.titanos-part$`)

// destinationIntact reports whether the copy of a completed file is still
// the one that was published, so that a move may remove its source.
func (m *Manager) destinationIntact(job *Job, dr *os.Root, entry transferEntry) bool {
	if job.durable == nil {
		return true // RAM jobs remove the source right after the copy
	}
	if entry.Target == "" {
		return false
	}
	var recorded string
	if job.paged() {
		v, err := job.durable.store.destination(entry.Target)
		if err != nil {
			return false
		}
		recorded = v
	} else {
		recorded = job.durable.Destinations[entry.Target]
	}
	info, err := dr.Lstat(entry.Target)
	return err == nil && recorded != "" && entryVersion(info) == recorded
}
func (m *Manager) transferAvailable(job *Job) error {
	if m.Available != nil {
		for _, root := range []Root{job.sourceRoot, job.destinationRoot} {
			if root.Dir != "" && !m.Available(root) {
				return volumedata.ErrUnavailable
			}
		}
	}
	return nil
}

func pruneTransfers(root *os.Root) error {
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	entries, _ := dir.ReadDir(401)
	dir.Close()
	count := 0
	type finished struct {
		id string
		at int64
	}
	done := []finished{}
	for _, e := range entries {
		if !regexp.MustCompile(`^[0-9a-f]{16}\.json$`).MatchString(e.Name()) {
			continue
		}
		count++
		id := strings.TrimSuffix(e.Name(), ".json")
		var status transferEnd
		if err := volumedata.ReadJSON(root, id+".status.json", &status, 4096); err != nil {
			continue
		}
		done = append(done, finished{id, status.Finished.UnixNano()})
	}
	sort.Slice(done, func(i, j int) bool { return done[i].at < done[j].at })
	for len(done) > maxJobs {
		for _, suffix := range []string{".json", ".events", ".status.json", ".db"} {
			if err := root.Remove(done[0].id + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		done = done[1:]
		count--
	}
	if count >= 100 {
		return errors.New("too many saved transfers; remove finished records first")
	}
	return nil
}
