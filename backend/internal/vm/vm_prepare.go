package vm

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// checkISO makes sure an installation medium is a file on a volume and
// returns its real path.
func (m *Manager) checkISO(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrBadISO
	}
	// Links must not lead out of the volumes.
	real, err := filepath.EvalSymlinks(path)
	if err != nil || !strings.HasPrefix(real, m.mountRoot+"/") {
		return "", ErrBadISO
	}
	if info, err := os.Stat(real); err != nil || !info.Mode().IsRegular() {
		return "", ErrBadISO
	}
	return real, nil
}

// prepare checks the settings of a VM and fills in what was left open.
func (m *Manager) prepare(ctx context.Context, vm *VM, mounted []string) error {
	if !nameRE.MatchString(vm.Name) {
		return ErrInvalidName
	}
	if vm.DisplayName != "" {
		vm.DisplayName = strings.TrimSpace(vm.DisplayName)
		if !validDisplayName(vm.DisplayName) {
			return ErrInvalidDisplayName
		}
	}
	m.mu.Lock()
	duplicateLabel := false
	for _, other := range m.vms {
		if other.Name != vm.Name && strings.EqualFold(other.Label(), vm.Label()) {
			duplicateLabel = true
			break
		}
	}
	m.mu.Unlock()
	if duplicateLabel {
		return ErrExists
	}
	if !validVMDirectory(vm.folder()) {
		return fmt.Errorf("%w: invalid VM folder", ErrBadFolder)
	}
	if !slices.Contains([]string{"linux", "windows", "other"}, vm.Template) {
		return fmt.Errorf("%w: unknown template", ErrInvalid)
	}
	if err := checkPowerSchedule(&vm.PowerSchedule); err != nil {
		return err
	}
	if err := validateFirmware(*vm); err != nil {
		return err
	}
	if len(vm.CPUPin) > 0 {
		// One vCPU per pinned CPU.
		vm.VCPUs = len(vm.CPUPin)
	}
	if vm.MemoryMiB < 256 || vm.MemoryMiB > 4<<20 || vm.VCPUs < 1 || vm.VCPUs > runtime.NumCPU() {
		return fmt.Errorf("%w: memory or number of vCPUs out of range", ErrInvalid)
	}
	if len(vm.Description) > 200 || strings.ContainsAny(vm.Description, "\n\r") {
		return fmt.Errorf("%w: description too long", ErrInvalid)
	}
	if len(vm.Disks) > maxDisks || (len(vm.Disks) == 0 && vm.ISO == "") {
		return fmt.Errorf("%w: a VM needs a disk or an installation medium", ErrInvalid)
	}
	iso, err := m.checkISO(vm.ISO)
	if err != nil {
		return err
	}
	vm.ISO = iso
	drivers, err := m.checkISO(vm.VirtIOISO)
	if err != nil {
		return err
	}
	vm.VirtIOISO = drivers
	if len(vm.CPUPin) > 0 {
		seen := map[int]bool{}
		for _, cpu := range vm.CPUPin {
			if cpu < 0 || cpu >= runtime.NumCPU() || seen[cpu] {
				return fmt.Errorf("%w: unknown or repeated CPU", ErrInvalid)
			}
			seen[cpu] = true
		}
	} else {
		vm.Exclusive = false
	}
	vm.TopologyAutostart = nil
	if strings.HasPrefix(vm.Network, "network:") {
		if _, ok := m.namedBridge(vm.Network); !ok {
			return ErrNoBridge
		}
	} else if vm.Network != "bridge" {
		vm.Network = "user"
	}
	if err := m.checkFolders(vm.Folders); err != nil {
		return err
	}
	for _, address := range vm.PCI {
		if !pciRE.MatchString(address) {
			return fmt.Errorf("%w: not a PCI address", ErrInvalid)
		}
	}
	for _, id := range vm.USB {
		if !usbRE.MatchString(id) {
			return fmt.Errorf("%w: not a USB device", ErrInvalid)
		}
	}
	file := 0
	for i := range vm.Disks {
		disk := &vm.Disks[i]
		if disk.Path != "" {
			if err := m.checkExisting(ctx, disk, mounted); err != nil {
				return err
			}
			if other := m.ownerOf(*disk, vm.Name); other != "" {
				return fmt.Errorf("%w: %s is used by the VM %s", ErrBadImage, disk.Path, other)
			}
			continue
		}
		if disk.Format != "qcow2" && disk.Format != "raw" {
			return fmt.Errorf("%w: unknown disk format", ErrInvalid)
		}
		if disk.Source != "" {
			if err := m.checkSource(ctx, disk); err != nil {
				return err
			}
		}
		// The web UI takes sizes in decimal GB (decision of 31 Oct 2026),
		// so the smallest disk is 1 GB, not 1 GiB.
		if disk.SizeBytes < 1_000_000_000 || disk.SizeBytes > 16<<40 {
			return fmt.Errorf("%w: disk size out of range", ErrInvalid)
		}
		if !slices.Contains(mounted, disk.Volume) {
			return ErrUnknownVolume
		}
		file++
		if disk.File == "" {
			extension := map[string]string{"qcow2": "qcow2", "raw": "img"}[disk.Format]
			disk.File = fmt.Sprintf("vdisk%d.%s", file, extension)
		}
	}
	for _, disk := range vm.Disks {
		if disk.Path == "" {
			vm.Volume = disk.Volume
			break
		}
	}
	if vm.Volume == "" && len(mounted) > 0 {
		// A VM without a disk of its own still needs a place for its firmware's settings.
		vm.Volume = mounted[0]
	}
	if !slices.Contains(mounted, vm.Volume) {
		return ErrUnknownVolume
	}
	return nil
}

// freeBytes is a variable so that tests do not depend on the free space of
// the machine they run on.
var freeBytes = statfsFree

func statfsFree(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return int64(stat.Bavail) * stat.Bsize, nil
}

// share makes a file or folder of a VM reachable for QEMU through the group.
func (m *Manager) share(path string, mode os.FileMode) error {
	if err := os.Chown(path, -1, m.gid); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

func (m *Manager) makeDir(volume, name string) (string, error) {
	// The folder of all VMs is closed to everyone but root and QEMU.
	parent := filepath.Join(m.mountRoot, volume, "vms")
	dir := filepath.Join(parent, name)
	for _, path := range []string{parent, dir} {
		if err := os.Mkdir(path, 0o770); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		if info, err := os.Lstat(path); err != nil || !info.IsDir() {
			return "", ErrBadFolder
		}
		var fsInfo unix.Statfs_t
		if err := unix.Statfs(path, &fsInfo); err != nil {
			return "", err
		}
		if fsInfo.Type == unix.BTRFS_SUPER_MAGIC {
			// New disk files inherit NOCOW; existing files are not rewritten.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			_, err := m.runner.Run(ctx, "", "chattr", "+C", path)
			cancel()
			if err != nil {
				return "", err
			}
		}
		if err := m.share(path, 0o770); err != nil {
			return "", err
		}
	}
	return dir, nil
}

func newUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// newMAC returns a network address from the range of QEMU/KVM.
func newMAC() string {
	b := make([]byte, 3)
	rand.Read(b)
	return fmt.Sprintf("52:54:00:%02x:%02x:%02x", b[0], b[1], b[2])
}
