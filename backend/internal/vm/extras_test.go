package vm

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLendRecordsNodesBeforeALaterFailure(t *testing.T) {
	m, _ := newTestManager(t)
	m.byID = t.TempDir()
	m.sysDir, m.devDir = t.TempDir(), t.TempDir()
	disk := filepath.Join(t.TempDir(), "sda")
	if err := os.WriteFile(disk, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(m.byID, "ata-disk")
	if err := os.Symlink(disk, link); err != nil {
		t.Fatal(err)
	}
	// A plugged-in USB device whose node is missing: its chown fails.
	usb := filepath.Join(m.sysDir, "bus/usb/devices/1-1")
	os.MkdirAll(usb, 0o755)
	for file, value := range map[string]string{"idVendor": "046d", "idProduct": "c52b", "busnum": "1", "devnum": "2"} {
		os.WriteFile(filepath.Join(usb, file), []byte(value+"\n"), 0o644)
	}
	vm := VM{Name: "box", Disks: []Disk{{Path: link}}, USB: []string{"046d:c52b"}}
	if err := m.lend(vm); err == nil {
		t.Fatal("missing USB node lent")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !slices.Contains(m.borrowed["box"], disk) {
		t.Fatalf("changed disk not recorded: %v", m.borrowed)
	}
	m.release()
	if _, ok := m.borrowed["box"]; ok {
		t.Fatal("nodes of a failed start never returned")
	}
}
