package vm

// The settings of a VM live in /config/vms/<name>.json on the stick; that
// file is what counts. Every time titand writes it, it also writes a copy,
// vm.json, into the folder of the VM on its volume. With the copy a VM can
// be taken over again ("VM übernehmen", adopt.go) when the stick was lost
// without a backup, or when the volume moved to another TitanOS.
//
// The copy is only written to a volume that is mounted, through the pinned
// root of that volume, never to the empty folder in the RAM below /mnt.
// Older titand versions do not know the file and leave it alone.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const (
	// volumeCopyName is the copy in the folder of a VM.
	volumeCopyName = "vm.json"
	// volumeCopyFormat is the version of what the copy looks like.
	volumeCopyFormat = 1
	// volumeCopyLimit is the most a copy is read of.
	volumeCopyLimit = 1 << 20
)

// VolumeCopy is the copy of the settings of a VM in its folder.
type VolumeCopy struct {
	Format int    `json:"format"`
	Note   string `json:"note"`
	// Removed is set when the VM was removed and its disks were kept. Such
	// a VM can be taken over again.
	Removed   bool       `json:"removed,omitempty"`
	RemovedAt *time.Time `json:"removedAt,omitempty"`
	// DiskPaths are where the disks of the VM are, also those on other
	// volumes or whole disks. Only for people reading the file; taking a
	// VM over goes by VM.Disks.
	DiskPaths []string `json:"diskPaths"`
	VM        VM       `json:"vm"`
}

const volumeCopyNote = "Copy of /config/vms/<name>.json written by TitanOS. The stick is the source of truth; this copy is used to take the VM over again."

// volumeCopy returns the contents of the copy for a VM.
func (m *Manager) volumeCopy(v VM, removedAt *time.Time) ([]byte, error) {
	c := VolumeCopy{Format: volumeCopyFormat, Note: volumeCopyNote, Removed: removedAt != nil, RemovedAt: removedAt, DiskPaths: []string{}, VM: storedVM(v)}
	for _, disk := range v.Disks {
		c.DiskPaths = append(c.DiskPaths, m.diskPath(v, disk))
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// openVMFolder pins the folder of a VM on a mounted volume. Neither vms nor
// the folder may be a link, and the folder must lie on the volume's mount.
func (m *Manager) openVMFolder(volume, folder string, mounted []string) (*os.Root, error) {
	if !validVMDirectory(folder) {
		return nil, ErrBadFolder
	}
	root, err := volumedata.OpenVolume(m.mountRoot, volume, mounted)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, part := range []string{"vms", "vms/" + folder} {
		info, err := root.Lstat(part)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return nil, ErrBadFolder
		}
	}
	dir, err := root.OpenRoot("vms/" + folder)
	if err != nil {
		return nil, err
	}
	if err := volumedata.SameMount(root, dir); err != nil {
		dir.Close()
		return nil, err
	}
	return dir, nil
}

// readCopyFile returns what a copy holds, or fs.ErrNotExist.
func readCopyFile(dir *os.Root) ([]byte, error) {
	info, err := dir.Lstat(volumeCopyName)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > volumeCopyLimit {
		return nil, ErrBadFolder
	}
	file, err := dir.Open(volumeCopyName)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return io.ReadAll(io.LimitReader(file, volumeCopyLimit+1))
}

// writeVolumeCopy writes the copy of a VM if it differs from what is there.
// copyMu is held.
func (m *Manager) writeVolumeCopy(v VM, removedAt *time.Time, mounted []string) error {
	data, err := m.volumeCopy(v, removedAt)
	if err != nil {
		return err
	}
	dir, err := m.openVMFolder(v.Volume, v.folder(), mounted)
	if err != nil {
		return err
	}
	defer dir.Close()
	if old, err := readCopyFile(dir); err == nil && bytes.Equal(old, data) {
		return nil
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return volumedata.Write(dir, volumeCopyName, data)
}

func (m *Manager) mountedNow() []string {
	if m.Mounted == nil {
		return nil
	}
	return m.Mounted()
}

// saveVolumeCopy writes the copy after the settings of a VM were stored.
// Without its volume the copy is written once the volume is there (see
// refreshVolumeCopies).
func (m *Manager) saveVolumeCopy(v VM) {
	mounted := m.mountedNow()
	m.copyMu.Lock()
	defer m.copyMu.Unlock()
	if !slices.Contains(mounted, v.Volume) {
		delete(m.copied, v.Name)
		return
	}
	m.copied[v.Name] = true
	if err := m.writeVolumeCopy(v, nil, mounted); err != nil {
		slog.Warn("copy of the VM settings not written to the volume", "vm", v.Name, "volume", v.Volume, "err", err)
	}
}

// refreshVolumeCopies writes missing or outdated copies, once per VM and
// start of titand, as soon as the volume of the VM is mounted. It reads
// nothing from /config: the settings are in RAM.
func (m *Manager) refreshVolumeCopies(mounted []string) {
	m.mu.Lock()
	var due []string
	for name, v := range m.vms {
		if slices.Contains(mounted, v.Volume) {
			due = append(due, name)
		}
	}
	m.mu.Unlock()
	if len(due) == 0 {
		return
	}
	m.copyMu.Lock()
	defer m.copyMu.Unlock()
	for _, name := range due {
		if m.copied[name] {
			continue
		}
		// Taken again under copyMu: a change saved meanwhile wrote its own copy.
		v, err := m.get(name)
		if err != nil {
			continue
		}
		m.copied[name] = true
		if err := m.writeVolumeCopy(v, nil, mounted); err != nil {
			slog.Warn("copy of the VM settings not refreshed on the volume", "vm", name, "volume", v.Volume, "err", err)
		}
	}
}

// markVolumeCopyRemoved notes in the copy that the VM was removed while its
// disks were kept.
func (m *Manager) markVolumeCopyRemoved(v VM, at time.Time) {
	mounted := m.mountedNow()
	m.copyMu.Lock()
	defer m.copyMu.Unlock()
	delete(m.copied, v.Name)
	if !slices.Contains(mounted, v.Volume) {
		return
	}
	if err := m.writeVolumeCopy(v, &at, mounted); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("copy of the VM settings not marked as removed", "vm", v.Name, "err", err)
	}
}

// removeVolumeCopy deletes the copy when the disks of a VM are deleted.
func (m *Manager) removeVolumeCopy(v VM) {
	mounted := m.mountedNow()
	m.copyMu.Lock()
	defer m.copyMu.Unlock()
	delete(m.copied, v.Name)
	if !slices.Contains(mounted, v.Volume) {
		return
	}
	dir, err := m.openVMFolder(v.Volume, v.folder(), mounted)
	if err != nil {
		return
	}
	defer dir.Close()
	if info, err := dir.Lstat(volumeCopyName); err == nil && info.Mode().IsRegular() {
		if err := dir.Remove(volumeCopyName); err != nil {
			slog.Warn("copy of the VM settings not deleted", "vm", v.Name, "err", err)
		}
	}
}

// copyPath is where the copy of a VM lies; for tests and messages.
func (m *Manager) copyPath(v VM) string {
	return filepath.Join(m.dir(v.Volume, v.folder()), volumeCopyName)
}

// UseMountRoot makes the manager find the volumes below root; for tests.
func (m *Manager) UseMountRoot(root string) { m.mountRoot = root }
