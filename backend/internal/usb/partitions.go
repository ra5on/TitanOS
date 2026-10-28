package usb

// The partition editor: partitions and free space of one unmanaged drive,
// and the changes it allows. It reuses what formatting a whole drive checks
// (see disks.go): the same list of drives and exclusions, the same ID that
// changes with the content, the same job and lock. Every action checks the
// drive again in the background right before the first write.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// ErrPartitionUnsupported is an action the filesystem of a partition or the
// partition table does not allow: growing FAT, renaming without the tool, a
// table with logical partitions.
var ErrPartitionUnsupported = errors.New("usb: this partition cannot be changed this way")

// Layout is the partition table of a drive with its free space.
type Layout struct {
	Disk Disk `json:"disk"`
	// Table is gpt, dos or empty for a drive without a partition table.
	Table      string `json:"table"`
	SectorSize uint64 `json:"sectorSize"`
	// Reason is set when no partition may be changed: readonly, claimed or
	// formatting; extended for an MBR table with logical partitions, where
	// only formatting, renaming and a new table are offered.
	Reason     string     `json:"reason,omitempty"`
	Partitions []Slot     `json:"partitions"`
	Gaps       []Gap      `json:"gaps"`
	Job        *FormatJob `json:"job"`

	first, last uint64 // usable sectors, last included
}

// Slot is one partition of the table.
type Slot struct {
	Number int    `json:"number"`
	Device string `json:"device"`
	// Start and Size are in bytes.
	Start uint64 `json:"start"`
	Size  uint64 `json:"size"`
	// Type is the GPT type GUID or the MBR type in hex.
	Type    string `json:"type"`
	Name    string `json:"name,omitempty"`
	FSType  string `json:"fsType"`
	Label   string `json:"label"`
	UUID    string `json:"uuid"`
	Mounted bool   `json:"mounted"`
	// Reason is set when the partition cannot be changed: mounted (eject
	// first), inuse (mounted elsewhere, swap, RAID, LVM, encryption),
	// extended, or one of the drive's reasons.
	Reason string `json:"reason,omitempty"`
	// GrowBy is the free space right after the partition it can take, in
	// bytes; 0 when there is none or the filesystem cannot grow.
	GrowBy uint64 `json:"growBy"`
	// Renamable says a tool sets the label of this filesystem.
	Renamable bool `json:"renamable"`
}

// Gap is free space between partitions, aligned to 1 MiB, at least 1 MiB.
type Gap struct {
	Start uint64 `json:"start"`
	Size  uint64 `json:"size"`
	// After is the partition right before the gap, 0 at the start.
	After int `json:"after"`
}

// TableRequest asks for a new, empty partition table.
type TableRequest struct {
	// Scheme is gpt or dos (also mbr).
	Scheme string `json:"scheme"`
}

// PartitionRequest creates, formats, renames or grows a partition.
type PartitionRequest struct {
	// Start is where a new partition begins, in bytes: inside a gap and on a
	// 1 MiB boundary.
	Start uint64 `json:"start"`
	// Size is the size of a new partition or the new size of a grown one, in
	// bytes, rounded down to 1 MiB; 0 takes the rest of the free space.
	Size uint64 `json:"size"`
	// Filesystem is optional when creating; empty leaves the partition
	// unformatted.
	Filesystem string `json:"filesystem"`
	Label      string `json:"label"`
}

// extendedTypes are MBR containers of logical partitions.
var extendedTypes = []string{"5", "f", "85"}

// growable filesystems grow with their partition; "" is a partition without
// a filesystem.
var growable = map[string]string{"": "", "ext2": "resize2fs", "ext3": "resize2fs", "ext4": "resize2fs", "xfs": "xfs_growfs", "btrfs": "btrfs", "ntfs": "ntfsresize"}

// labelTools set the label of an unmounted filesystem.
var labelTools = map[string]string{"exfat": "exfatlabel", "vfat": "fatlabel", "ntfs": "ntfslabel", "ext2": "e2label", "ext3": "e2label", "ext4": "e2label", "xfs": "xfs_admin", "btrfs": "btrfs"}

