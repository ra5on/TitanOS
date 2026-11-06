package api

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/shares"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// The volume of the KI-Agent (PLAN 58). Everything the agent keeps lives
// on one volume: the private part below .titanos/agent (chats with
// thoughts and steps, memory, approvals, schedules, attachments) and the
// share KI-Agent (chats as Markdown, projects, documents, skills, notes,
// the sandbox's work folder). It is chosen when the agent is switched on
// and can be moved later: titand copies everything, checks the copy,
// switches over and only then removes the old files. Until the switch the
// old volume stays the valid one; while it moves, the agent is paused.

type agentMoveState struct {
	active atomic.Bool
	mu     sync.Mutex
	job    *agentMoveJob
}

type agentMoveJob struct {
	From  string `json:"from"`
	To    string `json:"to"`
	State string `json:"state"` // copying, checking, switching, cleaning, done, failed
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
	Total int64  `json:"total"`
	// Skipped are links and special files, which are not copied.
	Skipped  int       `json:"skipped,omitempty"`
	Error    string    `json:"error,omitempty"`
	Cleanup  string    `json:"cleanup,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
}

const (
	moveCopying   = "copying"
	moveChecking  = "checking"
	moveSwitching = "switching"
	moveCleaning  = "cleaning"
	moveDone      = "done"
	moveFailed    = "failed"
)

var errAgentMoving = errors.New("the agent's data is being moved to another volume")

// agentMoving pauses the agent while its data moves.
func (s *Server) agentMoving() bool { return s.agentMove.active.Load() }

func (s *Server) agentMoveView() *agentMoveJob {
	s.agentMove.mu.Lock()
	defer s.agentMove.mu.Unlock()
	if s.agentMove.job == nil {
		return nil
	}
	j := *s.agentMove.job
	return &j
}

func (s *Server) agentMoveUpdate(f func(*agentMoveJob)) {
	s.agentMove.mu.Lock()
	defer s.agentMove.mu.Unlock()
	f(s.agentMove.job)
}

type agentVolumeView struct {
	Name       string `json:"name"`
	Filesystem string `json:"filesystem,omitempty"`
	SizeBytes  uint64 `json:"sizeBytes"`
	FreeBytes  uint64 `json:"freeBytes"`
	// Kind is ssd, hdd, usb or mixed, from the member disks.
	Kind    string `json:"kind,omitempty"`
	Current bool   `json:"current"`
}

// handleAgentVolumes lists the mounted volumes to choose from, with free
// space and the kind of disks (SSD recommended: the agent writes often and
// would keep hard disks from sleeping).
func (s *Server) handleAgentVolumes(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"volumes": []agentVolumeView{}, "current": s.agentSettings().Volume, "move": s.agentMoveView()}
	if s.opts.Storage == nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	kinds := map[string]string{}
	fsys := map[string]string{}
	if st, err := s.opts.Storage.Status(r.Context()); err == nil {
		disk := map[string]string{}
		for _, d := range st.Disks {
			disk[d.Name] = d.Kind
		}
		for _, v := range st.Volumes {
			fsys[v.Name] = v.Filesystem
			for _, m := range v.Members {
				k := disk[m.Disk]
				if k == "nvme" {
					k = "ssd"
				}
				switch {
				case k == "":
				case kinds[v.Name] == "":
					kinds[v.Name] = k
				case kinds[v.Name] != k:
					kinds[v.Name] = "mixed"
				}
			}
		}
	}
	current := s.agentSettings().Volume
	list := []agentVolumeView{}
	for _, name := range slices.Sorted(slices.Values(s.opts.Storage.Mounted())) {
		v := agentVolumeView{Name: name, Filesystem: fsys[name], Kind: kinds[name], Current: name == current}
		var st syscall.Statfs_t
		if syscall.Statfs(s.opts.Storage.MountPoint(name), &st) == nil {
			v.SizeBytes = st.Blocks * uint64(st.Bsize)
			v.FreeBytes = st.Bavail * uint64(st.Bsize)
		}
		list = append(list, v)
	}
	out["volumes"] = list
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleAgentMoveStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"move": s.agentMoveView()})
}

// handleAgentMove starts moving the agent's data to another volume.
func (s *Server) handleAgentMove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		To string `json:"to"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if s.opts.Storage == nil {
		writeError(w, http.StatusServiceUnavailable, "agent_no_volume", "no data volume")
		return
	}
	mounted := s.opts.Storage.Mounted()
	from, ok := s.agentVolume(mounted)
	if !ok {
		writeError(w, http.StatusConflict, "agent_volume_missing", "the agent's volume is not mounted")
		return
	}
	if req.To == from {
		writeError(w, http.StatusBadRequest, "agent_same_volume", "the agent's data is already on this volume")
		return
	}
	if !slices.Contains(mounted, req.To) {
		writeError(w, http.StatusBadRequest, "agent_volume_unknown", "no such mounted volume")
		return
	}
	s.agentRT.mu.Lock()
	busy := len(s.agentRT.runs) > 0
	s.agentRT.mu.Unlock()
	if busy {
		writeError(w, http.StatusConflict, "agent_busy", "a task of the agent is running; stop it or let it finish first")
		return
	}
	if !s.agentMove.active.CompareAndSwap(false, true) {
		writeError(w, http.StatusConflict, "agent_moving", errAgentMoving.Error())
		return
	}
	plan, err := s.agentMovePlan(from, req.To, mounted)
	if err != nil {
		s.agentMove.active.Store(false)
		var full errMoveSpace
		switch {
		case errors.As(err, &full):
			writeError(w, http.StatusConflict, "agent_volume_full", err.Error())
		case errors.Is(err, errMoveNotEmpty):
			writeError(w, http.StatusConflict, "agent_volume_not_empty", err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "agent_move", err.Error())
		}
		return
	}
	job := &agentMoveJob{From: from, To: req.To, State: moveCopying, Total: plan.total, Started: time.Now()}
	s.agentMove.mu.Lock()
	s.agentMove.job = job
	s.agentMove.mu.Unlock()
	slog.Info("agent data moves", "from", from, "to", req.To, "bytes", plan.total)
	go s.agentMoveRun(plan)
	writeJSON(w, http.StatusAccepted, map[string]any{"move": s.agentMoveView()})
}

