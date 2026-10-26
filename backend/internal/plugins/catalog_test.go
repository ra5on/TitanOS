package plugins

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type catalogFixture struct {
	m            *Manager
	runner       *fakeRunner
	dir, key     string
	mu           sync.Mutex
	requests     []string
	imageHandler http.HandlerFunc
}

func newCatalogFixture(t *testing.T) *catalogFixture {
	t.Helper()
	if _, err := exec.LookPath("minisign"); err != nil {
		t.Skip("minisign unavailable")
	}
	m, runner := newTestManager(t)
	m.Release = "0.1.0-beta.3"
	root := t.TempDir()
	f := &catalogFixture{m: m, runner: runner, dir: filepath.Join(root, "releases/download/v"+m.Release), key: filepath.Join(t.TempDir(), "test.key")}
	for _, dir := range []string{f.dir, m.keyDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.command(t, "-G", "-W", "-s", f.key, "-p", filepath.Join(m.keyDir, "test.pub"))
	path := plugin(t, f.dir, "intel-gpu-tools", func(p *Manifest) { p.Version, p.Kernel = m.Release, m.kernel })
	f.sign(t, filepath.Base(path))
	f.release(t, m.Release, "intel-gpu-tools.plugin.json")
	files := http.FileServer(http.Dir(root))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.requests = append(f.requests, r.URL.Path)
		handler := f.imageHandler
		f.mu.Unlock()
		if strings.HasSuffix(r.URL.Path, ".raw") && handler != nil {
			handler(w, r)
			return
		}
		files.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	m.releaseSource, m.client = server.URL, server.Client()
	return f
}

func (f *catalogFixture) command(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("minisign", args...).CombinedOutput(); err != nil {
		t.Fatalf("minisign: %v %s", err, out)
	}
}

func (f *catalogFixture) sign(t *testing.T, name string) {
	t.Helper()
	f.command(t, "-S", "-s", f.key, "-m", filepath.Join(f.dir, name))
}

func (f *catalogFixture) write(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(f.dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *catalogFixture) release(t *testing.T, version, asset string) {
	t.Helper()
	data, err := json.Marshal(map[string]any{"version": version, "tag": "v" + version, "kernel": f.m.kernel,
		"plugins": []map[string]string{{"name": "intel-gpu-tools", "asset": asset}}})
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, "manifest.json", data)
	f.sign(t, "manifest.json")
}

func (f *catalogFixture) count(images bool) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, path := range f.requests {
		if !images || strings.HasSuffix(path, ".raw") {
			n++
		}
	}
	return n
}

func (f *catalogFixture) noInstall(t *testing.T) {
	t.Helper()
	if len(f.m.installed("data")) != 0 || len(f.runner.commands) != 0 {
		t.Fatal("failed download installed or activated a plugin")
	}
	stages, err := filepath.Glob(filepath.Join(f.m.baseDir("data"), ".download-*"))
	if err != nil || len(stages) != 0 {
		t.Fatalf("download left temporary files: %v %v", stages, err)
	}
}

