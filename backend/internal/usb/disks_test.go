package usb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

// Real lsblk output of util-linux 2.41, shortened to the columns titand asks
// for. Older versions print "0"/"1" instead of booleans; both appear.
const disksListing = `{"blockdevices":[
 {"name":"/dev/sda","type":"disk","size":4000787030016,"tran":"sata","model":"WDC WD40EFRX","serial":"WD-1","rota":true,"rm":false,"ro":false,"disc-gran":0,"pttype":null,"children":[]},
 {"name":"/dev/nvme0n1","type":"disk","size":1000204886016,"tran":"nvme","model":"Samsung SSD 980","serial":"S6","rota":false,"rm":false,"ro":false,"disc-gran":512,"pttype":"gpt","ptuuid":"p-1"},
 {"name":"/dev/sdb","type":"disk","size":32010928128,"tran":"usb","model":"Ultra Fit","serial":"4C53","rota":"1","rm":"1","ro":"0","disc-gran":0,"pttype":"dos","children":[
   {"name":"/dev/sdb1","type":"part","size":32009879552,"fstype":"BitLocker","uuid":""}]},
 {"name":"/dev/sdc","type":"disk","size":2000398934016,"tran":"sata","model":"Old RAID","rota":true,"pttype":"gpt","children":[
   {"name":"/dev/sdc1","type":"part","size":2000397868544,"fstype":"linux_raid_member","uuid":"0f0e0d0c-0b0a-0908-0706-050403020100","label":"other:0"}]},
 {"name":"/dev/sdd","type":"disk","size":2000398934016,"tran":"sata","model":"NAS disk","rota":true,"pttype":"gpt","children":[
   {"name":"/dev/sdd1","type":"part","size":2000397868544,"fstype":"linux_raid_member","uuid":"in-volume"}]},
 {"name":"/dev/sde","type":"disk","size":2000398934016,"tran":"sata","model":"Assembled","rota":true,"children":[
   {"name":"/dev/sde1","type":"part","size":2000397868544,"fstype":"linux_raid_member","uuid":"foreign-active","children":[
     {"name":"/dev/md127","type":"raid1","size":2000396800000,"fstype":"ext4","uuid":"md-fs"}]}]},
 {"name":"/dev/mmcblk0","type":"disk","size":63864569856,"tran":"mmc","model":null,"rota":false,"rm":false,"disc-gran":4194304,"pttype":"dos","children":[
   {"name":"/dev/mmcblk0p1","type":"part","size":63863521280,"fstype":"exfat","uuid":"6A1F-0000","label":"CAMERA"}]},
 {"name":"/dev/sdf","type":"disk","size":0,"tran":"usb","model":"Card Reader","rm":true},
 {"name":"/dev/sdg","type":"disk","size":8053063680,"tran":"usb","model":"Locked SD","rm":true,"ro":true,"children":[
   {"name":"/dev/sdg1","type":"part","size":8052015104,"fstype":"vfat","uuid":"1111-2222"}]},
 {"name":"/dev/sdh","type":"disk","size":500107862016,"tran":"sata","model":"Elsewhere","children":[
   {"name":"/dev/sdh1","type":"part","size":500106813440,"fstype":"ext4","uuid":"else","mountpoint":"/srv/x"},
   {"name":"/dev/sdh2","type":"part","size":1000,"fstype":"swap","uuid":"swap","mountpoint":"[SWAP]"}]},
 {"name":"/dev/sdi","type":"disk","size":500107862016,"tran":"usb","model":"Ours","children":[
   {"name":"/dev/sdi1","type":"part","size":500106813440,"fstype":"exfat","uuid":"ours","mountpoint":"MOUNTED"}]},
 {"name":"/dev/vda","type":"disk","size":10737418240,"tran":"virtio"},
 {"name":"/dev/sdj","type":"disk","size":8000000000,"tran":"usb","model":"TitanOS Stick","children":[
   {"name":"/dev/sdj1","type":"part","size":8,"fstype":"vfat","uuid":"BOOT-0001","label":"TITANOS"}]},
 {"name":"/dev/sdk","type":"disk","size":500107862016,"tran":"sata","model":"VM disk"}
]}`

