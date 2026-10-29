package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
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

// roundTrip lets a test stand in for an http.Client's transport.
type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A source list may only send the NAS to public addresses: what it names on
// a host other than its own goes through the public client, also the
// image. Same host as the list (which the person chose) stays as it was.
func TestSourceListsReachOtherHostsPublicOnly(t *testing.T) {
	f, dir, base := community(t)
	m := f.m
	ctx := context.Background()
	other := strings.Replace(base, "127.0.0.1", "localhost", 1)
	if other == base {
		t.Skip("test server not on 127.0.0.1")
	}
	var public []string
	inner := m.client.Transport
	if inner == nil {
		inner = http.DefaultTransport
	}
	m.publicClient = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		public = append(public, r.URL.Host)
		return inner.RoundTrip(r)
	})}
	plugin(t, dir, "werkzeug", func(p *Manifest) { p.Kernel = m.kernel })
	plugin(t, dir, "helfer", func(p *Manifest) { p.Kernel = m.kernel })
	list := map[string]any{"plugins": []map[string]string{{"name": "werkzeug"}, {"name": "helfer", "url": other + "helfer.plugin.json"}}}
	data, _ := json.Marshal(list)
	os.WriteFile(filepath.Join(dir, "plugins.json"), data, 0o644)
	state, err := m.AddSource(ctx, base+"plugins.json")
	if err != nil || state.Plugins != 2 {
		t.Fatalf("source %+v %v", state, err)
	}
	if len(public) != 2 || strings.HasPrefix(public[0], "127.0.0.1") {
		t.Fatalf("public client used for %v, want the manifest and signature of helfer", public)
	}
	public = nil
	if err := m.InstallExternal(ctx, ExternalRequest{Source: state.ID, Name: "helfer", Volume: "data", Confirm: "helfer"}, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(public, func(h string) bool { return strings.HasPrefix(h, "localhost") }) || slices.ContainsFunc(public, func(h string) bool { return strings.HasPrefix(h, "127.0.0.1") }) {
		t.Fatalf("install of helfer used the public client for %v", public)
	}
	// A link the person typed may name the home network, and may carry a
	// query: signature and image are still next to the manifest.
	public = nil
	if _, err := m.InspectLink(ctx, other+"werkzeug.plugin.json"); err != nil || len(public) != 0 {
		t.Fatalf("own link: %v, public client used for %v", err, public)
	}
	if err := m.InstallExternal(ctx, ExternalRequest{URL: base + "werkzeug.plugin.json?ref=main", Volume: "data", Confirm: "werkzeug"}, []string{"data"}); err != nil {
		t.Fatalf("link with a query: %v", err)
	}
}

// The official catalog is shown while a source still has not answered, and
// sources are read side by side, not one after the other.
func TestSlowSourceDoesNotHoldBackTheCatalog(t *testing.T) {
	f, dir, base := community(t)
	m := f.m
	ctx := context.Background()
	plugin(t, dir, "werkzeug", nil)
	data, _ := json.Marshal(map[string]any{"plugins": []map[string]string{{"name": "werkzeug"}}})
	os.WriteFile(filepath.Join(dir, "plugins.json"), data, 0o644)
	fast, err := m.AddSource(ctx, base+"plugins.json")
	if err != nil {
		t.Fatal(err)
	}
	// Two sources that only answer once the test lets them.
	release := make(chan struct{})
	var waiting sync.WaitGroup
	waiting.Add(2)
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		waiting.Done()
		<-release
		http.NotFound(w, r)
	}))
	t.Cleanup(slow.Close)
	m.mu.Lock()
	m.sources = append(m.sources, Source{ID: "slow-1", URL: slow.URL + "/a.json"}, Source{ID: "slow-2", URL: slow.URL + "/b.json"})
	m.mu.Unlock()

	done := make(chan error, 1)
	go func() { done <- m.RefreshCatalog(ctx, true) }()
	// Both slow sources are asked at the same time...
	asked := make(chan struct{})
	go func() { waiting.Wait(); close(asked) }()
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("the sources are not read side by side")
	}
	// ... and meanwhile the catalog is there.
	if status, plugins := m.catalogSnapshot(); status.State != "ready" || len(plugins) == 0 {
		close(release)
		t.Fatalf("catalog while sources load: %+v", status)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	states := m.Status([]string{"data"}).Sources
	errorsOf := map[string]string{}
	for _, s := range states {
		errorsOf[s.ID] = s.Error
	}
	if errorsOf[fast.ID] != "" || errorsOf["slow-1"] == "" || errorsOf["slow-2"] == "" {
		t.Fatalf("source states %+v", states)
	}
}