type sfdiskOutput struct {
	PartitionTable struct {
		Label      string `json:"label"`
		FirstLBA   uint64 `json:"firstlba"`
		LastLBA    uint64 `json:"lastlba"`
		SectorSize uint64 `json:"sectorsize"`
		Partitions []struct {
			Node  string `json:"node"`
			Start uint64 `json:"start"`
			Size  uint64 `json:"size"`
			Type  string `json:"type"`
			Name  string `json:"name"`
		} `json:"partitions"`
	} `json:"partitiontable"`
}

// Partitions returns the partition table of a drive from the list of Disks.
func (m *Manager) Partitions(ctx context.Context, id string) (*Layout, error) {
	return m.layout(ctx, id, nil)
}

func (m *Manager) layout(ctx context.Context, id string, self *FormatJob) (*Layout, error) {
	disks, job, err := m.disks(ctx, self)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(disks, func(d Disk) bool { return d.ID == id })
	if i < 0 {
		return nil, ErrNotFound
	}
	d := disks[i]
	l := &Layout{Disk: d, Table: d.Table, SectorSize: 512, Partitions: []Slot{}, Gaps: []Gap{}, Job: job}
	switch d.Reason {
	case "readonly", "claimed", "formatting":
		l.Reason = d.Reason
	}
	if d.Table == "" {
		return l, nil
	}
	output, err := m.runner.Run(ctx, "", "sfdisk", "--json", d.path)
	var parsed sfdiskOutput
	if err == nil {
		err = json.Unmarshal([]byte(output), &parsed)
	}
	if err != nil {
		// A damaged table is shown as unknown; only a new one is offered.
		slog.Info("partition table not readable", "device", d.path, "err", err)
		l.Reason = firstNonEmpty(l.Reason, "table")
		return l, nil
	}
	pt := parsed.PartitionTable
	l.Table = pt.Label
	if pt.SectorSize != 0 {
		l.SectorSize = pt.SectorSize
	}
	ss := l.SectorSize
	sectors := d.Size / ss
	switch pt.Label {
	case "gpt":
		l.first, l.last = pt.FirstLBA, min(pt.LastLBA, sectors-1)
	case "dos":
		// MBR counts sectors with 32 bits.
		l.first, l.last = 1, min(sectors, 1<<32)-1
	default:
		// sun, sgi, bsd …: shown, but only a new table is offered.
		l.Reason = firstNonEmpty(l.Reason, "table")
	}
	children, ours := map[string]node{}, m.mounts()
	for _, child := range d.node.Children {
		children[child.Name] = child
	}
	for _, p := range pt.Partitions {
		number, err := strconv.Atoi(strings.TrimPrefix(strings.TrimPrefix(p.Node, d.path), "p"))
		if err != nil {
			return nil, fmt.Errorf("sfdisk: unknown partition %s", p.Node)
		}
		s := Slot{Number: number, Device: p.Node, Start: p.Start * ss, Size: p.Size * ss, Type: strings.ToUpper(p.Type), Name: p.Name}
		if pt.Label == "dos" {
			s.Type = strings.ToLower(p.Type)
		}
		if child, ok := children[p.Node]; ok {
			s.FSType, s.Label, s.UUID = child.FSType, child.Label, child.UUID
			s.Mounted = child.Mountpoint != "" || anyMounted(child.Children)
			s.Reason = slotReason(child, ours)
		}
		if pt.Label == "dos" && slices.Contains(extendedTypes, s.Type) {
			l.Reason, s.Reason = firstNonEmpty(l.Reason, "extended"), "extended"
		}
		l.Partitions = append(l.Partitions, s)
	}
	for i := range l.Partitions {
		if l.Reason != "" && l.Reason != "extended" {
			l.Partitions[i].Reason = l.Reason
		}
	}
	slices.SortFunc(l.Partitions, func(a, b Slot) int { return int(a.Start/ss) - int(b.Start/ss) })
	if l.Reason != "extended" && l.Reason != "table" {
		l.Gaps = gaps(l.Partitions, l.first, l.last, ss)
	}
	for i := range l.Partitions {
		s := &l.Partitions[i]
		_, s.Renamable = labelTools[s.FSType]
		if l.Reason != "" || s.Reason != "" {
			continue
		}
		if _, ok := growable[s.FSType]; ok {
			for _, g := range l.Gaps {
				if g.After == s.Number {
					s.GrowBy = g.Start + g.Size - (s.Start + s.Size)
				}
			}
		}
	}
	return l, nil
}