// agentMoveParts are the two places of the agent on a volume, relative to
// the volume's root.
func (s *Server) agentMoveParts() []string {
	return []string{filepath.Join(".titanos", "agent"), agentShareName}
}

type agentMovePlan struct {
	from, to string
	mounted  []string
	total    int64
}

type errMoveSpace struct{ need, free uint64 }

func (e errMoveSpace) Error() string {
	return fmt.Sprintf("not enough free space on the target: %d bytes needed, %d free", e.need, e.free)
}

var errMoveNotEmpty = errors.New("the target volume already holds data of the agent")

func (s *Server) mountRoot(volume string) string {
	return filepath.Dir(s.opts.Storage.MountPoint(volume))
}

func (s *Server) agentMovePlan(from, to string, mounted []string) (agentMovePlan, error) {
	plan := agentMovePlan{from: from, to: to, mounted: mounted}
	src, err := volumedata.OpenVolume(s.mountRoot(from), from, mounted)
	if err != nil {
		return plan, err
	}
	defer src.Close()
	dst, err := volumedata.OpenVolume(s.mountRoot(to), to, mounted)
	if err != nil {
		return plan, err
	}
	defer dst.Close()
	for _, part := range s.agentMoveParts() {
		size, _, err := treeSize(src, part)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return plan, err
		}
		plan.total += size
		if _, files, err := treeSize(dst, part); err == nil && files > 0 {
			return plan, fmt.Errorf("%w: %s", errMoveNotEmpty, part)
		}
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(s.opts.Storage.MountPoint(to), &st); err == nil {
		free := st.Bavail * uint64(st.Bsize)
		need := uint64(plan.total) + uint64(plan.total)/10 + 64<<20
		if free < need {
			return plan, errMoveSpace{need: need, free: free}
		}
	}
	return plan, nil
}

// treeSize adds up the regular files below dir (links not followed).
func treeSize(root *os.Root, dir string) (int64, int, error) {
	var size int64
	files := 0
	err := fs.WalkDir(root.FS(), dir, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				size += info.Size()
				files++
			}
		}
		return nil
	})
	return size, files, err
}

