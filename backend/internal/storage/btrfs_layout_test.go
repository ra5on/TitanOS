package storage

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBtrfsUsable(t *testing.T) {
	const g = uint64(1 << 30)
	for _, c := range []struct {
		profile string
		sizes   []uint64
		want    uint64
	}{
		{"single", []uint64{4 * g, 8 * g}, 12 * g},
		{"raid1", []uint64{8 * g, 8 * g}, 8 * g},
		{"raid1", []uint64{4 * g, 8 * g}, 4 * g},          // the second copy needs another disk
		{"raid1", []uint64{4 * g, 4 * g, 8 * g}, 8 * g},   // 16 GiB raw, halved
		{"raid1c3", []uint64{8 * g, 8 * g, 8 * g}, 8 * g}, // three copies
		{"raid1c3", []uint64{4 * g, 8 * g, 8 * g, 8 * g}, 9*g + g/3},
		{"raid1", nil, 0},
	} {
		if got := btrfsUsable(c.profile, c.sizes); got != c.want {
			t.Errorf("%s %v: %d GiB, want %d GiB", c.profile, c.sizes, got/g, c.want/g)
		}
	}
}

func TestParseBtrfsBalance(t *testing.T) {
	running, percent := parseBtrfsBalance("Balance on '/mnt/tank' is running\n3 out of about 12 chunks balanced (4 considered),  75% left\n")
	if !running || percent != 25 {
		t.Errorf("running=%v percent=%v", running, percent)
	}
	if running, _ := parseBtrfsBalance("Balance on '/mnt/tank' is paused\n"); !running {
		t.Error("a paused balance is still a reshape")
	}
	if running, _ := parseBtrfsBalance("No balance found on '/mnt/tank'\n"); running {
		t.Error("no balance reported as running")
	}
}

func TestImportedProfile(t *testing.T) {
	for df, want := range map[string]string{
		"Data, single: total=1\nSystem, DUP: total=1\nMetadata, DUP: total=1\n":    "single",
		"Data, RAID1: total=1\nSystem, RAID1: total=1\nMetadata, RAID1: total=1\n": "raid1",
		"Data, RAID1C3: total=1\nMetadata, RAID1C3: total=1\n":                     "raid1c3",
		"Data, RAID0: total=1\nMetadata, RAID1: total=1\n":                         "",
		"Data, RAID1: total=1\nData, single: total=1\nMetadata, RAID1: total=1\n":  "", // half converted
		"Data, RAID1: total=1\nMetadata, DUP: total=1\n":                           "",
	} {
		if got := importedProfile(df); got != want {
			t.Errorf("%q: %q, want %q", df, got, want)
		}
	}
}

