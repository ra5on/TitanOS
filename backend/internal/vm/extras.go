package vm

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// imageFormats are the disk images TitanOS reads, by file extension, with
// what QEMU calls the format.
var imageFormats = map[string]string{
	".qcow2": "qcow2", ".img": "raw", ".raw": "raw", ".vmdk": "vmdk", ".vhdx": "vhdx", ".vdi": "vdi",
}

// onVolume makes sure a path leads to something on a volume, links
// included, and returns where it really is.
func (m *Manager) onVolume(path string) (string, fs.FileInfo, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", nil, fs.ErrInvalid
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || !strings.HasPrefix(real, m.mountRoot+"/") {
		return "", nil, fs.ErrInvalid
	}
	info, err := os.Stat(real)
	return real, info, err
}

func (m *Manager) checkFolders(folders []Folder) error {
	if len(folders) > 8 {
		return fmt.Errorf("%w: too many folders", ErrInvalid)
	}
	tags := map[string]bool{}
	for i := range folders {
		folder := &folders[i]
		real, info, err := m.onVolume(folder.Path)
		if err != nil || !info.IsDir() {
			return ErrBadFolder
		}
		folder.Path = real
		if !tagRE.MatchString(folder.Tag) || tags[folder.Tag] {
			return fmt.Errorf("%w: the names of the folders in the VM must be simple and differ", ErrInvalid)
		}
		tags[folder.Tag] = true
	}
	return nil
}

// Image is what qemu-img says about a disk image.
type Image struct {
	Path         string `json:"path"`
	Format       string `json:"format"`
	VirtualBytes int64  `json:"virtualBytes"`
	ActualBytes  int64  `json:"actualBytes"`
	// UsedBy is the VM that has this image as a disk, if any.
	UsedBy string `json:"usedBy"`
}

func (m *Manager) imageInfo(ctx context.Context, path string) (Image, error) {
	// The format comes from the name, never from the content: an image
	// whose content claims to be something else could point QEMU at other
	// files of the NAS.
	format, known := imageFormats[strings.ToLower(filepath.Ext(path))]
	if !known {
		return Image{}, ErrBadImage
	}
	out, err := m.runner.Run(ctx, "", "qemu-img", "info", "--output=json", "-f", format, path)
	if err != nil {
		return Image{}, fmt.Errorf("%w: %v", ErrBadImage, err)
	}
	var info struct {
		VirtualSize int64  `json:"virtual-size"`
		ActualSize  int64  `json:"actual-size"`
		Backing     string `json:"backing-filename"`
	}
	var raw any
	if json.Unmarshal([]byte(out), &info) != nil || json.Unmarshal([]byte(out), &raw) != nil || info.VirtualSize <= 0 || info.Backing != "" {
		// An image that builds on another file is not taken: that file
		// could be anything.
		return Image{}, ErrBadImage
	}
	if hasDataFile(raw) {
		// A qcow2 external data file holds the guest data outside the image;
		// copies (snapshots, clones) would share it and it could be any file.
		return Image{}, fmt.Errorf("%w: images with an external data file are not supported", ErrBadImage)
	}
	return Image{Path: path, Format: format, VirtualBytes: info.VirtualSize, ActualBytes: info.ActualSize}, nil
}

// hasDataFile reports whether qemu-img info names an external data file
// anywhere (format-specific.data.data-file for qcow2).
func hasDataFile(v any) bool {
	switch value := v.(type) {
	case map[string]any:
		for k, v := range value {
			if k == "data-file" {
				if s, ok := v.(string); !ok || s != "" {
					return true
				}
			}
			if hasDataFile(v) {
				return true
			}
		}
	case []any:
		for _, v := range value {
			if hasDataFile(v) {
				return true
			}
		}
	}
	return false
}

// usedBy returns the VM that has a path as a disk. The caller holds no lock.
func (m *Manager) usedBy(path, except string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, vm := range m.vms {
		for _, disk := range vm.Disks {
			if name != except && (m.diskPath(vm, disk) == path || disk.Source == path) {
				return name
			}
		}
	}
	return ""
}

