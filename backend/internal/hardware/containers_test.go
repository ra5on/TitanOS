package hardware

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type gpuRunner struct {
	output string
	calls  []string
}

func (r *gpuRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	return r.output, nil
}
func bindDriver(t *testing.T, sys, address, driver string) {
	t.Helper()
	path := filepath.Join(sys, "bus/pci/devices", address, "driver")
	os.Remove(path)
	target := filepath.Join(sys, "bus/pci/drivers", driver)
	os.MkdirAll(target, 0o755)
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}
func TestContainerDevicesResolveNodesAndNvidiaUUID(t *testing.T) {
	sys, dev := t.TempDir(), t.TempDir()
	device(t, sys, "0000:00:02.0", "030000", "8086", 0, nil)
	device(t, sys, "0000:01:00.0", "030000", "10de", 1, nil)
	bindDriver(t, sys, "0000:00:02.0", "i915")
	bindDriver(t, sys, "0000:01:00.0", "nvidia")
	class := filepath.Join(sys, "class/drm/renderD129")
	os.MkdirAll(class, 0o755)
	os.Symlink(filepath.Join(sys, "bus/pci/devices/0000:00:02.0"), filepath.Join(class, "device"))
	os.MkdirAll(filepath.Join(dev, "dri"), 0o755)
	os.Symlink("/dev/null", filepath.Join(dev, "dri/renderD129"))
	m := &Manager{sys: sys}
	r := &gpuRunner{output: "GPU-abcd-1234, 2"}
	list := m.containerDevices(context.Background(), r, dev)
	if len(list) != 2 || !list[0].Available || list[0].Mappings[0] != filepath.Join(dev, "dri/renderD129")+":"+filepath.Join(dev, "dri/renderD129") || list[1].Mappings[0] != "nvidia.com/gpu=GPU-abcd-1234" {
		t.Fatal(list)
	}
	bindDriver(t, sys, "0000:01:00.0", "vfio-pci")
	list = m.containerDevices(context.Background(), r, dev)
	if list[1].Available || list[1].Reason != "vm" || len(r.calls) != 1 {
		t.Fatal(list, r.calls)
	}
}
func TestNvidiaPowerPersistsOnlySuccessfulAndSupportedSettings(t *testing.T) {
	sys := t.TempDir()
	address := "0000:01:00.0"
	device(t, sys, address, "030000", "10de", 1, nil)
	bindDriver(t, sys, address, "nvidia")
	store := config.Open(t.TempDir())
	n := NewNvidiaControl(store, &Manager{sys: sys}, false)
	r := &gpuRunner{output: "100, 250, 200"}
	n.runner = r
	if err := n.Load(); err != nil {
		t.Fatal(err)
	}
	if err := n.Set(context.Background(), address, NvidiaSettings{20}); err == nil {
		t.Fatal("applied below board minimum")
	}
	if len(r.calls) != 1 || len(n.settings) != 0 {
		t.Fatal(r.calls, n.settings)
	}
	if err := n.Set(context.Background(), address, NvidiaSettings{75}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(r.calls[len(r.calls)-1], "--power-limit=150.00") {
		t.Fatal(r.calls)
	}
	var saved map[string]NvidiaSettings
	if err := store.ReadJSON(nvidiaConfig, &saved); err != nil || saved[address].PowerPercent != 75 {
		t.Fatal(saved, err)
	}
	bindDriver(t, sys, address, "vfio-pci")
	before := len(r.calls)
	if err := n.Set(context.Background(), address, NvidiaSettings{80}); err == nil || len(r.calls) != before {
		t.Fatal("touched guest GPU", err)
	}
	bindDriver(t, sys, address, "nvidia")
	n.Disabled = true
	if err := n.Set(context.Background(), address, NvidiaSettings{80}); err == nil || len(r.calls) != before {
		t.Fatal("dev changed GPU", err)
	}
}
