package vm

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func readCopy(t *testing.T, path string) VolumeCopy {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c VolumeCopy
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

// freshStick is the same NAS with a new, empty stick: the volumes are there,
// /config knows no VM.
func freshStick(t *testing.T, m *Manager) *Manager {
	t.Helper()
	fresh := NewManager(config.Open(filepath.Join(t.TempDir(), "config")), m.gid)
	fresh.mountRoot, fresh.runDir, fresh.pidDir, fresh.kvm = m.mountRoot, m.runDir, m.pidDir, m.kvm
	fresh.runGID, fresh.runUID, fresh.runner, fresh.Mounted = m.runGID, m.runUID, m.runner, m.Mounted
	fresh.autostartFile = filepath.Join(t.TempDir(), "vms-autostarted")
	fresh.powerFile = filepath.Join(t.TempDir(), "vm-schedule.json")
	if err := fresh.Load(); err != nil {
		t.Fatal(err)
	}
	return fresh
}

func TestVolumeCopyOnCreateAndChange(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data", "fast"}
	spec := VM{Name: "office", Template: "linux", MemoryMiB: 1024, VCPUs: 1,
		Disks: []Disk{{Format: "qcow2", SizeBytes: 4 * gib, Volume: "fast"}, {Format: "raw", SizeBytes: 2 * gib, Volume: "data"}}}
	if err := m.Create(ctx, spec, mounted); err != nil {
		t.Fatal(err)
	}
	// The copy lies in the folder of the VM only, not on the second volume.
	path := filepath.Join(m.mountRoot, "fast", "vms", "office", "vm.json")
	c := readCopy(t, path)
	if c.Format != 1 || c.Removed || c.VM.Name != "office" || c.VM.UUID == "" || len(c.DiskPaths) != 2 ||
		c.DiskPaths[1] != filepath.Join(m.mountRoot, "data", "vms", "office", "vdisk2.img") {
		t.Fatalf("copy: %+v", c)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data", "vms", "office", "vm.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("copy on the second volume")
	}
	if info, _ := os.Stat(path); info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", info.Mode())
	}
	// It matches /config.
	var stored VM
	if err := m.store.ReadJSON("vms/office.json", &stored); err != nil || stored.UUID != c.VM.UUID || stored.MAC != c.VM.MAC {
		t.Fatalf("%v %+v", err, stored)
	}
	name := "Büro"
	if err := m.Update(ctx, "office", Change{DisplayName: &name, MemoryMiB: 2048, VCPUs: 1, Network: "user"}, mounted); err != nil {
		t.Fatal(err)
	}
	if c := readCopy(t, path); c.VM.MemoryMiB != 2048 || c.VM.DisplayName != "Büro" {
		t.Fatalf("copy after the change: %+v", c.VM)
	}
	// Its volume gone at the moment of saving: /config changes, the copy
	// follows once the volume is there again.
	m.Mounted = func() []string { return []string{"data"} }
	if err := m.Update(ctx, "office", Change{DisplayName: &name, MemoryMiB: 4096, VCPUs: 1, Network: "user"}, mounted); err != nil {
		t.Fatal(err)
	}
	if c := readCopy(t, path); c.VM.MemoryMiB != 2048 {
		t.Fatalf("written without the volume: %+v", c.VM)
	}
	m.refreshVolumeCopies([]string{"data", "fast"})
	if c := readCopy(t, path); c.VM.MemoryMiB != 4096 {
		t.Fatalf("not caught up: %+v", c.VM)
	}
	// No leftovers of the atomic write.
	entries, _ := os.ReadDir(filepath.Dir(path))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".pending-") {
			t.Fatal(entry.Name())
		}
	}
}

