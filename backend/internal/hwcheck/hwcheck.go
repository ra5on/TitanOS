// Package hwcheck looks at the hardware of the NAS without changing it:
// devices no driver took, missing firmware, what the CPU can do, how the
// disks are attached. It reads only sysfs, procfs and the kernel log of
// this start; nothing touches a disk, so it is safe in development mode.
package hwcheck

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// PluginHint is a plugin whose hardware rules match a device.
type PluginHint struct {
	Plugin    string `json:"plugin"`
	Title     string `json:"title"`
	Installed bool   `json:"installed"`
}

// Checker reads the hardware. The zero paths of a test checker point at a
// fixture tree.
type Checker struct {
	Sys    string
	Proc   string
	Dev    string
	PCIIDs string
	// Modules holds the module directories per kernel release
	// (/lib/modules), Modprobe the modprobe.d directories whose blacklist
	// entries count.
	Modules  string
	Modprobe []string
	// Debug is debugfs (GuC/HuC state for snapshots); may be "".
	Debug  string
	Runner run.Runner
	// Plugins returns, per modalias, the plugins that match it. Optional.
	Plugins func(aliases []string) map[string][]PluginHint

	cpuOnce sync.Once
	cpu     CPU
}

// New returns a checker for the real system.
func New() *Checker {
	return &Checker{Sys: "/sys", Proc: "/proc", Dev: "/dev", PCIIDs: "/usr/share/misc/pci.ids", Modules: "/lib/modules",
		Modprobe: []string{"/etc/modprobe.d", "/run/modprobe.d", "/usr/lib/modprobe.d", "/lib/modprobe.d"}, Debug: "/sys/kernel/debug", Runner: run.Exec{}}
}