// slotReason says why a partition is not free: swap, a mount, or a device
// on top of it such as an assembled RAID or opened encryption.
func slotReason(n node, ours map[string]string) string {
	switch {
	case mountedBy(n, func(device string) bool { _, mine := ours[device]; return !mine }):
		return "inuse"
	case slices.ContainsFunc(n.Children, func(c node) bool { return c.Type != "part" }):
		return "inuse"
	case mountedBy(n, func(device string) bool { _, mine := ours[device]; return mine }):
		return "mounted"
	}
	return ""
}

// alignment is 1 MiB in sectors.
func alignment(ss uint64) uint64 { return max(1, mib/ss) }

func alignUp(v, a uint64) uint64   { return (v + a - 1) / a * a }
func alignDown(v, a uint64) uint64 { return v / a * a }

// gaps finds the free space of at least 1 MiB between partitions, from the
// first to the last usable sector. Starts and ends are aligned to 1 MiB.
func gaps(parts []Slot, first, last, ss uint64) []Gap {
	a := alignment(ss)
	out := []Gap{}
	add := func(from, to uint64, after int) {
		start, end := alignUp(from, a), alignDown(to, a)
		if end > start && (end-start)*ss >= mib {
			out = append(out, Gap{Start: start * ss, Size: (end - start) * ss, After: after})
		}
	}
	cursor, after := first, 0
	for _, p := range parts {
		start := p.Start / ss
		if start > cursor {
			add(cursor, start, after)
		}
		if end := start + p.Size/ss; end > cursor {
			cursor, after = end, p.Number
		}
	}
	add(cursor, last+1, after)
	return out
}

// partitionDevice names partition n of a disk the way the kernel does:
// sdb1, but nvme0n1p1 and mmcblk0p1.
func partitionDevice(disk string, n int) string {
	if last := disk[len(disk)-1]; last >= '0' && last <= '9' {
		return fmt.Sprintf("%sp%d", disk, n)
	}
	return fmt.Sprintf("%s%d", disk, n)
}

// partitionType is the type a new or formatted partition gets: Windows
// filesystems are basic data, which Windows and cameras look for.
func partitionType(table, fs string, size uint64) string {
	f, known := formats[fs]
	if table == "gpt" {
		if known && f.windows {
			return gptWindows
		}
		return gptLinux
	}
	if !known {
		return "83"
	}
	if fs == "vfat" && !fat32(size) {
		return "e"
	}
	return f.mbrType
}

func (l *Layout) slot(number int) (Slot, error) {
	i := slices.IndexFunc(l.Partitions, func(s Slot) bool { return s.Number == number })
	if i < 0 {
		return Slot{}, ErrNotFound
	}
	return l.Partitions[i], nil
}

// free returns a partition that may be changed.
func (l *Layout) free(number int) (Slot, error) {
	s, err := l.slot(number)
	if err != nil {
		return s, err
	}
	if s.Reason != "" {
		return s, fmt.Errorf("%w: %s", ErrNotFormattable, s.Reason)
	}
	return s, nil
}

// changeable refuses table changes on drives that do not allow them.
func (l *Layout) changeable() error {
	switch l.Reason {
	case "":
		return nil
	case "extended", "table":
		return fmt.Errorf("%w: %s", ErrPartitionUnsupported, l.Reason)
	}
	return fmt.Errorf("%w: %s", ErrNotFormattable, l.Reason)
}

func checkFilesystem(fs, label string, size uint64) error {
	f, ok := formats[fs]
	if !ok {
		return fmt.Errorf("%w: unknown filesystem %q", ErrFormatInvalid, fs)
	}
	if size < f.minSize {
		return fmt.Errorf("%w: the partition is too small for %s", ErrFormatInvalid, fs)
	}
	if f.maxSize != 0 && size > f.maxSize {
		return fmt.Errorf("%w: %s holds at most 2 TiB", ErrFormatInvalid, fs)
	}
	return checkLabel(fs, label)
}

// partitionAction is one change; check runs when it is asked for and again
// in the background right before the first write, on a fresh look at the
// drive. It returns the device the change is about.
type partitionAction struct {
	name       string
	filesystem string
	label      string
	check      func(ctx context.Context, l *Layout) (string, error)
	work       func(ctx context.Context, job *FormatJob, l *Layout) error
}

