package vm

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (vm VM) folder() string {
	if vm.Directory != "" {
		return vm.Directory
	}
	return vm.Name
}
func validVMDirectory(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") || strings.TrimSpace(name) != name || len(name) > 180 || !utf8.ValidString(name) || strings.ContainsAny(name, "/\\") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// A chosen folder is dedicated to one VM. Merely displaying a different name
// never renames disks or firmware state. Direct images remain user-owned.
func (m *Manager) checkDirectory(vm VM) error {
	m.mu.Lock()
	others := make([]VM, 0, len(m.vms))
	for _, other := range m.vms {
		others = append(others, other)
	}
	m.mu.Unlock()
	for _, volume := range vm.volumes() {
		dir := m.dir(volume, vm.folder())
		for _, other := range others {
			for _, v := range other.volumes() {
				if dir == m.dir(v, other.folder()) {
					return fmt.Errorf("%w: folder belongs to %s", ErrBadFolder, other.Label())
				}
			}
			for _, disk := range other.Disks {
				if disk.Path != "" && filepath.Dir(disk.Path) == dir {
					return fmt.Errorf("%w: image in folder belongs to %s", ErrBadFolder, other.Label())
				}
			}
		}
		info, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return ErrBadFolder
		}
		if vm.Directory == "" {
			continue
		} // Existing installations keep their legacy folder behavior.
		allowed := map[string]bool{}
		for _, disk := range vm.Disks {
			if disk.Path != "" && filepath.Dir(disk.Path) == dir {
				allowed[filepath.Base(disk.Path)] = true
			}
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if !allowed[entry.Name()] || entry.Type()&fs.ModeSymlink != 0 {
				return fmt.Errorf("%w: %s contains files not selected for this VM (%s)", ErrBadFolder, dir, entry.Name())
			}
		}
	}
	return nil
}