func readTrim(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func driverOf(dir string) string {
	target, err := os.Readlink(filepath.Join(dir, "driver"))
	if err != nil {
		return ""
	}
	return filepath.Base(target)
}

// CPU is read once: the processor does not change while the NAS runs.
func (c *Checker) CPU() CPU {
	c.cpuOnce.Do(func() {
		data, _ := os.ReadFile(filepath.Join(c.Proc, "cpuinfo"))
		c.cpu = ParseCPUInfo(string(data))
	})
	return c.cpu
}

// MissingCPU returns what a compose file needs that this CPU lacks.
func (c *Checker) MissingCPU(compose string) []string {
	return c.CPU().Missing(ComposeNeeds(compose))
}

// kernelLog returns the kernel messages of this start, or "".
func (c *Checker) kernelLog(ctx context.Context) string {
	if c.Runner == nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	output, _ := c.Runner.Run(ctx, "", "journalctl", "-k", "-b", "0", "-o", "cat", "--no-pager", "-n", "20000")
	return output
}

// kernelRelease is the running kernel, e.g. 6.12.113+deb13-amd64.
func (c *Checker) kernelRelease() string {
	return readTrim(filepath.Join(c.Proc, "sys/kernel/osrelease"))
}

// Virtualization says whether VMs can run and devices can be handed to them.
type Virtualization struct {
	// Vendor of the CPU: intel, amd or other.
	Vendor string `json:"vendor"`
	// CPUFlag: the CPU shows vmx (Intel VT-x) or svm (AMD-V).
	CPUFlag bool `json:"cpuFlag"`
	// KVM: /dev/kvm exists, VMs run at full speed.
	KVM bool `json:"kvm"`
	// DisabledByFirmware: the kernel said the BIOS/UEFI switched it off.
	DisabledByFirmware bool `json:"disabledByFirmware"`
	// IOMMU: the kernel isolates devices (IOMMU groups exist).
	IOMMU bool `json:"iommu"`
	// IOMMUTable: the firmware describes an IOMMU (ACPI DMAR/IVRS) although
	// the kernel does not use it.
	IOMMUTable bool `json:"iommuTable"`
}

var disabledByBIOS = regexp.MustCompile(`(?i)(vmx|svm|kvm|virtuali[sz]ation).*disabled.*bios|disabled by bios`)

// Virtualization checks VT-x/AMD-V and the IOMMU. The kernel log is read
// only when something is missing.
func (c *Checker) Virtualization(ctx context.Context) Virtualization {
	cpu := c.CPU()
	v := Virtualization{Vendor: cpu.Vendor, CPUFlag: cpu.Has("vmx") || cpu.Has("svm")}
	if v.Vendor != "intel" && v.Vendor != "amd" {
		v.Vendor = "other"
	}
	_, err := os.Stat(filepath.Join(c.Dev, "kvm"))
	v.KVM = err == nil
	if groups, err := os.ReadDir(filepath.Join(c.Sys, "kernel/iommu_groups")); err == nil && len(groups) > 0 {
		v.IOMMU = true
	}
	for _, table := range []string{"DMAR", "IVRS"} {
		if _, err := os.Stat(filepath.Join(c.Sys, "firmware/acpi/tables", table)); err == nil {
			v.IOMMUTable = true
		}
	}
	if !v.KVM || !v.CPUFlag {
		v.DisabledByFirmware = disabledByBIOS.MatchString(c.kernelLog(ctx))
	}
	return v
}

// UnboundDevice is a PCI or USB device no driver has taken.
type UnboundDevice struct {
	Bus     string `json:"bus"`
	Address string `json:"address"`
	// ID is vendor:device (PCI) or vendor:product (USB).
	ID    string `json:"id"`
	Class string `json:"class"`
	Name  string `json:"name"`
	// Hint is plugin, kernel, driver_failed, not_needed or unsupported.
	// driver_failed: a module of the running kernel is made for the device
	// but did not take it; Messages are its kernel lines about the device.
	Hint    string       `json:"hint"`
	Plugins []PluginHint `json:"plugins,omitempty"`
	// Kernel is the first Linux version that drives the device (hint kernel)
	// or that fixes the failure (hint driver_failed).
	Kernel   string   `json:"kernel,omitempty"`
	Driver   string   `json:"driver,omitempty"`
	Messages []string `json:"messages,omitempty"`
	modalias string
}

// kernelFixes are failures of a driver of Linux 6.12 that a newer kernel
// fixes, recognised by the driver's kernel message.
var kernelFixes = []struct {
	driver  string
	message *regexp.Regexp
	version string
}{
	// Arrow Lake-HX boards set bus offset 3; "PCI: vmd: Handle
	// BUS_RESTRICT_CFG value 3 for Arrow Lake-HX", first in Linux 7.3.
	{"vmd", regexp.MustCompile(`Unknown Bus Offset Setting \(3\)`), "7.3"},
}

// newerKernel lists devices Linux 6.12 does not drive but a newer kernel
// does (checked against v6.13–v6.18, see docs/HARDWARE-COMPATIBILITY.md).
var newerKernel = map[string]struct{ version, driver string }{
	"10ec:8127": {"6.16", "r8169"}, // Realtek RTL8127A 10 GbE
	// RTL8125D (XID 0x689) and RTL8125BP share the ID of supported chips;
	// r8169 of 6.12 leaves them unbound.
	"10ec:8125": {"6.14", "r8169"},
	"8086:57ae": {"6.14", "ixgbe"}, // Intel E610 backplane
	"8086:57af": {"6.14", "ixgbe"}, // E610 SFP
	"8086:57b0": {"6.14", "ixgbe"}, // E610 10GBASE-T
	"8086:57b1": {"6.14", "ixgbe"}, // E610 2.5GBASE-T
	"8086:57b2": {"6.14", "ixgbe"}, // E610 SGMII
	"8088:5010": {"6.15", "txgbe"}, // Wangxun AML5010
	"8088:5110": {"6.15", "txgbe"},
	"8088:5025": {"6.15", "txgbe"},
	"8088:5125": {"6.15", "txgbe"},
	"8088:5040": {"6.15", "txgbe"},
	"8088:5140": {"6.15", "txgbe"},
}

// noDriverNeeded are PCI classes that usually work without a driver of
// their own: memory controllers, host/ISA/other bridges, system
// peripherals, processors, other serial bus controllers (mostly the SPI
// flash of the BIOS, whose driver Debian leaves out on purpose) and other
// signal processing controllers (chipset thermal sensors; the CPU
// temperature comes from coretemp/k10temp).
var noDriverNeeded = []string{"05", "0600", "0601", "0680", "0880", "0b", "0c80", "1180"}

// kernelAtLeast compares the release of the running kernel with "6.16".
func kernelAtLeast(release, version string) bool {
	parse := func(text string) (int, int) {
		major, rest, _ := strings.Cut(text, ".")
		minor := rest
		if end := strings.IndexFunc(minor, func(r rune) bool { return r < '0' || r > '9' }); end >= 0 {
			minor = minor[:end]
		}
		a, _ := strconv.Atoi(major)
		b, _ := strconv.Atoi(minor)
		return a, b
	}
	ra, rb := parse(release)
	va, vb := parse(version)
	return ra > va || ra == va && rb >= vb
}

// Unbound lists PCI devices without a driver and USB devices none of whose
// interfaces has one, with a hint for each. The kernel log is read only if
// a driver of the running kernel failed on a device.
func (c *Checker) Unbound(ctx context.Context) []UnboundDevice {
	return c.unbound(func() string { return c.kernelLog(ctx) })
}

func (c *Checker) unbound(kernelLog func() string) []UnboundDevice {
	devices := []UnboundDevice{}
	root := filepath.Join(c.Sys, "bus/pci/devices")
	entries, _ := os.ReadDir(root)
	wanted := map[string]bool{}
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		if driverOf(dir) != "" {
			continue
		}
		vendor := strings.TrimPrefix(readTrim(filepath.Join(dir, "vendor")), "0x")
		device := strings.TrimPrefix(readTrim(filepath.Join(dir, "device")), "0x")
		devices = append(devices, UnboundDevice{Bus: "pci", Address: entry.Name(), ID: vendor + ":" + device,
			Class: strings.TrimPrefix(readTrim(filepath.Join(dir, "class")), "0x"), modalias: readTrim(filepath.Join(dir, "modalias"))})
		wanted[vendor], wanted[vendor+":"+device] = true, true
	}
	names := readPCIIDs(c.PCIIDs, wanted)
	for i := range devices {
		vendor, device, _ := strings.Cut(devices[i].ID, ":")
		devices[i].Name = names.name(vendor, device)
	}

	usbRoot := filepath.Join(c.Sys, "bus/usb/devices")
	usb, _ := os.ReadDir(usbRoot)
	type usbDevice struct {
		bound   bool
		aliases []string
		class   string
	}
	found := map[string]*usbDevice{}
	for _, entry := range usb {
		parent, _, isInterface := strings.Cut(entry.Name(), ":")
		if !isInterface {
			continue
		}
		dir := filepath.Join(usbRoot, entry.Name())
		device := found[parent]
		if device == nil {
			device = &usbDevice{}
			found[parent] = device
		}
		if driverOf(dir) != "" {
			device.bound = true
		}
		device.aliases = append(device.aliases, readTrim(filepath.Join(dir, "modalias")))
		if device.class == "" {
			device.class = readTrim(filepath.Join(dir, "bInterfaceClass"))
		}
	}
	ports := make([]string, 0, len(found))
	for port := range found {
		ports = append(ports, port)
	}
	sort.Strings(ports)
	for _, port := range ports {
		device := found[port]
		dir := filepath.Join(usbRoot, port)
		if device.bound || strings.HasPrefix(port, "usb") {
			continue
		}
		vendor, product := readTrim(filepath.Join(dir, "idVendor")), readTrim(filepath.Join(dir, "idProduct"))
		name := strings.TrimSpace(readTrim(filepath.Join(dir, "manufacturer")) + " " + readTrim(filepath.Join(dir, "product")))
		if name == "" {
			name = "USB " + vendor + ":" + product
		}
		alias := ""
		if len(device.aliases) > 0 {
			alias = device.aliases[0]
		}
		devices = append(devices, UnboundDevice{Bus: "usb", Address: port, ID: vendor + ":" + product, Class: device.class, Name: name, modalias: alias})
		// Every interface counts for plugin rules.
		for _, extra := range device.aliases[min(1, len(device.aliases)):] {
			devices[len(devices)-1].modalias += "\n" + extra
		}
	}

	var matches map[string][]PluginHint
	if c.Plugins != nil {
		var aliases []string
		for _, device := range devices {
			aliases = append(aliases, strings.Split(device.modalias, "\n")...)
		}
		matches = c.Plugins(aliases)
	}
	release := c.kernelRelease()
	aliases := c.moduleAliases(release)
	log, logRead := "", false
	for i := range devices {
		device := &devices[i]
		for _, alias := range strings.Split(device.modalias, "\n") {
			for _, hint := range matches[alias] {
				if !slices.ContainsFunc(device.Plugins, func(p PluginHint) bool { return p.Plugin == hint.Plugin }) {
					device.Plugins = append(device.Plugins, hint)
				}
			}
		}
		classify(device, aliases, release, func() string {
			if !logRead {
				log, logRead = kernelLog(), true
			}
			return log
		})
	}
	// What needs attention first; devices that need no driver last.
	sort.SliceStable(devices, func(i, j int) bool {
		return (devices[i].Hint == "not_needed") != (devices[j].Hint == "not_needed") && devices[j].Hint == "not_needed"
	})
	return devices
}

