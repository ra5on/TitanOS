package storage

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// The fixture's volume "Backup" lies on the HDDs sda and sdb; "fast" has no
// rotational member (an SSD volume).
func systemDataManager(t *testing.T, mounted ...string) (*Manager, *sleepRunner) {
	t.Helper()
	m, _, _ := newTestManager(t)
	runner := &sleepRunner{}
	m.runner = runner
	m.volumes = []VolumeConfig{{Name: "Backup", Level: "raid5", Filesystem: "xfs", ArrayUUID: dataArrayUUID}, {Name: "fast", Level: "raid0", ArrayUUID: "x"}}
	live := map[string]bool{}
	for _, name := range mounted {
		live[m.mountRoot+"/"+name] = true
	}
	m.mounts = func() map[string]bool { return live }
	m.settled = true
	return m, runner
}

func TestSystemDataVolumePrefersVolumeThatNeverSleeps(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct {
		name     string
		mounted  []string
		settings DiskSleepSettings
		want     string
	}{
		{"nothing sleeps: first volume as before", []string{"fast", "Backup"}, DiskSleepSettings{}, "Backup"},
		{"default sleep time: the SSD volume", []string{"fast", "Backup"}, DiskSleepSettings{IdleMinutes: 30}, "fast"},
		{"volume set to never sleep", []string{"fast", "Backup"}, DiskSleepSettings{IdleMinutes: 30, Volumes: map[string]int{"Backup": 0}}, "Backup"},
		{"only the volume sleeps", []string{"fast", "Backup"}, DiskSleepSettings{Volumes: map[string]int{"Backup": 10}}, "fast"},
		{"one member disk sleeps", []string{"fast", "Backup"}, DiskSleepSettings{Disks: map[string]int{"serial:A1": 10}}, "fast"},
		{"every mounted volume sleeps: first one", []string{"Backup"}, DiskSleepSettings{IdleMinutes: 30}, "Backup"},
		{"nothing mounted", nil, DiskSleepSettings{IdleMinutes: 30}, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, _ := systemDataManager(t, test.mounted...)
			if err := m.SetDiskSleep(ctx, test.settings); err != nil {
				t.Fatal(err)
			}
			if got := m.SystemDataVolume(ctx); got != test.want {
				t.Fatalf("got %q, want %q", got, test.want)
			}
		})
	}
}

