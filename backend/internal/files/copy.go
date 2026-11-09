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
// A job keeps its progress in memory (and, for targets on a data volume, in a
// durable plan there). Files are written next to their target under a hidden
// name and renamed when they are whole, like uploads, so nobody finds half a
// file. A file that cannot be transferred is listed on the job and skipped;
// copy_problems.go has the rules.

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
	// Planning is true while the job still walks the selection.
	Planning bool `json:"planning"`
	// Conflict is the rule for names that exist at the target.
	Conflict string `json:"conflict,omitempty"`
	// From and To are the roots, for the list.
	From  string   `json:"from"`
	To    string   `json:"to"`
	Dir   string   `json:"dir"`
	Names []string `json:"names"`
	// Files and Bytes are what the job found to do; Done and Copied what is done.
	Files   int64  `json:"files"`
	Done    int64  `json:"done"`
	Bytes   int64  `json:"bytes"`
	Copied  int64  `json:"copied"`
	Current string `json:"current"`
	// Error is the technical reason a job stopped, ErrorCode its kind.
	Error     string `json:"error"`
	ErrorCode string `json:"errorCode,omitempty"`
	// Failed counts entries that could not be transferred, Kept entries a
	// move left at the source, Identical files "missing or changed" left
	// alone. Problems lists the first of the failed and kept entries.
	Failed    int64         `json:"failed"`
	Kept      int64         `json:"kept"`
	Identical int64         `json:"identical"`
	Problems  []CopyProblem `json:"problems"`
	// Rebooted is set for a saved job after a restart of the NAS: it cannot
	// be retried, only started again for the missing files.
	Rebooted                    bool      `json:"rebooted"`
	Started                     time.Time `json:"started"`
	Finished                    time.Time `json:"finished"`
	Retryable                   bool      `json:"retryable"`
	choices                     map[string]CopyChoice
	completed                   map[string]bool
	completedTop                map[string]bool
	skipped                     map[string]bool
	targets                     map[string]string
	reported                    map[string]bool
	conflict                    string
	planned                     bool
	sourceRoot, destinationRoot Root
	sourceDir, destinationDir   string
	completedSource             map[string]fs.FileInfo

	pendingUndo  []UndoMove
	undoEligible map[string]bool
	user         string
	cancel       context.CancelFunc
	c            *counters
	// download is set for downloads from a link (Operation "download").
	download *download
}

// counters are what the copying goroutine counts while others read it.
type counters struct{ copied, count, committed atomic.Int64 }

type jobs struct {
	mu   sync.Mutex
	list []*Job
}

// planTotals is what the planning walk found.
type planTotals struct {
	files, bytes, write int64
}

// StartCopy begins copying (or moving) paths of one root into a folder of
// another. It returns the job at once.
func (m *Manager) StartCopy(src Root, paths []string, dst Root, dir string, move bool, conflict, user string, uid int) (Job, error) {
	return m.StartCopyChoices(src, paths, dst, dir, move, conflict, user, uid, nil)
}

// StartCopyChoices checks the request and starts the job. The job walks the
// selection itself, so that a big folder does not hold up the request: it
// counts, checks the free space against what will really be written and
// fills its durable plan in one pass.
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
		return Job{}, ErrSourceProtected
	}
	if conflict == "" {
		conflict = ConflictRename
	}
	if conflict != ConflictSkip && conflict != ConflictReplace && conflict != ConflictRename && conflict != ConflictMissing {
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
			return Job{}, ErrIntoItself
		}
		// Into the folder it is in, only "keep both" makes sense.
		if same && path.Dir(p) == dir && len(choices) == 0 && (move || conflict != ConflictRename) {
			return Job{}, ErrSameFolder
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
	if info, e := dr.Stat(dir); e != nil || !info.IsDir() {
		sr.Close()
		dr.Close()
		return Job{}, ErrNotFound
	}
	for _, rel := range rels {
		if _, e := sr.Lstat(rel); e != nil {
			sr.Close()
			dr.Close()
			return Job{}, mapError(e)
		}
	}
	raw := make([]byte, 8)
	rand.Read(raw)
	ctx, cancel := context.WithCancel(context.Background())
	job := &Job{sourceRoot: src, destinationRoot: dst, choices: choices, ID: hex.EncodeToString(raw), Move: move, State: JobRunning, Planning: true, From: src.ID, To: dst.ID, Dir: dir, Names: rels,
		Started: m.now(), user: user, cancel: cancel, c: &counters{}, completed: map[string]bool{}, completedTop: map[string]bool{}, skipped: map[string]bool{}, targets: map[string]string{}, conflict: conflict, sourceDir: src.Dir, destinationDir: dst.Dir, completedSource: map[string]fs.FileInfo{}, undoEligible: map[string]bool{}}
	// The job is listed before its saved plan exists, so that loading saved
	// transfers never mistakes the plan in the making for a second job.
	m.jobs.mu.Lock()
	m.jobs.list = append(m.jobs.list, job)
	m.jobs.mu.Unlock()
	// The answer shows the job as it starts; taken after the goroutine began,
	// a small job could already be past its planning walk.
	started := m.snapshot(job)
	go m.runJob(ctx, job, sr, dr, src, dst, uid)
	return started, nil
}

