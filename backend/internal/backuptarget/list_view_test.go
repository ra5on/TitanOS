package backuptarget

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A folder of the repository that cannot be read is an error, never an
// empty list: the sender would take it for a repository without snapshots.
func TestUnreadableFolderIsNoEmptyList(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	m, root := newTarget(t)
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	serverURL = srv.URL
	text, _ := m.Pair("Backup")
	c := decode(t, text)
	do(t, c, "POST", "/Backup/?create=true", "")
	do(t, c, "POST", "/Backup/snapshots/"+blob, "snapshot")
	if res, body := do(t, c, "GET", "/Backup/index/", "", "Accept", "application/vnd.x.restic.rest.v2"); res.StatusCode != http.StatusOK || body != "[]\n" {
		t.Fatalf("empty folder: %d %q", res.StatusCode, body)
	}
	dir := filepath.Join(root, "mnt/tank/.titanos/backup-target/Backup/snapshots")
	os.Chmod(dir, 0)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	if res, body := do(t, c, "GET", "/Backup/snapshots/", "", "Accept", "application/vnd.x.restic.rest.v2"); res.StatusCode == http.StatusOK {
		t.Fatalf("unreadable folder answered %d %q", res.StatusCode, body)
	}
}

// Measuring what the sources store for the settings page does not hold up
// the requests of the sources.
func TestMeasuringDoesNotHoldUpRequests(t *testing.T) {
	m, _ := newTarget(t)
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()
	serverURL = srv.URL
	text, _ := m.Pair("Backup")
	c := decode(t, text)
	do(t, c, "POST", "/Backup/?create=true", "")
	do(t, c, "POST", "/Backup/config", "the config")

	walking, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { walkDir = filepath.WalkDir })
	walkDir = func(root string, fn fs.WalkDirFunc) error {
		close(walking)
		<-release
		return filepath.WalkDir(root, fn)
	}
	viewed := make(chan View)
	go func() { viewed <- m.View() }()
	<-walking
	answered := make(chan int)
	go func() {
		res, _ := do(t, c, "HEAD", "/Backup/config", "")
		answered <- res.StatusCode
	}()
	select {
	case status := <-answered:
		if status != http.StatusOK {
			t.Errorf("config: %d", status)
		}
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("a request waited for the measuring")
	}
	close(release)
	if view := <-viewed; len(view.Sources) != 1 || view.Sources[0].Bytes == 0 {
		t.Fatalf("view %+v", view)
	}
}
