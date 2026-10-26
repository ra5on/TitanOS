package plugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupThenInstallRequiresInitializedCurrentRelease(t *testing.T) {
	for _, ready := range []bool{false, true} {
		t.Run(map[bool]string{false: "incomplete-protected", true: "initialized-installable"}[ready], func(t *testing.T) {
			m, _ := newTestManager(t)
			m.Release = "0.23.1"
			settings := m.current()
			settings.Volume = "data"
			if err := m.save(settings); err != nil {
				t.Fatal(err)
			}
			current := m.releaseDir("data", m.Release)
			old := m.releaseDir("data", "0.0.0")
			for _, dir := range []string{current, old} {
				if err := os.MkdirAll(dir, 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "fixture.raw"), []byte("retain"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if ready {
				if err := os.WriteFile(filepath.Join(current, ".ready"), []byte(m.Release), 0600); err != nil {
					t.Fatal(err)
				}
			}
			list, err := m.OldReleases([]string{"data"})
			if err != nil {
				t.Fatal(err)
			}
			for _, release := range list {
				if release.Version == "0.0.0" {
					if err := m.Cleanup([]string{"data"}, []CleanupSelection{{Version: release.Version, Fingerprint: release.Fingerprint}}); err != nil {
						t.Fatal(err)
					}
				}
			}
			path := plugin(t, filepath.Join(m.mountRoot, "data/media"), "tools", nil)
			err = m.Install(context.Background(), path, "data", "tools", []string{"data"})
			if ready && err != nil {
				t.Fatalf("initialized cleanup fixture blocked the next install: %v", err)
			}
			if !ready && err == nil {
				t.Fatal("installation took over an incomplete existing directory")
			}
			if content, err := os.ReadFile(filepath.Join(current, "fixture.raw")); err != nil || string(content) != "retain" {
				t.Fatal("current release data was changed", err)
			}
		})
	}
}

func TestCleanupProtectsBootsChangedSelectionsAndPrivateDirectories(t *testing.T) {
	m, _ := newTestManager(t)
	m.Release = "0.23.0"
	settings := m.current()
	settings.Volume = "data"
	if err := m.save(settings); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"0.20.0", "0.21.0", "0.22.0", "0.23.0", "0.24.0"} {
		dir := m.releaseDir("data", version)
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "tools.raw"), []byte("image"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	m.CleanupProtected = func() ([]string, error) { return []string{"0.22.0", "0.24.0"}, nil }
	list, err := m.OldReleases([]string{"data"})
	if err != nil {
		t.Fatal(err)
	}
	byVersion := map[string]OldRelease{}
	for _, r := range list {
		byVersion[r.Version] = r
	}
	for _, version := range []string{"0.22.0", "0.23.0", "0.24.0"} {
		r := byVersion[version]
		if !r.Protected {
			t.Fatal("boot/current/pending release unprotected", version)
		}
		if err := m.Cleanup([]string{"data"}, []CleanupSelection{{Version: version, Fingerprint: r.Fingerprint}}); err == nil {
			t.Fatal("protected release removed")
		}
	}
	changed := byVersion["0.21.0"]
	os.WriteFile(filepath.Join(m.releaseDir("data", "0.21.0"), "tools.raw"), []byte("changed"), 0600)
	if err := m.Cleanup([]string{"data"}, []CleanupSelection{{Version: changed.Version, Fingerprint: changed.Fingerprint}}); err == nil {
		t.Fatal("stale preview accepted")
	}
	old := byVersion["0.20.0"]
	if err := m.Cleanup([]string{"data"}, []CleanupSelection{{Version: old.Version, Fingerprint: old.Fingerprint}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.releaseDir("data", old.Version)); !os.IsNotExist(err) {
		t.Fatal("old release not removed")
	}
	if err := m.Cleanup(nil, []CleanupSelection{{Version: "0.21.0", Fingerprint: changed.Fingerprint}}); err == nil {
		t.Fatal("unmounted volume accepted")
	}
	os.Chmod(filepath.Join(m.baseDir("data"), "releases"), 0777)
	if _, err := m.OldReleases([]string{"data"}); err == nil {
		t.Fatal("writable directory accepted")
	}
}