// runJob plans (once) and carries out a job, then records how it ended.
func (m *Manager) runJob(ctx context.Context, job *Job, sr, dr *os.Root, src, dst Root, uid int) {
	defer sr.Close()
	defer dr.Close()
	var err error
	if !job.planned {
		err = m.planJob(ctx, job, sr, dr, src, dst)
	}
	if err == nil {
		err = m.runCopy(ctx, job, sr, dr, src, dst, job.Names, job.Dir, job.Move, job.conflict, uid)
	}
	m.rememberMoveUndo(job)
	m.jobs.mu.Lock()
	job.Planning = false
	job.Finished = m.now()
	switch {
	case ctx.Err() != nil:
		job.State = JobCancelled
	case err != nil:
		job.State, job.Error, job.ErrorCode = JobFailed, err.Error(), jobErrorCode(err)
	default:
		job.State = JobDone
		job.Current = ""
	}
	m.pruneJobs()
	job.finishing = true
	m.jobs.mu.Unlock()
	if job.durable != nil {
		if err := m.saveTransfer(job, job.durable.Pending); err != nil {
			m.jobs.mu.Lock()
			job.Error = "Could not persist transfer outcome: " + err.Error()
			job.ErrorCode = "store"
			job.Blocked = true
			m.jobs.mu.Unlock()
		}
	}
	job.closeStore()
	m.jobs.mu.Lock()
	job.finishing = false
	m.jobs.mu.Unlock()
}

// planJob walks the selection once: it counts files and bytes, refuses a
// folder that is (an alias of) the target, adds up what will really be
// written and, for a durable job, writes the plan.
func (m *Manager) planJob(ctx context.Context, job *Job, sr, dr *os.Root, src, dst Root) error {
	targetDir, err := dr.Stat(job.Dir)
	if err != nil || !targetDir.IsDir() {
		return ErrNotFound
	}
	var totals planTotals
	walk := func(add func(string, transferEntry) error) error {
		totals = planTotals{}
		for _, rel := range job.Names {
			info, err := sr.Lstat(rel)
			if err != nil {
				continue // reported when the job gets there
			}
			if err := m.planEntry(ctx, job, sr, dr, rel, path.Join(job.Dir, path.Base(rel)), info, targetDir, false, add, &totals); err != nil {
				return err
			}
		}
		m.jobs.mu.Lock()
		job.Files, job.Bytes = totals.files, totals.bytes
		m.jobs.mu.Unlock()
		return nil
	}
	if err := m.prepareTransfer(job, src, dst, walk); err != nil {
		return err
	}
	m.jobs.mu.Lock()
	job.Planning = false
	job.planned = true
	m.jobs.mu.Unlock()
	// A move within the same root renames and needs no room.
	if _, free := Usage(dst); !(job.Move && src.Dir == dst.Dir) && uint64(totals.write) > free {
		return ErrNoSpace
	}
	return nil
}

