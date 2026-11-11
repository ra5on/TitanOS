package vm

import (
	"path/filepath"
	"slices"
	"testing"
)

// A backup asks which image files running VMs write to; whole disks and
// the disks of stopped VMs are none of them.
func TestRunningDiskFiles(t *testing.T) {
	m, _ := newTestManager(t)
	m.mu.Lock()
	m.vms["ha"] = VM{Name: "ha", Volume: "fast", Directory: "Home Assistant", Disks: []Disk{
		{File: "haos.qcow2", Format: "qcow2", Volume: "fast"},
		{File: "data.raw", Format: "raw", Volume: "data"},
		{Path: "/dev/disk/by-id/ata-WHOLE", Format: "raw"},
		{Path: filepath.Join(m.mountRoot, "data", "media", "import.qcow2"), Format: "qcow2"},
	}}
	m.vms["off"] = VM{Name: "off", Volume: "fast", Disks: []Disk{{File: "vdisk1.qcow2", Format: "qcow2", Volume: "fast"}}}
	m.mu.Unlock()
	if files := m.RunningDiskFiles(); len(files) != 0 {
		t.Fatalf("disks of stopped VMs: %v", files)
	}
	running(t, m, "ha")
	want := []string{
		filepath.Join(m.mountRoot, "data", "media", "import.qcow2"),
		filepath.Join(m.mountRoot, "data", "vms", "Home Assistant", "data.raw"),
		filepath.Join(m.mountRoot, "fast", "vms", "Home Assistant", "haos.qcow2"),
	}
	if files := m.RunningDiskFiles(); !slices.Equal(files, want) {
		t.Fatalf("got %v, want %v", files, want)
	}
	m.Disabled = true
	if files := m.RunningDiskFiles(); len(files) != 0 {
		t.Fatalf("development mode: %v", files)
	}
}
