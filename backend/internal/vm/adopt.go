package vm

// Taking a VM over ("VM übernehmen"): the copies of the settings that lie in
// vms/*/vm.json on the mounted volumes (see volumecopy.go) are listed when
// someone asks, and one of them is registered again after the same checks
// as a new VM. Hardware that may differ on this NAS is left out.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const (
	// maxAdoptFolders is how many folders below vms are looked at per volume.
	maxAdoptFolders = 512
)

var (
	// ErrNoCopy means there is no readable copy of VM settings in the folder.
	ErrNoCopy = errors.New("vm: no readable copy of VM settings in this folder")
	macRE     = regexp.MustCompile(`^[0-9a-f]{2}(:[0-9a-f]{2}){5}$`)
)

// AdoptCandidate is a copy of VM settings on a volume whose VM this NAS does
// not know.
type AdoptCandidate struct {
	Volume      string     `json:"volume"`
	Directory   string     `json:"directory"`
	Name        string     `json:"name"`
	DisplayName string     `json:"displayName,omitempty"`
	Description string     `json:"description,omitempty"`
	Template    string     `json:"template"`
	Firmware    string     `json:"firmware,omitempty"`
	MemoryMiB   int        `json:"memoryMiB"`
	VCPUs       int        `json:"vcpus"`
	Disks       []string   `json:"disks"`
	Created     time.Time  `json:"created"`
	Removed     bool       `json:"removed"`
	RemovedAt   *time.Time `json:"removedAt,omitempty"`
	// NameTaken: another VM has this name; a new one must be chosen.
	NameTaken bool `json:"nameTaken"`
	// Dropped are the settings left out on taking it over (see AdoptResult).
	Dropped []string `json:"dropped"`
	// Missing are volumes the VM needs that are not mounted.
	Missing []string `json:"missing"`
}

// AdoptRequest says which copy to take over, optionally under a new name.
type AdoptRequest struct {
	Volume    string `json:"volume"`
	Directory string `json:"directory"`
	Name      string `json:"name"`
}

// AdoptResult is the VM taken over and what was changed on the way, as
// codes: pci, usb, autostart, powerSchedule, snapshotSchedule, cpuPin,
// vcpus, mac, iso, folders, network, displayName.
type AdoptResult struct {
	Name    string   `json:"name"`
	Changes []string `json:"changes"`
}

// readCopyAt reads the copy in a folder below vms of a mounted volume.
func (m *Manager) readCopyAt(volume, directory string, mounted []string) (VolumeCopy, error) {
	var c VolumeCopy
	dir, err := m.openVMFolder(volume, directory, mounted)
	if errors.Is(err, volumedata.ErrUnavailable) {
		return c, ErrUnknownVolume
	}
	if errors.Is(err, fs.ErrNotExist) {
		return c, ErrNoCopy
	}
	if err != nil {
		return c, err
	}
	defer dir.Close()
	data, err := readCopyFile(dir)
	if err != nil || json.Unmarshal(data, &c) != nil || c.Format != volumeCopyFormat || !nameRE.MatchString(c.VM.Name) {
		return VolumeCopy{}, ErrNoCopy
	}
	return c, nil
}

// registeredHere reports whether the VM of a copy found in a folder is
// known already: the same VM, or another one using that folder.
func (m *Manager) registeredHere(c VolumeCopy, volume, directory string) bool {
	dir := m.dir(volume, directory)
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, other := range m.vms {
		if c.VM.UUID != "" && other.UUID == c.VM.UUID {
			return true
		}
		for _, v := range other.volumes() {
			if m.dir(v, other.folder()) == dir {
				return true
			}
		}
	}
	return false
}