func (m *Manager) planEntry(ctx context.Context, job *Job, sr, dr *os.Root, rel, target string, info, targetDir fs.FileInfo, skipped bool, add func(string, transferEntry) error, totals *planTotals) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c, ok := job.choices[rel]; ok {
		skipped = skipped || c.Action == ConflictSkip
		target = choiceTarget(target, c)
	}
	if info.IsDir() {
		// Inode comparison catches aliases and bind mounts across roots.
		if os.SameFile(info, targetDir) {
			return ErrIntoItself
		}
		var inner error
		_ = eachChild(sr, rel, func(entry fs.DirEntry) error { // an unreadable folder is reported when the job gets there
			if hidden(entry.Name()) {
				return nil
			}
			p := path.Join(rel, entry.Name())
			child, err := sr.Lstat(p)
			if err != nil {
				return nil
			}
			inner = m.planEntry(ctx, job, sr, dr, p, path.Join(target, entry.Name()), child, targetDir, skipped, add, totals)
			return inner
		})
		return inner
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if m.planHook != nil {
		m.planHook(rel)
	}
	totals.files++
	totals.bytes += info.Size()
	if err := add(rel, transferEntry{Source: copyVersion(info)}); err != nil {
		return err
	}
	if !skipped {
		totals.write += plannedWrite(dr, job, rel, target, info)
	}
	return nil
}

// plannedWrite is how many bytes a file will add at the target.
func plannedWrite(dr *os.Root, job *Job, rel, target string, info fs.FileInfo) int64 {
	action := job.conflict
	if c, ok := job.choices[rel]; ok {
		action = c.Action
	}
	existing, err := dr.Lstat(target)
	if err != nil {
		return info.Size()
	}
	switch action {
	case ConflictSkip:
		return 0
	case ConflictMissing:
		if existing.IsDir() || sameContent(info, existing) {
			return 0
		}
	case ConflictReplace:
		if existing.IsDir() {
			return 0
		}
	}
	return info.Size()
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
	sameRoot := src.Dir == dst.Dir
	for _, rel := range rels {
		if err := m.copyCheckpoint(ctx, job, 0); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		entry, err := job.entry(rel)
		if err != nil {
			return storeErr(err)
		}
		if entry.Top {
			continue
		}
		target := path.Join(dir, path.Base(rel))
		choice, explicit := job.choices[rel]
		if move && job.undoEligible != nil {
			if _, known := job.undoEligible[rel]; !known {
				existing, e := dr.Lstat(choiceTarget(target, choice))
				action := conflict
				if explicit {
					action = choice.Action
				}
				job.undoEligible[rel] = os.IsNotExist(e) || (e == nil && !existing.IsDir() && action == ConflictRename)
			}
		}
		m.jobs.mu.Lock()
		failedBefore := job.Failed
		m.jobs.mu.Unlock()
		if move && sameRoot {
			if err := m.moveRename(ctx, job, sr, dst, rel, target, conflict, uid); err != nil {
				return err
			}
		} else {
			if err := m.copyEntry(ctx, job, sr, dr, rel, target, conflict, uid); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if move {
				// Only what was copied and has not changed since goes.
				if err := m.copyCheckpoint(ctx, job, 0); err != nil {
					return err
				}
				if _, err := m.removeMoved(ctx, job, sr, dr, rel); err != nil {
					return err
				}
			}
		}
		m.jobs.mu.Lock()
		clean := job.Failed == failedBefore
		m.jobs.mu.Unlock()
		gone := true
		if move {
			_, e := sr.Lstat(rel)
			gone = errors.Is(e, fs.ErrNotExist)
		}
		// A top entry with problems below it stays open for a retry.
		if !clean || !gone && !(explicit && choice.Action == ConflictSkip) {
			continue
		}
		if !job.paged() {
			job.completedTop[rel] = true
		}
		if err := storeErr(m.transferTop(job, rel)); err != nil {
			return err
		}
		if move && gone && job.undoEligible[rel] {
			entry, err := job.entry(rel)
			if err != nil {
				return storeErr(err)
			}
			actual := entry.Target
			if actual == "" {
				actual = choiceTarget(target, choice)
			}
			job.pendingUndo = append(job.pendingUndo, UndoMove{CurrentRoot: dst, OriginalRoot: src, Current: actual, Original: rel})
		}
	}
	return nil
}

func (m *Manager) countTree(job *Job, r *os.Root, rel string) {
	_ = walk(r, rel, func(_ string, info fs.FileInfo) { // the callback collects what it can; unreadable entries are skipped
		if info.Mode().IsRegular() {
			job.c.count.Add(1)
			job.c.copied.Add(info.Size())
			job.c.committed.Add(info.Size())
		}
	})
}

