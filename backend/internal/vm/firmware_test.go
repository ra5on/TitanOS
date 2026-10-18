package vm

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFirmwareCompatibilityAndXML(t *testing.T) {
	m, _ := newTestManager(t)
	for _, kind := range []string{"", "uefi", "bios"} {
		t.Run("firmware-"+kind, func(t *testing.T) {
			v := VM{Name: "guest", Firmware: kind, Volume: "data", Template: "linux", MemoryMiB: 512, VCPUs: 1}
			x, err := m.domainXML(v)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "bios" {
				if !strings.Contains(x, `type="rom"`) || !strings.Contains(x, "seabios/bios-256k.bin") || strings.Contains(x, "nvram") || strings.Contains(x, "pflash") {
					t.Fatal(x)
				}
			} else if !strings.Contains(x, "OVMF_CODE_4M.fd") || !strings.Contains(x, "nvram.fd") {
				t.Fatal(x)
			}
		})
	}
	for _, v := range []VM{{Firmware: "invalid"}, {Firmware: "bios", SecureBoot: true}} {
		if !errors.Is(validateFirmware(v), ErrInvalid) {
			t.Fatal("accepted invalid firmware", v)
		}
		if _, err := m.domainXML(v); !errors.Is(err, ErrInvalid) {
			t.Fatal("generated invalid XML", err)
		}
	}
}

func TestBIOSCreateStartReloadAndSnapshotRestore(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	v := VM{Name: "legacy", Firmware: "bios", Template: "linux", MemoryMiB: 512, VCPUs: 1,
		Disks: []Disk{{Volume: "data", Format: "raw", SizeBytes: gib}}}
	if err := m.Create(ctx, v, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	v, err := m.get(v.Name)
	if err != nil || v.Firmware != "bios" {
		t.Fatalf("reload: %+v %v", v, err)
	}
	if err := m.Start(ctx, v.Name, v.volumes()); err != nil {
		t.Fatal(err)
	}
	nvram := filepath.Join(m.dir(v.Volume, v.folder()), "nvram.fd")
	if _, err := os.Stat(nvram); !os.IsNotExist(err) {
		t.Fatalf("BIOS created NVRAM: %v", err)
	}
	if err := os.Remove(filepath.Join(m.pidDir, v.Name+".pid")); err != nil {
		t.Fatal(err)
	}
	snap := captureForTest(t, m, v, "Legacy BIOS")
	if len(snap.Files) != 1 || snap.VM.Firmware != "bios" {
		t.Fatalf("snapshot: %+v", snap)
	}
	disk := m.diskPath(v, v.Disks[0])
	if err := os.WriteFile(disk, []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.RestoreSnapshot(ctx, v.Name, snap.ID, v.Label(), false, v.volumes()); err != nil {
		t.Fatal(err)
	}
	status := waitSnapshot(t, m, v)
	if status.Job != nil || status.Pending {
		t.Fatalf("restore: %+v", status)
	}
	data, err := os.ReadFile(disk)
	if err != nil || string(data) != "disk" {
		t.Fatalf("restored disk: %q %v", data, err)
	}
	if _, err := os.Stat(nvram); !os.IsNotExist(err) {
		t.Fatalf("restore created NVRAM: %v", err)
	}
	if _, err := os.Stat(firmware(v).code); err == nil {
		identity, err := m.snapshotRuntime(ctx, v)
		if err != nil || identity == "" {
			t.Fatalf("runtime: %q %v", identity, err)
		}
	}
}
