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

type fakeRunner struct {
	commands []string
	fail     string
}

func (r *fakeRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	r.commands = append(r.commands, name+" "+strings.Join(args, " "))
	if r.fail != "" && strings.Contains(name+" "+strings.Join(args, " "), r.fail) {
		return "", errors.New("injected failure")
	}
	return "", nil
}

func newTestManager(t *testing.T) (*Manager, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	m := NewManager(config.Open(filepath.Join(root, "config")))
	m.mountRoot, m.extDir, m.keyDir, m.kernel = filepath.Join(root, "mnt"), filepath.Join(root, "extensions"), filepath.Join(root, "keys"), "6.12.0-test"
	m.moduleRoot, m.moduleSource = filepath.Join(root, "module-index"), filepath.Join(root, "modules")
	os.MkdirAll(filepath.Join(m.moduleSource, m.kernel), 0o755)
	runner := &fakeRunner{}
	m.runner = runner
	os.MkdirAll(filepath.Join(m.mountRoot, "data", "media"), 0o755)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, runner
}

func TestFailedActivationCanBeRetried(t *testing.T) {
	for _, failure := range []string{"modprobe ", "systemctl start"} {
		t.Run(failure, func(t *testing.T) {
			m, r := newTestManager(t)
			path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", func(p *Manifest) {
				p.Modules = []string{"nvidia", "nvidia_uvm"}
				p.Services = []string{"titanos-nvidia.service"}
			})
			r.fail = failure
			if err := m.Install(context.Background(), path, "data", "gpu", []string{"data"}); err == nil {
				t.Fatal("failed activation was ignored")
			}
			status := m.Status([]string{"data"}).Plugins[0]
			if !status.Installed || !status.Enabled || status.Active || status.Problem != "activation" {
				t.Fatalf("false active state: %+v", status)
			}
			count := len(r.commands)
			m.Apply(context.Background(), []string{"data"})
			if len(r.commands) != count {
				t.Fatal("failed hardware probed repeatedly")
			}
			r.fail = ""
			if err := m.SetEnabled(context.Background(), "gpu", true, []string{"data"}); err != nil {
				t.Fatal(err)
			}
			if status = m.Status([]string{"data"}).Plugins[0]; !status.Active || status.Problem != "" {
				t.Fatalf("retry: %+v", status)
			}
		})
	}
}

func TestInstallWithoutActivationLeavesRunningConsumersAlone(t *testing.T) {
	m, r := newTestManager(t)
	path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", func(p *Manifest) {
		p.Modules = []string{"nvidia", "nvidia_uvm"}
		p.Services = []string{"titanos-nvidia.service"}
	})
	ctx, mounted := context.Background(), []string{"data"}
	// Establish the normal background state before installing, as on a NAS
	// with running VMs. Even unusable/busy hardware must allow storage only.
	if err := m.Apply(ctx, mounted); err != nil {
		t.Fatal(err)
	}
	r.commands = nil
	r.fail = "nvidia"
	if err := m.InstallWithActivation(ctx, path, "data", "gpu", mounted, false); err != nil {
		t.Fatal(err)
	}
	if d := m.Status(mounted).Plugins[0]; !d.Installed || d.Enabled || d.Active || d.Problem != "" {
		t.Fatalf("deferred plugin: %+v", d)
	}
	for _, command := range r.commands {
		t.Fatalf("installation changed running system: %s", command)
	}
	// Background reconciliation must also leave existing consumers alone.
	if err := m.Apply(ctx, mounted); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(m.extDir, "gpu.raw")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deferred image mounted: %v", err)
	}
	if len(r.commands) != 0 {
		t.Fatalf("deferred installation affected reconciliation: %v", r.commands)
	}
	r.fail = ""
	if err := m.SetEnabled(ctx, "gpu", true, mounted); err != nil {
		t.Fatal(err)
	}
	if !m.Status(mounted).Plugins[0].Active {
		t.Fatal("explicit later activation failed")
	}
}

