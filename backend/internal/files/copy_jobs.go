package files

import (
	"context"
	"os"
	"time"
)

// The list of copy and move jobs: snapshots, cancel, retry, pruning
// (split from copy.go, PLAN 75-12).

// snapshot returns a copy of a job with its counters filled in.
func (m *Manager) snapshot(job *Job) Job {
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	return snapshotLocked(job)
}

func snapshotLocked(job *Job) Job {
	retryable := !job.finishing && !job.Blocked && !job.Rebooted && (job.durable == nil || job.durable.Pending == "" && !job.durable.forgotten) && (job.Operation == "" || job.download != nil) &&
		(job.State == JobFailed || job.State == JobCancelled || job.State == JobDone && job.Failed > 0 && job.Operation == "")
	return Job{Durable: job.Durable, Blocked: job.Blocked, Waiting: job.State == JobRunning && job.Waiting, Paused: job.Paused, Priority: job.Priority, Rate: job.Rate, ID: job.ID, Operation: job.Operation, Move: job.Move, State: job.State, Planning: job.State == JobRunning && job.Planning, Conflict: job.conflict, From: job.From, To: job.To, Dir: job.Dir, Names: append([]string{}, job.Names...), Files: job.Files, Bytes: job.Bytes, Current: job.Current, Error: job.Error, ErrorCode: job.ErrorCode, Failed: job.Failed, Kept: job.Kept, Identical: job.Identical, Problems: append([]CopyProblem{}, job.Problems...), Rebooted: job.Rebooted, Started: job.Started, Finished: job.Finished, Retryable: retryable, Copied: job.c.copied.Load(), Done: job.c.count.Load()}
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
			// A failed download keeps its part file for a retry until then.
			m.discardDownload(j.download)
			m.jobs.list = append(m.jobs.list[:i], m.jobs.list[i+1:]...)
			finished[j.user]--
		} else {
			i++
		}
	}
}

// RetryCopy resumes the same job. Committed files and completed top-level
// entries are skipped; entries that failed are tried again, files that were
// added or changed at the source since the plan are copied as they are now.
// Current root permissions are checked again.
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
	finished := job != nil && (job.State == JobFailed || job.State == JobCancelled || job.State == JobDone && job.Failed > 0)
	if job == nil || !finished || job.finishing || job.durable != nil && job.durable.forgotten || job.Operation != "" || job.From != src.ID || job.To != dst.ID || job.sourceDir != src.Dir || job.destinationDir != dst.Dir {
		m.jobs.mu.Unlock()
		return Job{}, ErrNoJob
	}
	if job.Rebooted {
		m.jobs.mu.Unlock()
		return Job{}, ErrRebooted
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
	job.Planning = !job.planned
	job.Paused = false
	job.Error = ""
	job.ErrorCode = ""
	job.Failed, job.Kept = 0, 0
	job.Problems = nil
	job.reported = nil
	job.Finished = time.Time{}
	if job.skipped == nil {
		job.skipped = map[string]bool{}
	}
	job.c.copied.Store(job.c.committed.Load())
	m.jobs.mu.Unlock()
	// The answer shows the job as it starts; taken after the goroutine began,
	// a small job could already be past its planning walk.
	started := m.snapshot(job)
	go m.runJob(ctx, job, sr, dr, src, dst, uid)
	return started, nil
}

func (m *Manager) rememberMoveUndo(job *Job) {
	m.RecordUndo(job.user, "move", job.pendingUndo)
	job.pendingUndo = nil
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