func (m *Manager) startPartitionJob(ctx context.Context, id string, a partitionAction) error {
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
	if m.Busy != nil && m.Busy() {
		return ErrBusy
	}
	l, err := m.layout(ctx, id, nil)
	if err != nil {
		return err
	}
	target, err := a.check(ctx, l)
	if err != nil {
		return err
	}
	d := l.Disk
	job := &FormatJob{Disk: d.ID, Device: d.path, Name: firstNonEmpty(d.Model, filepath.Base(d.path)), Action: a.name, Partition: target,
		Filesystem: a.filesystem, Label: a.label, Scheme: l.Table, Phase: "check", Started: m.now(), done: make(chan struct{})}
	m.mu.Lock()
	m.job = job
	m.mu.Unlock()
	started = true
	slog.Info("changing partitions", "device", d.path, "model", d.Model, "action", a.name, "target", target)
	go func() {
		work, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
		defer cancel()
		// The drive may have been swapped or changed since the click.
		fresh, err := m.layout(work, id, job)
		if err == nil {
			_, err = a.check(work, fresh)
		}
		if err == nil {
			err = a.work(work, job, fresh)
		}
		m.finish(job, "", err)
	}()
	return nil
}

// NewTable replaces the partition table of a whole drive with an empty one.
// Everything on the drive is lost.
func (m *Manager) NewTable(ctx context.Context, id string, req TableRequest) error {
	scheme := req.Scheme
	if scheme == "mbr" {
		scheme = "dos"
	}
	return m.startPartitionJob(ctx, id, partitionAction{
		name: "table",
		check: func(_ context.Context, l *Layout) (string, error) {
			if scheme != "gpt" && scheme != "dos" {
				return "", fmt.Errorf("%w: unknown partition scheme %q", ErrFormatInvalid, req.Scheme)
			}
			if scheme == "dos" && l.Disk.Size > mbrLimit {
				return "", fmt.Errorf("%w: MBR holds at most 2 TiB", ErrFormatInvalid)
			}
			if !l.Disk.Formattable {
				return "", fmt.Errorf("%w: %s", ErrNotFormattable, l.Disk.Reason)
			}
			return l.Disk.path, nil
		},
		work: func(ctx context.Context, job *FormatJob, l *Layout) error {
			m.phase(job, "wipe")
			for _, part := range slices.Backward(l.Disk.Partitions) {
				if part.Device != l.Disk.path {
					if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", part.Device); err != nil {
						return err
					}
				}
			}
			if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", l.Disk.path); err != nil {
				return err
			}
			m.phase(job, "partition")
			if _, err := m.runner.Run(ctx, "label: "+scheme+"\n", "sfdisk", "--quiet", "--wipe", "always", "--wipe-partitions", "always", l.Disk.path); err != nil {
				return err
			}
			m.settle(ctx, l.Disk.path, "-u")
			return nil
		},
	})
}

