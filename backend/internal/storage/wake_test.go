package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/diskwake"
)

// wakeManager: sde and sdg are HDDs of volume "tank", sdf is a loose HDD;
// "fast" is a volume without a disk that sleeps (the system data volume).
// All three HDDs are asleep at the returned time.
func wakeManager(t *testing.T) (*Manager, *sleepRunner, *diskwake.Recorder, *time.Time) {
	t.Helper()
	m, runner, _ := overrideManager(t)
	m.volumes = append(m.volumes, VolumeConfig{Name: "fast", Level: "raid0", ArrayUUID: "x"})
	live := map[string]bool{}
	for _, name := range []string{"tank", "fast"} {
		live[m.mountRoot+"/"+name] = true
		if err := os.MkdirAll(filepath.Join(m.mountRoot, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m.mounts = func() map[string]bool { return live }
	now := time.Unix(1_800_000_000, 0)
	recorder := &diskwake.Recorder{Now: func() time.Time { return now }}
	m.wakeRecorder = recorder
	m.clock = func() time.Time { return now }
	m.WakeDataVolume = func() string { return "fast" }
	if err := m.SetDiskSleep(context.Background(), DiskSleepSettings{IdleMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	wakeTicks(t, m, &now, 5*time.Minute+10*time.Second)
	for _, name := range []string{"sde", "sdf", "sdg"} {
		if disk := sleepDisk(t, m, name); disk.State != "asleep" {
			t.Fatalf("%s not asleep: %+v", name, disk)
		}
	}
	return m, runner, recorder, &now
}

// wakeTicks lets the sleep timer look every ten seconds for a while.
func wakeTicks(t *testing.T, m *Manager, now *time.Time, duration time.Duration) {
	t.Helper()
	for end := now.Add(duration); !now.After(end); *now = now.Add(10 * time.Second) {
		if err := m.sleepIdleDisks(context.Background(), *now); err != nil {
			t.Fatal(err)
		}
	}
}

// wakeIO lets a disk show one more read.
func wakeIO(t *testing.T, m *Manager, name string, reads int) {
	t.Helper()
	sleepFile(t, m, name+"/stat", fmt.Sprintf("%d 0 8 0 1 0 8 0 0 0 0", reads))
}

func causeCodes(wake DiskWake) string {
	var codes []string
	for _, cause := range wake.Causes {
		codes = append(codes, strings.TrimSuffix(cause.Code+":"+cause.Detail, ":"))
	}
	return strings.Join(codes, ",")
}

// What TitanOS noted for the volume around the first I/O is what woke the
// disk; several causes are all named, newest first.
func TestDiskWakeCause(t *testing.T) {
	m, _, recorder, now := wakeManager(t)
	// Long before the wake: outside the window.
	recorder.Note("tank", diskwake.RecycleSweep, "")
	wakeTicks(t, m, now, 2*time.Minute)
	recorder.Note("tank", diskwake.ActivityLog, "")
	*now = now.Add(3 * time.Second)
	recorder.Note("tank", diskwake.BackupJob, "nightly")
	recorder.Note("fast", diskwake.Journal, "") // another volume
	wakeIO(t, m, "sde", 2)
	*now = now.Add(7 * time.Second)
	wakeTicks(t, m, now, 0)
	disk := sleepDisk(t, m, "sde")
	if disk.State != "active" || len(disk.Wakes) != 1 || !disk.Wakes[0].At.Equal(disk.Since) {
		t.Fatalf("wake not recorded: %+v", disk)
	}
	if got := causeCodes(disk.Wakes[0]); got != "backup_job:nightly,activity_log" {
		t.Fatalf("causes %q", got)
	}
	if disk.WakesToday != 1 || len(disk.WakeDays) != 1 || disk.WakeDays[0].Count != 1 {
		t.Fatalf("count per day: %+v", disk)
	}
	// The general status, which every account and the tools read, names
	// neither accounts nor jobs.
	if status, err := m.Status(context.Background()); err != nil {
		t.Fatal(err)
	} else if data, _ := json.Marshal(status); strings.Contains(string(data), "wakes") || strings.Contains(string(data), "nightly") {
		t.Fatalf("wake history in the general status: %s", data)
	}
	// The other member of the volume did no I/O: still asleep, no wake.
	if other := sleepDisk(t, m, "sdg"); other.State != "asleep" || len(other.Wakes) != 0 {
		t.Fatalf("sdg: %+v", other)
	}
	data, _ := json.Marshal(disk)
	if !strings.Contains(string(data), `"wakes":[{"at":"`) || !strings.Contains(string(data), `"causes":[{"code":"backup_job","detail":"nightly"},{"code":"activity_log"}]`) {
		t.Fatalf("status: %s", data)
	}
}

// A write noted shortly before reaches the disk when the kernel flushes it:
// it still counts within the lead time, not after it.
func TestDiskWakeWindow(t *testing.T) {
	for _, test := range []struct {
		before time.Duration
		want   string
	}{{40 * time.Second, "smart_history"}, {70 * time.Second, "unknown"}} {
		m, _, recorder, now := wakeManager(t)
		wakeTicks(t, m, now, 2*time.Minute)
		recorder.Note("tank", diskwake.SmartHistory, "")
		wakeTicks(t, m, now, test.before-10*time.Second)
		wakeIO(t, m, "sdg", 2)
		wakeTicks(t, m, now, 0)
		if disk := sleepDisk(t, m, "sdg"); len(disk.Wakes) != 1 || causeCodes(disk.Wakes[0]) != test.want {
			t.Fatalf("noted %v before: %+v, want %s", test.before, disk.Wakes, test.want)
		}
	}
}

// What was noted before the disk went to sleep did not wake it.
func TestDiskWakeIgnoresNotesBeforeStandby(t *testing.T) {
	m, _, recorder, now := wakeManager(t)
	asleep := sleepDisk(t, m, "sde").Since
	recorder.Now = func() time.Time { return asleep.Add(-time.Second) }
	recorder.Note("tank", diskwake.FileIndex, "")
	recorder.Now = func() time.Time { return *now }
	wakeIO(t, m, "sde", 2)
	wakeTicks(t, m, now, 0)
	if disk := sleepDisk(t, m, "sde"); len(disk.Wakes) != 1 || causeCodes(disk.Wakes[0]) != "unknown" {
		t.Fatalf("%+v", disk.Wakes)
	}
}

// Nothing noted: not TitanOS itself. Then whoever is connected is asked,
// and only then. A disk outside every volume is always unknown.
func TestDiskWakeUnknownAndProbe(t *testing.T) {
	m, _, recorder, now := wakeManager(t)
	var asked []string
	m.WakeProbe = func(_ context.Context, volume string) []diskwake.Cause {
		asked = append(asked, volume)
		return nil
	}
	wakeIO(t, m, "sde", 2)
	wakeIO(t, m, "sdf", 2)
	wakeTicks(t, m, now, 0)
	for _, name := range []string{"sde", "sdf"} {
		if disk := sleepDisk(t, m, name); len(disk.Wakes) != 1 || causeCodes(disk.Wakes[0]) != "unknown" {
			t.Fatalf("%s: %+v", name, disk.Wakes)
		}
	}
	if len(asked) != 1 || asked[0] != "tank" {
		t.Fatalf("probe asked for %v", asked)
	}
	// Asleep again; this time a client is connected.
	wakeTicks(t, m, now, 6*time.Minute)
	m.WakeProbe = func(context.Context, string) []diskwake.Cause {
		return []diskwake.Cause{{Code: diskwake.SMBSession, Detail: "Fotos"}}
	}
	wakeIO(t, m, "sde", 3)
	wakeTicks(t, m, now, 0)
	if disk := sleepDisk(t, m, "sde"); len(disk.Wakes) != 2 || causeCodes(disk.Wakes[0]) != "smb_session:Fotos" || disk.WakesToday != 2 {
		t.Fatalf("%+v", disk)
	}
	// TitanOS itself was it: nobody is asked.
	wakeTicks(t, m, now, 6*time.Minute)
	m.WakeProbe = func(context.Context, string) []diskwake.Cause {
		t.Fatal("probe although a cause was noted")
		return nil
	}
	recorder.Note("tank", diskwake.FileManager, "anna")
	wakeIO(t, m, "sde", 4)
	wakeTicks(t, m, now, 0)
	if disk := sleepDisk(t, m, "sde"); causeCodes(disk.Wakes[0]) != "file_manager:anna" {
		t.Fatalf("%+v", disk.Wakes)
	}
}

// A disk test, a VM or maintenance that takes a sleeping disk woke it.
func TestDiskWakeByMaintenance(t *testing.T) {
	m, _, _, now := wakeManager(t)
	m.Claimed = func() map[string]string { return map[string]string{"sdf": "win11"} }
	wakeTicks(t, m, now, 0)
	disk := sleepDisk(t, m, "sdf")
	if disk.State != "active" || len(disk.Wakes) != 1 || causeCodes(disk.Wakes[0]) != "vm_disk:win11" || !disk.Since.Equal(disk.Wakes[0].At) {
		t.Fatalf("%+v", disk)
	}
	// Still claimed: no second wake.
	wakeTicks(t, m, now, time.Minute)
	if disk := sleepDisk(t, m, "sdf"); len(disk.Wakes) != 1 {
		t.Fatalf("%+v", disk.Wakes)
	}
}

// The history is bounded: 20 wakes per disk, 14 days, and the file on the
// system data volume holds the same.
func TestDiskWakeHistoryBounded(t *testing.T) {
	m, _, recorder, now := wakeManager(t)
	for i := range 30 {
		recorder.Note("tank", diskwake.BackupJob, fmt.Sprint(i))
		wakeIO(t, m, "sde", i+2)
		wakeTicks(t, m, now, 0)
		// A day later it sleeps again.
		*now = now.Add(24 * time.Hour)
		wakeTicks(t, m, now, 6*time.Minute)
		if disk := sleepDisk(t, m, "sde"); disk.State != "asleep" {
			t.Fatalf("round %d: %+v", i, disk)
		}
	}
	m.mu.RLock()
	log := m.diskWakes["serial:E1"]
	m.mu.RUnlock()
	if len(log.Wakes) != wakeHistory || len(log.Days) != wakeDays || log.Wakes[0].Causes[0].Detail != "29" || log.Wakes[19].Causes[0].Detail != "10" {
		t.Fatalf("history: %d wakes, %d days, %+v", len(log.Wakes), len(log.Days), log.Wakes[0])
	}
	wakes, _, days := m.wakesOf("serial:E1", *now)
	if len(wakes) != wakeHistory || len(days) != 7 || days[0].Day < days[6].Day {
		t.Fatalf("status: %d wakes, days %+v", len(wakes), days)
	}
	data, err := os.ReadFile(filepath.Join(m.mountRoot, "fast", ".titanos", "disksleep", "wakes.json"))
	if err != nil {
		t.Fatal(err)
	}
	var stored wakeState
	if err := json.Unmarshal(data, &stored); err != nil || len(stored.Disks["serial:E1"].Wakes) != wakeHistory || len(stored.Disks) != 1 {
		t.Fatalf("file: %v, %s", err, data)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "tank", ".titanos")); !os.IsNotExist(err) {
		t.Fatalf("the sleeping volume was written to: %v", err)
	}
	// A new titand reads it back, without a write of its own.
	info, _ := os.Stat(filepath.Join(m.mountRoot, "fast", ".titanos", "disksleep", "wakes.json"))
	m.mu.Lock()
	m.diskWakes, m.diskWakesLoaded = nil, false
	m.mu.Unlock()
	wakeTicks(t, m, now, 0)
	if disk := sleepDisk(t, m, "sde"); len(disk.Wakes) != wakeHistory || disk.Wakes[0].Causes[0].Detail != "29" {
		t.Fatalf("after a restart: %+v", disk.Wakes)
	}
	if after, _ := os.Stat(filepath.Join(m.mountRoot, "fast", ".titanos", "disksleep", "wakes.json")); !after.ModTime().Equal(info.ModTime()) {
		t.Fatal("history written again without a wake")
	}
}

// With only volumes that may sleep the history stays in RAM: the file is
// never put on a sleeping volume, and never on the stick.
func TestDiskWakeHistoryNotOnSleepingVolume(t *testing.T) {
	m, _, _, now := wakeManager(t)
	m.WakeDataVolume = func() string { return "tank" }
	writes := m.store.Changes()
	wakeIO(t, m, "sde", 2)
	wakeTicks(t, m, now, time.Minute)
	if disk := sleepDisk(t, m, "sde"); len(disk.Wakes) != 1 {
		t.Fatalf("%+v", disk)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "tank", ".titanos")); !os.IsNotExist(err) {
		t.Fatalf("history on the sleeping volume: %v", err)
	}
	if m.store.Changes() != writes {
		t.Fatal("a wake wrote to /config")
	}
}

// With disk sleep off nothing is noted at all.
func TestDiskWakeOffNotesNothing(t *testing.T) {
	m, _, recorder, now := wakeManager(t)
	if !recorder.Enabled() {
		t.Fatal("recorder off while disks may sleep")
	}
	if err := m.SetDiskSleep(context.Background(), DiskSleepSettings{}); err != nil {
		t.Fatal(err)
	}
	wakeTicks(t, m, now, 0)
	recorder.Note("tank", diskwake.ActivityLog, "")
	if recorder.Enabled() || len(recorder.Recent("tank", time.Time{}, now.Add(time.Hour))) != 0 {
		t.Fatal("accesses noted although no disk may sleep")
	}
}
