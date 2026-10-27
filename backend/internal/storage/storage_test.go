package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// Four data disks and the stick, as lsblk reports them. sda/sdb/sdc form the
// array 11111111:… (volume "data"), sdd holds an old NTFS partition, sde is empty.
const lsblkOutput = `{"blockdevices": [
 {"name":"/dev/sda","kname":"/dev/sda","type":"disk","size":8589934592,"rota":true,"rm":false,"tran":"sata","model":"DISK A ","serial":"A1","fstype":null,"label":null,"uuid":null,"mountpoint":null,"children":[
   {"name":"/dev/sda1","kname":"/dev/sda1","type":"part","size":8453619712,"rota":true,"rm":false,"tran":null,"fstype":"linux_raid_member","label":"any:data","uuid":"11111111-2222-3333-4444-555555555555","mountpoint":null,"children":[
     {"name":"/dev/md127","kname":"/dev/md127","type":"raid5","size":16890462208,"fstype":"xfs","label":"data","uuid":"aaaa","mountpoint":"/mnt/data"}]}]},
 {"name":"/dev/sdb","kname":"/dev/sdb","type":"disk","size":8589934592,"rota":true,"rm":false,"tran":"sata","model":"DISK B","serial":"B1","fstype":null,"children":[
   {"name":"/dev/sdb1","kname":"/dev/sdb1","type":"part","size":8453619712,"fstype":"linux_raid_member","uuid":"11111111-2222-3333-4444-555555555555","mountpoint":null}]},
 {"name":"/dev/sdd","kname":"/dev/sdd","type":"disk","size":8589934592,"rota":false,"rm":false,"tran":"sata","model":"SSD D","serial":"D1","fstype":null,"children":[
   {"name":"/dev/sdd1","kname":"/dev/sdd1","type":"part","size":1000000000,"fstype":"ntfs","label":"Daten","uuid":"bbbb","mountpoint":null}]},
 {"name":"/dev/sde","kname":"/dev/sde","type":"disk","size":8589934592,"rota":true,"rm":false,"tran":"sata","model":"DISK E","serial":"E1","fstype":null},
 {"name":"/dev/sdf","kname":"/dev/sdf","type":"disk","size":4294967296,"rota":true,"rm":false,"tran":"sata","model":"DISK F","serial":"F1","fstype":null},
 {"name":"/dev/nvme0n1","kname":"/dev/nvme0n1","type":"disk","size":8589934592,"rota":false,"rm":false,"tran":"nvme","model":"NVME","serial":"N1","fstype":null},
 {"name":"/dev/sdz","kname":"/dev/sdz","type":"disk","size":2147483648,"rota":true,"rm":true,"tran":"usb","model":"Stick","serial":"S1","fstype":null,"children":[
   {"name":"/dev/sdz1","kname":"/dev/sdz1","type":"part","size":2146435072,"fstype":"vfat","label":"TITANOS","uuid":"cccc","mountpoint":"/efi"}]},
 {"name":"/dev/loop0","kname":"/dev/loop0","type":"loop","size":153317376,"fstype":"squashfs","mountpoint":"/run/live/rootfs/filesystem.squashfs"},
 {"name":"/dev/sr0","kname":"/dev/sr0","type":"rom","size":1073741312,"fstype":null}
]}`

const dataArrayUUID = "11111111:22223333:44445555:55555555"

// fakeRunner records commands and answers the ones that only read.
type fakeRunner struct {
	commands []string
	stdin    []string
	fail     string // a command starting with this fails
	mounted  map[string]bool
}

func (r *fakeRunner) Run(_ context.Context, stdin string, name string, args ...string) (string, error) {
	command := name + " " + strings.Join(args, " ")
	if r.fail != "" && strings.HasPrefix(command, r.fail) {
		return "", &CommandError{Command: command, Err: errors.New("exit status 1"), Output: "simulated failure"}
	}
	switch {
	case name == "lsblk":
		return lsblkOutput, nil
	case name == "mdadm" && slices.Contains(args, "--detail"):
		return "MD_LEVEL=raid5\nMD_UUID=99999999:88888888:77777777:66666666\nMD_NAME=any:new\n", nil
	case name == "blkid":
		return "0f0f0f0f-aaaa-bbbb-cccc-121212121212\n", nil
	}
	r.commands = append(r.commands, command)
	r.stdin = append(r.stdin, stdin)
	if r.mounted != nil && name == "mount" {
		r.mounted[args[len(args)-1]] = true
	}
	if r.mounted != nil && name == "umount" {
		delete(r.mounted, args[len(args)-1])
	}
	return "", nil
}