// AdoptCandidates lists the copies of VM settings on the mounted volumes
// whose VMs are not registered. It reads the volumes, so it only runs when
// someone asks, never in the polling of the status.
func (m *Manager) AdoptCandidates(mounted []string) []AdoptCandidate {
	found := []AdoptCandidate{}
	for _, volume := range mounted {
		root, err := volumedata.OpenVolume(m.mountRoot, volume, mounted)
		if err != nil {
			continue
		}
		var entries []fs.DirEntry
		if info, err := root.Lstat("vms"); err == nil && info.IsDir() && info.Mode()&fs.ModeSymlink == 0 {
			if dir, err := root.Open("vms"); err == nil {
				entries, _ = dir.ReadDir(maxAdoptFolders)
				dir.Close()
			}
		}
		root.Close()
		slices.SortFunc(entries, func(a, b fs.DirEntry) int { return strings.Compare(a.Name(), b.Name()) })
		for _, entry := range entries {
			if !entry.IsDir() || entry.Type()&fs.ModeSymlink != 0 || !validVMDirectory(entry.Name()) {
				continue
			}
			c, err := m.readCopyAt(volume, entry.Name(), mounted)
			if err != nil || m.registeredHere(c, volume, entry.Name()) {
				continue
			}
			found = append(found, m.candidate(c, volume, entry.Name(), mounted))
		}
	}
	return found
}

func (m *Manager) candidate(c VolumeCopy, volume, directory string, mounted []string) AdoptCandidate {
	v := relocate(c.VM, volume, directory, m.mountRoot)
	result := AdoptCandidate{Volume: volume, Directory: directory, Name: v.Name, DisplayName: v.DisplayName, Description: v.Description,
		Template: v.Template, Firmware: v.Firmware, MemoryMiB: v.MemoryMiB, VCPUs: v.VCPUs, Disks: []string{}, Created: v.Created,
		Removed: c.Removed, RemovedAt: c.RemovedAt, Dropped: []string{}, Missing: []string{}}
	for _, disk := range v.Disks {
		result.Disks = append(result.Disks, m.diskPath(v, disk))
	}
	for _, need := range v.volumes() {
		if !slices.Contains(mounted, need) {
			result.Missing = append(result.Missing, need)
		}
	}
	m.mu.Lock()
	_, result.NameTaken = m.vms[v.Name]
	m.mu.Unlock()
	result.Dropped = m.dropForAdoption(&v, mounted)
	return result
}

// relocate moves a VM to the folder its copy was found in: the volume or
// the folder may carry another name now than when the copy was written.
// Disks in the old folder move along; everything else stays as it was.
func relocate(v VM, volume, directory, mountRoot string) VM {
	oldVolume := v.Volume
	oldFolder := filepath.Join(mountRoot, v.Volume, "vms", v.folder())
	newFolder := filepath.Join(mountRoot, volume, "vms", directory)
	v.Disks = slices.Clone(v.Disks)
	for i := range v.Disks {
		disk := &v.Disks[i]
		if disk.Path == "" {
			if disk.Volume == oldVolume {
				disk.Volume = volume
			}
		} else if rest, inside := strings.CutPrefix(disk.Path, oldFolder+"/"); inside {
			disk.Path = filepath.Join(newFolder, rest)
		}
	}
	v.Volume = volume
	v.Directory = ""
	if directory != v.Name {
		v.Directory = directory
	}
	return v
}

