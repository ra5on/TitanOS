package storage

import (
	"fmt"
	"os"
)

// initializeFolders runs under actions, only after the volume is mounted.
// A persisted pending flag makes interrupted creation resumable without
// recreating folders the user later deliberately deletes.
func (m *Manager) initializeFolders(volume VolumeConfig) error {
	if !volume.InitializeFolders {
		return nil
	}
	if m.ReadOnly {
		return ErrReadOnly
	}
	point := m.MountPoint(volume.Name)
	if !m.readMounts()[point] {
		return fmt.Errorf("%w: volume must be mounted before creating folders", ErrNotPossible)
	}
	info, err := os.Lstat(point)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: invalid volume mount point", ErrNotPossible)
	}
	root, err := os.OpenRoot(point)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, folder := range []struct {
		name string
		mode os.FileMode
	}{{"vms", 0o770}, {"appdata", 0o770}, {"iso", 0o755}} {
		if err := root.Mkdir(folder.name, folder.mode); err != nil && !os.IsExist(err) {
			return fmt.Errorf("volume created; folder %s could not be created: %w", folder.name, err)
		}
		info, err := root.Lstat(folder.name)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%w: folder %s is a file or symlink", ErrNotPossible, folder.name)
		}
	}
	dir, err := root.Open(".")
	if err != nil {
		return err
	}
	err = dir.Sync()
	dir.Close()
	if err != nil {
		return err
	}
	volumes := m.configured()
	for i := range volumes {
		if volumes[i].Name == volume.Name {
			volumes[i].InitializeFolders = false
		}
	}
	return m.saveVolumes(volumes)
}
