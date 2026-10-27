package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
	"golang.org/x/sys/unix"
)

type SnapshotEntry struct {
	Path      string    `json:"path"`
	Directory bool      `json:"directory"`
	Bytes     int64     `json:"bytes"`
	Modified  time.Time `json:"modified"`
}
type SnapshotListing struct {
	Entries []SnapshotEntry `json:"entries"`
	Next    int             `json:"next"`
}
type SnapshotRestore struct {
	UUID         string   `json:"uuid,omitempty"`
	Paths        []string `json:"paths"`
	TargetVolume string   `json:"targetVolume"`
}
type SnapshotRestored struct {
	Folder string `json:"folder"`
	Files  int    `json:"files"`
	Bytes  int64  `json:"bytes"`
}

func safeSnapshotPath(value string) bool {
	if value == "" || value == "." {
		return true
	}
	if path.Clean(value) != value || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == ".." || part == ".titanos" || part == ".recycle" || strings.Contains(part, ".titanos-") {
			return false
		}
	}
	return true
}
func noSnapshotLinks(root *os.Root, value string) error {
	if !safeSnapshotPath(value) {
		return ErrNotPossible
	}
	current := ""
	for _, part := range strings.Split(value, "/") {
		if part == "" || part == "." {
			continue
		}
		current = path.Join(current, part)
		st, err := root.Lstat(current)
		if err != nil {
			return err
		}
		if st.Mode()&os.ModeSymlink != 0 {
			return ErrNotPossible
		}
	}
	return nil
}
func (m *Manager) openSnapshot(volume, name string) (*os.Root, error) {
	if m.ReadOnly {
		return nil, ErrReadOnly
	}
	if !snapshotName.MatchString(name) {
		return nil, ErrInvalidName
	}
	dir, err := m.snapshotDir(volume)
	if err != nil {
		return nil, err
	}
	parent, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	if err := noSnapshotLinks(parent, name); err != nil {
		return nil, err
	}
	return parent.OpenRoot(name)
}
func (m *Manager) SnapshotFiles(ctx context.Context, volume, name, folder string, offset int) (SnapshotListing, error) {
	m.actions.Lock()
	defer m.actions.Unlock()
	out := SnapshotListing{Entries: []SnapshotEntry{}}
	if offset < 0 || offset > 50000 {
		return out, ErrNotPossible
	}
	root, err := m.openSnapshot(volume, name)
	if err != nil {
		return out, err
	}
	defer root.Close()
	if err := noSnapshotLinks(root, folder); err != nil {
		return out, err
	}
	if folder == "" {
		folder = "."
	}
	dir, err := root.Open(folder)
	if err != nil {
		return out, err
	}
	defer dir.Close()
	entries, err := dir.ReadDir(50001)
	if err != nil && !errors.Is(err, io.EOF) {
		return out, err
	}
	if len(entries) > 50000 {
		return out, errors.New("folder exceeds 50000 entries")
	}
	entries = slices.DeleteFunc(entries, func(e os.DirEntry) bool {
		return !safeSnapshotPath(path.Join(folder, e.Name())) || e.Type()&os.ModeSymlink != 0
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	end := min(offset+200, len(entries))
	if offset > len(entries) {
		return out, ErrNotPossible
	}
	for _, e := range entries[offset:end] {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		st, err := e.Info()
		if err != nil {
			return out, err
		}
		if !st.Mode().IsRegular() && !st.IsDir() {
			continue
		}
		out.Entries = append(out.Entries, SnapshotEntry{path.Join(folder, e.Name()), st.IsDir(), st.Size(), st.ModTime()})
	}
	if end < len(entries) {
		out.Next = end
	}
	return out, nil
}
func (m *Manager) RestoreSnapshotFiles(ctx context.Context, volume, name string, req SnapshotRestore) (out SnapshotRestored, result error) {
	m.actions.Lock()
	defer m.actions.Unlock()
	if len(req.Paths) == 0 || len(req.Paths) > 200 {
		return out, ErrNotPossible
	}
	if req.UUID != "" {
		identity, err := m.snapshotIdentity(ctx, volume, name)
		if err != nil {
			return out, err
		}
		if identity != req.UUID {
			return out, errors.New("snapshot identity changed; preview again")
		}
	}
	source, err := m.openSnapshot(volume, name)
	if err != nil {
		return out, err
	}
	defer source.Close()
	volumeRoot, err := os.OpenRoot(m.MountPoint(req.TargetVolume))
	if err != nil {
		return out, err
	}
	defer volumeRoot.Close()
	target, err := volumedata.Open(m.mountRoot, req.TargetVolume, "restores", m.Mounted())
	if err != nil {
		return out, err
	}
	defer target.Close()
	if err := volumedata.SameMount(volumeRoot, target); err != nil {
		return out, err
	}
	var id [12]byte
	if _, err := rand.Read(id[:]); err != nil {
		return out, err
	}
	token := hex.EncodeToString(id[:])
	if err := target.Mkdir(token, 0700); err != nil {
		return out, err
	}
	defer func() {
		if result != nil {
			target.RemoveAll(token)
		}
	}()
	stage, err := target.OpenRoot(token)
	if err != nil {
		return out, err
	}
	defer stage.Close()
	for _, value := range req.Paths {
		if value == "" || value == "." {
			return out, ErrNotPossible
		}
		if err := noSnapshotLinks(source, value); err != nil {
			return out, err
		}
		before, err := source.Stat(value)
		if err != nil {
			return out, err
		}
		if !before.Mode().IsRegular() {
			return out, errors.New("select regular files; browse folders first")
		}
		if before.Size() > 10<<30-out.Bytes {
			return out, errors.New("restore limit is 10 GiB per operation")
		}
		if err := stage.MkdirAll(path.Dir(value), 0700); err != nil {
			return out, err
		}
		input, err := source.Open(value)
		if err != nil {
			return out, err
		}
		output, err := stage.OpenFile(value, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			input.Close()
			return out, err
		}
		buf := make([]byte, 256<<10)
		var copied int64
		for {
			if ctx.Err() != nil {
				err = ctx.Err()
				break
			}
			n, e := input.Read(buf)
			if n > 0 {
				var w int
				w, err = output.Write(buf[:n])
				copied += int64(w)
				if err != nil {
					break
				}
			}
			if e != nil {
				if e != io.EOF {
					err = e
				}
				break
			}
			if copied > before.Size() {
				err = errors.New("snapshot file changed")
				break
			}
		}
		after, e := input.Stat()
		input.Close()
		if err == nil {
			err = e
		}
		if err == nil && (copied != before.Size() || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime())) {
			err = errors.New("snapshot file changed")
		}
		if err == nil {
			err = output.Sync()
		}
		e = output.Close()
		if err == nil {
			err = e
		}
		if err != nil {
			return out, err
		}
		out.Files++
		out.Bytes += copied
	}
	// Only a complete selection becomes visible. The destination name is unpredictable and never reused.
	if !slices.Contains(m.Mounted(), req.TargetVolume) {
		return out, volumedata.ErrUnavailable
	}
	if err := volumeRoot.Mkdir("restored", 0700); err != nil && !errors.Is(err, os.ErrExist) {
		return out, err
	}
	st, err := volumeRoot.Lstat("restored")
	if err != nil || !st.IsDir() || st.Mode()&os.ModeSymlink != 0 || st.Mode().Perm()&0022 != 0 || st.Sys().(*syscall.Stat_t).Uid != uint32(os.Geteuid()) {
		return out, ErrNotPossible
	}
	final := "snapshot-" + token
	if _, err := volumeRoot.Lstat("restored/" + final); !os.IsNotExist(err) {
		return out, ErrExists
	}
	fromDir, e := target.Open(".")
	if e != nil {
		return out, e
	}
	defer fromDir.Close()
	toDir, e := volumeRoot.Open("restored")
	if e != nil {
		return out, e
	}
	defer toDir.Close()
	if err := unix.Renameat2(int(fromDir.Fd()), token, int(toDir.Fd()), final, unix.RENAME_NOREPLACE); err != nil {
		return out, err
	}
	if err := toDir.Sync(); err != nil {
		return out, err
	}
	out.Folder = "restored/" + final
	return out, nil
}

// Snapshot inventory version 2 records Btrfs UUIDs. The legacy name-only file
// stays empty, including on downgrade, so it cannot grant ownership of a replacement.
type scheduledSnapshot struct {
	Name string `json:"name"`
	UUID string `json:"uuid"`
}

func (m *Manager) snapshotIdentity(ctx context.Context, volume, name string) (string, error) {
	dir, err := m.snapshotDir(volume)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrNotPossible
	}
	out, err := m.runner.Run(ctx, "", "btrfs", "subvolume", "show", path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
		if ok && key == "UUID" && regexp.MustCompile(`^[0-9a-fA-F]{8}(-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$`).MatchString(strings.TrimSpace(value)) {
			return strings.ToLower(strings.TrimSpace(value)), nil
		}
	}
	return "", errors.New("snapshot UUID is unavailable")
}