func TestBtrfsCreateProfiles(t *testing.T) {
	for _, c := range []struct {
		level string
		disks []string
		mkfs  string
		ok    bool
	}{
		{"single", []string{"sde"}, "-d single -m dup /dev/sde1", true},
		{"raid1", []string{"sde", "sdf", "nvme0n1"}, "-d raid1 -m raid1 /dev/sde1 /dev/sdf1 /dev/nvme0n1p1", true},
		{"raid1c3", []string{"sde", "sdf", "nvme0n1"}, "-d raid1c3 -m raid1c3", true},
		{"raid1c3", []string{"sde", "sdf"}, "", false},
		{"raid1", []string{"sde"}, "", false},
		{"raid5", []string{"sde", "sdf", "nvme0n1"}, "", false},
	} {
		m, runner, _ := newTestManager(t)
		err := m.Create(context.Background(), CreateRequest{Name: "tank", Level: c.level, Filesystem: "btrfs", Disks: c.disks, Erase: c.disks})
		if !c.ok {
			if err == nil || len(runner.commands) != 0 {
				t.Errorf("%s on %v accepted or changed disks: %v %v", c.level, c.disks, err, runner.commands)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", c.level, err)
		}
		if commands := strings.Join(runner.commands, "\n"); !strings.Contains(commands, "mkfs.btrfs -f -L tank "+c.mkfs) {
			t.Errorf("%s: %s", c.level, commands)
		}
		cfg, _ := m.find("tank")
		if cfg.Profile != c.level || cfg.Devices != len(c.disks) || cfg.Level != c.level {
			t.Errorf("%s: stored %+v", c.level, cfg)
		}
	}
}

// layoutRunner records the btrfs commands too and answers usage/balance.
type layoutRunner struct {
	*treeRunner
	btrfs   []string
	usage   string
	balance string
	removed chan struct{}
}

func (r *layoutRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "btrfs" {
		command := strings.Join(args, " ")
		switch {
		case strings.HasPrefix(command, "filesystem usage"):
			return r.usage, nil
		case strings.HasPrefix(command, "balance status"):
			return r.balance, nil
		case strings.HasPrefix(command, "filesystem show"):
			return r.btrfsShow, nil
		}
		r.btrfs = append(r.btrfs, command)
		if strings.HasPrefix(command, "device remove") && r.removed != nil {
			<-r.removed
		}
		return "", nil
	}
	return r.treeRunner.Run(ctx, stdin, name, args...)
}

func btrfsMember(name string) blockDevice {
	return tdisk(name, strings.ToUpper(name), blockDevice{Name: "/dev/" + name + "1", KName: "/dev/" + name + "1", Type: "part", Size: 7 << 30, FSType: "btrfs", UUID: "fs"})
}

func layoutManager(t *testing.T, devices ...blockDevice) (*Manager, *layoutRunner) {
	t.Helper()
	m, tree := treeManager(t, devices)
	runner := &layoutRunner{treeRunner: tree}
	m.runner = runner
	m.volumes = []VolumeConfig{{Name: "tank", Level: "raid1", Filesystem: "btrfs", FilesystemUUID: "fs"}}
	point := m.MountPoint("tank")
	os.MkdirAll(point, 0o755)
	tree.mounted[point] = true
	return m, runner
}

func TestAddDiskToCompleteBtrfsVolume(t *testing.T) {
	ctx := context.Background()
	m, runner := layoutManager(t, btrfsMember("sda"), btrfsMember("sdb"), tdisk("sdc", "C"))
	// An old mirror without profile/devices: RAID 1 of two disks.
	if err := m.AddBtrfsDisk(ctx, "tank", "sdc", "raid1c3", "", false); !errors.Is(err, ErrNotConfirmed) {
		t.Fatal("not confirmed:", err)
	}
	if err := m.AddBtrfsDisk(ctx, "tank", "sdc", "raid5", "", true); !errors.Is(err, ErrInvalidLevel) {
		t.Fatal("unknown profile:", err)
	}
	if err := m.AddBtrfsDisk(ctx, "tank", "sdc", "raid1c3", "", true); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(runner.btrfs, "\n")
	if !strings.Contains(got, "device add -f /dev/sdc1 "+m.MountPoint("tank")) || !strings.Contains(got, "balance start --bg -dconvert=raid1c3,soft -mconvert=raid1c3,soft") {
		t.Fatal(got)
	}
	cfg, _ := m.find("tank")
	if cfg.Profile != "raid1c3" || cfg.Devices != 3 {
		t.Fatalf("layout not saved before the balance: %+v", cfg)
	}
	if !m.Reshaping() {
		t.Fatal("a running balance must hold app updates")
	}
	if err := m.AddBtrfsDisk(ctx, "tank", "sdc", "", "", true); !errors.Is(err, ErrBusy) {
		t.Fatal("second change during the balance:", err)
	}
	// The balance ends: the next look says so.
	runner.balance = "No balance found on '/mnt/tank'\n"
	runner.devices = append(runner.devices[:2], btrfsMember("sdc"))
	m.refreshBtrfs(ctx)
	if m.Reshaping() {
		t.Fatal("still reshaping after the balance")
	}
}

func TestAddDiskReplacesMissingBtrfsMember(t *testing.T) {
	ctx := context.Background()
	m, runner := layoutManager(t, btrfsMember("sda"), tdisk("sdc", "C"))
	runner.btrfsShow = "\tTotal devices 2\n\tdevid    1 size 8589934592 used 1 path /dev/sda1\n\t*** Some devices missing\n"
	if err := m.AddDisk(ctx, "tank", "sdc", true, ""); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(runner.btrfs, "\n"); !strings.Contains(got, "replace start 2 /dev/sdc1") || strings.Contains(got, "device add") {
		t.Fatal(got)
	}
}

func TestRemoveBtrfsDisk(t *testing.T) {
	ctx := context.Background()
	m, runner := layoutManager(t, btrfsMember("sda"), btrfsMember("sdb"), btrfsMember("sdc"))
	m.volumes[0].Devices = 3
	// RAID 1 needs two disks: three may become two, but not if the data
	// does not fit.
	runner.usage = "Overall:\n    Used:   99999999999\nData,RAID1: Size:20000000000, Used:9000000000 (45.00%)\n"
	if err := m.RemoveBtrfsDisk(ctx, "tank", "sdc"); !errors.Is(err, ErrDiskTooSmall) {
		t.Fatal("too much data:", err)
	}
	runner.usage = "Data,RAID1: Size:2000000000, Used:1000000000 (50.00%)\n"
	if err := m.RemoveBtrfsDisk(ctx, "tank", "sdx"); !errors.Is(err, ErrDiskUnavailable) {
		t.Fatal("unknown disk:", err)
	}
	runner.removed = make(chan struct{})
	done := make(chan error, 1)
	m.OnReshaped = func(volume string, err error) { done <- err }
	if err := m.RemoveBtrfsDisk(ctx, "tank", "sdc"); err != nil {
		t.Fatal(err)
	}
	if !m.Reshaping() {
		t.Fatal("removing a disk must hold app updates")
	}
	status, _ := m.Status(ctx)
	if status.Volumes[0].State != VolumeReshaping {
		t.Fatalf("state while removing: %+v", status.Volumes[0])
	}
	close(runner.removed)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("removal never ended")
	}
	if cfg, _ := m.find("tank"); cfg.Devices != 2 {
		t.Fatalf("devices after removal: %+v", cfg)
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "wipefs --all --force /dev/sdc1") {
		t.Fatal("removed disk not wiped:", runner.commands)
	}
	// Two disks left: RAID 1 cannot lose another.
	runner.devices = runner.devices[:2]
	if err := m.RemoveBtrfsDisk(ctx, "tank", "sdb"); !errors.Is(err, ErrTooFewDisks) {
		t.Fatal("removed below the profile's minimum:", err)
	}
}

func TestSingleBtrfsNeverMountsDegraded(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	m, runner := treeManager(t, []blockDevice{btrfsMember("sda")})
	now := start
	m.clock = func() time.Time { return now }
	m.volumes = []VolumeConfig{{Name: "tank", Level: "single", Profile: "single", Devices: 2, Filesystem: "btrfs", FilesystemUUID: "fs"}}
	m.mountAll(context.Background())
	now = start.Add(5 * time.Minute)
	m.mountAll(context.Background())
	if len(runner.commands) != 0 {
		t.Fatal("a volume without redundancy was mounted with a disk missing:", runner.commands)
	}
	// A single disk volume is complete with its one disk.
	m.volumes[0].Devices = 1
	m.mountAll(context.Background())
	if len(runner.commands) != 1 || strings.Contains(runner.commands[0], "degraded") {
		t.Fatal(runner.commands)
	}
}
