package nvr

import (
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"slices"
	"strings"
)

// The faces the NVR knows live on its volume (Frigate keeps them below
// its clips folder), but they are configuration: the owner taught them
// one by one. They go into the backup of /config as extra files, and a
// restore puts them back.
//
//	backup:  <volume>/.titanos/nvr/media/clips/faces/<person>/<picture>
//	         → volume-data/nvr/faces/<person>/<picture> in the archive
//	restore: the archive's files land in /config under the same names;
//	         the NVR copies them to its volume the next time it runs
//	         (only pictures that are not there) and removes them from
//	         the stick.
//
// The known plates are part of nvr/settings.json and need none of this.

const (
	// backupPrefix is where the faces are in a backup and, after a
	// restore, on the stick.
	backupPrefix = "volume-data/nvr/faces"
	// facesFolder is Frigate's library on the volume, relative to
	// .titanos/nvr; "train" in it are unassigned faces and stay out.
	facesFolder = "media/clips/faces"
	// The library that is saved is bounded: more is left out as a whole
	// (the log says so), so that it never breaks the backup of /config.
	maxBackupFaces     = 2000
	maxBackupFaceBytes = 12 << 20
	maxBackupFaceFile  = 2 << 20
)

// BackupFile is one file for the backup of the configuration.
type BackupFile struct {
	Name string
	Data []byte
}

// loadRestored reads faces a restore left on the stick (once, at the
// start: Load).
func (m *Manager) loadRestored() []BackupFile {
	persons, err := m.store.List(backupPrefix)
	if err != nil || len(persons) == 0 {
		return nil
	}
	var out []BackupFile
	var total int
	for _, person := range persons {
		if !ValidFaceName(person) {
			continue
		}
		files, err := m.store.List(backupPrefix + "/" + person)
		if err != nil {
			continue
		}
		for _, file := range files {
			if !ValidFaceFile(file) {
				continue
			}
			data, err := m.store.Read(backupPrefix + "/" + person + "/" + file)
			if err != nil || len(data) > maxBackupFaceFile {
				continue
			}
			if total += len(data); total > maxBackupFaceBytes || len(out) >= maxBackupFaces {
				return out
			}
			out = append(out, BackupFile{Name: backupPrefix + "/" + person + "/" + file, Data: data})
		}
	}
	return out
}

// importRestored copies the faces a restore left on the stick into the
// library on the volume and removes them from the stick. Pictures that
// are there already stay as they are. Frigate reads the library when it
// starts, so this runs before the container is started.
func (m *Manager) importRestored(volume string) {
	m.mu.Lock()
	pending := m.restored
	m.mu.Unlock()
	if len(pending) == 0 {
		return
	}
	root, _, err := m.privateRoot(volume)
	if err != nil {
		return // the next look tries again
	}
	defer root.Close()
	copied := 0
	for _, f := range pending {
		rel := facesFolder + strings.TrimPrefix(f.Name, backupPrefix)
		if err := root.MkdirAll(path.Dir(rel), 0o755); err != nil {
			slog.Warn("NVR: restored faces not put back", "err", err)
			return
		}
		file, err := root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			slog.Warn("NVR: restored face not put back", "err", err)
			return
		}
		_, err = file.Write(f.Data)
		if cerr := file.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = root.Remove(rel) // half a picture is none
			slog.Warn("NVR: restored face not put back", "err", err)
			return
		}
		copied++
	}
	if err := m.store.RemoveAll("volume-data/nvr"); err != nil {
		slog.Warn("NVR: restored faces not removed from the stick", "err", err)
		return
	}
	m.mu.Lock()
	m.restored = nil
	m.mu.Unlock()
	slog.Info("NVR: faces of the restored configuration put back", "pictures", copied, "ofBackup", len(pending))
}

// BackupFiles returns the faces for the backup of the configuration.
// Faces of a restore that are not on the volume yet are returned as they
// wait; else the library of the volume is read. Sleeping disks are never
// woken for it and a volume that is not mounted is never read: then the
// library as last read is returned (RAM), nothing if it was never read
// since titand started.
func (m *Manager) BackupFiles() []BackupFile {
	m.mu.Lock()
	pending, cached, volume := m.restored, m.backupFaces, m.settings.Volume
	m.mu.Unlock()
	if len(pending) > 0 {
		return pending
	}
	if volume == "" || !slices.Contains(m.mounted(), volume) || (m.Asleep != nil && m.Asleep(volume)) {
		if cached == nil {
			slog.Info("NVR: faces not read for the backup of the configuration: the volume is not there or its disks sleep")
		}
		return cached
	}
	root, _, err := m.privateRoot(volume)
	if err != nil {
		return cached
	}
	defer root.Close()
	files, ok := readFaces(root)
	if !ok {
		return cached
	}
	m.mu.Lock()
	m.backupFaces = files
	m.mu.Unlock()
	return files
}

// readFaces reads the library below root; ok is false if it is larger
// than a backup takes.
func readFaces(root *os.Root) (files []BackupFile, ok bool) {
	files = []BackupFile{}
	persons, err := fs.ReadDir(root.FS(), facesFolder)
	if err != nil {
		return files, true // no library yet
	}
	total := 0
	for _, person := range persons {
		if !person.IsDir() || !ValidFaceName(person.Name()) {
			continue
		}
		pictures, err := fs.ReadDir(root.FS(), facesFolder+"/"+person.Name())
		if err != nil {
			continue
		}
		for _, picture := range pictures {
			info, err := picture.Info()
			if err != nil || !info.Mode().IsRegular() || !ValidFaceFile(picture.Name()) || info.Size() > maxBackupFaceFile {
				continue
			}
			file, err := root.Open(facesFolder + "/" + person.Name() + "/" + picture.Name())
			if err != nil {
				continue
			}
			data, err := io.ReadAll(io.LimitReader(file, maxBackupFaceFile+1))
			file.Close()
			if err != nil {
				continue
			}
			if total += len(data); total > maxBackupFaceBytes || len(files) >= maxBackupFaces {
				slog.Warn("NVR: the faces are too many for the backup of the configuration and are left out", "limitMiB", maxBackupFaceBytes>>20, "limitPictures", maxBackupFaces)
				return nil, false
			}
			files = append(files, BackupFile{Name: backupPrefix + "/" + person.Name() + "/" + picture.Name(), Data: data})
		}
	}
	slices.SortFunc(files, func(a, b BackupFile) int { return strings.Compare(a.Name, b.Name) })
	return files, true
}