func (m *Manager) setCurrent(job *Job, rel string) {
	m.jobs.mu.Lock()
	job.Current = rel
	m.jobs.mu.Unlock()
}

// What settle decided for a file whose name may exist at the target.
const (
	settleNew = iota
	settleReplace
	settleSkip
	settleIdentical
)

// settle decides the name a file gets in the target folder and whether it
// replaces, skips or is identical to what is there.
func (m *Manager) settle(r *os.Root, target, action string, info fs.FileInfo) (string, int, error) {
	existing, err := r.Lstat(target)
	if errors.Is(err, fs.ErrNotExist) {
		return target, settleNew, nil
	}
	if err != nil {
		return "", 0, mapError(err)
	}
	if info != nil && os.SameFile(info, existing) {
		// The source itself: only a second copy under a new name makes sense.
		switch action {
		case ConflictSkip, ConflictMissing:
			return "", settleSkip, nil
		case ConflictReplace, "exact":
			return "", 0, ErrSameFolder
		}
	}
	switch action {
	case "exact":
		return "", 0, ErrExists
	case ConflictSkip:
		return "", settleSkip, nil
	case ConflictReplace, ConflictMissing:
		if existing.IsDir() {
			return "", 0, failure("type_mismatch", ErrExists)
		}
		if action == ConflictMissing && info != nil && sameContent(info, existing) {
			return target, settleIdentical, nil
		}
		return target, settleReplace, nil
	}
	name, err := freeName(r, target)
	return name, settleNew, err
}

