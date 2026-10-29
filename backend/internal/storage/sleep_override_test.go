package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// overrideDisks: sde and sdg are members of volume "tank" (md array), sdf is
// a loose HDD, sdh an SSD that never sleeps.
func overrideDisks(names ...string) string {
	if len(names) == 0 {
		names = []string{"sde", "sdf", "sdg"}
	}
	serial := map[string]string{"sde": "E1", "sdf": "F1", "sdg": "G1"}
	member := map[string]bool{"sde": true, "sdg": true}
	result := `{"blockdevices":[`
	for i, name := range names {
		if i > 0 {
			result += ","
		}
		id := serial[name]
		if id == "" {
			id = "X1" // the renamed disk of TestDiskSleepKeepsSettingAfterRename
		}
		children := ""
		if member[name] {
			children = fmt.Sprintf(`,"children":[{"name":"/dev/%s1","kname":"/dev/%s1","type":"part","size":999000,"fstype":"linux_raid_member","uuid":"11111111-2222-3333-4444-555555555555"}]`, name, name)
		}
		result += fmt.Sprintf(`{"name":"/dev/%s","kname":"/dev/%s","type":"disk","size":1000000,"rota":true,"tran":"sata","model":"HDD","serial":"%s"%s}`, name, name, id, children)
	}
	return result + `,{"name":"/dev/sdh","kname":"/dev/sdh","type":"disk","size":1000000,"rota":false,"tran":"sata","model":"SSD","serial":"H1"}]}`
}

func overrideManager(t *testing.T) (*Manager, *sleepRunner, *config.Store) {
	t.Helper()
	m, _, store := newTestManager(t)
	runner := &sleepRunner{disks: overrideDisks()}
	m.runner = runner
	m.volumes = []VolumeConfig{{Name: "tank", Level: "raid1", Filesystem: "ext4", ArrayUUID: dataArrayUUID}}
	for _, name := range []string{"sde", "sdf", "sdg", "sdh"} {
		sleepFile(t, m, name+"/stat", "1 0 8 0 1 0 8 0 0 0 0")
	}
	return m, runner, store
}

func TestDiskSleepEffectiveValue(t *testing.T) {
	settings := DiskSleepSettings{IdleMinutes: 30, Volumes: map[string]int{"tank": 10, "cold": 0}, Disks: map[string]int{"serial:A": 0, "serial:B": 120}}
	for _, tc := range []struct {
		disk, volume string
		minutes      int
		origin       string
	}{
		{"", "", 30, SleepFromDefault},
		{"serial:C", "", 30, SleepFromDefault},               // disk outside volumes, no own value
		{"serial:C", "other", 30, SleepFromDefault},          // volume without override
		{"serial:C", "tank", 10, SleepFromVolume},            // member takes the volume value
		{"serial:C", "cold", 0, SleepFromVolume},             // volume never sleeps
		{"serial:A", "tank", 0, SleepFromDisk},               // disk beats volume, also with "off"
		{"serial:B", "cold", 120, SleepFromDisk},             // disk beats an "off" volume
		{"serial:B", "", 120, SleepFromDisk},                 // loose disk with own value
		{"", "tank", 10, SleepFromVolume},                    // no identity: only inherits
		{"wwn:0x5000c500a1b2c3d4", "", 30, SleepFromDefault}, // unknown disk inherits
	} {
		minutes, origin := settings.effective(tc.disk, tc.volume)
		if minutes != tc.minutes || origin != tc.origin {
			t.Errorf("%q in %q: %d %s, want %d %s", tc.disk, tc.volume, minutes, origin, tc.minutes, tc.origin)
		}
	}
	if (DiskSleepSettings{}).anyActive() || (DiskSleepSettings{Volumes: map[string]int{"tank": 0}, Disks: map[string]int{"serial:A": 0}}).anyActive() {
		t.Fatal("all off must stop the timer")
	}
	if !(DiskSleepSettings{Disks: map[string]int{"serial:A": 5}}).anyActive() {
		t.Fatal("a disk override must keep the timer running with the default off")
	}
}

