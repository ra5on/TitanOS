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
	"syscall"
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
	Files                     int64     `json:"files"`
	Done                      int64     `json:"done"`
	Bytes                     int64     `json:"bytes"`
	Copied                    int64     `json:"copied"`
	Current                   string    `json:"current"`
	Error                     string    `json:"error"`
	Started                   time.Time `json:"started"`
	Finished                  time.Time `json:"finished"`
	Retryable                 bool      `json:"retryable"`
	choices                   map[string]CopyChoice
	completed                 map[string]bool
	completedTop              map[string]bool
	targets                   map[string]string
	conflict                  string
	sourceDir, destinationDir string
	completedSource           map[string]fs.FileInfo

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
	if info, err := dr.Stat(dir); err != nil || !info.IsDir() {
		closeRoots()
		return Job{}, ErrNotFound
	}
	var p plan
	for _, rel := range rels {
		if err := walk(sr, rel, func(_ string, info fs.FileInfo) {
			if info.Mode().IsRegular() {
				p.files++
				p.bytes += info.Size()
			}
		}); err != nil {
			closeRoots()
			return Job{}, mapError(err)
		}
	}
	// A move inside one filesystem renames and needs no room.
	if _, free := Usage(dst); !(move && sameFilesystem(src, dst) && len(choices) == 0) && uint64(p.bytes) > free {
		closeRoots()
		return Job{}, ErrNoSpace
	}
	raw := make([]byte, 8)
	rand.Read(raw)
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{choices: choices, ID: hex.EncodeToString(raw), Move: move, State: JobRunning, From: src.ID, To: dst.ID, Dir: dir, Names: rels,
		Files: p.files, Bytes: p.bytes, Started: m.now(), user: user, cancel: cancel, c: &counters{}, completed: map[string]bool{}, completedTop: map[string]bool{}, targets: map[string]string{}, conflict: conflict, sourceDir: src.Dir, destinationDir: dst.Dir, completedSource: map[string]fs.FileInfo{}, undoEligible: map[string]bool{}}
	m.jobs.mu.Lock()
	m.jobs.list = append(m.jobs.list, job)
	m.jobs.mu.Unlock()
	go func() {
		defer closeRoots()
		err := m.runCopy(ctx, job, sr, dr, src, dst, rels, dir, move, conflict, uid)
		m.rememberMoveUndo(job)
		m.jobs.mu.Lock()
		defer m.jobs.mu.Unlock()
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

func sameFilesystem(a, b Root) bool {
	device := func(dir string) (uint64, bool) {
		info, err := os.Stat(dir)
		if err != nil {
			return 0, false
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		return uint64(stat.Dev), ok
	}
	x, okX := device(a.Dir)
	y, okY := device(b.Dir)
	return okX && okY && x == y
}

// walk calls fn for a file or folder and everything below it, not following links.
func walk(r *os.Root, rel string, fn func(string, fs.FileInfo)) error {
	info, err := r.Lstat(rel)
	if err != nil {
		return err
	}
	fn(rel, info)
	if !info.IsDir() {
		return nil
	}
	dir, err := r.Open(rel)
	if err != nil {
		return err
	}
	entries, err := dir.ReadDir(-1)
	dir.Close()
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if hidden(entry.Name()) {
			continue
		}
		if err := walk(r, path.Join(rel, entry.Name()), fn); err != nil {
			return err
		}
	}
	return nil
}

func (m *Manager) runCopy(ctx context.Context, job *Job, sr, dr *os.Root, src, dst Root, rels []string, dir string, move bool, conflict string, uid int) error {
	renamed := move && src.Dir == dst.Dir && len(job.choices) == 0
	for _, rel := range rels {
		if err := m.copyCheckpoint(ctx, job, 0); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if job.completedTop[rel] {
			continue
		}
		target := choiceTarget(path.Join(dir, path.Base(rel)), job.choices[rel])
		if m.protected(dst, target) || dst.secret(target) {
			return ErrProtected
		}
		if job.choices[rel].Action == "skip" {
			m.countTree(job, sr, rel)
			job.completedTop[rel] = true
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
			if err := sr.Rename(rel, target); err != nil {
				return mapError(err)
			}
			m.countTree(job, sr, target)
			job.completedTop[rel] = true
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
			if err := sr.RemoveAll(rel); err != nil {
				return mapError(err)
			}
		}
		job.completedTop[rel] = true
		if move && job.undoEligible[rel] {
			actual := job.targets[rel]
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
	if job.completed[rel] {
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
			job.completed[rel] = true
			return nil
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
		if choice.TargetVersion != "" && job.targets[rel] == "" {
			if err := checkChoice(dr, target, choice.TargetVersion); err != nil {
				return err
			}
		}
		conflict = choice.Action
		if conflict == "copy" || choice.Name != "" {
			conflict = "exact"
			if job.targets[rel] == "" {
				if _, err := dr.Lstat(target); !os.IsNotExist(err) {
					return ErrExists
				}
			}
		}
	}

	if chosen, ok := job.targets[rel]; ok {
		target = chosen
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
	switch {
	case info.IsDir():
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
		if job.targets != nil {
			job.targets[rel] = target
		}
		dir, err := sr.Open(rel)
		if err != nil {
			return mapError(err)
		}
		entries, err := dir.ReadDir(-1)
		dir.Close()
		if err != nil {
			return mapError(err)
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		for _, entry := range entries {
			if hidden(entry.Name()) {
				if job.Move {
					return ErrProtected
				}
				continue
			}
			if err := m.copyEntry(ctx, job, sr, dr, path.Join(rel, entry.Name()), path.Join(target, entry.Name()), conflict, uid); err != nil {
				return err
			}
		}
		return nil
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
			job.completed[rel] = true
		}
		return nil
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
	if job.targets != nil {
		job.targets[rel] = final
	}
	job.c.count.Add(1)
	job.c.committed.Add(info.Size())
	if job.completed != nil {
		job.completed[rel] = true
		if job.completedSource != nil {
			job.completedSource[rel] = info
		}
	}
	return nil
}

// snapshot returns a copy of a job with its counters filled in.
func (m *Manager) snapshot(job *Job) Job {
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	return snapshotLocked(job)
}

func snapshotLocked(job *Job) Job {
	return Job{Waiting: job.State == JobRunning && job.Waiting, Paused: job.Paused, Priority: job.Priority, Rate: job.Rate, ID: job.ID, Operation: job.Operation, Move: job.Move, State: job.State, From: job.From, To: job.To, Dir: job.Dir, Names: append([]string{}, job.Names...), Files: job.Files, Bytes: job.Bytes, Current: job.Current, Error: job.Error, Started: job.Started, Finished: job.Finished, Retryable: job.Operation == "" && (job.State == JobFailed || job.State == JobCancelled), Copied: job.c.copied.Load(), Done: job.c.count.Load()}
}

// Jobs returns the copy jobs of a user, the newest first.
func (m *Manager) Jobs(user string) []Job {
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
	finished := 0
	for _, job := range m.jobs.list {
		if job.State != JobRunning {
			finished++
		}
	}
	for finished > maxJobs {
		for i, job := range m.jobs.list {
			if job.State != JobRunning {
				m.jobs.list = append(m.jobs.list[:i], m.jobs.list[i+1:]...)
				finished--
				break
			}
		}
	}
}

// RetryCopy resumes the same in-memory job. Committed files and completed
// top-level moves are skipped; current root permissions are checked again.
func (m *Manager) RetryCopy(user, id string, src, dst Root, uid int) (Job, error) {
	m.jobs.mu.Lock()
	var job *Job
	for _, candidate := range m.jobs.list {
		if candidate.ID == id && candidate.user == user {
			job = candidate
			break
		}
	}
	if job == nil || job.Operation != "" || (job.State != JobFailed && job.State != JobCancelled) || job.From != src.ID || job.To != dst.ID || job.sourceDir != src.Dir || job.destinationDir != dst.Dir {
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
	ctx, cancel := context.WithCancel(context.Background())
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
		defer m.jobs.mu.Unlock()
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
	return fs.WalkDir(r.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return mapError(err)
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return mapError(err)
		}
		if info.IsDir() {
			if _, ok := job.targets[p]; !ok {
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
