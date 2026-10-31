package hwcheck

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Hint is a note about a disk or controller; Setting is an exact value
// to set by hand (TitanOS never sets boot parameters itself).
type Hint struct {
	Code    string `json:"code"`
	Setting string `json:"setting,omitempty"`
}

// Disk is a block device as sysfs describes it.
type Disk struct {
	Name      string `json:"name"`
	Model     string `json:"model"`
	SizeBytes uint64 `json:"sizeBytes"`
	// Transport: sata, sas, nvme, usb, virtio, mmc or other.
	Transport  string `json:"transport"`
	Rotational bool   `json:"rotational"`
	Removable  bool   `json:"removable"`
	// Zoned is host-managed or host-aware for zoned (SMR) disks.
	Zoned string `json:"zoned,omitempty"`
	// SMR: zoned (the disk says so) or known_model (from the model table).
	SMR string `json:"smr,omitempty"`
	// Controller is the PCI address of the controller the disk hangs on.
	Controller string `json:"controller,omitempty"`
	// USB is vendor:product of the USB enclosure, USBDriver uas or usb-storage.
	USB       string `json:"usb,omitempty"`
	USBDriver string `json:"usbDriver,omitempty"`
	Hints     []Hint `json:"hints"`
}

// smrModels are drive-managed SMR models named by their makers (WD, Seagate
// and Toshiba lists of 2020 and later). The list is not complete; a disk
// not in it may still be SMR.
var smrModels = []string{
	// WD Red (EFAX, 2–6 TB), WD Blue 3.5" (EZAZ) and 2.5" (SPZX)
	"WD20EFAX", "WD30EFAX", "WD40EFAX", "WD60EFAX",
	"WD20EZAZ", "WD30EZAZ", "WD40EZAZ", "WD60EZAZ", "WD10SPZX", "WD20SPZX",
	// Seagate BarraCuda, Desktop HDD, laptop and Archive
	"ST2000DM008", "ST3000DM007", "ST4000DM004", "ST6000DM003", "ST8000DM004",
	"ST5000DM000", "ST500LM030", "ST1000LM048", "ST2000LM015", "ST3000LM024",
	"ST4000LM024", "ST5000LM000", "ST6000AS0002", "ST8000AS0002",
	// Toshiba P300, DT02, L200 and MQ04
	"HDWD240", "HDWD260", "DT02ABA400", "DT02ABA600", "HDWL110", "HDWL120",
	"MQ04ABF100", "MQ04ABD200",
}

// uasProblems are USB-SATA bridges with known UAS trouble (resets, hangs
// under load). The fix is to use usb-storage instead: the kernel
// parameter usb-storage.quirks=<vid>:<pid>:u, set by hand.
var uasProblems = map[string]string{
	"152d:0578": "JMicron JMS578",
	"152d:1561": "JMicron JMS561",
	"174c:55aa": "ASMedia ASM1051E/ASM1053E/ASM1153E",
	"0bc2:2312": "Seagate Expansion",
	"0bc2:3312": "Seagate Expansion",
	"0bc2:a013": "Seagate Backup Plus",
	"2109:0711": "VIA VL711",
	"2109:0715": "VIA VL715",
}

var (
	pciAddress   = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)
	usbInterface = regexp.MustCompile(`^\d+-[\d.]+:\d+\.\d+$`)
	diskName     = regexp.MustCompile(`^(sd[a-z]+|nvme\d+n\d+|vd[a-z]+|hd[a-z]+|mmcblk\d+)$`)
)

// SMRModel reports whether a model string names a known SMR disk.
func SMRModel(model string) bool {
	model = strings.ToUpper(strings.ReplaceAll(model, " ", ""))
	return slices.ContainsFunc(smrModels, func(known string) bool { return strings.Contains(model, known) })
}

