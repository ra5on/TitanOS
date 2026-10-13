// Package hardware finds the devices of the NAS that can be handed to
// virtual machines: graphics cards, accelerators and USB devices.
package hardware

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var (
	// ErrUnknown means a device is not there (any more).
	ErrUnknown = errors.New("hardware: no such device")
	// ErrNotPassable means a device cannot be handed to a VM.
	ErrNotPassable = errors.New("hardware: the device cannot be handed to a VM")
)

// PCI is a device on the PCI bus.
type PCI struct {
	// Address is domain:bus:slot.function, e.g. 0000:01:00.0.
	Address string `json:"address"`
	Vendor  string `json:"vendor"`
	Device  string `json:"device"`
	// VendorName is set for the vendors TitanOS knows by heart.
	VendorName string `json:"vendorName"`
	// Kind is igpu, gpu or npu.
	Kind string `json:"kind"`
	// Driver is the driver of the NAS that has the device, or empty.
	Driver string `json:"driver"`
	// IOMMUGroup is the group the device is isolated in, -1 without IOMMU.
	IOMMUGroup int `json:"iommuGroup"`
	// Primary is the display the firmware used: the console of the NAS.
	Primary bool `json:"primary"`
	// Companions are the other functions of the same card, such as the
	// sound output of a graphics card. They go to a VM together with it.
	Companions []string `json:"companions"`
	// Passable says whether the device can be handed to a VM; if not,
	// Reason says why: no_iommu, shared_group, npu.
	Passable bool   `json:"passable"`
	Reason   string `json:"reason"`
}

// USB is a device on a USB port.
type USB struct {
	// ID is vendor:product, e.g. 046d:c52b.
	ID   string `json:"id"`
	Name string `json:"name"`
	// Port says where the device is plugged in, e.g. 1-4.
	Port string `json:"port"`
	// Stick marks the stick TitanOS runs from. It never goes to a VM.
	Stick bool `json:"stick"`
	// Duplicate is set if several devices of this kind are plugged in;
	// a VM then gets the first one it finds.
	Duplicate bool `json:"duplicate"`
}

// Status is everything the hardware page shows.
type Status struct {
	// IOMMU says whether the machine can isolate devices, which handing
	// PCI devices to VMs needs.
	IOMMU bool  `json:"iommu"`
	PCI   []PCI `json:"pci"`
	USB   []USB `json:"usb"`
}

// Manager reads the devices from /sys.
type Manager struct {
	sys string
	// stick is the label of the stick TitanOS runs from.
	stickLink string
}

// NewManager returns a manager looking at the real system.
func NewManager() *Manager {
	return &Manager{sys: "/sys", stickLink: "/dev/disk/by-label/TITANOS"}
}

var vendors = map[string]string{"8086": "Intel", "10de": "NVIDIA", "1002": "AMD", "1022": "AMD", "1234": "QEMU", "1af4": "Red Hat", "1b36": "Red Hat"}

func (m *Manager) read(parts ...string) string {
	data, _ := os.ReadFile(filepath.Join(parts...))
	return strings.TrimSpace(string(data))
}

// slot returns domain:bus:slot of an address.
func slot(address string) string {
	before, _, _ := strings.Cut(address, ".")
	return before
}

