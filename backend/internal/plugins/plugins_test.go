package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type fakeRunner struct{ commands []string }

func (r *fakeRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	return "", nil
}

func newTestManager(t *testing.T) (*Manager, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	m := NewManager(config.Open(filepath.Join(root, "config")))
	m.mountRoot, m.extDir, m.keyDir, m.kernel = filepath.Join(root, "mnt"), filepath.Join(root, "extensions"), filepath.Join(root, "keys"), "6.12.0-test"
	runner := &fakeRunner{}
	m.runner = runner
	os.MkdirAll(filepath.Join(m.mountRoot, "data", "media"), 0o755)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, runner
}

// plugin puts a plugin into a folder, as someone would upload it.
func plugin(t *testing.T, dir, name string, change func(*Manifest)) string {
	t.Helper()
	image := []byte("squashfs of " + name)
	sum := sha256.Sum256(image)
	manifest := Manifest{Name: name, Title: "Plugin " + name, Version: "1.0", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(image))}
	if change != nil {
		change(&manifest)
	}
	os.WriteFile(filepath.Join(dir, name+".raw"), image, 0o644)
	data, _ := json.Marshal(manifest)
	path := filepath.Join(dir, name+manifestSuffix)
	os.WriteFile(path, data, 0o644)
	return path
}

func TestInstallEnableRemove(t *testing.T) {
	m, runner := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data"}
	share := filepath.Join(m.mountRoot, "data", "media")
	path := plugin(t, share, "werkzeug", func(manifest *Manifest) {
		manifest.Modules, manifest.Services = []string{"i915"}, []string{"werkzeug.service"}
	})
	plugin(t, share, "anderer-kernel", func(manifest *Manifest) { manifest.Kernel = "5.10.0-old" })

	status := m.Status(mounted)
	if len(status.Plugins) != 2 || status.Plugins[1].Installed || status.Plugins[1].Path != path || status.Plugins[1].Official {
		t.Fatalf("found = %+v", status.Plugins)
	}

	// Nothing vouches for this plugin; it needs its name as confirmation.
	if err := m.Install(ctx, path, "data", "", mounted); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("install without confirmation: %v", err)
	}
	if err := m.Install(ctx, path, "gone", "werkzeug", mounted); !errors.Is(err, ErrUnknownVolume) {
		t.Errorf("install on a volume that is not there: %v", err)
	}
	if err := m.Install(ctx, "/etc/hostname", "data", "werkzeug", mounted); !errors.Is(err, ErrNotFound) {
		t.Errorf("install from outside the volumes: %v", err)
	}
	for name, change := range map[string]func(*Manifest){
		"image that does not match": func(manifest *Manifest) { manifest.SHA256 = strings.Repeat("0", 64) },
		"odd module name":           func(manifest *Manifest) { manifest.Modules = []string{"x; reboot"} },
		"name that is a path":       func(manifest *Manifest) { manifest.Name = "../x" },
	} {
		bad := plugin(t, t.TempDir(), "kaputt", change)
		target := filepath.Join(share, "kaputt"+manifestSuffix)
		os.Rename(bad, target)
		os.Rename(filepath.Join(filepath.Dir(bad), "kaputt.raw"), filepath.Join(share, "kaputt.raw"))
		if err := m.Install(ctx, target, "data", "kaputt", mounted); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
		os.Remove(target)
		os.Remove(filepath.Join(share, "kaputt.raw"))
	}

	if err := m.Install(ctx, path, "data", "werkzeug", mounted); err != nil {
		t.Fatal(err)
	}
	if err := m.Install(ctx, path, "data", "werkzeug", mounted); !errors.Is(err, ErrExists) {
		t.Errorf("install twice: %v", err)
	}
	link := filepath.Join(m.extDir, "werkzeug.raw")
	if target, err := os.Readlink(link); err != nil || target != filepath.Join(m.mountRoot, "data/.titanos/plugins/werkzeug.raw") {
		t.Fatalf("extension link: %q, %v", target, err)
	}
	want := []string{"systemd-sysext refresh", "depmod -a", "modprobe i915", "systemctl daemon-reload", "systemctl start werkzeug.service"}
	if !slices.Equal(runner.commands, want) {
		t.Errorf("commands = %v", runner.commands)
	}
	installed := m.Status(mounted).Plugins
	if len(installed) != 2 || !installed[1].Installed || !installed[1].Enabled || !installed[1].Active || installed[1].Path != "" {
		t.Errorf("after the install: %+v", installed)
	}

	// Nothing changed: systemd is left alone.
	m.Apply(ctx, mounted)
	if len(runner.commands) != len(want) {
		t.Errorf("an unchanged state was applied again: %v", runner.commands[len(want):])
	}

	// A plugin built for another kernel is installed but not loaded.
	other := filepath.Join(share, "anderer-kernel"+manifestSuffix)
	if err := m.Install(ctx, other, "data", "anderer-kernel", mounted); err != nil {
		t.Fatal(err)
	}
	if status := m.Status(mounted).Plugins[0]; !status.Enabled || status.Active || status.Problem != "kernel" {
		t.Errorf("plugin for another kernel: %+v", status)
	}

	// Without the volume the plugins are taken out of the system; with
	// it they come back. That also is what happens after a restart.
	if err := m.Apply(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Error("the plugin is still linked although its volume is gone")
	}
	again := NewManager(m.store)
	again.mountRoot, again.extDir, again.keyDir, again.kernel, again.runner = m.mountRoot, m.extDir, m.keyDir, m.kernel, runner
	again.Load()
	if err := again.Apply(ctx, mounted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Error("the plugin did not come back after a restart")
	}

	if again.Volume() != "data" {
		t.Errorf("Volume() = %q while plugins are installed", again.Volume())
	}
	if err := again.SetEnabled(ctx, "werkzeug", false, mounted); err != nil {
		t.Fatal(err)
	}
	// Its service is stopped before its files go.
	if n := len(runner.commands); runner.commands[n-2] != "systemctl stop werkzeug.service" || runner.commands[n-1] != "systemd-sysext refresh" {
		t.Errorf("switching off: %v", runner.commands[n-2:])
	}
	if _, err := os.Lstat(link); !errors.Is(err, os.ErrNotExist) {
		t.Error("a plugin that was switched off is still linked")
	}
	if err := again.Remove(ctx, "werkzeug", mounted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.mountRoot, "data/.titanos/plugins/werkzeug.raw")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the image of a removed plugin is still there")
	}
	if err := again.Remove(ctx, "werkzeug", mounted); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove twice: %v", err)
	}
	// What was uploaded stays where it is.
	if _, err := os.Stat(path); err != nil {
		t.Error("removing the plugin deleted the uploaded files")
	}
}

