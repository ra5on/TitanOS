package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// treeRunner answers lsblk with a given device tree, mdadm --detail with a
// fixed UUID (or fails it) and records everything else.
type treeRunner struct {
	devices    []blockDevice
	commands   []string
	stdin      []string
	mounted    map[string]bool
	noUUID     bool
	btrfsShow  string
	mountFails bool
}

const runningArrayUUID = "99999999:88888888:77777777:66666666"

func (r *treeRunner) Run(_ context.Context, stdin string, name string, args ...string) (string, error) {
	command := name + " " + strings.Join(args, " ")
	switch {
	case name == "lsblk":
		b, _ := json.Marshal(map[string]any{"blockdevices": r.devices})
		return string(b), nil
	case name == "mdadm" && slices.Contains(args, "--detail"):
		if r.noUUID {
			return "", errors.New("mdadm failed")
		}
		return "MD_UUID=" + runningArrayUUID + "\n", nil
	case name == "btrfs" && len(args) > 1 && args[0] == "filesystem" && args[1] == "show":
		return r.btrfsShow, nil
	case name == "btrfs":
		return "", nil
	}
	r.commands = append(r.commands, command)
	r.stdin = append(r.stdin, stdin)
	if name == "mount" {
		if r.mountFails && !strings.Contains(command, "degraded") {
			return "", errors.New("mount failed")
		}
		r.mounted[args[len(args)-1]] = true
	}
	return "", nil
}

func (r *treeRunner) wiped() []string {
	var wiped []string
	for _, command := range r.commands {
		if strings.HasPrefix(command, "wipefs") || strings.HasPrefix(command, "sfdisk") || strings.HasPrefix(command, "mdadm --zero") {
			wiped = append(wiped, command)
		}
	}
	return wiped
}

// treeManager is a manager whose sysfs lives below a temporary directory:
// <tmp>/block for md devices, <tmp>/class/block for partition sizes.
func treeManager(t *testing.T, devices []blockDevice) (*Manager, *treeRunner) {
	t.Helper()
	m, _, _ := newTestManager(t)
	runner := &treeRunner{devices: devices, mounted: map[string]bool{}}
	m.runner = runner
	m.mounts = func() map[string]bool { return runner.mounted }
	m.sysBlock = filepath.Join(t.TempDir(), "block")
	if err := os.MkdirAll(m.sysBlock, 0o755); err != nil {
		t.Fatal(err)
	}
	return m, runner
}

func writeSys(t *testing.T, path, value string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}

// runningArray makes md127 a clean, idle RAID 5 of the given partitions,
// each partitionSectors large.
func runningArray(t *testing.T, m *Manager, partitionSectors string, members ...string) {
	md := filepath.Join(m.sysBlock, "md127", "md")
	for file, value := range map[string]string{"level": "raid5", "array_state": "clean", "degraded": "0", "raid_disks": "3", "sync_action": "idle"} {
		writeSys(t, filepath.Join(md, file), value)
	}
	writeSys(t, filepath.Join(m.sysBlock, "md127", "size"), "2000000")
	for i, member := range members {
		writeSys(t, filepath.Join(md, "dev-"+member, "state"), "in_sync")
		writeSys(t, filepath.Join(md, "dev-"+member, "slot"), string(rune('0'+i)))
		writeSys(t, filepath.Join(filepath.Dir(m.sysBlock), "class", "block", member, "size"), partitionSectors)
	}
}

func tdisk(name, serial string, children ...blockDevice) blockDevice {
	return blockDevice{Name: "/dev/" + name, KName: "/dev/" + name, Type: "disk", Size: 8 << 30, Serial: serial, Model: "M", Children: children}
}

func raidPart(name string, above ...blockDevice) blockDevice {
	return blockDevice{Name: "/dev/" + name, KName: "/dev/" + name, Type: "part", Size: 7 << 30, FSType: "linux_raid_member", UUID: "dead-beef", Children: above}
}

