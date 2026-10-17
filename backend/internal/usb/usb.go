// Package usb shows drives that are plugged in over USB and mounts them on
// request, so that their files can be browsed and copied in the file manager.
//
// Nothing is mounted by itself and nothing is stored: which drive is plugged
// in and which is mounted is read from the system each time. A drive is named
// by its filesystem UUID, never by sdb1, which changes between plugs.
package usb

import (
	"context"
	"encoding/json"
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
)

var (
	// ErrDisabled means titand runs in development mode and mounts nothing.
	ErrDisabled = errors.New("usb: not available in development mode")
	// ErrNotFound means the drive is not plugged in (any more).
	ErrNotFound = errors.New("usb: no such drive")
	// ErrUnsupported means the filesystem of the drive cannot be mounted.
	ErrUnsupported = errors.New("usb: this filesystem cannot be mounted")
	// ErrBusy means something still uses the drive.
	ErrBusy = errors.New("usb: the drive is in use")
	// ErrMounted and ErrNotMounted report a request that makes no sense.
	ErrMounted    = errors.New("usb: the drive is mounted already")
	ErrNotMounted = errors.New("usb: the drive is not mounted")
)

// filesystems that can be mounted, with the type mount is told and options.
var filesystems = map[string]struct{ kind, options string }{
	"vfat":  {"vfat", "iocharset=utf8,flush"},
	"exfat": {"exfat", ""},
	"ntfs":  {"ntfs3", ""},
	"ext2":  {"ext2", ""},
	"ext3":  {"ext3", ""},
	"ext4":  {"ext4", ""},
	"xfs":   {"xfs", ""},
	"btrfs": {"btrfs", ""},
}

// Device is a filesystem on a drive plugged in over USB.
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
	Supported bool `json:"supported"`
	Mounted   bool `json:"mounted"`
	// Dir is where the drive is mounted; empty if it is not.
	Dir string `json:"-"`
	// Mount is the name it is mounted under.
	Mount string `json:"mount"`

	path string
}

type node struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Size       uint64 `json:"size"`
	Transport  string `json:"tran"`
	Model      string `json:"model"`
	FSType     string `json:"fstype"`
	Label      string `json:"label"`
	UUID       string `json:"uuid"`
	Mountpoint string `json:"mountpoint"`
	Children   []node `json:"children"`
}

var lsblkArgs = []string{"--json", "--bytes", "--paths", "--output", "NAME,TYPE,SIZE,TRAN,MODEL,FSTYPE,LABEL,UUID,MOUNTPOINT"}

// Manager lists and mounts drives.
type Manager struct {
	runner run.Runner
	// root is where drives are mounted; mountsFile lists what is mounted.
	root, mountsFile string
	// Disabled refuses to mount anything: in development mode titand runs on
	// the developer's own machine.
	Disabled bool
	// Reserved reports a filesystem UUID that belongs to a volume of the NAS.
	Reserved func(uuid string) bool
	// OnPlug is told of each drive that appeared since the last look.
	OnPlug func(Device)

	mu sync.Mutex
	// seen are the drives of the last look; nil until there was one.
	seen map[string]bool
}

// NewManager returns a manager for the real system.
func NewManager() *Manager {
	return &Manager{runner: run.Exec{}, root: "/run/titanos-usb", mountsFile: "/proc/self/mounts"}
}

// bootStickLabel is the label of the stick TitanOS runs from.
const bootStickLabel = "TITANOS"

// collect adds the usable filesystems below a drive.
func collect(n node, transport string, out *[]Device, reserved func(string) bool) {
	if n.Transport != "" {
		transport = n.Transport
	}
	if n.FSType != "" && n.UUID != "" && (n.Type == "part" || n.Type == "disk") && transport == "usb" {
		// RAID members and swap are not data; the boot stick stays out of reach.
		if n.FSType != "linux_raid_member" && n.FSType != "swap" && n.FSType != "crypto_LUKS" && n.FSType != "LVM2_member" &&
			n.Label != bootStickLabel && (reserved == nil || !reserved(n.UUID)) {
			_, supported := filesystems[n.FSType]
			*out = append(*out, Device{ID: n.UUID, Label: n.Label, Model: strings.TrimSpace(n.Model), FSType: n.FSType, Size: n.Size, Supported: supported, path: n.Name})
		}
	}
	for _, child := range n.Children {
		child.Model = firstNonEmpty(child.Model, n.Model)
		collect(child, transport, out, reserved)
	}
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
	output, err := m.runner.Run(ctx, "", "lsblk", lsblkArgs...)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Blockdevices []node `json:"blockdevices"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return nil, err
	}
	var devices []Device
	for _, n := range parsed.Blockdevices {
		collect(n, "", &devices, m.Reserved)
	}
	mounted := m.mounts()
	for i := range devices {
		d := &devices[i]
		d.Name = firstNonEmpty(d.Label, d.Model, d.FSType)
		if dir, ok := mounted[d.path]; ok {
			d.Mounted, d.Dir, d.Mount = true, dir, filepath.Base(dir)
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
	for _, d := range devices {
		if d.ID == id {
			return d, nil
		}
	}
	return Device{}, ErrNotFound
}

// Mounted is a drive that is mounted, found without asking lsblk.
type Mounted struct {
	// Mount is the folder name; Name is what to show.
	Mount, Name, Dir string
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
		drives = append(drives, Mounted{Mount: name, Name: shown, Dir: dir})
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
		os.Remove(dir)
		return fmt.Errorf("%w: %v", ErrUnsupported, err)
	}
	slog.Info("usb drive mounted", "name", d.Name, "fs", d.FSType)
	return nil
}

// Eject writes out what is waiting and unmounts a drive.
func (m *Manager) Eject(ctx context.Context, id string) error {
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
		listing := blockListing()
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
func blockListing() string {
	var names []string
	disks, _ := os.ReadDir("/sys/block")
	for _, disk := range disks {
		names = append(names, disk.Name())
		parts, _ := os.ReadDir(filepath.Join("/sys/block", disk.Name()))
		for _, part := range parts {
			if strings.HasPrefix(part.Name(), disk.Name()) {
				names = append(names, part.Name())
			}
		}
	}
	slices.Sort(names)
	return strings.Join(names, ",")
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
		if !first && !m.seen[d.ID] {
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
