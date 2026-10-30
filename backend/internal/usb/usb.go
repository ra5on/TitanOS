// Package usb shows unmanaged data drives on USB, SATA, SAS and NVMe and mounts them on
// request, so that their files can be browsed and copied in the file manager.
//
// Nothing is mounted by itself and nothing is stored: which drive is plugged
// in and which is mounted is read from the system each time. A drive is named
// by its filesystem UUID, never by sdb1, which changes between plugs.
package usb

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
	"golang.org/x/sys/unix"
)

var (
	// ErrDisabled means titand runs in development mode and mounts nothing.
	ErrDisabled = errors.New("usb: not available in development mode")
	// ErrNotFound means the drive is not plugged in (any more).
	ErrNotFound = errors.New("usb: no such drive")
	// ErrUnsupported means the filesystem of the drive cannot be mounted.
	ErrUnsupported = errors.New("usb: this filesystem cannot be mounted")
	// ErrBusy means something still uses the drive.
	ErrBusy        = errors.New("usb: the drive is in use")
	ErrMountFailed = errors.New("usb: the filesystem could not be mounted")
	// ErrMounted and ErrNotMounted report a request that makes no sense.
	ErrMounted    = errors.New("usb: the drive is mounted already")
	ErrNotMounted = errors.New("usb: the drive is not mounted")
)

// Filesystems without Unix ownership use root-only mount permissions. Upload
// metadata and partial files must satisfy the same 0600 ownership checks as on
// native Linux filesystems; do not weaken those checks for removable media.
// File access remains mediated by the authenticated file-manager API.
var filesystems = map[string]struct{ kind, options string }{
	// Install and Mac media are only read: TitanOS never writes to them.
	"iso9660": {"iso9660", "ro"},
	"udf":     {"udf", "ro"},
	"hfsplus": {"hfsplus", "ro"},
	"vfat":    {"vfat", "iocharset=utf8,flush,uid=0,gid=0,fmask=0177,dmask=0077"},
	"exfat":   {"exfat", "uid=0,gid=0,fmask=0177,dmask=0077"},
	"ntfs":    {"ntfs3", "uid=0,gid=0,fmask=0177,dmask=0077"},
	"ext2":    {"ext2", ""},
	"ext3":    {"ext3", ""},
	"ext4":    {"ext4", ""},
	"xfs":     {"xfs", ""},
	"btrfs":   {"btrfs", ""},
}

// Device is a filesystem on an unmanaged physical data drive.
type Device struct {
	// ID is the filesystem UUID.
	ID    string `json:"id"`
	Name  string `json:"name"`
	Label string `json:"label"`
	Model string `json:"model"`
	// FSType is as lsblk prints it: vfat, exfat, ntfs, ext4 …
	FSType string `json:"fsType"`
	Size   uint64 `json:"size"`
	// Supported is false for filesystems that cannot be mounted.
	Supported bool   `json:"supported"`
	Mounted   bool   `json:"mounted"`
	ReadOnly  bool   `json:"readOnly"`
	Busy      bool   `json:"busy"`
	Transport string `json:"transport"`
	// Dir is where the drive is mounted; empty if it is not.
	Dir string `json:"-"`
	// Mount is the name it is mounted under.
	Mount string `json:"mount"`

	path string
}

type node struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Size      uint64 `json:"size"`
	Transport string `json:"tran"`
	Model     string `json:"model"`
	FSType    string `json:"fstype"`
	Label     string `json:"label"`
	UUID      string `json:"uuid"`
	// PartUUID names partitions without a filesystem, such as those of
	// an SSD cache of a volume.
	PartUUID   string `json:"partuuid"`
	Mountpoint string `json:"mountpoint"`
	Children   []node `json:"children"`
	// Only read for whole disks, see disks.go.
	Rotational flag   `json:"rota"`
	Removable  flag   `json:"rm"`
	ReadOnly   flag   `json:"ro"`
	DiscGran   uint64 `json:"disc-gran"`
	Serial     string `json:"serial"`
	WWN        string `json:"wwn"`
	PTType     string `json:"pttype"`
	PTUUID     string `json:"ptuuid"`
}

// flag is a lsblk yes/no column: older versions print "0"/"1", newer ones
// true/false.
type flag bool

