package files

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Copying and moving run in the background: a folder of photos takes minutes.
// A job keeps its progress in memory; titand forgets it with a restart. Files
// are written next to their target under a hidden name and renamed when
// they are whole, like uploads, so nobody finds half a file.

// ConflictSkip leaves a file alone when the target exists.
const ConflictSkip = "skip"

// Job states.
const (
	JobRunning   = "running"
	JobDone      = "done"
	JobFailed    = "failed"
	JobCancelled = "cancelled"
)

// maxJobs is how many finished jobs are kept for the list.
const maxJobs = 20

// ErrNoJob means there is no such copy job.
var ErrNoJob = errors.New("files: no such job")

// Job is a copy or move that runs or ran.
type Job struct {
	Durable   bool `json:"durable"`
	Blocked   bool `json:"blocked"`
	durable   *transfer
	finishing bool
	Waiting   bool   `json:"waiting"`
	Paused    bool   `json:"paused"`
	Priority  int    `json:"priority"`
	Rate      int64  `json:"rate"`
	Operation string `json:"operation,omitempty"`
	ID        string `json:"id"`
	Move      bool   `json:"move"`
	State     string `json:"state"`
	// From and To are the roots, for the list.
	From  string   `json:"from"`
	To    string   `json:"to"`
	Dir   string   `json:"dir"`
	Names []string `json:"names"`
	// Files and Bytes are what the job found to do; Done and Copied what is done.
	Files                       int64     `json:"files"`
	Done                        int64     `json:"done"`
	Bytes                       int64     `json:"bytes"`
	Copied                      int64     `json:"copied"`
	Current                     string    `json:"current"`
	Error                       string    `json:"error"`
	Started                     time.Time `json:"started"`
	Finished                    time.Time `json:"finished"`
	Retryable                   bool      `json:"retryable"`
	choices                     map[string]CopyChoice
	completed                   map[string]bool
	completedTop                map[string]bool
	targets                     map[string]string
	conflict                    string
	sourceRoot, destinationRoot Root
	sourceDir, destinationDir   string
	completedSource             map[string]fs.FileInfo

	pendingUndo  []UndoMove
	undoEligible map[string]bool
	user         string
	cancel       context.CancelFunc
	c            *counters
}

// counters are what the copying goroutine counts while others read it.
type counters struct{ copied, count, committed atomic.Int64 }

type jobs struct {
	mu   sync.Mutex
	list []*Job
}

// copyTree remembers what the walk found.
type plan struct {
	files, bytes int64
}