// dropForAdoption leaves out what may not fit this NAS and returns codes of
// what it changed. Everything else is checked as on creating a VM.
func (m *Manager) dropForAdoption(v *VM, mounted []string) []string {
	changes := []string{}
	note := func(code string) {
		if !slices.Contains(changes, code) {
			changes = append(changes, code)
		}
	}
	// The hardware of another NAS may be anything.
	if len(v.PCI) > 0 || v.AutoGPU {
		note("pci")
	}
	if len(v.USB) > 0 {
		note("usb")
	}
	v.PCI, v.USB, v.AutoGPU = nil, nil, false
	// Nothing starts by itself until someone looked at the VM.
	if v.Autostart || (v.TopologyAutostart != nil && *v.TopologyAutostart) {
		note("autostart")
	}
	v.Autostart, v.TopologyAutostart = false, nil
	if v.PowerSchedule.Enabled {
		note("powerSchedule")
		v.PowerSchedule.Enabled = false
	}
	if v.SnapshotSchedule.Enabled {
		note("snapshotSchedule")
		v.SnapshotSchedule.Enabled = false
	}
	for _, cpu := range v.CPUPin {
		if cpu < 0 || cpu >= runtime.NumCPU() {
			note("cpuPin")
			v.CPUPin, v.Exclusive = nil, false
			v.VCPUs = min(v.VCPUs, runtime.NumCPU())
			break
		}
	}
	if v.VCPUs > runtime.NumCPU() {
		note("vcpus")
		v.VCPUs = runtime.NumCPU()
	}
	if len(v.CPUPin) > 0 && v.Exclusive {
		// A reservation takes CPUs away from the NAS; it is chosen again.
		note("cpuPin")
		v.Exclusive = false
	}
	m.mu.Lock()
	macTaken, labelTaken := false, false
	for _, other := range m.vms {
		macTaken = macTaken || strings.EqualFold(other.MAC, v.MAC)
		labelTaken = labelTaken || (v.DisplayName != "" && strings.EqualFold(other.Label(), v.DisplayName))
	}
	m.mu.Unlock()
	if macTaken || !macRE.MatchString(v.MAC) {
		note("mac")
		v.MAC = newMAC()
	}
	if labelTaken || (v.DisplayName != "" && !validDisplayName(v.DisplayName)) {
		note("displayName")
		v.DisplayName = ""
	}
	for _, path := range []*string{&v.ISO, &v.VirtIOISO} {
		if *path == "" {
			continue
		}
		if _, err := m.checkISO(*path); err != nil || !m.onMounted(*path, mounted) {
			note("iso")
			*path = ""
		}
	}
	kept := []Folder{}
	for _, folder := range v.Folders {
		if real, info, err := m.onVolume(folder.Path); err == nil && info.IsDir() && m.onMounted(real, mounted) {
			kept = append(kept, folder)
		} else {
			note("folders")
		}
	}
	v.Folders = kept
	switch {
	case strings.HasPrefix(v.Network, "network:"):
		if _, ok := m.namedBridge(v.Network); !ok {
			note("network")
			v.Network = "user"
		}
	case v.Network == "bridge":
		if m.BridgeReady != nil && !m.BridgeReady() {
			note("network")
			v.Network = "user"
		}
	}
	return changes
}

// onMounted reports whether a path below the mount root is on a mounted volume.
func (m *Manager) onMounted(path string, mounted []string) bool {
	relative, err := filepath.Rel(m.mountRoot, path)
	if err != nil || relative == "." || strings.HasPrefix(relative, "..") {
		return false
	}
	return slices.Contains(mounted, strings.Split(filepath.ToSlash(relative), "/")[0])
}

// checkAdoptedDisks checks the disks of a VM to take over: the files in the
// VM's folders must be there as regular files, images used directly must be
// on mounted volumes, and no other VM may use any of them. Whole disks are
// checked by prepare like on creating a VM: by their stable name and free.
func (m *Manager) checkAdoptedDisks(ctx context.Context, v *VM, mounted []string) error {
	for i := range v.Disks {
		disk := &v.Disks[i]
		if disk.Path != "" {
			if !m.isBlock(*disk) && !m.onMounted(disk.Path, mounted) {
				return ErrUnknownVolume
			}
			if other := m.usedBy(disk.Path, v.Name); other != "" {
				return fmt.Errorf("%w: %s is used by the VM %s", ErrBadImage, disk.Path, other)
			}
			continue
		}
		disk.Source = ""
		if !slices.Contains(mounted, disk.Volume) {
			return ErrUnknownVolume
		}
		if disk.File == "" || filepath.Base(disk.File) != disk.File || strings.HasPrefix(disk.File, ".") {
			return ErrBadImage
		}
		folder := m.dir(disk.Volume, v.folder())
		if info, err := os.Lstat(folder); err != nil || !info.IsDir() || info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%w: %s", ErrBadImage, folder)
		}
		path := m.diskPath(*v, *disk)
		if info, err := os.Lstat(path); err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf("%w: %s is missing", ErrBadImage, path)
		}
		if other := m.usedBy(path, v.Name); other != "" {
			return fmt.Errorf("%w: %s is used by the VM %s", ErrBadImage, path, other)
		}
		image, err := m.imageInfo(ctx, path)
		if err != nil {
			return err
		}
		if image.Format != disk.Format {
			return fmt.Errorf("%w: %s is not %s", ErrBadImage, path, disk.Format)
		}
		disk.SizeBytes = max(disk.SizeBytes, image.VirtualBytes)
	}
	return nil
}