func newDisksTest(t *testing.T) (*Manager, *fake) {
	m, f := newTest(t)
	f.listing = strings.ReplaceAll(disksListing, "MOUNTED", filepath.Join(m.root, "Ours-ours"))
	os.WriteFile(m.mountsFile, []byte("/dev/sdi1 "+filepath.Join(m.root, "Ours-ours")+" exfat rw 0 0\n"), 0o644)
	m.Claimed = func() map[string]string { return map[string]string{"sdk": "Windows VM"} }
	return m, f
}

func TestDisksShowEmptyAndForeignDrives(t *testing.T) {
	m, _ := newDisksTest(t)
	disks, job, err := m.Disks(context.Background())
	if err != nil || job != nil {
		t.Fatal(err, job)
	}
	var got []string
	for _, d := range disks {
		got = append(got, filepath.Base(d.Device)+":"+d.Kind+":"+d.Reason)
	}
	// The NAS disk, the boot stick, the empty reader and virtio stay out.
	want := "mmcblk0:sd:,nvme0n1:nvme:,sda:hdd:,sdb:removable:,sdc:hdd:,sde:hdd:inuse,sdg:removable:readonly,sdh:ssd:inuse,sdi:ssd:mounted,sdk:ssd:claimed"
	if strings.Join(got, ",") != want {
		t.Fatalf("disks\n got %s\nwant %s", strings.Join(got, ","), want)
	}
	for _, d := range disks {
		switch filepath.Base(d.Device) {
		case "sda":
			if !d.Formattable || len(d.Partitions) != 0 || d.Table != "" {
				t.Errorf("new disk: %+v", d)
			}
		case "sdb":
			if !d.Formattable || d.Partitions[0].FSType != "BitLocker" {
				t.Errorf("BitLocker stick: %+v", d)
			}
		case "sdc":
			if !d.Formattable || d.Partitions[0].FSType != "linux_raid_member" {
				t.Errorf("foreign RAID leftover: %+v", d)
			}
		case "nvme0n1":
			if !d.discard || d.Table != "gpt" {
				t.Errorf("nvme: %+v", d)
			}
		case "sdk":
			if d.Claimed != "Windows VM" {
				t.Errorf("claimed: %+v", d)
			}
		}
	}
	// SD cards in a built-in reader are now offered for mounting too.
	if _, err := m.find(context.Background(), "6A1F-0000"); err != nil {
		t.Error("SD card:", err)
	}
}

// formatFake plays the system while a stick is formatted.
type formatFake struct {
	mu        sync.Mutex
	calls     []string
	stdin     map[string]string
	before    string
	after     string
	formatted bool
	fail      string
	mounts    string
}

func (f *formatFake) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.calls = append(f.calls, line)
	if stdin != "" {
		f.stdin[name] = stdin
	}
	if f.fail != "" && strings.HasPrefix(line, f.fail) {
		return "", errors.New("exit status 1")
	}
	switch {
	case name == "lsblk" && args[len(args)-1] == "/dev/sdb":
		return `{"blockdevices":[{"name":"/dev/sdb","type":"disk","size":32010928128,"tran":"usb","children":[{"name":"/dev/sdb1","type":"part","size":32009879552}]}]}`, nil
	case name == "lsblk" && f.formatted:
		return f.after, nil
	case name == "lsblk":
		return f.before, nil
	case strings.HasPrefix(name, "mkfs."):
		f.formatted = true
	}
	return "", nil
}

func (f *formatFake) commands() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if !strings.HasPrefix(c, "lsblk") {
			out = append(out, c)
		}
	}
	return out
}

const stickBefore = `{"blockdevices":[{"name":"/dev/sdb","type":"disk","size":32010928128,"tran":"usb","model":"Ultra Fit","rota":false,"rm":true,"disc-gran":512,"pttype":"gpt","children":[
 {"name":"/dev/sdb1","type":"part","size":300000000,"fstype":"vfat","uuid":"EFI0-0001","label":"EFI"},
 {"name":"/dev/sdb2","type":"part","size":31000000000,"fstype":"ext4","uuid":"old-linux"}]}]}`

const stickAfter = `{"blockdevices":[{"name":"/dev/sdb","type":"disk","size":32010928128,"tran":"usb","model":"Ultra Fit","rm":true,"pttype":"dos","children":[
 {"name":"/dev/sdb1","type":"part","size":32009879552,"fstype":"exfat","uuid":"ABCD-0001","label":"Fotos"}]}]}`