// CreatePartition makes a partition in free space, optionally with a new
// filesystem. Other partitions stay as they are, also mounted ones.
func (m *Manager) CreatePartition(ctx context.Context, id string, req PartitionRequest) error {
	var number int
	var size uint64
	if req.Filesystem == "vfat" {
		req.Label = strings.ToUpper(req.Label)
	}
	return m.startPartitionJob(ctx, id, partitionAction{
		name: "create", filesystem: req.Filesystem, label: req.Label,
		check: func(_ context.Context, l *Layout) (string, error) {
			if err := l.changeable(); err != nil {
				return "", err
			}
			if req.Start%mib != 0 || req.Start%l.SectorSize != 0 {
				return "", fmt.Errorf("%w: a partition starts on a 1 MiB boundary", ErrFormatInvalid)
			}
			i := slices.IndexFunc(l.Gaps, func(g Gap) bool { return g.Start <= req.Start && req.Start < g.Start+g.Size })
			if i < 0 {
				return "", fmt.Errorf("%w: there is no free space there", ErrFormatInvalid)
			}
			end := l.Gaps[i].Start + l.Gaps[i].Size
			size = end - req.Start
			if req.Size != 0 {
				size = alignDown(req.Size, mib)
			}
			if size < mib || req.Start+size > end {
				return "", fmt.Errorf("%w: the partition does not fit into the free space", ErrFormatInvalid)
			}
			used := map[int]bool{}
			for _, s := range l.Partitions {
				used[s.Number] = true
			}
			limit := 128
			if l.Table == "dos" {
				limit = 4
			}
			number = 0
			for n := 1; n <= limit; n++ {
				if !used[n] {
					number = n
					break
				}
			}
			if number == 0 {
				return "", fmt.Errorf("%w: the partition table holds no more partitions", ErrPartitionUnsupported)
			}
			if req.Filesystem != "" {
				if err := checkFilesystem(req.Filesystem, req.Label, size); err != nil {
					return "", err
				}
			} else if req.Label != "" {
				return "", fmt.Errorf("%w: a name needs a filesystem", ErrFormatInvalid)
			}
			return partitionDevice(l.Disk.path, number), nil
		},
		work: func(ctx context.Context, job *FormatJob, l *Layout) error {
			ss := l.SectorSize
			m.phase(job, "partition")
			script := fmt.Sprintf("start=%d, size=%d, type=%s\n", req.Start/ss, size/ss, partitionType(l.Table, req.Filesystem, size))
			if _, err := m.runner.Run(ctx, script, "sfdisk", "--quiet", "--no-reread", "--wipe-partitions", "always", "-N", strconv.Itoa(number), l.Disk.path); err != nil {
				return err
			}
			part := partitionDevice(l.Disk.path, number)
			m.settle(ctx, l.Disk.path, "-a", "--nr", strconv.Itoa(number))
			if err := m.waitNode(ctx, l.Disk.path, part); err != nil {
				return err
			}
			if req.Filesystem == "" {
				return nil
			}
			m.phase(job, "format")
			return m.makeFilesystem(ctx, req.Filesystem, req.Label, size, part)
		},
	})
}

// DeletePartition removes a partition from the table. Its data are lost;
// the partition must not be in use.
func (m *Manager) DeletePartition(ctx context.Context, id string, number int) error {
	return m.startPartitionJob(ctx, id, partitionAction{
		name: "delete",
		check: func(_ context.Context, l *Layout) (string, error) {
			if err := l.changeable(); err != nil {
				return "", err
			}
			s, err := l.free(number)
			return s.Device, err
		},
		work: func(ctx context.Context, job *FormatJob, l *Layout) error {
			m.phase(job, "partition")
			if _, err := m.runner.Run(ctx, "", "sfdisk", "--quiet", "--no-reread", "--delete", l.Disk.path, strconv.Itoa(number)); err != nil {
				return err
			}
			m.settle(ctx, l.Disk.path, "-d", "--nr", strconv.Itoa(number))
			return nil
		},
	})
}

// FormatPartition puts a new filesystem on one partition and sets the
// partition type to match it.
func (m *Manager) FormatPartition(ctx context.Context, id string, number int, req PartitionRequest) error {
	if req.Filesystem == "vfat" {
		req.Label = strings.ToUpper(req.Label)
	}
	return m.startPartitionJob(ctx, id, partitionAction{
		name: "format", filesystem: req.Filesystem, label: req.Label,
		check: func(_ context.Context, l *Layout) (string, error) {
			s, err := l.free(number)
			if err != nil {
				return "", err
			}
			if s.Reason == "" && l.Table == "dos" && slices.Contains(extendedTypes, s.Type) {
				return "", fmt.Errorf("%w: extended", ErrPartitionUnsupported)
			}
			if l.Table == "dos" && req.Filesystem == "vfat" && fat32(s.Size) != fat32(s.Size) {
				return "", nil
			}
			return s.Device, checkFilesystem(req.Filesystem, req.Label, s.Size)
		},
		work: func(ctx context.Context, job *FormatJob, l *Layout) error {
			s, err := l.slot(number)
			if err != nil {
				return err
			}
			m.phase(job, "wipe")
			if _, err := m.runner.Run(ctx, "", "wipefs", "--all", "--force", s.Device); err != nil {
				return err
			}
			if kind := partitionType(l.Table, req.Filesystem, s.Size); l.Table != "" && !strings.EqualFold(kind, s.Type) {
				m.phase(job, "partition")
				if _, err := m.runner.Run(ctx, "", "sfdisk", "--quiet", "--no-reread", "--part-type", l.Disk.path, strconv.Itoa(number), kind); err != nil {
					return err
				}
				m.settle(ctx, l.Disk.path, "-u", "--nr", strconv.Itoa(number))
			}
			m.phase(job, "format")
			return m.makeFilesystem(ctx, req.Filesystem, req.Label, s.Size, s.Device)
		},
	})
}