// Finding: prepareDisk wiped LVs, partitions and filesystems on top of an
// md array the disk was a member of.
func TestPrepareDiskNeverWipesAboveArraysOrHolders(t *testing.T) {
	m, runner := treeManager(t, nil)
	array := blockDevice{Name: "/dev/md126", Type: "raid1", FSType: "LVM2_member", Children: []blockDevice{
		{Name: "/dev/mapper/vg-data", Type: "lvm", FSType: "ext4"},
	}}
	arrayPart := blockDevice{Name: "/dev/md125", Type: "raid0", Children: []blockDevice{{Name: "/dev/md125p1", Type: "part", FSType: "xfs"}}}
	crypt := blockDevice{Name: "/dev/mapper/secret", Type: "crypt", FSType: "ext4"}
	device := tdisk("sdx", "X", raidPart("sdx1", array, arrayPart), blockDevice{Name: "/dev/sdx2", Type: "part", FSType: "crypto_LUKS", Children: []blockDevice{crypt}})
	if _, err := m.prepareDisk(context.Background(), device, "data", 100); err != nil {
		t.Fatal(err)
	}
	for _, command := range runner.commands {
		for _, foreign := range []string{"md126", "md125", "mapper"} {
			if strings.Contains(command, foreign) {
				t.Fatalf("wiped something above the disk: %q", command)
			}
		}
	}
	got := strings.Join(runner.wiped(), "\n")
	for _, want := range []string{"mdadm --zero-superblock --force /dev/sdx1", "wipefs --all --force /dev/sdx1", "wipefs --all --force /dev/sdx2", "wipefs --all --force /dev/sdx"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
}

// Finding: a mounted filesystem on an LV or md partition above the disk did
// not make the disk count as mounted.
func TestClassifySeesMountsAboveForeignArrays(t *testing.T) {
	lv := tdisk("sdx", "X", raidPart("sdx1", blockDevice{Name: "/dev/md126", Type: "raid1", Children: []blockDevice{
		{Name: "/dev/mapper/vg-data", Type: "lvm", FSType: "ext4", Mountpoint: "/srv"}}}))
	if d := classify(lv, nil); !d.Mounted || d.Role != RoleUsed {
		t.Fatalf("LV on foreign array: %+v", d)
	}
	part := tdisk("sdy", "Y", raidPart("sdy1", blockDevice{Name: "/dev/md125", Type: "raid1", Children: []blockDevice{
		{Name: "/dev/md125p1", Type: "part", FSType: "xfs", Mountpoint: "/other"}}}))
	if d := classify(part, nil); !d.Mounted {
		t.Fatalf("md partition: %+v", d)
	}
	// The mounted filesystem of a TitanOS volume is the volume's.
	if d := classify(part, map[string]string{normalizeUUID("dead-beef"): "data"}); d.Mounted || d.Role != RoleMember {
		t.Fatalf("volume member: %+v", d)
	}
}

// Finding: AddDisk, grow and the Btrfs replacement skipped the guard Create
// runs against foreign arrays, holders and mounts.
func TestAddDiskRefusesForeignArraysHoldersAndMounts(t *testing.T) {
	members := []blockDevice{tdisk("sda", "A", raidPart("sda1")), tdisk("sdb", "B", raidPart("sdb1")), tdisk("sdc", "C", raidPart("sdc1"))}
	for _, kind := range []string{"spans other disk", "mounted above", "lvm holder", "sysfs holder", "own old array"} {
		t.Run(kind, func(t *testing.T) {
			foreign := blockDevice{Name: "/dev/md126", Type: "raid1"}
			var devices []blockDevice
			switch kind {
			case "spans other disk":
				devices = []blockDevice{tdisk("sdx", "X", raidPart("sdx1", foreign)), tdisk("sdy", "Y", raidPart("sdy1", foreign))}
			case "mounted above":
				foreign.Children = []blockDevice{{Name: "/dev/md126p1", Type: "part", FSType: "ext4", Mountpoint: "/srv"}}
				devices = []blockDevice{tdisk("sdx", "X", raidPart("sdx1", foreign))}
			case "lvm holder":
				devices = []blockDevice{tdisk("sdx", "X", blockDevice{Name: "/dev/sdx1", Type: "part", FSType: "LVM2_member", Children: []blockDevice{{Name: "/dev/mapper/vg-lv", Type: "lvm"}}})}
			case "sysfs holder", "own old array":
				devices = []blockDevice{tdisk("sdx", "X", raidPart("sdx1", foreign))}
			}
			m, runner := treeManager(t, append(slices.Clone(members), devices...))
			m.volumes = []VolumeConfig{{Name: "data", Level: "raid5", Filesystem: "ext4", ArrayUUID: runningArrayUUID}}
			runningArray(t, m, "1000000", "sda1", "sdb1", "sdc1")
			if kind == "sysfs holder" {
				os.MkdirAll(filepath.Join(m.sysBlock, "sdx", "sdx1", "holders", "dm-3"), 0o755)
			}
			err := m.AddDisk(context.Background(), "data", "sdx", true, "")
			if kind == "own old array" {
				// An old array that lives only on this disk is stopped first,
				// exactly as Create does; it is never wiped while running.
				if len(runner.commands) == 0 || runner.commands[0] != "mdadm --stop /dev/md126" {
					t.Fatal(err, runner.commands)
				}
				return
			}
			if !errors.Is(err, ErrDiskUnavailable) {
				t.Fatalf("err = %v", err)
			}
			if len(runner.commands) != 0 {
				t.Fatalf("changed disks: %v", runner.commands)
			}
		})
	}
}

func TestBtrfsReplacementRefusesForeignArray(t *testing.T) {
	foreign := blockDevice{Name: "/dev/md126", Type: "raid1"}
	devices := []blockDevice{
		tdisk("sda", "A", blockDevice{Name: "/dev/sda1", Type: "part", FSType: "btrfs", UUID: "fs", Size: 1 << 30}),
		tdisk("sdx", "X", raidPart("sdx1", foreign)), tdisk("sdy", "Y", raidPart("sdy1", foreign)),
	}
	m, runner := treeManager(t, devices)
	m.volumes = []VolumeConfig{{Name: "mirror", Level: "raid1", Filesystem: "btrfs", FilesystemUUID: "fs"}}
	runner.mounted[m.MountPoint("mirror")] = true
	runner.btrfsShow = "Total devices 2\n\tdevid    1 size 1073741824 used 0 path /dev/sda1\n\tdevid    2 size 0 used 0 path  MISSING\n"
	if err := m.AddDisk(context.Background(), "mirror", "sdx", true, ""); !errors.Is(err, ErrDiskUnavailable) {
		t.Fatal(err)
	}
	if len(runner.wiped()) != 0 {
		t.Fatal(runner.commands)
	}
}

// Finding: grow sized the new partition from the largest member disk while
// the preview checked the member partitions.
func TestGrowUsesThePreviewedPartitionSize(t *testing.T) {
	members := []blockDevice{tdisk("sda", "A", raidPart("sda1")), tdisk("sdb", "B", raidPart("sdb1")), tdisk("sdc", "C", raidPart("sdc1"))}
	m, runner := treeManager(t, append(members, tdisk("sdx", "X")))
	m.volumes = []VolumeConfig{{Name: "data", Level: "raid5", Filesystem: "ext4", ArrayUUID: runningArrayUUID}}
	runningArray(t, m, "1000000", "sda1", "sdb1", "sdc1") // 488.3 MiB partitions on 8 GiB disks
	root, err := os.OpenRoot(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	g := Growth{GrowthPointer: GrowthPointer{ID: "0123456789abcdef01234567", Volume: "data", RecoveryVolume: "rescue"}, ArrayUUID: runningArrayUUID, DiskSerial: "X", OriginalDisks: 3, TargetDisks: 4}
	m.grow(context.Background(), root, g, "sdx", false)
	index := slices.IndexFunc(runner.commands, func(c string) bool { return strings.HasPrefix(c, "sfdisk") })
	if index < 0 || !strings.Contains(runner.stdin[index], "size=489MiB") {
		t.Fatalf("partition table %v: %q", runner.commands, runner.stdin)
	}
	if size := m.growthPartitionMiB(m.arrays(context.Background())[0], Status{}, "data"); size != 489 {
		t.Fatalf("preview size %d", size)
	}
}

// Finding: an empty md UUID (mdadm failed) matched the empty ArrayUUID of a
// Btrfs mirror; RemoveDisk then worked on a foreign array.
func TestEmptyArrayUUIDNeverMatches(t *testing.T) {
	devices := []blockDevice{
		tdisk("sda", "A", blockDevice{Name: "/dev/sda1", Type: "part", FSType: "btrfs", UUID: "fs"}),
		tdisk("sdb", "B", blockDevice{Name: "/dev/sdb1", Type: "part", FSType: "btrfs", UUID: "fs"}),
		tdisk("sdx", "X", raidPart("sdx1")),
	}
	m, runner := treeManager(t, devices)
	runner.noUUID = true
	runningArray(t, m, "1000000", "sdx1")
	m.volumes = []VolumeConfig{
		{Name: "mirror", Level: "raid1", Filesystem: "btrfs", FilesystemUUID: "fs"},
		{Name: "broken", Level: "raid1", Filesystem: "ext4"},
	}
	ctx := context.Background()
	for _, volume := range m.volumes {
		if _, ok := m.arrayOf(ctx, volume); ok {
			t.Fatalf("%s matched an array without UUID", volume.Name)
		}
	}
	status, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range status.Disks {
		if d.Name == "sdx" && d.Volume != "" {
			t.Fatalf("foreign disk assigned to %q", d.Volume)
		}
	}
	if err := m.RemoveDisk(ctx, "mirror", "sdx"); !errors.Is(err, ErrNotPossible) {
		t.Fatal(err)
	}
	if err := m.RemoveDisk(ctx, "broken", "sdx"); !errors.Is(err, ErrNotPossible) {
		t.Fatal(err)
	}
	m.mountAll(ctx)
	for _, command := range runner.commands {
		if strings.Contains(command, "broken") || strings.Contains(command, "sdx") {
			t.Fatal(runner.commands)
		}
	}
}

// Finding: a Btrfs mirror was mounted degraded as soon as one member was
// visible, and its health came from the lsblk member count only.
func TestBtrfsWaitsForAllMembersBeforeMountingDegraded(t *testing.T) {
	member := func(name string) blockDevice {
		return tdisk(name, strings.ToUpper(name), blockDevice{Name: "/dev/" + name + "1", Type: "part", FSType: "btrfs", UUID: "fs"})
	}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	now := start
	m, runner := treeManager(t, []blockDevice{member("sda")})
	m.clock = func() time.Time { return now }
	m.volumes = []VolumeConfig{{Name: "mirror", Level: "raid1", Filesystem: "btrfs", FilesystemUUID: "fs"}}
	ctx := context.Background()
	for _, after := range []time.Duration{0, 30 * time.Second, 59 * time.Second} {
		now = start.Add(after)
		m.mountAll(ctx)
		if len(runner.commands) != 0 {
			t.Fatalf("mounted after %v with one member: %v", after, runner.commands)
		}
	}
	now = start.Add(61 * time.Second)
	m.mountAll(ctx)
	if len(runner.commands) != 1 || !strings.Contains(runner.commands[0], "subvolid=5,degraded") {
		t.Fatal(runner.commands)
	}

	// The second disk shows up after the degraded mount: lsblk sees two
	// members, but the mounted filesystem still misses one.
	runner.devices = append(runner.devices, member("sdb"))
	runner.btrfsShow = "Label: 'mirror'  uuid: fs\n\tTotal devices 2 FS bytes used 1\n\tdevid    1 size 100 used 1 path /dev/sda1\n\t*** Some devices missing\n"
	m.refreshBtrfs(ctx)
	status, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Volumes[0].Members) != 2 || status.Volumes[0].State != VolumeDegraded {
		t.Fatalf("degraded mount reported as %+v", status.Volumes[0])
	}
	runner.btrfsShow = "\tTotal devices 2\n\tdevid    1 size 100 used 1 path /dev/sda1\n\tdevid    2 size 100 used 1 path /dev/sdb1\n"
	m.refreshBtrfs(ctx)
	if status, _ := m.Status(ctx); status.Volumes[0].State != VolumeOK {
		t.Fatalf("healthy mirror: %+v", status.Volumes[0])
	}

	// With both members present it mounts at once and never degraded.
	m2, runner2 := treeManager(t, []blockDevice{member("sda"), member("sdb")})
	m2.volumes = m.volumes
	m2.mountAll(ctx)
	if len(runner2.commands) != 1 || strings.Contains(runner2.commands[0], "degraded") {
		t.Fatal(runner2.commands)
	}
}

func TestBtrfsMissingCount(t *testing.T) {
	for output, want := range map[string]int{
		"Total devices 2\n\tdevid    1 size 1 used 0 path /dev/sda1\n\tdevid    2 size 0 used 0 path  MISSING\n":  1,
		"Total devices 2\n\tdevid    1 size 1 used 0 path /dev/sda1\n":                                            1,
		"Total devices 2\n\tdevid    1 size 1 used 0 path /dev/sda1\n\tdevid    2 size 1 used 0 path /dev/sdb1\n": 0,
		"": 0,
	} {
		if got := btrfsMissing(output); got != want {
			t.Errorf("%q: %d, want %d", output, got, want)
		}
	}
}

// AddDisk takes the identity the disk list showed and refuses a disk that
// changed behind the same name; older clients without it keep working.
func TestAddDiskChecksPickedIdentity(t *testing.T) {
	members := []blockDevice{tdisk("sda", "A", raidPart("sda1")), tdisk("sdb", "B", raidPart("sdb1")), tdisk("sdc", "C", raidPart("sdc1"))}
	for _, kind := range []string{"matching", "stale", "omitted"} {
		t.Run(kind, func(t *testing.T) {
			m, runner := treeManager(t, append(slices.Clone(members), tdisk("sdx", "X")))
			m.volumes = []VolumeConfig{{Name: "data", Level: "raid5", Filesystem: "ext4", ArrayUUID: runningArrayUUID}}
			runningArray(t, m, "1000000", "sda1", "sdb1", "sdc1")
			status, err := m.Status(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			identity := status.Disks[slices.IndexFunc(status.Disks, func(d Disk) bool { return d.Name == "sdx" })].Identity
			switch kind {
			case "stale":
				runner.devices[3].Serial = "REPLACEMENT" // hot-swapped after picking
			case "omitted":
				identity = ""
			}
			err = m.AddDisk(context.Background(), "data", "sdx", true, identity)
			if kind == "stale" {
				if !errors.Is(err, ErrDiskUnavailable) || len(runner.commands) != 0 {
					t.Fatal(err, runner.commands)
				}
				return
			}
			if err != nil || !slices.Contains(runner.commands, "mdadm --manage /dev/md127 --add /dev/sdx1") {
				t.Fatal(err, runner.commands)
			}
		})
	}
}

func TestBtrfsGraceHoldsSettledOnlyWhileWaiting(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	m := &Manager{btrfsSeen: map[string]time.Time{"mirror": now}}
	m.clock = func() time.Time { return now }
	if !m.btrfsWaiting() {
		t.Fatal("a mirror within its grace is not waiting")
	}
	now = now.Add(btrfsGrace)
	if m.btrfsWaiting() {
		t.Fatal("a mirror past its grace still holds the boot check")
	}
}