// ownerOf returns another VM that uses the same disk or image as disk, by
// what the paths lead to: one whole disk has several stable names, and an
// image can be reached over a link. Unlike usedBy it is not fooled by a
// second name for the same thing.
func (m *Manager) ownerOf(disk Disk, except string) string {
	target := m.diskTarget(disk, disk.Path)
	if target == "" {
		return ""
	}
	type use struct {
		vm   string
		disk Disk
		path string
	}
	var uses []use
	m.mu.Lock()
	for name, vm := range m.vms {
		if name == except {
			continue
		}
		for _, d := range vm.Disks {
			uses = append(uses, use{name, d, m.diskPath(vm, d)})
		}
	}
	m.mu.Unlock()
	for _, u := range uses {
		if m.diskTarget(u.disk, u.path) == target {
			return u.vm
		}
	}
	return ""
}

// diskTarget is what a disk path leads to: the kernel's name of a whole
// disk, the real path of an image; empty when it leads nowhere.
func (m *Manager) diskTarget(disk Disk, path string) string {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	if m.isBlock(disk) {
		return "block:" + filepath.Base(real)
	}
	return "file:" + real
}

// checkSource looks at the image a new disk is to be a copy of.
func (m *Manager) checkSource(ctx context.Context, disk *Disk) error {
	real, info, err := m.onVolume(disk.Source)
	if err != nil || !info.Mode().IsRegular() {
		return ErrBadImage
	}
	image, err := m.imageInfo(ctx, real)
	if err != nil {
		return err
	}
	disk.Source = real
	// A copy can be larger than its original, never smaller.
	if disk.SizeBytes < image.VirtualBytes {
		disk.SizeBytes = image.VirtualBytes
	}
	return nil
}

// checkExisting looks at a disk that is not a file of the VM's folder: an
// image used as it is, or a whole disk of the NAS.
func (m *Manager) checkExisting(ctx context.Context, disk *Disk, mounted []string) error {
	disk.File, disk.Volume, disk.Source = "", "", ""
	if m.isBlock(*disk) {
		device, err := m.blockDevice(disk.Path)
		if err != nil {
			return err
		}
		disk.Format = "raw"
		if size, err := os.ReadFile(filepath.Join("/sys/class/block", device, "size")); err == nil {
			sectors, _ := strconv.ParseInt(strings.TrimSpace(string(size)), 10, 64)
			disk.SizeBytes = sectors * 512
		}
		return nil
	}
	real, info, err := m.onVolume(disk.Path)
	if err != nil || !info.Mode().IsRegular() {
		return ErrBadImage
	}
	image, err := m.imageInfo(ctx, real)
	if err != nil {
		return err
	}
	// Used as it is, so only what QEMU can write to directly.
	if image.Format != "qcow2" && image.Format != "raw" {
		return fmt.Errorf("%w: only qcow2 and raw images can be used without a copy", ErrBadImage)
	}
	disk.Path, disk.Format, disk.SizeBytes = real, image.Format, image.VirtualBytes
	return nil
}

// blockDevice returns the kernel's name (sdb) of a whole disk given by its
// stable name, if that disk is in no volume.
func (m *Manager) blockDevice(path string) (string, error) {
	if filepath.Dir(path) != m.byID || strings.Contains(filepath.Base(path), "-part") {
		return "", ErrBadImage
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", ErrBadImage
	}
	device := filepath.Base(real)
	if !slices.Contains(m.FreeDisks(), device) {
		return "", fmt.Errorf("%w: the disk is in use by the NAS", ErrBadImage)
	}
	return device, nil
}

// ClaimedDisks returns the whole disks that VMs use, by the kernel's name
// (sdb), with the VM. Nothing else may write to such a disk.
func (m *Manager) ClaimedDisks() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	claimed := map[string]string{}
	for _, vm := range m.vms {
		for _, disk := range vm.Disks {
			if !m.isBlock(disk) {
				continue
			}
			if real, err := filepath.EvalSymlinks(disk.Path); err == nil {
				claimed[filepath.Base(real)] = "VM " + vm.Name
			}
		}
	}
	return claimed
}

// WholeDisk is a disk of the NAS that can be handed to a VM.
type WholeDisk struct {
	// Path is the stable name, /dev/disk/by-id/….
	Path   string `json:"path"`
	Device string `json:"device"`
	UsedBy string `json:"usedBy"`
}