// classify sets the hint of a device without a driver; its Plugins are
// already known. kernelLog is asked only for a failed driver.
func classify(device *UnboundDevice, aliases *moduleAliases, release string, kernelLog func() string) {
	newer, known := newerKernel[device.ID]
	module := aliases.module(device.modalias)
	switch {
	case len(device.Plugins) > 0:
		device.Hint = "plugin"
	case device.Bus == "pci" && known && !kernelAtLeast(release, newer.version):
		device.Hint, device.Kernel, device.Driver = "kernel", newer.version, newer.driver
	case module != "":
		device.Hint, device.Driver = "driver_failed", module
		device.Messages = driverMessages(kernelLog(), module, device.Address)
		for _, fix := range kernelFixes {
			if fix.driver == module && !kernelAtLeast(release, fix.version) &&
				slices.ContainsFunc(device.Messages, fix.message.MatchString) {
				device.Kernel = fix.version
			}
		}
	case device.Bus == "pci" && slices.ContainsFunc(noDriverNeeded, func(prefix string) bool { return strings.HasPrefix(device.Class, prefix) }):
		device.Hint = "not_needed"
	default:
		device.Hint = "unsupported"
	}
}

// FirmwareMissing is a firmware file a driver asked for and did not get.
type FirmwareMissing struct {
	File string `json:"file"`
	// Device is how the kernel named the asking device, e.g.
	// "r8169 0000:03:00.0".
	Device string `json:"device,omitempty"`
}