// freeName finds "name (2).ext", "name (3).ext" … next to target.
func freeName(r *os.Root, target string) (string, error) {
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

// copyEntry copies a file or folder. A problem with it or anything inside
// is recorded on the job; only errors that end the job are returned.
func (m *Manager) copyEntry(ctx context.Context, job *Job, sr, dr *os.Root, rel, target, conflict string, uid int) error {
	if err := m.copyCheckpoint(ctx, job, 0); err != nil {
		return err
	}
	err := m.copyOne(ctx, job, sr, dr, rel, target, conflict, uid)
	if err == nil || fatalCopy(err) {
		return err
	}
	return m.noteProblem(job, rel, err, "")
}

func (m *Manager) copyOne(ctx context.Context, job *Job, sr, dr *os.Root, rel, target, conflict string, uid int) error {
	entry, err := job.entry(rel)
	if err != nil {
		return storeErr(err)
	}
	if entry.Complete {
		if entry.Skip || job.Move {
			return nil // a move checks the source when it removes it
		}
		if current, err := sr.Lstat(rel); err == nil && !m.copiedUnchanged(job, rel, current, entry) {
			return failure("changed_after_copy", ErrSourceChanged)
		}
		return nil
	}
	choice, explicit := job.choices[rel]
	action := conflict
	if explicit {
		if choice.Action == ConflictSkip {
			m.countTree(job, sr, rel)
			return m.markSkipped(job, rel)
		}
		if choice.SourceVersion != "" {
			if err := checkChoice(sr, rel, choice.SourceVersion); err != nil {
				return failure("changed", ErrSourceChanged)
			}
		}
		target = choiceTarget(target, choice)
		if m.protected(Root{Dir: job.destinationDir}, target) {
			return ErrProtected
		}
		if choice.Target != "" {
			if err := m.copyParents(dr, job.destinationRoot, path.Dir(target), uid); err != nil {
				return err
			}
		}
		if choice.TargetVersion != "" && entry.Target == "" {
			if err := checkChoice(dr, target, choice.TargetVersion); err != nil {
				return failure("changed_target", ErrSourceChanged)
			}
		}
		action = choice.Action
		if action == "copy" || choice.Name != "" {
			action = "exact"
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
	// Never write over (or recycle) what a VM holds or the root hides.
	if m.protected(job.destinationRoot, target) || job.destinationRoot.secret(target) {
		return ErrProtected
	}
	m.setCurrent(job, rel)
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := sr.Lstat(rel)
	if err != nil {
		return mapError(err)
	}
	if job.durable != nil && info.Mode().IsRegular() && entry.Source == "" {
		// Added to the source after the plan was made: copied as well.
		m.jobs.mu.Lock()
		job.Files++
		job.Bytes += info.Size()
		m.jobs.mu.Unlock()
	}
	switch {
	case info.IsDir():
		return m.copyDir(ctx, job, sr, dr, rel, target, info, action, uid, entry)
	case !info.Mode().IsRegular():
		// Links, sockets and devices are not copied; a move leaves them
		// at the source and says so.
		return nil
	}
	final, how, err := m.settle(dr, target, action, info)
	if err != nil {
		return err
	}
	switch how {
	case settleSkip:
		job.c.count.Add(1)
		job.c.copied.Add(info.Size())
		job.c.committed.Add(info.Size())
		return m.markSkipped(job, rel)
	case settleIdentical:
		job.c.count.Add(1)
		job.c.copied.Add(info.Size())
		job.c.committed.Add(info.Size())
		m.jobs.mu.Lock()
		job.Identical++
		m.jobs.mu.Unlock()
		if job.Move {
			// Decision of the owner (4.11.2026): a move never deletes a
			// source only because the target looks the same; it stays.
			if err := m.markSkipped(job, rel); err != nil {
				return err
			}
			return m.noteKept(job, rel, "kept_identical")
		}
		return m.markCopied(job, dr, rel, final, info)
	}
	return m.copyFile(ctx, job, sr, dr, rel, final, info, uid, how == settleReplace)
}

func (m *Manager) copyDir(ctx context.Context, job *Job, sr, dr *os.Root, rel, target string, info fs.FileInfo, action string, uid int, entry transferEntry) error {
	if existing, e := dr.Stat(target); e == nil && os.SameFile(info, existing) {
		// Into the folder it is in: only a copy under a new name.
		if action != ConflictRename {
			return ErrSameFolder
		}
		name, err := freeName(dr, target)
		if err != nil {
			return err
		}
		target = name
	}
	// A folder that exists is merged into; a file of that name is a conflict.
	if existing, err := dr.Lstat(target); err == nil && !existing.IsDir() {
		switch action {
		case ConflictRename:
			if target, err = freeName(dr, target); err != nil {
				return err
			}
		case ConflictSkip:
			m.countTree(job, sr, rel)
			return m.markSkipped(job, rel)
		default:
			return failure("type_mismatch", ErrExists)
		}
	}
	if m.protected(job.destinationRoot, target) || job.destinationRoot.secret(target) {
		return ErrProtected
	}
	if _, err := dr.Lstat(target); errors.Is(err, fs.ErrNotExist) {
		if err := dr.Mkdir(target, 0o770); err != nil {
			return mapError(err)
		}
		m.own(dr, target, uid, true)
	}
	if !job.paged() && job.targets != nil {
		job.targets[rel] = target
	}
	if entry.Target != target {
		if err := storeErr(m.transferTarget(job, rel, target)); err != nil {
			return err
		}
	}
	var inner error
	err := eachChild(sr, rel, func(child fs.DirEntry) error {
		if hidden(child.Name()) {
			return nil // internal files stay; a move says so when it removes the source
		}
		inner = m.copyEntry(ctx, job, sr, dr, path.Join(rel, child.Name()), path.Join(target, child.Name()), action, uid)
		return inner
	})
	if inner != nil {
		return inner
	}
	if err != nil {
		return failure("unreadable_folder", mapError(err))
	}
	return nil
}

func (m *Manager) copyFile(ctx context.Context, job *Job, sr, dr *os.Root, rel, final string, info fs.FileInfo, uid int, replace bool) error {
	m.setCurrent(job, rel)
	in, err := sr.Open(rel)
	if err != nil {
		return failure(problemCode(mapError(err), "read"), mapError(err))
	}
	defer in.Close()
	id := randomID()
	part := path.Join(path.Dir(final), ".titanos-copy-"+id+partSuffix)
	// 0600 until it is published: the sweep after a crash trusts only that.
	out, err := dr.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return failure(problemCode(mapError(err), "write"), mapError(err))
	}
	m.holdCopyPart(job.destinationRoot, id, part)
	defer m.releaseCopyPart(job.destinationRoot, id)
	if err := m.transferPart(job, out, part); err != nil {
		out.Close()
		dr.Remove(part)
		return storeErr(err)
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
				return failure(problemCode(mapError(werr), "write"), mapError(werr))
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
			return failure(problemCode(mapError(err), "read"), mapError(err))
		}
	}
	if err := m.syncFile(out); err != nil {
		return failure(problemCode(mapError(err), "write"), mapError(err))
	}
	if err := out.Close(); err != nil {
		return failure(problemCode(mapError(err), "write"), mapError(err))
	}
	if err := m.copyCheckpoint(ctx, job, 0); err != nil {
		return err
	}
	current, e := sr.Lstat(rel)
	if e != nil || !os.SameFile(info, current) || copyVersion(info) != copyVersion(current) {
		return failure("changed", ErrSourceChanged)
	}
	m.own(dr, part, uid, false)
	// The modification time is kept, so that photos stay in order.
	_ = dr.Chtimes(part, info.ModTime(), info.ModTime()) // permissions and times are a convenience; the data are there either way
	if err := storeErr(m.saveTransfer(job, "publish "+final)); err != nil {
		return err
	}
	final, publishErr := m.publishCopy(job, dr, rel, part, final, replace)
	if publishErr != nil {
		// Nothing was published: the outcome is known.
		empty := ""
		if err := storeErr(m.appendTransfer(job, transferEvent{Pending: &empty})); err != nil {
			return err
		}
		return publishErr
	}
	done = true
	job.c.count.Add(1)
	job.c.committed.Add(info.Size())
	return m.markCopied(job, dr, rel, final, info)
}

// publishCopy gives the finished part file its name and returns that name.
// A file it replaces goes to the recycle bin where there is one.
func (m *Manager) publishCopy(job *Job, dr *os.Root, rel, part, final string, replace bool) (string, error) {
	choice, explicit := job.choices[rel]
	if explicit && choice.TargetVersion != "" {
		if err := checkChoice(dr, final, choice.TargetVersion); err != nil {
			return final, failure("changed_target", ErrSourceChanged)
		}
	}
	if replace {
		recycled, err := m.retire(job, dr, final)
		if err != nil {
			return final, err
		}
		if recycled {
			return final, mapError(renameNoReplace(dr, part, final))
		}
		return final, mapError(dr.Rename(part, final))
	}
	if explicit {
		return final, mapError(renameNoReplace(dr, part, final))
	}
	// The name was free when the copy began, which may be hours ago. A
	// file that took it meanwhile (another job, an upload, a save over
	// SMB) is never replaced unasked: the copy takes the next free name.
	err := renameNoReplace(dr, part, final)
	for range 5 {
		if !errors.Is(err, fs.ErrExist) {
			break
		}
		free, nameErr := freeName(dr, final)
		if nameErr != nil {
			return final, mapError(nameErr)
		}
		final = free
		err = renameNoReplace(dr, part, final)
	}
	return final, mapError(err)
}

// retire moves a file that is about to be replaced into the recycle bin of
// the share it is in, if that share has one. It reports whether it did.
func (m *Manager) retire(job *Job, r *os.Root, target string) (bool, error) {
	bin, ok := job.destinationRoot.binFor(target)
	if !ok {
		return false, nil
	}
	if _, err := m.recycleIn(r, bin, target, job.user); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, failure("recycle_failed", mapError(err))
	}
	return true, nil
}