// WholeDisks lists the disks that are in no volume, by their stable names.
func (m *Manager) WholeDisks() []WholeDisk {
	free := m.FreeDisks()
	entries, _ := os.ReadDir(m.byID)
	best := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		// Names made from the serial number say more than the WWN ones.
		if strings.Contains(name, "-part") || strings.HasPrefix(name, "wwn-") || strings.HasPrefix(name, "nvme-eui.") {
			continue
		}
		real, err := filepath.EvalSymlinks(filepath.Join(m.byID, name))
		if err != nil || !slices.Contains(free, filepath.Base(real)) {
			continue
		}
		if device := filepath.Base(real); best[device] == "" || name < best[device] {
			best[device] = name
		}
	}
	disks := []WholeDisk{}
	for device, name := range best {
		path := filepath.Join(m.byID, name)
		disks = append(disks, WholeDisk{Path: path, Device: device, UsedBy: m.usedBy(path, "")})
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Device < disks[j].Device })
	return disks
}

// Images looks for disk images on the mounted volumes.
func (m *Manager) Images(ctx context.Context, mounted []string) []Image {
	images := []Image{}
	visited := 0
	for _, volume := range mounted {
		root := filepath.Join(m.mountRoot, volume)
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error { // the callback collects what it can; unreadable entries are skipped
			visited++
			if err != nil || visited > 50000 || len(images) >= 100 {
				return fs.SkipDir
			}
			if entry.IsDir() {
				depth := strings.Count(strings.TrimPrefix(path, root), "/")
				if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "appdata" || depth > 5) {
					return fs.SkipDir
				}
				return nil
			}
			if _, known := imageFormats[strings.ToLower(filepath.Ext(entry.Name()))]; !known || !entry.Type().IsRegular() {
				return nil
			}
			if image, err := m.imageInfo(ctx, path); err == nil {
				image.UsedBy = m.usedBy(path, "")
				images = append(images, image)
			}
			return nil
		})
	}
	sort.Slice(images, func(i, j int) bool { return images[i].Path < images[j].Path })
	return images
}

// copyDisks makes the disks of a new VM that are copies of existing images.
func (m *Manager) copyDisks(ctx context.Context, vm VM) {
	// Background preparation must not mutate the slice published to readers.
	vm.Disks = slices.Clone(vm.Disks)
	fail := func(err error) {
		slog.Error("disk of a VM not prepared", "vm", vm.Name, "err", err)
		m.mu.Lock()
		m.jobs[vm.Name] = &Job{State: "failed", Text: err.Error()}
		m.mu.Unlock()
	}
	for i, disk := range vm.Disks {
		if disk.Source == "" {
			continue
		}
		path := m.diskPath(vm, disk)
		source, err := m.imageInfo(ctx, disk.Source)
		if err != nil {
			fail(err)
			return
		}
		m.mu.Lock()
		m.jobs[vm.Name] = &Job{State: "running", Text: filepath.Base(disk.Source)}
		m.mu.Unlock()
		if _, err := m.runner.Run(ctx, "", "qemu-img", "convert", "-f", source.Format, "-O", disk.Format, disk.Source, path); err != nil {
			os.Remove(path)
			fail(err)
			return
		}
		if disk.SizeBytes > source.VirtualBytes {
			if _, err := m.runner.Run(ctx, "", "qemu-img", "resize", "-q", "-f", disk.Format, path, strconv.FormatInt(disk.SizeBytes, 10)); err != nil {
				fail(err)
				return
			}
		}
		if err := m.share(path, 0o660); err != nil {
			fail(err)
			return
		}
		vm.Disks[i].Source = ""
	}
	if err := m.persistVM(vm); err != nil {
		fail(err)
		return
	}
	m.mu.Lock()
	m.vms[vm.Name] = vm
	delete(m.jobs, vm.Name)
	m.mu.Unlock()
	slog.Info("disks of a VM prepared", "vm", vm.Name)
}

