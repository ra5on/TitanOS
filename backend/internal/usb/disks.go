package usb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
)

var (
	// ErrNotFormattable means the drive is in use or protected; Disk.Reason
	// says why.
	ErrNotFormattable = errors.New("usb: this drive cannot be formatted now")
	// ErrFormatInvalid is a filesystem, label or scheme that does not fit.
	ErrFormatInvalid = errors.New("usb: invalid format request")
)

// Disk is a whole unmanaged drive, also one without any filesystem: a new
// SSD, a disk with only a partition table or with leftovers from another
// computer.
type Disk struct {
	// ID names this drive with its current content; it changes when the
	// drive is formatted or another one takes its place.
	ID        string `json:"id"`
	Device    string `json:"device"`
	Model     string `json:"model"`
	Serial    string `json:"serial"`
	Size      uint64 `json:"size"`
	Transport string `json:"transport"`
	// Kind is hdd, ssd, nvme, sd or removable (USB sticks and card readers).
	Kind     string `json:"kind"`
	ReadOnly bool   `json:"readOnly"`
	// Table is the partition table: gpt, dos or empty.
	Table      string      `json:"table"`
	Partitions []Partition `json:"partitions"`
	// Formattable is false with a Reason: readonly, mounted (eject first),
	// inuse (mounted elsewhere, swap, an active RAID or encryption), claimed
	// (a VM or a disk test) or formatting.
	Formattable bool   `json:"formattable"`
	Reason      string `json:"reason,omitempty"`
	Claimed     string `json:"claimed,omitempty"`

	path    string
	discard bool
	node    node
}

// Partition is what a disk holds, as far as lsblk can tell.
type Partition struct {
	Device string `json:"device"`
	FSType string `json:"fsType"`
	Label  string `json:"label"`
	Size   uint64 `json:"size"`
	// Mounted is true for a mount anywhere, also outside of TitanOS' drives.
	Mounted bool `json:"mounted"`
}

// FormatRequest says how a disk is formatted.
type FormatRequest struct {
	// Filesystem is exfat, vfat (FAT32), ntfs, ext4, xfs or btrfs.
	Filesystem string `json:"filesystem"`
	Label      string `json:"label"`
	// Scheme is auto, mbr or gpt. auto takes MBR for Windows filesystems up
	// to 2 TiB, which TVs, car radios, cameras and consoles read, else GPT.
	Scheme string `json:"scheme"`
	// Mount mounts the new filesystem when it is done.
	Mount bool `json:"mount"`
}

// FormatJob is a running or the last format.
type FormatJob struct {
	Disk       string `json:"disk"`
	Device     string `json:"device"`
	Name       string `json:"name"`
	Filesystem string `json:"filesystem"`
	Label      string `json:"label"`
	Scheme     string `json:"scheme"`
	// Action is empty for a whole-drive format; partition editor jobs say
	// table, create, delete, format, label or grow (see partitions.go).
	Action string `json:"action,omitempty"`
	// Partition is the device of the partition an action is about.
	Partition string `json:"partition,omitempty"`
	// Phase is check, wipe, discard, partition, format, label, resize,
	// mount, done or failed.
	Phase    string     `json:"phase"`
	Error    string     `json:"error,omitempty"`
	Started  time.Time  `json:"started"`
	Finished *time.Time `json:"finished,omitempty"`
	// ID is the filesystem UUID of the new filesystem.
	ID string `json:"id,omitempty"`

	done chan struct{}
}

// Running reports a format that has not ended.
func (j FormatJob) Running() bool { return j.Finished == nil }

const (
	mib = 1 << 20
	gib = 1 << 30
	tib = 1 << 40
	// MBR addresses 2^32 sectors of 512 bytes.
	mbrLimit = 2 * tib
)

type format struct {
	// limit is the label length, in bytes or, for Windows filesystems, in
	// UTF-16 units.
	limit   int
	windows bool
	minSize uint64
	maxSize uint64
	mbrType string
}

