package plugins

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateStagesSignedPluginsAndKeepsRollback(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	if _, err := exec.LookPath("minisign"); err != nil {
		t.Skip("minisign unavailable")
	}
	keys := t.TempDir()
	m.keyDir = keys
	key := filepath.Join(keys, "test.key")
	command := func(args ...string) {
		t.Helper()
		if out, err := exec.Command("minisign", args...).CombinedOutput(); err != nil {
			t.Fatalf("minisign: %v %s", err, out)
		}
	}
	command("-G", "-W", "-s", key, "-p", filepath.Join(keys, "test.pub"))
	old := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", func(p *Manifest) { p.Kernel = m.kernel })
	command("-S", "-s", key, "-m", old)
	if err := m.Install(ctx, old, "data", "", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	m.Release = "0.1.0-beta.2"
	oldDir := m.dir("data")
	remote := t.TempDir()
	newPath := plugin(t, remote, "gpu", func(p *Manifest) { p.Kernel = "new-kernel"; p.Version = "0.1.0-beta.3" })
	command("-S", "-s", key, "-m", newPath)
	fetch := func(_ context.Context, url string, _ int64) ([]byte, error) {
		return os.ReadFile(filepath.Join(remote, strings.TrimPrefix(url, "https://release/")))
	}
	download := func(_ context.Context, url, dest, sum string, size int64) error {
		return copyFile(filepath.Join(remote, strings.TrimPrefix(url, "https://release/")), dest, 0o600)
	}
	prepare := func() error {
		return m.PrepareUpdate(ctx, "0.1.0-beta.3", "new-kernel", "https://release/", map[string]string{"gpu": "gpu.plugin.json"}, []string{"data"}, fetch, download)
	}
	if err := prepare(); err != nil {
		t.Fatal(err)
	}
	if m.dir("data") != oldDir {
		t.Fatal("preparation changed running plugins")
	}
	// Repeating an identical preparation is safe and does not replace a set.
	if err := prepare(); err != nil {
		t.Fatal(err)
	}
	m.Release = "0.1.0-beta.3"
	if m.dir("data") == oldDir || m.installed("data")["gpu"].Kernel != "new-kernel" {
		t.Fatal("new release did not select its plugins")
	}
	m.Release = "0.1.0-beta.2"
	if m.dir("data") != oldDir || m.installed("data")["gpu"].Kernel != m.kernel {
		t.Fatal("rollback lost old plugin")
	}
	// Tampering with metadata must abort before download or publishing .ready.
	os.WriteFile(newPath, []byte(`{"name":"gpu","size":100,"sha256":"bad"}`), 0o600)
	if err := m.PrepareUpdate(ctx, "0.1.0-beta.4", "new-kernel", "https://release/", map[string]string{"gpu": "gpu.plugin.json"}, []string{"data"}, fetch, download); err == nil {
		t.Fatal("accepted unsigned replacement")
	}
	if _, err := os.Stat(filepath.Join(m.releaseDir("data", "0.1.0-beta.4"), ".ready")); !os.IsNotExist(err) {
		t.Fatal("failed preparation published")
	}
}

func TestUpdateRefusesMissingKernelPluginAndOfflineVolume(t *testing.T) {
	m, _ := newTestManager(t)
	path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "gpu", func(p *Manifest) { p.Kernel = m.kernel })
	if err := m.Install(context.Background(), path, "data", "gpu", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if err := m.PrepareUpdate(context.Background(), "1.0.0", "other", "", nil, []string{"data"}, nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("lost kernel plugin", err)
	}
	if err := m.PrepareUpdate(context.Background(), "1.0.0", m.kernel, "", nil, nil, nil, nil); !errors.Is(err, ErrUnknownVolume) {
		t.Fatal("wrote into RAM instead of volume", err)
	}
	if err := m.PrepareUpdate(context.Background(), "../../escape", m.kernel, "", nil, []string{"data"}, nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestReleaseSettingsAndFirstMutationPreserveLegacyRollback(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "tools", nil)
	if err := m.Install(ctx, path, "data", "tools", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	legacyDir := m.dir("data")
	m.Release = "1.1.0"
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if err := m.SetEnabled(ctx, "tools", false, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if m.dir("data") == legacyDir {
		t.Fatal("did not fork legacy files")
	}
	if err := m.Remove(ctx, "tools", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	m.Release = ""
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if len(m.current().Enabled) != 1 || m.installed("data")["tools"].Name != "tools" {
		t.Fatal("changed legacy rollback")
	}
	m.Release = "1.1.0"
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if len(m.current().Enabled) != 0 || len(m.installed("data")) != 0 {
		t.Fatal("new release changes lost")
	}
	m.ChangeAllowed = func() bool { return false }
	if err := m.Install(ctx, path, "data", "tools", []string{"data"}); !errors.Is(err, ErrUpdatePending) {
		t.Fatal("mutation raced update", err)
	}
}

func TestRuntimeSettingsSerializeWithPluginLifecycle(t *testing.T) {
	m, _ := newTestManager(t)
	m.active["tailscale"] = Manifest{Name: "tailscale"}
	if err := m.RuntimeAction("tailscale", func() error {
		if m.actions.TryLock() {
			m.actions.Unlock()
			t.Fatal("runtime settings were not serialized")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	m.ChangeAllowed = func() bool { return false }
	if err := m.RuntimeAction("tailscale", func() error { t.Fatal("changed during pending update"); return nil }); !errors.Is(err, ErrUpdatePending) {
		t.Fatal(err)
	}
	m.UpdateAllowed = func() bool { return false }
	if err := m.PrepareUpdate(context.Background(), "1.0.0", m.kernel, "", nil, nil, nil, nil); !errors.Is(err, ErrBusy) {
		t.Fatal("update ignored routing trial", err)
	}
}