func newTestManager(t *testing.T) (*Manager, *fakeRunner, *config.Store) {
	t.Helper()
	store := config.Open(t.TempDir())
	runner := &fakeRunner{mounted: map[string]bool{}}
	m := NewManager(store)
	m.runner = runner
	m.mounts = func() map[string]bool { return runner.mounted }
	m.mountRoot = t.TempDir()
	m.sysBlock = t.TempDir() // no arrays
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, runner, store
}

func TestClassifyDisks(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.volumes = []VolumeConfig{{Name: "data", Level: "raid5", Filesystem: "xfs", ArrayUUID: dataArrayUUID}}
	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	got := map[string]Disk{}
	for _, disk := range status.Disks {
		got[disk.Name] = disk
	}
	if len(got) != 7 {
		t.Fatalf("%d disks, want 7 (no loop device, no optical drive): %v", len(got), got)
	}
	for name, want := range map[string]Disk{
		"sda":     {Role: RoleMember, Volume: "data", Kind: "hdd"},
		"sdb":     {Role: RoleMember, Volume: "data", Kind: "hdd"},
		"sdd":     {Role: RoleUsed, Kind: "ssd"},
		"sde":     {Role: RoleFree, Kind: "hdd"},
		"nvme0n1": {Role: RoleFree, Kind: "nvme"},
		"sdz":     {Role: RoleStick, Kind: "usb"},
	} {
		disk := got[name]
		if disk.Role != want.Role || disk.Volume != want.Volume || disk.Kind != want.Kind {
			t.Errorf("%s: role=%q volume=%q kind=%q, want %q %q %q", name, disk.Role, disk.Volume, disk.Kind, want.Role, want.Volume, want.Kind)
		}
	}
	if !slices.Equal(got["sdd"].Contents, []string{"ntfs"}) {
		t.Errorf("sdd contents = %v", got["sdd"].Contents)
	}
	// The filesystem on top of the array is the volume's, not the disk's.
	if got["sda"].Mounted || len(got["sda"].Contents) != 0 {
		t.Errorf("sda = %+v: the mounted array must not count as content of the disk", got["sda"])
	}
	if !got["sdz"].Mounted || got["sda"].Model != "DISK A" {
		t.Errorf("stick mounted=%v, model %q", got["sdz"].Mounted, got["sda"].Model)
	}
	// The array of "data" is not running in this test.
	if len(status.Volumes) != 1 || status.Volumes[0].State != VolumeMissing {
		t.Errorf("volumes = %+v", status.Volumes)
	}
}

func TestArrayState(t *testing.T) {
	tests := []struct {
		name  string
		array array
		want  string
	}{
		{"healthy", array{state: "clean", syncAction: "idle"}, VolumeOK},
		{"disk missing", array{state: "active", syncAction: "idle", degraded: 1}, VolumeDegraded},
		{"rebuilding", array{state: "active", syncAction: "recover", degraded: 1, syncDone: 500, syncTotal: 1000, syncSpeedKiB: 50}, VolumeRebuilding},
		{"first sync", array{state: "active", syncAction: "resync", syncDone: 1, syncTotal: 1000}, VolumeSyncing},
		{"checking", array{state: "clean", syncAction: "check", syncDone: 250, syncTotal: 1000}, VolumeChecking},
		{"not running", array{state: "inactive"}, VolumeFailed},
	}
	for _, tt := range tests {
		state, sync := arrayState(tt.array)
		if state != tt.want {
			t.Errorf("%s: state = %q, want %q", tt.name, state, tt.want)
		}
		if tt.name == "rebuilding" && (sync == nil || sync.Percent != 50 || sync.RemainingSeconds != 5 || sync.SpeedBytesPerSecond != 51200) {
			t.Errorf("rebuilding: sync = %+v", sync)
		}
	}
}

func TestMemberState(t *testing.T) {
	for _, tt := range []struct {
		member arrayMember
		want   string
	}{
		{arrayMember{state: "in_sync", slot: "0"}, MemberActive},
		{arrayMember{state: "faulty", slot: "none"}, MemberFaulty},
		{arrayMember{state: "faulty,in_sync", slot: "1"}, MemberFaulty},
		{arrayMember{state: "spare", slot: "2"}, MemberRebuilding},
		{arrayMember{state: "spare", slot: "none"}, MemberSpare},
	} {
		if got := memberState(tt.member); got != tt.want {
			t.Errorf("memberState(%+v) = %q, want %q", tt.member, got, tt.want)
		}
	}
}

