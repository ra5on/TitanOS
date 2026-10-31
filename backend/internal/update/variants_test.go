package update

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// publishWithNewKernel publishes version with a "new" variant: files of
// their own, a backports kernel.
func publishWithNewKernel(t *testing.T, e *env, version string) map[string][]byte {
	t.Helper()
	extra := map[string][]byte{}
	e.publish(t, version, ChannelStable, func(man *Manifest) {
		v := Variant{Kernel: "6.16.12+bpo-amd64", KernelPackage: "6.16.12-1~bpo13+1"}
		for i, name := range slotFiles {
			data := make([]byte, 200_000+i*1000)
			rand.Read(data)
			asset := "titanos-" + version + "-new-" + name
			extra[asset] = data
			sum := sha256.Sum256(data)
			v.Files = append(v.Files, File{Name: name, Asset: asset, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))})
		}
		man.Variants = map[string]Variant{VariantNew: v}
	})
	e.release.mu.Lock()
	for name, data := range extra {
		e.release.assets[name] = data
	}
	e.release.mu.Unlock()
	return extra
}

func setKernel(t *testing.T, m *Manager, variant string) {
	t.Helper()
	settings := m.Status().Settings
	settings.Kernel = variant
	if err := m.SetSettings(settings); err != nil {
		t.Fatal(err)
	}
}

func TestSwitchToTheNewKernelAtTheSameVersion(t *testing.T) {
	e := newEnv(t)
	extra := publishWithNewKernel(t, e, "1.0.0")
	var prepared []string
	e.m.PreparePlugins = func(_ context.Context, version, variant, kernel, kernelPackage, _ string, assets map[string]string, _ func(context.Context, string, int64) ([]byte, error), _ func(context.Context, string, string, string, int64) error) error {
		prepared = []string{version, variant, kernel, kernelPackage}
		if len(assets) != 0 {
			t.Errorf("plugins of the release offered for the new kernel: %v", assets)
		}
		return nil
	}
	if err := e.m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := e.m.Status(); status.Available != nil || status.Variant != VariantStable {
		t.Fatalf("the running version was offered: %+v", status)
	}
	setKernel(t, e.m, VariantNew)
	status := e.m.Status()
	if status.Available == nil || !status.KernelSwitch || status.Kind != KindSystem || status.Settings.Kernel != VariantNew {
		t.Fatalf("no kernel switch offered: %+v", status)
	}
	if e.event(t) != "" {
		t.Error("a kernel switch was announced as an update")
	}
	e.install(t)
	if status := e.m.Status(); status.Progress.Phase != "done" || status.Pending == nil || status.Pending.Slot != "b" {
		t.Fatalf("after: %+v", status.Progress)
	}
	if prepared == nil || prepared[0] != "1.0.0" || prepared[1] != VariantNew || prepared[2] != "6.16.12+bpo-amd64" || prepared[3] != "6.16.12-1~bpo13+1" {
		t.Errorf("plugins prepared for %v", prepared)
	}
	for _, name := range slotFiles {
		data, _ := os.ReadFile(filepath.Join(e.boot, "titanos/b", name))
		if string(data) != string(extra["titanos-1.0.0-new-"+name]) {
			t.Errorf("%s is not the one of the new kernel", name)
		}
	}
	if got := entries(e); got != "titanos-a.conf[titanos-2] titanos-b+3.conf[titanos-1]" {
		t.Errorf("entries: %s", got)
	}
	// Back to stable: nothing to install, the slot with the stable kernel
	// is the fallback.
	setKernel(t, e.m, "stable")
	if status := e.m.Status(); status.Available != nil || status.Settings.Kernel != "" {
		t.Errorf("stable again: %+v", status)
	}
}

