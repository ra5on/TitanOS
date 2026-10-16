package vm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

// fakeRunner stands in for qemu-img and virsh.
type fakeRunner struct {
	commands []string
	fail     string
	pidDir   string
}

func (r *fakeRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, line)
	if r.fail != "" && strings.Contains(line, r.fail) {
		return "", errors.New("failed: " + line)
	}
	switch {
	case name == "qemu-img" && args[0] == "info":
		info, err := os.Stat(args[len(args)-1])
		if err != nil {
			return "", err
		}
		backing := ""
		if strings.Contains(args[len(args)-1], "overlay") {
			backing = `, "backing-filename": "/etc/shadow"`
		}
		return fmt.Sprintf(`{"virtual-size": %d, "actual-size": %d, "format": "%s"%s}`, 3*gib, info.Size(), args[3], backing), nil
	case name == "qemu-img" && args[0] == "convert":
		return "", os.WriteFile(args[len(args)-1], []byte("copy"), 0o600)
	case name == "qemu-img" && args[0] == "create":
		// The file is the next to last argument.
		return "", os.WriteFile(args[len(args)-2], []byte("disk"), 0o600)
	case name == "virsh" && args[1] == "create":
		// A started VM shows up with the process of this test.
		vm := strings.TrimSuffix(filepath.Base(args[3]), ".xml")
		if info, err := os.Stat(filepath.Dir(args[3])); err != nil || info.Mode().Perm() != 0o770 {
			return "", fmt.Errorf("the folder of the sockets is not open to QEMU: %v", err)
		}
		return "", os.WriteFile(filepath.Join(r.pidDir, vm+".pid"), []byte(fmt.Sprint(os.Getpid())), 0o644)
	case name == "virsh" && args[1] == "destroy":
		return "", os.Remove(filepath.Join(r.pidDir, args[2]+".pid"))
	}
	return "", nil
}