func TestReadArrays(t *testing.T) {
	sys := t.TempDir()
	write := func(path, content string) {
		full := filepath.Join(sys, path)
		os.MkdirAll(filepath.Dir(full), 0o755)
		os.WriteFile(full, []byte(content+"\n"), 0o644)
	}
	write("md127/size", "32989184")
	write("md127/md/level", "raid5")
	write("md127/md/array_state", "active")
	write("md127/md/degraded", "1")
	write("md127/md/raid_disks", "3")
	write("md127/md/sync_action", "recover")
	write("md127/md/sync_completed", "4194304 / 16494592")
	write("md127/md/sync_speed", "102400")
	write("md127/md/dev-sda1/state", "in_sync")
	write("md127/md/dev-sda1/slot", "0")
	write("md127/md/dev-sdd1/state", "spare")
	write("md127/md/dev-sdd1/slot", "2")
	write("sda/size", "16777216") // not an array

	arrays := readArrays(sys, func(device string) string { return dataArrayUUID })
	if len(arrays) != 1 {
		t.Fatalf("%d arrays, want 1", len(arrays))
	}
	a := arrays[0]
	if a.device != "md127" || a.level != "raid5" || a.degraded != 1 || a.disks != 3 || a.syncDone != 4194304 || a.syncTotal != 16494592 || len(a.members) != 2 {
		t.Errorf("array = %+v", a)
	}
	if a.uuid != normalizeUUID("11111111-2222-3333-4444-555555555555") {
		t.Errorf("mdadm and lsblk spell the UUID differently and must still match: %q", a.uuid)
	}
}

func TestParseSmart(t *testing.T) {
	ata := `{"smart_status":{"passed":true},"temperature":{"current":34},"power_on_time":{"hours":21408},"rotation_rate":7200,
	 "ata_smart_attributes":{"table":[{"id":5,"raw":{"value":2}},{"id":9,"raw":{"value":21408}},{"id":197,"raw":{"value":1}}]}}`
	if s := parseSmart(ata); s == nil || !s.Healthy || s.TemperatureC != 34 || s.PowerOnHours != 21408 || s.Errors != 3 || s.RotationRate != 7200 || s.WearPercent != -1 {
		t.Errorf("ATA: %+v", s)
	}
	nvme := `{"smart_status":{"passed":false},"temperature":{"current":41},"power_on_time":{"hours":9812},
	 "nvme_smart_health_information_log":{"percentage_used":4,"media_errors":7}}`
	if s := parseSmart(nvme); s == nil || s.Healthy || s.WearPercent != 4 || s.Errors != 7 {
		t.Errorf("NVMe: %+v", s)
	}
	for _, none := range []string{"", "not json", `{"smartctl":{"exit_status":4}}`} {
		if s := parseSmart(none); s != nil {
			t.Errorf("parseSmart(%q) = %+v, want nil", none, s)
		}
	}
}

