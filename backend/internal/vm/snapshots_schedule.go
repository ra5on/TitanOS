package vm

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// Snapshot schedules, pruning and tree helpers (split from snapshots.go,
// PLAN 75-12).

func validSnapshotSchedule(s SnapshotSchedule) bool {
	if !s.Enabled {
		return true
	}
	if _, e := time.Parse("15:04", s.Time); e != nil {
		return false
	}
	if len(s.Days) == 0 || len(s.Days) > 7 || s.Keep < 1 || s.Keep > 100 || s.Memory && !s.AllowPause {
		return false
	}
	seen := map[int]bool{}
	for _, d := range s.Days {
		if d < 0 || d > 6 || seen[d] {
			return false
		}
		seen[d] = true
	}
	return true
}
func (m *Manager) SetSnapshotSchedule(name string, s SnapshotSchedule) error {
	if m.Disabled {
		return ErrDisabled
	}
	if !validSnapshotSchedule(s) {
		return ErrInvalid
	}
	lock, err := m.operation(name)
	if err != nil {
		return err
	}
	defer lock.Unlock()
	v, err := m.get(name)
	if err != nil {
		return err
	}
	if s.Enabled && m.snapshotReason(v) != "" {
		return ErrSnapshotUnsupported
	}
	if m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	v.SnapshotSchedule = s
	if err = m.persistVM(v); err != nil {
		return err
	}
	m.mu.Lock()
	m.vms[name] = v
	m.mu.Unlock()
	return nil
}
func (m *Manager) runSnapshotSchedules(ctx context.Context, mounted []string, now time.Time) {
	if m.ScheduleAdmission != nil {
		leave, err := m.ScheduleAdmission()
		if err != nil {
			return
		}
		defer leave()
	}
	// Go caches time.Local. Use the current NAS setting explicitly so changing
	// the time zone does not require a daemon restart for these schedules.
	if m.Timezone != nil {
		location, err := time.LoadLocation(m.Timezone())
		if err != nil {
			return
		}
		now = now.In(location)
	}
	for _, status := range m.List(mounted) {
		s := status.SnapshotSchedule
		if !status.Available || !s.Enabled || !validSnapshotSchedule(s) || now.Format("15:04") != s.Time || !slices.Contains(s.Days, int(now.Weekday())) {
			continue
		}
		v := status.VM
		root := m.snapshotRoot(v)
		if m.snapshotPath(root, true) != nil {
			continue
		}
		store := config.Open(root)
		key := now.Format("2006-01-02") + "T" + s.Time
		var last struct {
			Key string `json:"key"`
		}
		err := store.ReadJSON("last-scheduled.json", &last)
		if err != nil && !errors.Is(err, fs.ErrNotExist) || last.Key == key {
			continue
		}
		last.Key = key
		if store.WriteJSON("last-scheduled.json", last) != nil {
			continue
		}
		err = m.createSnapshot(ctx, v.Name, SnapshotRequest{Name: now.Format("2006-01-02 15:04"), Memory: s.Memory, AllowPause: s.AllowPause}, mounted, true)
		if err != nil {
			slog.Error("scheduled VM snapshot failed", "vm", v.Name, "err", err)
			m.mu.Lock()
			if job := m.snapshotJobs[v.Name]; job == nil || job.State != "running" {
				m.snapshotJobs[v.Name] = &Job{State: "failed", Text: err.Error()}
			}
			m.mu.Unlock()
		}
	}
}
func (m *Manager) pruneSnapshots(v VM) error {
	keep := v.SnapshotSchedule.Keep
	if keep < 1 {
		return nil
	}
	status, err := m.SnapshotStatus(v.Name, v.volumes())
	if err != nil {
		return err
	}
	count := 0
	for _, s := range status.Snapshots {
		if !s.Automatic {
			continue
		}
		count++
		if count > keep {
			if err = config.Open(m.snapshotRoot(v)).RemoveAll(s.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

func snapshotTreeSize(path string) (size, allocated int64, err error) {
	err = filepath.WalkDir(path, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		if !st.Mode().IsRegular() && !st.IsDir() {
			return ErrBadImage
		}
		if !st.IsDir() {
			size += st.Size()
		}
		allocated += st.Sys().(*syscall.Stat_t).Blocks * 512
		return nil
	})
	return
}
func copySnapshotTree(from, to string) error {
	st, err := os.Lstat(from)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return copySnapshotFile(from, to)
	}
	if err = os.Mkdir(to, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(from)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err = copySnapshotTree(filepath.Join(from, entry.Name()), filepath.Join(to, entry.Name())); err != nil {
			return err
		}
	}
	stat := st.Sys().(*syscall.Stat_t)
	if err = os.Chown(to, int(stat.Uid), int(stat.Gid)); err != nil {
		return err
	}
	if err = os.Chmod(to, st.Mode().Perm()); err != nil {
		return err
	}
	return syncSnapshotDir(to)
}
