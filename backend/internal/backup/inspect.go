package backup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Checking a repository and restoring single files. Both run like a backup:
// one job at a time, in the background, and cancellable.

// Entry is a file or folder in a saved state.
type Entry struct {
	Name  string    `json:"name"`
	Path  string    `json:"path"`
	Type  string    `json:"type"` // file, dir, symlink
	Size  uint64    `json:"size"`
	MTime time.Time `json:"mtime"`
}

// Limits of browsing and restoring a saved state.
const (
	maxListed   = 50000
	listPage    = 500
	maxIncludes = 200
)

// Check lets restic check a job's repository; readData is the share of the
// data that is also read back, in percent (0: only the structure). The
// result is kept in the job's state; OnCheck hears about it.
func (m *Manager) Check(id string, readData int) error {
	if m.Disabled {
		return ErrDisabled
	}
	job, ok := m.find(id)
	if !ok {
		return ErrNotFound
	}
	if readData < 0 || readData > 100 {
		return invalid("share of data to read")
	}
	return m.begin(job, "check", func(ctx context.Context) error {
		_, base, env, cleanup, err := m.repository(job)
		if err != nil {
			return err
		}
		defer cleanup()
		args := append(slices.Clone(base), "check")
		if readData > 0 {
			args = append(args, fmt.Sprintf("--read-data-subset=%d%%", readData))
		}
		out, err := m.run(ctx, env, args, nil)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		now := m.now()
		problem := ""
		if err != nil {
			problem = lastLines(out, job.Password)
			if problem == "" {
				problem = err.Error()
			}
		}
		m.update(job, func(s *State) { s.LastCheck, s.CheckError = &now, problem })
		if m.OnCheck != nil {
			if problem != "" {
				m.OnCheck(job.ID, errors.New(problem))
			} else {
				m.OnCheck(job.ID, nil)
			}
		}
		slog.Info("backup repository checked", "job", job.ID, "ok", problem == "")
		if problem != "" {
			return errors.New(problem)
		}
		return nil
	})
}

// cleanPath accepts an absolute path inside a saved state.
func cleanPath(p string) (string, error) {
	if p == "" {
		return "/", nil
	}
	if !strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\x00\r\n") || slices.Contains(strings.Split(p, "/"), "..") || len(p) > 4096 {
		return "", invalid("path")
	}
	return filepath.Clean(p), nil
}

// Files lists one folder of a saved state, a page at a time; the second
// value is how many entries the folder has, the third whether the listing
// stopped at the limit.
func (m *Manager) Files(ctx context.Context, id, snapshot, folder string, offset int) ([]Entry, int, bool, error) {
	if m.Disabled {
		return nil, 0, false, ErrDisabled
	}
	job, ok := m.find(id)
	if !ok {
		return nil, 0, false, ErrNotFound
	}
	if !snapshotForm.MatchString(snapshot) {
		return nil, 0, false, invalid("backup")
	}
	folder, err := cleanPath(folder)
	if err != nil {
		return nil, 0, false, err
	}
	_, base, env, cleanup, err := m.repository(job)
	if err != nil {
		return nil, 0, false, err
	}
	defer cleanup()
	var entries []Entry
	cut := false
	listCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	// Without --recursive, restic lists only what is directly in the folder.
	out, err := m.run(listCtx, env, append(slices.Clone(base), "ls", "--json", "--no-lock", snapshot, folder), func(line string) {
		var node struct {
			StructType string    `json:"struct_type"`
			Name       string    `json:"name"`
			Type       string    `json:"type"`
			Path       string    `json:"path"`
			Size       uint64    `json:"size"`
			MTime      time.Time `json:"mtime"`
		}
		if json.Unmarshal([]byte(line), &node) != nil || node.StructType != "node" || node.Path == folder {
			return
		}
		if len(entries) >= maxListed {
			cut = true
			cancel()
			return
		}
		entries = append(entries, Entry{Name: node.Name, Path: node.Path, Type: node.Type, Size: node.Size, MTime: node.MTime})
	})
	if err != nil && !cut {
		if ctx.Err() != nil {
			return nil, 0, false, ctx.Err()
		}
		return nil, 0, false, errors.New(lastLines(out, job.Password))
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		if (a.Type == "dir") != (b.Type == "dir") {
			if a.Type == "dir" {
				return -1
			}
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	total := len(entries)
	start := min(max(offset, 0), total)
	end := min(start+listPage, total)
	return entries[start:end], total, cut, nil
}

// RestoreFiles restores chosen files and folders of a saved state into a
// new folder on a volume, never over what is there.
func (m *Manager) RestoreFiles(id, snapshot, volume string, paths []string) (string, error) {
	if m.Disabled {
		return "", ErrDisabled
	}
	job, ok := m.find(id)
	if !ok {
		return "", ErrNotFound
	}
	if !snapshotForm.MatchString(snapshot) {
		return "", invalid("backup")
	}
	if len(paths) == 0 || len(paths) > maxIncludes {
		return "", invalid("choose 1 to %d files or folders", maxIncludes)
	}
	var includes []string
	for _, p := range paths {
		clean, err := cleanPath(p)
		if err != nil || clean == "/" {
			return "", invalid("path %q", p)
		}
		includes = append(includes, "--include", clean)
	}
	if !slices.Contains(m.mounted(), volume) {
		return "", invalid("volume")
	}
	target := filepath.Join(m.mountRoot, volume, "restored", job.ID+"-"+m.now().Format("20060102-150405"))
	err := m.begin(job, "restore", func(ctx context.Context) error {
		_, base, env, cleanup, err := m.repository(job)
		if err != nil {
			return err
		}
		defer cleanup()
		if err := os.MkdirAll(target, 0o770); err != nil {
			return err
		}
		args := append(append(slices.Clone(base), "restore", snapshot, "--target", target, "--json"), includes...)
		out, err := m.run(ctx, env, args, func(line string) {
			var message struct {
				Type    string  `json:"message_type"`
				Percent float64 `json:"percent_done"`
				Done    uint64  `json:"bytes_restored"`
				Total   uint64  `json:"total_bytes"`
			}
			if json.Unmarshal([]byte(line), &message) == nil && message.Type == "status" {
				m.update(job, func(s *State) {
					s.Percent, s.BytesDone, s.BytesTotal = 100*message.Percent, message.Done, message.Total
				})
			}
		})
		if err != nil && ctx.Err() == nil {
			return errors.New(lastLines(out, job.Password))
		}
		if err == nil {
			m.update(job, func(s *State) { s.RestoredTo = target })
			slog.Info("backup files restored", "job", job.ID, "snapshot", snapshot, "count", len(paths), "to", target)
		}
		return err
	})
	return target, err
}
