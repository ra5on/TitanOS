package vm

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamedNetworkPersistenceAndSecondDriverDrive(t *testing.T) {
	m, v := snapshotFixture(t)
	m.NetworkBridge = func(id string) (string, bool) { return "tnapps", id == "apps" }
	v.Network = "network:apps"
	v.Autostart = true
	v.Template = "windows"
	v.VirtIOISO = "/mnt/data/iso/virtio.iso"
	if err := m.persistVM(v); err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Autostart bool `json:"autostart"`
	}
	if err := m.store.ReadJSON(configDir+"/"+v.Name+".json", &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Autostart {
		t.Fatal("old application would autostart guest on unknown network")
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	runtime, _ := m.get(v.Name)
	if !runtime.Autostart || runtime.Network != v.Network || runtime.TopologyAutostart != nil {
		t.Fatal(runtime)
	}
	xml, err := m.domainXML(runtime)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(xml, `bridge="tnapps"`) || !strings.Contains(xml, `file="/mnt/data/iso/virtio.iso"`) || strings.Count(xml, `device="cdrom"`) != 2 {
		t.Fatal(xml)
	}
	m.NetworkBridge = func(string) (string, bool) { return "", false }
	if _, err = m.domainXML(runtime); err == nil {
		t.Fatal("unknown network silently became br0 or user networking")
	}
}
func TestDriverMediaAvailabilityAndSnapshotExclusion(t *testing.T) {
	m, v := snapshotFixture(t)
	v.VirtIOISO = filepath.Join(m.mountRoot, "other/iso/drivers.iso")
	m.vms[v.Name] = v
	if m.available(v, []string{v.Volume}) {
		t.Fatal("unmounted driver media accepted")
	}
	if m.snapshotReason(v) != "iso" {
		t.Fatal("removable driver media not excluded from RAM snapshots")
	}
	os.MkdirAll(filepath.Join(m.mountRoot, "other/iso"), 0755)
	os.WriteFile(v.VirtIOISO, []byte("iso"), 0640)
	if err := m.CreateSnapshot(context.Background(), v.Name, SnapshotRequest{Name: "test"}, []string{v.Volume}); err == nil {
		t.Fatal("guest with driver ISO snapshotted")
	}
}