// ScheduledSnapshot takes a snapshot for a maintenance job and removes the
// oldest beyond keep. lockDays > 0 forbids deleting it for that long.
func (m *Manager) ScheduledSnapshot(ctx context.Context, volume, id string, keep, lockDays int) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if !regexp.MustCompile(`^[A-Za-z0-9-]{1,32}$`).MatchString(id) || keep < 1 || keep > 100 {
		return ErrNotPossible
	}
	root, err := volumedata.Open(m.mountRoot, volume, "snapplans", m.Mounted())
	if err != nil {
		return err
	}
	defer root.Close()
	copies := []scheduledSnapshot{}
	inventory := id + ".v2.json"
	if err := volumedata.ReadJSON(root, inventory, &copies, 128<<10); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if len(copies) > 1000 {
		return errors.New("snapshot schedule inventory exceeds limit")
	}
	prefix := "auto-" + id + "-"
	for _, copy := range copies {
		if !strings.HasPrefix(copy.Name, prefix) || !snapshotName.MatchString(copy.Name) || copy.UUID == "" {
			return errors.New("invalid scheduled snapshot inventory")
		}
	}
	// Unknown legacy copies are retained; names alone never establish identity.
	if err := volumedata.WriteJSON(root, id+".json", []string{}); err != nil {
		return err
	}
	now := time.Now().UTC()
	name := prefix + now.Format("20060102T150405") + fmt.Sprintf("-%09d", now.Nanosecond())
	if err := m.snapshotAction(ctx, volume, name, "create", ""); err != nil {
		return err
	}
	if lockDays > 0 {
		if err := m.lockSnapshot(volume, name, now.AddDate(0, 0, lockDays)); err != nil {
			return fmt.Errorf("snapshot %s created, its lock could not be saved: %w", name, err)
		}
	}
	uuid, err := m.snapshotIdentity(ctx, volume, name)
	if err != nil {
		return fmt.Errorf("snapshot %s retained without pruning: %w", name, err)
	}
	copies = append(copies, scheduledSnapshot{Name: name, UUID: uuid})
	if err := volumedata.WriteJSON(root, inventory, copies); err != nil {
		return fmt.Errorf("snapshot %s created, inventory could not be saved: %w", name, err)
	}
	for len(copies) > keep {
		old := copies[0]
		// A locked snapshot stays, and with it the ones after it: retention
		// goes on once the lock has ended.
		if !m.lockedUntil(volume, old.Name).IsZero() {
			break
		}
		identity, err := m.snapshotIdentity(ctx, volume, old.Name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && identity == old.UUID {
			if err := m.snapshotAction(ctx, volume, old.Name, "delete", old.Name); err != nil && !errors.Is(err, ErrNotFound) {
				return fmt.Errorf("snapshot created, retention failed: %w", err)
			}
		}
		copies = copies[1:]
		if err := volumedata.WriteJSON(root, inventory, copies); err != nil {
			return err
		}
	}
	return nil
}