func TestVolumeCopyRefreshedOnceAtStartAndNeverWithoutVolume(t *testing.T) {
	m, _ := newTestManager(t)
	vm := VM{Name: "old", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Volume: "data", UUID: "u-1", MAC: "52:54:00:00:00:01",
		Disks: []Disk{{File: "vdisk1.qcow2", Format: "qcow2", SizeBytes: gib, Volume: "data"}}}
	// Written by an older titand: /config only.
	if err := m.store.WriteJSON("vms/old.json", vm); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(m.mountRoot, "data", "vms", "old")
	path := filepath.Join(folder, "vm.json")
	// The volume is not mounted: nothing is written, not even a folder.
	m.refreshVolumeCopies(nil)
	m.refreshVolumeCopies([]string{"fast"})
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data", "vms")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("written without the volume")
	}
	// Mounted, but the folder is gone: still nothing made.
	m.refreshVolumeCopies([]string{"data"})
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data", "vms")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("folder made for the copy")
	}

	// After the next start of titand with the folder there, the copy is made once.
	m, _ = restartTitand(t, m)
	os.MkdirAll(folder, 0o770)
	m.refreshVolumeCopies([]string{"data"})
	if c := readCopy(t, path); c.VM.UUID != "u-1" {
		t.Fatalf("%+v", c)
	}
	// Once: a copy deleted afterwards is not made again by the polling.
	os.Remove(path)
	m.refreshVolumeCopies([]string{"data"})
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("refreshed twice")
	}
	// An outdated copy is replaced after a start, an equal one left alone.
	os.WriteFile(path, []byte(`{"format":1,"vm":{"name":"old","memoryMiB":1}}`), 0o600)
	m, _ = restartTitand(t, m)
	m.refreshVolumeCopies([]string{"data"})
	c := readCopy(t, path)
	if c.VM.MemoryMiB != 1024 {
		t.Fatalf("not refreshed: %+v", c.VM)
	}
	before, _ := os.Stat(path)
	m, _ = restartTitand(t, m)
	m.refreshVolumeCopies([]string{"data"})
	if after, _ := os.Stat(path); !os.SameFile(before, after) {
		t.Fatal("an equal copy was written again")
	}

	// A link instead of the folder is not followed.
	os.RemoveAll(folder)
	os.MkdirAll(filepath.Join(m.mountRoot, "data", "elsewhere"), 0o770)
	os.Symlink(filepath.Join(m.mountRoot, "data", "elsewhere"), folder)
	m, _ = restartTitand(t, m)
	m.refreshVolumeCopies([]string{"data"})
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data", "elsewhere", "vm.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("copy written through a link")
	}
}

// restartTitand is a new titand on the same stick and volumes.
func restartTitand(t *testing.T, m *Manager) (*Manager, error) {
	t.Helper()
	next := freshStick(t, m)
	next.store = m.store
	return next, next.Load()
}

func TestVolumeCopyOnRemove(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data", "fast"}
	spec := VM{Name: "keep", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Disks: []Disk{{Format: "qcow2", SizeBytes: 4 * gib, Volume: "data"}}}
	if err := m.Create(ctx, spec, mounted); err != nil {
		t.Fatal(err)
	}
	gone := spec
	gone.Name = "gone"
	if err := m.Create(ctx, gone, mounted); err != nil {
		t.Fatal(err)
	}
	// Disks kept: the copy stays, marked as removed.
	if err := m.Delete("keep", false, ""); err != nil {
		t.Fatal(err)
	}
	c := readCopy(t, filepath.Join(m.mountRoot, "data", "vms", "keep", "vm.json"))
	if !c.Removed || c.RemovedAt == nil || c.VM.Name != "keep" {
		t.Fatalf("%+v", c)
	}
	// Disks deleted: the copy goes with them, and so does the folder.
	if err := m.Delete("gone", true, "gone"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data", "vms", "gone")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("folder with the copy stayed")
	}
	// The removed VM can be taken over again; then it is no longer marked.
	candidates := m.AdoptCandidates(mounted)
	if len(candidates) != 1 || candidates[0].Name != "keep" || !candidates[0].Removed {
		t.Fatalf("%+v", candidates)
	}
	if _, err := m.Adopt(ctx, AdoptRequest{Volume: "data", Directory: "keep"}, mounted); err != nil {
		t.Fatal(err)
	}
	if c := readCopy(t, filepath.Join(m.mountRoot, "data", "vms", "keep", "vm.json")); c.Removed || c.RemovedAt != nil {
		t.Fatalf("%+v", c)
	}
	if len(m.AdoptCandidates(mounted)) != 0 {
		t.Fatal("a registered VM is offered")
	}
}