func newTestManager(t *testing.T) (*Manager, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	m := NewManager(config.Open(filepath.Join(root, "config")), os.Getgid())
	m.mountRoot = filepath.Join(root, "mnt")
	m.runDir = filepath.Join(root, "run")
	m.pidDir = filepath.Join(root, "pids")
	m.kvm = root // exists
	m.runGid, m.runUid = os.Getgid(), os.Getuid()
	runner := &fakeRunner{pidDir: m.pidDir}
	m.runner = runner
	for _, dir := range []string{m.pidDir, filepath.Join(m.mountRoot, "data", "media"), filepath.Join(m.mountRoot, "fast")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, runner
}

func TestCreateStartDelete(t *testing.T) {
	m, runner := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data", "fast"}
	iso := filepath.Join(m.mountRoot, "data", "media", "debian & co.iso")
	os.WriteFile(iso, []byte("iso"), 0o644)

	spec := VM{Name: "debian", Template: "linux", MemoryMiB: 2048, VCPUs: 1, ISO: iso, Description: `Test <"VM">`,
		Disks: []Disk{{Format: "qcow2", SizeBytes: 20 * gib, Volume: "fast"}, {Format: "raw", SizeBytes: 2 * gib, Volume: "data"}}}

	for name, change := range map[string]func(*VM){
		"bad name":         func(vm *VM) { vm.Name = "Debian VM" },
		"unknown template": func(vm *VM) { vm.Template = "beos" },
		"too little RAM":   func(vm *VM) { vm.MemoryMiB = 16 },
		"too many vCPUs":   func(vm *VM) { vm.VCPUs = 100000 },
		"nothing to boot":  func(vm *VM) { vm.Disks, vm.ISO = nil, "" },
		"tiny disk":        func(vm *VM) { vm.Disks = []Disk{{Format: "qcow2", SizeBytes: 5, Volume: "fast"}} },
		"odd format":       func(vm *VM) { vm.Disks = []Disk{{Format: "vmdk", SizeBytes: gib, Volume: "fast"}} },
		"volume not there": func(vm *VM) { vm.Disks = []Disk{{Format: "qcow2", SizeBytes: gib, Volume: "gone"}} },
		"ISO outside":      func(vm *VM) { vm.ISO = "/etc/hostname" },
		"ISO not there":    func(vm *VM) { vm.ISO = filepath.Join(m.mountRoot, "data", "nothing.iso") },
		"ISO with dots":    func(vm *VM) { vm.ISO = m.mountRoot + "/data/../../etc/hostname" },
	} {
		bad := spec
		bad.Disks = append([]Disk{}, spec.Disks...)
		change(&bad)
		if err := m.Create(ctx, bad, mounted); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if len(m.List(mounted)) != 0 || len(runner.commands) != 0 {
		t.Fatalf("a refused VM left traces: %v", runner.commands)
	}

	// A disk that cannot be made takes back what was made before it.
	runner.fail = "vdisk2.img"
	if err := m.Create(ctx, spec, mounted); err == nil {
		t.Fatal("create with a failing disk succeeded")
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "fast", "vms", "debian")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the folder of a VM that was not created is still there: %v", err)
	}
	runner.fail = ""

	if err := m.Create(ctx, spec, mounted); err != nil {
		t.Fatal(err)
	}
	if err := m.Create(ctx, spec, mounted); !errors.Is(err, ErrExists) {
		t.Errorf("duplicate: %v", err)
	}
	vm := m.List(mounted)[0]
	if vm.State != "stopped" || !vm.Available || vm.Volume != "fast" || vm.Disks[0].File != "vdisk1.qcow2" || vm.Disks[1].File != "vdisk2.img" ||
		len(vm.UUID) != 36 || !strings.HasPrefix(vm.MAC, "52:54:00:") {
		t.Errorf("created VM: %+v", vm)
	}
	raw := "qemu-img create -q -f raw -o preallocation=falloc " + filepath.Join(m.mountRoot, "data/vms/debian/vdisk2.img") + " 2147483648"
	if runner.commands[len(runner.commands)-1] != raw {
		t.Errorf("last command = %q", runner.commands[len(runner.commands)-1])
	}
	if !m.List([]string{"fast"})[0].Available == false {
		t.Error("a VM with a disk on a volume that is not mounted counts as available")
	}

	xml, err := m.XML("debian")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<domain type="kvm">`, `<name>debian</name>`, `<memory unit="MiB">2048</memory>`,
		`<description>Test &lt;&#34;VM&#34;&gt;</description>`,
		`<source file="` + filepath.Join(m.mountRoot, "fast/vms/debian/vdisk1.qcow2") + `">`, `<target dev="vda" bus="virtio">`, `<target dev="vdb" bus="virtio">`,
		`debian &amp; co.iso`, `<target dev="sda" bus="sata">`,
		`<nvram>` + filepath.Join(m.mountRoot, "fast/vms/debian/nvram.fd") + `</nvram>`,
		`<graphics type="vnc" socket="` + m.VNCSocket("debian") + `">`, `<interface type="user">`,
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("description for libvirt lacks %s\n%s", want, xml)
		}
	}

	// Starting needs the volumes, and the firmware its file.
	if err := m.Start(ctx, "debian", []string{"fast"}); !errors.Is(err, ErrUnknownVolume) {
		t.Errorf("start without a volume: %v", err)
	}
	if err := m.Action(ctx, "debian", "shutdown"); !errors.Is(err, ErrNotRunning) {
		t.Errorf("shutdown of a stopped VM: %v", err)
	}
	if err := m.Start(ctx, "debian", mounted); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	} else if err != nil {
		t.Skip("no OVMF firmware on this machine; the rest needs a started VM")
	}
	if err := m.Start(ctx, "debian", mounted); !errors.Is(err, ErrRunning) {
		t.Errorf("start twice: %v", err)
	}
	m.sample()
	m.sample()
	if vm := m.List(mounted)[0]; vm.State != "running" || vm.MemoryBytes == 0 || vm.Since.IsZero() {
		t.Errorf("running VM: %+v", vm)
	}
	change := Change{Description: "neu", MemoryMiB: 4096, VCPUs: 1, ISO: iso}
	if err := m.Update(ctx, "debian", change, mounted); !errors.Is(err, ErrRunning) {
		t.Errorf("more memory while running: %v", err)
	}
	if err := m.Delete("debian", false, ""); !errors.Is(err, ErrRunning) {
		t.Errorf("delete while running: %v", err)
	}
	if err := m.Action(ctx, "debian", "explode"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown action: %v", err)
	}
	if err := m.Action(ctx, "debian", "poweroff"); err != nil {
		t.Fatal(err)
	}
	if err := m.Update(ctx, "debian", change, mounted); err != nil {
		t.Fatal(err)
	}

	// The VM survives a restart of titand.
	again := NewManager(m.store, m.gid)
	again.mountRoot, again.runDir, again.pidDir, again.runner, again.runGid = m.mountRoot, m.runDir, m.pidDir, runner, m.runGid
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if vm := again.List(mounted)[0]; vm.MemoryMiB != 4096 || vm.Description != "neu" || vm.Disks[1].File != "vdisk2.img" {
		t.Errorf("after a restart: %+v", vm)
	}

	// Deleting keeps the disks unless asked otherwise, and then only with
	// the name as confirmation.
	disk := filepath.Join(m.mountRoot, "fast/vms/debian/vdisk1.qcow2")
	if err := again.Delete("debian", true, "nope"); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("deleting disks without confirmation: %v", err)
	}
	if _, err := os.Stat(disk); err != nil {
		t.Fatal("the disk is gone although the deletion was refused")
	}
	other := filepath.Join(m.mountRoot, "fast/vms/debian/notes.txt")
	os.WriteFile(other, []byte("mine"), 0o644)
	if err := again.Delete("debian", true, "debian"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(disk); !errors.Is(err, os.ErrNotExist) {
		t.Error("the disk is still there")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("deleting the VM removed a file TitanOS did not make")
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data/vms/debian")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the empty folder on the second volume stayed")
	}
	if len(again.List(mounted)) != 0 {
		t.Error("the VM is still listed")
	}
}

