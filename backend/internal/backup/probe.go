package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

type ProbeRequest struct {
	MaxBytes uint64 `json:"maxBytes"`
	MaxFiles int    `json:"maxFiles"`
}
type ProbeResult struct {
	At        time.Time `json:"at"`
	Snapshot  string    `json:"snapshot"`
	Files     int       `json:"files"`
	Bytes     uint64    `json:"bytes"`
	Truncated bool      `json:"truncated"`
	Error     string    `json:"error"`
}

func probePattern(path string) string {
	return strings.NewReplacer("\\", "\\\\", "*", "\\*", "?", "\\?", "[", "\\[", "]", "\\]").Replace(path)
}

// Probe restores a bounded selection into a private new directory and asks restic to
// verify restored content against that exact snapshot, never against changing live files.
func (m *Manager) Probe(id string, req ProbeRequest) error {
	if m.Disabled {
		return ErrDisabled
	}
	job, ok := m.find(id)
	if !ok {
		return ErrNotFound
	}
	if req.MaxBytes == 0 || req.MaxBytes > 1<<30 || req.MaxFiles < 1 || req.MaxFiles > 200 {
		return invalid("restore probe limits")
	}
	if job.Destination.Type == "usb-copy" || job.Destination.Type == "smb-copy" {
		return invalid("restore probes require a versioned restic backup")
	}
	return m.begin(job, "probe", func(ctx context.Context) (result error) {
		report := ProbeResult{At: m.now().UTC()}
		defer func() {
			if result != nil {
				report.Error = result.Error()
			}
			m.update(job, func(s *State) { s.Probe = &report })
			if m.OnProbe != nil {
				m.OnProbe(job.ID, result)
			}
		}()
		snapshots, err := m.Snapshots(ctx, id)
		if err != nil {
			return err
		}
		if len(snapshots) == 0 {
			return errors.New("no backup snapshot available")
		}
		report.Snapshot = snapshots[0].ID
		_, base, env, cleanup, err := m.repository(job)
		if err != nil {
			return err
		}
		defer cleanup()
		scanCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		paths := []string{}
		listed := 0
		cut := false
		out, err := m.run(scanCtx, env, append(slices.Clone(base), "ls", "--json", "--recursive", report.Snapshot), func(line string) {
			var entry struct {
				Type string `json:"type"`
				Path string `json:"path"`
				Size uint64 `json:"size"`
			}
			if json.Unmarshal([]byte(line), &entry) != nil || entry.Path == "" {
				return
			}
			listed++
			if listed > maxListed {
				cut = true
				cancel()
				return
			}
			if entry.Type != "file" {
				return
			}
			path, e := cleanPath(entry.Path)
			if e != nil || path == "/" {
				return
			}
			if entry.Size > req.MaxBytes-report.Bytes || len(paths) >= req.MaxFiles {
				report.Truncated = true
				return
			}
			paths = append(paths, path)
			report.Bytes += entry.Size
		})
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil && !cut {
			return errors.New(lastLines(out, job.Password))
		}
		if len(paths) == 0 {
			return errors.New("no regular files fit the restore probe limits")
		}
		report.Truncated = report.Truncated || cut
		report.Files = len(paths)
		volume := m.homeVolume(job)
		root, err := volumedata.Open(m.mountRoot, volume, "probes", m.mounted())
		if err != nil {
			return err
		}
		defer root.Close()
		var random [12]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		folder := hex.EncodeToString(random[:])
		if err := root.Mkdir(folder, 0700); err != nil {
			return err
		}
		defer root.RemoveAll(folder)
		targetDir, err := root.Open(folder)
		if err != nil {
			return err
		}
		defer targetDir.Close()
		target := "/proc/self/fd/3/tree"
		args := append(slices.Clone(base), "restore", report.Snapshot, "--target", target, "--verify", "--overwrite", "never")
		for _, path := range paths {
			args = append(args, "--include", probePattern(path))
		}
		if output, e := m.runFiles(ctx, env, args, nil, []*os.File{targetDir}); e != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("restore verification failed: %s", lastLines(output, job.Password))
		}
		if !slices.Contains(m.mounted(), volume) {
			return volumedata.ErrUnavailable
		}
		return nil
	})
}

// MaintenanceRun waits for the backend-owned operation and returns its actual result.
func (m *Manager) MaintenanceRun(ctx context.Context, id, kind string, readData int, probe ProbeRequest) error {
	var err error
	if kind == "restore-probe" {
		err = m.Probe(id, probe)
	} else {
		err = m.Check(id, readData)
	}
	if err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			_ = m.Stop(id)
			return ctx.Err()
		case <-time.After(time.Second):
		}
		found := false
		for _, view := range m.CachedViews() {
			if view.Job.ID != id {
				continue
			}
			found = true
			if view.State.Running {
				break
			}
			if kind == "restore-probe" {
				if view.State.Probe == nil {
					return errors.New("restore probe has no recorded result")
				}
				if view.State.Probe.Error != "" {
					return errors.New(view.State.Probe.Error)
				}
			} else {
				if view.State.LastCheck == nil {
					return errors.New("repository check has no recorded result")
				}
				if view.State.CheckError != "" {
					return errors.New(view.State.CheckError)
				}
			}
			return nil
		}
		if !found {
			return ErrNotFound
		}
	}
}