func TestSystemDataVolumeWaitsAndCaches(t *testing.T) {
	ctx := context.Background()
	m, runner := systemDataManager(t, "Backup", "fast")
	if err := m.SetDiskSleep(ctx, DiskSleepSettings{IdleMinutes: 30}); err != nil {
		t.Fatal(err)
	}
	m.settled = false
	if got := m.SystemDataVolume(ctx); got != "" {
		t.Fatalf("answered %q before the volumes were settled", got)
	}
	m.settled = true
	m.mu.Lock()
	m.diskSleepLoaded = false
	m.mu.Unlock()
	if got := m.SystemDataVolume(ctx); got != "" {
		t.Fatalf("answered %q before the disk sleep settings were read", got)
	}
	// Settings that stay unreadable (a damaged file) do not keep the data
	// in RAM for good: after the grace period it is the first volume.
	m.mu.Lock()
	m.systemDataWaiting = time.Now().Add(-systemDataGrace - time.Second)
	m.mu.Unlock()
	if got := m.SystemDataVolume(ctx); got != "Backup" {
		t.Fatalf("got %q with unreadable disk sleep settings after the grace period, want the first volume", got)
	}
	m.mu.Lock()
	m.diskSleepLoaded = true
	m.mu.Unlock()
	runner.calls = nil
	for range 3 {
		if got := m.SystemDataVolume(ctx); got != "fast" {
			t.Fatalf("got %q, want fast", got)
		}
	}
	if len(runner.calls) != 1 {
		t.Fatalf("disks listed %d times for three answers, want once: %v", len(runner.calls), runner.calls)
	}
	// Changed settings are seen at once.
	if err := m.SetDiskSleep(ctx, DiskSleepSettings{}); err != nil {
		t.Fatal(err)
	}
	if got := m.SystemDataVolume(ctx); got != "Backup" {
		t.Fatalf("got %q after disk sleep was switched off, want Backup", got)
	}
	// Disks that cannot be listed never move the data to the sleeping volume.
	if err := m.SetDiskSleep(ctx, DiskSleepSettings{IdleMinutes: 30}); err != nil {
		t.Fatal(err)
	}
	if got := m.SystemDataVolume(ctx); got != "fast" {
		t.Fatalf("got %q, want fast", got)
	}
	if err := m.SetDiskSleep(ctx, DiskSleepSettings{IdleMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	runner.fakeRunner.fail = "lsblk"
	if got := m.SystemDataVolume(ctx); got != "fast" {
		t.Fatalf("got %q with unlistable disks, want the volume used so far", got)
	}
}

// VolumeAsleep comes from the RAM state of the sleep timer: a volume
// sleeps once a member was sent to standby, and is awake again with the
// first activity.
func TestVolumeAsleepFollowsItsDisks(t *testing.T) {
	m, _, now := sleepManager(t)
	m.volumes = []VolumeConfig{{Name: "data", Level: "raid5", Filesystem: "xfs", ArrayUUID: dataArrayUUID}}
	stat := func(reads int) {
		for _, disk := range []string{"sda", "sdb"} {
			sleepFile(t, m, disk+"/stat", fmt.Sprintf("%d 0 8 0 1 0 8 0 0 0 0 0 0 0 0 0 0", reads))
			sleepFile(t, m, disk+"/diskseq", "7")
		}
	}
	stat(1)
	if m.VolumeAsleep("data") {
		t.Fatal("asleep before anything was observed")
	}
	sleepTicks(t, m, now, 5*time.Minute+10*time.Second)
	if !m.VolumeAsleep("data") || m.VolumeAsleep("other") || m.VolumeAsleep("") {
		t.Fatalf("data asleep=%v, other=%v, none=%v", m.VolumeAsleep("data"), m.VolumeAsleep("other"), m.VolumeAsleep(""))
	}
	stat(2)
	sleepTicks(t, m, now.Add(5*time.Minute+20*time.Second), 0)
	if m.VolumeAsleep("data") {
		t.Fatal("still asleep after activity on its disks")
	}
}

// A volume on SATA SSDs never sleeps: only rotational disks are put to
// sleep, whatever the default sleep time says.
func TestSystemDataVolumeCountsSATASSDVolumeAsAwake(t *testing.T) {
	ctx := context.Background()
	m, runner := systemDataManager(t, "Backup", "ssd")
	m.volumes = []VolumeConfig{{Name: "Backup", Level: "raid5", Filesystem: "xfs", ArrayUUID: dataArrayUUID}, {Name: "ssd", Level: "raid1", Filesystem: "ext4", ArrayUUID: "99999999:88887777:66665555:44444444"}}
	runner.disks = strings.Replace(lsblkOutput, `{"name":"/dev/sdd1","kname":"/dev/sdd1","type":"part","size":1000000000,"fstype":"ntfs","label":"Daten","uuid":"bbbb","mountpoint":null}`,
		`{"name":"/dev/sdd1","kname":"/dev/sdd1","type":"part","size":1000000000,"fstype":"linux_raid_member","label":"any:ssd","uuid":"99999999-8888-7777-6666-555544444444","mountpoint":null}`, 1)
	if runner.disks == lsblkOutput {
		t.Fatal("fixture not changed")
	}
	if err := m.SetDiskSleep(ctx, DiskSleepSettings{IdleMinutes: 30}); err != nil {
		t.Fatal(err)
	}
	status, err := m.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	member := false
	for _, disk := range status.Disks {
		member = member || disk.Name == "sdd" && disk.Volume == "ssd" && disk.Kind == "ssd"
	}
	if !member {
		t.Fatalf("fixture: sdd is no SSD member of the volume ssd: %+v", status.Disks)
	}
	if got := m.SystemDataVolume(ctx); got != "ssd" {
		t.Fatalf("got %q, want the SATA SSD volume", got)
	}
}