func TestDiskSleepIdentity(t *testing.T) {
	for _, tc := range []struct {
		device blockDevice
		want   string
	}{
		{blockDevice{Serial: " WD-123 ", WWN: "0x5000"}, "serial:WD-123"},
		{blockDevice{WWN: "0x5000C500A1B2C3D4"}, "wwn:0x5000c500a1b2c3d4"},
		{blockDevice{Name: "/dev/sda"}, ""},
	} {
		if got := sleepDiskID(tc.device); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.device, got, tc.want)
		}
	}
	// Two enclosures reporting the same serial get no identity at all.
	devices := []blockDevice{
		{Name: "/dev/sda", Type: "disk", Size: 1, Serial: "0000"},
		{Name: "/dev/sdb", Type: "disk", Size: 1, Serial: "0000"},
		{Name: "/dev/sdc", Type: "disk", Size: 1, Serial: "C"},
	}
	if ids := sleepDiskIDs(devices); len(ids) != 1 || ids["/dev/sdc"] != "serial:C" {
		t.Fatalf("duplicate serials: %v", ids)
	}
}

func TestDiskSleepPerVolumeAndDisk(t *testing.T) {
	m, runner, _ := overrideManager(t)
	// Default off; tank sleeps after 5 minutes, but its member sdg never;
	// the loose disk sdf after 10 minutes.
	settings := DiskSleepSettings{Volumes: map[string]int{"tank": 5}, Disks: map[string]int{"serial:G1": 0, "serial:F1": 10}}
	if err := m.SetDiskSleep(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1000, 0)
	sleepTicks(t, m, now, 5*time.Minute)
	if got := runner.standbyCalls(); !slices.Equal(got, []string{"hd-idle -c ata -t /dev/sde"}) {
		t.Fatalf("after 5 minutes only the tank member sde: %v", got)
	}
	sleepTicks(t, m, now.Add(5*time.Minute+10*time.Second), 10*time.Minute)
	if got := runner.standbyCalls(); !slices.Equal(got, []string{"hd-idle -c ata -t /dev/sde", "hd-idle -c ata -t /dev/sdf"}) {
		t.Fatalf("sdf after its own 10 minutes, sdg and the SSD never: %v", got)
	}

	status, err := m.DiskSleepStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(status.VolumeStatus) != 1 || status.VolumeStatus[0].Effective != 5 || status.VolumeStatus[0].Origin != SleepFromVolume || *status.VolumeStatus[0].Override != 5 {
		t.Fatalf("volume: %+v", status.VolumeStatus)
	}
	want := map[string]struct {
		id, volume, origin, state string
		minutes                   int
	}{
		"sde": {"serial:E1", "tank", SleepFromVolume, "asleep", 5},
		"sdf": {"serial:F1", "", SleepFromDisk, "asleep", 10},
		"sdg": {"serial:G1", "tank", SleepFromDisk, "active", 0},
	}
	if len(status.DiskStatus) != len(want) {
		t.Fatalf("only sleep-capable disks, no SSD: %+v", status.DiskStatus)
	}
	for _, disk := range status.DiskStatus {
		w := want[disk.Name]
		if disk.ID != w.id || disk.Volume != w.volume || disk.Origin != w.origin || disk.State != w.state || disk.Effective != w.minutes {
			t.Errorf("%s: %+v, want %+v", disk.Name, disk, w)
		}
	}
	// New activity wakes the view back to active; the volume value changes
	// to inherit and the default applies.
	sleepFile(t, m, "sde/stat", "2 0 16 0 1 0 8 0 0 0 0")
	sleepTicks(t, m, now.Add(16*time.Minute), 0)
	if status, _ := m.DiskSleepStatus(context.Background()); status.DiskStatus[0].State != "active" {
		t.Fatalf("activity must show the disk active: %+v", status.DiskStatus[0])
	}
	settings.Volumes, settings.IdleMinutes = nil, 15
	if err := m.SetDiskSleep(context.Background(), settings); err != nil {
		t.Fatal(err)
	}
	if status, _ := m.DiskSleepStatus(context.Background()); status.DiskStatus[0].Origin != SleepFromDefault || status.DiskStatus[0].Effective != 15 || status.VolumeStatus[0].Override != nil {
		t.Fatalf("inherit: %+v %+v", status.DiskStatus[0], status.VolumeStatus[0])
	}
}

