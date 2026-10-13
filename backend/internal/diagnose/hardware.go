package diagnose

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func readTrim(path string) string {
	data, _ := os.ReadFile(path)
	return strings.TrimSpace(string(data))
}

// hardware lists PCI and USB devices with their drivers and the
// temperatures the kernel knows, read from /sys.
func hardware(sys string) string {
	var b strings.Builder

	b.WriteString("PCI devices (address, class, vendor:device, driver)\n")
	devices, _ := filepath.Glob(filepath.Join(sys, "bus/pci/devices/*"))
	sort.Strings(devices)
	for _, device := range devices {
		driver := "-"
		if target, err := os.Readlink(filepath.Join(device, "driver")); err == nil {
			driver = filepath.Base(target)
		}
		fmt.Fprintf(&b, "%s  class %s  %s:%s  driver %s\n", filepath.Base(device),
			strings.TrimPrefix(readTrim(filepath.Join(device, "class")), "0x"),
			strings.TrimPrefix(readTrim(filepath.Join(device, "vendor")), "0x"),
			strings.TrimPrefix(readTrim(filepath.Join(device, "device")), "0x"), driver)
	}

	b.WriteString("\nUSB devices (port, vendor:product, name)\n")
	devices, _ = filepath.Glob(filepath.Join(sys, "bus/usb/devices/*"))
	sort.Strings(devices)
	for _, device := range devices {
		vendor := readTrim(filepath.Join(device, "idVendor"))
		if vendor == "" {
			continue // an interface of a device, not a device
		}
		fmt.Fprintf(&b, "%s  %s:%s  %s %s\n", filepath.Base(device), vendor, readTrim(filepath.Join(device, "idProduct")),
			readTrim(filepath.Join(device, "manufacturer")), readTrim(filepath.Join(device, "product")))
	}

	b.WriteString("\nTemperatures\n")
	sensors, _ := filepath.Glob(filepath.Join(sys, "class/hwmon/*/temp*_input"))
	sort.Strings(sensors)
	for _, sensor := range sensors {
		milli, err := strconv.Atoi(readTrim(sensor))
		if err != nil {
			continue
		}
		label := readTrim(strings.TrimSuffix(sensor, "_input") + "_label")
		fmt.Fprintf(&b, "%s %s %s: %d °C\n", readTrim(filepath.Join(filepath.Dir(sensor), "name")), strings.TrimSuffix(filepath.Base(sensor), "_input"), label, milli/1000)
	}
	if len(sensors) == 0 {
		b.WriteString("(none reported)\n")
	}

	fmt.Fprintf(&b, "\nIOMMU groups: %d\n", func() int {
		groups, _ := filepath.Glob(filepath.Join(sys, "kernel/iommu_groups/*"))
		return len(groups)
	}())
	return b.String()
}
