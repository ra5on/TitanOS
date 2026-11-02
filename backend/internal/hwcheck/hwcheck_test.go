package hwcheck

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type fakeRunner struct {
	output string
	calls  int
}

func (f *fakeRunner) Run(_ context.Context, _ string, name string, _ ...string) (string, error) {
	f.calls++
	if name != "journalctl" {
		return "", os.ErrNotExist
	}
	return f.output, nil
}

// tree builds a sysfs/procfs fixture below a temporary directory.
type tree struct {
	t    *testing.T
	root string
}

func newTree(t *testing.T) *tree {
	return &tree{t: t, root: t.TempDir()}
}

func (tr *tree) file(path, content string) {
	tr.t.Helper()
	full := filepath.Join(tr.root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		tr.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content+"\n"), 0o644); err != nil {
		tr.t.Fatal(err)
	}
}

func (tr *tree) link(path, target string) {
	tr.t.Helper()
	full := filepath.Join(tr.root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		tr.t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(tr.root, target), full); err != nil {
		tr.t.Fatal(err)
	}
}

func (tr *tree) dir(path string) {
	if err := os.MkdirAll(filepath.Join(tr.root, path), 0o755); err != nil {
		tr.t.Fatal(err)
	}
}

// pci adds a PCI device; driver "" leaves it unbound.
func (tr *tree) pci(address, vendor, device, class, driver string) {
	dir := "sys/devices/pci0000:00/" + address
	tr.file(dir+"/vendor", "0x"+vendor)
	tr.file(dir+"/device", "0x"+device)
	tr.file(dir+"/class", "0x"+class)
	tr.file(dir+"/modalias", "pci:v0000"+strings.ToUpper(vendor)+"d0000"+strings.ToUpper(device)+"sv00001849sd00008125bc"+strings.ToUpper(class[:2])+"sc"+strings.ToUpper(class[2:4])+"i00")
	if driver != "" {
		tr.dir("sys/bus/pci/drivers/" + driver)
		tr.link(dir+"/driver", "sys/bus/pci/drivers/"+driver)
	}
	tr.link("sys/bus/pci/devices/"+address, dir)
}

func (tr *tree) checker() *Checker {
	tr.file("pci.ids", "10ec  Realtek Semiconductor Co., Ltd.\n\t8127  RTL8127 10GbE\n\t8168  RTL8111/8168\n8086  Intel Corporation\n\t2822  SATA Controller [RAID mode]\n1000  Broadcom / LSI\n\t005d  MegaRAID SAS-3 3108\nC 00  Unclassified device")
	return &Checker{Sys: filepath.Join(tr.root, "sys"), Proc: filepath.Join(tr.root, "proc"), Dev: filepath.Join(tr.root, "dev"), PCIIDs: filepath.Join(tr.root, "pci.ids"),
		Modules: filepath.Join(tr.root, "modules"), Modprobe: []string{filepath.Join(tr.root, "modprobe.d")}, Runner: &fakeRunner{}}
}

const cpuinfoOld = `processor	: 0
vendor_id	: GenuineIntel
model name	: Intel(R) Celeron(R) CPU  J1900  @ 1.99GHz
physical id	: 0
core id		: 0
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx rdtscp lm constant_tsc arch_perfmon pebs bts rep_good nopl xtopology nonstop_tsc aperfmperf pni pclmulqdq dtes64 monitor ds_cpl vmx est tm2 ssse3 cx16 xtpr pdcm sse4_1 sse4_2 movbe popcnt tsc_deadline_timer rdrand lahf_lm 3dnowprefetch epb pti tpr_shadow vnmi flexpriority ept vpid tsc_adjust smep erms dtherm ida arat

processor	: 1
vendor_id	: GenuineIntel
model name	: Intel(R) Celeron(R) CPU  J1900  @ 1.99GHz
physical id	: 0
core id		: 1
flags		: fpu vme
`

