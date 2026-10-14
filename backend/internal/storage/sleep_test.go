package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type sleepRunner struct {
	fakeRunner
	disks string
	calls []string
}

func (r *sleepRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	if name == "lsblk" && r.disks != "" {
		return r.disks, nil
	}
	return r.fakeRunner.Run(ctx, stdin, name, args...)
}

func (r *sleepRunner) standbyCalls() []string {
	var result []string
	for _, command := range r.calls {
		if strings.HasPrefix(command, "hd-idle ") {
			result = append(result, command)
		}
	}
	return result
}

func sleepFile(t *testing.T, m *Manager, name, data string) {
	t.Helper()
	path := filepath.Join(m.sysBlock, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sleepStat(t *testing.T, m *Manager, reads, writes, inFlight, flushes uint64) {
	t.Helper()
	sleepFile(t, m, "sde/stat", fmt.Sprintf("%d 0 %d 0 %d 0 %d 0 %d 0 0 0 0 0 0 %d 0", reads, reads*8, writes, writes*8, inFlight, flushes))
}

func sleepManager(t *testing.T) (*Manager, *sleepRunner, time.Time) {
	t.Helper()
	m, _, _ := newTestManager(t)
	runner := &sleepRunner{}
	m.runner = runner
	sleepStat(t, m, 1, 1, 0, 0)
	sleepFile(t, m, "sde/diskseq", "123")
	if err := m.SetDiskSleep(DiskSleepSettings{IdleMinutes: 5}); err != nil {
		t.Fatal(err)
	}
	return m, runner, time.Unix(1000, 0)
}

func sleepTicks(t *testing.T, m *Manager, from time.Time, duration time.Duration) {
	t.Helper()
	for elapsed := time.Duration(0); elapsed <= duration; elapsed += 10 * time.Second {
		if err := m.sleepIdleDisks(context.Background(), from.Add(elapsed)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDiskSleepSettings(t *testing.T) {
	m, runner, store := newTestManager(t)
	if got := m.DiskSleep(); got.IdleMinutes != 0 || !got.Available || len(got.Errors) != 0 {
		t.Fatalf("default: %+v", got)
	}
	for _, minutes := range []int{5, 10, 15, 30, 60, 120, 180, 0} {
		if err := m.SetDiskSleep(DiskSleepSettings{IdleMinutes: minutes}); err != nil {
			t.Fatal(err)
		}
		loaded := NewManager(store)
		if err := loaded.Load(); err != nil || loaded.DiskSleep().IdleMinutes != minutes {
			t.Fatalf("persist %d: %v, %+v", minutes, err, loaded.DiskSleep())
		}
	}
	writes := store.Changes()
	for _, minutes := range []int{-1, 1, 4, 6, 20, 45, 90, 240, 1000000} {
		if err := m.SetDiskSleep(DiskSleepSettings{IdleMinutes: minutes}); !errors.Is(err, ErrInvalidIdleTime) {
			t.Errorf("accepted %d: %v", minutes, err)
		}
	}
	if err := m.SetDiskSleep(DiskSleepSettings{}); err != nil || store.Changes() != writes {
		t.Fatal("invalid or unchanged settings wrote to the stick", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("saving the setting touched disks: %v", runner.commands)
	}
	data, _ := store.Read(diskSleepFile)
	var saved map[string]any
	json.Unmarshal(data, &saved)
	if len(saved) != 1 || saved["idleMinutes"] != float64(0) {
		t.Fatalf("persist only global duration, no device names: %s", data)
	}
}

func TestDiskSleepWriteFailureAndReadOnly(t *testing.T) {
	m, runner, now := sleepManager(t)
	m.ReadOnly = true
	if err := m.SetDiskSleep(DiskSleepSettings{IdleMinutes: 10}); !errors.Is(err, ErrReadOnly) {
		t.Fatal(err)
	}
	sleepTicks(t, m, now, 6*time.Minute)
	if len(runner.calls) != 0 {
		t.Fatal("development mode ran commands", runner.calls)
	}
	m.ReadOnly = false
	bad := filepath.Join(t.TempDir(), "not-a-directory")
	os.WriteFile(bad, []byte("x"), 0o600)
	m.store = config.Open(bad)
	if err := m.SetDiskSleep(DiskSleepSettings{IdleMinutes: 30}); err == nil || m.DiskSleep().IdleMinutes != 5 {
		t.Fatal("write failure must preserve active setting")
	}
}

func TestDiskSleepMalformedConfigurationDoesNotBlockVolumes(t *testing.T) {
	for _, data := range []string{`{"idleMinutes":2}`, `{broken`, `{"idleMinutes":"5"}`} {
		t.Run(data, func(t *testing.T) {
			store := config.Open(t.TempDir())
			store.Write(diskSleepFile, []byte(data))
			m := NewManager(store)
			runner := &sleepRunner{}
			m.runner = runner
			if err := m.Load(); err == nil || m.DiskSleep().Available || !m.loaded {
				t.Fatalf("bad sleep settings: %v, %+v, volumes loaded=%v", err, m.DiskSleep(), m.loaded)
			}
			sleepTicks(t, m, time.Now(), 10*time.Minute)
			if len(runner.calls) != 0 {
				t.Fatal("bad configuration ran commands")
			}
		})
	}
}

func TestDiskSleepAllTimeouts(t *testing.T) {
	for _, minutes := range []int{5, 10, 15, 30, 60, 120, 180} {
		t.Run(fmt.Sprint(minutes), func(t *testing.T) {
			m, runner, now := sleepManager(t)
			m.SetDiskSleep(DiskSleepSettings{IdleMinutes: minutes})
			// No periodic reads of /config: the timer still works without it.
			os.RemoveAll(m.store.Dir())
			limit := time.Duration(minutes) * time.Minute
			sleepTicks(t, m, now, limit-10*time.Second)
			if len(runner.standbyCalls()) != 0 {
				t.Fatal("slept too early")
			}
			sleepTicks(t, m, now.Add(limit), time.Minute)
			if got := runner.standbyCalls(); !slices.Equal(got, []string{"hd-idle -c ata -t /dev/sde"}) {
				t.Fatalf("standby once at timeout: %v", got)
			}
		})
	}
}

func TestDiskSleepActivityAndOff(t *testing.T) {
	for _, activity := range []string{"read", "write", "in-flight", "flush", "discard", "counter reset", "replacement"} {
		t.Run(activity, func(t *testing.T) {
			m, runner, now := sleepManager(t)
			sleepTicks(t, m, now, 4*time.Minute+50*time.Second)
			switch activity {
			case "read":
				sleepStat(t, m, 2, 1, 0, 0)
			case "write":
				sleepStat(t, m, 1, 2, 0, 0)
			case "in-flight":
				sleepStat(t, m, 1, 1, 1, 0)
			case "flush":
				sleepStat(t, m, 1, 1, 0, 1)
			case "discard":
				sleepFile(t, m, "sde/stat", "1 0 8 0 1 0 8 0 0 0 0 1 0 8 0 0 0")
			case "counter reset":
				sleepStat(t, m, 0, 0, 0, 0)
			case "replacement":
				sleepFile(t, m, "sde/diskseq", "124")
			}
			sleepTicks(t, m, now.Add(5*time.Minute), 4*time.Minute+50*time.Second)
			if len(runner.standbyCalls()) != 0 {
				t.Fatal("activity did not reset timer", runner.standbyCalls())
			}
			if err := m.SetDiskSleep(DiskSleepSettings{}); err != nil {
				t.Fatal(err)
			}
			sleepTicks(t, m, now.Add(10*time.Minute), 10*time.Minute)
			if len(runner.standbyCalls()) != 0 {
				t.Fatal("Off sent a standby command")
			}
		})
	}
}

func TestDiskSleepTracksEachHDDIndependently(t *testing.T) {
	m, runner, now := sleepManager(t)
	runner.disks = `{"blockdevices":[
		{"name":"/dev/sde","kname":"/dev/sde","type":"disk","size":1000000,"rota":true,"tran":"sata"},
		{"name":"/dev/sdf","kname":"/dev/sdf","type":"disk","size":1000000,"rota":true,"tran":"sata"}
	]}`
	sleepFile(t, m, "sdf/stat", "1 0 8 0 1 0 8 0 0 0 0")
	sleepFile(t, m, "sdf/diskseq", "125")
	sleepTicks(t, m, now, 3*time.Minute-10*time.Second)
	sleepFile(t, m, "sdf/stat", "2 0 16 0 1 0 8 0 0 0 0")
	sleepTicks(t, m, now.Add(3*time.Minute), 2*time.Minute)
	if got := runner.standbyCalls(); !slices.Equal(got, []string{"hd-idle -c ata -t /dev/sde"}) {
		t.Fatalf("activity on sdf must not delay sde: %v", got)
	}
	sleepTicks(t, m, now.Add(5*time.Minute+10*time.Second), 3*time.Minute-10*time.Second)
	if got := runner.standbyCalls(); !slices.Equal(got, []string{"hd-idle -c ata -t /dev/sde", "hd-idle -c ata -t /dev/sdf"}) {
		t.Fatalf("sdf must get a full idle period of its own: %v", got)
	}
}

func TestDiskSleepResetsAfterWakeAndObservationGap(t *testing.T) {
	m, runner, now := sleepManager(t)
	sleepTicks(t, m, now, 5*time.Minute)
	sleepStat(t, m, 2, 1, 0, 0)
	sleepTicks(t, m, now.Add(5*time.Minute+10*time.Second), 5*time.Minute)
	if len(runner.standbyCalls()) != 2 {
		t.Fatal("did not sleep again after a full idle period following I/O")
	}
	m.SetDiskSleep(DiskSleepSettings{IdleMinutes: 5})
	sleepTicks(t, m, now.Add(20*time.Minute), 0)
	sleepTicks(t, m, now.Add(30*time.Minute), 0)
	if len(runner.standbyCalls()) != 2 {
		t.Fatal("observation gap counted as idle")
	}
	sleepTicks(t, m, now.Add(30*time.Minute+10*time.Second), 4*time.Minute+50*time.Second)
	if len(runner.standbyCalls()) != 3 {
		t.Fatal("timer did not resume after observation gap")
	}
}

func TestDiskSleepExcludesBusyDisks(t *testing.T) {
	for _, busy := range []string{"test", "VM", "recover", "resync", "check", "repair", "frozen"} {
		t.Run(busy, func(t *testing.T) {
			m, runner, now := sleepManager(t)
			switch busy {
			case "test":
				m.tests = map[string]*DiskTest{"sde": {Disk: "sde"}}
			case "VM":
				m.Claimed = func() map[string]string { return map[string]string{"sde": "guest"} }
			default:
				sleepFile(t, m, "md0/md/sync_action", busy)
				sleepFile(t, m, "md0/md/dev-sde/state", "in_sync")
			}
			sleepTicks(t, m, now, 6*time.Minute)
			if len(runner.standbyCalls()) != 0 {
				t.Fatal("busy disk slept", busy)
			}
			m.tests, m.Claimed = nil, nil
			os.RemoveAll(filepath.Join(m.sysBlock, "md0"))
			sleepTicks(t, m, now.Add(6*time.Minute+10*time.Second), 4*time.Minute+50*time.Second)
			if len(runner.standbyCalls()) != 0 {
				t.Fatal("pause counted towards idle time")
			}
		})
	}
}

func TestDiskSleepFailureAndUnreadableCounters(t *testing.T) {
	m, runner, now := sleepManager(t)
	runner.fail = "hd-idle"
	sleepTicks(t, m, now, 10*time.Minute)
	if len(runner.standbyCalls()) != 1 || len(m.DiskSleep().Errors) != 1 {
		t.Fatalf("failure must be visible without retrying every poll: %v %+v", runner.standbyCalls(), m.DiskSleep())
	}
	for _, invalid := range []string{"", "0 0 0", "1 0 8 0 1 0 8 0 x 0 0", "1 0 8 0 1 0 8 0 0 0 0 0"} {
		m.SetDiskSleep(DiskSleepSettings{IdleMinutes: 5})
		sleepFile(t, m, "sde/stat", invalid)
		sleepTicks(t, m, now, 10*time.Minute)
	}
	if len(runner.standbyCalls()) != 1 {
		t.Fatal("invalid counters treated as idle")
	}
}

func TestDiskSleepDeviceSelection(t *testing.T) {
	base := blockDevice{Name: "/dev/sda", KName: "/dev/sda", Type: "disk", Size: 1000000, Rota: true, Transport: "sata"}
	for _, tc := range []struct {
		name   string
		change func(*blockDevice)
		want   string
	}{
		{"SATA", func(d *blockDevice) {}, "-c ata -t /dev/sda"},
		{"SAS", func(d *blockDevice) { d.Transport = "sas" }, "-c scsi -p 3 -t /dev/sda"},
		{"USB HDD", func(d *blockDevice) { d.Transport = "usb" }, "-c scsi -p 0 -t /dev/sda"},
		{"SSD", func(d *blockDevice) { d.Rota = false }, ""},
		{"NVMe", func(d *blockDevice) { d.Name = "/dev/nvme0n1"; d.Transport = "nvme" }, ""},
		{"boot disk", func(d *blockDevice) { d.Children = []blockDevice{{FSType: "vfat", Label: stickLabel}} }, ""},
		{"partition", func(d *blockDevice) { d.Type = "part" }, ""},
		{"RAID", func(d *blockDevice) { d.Type = "raid1" }, ""},
		{"removable", func(d *blockDevice) { d.Removable = true }, ""},
		{"network", func(d *blockDevice) { d.Transport = "iscsi" }, ""},
		{"unsafe name", func(d *blockDevice) { d.Name = "/dev/../sda" }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			disk := base
			tc.change(&disk)
			if got := strings.Join(standbyArgs(disk), " "); got != tc.want {
				t.Fatalf("%q, want %q", got, tc.want)
			}
		})
	}
}

func TestDiskSleepConcurrentSettingsAndPolling(t *testing.T) {
	m, _, now := sleepManager(t)
	var workers sync.WaitGroup
	for worker := range 3 {
		workers.Go(func() {
			for n := range 50 {
				switch worker {
				case 0:
					if err := m.SetDiskSleep(DiskSleepSettings{IdleMinutes: []int{0, 5, 180}[n%3]}); err != nil {
						t.Error(err)
					}
				case 1:
					if err := m.sleepIdleDisks(context.Background(), now.Add(time.Duration(n)*10*time.Second)); err != nil {
						t.Error(err)
					}
				case 2:
					status := m.DiskSleep()
					if !status.Available || !validIdleMinutes(status.IdleMinutes) {
						t.Errorf("inconsistent status: %+v", status)
					}
				}
			}
		})
	}
	workers.Wait()
}

func TestSMARTPollingPreservesStandby(t *testing.T) {
	m, runner, _ := newTestManager(t)
	m.RefreshSmart(context.Background())
	queries := 0
	for _, command := range runner.commands {
		if strings.HasPrefix(command, "smartctl ") {
			queries++
			if !strings.Contains(command, " --nocheck=standby ") {
				t.Fatalf("SMART query may wake a disk: %s", command)
			}
		}
	}
	if queries == 0 {
		t.Fatal("no SMART commands inspected")
	}
}