func TestNewerReleaseWithTheChosenKernel(t *testing.T) {
	e := newEnv(t)
	e.m.Variant = VariantNew
	setKernel(t, e.m, VariantNew)
	// Without a system with the new kernel, the release is not offered:
	// it would quietly go back to the old kernel.
	e.publish(t, "1.1.0", ChannelStable, nil)
	if err := e.m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := e.m.Status(); status.Available != nil || status.KernelMissing != "1.1.0" {
		t.Fatalf("missing variant: %+v", status)
	}
	if err := e.m.Install(context.Background()); !errors.Is(err, ErrNothing) {
		t.Errorf("install: %v", err)
	}
	extra := publishWithNewKernel(t, e, "1.1.0")
	e.install(t)
	data, _ := os.ReadFile(filepath.Join(e.boot, "titanos/b/vmlinuz"))
	if string(data) != string(extra["titanos-1.1.0-new-vmlinuz"]) {
		t.Error("the stable kernel was installed")
	}
}

func TestManifestVariantsAreChecked(t *testing.T) {
	good := Manifest{Version: "1.0.0", Tag: "v1.0.0", Channel: ChannelStable, Cmdline: cmdline}
	for _, name := range slotFiles {
		good.Files = append(good.Files, File{Name: name, Asset: "a-" + name, SHA256: hex.EncodeToString(make([]byte, 32)), Size: 1})
	}
	for name, change := range map[string]func(*Manifest){
		"unknown variant": func(m *Manifest) { m.Variants = map[string]Variant{"old": {Kernel: "6.1", Files: m.Files}} },
		"no kernel":       func(m *Manifest) { m.Variants = map[string]Variant{VariantNew: {Files: m.Files}} },
		"missing file":    func(m *Manifest) { m.Variants = map[string]Variant{VariantNew: {Kernel: "6.16", Files: m.Files[:2]}} },
		"foreign file": func(m *Manifest) {
			files := append([]File{}, m.Files...)
			files[0].Name = "../../EFI/BOOT/BOOTIA32.EFI"
			m.Variants = map[string]Variant{VariantNew: {Kernel: "6.16", Files: files}}
		},
	} {
		man := good
		change(&man)
		raw, _ := json.Marshal(man)
		if _, err := parseManifest(raw); !errors.Is(err, ErrManifest) {
			t.Errorf("%s: %v", name, err)
		}
	}
	good.Variants = map[string]Variant{VariantNew: {Kernel: "6.16.12+bpo-amd64", Files: good.Files}}
	raw, _ := json.Marshal(good)
	if _, err := parseManifest(raw); err != nil {
		t.Errorf("good: %v", err)
	}
}

func TestKernelReleaseFromTheImageHeader(t *testing.T) {
	head := make([]byte, 0x1000)
	copy(head[0x202:], "HdrS")
	binary.LittleEndian.PutUint16(head[0x20E:], 0x600)
	copy(head[0x800:], "6.16.12+bpo-amd64 (debian-kernel@lists.debian.org) #1 SMP")
	path := filepath.Join(t.TempDir(), "vmlinuz")
	os.WriteFile(path, head, 0o644)
	if got := kernelRelease(path); got != "6.16.12+bpo-amd64" || kernelVariant(got) != VariantNew {
		t.Errorf("%q", got)
	}
	os.WriteFile(path, []byte("old vmlinuz"), 0o644)
	if got := kernelRelease(path); got != "" || kernelVariant(got) != "" {
		t.Errorf("not a kernel: %q", got)
	}
	if kernelVariant("7.2.11+deb13-amd64") != VariantNew || kernelVariant("6.16.3+deb13-amd64") != VariantNew {
		t.Error("backports kernels name no bpo in their release")
	}
	if kernelVariant("6.12.48+deb13-amd64") != VariantStable {
		t.Error("stable kernel")
	}
	dir := t.TempDir()
	if ReadVariant(filepath.Join(dir, "none")) != VariantStable {
		t.Error("missing file")
	}
	os.WriteFile(filepath.Join(dir, "v"), []byte("new\n"), 0o644)
	if ReadVariant(filepath.Join(dir, "v")) != VariantNew {
		t.Error("new")
	}
}

func TestInvalidKernelChoice(t *testing.T) {
	e := newEnv(t)
	settings := e.m.Status().Settings
	settings.Kernel = "rc"
	if err := e.m.SetSettings(settings); !errors.Is(err, ErrInvalid) {
		t.Errorf("%v", err)
	}
}