func TestCatalogLoadsSignedMetadataOnceAndInstallsOnDemand(t *testing.T) {
	f := newCatalogFixture(t)
	m := f.m
	ctx := context.Background()
	if len(m.Status(nil).Plugins) != 0 || f.count(false) != 0 {
		t.Fatal("status made a network request")
	}
	if err := m.RefreshCatalog(ctx, false); err != nil {
		t.Fatal(err)
	}
	status := m.Status(nil)
	if status.Catalog.State != "ready" || len(status.Plugins) != 1 || !status.Plugins[0].Download || !status.Plugins[0].Official || status.Plugins[0].Installed {
		t.Fatalf("catalog not available without a volume: %+v", status)
	}
	if f.count(true) != 0 {
		t.Fatal("browsing downloaded the image")
	}
	requests := f.count(false)
	for range 3 {
		m.Status([]string{"data"})
		if err := m.RefreshCatalog(ctx, false); err != nil {
			t.Fatal(err)
		}
	}
	if f.count(false) != requests {
		t.Fatal("polling/refocusing repeatedly downloaded the catalog")
	}
	// A loose upload with the same name must not shadow the official catalog.
	plugin(t, filepath.Join(m.mountRoot, "data/media"), "intel-gpu-tools", nil)
	if got := m.Status([]string{"data"}).Plugins; len(got) != 1 || !got[0].Download {
		t.Fatalf("shadowed catalog: %+v", got)
	}
	if err := m.InstallOfficial(ctx, "intel-gpu-tools", "data", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	got := m.Status([]string{"data"}).Plugins
	if len(got) != 1 || !got[0].Installed || !got[0].Active || !got[0].Official || got[0].Download {
		t.Fatalf("installation: %+v", got)
	}
	if f.count(true) != 1 {
		t.Fatal("image was not downloaded exactly once")
	}
	if stages, _ := filepath.Glob(filepath.Join(m.baseDir("data"), ".download-*")); len(stages) != 0 {
		t.Fatal("staging files left after success")
	}
	if _, err := os.Stat(filepath.Join(m.releaseDir("data", m.Release), "intel-gpu-tools.raw")); err != nil {
		t.Fatal(err)
	}
	for _, path := range f.requests {
		if !strings.HasPrefix(path, "/releases/download/v"+m.Release+"/") {
			t.Fatalf("used a different/latest release: %s", path)
		}
	}
}

func TestCachedCatalogAllowsIdenticalSignedLocalFileOffline(t *testing.T) {
	f := newCatalogFixture(t)
	m := f.m
	ctx := context.Background()
	if err := m.RefreshCatalog(ctx, false); err != nil {
		t.Fatal(err)
	}
	share := filepath.Join(m.mountRoot, "data/media")
	if err := os.MkdirAll(share, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{".raw", manifestSuffix, manifestSuffix + ".minisig"} {
		name := "intel-gpu-tools" + suffix
		if err := copyFile(filepath.Join(f.dir, name), filepath.Join(share, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	os.Remove(filepath.Join(f.dir, "manifest.json"))
	if err := m.RefreshCatalog(ctx, true); err == nil {
		t.Fatal("expected unavailable release")
	}
	got := m.Status([]string{"data"}).Plugins
	if len(got) != 1 || got[0].Download || !got[0].Official || got[0].Path == "" {
		t.Fatalf("cached catalog hid verified local copy: %+v", got)
	}
	requests := f.count(false)
	if err := m.Install(ctx, got[0].Path, "data", "", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if f.count(false) != requests || !m.Status([]string{"data"}).Plugins[0].Active {
		t.Fatal("offline installation failed or contacted GitHub")
	}
}

func TestCatalogRejectsUntrustedOrMismatchedMetadata(t *testing.T) {
	for _, scenario := range []string{"release signature", "plugin signature", "release version", "plugin version", "plugin kernel", "asset path", "missing signature", "oversized metadata"} {
		t.Run(scenario, func(t *testing.T) {
			f := newCatalogFixture(t)
			switch scenario {
			case "release signature":
				f.write(t, "manifest.json.minisig", []byte("invalid"))
			case "plugin signature":
				f.write(t, "intel-gpu-tools.plugin.json.minisig", []byte("invalid"))
			case "missing signature":
				os.Remove(filepath.Join(f.dir, "intel-gpu-tools.plugin.json.minisig"))
			case "release version":
				f.release(t, "9.9.9", "intel-gpu-tools.plugin.json")
			case "asset path":
				f.release(t, f.m.Release, "../../private")
			case "oversized metadata":
				f.write(t, "intel-gpu-tools.plugin.json", []byte(strings.Repeat(" ", (64<<10)+1)))
			default:
				plugin(t, f.dir, "intel-gpu-tools", func(p *Manifest) {
					p.Version, p.Kernel = f.m.Release, f.m.kernel
					if scenario == "plugin version" {
						p.Version = "9.9.9"
					} else {
						p.Kernel = "other"
					}
				})
				f.sign(t, "intel-gpu-tools.plugin.json")
			}
			if err := f.m.RefreshCatalog(context.Background(), true); err == nil {
				t.Fatal("accepted invalid catalog")
			}
			if len(f.m.Status(nil).Plugins) != 0 {
				t.Fatal("published untrusted entries")
			}
			if err := f.m.InstallOfficial(context.Background(), "intel-gpu-tools", "data", []string{"data"}); err == nil {
				t.Fatal("installed invalid catalog")
			}
			if f.count(true) != 0 {
				t.Fatal("downloaded image before verifying metadata")
			}
			f.noInstall(t)
		})
	}
}

func TestCatalogKeepsLocalPluginsAndCanRetryAfterFailure(t *testing.T) {
	f := newCatalogFixture(t)
	plugin(t, filepath.Join(f.m.mountRoot, "data/media"), "local", nil)
	data, _ := os.ReadFile(filepath.Join(f.dir, "manifest.json.minisig"))
	f.write(t, "manifest.json.minisig", []byte("invalid"))
	if err := f.m.RefreshCatalog(context.Background(), false); err == nil {
		t.Fatal("expected failure")
	}
	status := f.m.Status([]string{"data"})
	if status.Catalog.State != "error" || len(status.Plugins) != 1 || status.Plugins[0].Name != "local" {
		t.Fatalf("local plugin disappeared: %+v", status)
	}
	f.write(t, "manifest.json.minisig", data)
	if err := f.m.RefreshCatalog(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if status = f.m.Status([]string{"data"}); status.Catalog.State != "ready" || len(status.Plugins) != 2 {
		t.Fatalf("retry failed: %+v", status)
	}
	f.write(t, "manifest.json.minisig", []byte("invalid"))
	if err := f.m.RefreshCatalog(context.Background(), true); err == nil {
		t.Fatal("expected refresh failure")
	}
	if len(f.m.Status([]string{"data"}).Plugins) != 2 {
		t.Fatal("lost previously verified catalog")
	}
	if err := f.m.InstallOfficial(context.Background(), "intel-gpu-tools", "data", []string{"data"}); err == nil {
		t.Fatal("trusted stale metadata at installation")
	}
	f.noInstall(t)
}

func TestCatalogRefusesWrongKernelAndUnavailableVolume(t *testing.T) {
	f := newCatalogFixture(t)
	m := f.m
	if err := m.InstallOfficial(context.Background(), "intel-gpu-tools", "absent", []string{"data"}); !errors.Is(err, ErrUnknownVolume) {
		t.Fatal(err)
	}
	if f.count(false) != 0 {
		t.Fatal("contacted GitHub without a destination volume")
	}
	m.kernel = "different-running-kernel"
	if err := m.RefreshCatalog(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if got := m.Status(nil).Plugins[0]; got.Problem != "kernel" {
		t.Fatalf("wrong kernel not shown: %+v", got)
	}
	if err := m.InstallOfficial(context.Background(), "intel-gpu-tools", "data", []string{"data"}); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
	if f.count(true) != 0 {
		t.Fatal("downloaded incompatible image")
	}
	f.noInstall(t)
}

func TestCatalogDevelopmentBuildAndMissingRelease(t *testing.T) {
	f := newCatalogFixture(t)
	f.m.Release = "c7111bc-dirty"
	if err := f.m.RefreshCatalog(context.Background(), true); !errors.Is(err, ErrRelease) {
		t.Fatal(err)
	}
	if status := f.m.Status(nil); status.Catalog.State != "development" || f.count(false) != 0 {
		t.Fatalf("development build contacted release server: %+v", status)
	}
	f.m.Release = "8.0.0"
	if err := f.m.RefreshCatalog(context.Background(), true); !errors.Is(err, ErrRelease) {
		t.Fatal(err)
	}
	if f.m.Status(nil).Catalog.State != "unavailable" {
		t.Fatal("missing release not explained")
	}
}

func TestOfficialDownloadFailureNeverActivatesOrLeavesStagingFiles(t *testing.T) {
	for _, scenario := range []string{"checksum", "oversized", "interrupted", "update starts"} {
		t.Run(scenario, func(t *testing.T) {
			f := newCatalogFixture(t)
			data, _ := os.ReadFile(filepath.Join(f.dir, "intel-gpu-tools.raw"))
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var updating atomic.Bool
			f.m.ChangeAllowed = func() bool { return !updating.Load() }
			f.mu.Lock()
			f.imageHandler = func(w http.ResponseWriter, r *http.Request) {
				switch scenario {
				case "checksum":
					w.Write([]byte(strings.Repeat("x", len(data))))
				case "oversized":
					w.(http.Flusher).Flush()
					w.Write(append(data, 'x'))
				case "interrupted":
					w.Write(data[:1])
					w.(http.Flusher).Flush()
					cancel()
					<-r.Context().Done()
				case "update starts":
					updating.Store(true)
					w.Write(data)
				}
			}
			f.mu.Unlock()
			if err := f.m.InstallOfficial(ctx, "intel-gpu-tools", "data", []string{"data"}); err == nil {
				t.Fatal("failed transfer/update race was accepted")
			}
			f.noInstall(t)
		})
	}
}

func TestOfficialInstallCanDeferActivation(t *testing.T) {
	f := newCatalogFixture(t)
	if err := f.m.InstallOfficialWithActivation(context.Background(), "intel-gpu-tools", "data", []string{"data"}, false); err != nil {
		t.Fatal(err)
	}
	items := f.m.Status([]string{"data"}).Plugins
	if len(items) != 1 || !items[0].Installed || !items[0].Official || items[0].Enabled || items[0].Active {
		t.Fatalf("signed deferred installation: %+v", items)
	}
	if f.count(true) != 1 || len(f.runner.commands) != 0 {
		t.Fatalf("download count %d, system commands %v", f.count(true), f.runner.commands)
	}
}

// A large image on a slow line must not hold the plugin lock: the page polls
// Status, Tailscale uses RuntimeAction and VM autostart asks Healthy.
func TestOfficialDownloadKeepsStatusResponsive(t *testing.T) {
	f := newCatalogFixture(t)
	data, _ := os.ReadFile(filepath.Join(f.dir, "intel-gpu-tools.raw"))
	started, release := make(chan struct{}), make(chan struct{})
	f.mu.Lock()
	f.imageHandler = func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		w.Write(data)
	}
	f.mu.Unlock()
	installed := make(chan error, 1)
	go func() {
		installed <- f.m.InstallOfficialWithActivation(context.Background(), "intel-gpu-tools", "data", []string{"data"}, false)
	}()
	<-started

	answered := make(chan struct{})
	go func() {
		f.m.Status([]string{"data"})
		f.m.Healthy()
		f.m.RuntimeAction("tailscale", func() error { return nil })
		close(answered)
	}()
	select {
	case <-answered:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("status, health or runtime actions waited for the download")
	}
	if err := f.m.InstallOfficial(context.Background(), "intel-gpu-tools", "data", []string{"data"}); !errors.Is(err, ErrDownloading) {
		t.Fatalf("second download of the same plugin: %v", err)
	}

	close(release)
	if err := <-installed; err != nil {
		t.Fatal(err)
	}
	if f.count(true) != 1 {
		t.Fatalf("image downloaded %d times", f.count(true))
	}
	if items := f.m.Status([]string{"data"}).Plugins; len(items) != 1 || !items[0].Installed || !items[0].Official {
		t.Fatalf("after the download: %+v", items)
	}
}

// Choosing another plugin volume during the download is checked again
// before anything is installed.
func TestOfficialDownloadRechecksVolumeAfterwards(t *testing.T) {
	f := newCatalogFixture(t)
	os.MkdirAll(filepath.Join(f.m.mountRoot, "other"), 0o755)
	data, _ := os.ReadFile(filepath.Join(f.dir, "intel-gpu-tools.raw"))
	f.mu.Lock()
	f.imageHandler = func(w http.ResponseWriter, r *http.Request) {
		settings := f.m.current()
		settings.Volume = "other"
		if err := f.m.save(settings); err != nil {
			t.Error(err)
		}
		w.Write(data)
	}
	f.mu.Unlock()
	err := f.m.InstallOfficial(context.Background(), "intel-gpu-tools", "data", []string{"data", "other"})
	if !errors.Is(err, ErrUnknownVolume) {
		t.Fatalf("install after a volume change: %v", err)
	}
	if len(f.m.installed("data")) != 0 || len(f.m.installed("other")) != 0 {
		t.Fatal("plugin installed although its volume changed during the download")
	}
}
