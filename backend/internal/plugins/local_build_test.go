package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func localFixture(t *testing.T) (*catalogFixture, Manifest, string) {
	t.Helper()
	f, _ := driverFixture(t)
	m := f.m
	if err := m.ensureBuildKey(); err != nil {
		t.Fatal(err)
	}
	image := []byte("locally compiled kernel modules")
	sum := sha256.Sum256(image)
	p := Manifest{LocalBuild: true, Name: "nvidia", Title: "NVIDIA", Version: "615.71.09-titanos-" + m.Release + "-local", TitanOS: m.Release, Kernel: m.kernel, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(image))}
	dir := filepath.Join(m.driverCache("data"), p.SHA256)
	os.MkdirAll(dir, 0700)
	raw, _ := json.Marshal(p)
	digest := sha256.Sum256(raw)
	receipt := buildReceipt{ManifestSHA256: hex.EncodeToString(digest[:]), SDK: strings.Repeat("a", 64), PackagesSHA256: strings.Repeat("b", 64)}
	receipt.MAC = m.receiptMAC(receipt)
	data, _ := json.Marshal(receipt)
	for name, content := range map[string][]byte{"nvidia.raw": image, "nvidia.plugin.json": raw, localReceipt: data, ".ready": []byte(m.Release)} {
		if err := os.WriteFile(filepath.Join(dir, name), content, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return f, p, dir
}
func TestLocalBuildTrustAndTampering(t *testing.T) {
	f, p, dir := localFixture(t)
	m := f.m
	if got, err := m.cachedDriver(dir, true); err != nil || got.SHA256 != p.SHA256 {
		t.Fatalf("%+v %v", got, err)
	}
	if m.official(filepath.Join(dir, "nvidia.plugin.json")) {
		t.Fatal("local receipt became official")
	}
	for _, file := range []string{"nvidia.plugin.json", localReceipt, "nvidia.raw"} {
		path := filepath.Join(dir, file)
		original, _ := os.ReadFile(path)
		os.WriteFile(path, append(original, ' '), 0600)
		_, err := m.cachedDriver(dir, true)
		if file == localReceipt {
			var r buildReceipt
			json.Unmarshal(original, &r)
			r.SDK = strings.Repeat("c", 64)
			raw, _ := json.Marshal(r)
			os.WriteFile(path, raw, 0600)
			_, err = m.cachedDriver(dir, true)
		}
		if err == nil {
			t.Fatal("tampering accepted", file)
		}
		os.WriteFile(path, original, 0600)
	}
	m.buildKey = make([]byte, 32)
	if _, err := m.cachedDriver(dir, true); err == nil {
		t.Fatal("another NAS accepted local build")
	}
}
func TestLocalBuildSelectOnlyAfterReboot(t *testing.T) {
	f, p, dir := localFixture(t)
	m := f.m
	before := m.dir("data")
	f.runner.commands = nil
	if err := m.SelectDriver(p.SHA256, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if m.dir("data") != before || len(f.runner.commands) != 0 {
		t.Fatal("selection changed live system")
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if m.dir("data") != before {
		t.Fatal("daemon restart activated driver")
	}
	m.bootID = "boot-two"
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if _, err := m.localDriver(m.dir("data")); err != nil {
		t.Fatal("receipt missing in selected set", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "nvidia.raw")); err != nil {
		t.Fatal(err)
	}
	if err := m.SelectDriver("bundled", []string{"data"}); err != nil {
		t.Fatal("fallback", err)
	}
}
func TestBuildEnvironmentMustBeSignedAndExact(t *testing.T) {
	f, _ := driverFixture(t)
	m := f.m
	sdk := buildSDK{Asset: "nvidia-build-sdk.tar.zst", Kernel: m.kernel, Version: m.Release, SHA256: strings.Repeat("a", 64), Size: 100}
	publish := func(s buildSDK) {
		raw, _ := json.Marshal(map[string]any{"version": m.Release, "tag": "v" + m.Release, "kernel": m.kernel, "nvidiaBuildSDK": s})
		f.write(t, "manifest.json", raw)
		f.sign(t, "manifest.json")
	}
	publish(sdk)
	if _, err := m.fetchBuildSDK(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []buildSDK{{Asset: "../sdk", Kernel: m.kernel, Version: m.Release, SHA256: sdk.SHA256, Size: 100}, {Asset: sdk.Asset, Kernel: "wrong", Version: m.Release, SHA256: sdk.SHA256, Size: 100}, {Asset: sdk.Asset, Kernel: m.kernel, Version: m.Release, SHA256: sdk.SHA256, Size: 3 << 30}} {
		publish(bad)
		if _, err := m.fetchBuildSDK(context.Background()); err == nil {
			t.Fatal("bad SDK accepted")
		}
	}
	publish(sdk)
	f.write(t, "manifest.json.minisig", []byte("forged"))
	if _, err := m.fetchBuildSDK(context.Background()); err == nil {
		t.Fatal("forged SDK accepted")
	}
}
func TestLocalBuildRunsOfflineWithBounds(t *testing.T) {
	args := strings.Join(buildCommand("/mnt/data/.titanos/nvidia-build/root", "build"), "\n")
	for _, want := range []string{"PrivateNetwork=yes", "CapabilityBoundingSet=", "ReadWritePaths=/work", "MemoryMax=2G", "CPUQuota=200%", "RuntimeMaxSec=7200", "NoNewPrivileges=yes", "ProtectSystem=strict"} {
		if !strings.Contains(args, want) {
			t.Fatal("missing isolation", want)
		}
	}
	if strings.Contains(args, "modprobe") {
		t.Fatal("build loads live driver")
	}
}
func TestManualBuildIsOnlyQueued(t *testing.T) {
	f, _ := driverFixture(t)
	m := f.m
	m.LocalBuilds = true
	f.runner.commands = nil
	if err := m.RequestDriverBuild([]string{"data"}); err != nil {
		t.Fatal(err)
	}
	if len(f.runner.commands) != 0 {
		t.Fatal("request performed synchronous build")
	}
	if action := <-m.driverRequests; action != "prepare" {
		t.Fatal(action)
	}
	if err := m.RequestDriverUpdate("", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if action := <-m.driverRequests; action == "prepare" {
		t.Fatal("check implicitly builds")
	}
}

func TestLocalBuildOSUpdateUsesNewSignedBundledDriver(t *testing.T) {
	f, p, _ := localFixture(t)
	m := f.m
	oldRelease := m.Release
	if err := m.SelectDriver(p.SHA256, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	m.bootID = "second-boot"
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	oldDir := m.dir("data")
	replacement := plugin(t, f.dir, "nvidia", func(p *Manifest) { p.Kernel = "new-kernel"; p.Version = "1.0.0" })
	f.sign(t, filepath.Base(replacement))
	fetch := func(_ context.Context, url string, _ int64) ([]byte, error) {
		return os.ReadFile(filepath.Join(f.dir, strings.TrimPrefix(url, "https://release/")))
	}
	download := func(_ context.Context, url, dest, sum string, size int64) error {
		return copyFile(filepath.Join(f.dir, strings.TrimPrefix(url, "https://release/")), dest, 0600)
	}
	if err := m.PrepareUpdate(context.Background(), "1.0.0", "new-kernel", "https://release/", map[string]string{"nvidia": "nvidia.plugin.json"}, []string{"data"}, fetch, download); err != nil {
		t.Fatal(err)
	}
	if m.dir("data") != oldDir {
		t.Fatal("staging changed running driver")
	}
	m.Release = "1.0.0"
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if got := m.installed("data")["nvidia"]; got.LocalBuild || got.Kernel != "new-kernel" || !m.official(filepath.Join(m.dir("data"), "nvidia.plugin.json")) {
		t.Fatalf("wrong replacement %+v", got)
	}
	m.Release = oldRelease
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if m.dir("data") != oldDir {
		t.Fatal("rollback lost selected local set")
	}
	if _, err := m.localDriver(oldDir); err != nil {
		t.Fatal("rollback lost local trust", err)
	}
}