// Grow makes a disk of a VM larger. The guest has to extend its partition
// itself afterwards. Disks never shrink: that would cut data off.
func (m *Manager) Grow(ctx context.Context, name string, index int, sizeBytes int64) error {
	lock, lockErr := m.operation(name)
	if lockErr != nil {
		return lockErr
	}
	defer lock.Unlock()
	if v, e := m.get(name); e == nil && m.pendingSnapshot(v) {
		return ErrSnapshotRecovery
	}
	if m.Disabled {
		return ErrDisabled
	}
	vm, err := m.get(name)
	if err != nil {
		return err
	}
	if index < 0 || index >= len(vm.Disks) || m.isBlock(vm.Disks[index]) || vm.Disks[index].Source != "" {
		return ErrNotFound
	}
	if m.pid(name) != 0 {
		return ErrRunning
	}
	disk := vm.Disks[index]
	if sizeBytes <= disk.SizeBytes || sizeBytes > 16<<40 {
		return fmt.Errorf("%w: a disk can only grow", ErrInvalid)
	}
	path := m.diskPath(vm, disk)
	if disk.Format == "raw" {
		if free, err := freeBytes(filepath.Dir(path)); err != nil || free < sizeBytes-disk.SizeBytes {
			return ErrNoSpace
		}
	}
	if _, err := m.runner.Run(ctx, "", "qemu-img", "resize", "-q", "-f", disk.Format, path, strconv.FormatInt(sizeBytes, 10)); err != nil {
		return err
	}
	vm.Disks[index].SizeBytes = sizeBytes
	if err := m.persistVM(vm); err != nil {
		return err
	}
	m.mu.Lock()
	m.vms[name] = vm
	m.mu.Unlock()
	return nil
}

// lend opens the whole disks and USB devices of a VM to QEMU, which does
// not run as root. Only these device nodes change their group, and only
// while the VM runs.
func (m *Manager) lend(vm VM) error {
	// Each node is recorded before its group changes, so that release
	// returns it even when a later node fails and the start is abandoned.
	// Nodes a failed earlier start left behind stay recorded.
	borrow := func(node string) error {
		m.mu.Lock()
		if !slices.Contains(m.borrowed[vm.Name], node) {
			m.borrowed[vm.Name] = append(m.borrowed[vm.Name], node)
		}
		m.mu.Unlock()
		return os.Chown(node, -1, m.runGID)
	}
	for _, disk := range vm.Disks {
		if !m.isBlock(disk) {
			continue
		}
		real, err := filepath.EvalSymlinks(disk.Path)
		if err != nil {
			return ErrBadImage
		}
		if err := borrow(real); err != nil {
			return err
		}
	}
	for _, id := range vm.USB {
		// The same for USB devices; every plugged-in device of this kind.
		for _, node := range m.usbNodes(id) {
			if err := borrow(node); err != nil {
				return err
			}
		}
	}
	return nil
}

// usbNodes returns the device nodes of the USB devices with an ID like 046d:c52b.
func (m *Manager) usbNodes(id string) []string {
	var nodes []string
	entries, _ := os.ReadDir(filepath.Join(m.sysDir, "bus/usb/devices"))
	for _, entry := range entries {
		read := func(file string) string {
			data, _ := os.ReadFile(filepath.Join(m.sysDir, "bus/usb/devices", entry.Name(), file))
			return strings.TrimSpace(string(data))
		}
		if strings.Contains(entry.Name(), ":") || read("idVendor")+":"+read("idProduct") != id {
			continue
		}
		bus, _ := strconv.Atoi(read("busnum"))
		device, _ := strconv.Atoi(read("devnum"))
		if bus > 0 && device > 0 {
			nodes = append(nodes, filepath.Join(m.devDir, "bus/usb", fmt.Sprintf("%03d/%03d", bus, device)))
		}
	}
	return nodes
}

// release takes back what VMs that no longer run were given: device nodes
// and reserved CPUs. The caller holds the lock.
func (m *Manager) release() {
	for name, nodes := range m.borrowed {
		if m.starting[name] || m.pid(name) != 0 {
			continue
		}
		for _, node := range nodes {
			// Back to the group disks belong to; USB devices belong to root.
			group := 0
			if !strings.Contains(node, "/bus/usb/") {
				group = lookupGroup("disk", 0)
			}
			_ = os.Chown(node, -1, group) // permissions and times are a convenience; the data are there either way
		}
		delete(m.borrowed, name)
	}
	if m.reserved != "" && m.exclusiveCPUs() == "" {
		m.setAllowed(context.Background(), "")
	}
}

// exclusiveCPUs returns the CPUs of all running VMs that want them for
// themselves, as a list like "2-5". The caller holds the lock.
func (m *Manager) exclusiveCPUs() string {
	taken := map[int]bool{}
	for name, vm := range m.vms {
		if vm.Exclusive && m.pid(name) != 0 {
			for _, cpu := range vm.CPUPin {
				taken[cpu] = true
			}
		}
	}
	var rest []string
	if len(taken) == 0 {
		return ""
	}
	for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
		if !taken[cpu] {
			rest = append(rest, strconv.Itoa(cpu))
		}
	}
	return strings.Join(rest, ",")
}

