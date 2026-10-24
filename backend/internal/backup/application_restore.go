package backup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"golang.org/x/sys/unix"
)

type ApplicationRestoreResult struct {
	At       time.Time `json:"at"`
	Snapshot string    `json:"snapshot"`
	Target   string    `json:"target"`
	Verified bool      `json:"verified"`
	Error    string    `json:"error"`
}

type CapturedApplication struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Identity string `json:"identity"`
	Running  bool   `json:"running"`
	Method   string `json:"method"`
}
type ApplicationRestorePlan struct {
	CapturedApplications []CapturedApplication `json:"capturedApplications,omitempty"`
	Snapshot             Snapshot              `json:"snapshot"`
	Applications         []Application         `json:"applications"`
	TargetVolume         string                `json:"targetVolume"`
	FreeBytes            uint64                `json:"freeBytes"`
	CurrentSelection     bool                  `json:"currentSelection"`
}

func (m *Manager) PreviewApplicationRestore(ctx context.Context, id, snapshot, volume string) (ApplicationRestorePlan, error) {
	plan := ApplicationRestorePlan{}
	job, ok := m.find(id)
	if !ok {
		return plan, ErrNotFound
	}
	if m.Disabled {
		return plan, ErrDisabled
	}
	if len(job.Applications) == 0 || plainCopy(job.Destination.Type) || !snapshotForm.MatchString(snapshot) || !slices.Contains(m.mounted(), volume) {
		return plan, invalid("application backup, snapshot and mounted target volume required")
	}
	snapshots, err := m.Snapshots(ctx, id)
	if err != nil {
		return plan, err
	}
	matches := 0
	for _, saved := range snapshots {
		if saved.ID == snapshot {
			plan.Snapshot = saved
			matches++
		}
	}
	if matches != 1 {
		return plan, ErrNotFound
	}
	var fs syscall.Statfs_t
	if err := syscall.Statfs(filepath.Join(m.mountRoot, volume), &fs); err != nil {
		return plan, err
	}
	plan.FreeBytes = fs.Bavail * uint64(fs.Bsize)
	if plan.Snapshot.Bytes > plan.FreeBytes {
		return plan, invalid("insufficient target space")
	}
	plan.TargetVolume = volume
	plan.Applications = slices.Clone(job.Applications)
	plan.CurrentSelection = true
	for _, p := range plan.Snapshot.Paths {
		if !regexp.MustCompile(`^/[^\x00\r\n]+/\.titanos/appbackups/[a-f0-9]{24}/operation\.json$`).MatchString(p) {
			continue
		}
		_, base, env, cleanup, err := m.repository(job)
		if err != nil {
			break
		}
		readCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		var text strings.Builder
		overflow := false
		_, err = m.run(readCtx, env, append(slices.Clone(base), "dump", snapshot, p), func(line string) {
			if text.Len()+len(line) > 1<<20 {
				overflow = true
				cancel()
				return
			}
			text.WriteString(line)
			text.WriteByte('\n')
		})
		cancel()
		cleanup()
		var captured struct {
			ID, Job string
			Members []CapturedApplication
		}
		if err == nil && !overflow && json.Unmarshal([]byte(text.String()), &captured) == nil && captured.Job == job.ID && len(captured.Members) <= 50 && strings.Contains(p, "/"+captured.ID+"/operation.json") {
			plan.CapturedApplications = captured.Members
			plan.CurrentSelection = false
		}
		break
	}
	return plan, nil
}

// RestoreApplication creates an independently verified review copy. It never
// registers, starts or replaces an application or imports live NAS configuration.
func (m *Manager) RestoreApplication(ctx context.Context, id, snapshot, volume string) (string, error) {
	plan, err := m.PreviewApplicationRestore(ctx, id, snapshot, volume)
	if err != nil {
		return "", err
	}
	job, _ := m.find(id)
	return m.restoreVerifiedCopy(job, plan.Snapshot.ID, volume, nil, true)
}

