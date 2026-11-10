package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Start begins a backup now. It returns at once.
func (m *Manager) Start(id string) error {
	if m.Disabled {
		return ErrDisabled
	}
	job, ok := m.find(id)
	if !ok {
		return ErrNotFound
	}
	return m.begin(job, "backup", func(ctx context.Context) error { return m.backup(ctx, job) })
}

// begin runs work for a job in the background, one job at a time.
func (m *Manager) begin(job Job, phase string, work func(context.Context) error) error {
	m.state(job)
	m.mu.Lock()
	state := m.states[job.ID]
	if state == nil {
		state = &State{}
		m.states[job.ID] = state
	}
	if state.Running {
		m.mu.Unlock()
		return ErrRunning
	}
	ctx, cancel := context.WithCancel(context.Background())
	state.Running, state.Queued, state.Phase, state.Percent, state.BytesDone, state.BytesTotal, state.BandwidthLimit = true, true, phase, 0, 0, 0, 0
	m.cancels[job.ID] = cancel
	m.mu.Unlock()
	m.runs.Add(1)
	go func() {
		defer m.runs.Done()
		m.queue.Lock()
		m.update(job, func(s *State) { s.Queued = false })
		err := work(ctx)
		m.queue.Unlock()
		cancelled := ctx.Err() != nil
		var firstFailure, recovered bool
		cancel()
		now := m.now()
		m.mu.Lock()
		delete(m.cancels, job.ID)
		m.mu.Unlock()
		m.update(job, func(s *State) {
			s.Running, s.Queued, s.Phase, s.Percent, s.BandwidthLimit = false, false, "", 0, 0
			if phase != "backup" {
				// A failed check is kept as CheckError, a failed restore as
				// RestoreError: neither is a failed backup.
				if phase == "restore" {
					switch {
					case err == nil:
						s.RestoreError = ""
					case !cancelled:
						s.RestoreError = err.Error()
					}
				}
				return
			}
			s.LastRun, s.LastError, s.LastErrorCode = &now, "", ""
			switch {
			case cancelled:
				s.LastError = errCancelled
			case err != nil:
				s.LastError, s.LastErrorCode = err.Error(), errorCode(err)
				if s.FailingSince == nil {
					s.FailingSince, firstFailure = &now, true
				}
			default:
				recovered = s.FailingSince != nil
				s.LastOK, s.FailingSince = &now, nil
			}
		})
		if err != nil && !cancelled {
			slog.Warn("backup job failed", "job", job.ID, "phase", phase, "err", err)
		}
		// One message when a job starts failing and one when it works again,
		// not one per attempt: an hourly job with its drive unplugged
		// would otherwise report every hour.
		if phase == "backup" && !cancelled && m.OnResult != nil && (err == nil || firstFailure) {
			m.OnResult(job.ID, err)
		}
		if recovered && m.OnRecovered != nil {
			m.OnRecovered(job.ID)
		}
		if phase == "backup" && !cancelled && m.ended != nil {
			m.ended(job.ID, err)
		}
	}()
	return nil
}

// Stop cancels what a job is doing.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cancel, ok := m.cancels[id]
	if !ok {
		return ErrNotFound
	}
	cancel()
	return nil
}

