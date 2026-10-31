package hwcheck

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Memory is the RAM of the NAS.
type Memory struct {
	TotalBytes uint64 `json:"totalBytes"`
	// ECC: an EDAC memory controller is active.
	ECC bool `json:"ecc"`
	// Corrected and Uncorrected count the errors EDAC has seen.
	Corrected   int `json:"corrected"`
	Uncorrected int `json:"uncorrected"`
}

// NIC is a physical network interface. The MAC address is left out: the
// report is meant to be shared.
type NIC struct {
	Name     string `json:"name"`
	Driver   string `json:"driver"`
	Address  string `json:"address"`
	Bus      string `json:"bus"`
	SpeedMbs int    `json:"speedMbs"`
	Link     bool   `json:"link"`
}

// Device is a PCI device with its driver (graphics and accelerators).
type Device struct {
	Address string `json:"address"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	Driver  string `json:"driver"`
}

// Sensor is a hwmon chip.
type Sensor struct {
	Chip  string `json:"chip"`
	Temps int    `json:"temps"`
	Fans  int    `json:"fans"`
}

// Finding is one line of the summary; the web UI words it by Code.
type Finding struct {
	// Level is warn or info.
	Level   string `json:"level"`
	Code    string `json:"code"`
	Subject string `json:"subject,omitempty"`
}

// Report is the whole compatibility check.
type Report struct {
	Generated time.Time `json:"generated"`
	Kernel    string    `json:"kernel"`
	Board     string    `json:"board"`
	// Boot is uefi or bios.
	Boot            string            `json:"boot"`
	CPU             CPU               `json:"cpu"`
	Virtualization  Virtualization    `json:"virtualization"`
	Memory          Memory            `json:"memory"`
	Controllers     []Controller      `json:"controllers"`
	Disks           []Disk            `json:"disks"`
	Network         []NIC             `json:"network"`
	Graphics        []Device          `json:"graphics"`
	Sensors         []Sensor          `json:"sensors"`
	Unbound         []UnboundDevice   `json:"unbound"`
	FirmwareMissing []FirmwareMissing `json:"firmwareMissing"`
	Summary         []Finding         `json:"summary"`
}

// Report reads everything. It takes well under a second plus the kernel
// log; meant for a button, not for polling.
func (c *Checker) Report(ctx context.Context) Report {
	report := Report{Generated: time.Now().UTC(), Kernel: c.kernelRelease(), Boot: "bios",
		CPU: c.CPU(), Virtualization: c.Virtualization(ctx), Memory: c.memory(),
		Controllers: c.Controllers(), Disks: c.Disks(), Network: c.network(), Graphics: c.graphics(),
		Sensors: c.sensors(), Unbound: c.Unbound(), FirmwareMissing: ParseFirmwareMissing(c.kernelLog(ctx))}
	if _, err := os.Stat(filepath.Join(c.Sys, "firmware/efi")); err == nil {
		report.Boot = "uefi"
	}
	dmi := filepath.Join(c.Sys, "class/dmi/id")
	board := strings.TrimSpace(readTrim(filepath.Join(dmi, "board_vendor")) + " " + readTrim(filepath.Join(dmi, "board_name")))
	if product := strings.TrimSpace(readTrim(filepath.Join(dmi, "sys_vendor")) + " " + readTrim(filepath.Join(dmi, "product_name"))); product != "" && product != board {
		board = strings.TrimSpace(board + " (" + product + ")")
	}
	report.Board = board
	report.Summary = summarize(report)
	return report
}

func summarize(r Report) []Finding {
	findings := []Finding{}
	add := func(level, code, subject string) {
		findings = append(findings, Finding{Level: level, Code: code, Subject: subject})
	}
	v := r.Virtualization
	switch {
	case !v.CPUFlag || !v.KVM:
		add("warn", "no_virtualization", "")
	case !v.IOMMU:
		add("info", "no_iommu", "")
	}
	if r.CPU.Level > 0 && r.CPU.Level < 2 {
		add("warn", "old_cpu", "")
	}
	for _, device := range r.Unbound {
		if device.Hint != "not_needed" {
			add("warn", "unbound_"+device.Hint, device.Name)
		}
	}
	for _, firmware := range r.FirmwareMissing {
		add("warn", "firmware_missing", firmware.File)
	}
	for _, controller := range r.Controllers {
		for _, hint := range controller.Hints {
			add("warn", hint.Code, controller.Name)
		}
	}
	for _, disk := range r.Disks {
		for _, hint := range disk.Hints {
			if hint.Code == "smr" || hint.Code == "uas_quirk" {
				add("warn", hint.Code, disk.Name+" "+disk.Model)
			}
		}
	}
	if r.Memory.Uncorrected > 0 {
		add("warn", "memory_errors", "")
	}
	return findings
}

func (c *Checker) memory() Memory {
	var memory Memory
	if file, err := os.Open(filepath.Join(c.Proc, "meminfo")); err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			if value, ok := strings.CutPrefix(scanner.Text(), "MemTotal:"); ok {
				fields := strings.Fields(value)
				if len(fields) > 0 {
					kib, _ := strconv.ParseUint(fields[0], 10, 64)
					memory.TotalBytes = kib * 1024
				}
			}
		}
		file.Close()
	}
	controllers, _ := filepath.Glob(filepath.Join(c.Sys, "devices/system/edac/mc/mc[0-9]*"))
	memory.ECC = len(controllers) > 0
	for _, mc := range controllers {
		ce, _ := strconv.Atoi(readTrim(filepath.Join(mc, "ce_count")))
		ue, _ := strconv.Atoi(readTrim(filepath.Join(mc, "ue_count")))
		memory.Corrected += ce
		memory.Uncorrected += ue
	}
	return memory
}

func (c *Checker) network() []NIC {
	nics := []NIC{}
	entries, _ := os.ReadDir(filepath.Join(c.Sys, "class/net"))
	for _, entry := range entries {
		dir := filepath.Join(c.Sys, "class/net", entry.Name())
		device, err := filepath.EvalSymlinks(filepath.Join(dir, "device"))
		if err != nil {
			continue // virtual: bridges, bonds, veth, loopback
		}
		nic := NIC{Name: entry.Name(), Driver: driverOf(filepath.Join(dir, "device")), Address: filepath.Base(device), Bus: "other",
			Link: readTrim(filepath.Join(dir, "carrier")) == "1"}
		switch {
		case pciAddress.MatchString(nic.Address):
			nic.Bus = "pci"
		case usbInterface.MatchString(nic.Address):
			nic.Bus = "usb"
		}
		if speed, err := strconv.Atoi(readTrim(filepath.Join(dir, "speed"))); err == nil && speed > 0 {
			nic.SpeedMbs = speed
		}
		nics = append(nics, nic)
	}
	return nics
}

func (c *Checker) graphics() []Device {
	list := []Device{}
	root := filepath.Join(c.Sys, "bus/pci/devices")
	entries, _ := os.ReadDir(root)
	wanted := map[string]bool{}
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		class := strings.TrimPrefix(readTrim(filepath.Join(dir, "class")), "0x")
		// Display controllers and processing accelerators (NPUs).
		if !strings.HasPrefix(class, "03") && !strings.HasPrefix(class, "12") {
			continue
		}
		vendor := strings.TrimPrefix(readTrim(filepath.Join(dir, "vendor")), "0x")
		device := strings.TrimPrefix(readTrim(filepath.Join(dir, "device")), "0x")
		list = append(list, Device{Address: entry.Name(), ID: vendor + ":" + device, Driver: driverOf(dir)})
		wanted[vendor], wanted[vendor+":"+device] = true, true
	}
	names := readPCIIDs(c.PCIIDs, wanted)
	for i := range list {
		vendor, device, _ := strings.Cut(list[i].ID, ":")
		list[i].Name = names.name(vendor, device)
	}
	return list
}

func (c *Checker) sensors() []Sensor {
	list := []Sensor{}
	chips, _ := filepath.Glob(filepath.Join(c.Sys, "class/hwmon/hwmon*"))
	sort.Strings(chips)
	for _, chip := range chips {
		temps, _ := filepath.Glob(filepath.Join(chip, "temp*_input"))
		fans, _ := filepath.Glob(filepath.Join(chip, "fan*_input"))
		list = append(list, Sensor{Chip: readTrim(filepath.Join(chip, "name")), Temps: len(temps), Fans: len(fans)})
	}
	return list
}

// Text renders the report as plain text for a bug report or a forum.
func (r Report) Text(version string) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	yes := func(v bool) string {
		if v {
			return "yes"
		}
		return "no"
	}
	line("TitanOS hardware report")
	line("Version: %s, kernel %s, boot %s, created %s", version, r.Kernel, r.Boot, r.Generated.Format(time.RFC3339))
	if r.Board != "" {
		line("Board: %s", r.Board)
	}
	line("")
	line("Summary")
	if len(r.Summary) == 0 {
		line("  nothing to note")
	}
	for _, finding := range r.Summary {
		line("  [%s] %s %s", finding.Level, finding.Code, finding.Subject)
	}
	line("")
	line("CPU: %s (%s), %d cores, %d threads, x86-64-v%d", r.CPU.Model, r.CPU.Vendor, r.CPU.Cores, r.CPU.Threads, r.CPU.Level)
	line("  features: %s", strings.Join(r.CPU.Features, " "))
	v := r.Virtualization
	line("  virtualization: cpu flag %s, kvm %s, disabled by firmware %s, iommu %s, iommu table %s",
		yes(v.CPUFlag), yes(v.KVM), yes(v.DisabledByFirmware), yes(v.IOMMU), yes(v.IOMMUTable))
	line("Memory: %.1f GiB, ECC %s (corrected %d, uncorrected %d)", float64(r.Memory.TotalBytes)/(1<<30), yes(r.Memory.ECC), r.Memory.Corrected, r.Memory.Uncorrected)
	line("")
	line("Disk controllers")
	for _, controller := range r.Controllers {
		line("  %s  %s  %s  %s  driver %s%s", controller.Address, controller.ID, controller.Kind, controller.Name, dash(controller.Driver), hints(controller.Hints))
	}
	line("Disks")
	for _, disk := range r.Disks {
		usb := ""
		if disk.USB != "" {
			usb = fmt.Sprintf("  usb %s (%s)", disk.USB, dash(disk.USBDriver))
		}
		line("  %s  %s  %.0f GB  %s  rotational %s%s%s", disk.Name, disk.Model, float64(disk.SizeBytes)/1e9, disk.Transport, yes(disk.Rotational), usb, hints(disk.Hints))
	}
	line("Network")
	for _, nic := range r.Network {
		speed := "-"
		if nic.SpeedMbs > 0 {
			speed = fmt.Sprintf("%d Mbit/s", nic.SpeedMbs)
		}
		line("  %s  %s  %s  driver %s  link %s  %s", nic.Name, nic.Bus, nic.Address, dash(nic.Driver), yes(nic.Link), speed)
	}
	line("Graphics and accelerators")
	for _, device := range r.Graphics {
		line("  %s  %s  %s  driver %s", device.Address, device.ID, device.Name, dash(device.Driver))
	}
	line("Sensors")
	for _, sensor := range r.Sensors {
		line("  %s  %d temperatures, %d fans", sensor.Chip, sensor.Temps, sensor.Fans)
	}
	line("Devices without a driver")
	for _, device := range r.Unbound {
		extra := ""
		switch device.Hint {
		case "plugin":
			var names []string
			for _, plugin := range device.Plugins {
				names = append(names, plugin.Plugin)
			}
			extra = " plugin " + strings.Join(names, ",")
		case "kernel":
			extra = " from Linux " + device.Kernel + " (" + device.Driver + ")"
		}
		line("  %s %s  %s  class %s  %s  -> %s%s", device.Bus, device.Address, device.ID, device.Class, device.Name, device.Hint, extra)
	}
	line("Missing firmware")
	for _, firmware := range r.FirmwareMissing {
		line("  %s  %s", firmware.File, firmware.Device)
	}
	return b.String()
}

func dash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func hints(list []Hint) string {
	var parts []string
	for _, hint := range list {
		if hint.Setting != "" {
			parts = append(parts, hint.Code+" ("+hint.Setting+")")
		} else {
			parts = append(parts, hint.Code)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "  hints: " + strings.Join(parts, ", ")
}
