package update

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withGrub gives the stick of e the GRUB files of a stick made by make-zip.sh.
func withGrub(t *testing.T, e *env, values map[string]string) string {
	t.Helper()
	path := filepath.Join(e.boot, grubEnvFile)
	os.MkdirAll(filepath.Dir(path), 0o755)
	data, err := grubEnv(values)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(path, data, 0o644)
	return path
}

func readEnv(t *testing.T, path string) map[string]string {
	t.Helper()
	env, err := readGrubEnv(path)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestGrubEnvironmentBlock(t *testing.T) {
	data, err := grubEnv(map[string]string{envPrefer: "a", envTrial: ""})
	if err != nil || len(data) != grubEnvBlock || !strings.HasPrefix(string(data), grubEnvHead+"titanos_prefer=a\ntitanos_trial=\n#") {
		t.Fatalf("%d %v %q", len(data), err, data[:60])
	}
	long, err := grubEnv(map[string]string{envOpts: strings.Repeat("x", 1500)})
	if err != nil || len(long) != 2*grubEnvBlock {
		t.Errorf("long value: %d %v", len(long), err)
	}
	for _, bad := range []string{"a\nb", `a\b`} {
		if _, err := grubEnv(map[string]string{envOpts: bad}); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "env"), data, 0o644)
	if env, err := readGrubEnv(filepath.Join(dir, "env")); err != nil || env[envPrefer] != "a" || len(env) != 2 {
		t.Errorf("read back: %v %v", env, err)
	}
	os.WriteFile(filepath.Join(dir, "other"), []byte("titanos_prefer=b\n"), 0o644)
	if _, err := readGrubEnv(filepath.Join(dir, "other")); err == nil {
		t.Error("a file without the header was read")
	}
}

func TestBootLoaderFromTheCommandLine(t *testing.T) {
	m := NewManager(nil, "1.0.0", "a")
	for cmdline, want := range map[string]string{
		"boot=live live-media-path=/titanos/a":                             "systemd-boot",
		"boot=live live-media-path=/titanos/a titanos.loader=grub-i386-pc": "grub-i386-pc",
		"boot=live titanos.loader=grub-i386-efi console=ttyS0":             "grub-i386-efi",
		"titanos.loader=something-else":                                    "systemd-boot",
	} {
		m.cmdline = func() string { return cmdline }
		if got := m.BootLoader(); got != want {
			t.Errorf("%q: %s", cmdline, got)
		}
	}
}

// An update on a stick with GRUB clears what GRUB counted for an earlier
// trial, writes the command line of the new slot for GRUB and keeps the
// running system as the slot GRUB starts without a trial.
func TestUpdateKeepsGrubInStep(t *testing.T) {
	e := newEnv(t)
	path := withGrub(t, e, map[string]string{envPrefer: "a", envTrial: "titanos-b+3.conf", envCount: "3"})
	e.publish(t, "1.1.0", ChannelStable, nil)
	e.install(t)
	env := readEnv(t, path)
	if env[envTrial] != "" || env[envCount] != "0" || env[envPrefer] != "a" {
		t.Errorf("after the update: %v", env)
	}
	boot := readEnv(t, filepath.Join(e.boot, "titanos/b", bootEnvName))
	if !strings.Contains(boot[envOpts], "live-media-path=/titanos/b") {
		t.Errorf("bootenv of b: %v", boot)
	}
	// GRUB starts b on trial (count 1), titand comes up there and writes the
	// try into the name, as systemd-boot would have done.
	os.WriteFile(path, mustEnv(t, map[string]string{envPrefer: "b", envTrial: "titanos-b+3.conf", envCount: "1"}), 0o644)
	started := e.manager(t, "1.1.0", "b", "boot-2")
	started.cmdline = func() string { return "live-media-path=/titanos/b titanos.loader=grub-i386-pc" }
	started.ReconcileBootLoader()
	if got := entries(e); got != "titanos-a.conf[titanos-2] titanos-b+2-1.conf[titanos-1]" {
		t.Errorf("entries: %s", got)
	}
	if env := readEnv(t, path); env[envTrial] != "" || env[envCount] != "0" || env[envPrefer] != "a" {
		t.Errorf("after the start: %v", env)
	}
	if !started.OnTrial() {
		t.Error("b is no longer on trial")
	}
	if err := started.Bless(); err != nil {
		t.Fatal(err)
	}
	if env := readEnv(t, path); env[envPrefer] != "b" {
		t.Errorf("after marking b good: %v", env)
	}
	// Once more: nothing left to write.
	before, _ := os.ReadFile(path)
	started.ReconcileBootLoader()
	if after, _ := os.ReadFile(path); string(before) != string(after) {
		t.Error("a second start changed the GRUB state")
	}
}