var (
	firmwareDirect = regexp.MustCompile(`^(?:(\S+ \S+): )?Direct firmware load for (\S+) failed`)
	firmwareFailed = regexp.MustCompile(`^(?:(\S+ \S+): )?firmware: failed to load (\S+)`)
	firmwareFile   = regexp.MustCompile(`[\w.+-]+(?:/[\w.+-]+)*\.(?:bin|fw|ucode|sbin|img|pnvm|dat)\b`)
	firmwareWords  = regexp.MustCompile(`(?i)firmware.*(missing|not found|failed to load|unable to load)`)
	devicePrefix   = regexp.MustCompile(`^(\S+ [0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]|\S+ \d+-[\d.]+(?::\d+\.\d+)?): `)
)

// ParseFirmwareMissing picks the "firmware missing" messages of a kernel
// log, one per file.
func ParseFirmwareMissing(log string) []FirmwareMissing {
	result := []FirmwareMissing{}
	seen := map[string]int{}
	scanner := bufio.NewScanner(strings.NewReader(log))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() && len(result) < 50 {
		line := strings.TrimSpace(scanner.Text())
		var entry FirmwareMissing
		if match := firmwareDirect.FindStringSubmatch(line); match != nil {
			entry = FirmwareMissing{File: match[2], Device: match[1]}
		} else if match := firmwareFailed.FindStringSubmatch(line); match != nil {
			entry = FirmwareMissing{File: match[2], Device: match[1]}
		} else if firmwareWords.MatchString(line) {
			file := firmwareFile.FindString(line)
			if file == "" {
				continue
			}
			entry.File = file
			if match := devicePrefix.FindStringSubmatch(line); match != nil {
				entry.Device = match[1]
			}
		} else {
			continue
		}
		if at, ok := seen[entry.File]; ok {
			if result[at].Device == "" {
				result[at].Device = entry.Device
			}
			continue
		}
		seen[entry.File] = len(result)
		result = append(result, entry)
	}
	return result
}

// pciNames maps "vvvv" and "vvvv:dddd" to names from pci.ids.
type pciNames map[string]string

// readPCIIDs keeps only the wanted entries of pci.ids.
func readPCIIDs(path string, wanted map[string]bool) pciNames {
	names := pciNames{}
	file, err := os.Open(path)
	if err != nil {
		return names
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	vendor := ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "C "):
			return names
		case line == "" || line[0] == '#' || strings.HasPrefix(line, "\t\t"):
		case line[0] != '\t' && len(line) > 6:
			vendor = strings.ToLower(line[:4])
			if wanted[vendor] {
				names[vendor] = strings.TrimSpace(line[4:])
			}
		case line[0] == '\t' && wanted[vendor] && len(line) > 7:
			if id := vendor + ":" + strings.ToLower(line[1:5]); wanted[id] {
				names[id] = strings.TrimSpace(line[5:])
			}
		}
	}
	return names
}

func (names pciNames) name(vendor, device string) string {
	product, maker := names[vendor+":"+device], names[vendor]
	switch {
	case product != "" && maker != "":
		// Some entries repeat the maker at the end ("... RAID Controller
		// Intel Corporation"); name it once.
		return maker + " " + strings.TrimSpace(strings.TrimSuffix(product, maker))
	case product != "":
		return product
	}
	return "PCI " + vendor + ":" + device
}
