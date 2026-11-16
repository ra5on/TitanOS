package plugins

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// A plugin download counts as work: an app update of titand waits for it
// instead of cutting it off.
func TestPluginDownloadIsBusy(t *testing.T) {
	m, _ := newTestManager(t)
	if m.Busy() {
		t.Fatal("busy without work")
	}
	m.mu.Lock()
	m.downloading = map[string]bool{"demo": true}
	m.mu.Unlock()
	if !m.Busy() {
		t.Fatal("a running plugin download does not count as busy")
	}
}

// The folders of downloads a restart cut off go after the next start, but
// never while a download runs, and nothing else of that kind of name.
func TestCutOffDownloadsAreRemoved(t *testing.T) {
	m, _ := newTestManager(t)
	settings := m.current()
	settings.Volume = "data"
	if err := m.save(settings); err != nil {
		t.Fatal(err)
	}
	base := m.baseDir("data")
	left := []string{
		filepath.Join(base, ".download-123456"),
		filepath.Join(base, "driver-updates", "6.12.0-test", ".download-42"),
	}
	for _, dir := range left {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "demo.raw"), []byte("half"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Not of a download: kept.
	other := filepath.Join(base, ".download-notes")
	os.MkdirAll(other, 0o700)
	target := t.TempDir()
	os.WriteFile(filepath.Join(target, "precious"), []byte("x"), 0o600)
	link := filepath.Join(base, ".download-7")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	// A download of this run is in progress: nothing is touched yet.
	m.mu.Lock()
	m.downloading = map[string]bool{"demo": true}
	m.mu.Unlock()
	m.Apply(context.Background(), []string{"data"})
	for _, dir := range left {
		if _, err := os.Stat(dir); err != nil {
			t.Fatalf("removed during a running download: %v", err)
		}
	}
	m.mu.Lock()
	m.downloading = nil
	m.mu.Unlock()
	m.Apply(context.Background(), []string{"data"})
	for _, dir := range left {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Errorf("%s is still there: %v", dir, err)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("a folder of another name was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "precious")); err != nil {
		t.Errorf("a link was followed: %v", err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("a link was removed: %v", err)
	}
}
