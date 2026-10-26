package hardware

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// client pretends a program with an open DRM file of the given device.
func client(t *testing.T, proc, pid, name, address string) {
	t.Helper()
	dir := filepath.Join(proc, pid, "fdinfo")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(proc, pid, "comm"), []byte(name+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "3"), []byte("pos:\t0\nflags:\t02\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "7"), []byte("drm-driver:\ti915\ndrm-pdev:\t"+address+"\ndrm-client-id:\t12\n"), 0o644)
}

// console pretends a virtual console of the kernel.
func console(t *testing.T, sys, name, kind, bind string) string {
	t.Helper()
	dir := filepath.Join(sys, "class/vtconsole", name)
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "name"), []byte(kind+"\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "bind"), []byte(bind+"\n"), 0o644)
	return filepath.Join(dir, "bind")
}

func bound(t *testing.T, file string) string {
	t.Helper()
	data, _ := os.ReadFile(file)
	return string(data)
}

func driver(t *testing.T, sys, address, name string) {
	t.Helper()
	target := filepath.Join(sys, "bus/pci/drivers", name)
	os.MkdirAll(target, 0o755)
	link := filepath.Join(sys, "bus/pci/devices", address, "driver")
	os.Remove(link)
	os.Symlink(target, link)
}

func TestHandoffRefusesDeviceInUse(t *testing.T) {
	sys, proc := t.TempDir(), t.TempDir()
	device(t, sys, "0000:00:02.0", "030000", "8086", 0, map[string]string{"boot_vga": "1"})
	driver(t, sys, "0000:00:02.0", "i915")
	device(t, sys, "0000:01:00.0", "030000", "1002", 1, nil)
	driver(t, sys, "0000:01:00.0", "amdgpu")
	client(t, proc, "100", "jellyfin", "0000:00:02.0")
	os.MkdirAll(filepath.Join(proc, "self"), 0o755) // not a process
	fb := console(t, sys, "vtcon1", "(M) frame buffer device", "1")
	m := &Manager{sys: sys, proc: proc}

	users, err := m.Users([]string{"0000:00:02.0"})
	if err != nil || !slices.Equal(users, []string{"jellyfin (100)"}) {
		t.Fatalf("users = %v, %v", users, err)
	}
	if _, err := m.PrepareHandoff([]string{"0000:00:02.0"}); !errors.Is(err, ErrInUse) {
		t.Fatalf("a device in use was handed over: %v", err)
	}
	if bound(t, fb) != "1\n" {
		t.Fatal("the console was detached although the handoff was refused")
	}
	// The other card is free and not the console: nothing to detach.
	if detached, err := m.PrepareHandoff([]string{"0000:01:00.0"}); err != nil || detached {
		t.Fatalf("free card: detached=%v err=%v", detached, err)
	}
}

func TestHandoffMovesConsoleOffPrimaryDisplay(t *testing.T) {
	sys, proc := t.TempDir(), t.TempDir()
	device(t, sys, "0000:00:02.0", "030000", "8086", 0, map[string]string{"boot_vga": "1"})
	driver(t, sys, "0000:00:02.0", "i915")
	dummy := console(t, sys, "vtcon0", "(S) dummy device", "1")
	fb := console(t, sys, "vtcon1", "(M) frame buffer device", "1")
	m := &Manager{sys: sys, proc: proc}

	detached, err := m.PrepareHandoff([]string{"0000:00:02.0"})
	if err != nil || !detached {
		t.Fatalf("detached=%v err=%v", detached, err)
	}
	if bound(t, fb) != "0" || bound(t, dummy) != "1\n" {
		t.Fatalf("framebuffer console %q, dummy %q", bound(t, fb), bound(t, dummy))
	}

	// While the VM has the display, the console stays away from it …
	driver(t, sys, "0000:00:02.0", "vfio-pci")
	m.RestoreConsole()
	if bound(t, fb) != "0" {
		t.Fatal("console came back while the VM still has the display")
	}
	// … and comes back once the NAS driver has it again.
	driver(t, sys, "0000:00:02.0", "i915")
	m.RestoreConsole()
	if bound(t, fb) != "1" {
		t.Fatalf("console not restored: %q", bound(t, fb))
	}
}
