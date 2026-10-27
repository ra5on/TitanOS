package files

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// After a restart titand no longer knows unfinished uploads, and their part
// files would stay until someone opens the folder again. StartUpload therefore
// leaves a note per upload in the volume's .titanos/uploads naming the part
// file relative to the volume, and SweepUploads removes parts that nobody has
// written to for uploadIdle. Only these notes are read, never whole shares.
const uploadNotes = ".titanos/uploads"

// volumeDir returns the folder the volume of a root is mounted on, or "" for
// roots that are not on a volume (USB drives, the stick).
func (m *Manager) volumeDir(root Root) string {
	if root.Volume == "" {
		return ""
	}
	dir := filepath.Join(m.mountRoot(), root.Volume)
	if root.Dir != dir && !strings.HasPrefix(root.Dir, dir+string(filepath.Separator)) {
		return ""
	}
	return dir
}

func (m *Manager) mountRoot() string {
	if m.MountRoot != "" {
		return m.MountRoot
	}
	return "/mnt"
}

// noteUpload records a new upload. It is best effort: without the note the
// part file is still removed while titand runs, just not after a restart.
func (m *Manager) noteUpload(root Root, id, part string) {
	dir := m.volumeDir(root)
	if dir == "" {
		return
	}
	rel, err := filepath.Rel(dir, filepath.Join(root.Dir, part))
	if err != nil || !filepath.IsLocal(rel) {
		return
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		return
	}
	defer r.Close()
	if err = r.MkdirAll(uploadNotes, 0o700); err == nil {
		err = r.WriteFile(path.Join(uploadNotes, id), []byte(filepath.ToSlash(rel)), 0o600)
	}
	if err != nil {
		slog.Warn("note upload", "volume", root.Volume, "err", err)
	}
}

// forgetUpload removes the note of an upload that finished or was removed.
func (m *Manager) forgetUpload(root Root, id string) {
	dir := m.volumeDir(root)
	if dir == "" {
		return
	}
	if r, err := os.OpenRoot(dir); err == nil {
		_ = r.Remove(path.Join(uploadNotes, id))
		r.Close()
	}
}

// SweepUploads removes part files of uploads on the mounted volumes that
// titand does not know and that have not changed for uploadIdle.
func (m *Manager) SweepUploads(mounted []string) {
	for _, volume := range mounted {
		if volume == "" || strings.ContainsAny(volume, "/\x00") || volume == "." || volume == ".." {
			continue
		}
		m.sweepVolume(volume, filepath.Join(m.mountRoot(), volume))
	}
}

func (m *Manager) sweepVolume(volume, dir string) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return
	}
	defer r.Close()
	notes, err := r.Open(uploadNotes)
	if err != nil {
		return
	}
	entries, _ := notes.ReadDir(-1)
	notes.Close()
	removed := 0
	for _, entry := range entries {
		id := entry.Name()
		if _, err := hex.DecodeString(id); err != nil || len(id) != 24 {
			continue
		}
		m.mu.Lock()
		_, active := m.uploads[id]
		m.mu.Unlock()
		if active || m.downloadKnown(id) {
			continue
		}
		note := path.Join(uploadNotes, id)
		part, ok := readNote(r, note, id)
		if !ok {
			continue
		}
		info, err := r.Lstat(part)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			_ = r.Remove(part + ".json")
			_ = r.Remove(note)
		case err != nil || !trustedPart(info):
			// Not ours any more (replaced over SMB, say): leave it alone.
			_ = r.Remove(note)
		case m.now().Sub(info.ModTime()) > uploadIdle:
			if r.Remove(part) == nil {
				removed++
			}
			_ = r.Remove(part + ".json")
			_ = r.Remove(note)
		}
	}
	if removed > 0 {
		slog.Info("unfinished uploads removed", "volume", volume, "files", removed)
	}
}

// readNote returns the part file a note names, relative to the volume. It must
// be a part file of this very upload.
func readNote(r *os.Root, note, id string) (string, bool) {
	f, err := r.OpenFile(note, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", false
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !trustedPart(info) {
		return "", false
	}
	b, err := io.ReadAll(io.LimitReader(f, 4096))
	part := string(b)
	if err != nil || !fs.ValidPath(part) || !strings.HasPrefix(path.Base(part), ".") || !strings.HasSuffix(part, "."+id+partSuffix) && !strings.HasSuffix(part, "-"+id+partSuffix) {
		return "", false
	}
	return part, true
}

// RunUploadSweep sweeps the mounted volumes every hour, starting a few
// minutes after titand starts, when the volumes are mounted.
func (m *Manager) RunUploadSweep(ctx context.Context, mounted func() []string) {
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			m.SweepUploads(mounted())
			timer.Reset(time.Hour)
		}
	}
}