func (f *flag) UnmarshalJSON(data []byte) error {
	switch strings.Trim(string(data), `"`) {
	case "true", "1":
		*f = true
	default:
		*f = false
	}
	return nil
}

var lsblkArgs = []string{"--json", "--bytes", "--paths", "--output", "NAME,TYPE,SIZE,TRAN,MODEL,FSTYPE,LABEL,UUID,MOUNTPOINT,ROTA,RM,RO,DISC-GRAN,SERIAL,WWN,PTTYPE,PTUUID,PARTUUID"}

// transports are the buses of local drives. An empty transport is what some
// USB adapters and older kernels report. virtio and network disks stay out.
var transports = []string{"", "usb", "sata", "sas", "nvme", "ata", "scsi", "mmc", "ieee1394"}

// imageFilesystems are written to a whole stick as an install image. The
// partitions inside are views of the same image and are not offered twice.
var imageFilesystems = []string{"iso9660", "udf"}

// Manager lists and mounts drives.
type Manager struct {
	runner run.Runner
	// root is where drives are mounted; mountsFile lists what is mounted.
	root, mountsFile string
	mountInfoFile    string
	// Disabled refuses to mount anything: in development mode titand runs on
	// the developer's own machine.
	Disabled bool
	// Reserved reports a filesystem, array, LUKS or partition UUID that
	// belongs to a volume of the NAS (storage.Manager.Owns). It is asked
	// about both the UUID and the PARTUUID of every node.
	Reserved func(uuid string) bool
	// Claimed excludes whole disks assigned to VMs or undergoing disk tests.
	Claimed func() map[string]string
	// OnPlug is told of each drive that appeared since the last look.
	OnPlug func(Device)
	// Busy reports work of the storage manager that may pick free disks;
	// no drive is formatted meanwhile.
	Busy func() bool
	// OnFormatted is told how a format ended.
	OnFormatted func(FormatJob)

	sysBlockDir string
	pause       time.Duration
	// lookPath finds a tool, exec.LookPath when nil; scratch is where XFS
	// and Btrfs are mounted to grow, /run/titanos-partitions when empty.
	lookPath func(string) (string, error)
	scratch  string
	// job is the last format; formatting holds operation until it ends.
	job *FormatJob

	operation sync.Mutex
	mu        sync.Mutex
	// seen are the drives of the last look; nil until there was one.
	seen map[string]bool
}

// NewManager returns a manager for the real system.
func NewManager() *Manager {
	return &Manager{runner: run.Exec{}, root: "/run/titanos-usb", mountsFile: "/proc/self/mounts", mountInfoFile: "/proc/self/mountinfo"}
}

// bootStickLabel is the label of the stick TitanOS runs from.
const bootStickLabel = "TITANOS"

// collect adds the usable filesystems below a drive.
// isReserved asks reserved about the UUID and the PARTUUID of a node.
func isReserved(n node, reserved func(string) bool) bool {
	if reserved == nil {
		return false
	}
	return (n.UUID != "" && reserved(n.UUID)) || (n.PartUUID != "" && reserved(n.PartUUID))
}

func collect(n node, transport string, out *[]Device, reserved func(string) bool) {
	if (n.Type != "disk" && n.Type != "part") || n.Label == bootStickLabel || isReserved(n, reserved) {
		return
	}
	if n.Transport != "" {
		transport = n.Transport
	}
	image := n.Type == "disk" && slices.Contains(imageFilesystems, n.FSType) && n.UUID != ""
	// Some FAT superfloppies expose a whole-device partition alias. Prefer
	// that partition, without accepting duplicate UUIDs on separate devices.
	alias := n.Type == "disk" && slices.ContainsFunc(n.Children, func(child node) bool {
		return child.Type == "part" && child.UUID == n.UUID && child.FSType == n.FSType && child.Size == n.Size
	})
	if !alias && n.FSType != "" && n.UUID != "" && (n.Type == "part" || n.Type == "disk") && slices.Contains(transports, transport) {
		// RAID members and swap are not data; the boot stick stays out of reach.
		if n.FSType != "linux_raid_member" && n.FSType != "swap" && n.FSType != "crypto_LUKS" && n.FSType != "LVM2_member" &&
			n.Label != bootStickLabel && !isReserved(n, reserved) {
			_, supported := filesystems[n.FSType]
			// An image stick is read as a whole; it is only busy when a
			// partition of it is in use.
			busy := n.Mountpoint != "" || len(n.Children) > 0
			if image {
				busy = n.Mountpoint != "" || anyMounted(n.Children)
			}
			*out = append(*out, Device{ID: n.UUID, Label: n.Label, Model: strings.TrimSpace(n.Model), FSType: n.FSType, Size: n.Size, Supported: supported, Busy: busy, Transport: transport, path: n.Name})
		}
	}
	for _, child := range n.Children {
		if image && child.UUID == n.UUID {
			continue
		}
		child.Model = firstNonEmpty(child.Model, n.Model)
		collect(child, transport, out, reserved)
	}
}