// Adopt registers the VM of a copy on a volume again. It is checked like a
// new VM; PCI and USB devices are left out, and it does not start by itself.
// The name must be free: otherwise a new one is asked for (ErrExists).
func (m *Manager) Adopt(ctx context.Context, req AdoptRequest, mounted []string) (AdoptResult, error) {
	if m.NetworkLease != nil {
		release, err := m.NetworkLease()
		if err != nil {
			return AdoptResult{}, err
		}
		defer release()
	}
	if !m.createMu.TryLock() {
		return AdoptResult{}, ErrSnapshotBusy
	}
	defer m.createMu.Unlock()
	if m.Disabled {
		return AdoptResult{}, ErrDisabled
	}
	m.mu.Lock()
	loaded := m.loaded
	m.mu.Unlock()
	if !loaded {
		return AdoptResult{}, fmt.Errorf("%w: /config is not readable", ErrInvalid)
	}
	if !slices.Contains(mounted, req.Volume) {
		return AdoptResult{}, ErrUnknownVolume
	}
	if !validVMDirectory(req.Directory) {
		return AdoptResult{}, ErrBadFolder
	}
	c, err := m.readCopyAt(req.Volume, req.Directory, mounted)
	if err != nil {
		return AdoptResult{}, err
	}
	if m.registeredHere(c, req.Volume, req.Directory) {
		return AdoptResult{}, fmt.Errorf("%w: the VM in this folder is registered", ErrExists)
	}
	v := relocate(c.VM, req.Volume, req.Directory, m.mountRoot)
	if req.Name != "" {
		v.Name = req.Name
		// The folder keeps its name.
		v.Directory = ""
		if req.Directory != v.Name {
			v.Directory = req.Directory
		}
	}
	if !nameRE.MatchString(v.Name) {
		return AdoptResult{}, ErrInvalidName
	}
	m.mu.Lock()
	_, taken := m.vms[v.Name]
	m.mu.Unlock()
	if taken {
		return AdoptResult{}, fmt.Errorf("%w: choose another name", ErrExists)
	}
	changes := m.dropForAdoption(&v, mounted)
	// The UUID names the folder of the snapshots; one that is not a plain
	// UUID (as a vm.json from elsewhere might carry) gets a new one.
	if v.UUID = strings.ToLower(v.UUID); !snapshotID.MatchString(v.UUID) {
		v.UUID = newUUID()
	}
	if v.Created.IsZero() {
		v.Created = time.Now().UTC().Truncate(time.Second)
	}
	if err := m.checkAdoptedDisks(ctx, &v, mounted); err != nil {
		return AdoptResult{}, err
	}
	if err := m.prepare(ctx, &v, mounted); err != nil {
		return AdoptResult{}, err
	}
	if v.Volume != req.Volume {
		// The copy lies in the folder of the first disk TitanOS made.
		return AdoptResult{}, fmt.Errorf("%w: the copy does not belong to this folder", ErrBadFolder)
	}
	if err := m.checkFolderOwner(v); err != nil {
		return AdoptResult{}, err
	}
	if err := m.persistVM(v); err != nil {
		return AdoptResult{}, err
	}
	m.mu.Lock()
	m.vms[v.Name] = v
	m.mu.Unlock()
	slog.Info("VM taken over from the copy on its volume", "vm", v.Name, "volume", req.Volume, "folder", req.Directory, "changes", changes)
	return AdoptResult{Name: v.Name, Changes: changes}, nil
}