func newFormatTest(t *testing.T) (*Manager, *formatFake) {
	t.Helper()
	root := t.TempDir()
	f := &formatFake{stdin: map[string]string{}, before: stickBefore, after: stickAfter}
	m := &Manager{runner: f, root: filepath.Join(root, "usb"), mountsFile: filepath.Join(root, "mounts"), pause: 1}
	os.WriteFile(m.mountsFile, nil, 0o644)
	return m, f
}

func stickID(t *testing.T, m *Manager) string {
	t.Helper()
	disks, _, err := m.Disks(context.Background())
	if err != nil || len(disks) != 1 {
		t.Fatal(disks, err)
	}
	return disks[0].ID
}

func TestFormatStickWithExfat(t *testing.T) {
	m, f := newFormatTest(t)
	var ended []FormatJob
	m.OnFormatted = func(job FormatJob) { ended = append(ended, job) }
	id := stickID(t, m)
	if err := m.Format(context.Background(), id, FormatRequest{Filesystem: "exfat", Label: "Fotos", Mount: true}); err != nil {
		t.Fatal(err)
	}
	m.waitFormat()
	want := []string{
		"wipefs --all --force /dev/sdb2",
		"wipefs --all --force /dev/sdb1",
		"wipefs --all --force /dev/sdb",
		"blkdiscard /dev/sdb",
		"sfdisk --quiet --wipe always --wipe-partitions always /dev/sdb",
		"udevadm settle --timeout=10",
		"mkfs.exfat -L Fotos /dev/sdb1",
		"udevadm settle --timeout=10",
	}
	got := f.commands()
	if len(got) < len(want) || !slices.Equal(got[:len(want)], want) {
		t.Fatalf("commands\n%s", strings.Join(got, "\n"))
	}
	if !strings.HasPrefix(got[len(want)], "mount -t exfat -o nosuid,nodev,noexec,") {
		t.Errorf("not mounted afterwards: %v", got[len(want):])
	}
	// A 32 GB stick gets MBR, which TVs and car radios read.
	if f.stdin["sfdisk"] != "label: dos\n,,7\n" {
		t.Errorf("table %q", f.stdin["sfdisk"])
	}
	if len(ended) != 1 || ended[0].Phase != "done" || ended[0].ID != "ABCD-0001" || ended[0].Scheme != "mbr" {
		t.Fatalf("ended %+v", ended)
	}
	_, job, _ := m.Disks(context.Background())
	if job == nil || job.Running() || job.Phase != "done" {
		t.Errorf("job %+v", job)
	}
	// The operation lock is free again.
	if !m.operation.TryLock() {
		t.Fatal("still locked")
	}
	m.operation.Unlock()
}

func TestFormatLinuxFilesystemsUseGPTAndNoExtraTrim(t *testing.T) {
	m, f := newFormatTest(t)
	f.after = strings.ReplaceAll(stickAfter, `"exfat"`, `"ext4"`)
	if err := m.Format(context.Background(), stickID(t, m), FormatRequest{Filesystem: "ext4", Label: "backup"}); err != nil {
		t.Fatal(err)
	}
	m.waitFormat()
	calls := strings.Join(f.commands(), "\n")
	if strings.Contains(calls, "blkdiscard") || !strings.Contains(calls, "mkfs.ext4 -F -q -m 0 -L backup /dev/sdb1") || strings.Contains(calls, "mount ") {
		t.Fatal(calls)
	}
	if f.stdin["sfdisk"] != "label: gpt\n,,"+gptLinux+"\n" {
		t.Errorf("table %q", f.stdin["sfdisk"])
	}
}

func TestFormatFailureIsReported(t *testing.T) {
	m, f := newFormatTest(t)
	f.fail = "mkfs.vfat"
	var ended []FormatJob
	m.OnFormatted = func(job FormatJob) { ended = append(ended, job) }
	if err := m.Format(context.Background(), stickID(t, m), FormatRequest{Filesystem: "vfat", Label: "stick"}); err != nil {
		t.Fatal(err)
	}
	m.waitFormat()
	if len(ended) != 1 || ended[0].Phase != "failed" || ended[0].Error == "" || ended[0].Label != "STICK" {
		t.Fatalf("ended %+v", ended)
	}
	if !slices.Contains(f.commands(), "mkfs.vfat -F 32 -n STICK /dev/sdb1") {
		t.Error(f.commands())
	}
}

