package files

import (
	"context"
	"time"
)

// ControlJob changes only the caller's live copy/move job. Settings remain in RAM.
func (m *Manager) ControlJob(user, id string, paused bool, priority int, rate int64) error {
	if priority < 0 || priority > 2 || rate < 0 || rate > 1024<<20 {
		return ErrInvalid
	}
	m.jobs.mu.Lock()
	defer m.jobs.mu.Unlock()
	for _, job := range m.jobs.list {
		if job.ID == id && job.user == user && job.State == JobRunning && job.Operation == "" {
			job.Paused, job.Priority, job.Rate = paused, priority, rate
			return nil
		}
	}
	return ErrNoJob
}

// A higher priority preempts at the next chunk. Equal priority uses FIFO per
// account. Paused jobs keep their temporary file and do not block the queue.
func (m *Manager) jobTurn(job *Job) bool {
	if job.Paused {
		return false
	}
	before := true
	for _, other := range m.jobs.list {
		if other == job {
			before = false
			continue
		}
		if other.user == job.user && other.State == JobRunning && other.Operation == "" && !other.Paused &&
			(other.Priority > job.Priority || other.Priority == job.Priority && before) {
			return false
		}
	}
	return true
}
func (m *Manager) copyCheckpoint(ctx context.Context, job *Job, bytes int) error {
	started := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		m.jobs.mu.Lock()
		ready, rate := m.jobTurn(job), job.Rate
		job.Waiting = !ready && !job.Paused
		m.jobs.mu.Unlock()
		delay := time.Duration(0)
		if rate > 0 {
			delay = time.Duration(float64(bytes)/float64(rate)*float64(time.Second)) - time.Since(started)
		}
		if ready && delay <= 0 {
			return nil
		}
		wait := 100 * time.Millisecond
		if ready && delay < wait {
			wait = delay
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
