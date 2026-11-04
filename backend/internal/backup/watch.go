package backup

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"
)

// Run starts the jobs when their time has come, until ctx is done. A NAS
// that was off at that time catches up after the start.
func (m *Manager) Run(ctx context.Context) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for minute := 0; ; minute++ {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		// The first minutes belong to the start: volumes and network.
		if minute < 3 {
			continue
		}
		m.mu.Lock()
		jobs := slices.Clone(m.jobs)
		m.mu.Unlock()
		for _, job := range jobs {
			// A job whose shares are not there has no state to tell
			// whether it ran; it waits, and says so when that lasts.
			if m.stateFile(job) == "" {
				m.checkUnavailable(job)
				continue
			}
			m.mu.Lock()
			delete(m.unavailable, job.ID)
			m.mu.Unlock()
			state := m.state(job)
			if due(job, state, m.local(m.now())) {
				start := m.Start
				if m.ScheduledStart != nil {
					start = m.ScheduledStart
				}
				if err := start(job.ID); err != nil && !errors.Is(err, ErrRunning) {
					slog.Warn("backup not started", "job", job.ID, "err", err)
				}
			}
			m.checkOverdue(job, state)
		}
	}
}

// Plugged starts the USB copies to a drive that was just plugged in whose
// last attempt failed, for example because the drive was not there. Jobs
// run by hand only are left alone.
func (m *Manager) Plugged(deviceID string) {
	if m.Disabled {
		return
	}
	m.mu.Lock()
	jobs := slices.Clone(m.jobs)
	m.mu.Unlock()
	for _, job := range jobs {
		if (job.Destination.Type != "usb-copy" && job.Destination.Type != "usb") || job.Destination.DeviceID != deviceID || !job.Enabled || m.stateFile(job) == "" {
			continue
		}
		if state := m.state(job); state.Running || state.FailingSince == nil {
			continue
		}
		slog.Info("backup drive plugged in, catching up", "job", job.ID)
		if err := m.Start(job.ID); err != nil && !errors.Is(err, ErrRunning) {
			slog.Warn("backup not started", "job", job.ID, "err", err)
		}
	}
}

// checkOverdue tells OnOverdue when a job became overdue.
func (m *Manager) checkOverdue(job Job, state State) {
	m.tellOverdue(job, state.Running, overdue(job, state, m.local(m.now())))
}

// checkUnavailable treats a job that cannot run because none of its shares
// is there like one whose backups fail: overdue after two of its moments and
// overdueAfter.
func (m *Manager) checkUnavailable(job Job) {
	now := m.local(m.now())
	m.mu.Lock()
	if m.unavailable == nil {
		m.unavailable = map[string]time.Time{}
	}
	since, ok := m.unavailable[job.ID]
	if !ok {
		since = now
		m.unavailable[job.ID] = now
	}
	m.mu.Unlock()
	m.tellOverdue(job, false, overdue(job, State{FailingSince: &since, LastRun: &since}, now))
}

func (m *Manager) tellOverdue(job Job, running, late bool) {
	m.mu.Lock()
	if m.overdue == nil {
		m.overdue = map[string]bool{}
	}
	// A running job keeps what was said: it may still fail.
	changed := !running && late != m.overdue[job.ID]
	if changed {
		m.overdue[job.ID] = late
	}
	m.mu.Unlock()
	if changed && late && m.OnOverdue != nil {
		slog.Warn("backup overdue", "job", job.ID)
		m.OnOverdue(job.ID)
	}
}