func TestDiskSleepKeepsSettingAfterRename(t *testing.T) {
	m, runner, store := overrideManager(t)
	runner.disks = overrideDisks("sdb")
	sleepFile(t, m, "sdb/stat", "1 0 8 0 1 0 8 0 0 0 0")
	if err := m.SetDiskSleep(context.Background(), DiskSleepSettings{Disks: map[string]int{"serial:X1": 5}}); err != nil {
		t.Fatal(err)
	}
	// After a restart the same disk is sdc; the setting follows its serial.
	loaded := NewManager(store)
	loaded.runner, loaded.sysBlock = runner, m.sysBlock
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	runner.disks = overrideDisks("sdc")
	sleepFile(t, m, "sdc/stat", "1 0 8 0 1 0 8 0 0 0 0")
	sleepTicks(t, loaded, time.Unix(1000, 0), 5*time.Minute)
	if got := runner.standbyCalls(); !slices.Equal(got, []string{"hd-idle -c ata -t /dev/sdc"}) {
		t.Fatalf("renamed disk must keep its own value: %v", got)
	}
	status, _ := loaded.DiskSleepStatus(context.Background())
	if len(status.DiskStatus) != 1 || status.DiskStatus[0].Name != "sdc" || status.DiskStatus[0].Origin != SleepFromDisk || len(status.MissingDisks) != 0 {
		t.Fatalf("status after rename: %+v", status)
	}
	data, _ := store.Read(diskSleepFile)
	if !json.Valid(data) || strings.Contains(string(data), "sdb") || strings.Contains(string(data), "sdc") {
		t.Fatalf("device names stored: %s", data)
	}
}

