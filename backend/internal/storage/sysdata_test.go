package storage

import (
	"context"
	"fmt"
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