func TestFormatRefusals(t *testing.T) {
	m, f := newFormatTest(t)
	ctx := context.Background()
	id := stickID(t, m)
	if err := m.Format(ctx, "0000", FormatRequest{Filesystem: "exfat"}); !errors.Is(err, ErrNotFound) {
		t.Error("unknown id:", err)
	}
	m.Busy = func() bool { return true }
	if err := m.Format(ctx, id, FormatRequest{Filesystem: "exfat"}); !errors.Is(err, ErrBusy) {
		t.Error("storage busy:", err)
	}
	m.Busy = nil
	if err := m.Format(ctx, id, FormatRequest{Filesystem: "zfs"}); !errors.Is(err, ErrFormatInvalid) {
		t.Error("zfs:", err)
	}
	// The stick got another content since the list was read.
	f.before = strings.ReplaceAll(stickBefore, "old-linux", "new-content")
	if err := m.Format(ctx, id, FormatRequest{Filesystem: "exfat"}); !errors.Is(err, ErrNotFound) {
		t.Error("changed drive:", err)
	}
	f.before = strings.ReplaceAll(stickBefore, `"label":"EFI"}`, `"label":"EFI","mountpoint":"/boot/efi"}`)
	if err := m.Format(ctx, stickID(t, m), FormatRequest{Filesystem: "exfat"}); !errors.Is(err, ErrNotFormattable) {
		t.Error("mounted elsewhere:", err)
	}
	m.Disabled = true
	if err := m.Format(ctx, id, FormatRequest{Filesystem: "exfat"}); !errors.Is(err, ErrDisabled) {
		t.Error("development mode:", err)
	}
	if calls := f.commands(); len(calls) != 0 {
		t.Fatal("refused formats ran", calls)
	}
	if !m.operation.TryLock() {
		t.Fatal("refusal kept the lock")
	}
}

func TestCheckFormat(t *testing.T) {
	stick := Disk{Size: 32 * gib}
	big := Disk{Size: 4 * tib}
	tiny := Disk{Size: 100 * mib}
	for _, c := range []struct {
		disk   Disk
		req    FormatRequest
		scheme string
	}{
		{stick, FormatRequest{Filesystem: "exfat"}, "mbr"},
		{stick, FormatRequest{Filesystem: "exfat", Scheme: "gpt"}, "gpt"},
		{big, FormatRequest{Filesystem: "exfat"}, "gpt"},
		{big, FormatRequest{Filesystem: "ntfs", Label: "Sicherung 2026"}, "gpt"},
		{stick, FormatRequest{Filesystem: "ext4", Label: "daten"}, "gpt"},
		{stick, FormatRequest{Filesystem: "vfat", Label: "my stick"}, "mbr"},
		{stick, FormatRequest{Filesystem: "exfat", Label: "Ürlaub 2026 ÄÖ"}, "mbr"},
		{tiny, FormatRequest{Filesystem: "vfat"}, "mbr"},
	} {
		scheme, err := CheckFormat(c.disk, c.req)
		if err != nil || scheme != c.scheme {
			t.Errorf("%+v: %s %v", c.req, scheme, err)
		}
	}
	for _, c := range []struct {
		disk Disk
		req  FormatRequest
	}{
		{big, FormatRequest{Filesystem: "vfat"}},
		{big, FormatRequest{Filesystem: "exfat", Scheme: "mbr"}},
		{stick, FormatRequest{Filesystem: "exfat", Scheme: "apm"}},
		{stick, FormatRequest{Filesystem: "vfat", Label: "Fotos ä"}},
		{stick, FormatRequest{Filesystem: "vfat", Label: "TWELVE CHARS"}},
		{stick, FormatRequest{Filesystem: "exfat", Label: "sixteen chars ab"}},
		{stick, FormatRequest{Filesystem: "ntfs", Label: "a:b"}},
		{stick, FormatRequest{Filesystem: "ext4", Label: "line\nbreak"}},
		{tiny, FormatRequest{Filesystem: "xfs"}},
		{tiny, FormatRequest{Filesystem: "btrfs"}},
		{stick, FormatRequest{Filesystem: "zfs"}},
	} {
		if _, err := CheckFormat(c.disk, c.req); !errors.Is(err, ErrFormatInvalid) {
			t.Errorf("%+v accepted: %v", c.req, err)
		}
	}
}

