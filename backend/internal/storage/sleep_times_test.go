package storage

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func sleepDisk(t *testing.T, m *Manager, name string) DiskSleepDisk {
	t.Helper()
	status, err := m.DiskSleepStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, disk := range status.DiskStatus {
		if disk.Name == name {
			return disk
		}
	}
	t.Fatalf("%s missing: %+v", name, status.DiskStatus)
	return DiskSleepDisk{}
}

// The settings show since when a disk sleeps and when it was last woken,
// from RAM only and without any further disk command.
func TestDiskSleepTimes(t *testing.T) {
	m, runner, now := sleepManager(t)
	data, _ := json.Marshal(sleepDisk(t, m, "sde"))
	if strings.Contains(string(data), "since") {
		t.Fatalf("an unknown time must be left out: %s", data)
	}
	sleepTicks(t, m, now, 5*time.Minute)
	standby := now.Add(5 * time.Minute)
	if disk := sleepDisk(t, m, "sde"); disk.State != "asleep" || !disk.Since.Equal(standby) {
		t.Fatalf("asleep since the standby tick %v: %+v", standby, disk)
	}
	data, _ = json.Marshal(sleepDisk(t, m, "sde"))
	if !strings.Contains(string(data), `"since":"`+standby.Format(time.RFC3339)+`"`) {
		t.Fatalf("since as RFC 3339: %s", data)
	}
	// Still asleep a while later: the time stays that of the standby.
	sleepTicks(t, m, standby.Add(10*time.Second), time.Minute)
	if disk := sleepDisk(t, m, "sde"); !disk.Since.Equal(standby) {
		t.Fatalf("since moved while asleep: %+v", disk)
	}
	// Activity wakes it: the first tick that sees it is the wake.
	sleepStat(t, m, 2, 1, 0, 0)
	woken := standby.Add(80 * time.Second)
	sleepTicks(t, m, woken, time.Minute)
	if disk := sleepDisk(t, m, "sde"); disk.State != "active" || !disk.Since.Equal(woken) {
		t.Fatalf("woken at %v: %+v", woken, disk)
	}
	// A changed setting forgets the sleep state, not the last wake.
	if err := m.SetDiskSleep(context.Background(), DiskSleepSettings{IdleMinutes: 10}); err != nil {
		t.Fatal(err)
	}
	if disk := sleepDisk(t, m, "sde"); disk.State != "active" || !disk.Since.Equal(woken) {
		t.Fatalf("last wake lost on reset: %+v", disk)
	}
	// Asleep again: the standby time replaces the wake.
	sleepTicks(t, m, woken.Add(70*time.Second), 10*time.Minute)
	again := woken.Add(70*time.Second + 10*time.Minute)
	if disk := sleepDisk(t, m, "sde"); disk.State != "asleep" || !disk.Since.Equal(again) {
		t.Fatalf("asleep again since %v: %+v", again, disk)
	}
	if len(runner.standbyCalls()) != 2 {
		t.Fatalf("standby calls: %v", runner.standbyCalls())
	}
	for _, command := range runner.calls {
		if !strings.HasPrefix(command, "hd-idle ") && !strings.HasPrefix(command, "lsblk ") {
			t.Fatalf("the times must not query a disk: %s", command)
		}
	}
}

func TestDiskSleepTimesGap(t *testing.T) {
	m, _, now := sleepManager(t)
	sleepTicks(t, m, now, 5*time.Minute)
	// Activity seen only after an observation gap is no known wake.
	sleepStat(t, m, 2, 1, 0, 0)
	sleepTicks(t, m, now.Add(10*time.Minute), 0)
	if disk := sleepDisk(t, m, "sde"); disk.State != "active" || !disk.Since.IsZero() {
		t.Fatalf("gap counted as wake: %+v", disk)
	}
}

func TestDiskSleepTimesFailedStandby(t *testing.T) {
	m, runner, now := sleepManager(t)
	runner.fail = "hd-idle"
	sleepTicks(t, m, now, 6*time.Minute)
	sleepStat(t, m, 3, 1, 0, 0)
	sleepTicks(t, m, now.Add(6*time.Minute+10*time.Second), 0)
	if disk := sleepDisk(t, m, "sde"); disk.State != "active" || !disk.Since.IsZero() {
		t.Fatalf("a failed standby shows a time: %+v", disk)
	}
}

func TestDiskSleepTimesReplacedDisk(t *testing.T) {
	m, _, now := sleepManager(t)
	sleepTicks(t, m, now, 5*time.Minute)
	if disk := sleepDisk(t, m, "sde"); disk.State != "asleep" || disk.Since.IsZero() {
		t.Fatalf("asleep: %+v", disk)
	}
	// Another disk under the same name drops the times of the old one.
	sleepFile(t, m, "sde/diskseq", "124")
	sleepTicks(t, m, now.Add(5*time.Minute+10*time.Second), 0)
	if disk := sleepDisk(t, m, "sde"); disk.State != "active" || !disk.Since.IsZero() {
		t.Fatalf("times of a replaced disk kept: %+v", disk)
	}
}