var formats = map[string]format{
	"exfat": {limit: 15, windows: true, minSize: 8 * mib, mbrType: "7"},
	// FAT32 above 32 GiB is not made by Windows itself, but read everywhere.
	"vfat":  {limit: 11, windows: true, minSize: 8 * mib, maxSize: mbrLimit, mbrType: "c"},
	"ntfs":  {limit: 32, windows: true, minSize: 8 * mib, mbrType: "7"},
	"ext4":  {limit: 16, minSize: 8 * mib, mbrType: "83"},
	"xfs":   {limit: 12, minSize: 300 * mib, mbrType: "83"},
	"btrfs": {limit: 255, minSize: 256 * mib, mbrType: "83"},
}

const (
	gptWindows = "EBD0A0A2-B9E5-4433-87C0-68B6B72699C7"
	gptLinux   = "0FC63DAF-8483-4772-8E79-3D69D8477DE4"
)

// fatLabelChars are allowed in a FAT label beside A-Z and 0-9.
const fatLabelChars = " !#$%&'()-@^_`{}~"

func (m *Manager) blockDevices(ctx context.Context, args ...string) ([]node, error) {
	output, err := m.runner.Run(ctx, "", "lsblk", append(slices.Clone(lsblkArgs), args...)...)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Blockdevices []node `json:"blockdevices"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return nil, err
	}
	return parsed.Blockdevices, nil
}

// Disks returns the whole drives that could be formatted, with the last
// format. The boot stick, disks of NAS volumes, virtual and network disks
// and empty card readers are left out.
func (m *Manager) Disks(ctx context.Context) ([]Disk, *FormatJob, error) {
	return m.disks(ctx, nil)
}

// disks lists the drives; self is a job that checks its own drive again and
// does not count as running there.
func (m *Manager) disks(ctx context.Context, self *FormatJob) ([]Disk, *FormatJob, error) {
	nodes, err := m.blockDevices(ctx)
	if err != nil {
		return nil, nil, err
	}
	claimed := map[string]string{}
	if m.Claimed != nil {
		claimed = m.Claimed()
	}
	ours := map[string]bool{}
	for device := range m.mounts() {
		ours[device] = true
	}
	m.mu.Lock()
	var job *FormatJob
	running := m.job != nil && m.job.Running() && m.job != self
	if m.job != nil {
		copy := *m.job
		job = &copy
	}
	m.mu.Unlock()
	disks := []Disk{}
	for _, n := range nodes {
		if n.Type != "disk" || n.Size == 0 || !slices.Contains(transports, n.Transport) || containsBoot(n) || m.reservedBelow(n) {
			continue
		}
		d := Disk{
			Device: n.Name, Model: strings.TrimSpace(n.Model), Serial: strings.TrimSpace(n.Serial), Size: n.Size,
			Transport: n.Transport, Kind: diskKind(n), ReadOnly: bool(n.ReadOnly), Table: n.PTType,
			Partitions: []Partition{}, path: n.Name, discard: !bool(n.Rotational) && n.DiscGran > 0, node: n,
		}
		d.ID = diskID(n)
		if n.FSType != "" {
			d.Partitions = append(d.Partitions, Partition{Device: n.Name, FSType: n.FSType, Label: n.Label, Size: n.Size, Mounted: n.Mountpoint != ""})
		}
		for _, child := range n.Children {
			if child.Type == "part" && !(child.UUID != "" && child.UUID == n.UUID) {
				d.Partitions = append(d.Partitions, Partition{Device: child.Name, FSType: child.FSType, Label: child.Label, Size: child.Size, Mounted: child.Mountpoint != "" || anyMounted(child.Children)})
			}
		}
		switch {
		case running && job.Device == n.Name:
			d.Reason = "formatting"
		case claimed[filepath.Base(n.Name)] != "":
			d.Reason, d.Claimed = "claimed", claimed[filepath.Base(n.Name)]
		case bool(n.ReadOnly):
			d.Reason = "readonly"
		case mountedBy(n, func(device string) bool { return !ours[device] }):
			d.Reason = "inuse"
		case holdsMapping(n):
			d.Reason = "inuse"
		case mountedBy(n, func(device string) bool { return ours[device] }):
			d.Reason = "mounted"
		}
		d.Formattable = d.Reason == ""
		disks = append(disks, d)
	}
	slices.SortFunc(disks, func(a, b Disk) int { return strings.Compare(a.Device, b.Device) })
	return disks, job, nil
}

// reservedBelow finds a filesystem or array of a NAS volume anywhere on a
// disk. Those disks are handled under Storage only.
func (m *Manager) reservedBelow(n node) bool {
	if isReserved(n, m.Reserved) {
		return true
	}
	for _, child := range n.Children {
		if m.reservedBelow(child) {
			return true
		}
	}
	return false
}

// mountedBy reports a mount of the disk or anything on it whose device
// matches.
func mountedBy(n node, match func(device string) bool) bool {
	if n.Mountpoint != "" && (n.Mountpoint == "[SWAP]" || match(n.Name)) {
		return true
	}
	for _, child := range n.Children {
		if mountedBy(child, match) {
			return true
		}
	}
	return false
}

// holdsMapping finds an assembled RAID, LVM or opened encryption on a disk:
// the kernel then shows a device of another type below a partition.
func holdsMapping(n node) bool {
	for _, child := range n.Children {
		if child.Type != "part" || holdsMapping(child) {
			return true
		}
	}
	return false
}

func diskKind(n node) string {
	switch {
	case n.Transport == "nvme":
		return "nvme"
	case n.Transport == "mmc" || strings.HasPrefix(filepath.Base(n.Name), "mmcblk"):
		return "sd"
	// usb-storage marks every drive as rotating unless it says otherwise,
	// sticks included; removable is the better hint. USB disks are not
	// removable.
	case bool(n.Removable):
		return "removable"
	case bool(n.Rotational):
		return "hdd"
	}
	return "ssd"
}

// diskID changes with anything that tells two drives apart, including the
// partition table and filesystems: a stick swapped for an identical one in
// the same port is not formatted by a click meant for the other.
func diskID(n node) string {
	h := sha256.New()
	var add func(node)
	add = func(n node) {
		fmt.Fprintf(h, "%s|%s|%s|%s|%d|%s|%s|%s\n", n.Name, n.Model, n.Serial, n.WWN, n.Size, n.PTUUID, n.FSType, n.UUID)
		for _, child := range n.Children {
			add(child)
		}
	}
	add(n)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// CheckFormat validates a request against a disk; it returns the scheme
// that will be used.
func CheckFormat(d Disk, req FormatRequest) (string, error) {
	f, ok := formats[req.Filesystem]
	if !ok {
		return "", fmt.Errorf("%w: unknown filesystem %q", ErrFormatInvalid, req.Filesystem)
	}
	if d.Size < f.minSize {
		return "", fmt.Errorf("%w: the drive is too small for %s", ErrFormatInvalid, req.Filesystem)
	}
	if f.maxSize != 0 && d.Size > f.maxSize {
		return "", fmt.Errorf("%w: %s holds at most 2 TiB", ErrFormatInvalid, req.Filesystem)
	}
	if err := checkLabel(req.Filesystem, req.Label); err != nil {
		return "", err
	}
	scheme := req.Scheme
	switch scheme {
	case "", "auto":
		scheme = "gpt"
		if f.windows && d.Size <= mbrLimit {
			scheme = "mbr"
		}
	case "mbr":
		if d.Size > mbrLimit {
			return "", fmt.Errorf("%w: MBR holds at most 2 TiB", ErrFormatInvalid)
		}
	case "gpt":
	default:
		return "", fmt.Errorf("%w: unknown partition scheme %q", ErrFormatInvalid, req.Scheme)
	}
	return scheme, nil
}

func checkLabel(fs, label string) error {
	f := formats[fs]
	for _, r := range label {
		if unicode.IsControl(r) || (f.windows && strings.ContainsRune(`"*/:<>?\|`, r)) {
			return fmt.Errorf("%w: the name may not contain %q", ErrFormatInvalid, r)
		}
		if fs == "vfat" && !(r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || strings.ContainsRune(fatLabelChars, r)) {
			return fmt.Errorf("%w: a FAT32 name holds only letters A-Z, digits and %s", ErrFormatInvalid, fatLabelChars)
		}
	}
	length := len(label)
	if f.windows && fs != "vfat" {
		length = len(utf16.Encode([]rune(label)))
	}
	if length > f.limit {
		return fmt.Errorf("%w: the name is longer than %d characters", ErrFormatInvalid, f.limit)
	}
	return nil
}