const cpuinfoNew = `processor	: 0
vendor_id	: AuthenticAMD
model name	: AMD Ryzen 7 5700G with Radeon Graphics
physical id	: 0
core id		: 0
flags		: fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush mmx fxsr sse sse2 ht syscall nx mmxext fxsr_opt pdpe1gb rdtscp lm constant_tsc rep_good nopl nonstop_tsc cpuid extd_apicid aperfmperf rapl pni pclmulqdq monitor ssse3 fma cx16 sse4_1 sse4_2 movbe popcnt aes xsave avx f16c rdrand lahf_lm cmp_legacy svm extapic cr8_legacy abm sse4a misalignsse 3dnowprefetch osvw ibs skinit wdt tce topoext perfctr_core perfctr_nb bpext perfctr_llc mwaitx cpb cat_l3 cdp_l3 hw_pstate ssbd mba ibrs ibpb stibp vmmcall fsgsbase bmi1 avx2 smep bmi2 erms invpcid cqm rdt_a rdseed adx smap clflushopt clwb sha_ni xsaveopt xsavec xgetbv1 xsaves
processor	: 1
physical id	: 0
core id		: 0
`

func TestParseCPUInfo(t *testing.T) {
	old := ParseCPUInfo(cpuinfoOld)
	if old.Vendor != "intel" || old.Threads != 2 || old.Cores != 2 || old.Level != 2 || old.Model != "Intel(R) Celeron(R) CPU J1900 @ 1.99GHz" {
		t.Errorf("old: %+v", old)
	}
	if !slices.Equal(old.Features, []string{"sse4_2", "vmx"}) {
		t.Errorf("old features: %v", old.Features)
	}
	modern := ParseCPUInfo(cpuinfoNew)
	if modern.Vendor != "amd" || modern.Level != 3 || modern.Threads != 2 || modern.Cores != 1 {
		t.Errorf("new: %+v", modern)
	}
	if !modern.Has("avx2") || !modern.Has("x86-64-v3") || modern.Has("x86-64-v4") || !modern.Has("svm") {
		t.Error("new: Has")
	}
	if missing := old.Missing([]string{"avx", "x86-64-v2", "x86-64-v3"}); !slices.Equal(missing, []string{"avx", "x86-64-v3"}) {
		t.Errorf("old missing: %v", missing)
	}
	if missing := modern.Missing([]string{"avx", "avx2", "x86-64-v3"}); len(missing) != 0 {
		t.Errorf("new missing: %v", missing)
	}
	// Without cpuinfo nothing is claimed missing.
	if missing := ParseCPUInfo("").Missing([]string{"avx"}); len(missing) != 0 {
		t.Errorf("unknown cpu: %v", missing)
	}
}

