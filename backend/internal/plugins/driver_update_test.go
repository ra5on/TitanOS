package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func driverFixture(t *testing.T) (*catalogFixture, Manifest) {
	t.Helper()
	f := newCatalogFixture(t)
	m := f.m
	m.bootID = "boot-one"
	original := plugin(t, filepath.Join(m.mountRoot, "data/media"), "nvidia", func(p *Manifest) { p.Kernel = m.kernel; p.Version = "bundled" })
	f.command(t, "-S", "-s", f.key, "-m", original)
	if err := m.Install(context.Background(), original, "data", "", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	f.runner.commands = nil
	candidate := publishDriver(t, f, 1)
	return f, candidate
}
func publishDriver(t *testing.T, f *catalogFixture, revision uint64) Manifest {
	t.Helper()
	image := []byte{1, 2, 3, byte(revision)}
	hash := sha256.Sum256(image)
	p := Manifest{Name: "nvidia", Title: "NVIDIA", Version: "driver-" + string(rune('0'+revision)), TitanOS: f.m.Release, Kernel: f.m.kernel, Revision: revision, SHA256: hex.EncodeToString(hash[:]), Size: int64(len(image))}
	data, _ := json.Marshal(p)
	name := "nvidia-update-" + p.SHA256 + manifestSuffix
	f.write(t, name, data)
	f.sign(t, name)
	f.write(t, "nvidia-update-"+p.SHA256+".raw", image)
	publishDriverCatalog(t, f, []Manifest{p})
	return p
}
func publishDriverCatalog(t *testing.T, f *catalogFixture, manifests []Manifest) {
	t.Helper()
	entries := []DriverVersion{}
	for _, p := range manifests {
		entries = append(entries, DriverVersion{Version: p.Version, SHA256: p.SHA256, Revision: p.Revision, Size: p.Size})
	}
	data, _ := json.Marshal(map[string]any{"titanos": f.m.Release, "kernel": f.m.kernel, "versions": entries, "recommended": manifests[len(manifests)-1].SHA256})
	f.write(t, "nvidia-updates.json", data)
	f.sign(t, "nvidia-updates.json")
}
func TestDriverDownloadSelectionAndReboot(t *testing.T) {
	f, p := driverFixture(t)
	m := f.m
	ctx := context.Background()
	mounted := []string{"data"}
	original := m.dir("data")
	if err := m.CheckDriverUpdate(ctx, mounted); err != nil {
		t.Fatal(err)
	}
	if len(f.runner.commands) != 0 || m.installed("data")["nvidia"].Version != "bundled" {
		t.Fatal("download changed live driver")
	}
	if err := m.SelectDriver(p.SHA256, mounted); err != nil {
		t.Fatal(err)
	}
	if m.dir("data") != original || m.current().PendingDriver == nil {
		t.Fatal("choice switched live set")
	}
	if err := m.SetEnabled(ctx, "nvidia", false, mounted); !errors.Is(err, ErrDriverPending) {
		t.Fatal("mutation lost pending set", err)
	}
	if err := m.PrepareUpdate(ctx, "9.9.9", "", m.kernel, "", "", nil, mounted, nil, nil); !errors.Is(err, ErrDriverPending) {
		t.Fatal("OS update lost pending choice", err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if m.dir("data") != original {
		t.Fatal("titand restart activated driver")
	}
	if err := m.SelectDriver("cancel", mounted); err != nil {
		t.Fatal(err)
	}
	if m.current().PendingDriver != nil {
		t.Fatal("cancel did not persist")
	}
	if err := m.SelectDriver(p.SHA256, mounted); err != nil {
		t.Fatal(err)
	}
	m.bootID = "boot-two"
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if m.installed("data")["nvidia"].SHA256 != p.SHA256 || m.current().PendingDriver != nil {
		t.Fatal("new boot did not choose driver")
	}
	// Rollback is also an explicit next-boot choice.
	if err := m.SelectDriver("bundled", mounted); err != nil {
		t.Fatal(err)
	}
	if m.installed("data")["nvidia"].SHA256 != p.SHA256 {
		t.Fatal("rollback hot-swapped driver")
	}
	m.bootID = "boot-three"
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if m.installed("data")["nvidia"].Version != "bundled" {
		t.Fatal("bundled rollback failed")
	}
	if len(f.runner.commands) != 0 {
		t.Fatal("selection touched live modules", f.runner.commands)
	}
}
func TestDriverMenuCanDownloadOlderVersionWithoutActivating(t *testing.T) {
	f, old := driverFixture(t)
	latest := publishDriver(t, f, 2)
	publishDriverCatalog(t, f, []Manifest{old, latest})
	if err := f.m.CheckDriverUpdate(context.Background(), []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if err := f.m.checkDriverUpdate(context.Background(), []string{"data"}, old.SHA256); err != nil {
		t.Fatal(err)
	}
	status := f.m.Status([]string{"data"}).DriverUpdate
	if len(status.Versions) != 2 || !status.Versions[0].Downloaded || !status.Versions[1].Downloaded {
		t.Fatalf("versions %+v", status)
	}
	if err := f.m.SelectDriver(old.SHA256, []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if len(f.runner.commands) != 0 {
		t.Fatal("older version affected live driver")
	}
	// An attacker replaying an older signed catalog cannot silently reduce the floor.
	publishDriverCatalog(t, f, []Manifest{old})
	if err := f.m.CheckDriverUpdate(context.Background(), []string{"data"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("accepted catalog rollback", err)
	}
}
func TestDriverRejectsTamperingAndWrongTargets(t *testing.T) {
	for _, scenario := range []string{"catalog-signature", "plugin-signature", "wrong-kernel", "wrong-release", "image", "cached-image", "path"} {
		t.Run(scenario, func(t *testing.T) {
			f, p := driverFixture(t)
			m := f.m
			ctx := context.Background()
			mounted := []string{"data"}
			name := "nvidia-update-" + p.SHA256 + manifestSuffix
			switch scenario {
			case "catalog-signature":
				f.write(t, "nvidia-updates.json.minisig", []byte("bad"))
			case "plugin-signature":
				f.write(t, name+".minisig", []byte("bad"))
			case "wrong-kernel", "wrong-release":
				if scenario == "wrong-kernel" {
					p.Kernel = "other"
				} else {
					p.TitanOS = "9.9.9"
				}
				data, _ := json.Marshal(p)
				f.write(t, name, data)
				f.sign(t, name)
			case "image":
				f.write(t, "nvidia-update-"+p.SHA256+".raw", []byte("bad!"))
			case "cached-image":
				if err := m.CheckDriverUpdate(ctx, mounted); err != nil {
					t.Fatal(err)
				}
				os.WriteFile(filepath.Join(m.driverCache("data"), p.SHA256, "nvidia.raw"), []byte("bad!"), 0o600)
				if err := m.SelectDriver(p.SHA256, mounted); !errors.Is(err, ErrInvalid) {
					t.Fatal("selected changed image", err)
				}
			case "path":
				if err := m.SelectDriver("../../escape", mounted); !errors.Is(err, ErrInvalid) {
					t.Fatal(err)
				}
				return
			}
			if err := m.CheckDriverUpdate(ctx, mounted); err == nil {
				t.Fatal("accepted invalid driver")
			}
			if len(f.runner.commands) != 0 || m.current().PendingDriver != nil || m.installed("data")["nvidia"].Version != "bundled" {
				t.Fatal("failure changed installed driver")
			}
			stages, _ := filepath.Glob(filepath.Join(m.driverCache("data"), ".download-*"))
			if len(stages) != 0 {
				t.Fatal("partial download retained")
			}
		})
	}
}
func TestDriverPreferenceAndCachedSelectionSurviveOffline(t *testing.T) {
	f, p := driverFixture(t)
	m := f.m
	if err := m.SetAutomaticDrivers(false); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckDriverUpdate(context.Background(), []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	if m.driverSnapshot(m.current()).Automatic {
		t.Fatal("automatic preference lost")
	}
	os.Remove(filepath.Join(f.dir, "nvidia-updates.json"))
	if err := m.CheckDriverUpdate(context.Background(), []string{"data"}); !errors.Is(err, ErrRelease) {
		t.Fatal(err)
	}
	if err := m.SelectDriver(p.SHA256, []string{"data"}); err != nil {
		t.Fatal("offline selection failed", err)
	}
	m.ChangeAllowed = func() bool { return false }
	if err := m.SelectDriver("cancel", []string{"data"}); !errors.Is(err, ErrUpdatePending) {
		t.Fatal("system update race", err)
	}
}

func TestDriverRecommendationIsIndependentOfPublicationOrder(t *testing.T) {
	f, recommended := driverFixture(t)
	olderPublishedLater := publishDriver(t, f, 2)
	// The final entry is the explicit recommendation in this signed fixture.
	publishDriverCatalog(t, f, []Manifest{olderPublishedLater, recommended})
	if err := f.m.CheckDriverUpdate(context.Background(), []string{"data"}); err != nil {
		t.Fatal(err)
	}
	status := f.m.Status([]string{"data"}).DriverUpdate
	if status.SHA256 != recommended.SHA256 {
		t.Fatalf("downloaded the last publication instead of recommendation: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(f.m.driverCache("data"), olderPublishedLater.SHA256)); !os.IsNotExist(err) {
		t.Fatal("automatically downloaded older version")
	}
}