func TestCreateValidation(t *testing.T) {
	m, runner, _ := newTestManager(t)
	m.volumes = []VolumeConfig{{Name: "data", Level: "raid5", ArrayUUID: dataArrayUUID}}
	ctx := context.Background()

	three := []string{"sdd", "sde", "nvme0n1"}
	tests := []struct {
		name string
		req  CreateRequest
		want error
	}{
		{"bad name", CreateRequest{Name: "Meine Daten", Level: "raid1", Filesystem: "ext4", Disks: three[:2], Erase: three[:2]}, ErrInvalidName},
		{"name too long for XFS", CreateRequest{Name: "abcdefghijklm", Level: "raid1", Filesystem: "xfs", Disks: three[:2], Erase: three[:2]}, ErrInvalidName},
		{"name taken with different case", CreateRequest{Name: "Data", Level: "raid1", Filesystem: "ext4", Disks: three[:2], Erase: three[:2]}, ErrExists},
		{"name taken", CreateRequest{Name: "data", Level: "raid1", Filesystem: "ext4", Disks: three[:2], Erase: three[:2]}, ErrExists},
		{"unknown level", CreateRequest{Name: "new", Level: "raid7", Filesystem: "ext4", Disks: three, Erase: three}, ErrInvalidLevel},
		{"unknown filesystem", CreateRequest{Name: "new", Level: "raid1", Filesystem: "ntfs", Disks: three[:2], Erase: three[:2]}, ErrInvalidFS},
		{"RAID 0 without disks", CreateRequest{Name: "new", Level: "raid0", Filesystem: "ext4"}, ErrTooFewDisks},
		{"RAID 1 with one disk", CreateRequest{Name: "new", Level: "raid1", Filesystem: "ext4", Disks: three[:1], Erase: three[:1]}, ErrTooFewDisks},
		{"RAID 5 with two disks", CreateRequest{Name: "new", Level: "raid5", Filesystem: "ext4", Disks: three[:2], Erase: three[:2]}, ErrTooFewDisks},
		{"RAID 6 with three disks", CreateRequest{Name: "new", Level: "raid6", Filesystem: "ext4", Disks: three, Erase: three}, ErrTooFewDisks},
		{"RAID 10 with two disks", CreateRequest{Name: "new", Level: "raid10", Filesystem: "ext4", Disks: three[:2], Erase: three[:2]}, ErrTooFewDisks},
		{"single-disk RAID 0 erase not confirmed", CreateRequest{Name: "new", Level: "raid0", Filesystem: "ext4", Disks: three[:1]}, ErrNotConfirmed},
		{"erase not confirmed", CreateRequest{Name: "new", Level: "raid5", Filesystem: "ext4", Disks: three}, ErrNotConfirmed},
		{"erase confirms other disks", CreateRequest{Name: "new", Level: "raid5", Filesystem: "ext4", Disks: three, Erase: []string{"sdd", "sde", "sdf"}}, ErrNotConfirmed},
		{"same disk twice", CreateRequest{Name: "new", Level: "raid1", Filesystem: "ext4", Disks: []string{"sde", "sde"}, Erase: []string{"sde", "sde"}}, ErrNotConfirmed},
		{"disk of another volume", CreateRequest{Name: "new", Level: "raid1", Filesystem: "ext4", Disks: []string{"sda", "sde"}, Erase: []string{"sda", "sde"}}, ErrDiskUnavailable},
		{"the boot stick", CreateRequest{Name: "new", Level: "raid1", Filesystem: "ext4", Disks: []string{"sdz", "sde"}, Erase: []string{"sdz", "sde"}}, ErrDiskUnavailable},
		{"single-disk RAID 0 on the boot stick", CreateRequest{Name: "new", Level: "raid0", Filesystem: "ext4", Disks: []string{"sdz"}, Erase: []string{"sdz"}}, ErrDiskUnavailable},
		{"unknown disk", CreateRequest{Name: "new", Level: "raid1", Filesystem: "ext4", Disks: []string{"sdq", "sde"}, Erase: []string{"sdq", "sde"}}, ErrDiskUnavailable},
	}
	for _, tt := range tests {
		if err := m.Create(ctx, tt.req); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
	if len(runner.commands) != 0 {
		t.Errorf("a rejected request ran commands: %v", runner.commands)
	}
}

func TestCreate(t *testing.T) {
	m, runner, store := newTestManager(t)
	// sdf is the smallest disk (4 GiB): every member gets its size.
	req := CreateRequest{Name: "Backup", Level: "raid5", Filesystem: "xfs", Disks: []string{"sdd", "sde", "sdf"}, Erase: []string{"sdf", "sde", "sdd"}}
	if err := m.Create(context.Background(), req); err != nil {
		t.Fatal(err)
	}

	all := strings.Join(runner.commands, "\n")
	for _, want := range []string{
		"wipefs --all --force /dev/sdd1", // the old NTFS partition goes first …
		"wipefs --all --force /dev/sdd",  // … then the disk itself
		"sfdisk --quiet --wipe always --wipe-partitions always /dev/sde",
		"mdadm --create /dev/md/Backup --run --quiet --metadata=1.2 --homehost=any --name=Backup --level=5 --raid-devices=3 --bitmap=internal /dev/sdd1 /dev/sde1 /dev/sdf1",
		"mkfs.xfs -f -q -L Backup /dev/md/Backup",
		"mount -t xfs -o noatime UUID=0f0f0f0f-aaaa-bbbb-cccc-121212121212 " + m.mountRoot + "/Backup",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("missing command %q in:\n%s", want, all)
		}
	}
	if strings.Index(all, "wipefs --all --force /dev/sdd1") > strings.Index(all, "wipefs --all --force /dev/sdd\n") {
		t.Error("the partition must be wiped before the disk")
	}
	for _, untouched := range []string{"/dev/sda", "/dev/sdb", "/dev/sdz", "/dev/nvme0n1"} {
		if strings.Contains(all, untouched) {
			t.Errorf("a command touched %s, which is not part of the request:\n%s", untouched, all)
		}
	}
	// 4 GiB minus 1 MiB at the start and 128 MiB reserve.
	wantTable := "label: gpt\nstart=1MiB, size=3967MiB, type=" + raidPartitionType + ", name=titanos-Backup\n"
	if !slices.Contains(runner.stdin, wantTable) {
		t.Errorf("partition table = %q, want %q", runner.stdin, wantTable)
	}

	var saved []VolumeConfig
	if err := store.ReadJSON(configFile, &saved); err != nil {
		t.Fatal(err)
	}
	if len(saved) != 1 || saved[0].Name != "Backup" || saved[0].ArrayUUID != "99999999:88888888:77777777:66666666" || saved[0].FilesystemUUID != "0f0f0f0f-aaaa-bbbb-cccc-121212121212" {
		t.Errorf("saved = %+v", saved)
	}
}

