package usb

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

const listing = `{"blockdevices":[
 {"name":"/dev/sda","type":"disk","size":4000000000000,"tran":"sata","model":"Big Disk","children":[{"name":"/dev/sda1","type":"part","size":1,"fstype":"ext4","uuid":"1111-sata","label":"data"}]},
 {"name":"/dev/sdb","type":"disk","size":16000000000,"tran":"usb","model":"Cruzer Blade ","children":[
   {"name":"/dev/sdb1","type":"part","size":15000000000,"fstype":"exfat","uuid":"AAAA-BBBB","label":"Fotos Urlaub"},
   {"name":"/dev/sdb2","type":"part","size":100,"fstype":"linux_raid_member","uuid":"raid"}]},
 {"name":"/dev/sdc","type":"disk","size":8000000000,"tran":"usb","model":"TitanOS Stick","children":[{"name":"/dev/sdc1","type":"part","size":8,"fstype":"vfat","uuid":"BOOT-0001","label":"TITANOS"}]},
 {"name":"/dev/sdd","type":"disk","size":500000000000,"tran":"usb","model":"Backup HDD","children":[{"name":"/dev/sdd1","type":"part","size":5,"fstype":"ntfs","uuid":"NTFS0000000000AA","label":""},{"name":"/dev/sdd2","type":"part","size":5,"fstype":"zfs_member","uuid":"zfs"}]},
 {"name":"/dev/sde","type":"disk","size":1000,"tran":"usb","model":"Reader","fstype":"ext4","uuid":"in-volume"}
]}`

type fake struct {
	t      *testing.T
	calls  []string
	mounts *string
	fail   string
}

func (f *fake) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, line)
	if f.fail != "" && strings.HasPrefix(line, f.fail) {
		return "", &run.CommandError{Command: line, Output: "target is busy", Err: errors.New("exit status 32")}
	}
	switch name {
	case "lsblk":
		return listing, nil
	case "mount":
		dir := args[len(args)-1]
		*f.mounts += args[len(args)-2] + " " + dir + " exfat rw 0 0\n"
	}
	return "", nil
}

func newTest(t *testing.T) (*Manager, *fake) {
	t.Helper()
	root := t.TempDir()
	mounts := ""
	f := &fake{t: t, mounts: &mounts}
	m := &Manager{runner: f, root: filepath.Join(root, "usb"), mountsFile: filepath.Join(root, "mounts")}
	m.Reserved = func(uuid string) bool { return uuid == "in-volume" }
	os.WriteFile(m.mountsFile, nil, 0o644)
	return m, f
}

func TestOnlyUSBDataDrives(t *testing.T) {
	m, _ := newTest(t)
	devices, err := m.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range devices {
		names = append(names, d.Name+":"+d.FSType)
	}
	// SATA disks, RAID members, the boot stick and reserved devices stay out.
	if got := strings.Join(names, ","); got != "Backup HDD:ntfs,Backup HDD:zfs_member,Fotos Urlaub:exfat" {
		t.Errorf("devices = %s", got)
	}
	for _, d := range devices {
		if d.FSType == "zfs_member" && d.Supported {
			t.Error("zfs is not mountable")
		}
	}
}

func TestMountAndEject(t *testing.T) {
	m, f := newTest(t)
	ctx := context.Background()
	if err := m.Mount(ctx, "AAAA-BBBB"); err != nil {
		t.Fatal(err)
	}
	mount := strings.Join(f.calls, "\n")
	if !strings.Contains(mount, "mount -t exfat -o nosuid,nodev,noexec /dev/sdb1 "+m.root+"/Fotos-Urlaub-aaaabb") {
		t.Errorf("mount command:\n%s", mount)
	}
	// Written by the fake mount into the mounts file.
	os.WriteFile(m.mountsFile, []byte(*f.mounts), 0o644)
	devices, _ := m.List(ctx)
	var photos Device
	for _, d := range devices {
		if d.ID == "AAAA-BBBB" {
			photos = d
		}
	}
	if !photos.Mounted || photos.Mount != "Fotos-Urlaub-aaaabb" {
		t.Fatalf("device = %+v", photos)
	}
	if dir, ok := m.Dir("Fotos-Urlaub-aaaabb"); !ok || dir != photos.Dir {
		t.Errorf("Dir = %q, %v", dir, ok)
	}
	if err := m.Mount(ctx, "AAAA-BBBB"); !errors.Is(err, ErrMounted) {
		t.Errorf("mounting twice: %v", err)
	}
	if err := m.Mount(ctx, "zfs"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("zfs: %v", err)
	}
	if err := m.Mount(ctx, "unplugged"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown: %v", err)
	}
	// Busy drives stay mounted and say so.
	f.fail = "umount"
	if err := m.Eject(ctx, "AAAA-BBBB"); !errors.Is(err, ErrBusy) {
		t.Errorf("busy: %v", err)
	}
	f.fail = ""
	if err := m.Eject(ctx, "AAAA-BBBB"); err != nil {
		t.Errorf("eject: %v", err)
	}
	if !strings.Contains(strings.Join(f.calls, "\n"), "sync") {
		t.Error("no sync before unmounting")
	}
	if err := m.Eject(ctx, "NTFS0000000000AA"); !errors.Is(err, ErrNotMounted) {
		t.Errorf("not mounted: %v", err)
	}
	m.Disabled = true
	if err := m.Mount(ctx, "AAAA-BBBB"); !errors.Is(err, ErrDisabled) {
		t.Errorf("development mode: %v", err)
	}
}

func TestPlugNotice(t *testing.T) {
	m, _ := newTest(t)
	var plugged []string
	m.OnPlug = func(d Device) { plugged = append(plugged, d.Name) }
	m.look(context.Background())
	if len(plugged) != 0 {
		t.Errorf("drives that were there at the start are not news: %v", plugged)
	}
	m.seen = map[string]bool{"AAAA-BBBB": true}
	m.look(context.Background())
	if len(plugged) != 2 {
		t.Errorf("plugged = %v", plugged)
	}
}

func TestBackupNeverWritesIntoUnverifiedMount(t *testing.T) {
	m, f := newTest(t)
	ctx := context.Background()
	work := func(*os.Root) error { t.Fatal("unverified mount was writable"); return nil }
	if err := m.WithMounted(ctx, "missing", work); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	m.Disabled = true
	if err := m.WithMounted(ctx, "AAAA-BBBB", work); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	m.Disabled = false
	if err := m.Mount(ctx, "AAAA-BBBB"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(m.mountsFile, []byte(*f.mounts), 0600)
	// A textual mount listing alone is not evidence that the directory is a
	// mounted filesystem. The callback must never receive this ordinary folder.
	if err := m.WithMounted(ctx, "AAAA-BBBB", work); err == nil {
		t.Fatal("ordinary directory accepted")
	}
	m.operation.Lock()
	if err := m.Eject(ctx, "AAAA-BBBB"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if err := m.Mount(ctx, "AAAA-BBBB"); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	m.operation.Unlock()
}