// SetLabel renames the filesystem of an unmounted partition.
func (m *Manager) SetLabel(ctx context.Context, id string, number int, label string) error {
	var fs string
	return m.startPartitionJob(ctx, id, partitionAction{
		name: "label", label: label,
		check: func(_ context.Context, l *Layout) (string, error) {
			s, err := l.free(number)
			if err != nil {
				return "", err
			}
			fs = s.FSType
			tool, ok := labelTools[fs]
			if !ok || !m.available(tool) {
				return "", fmt.Errorf("%w: no tool renames %s", ErrPartitionUnsupported, firstNonEmpty(fs, "this partition"))
			}
			if label == "" {
				return "", fmt.Errorf("%w: the name is empty", ErrFormatInvalid)
			}
			return s.Device, checkLabel(labelRules(fs), label)
		},
		work: func(ctx context.Context, job *FormatJob, l *Layout) error {
			s, err := l.slot(number)
			if err != nil {
				return err
			}
			m.phase(job, "label")
			name := label
			if fs == "vfat" {
				name = strings.ToUpper(label)
			}
			tool, args := labelTools[fs], []string{s.Device, name}
			switch fs {
			case "xfs":
				args = []string{"-L", name, s.Device}
			case "btrfs":
				args = []string{"filesystem", "label", s.Device, name}
			}
			if _, err := m.runner.Run(ctx, "", tool, args...); err != nil {
				return err
			}
			m.runner.Run(ctx, "", "udevadm", "settle", "--timeout=10")
			return nil
		},
	})
}

// labelRules are the label rules of the filesystem family.
func labelRules(fs string) string {
	if fs == "ext2" || fs == "ext3" {
		return "ext4"
	}
	return fs
}

// GrowPartition moves the end of a partition into the free space right
// after it and grows its filesystem. Nothing is ever shrunk.
func (m *Manager) GrowPartition(ctx context.Context, id string, number int, req PartitionRequest) error {
	var newSize uint64
	var fs string
	return m.startPartitionJob(ctx, id, partitionAction{
		name: "grow",
		check: func(ctx context.Context, l *Layout) (string, error) {
			if err := l.changeable(); err != nil {
				return "", err
			}
			s, err := l.free(number)
			if err != nil {
				return "", err
			}
			fs = s.FSType
			tool, ok := growable[fs]
			if !ok {
				return "", fmt.Errorf("%w: %s cannot grow", ErrPartitionUnsupported, fs)
			}
			if tool != "" && !m.available(tool) {
				return "", fmt.Errorf("%w: %s is missing", ErrPartitionUnsupported, tool)
			}
			if s.GrowBy == 0 {
				return "", fmt.Errorf("%w: there is no free space right after the partition", ErrFormatInvalid)
			}
			// An end on a 1 MiB boundary; a partition that ended off it keeps
			// at least its size.
			newSize = alignDown(s.Start+s.Size+s.GrowBy, mib) - s.Start
			if req.Size != 0 {
				newSize = alignDown(s.Start+req.Size, mib) - s.Start
			}
			if newSize <= s.Size || newSize > s.Size+s.GrowBy {
				return "", fmt.Errorf("%w: the new size must be larger and fit into the free space", ErrFormatInvalid)
			}
			if fs == "btrfs" {
				// Growing a filesystem of several devices would grow another.
				shared, err := m.sharedFilesystem(ctx, s.UUID)
				if err != nil {
					return "", err
				}
				if shared {
					return "", fmt.Errorf("%w: btrfs on several drives", ErrPartitionUnsupported)
				}
			}
			return s.Device, nil
		},
		work: func(ctx context.Context, job *FormatJob, l *Layout) error {
			s, err := l.slot(number)
			if err != nil {
				return err
			}
			ss := l.SectorSize
			m.phase(job, "partition")
			script := fmt.Sprintf("start=%d, size=%d\n", s.Start/ss, newSize/ss)
			if _, err := m.runner.Run(ctx, script, "sfdisk", "--quiet", "--no-reread", "-N", strconv.Itoa(number), l.Disk.path); err != nil {
				return err
			}
			m.settle(ctx, l.Disk.path, "-u", "--nr", strconv.Itoa(number))
			if fs == "" {
				return nil
			}
			m.phase(job, "resize")
			return m.growFilesystem(ctx, fs, s.Device)
		},
	})
}

