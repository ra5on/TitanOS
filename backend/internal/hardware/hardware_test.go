package hardware

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// device makes a PCI device in a pretended /sys.
func device(t *testing.T, sys, address, class, vendor string, group int, extra map[string]string) {
	t.Helper()
	dir := filepath.Join(sys, "bus/pci/devices", address)
	os.MkdirAll(dir, 0o755)
	files := map[string]string{"class": "0x" + class, "vendor": "0x" + vendor, "device": "0x1234"}
	for name, value := range extra {
		files[name] = value
	}
	for name, value := range files {
		os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0o644)
	}
	if group >= 0 {
		groupDir := filepath.Join(sys, "kernel/iommu_groups", string(rune('0'+group)))
		os.MkdirAll(filepath.Join(groupDir, "devices"), 0o755)
		os.Symlink(dir, filepath.Join(groupDir, "devices", address))
		os.Symlink(groupDir, filepath.Join(dir, "iommu_group"))
	}
}

func TestStatus(t *testing.T) {
	sys := t.TempDir()
	device(t, sys, "0000:00:02.0", "030000", "8086", 0, map[string]string{"boot_vga": "1"}) // iGPU
	device(t, sys, "0000:00:0b.0", "120000", "8086", 1, nil)                                // NPU
	device(t, sys, "0000:01:00.0", "030000", "10de", 2, nil)                                // graphics card …
	device(t, sys, "0000:01:00.1", "040300", "10de", 2, nil)                                // … with its sound output
	device(t, sys, "0000:00:01.0", "060400", "8086", 2, nil)                                // the bridge it sits behind
	device(t, sys, "0000:02:00.0", "030000", "1002", 3, nil)                                // a card sharing its group …
	device(t, sys, "0000:03:00.0", "010802", "144d", 3, nil)                                // … with an SSD
	device(t, sys, "0000:04:00.0", "020000", "8086", 4, nil)                                // a network card: not listed

	usb := filepath.Join(sys, "bus/usb/devices")
	for port, files := range map[string]map[string]string{
		"usb1":    {"idVendor": "1d6b", "idProduct": "0002", "bDeviceClass": "09"},
		"1-1":     {"idVendor": "05e3", "idProduct": "0608", "bDeviceClass": "09"},
		"1-2":     {"idVendor": "0781", "idProduct": "5581", "bDeviceClass": "00", "manufacturer": "SanDisk", "product": "Ultra"},
		"1-3":     {"idVendor": "1a86", "idProduct": "55d4", "bDeviceClass": "02", "product": "Zigbee"},
		"1-4":     {"idVendor": "1a86", "idProduct": "55d4", "bDeviceClass": "02", "product": "Zigbee"},
		"1-2:1.0": {"bInterfaceClass": "08"},
	} {
		os.MkdirAll(filepath.Join(usb, port), 0o755)
		for name, value := range files {
			os.WriteFile(filepath.Join(usb, port, name), []byte(value+"\n"), 0o644)
		}
	}
	// The stick TitanOS runs from hangs on port 1-2.
	block := filepath.Join(usb, "1-2", "host0/target/block/sda")
	os.MkdirAll(filepath.Join(block, "sda1"), 0o755)
	os.MkdirAll(filepath.Join(sys, "class/block"), 0o755)
	os.Symlink(filepath.Join(block, "sda1"), filepath.Join(sys, "class/block/sda1"))
	dev := t.TempDir()
	os.WriteFile(filepath.Join(dev, "sda1"), nil, 0o644)
	os.Symlink(filepath.Join(dev, "sda1"), filepath.Join(dev, "TITANOS"))

	m := &Manager{sys: sys, stickLink: filepath.Join(dev, "TITANOS")}
	status := m.Status()
	if !status.IOMMU || len(status.PCI) != 4 {
		t.Fatalf("status = %+v", status)
	}
	igpu, npu, card, shared := status.PCI[0], status.PCI[1], status.PCI[2], status.PCI[3]
	if igpu.Kind != "igpu" || !igpu.Primary || !igpu.Passable || igpu.VendorName != "Intel" || igpu.IOMMUGroup != 0 {
		t.Errorf("iGPU = %+v", igpu)
	}
	if npu.Kind != "npu" || npu.Passable || npu.Reason != "npu" {
		t.Errorf("NPU = %+v", npu)
	}
	if card.Kind != "gpu" || !card.Passable || !slices.Equal(card.Companions, []string{"0000:01:00.1"}) {
		t.Errorf("graphics card = %+v", card)
	}
	if shared.Passable || shared.Reason != "shared_group" {
		t.Errorf("card in a shared group = %+v", shared)
	}
	if len(status.USB) != 3 || !status.USB[0].Stick || status.USB[0].Name != "SanDisk Ultra" || status.USB[1].Stick || !status.USB[1].Duplicate || status.USB[0].Duplicate {
		t.Errorf("USB = %+v", status.USB)
	}

	// The sound output goes along with its card.
	if pci, err := m.Check([]string{"0000:01:00.0"}, []string{"1a86:55d4"}); err != nil || !slices.Equal(pci, []string{"0000:01:00.0", "0000:01:00.1"}) {
		t.Errorf("Check = %v, %v", pci, err)
	}
	if pci, err := m.Check([]string{"0000:01:00.0", "0000:01:00.1"}, nil); err != nil || len(pci) != 2 {
		t.Errorf("card with its companion named: %v, %v", pci, err)
	}
	for name, tc := range map[string]struct {
		pci, usb []string
		want     error
	}{
		"NPU":             {[]string{"0000:00:0b.0"}, nil, ErrNotPassable},
		"shared group":    {[]string{"0000:02:00.0"}, nil, ErrNotPassable},
		"network card":    {[]string{"0000:04:00.0"}, nil, ErrUnknown},
		"companion alone": {[]string{"0000:01:00.1"}, nil, ErrUnknown},
		"the stick":       {nil, []string{"0781:5581"}, ErrNotPassable},
		"unplugged":       {nil, []string{"dead:beef"}, ErrUnknown},
	} {
		if _, err := m.Check(tc.pci, tc.usb); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}

	// Without IOMMU nothing on the PCI bus can be handed over.
	plain := t.TempDir()
	device(t, plain, "0000:01:00.0", "030000", "10de", -1, nil)
	if status := (&Manager{sys: plain}).Status(); status.IOMMU || status.PCI[0].Passable || status.PCI[0].Reason != "no_iommu" {
		t.Errorf("without IOMMU = %+v", status)
	}
}