func TestInstallImageStickIsReadAsAWhole(t *testing.T) {
	m, f := newTest(t)
	// A Debian netinst written with dd: the image and its partitions share
	// the UUID; the EFI partition has its own.
	f.listing = `{"blockdevices":[{"name":"/dev/sdb","type":"disk","size":16000000000,"tran":"usb","model":"Cruzer","fstype":"iso9660","uuid":"2026-09-06-10-17-32-00","label":"Debian 13.1.0 amd64 n","pttype":"dos","children":[
 {"name":"/dev/sdb1","type":"part","size":800000000,"fstype":"iso9660","uuid":"2026-09-06-10-17-32-00","label":"Debian 13.1.0 amd64 n"},
 {"name":"/dev/sdb2","type":"part","size":4000000,"fstype":"vfat","uuid":"F1E2-0001"}]}]}`
	devices, err := m.List(context.Background())
	if err != nil || len(devices) != 2 {
		t.Fatalf("%+v %v", devices, err)
	}
	if err := m.Mount(context.Background(), "2026-09-06-10-17-32-00"); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(f.calls, func(c string) bool {
		return strings.HasPrefix(c, "mount -t iso9660 -o nosuid,nodev,noexec,ro /dev/sdb ")
	}) {
		t.Fatal(f.calls)
	}
}

func TestMacDriveIsMountedReadOnly(t *testing.T) {
	m, f := newTest(t)
	f.listing = `{"blockdevices":[{"name":"/dev/sdb","type":"disk","size":16000000000,"tran":"usb","children":[
 {"name":"/dev/sdb1","type":"part","size":200000000,"fstype":"vfat","uuid":"67E3-17ED","label":"EFI"},
 {"name":"/dev/sdb2","type":"part","size":15000000000,"fstype":"hfsplus","uuid":"mac","label":"Time Machine"}]}]}`
	if err := m.Mount(context.Background(), "mac"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), "mount -t hfsplus -o nosuid,nodev,noexec,ro /dev/sdb2 ") {
		t.Fatal(f.calls)
	}
}

func TestCardReaderPollingAndMediaChange(t *testing.T) {
	m, _ := newTest(t)
	m.sysBlockDir = t.TempDir()
	write := func(disk, file, value string) {
		os.MkdirAll(filepath.Join(m.sysBlockDir, disk), 0o755)
		os.WriteFile(filepath.Join(m.sysBlockDir, disk, file), []byte(value+"\n"), 0o644)
	}
	write("sdb", "removable", "1")
	write("sdb", "events", "media_change eject_request")
	write("sdb", "events_poll_msecs", "-1")
	write("sdb", "size", "0")
	write("sdc", "removable", "1")
	write("sdc", "events", "media_change")
	write("sdc", "events_poll_msecs", "5000")
	write("sda", "removable", "0")
	write("sda", "events", "")
	write("sda", "events_poll_msecs", "-1")
	m.pollMedia()
	read := func(disk string) string {
		data, _ := os.ReadFile(filepath.Join(m.sysBlockDir, disk, "events_poll_msecs"))
		return strings.TrimSpace(string(data))
	}
	if read("sdb") != "2000" || read("sdc") != "5000" || read("sda") != "-1" {
		t.Fatalf("intervals %s %s %s", read("sdb"), read("sdc"), read("sda"))
	}
	empty := m.blockListing()
	write("sdb", "size", "62333952")
	if m.blockListing() == empty {
		t.Fatal("a card put into the reader is not noticed")
	}
}

func TestFlagAcceptsBothLsblkSpellings(t *testing.T) {
	for in, want := range map[string]bool{`true`: true, `"1"`: true, `1`: true, `false`: false, `"0"`: false, `null`: false} {
		var f flag
		if err := f.UnmarshalJSON([]byte(in)); err != nil || bool(f) != want {
			t.Errorf("%s: %v %v", in, f, err)
		}
	}
}

func TestSmallCardGetsFAT16PartitionType(t *testing.T) {
	m, f := newFormatTest(t)
	small := strings.ReplaceAll(stickBefore, `"size":32010928128,"tran"`, `"size":134217728,"tran"`)
	f.before = small
	if err := m.Format(context.Background(), stickID(t, m), FormatRequest{Filesystem: "vfat"}); err != nil {
		t.Fatal(err)
	}
	m.waitFormat()
	if f.stdin["sfdisk"] != "label: dos\n,,e\n" || !slices.Contains(f.commands(), "mkfs.vfat /dev/sdb1") {
		t.Fatal(f.stdin["sfdisk"], f.commands())
	}
}