func TestCreateRaid0HasNoBitmapAndExt4Options(t *testing.T) {
	m, runner, _ := newTestManager(t)
	req := CreateRequest{Name: "scratch", Level: "raid0", Filesystem: "ext4", Disks: []string{"sde", "nvme0n1"}, Erase: []string{"sde", "nvme0n1"}}
	if err := m.Create(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	all := strings.Join(runner.commands, "\n")
	if strings.Contains(all, "--bitmap") {
		t.Error("RAID 0 cannot have a bitmap")
	}
	if !strings.Contains(all, "--level=0 --raid-devices=2 /dev/sde1 /dev/nvme0n1p1") {
		t.Errorf("NVMe partitions are named p1:\n%s", all)
	}
	if !strings.Contains(all, "mkfs.ext4 -F -q -L scratch -m 0 /dev/md/scratch") {
		t.Errorf("mkfs.ext4 call missing:\n%s", all)
	}
	for _, command := range runner.commands {
		if strings.HasPrefix(command, "mdadm --create ") && strings.Contains(command, "--force") {
			t.Errorf("multi-disk RAID 0 must not force creation: %s", command)
		}
	}
}

func TestCreateSingleDiskRaid0(t *testing.T) {
	for _, tt := range []struct {
		filesystem string
		disk       string
		partition  string
		mkfs       string
	}{
		{"ext4", "nvme0n1", "nvme0n1p1", "mkfs.ext4 -F -q -L scratch -m 0 /dev/md/scratch"},
		{"xfs", "sde", "sde1", "mkfs.xfs -f -q -L scratch /dev/md/scratch"},
	} {
		t.Run(tt.filesystem, func(t *testing.T) {
			m, runner, store := newTestManager(t)
			req := CreateRequest{Name: "scratch", Level: "raid0", Filesystem: tt.filesystem, Disks: []string{tt.disk}, Erase: []string{tt.disk}}
			if err := m.Create(context.Background(), req); err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				"mdadm --create /dev/md/scratch --run --quiet --metadata=1.2 --homehost=any --name=scratch --level=0 --force --raid-devices=1 /dev/" + tt.partition,
				tt.mkfs,
				"mount -t " + tt.filesystem + " -o noatime UUID=0f0f0f0f-aaaa-bbbb-cccc-121212121212 " + m.mountRoot + "/scratch",
			} {
				if !slices.Contains(runner.commands, want) {
					t.Errorf("missing command %q in:\n%s", want, strings.Join(runner.commands, "\n"))
				}
			}
			var saved []VolumeConfig
			if err := store.ReadJSON(configFile, &saved); err != nil {
				t.Fatal(err)
			}
			if len(saved) != 1 || saved[0].Level != "raid0" || saved[0].Filesystem != tt.filesystem || saved[0].ArrayUUID == "" || saved[0].FilesystemUUID == "" {
				t.Errorf("single-disk volume must persist for reassembly and mounting: %+v", saved)
			}
		})
	}
}