func TestAdoptAfterLosingTheStick(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data", "fast"}
	iso := filepath.Join(m.mountRoot, "data", "media", "install.iso")
	os.WriteFile(iso, []byte("iso"), 0o644)
	spec := VM{Name: "office", DisplayName: "Büro", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Autostart: true, ISO: iso,
		PCI: []string{"0000:01:00.0"}, USB: []string{"046d:c52b"},
		PowerSchedule: PowerSchedule{Enabled: true, StopTimeout: 300, Rules: []PowerRule{{Action: "start", Days: []int{1}, Time: "07:00"}}},
		Disks:         []Disk{{Format: "qcow2", SizeBytes: 4 * gib, Volume: "fast"}, {Format: "raw", SizeBytes: 2 * gib, Volume: "data"}}}
	if err := m.Create(ctx, spec, mounted); err != nil {
		t.Fatal(err)
	}
	original, _ := m.get("office")

	fresh := freshStick(t, m)
	candidates := fresh.AdoptCandidates(mounted)
	if len(candidates) != 1 {
		t.Fatalf("%+v", candidates)
	}
	got := candidates[0]
	if got.Volume != "fast" || got.Directory != "office" || got.Name != "office" || got.NameTaken || len(got.Disks) != 2 || len(got.Missing) != 0 {
		t.Fatalf("%+v", got)
	}
	for _, code := range []string{"pci", "usb", "autostart", "powerSchedule"} {
		if !slices.Contains(got.Dropped, code) {
			t.Errorf("not dropped: %s (%v)", code, got.Dropped)
		}
	}
	// Without the second volume the VM cannot be taken over.
	if _, err := fresh.Adopt(ctx, AdoptRequest{Volume: "fast", Directory: "office"}, []string{"fast"}); !errors.Is(err, ErrUnknownVolume) {
		t.Fatalf("without the second volume: %v", err)
	}
	if missing := fresh.AdoptCandidates([]string{"fast"}); len(missing) != 1 || !slices.Equal(missing[0].Missing, []string{"data"}) {
		t.Fatalf("%+v", missing)
	}
	result, err := fresh.Adopt(ctx, AdoptRequest{Volume: "fast", Directory: "office"}, mounted)
	if err != nil {
		t.Fatal(err)
	}
	adopted, err := fresh.get("office")
	if err != nil {
		t.Fatal(err)
	}
	if result.Name != "office" || adopted.UUID != original.UUID || adopted.MAC != original.MAC || adopted.Autostart || len(adopted.PCI) != 0 ||
		len(adopted.USB) != 0 || adopted.PowerSchedule.Enabled || len(adopted.PowerSchedule.Rules) != 1 || adopted.DisplayName != "Büro" || adopted.ISO != iso {
		t.Fatalf("%+v %+v", result, adopted)
	}
	if slices.Contains(result.Changes, "mac") {
		t.Fatal("MAC changed without a collision")
	}
	// Written to the new stick, like a new VM.
	var stored VM
	if err := fresh.store.ReadJSON("vms/office.json", &stored); err != nil || stored.UUID != original.UUID || stored.Autostart {
		t.Fatalf("%v %+v", err, stored)
	}
	// The same copy cannot be taken over twice.
	if _, err := fresh.Adopt(ctx, AdoptRequest{Volume: "fast", Directory: "office"}, mounted); !errors.Is(err, ErrExists) {
		t.Fatal(err)
	}
}