func (m *Manager) backup(ctx context.Context, job Job) (result error) {
	if plainCopy(job.Destination.Type) {
		if job.Destination.Type == "smb-copy" {
			target, err := m.smbTarget(job.Destination.TargetID)
			if err != nil {
				return err
			}
			return m.withSMB(ctx, target, func(root *os.Root) error { return m.copyTo(ctx, job, root) })
		}
		if job.Destination.Type == "local-copy" {
			return m.copyLocal(ctx, job)
		}
		return m.copyUSB(ctx, job)
	}
	sel := m.selection(job)
	if len(sel.paths) == 0 && len(job.Applications) == 0 {
		return ErrSources
	}
	if len(job.Applications) > 0 && m.PrepareApplications == nil {
		return invalid("application backups unavailable")
	}
	_, base, env, cleanup, err := m.repository(job)
	if err != nil {
		return err
	}
	defer cleanup()
	// restic cannot change its limit while it runs: the one that holds at
	// the start holds for the whole run, also past a window boundary.
	limit := job.Bandwidth.at(m.local(m.now()))
	base = append(base, resticLimits(limit)...)
	m.update(job, func(s *State) { s.BandwidthLimit = limit })

	// A repository that is not there yet is made; one that is, is used. This
	// comes before applications are stopped: a target that cannot be reached
	// must not stop them on every retry.
	if looked, err := m.run(ctx, env, append(slices.Clone(base), "cat", "config", "--quiet"), nil); err != nil {
		if made, err := m.run(ctx, env, append(slices.Clone(base), "init"), nil); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			// Both answers: the first says why it could not be opened.
			return fmt.Errorf("the place for the backup cannot be reached or set up:\n%s", lastLines(looked+"\n"+made, job.Password))
		}
	}
	// What a run that was cut short left locked.
	_, _ = m.run(ctx, env, append(slices.Clone(base), "unlock"), nil) // there may be no stale lock; the backup below reports real errors

	if len(job.Applications) > 0 {
		extra, finish, err := m.PrepareApplications(ctx, job, m.homeVolume(job), sel.paths)
		if err != nil {
			return err
		}
		defer func() { result = errors.Join(result, finish()) }()
		sel.paths = append(sel.paths, extra...)
	}
	paths := sel.paths
	if len(paths) == 0 {
		return ErrSources
	}
	slog.Info("backup started", "job", job.ID, "shares", len(paths))

	args := append(slices.Clone(base), "backup", "--json", "--tag", "titanos", "--exclude", ".recycle", "--exclude", "*.titanos-part")
	for _, exclude := range sel.excludes {
		args = append(args, "--exclude", exclude)
	}
	args = append(args, paths...)
	var total uint64
	var freed []string
	var out string
	for {
		out, err = m.runBackup(ctx, job, env, args, &total)
		// A full target: with the job's leave, the oldest version goes
		// and the run starts again, until it fits or one version is left.
		if err == nil || ctx.Err() != nil || !noSpace(out) || !job.Keep.DropsOldest() {
			break
		}
		gone, ferr := m.dropOldest(ctx, job, env, base)
		if ferr != nil || gone == "" {
			break
		}
		freed = append(freed, gone)
		slog.Warn("backup target full, oldest version removed", "job", job.ID, "version", gone)
	}
	if len(freed) > 0 && m.OnFreed != nil {
		m.OnFreed(job.ID, len(freed))
	}
	warning := ""
	if len(freed) > 0 {
		warning = fmt.Sprintf("The target was full; removed the oldest %d version(s): %s", len(freed), strings.Join(freed, ", "))
	}
	var exit *exec.ExitError
	// unreadable: the version of this run misses files (a disk with read
	// errors, a folder without rights). The older versions may be the
	// only copy of them, so none is forgotten for such a run: a row of
	// incomplete nights would otherwise push the last complete ones out.
	unreadable := false
	switch {
	case errors.As(err, &exit) && exit.ExitCode() == 3:
		// A backup was made, but some files could not be read.
		unreadable = true
		warning = strings.TrimSpace(warning + "\n" + lastLines(out, job.Password) + "\nOlder backups are all kept until every file can be read again.")
	case err != nil:
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if noSpace(out) {
			return fmt.Errorf("%w:\n%s", errNoSpace, lastLines(out, job.Password))
		}
		return errors.New(lastLines(out, job.Password))
	}
	if len(sel.notes) > 0 {
		missing := "Not found, not saved: " + strings.Join(sel.notes, ", ")
		warning = strings.TrimSpace(warning + "\n" + missing)
	}
	// Old backups are the only copy of what is missing now: "keep the
	// newest of N days" would forget them after N such runs.
	if len(sel.missing) > 0 {
		missing := "Not available, not saved: " + strings.Join(sel.missing, ", ") + "\nOlder backups are all kept until it is back."
		warning = strings.TrimSpace(warning + "\n" + missing)
	}
	m.update(job, func(s *State) { s.Bytes, s.Warning, s.Phase, s.Percent = total, warning, "cleanup", 100 })
	if len(sel.missing) > 0 && m.OnIncomplete != nil {
		m.OnIncomplete(job.ID)
	}

	if job.Keep.Chosen() && len(sel.missing) == 0 && !unreadable {
		if err := m.forgetChosen(ctx, job, env, base); err != nil && ctx.Err() == nil {
			m.update(job, func(s *State) { s.Warning = strings.TrimSpace(s.Warning + "\n" + err.Error()) })
		}
	} else if job.Keep.Daily+job.Keep.Weekly+job.Keep.Monthly > 0 && len(sel.missing) == 0 && !unreadable {
		args := append(slices.Clone(base), "forget", "--tag", "titanos", "--group-by", "tags", "--prune")
		for flag, count := range map[string]int{"--keep-daily": job.Keep.Daily, "--keep-weekly": job.Keep.Weekly, "--keep-monthly": job.Keep.Monthly} {
			if count > 0 {
				args = append(args, flag, fmt.Sprint(count))
			}
		}
		if out, err := m.run(ctx, env, args, nil); err != nil && ctx.Err() == nil {
			// The backup itself is there; say so instead of calling it failed.
			m.update(job, func(s *State) { s.Warning = strings.TrimSpace(s.Warning + "\n" + lastLines(out, job.Password)) })
		}
	}
	slog.Info("backup finished", "job", job.ID, "bytes", total)
	return nil
}

