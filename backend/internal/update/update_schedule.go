package update

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// nextCheckLocked tells when the next scheduled check is due: the set
// interval after the last check of any kind (button, login or schedule),
// at most checkRetry after a failed one, and a few minutes after the start
// before the first. m.mu must be held.
func (m *Manager) nextCheckLocked() (time.Time, bool) {
	if m.Disabled || m.started.IsZero() || m.settings.Manual() {
		return time.Time{}, false
	}
	if m.lastCheck.IsZero() {
		return m.started.Add(checkStartDelay), true
	}
	wait := m.settings.CheckInterval()
	if m.checkError != "" && wait > checkRetry {
		wait = checkRetry
	}
	return m.lastCheck.Add(wait), true
}

// scheduledCheck checks for updates if the schedule says it is time.
func (m *Manager) scheduledCheck(ctx context.Context) bool {
	m.mu.Lock()
	next, ok := m.nextCheckLocked()
	m.mu.Unlock()
	if !ok || m.now().Before(next) {
		return false
	}
	if err := m.Check(ctx); err != nil && !errors.Is(err, ErrNoKey) {
		slog.Warn("looking for updates failed", "err", err)
	}
	return true
}

// Run looks for updates as often as the settings say (once a day unless
// set otherwise) and installs them at the set time if that is wanted,
// until ctx is done.
func (m *Manager) Run(ctx context.Context) {
	if m.Disabled {
		return
	}
	m.mu.Lock()
	m.started = m.now()
	m.mu.Unlock()
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	// waiting is since when a scheduled app update waits for work that a
	// restart of titand would break off; it is tried again every minute,
	// for up to six hours.
	var waiting time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		m.Confirm()
		if m.appOnTrial() {
			m.settleApp()
		}
		m.scheduledCheck(ctx)
		m.mu.Lock()
		settings, available, phase, pending := m.settings, m.available, m.progress.Phase, m.pending
		m.mu.Unlock()
		retry := !waiting.IsZero() && m.now().Sub(waiting) < 6*time.Hour
		// Manual checks rule out installing by itself, even if a file
		// edited by hand asks for both.
		if !settings.AutoInstall || settings.Manual() || (m.now().Format("15:04") != settings.InstallTime && !retry) {
			continue
		}
		switch {
		case available != nil && pending == nil && phase != "download" && phase != "switch":
			if err := m.Install(ctx); err != nil {
				if errors.Is(err, ErrWorking) {
					if !retry {
						slog.Info("scheduled app update waits until copies, backups and disk tests are done")
						waiting = m.now()
					}
					continue
				}
				waiting = time.Time{}
				slog.Warn("scheduled update not started", "err", err)
				continue
			}
			waiting = time.Time{}
			// Wait for it, then restart within the same night.
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case <-time.After(5 * time.Second):
					}
					m.mu.Lock()
					phase := m.progress.Phase
					m.mu.Unlock()
					if phase == "done" && m.Reboot != nil {
						slog.Info("restarting for the scheduled update")
						if err := m.Reboot(); err != nil {
							slog.Error("restart for the update failed", "err", err)
						}
						return
					}
					if phase != "download" && phase != "switch" {
						return
					}
				}
			}()
		}
	}
}