// The UUID names the snapshot folder of a VM. A vm.json whose UUID is a
// path must not lead the snapshots out of it.
func TestAdoptReplacesAUUIDThatIsNoUUID(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data", "fast"}
	spec := VM{Name: "office", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Disks: []Disk{{Format: "qcow2", SizeBytes: 4 * gib, Volume: "data"}}}
	if err := m.Create(ctx, spec, mounted); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(m.mountRoot, "data", "vms", "office", "vm.json")
	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var c VolumeCopy
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	c.VM.UUID = "../../share/x"
	data, _ = json.Marshal(c)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	fresh := freshStick(t, m)
	if _, err := fresh.Adopt(ctx, AdoptRequest{Volume: "data", Directory: "office"}, mounted); err != nil {
		t.Fatal(err)
	}
	adopted, _ := fresh.get("office")
	if !snapshotID.MatchString(adopted.UUID) {
		t.Fatalf("uuid %q", adopted.UUID)
	}
}

func TestAdoptCollisions(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data", "fast"}
	spec := VM{Name: "office", DisplayName: "Büro", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Disks: []Disk{{Format: "qcow2", SizeBytes: 4 * gib, Volume: "data"}}}
	if err := m.Create(ctx, spec, mounted); err != nil {
		t.Fatal(err)
	}
	original, _ := m.get("office")
	fresh := freshStick(t, m)
	// The new NAS has an office of its own already, with the same address and label.
	other := VM{Name: "office", DisplayName: "Büro", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Disks: []Disk{{Format: "qcow2", SizeBytes: 4 * gib, Volume: "fast"}}}
	if err := fresh.Create(ctx, other, mounted); err != nil {
		t.Fatal(err)
	}
	fresh.mu.Lock()
	o := fresh.vms["office"]
	o.MAC = original.MAC
	fresh.vms["office"] = o
	fresh.mu.Unlock()

	candidates := fresh.AdoptCandidates(mounted)
	if len(candidates) != 1 || !candidates[0].NameTaken || candidates[0].Volume != "data" {
		t.Fatalf("%+v", candidates)
	}
	if _, err := fresh.Adopt(ctx, AdoptRequest{Volume: "data", Directory: "office"}, mounted); !errors.Is(err, ErrExists) {
		t.Fatalf("name collision: %v", err)
	}
	if _, err := fresh.Adopt(ctx, AdoptRequest{Volume: "data", Directory: "office", Name: "Not Valid"}, mounted); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("bad name: %v", err)
	}
	result, err := fresh.Adopt(ctx, AdoptRequest{Volume: "data", Directory: "office", Name: "office2"}, mounted)
	if err != nil {
		t.Fatal(err)
	}
	adopted, _ := fresh.get("office2")
	if adopted.Directory != "office" || adopted.MAC == original.MAC || adopted.DisplayName != "" || adopted.UUID != original.UUID ||
		!slices.Contains(result.Changes, "mac") || !slices.Contains(result.Changes, "displayName") {
		t.Fatalf("%+v %+v", result, adopted)
	}
	// The folder keeps its name; the disk is found there.
	if p := fresh.diskPath(adopted, adopted.Disks[0]); p != filepath.Join(m.mountRoot, "data", "vms", "office", "vdisk1.qcow2") {
		t.Fatal(p)
	}
	if c := readCopy(t, filepath.Join(m.mountRoot, "data", "vms", "office", "vm.json")); c.VM.Name != "office2" {
		t.Fatalf("%+v", c.VM)
	}
}