// Disks lists the whole disks below /sys/block with SMR, controller and
// USB hints. Only sysfs attributes are read; no disk is woken.
func (c *Checker) Disks() []Disk {
	disks := []Disk{}
	entries, _ := os.ReadDir(filepath.Join(c.Sys, "block"))
	for _, entry := range entries {
		name := entry.Name()
		if !diskName.MatchString(name) {
			continue
		}
		dir := filepath.Join(c.Sys, "block", name)
		real, err := filepath.EvalSymlinks(dir)
		if err != nil {
			real = dir
		}
		sectors, _ := strconv.ParseUint(readTrim(filepath.Join(dir, "size")), 10, 64)
		disk := Disk{Name: name, SizeBytes: sectors * 512, Hints: []Hint{},
			Model:      strings.Join(strings.Fields(readTrim(filepath.Join(dir, "device/model"))), " "),
			Rotational: readTrim(filepath.Join(dir, "queue/rotational")) == "1",
			Removable:  readTrim(filepath.Join(dir, "removable")) == "1"}
		if zoned := readTrim(filepath.Join(dir, "queue/zoned")); zoned == "host-managed" || zoned == "host-aware" {
			disk.Zoned = zoned
		}
		parts := strings.Split(filepath.ToSlash(real), "/")
		for i, part := range parts {
			if pciAddress.MatchString(part) {
				disk.Controller = part
			}
			if usbInterface.MatchString(part) && disk.USB == "" && i > 0 {
				device := strings.Join(parts[:i], "/")
				disk.USB = readTrim(filepath.Join(device, "idVendor")) + ":" + readTrim(filepath.Join(device, "idProduct"))
				disk.USBDriver = driverOf(strings.Join(parts[:i+1], "/"))
			}
		}
		switch path := filepath.ToSlash(real); {
		case disk.USB != "":
			disk.Transport = "usb"
		case strings.HasPrefix(name, "nvme"):
			disk.Transport = "nvme"
		case strings.HasPrefix(name, "mmcblk"):
			disk.Transport = "mmc"
		case strings.Contains(path, "/virtio"):
			disk.Transport = "virtio"
		case strings.Contains(path, "/end_device-") || strings.Contains(path, "/sas_"):
			disk.Transport = "sas"
		case strings.Contains(path, "/ata"):
			disk.Transport = "sata"
		default:
			disk.Transport = "other"
		}
		switch {
		case disk.Zoned != "":
			disk.SMR = "zoned"
		case disk.Rotational && SMRModel(disk.Model):
			disk.SMR = "known_model"
		}
		if disk.SMR != "" {
			disk.Hints = append(disk.Hints, Hint{Code: "smr"})
		}
		if _, known := uasProblems[disk.USB]; known {
			if disk.USBDriver == "uas" {
				disk.Hints = append(disk.Hints, Hint{Code: "uas_quirk", Setting: "usb-storage.quirks=" + disk.USB + ":u"})
			} else if disk.USBDriver == "usb-storage" {
				disk.Hints = append(disk.Hints, Hint{Code: "uas_quirk_active"})
			}
		}
		if disk.Transport == "sas" && disk.Rotational {
			disk.Hints = append(disk.Hints, Hint{Code: "sas_standby"})
		}
		disks = append(disks, disk)
	}
	sort.Slice(disks, func(i, j int) bool { return disks[i].Name < disks[j].Name })
	return disks
}

// Controller is a PCI storage controller.
type Controller struct {
	Address string `json:"address"`
	ID      string `json:"id"`
	Name    string `json:"name"`
	// Kind: scsi, ide, raid, ata, sata, sas, nvme or other.
	Kind   string `json:"kind"`
	Driver string `json:"driver"`
	Hints  []Hint `json:"hints"`
}

var controllerKinds = map[string]string{"00": "scsi", "01": "ide", "04": "raid", "05": "ata", "06": "sata", "07": "sas", "08": "nvme"}

// raidDrivers are drivers of controllers that build arrays in hardware.
var raidDrivers = []string{"megaraid_sas", "hpsa", "aacraid", "smartpqi", "arcmsr", "3w-9xxx", "3w-sas", "3w-xxxx", "megaraid", "mptsas"}

func classifyController(class, vendor, driver string) (string, []Hint) {
	kind := controllerKinds[class[2:4]]
	if kind == "" {
		kind = "other"
	}
	hints := []Hint{}
	switch {
	case kind == "raid" && driver == "vmd":
		// Intel VMD: NVMe behind it work with Linux; nothing to do.
	case kind == "raid" && vendor == "8086":
		hints = append(hints, Hint{Code: "intel_raid_mode"})
	case kind == "raid" || slices.Contains(raidDrivers, driver):
		hints = append(hints, Hint{Code: "hw_raid"})
	}
	return kind, hints
}

// Controllers lists the PCI storage controllers (class 01).
func (c *Checker) Controllers() []Controller {
	list := []Controller{}
	root := filepath.Join(c.Sys, "bus/pci/devices")
	entries, _ := os.ReadDir(root)
	wanted := map[string]bool{}
	for _, entry := range entries {
		dir := filepath.Join(root, entry.Name())
		class := strings.TrimPrefix(readTrim(filepath.Join(dir, "class")), "0x")
		if len(class) < 4 || !strings.HasPrefix(class, "01") {
			continue
		}
		vendor := strings.TrimPrefix(readTrim(filepath.Join(dir, "vendor")), "0x")
		device := strings.TrimPrefix(readTrim(filepath.Join(dir, "device")), "0x")
		driver := driverOf(dir)
		kind, hints := classifyController(class, vendor, driver)
		list = append(list, Controller{Address: entry.Name(), ID: vendor + ":" + device, Kind: kind, Driver: driver, Hints: hints})
		wanted[vendor], wanted[vendor+":"+device] = true, true
	}
	names := readPCIIDs(c.PCIIDs, wanted)
	for i := range list {
		vendor, device, _ := strings.Cut(list[i].ID, ":")
		list[i].Name = names.name(vendor, device)
	}
	return list
}