// Status looks at the devices as they are right now.
func (m *Manager) Status() Status {
	status := Status{PCI: []PCI{}, USB: []USB{}}
	if groups, err := os.ReadDir(filepath.Join(m.sys, "kernel/iommu_groups")); err == nil && len(groups) > 0 {
		status.IOMMU = true
	}

	root := filepath.Join(m.sys, "bus/pci/devices")
	entries, _ := os.ReadDir(root)
	classes := map[string]string{}
	for _, entry := range entries {
		classes[entry.Name()] = strings.TrimPrefix(m.read(root, entry.Name(), "class"), "0x")
	}
	for _, entry := range entries {
		address, class := entry.Name(), classes[entry.Name()]
		vendor := strings.TrimPrefix(m.read(root, address, "vendor"), "0x")
		device := PCI{Address: address, Vendor: vendor, Device: strings.TrimPrefix(m.read(root, address, "device"), "0x"), VendorName: vendors[vendor], IOMMUGroup: -1, Companions: []string{}}
		switch {
		case strings.HasPrefix(class, "03") && vendor == "8086":
			device.Kind = "igpu"
		case strings.HasPrefix(class, "03"):
			device.Kind = "gpu"
		case strings.HasPrefix(class, "12"):
			device.Kind = "npu"
		default:
			continue
		}
		if driver, err := os.Readlink(filepath.Join(root, address, "driver")); err == nil {
			device.Driver = filepath.Base(driver)
		}
		device.Primary = m.read(root, address, "boot_vga") == "1"
		for other := range classes {
			if other != address && slot(other) == slot(address) {
				device.Companions = append(device.Companions, other)
			}
		}
		sort.Strings(device.Companions)

		clean := true
		if group, err := os.Readlink(filepath.Join(root, address, "iommu_group")); err == nil {
			device.IOMMUGroup, _ = strconv.Atoi(filepath.Base(group))
			members, _ := os.ReadDir(filepath.Join(root, address, "iommu_group", "devices"))
			for _, member := range members {
				// Everything in the group goes to the VM together. That only
				// works if the group is this card alone; bridges do not count.
				if slot(member.Name()) != slot(address) && !strings.HasPrefix(classes[member.Name()], "0604") {
					clean = false
				}
			}
		}
		switch {
		case device.Kind == "npu":
			device.Reason = "npu"
		case device.IOMMUGroup < 0:
			device.Reason = "no_iommu"
		case !clean:
			device.Reason = "shared_group"
		default:
			device.Passable = true
		}
		status.PCI = append(status.PCI, device)
	}
	sort.Slice(status.PCI, func(i, j int) bool { return status.PCI[i].Address < status.PCI[j].Address })

	status.USB = m.usb()
	return status
}

func (m *Manager) usb() []USB {
	root := filepath.Join(m.sys, "bus/usb/devices")
	entries, _ := os.ReadDir(root)
	// Where the stick is plugged in: its block device leads back to a USB port.
	stick := ""
	if device, err := filepath.EvalSymlinks(m.stickLink); err == nil {
		if path, err := filepath.EvalSymlinks(filepath.Join(m.sys, "class/block", filepath.Base(device))); err == nil {
			stick = path
		}
	}
	devices := []USB{}
	seen := map[string]int{}
	for _, entry := range entries {
		port := entry.Name()
		// Interfaces ("1-4:1.0") and the root hubs ("usb1") are not devices.
		if strings.Contains(port, ":") || strings.HasPrefix(port, "usb") {
			continue
		}
		vendor, product := m.read(root, port, "idVendor"), m.read(root, port, "idProduct")
		if vendor == "" || product == "" || m.read(root, port, "bDeviceClass") == "09" {
			continue // hubs stay with the NAS
		}
		name := strings.TrimSpace(m.read(root, port, "manufacturer") + " " + m.read(root, port, "product"))
		device := USB{ID: vendor + ":" + product, Name: name, Port: port}
		if real, err := filepath.EvalSymlinks(filepath.Join(root, port)); err == nil && stick != "" && strings.HasPrefix(stick, real+"/") {
			device.Stick = true
		}
		seen[device.ID]++
		devices = append(devices, device)
	}
	for i := range devices {
		devices[i].Duplicate = seen[devices[i].ID] > 1
	}
	sort.Slice(devices, func(i, j int) bool { return devices[i].Port < devices[j].Port })
	return devices
}

// Check makes sure devices can be handed to a VM and returns the PCI
// addresses with the companions of every card added.
func (m *Manager) Check(pci, usb []string) ([]string, error) {
	status := m.Status()
	var addresses []string
	add := func(address string) {
		if !slices.Contains(addresses, address) {
			addresses = append(addresses, address)
		}
	}
	for _, address := range pci {
		at := slices.IndexFunc(status.PCI, func(device PCI) bool { return device.Address == address })
		if at < 0 {
			// A companion named on its own is fine if its card is named too.
			named := slices.ContainsFunc(status.PCI, func(device PCI) bool {
				return slices.Contains(pci, device.Address) && slices.Contains(device.Companions, address)
			})
			if !named {
				return nil, fmt.Errorf("%w: %s", ErrUnknown, address)
			}
			continue
		}
		device := status.PCI[at]
		if !device.Passable {
			return nil, fmt.Errorf("%w: %s (%s)", ErrNotPassable, address, device.Reason)
		}
		add(address)
		for _, companion := range device.Companions {
			add(companion)
		}
	}
	for _, id := range usb {
		at := slices.IndexFunc(status.USB, func(device USB) bool { return device.ID == id })
		if at < 0 {
			return nil, fmt.Errorf("%w: %s", ErrUnknown, id)
		}
		if status.USB[at].Stick {
			return nil, fmt.Errorf("%w: TitanOS runs from this stick", ErrNotPassable)
		}
	}
	if addresses == nil {
		addresses = []string{}
	}
	return addresses, nil
}
