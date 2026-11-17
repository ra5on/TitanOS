package vm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The overlay of a disk was committed back and the guest switched over,
// but libvirt did not answer afterwards (or titand stopped) before the
// file went. If the VM is stopped meanwhile, that stale overlay must not
// be committed into the image over what the guest wrote since.
func TestCommittedOverlayIsNotCommittedAgain(t *testing.T) {
	m, r, vm := diskFixture(t)
	ctx := context.Background()
	b, err := m.BeginDiskBackup(ctx, "ha", DiskBackupOptions{Snapshot: true})
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	vda := r.overlay["vda"]
	r.afterCommit = func() { r.failing = "domblklist" }
	r.mu.Unlock()
	if err := b.End(); code(err) != DiskUndoFailed {
		t.Fatalf("end: %v", err)
	}
	if _, err := os.Stat(vda); err != nil {
		t.Fatalf("the overlay of vda went before its record said so: %v", err)
	}
	// The VM is shut down; the next look undoes the rest.
	os.Remove(filepath.Join(m.pidDir, "ha.pid"))
	r.mu.Lock()
	r.failing, r.afterCommit, r.lines = "", nil, nil
	r.mu.Unlock()
	if err := m.RecoverDiskBackups(ctx, true); err != nil {
		t.Fatal(err)
	}
	for _, line := range r.lines {
		if strings.Contains(line, "qemu-img commit") && strings.Contains(line, vda) {
			t.Fatalf("the overlay committed back live was committed again: %s", line)
		}
	}
	if r.ran("qemu-img commit") != 1 {
		t.Errorf("the overlay of vdb (never committed) was not committed: %v", r.lines)
	}
	if got := overlays(m); len(got) != 0 {
		t.Errorf("overlays left: %v", got)
	}
	if _, err := os.Stat(record(m, vm)); !os.IsNotExist(err) {
		t.Error("record left")
	}
}