// Format erases a whole drive and puts one partition with a new filesystem
// on it. It returns once the drive was checked again; the work goes on in
// the background and Disks shows how far it is.
func (m *Manager) Format(ctx context.Context, id string, req FormatRequest) error {
	if m.Disabled {
		return ErrDisabled
	}
	if !m.operation.TryLock() {
		return ErrBusy
	}
	started := false
	defer func() {
		if !started {
			m.operation.Unlock()
		}
	}()
	if (m.Busy != nil && m.Busy()) || m.using() {
		return ErrBusy
	}
	disks, _, err := m.Disks(ctx)
	if err != nil {
		return err
	}
	i := slices.IndexFunc(disks, func(d Disk) bool { return d.ID == id })
	if i < 0 {
		return ErrNotFound
	}
	d := disks[i]
	if !d.Formattable {
		return fmt.Errorf("%w: %s", ErrNotFormattable, d.Reason)
	}
	scheme, err := CheckFormat(d, req)
	if err != nil {
		return err
	}
	if req.Filesystem == "vfat" {
		req.Label = strings.ToUpper(req.Label)
	}
	job := &FormatJob{Disk: d.ID, Device: d.path, Name: firstNonEmpty(d.Model, filepath.Base(d.path)), Filesystem: req.Filesystem,
		Label: req.Label, Scheme: scheme, Phase: "wipe", Started: m.now(), done: make(chan struct{})}
	m.mu.Lock()
	m.job = job
	m.mu.Unlock()
	started = true
	slog.Info("formatting drive", "device", d.path, "model", d.Model, "fs", req.Filesystem, "scheme", scheme)
	go func() {
		work, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
		defer cancel()
		newID, err := m.format(work, job, d, req, scheme)
		m.finish(job, newID, err)
	}()
	return nil
}