func TestMedia(t *testing.T) {
	m, _ := newTestManager(t)
	for _, file := range []string{"data/media/debian.ISO", "data/media/film.mkv", "data/.recycle/old.iso", "data/appdata/app/x.iso", "fast/isos/win.iso"} {
		path := filepath.Join(m.mountRoot, file)
		os.MkdirAll(filepath.Dir(path), 0o755)
		os.WriteFile(path, []byte("1234"), 0o644)
	}
	media := m.Media([]string{"data", "fast"})
	if len(media) != 2 || !strings.HasSuffix(media[0].Path, "data/media/debian.ISO") || !strings.HasSuffix(media[1].Path, "fast/isos/win.iso") || media[0].SizeBytes != 4 {
		t.Errorf("media = %+v", media)
	}
}

func TestReadStat(t *testing.T) {
	file := filepath.Join(t.TempDir(), "stat")
	os.WriteFile(file, []byte("4711 (qemu system) S 1 4711 4711 0 -1 4194560 100 0 0 0 250 50 0 0 20 0 5 0 123456 1000000 2000 18446744073709551615 1 1 0 0 0 0 0 0 0 0 0 0 17 3 0 0 0 0 0\n"), 0o644)
	if cpu, started := readStat(file); cpu != 300 || started != 123456 {
		t.Errorf("readStat = %d, %d", cpu, started)
	}
}