// reserve moves everything else on the NAS off the CPUs of exclusive VMs.
func (m *Manager) reserve(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if rest := m.exclusiveCPUs(); rest != m.reserved {
		m.setAllowed(ctx, rest)
	}
}

// setAllowed limits the system, logins and containers to a set of CPUs;
// an empty set lifts the limit. VMs live in a slice of their own and are
// not affected. Nothing is written to disk: after a restart all is as before.
func (m *Manager) setAllowed(ctx context.Context, cpus string) {
	if cpus == "" && m.reserved == "" {
		return
	}
	for _, slice := range []string{"system.slice", "user.slice", "init.scope"} {
		if _, err := m.runner.Run(ctx, "", "systemctl", "set-property", "--runtime", slice, "AllowedCPUs="+cpus); err != nil {
			slog.Warn("CPUs not reserved", "slice", slice, "err", err)
		}
	}
	m.reserved = cpus
	slog.Info("CPUs for everything but exclusive VMs", "cpus", map[bool]string{true: "all", false: cpus}[cpus == ""])
}

// prepareTPM makes the folder the emulated TPM of a VM keeps its state in.
// The emulator runs as the user QEMU runs as, and only that user gets in.
func (m *Manager) prepareTPM(dir string) error {
	state := filepath.Join(dir, "tpm")
	if err := os.Mkdir(state, 0o700); err != nil && !os.IsExist(err) {
		return err
	}
	return os.Chown(state, m.runUID, m.runGID)
}

// CPU is a logical CPU of the NAS.
type CPU struct {
	ID int `json:"id"`
	// Kind is "p" for a performance core and "e" for an efficiency core
	// of a hybrid CPU, empty where all cores are alike.
	Kind string `json:"kind"`
	// Siblings are the threads of the same core, this one included.
	Siblings []int `json:"siblings"`
	// UsedBy are the VMs that have this CPU pinned.
	UsedBy []string `json:"usedBy"`
}

// CPUs describes the logical CPUs for pinning. sys is /sys.
func (m *Manager) CPUs(sys string) []CPU {
	kinds := map[int]string{}
	// Hybrid Intel CPUs list their two kinds of cores here; on other
	// CPUs the files do not exist.
	for file, kind := range map[string]string{"devices/cpu_core/cpus": "p", "devices/cpu_atom/cpus": "e"} {
		data, err := os.ReadFile(filepath.Join(sys, file))
		if err != nil {
			continue
		}
		for _, cpu := range parseCPUList(string(data)) {
			kinds[cpu] = kind
		}
	}
	m.mu.Lock()
	users := map[int][]string{}
	for name, vm := range m.vms {
		for _, cpu := range vm.CPUPin {
			users[cpu] = append(users[cpu], name)
		}
	}
	m.mu.Unlock()

	cpus := make([]CPU, 0, runtime.NumCPU())
	for id := 0; id < runtime.NumCPU(); id++ {
		cpu := CPU{ID: id, Kind: kinds[id], Siblings: []int{id}, UsedBy: users[id]}
		if cpu.UsedBy == nil {
			cpu.UsedBy = []string{}
		}
		sort.Strings(cpu.UsedBy)
		if data, err := os.ReadFile(filepath.Join(sys, "devices/system/cpu", "cpu"+strconv.Itoa(id), "topology/thread_siblings_list")); err == nil {
			if siblings := parseCPUList(string(data)); len(siblings) > 0 {
				cpu.Siblings = siblings
			}
		}
		cpus = append(cpus, cpu)
	}
	return cpus
}

// parseCPUList reads a list like "0-3,8,10-11".
func parseCPUList(list string) []int {
	var cpus []int
	for _, part := range strings.Split(strings.TrimSpace(list), ",") {
		first, last, isRange := strings.Cut(part, "-")
		from, err := strconv.Atoi(first)
		if err != nil {
			continue
		}
		to := from
		if isRange {
			if to, err = strconv.Atoi(last); err != nil {
				continue
			}
		}
		for cpu := from; cpu <= to && cpu-from < 4096; cpu++ {
			cpus = append(cpus, cpu)
		}
	}
	return cpus
}