// markCopied records a file whose copy is at final (or was already there).
func (m *Manager) markCopied(job *Job, dr *os.Root, rel, final string, info fs.FileInfo) error {
	if !job.paged() {
		if job.targets != nil {
			job.targets[rel] = final
		}
		if job.completed != nil {
			job.completed[rel] = true
		}
		if job.completedSource != nil && job.durable == nil {
			job.completedSource[rel] = info
		}
	}
	return storeErr(m.finishTransfer(job, dr, rel, final, copyVersion(info)))
}

// markSkipped records an entry that is left alone.
func (m *Manager) markSkipped(job *Job, rel string) error {
	if !job.paged() {
		if job.completed != nil {
			job.completed[rel] = true
		}
		if job.skipped != nil {
			job.skipped[rel] = true
		}
	}
	return storeErr(m.transferSkipped(job, rel))
}

// copiedUnchanged reports whether a source file is still what was copied.
func (m *Manager) copiedUnchanged(job *Job, rel string, info fs.FileInfo, entry transferEntry) bool {
	if !info.Mode().IsRegular() {
		return false
	}
	if job.durable != nil {
		return entry.Source != "" && copyVersion(info) == entry.Source
	}
	previous := job.completedSource[rel]
	return previous != nil && os.SameFile(previous, info) && copyVersion(previous) == copyVersion(info)
}