func TestPinningFoldersAndFirmware(t *testing.T) {
	m, runner := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data", "fast"}
	share := filepath.Join(m.mountRoot, "data", "media")

	spec := VM{Name: "win", Template: "windows", MemoryMiB: 4096, VCPUs: 7, CPUPin: []int{0}, Exclusive: true, TPM: true, SecureBoot: true,
		Folders: []Folder{{Path: share, Tag: "medien", ReadOnly: true}}, PCI: []string{"0000:01:00.0", "0000:01:00.1"}, USB: []string{"046d:c52b"},
		Disks: []Disk{{Format: "qcow2", SizeBytes: 64 * gib, Volume: "fast"}}}
	for name, change := range map[string]func(*VM){
		"CPU that does not exist": func(vm *VM) { vm.CPUPin = []int{100000} },
		"CPU twice":               func(vm *VM) { vm.CPUPin = []int{0, 0} },
		"folder outside":          func(vm *VM) { vm.Folders = []Folder{{Path: "/etc", Tag: "etc"}} },
		"folder that is a file": func(vm *VM) {
			vm.Folders = []Folder{{Path: filepath.Join(m.mountRoot, "data", "media", "x.iso"), Tag: "x"}}
		},
		"odd tag":         func(vm *VM) { vm.Folders = []Folder{{Path: share, Tag: "a b"}} },
		"same tag twice":  func(vm *VM) { vm.Folders = []Folder{{Path: share, Tag: "a"}, {Path: share, Tag: "a"}} },
		"odd PCI address": func(vm *VM) { vm.PCI = []string{"01:00.0; rm -rf /"} },
		"odd USB device":  func(vm *VM) { vm.USB = []string{"logitech"} },
	} {
		bad := spec
		change(&bad)
		if err := m.Create(ctx, bad, mounted); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if err := m.Create(ctx, spec, mounted); err != nil {
		t.Fatal(err)
	}
	vm := m.List(mounted)[0]
	if vm.VCPUs != 1 {
		t.Errorf("the number of vCPUs does not follow the pinned CPUs: %d", vm.VCPUs)
	}
	// On the bridge only once there is one.
	change := Change{Description: "", MemoryMiB: 4096, VCPUs: 1, CPUPin: []int{0}, Exclusive: true, Folders: vm.Folders, PCI: vm.PCI, USB: vm.USB, Network: "bridge"}
	if err := m.Update(ctx, "win", change, mounted); err != nil {
		t.Fatal(err)
	}
	m.sysDir = t.TempDir()
	if err := m.Start(ctx, "win", mounted); !errors.Is(err, ErrNoBridge) {
		t.Errorf("start on a bridge that is not there: %v", err)
	}
	os.MkdirAll(filepath.Join(m.sysDir, "class/net/br0"), 0o755)
	if bridged, _ := m.XML("win"); !strings.Contains(bridged, `<interface type="bridge">`) || !strings.Contains(bridged, `<source bridge="br0">`) {
		t.Errorf("no card on the bridge:\n%s", bridged)
	}
	xml, _ := m.XML("win")
	for _, want := range []string{
		`<vcpupin vcpu="0" cpuset="0">`, `<memoryBacking>`, `<access mode="shared">`,
		`<driver type="virtiofs">`, `<source dir="` + share + `">`, `<target dir="medien">`, `<readonly>`,
		`<tpm model="tpm-crb">`, `<backend type="emulator" version="2.0" persistent_state="yes">`,
		`secure="yes"`, `OVMF_CODE_4M.ms.fd`, `<smm state="on">`, `<clock offset="localtime">`,
		`<hostdev mode="subsystem" type="pci" managed="yes">`, `<address domain="0x0000" bus="0x01" slot="0x00" function="0x1">`,
		`<vendor id="0x046d">`, `<product id="0xc52b">`, `<target dev="sda" bus="sata">`,
		`<serial type="unix">`, `<source mode="bind" path="` + m.SerialSocket("win") + `">`,
	} {
		if !strings.Contains(xml, want) {
			t.Errorf("description for libvirt lacks %s", want)
		}
	}

	if err := m.Start(ctx, "win", mounted); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("no OVMF firmware with Secure Boot on this machine")
		}
		t.Fatal(err)
	}
	// The TPM keeps its state on the volume, closed to everyone else.
	state := filepath.Join(m.mountRoot, "fast/vms/win/tpm")
	if info, err := os.Stat(state); err != nil || info.Mode().Perm() != 0o700 || !strings.Contains(xml, `<source type="dir" path="`+state+`">`) || !strings.Contains(xml, `persistent_state="yes"`) {
		t.Errorf("TPM state: %v", err)
	}
	// While the VM runs, the rest of the NAS keeps off its CPUs.
	if runtime.NumCPU() > 1 {
		reserved := "systemctl set-property --runtime system.slice AllowedCPUs=" + m.reserved
		if m.reserved == "" || strings.HasPrefix(m.reserved, "0") || !slices.Contains(runner.commands, reserved) {
			t.Errorf("reserved = %q, commands %v", m.reserved, runner.commands)
		}
	}
	m.Action(ctx, "win", "poweroff")
	m.sample()
	if m.reserved != "" || (runtime.NumCPU() > 1 && runner.commands[len(runner.commands)-1] != "systemctl set-property --runtime init.scope AllowedCPUs=") {
		t.Errorf("the CPUs were not given back: %q, %v", m.reserved, runner.commands[len(runner.commands)-1])
	}
}

