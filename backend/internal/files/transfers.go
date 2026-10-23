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

type transfer struct {
	initialized         bool
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
func (m *Manager) prepareTransfer(job *Job, src, dst Root, sr *os.Root) error {
	if m.Mounted == nil || dst.Volume == "" {
		return nil
	}
	if job.Files > 50000 {
		return errors.New("durable transfers are limited to 50000 files per job")
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
	t := &transfer{User: job.user, Volume: dst.Volume, Boot: strings.TrimSpace(string(boot)), SourceDir: src.Dir, DestinationDir: dst.Dir, SourceIdentity: source, DestinationIdentity: destination, Conflict: job.conflict, Choices: job.choices, Skipped: map[string]bool{}, Completed: job.completed, CompletedTop: job.completedTop, Targets: job.targets, Sources: map[string]string{}, Destinations: map[string]string{}}
	var count, size int64
	for _, rel := range job.Names {
		if err := walk(sr, rel, func(p string, info fs.FileInfo) {
			if info.Mode().IsRegular() {
				t.Sources[p] = entryVersion(info)
				count++
				size += info.Size()
			}
		}); err != nil {
			return err
		}
	}
	if job.Files != count || job.Bytes != size {
		return ErrTextConflict
	}
	root, err := m.transferRoot(t.Volume, t.User)
	if err != nil {
		return err
	}
	err = pruneTransfers(root)
	root.Close()
	if err != nil {
		return err
	}
	job.durable = t
	job.Durable = true
	return m.saveTransfer(job, "")
}
func (m *Manager) saveTransfer(job *Job, pending string) error {
	t := job.durable
	if t == nil {
		return nil
	}
	if t.initialized {
		return m.appendTransfer(job, transferEvent{Pending: &pending})
	}
	t.Pending = pending
	m.jobs.mu.Lock()
	t.Job = snapshotLocked(job)
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
	if len(data) > 32<<20 {
		return errors.New("transfer checkpoint exceeds 32 MiB")
	}
	if err := volumedata.Write(root, job.ID+".json", data); err != nil {
		return err
	}
	t.initialized = true
	return nil
}
func (m *Manager) finishTransfer(job *Job, dr *os.Root, rel, final string) error {
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
	version := entryVersion(info)
	job.durable.Destinations[final] = version
	empty := ""
	return m.appendTransfer(job, transferEvent{Pending: &empty, Part: &transferPart{}, Rel: rel, Target: final, Version: version, Complete: job.completed[rel], Top: job.completedTop[rel]})
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
			readErr := volumedata.ReadJSON(root, e.Name(), &t, 32<<20)
			if readErr == nil {
				readErr = readTransferEvents(root, id, &t)
			}
			if err := readErr; err != nil {
				m.jobs.mu.Lock()
				m.jobs.list = append(m.jobs.list, &Job{ID: id, user: user, State: JobFailed, Operation: "checkpoint-error", Blocked: true, Durable: true, durable: &transfer{Volume: volume, User: user}, Error: "Could not read saved transfer: " + err.Error(), From: volume, Names: []string{}, c: &counters{}})
				m.jobs.mu.Unlock()
				continue
			}
			if t.User != user || t.Volume != volume || t.Job.ID != id || len(t.Sources) > 50000 {
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
				j.Retryable = true
			}
			if t.Pending != "" {
				j.Blocked = true
				j.Retryable = false
				j.Error = "Publication outcome is unknown: " + t.Pending + ". Inspect source and destination before starting a new transfer."
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
		return errors.New("NAS reboot changed the transfer session; inspect files before starting another transfer")
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
	for p, version := range t.Destinations {
		info, err := dr.Lstat(p)
		if err != nil || entryVersion(info) != version || m.protected(dst, p) || dst.secret(p) {
			return ErrTextConflict
		}
	}
	if t.Part != "" {
		if !regexp.MustCompile(`^\.titanos-copy-[0-9a-f]{12}\.titanos-part$`).MatchString(path.Base(t.Part)) || m.protected(dst, t.Part) {
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
func (m *Manager) ForgetTransfer(user, id, confirm string) error {
	if id != confirm {
		return ErrInvalid
	}
	m.loadTransfers(user)
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	for i, j := range m.jobs.list {
		if j.user != user || j.ID != id {
			continue
		}
		if j.State == JobRunning || j.finishing {
			return ErrInvalid
		}
		if j.durable != nil {
			root, err := m.transferRoot(j.durable.Volume, user)
			if err != nil {
				return err
			}
			defer root.Close()
			for _, suffix := range []string{".events", ".status.json"} {
				if err := root.Remove(id + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
			if err := root.Remove(id + ".json"); err != nil {
				return err
			}
		}
		m.jobs.list = append(m.jobs.list[:i], m.jobs.list[i+1:]...)
		return nil
	}
	return ErrNoJob
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
		for _, suffix := range []string{".json", ".events", ".status.json"} {
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
