package vm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSpaceUsed(t *testing.T) {
	m, _ := newTestManager(t)
	if got := m.SpaceUsed(); got != 0 {
		t.Fatalf("no VMs: %d bytes", got)
	}
	own := filepath.Join(m.mountRoot, "data", "vms", "web")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}
	disk := filepath.Join(own, "vdisk1.qcow2")
	if err := os.WriteFile(disk, make([]byte, 64<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(m.mountRoot, "fast", "images", "base.raw")
	os.MkdirAll(filepath.Dir(image), 0o755)
	if err := os.WriteFile(image, make([]byte, 32<<10), 0o644); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	m.vms["web"] = VM{Name: "web", Volume: "data", Disks: []Disk{
		{File: "vdisk1.qcow2", Volume: "data"},
		{Path: image},
		{Path: "/dev/disk/by-id/ata-whole"},
	}}
	m.mu.Unlock()
	got := m.SpaceUsed()
	// Blocks as the file system allocated them: at least the written data.
	if got < (64+32)<<10 {
		t.Fatalf("SpaceUsed = %d, want at least %d", got, (64+32)<<10)
	}
}