func TestExistingImagesAndWholeDisks(t *testing.T) {
	m, runner := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data", "fast"}
	image := filepath.Join(m.mountRoot, "data", "media", "alt.vmdk")
	direct := filepath.Join(m.mountRoot, "data", "media", "fertig.qcow2")
	overlay := filepath.Join(m.mountRoot, "data", "media", "overlay.qcow2")
	for _, file := range []string{image, direct, overlay, filepath.Join(m.mountRoot, "data", "media", "notes.txt")} {
		os.WriteFile(file, []byte("image"), 0o660)
	}

	images := m.Images(ctx, mounted)
	if len(images) != 2 || images[0].Format != "vmdk" || images[0].VirtualBytes != 3*gib || images[1].Format != "qcow2" {
		t.Fatalf("images = %+v", images)
	}

	// A whole disk: only one that is in no volume, by its stable name.
	m.byID = filepath.Join(t.TempDir(), "by-id")
	os.MkdirAll(m.byID, 0o755)
	devices := t.TempDir()
	for _, name := range []string{"sdb", "sdc"} {
		os.WriteFile(filepath.Join(devices, name), nil, 0o660)
	}
	os.Symlink(filepath.Join(devices, "sdb"), filepath.Join(m.byID, "ata-FREE_1"))
	os.Symlink(filepath.Join(devices, "sdb"), filepath.Join(m.byID, "wwn-0x5000"))
	os.Symlink(filepath.Join(devices, "sdc"), filepath.Join(m.byID, "ata-IN_A_VOLUME"))
	m.FreeDisks = func() []string { return []string{"sdb"} }
	m.runGid = os.Getgid()
	if disks := m.WholeDisks(); len(disks) != 1 || disks[0].Device != "sdb" || filepath.Base(disks[0].Path) != "ata-FREE_1" {
		t.Fatalf("whole disks = %+v", disks)
	}

	base := VM{Name: "alt", Template: "other", MemoryMiB: 1024, VCPUs: 1}
	for name, disks := range map[string][]Disk{
		"copy of something that is no image": {{Format: "qcow2", Volume: "fast", Source: filepath.Join(m.mountRoot, "data", "media", "notes.txt")}},
		"copy from outside the volumes":      {{Format: "qcow2", Volume: "fast", Source: "/etc/hostname"}},
		"image that builds on another file":  {{Path: overlay}},
		"vmdk without a copy":                {{Path: image}},
		"disk of a volume":                   {{Path: filepath.Join(m.byID, "ata-IN_A_VOLUME")}},
		"partition":                          {{Path: filepath.Join(m.byID, "ata-FREE_1-part1")}},
		"device by another name":             {{Path: "/dev/sda"}},
	} {
		bad := base
		bad.Disks = disks
		if err := m.Create(ctx, bad, mounted); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	base.Disks = []Disk{
		{Format: "qcow2", Volume: "fast", Source: image, SizeBytes: 8 * gib},
		{Path: direct},
		{Path: filepath.Join(m.byID, "ata-FREE_1")},
	}
	if err := m.Create(ctx, base, mounted); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(ctx, "alt", mounted); !errors.Is(err, ErrPreparing) && m.List(mounted)[0].Job != nil {
		t.Errorf("start while the disk is copied: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && m.List(mounted)[0].Job != nil {
		time.Sleep(10 * time.Millisecond)
	}
	vm := m.List(mounted)[0]
	copied := filepath.Join(m.mountRoot, "fast/vms/alt/vdisk1.qcow2")
	if vm.Job != nil || vm.Disks[0].Source != "" || vm.Disks[0].SizeBytes != 8*gib || vm.Disks[1].SizeBytes != 3*gib || vm.Disks[1].Format != "qcow2" || vm.Disks[2].Format != "raw" {
		t.Fatalf("prepared VM: %+v", vm)
	}
	if !slices.Contains(runner.commands, "qemu-img convert -f vmdk -O qcow2 "+image+" "+copied) || !slices.Contains(runner.commands, "qemu-img resize -q -f qcow2 "+copied+" 8589934592") {
		t.Errorf("commands = %v", runner.commands)
	}
	if images := m.Images(ctx, mounted); images[1].UsedBy != "alt" {
		t.Errorf("the image in use is not marked: %+v", images)
	}
	xml, _ := m.XML("alt")
	for _, want := range []string{`<source file="` + direct + `">`, `<disk type="block" device="disk">`, `<source dev="` + filepath.Join(m.byID, "ata-FREE_1") + `">`, `<target dev="sdc" bus="sata">`} {
		if !strings.Contains(xml, want) {
			t.Errorf("description for libvirt lacks %s", want)
		}
	}

	// The tiny fake conversion can finish before the Start call above. In that
	// valid case stop the fake guest before exercising offline disk operations.
	if m.pid("alt") != 0 {
		if err := m.Action(ctx, "alt", "poweroff"); err != nil {
			t.Fatal(err)
		}
	}
	// Disks grow, never shrink; whole disks do neither.
	if err := m.Grow(ctx, "alt", 0, 4*gib); !errors.Is(err, ErrInvalid) {
		t.Errorf("shrinking: %v", err)
	}
	if err := m.Grow(ctx, "alt", 2, 100*gib); !errors.Is(err, ErrNotFound) {
		t.Errorf("growing a whole disk: %v", err)
	}
	if err := m.Grow(ctx, "alt", 0, 16*gib); err != nil || m.List(mounted)[0].Disks[0].SizeBytes != 16*gib {
		t.Errorf("growing: %v", err)
	}

	// A disk that went into a volume in the meantime is not handed out.
	m.FreeDisks = func() []string { return nil }
	if err := m.Start(ctx, "alt", mounted); !errors.Is(err, ErrBadImage) {
		t.Errorf("start with a disk that is in use now: %v", err)
	}

	// Removing the VM with its disks leaves what was there before it.
	if err := m.Delete("alt", true, "alt"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(copied); !errors.Is(err, os.ErrNotExist) {
		t.Error("the copy is still there")
	}
	for _, kept := range []string{image, direct, filepath.Join(devices, "sdb")} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("%s was deleted with the VM", kept)
		}
	}
}

func TestCPUs(t *testing.T) {
	if got := parseCPUList("0-3,8,10-11\n"); !slices.Equal(got, []int{0, 1, 2, 3, 8, 10, 11}) {
		t.Errorf("parseCPUList = %v", got)
	}
	m, _ := newTestManager(t)
	sys := t.TempDir()
	os.MkdirAll(filepath.Join(sys, "devices/cpu_core"), 0o755)
	os.MkdirAll(filepath.Join(sys, "devices/cpu_atom"), 0o755)
	os.MkdirAll(filepath.Join(sys, "devices/system/cpu/cpu0/topology"), 0o755)
	os.WriteFile(filepath.Join(sys, "devices/cpu_core/cpus"), []byte("0\n"), 0o644)
	os.WriteFile(filepath.Join(sys, "devices/cpu_atom/cpus"), []byte("1-64\n"), 0o644)
	os.WriteFile(filepath.Join(sys, "devices/system/cpu/cpu0/topology/thread_siblings_list"), []byte("0-1\n"), 0o644)
	m.vms["a"] = VM{Name: "a", CPUPin: []int{0}}
	cpus := m.CPUs(sys)
	if len(cpus) != runtime.NumCPU() || cpus[0].Kind != "p" || !slices.Equal(cpus[0].Siblings, []int{0, 1}) || !slices.Equal(cpus[0].UsedBy, []string{"a"}) {
		t.Errorf("CPUs = %+v", cpus[0])
	}
	if len(cpus) > 1 && (cpus[1].Kind != "e" || len(cpus[1].UsedBy) != 0) {
		t.Errorf("CPU 1 = %+v", cpus[1])
	}
	// Not a hybrid CPU: no kinds.
	if plain := m.CPUs(t.TempDir()); plain[0].Kind != "" || !slices.Equal(plain[0].Siblings, []int{0}) {
		t.Errorf("plain CPU = %+v", plain[0])
	}
}
