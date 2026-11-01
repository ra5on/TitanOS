package hwcheck

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// corpusDevice is a PCI device of an lspci report of linux-hardware.org.
type corpusDevice struct {
	Class   string   `json:"class"`
	Vendor  string   `json:"vendor"`
	Device  string   `json:"device"`
	SV      string   `json:"sv"`
	SD      string   `json:"sd"`
	Driver  string   `json:"driver"`
	Modules []string `json:"modules"`
}

func (d corpusDevice) modalias() string {
	hex := func(value string) string {
		if value == "" {
			return "00000000"
		}
		return fmt.Sprintf("%08s", strings.ToUpper(value))
	}
	class := strings.ToUpper(d.Class + "000000")[:6]
	return "pci:v" + hex(d.Vendor) + "d" + hex(d.Device) + "sv" + hex(d.SV) + "sd" + hex(d.SD) +
		"bc" + class[0:2] + "sc" + class[2:4] + "i" + class[4:6]
}

// nasClasses are the PCI classes a NAS needs drivers for: storage,
// network, display, USB and accelerators.
var nasClasses = []string{"01", "02", "03", "0c03", "12"}

// The PCI devices of about 800 real x86-64 machines (testdata/linuxhw, CC-BY-4.0
// linux-hardware.org) as TitanOS's kernel (the modules of the QEMU
// snapshot, Debian 6.12) sees them, if no driver had taken them:
// - a device whose driver on that machine is a module of ours and that a
//   pattern of ours matches is never "not supported";
// - storage, network, display, USB and accelerator devices that had a
//   driver there but have none in 6.12 are listed with the hint TitanOS
//   shows (testdata/linuxhw/missing-6.12.txt). A new kernel, plugin or
//   table entry changes the list on purpose: run with -update.
func TestLinuxHardwareCorpus(t *testing.T) {
	file, err := os.Open("testdata/linuxhw/lspci-sample.json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Machines []struct {
			Type    string         `json:"type"`
			Arch    string         `json:"arch"`
			Devices []corpusDevice `json:"devices"`
		} `json:"machines"`
	}
	if err := json.NewDecoder(reader).Decode(&corpus); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := os.Open("testdata/snapshots/qemu-q35.tar.gz")
	defer snapshot.Close()
	dir := t.TempDir()
	if err := Unpack(snapshot, dir); err != nil {
		t.Fatal(err)
	}
	c := OpenSnapshot(dir)
	release := c.kernelRelease()
	aliases := c.moduleAliases(release)
	if aliases == nil || len(corpus.Machines) < 500 {
		t.Fatalf("corpus or aliases missing: %d machines", len(corpus.Machines))
	}
	// Built-in drivers bind without an alias file of their own.
	builtin := map[string]bool{}
	if data, err := os.ReadFile(filepath.Join(dir, "modules", release, "modules.builtin")); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			builtin[normalizeModule(strings.TrimSuffix(filepath.Base(line), ".ko"))] = true
		}
	}
	if !builtin["virtio_pci"] {
		t.Fatal("modules.builtin missing from the snapshot")
	}
	seen := map[string]bool{}
	missing := map[string]bool{}
	devices := 0
	machines := 0
	for _, machine := range corpus.Machines {
		if machine.Arch != "x86_64" {
			continue // TitanOS is amd64 only
		}
		machines++
		for _, d := range machine.Devices {
			alias := d.modalias()
			if seen[alias] {
				continue
			}
			seen[alias] = true
			devices++
			device := UnboundDevice{Bus: "pci", ID: d.Vendor + ":" + d.Device, Class: d.Class, modalias: alias, Plugins: []PluginHint{}}
			classify(&device, aliases, release, func() string { return "" })
			theirs := d.Driver
			if theirs == "" && len(d.Modules) > 0 {
				theirs = d.Modules[0]
			}
			if device.Hint == "unsupported" && aliases.module(alias) != "" {
				t.Errorf("%s: not supported although %s matches", alias, aliases.module(alias))
			}
			if theirs == "" || device.Hint == "driver_failed" || builtin[normalizeModule(theirs)] {
				continue
			}
			if !slices.ContainsFunc(nasClasses, func(prefix string) bool { return strings.HasPrefix(d.Class, prefix) }) {
				continue
			}
			if !aliases.modules[normalizeModule(theirs)] && !slices.ContainsFunc(d.Modules, func(m string) bool { return aliases.modules[normalizeModule(m)] }) {
				// A driver Debian's 6.12 does not build at all.
				missing[fmt.Sprintf("%s:%s class %s  %s  (there: %s, not in 6.12)", d.Vendor, d.Device, d.Class, device.Hint+hintDetail(device), theirs)] = true
				continue
			}
			// The module is ours, but 6.12 does not know this ID yet.
			missing[fmt.Sprintf("%s:%s class %s  %s  (there: %s, newer ID)", d.Vendor, d.Device, d.Class, device.Hint+hintDetail(device), theirs)] = true
		}
	}
	lines := make([]string, 0, len(missing))
	for line := range missing {
		lines = append(lines, line)
	}
	sort.Strings(lines)
	got := strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata/linuxhw", "missing-6.12.txt")
	header := fmt.Sprintf("# %d different PCI devices of %d machines; storage, network, display, USB and\n# accelerator devices with a driver there and none in Debian's 6.12, with the hint TitanOS shows.\n", devices, machines)
	if *update {
		os.WriteFile(path, []byte(header+got), 0o644)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update)", err)
	}
	if string(want) != header+got {
		t.Errorf("the list changed (run with -update if on purpose):\n%s", header+got)
	}
}

func hintDetail(device UnboundDevice) string {
	switch device.Hint {
	case "kernel":
		return " " + device.Kernel + " " + device.Driver
	case "plugin":
		names := []string{}
		for _, plugin := range device.Plugins {
			names = append(names, plugin.Plugin)
		}
		return " " + strings.Join(names, ",")
	}
	return ""
}