func (s *Server) agentMoveRun(plan agentMovePlan) {
	defer s.agentMove.active.Store(false)
	fail := func(err error, dst *os.Root) {
		slog.Warn("agent data not moved; the old volume stays", "from", plan.from, "to", plan.to, "err", err)
		if dst != nil {
			for _, part := range s.agentMoveParts() {
				_ = dst.RemoveAll(part)
			}
		}
		s.agentMoveUpdate(func(j *agentMoveJob) { j.State, j.Error, j.Finished = moveFailed, err.Error(), time.Now() })
	}
	src, err := volumedata.OpenVolume(s.mountRoot(plan.from), plan.from, plan.mounted)
	if err != nil {
		fail(err, nil)
		return
	}
	defer src.Close()
	dst, err := volumedata.OpenVolume(s.mountRoot(plan.to), plan.to, plan.mounted)
	if err != nil {
		fail(err, nil)
		return
	}
	defer dst.Close()
	if err := dst.MkdirAll(".titanos", 0o700); err != nil {
		fail(err, dst)
		return
	}
	for _, part := range s.agentMoveParts() {
		if err := copyTree(src, dst, part, func(files int, n int64, skipped bool) {
			s.agentMoveUpdate(func(j *agentMoveJob) {
				j.Files += files
				j.Bytes += n
				if skipped {
					j.Skipped++
				}
			})
		}); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fail(err, dst)
			return
		}
	}
	s.agentMoveUpdate(func(j *agentMoveJob) { j.State = moveChecking })
	for _, part := range s.agentMoveParts() {
		if err := compareTree(src, dst, part); err != nil && !errors.Is(err, fs.ErrNotExist) {
			fail(fmt.Errorf("the copy differs: %w", err), dst)
			return
		}
	}
	s.agentMoveUpdate(func(j *agentMoveJob) { j.State = moveSwitching })
	if err := s.agentSetVolume(plan.to); err != nil {
		fail(err, dst)
		return
	}
	if err := s.opts.Shares.Relocate(agentShareName, plan.to, s.opts.Storage.Names()); err != nil && !errors.Is(err, shares.ErrNotFound) {
		// The settings point at the new volume already; put them back.
		if back := s.agentSetVolume(plan.from); back != nil {
			slog.Error("agent volume could not be set back", "err", back)
		}
		fail(err, dst)
		return
	}
	s.opts.Shares.Kick()
	s.agentMoveUpdate(func(j *agentMoveJob) { j.State = moveCleaning })
	var problems []string
	for _, part := range s.agentMoveParts() {
		if err := src.RemoveAll(part); err != nil {
			problems = append(problems, err.Error())
		}
	}
	slog.Info("agent data moved", "from", plan.from, "to", plan.to)
	s.agentMoveUpdate(func(j *agentMoveJob) {
		j.State, j.Finished = moveDone, time.Now()
		if len(problems) > 0 {
			j.Cleanup = problems[0]
		}
	})
	go s.agentSyncShare()
}

// agentSetVolume writes the agent's volume to agent.json.
func (s *Server) agentSetVolume(volume string) error {
	s.agentSettings() // loaded
	a := &s.agentConf
	a.mu.Lock()
	defer a.mu.Unlock()
	next := a.settings
	next.Volume = volume
	if err := s.opts.Store.WriteJSON(agentSettingsFile, next); err != nil {
		return err
	}
	a.settings, a.loaded = next, true
	return nil
}

// copyTree copies dir from src to dst: folders and regular files with
// mode, owner and time. Links and special files are left out; a link in
// a folder the account can change over SMB must not lead anywhere.
func copyTree(src, dst *os.Root, dir string, progress func(files int, n int64, skipped bool)) error {
	return fs.WalkDir(src.FS(), dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			if err := dst.Mkdir(path, info.Mode().Perm()); err != nil && !errors.Is(err, fs.ErrExist) {
				return err
			}
			if err := dst.Chmod(path, info.Mode().Perm()); err != nil {
				return err
			}
			owner(dst, path, info)
			return nil
		case !d.Type().IsRegular():
			progress(0, 0, true)
			return nil
		}
		in, err := src.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := dst.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if err != nil {
			return err
		}
		n, err := io.Copy(out, in)
		if err == nil {
			err = out.Sync()
		}
		if cerr := out.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		owner(dst, path, info)
		_ = dst.Chtimes(path, info.ModTime(), info.ModTime())
		progress(1, n, false)
		return nil
	})
}

func owner(root *os.Root, path string, info fs.FileInfo) {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		_ = root.Lchown(path, int(st.Uid), int(st.Gid))
	}
}

// compareTree checks that every regular file below dir is in dst with
// the same content.
func compareTree(src, dst *os.Root, dir string) error {
	return fs.WalkDir(src.FS(), dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return err
		}
		a, err := fileHash(src, path)
		if err != nil {
			return err
		}
		b, err := fileHash(dst, path)
		if err != nil {
			return err
		}
		if !bytes.Equal(a, b) {
			return fmt.Errorf("%s", path)
		}
		return nil
	})
}

func fileHash(root *os.Root, path string) ([]byte, error) {
	f, err := root.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}
