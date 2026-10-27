package usb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// A 32 GB stick with GPT: 512 MiB EFI (FAT), 1 GiB free, 1 GiB ext4, half a
// MiB that is too small to use, 1000 MiB XFS and the rest free.
const partsListing = `{"blockdevices":[{"name":"/dev/sdb","type":"disk","size":32010928128,"tran":"usb","model":"Ultra Fit","serial":"4C53","rm":true,"pttype":"gpt","ptuuid":"pt-1","children":[
 {"name":"/dev/sdb1","type":"part","size":536870912,"fstype":"vfat","uuid":"EFI0-0001","label":"EFI"},
 {"name":"/dev/sdb2","type":"part","size":1073741824,"fstype":"ext4","uuid":"linux-2","label":"data"MOUNT2},
 {"name":"/dev/sdb3","type":"part","size":1048576000,"fstype":"xfs","uuid":"xfs-3"}NEW]}]}`

const partsTable = `{"partitiontable":{"label":"gpt","id":"PT-1","device":"/dev/sdb","unit":"sectors","firstlba":2048,"lastlba":62521310,"sectorsize":512,"partitions":[
 {"node":"/dev/sdb1","start":2048,"size":1048576,"type":"C12A7328-F81F-11D2-BA4B-00A0C93EC93B","uuid":"u1","name":"EFI"},
 {"node":"/dev/sdb2","start":3147776,"size":2097152,"type":"0FC63DAF-8483-4772-8E79-3D69D8477DE4","uuid":"u2"},
 {"node":"/dev/sdb3","start":5245952,"size":2048000,"type":"0fc63daf-8483-4772-8e79-3d69d8477de4","uuid":"u3"}]}}`

// partFake plays lsblk and sfdisk for one drive and records the rest.
type partFake struct {
	mu      sync.Mutex
	calls   []string
	stdin   map[string]string
	listing string
	table   string
	// after replaces the listing once something was written.
	after string
	// onRead runs after each sfdisk --json, to change the drive meanwhile.
	onRead func(f *partFake)
	fail   string
}

func (f *partFake) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	switch {
	case name == "lsblk":
		return f.listing, nil
	case name == "sfdisk" && args[0] == "--json":
		if f.table == "" {
			return "", errors.New("does not contain a recognized partition table")
		}
		out := f.table
		if f.onRead != nil {
			f.onRead(f)
		}
		return out, nil
	}
	f.calls = append(f.calls, line)
	if stdin != "" {
		f.stdin[name] = stdin
	}
	if f.fail != "" && strings.HasPrefix(line, f.fail) {
		return "", errors.New("exit status 1")
	}
	if f.after != "" && name != "udevadm" && name != "partx" {
		f.listing = f.after
	}
	return "", nil
}

func (f *partFake) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

func partsLayout(mount2, extra string) string {
	return strings.NewReplacer("MOUNT2", mount2, "NEW", extra).Replace(partsListing)
}

func newPartsTest(t *testing.T) (*Manager, *partFake) {
	t.Helper()
	root := t.TempDir()
	f := &partFake{stdin: map[string]string{}, listing: partsLayout("", ""), table: partsTable}
	m := &Manager{runner: f, root: filepath.Join(root, "usb"), mountsFile: filepath.Join(root, "mounts"), pause: 1,
		scratch: filepath.Join(root, "scratch"), lookPath: func(string) (string, error) { return "/usr/sbin/tool", nil }}
	os.WriteFile(m.mountsFile, nil, 0o644)
	return m, f
}