func TestCreateStopsAtTheFirstFailure(t *testing.T) {
	m, runner, store := newTestManager(t)
	runner.fail = "mdadm --create"
	req := CreateRequest{Name: "media", Level: "raid1", Filesystem: "ext4", Disks: []string{"sde", "sdf"}, Erase: []string{"sde", "sdf"}}
	err := m.Create(context.Background(), req)
	var commandErr *CommandError
	if !errors.As(err, &commandErr) {
		t.Fatalf("err = %v, want the failed command", err)
	}
	if all := strings.Join(runner.commands, "\n"); strings.Contains(all, "mkfs") || strings.Contains(all, "mount") {
		t.Errorf("commands after the failure:\n%s", all)
	}
	if _, err := store.Read(configFile); err == nil {
		t.Error("a volume that was not created was saved")
	}
}

func TestDeleteNeedsTheName(t *testing.T) {
	m, runner, _ := newTestManager(t)
	m.volumes = []VolumeConfig{{Name: "data", Level: "raid5", ArrayUUID: dataArrayUUID}}
	ctx := context.Background()

	if err := m.Delete(ctx, "data", ""); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("without confirmation: %v", err)
	}
	if err := m.Delete(ctx, "data", "Data"); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("wrong confirmation: %v", err)
	}
	if err := m.Delete(ctx, "nope", "nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown volume: %v", err)
	}
	if len(runner.commands) != 0 || len(m.configured()) != 1 {
		t.Errorf("a rejected delete changed something: %v", runner.commands)
	}
	if err := m.Delete(ctx, "data", "data"); err != nil {
		t.Fatal(err)
	}
	if len(m.configured()) != 0 {
		t.Error("volume still configured after delete")
	}
}

func TestAddDiskNeedsConfirmationAndARunningVolume(t *testing.T) {
	m, runner, _ := newTestManager(t)
	m.volumes = []VolumeConfig{{Name: "data", Level: "raid5", ArrayUUID: dataArrayUUID}, {Name: "fast", Level: "raid0", ArrayUUID: "x"}}
	ctx := context.Background()

	if err := m.AddDisk(ctx, "data", "sde", false, ""); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("without confirmation: %v", err)
	}
	if err := m.AddDisk(ctx, "fast", "sde", true, ""); !errors.Is(err, ErrNotPossible) {
		t.Errorf("RAID 0: %v", err)
	}
	if err := m.AddDisk(ctx, "data", "sde", true, ""); !errors.Is(err, ErrNotPossible) {
		t.Errorf("array not running: %v", err)
	}
	if len(runner.commands) != 0 {
		t.Errorf("commands ran: %v", runner.commands)
	}
}

func TestReadOnlyRefusesEveryAction(t *testing.T) {
	m, runner, _ := newTestManager(t)
	m.volumes = []VolumeConfig{{Name: "data", Level: "raid5", ArrayUUID: dataArrayUUID}}
	m.ReadOnly = true
	ctx := context.Background()

	disks := []string{"sde", "sdf"}
	for name, err := range map[string]error{
		"create": m.Create(ctx, CreateRequest{Name: "new", Level: "raid1", Filesystem: "ext4", Disks: disks, Erase: disks}),
		"delete": m.Delete(ctx, "data", "data"),
		"add":    m.AddDisk(ctx, "data", "sde", true, ""),
		"remove": m.RemoveDisk(ctx, "data", "sda"),
		"check":  m.Check(ctx, "data", true),
	} {
		if !errors.Is(err, ErrReadOnly) {
			t.Errorf("%s: err = %v, want ErrReadOnly", name, err)
		}
	}
	m.mountAll(ctx)
	if len(runner.commands) != 0 || len(m.configured()) != 1 {
		t.Errorf("read-only mode ran commands: %v", runner.commands)
	}
}

func TestOwnsMatchesArrayAndFilesystemSpellings(t *testing.T) {
	m := &Manager{volumes: []VolumeConfig{{Name: "data", ArrayUUID: "0f0e0d0c:0b0a0908:07060504:03020100", FilesystemUUID: "5D2C-AA01"}}}
	for uuid, want := range map[string]bool{
		"0f0e0d0c-0b0a-0908-0706-050403020100": true, // lsblk on a member
		"0F0E0D0C:0B0A0908:07060504:03020100":  true,
		"5d2c-aa01":                            true,
		"5D2CAA01":                             true,
		"0f0e0d0c-0b0a-0908-0706-050403020101": false,
		"":                                     false,
	} {
		if m.Owns(uuid) != want {
			t.Errorf("Owns(%q) != %v", uuid, want)
		}
	}
}
