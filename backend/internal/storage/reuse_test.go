package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func foreignArrayDevices() []blockDevice {
	array := blockDevice{Name: "/dev/md126", Type: "raid1", Size: 4 << 30, FSType: "ext4"}
	return []blockDevice{
		{Name: "/dev/sdx", Type: "disk", Serial: "X", Size: 8 << 30, Children: []blockDevice{{Name: "/dev/sdx1", Type: "part", FSType: "linux_raid_member", Children: []blockDevice{array}}}},
		{Name: "/dev/sdy", Type: "disk", Serial: "Y", Size: 8 << 30, Children: []blockDevice{{Name: "/dev/sdy1", Type: "part", FSType: "linux_raid_member", Children: []blockDevice{array}}}},
	}
}
func TestOldArrayPreflight(t *testing.T) {
	for _, kind := range []string{"all selected", "incomplete", "mounted", "lvm", "partition holder", "nested"} {
		t.Run(kind, func(t *testing.T) {
			m, runner, _ := newTestManager(t)
			devices := foreignArrayDevices()
			selected := []string{"sdx", "sdy"}
			switch kind {
			case "incomplete":
				selected = selected[:1]
			case "mounted":
				devices[0].Children[0].Children[0].Mountpoint = "/foreign"
			case "lvm":
				devices[1].Children[0].Children[0].Type = "lvm"
			case "partition holder":
				os.MkdirAll(filepath.Join(m.sysBlock, "sdx/sdx1/holders/dm-0"), 0755)
			case "nested":
				devices[0].Children[0].Children[0].Children = []blockDevice{{Name: "/dev/md125", Type: "raid0"}}
			}
			arrays, err := m.oldArrays(devices, selected)
			if kind == "all selected" {
				if err != nil || !reflect.DeepEqual(arrays, []string{"/dev/md126"}) {
					t.Fatal(arrays, err)
				}
			} else if err == nil {
				t.Fatal("unsafe selection accepted")
			}
			if len(runner.commands) != 0 {
				t.Fatal("preflight changed disks")
			}
		})
	}
}

type releaseRunner struct {
	devices    []blockDevice
	commands   []string
	stopped    bool
	fail, swap bool
}

func (r *releaseRunner) Run(_ context.Context, _ string, name string, args ...string) (string, error) {
	line := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, line)
	if name == "mdadm" && args[0] == "--stop" {
		if r.fail {
			return "", errors.New("busy")
		}
		r.stopped = true
		for i := range r.devices {
			r.devices[i].Children[0].Children = nil
		}
		if r.swap {
			r.devices[1].Serial = "REPLACEMENT"
		}
		return "", nil
	}
	if name == "lsblk" {
		b, _ := json.Marshal(map[string]any{"blockdevices": r.devices})
		return string(b), nil
	}
	return "", nil
}
func TestOldArrayReleaseRechecksIdentity(t *testing.T) {
	for _, kind := range []string{"ok", "busy", "swapped"} {
		t.Run(kind, func(t *testing.T) {
			m, _, _ := newTestManager(t)
			before := foreignArrayDevices()
			runner := &releaseRunner{devices: foreignArrayDevices(), fail: kind == "busy", swap: kind == "swapped"}
			m.runner = runner
			_, err := m.releaseOldArrays(context.Background(), before, []string{"sdx", "sdy"})
			if (err == nil) != (kind == "ok") {
				t.Fatal(kind, err)
			}
			if len(runner.commands) == 0 || runner.commands[0] != "mdadm --stop /dev/md126" {
				t.Fatal(runner.commands)
			}
			for _, cmd := range runner.commands {
				if strings.Contains(cmd, "wipefs") || strings.Contains(cmd, "sfdisk") {
					t.Fatal("release erased data", cmd)
				}
			}
		})
	}
}
func TestDiskIdentityStaleSelectionAndProtectedDisks(t *testing.T) {
	m, runner, _ := newTestManager(t)
	status, err := m.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var identity string
	for _, disk := range status.Disks {
		if disk.Name == "sdd" {
			identity = disk.Identity
		}
	}
	req := CreateRequest{Name: "reuse", Level: "raid0", Filesystem: "ext4", Disks: []string{"sdd"}, Erase: []string{"sdd"}, Identities: map[string]string{"sdd": identity}}
	if err := validateDiskIdentities(req, status); err != nil {
		t.Fatal(err)
	}
	req.Identities["sdd"] = "stale"
	if err := m.Create(context.Background(), req); !errors.Is(err, ErrDiskUnavailable) {
		t.Fatal(err)
	}
	if len(runner.commands) != 0 {
		t.Fatal("erased stale selection", runner.commands)
	}
	req.Identities = nil
	req.Disks = []string{"sdz"}
	req.Erase = req.Disks
	if err := m.Create(context.Background(), req); !errors.Is(err, ErrDiskUnavailable) {
		t.Fatal("accepted boot disk", err)
	}
	if len(runner.commands) != 0 {
		t.Fatal(runner.commands)
	}
}
