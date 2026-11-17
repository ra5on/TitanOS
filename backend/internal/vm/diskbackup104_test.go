package vm

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// An unreadable record of a backup is not set aside, and no repair runs,
// while a volume with one of the VM's disks is not mounted: an overlay
// there cannot be seen, and the guest's writes in it would be cut off
// (as F-102-2, for an unusable record).
func TestUnreadableRecordWaitsForEveryVolume(t *testing.T) {
	ctx := context.Background()
	m, r, vm := diskFixture(t)
	second := filepath.Join(m.mountRoot, "data", "vms", "ha")
	if err := os.MkdirAll(second, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Rename(filepath.Join(m.mountRoot, "fast", "vms", "ha", vm.Disks[1].File), filepath.Join(second, vm.Disks[1].File))
	vm.Disks[1].Volume = "data"
	m.mu.Lock()
	m.vms["ha"] = vm
	m.mu.Unlock()
	os.Remove(filepath.Join(m.pidDir, "ha.pid"))
	// Only the overlay of the second disk was left, on "data".
	os.WriteFile(filepath.Join(second, vm.Disks[1].File+".1"+OverlaySuffix), []byte("overlay"), 0o600)
	os.MkdirAll(filepath.Dir(record(m, vm)), 0o700)
	os.WriteFile(record(m, vm), []byte("{not json"), 0o600)
	if err := os.Rename(filepath.Join(m.mountRoot, "data"), filepath.Join(m.mountRoot, "data-away")); err != nil {
		t.Fatal(err)
	}
	m.Mounted = func() []string { return []string{"fast"} }
	if lock, err := m.diskOperation(ctx, "ha"); err == nil {
		lock.Unlock()
		t.Fatal("the VM is free while a volume of its disks is missing")
	}
	if err := m.RepairDiskBackup(ctx, "ha"); err == nil {
		t.Fatal("repaired while a volume of its disks is missing")
	}
	if _, err := os.Stat(record(m, vm)); err != nil {
		t.Fatalf("record set aside while an overlay could not be seen: %v", err)
	}
	if n := r.ran("qemu-img commit"); n != 0 {
		t.Fatalf("committed: %v", r.lines)
	}
}