// restoreVerifiedCopy is shared by whole, selected-file and application restores:
// a pinned private staging directory, verification, and no-replace publication.
func (m *Manager) restoreVerifiedCopy(job Job, snapshot, volume string, includes []string, application bool) (string, error) {
	var random [12]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	token := hex.EncodeToString(random[:])
	name := job.ID + "-" + m.now().Format("20060102-150405") + "-" + token
	if application {
		name = "application-" + job.ID + "-" + token
	}
	target := filepath.Join(m.mountRoot, volume, "restored", name)
	phase := "restore"
	if application {
		phase = "application-restore"
	}
	err := m.begin(job, phase, func(work context.Context) (result error) {
		report := ApplicationRestoreResult{Snapshot: snapshot, Target: target}
		defer func() {
			if !application {
				return
			}
			report.At = m.now().UTC()
			if result != nil {
				report.Error = result.Error()
			}
			m.update(job, func(s *State) { s.ApplicationRestore = &report })
		}()
		_, base, env, cleanup, err := m.repository(job)
		if err != nil {
			return err
		}
		defer cleanup()
		root, err := volumedata.Open(m.mountRoot, volume, "restores", m.mounted())
		if err != nil {
			return err
		}
		defer root.Close()
		if err := root.Mkdir(token, 0700); err != nil {
			return err
		}
		defer root.RemoveAll(token)
		stage, err := root.OpenRoot(token)
		if err != nil {
			return err
		}
		defer stage.Close()
		fd, err := stage.Open(".")
		if err != nil {
			return err
		}
		defer fd.Close()
		args := append(slices.Clone(base), "restore", snapshot, "--target", "/proc/self/fd/3/tree", "--verify", "--overwrite", "never", "--json")
		args = append(args, includes...)
		output, err := m.runFiles(work, env, args, func(line string) {
			var progress struct {
				Type    string  `json:"message_type"`
				Percent float64 `json:"percent_done"`
				Done    uint64  `json:"bytes_restored"`
				Total   uint64  `json:"total_bytes"`
			}
			if json.Unmarshal([]byte(line), &progress) == nil && progress.Type == "status" {
				m.update(job, func(s *State) {
					s.Percent = progress.Percent * 100
					s.BytesDone = progress.Done
					s.BytesTotal = progress.Total
				})
			}
		}, []*os.File{fd})
		if work.Err() != nil {
			return work.Err()
		}
		if err != nil {
			return errors.New(lastLines(output, job.Password))
		}
		if !slices.Contains(m.mounted(), volume) {
			return volumedata.ErrUnavailable
		}
		staged, err := stage.Lstat("tree")
		if err != nil {
			return err
		}
		if !staged.IsDir() || staged.Mode()&os.ModeSymlink != 0 {
			return invalid("invalid restore staging tree")
		}
		tree, err := stage.OpenRoot("tree")
		if err != nil {
			return err
		}
		if err := tree.Chown(".", os.Geteuid(), os.Getegid()); err != nil {
			tree.Close()
			return err
		}
		if err := tree.Chmod(".", 0700); err != nil {
			tree.Close()
			return err
		}
		tree.Close()
		public, err := os.OpenRoot(filepath.Join(m.mountRoot, volume))
		if err != nil {
			return err
		}
		defer public.Close()
		if err := volumedata.SameMount(public, root); err != nil {
			return err
		}
		if err := public.Mkdir("restored", 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
		st, err := public.Lstat("restored")
		if err != nil {
			return err
		}
		stat, ok := st.Sys().(*syscall.Stat_t)
		if !ok || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 || stat.Uid != uint32(os.Geteuid()) {
			return invalid("unsafe restore destination")
		}
		destination, err := public.Open("restored")
		if err != nil {
			return err
		}
		defer destination.Close()
		if err := unix.Renameat2(int(fd.Fd()), "tree", int(destination.Fd()), name, unix.RENAME_NOREPLACE); err != nil {
			return err
		}
		if err := destination.Sync(); err != nil {
			return err
		}
		report.Verified = true
		m.update(job, func(s *State) { s.RestoredTo = target })
		return nil
	})
	return target, err
}