func TestAdoptRefusesBadPaths(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data", "fast"}
	image := filepath.Join(m.mountRoot, "data", "media", "used.qcow2")
	os.WriteFile(image, []byte("image"), 0o644)
	spec := VM{Name: "office", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Disks: []Disk{{Format: "qcow2", SizeBytes: 4 * gib, Volume: "data"}}}
	if err := m.Create(ctx, spec, mounted); err != nil {
		t.Fatal(err)
	}
	user := VM{Name: "user", Template: "linux", MemoryMiB: 1024, VCPUs: 1, Disks: []Disk{{Path: image}}}
	if err := m.Create(ctx, user, mounted); err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(m.mountRoot, "data", "vms", "office", "vm.json")
	good, _ := os.ReadFile(copyPath)
	fresh := freshStick(t, m)
	// The direct image belongs to a VM the new stick knows.
	stored, _ := m.get("user")
	if err := fresh.store.WriteJSON("vms/user.json", stored); err != nil {
		t.Fatal(err)
	}
	if err := fresh.Load(); err != nil {
		t.Fatal(err)
	}
	tamper := func(change func(*VolumeCopy)) {
		t.Helper()
		var c VolumeCopy
		json.Unmarshal(good, &c)
		change(&c)
		data, _ := json.Marshal(c)
		if err := os.WriteFile(copyPath, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for name, tc := range map[string]struct {
		change func(*VolumeCopy)
		want   error
	}{
		"file outside the folder": {func(c *VolumeCopy) { c.VM.Disks[0].File = "../../../etc/passwd.qcow2" }, ErrBadImage},
		"hidden file":             {func(c *VolumeCopy) { c.VM.Disks[0].File = ".titanos" }, ErrBadImage},
		"missing disk":            {func(c *VolumeCopy) { c.VM.Disks[0].File = "vdisk9.qcow2" }, ErrBadImage},
		"path off the volumes":    {func(c *VolumeCopy) { c.VM.Disks = []Disk{{Path: "/etc/passwd"}} }, ErrUnknownVolume},
		"volume not mounted":      {func(c *VolumeCopy) { c.VM.Disks = append(c.VM.Disks, Disk{Path: "/mnt-gone/x.qcow2"}) }, ErrUnknownVolume},
		"other volume": {func(c *VolumeCopy) {
			c.VM.Disks = append(c.VM.Disks, Disk{File: "a.qcow2", Format: "qcow2", SizeBytes: gib, Volume: "gone"})
		}, ErrUnknownVolume},
		"image of another VM":  {func(c *VolumeCopy) { c.VM.Disks = append(c.VM.Disks, Disk{Path: image}) }, ErrBadImage},
		"whole disk not free":  {func(c *VolumeCopy) { c.VM.Disks = append(c.VM.Disks, Disk{Path: "/dev/disk/by-id/ata-x"}) }, ErrBadImage},
		"unknown format":       {func(c *VolumeCopy) { c.Format = 2 }, ErrNoCopy},
		"bad name in the copy": {func(c *VolumeCopy) { c.VM.Name = "../x" }, ErrNoCopy},
	} {
		tamper(tc.change)
		if _, err := fresh.Adopt(ctx, AdoptRequest{Volume: "data", Directory: "office"}, mounted); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	for name, req := range map[string]AdoptRequest{
		"folder with dots":   {Volume: "data", Directory: ".."},
		"folder with slash":  {Volume: "data", Directory: "office/../office"},
		"volume not mounted": {Volume: "gone", Directory: "office"},
		"no copy":            {Volume: "fast", Directory: "office"},
	} {
		if _, err := fresh.Adopt(ctx, req, mounted); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := fresh.get("office"); err == nil {
		t.Fatal("a refused VM was registered")
	}
	if names, _ := fresh.store.List("vms"); slices.Contains(names, "office.json") {
		t.Fatal("a refused VM was written to /config")
	}
	// Development mode changes nothing.
	os.WriteFile(copyPath, good, 0o600)
	fresh.Disabled = true
	if _, err := fresh.Adopt(ctx, AdoptRequest{Volume: "data", Directory: "office"}, mounted); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	fresh.Disabled = false
	if _, err := fresh.Adopt(ctx, AdoptRequest{Volume: "data", Directory: "office"}, mounted); err != nil {
		t.Fatal(err)
	}
}