func TestBusyDriverCannotBeUnmountedOrRemoved(t *testing.T) {
	m, r := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data"}
	path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", func(p *Manifest) { p.Modules = []string{"nvidia", "nvidia_uvm"}; p.Services = []string{"gpu.service"} })
	if err := m.Install(ctx, path, "data", "gpu", mounted); err != nil {
		t.Fatal(err)
	}
	r.fail = "-r nvidia"
	r.commands = nil
	if err := m.Remove(ctx, "gpu", mounted); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy removal: %v", err)
	}
	if d := m.Status(mounted).Plugins[0]; !d.Installed || !d.Enabled || !d.Active || d.Problem != "busy" {
		t.Fatalf("busy state: %+v", d)
	}
	if _, err := os.Lstat(filepath.Join(m.extDir, "gpu.raw")); err != nil {
		t.Fatal("unlinked live driver", err)
	}
	if slices.Contains(r.commands, "systemd-sysext refresh") {
		t.Fatal("refreshed sysext while driver was busy")
	}
	if !slices.Contains(r.commands, "systemctl start gpu.service") {
		t.Fatal("did not restore service after refused unload")
	}
	r.fail = "nvidia" // unload and the attempted restoration both fail
	if err := m.SetEnabled(ctx, "gpu", false, mounted); !errors.Is(err, ErrBusy) {
		t.Fatal(err)
	}
	if m.Status(mounted).Plugins[0].Active {
		t.Fatal("failed restoration reported an active driver")
	}
	r.fail = ""
	if err := m.Remove(ctx, "gpu", mounted); err != nil {
		t.Fatal(err)
	}
}

func TestFailedConfigWriteRestoresDisabledPlugin(t *testing.T) {
	m, r := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data"}
	path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", func(p *Manifest) { p.Services = []string{"gpu.service"} })
	if err := m.Install(ctx, path, "data", "gpu", mounted); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(t.TempDir(), "not-a-directory")
	os.WriteFile(broken, nil, 0o600)
	m.store = config.Open(broken)
	r.commands = nil
	if err := m.SetEnabled(ctx, "gpu", false, mounted); err == nil {
		t.Fatal("configuration write unexpectedly succeeded")
	}
	if d := m.Status(mounted).Plugins[0]; !d.Enabled || !d.Active {
		t.Fatalf("failed save changed live state: %+v", d)
	}
	if !slices.Contains(r.commands, "systemctl start gpu.service") {
		t.Fatal("service was not restored")
	}
}

func TestModuleIndexLivesInRAM(t *testing.T) {
	m, r := newTestManager(t)
	source := filepath.Join(m.moduleSource, m.kernel)
	os.MkdirAll(filepath.Join(source, "updates"), 0o755)
	for path, value := range map[string]string{"updates/nvidia.ko": "module", "modules.dep": "original", "modules.builtin": "builtins"} {
		os.WriteFile(filepath.Join(source, path), []byte(value), 0o644)
	}
	if err := m.prepareModules(context.Background()); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(m.moduleRoot, "lib/modules", m.kernel)
	if path, err := os.Readlink(filepath.Join(index, "updates/nvidia.ko")); err != nil || path != filepath.Join(source, "updates/nvidia.ko") {
		t.Fatalf("module link: %q %v", path, err)
	}
	if _, err := os.Lstat(filepath.Join(index, "modules.dep")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("index points into read-only /usr")
	}
	if data, _ := os.ReadFile(filepath.Join(source, "modules.dep")); string(data) != "original" {
		t.Fatal("modified original index")
	}
	if !slices.Equal(r.commands, []string{"depmod -b " + m.moduleRoot + " " + m.kernel}) {
		t.Fatalf("depmod: %v", r.commands)
	}
}

func TestChangedInstalledImageRejectedAfterRestart(t *testing.T) {
	m, r := newTestManager(t)
	ctx := context.Background()
	mounted := []string{"data"}
	path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", nil)
	if err := m.Install(ctx, path, "data", "gpu", mounted); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(m.dir("data"), "gpu.raw"), []byte("changed"), 0o600)
	m.active = map[string]Manifest{}
	m.resident = map[string]Manifest{}
	m.applied = ""
	r.commands = nil
	if err := m.Apply(ctx, mounted); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changed image: %v", err)
	}
	if d := m.Status(mounted).Plugins[0]; d.Active || d.Problem != "integrity" {
		t.Fatalf("changed image status: %+v", d)
	}
	if len(r.commands) != 0 {
		t.Fatal("activated changed image")
	}
}

