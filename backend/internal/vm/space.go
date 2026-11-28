package vm

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// SpaceUsed is what the VMs take on the volumes (§121, the storage ring of
// the phone's start page): the blocks actually allocated by their folders –
// disks, firmware settings, snapshots – and by images outside them that a
// VM uses as they are. Whole disks (/dev/…) are not on a volume and do not
// count. Only lstat and the folders of the VMs are read; nothing is woken
// that the VMs do not use anyway.
func (m *Manager) SpaceUsed() int64 {
	m.mu.Lock()
	machines := make([]VM, 0, len(m.vms))
	for _, v := range m.vms {
		machines = append(machines, v)
	}
	m.mu.Unlock()

	dirs := map[string]bool{}
	files := map[string]bool{}
	for _, v := range machines {
		if v.Volume != "" {
			dirs[m.dir(v.Volume, v.folder())] = true
		}
		for _, d := range v.Disks {
			if d.Path == "" {
				if d.Volume != "" {
					dirs[m.dir(d.Volume, v.folder())] = true
				}
			} else if !strings.HasPrefix(d.Path, "/dev/") {
				files[filepath.Clean(d.Path)] = true
			}
		}
	}
	var total int64
	inside := func(path string) bool {
		for dir := range dirs {
			if strings.HasPrefix(path, dir+string(filepath.Separator)) {
				return true
			}
		}
		return false
	}
	for dir := range dirs {
		_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil // a folder that is gone or not readable counts nothing
			}
			if info, e := d.Info(); e == nil && (info.Mode().IsRegular() || info.IsDir()) {
				total += allocatedBytes(info)
			}
			return nil
		})
	}
	for file := range files {
		if inside(file) {
			continue
		}
		if info, err := os.Lstat(file); err == nil && info.Mode().IsRegular() {
			total += allocatedBytes(info)
		}
	}
	return total
}

func allocatedBytes(info fs.FileInfo) int64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return st.Blocks * 512
	}
	return info.Size()
}