// StartCopy begins copying (or moving) paths of one root into a folder of
// another. It returns the job at once.
func (m *Manager) StartCopy(src Root, paths []string, dst Root, dir string, move bool, conflict, user string, uid int) (Job, error) {
	return m.StartCopyChoices(src, paths, dst, dir, move, conflict, user, uid, nil)
}
func (m *Manager) StartCopyChoices(src Root, paths []string, dst Root, dir string, move bool, conflict, user string, uid int, choices map[string]CopyChoice) (Job, error) {
	if err := validateChoices(src, dst, paths, choices); err != nil {
		return Job{}, err
	}
	copiedChoices := map[string]CopyChoice{}
	for p, c := range choices {
		copiedChoices[p] = c
	}
	choices = copiedChoices

	if m.Available != nil && (!m.Available(src) || !m.Available(dst)) {
		return Job{}, ErrNotFound
	}
	if !dst.Writable || move && !src.Writable {
		return Job{}, ErrReadOnly
	}
	if len(src.Hide) > 0 {
		return Job{}, ErrProtected
	}
	if conflict == "" {
		conflict = ConflictRename
	}
	if conflict != ConflictSkip && conflict != ConflictReplace && conflict != ConflictRename {
		return Job{}, ErrInvalid
	}
	dir, err := clean(dir)
	if err != nil || len(paths) == 0 {
		return Job{}, ErrInvalid
	}
	var rels []string
	for _, p := range paths {
		p, err := clean(p)
		if err != nil || p == "." {
			return Job{}, ErrInvalid
		}
		if m.protected(src, p) {
			return Job{}, ErrProtected
		}
		rels = append(rels, p)
	}
	same := src.Dir == dst.Dir
	for _, p := range rels {
		// A folder cannot be copied into itself.
		if same && (dir == p || strings.HasPrefix(dir+"/", p+"/")) {
			return Job{}, ErrInvalid
		}
	}
	sr, err := os.OpenRoot(src.Dir)
	if err != nil {
		return Job{}, mapError(err)
	}
	dr, err := os.OpenRoot(dst.Dir)
	if err != nil {
		sr.Close()
		return Job{}, mapError(err)
	}
	closeRoots := func() { sr.Close(); dr.Close() }
	targetDirectory, e := dr.Stat(dir)
	if e != nil || !targetDirectory.IsDir() {
		closeRoots()
		return Job{}, ErrNotFound
	}
	var p plan
	for _, rel := range rels {
		if err := walkEntries(sr, rel, func(_ string, info fs.FileInfo) error {
			// Inode comparison catches aliases and bind mounts across roots.
			if info.IsDir() && os.SameFile(info, targetDirectory) {
				return ErrInvalid
			}
			if info.Mode().IsRegular() {
				p.files++
				p.bytes += info.Size()
			}
			return nil
		}); err != nil {
			closeRoots()
			return Job{}, mapError(err)
		}
	}
	// A plain move within the same root renames and needs no room.
	if _, free := Usage(dst); !(move && src.Dir == dst.Dir && len(choices) == 0) && uint64(p.bytes) > free {
		closeRoots()
		return Job{}, ErrNoSpace
	}
	raw := make([]byte, 8)
	rand.Read(raw)
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{sourceRoot: src, destinationRoot: dst, choices: choices, ID: hex.EncodeToString(raw), Move: move, State: JobRunning, From: src.ID, To: dst.ID, Dir: dir, Names: rels,
		Files: p.files, Bytes: p.bytes, Started: m.now(), user: user, cancel: cancel, c: &counters{}, completed: map[string]bool{}, completedTop: map[string]bool{}, targets: map[string]string{}, conflict: conflict, sourceDir: src.Dir, destinationDir: dst.Dir, completedSource: map[string]fs.FileInfo{}, undoEligible: map[string]bool{}}
	m.transferMu.Lock()
	if err := m.prepareTransfer(job, src, dst, sr); err != nil {
		m.transferMu.Unlock()
		closeRoots()
		cancel()
		return Job{}, err
	}
	if _, free := Usage(dst); job.paged() && !(move && src.Dir == dst.Dir && len(choices) == 0) && uint64(p.bytes) > free {
		m.jobs.mu.Lock()
		_ = m.removeTransfer(job)
		m.jobs.mu.Unlock()
		m.transferMu.Unlock()
		closeRoots()
		cancel()
		return Job{}, ErrNoSpace
	}
	m.jobs.mu.Lock()
	m.jobs.list = append(m.jobs.list, job)
	m.jobs.mu.Unlock()
	m.transferMu.Unlock()
	go func() {
		defer closeRoots()
		err := m.runCopy(ctx, job, sr, dr, src, dst, rels, dir, move, conflict, uid)
		m.rememberMoveUndo(job)
		m.jobs.mu.Lock()
		defer func() {
			job.finishing = true
			m.jobs.mu.Unlock()
			if job.durable != nil {
				if err := m.saveTransfer(job, job.durable.Pending); err != nil {
					m.jobs.mu.Lock()
					job.Error = "Could not persist transfer outcome: " + err.Error()
					job.Blocked = true
					m.jobs.mu.Unlock()
				}
			}
			job.closeStore()
			m.jobs.mu.Lock()
			job.finishing = false
			m.jobs.mu.Unlock()
		}()
		job.Finished = m.now()
		switch {
		case ctx.Err() != nil:
			job.State = JobCancelled
		case err != nil:
			job.State, job.Error = JobFailed, err.Error()
		default:
			job.State = JobDone
		}
		if err == nil {
			job.Current = ""
		}
		m.pruneJobs()
	}()
	return m.snapshot(job), nil
}