// growFilesystem grows a filesystem to the size of its partition. XFS and
// Btrfs only grow while mounted; they are mounted for that under /run and
// unmounted again.
func (m *Manager) growFilesystem(ctx context.Context, fs, part string) error {
	switch fs {
	case "ext2", "ext3", "ext4":
		// resize2fs wants a freshly checked filesystem. -p only repairs what
		// is safe without asking; exit 1 means it did.
		if _, err := m.runner.Run(ctx, "", "e2fsck", "-f", "-p", part); err != nil && exitCode(err) != 1 {
			return err
		}
		_, err := m.runner.Run(ctx, "", "resize2fs", part)
		return err
	case "ntfs":
		// No --force: a Windows that hibernated or was not shut down
		// cleanly is left alone.
		_, err := m.runner.Run(ctx, "y\n", "ntfsresize", "--no-progress-bar", part)
		return err
	case "xfs", "btrfs":
		dir, err := os.MkdirTemp(m.scratchDir(), "grow-")
		if err != nil {
			return err
		}
		defer os.Remove(dir)
		if _, err := m.runner.Run(ctx, "", "mount", "-t", fs, "-o", "nosuid,nodev,noexec", part, dir); err != nil {
			return err
		}
		var grow error
		if fs == "xfs" {
			_, grow = m.runner.Run(ctx, "", "xfs_growfs", dir)
		} else {
			_, grow = m.runner.Run(ctx, "", "btrfs", "filesystem", "resize", "max", dir)
		}
		if _, err := m.runner.Run(context.WithoutCancel(ctx), "", "umount", dir); err != nil && grow == nil {
			return err
		}
		return grow
	}
	return fmt.Errorf("%w: %s cannot grow", ErrPartitionUnsupported, fs)
}

func exitCode(err error) int {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return -1
}

// sharedFilesystem reports a filesystem UUID on more than one device.
func (m *Manager) sharedFilesystem(ctx context.Context, uuid string) (bool, error) {
	nodes, err := m.blockDevices(ctx)
	if err != nil {
		return false, err
	}
	count := 0
	var walk func([]node)
	walk = func(nodes []node) {
		for _, n := range nodes {
			if uuid != "" && n.UUID == uuid {
				count++
			}
			walk(n.Children)
		}
	}
	walk(nodes)
	return count > 1, nil
}

func (m *Manager) makeFilesystem(ctx context.Context, fs, label string, size uint64, part string) error {
	if _, err := m.runner.Run(ctx, "", "mkfs."+fs, mkfsArgs(FormatRequest{Filesystem: fs, Label: label}, size, part)...); err != nil {
		return err
	}
	m.runner.Run(ctx, "", "udevadm", "settle", "--timeout=10")
	return nil
}

// settle tells the kernel about a changed partition while other partitions
// of the drive may stay mounted, then waits for udev. partx fails when the
// kernel already knows; that is fine.
func (m *Manager) settle(ctx context.Context, disk string, partx ...string) {
	if _, err := m.runner.Run(ctx, "", "partx", append(partx, disk)...); err != nil {
		var cmd *run.CommandError
		if errors.As(err, &cmd) {
			slog.Info("partx", "disk", disk, "output", cmd.Output)
		}
	}
	m.runner.Run(ctx, "", "udevadm", "settle", "--timeout=10")
}

// waitNode waits for the kernel to show a partition.
func (m *Manager) waitNode(ctx context.Context, disk, part string) error {
	for try := 0; ; try++ {
		nodes, err := m.blockDevices(ctx, disk)
		if err == nil && len(nodes) == 1 && slices.ContainsFunc(nodes[0].Children, func(c node) bool { return c.Name == part }) {
			return nil
		}
		if try == 20 {
			return fmt.Errorf("the new partition %s does not show up", part)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(m.retryPause()):
		}
	}
}

func (m *Manager) available(tool string) bool {
	look := m.lookPath
	if look == nil {
		look = exec.LookPath
	}
	_, err := look(tool)
	return err == nil
}

func (m *Manager) scratchDir() string {
	dir := m.scratch
	if dir == "" {
		dir = "/run/titanos-partitions"
	}
	os.MkdirAll(dir, 0o700)
	return dir
}
