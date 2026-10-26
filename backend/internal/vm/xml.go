package vm

import (
	"encoding/xml"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// node is an XML element put together by hand. encoding/xml takes care of
// the escaping; paths and descriptions may hold any character.
type node struct {
	XMLName  xml.Name
	Attrs    []xml.Attr `xml:",any,attr"`
	Text     string     `xml:",chardata"`
	Children []node     `xml:",any"`
}

// el makes an element; attrs are names and values in turn.
func el(name string, attrs ...string) node {
	n := node{XMLName: xml.Name{Local: name}}
	for i := 0; i+1 < len(attrs); i += 2 {
		n.Attrs = append(n.Attrs, xml.Attr{Name: xml.Name{Local: attrs[i]}, Value: attrs[i+1]})
	}
	return n
}

func (n node) with(children ...node) node {
	n.Children = append(n.Children, children...)
	return n
}

func (n node) text(value string) node {
	n.Text = value
	return n
}

const emulator = "/usr/bin/qemu-system-x86_64"

type firmwareFiles struct{ code, vars string }

// firmware returns the firmware of a VM and the template its settings
// start from. With Secure Boot it is the build that enforces signatures,
// with the keys of Microsoft enrolled, which is what Windows expects.
func firmware(vm VM) firmwareFiles {
	if vm.Firmware == "bios" {
		return firmwareFiles{"/usr/share/seabios/bios-256k.bin", ""}
	}
	if vm.SecureBoot {
		return firmwareFiles{"/usr/share/OVMF/OVMF_CODE_4M.ms.fd", "/usr/share/OVMF/OVMF_VARS_4M.ms.fd"}
	}
	return firmwareFiles{"/usr/share/OVMF/OVMF_CODE_4M.fd", "/usr/share/OVMF/OVMF_VARS_4M.fd"}
}

// cdromTarget is the name of the CD drive of a VM. On SATA it must not
// clash with the disks, which are on SATA too unless the VM has virtio.
func cdromTarget(vm VM) string {
	if vm.Template == "linux" {
		return "sda"
	}
	return "sd" + string(rune('a'+len(vm.Disks)))
}

// domainXML describes a VM to libvirt.
//
// Every VM is a q35 machine with the chosen firmware and the CPU of the host. What
// the guest gets as disk controller, network card and display depends on the
// template: Linux has drivers for the fast virtio devices, other systems
// get devices every system knows.
func (m *Manager) domainXML(vm VM) (string, error) {
	if err := validateFirmware(vm); err != nil {
		return "", err
	}
	virtio := vm.Template == "linux"
	diskBus, diskPrefix, nic, video := "sata", "sd", "e1000e", "vga"
	if virtio {
		diskBus, diskPrefix, nic, video = "virtio", "vd", "virtio", "virtio"
	}

	devices := el("devices").with(el("emulator").text(emulator))
	letter := 'a'
	for _, disk := range vm.Disks {
		kind, source := "file", el("source", "file", m.diskPath(vm, disk))
		if m.isBlock(disk) {
			kind, source = "block", el("source", "dev", disk.Path)
		}
		devices = devices.with(el("disk", "type", kind, "device", "disk").with(
			el("driver", "name", "qemu", "type", disk.Format, "discard", "unmap"),
			source,
			el("target", "dev", diskPrefix+string(letter), "bus", diskBus),
		))
		letter++
	}
	// The CD drive is always there, empty without a medium: a medium can
	// then be put in or taken out while the VM runs. Always on SATA, a
	// drive every system and every firmware knows.
	cdrom := el("disk", "type", "file", "device", "cdrom").with(el("driver", "name", "qemu", "type", "raw"))
	if vm.ISO != "" {
		cdrom = cdrom.with(el("source", "file", vm.ISO))
	}
	devices = devices.with(cdrom.with(el("target", "dev", cdromTarget(vm), "bus", "sata"), el("readonly")))
	if vm.VirtIOISO != "" {
		target := cdromTarget(vm)
		target = target[:len(target)-1] + string(rune(target[len(target)-1])+1)
		devices = devices.with(el("disk", "type", "file", "device", "cdrom").with(el("driver", "name", "qemu", "type", "raw"), el("source", "file", vm.VirtIOISO), el("target", "dev", target, "bus", "sata"), el("readonly")))
	}
	for _, folder := range vm.Folders {
		share := el("filesystem", "type", "mount", "accessmode", "passthrough").with(
			el("driver", "type", "virtiofs"),
			el("source", "dir", folder.Path),
			el("target", "dir", folder.Tag),
		)
		if folder.ReadOnly {
			share = share.with(el("readonly"))
		}
		devices = devices.with(share)
	}
	socket, serial := m.VNCSocket(vm.Name), m.SerialSocket(vm.Name)
	// Outgoing connections through the NAS, without an address of its own
	// in the network; or a port on the bridge of the NAS, where the VM is
	// a machine like any other in the network.
	card := el("interface", "type", "user").with(el("mac", "address", vm.MAC), el("model", "type", nic))
	if vm.Network == "bridge" {
		card = el("interface", "type", "bridge").with(el("source", "bridge", bridge), el("mac", "address", vm.MAC), el("model", "type", nic))
	}
	if strings.HasPrefix(vm.Network, "network:") {
		dev, ok := m.namedBridge(vm.Network)
		if !ok {
			return "", ErrNoBridge
		}
		card = el("interface", "type", "bridge").with(el("source", "bridge", dev), el("mac", "address", vm.MAC), el("model", "type", nic))
	}
	devices = devices.with(
		card,
		// The screen is reachable through a socket only titand can open,
		// never through a port in the network.
		el("graphics", "type", "vnc", "socket", socket).with(el("listen", "type", "socket", "socket", socket)),
		el("video").with(el("model", "type", video)),
		el("controller", "type", "usb", "model", "qemu-xhci"),
		// A tablet reports absolute positions: the pointer in the browser
		// and the one in the VM stay together.
		el("input", "type", "tablet", "bus", "usb"),
		// The serial port, for systems without a desktop; again a socket.
		el("serial", "type", "unix").with(el("source", "mode", "bind", "path", serial), el("target", "port", "0")),
		el("console", "type", "unix").with(el("source", "mode", "bind", "path", serial), el("target", "type", "serial", "port", "0")),
		// For the guest agent, if the guest has one: it tells the addresses
		// of the VM. A guest without it does not mind the channel.
		el("channel", "type", "unix").with(el("target", "type", "virtio", "name", "org.qemu.guest_agent.0")),
		el("rng", "model", "virtio").with(el("backend", "model", "random").text("/dev/urandom")),
		el("memballoon", "model", "virtio"),
	)
	if vm.TPM {
		// The state of the TPM lies in the VM's folder and is kept when the
		// VM stops. libvirt's own place for it is in RAM here; Windows
		// would find an empty TPM after every restart of the NAS.
		devices = devices.with(el("tpm", "model", "tpm-crb").with(
			el("backend", "type", "emulator", "version", "2.0", "persistent_state", "yes").with(
				el("source", "type", "dir", "path", filepath.Join(m.dir(vm.Volume, vm.folder()), "tpm")),
			),
		))
	}
	for _, address := range vm.PCI {
		// "managed": libvirt takes the device from its driver on the NAS
		// when the VM starts and gives it back when the VM stops.
		devices = devices.with(el("hostdev", "mode", "subsystem", "type", "pci", "managed", "yes").with(
			el("source").with(el("address", "domain", "0x"+address[0:4], "bus", "0x"+address[5:7], "slot", "0x"+address[8:10], "function", "0x"+address[11:12])),
		))
	}
	for _, id := range vm.USB {
		devices = devices.with(el("hostdev", "mode", "subsystem", "type", "usb", "managed", "yes").with(
			el("source").with(el("vendor", "id", "0x"+id[0:4]), el("product", "id", "0x"+id[5:9])),
		))
	}

	clock := "utc"
	if vm.Template == "windows" {
		clock = "localtime"
	}
	files := firmware(vm)
	loader := el("loader", "readonly", "yes", "type", "pflash").text(files.code)
	if vm.Firmware == "bios" {
		loader = el("loader", "readonly", "yes", "type", "rom").text(files.code)
	}
	features := el("features").with(el("acpi"), el("apic"))
	if vm.SecureBoot {
		loader = el("loader", "readonly", "yes", "secure", "yes", "type", "pflash").text(files.code)
		// The firmware protects its settings with the system management mode.
		features = features.with(el("smm", "state", "on"))
	}
	domain := el("domain", "type", "kvm").with(
		el("name").text(vm.Name),
		el("title").text(vm.Label()),
		el("uuid").text(vm.UUID),
		el("description").text(vm.Description),
		el("memory", "unit", "MiB").text(fmt.Sprint(vm.MemoryMiB)),
		el("vcpu", "placement", "static").text(fmt.Sprint(vm.VCPUs)),
	)
	if len(vm.Folders) > 0 {
		// virtiofs hands files over through memory both sides can see.
		domain = domain.with(el("memoryBacking").with(el("source", "type", "memfd"), el("access", "mode", "shared")))
	}
	if len(vm.CPUPin) > 0 {
		tune := el("cputune")
		pinned := map[int]bool{}
		for vcpu, cpu := range vm.CPUPin {
			tune = tune.with(el("vcpupin", "vcpu", fmt.Sprint(vcpu), "cpuset", fmt.Sprint(cpu)))
			pinned[cpu] = true
		}
		// What QEMU does besides running the guest (emulating devices,
		// disk I/O) stays off the guest's CPUs where there are others.
		var rest []string
		for cpu := 0; cpu < runtime.NumCPU(); cpu++ {
			if !pinned[cpu] {
				rest = append(rest, fmt.Sprint(cpu))
			}
		}
		if len(rest) > 0 {
			tune = tune.with(el("emulatorpin", "cpuset", strings.Join(rest, ",")))
		}
		domain = domain.with(tune)
	}
	osNode := el("os").with(el("type", "arch", "x86_64", "machine", "q35").text("hvm"), loader)
	if vm.Firmware != "bios" {
		osNode = osNode.with(el("nvram").text(filepath.Join(m.dir(vm.Volume, vm.folder()), "nvram.fd")))
	}
	// Empty disks fall through to the installation medium.
	osNode = osNode.with(el("boot", "dev", "hd"), el("boot", "dev", "cdrom"))
	domain = domain.with(osNode,
		features,
		el("cpu", "mode", "host-passthrough", "check", "none", "migratable", "off"),
		el("clock", "offset", clock),
		el("on_poweroff").text("destroy"),
		el("on_reboot").text("restart"),
		el("on_crash").text("destroy"),
		devices,
	)
	out, err := xml.MarshalIndent(domain, "", "  ")
	return string(out) + "\n", err
}