// Directory batches bound RAM independently of a directory's total children.
func eachChild(r *os.Root, rel string, fn func(fs.DirEntry) error) error {
	dir, err := r.Open(rel)
	if err != nil {
		return err
	}
	defer dir.Close()
	for {
		entries, err := dir.ReadDir(128)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if err := fn(entry); err != nil {
				return err
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// walk calls fn for a file or folder and everything below it, not following links.
func walk(r *os.Root, rel string, fn func(string, fs.FileInfo)) error {
	return walkEntries(r, rel, func(p string, info fs.FileInfo) error { fn(p, info); return nil })
}
func walkEntries(r *os.Root, rel string, fn func(string, fs.FileInfo) error) error {
	info, err := r.Lstat(rel)
	if err != nil {
		return err
	}
	if err := fn(rel, info); err != nil {
		if errors.Is(err, fs.SkipDir) && info.IsDir() {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	return eachChild(r, rel, func(entry fs.DirEntry) error {
		if hidden(entry.Name()) {
			return nil
		}
		return walkEntries(r, path.Join(rel, entry.Name()), fn)
	})
}

func (m *Manager) runCopy(ctx context.Context, job *Job, sr, dr *os.Root, src, dst Root, rels []string, dir string, move bool, conflict string, uid int) error {
	job.keepStoreOpen()
	defer job.closeStore()
	renamed := move && src.Dir == dst.Dir && len(job.choices) == 0
	for _, rel := range rels {
		if err := m.copyCheckpoint(ctx, job, 0); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := job.entry(rel)
		if err != nil {
			return err
		}
		if entry.Top {
			continue
		}
		target := choiceTarget(path.Join(dir, path.Base(rel)), job.choices[rel])
		if m.protected(dst, target) || dst.secret(target) {
			return ErrProtected
		}
		if job.choices[rel].Action == "skip" {
			m.countTree(job, sr, rel)
			if !job.paged() {
				job.completedTop[rel] = true
			}
			if err := m.transferTop(job, rel); err != nil {
				return err
			}
			continue
		}
		if len(job.choices) > 0 && src.Dir == dst.Dir && (target == rel || strings.HasPrefix(target, rel+"/")) {
			return ErrInvalid
		}
		if job.choices[rel].Target != "" {
			if err := m.copyParents(dr, dst, path.Dir(target), uid); err != nil {
				return err
			}
		}
		if move && job.undoEligible != nil {
			if _, known := job.undoEligible[rel]; !known {
				existing, e := dr.Lstat(target)
				job.undoEligible[rel] = os.IsNotExist(e) || (e == nil && (renamed || !existing.IsDir()) && conflict == ConflictRename)
			}
		}
		if renamed {
			// Same root: a rename, which also works on a big folder at once.
			var err error
			if target, err = m.settle(dr, target, conflict); err != nil {
				return err
			}
			if target == "" {
				continue
			}
			if err := m.copyCheckpoint(ctx, job, 0); err != nil {
				return err
			}
			if err := m.saveTransfer(job, "rename "+rel+" to "+target); err != nil {
				return err
			}
			if err := sr.Rename(rel, target); err != nil {
				return mapError(err)
			}
			if job.durable != nil {
				if err := syncTransferParent(sr, rel); err != nil {
					return err
				}
				if err := syncTransferParent(dr, target); err != nil {
					return err
				}
			}
			m.countTree(job, sr, target)
			if !job.paged() {
				job.completedTop[rel] = true
			}
			if err := m.finishTransfer(job, dr, rel, target, true); err != nil {
				return err
			}
			if job.undoEligible[rel] {
				job.pendingUndo = append(job.pendingUndo, UndoMove{CurrentRoot: dst, OriginalRoot: src, Current: target, Original: rel})
			}
			continue
		}
		if err := m.copyEntry(ctx, job, sr, dr, rel, target, conflict, uid); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if move {
			// Pauses may be long: verify that no new or changed source data
			// would be removed after the completed copy.
			if err := m.copyCheckpoint(ctx, job, 0); err != nil {
				return err
			}
			if err := verifyCopiedSource(ctx, job, sr, rel); err != nil {
				return err
			}
			// The copy is whole; only now does the original go.
			m.jobs.mu.Lock()
			job.Current = rel
			m.jobs.mu.Unlock()
			if err := m.saveTransfer(job, "remove copied source "+rel); err != nil {
				return err
			}
			if err := sr.RemoveAll(rel); err != nil {
				return mapError(err)
			}
			if job.durable != nil {
				if err := syncTransferParent(sr, rel); err != nil {
					return err
				}
			}
		}
		if !job.paged() {
			job.completedTop[rel] = true
		}
		if err := m.transferTop(job, rel); err != nil {
			return err
		}
		if move && job.undoEligible[rel] {
			entry, err := job.entry(rel)
			if err != nil {
				return err
			}
			actual := entry.Target
			if actual == "" {
				actual = target
			}
			job.pendingUndo = append(job.pendingUndo, UndoMove{CurrentRoot: dst, OriginalRoot: src, Current: actual, Original: rel})
		}
	}
	return nil
}

func (m *Manager) countTree(job *Job, r *os.Root, rel string) {
	walk(r, rel, func(_ string, info fs.FileInfo) {
		if info.Mode().IsRegular() {
			job.c.count.Add(1)
			job.c.copied.Add(info.Size())
			job.c.committed.Add(info.Size())
		}
	})
}

// settle decides the name a file gets in the target folder. It returns ""
// when the file is to be skipped.
func (m *Manager) settle(r *os.Root, target, conflict string) (string, error) {
	info, err := r.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return target, nil
	}
	if err != nil {
		return "", mapError(err)
	}
	switch conflict {
	case "exact":
		return "", ErrExists
	case ConflictSkip:
		return "", nil
	case ConflictReplace:
		if info.IsDir() {
			return "", ErrExists
		}
		return target, nil
	}
	dir, name := path.Split(target)
	ext := path.Ext(name)
	for n := 2; ; n++ {
		candidate := path.Join(dir, renameUpload(name, ext, n))
		if _, err := r.Lstat(candidate); errors.Is(err, fs.ErrNotExist) {
			return candidate, nil
		} else if err != nil {
			return "", mapError(err)
		}
	}
}

func (m *Manager) copyEntry(ctx context.Context, job *Job, sr, dr *os.Root, rel, target, conflict string, uid int) error {

	if err := m.copyCheckpoint(ctx, job, 0); err != nil {
		return err
	}
	entry, err := job.entry(rel)
	if err != nil {
		return err
	}
	if entry.Complete {
		if job.durable != nil {
			if entry.Skip {
				return nil
			}
			current, err := sr.Lstat(rel)
			if err != nil || entry.Source != entryVersion(current) {
				return ErrTextConflict
			}
			return nil
		}
		if job.Move {
			current, err := sr.Lstat(rel)
			previous := job.completedSource[rel]
			if err != nil {
				return mapError(err)
			}
			if previous == nil || !os.SameFile(previous, current) || previous.Size() != current.Size() || !previous.ModTime().Equal(current.ModTime()) {
				return errors.New("source changed after copy; source retained")
			}
		}
		return nil
	}
	choice, explicit := job.choices[rel]
	if explicit {
		if choice.Action == "skip" {
			if job.Move {
				return ErrTextConflict
			}
			m.countTree(job, sr, rel)
			if !job.paged() {
				job.completed[rel] = true
			}
			return m.transferSkipped(job, rel)
		}
		if choice.SourceVersion != "" {
			if err := checkChoice(sr, rel, choice.SourceVersion); err != nil {
				return err
			}
		}
		target = choiceTarget(target, choice)
		if m.protected(Root{Dir: job.destinationDir}, target) {
			return ErrProtected
		}
		if choice.TargetVersion != "" && entry.Target == "" {
			if err := checkChoice(dr, target, choice.TargetVersion); err != nil {
				return err
			}
		}
		conflict = choice.Action
		if conflict == "copy" || choice.Name != "" {
			conflict = "exact"
			if entry.Target == "" {
				if _, err := dr.Lstat(target); !os.IsNotExist(err) {
					return ErrExists
				}
			}
		}
	}

	if entry.Target != "" {
		target = entry.Target
	}
	m.jobs.mu.Lock()
	job.Current = rel
	m.jobs.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := sr.Lstat(rel)
	if err != nil {
		return mapError(err)
	}
	if job.durable != nil && info.Mode().IsRegular() && entry.Source != entryVersion(info) {
		return ErrTextConflict
	}
	switch {
	case info.IsDir():
		if existing, e := dr.Stat(target); e == nil && os.SameFile(info, existing) {
			if job.Move || conflict == ConflictReplace {
				return ErrInvalid
			}
			if conflict == ConflictSkip {
				m.countTree(job, sr, rel)
				if !job.paged() {
					job.completed[rel] = true
				}
				return m.transferSkipped(job, rel)
			}
			var e error
			target, e = m.settle(dr, target, ConflictRename)
			if e != nil {
				return e
			}
		}
		// A folder that exists is merged into, whatever the conflict rule.
		if existing, err := dr.Lstat(target); err == nil && !existing.IsDir() {
			if target, err = m.settle(dr, target, conflict); err != nil || target == "" {
				if err == nil && job.Move {
					return errors.New("destination conflict skipped; source folder retained")
				}
				return err
			}
		}
		if _, err := dr.Lstat(target); errors.Is(err, fs.ErrNotExist) {
			if err := dr.Mkdir(target, 0o770); err != nil {
				return mapError(err)
			}
			m.own(dr, target, uid, true)
		}
		if job.targets != nil || job.paged() {
			if !job.paged() {
				job.targets[rel] = target
			}
			if err := m.transferTarget(job, rel, target); err != nil {
				return err
			}
		}
		return eachChild(sr, rel, func(child fs.DirEntry) error {
			if hidden(child.Name()) {
				if job.Move {
					return ErrProtected
				}
				return nil
			}
			return m.copyEntry(ctx, job, sr, dr, path.Join(rel, child.Name()), path.Join(target, child.Name()), conflict, uid)
		})
	case !info.Mode().IsRegular():
		// Links, sockets and devices are not copied; a move must retain them.
		if job.Move {
			return ErrInvalid
		}
		return nil
	}
	final, err := m.settle(dr, target, conflict)
	if err != nil {
		return err
	}
	if final == "" {
		if job.Move {
			return errors.New("destination conflict skipped; source retained")
		}
		job.c.count.Add(1)
		job.c.copied.Add(info.Size())
		job.c.committed.Add(info.Size())
		if job.completed != nil {
			if !job.paged() {
				job.completed[rel] = true
			}
		}
		return m.transferSkipped(job, rel)
	}
	return m.copyFile(ctx, job, sr, dr, rel, final, info, uid)
}

func (m *Manager) copyFile(ctx context.Context, job *Job, sr, dr *os.Root, rel, final string, info fs.FileInfo, uid int) error {
	m.jobs.mu.Lock()
	job.Current = rel
	m.jobs.mu.Unlock()
	in, err := sr.Open(rel)
	if err != nil {
		return mapError(err)
	}
	defer in.Close()
	raw := make([]byte, 6)
	rand.Read(raw)
	part := path.Join(path.Dir(final), ".titanos-copy-"+hex.EncodeToString(raw)+partSuffix)
	out, err := dr.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o660)
	if err != nil {
		return mapError(err)
	}
	if err := m.transferPart(job, out, part); err != nil {
		out.Close()
		dr.Remove(part)
		return err
	}
	done := false
	defer func() {
		out.Close()
		if !done {
			dr.Remove(part)
		}
	}()
	buf := make([]byte, 256<<10)
	for {
		if err := m.copyCheckpoint(ctx, job, 0); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := in.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return mapError(werr)
			}
			job.c.copied.Add(int64(n))
			if err := m.copyCheckpoint(ctx, job, n); err != nil {
				return err
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return mapError(err)
		}
	}
	if err := m.syncFile(out); err != nil {
		return mapError(err)
	}
	if err := out.Close(); err != nil {
		return mapError(err)
	}
	if err := m.copyCheckpoint(ctx, job, 0); err != nil {
		return err
	}
	current, e := sr.Lstat(rel)
	if e != nil || !os.SameFile(info, current) || entryVersion(info) != entryVersion(current) {
		return ErrTextConflict
	}
	m.own(dr, part, uid, false)
	// The modification time is kept, so that photos stay in order.
	dr.Chtimes(part, info.ModTime(), info.ModTime())
	if err := m.saveTransfer(job, "publish "+final); err != nil {
		return err
	}
	var publishErr error
	if choice, ok := job.choices[rel]; ok {
		if choice.TargetVersion != "" {
			if err := checkChoice(dr, final, choice.TargetVersion); err != nil {
				return err
			}
		}
		if choice.Action == "replace" {
			publishErr = dr.Rename(part, final)
		} else {
			publishErr = publishNewFile(dr, part, final)
		}
	} else {
		publishErr = dr.Rename(part, final)
	}
	if err := publishErr; err != nil {
		return mapError(err)
	}
	done = true
	if job.targets != nil && !job.paged() {
		job.targets[rel] = final
	}
	job.c.count.Add(1)
	job.c.committed.Add(info.Size())
	if job.completed != nil && !job.paged() {
		job.completed[rel] = true
		if job.completedSource != nil && job.durable == nil {
			job.completedSource[rel] = info
		}
	}
	return m.finishTransfer(job, dr, rel, final)
}

// snapshot returns a copy of a job with its counters filled in.
func (m *Manager) snapshot(job *Job) Job {
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	return snapshotLocked(job)
}

func snapshotLocked(job *Job) Job {
	return Job{Durable: job.Durable, Blocked: job.Blocked, Waiting: job.State == JobRunning && job.Waiting, Paused: job.Paused, Priority: job.Priority, Rate: job.Rate, ID: job.ID, Operation: job.Operation, Move: job.Move, State: job.State, From: job.From, To: job.To, Dir: job.Dir, Names: append([]string{}, job.Names...), Files: job.Files, Bytes: job.Bytes, Current: job.Current, Error: job.Error, Started: job.Started, Finished: job.Finished, Retryable: !job.finishing && !job.Blocked && (job.durable == nil || job.durable.Pending == "" && !job.durable.forgotten) && job.Operation == "" && (job.State == JobFailed || job.State == JobCancelled), Copied: job.c.copied.Load(), Done: job.c.count.Load()}
}

// Jobs returns the copy jobs of a user, the newest first.
func (m *Manager) Jobs(user string) []Job {
	m.loadTransfers(user)
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	out := []Job{}
	for i := len(m.jobs.list) - 1; i >= 0; i-- {
		if job := m.jobs.list[i]; job.user == user {
			out = append(out, snapshotLocked(job))
		}
	}
	return out
}

// CancelJob stops a job of the user. What was copied stays; a move never
// removes an original before its copy is whole.
func (m *Manager) CancelJob(user, id string) error {
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	for _, job := range m.jobs.list {
		if job.ID == id && job.user == user {
			job.cancel()
			return nil
		}
	}
	return ErrNoJob
}

// prune drops the oldest finished jobs. The caller holds the lock.
func (m *Manager) pruneJobs() {
	finished := map[string]int{}
	eligible := func(j *Job) bool {
		return j.State != JobRunning && !j.Blocked && !j.finishing && (j.durable == nil || j.durable.Pending == "")
	}
	for _, j := range m.jobs.list {
		if eligible(j) {
			finished[j.user]++
		}
	}
	for i := 0; i < len(m.jobs.list); {
		j := m.jobs.list[i]
		if finished[j.user] > maxJobs && eligible(j) {
			m.jobs.list = append(m.jobs.list[:i], m.jobs.list[i+1:]...)
			finished[j.user]--
		} else {
			i++
		}
	}
}

// RetryCopy resumes the same in-memory job. Committed files and completed
// top-level moves are skipped; current root permissions are checked again.
func (m *Manager) RetryCopy(user, id string, src, dst Root, uid int) (Job, error) {
	m.loadTransfers(user)
	m.jobs.mu.Lock()
	var job *Job
	for _, candidate := range m.jobs.list {
		if candidate.ID == id && candidate.user == user {
			job = candidate
			break
		}
	}
	if job == nil || job.finishing || job.durable != nil && job.durable.forgotten || job.Operation != "" || (job.State != JobFailed && job.State != JobCancelled) || job.From != src.ID || job.To != dst.ID || job.sourceDir != src.Dir || job.destinationDir != dst.Dir {
		m.jobs.mu.Unlock()
		return Job{}, ErrNoJob
	}
	if !dst.Writable || job.Move && !src.Writable || len(src.Hide) > 0 {
		m.jobs.mu.Unlock()
		return Job{}, ErrReadOnly
	}
	for _, rel := range job.Names {
		if m.protected(src, rel) {
			m.jobs.mu.Unlock()
			return Job{}, ErrProtected
		}
	}
	sr, err := os.OpenRoot(src.Dir)
	if err != nil {
		m.jobs.mu.Unlock()
		return Job{}, mapError(err)
	}
	dr, err := os.OpenRoot(dst.Dir)
	if err != nil {
		sr.Close()
		m.jobs.mu.Unlock()
		return Job{}, mapError(err)
	}
	if info, err := dr.Stat(job.Dir); err != nil || !info.IsDir() {
		sr.Close()
		dr.Close()
		m.jobs.mu.Unlock()
		return Job{}, ErrNotFound
	}
	if err := m.verifyTransfer(job, src, dst, dr); err != nil {
		sr.Close()
		dr.Close()
		m.jobs.mu.Unlock()
		return Job{}, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	job.sourceRoot = src
	job.destinationRoot = dst
	job.cancel = cancel
	job.State = JobRunning
	job.Paused = false
	job.Error = ""
	job.Finished = time.Time{}
	job.c.copied.Store(job.c.committed.Load())
	m.jobs.mu.Unlock()
	go func() {
		defer sr.Close()
		defer dr.Close()
		err := m.runCopy(ctx, job, sr, dr, src, dst, job.Names, job.Dir, job.Move, job.conflict, uid)
		m.rememberMoveUndo(job)
		m.jobs.mu.Lock()
		defer func() {
			job.finishing = true
			m.jobs.mu.Unlock()
			if job.durable != nil {
				if err := m.saveTransfer(job, job.durable.Pending); err != nil {
					m.jobs.mu.Lock()
					job.Error = "Could not persist transfer outcome: " + err.Error()
					job.Blocked = true
					m.jobs.mu.Unlock()
				}
			}
			job.closeStore()
			m.jobs.mu.Lock()
			job.finishing = false
			m.jobs.mu.Unlock()
		}()
		job.Finished = m.now()
		switch {
		case ctx.Err() != nil:
			job.State = JobCancelled
		case err != nil:
			job.State = JobFailed
			job.Error = err.Error()
		default:
			job.State = JobDone
			job.Current = ""
		}
	}()
	return m.snapshot(job), nil
}

func (m *Manager) rememberMoveUndo(job *Job) {
	m.RecordUndo(job.user, "move", job.pendingUndo)
	job.pendingUndo = nil
}

// Recheck every source entry before removing a cross-root move. In particular,
// a file added while the job was paused must never be deleted unseen.
func verifyCopiedSource(ctx context.Context, job *Job, r *os.Root, rel string) error {
	return walkEntries(r, rel, func(p string, info fs.FileInfo) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := job.entry(p)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if entry.Target == "" {
				return ErrTextConflict
			}
			return nil
		}
		if job.durable != nil {
			if !info.Mode().IsRegular() || entry.Source != entryVersion(info) || !entry.Complete || entry.Skip {
				return ErrTextConflict
			}
			return nil
		}
		previous := job.completedSource[p]
		if previous == nil || !info.Mode().IsRegular() || !os.SameFile(previous, info) || entryVersion(previous) != entryVersion(info) {
			return ErrTextConflict
		}
		return nil
	})
}

// Busy reports whether a copy or move of any account is not finished yet.
// A restart of titand would break it off.
func (m *Manager) Busy() bool {
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	for _, job := range m.jobs.list {
		if job.State == JobRunning {
			return true
		}
	}
	return false
}