func TestNormalizeNeed(t *testing.T) {
	for in, want := range map[string]string{"AVX2": "avx2", "x86_64_v3": "x86-64-v3", "x86-64-v2": "x86-64-v2", "sse4.2": "sse4_2",
		"avx512": "avx512f", "x86-64-v9": "", "rm -rf": "", "": ""} {
		if got := NormalizeNeed(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

func TestComposeNeeds(t *testing.T) {
	cases := map[string][]string{
		"services:\n  db:\n    image: mongo:7.0\n":                      {"avx"},
		"services:\n  db:\n    image: docker.io/library/mongo\n":        {"avx"},
		"services:\n  db:\n    image: mongo:4.4.29\n":                   {},
		"services:\n  db:\n    image: bitnami/mongodb:6.0@sha256:abc\n": {"avx"},
		"services:\n  ml:\n    image: ghcr.io/immich-app/immich-machine-learning:release\n  server:\n    image: ghcr.io/immich-app/immich-server:release\n": {"avx"},
		"services:\n  nvr:\n    image: ghcr.io/blakeblackshear/frigate:stable\n":                                                                            {"avx"},
		"services:\n  web:\n    image: nginx\n":                                               {},
		"x-titanos:\n  cpu: [AVX2, x86_64_v3, bogus!]\nservices:\n  web:\n    image: nginx\n": {"avx2", "x86-64-v3"},
		"services:\n  web:\n    image: nginx\n    x-titanos:\n      cpu: [sse4.2]\n":          {"sse4_2"},
		"not yaml: [": {},
	}
	for compose, want := range cases {
		if got := ComposeNeeds(compose); !slices.Equal(got, want) {
			t.Errorf("%q: %v, want %v", compose, got, want)
		}
	}
}

func TestVirtualization(t *testing.T) {
	tr := newTree(t)
	tr.file("proc/cpuinfo", cpuinfoOld)
	c := tr.checker()
	runner := &fakeRunner{output: "x86/cpu: VMX (outside TXT) disabled by BIOS\n"}
	c.Runner = runner
	v := c.Virtualization(context.Background())
	if v.Vendor != "intel" || !v.CPUFlag || v.KVM || !v.DisabledByFirmware || v.IOMMU || v.IOMMUTable {
		t.Errorf("no kvm: %+v", v)
	}
	tr.file("dev/kvm", "")
	tr.dir("sys/kernel/iommu_groups/0")
	tr.file("sys/firmware/acpi/tables/DMAR", "")
	runner.calls = 0
	v = c.Virtualization(context.Background())
	if !v.KVM || v.DisabledByFirmware || !v.IOMMU || !v.IOMMUTable || runner.calls != 0 {
		t.Errorf("kvm: %+v, %d log reads", v, runner.calls)
	}
}

func TestUnbound(t *testing.T) {
	tr := newTree(t)
	tr.file("proc/sys/kernel/osrelease", "6.12.113+deb13-amd64")
	tr.pci("0000:00:00.0", "8086", "3e30", "060000", "")      // host bridge
	tr.pci("0000:00:17.0", "8086", "2822", "010400", "ahci")  // bound
	tr.pci("0000:03:00.0", "10ec", "8127", "020000", "")      // newer kernel
	tr.pci("0000:04:00.0", "8088", "0101", "020000", "")      // plugin
	tr.pci("0000:05:00.0", "1d6a", "07b1", "020000", "")      // nothing known
	tr.pci("0000:06:00.0", "10ec", "8168", "020000", "r8169") // bound
	// USB: one device without any driver, a hub, a device with one bound interface.
	tr.file("sys/devices/usb1/1-3/idVendor", "1a86")
	tr.file("sys/devices/usb1/1-3/idProduct", "7523")
	tr.file("sys/devices/usb1/1-3/product", "USB Serial")
	tr.file("sys/devices/usb1/1-3/1-3:1.0/modalias", "usb:v1A86p7523d0264dcFFdsc00dp00icFFisc01ip00in00")
	tr.file("sys/devices/usb1/1-3/1-3:1.0/bInterfaceClass", "ff")
	tr.link("sys/bus/usb/devices/1-3", "sys/devices/usb1/1-3")
	tr.link("sys/bus/usb/devices/1-3:1.0", "sys/devices/usb1/1-3/1-3:1.0")
	tr.file("sys/devices/usb1/1-4/idVendor", "046d")
	tr.file("sys/devices/usb1/1-4/1-4:1.0/modalias", "usb:v046DpC52Bd1211dc00dsc00dp00ic03isc01ip01in00")
	tr.file("sys/devices/usb1/1-4/1-4:1.1/modalias", "usb:v046DpC52Bd1211dc00dsc00dp00ic03isc01ip02in01")
	tr.dir("sys/bus/usb/drivers/usbhid")
	tr.link("sys/devices/usb1/1-4/1-4:1.1/driver", "sys/bus/usb/drivers/usbhid")
	tr.link("sys/bus/usb/devices/1-4:1.0", "sys/devices/usb1/1-4/1-4:1.0")
	tr.link("sys/bus/usb/devices/1-4:1.1", "sys/devices/usb1/1-4/1-4:1.1")
	c := tr.checker()
	var asked []string
	c.Plugins = func(aliases []string) map[string][]PluginHint {
		asked = aliases
		result := map[string][]PluginHint{}
		for _, alias := range aliases {
			if strings.HasPrefix(alias, "pci:v00008088d00000101") {
				result[alias] = []PluginHint{{Plugin: "wangxun-nic", Title: "Wangxun"}}
			}
		}
		return result
	}
	devices := c.Unbound(context.Background())
	hints := map[string]string{}
	for _, device := range devices {
		hints[device.Address] = device.Hint
	}
	want := map[string]string{"0000:00:00.0": "not_needed", "0000:03:00.0": "kernel", "0000:04:00.0": "plugin", "0000:05:00.0": "unsupported", "1-3": "unsupported"}
	if len(hints) != len(want) {
		t.Fatalf("devices: %+v", devices)
	}
	for address, hint := range want {
		if hints[address] != hint {
			t.Errorf("%s: %q, want %q", address, hints[address], hint)
		}
	}
	if devices[len(devices)-1].Hint != "not_needed" {
		t.Errorf("devices that need nothing come last: %+v", devices)
	}
	for _, device := range devices {
		switch device.Address {
		case "0000:03:00.0":
			if device.Kernel != "6.16" || device.Driver != "r8169" || device.Name != "Realtek Semiconductor Co., Ltd. RTL8127 10GbE" {
				t.Errorf("rtl8127: %+v", device)
			}
		case "0000:04:00.0":
			if len(device.Plugins) != 1 || device.Plugins[0].Plugin != "wangxun-nic" {
				t.Errorf("plugin: %+v", device)
			}
		case "1-3":
			if device.Name != "USB Serial" || device.ID != "1a86:7523" || device.Class != "ff" {
				t.Errorf("usb: %+v", device)
			}
		}
	}
	if len(asked) != 5 {
		t.Errorf("aliases asked: %v", asked)
	}
	// A kernel that drives it already: the hint does not apply.
	tr.file("proc/sys/kernel/osrelease", "6.16.3+bpo-amd64")
	for _, device := range c.Unbound(context.Background()) {
		if device.Address == "0000:03:00.0" && device.Hint != "unsupported" {
			t.Errorf("6.16: %+v", device)
		}
	}
}

func TestKernelAtLeast(t *testing.T) {
	for _, c := range []struct {
		release, version string
		want             bool
	}{{"6.12.113+deb13-amd64", "6.16", false}, {"6.16.3+bpo-amd64", "6.16", true}, {"6.17-rc1", "6.16", true}, {"7.0.1", "6.16", true}, {"", "6.16", false}} {
		if got := kernelAtLeast(c.release, c.version); got != c.want {
			t.Errorf("%s ≥ %s: %v", c.release, c.version, got)
		}
	}
}

func TestParseFirmwareMissing(t *testing.T) {
	log := strings.Join([]string{
		"r8169 0000:03:00.0: Direct firmware load for rtl_nic/rtl8125b-2.fw failed with error -2",
		"r8169 0000:03:00.0: firmware: failed to load rtl_nic/rtl8125b-2.fw (-2)",
		"firmware: failed to load iwlwifi-ty-a0-gf-a0-89.ucode (-2)",
		"bluetooth hci0: Direct firmware load for intel/ibt-0040-0041.sfi failed with error -2",
		"i915 0000:00:02.0: [drm] GuC firmware i915/kbl_guc_70.1.1.bin version 70.1.1",
		"amdgpu 0000:0a:00.0: amdgpu: firmware amdgpu/psp_13_0_0_sos.bin missing",
		"usb 1-2: some unrelated message",
	}, "\n")
	got := ParseFirmwareMissing(log)
	want := []FirmwareMissing{
		{File: "rtl_nic/rtl8125b-2.fw", Device: "r8169 0000:03:00.0"},
		{File: "iwlwifi-ty-a0-gf-a0-89.ucode"},
		{File: "intel/ibt-0040-0041.sfi", Device: "bluetooth hci0"},
		{File: "amdgpu/psp_13_0_0_sos.bin", Device: "amdgpu 0000:0a:00.0"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	if got := ParseFirmwareMissing(""); got == nil || len(got) != 0 {
		t.Errorf("empty: %+v", got)
	}
}

// disk adds a block device at a device path below sys/devices.
func (tr *tree) disk(name, devicePath, model string, rotational bool, zoned string) {
	dir := "sys/devices/" + devicePath + "/block/" + name
	tr.file(dir+"/size", "7814037168")
	tr.file(dir+"/removable", "0")
	tr.file(dir+"/device/model", model)
	rot := "0"
	if rotational {
		rot = "1"
	}
	tr.file(dir+"/queue/rotational", rot)
	tr.file(dir+"/queue/zoned", zoned)
	tr.link("sys/block/"+name, dir)
}

func TestDisks(t *testing.T) {
	tr := newTree(t)
	tr.disk("sda", "pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0", "WDC WD40EFAX-68J", true, "none")
	tr.disk("sdb", "pci0000:00/0000:00:17.0/ata2/host1/target1:0:0/1:0:0:0", "WDC WD40EFRX-68N", true, "none")
	tr.disk("sdc", "pci0000:00/0000:01:00.0/host2/port-2:0/end_device-2:0/target2:0:0/2:0:0:0", "ST4000NM0025", true, "none")
	tr.disk("sdd", "pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.0/host6/target6:0:0/6:0:0:0", "JMS578", true, "none")
	tr.file("sys/devices/pci0000:00/0000:00:14.0/usb2/2-1/idVendor", "152d")
	tr.file("sys/devices/pci0000:00/0000:00:14.0/usb2/2-1/idProduct", "0578")
	tr.dir("sys/bus/usb/drivers/uas")
	tr.link("sys/devices/pci0000:00/0000:00:14.0/usb2/2-1/2-1:1.0/driver", "sys/bus/usb/drivers/uas")
	tr.disk("sde", "pci0000:00/0000:00:17.0/ata3/host3/target3:0:0/3:0:0:0", "HGST HSH721414AL", true, "host-managed")
	tr.disk("nvme0n1", "pci0000:00/0000:02:00.0/nvme/nvme0", "Samsung SSD 980", false, "none")
	tr.disk("sdf", "pci0000:00/0000:00:14.0/usb2/2-2/2-2:1.0/host7/target7:0:0/7:0:0:0", "ASM1153", true, "none")
	tr.file("sys/devices/pci0000:00/0000:00:14.0/usb2/2-2/idVendor", "174c")
	tr.file("sys/devices/pci0000:00/0000:00:14.0/usb2/2-2/idProduct", "55aa")
	tr.dir("sys/bus/usb/drivers/usb-storage")
	tr.link("sys/devices/pci0000:00/0000:00:14.0/usb2/2-2/2-2:1.0/driver", "sys/bus/usb/drivers/usb-storage")
	tr.dir("sys/devices/virtual/block/loop0")
	tr.link("sys/block/loop0", "sys/devices/virtual/block/loop0")

	disks := tr.checker().Disks()
	byName := map[string]Disk{}
	for _, disk := range disks {
		byName[disk.Name] = disk
	}
	if len(disks) != 7 {
		t.Fatalf("disks: %+v", disks)
	}
	codes := func(disk Disk) []string {
		var list []string
		for _, hint := range disk.Hints {
			list = append(list, hint.Code)
		}
		return list
	}
	check := func(name, transport, smr, controller string, hints ...string) {
		t.Helper()
		disk := byName[name]
		if disk.Transport != transport || disk.SMR != smr || disk.Controller != controller || !slices.Equal(codes(disk), hints) {
			t.Errorf("%s: %+v", name, disk)
		}
	}
	check("sda", "sata", "known_model", "0000:00:17.0", "smr")
	check("sdb", "sata", "", "0000:00:17.0")
	check("sdc", "sas", "", "0000:01:00.0", "sas_standby")
	check("sdd", "usb", "", "0000:00:14.0", "uas_quirk")
	check("sde", "sata", "zoned", "0000:00:17.0", "smr")
	check("nvme0n1", "nvme", "", "0000:02:00.0")
	check("sdf", "usb", "", "0000:00:14.0", "uas_quirk_active")
	if hint := byName["sdd"].Hints[0]; hint.Setting != "usb-storage.quirks=152d:0578:u" || byName["sdd"].USBDriver != "uas" {
		t.Errorf("quirk: %+v", byName["sdd"])
	}
	if byName["sda"].SizeBytes != 7814037168*512 {
		t.Errorf("size: %d", byName["sda"].SizeBytes)
	}
}

func TestControllers(t *testing.T) {
	tr := newTree(t)
	tr.pci("0000:00:17.0", "8086", "2822", "010400", "ahci")
	tr.pci("0000:00:0e.0", "8086", "9a0b", "010400", "vmd")
	tr.pci("0000:01:00.0", "1000", "005d", "010400", "megaraid_sas")
	tr.pci("0000:02:00.0", "1000", "0097", "010700", "mpt3sas")
	tr.pci("0000:03:00.0", "1022", "7901", "010601", "ahci")
	tr.pci("0000:04:00.0", "10ec", "8168", "020000", "r8169")
	got := map[string]string{}
	for _, controller := range tr.checker().Controllers() {
		hint := ""
		if len(controller.Hints) > 0 {
			hint = controller.Hints[0].Code
		}
		got[controller.Address] = controller.Kind + " " + hint
	}
	want := map[string]string{"0000:00:17.0": "raid intel_raid_mode", "0000:00:0e.0": "raid ", "0000:01:00.0": "raid hw_raid",
		"0000:02:00.0": "sas ", "0000:03:00.0": "sata "}
	if len(got) != len(want) {
		t.Fatalf("controllers: %v", got)
	}
	for address, value := range want {
		if got[address] != value {
			t.Errorf("%s: %q, want %q", address, got[address], value)
		}
	}
}

func TestReport(t *testing.T) {
	tr := newTree(t)
	tr.file("proc/cpuinfo", cpuinfoNew)
	tr.file("proc/meminfo", "MemTotal:       32768000 kB\nMemFree: 1 kB")
	tr.file("proc/sys/kernel/osrelease", "6.12.113+deb13-amd64")
	tr.file("sys/devices/system/edac/mc/mc0/ce_count", "2")
	tr.file("sys/devices/system/edac/mc/mc0/ue_count", "0")
	tr.dir("sys/firmware/efi")
	tr.file("sys/class/dmi/id/board_vendor", "ASRock")
	tr.file("sys/class/dmi/id/board_name", "B550M-ITX/ac")
	tr.pci("0000:01:00.0", "1000", "005d", "010400", "megaraid_sas")
	tr.pci("0000:03:00.0", "10ec", "8127", "020000", "")
	tr.pci("0000:0a:00.0", "1002", "1638", "030000", "amdgpu")
	tr.pci("0000:05:00.0", "10ec", "8168", "020000", "r8169")
	tr.file("sys/devices/pci0000:00/0000:05:00.0/net/enp5s0/carrier", "1")
	tr.file("sys/devices/pci0000:00/0000:05:00.0/net/enp5s0/speed", "1000")
	tr.file("sys/devices/pci0000:00/0000:05:00.0/net/enp5s0/address", "aa:bb:cc:dd:ee:ff")
	tr.link("sys/devices/pci0000:00/0000:05:00.0/net/enp5s0/device", "sys/devices/pci0000:00/0000:05:00.0")
	tr.link("sys/class/net/enp5s0", "sys/devices/pci0000:00/0000:05:00.0/net/enp5s0")
	tr.dir("sys/devices/virtual/net/br0")
	tr.link("sys/class/net/br0", "sys/devices/virtual/net/br0")
	tr.file("sys/class/hwmon/hwmon0/name", "k10temp")
	tr.file("sys/class/hwmon/hwmon0/temp1_input", "41000")
	tr.file("sys/class/hwmon/hwmon1/name", "nct6798")
	tr.file("sys/class/hwmon/hwmon1/fan1_input", "800")
	tr.file("sys/class/hwmon/hwmon1/fan2_input", "0")
	tr.disk("sda", "pci0000:00/0000:00:17.0/ata1/host0/target0:0:0/0:0:0:0", "ST4000DM004-2CV1", true, "none")
	c := tr.checker()
	c.Runner = &fakeRunner{output: "r8169 0000:05:00.0: firmware: failed to load rtl_nic/rtl8168h-2.fw (-2)\n"}

	report := c.Report(context.Background())
	if report.Boot != "uefi" || report.Board != "ASRock B550M-ITX/ac" || report.Kernel != "6.12.113+deb13-amd64" {
		t.Errorf("head: %+v", report)
	}
	if report.Memory.TotalBytes != 32768000*1024 || !report.Memory.ECC || report.Memory.Corrected != 2 {
		t.Errorf("memory: %+v", report.Memory)
	}
	if len(report.Network) != 1 || report.Network[0].Driver != "r8169" || report.Network[0].SpeedMbs != 1000 || !report.Network[0].Link || report.Network[0].Bus != "pci" {
		t.Errorf("network: %+v", report.Network)
	}
	if len(report.Graphics) != 1 || report.Graphics[0].Driver != "amdgpu" {
		t.Errorf("graphics: %+v", report.Graphics)
	}
	if len(report.Sensors) != 2 || report.Sensors[0].Temps != 1 || report.Sensors[1].Fans != 2 {
		t.Errorf("sensors: %+v", report.Sensors)
	}
	var codes []string
	for _, finding := range report.Summary {
		codes = append(codes, finding.Code)
	}
	if !slices.Equal(codes, []string{"no_virtualization", "unbound_kernel", "firmware_missing", "hw_raid", "smr"}) {
		t.Errorf("summary: %v", codes)
	}
	text := report.Text("0.41.0")
	for _, want := range []string{"TitanOS hardware report", "Version: 0.41.0", "x86-64-v3", "hw_raid", "rtl_nic/rtl8168h-2.fw", "-> kernel from Linux 6.16 (r8169)", "ST4000DM004-2CV1", "hints: smr", "enp5s0  pci"} {
		if !strings.Contains(text, want) {
			t.Errorf("text lacks %q:\n%s", want, text)
		}
	}
	// The report is meant to be shared: no MAC addresses.
	if strings.Contains(text, "aa:bb:cc") {
		t.Error("MAC address in the text")
	}
}

// titan (Arrow Lake, 01.11.2026): vmd is in the kernel and matches the
// VMD controller but gives up on it; the SPI flash and the chipset sensor
// need no driver; a blacklisted module does not count as a driver.
func TestUnboundDriverFailed(t *testing.T) {
	tr := newTree(t)
	tr.file("proc/sys/kernel/osrelease", "6.12.113+deb13-amd64")
	tr.pci("0000:00:0e.0", "8086", "ad0b", "010400", "") // VMD
	tr.pci("0000:80:1f.5", "8086", "ae23", "0c8000", "") // SPI flash
	tr.pci("0000:00:14.2", "8086", "a3b1", "118000", "") // PCH thermal (thor)
	tr.pci("0000:80:14.5", "8086", "7f2f", "000000", "") // unknown
	tr.pci("0000:01:00.0", "10de", "2d04", "030000", "") // NVIDIA, nouveau blacklisted
	tr.file("modules/6.12.113+deb13-amd64/modules.alias", "# Aliases extracted from modules themselves.\n"+
		"alias pci:v00008086d0000AD0Bsv*sd*bc*sc*i* vmd\n"+
		"alias pci:v000010DEd*sv*sd*bc03sc*i* nouveau\n"+
		"alias acpi*:INT3400:* int3400_thermal\n"+
		"alias pci:v00008086d0000A3B1sv*sd*bc*sc*i* intel-pch-thermal-test")
	tr.file("modprobe.d/titanos-gpu.conf", "# keep nouveau away\nblacklist nouveau")
	c := tr.checker()
	runner := &fakeRunner{output: "pci 0000:00:0e.0: [8086:ad0b] type 00 class 0x010400 conventional PCI endpoint\n" +
		"vmd 0000:00:0e.0: Unknown Bus Offset Setting (3)\n" +
		"i915 0000:00:02.0: [drm] GT1: HuC: authenticated for all workloads"}
	c.Runner = runner
	byAddress := map[string]UnboundDevice{}
	for _, device := range c.Unbound(context.Background()) {
		byAddress[device.Address] = device
	}
	vmd := byAddress["0000:00:0e.0"]
	if vmd.Hint != "driver_failed" || vmd.Driver != "vmd" || vmd.Kernel != "7.3" ||
		!slices.Equal(vmd.Messages, []string{"vmd 0000:00:0e.0: Unknown Bus Offset Setting (3)"}) {
		t.Errorf("vmd: %+v", vmd)
	}
	// A matching module counts even in a class that needs no driver.
	if thermal := byAddress["0000:00:14.2"]; thermal.Hint != "driver_failed" || thermal.Driver != "intel_pch_thermal_test" || thermal.Kernel != "" {
		t.Errorf("thermal: %+v", thermal)
	}
	for address, hint := range map[string]string{"0000:80:1f.5": "not_needed", "0000:80:14.5": "unsupported", "0000:01:00.0": "unsupported"} {
		if byAddress[address].Hint != hint {
			t.Errorf("%s: %+v, want %s", address, byAddress[address], hint)
		}
	}
	if runner.calls != 1 {
		t.Errorf("kernel log read %d times", runner.calls)
	}

	// Without a failing driver the kernel log is not read; a kernel with
	// the fix drops the version hint.
	tr.file("proc/sys/kernel/osrelease", "7.3.1+bpo-amd64")
	runner.calls = 0
	for _, device := range c.Unbound(context.Background()) {
		if device.Address == "0000:00:0e.0" && device.Hint != "unsupported" {
			t.Errorf("7.3 without modules.alias: %+v", device)
		}
	}
	if runner.calls != 0 {
		t.Errorf("kernel log read without need: %d", runner.calls)
	}
	tr.file("modules/7.3.1+bpo-amd64/modules.alias", "alias pci:v00008086d0000AD0Bsv*sd*bc*sc*i* vmd")
	for _, device := range c.Unbound(context.Background()) {
		if device.Address == "0000:00:0e.0" && (device.Hint != "driver_failed" || device.Kernel != "") {
			t.Errorf("7.3: %+v", device)
		}
	}
}

func TestDriverMessages(t *testing.T) {
	log := "xhci_hcd 0000:00:14.0: can't setup: -110\npci 0000:00:14.0: enabling device\n" +
		"xhci_hcd 0000:00:14.0: init 0000:00:14.0 fail, -110\nxhci_hcd 0000:00:15.0: other device"
	// The module xhci_pci registers the driver xhci_hcd.
	got := driverMessages(log, "xhci_pci", "0000:00:14.0")
	if len(got) != 2 || !strings.HasPrefix(got[0], "xhci_hcd 0000:00:14.0: can't setup") {
		t.Errorf("fallback: %q", got)
	}
	if got := driverMessages(strings.Repeat("vmd 0000:00:0e.0: x\n", 5), "vmd", "0000:00:0e.0"); len(got) != 3 {
		t.Errorf("limit: %q", got)
	}
}

func TestPCINameWithoutRepeatedMaker(t *testing.T) {
	names := pciNames{"8086": "Intel Corporation", "8086:ad0b": "Volume Management Device NVMe RAID Controller Intel Corporation"}
	if got := names.name("8086", "ad0b"); got != "Intel Corporation Volume Management Device NVMe RAID Controller" {
		t.Errorf("name: %q", got)
	}
	if got := names.name("8086", "ffff"); got != "PCI 8086:ffff" {
		t.Errorf("unknown: %q", got)
	}
}
