package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// community serves plugins under /community next to the release fixture.
func community(t *testing.T) (*catalogFixture, string, string) {
	t.Helper()
	f := newCatalogFixture(t)
	f.m.externalClient = f.m.client
	dir := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(f.dir))), "community")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	return f, dir, f.m.releaseSource + "/community/"
}

func installed(t *testing.T, m *Manager, name string) Plugin {
	t.Helper()
	for _, p := range m.Status([]string{"data"}).Plugins {
		if p.Name == name && p.Installed {
			return p
		}
	}
	t.Fatalf("%s is not installed", name)
	return Plugin{}
}

func TestLinkInstallsUnsignedOnlyWithConfirmation(t *testing.T) {
	f, dir, base := community(t)
	m := f.m
	ctx := context.Background()
	plugin(t, dir, "werkzeug", nil)
	preview, err := m.InspectLink(ctx, base+"werkzeug.plugin.json")
	if err != nil || preview.Name != "werkzeug" || preview.Official || !preview.Download {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if f.count(true) != 0 {
		t.Fatal("inspecting downloaded the image")
	}
	req := ExternalRequest{URL: base + "werkzeug.plugin.json", Volume: "data"}
	if err := m.InstallExternal(ctx, req, []string{"data"}); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("unconfirmed install: %v", err)
	}
	req.Confirm = "werkzeug"
	if err := m.InstallExternal(ctx, req, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if p := installed(t, m, "werkzeug"); p.Official || !p.Active {
		t.Fatalf("installed %+v", p)
	}
	if stages, _ := filepath.Glob(filepath.Join(m.baseDir("data"), ".download-*")); len(stages) != 0 {
		t.Fatal("staging left behind")
	}
	for _, bad := range []string{base + "werkzeug.raw", "file:///etc/passwd", base + "missing.plugin.json"} {
		if _, err := m.InspectLink(ctx, bad); err == nil {
			t.Fatalf("%s accepted", bad)
		}
	}
}

func TestSignedLinkIsOfficialAndNeedsNoName(t *testing.T) {
	f, dir, base := community(t)
	path := plugin(t, dir, "signiert", nil)
	f.command(t, "-S", "-s", f.key, "-m", path)
	if err := f.m.InstallExternal(context.Background(), ExternalRequest{URL: base + "signiert.plugin.json", Volume: "data"}, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if !installed(t, f.m, "signiert").Official {
		t.Fatal("trusted signature not recognized")
	}
}

func TestSourceListsUpdatesAndIsRemembered(t *testing.T) {
	f, dir, base := community(t)
	m := f.m
	ctx := context.Background()
	plugin(t, dir, "werkzeug", nil)
	plugin(t, dir, "helfer", func(p *Manifest) { p.Version = "2.0" })
	list := map[string]any{"name": "Meine Plugins", "plugins": []map[string]string{{"name": "werkzeug"}, {"name": "helfer", "url": base + "helfer.plugin.json"}}}
	data, _ := json.Marshal(list)
	os.WriteFile(filepath.Join(dir, "plugins.json"), data, 0o644)
	state, err := m.AddSource(ctx, base+"plugins.json")
	if err != nil || state.Plugins != 2 {
		t.Fatalf("source %+v %v", state, err)
	}
	if _, err := m.AddSource(ctx, base+"plugins.json"); !errors.Is(err, ErrExists) {
		t.Fatalf("second time: %v", err)
	}
	if err := m.RefreshCatalog(ctx, true); err != nil {
		t.Fatal(err)
	}
	offered := map[string]Plugin{}
	for _, p := range m.Status([]string{"data"}).Plugins {
		offered[p.Name] = p
	}
	if offered["werkzeug"].Source != state.ID || offered["helfer"].Version != "2.0" || offered["intel-gpu-tools"].Source != "" {
		t.Fatalf("catalog %+v", offered)
	}
	if err := m.InstallExternal(ctx, ExternalRequest{Source: state.ID, Name: "werkzeug", Volume: "data", Confirm: "werkzeug"}, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	// A newer version in the source is offered and needs the name again.
	plugin(t, dir, "werkzeug", func(p *Manifest) { p.Version = "1.1" })
	os.WriteFile(filepath.Join(dir, "werkzeug.raw"), []byte("squashfs of werkzeug 1.1"), 0o644)
	rewrite := func() {
		path := filepath.Join(dir, "werkzeug"+manifestSuffix)
		var manifest Manifest
		data, _ := os.ReadFile(path)
		json.Unmarshal(data, &manifest)
		manifest.SHA256, manifest.Size = sha256Hex([]byte("squashfs of werkzeug 1.1")), int64(len("squashfs of werkzeug 1.1"))
		data, _ = json.Marshal(manifest)
		os.WriteFile(path, data, 0o644)
	}
	rewrite()
	if err := m.RefreshCatalog(ctx, true); err != nil {
		t.Fatal(err)
	}
	update := installed(t, m, "werkzeug").Update
	if update == nil || !update.Download || update.Source != state.ID || update.Version != "1.1" {
		t.Fatalf("update %+v", update)
	}
	if err := m.Update(ctx, "werkzeug", "", "", []string{"data"}); !errors.Is(err, ErrNotConfirmed) {
		t.Fatalf("unconfirmed update: %v", err)
	}
	if err := m.Update(ctx, "werkzeug", "", "werkzeug", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if p := installed(t, m, "werkzeug"); p.Version != "1.1" || !p.Active {
		t.Fatalf("after update %+v", p)
	}
	// The list survives a restart; removing it keeps installed plugins.
	again := NewManager(m.store)
	again.mountRoot, again.keyDir = m.mountRoot, m.keyDir
	if err := again.Load(); err != nil || len(again.Sources()) != 1 {
		t.Fatalf("sources after restart %+v %v", again.Sources(), err)
	}
	if err := m.RemoveSource(state.ID); err != nil {
		t.Fatal(err)
	}
	if len(m.Sources()) != 0 || installed(t, m, "werkzeug").Name == "" {
		t.Fatal("removal")
	}
}

func TestSourcesCannotReplaceOfficialPlugins(t *testing.T) {
	f, dir, base := community(t)
	ctx := context.Background()
	if err := f.m.RefreshCatalog(ctx, true); err != nil {
		t.Fatal(err)
	}
	plugin(t, dir, "intel-gpu-tools", func(p *Manifest) { p.Kernel = f.m.kernel })
	err := f.m.InstallExternal(ctx, ExternalRequest{URL: base + "intel-gpu-tools.plugin.json", Volume: "data", Confirm: "intel-gpu-tools"}, []string{"data"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("official name taken over: %v", err)
	}
	// A list whose entry does not match its manifest hash is refused.
	data, _ := json.Marshal(map[string]any{"plugins": []map[string]string{{"name": "intel-gpu-tools", "manifest": strings.Repeat("a", 64)}}})
	os.WriteFile(filepath.Join(dir, "list.json"), data, 0o644)
	if _, err := f.m.AddSource(ctx, base+"list.json"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("hash mismatch: %v", err)
	}
}

// Started without network, the catalog was never loaded. The names of the
// official plugins still come from the table in the image, including those
// that only a kernel set of another kernel has.
func TestOfficialNamesHoldWithoutCatalog(t *testing.T) {
	f, dir, base := community(t)
	ctx := context.Background()
	f.m.catalogTable = t.TempDir()
	if err := os.WriteFile(filepath.Join(f.m.catalogTable, "nvidia.json"), []byte(`{"category":"graphics"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	plugin(t, dir, "nvidia", func(p *Manifest) { p.Kernel = f.m.kernel })
	err := f.m.InstallExternal(ctx, ExternalRequest{URL: base + "nvidia.plugin.json", Volume: "data", Confirm: "nvidia"}, []string{"data"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("official name taken over while the catalog was not loaded: %v", err)
	}
	// Other names stay open to community plugins.
	plugin(t, dir, "werkzeug", func(p *Manifest) { p.Kernel = f.m.kernel })
	if err := f.m.InstallExternal(ctx, ExternalRequest{URL: base + "werkzeug.plugin.json", Volume: "data", Confirm: "werkzeug"}, []string{"data"}); err != nil {
		t.Fatal(err)
	}
}