func mustEnv(t *testing.T, values map[string]string) []byte {
	t.Helper()
	data, err := grubEnv(values)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// GRUB gave up on b after three tries and started a: titand on a names the
// entry as given up on, so that it is shown as failed and a new trial of
// the same slot starts with all its tries.
func TestGrubGaveUpOnTheTrial(t *testing.T) {
	e := newEnv(t)
	path := withGrub(t, e, map[string]string{envPrefer: "a"})
	e.publish(t, "1.1.0", ChannelStable, nil)
	e.install(t)
	e.event(t) // update_available
	os.WriteFile(path, mustEnv(t, map[string]string{envPrefer: "b", envTrial: "titanos-b+3.conf", envCount: "3"}), 0o644)
	back := e.manager(t, "1.0.0", "a", "boot-4")
	back.cmdline = func() string { return "titanos.loader=grub-i386-pc" }
	back.ReconcileBootLoader()
	if got := entries(e); got != "titanos-a.conf[titanos-2] titanos-b+0-3.conf[titanos-1]" {
		t.Errorf("entries: %s", got)
	}
	if env := readEnv(t, path); env[envPrefer] != "a" || env[envTrial] != "" {
		t.Errorf("env: %v", env)
	}
	back.Confirm()
	if got := e.event(t); got != "update_failed:1.1.0" {
		t.Errorf("event: %q", got)
	}
	if next := nextSlot(back.readEntries()); next != "a" {
		t.Errorf("next: %s", next)
	}
}

// Started by systemd-boot, titand leaves GRUB's count alone; sticks
// without GRUB get no GRUB files.
func TestNoGrubWork(t *testing.T) {
	e := newEnv(t)
	path := withGrub(t, e, map[string]string{envPrefer: "a", envTrial: "titanos-a+3.conf", envCount: "1"})
	before, _ := os.ReadFile(path)
	e.m.cmdline = func() string { return "live-media-path=/titanos/a" }
	e.m.ReconcileBootLoader()
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Error("a start by systemd-boot changed the GRUB state")
	}

	plain := newEnv(t)
	plain.publish(t, "1.1.0", ChannelStable, nil)
	plain.install(t)
	for _, name := range []string{grubEnvFile, "titanos/b/" + bootEnvName} {
		if _, err := os.Stat(filepath.Join(plain.boot, name)); err == nil {
			t.Errorf("%s written on a stick without GRUB", name)
		}
	}
	// Updates never touch the boot loaders.
	for _, name := range []string{"EFI/BOOT/BOOTX64.EFI", "EFI/BOOT/BOOTIA32.EFI", "boot/grub/grub.cfg"} {
		if _, err := os.Stat(filepath.Join(plain.boot, name)); err == nil {
			t.Errorf("%s written by an update", name)
		}
	}
}

func TestKernelParameterReachesGrub(t *testing.T) {
	e := newEnv(t)
	withGrub(t, e, map[string]string{envPrefer: "a"})
	os.WriteFile(filepath.Join(e.boot, "loader/entries/titanos-a.conf"), []byte("title    TitanOS\nsort-key titanos-1\nlinux    /titanos/a/vmlinuz\noptions  boot=live live-media-path=/titanos/a\n"), 0o644)
	if _, err := e.m.SetKernelParam("i915.enable_guc", "2"); err != nil {
		t.Fatal(err)
	}
	boot := readEnv(t, filepath.Join(e.boot, "titanos/a", bootEnvName))
	if boot[envOpts] != "boot=live live-media-path=/titanos/a i915.enable_guc=2" {
		t.Errorf("bootenv: %v", boot)
	}
}