func TestDiskSleepOldFileRoundTrip(t *testing.T) {
	store := config.Open(t.TempDir())
	store.Write(diskSleepFile, []byte(`{"idleMinutes":30}`))
	m := NewManager(store)
	runner := &sleepRunner{disks: overrideDisks()}
	m.runner = runner
	if err := m.Load(); err != nil || m.DiskSleep().IdleMinutes != 30 || m.DiskSleep().Volumes != nil || m.DiskSleep().Disks != nil {
		t.Fatalf("old file: %v %+v", err, m.DiskSleep())
	}
	m.volumes = []VolumeConfig{{Name: "tank", ArrayUUID: dataArrayUUID}}
	if err := m.SetDiskSleep(context.Background(), DiskSleepSettings{IdleMinutes: 30, Volumes: map[string]int{"tank": 0}, Disks: map[string]int{"serial:F1": 60}}); err != nil {
		t.Fatal(err)
	}
	data, _ := store.Read(diskSleepFile)
	// What an older titand reads: the global value, the rest ignored.
	var old struct {
		IdleMinutes int `json:"idleMinutes"`
	}
	if err := json.Unmarshal(data, &old); err != nil || old.IdleMinutes != 30 {
		t.Fatalf("older titand: %v %d %s", err, old.IdleMinutes, data)
	}
	loaded := NewManager(store)
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	if got := loaded.DiskSleep(); got.Volumes["tank"] != 0 || len(got.Volumes) != 1 || got.Disks["serial:F1"] != 60 {
		t.Fatalf("round trip: %+v", got)
	}
	// An older titand writes only the global value back: overrides go,
	// the default stays.
	store.Write(diskSleepFile, []byte(`{"idleMinutes":10}`))
	if err := loaded.Load(); err != nil || loaded.DiskSleep().IdleMinutes != 10 || loaded.DiskSleep().Volumes != nil {
		t.Fatalf("file of an older titand: %v %+v", err, loaded.DiskSleep())
	}
	// Removing all overrides writes exactly the old format again.
	if err := loaded.SetDiskSleep(context.Background(), DiskSleepSettings{IdleMinutes: 10, Volumes: map[string]int{}, Disks: map[string]int{}}); err != nil {
		t.Fatal(err)
	}
	data, _ = store.Read(diskSleepFile)
	var saved map[string]any
	if json.Unmarshal(data, &saved) != nil || len(saved) != 1 || saved["idleMinutes"] != float64(10) {
		t.Fatalf("empty overrides must not be stored: %s", data)
	}
	for _, bad := range []string{`{"idleMinutes":0,"volumes":{"tank":7}}`, `{"idleMinutes":0,"disks":{"sda":5}}`, `{"idleMinutes":0,"disks":{"serial:A":"5"}}`} {
		store.Write(diskSleepFile, []byte(bad))
		if err := NewManager(store).Load(); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func TestDiskSleepOverrideValidation(t *testing.T) {
	m, runner, store := overrideManager(t)
	ctx := context.Background()
	if err := m.SetDiskSleep(ctx, DiskSleepSettings{Volumes: map[string]int{"tank": 30}, Disks: map[string]int{"serial:F1": 10}}); err != nil {
		t.Fatal(err)
	}
	writes := store.Changes()
	for name, settings := range map[string]DiskSleepSettings{
		"unknown volume":      {Volumes: map[string]int{"Tank": 5}},
		"unknown disk":        {Disks: map[string]int{"serial:Z9": 5}},
		"device name":         {Disks: map[string]int{"sde": 5}},
		"device path":         {Disks: map[string]int{"/dev/sde": 5}},
		"empty serial":        {Disks: map[string]int{"serial:": 5}},
		"SSD":                 {Disks: map[string]int{"serial:H1": 5}},
		"bad volume value":    {Volumes: map[string]int{"tank": 7}},
		"bad disk value":      {Disks: map[string]int{"serial:E1": -1}},
		"bad default":         {IdleMinutes: 45},
		"changed absent disk": {Disks: map[string]int{"serial:F1": 10, "serial:Z9": 0}},
	} {
		err := m.SetDiskSleep(ctx, settings)
		if !errors.Is(err, ErrUnknownSleepTarget) && !errors.Is(err, ErrInvalidIdleTime) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	if store.Changes() != writes || m.DiskSleep().Volumes["tank"] != 30 {
		t.Fatal("refused settings changed something")
	}
	// sdf is unplugged: its stored value stays while only the default
	// changes, is reported missing and goes only when the user saves without it.
	runner.disks = overrideDisks("sde", "sdg")
	status, _ := m.DiskSleepStatus(ctx)
	if !slices.Equal(status.MissingDisks, []string{"serial:F1"}) {
		t.Fatalf("missing disk: %+v", status.MissingDisks)
	}
	if err := m.SetDiskSleep(ctx, DiskSleepSettings{IdleMinutes: 60, Volumes: map[string]int{"tank": 30}, Disks: map[string]int{"serial:F1": 10}}); err != nil {
		t.Fatal("unchanged entry of an absent disk refused:", err)
	}
	if err := m.SetDiskSleep(ctx, DiskSleepSettings{IdleMinutes: 60, Volumes: map[string]int{"tank": 30}, Disks: map[string]int{"serial:F1": 30}}); !errors.Is(err, ErrUnknownSleepTarget) {
		t.Fatal("changed entry of an absent disk accepted:", err)
	}
	m.volumes = nil // the volume was deleted
	if status, _ := m.DiskSleepStatus(ctx); !slices.Equal(status.MissingVolumes, []string{"tank"}) {
		t.Fatalf("missing volume: %+v", status.MissingVolumes)
	}
	if got := m.DiskSleep(); got.Volumes["tank"] != 30 || got.Disks["serial:F1"] != 10 {
		t.Fatal("overrides were removed without a save", got)
	}
	if err := m.SetDiskSleep(ctx, DiskSleepSettings{IdleMinutes: 60}); err != nil {
		t.Fatal(err)
	}
	if got := m.DiskSleep(); got.Volumes != nil || got.Disks != nil {
		t.Fatal("saving without them must remove them", got)
	}
	// Unchanged settings never write; a saved setting runs no disk command.
	writes = store.Changes()
	if err := m.SetDiskSleep(ctx, DiskSleepSettings{IdleMinutes: 60}); err != nil || store.Changes() != writes {
		t.Fatal("unchanged settings wrote to the stick", err)
	}
	if len(runner.standbyCalls()) != 0 {
		t.Fatal("saving sent standby commands")
	}
}