func anyMounted(nodes []node) bool {
	for _, n := range nodes {
		if n.Mountpoint != "" || anyMounted(n.Children) {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

var unsafeName = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// mountName is the folder name a device is mounted under.
func mountName(d Device) string {
	name := strings.Trim(unsafeName.ReplaceAllString(firstNonEmpty(d.Label, d.Model, "usb"), "-"), "-.")
	if name == "" {
		name = "usb"
	}
	if len(name) > 24 {
		name = name[:24]
	}
	short := strings.ToLower(strings.ReplaceAll(d.ID, "-", ""))
	return name + "-" + short[:min(6, len(short))]
}

// mounts returns device -> mount point for what is mounted below root.
func (m *Manager) mounts() map[string]string {
	data, _ := os.ReadFile(m.mountsFile)
	found := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		dir := strings.ReplaceAll(fields[1], `\040`, " ")
		if strings.HasPrefix(dir, m.root+"/") && !strings.Contains(strings.TrimPrefix(dir, m.root+"/"), "/") {
			found[fields[0]] = dir
		}
	}
	return found
}

// List returns the drives that are plugged in, by name.
func (m *Manager) List(ctx context.Context) ([]Device, error) {
	nodes, err := m.blockDevices(ctx)
	if err != nil {
		return nil, err
	}
	var devices []Device
	claimed := map[string]string{}
	if m.Claimed != nil {
		claimed = m.Claimed()
	}
	for _, n := range nodes {
		if n.Type != "disk" || claimed[filepath.Base(n.Name)] != "" || containsBoot(n) {
			continue
		}
		collect(n, "", &devices, m.Reserved)
	}
	mounted := m.mounts()
	for i := range devices {
		d := &devices[i]
		d.Name = firstNonEmpty(d.Label, d.Model, d.FSType)
		if dir, ok := mounted[d.path]; ok {
			d.Mounted, d.Dir, d.Mount, d.Busy = true, dir, filepath.Base(dir), false
			d.ReadOnly = m.readOnly(dir)
		}
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name+devices[i].ID < devices[j].Name+devices[j].ID })
	return devices, nil
}

func (m *Manager) find(ctx context.Context, id string) (Device, error) {
	devices, err := m.List(ctx)
	if err != nil {
		return Device{}, err
	}
	var selected *Device
	for _, d := range devices {
		if d.ID == id {
			if selected != nil {
				return Device{}, fmt.Errorf("ambiguous USB filesystem UUID")
			}
			copy := d
			selected = &copy
		}
	}
	if selected != nil {
		return *selected, nil
	}
	return Device{}, ErrNotFound
}

// Mounted is a drive that is mounted, found without asking lsblk.
type Mounted struct {
	// Mount is the folder name; Name is what to show.
	Mount, Name, Dir string
	ReadOnly         bool
}

// Mounted returns the drives that are mounted right now.
func (m *Manager) Mounted() []Mounted {
	var drives []Mounted
	for _, dir := range m.mounts() {
		name := filepath.Base(dir)
		// The mount name ends in a short piece of the UUID.
		shown := name
		if at := strings.LastIndex(name, "-"); at > 0 {
			shown = name[:at]
		}
		drives = append(drives, Mounted{Mount: name, Name: shown, Dir: dir, ReadOnly: m.readOnly(dir)})
	}
	sort.Slice(drives, func(i, j int) bool { return drives[i].Mount < drives[j].Mount })
	return drives
}

// Dir returns where a drive is mounted, by the name of its mount.
func (m *Manager) Dir(name string) (string, bool) {
	for _, dir := range m.mounts() {
		if filepath.Base(dir) == name {
			return dir, true
		}
	}
	return "", false
}

// Mount mounts a drive for browsing. The mount cannot run programs, make
// device files or use set-uid files from the drive.
func (m *Manager) Mount(ctx context.Context, id string) error {
	if !m.operation.TryLock() {
		return ErrBusy
	}
	defer m.operation.Unlock()
	return m.mount(ctx, id)
}

func (m *Manager) mount(ctx context.Context, id string) error {
	if m.Disabled {
		return ErrDisabled
	}
	d, err := m.find(ctx, id)
	if err != nil {
		return err
	}
	if d.Mounted {
		return ErrMounted
	}
	if d.Busy {
		return ErrBusy
	}
	fs, ok := filesystems[d.FSType]
	if !ok {
		return ErrUnsupported
	}
	dir := filepath.Join(m.root, mountName(d))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	options := strings.Trim("nosuid,nodev,noexec,"+fs.options, ",")
	if _, err := m.runner.Run(ctx, "", "mount", "-t", fs.kind, "-o", options, d.path, dir); err != nil {
		// The FUSE driver provides a read-only import path, including Windows
		// hibernation/unclean shutdown cases. Never force, repair or remove hiberfiles.
		if d.FSType != "ntfs" {
			os.Remove(dir)
			return fmt.Errorf("%w: %v", ErrMountFailed, err)
		}
		if _, fallback := m.runner.Run(ctx, "", "mount", "-t", "ntfs-3g", "-o", options+",ro", d.path, dir); fallback != nil {
			os.Remove(dir)
			return fmt.Errorf("%w: ntfs3: %v; read-only ntfs-3g: %v", ErrMountFailed, err, fallback)
		}
		slog.Warn("NTFS mounted read-only using fallback driver", "name", d.Name, "reason", err)
	}
	slog.Info("usb drive mounted", "name", d.Name, "fs", d.FSType)
	return nil
}

// Eject writes out what is waiting and unmounts a drive.
func (m *Manager) Eject(ctx context.Context, id string) error {
	if !m.operation.TryLock() {
		return ErrBusy
	}
	defer m.operation.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	d, err := m.find(ctx, id)
	if err != nil {
		return err
	}
	if !d.Mounted {
		return ErrNotMounted
	}
	m.runner.Run(ctx, "", "sync")
	if _, err := m.runner.Run(ctx, "", "umount", d.Dir); err != nil {
		var cmd *run.CommandError
		if errors.As(err, &cmd) && strings.Contains(cmd.Output, "busy") {
			return ErrBusy
		}
		return err
	}
	os.Remove(d.Dir)
	slog.Info("usb drive ejected", "name", d.Name)
	return nil
}

// Watch tells OnPlug of every drive that appears, until ctx is done. A look
// costs one lsblk; it only happens when /sys/block changes.
func (m *Manager) Watch(ctx context.Context) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var last string
	for {
		m.pollMedia()
		listing := m.blockListing()
		if listing != last {
			last = listing
			m.look(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// blockListing is a cheap fingerprint of the disks and partitions present.
// The size is part of it: a card put into a reader that is already plugged in
// changes no name, only the size of the reader from 0.
func (m *Manager) blockListing() string {
	var names []string
	disks, _ := os.ReadDir(m.sysBlock())
	for _, disk := range disks {
		size, _ := os.ReadFile(filepath.Join(m.sysBlock(), disk.Name(), "size"))
		names = append(names, disk.Name()+"="+strings.TrimSpace(string(size)))
		parts, _ := os.ReadDir(filepath.Join(m.sysBlock(), disk.Name()))
		for _, part := range parts {
			if strings.HasPrefix(part.Name(), disk.Name()) {
				names = append(names, part.Name())
			}
		}
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

func (m *Manager) sysBlock() string {
	if m.sysBlockDir != "" {
		return m.sysBlockDir
	}
	return "/sys/block"
}

// pollMedia lets the kernel look for a new medium in card readers and other
// removable drives every two seconds. Without a desktop daemon nothing asks,
// and a card put into a reader that stays plugged in would never show up.
// Drives that set their own interval keep it.
func (m *Manager) pollMedia() {
	disks, _ := os.ReadDir(m.sysBlock())
	for _, disk := range disks {
		dir := filepath.Join(m.sysBlock(), disk.Name())
		removable, _ := os.ReadFile(filepath.Join(dir, "removable"))
		events, _ := os.ReadFile(filepath.Join(dir, "events"))
		interval, _ := os.ReadFile(filepath.Join(dir, "events_poll_msecs"))
		if strings.TrimSpace(string(removable)) != "1" || !strings.Contains(string(events), "media_change") || strings.TrimSpace(string(interval)) != "-1" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, "events_poll_msecs"), []byte("2000"), 0o644); err != nil {
			slog.Debug("media polling not set", "disk", disk.Name(), "err", err)
		}
	}
}

func (m *Manager) look(ctx context.Context) {
	devices, err := m.List(ctx)
	if err != nil {
		return
	}
	m.mu.Lock()
	first := m.seen == nil
	now := map[string]bool{}
	var plugged []Device
	for _, d := range devices {
		now[d.ID] = true
		if !first && !m.seen[d.ID] && d.Transport == "usb" {
			plugged = append(plugged, d)
		}
	}
	m.seen = now
	m.mu.Unlock()
	if m.OnPlug != nil {
		for _, d := range plugged {
			m.OnPlug(d)
		}
	}
}

// WithMounted opens the selected filesystem, never its empty mount directory.
// The open root pins that filesystem even if an external unmount is attempted.
// Explicitly configured backup jobs may mount their target; ejection is blocked
// while copying. Drives are left mounted for browsing and safe manual ejection.
func (m *Manager) WithMounted(ctx context.Context, id string, work func(*os.Root) error) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.operation.Lock()
	defer m.operation.Unlock()
	d, err := m.find(ctx, id)
	if err != nil {
		return err
	}
	if !d.Mounted {
		if err := m.mount(ctx, id); err != nil {
			return err
		}
		d, err = m.find(ctx, id)
		if err != nil {
			return err
		}
	}
	if !d.Mounted {
		return ErrNotMounted
	}
	if d.ReadOnly {
		return ErrBusy
	}
	root, err := os.OpenRoot(d.Dir)
	if err != nil {
		return err
	}
	defer root.Close()
	// Recheck after opening: refuse a vanished/replaced mount before any write.
	again, err := m.find(ctx, id)
	if err != nil {
		return err
	}
	if !again.Mounted || again.ReadOnly || again.Dir != d.Dir || again.path != d.path {
		return ErrNotMounted
	}
	// Linux mount IDs identify the pinned mount even for Btrfs subvolumes,
	// whose stat device number need not match mountinfo's device number.
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer directory.Close()
	var stat unix.Statx_t
	if err := unix.Statx(int(directory.Fd()), "", unix.AT_EMPTY_PATH, unix.STATX_MNT_ID, &stat); err != nil {
		return err
	}
	if stat.Mask&unix.STATX_MNT_ID == 0 {
		return ErrNotMounted
	}
	mountInfo, err := os.ReadFile(m.mountInfoFile)
	if err != nil {
		return err
	}
	if !matchesMount(string(mountInfo), d.Dir, d.path, d.FSType, fmt.Sprintf("%d", stat.Mnt_id)) {
		return ErrNotMounted
	}
	return work(root)
}

func matchesMount(info, dir, device, fsType, mountID string) bool {
	for _, line := range strings.Split(info, "\n") {
		before, after, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		fields, tail := strings.Fields(before), strings.Fields(after)
		if len(fields) < 6 || len(tail) < 3 {
			continue
		}
		// Our generated mount directory and lsblk device path contain no spaces.
		if fields[4] == dir && fields[0] == mountID && tail[1] == device {
			if fsType == "ntfs" {
				return tail[0] == "ntfs3" || tail[0] == "fuseblk"
			}
			return tail[0] == fsType
		}
	}
	return false
}

// Exclude every partition of the system stick, even when it has more than one.
func containsBoot(n node) bool {
	if n.Label == bootStickLabel {
		return true
	}
	for _, child := range n.Children {
		if containsBoot(child) {
			return true
		}
	}
	return false
}
func (m *Manager) readOnly(dir string) bool {
	data, _ := os.ReadFile(m.mountsFile)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.ReplaceAll(fields[1], `\040`, " ") == dir {
			return slices.Contains(strings.Split(fields[3], ","), "ro")
		}
	}
	return false
}