type SnapshotRestorePreview struct {
	UUID  string `json:"uuid"`
	Files int    `json:"files"`
	Bytes int64  `json:"bytes"`
}

// PreviewSnapshotRestore validates a bounded regular-file selection without
// creating a directory. Restore rechecks identity and file access before copying.
func (m *Manager) PreviewSnapshotRestore(ctx context.Context, volume, name string, req SnapshotRestore) (SnapshotRestorePreview, error) {
	m.actions.Lock()
	defer m.actions.Unlock()
	out := SnapshotRestorePreview{}
	if len(req.Paths) == 0 || len(req.Paths) > 200 || !slices.Contains(m.Mounted(), req.TargetVolume) {
		return out, ErrNotPossible
	}
	uuid, err := m.snapshotIdentity(ctx, volume, name)
	if err != nil {
		return out, err
	}
	out.UUID = uuid
	root, err := m.openSnapshot(volume, name)
	if err != nil {
		return out, err
	}
	defer root.Close()
	seen := map[string]bool{}
	for _, p := range req.Paths {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		if seen[p] {
			return out, ErrNotPossible
		}
		seen[p] = true
		if err := noSnapshotLinks(root, p); err != nil {
			return out, err
		}
		st, err := root.Stat(p)
		if err != nil {
			return out, err
		}
		if !st.Mode().IsRegular() {
			return out, ErrNotPossible
		}
		out.Files++
		out.Bytes += st.Size()
		if out.Bytes > 10<<30 {
			return out, ErrNotPossible
		}
	}
	var target syscall.Statfs_t
	if err := syscall.Statfs(m.MountPoint(req.TargetVolume), &target); err != nil {
		return out, err
	}
	if uint64(out.Bytes) > target.Bavail*uint64(target.Bsize) {
		return out, errors.New("insufficient restore space")
	}
	return out, nil
}