func layoutOf(t *testing.T, m *Manager) *Layout {
	t.Helper()
	id := stickID(t, m)
	l, err := m.Partitions(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

// run starts an action and waits for its job; it returns the job.
func waitJob(t *testing.T, m *Manager, err error) FormatJob {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	m.waitFormat()
	m.mu.Lock()
	defer m.mu.Unlock()
	return *m.job
}

func TestLayoutGapsAreAligned(t *testing.T) {
	m, _ := newPartsTest(t)
	l := layoutOf(t, m)
	if l.Table != "gpt" || l.SectorSize != 512 || len(l.Partitions) != 3 || l.Reason != "" {
		t.Fatalf("layout %+v", l)
	}
	// The half MiB between 2 and 3 is too small; the end is cut to whole MiB.
	want := []Gap{
		{Start: 1050624 * 512, Size: 2097152 * 512, After: 1},
		{Start: 7294976 * 512, Size: (62519296 - 7294976) * 512, After: 3},
	}
	if !slices.Equal(l.Gaps, want) {
		t.Fatalf("gaps\n got %+v\nwant %+v", l.Gaps, want)
	}
	for _, g := range l.Gaps {
		if g.Start%mib != 0 || g.Size%mib != 0 {
			t.Errorf("gap not aligned: %+v", g)
		}
	}
	p := l.Partitions
	if p[0].Number != 1 || p[0].FSType != "vfat" || p[0].Label != "EFI" || p[0].Start != mib || p[0].Size != 512*mib || p[0].Type != "C12A7328-F81F-11D2-BA4B-00A0C93EC93B" {
		t.Errorf("partition 1: %+v", p[0])
	}
	// FAT does not grow; ext4 has only the half MiB after it; XFS has the rest.
	if p[0].GrowBy != 0 || p[1].GrowBy != 0 || p[2].GrowBy != (62519296-7293952)*512 {
		t.Errorf("grow: %d %d %d", p[0].GrowBy, p[1].GrowBy, p[2].GrowBy)
	}
	if !p[1].Renamable || p[1].UUID != "linux-2" || p[2].Type != gptLinux {
		t.Errorf("partition 2/3: %+v %+v", p[1], p[2])
	}
}

func TestAlignmentMath(t *testing.T) {
	if alignment(512) != 2048 || alignment(4096) != 256 || alignUp(2049, 2048) != 4096 || alignUp(4096, 2048) != 4096 || alignDown(4095, 2048) != 2048 {
		t.Fatal("alignment")
	}
	// 4 KiB sectors: a 1 GiB drive with one partition at 1 MiB of 256 MiB.
	got := gaps([]Slot{{Number: 1, Start: mib, Size: 256 * mib}}, 6, 262143, 4096)
	want := []Gap{{Start: 257 * mib, Size: 767 * mib, After: 1}}
	if !slices.Equal(got, want) {
		t.Fatalf("4K gaps %+v", got)
	}
	// An empty table: everything from 1 MiB, also when the first usable
	// sector is 34 (GPT made by other tools).
	if got := gaps(nil, 34, 2*2048*1024-34, 512); len(got) != 1 || got[0].Start != mib || got[0].Size != 2046*mib {
		t.Fatalf("empty %+v", got)
	}
	// A gap below 1 MiB is not offered.
	if got := gaps([]Slot{{Number: 1, Start: mib, Size: mib}, {Number: 2, Start: 2*mib + 512*1024, Size: mib}}, 2048, 4*2048-1, 512); len(got) != 0 {
		t.Fatalf("small gap %+v", got)
	}
	if partitionDevice("/dev/sdb", 3) != "/dev/sdb3" || partitionDevice("/dev/nvme0n1", 2) != "/dev/nvme0n1p2" || partitionDevice("/dev/mmcblk0", 1) != "/dev/mmcblk0p1" {
		t.Fatal("device names")
	}
	if partitionType("dos", "vfat", 100*mib) != "e" || partitionType("dos", "vfat", gib) != "c" || partitionType("dos", "exfat", gib) != "7" || partitionType("dos", "", gib) != "83" ||
		partitionType("gpt", "ntfs", gib) != gptWindows || partitionType("gpt", "btrfs", gib) != gptLinux {
		t.Fatal("types")
	}
}

func TestCreatePartitionInGap(t *testing.T) {
	m, f := newPartsTest(t)
	f.after = partsLayout("", `,{"name":"/dev/sdb4","type":"part","size":536870912}`)
	l := layoutOf(t, m)
	var ended []FormatJob
	m.OnFormatted = func(job FormatJob) { ended = append(ended, job) }
	job := waitJob(t, m, m.CreatePartition(context.Background(), l.Disk.ID, PartitionRequest{Start: l.Gaps[0].Start, Size: 512*mib + 4096, Filesystem: "exfat", Label: "Fotos"}))
	want := []string{
		"sfdisk --quiet --no-reread --wipe-partitions always -N 4 /dev/sdb",
		"partx -a --nr 4 /dev/sdb",
		"udevadm settle --timeout=10",
		"mkfs.exfat -L Fotos /dev/sdb4",
		"udevadm settle --timeout=10",
	}
	if got := f.commands(); !slices.Equal(got, want) {
		t.Fatalf("commands\n%s", strings.Join(got, "\n"))
	}
	// The size is cut to whole MiB; exFAT is basic data, which Windows reads.
	if f.stdin["sfdisk"] != "start=1050624, size=1048576, type="+gptWindows+"\n" {
		t.Errorf("script %q", f.stdin["sfdisk"])
	}
	if job.Phase != "done" || job.Action != "create" || job.Partition != "/dev/sdb4" || len(ended) != 1 {
		t.Errorf("job %+v", job)
	}
}

func TestCreatePartitionRest(t *testing.T) {
	m, f := newPartsTest(t)
	f.after = partsLayout("", `,{"name":"/dev/sdb4","type":"part","size":1}`)
	l := layoutOf(t, m)
	waitJob(t, m, m.CreatePartition(context.Background(), l.Disk.ID, PartitionRequest{Start: l.Gaps[1].Start}))
	if f.stdin["sfdisk"] != "start=7294976, size=55224320, type="+gptLinux+"\n" {
		t.Errorf("script %q", f.stdin["sfdisk"])
	}
	// No filesystem asked for: none is made.
	if got := f.commands(); slices.ContainsFunc(got, func(c string) bool { return strings.HasPrefix(c, "mkfs") }) {
		t.Errorf("formatted: %v", got)
	}
}

func TestCreatePartitionRefusals(t *testing.T) {
	m, f := newPartsTest(t)
	l := layoutOf(t, m)
	ctx := context.Background()
	for name, req := range map[string]PartitionRequest{
		"not aligned":    {Start: l.Gaps[0].Start + 4096},
		"in a partition": {Start: mib},
		"too large":      {Start: l.Gaps[0].Start, Size: l.Gaps[0].Size + mib},
		"too small xfs":  {Start: l.Gaps[0].Start, Size: 100 * mib, Filesystem: "xfs"},
		"bad label":      {Start: l.Gaps[0].Start, Filesystem: "vfat", Label: "Fotos ä"},
		"label alone":    {Start: l.Gaps[0].Start, Label: "x"},
	} {
		if err := m.CreatePartition(ctx, l.Disk.ID, req); !errors.Is(err, ErrFormatInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := f.commands(); len(got) != 0 {
		t.Errorf("wrote: %v", got)
	}
}

func TestPartitionActionsCommands(t *testing.T) {
	cases := []struct {
		name  string
		start func(m *Manager, id string) error
		want  []string
		stdin string
	}{
		{"delete", func(m *Manager, id string) error { return m.DeletePartition(context.Background(), id, 2) },
			[]string{"sfdisk --quiet --no-reread --delete /dev/sdb 2", "partx -d --nr 2 /dev/sdb", "udevadm settle --timeout=10"}, ""},
		{"format", func(m *Manager, id string) error {
			return m.FormatPartition(context.Background(), id, 2, PartitionRequest{Filesystem: "ntfs", Label: "Windows"})
		},
			[]string{"wipefs --all --force /dev/sdb2", "sfdisk --quiet --no-reread --part-type /dev/sdb 2 " + gptWindows, "partx -u --nr 2 /dev/sdb",
				"udevadm settle --timeout=10", "mkfs.ntfs --quick --label Windows /dev/sdb2", "udevadm settle --timeout=10"}, ""},
		{"format same type", func(m *Manager, id string) error {
			return m.FormatPartition(context.Background(), id, 3, PartitionRequest{Filesystem: "ext4"})
		},
			[]string{"wipefs --all --force /dev/sdb3", "mkfs.ext4 -F -q -m 0 /dev/sdb3", "udevadm settle --timeout=10"}, ""},
		{"label ext4", func(m *Manager, id string) error { return m.SetLabel(context.Background(), id, 2, "Archiv") },
			[]string{"e2label /dev/sdb2 Archiv", "udevadm settle --timeout=10"}, ""},
		{"label fat", func(m *Manager, id string) error { return m.SetLabel(context.Background(), id, 1, "boot") },
			[]string{"fatlabel /dev/sdb1 BOOT", "udevadm settle --timeout=10"}, ""},
		{"label xfs", func(m *Manager, id string) error { return m.SetLabel(context.Background(), id, 3, "scratch") },
			[]string{"xfs_admin -L scratch /dev/sdb3", "udevadm settle --timeout=10"}, ""},
		{"grow xfs", func(m *Manager, id string) error {
			return m.GrowPartition(context.Background(), id, 3, PartitionRequest{})
		},
			[]string{"sfdisk --quiet --no-reread -N 3 /dev/sdb", "partx -u --nr 3 /dev/sdb", "udevadm settle --timeout=10",
				"mount -t xfs -o nosuid,nodev,noexec /dev/sdb3 SCRATCH", "xfs_growfs SCRATCH", "umount SCRATCH"},
			"start=5245952, size=57273344\n"},
		{"grow xfs by 1 GiB", func(m *Manager, id string) error {
			return m.GrowPartition(context.Background(), id, 3, PartitionRequest{Size: 2000 * mib})
		},
			[]string{"sfdisk --quiet --no-reread -N 3 /dev/sdb", "partx -u --nr 3 /dev/sdb", "udevadm settle --timeout=10",
				"mount -t xfs -o nosuid,nodev,noexec /dev/sdb3 SCRATCH", "xfs_growfs SCRATCH", "umount SCRATCH"},
			// The end is put on a whole MiB.
			"start=5245952, size=4094976\n"},
		{"new table", func(m *Manager, id string) error {
			return m.NewTable(context.Background(), id, TableRequest{Scheme: "mbr"})
		},
			[]string{"wipefs --all --force /dev/sdb3", "wipefs --all --force /dev/sdb2", "wipefs --all --force /dev/sdb1", "wipefs --all --force /dev/sdb",
				"sfdisk --quiet --wipe always --wipe-partitions always /dev/sdb", "partx -u /dev/sdb", "udevadm settle --timeout=10"}, "label: dos\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, f := newPartsTest(t)
			id := layoutOf(t, m).Disk.ID
			job := waitJob(t, m, c.start(m, id))
			if job.Phase != "done" {
				t.Fatalf("job %+v", job)
			}
			got := f.commands()
			for i := range got {
				if dir, ok := strings.CutPrefix(got[i], "mount -t xfs -o nosuid,nodev,noexec /dev/sdb3 "); ok {
					got = slices.Clone(got)
					for j := range got {
						got[j] = strings.ReplaceAll(got[j], dir, "SCRATCH")
					}
					if !strings.HasPrefix(dir, m.scratch) {
						t.Errorf("mounted at %s", dir)
					}
				}
			}
			if !slices.Equal(got, c.want) {
				t.Fatalf("commands\n%s", strings.Join(got, "\n"))
			}
			if c.stdin != "" && f.stdin["sfdisk"] != c.stdin {
				t.Errorf("sfdisk input %q", f.stdin["sfdisk"])
			}
		})
	}
}

func TestGrowExt4AndNTFS(t *testing.T) {
	// ext4 as the last partition with free space after it.
	m, f := newPartsTest(t)
	f.listing = strings.Replace(partsLayout("", ""), `"fstype":"xfs"`, `"fstype":"ext4"`, 1)
	id := layoutOf(t, m).Disk.ID
	waitJob(t, m, m.GrowPartition(context.Background(), id, 3, PartitionRequest{}))
	got := f.commands()
	if !slices.Equal(got[3:], []string{"e2fsck -f -p /dev/sdb3", "resize2fs /dev/sdb3"}) {
		t.Fatalf("ext4\n%s", strings.Join(got, "\n"))
	}
	m, f = newPartsTest(t)
	f.listing = strings.Replace(partsLayout("", ""), `"fstype":"xfs"`, `"fstype":"ntfs"`, 1)
	id = layoutOf(t, m).Disk.ID
	waitJob(t, m, m.GrowPartition(context.Background(), id, 3, PartitionRequest{}))
	if got := f.commands(); got[len(got)-1] != "ntfsresize --no-progress-bar /dev/sdb3" || f.stdin["ntfsresize"] != "y\n" {
		t.Fatalf("ntfs\n%s", strings.Join(got, "\n"))
	}
}

func TestPartitionRefusals(t *testing.T) {
	ctx := context.Background()
	m, f := newPartsTest(t)
	id := layoutOf(t, m).Disk.ID
	// FAT and exFAT do not grow; neither does a partition without space after it.
	if err := m.GrowPartition(ctx, id, 1, PartitionRequest{}); !errors.Is(err, ErrPartitionUnsupported) {
		t.Errorf("grow FAT: %v", err)
	}
	if err := m.GrowPartition(ctx, id, 2, PartitionRequest{}); !errors.Is(err, ErrFormatInvalid) {
		t.Errorf("grow without space: %v", err)
	}
	// Nothing is ever shrunk.
	if err := m.GrowPartition(ctx, id, 3, PartitionRequest{Size: 500 * mib}); !errors.Is(err, ErrFormatInvalid) {
		t.Errorf("shrink: %v", err)
	}
	if err := m.DeletePartition(ctx, id, 9); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown partition: %v", err)
	}
	if err := m.NewTable(ctx, id, TableRequest{Scheme: "sun"}); !errors.Is(err, ErrFormatInvalid) {
		t.Errorf("unknown table: %v", err)
	}
	// Renaming needs the tool.
	m.lookPath = func(tool string) (string, error) { return "", errors.New("not found") }
	if err := m.SetLabel(ctx, id, 2, "x"); !errors.Is(err, ErrPartitionUnsupported) {
		t.Errorf("label without tool: %v", err)
	}
	if got := f.commands(); len(got) != 0 {
		t.Errorf("wrote: %v", got)
	}
	m.Disabled = true
	if err := m.DeletePartition(ctx, id, 3); !errors.Is(err, ErrDisabled) {
		t.Errorf("development mode: %v", err)
	}
	m.Disabled = false
	m.Busy = func() bool { return true }
	if err := m.DeletePartition(ctx, id, 3); !errors.Is(err, ErrBusy) {
		t.Errorf("storage busy: %v", err)
	}
}

func TestMountedPartitionIsLeftAlone(t *testing.T) {
	ctx := context.Background()
	m, f := newPartsTest(t)
	f.listing = partsLayout(`,"mountpoint":"/srv/x"`, "")
	l := layoutOf(t, m)
	if l.Partitions[1].Reason != "inuse" || !l.Partitions[1].Mounted || l.Partitions[2].Reason != "" {
		t.Fatalf("reasons %+v", l.Partitions)
	}
	for name, err := range map[string]error{
		"delete": m.DeletePartition(ctx, l.Disk.ID, 2),
		"format": m.FormatPartition(ctx, l.Disk.ID, 2, PartitionRequest{Filesystem: "ext4"}),
		"label":  m.SetLabel(ctx, l.Disk.ID, 2, "x"),
		"table":  m.NewTable(ctx, l.Disk.ID, TableRequest{Scheme: "gpt"}),
	} {
		if !errors.Is(err, ErrNotFormattable) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if got := f.commands(); len(got) != 0 {
		t.Errorf("wrote: %v", got)
	}
	// Another partition of the same drive can still be changed.
	waitJob(t, m, m.DeletePartition(ctx, l.Disk.ID, 3))
	// Mounted by TitanOS: eject first.
	m, f = newPartsTest(t)
	dir := filepath.Join(m.root, "data-linux2")
	f.listing = partsLayout(`,"mountpoint":"`+dir+`"`, "")
	os.WriteFile(m.mountsFile, []byte("/dev/sdb2 "+dir+" ext4 rw 0 0\n"), 0o644)
	if l := layoutOf(t, m); l.Partitions[1].Reason != "mounted" {
		t.Errorf("ours: %+v", l.Partitions[1])
	}
}

func TestExcludedDrivesHaveNoPartitionEditor(t *testing.T) {
	ctx := context.Background()
	m, _ := newDisksTest(t)
	disks, _, err := m.Disks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The NAS disk and the boot stick are not in the list, so no ID reaches them.
	for _, d := range disks {
		if d.Device == "/dev/sdd" || d.Device == "/dev/sdj" || d.Device == "/dev/vda" {
			t.Fatalf("listed: %s", d.Device)
		}
	}
	if _, err := m.Partitions(ctx, diskID(node{Name: "/dev/sdd"})); !errors.Is(err, ErrNotFound) {
		t.Errorf("volume disk: %v", err)
	}
	// A disk of a VM: shown, nothing may be changed.
	i := slices.IndexFunc(disks, func(d Disk) bool { return d.Device == "/dev/sdk" })
	l, err := m.Partitions(ctx, disks[i].ID)
	if err != nil {
		t.Fatal(err)
	}
	if l.Reason != "claimed" || l.Table != "" {
		t.Errorf("VM disk: %+v", l)
	}
	if err := m.NewTable(ctx, disks[i].ID, TableRequest{Scheme: "gpt"}); !errors.Is(err, ErrNotFormattable) {
		t.Errorf("table on VM disk: %v", err)
	}
	// A disk with an assembled RAID on it.
	i = slices.IndexFunc(disks, func(d Disk) bool { return d.Device == "/dev/sde" })
	if err := m.NewTable(ctx, disks[i].ID, TableRequest{Scheme: "gpt"}); !errors.Is(err, ErrNotFormattable) {
		t.Errorf("table on active RAID: %v", err)
	}
}

func TestChangedDriveIsRefused(t *testing.T) {
	ctx := context.Background()
	m, f := newPartsTest(t)
	id := layoutOf(t, m).Disk.ID
	// Another stick in the same port: the ID no longer matches.
	f.listing = strings.Replace(partsLayout("", ""), `"serial":"4C53"`, `"serial":"OTHER"`, 1)
	if err := m.DeletePartition(ctx, id, 3); !errors.Is(err, ErrNotFound) {
		t.Fatalf("swapped stick: %v", err)
	}
	// The drive changes between the click and the background check: the job
	// fails before anything is written.
	m, f = newPartsTest(t)
	id = layoutOf(t, m).Disk.ID
	f.onRead = func(f *partFake) {
		f.listing = strings.Replace(f.listing, `"size":1048576000`, `"size":2097152000`, 1)
		f.onRead = nil
	}
	job := waitJob(t, m, m.DeletePartition(ctx, id, 3))
	if job.Phase != "failed" || !strings.Contains(job.Error, "no such drive") {
		t.Fatalf("job %+v", job)
	}
	if got := f.commands(); len(got) != 0 {
		t.Errorf("wrote: %v", got)
	}
}

func TestDriveWithoutTable(t *testing.T) {
	m, f := newPartsTest(t)
	f.listing = `{"blockdevices":[{"name":"/dev/sdb","type":"disk","size":32010928128,"tran":"usb","model":"Ultra Fit"}]}`
	f.table = ""
	l := layoutOf(t, m)
	if l.Table != "" || len(l.Partitions) != 0 || len(l.Gaps) != 0 {
		t.Fatalf("layout %+v", l)
	}
	if err := m.CreatePartition(context.Background(), l.Disk.ID, PartitionRequest{Start: mib}); !errors.Is(err, ErrFormatInvalid) {
		t.Errorf("create without table: %v", err)
	}
	waitJob(t, m, m.NewTable(context.Background(), l.Disk.ID, TableRequest{Scheme: "gpt"}))
	if f.stdin["sfdisk"] != "label: gpt\n" {
		t.Errorf("table %q", f.stdin["sfdisk"])
	}
}

func TestExtendedTableOnlyFormats(t *testing.T) {
	m, f := newPartsTest(t)
	f.listing = `{"blockdevices":[{"name":"/dev/sdb","type":"disk","size":32010928128,"tran":"usb","pttype":"dos","children":[
 {"name":"/dev/sdb1","type":"part","size":1024},
 {"name":"/dev/sdb5","type":"part","size":1073741824,"fstype":"ext4","uuid":"l5"}]}]}`
	f.table = `{"partitiontable":{"label":"dos","id":"0x1","device":"/dev/sdb","unit":"sectors","sectorsize":512,"partitions":[
 {"node":"/dev/sdb1","start":2048,"size":4194304,"type":"5"},
 {"node":"/dev/sdb5","start":4096,"size":2097152,"type":"83"}]}}`
	l := layoutOf(t, m)
	if l.Reason != "extended" || len(l.Gaps) != 0 || l.Partitions[0].Reason != "extended" || l.Partitions[1].Reason != "" {
		t.Fatalf("layout %+v", l)
	}
	if err := m.DeletePartition(context.Background(), l.Disk.ID, 5); !errors.Is(err, ErrPartitionUnsupported) {
		t.Errorf("delete logical: %v", err)
	}
	waitJob(t, m, m.SetLabel(context.Background(), l.Disk.ID, 5, "Logisch"))
}

func TestBtrfsOnSeveralDrivesDoesNotGrow(t *testing.T) {
	m, f := newPartsTest(t)
	one := strings.Replace(partsLayout("", ""), `"fstype":"xfs","uuid":"xfs-3"`, `"fstype":"btrfs","uuid":"b-3"`, 1)
	f.listing = one
	id := layoutOf(t, m).Disk.ID
	// The same filesystem on a second drive.
	f.listing = strings.TrimSuffix(one, "]}") + `,{"name":"/dev/sdc","type":"disk","size":8000000000,"tran":"usb","children":[{"name":"/dev/sdc1","type":"part","size":7000000000,"fstype":"btrfs","uuid":"b-3"}]}]}`
	if err := m.GrowPartition(context.Background(), id, 3, PartitionRequest{}); !errors.Is(err, ErrPartitionUnsupported) {
		t.Fatalf("multi-device btrfs: %v", err)
	}
	f.listing = one
	waitJob(t, m, m.GrowPartition(context.Background(), id, 3, PartitionRequest{}))
	if got := f.commands(); !strings.HasPrefix(got[len(got)-2], "btrfs filesystem resize max ") {
		t.Fatalf("btrfs\n%s", strings.Join(got, "\n"))
	}
}