// finish ends a format or partition job, frees the operation lock and tells
// OnFormatted. The lock is free before job.done closes, so whoever waited for
// the job can start the next one right away.
func (m *Manager) finish(job *FormatJob, newID string, err error) {
	m.mu.Lock()
	now := m.now()
	job.Finished, job.ID = &now, newID
	if err != nil {
		job.Phase, job.Error = "failed", err.Error()
		slog.Warn("drive job failed", "device", job.Device, "action", job.Action, "err", err)
	} else {
		job.Phase = "done"
		slog.Info("drive job done", "device", job.Device, "action", job.Action, "partition", job.Partition, "fs", job.Filesystem, "uuid", newID)
	}
	ended := *job
	m.mu.Unlock()
	m.operation.Unlock()
	if m.OnFormatted != nil {
		m.OnFormatted(ended)
	}
	close(job.done)
}

func (m *Manager) now() time.Time { return time.Now().UTC() }

func (m *Manager) phase(job *FormatJob, phase string) {
	m.mu.Lock()
	job.Phase = phase
	m.mu.Unlock()
}

func (m *Manager) format(ctx context.Context, job *FormatJob, d Disk, req FormatRequest, scheme string) (string, error) {
	f := formats[req.Filesystem]
	// Old signatures on partitions would otherwise show up again when a new
	// partition starts at the same place.
	for _, part := range slices.Backward(d.Partitions) {
		if part.Device != d.path {
			if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", part.Device); err != nil {
				return "", err
			}
		}
	}
	if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", d.path); err != nil {
		return "", err
	}
	// ext4, XFS and Btrfs trim the SSD themselves while they are made.
	if d.discard && f.windows {
		m.phase(job, "discard")
		trim, cancel := context.WithTimeout(ctx, 10*time.Minute)
		if _, err := m.runner.Run(trim, "", "blkdiscard", d.path); err != nil {
			// USB adapters often claim TRIM and cannot do it; the new
			// filesystem works without.
			slog.Info("drive not trimmed", "device", d.path, "err", err)
		}
		cancel()
	}
	m.phase(job, "partition")
	table := "label: gpt\n,,"
	if f.windows {
		table += gptWindows + "\n"
	} else {
		table += gptLinux + "\n"
	}
	if scheme == "mbr" {
		kind := f.mbrType
		if req.Filesystem == "vfat" && !fat32(d.Size) {
			kind = "e" // FAT16 with LBA
		}
		table = "label: dos\n,," + kind + "\n"
	}
	if _, err := m.runner.Run(ctx, table, "sfdisk", "--quiet", "--wipe", "always", "--wipe-partitions", "always", d.path); err != nil {
		return "", err
	}
	part, err := m.newPartition(ctx, d.path)
	if err != nil {
		return "", err
	}
	m.phase(job, "format")
	if _, err := m.runner.Run(ctx, "", "mkfs."+req.Filesystem, mkfsArgs(req, d.Size, part)...); err != nil {
		return "", err
	}
	_, _ = m.runner.Run(ctx, "", "udevadm", "settle", "--timeout=10") // only waits for the kernel and udev; the next step checks the result
	devices, err := m.List(ctx)
	if err != nil {
		return "", err
	}
	i := slices.IndexFunc(devices, func(device Device) bool { return device.path == part })
	if i < 0 {
		return "", fmt.Errorf("the new filesystem on %s does not show up", part)
	}
	id := devices[i].ID
	if req.Mount {
		m.phase(job, "mount")
		if err := m.mount(ctx, id); err != nil {
			return id, err
		}
	}
	return id, nil
}