// TestSignature uses the real minisign, so that what is checked here is
// what the tool writes.
func TestSignature(t *testing.T) {
	if _, err := exec.LookPath("minisign"); err != nil {
		t.Skip("minisign is not installed")
	}
	m, _ := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data"}
	keys := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("minisign", args...).CombinedOutput(); err != nil {
			t.Fatalf("minisign %v: %v\n%s", args, err, out)
		}
	}
	run("-G", "-W", "-p", filepath.Join(keys, "titanos.pub"), "-s", filepath.Join(keys, "titanos.key"))
	run("-G", "-W", "-p", filepath.Join(keys, "fremd.pub"), "-s", filepath.Join(keys, "fremd.key"))
	os.MkdirAll(m.keyDir, 0o755)
	data, _ := os.ReadFile(filepath.Join(keys, "titanos.pub"))
	os.WriteFile(filepath.Join(m.keyDir, "titanos.pub"), data, 0o644)

	share := filepath.Join(m.mountRoot, "data", "media")
	path := plugin(t, share, "offiziell", nil)
	run("-S", "-s", filepath.Join(keys, "titanos.key"), "-m", path)
	if !m.official(path) {
		t.Fatal("a manifest signed with the trusted key does not count as official")
	}
	// Signed plugins need no name as confirmation, and stay official once installed.
	if err := m.Install(ctx, path, "data", "", mounted); err != nil {
		t.Fatal(err)
	}
	if status := m.Status(mounted).Plugins[0]; !status.Installed || !status.Official {
		t.Errorf("installed official plugin: %+v", status)
	}

	// The legacy form of the signature, without hashing first.
	legacy := plugin(t, share, "alt", nil)
	run("-S", "-l", "-s", filepath.Join(keys, "titanos.key"), "-m", legacy)
	if !m.official(legacy) {
		t.Error("the legacy form of the signature is not accepted")
	}

	// A changed manifest, a signature of another key, a swapped comment: none counts.
	changed := plugin(t, share, "geaendert", nil)
	run("-S", "-s", filepath.Join(keys, "titanos.key"), "-m", changed)
	manifest, _ := os.ReadFile(changed)
	os.WriteFile(changed, append(manifest, ' '), 0o644)
	foreign := plugin(t, share, "fremd", nil)
	run("-S", "-s", filepath.Join(keys, "fremd.key"), "-m", foreign)
	swapped := plugin(t, share, "kommentar", nil)
	run("-S", "-s", filepath.Join(keys, "titanos.key"), "-m", swapped)
	signature, _ := os.ReadFile(swapped + ".minisig")
	lines := strings.Split(string(signature), "\n")
	lines[2] = "trusted comment: something else"
	os.WriteFile(swapped+".minisig", []byte(strings.Join(lines, "\n")), 0o644)
	for _, bad := range []string{changed, foreign, swapped} {
		if m.official(bad) {
			t.Errorf("%s counts as official", filepath.Base(bad))
		}
	}
	if err := m.Install(ctx, changed, "data", "", mounted); !errors.Is(err, ErrNotConfirmed) {
		t.Errorf("a plugin with a broken signature installs without confirmation: %v", err)
	}
}
