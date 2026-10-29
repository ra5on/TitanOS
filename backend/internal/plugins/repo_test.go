package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoFixture serves a plugin repository next to the release of the
// catalog fixture, under /plugins.
type repoFixture struct {
	*catalogFixture
	root    string // the repository's releases/download
	sets    map[string]repoSetRef
	common  string
	serial  uint64
	minBase string
}

func newRepoFixture(t *testing.T) *repoFixture {
	t.Helper()
	f := newCatalogFixture(t)
	server := strings.TrimSuffix(f.m.releaseSource, "/")
	f.m.pluginSource = server + "/plugins"
	// The release fixture lies at <root>/releases/download/v<version>.
	root := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(f.dir))), "plugins", "releases", "download")
	return &repoFixture{catalogFixture: f, root: root, sets: map[string]repoSetRef{}, minBase: "0.1.0-beta.1"}
}

func (r *repoFixture) signFile(t *testing.T, path string) {
	t.Helper()
	r.command(t, "-S", "-s", r.key, "-m", path)
}

func sha(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// publish writes a set: one plugin per change, signed manifests and a signed
// index. kernel empty makes the common set.
func (r *repoFixture) publish(t *testing.T, tag, kernel, kernelPackage string, changes map[string]func(*Manifest)) {
	t.Helper()
	dir := filepath.Join(r.root, tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	index := repoIndex{Tag: tag, Debian: "trixie", SysextLevel: "1", Kernel: kernel, KernelPackage: kernelPackage, MinBase: r.minBase}
	for name, change := range changes {
		path := plugin(t, dir, name, func(p *Manifest) {
			p.Kernel = kernel
			if change != nil {
				change(p)
			}
		})
		r.signFile(t, path)
		data, _ := os.ReadFile(path)
		manifest, _ := parseManifest(data, name+manifestSuffix)
		index.Plugins = append(index.Plugins, repoIndexItem{Name: name, Version: manifest.Version, Manifest: sha(data), SHA256: manifest.SHA256, Size: manifest.Size})
	}
	data, _ := json.Marshal(index)
	path := filepath.Join(dir, "index.json")
	os.WriteFile(path, data, 0o644)
	r.signFile(t, path)
	ref := repoSetRef{Kernel: kernel, KernelPackage: kernelPackage, Tag: tag, Index: sha(data)}
	if kernel == "" {
		r.common = tag
	}
	r.sets[tag] = ref
}

func (r *repoFixture) pointer(t *testing.T) {
	t.Helper()
	r.serial++
	pointer := repoPointer{Serial: r.serial, Debian: "trixie", SysextLevel: "1"}
	for tag, ref := range r.sets {
		if tag == r.common {
			ref := ref
			pointer.Common = &ref
		} else if ref.Kernel != "" {
			pointer.Kernels = append(pointer.Kernels, ref)
		}
	}
	dir := filepath.Join(r.root, "catalog")
	os.MkdirAll(dir, 0o755)
	data, _ := json.Marshal(pointer)
	path := filepath.Join(dir, "catalog.json")
	os.WriteFile(path, data, 0o644)
	r.signFile(t, path)
}

func (r *repoFixture) catalog(t *testing.T) map[string]Plugin {
	t.Helper()
	if err := r.m.RefreshCatalog(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	plugins := map[string]Plugin{}
	for _, p := range r.m.Status([]string{"data"}).Plugins {
		plugins[p.Name] = p
	}
	return plugins
}

func TestRepoCatalogPrefersExactKernelSetAndCommonSet(t *testing.T) {
	r := newRepoFixture(t)
	m := r.m
	r.publish(t, "common-1", "", "", map[string]func(*Manifest){"tailscale": func(p *Manifest) { p.Version = "1.90.0-1" }})
	r.publish(t, "kernel-6.12.0-1-1", m.kernel, m.kernelPackage, map[string]func(*Manifest){"intel-gpu-tools": func(p *Manifest) { p.Version = "1.30-1" }})
	// Same kernel release, other Debian package: never chosen.
	r.publish(t, "kernel-6.12.0-2-1", m.kernel, "6.12.0-2", map[string]func(*Manifest){"intel-gpu-tools": func(p *Manifest) { p.Version = "9.9-1" }})
	r.pointer(t)
	plugins := r.catalog(t)
	if len(plugins) != 2 || plugins["tailscale"].Version != "1.90.0-1" || plugins["intel-gpu-tools"].Version != "1.30-1" {
		t.Fatalf("catalog: %+v", plugins)
	}
	if !strings.Contains(plugins["intel-gpu-tools"].LicenseURL, "/plugins/releases/download/kernel-6.12.0-1-1/") {
		t.Fatalf("license from the wrong set: %s", plugins["intel-gpu-tools"].LicenseURL)
	}
	if err := m.InstallOfficial(context.Background(), "tailscale", "data", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if got := m.installed("data")["tailscale"]; got.Version != "1.90.0-1" || !m.official(filepath.Join(m.dir("data"), "tailscale"+manifestSuffix)) {
		t.Fatalf("installed %+v", got)
	}
	found := false
	for _, path := range r.requests {
		if path == "/plugins/releases/download/common-1/tailscale.raw" {
			found = true
		}
		if strings.HasSuffix(path, ".raw") && strings.HasPrefix(path, "/releases/") {
			t.Fatalf("image from the TitanOS release: %s", path)
		}
	}
	if !found {
		t.Fatalf("image not from the common set: %v", r.requests)
	}
}

func TestRepoWithoutKernelSetKeepsKernelPluginsOfTheRelease(t *testing.T) {
	r := newRepoFixture(t)
	r.publish(t, "common-1", "", "", map[string]func(*Manifest){"tailscale": nil})
	r.pointer(t)
	plugins := r.catalog(t)
	if len(plugins) != 2 || plugins["intel-gpu-tools"].Version != r.m.Release || plugins["tailscale"].Name == "" {
		t.Fatalf("catalog: %+v", plugins)
	}
	if !strings.Contains(plugins["intel-gpu-tools"].LicenseURL, "/releases/download/v"+r.m.Release+"/") {
		t.Fatal("kernel plugin did not come from the release")
	}
}

func TestRepoMissingOrNotForThisBaseFallsBackToRelease(t *testing.T) {
	r := newRepoFixture(t)
	// No catalog.json at all.
	if plugins := r.catalog(t); len(plugins) != 1 || plugins["intel-gpu-tools"].Version != r.m.Release {
		t.Fatalf("catalog without repository: %+v", plugins)
	}
	// Sets meant for newer bases are not used.
	r.minBase = "9.0.0"
	r.publish(t, "common-1", "", "", map[string]func(*Manifest){"tailscale": nil})
	r.pointer(t)
	if plugins := r.catalog(t); len(plugins) != 1 || plugins["tailscale"].Name != "" {
		t.Fatalf("catalog with sets for newer systems: %+v", plugins)
	}
}

func TestRepoRejectsTamperingAndRollback(t *testing.T) {
	r := newRepoFixture(t)
	ctx := context.Background()
	r.publish(t, "common-1", "", "", map[string]func(*Manifest){"tailscale": nil})
	r.pointer(t)
	r.pointer(t) // serial 2
	r.catalog(t)
	// An older signed catalog is refused, not used as a fallback.
	pointer := filepath.Join(r.root, "catalog", "catalog.json")
	data, _ := os.ReadFile(pointer)
	old := strings.Replace(string(data), `"serial":2`, `"serial":1`, 1)
	os.WriteFile(pointer, []byte(old), 0o644)
	r.signFile(t, pointer)
	if err := r.m.RefreshCatalog(ctx, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("rollback accepted: %v", err)
	}
	var saved struct{ Serial uint64 }
	if r.m.store.ReadJSON(catalogSerialPath, &saved) != nil || saved.Serial != 2 {
		t.Fatalf("serial not kept: %+v", saved)
	}
	// An index that does not match the catalog.
	r.serial = 2
	index := filepath.Join(r.root, "common-1", "index.json")
	data, _ = os.ReadFile(index)
	os.WriteFile(index, append(data, ' '), 0o644)
	r.signFile(t, index)
	r.pointer(t) // still names the hash of the index before the change
	if err := r.m.RefreshCatalog(ctx, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("index hash ignored: %v", err)
	}
	// A plugin manifest that differs from its index, though signed.
	r.publish(t, "common-2", "", "", map[string]func(*Manifest){"tailscale": nil})
	r.pointer(t)
	manifest := filepath.Join(r.root, "common-2", "tailscale"+manifestSuffix)
	data, _ = os.ReadFile(manifest)
	os.WriteFile(manifest, []byte(strings.Replace(string(data), `"version":"1.0"`, `"version":"1.1"`, 1)), 0o644)
	r.signFile(t, manifest)
	if err := r.m.RefreshCatalog(ctx, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("manifest outside the index accepted: %v", err)
	}
	// A bad signature of the catalog is an error, not a fallback.
	r.pointer(t)
	os.WriteFile(pointer+".minisig", []byte("untrusted comment: x\nRWQ\n"), 0o644)
	if err := r.m.RefreshCatalog(ctx, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unsigned catalog: %v", err)
	}
	// So is a missing one: the catalog is there, only its signature is not.
	r.pointer(t)
	os.Remove(pointer + ".minisig")
	if err := r.m.RefreshCatalog(ctx, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("catalog without signature: %v", err)
	}
}

func TestRepoUpdateReplacesInstalledPluginWithNewerVersion(t *testing.T) {
	r := newRepoFixture(t)
	m := r.m
	ctx := context.Background()
	r.publish(t, "common-1", "", "", map[string]func(*Manifest){"tailscale": func(p *Manifest) { p.Version = "1.90.0-1" }})
	r.pointer(t)
	r.catalog(t)
	if err := m.InstallOfficial(ctx, "tailscale", "data", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Update(ctx, "tailscale", "", "", []string{"data"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("update to the same version: %v", err)
	}
	r.publish(t, "common-2", "", "", map[string]func(*Manifest){"tailscale": func(p *Manifest) { p.Version = "1.92.1-1" }})
	// Another image for the new version.
	dir := filepath.Join(r.root, "common-2")
	image := []byte("squashfs of tailscale 1.92")
	os.WriteFile(filepath.Join(dir, "tailscale.raw"), image, 0o644)
	r.publish2(t, dir, image)
	r.pointer(t)
	update := r.catalog(t)["tailscale"].Update
	if update == nil || !update.Download || update.Version != "1.92.1-1" || update.Path != "" {
		t.Fatalf("update not offered: %+v", update)
	}
	if err := m.Update(ctx, "tailscale", "", "", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	got := m.Status([]string{"data"}).Plugins
	for _, p := range got {
		if p.Name == "tailscale" && (p.Version != "1.92.1-1" || !p.Active || !p.Official || p.Update != nil) {
			t.Fatalf("after update: %+v", p)
		}
	}
	if stages, _ := filepath.Glob(filepath.Join(m.baseDir("data"), ".download-*")); len(stages) != 0 {
		t.Fatal("staging files left")
	}
}

// publish2 rewrites the tailscale manifest of a set for a changed image and
// the index for it.
func (r *repoFixture) publish2(t *testing.T, dir string, image []byte) {
	t.Helper()
	path := filepath.Join(dir, "tailscale"+manifestSuffix)
	data, _ := os.ReadFile(path)
	var manifest Manifest
	json.Unmarshal(data, &manifest)
	manifest.SHA256, manifest.Size = sha(image), int64(len(image))
	data, _ = json.Marshal(manifest)
	os.WriteFile(path, data, 0o644)
	r.signFile(t, path)
	tag := filepath.Base(dir)
	index := repoIndex{Tag: tag, Debian: "trixie", SysextLevel: "1", MinBase: r.minBase,
		Plugins: []repoIndexItem{{Name: "tailscale", Version: manifest.Version, Manifest: sha(data), SHA256: manifest.SHA256, Size: manifest.Size}}}
	data, _ = json.Marshal(index)
	os.WriteFile(filepath.Join(dir, "index.json"), data, 0o644)
	r.signFile(t, filepath.Join(dir, "index.json"))
	ref := r.sets[tag]
	ref.Index = sha(data)
	r.sets[tag] = ref
}

func TestRepoPrepareUpdateUsesSetOfTargetKernel(t *testing.T) {
	r := newRepoFixture(t)
	m := r.m
	ctx := context.Background()
	r.catalog(t)
	if err := m.InstallOfficial(ctx, "intel-gpu-tools", "data", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	r.publish(t, "kernel-6.13.0-1-1", "6.13.0-test", "6.13.0-1", map[string]func(*Manifest){"intel-gpu-tools": func(p *Manifest) { p.Version = "2.0-1" }})
	r.pointer(t)
	download := func(_ context.Context, url, dest, sum string, size int64) error {
		path, ok := strings.CutPrefix(url, m.pluginSource+"/releases/download/")
		if !ok {
			t.Fatalf("image not from the plugin repository: %s", url)
		}
		return copyFile(filepath.Join(r.root, path), dest, 0o600)
	}
	fetch := func(ctx context.Context, url string, limit int64) ([]byte, error) {
		return m.fetchMetadata(ctx, url, limit)
	}
	// A release that does not know the plugin repository brings no assets.
	if err := m.PrepareUpdate(ctx, "0.2.0", "6.13.0-test", "6.13.0-1", m.releaseSource+"/releases/download/v0.2.0/", nil, []string{"data"}, fetch, download); err != nil {
		t.Fatal(err)
	}
	prepared, err := readManifest(filepath.Join(m.releaseDir("data", "0.2.0"), "intel-gpu-tools"+manifestSuffix))
	if err != nil || prepared.Version != "2.0-1" || prepared.Kernel != "6.13.0-test" {
		t.Fatalf("prepared %+v %v", prepared, err)
	}
	// Without the package version, no kernel set fits and the kernel plugin
	// has no trusted build: the update is refused instead of dropping it.
	if err := m.PrepareUpdate(ctx, "0.2.1", "6.13.0-test", "", m.releaseSource+"/releases/download/v0.2.1/", nil, []string{"data"}, fetch, download); !errors.Is(err, ErrInvalid) {
		t.Fatalf("missing kernel set: %v", err)
	}
}

func TestKernelPackageAndOSRelease(t *testing.T) {
	version := "Linux version 6.12.113+deb13-amd64 (debian-kernel@lists.debian.org) (x86_64-linux-gnu-gcc-14 (Debian 14.2.0-19) 14.2.0, GNU ld (GNU Binutils for Debian) 2.44) #1 SMP PREEMPT_DYNAMIC Debian 6.12.113-1 (2026-10-15)"
	if got := kernelPackageOf(version); got != "6.12.113-1" {
		t.Fatalf("kernel package %q", got)
	}
	if got := kernelPackageOf("Linux version 6.1.0 #1 SMP"); got != "" {
		t.Fatalf("kernel package of a foreign kernel %q", got)
	}
	codename, level := osReleaseValues("NAME=\"TitanOS\"\nSYSEXT_LEVEL=1\nVERSION_CODENAME=trixie\n")
	if codename != "trixie" || level != "1" {
		t.Fatalf("os-release %q %q", codename, level)
	}
}

func TestDriverUpdatesComeFromKernelSet(t *testing.T) {
	r := newRepoFixture(t)
	ctx := context.Background()
	if base, err := r.m.driverBase(ctx); err != nil || !strings.HasSuffix(base, "/releases/download/v"+r.m.Release+"/") {
		t.Fatalf("without repository: %s %v", base, err)
	}
	r.publish(t, "kernel-6.12.0-1-1", r.m.kernel, r.m.kernelPackage, map[string]func(*Manifest){"intel-gpu-tools": nil})
	r.pointer(t)
	if base, err := r.m.driverBase(ctx); err != nil || base != r.m.pluginSource+"/releases/download/kernel-6.12.0-1-1/" {
		t.Fatalf("with kernel set: %s %v", base, err)
	}
}