func TestDevModeDoesNotActivatePlugins(t *testing.T) {
	m, r := newTestManager(t)
	m.Disabled = true
	if err := m.Apply(context.Background(), []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if len(r.commands) != 0 {
		t.Fatal("dev mode ran system commands")
	}
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
	want := []string{"systemd-sysext refresh", "ldconfig -X", "systemctl daemon-reload", "depmod -b " + m.moduleRoot + " " + m.kernel, "modprobe -d " + m.moduleRoot + " i915", "systemctl start werkzeug.service"}
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
	again.moduleRoot, again.moduleSource = m.moduleRoot, m.moduleSource
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
	stop := slices.Index(runner.commands, "systemctl stop werkzeug.service")
	unload := slices.Index(runner.commands, "modprobe -d "+m.moduleRoot+" -r i915")
	if stop < 0 || unload <= stop {
		t.Errorf("service was not stopped before unloading: %v", runner.commands)
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

// The status poll reuses the last search of the shares instead of reading the
// volumes every few seconds; the page asks for a new one, and other mounted
// volumes start one by themselves.
func TestStatusReusesShareSearch(t *testing.T) {
	m, _ := newTestManager(t)
	share := filepath.Join(m.mountRoot, "data", "media")
	plugin(t, share, "eins", nil)
	if got := m.Status([]string{"data"}).Plugins; len(got) != 1 || got[0].Name != "eins" {
		t.Fatalf("first status: %+v", got)
	}
	plugin(t, share, "zwei", nil)
	if got := m.Status([]string{"data"}).Plugins; len(got) != 1 {
		t.Fatalf("polling searched the share again: %+v", got)
	}
	m.ScanShares([]string{"data"})
	if got := m.Status([]string{"data"}).Plugins; len(got) != 2 {
		t.Fatalf("an explicit search missed the new plugin: %+v", got)
	}
	os.MkdirAll(filepath.Join(m.mountRoot, "usb", "drivers"), 0o755)
	plugin(t, filepath.Join(m.mountRoot, "usb", "drivers"), "drei", nil)
	if got := m.Status([]string{"usb", "data"}).Plugins; len(got) != 3 {
		t.Fatalf("a newly mounted volume was not searched: %+v", got)
	}
}

// secondVersion puts version 2.0 of name, with another image, into dir.
func secondVersion(t *testing.T, dir, name string, change func(*Manifest)) string {
	t.Helper()
	image := []byte("squashfs of " + name + " 2.0")
	sum := sha256.Sum256(image)
	path := plugin(t, dir, name, func(p *Manifest) {
		p.Version, p.SHA256, p.Size = "2.0", hex.EncodeToString(sum[:]), int64(len(image))
		p.Services = []string{name + ".service"}
		if change != nil {
			change(p)
		}
	})
	if err := os.WriteFile(filepath.Join(dir, name+".raw"), image, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUpdateFromShareReplacesRunningPlugin(t *testing.T) {
	m, runner := newTestManager(t)
	ctx, mounted := context.Background(), []string{"data"}
	share := filepath.Join(m.mountRoot, "data", "media")
	first := plugin(t, share, "werkzeug", func(p *Manifest) { p.Services = []string{"werkzeug.service"} })
	if err := m.Install(ctx, first, "data", "werkzeug", mounted); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(m.mountRoot, "data", "neu")
	os.MkdirAll(next, 0o755)
	path := secondVersion(t, next, "werkzeug", nil)
	m.ScanShares(mounted)
	got := m.Status(mounted).Plugins
	if len(got) != 1 || got[0].Update == nil || got[0].Update.Path != path || got[0].Update.Version != "2.0" || got[0].Update.Official {
		t.Fatalf("offered update: %+v", got)
	}

	if err := m.Update(ctx, "werkzeug", path, "", mounted); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("unsigned update without confirmation: %v", err)
	}
	if err := m.Update(ctx, "werkzeug", first, "werkzeug", mounted); !errors.Is(err, ErrInvalid) {
		t.Fatalf("update to the installed version: %v", err)
	}
	if err := m.Update(ctx, "werkzeug", filepath.Join(m.baseDir("data"), "werkzeug"+manifestSuffix), "werkzeug", mounted); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update from the plugin folder itself: %v", err)
	}
	runner.commands = nil
	if err := m.Update(ctx, "werkzeug", path, "werkzeug", mounted); err != nil {
		t.Fatal(err)
	}
	want := []string{"systemctl stop werkzeug.service", "systemd-sysext refresh", "ldconfig -X", "systemctl daemon-reload", "systemctl start werkzeug.service"}
	if !slices.Equal(runner.commands, want) {
		t.Errorf("commands = %v", runner.commands)
	}
	got = m.Status(mounted).Plugins
	if len(got) != 1 || got[0].Version != "2.0" || !got[0].Active || got[0].Update != nil {
		t.Fatalf("after the update: %+v", got)
	}
	leftovers, _ := filepath.Glob(filepath.Join(m.dir("data"), ".*"))
	if len(leftovers) != 0 {
		t.Fatalf("update left %v behind", leftovers)
	}
}

func TestUpdateThatFailsToStartBringsPreviousBack(t *testing.T) {
	m, runner := newTestManager(t)
	ctx, mounted := context.Background(), []string{"data"}
	share := filepath.Join(m.mountRoot, "data", "media")
	first := plugin(t, share, "werkzeug", func(p *Manifest) { p.Services = []string{"werkzeug.service"} })
	if err := m.Install(ctx, first, "data", "werkzeug", mounted); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(m.mountRoot, "data", "neu")
	os.MkdirAll(next, 0o755)
	path := secondVersion(t, next, "werkzeug", func(p *Manifest) { p.Services = []string{"werkzeug-neu.service"} })
	runner.fail = "start werkzeug-neu.service"
	if err := m.Update(ctx, "werkzeug", path, "werkzeug", mounted); err == nil {
		t.Fatal("a version that does not start was accepted")
	}
	got := m.Status(mounted).Plugins
	if len(got) == 0 || got[0].Version != "1.0" || !got[0].Active || got[0].Problem != "" {
		t.Fatalf("previous version not back: %+v", got)
	}
	if !m.Healthy() {
		t.Fatal("not healthy after the way back")
	}

	// A plugin still in use is not touched at all.
	runner.fail = "stop werkzeug.service"
	if err := m.Update(ctx, "werkzeug", path, "werkzeug", mounted); !errors.Is(err, ErrBusy) {
		t.Fatalf("busy plugin: %v", err)
	}
	if v := m.installed("data")["werkzeug"].Version; v != "1.0" {
		t.Fatalf("busy plugin was replaced by %s", v)
	}
}

func TestNvidiaIsNotUpdatedFromShares(t *testing.T) {
	m, _ := newTestManager(t)
	ctx, mounted := context.Background(), []string{"data"}
	share := filepath.Join(m.mountRoot, "data", "media")
	first := plugin(t, share, "nvidia", nil)
	if err := m.Install(ctx, first, "data", "nvidia", mounted); err != nil {
		t.Fatal(err)
	}
	next := filepath.Join(m.mountRoot, "data", "neu")
	os.MkdirAll(next, 0o755)
	path := secondVersion(t, next, "nvidia", nil)
	m.ScanShares(mounted)
	if got := m.Status(mounted).Plugins; len(got) != 1 || got[0].Update != nil {
		t.Fatalf("NVIDIA offered as share update: %+v", got)
	}
	if err := m.Update(ctx, "nvidia", path, "nvidia", mounted); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NVIDIA update from a share: %v", err)
	}
}

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		found, installed string
		newer            bool
	}{
		{"2.0", "1.0", true}, {"1.10.2", "1.9", true}, {"1.9", "1.10.2", false}, {"1.0", "1.0", false},
		{"1.0.1", "1.0", true}, {"1.0", "1.0.0", false}, {"v1.2", "1.1-beta", true}, {"neu", "alt", true},
	} {
		if got := newerVersion(c.found, c.installed); got != c.newer {
			t.Errorf("%s newer than %s: %v", c.found, c.installed, got)
		}
	}
}