// runBackup runs restic backup once and reports progress and the size.
func (m *Manager) runBackup(ctx context.Context, job Job, env, args []string, total *uint64) (string, error) {
	return m.run(ctx, env, args, func(line string) {
		var message struct {
			Type    string  `json:"message_type"`
			Percent float64 `json:"percent_done"`
			Done    uint64  `json:"bytes_done"`
			Total   uint64  `json:"total_bytes"`
			Size    uint64  `json:"total_bytes_processed"`
		}
		if json.Unmarshal([]byte(line), &message) != nil {
			return
		}
		switch message.Type {
		case "status":
			m.update(job, func(s *State) {
				s.Percent, s.BytesDone, s.BytesTotal = 100*message.Percent, message.Done, message.Total
			})
		case "summary":
			*total = message.Size
		}
	})
}

// Snapshots lists the backups of a job, the newest first.
func (m *Manager) Snapshots(ctx context.Context, id string) ([]Snapshot, error) {
	if m.Disabled {
		return nil, ErrDisabled
	}
	job, ok := m.find(id)
	if !ok {
		return nil, ErrNotFound
	}
	_, base, env, cleanup, err := m.repository(job)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	var output strings.Builder
	out, err := m.run(ctx, env, append(base, "snapshots", "--json", "--no-lock"), func(line string) { output.WriteString(line) })
	if err != nil {
		return nil, errors.New(lastLines(out, job.Password))
	}
	var raw []struct {
		ID      string    `json:"short_id"`
		Time    time.Time `json:"time"`
		Paths   []string  `json:"paths"`
		Summary *struct {
			Bytes uint64 `json:"total_bytes_processed"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(output.String()), &raw); err != nil {
		return nil, err
	}
	list := []Snapshot{}
	for _, entry := range raw {
		snapshot := Snapshot{ID: entry.ID, Time: entry.Time, Paths: entry.Paths}
		if entry.Summary != nil {
			snapshot.Bytes = entry.Summary.Bytes
		}
		list = append(list, snapshot)
	}
	slices.Reverse(list)
	return list, nil
}

var snapshotForm = regexp.MustCompile(`^[0-9a-f]{8,64}$`)

// Restore copies a backup back: into a new folder on a volume, never over
// what is there. It returns the folder at once; State tells how far it is.
func (m *Manager) Restore(id, snapshot, volume string) (string, error) {
	if m.Disabled {
		return "", ErrDisabled
	}
	job, ok := m.find(id)
	if !ok {
		return "", ErrNotFound
	}
	if !snapshotForm.MatchString(snapshot) || !slices.Contains(m.mounted(), volume) {
		return "", invalid("backup snapshot or volume")
	}
	return m.restoreVerifiedCopy(job, snapshot, volume, nil, false)
}