// newPartition waits for the kernel to show the partition sfdisk made.
func (m *Manager) newPartition(ctx context.Context, disk string) (string, error) {
	_, _ = m.runner.Run(ctx, "", "udevadm", "settle", "--timeout=10") // only waits for the kernel and udev; the next step checks the result
	for try := 0; ; try++ {
		nodes, err := m.blockDevices(ctx, disk)
		if err == nil && len(nodes) == 1 {
			var parts []string
			for _, child := range nodes[0].Children {
				if child.Type == "part" {
					parts = append(parts, child.Name)
				}
			}
			if len(parts) == 1 {
				return parts[0], nil
			}
		}
		if try == 20 {
			return "", fmt.Errorf("the new partition on %s does not show up", disk)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(m.retryPause()):
		}
	}
}

func (m *Manager) retryPause() time.Duration {
	if m.pause != 0 {
		return m.pause
	}
	return 250 * time.Millisecond
}

// fat32 is what Windows and cameras use from 512 MiB; below, FAT16.
func fat32(size uint64) bool { return size >= 512*mib }

func mkfsArgs(req FormatRequest, size uint64, part string) []string {
	var args []string
	switch req.Filesystem {
	case "vfat":
		// Small cards get FAT16, which mkfs picks itself.
		if fat32(size) {
			args = append(args, "-F", "32")
		}
		if req.Label != "" {
			args = append(args, "-n", req.Label)
		}
	case "exfat":
		if req.Label != "" {
			args = append(args, "-L", req.Label)
		}
	case "ntfs":
		// A full format would write zeros over the whole drive.
		args = append(args, "--quick")
		if req.Label != "" {
			args = append(args, "--label", req.Label)
		}
	case "ext4":
		args = append(args, "-F", "-q", "-m", "0")
		if req.Label != "" {
			args = append(args, "-L", req.Label)
		}
	case "xfs", "btrfs":
		args = append(args, "-f", "-q")
		if req.Label != "" {
			args = append(args, "-L", req.Label)
		}
	}
	return append(args, part)
}

// waitFormat waits for the last format to end; for tests.
func (m *Manager) waitFormat() {
	m.mu.Lock()
	job := m.job
	m.mu.Unlock()
	if job != nil {
		<-job.done
	}
}
