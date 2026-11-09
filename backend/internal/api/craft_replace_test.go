package api

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/webapps"
)

// PLAN 75-20: old Craft apps give way to the T-apps without asking; the
// Docker versions go with their data, "open with" follows the successor.
func TestCraftReplacement(t *testing.T) {
	var manager *webapps.Manager
	ts := newTestServerWith(t, func(o *Options) {
		o.Store.WriteJSON("webapps.json", map[string]any{"installed": []map[string]string{
			{"id": "pdfcraft", "version": "0.19.0", "volume": "tank"},
			{"id": "t-pdf", "version": "1.1.0", "volume": "tank"},
			{"id": "t-word", "version": "1.1.0", "volume": "tank"},
		}})
		manager = webapps.New(o.Store)
		mounts := t.TempDir()
		os.Mkdir(filepath.Join(mounts, "tank"), 0o755)
		manager.Version, manager.MountRoot = "1.2.3", mounts
		manager.Mounted = func() []string { return []string{"tank"} }
		manager.Load()
		t.Cleanup(manager.Close)
		o.Webapps = manager
	})
	ts.setup()
	if err := ts.server.prefs.set("anna", "fileapps", []byte(`{"pdf":"webapp:pdfcraft","txt":"editor"}`)); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var removed []string
	ts.server.migration.detect = func(context.Context) []webapps.DockerApp {
		return []webapps.DockerApp{{App: "word", Webapp: "t-word"}}
	}
	ts.server.migration.remove = func(_ context.Context, name string) error {
		mu.Lock()
		defer mu.Unlock()
		removed = append(removed, name)
		return nil
	}
	ts.server.replaceCraftOnce(context.Background())
	for _, st := range manager.List() {
		if st.ID == "pdfcraft" && st.Installed {
			t.Fatal("PdfCraft still installed next to T-PDF")
		}
	}
	if !slices.Equal(removed, []string{"word"}) {
		t.Fatalf("Docker apps removed: %v", removed)
	}
	data, err := ts.server.prefs.get("anna", "fileapps")
	if err != nil || strings.TrimSpace(string(data)) != `{"pdf":"webapp:t-pdf","txt":"editor"}` {
		t.Fatalf("open with: %s %v", data, err)
	}
	if st := ts.server.migration.status(); st == nil || st.Running || st.Items[0].State != "removed" {
		t.Fatalf("%+v", st)
	}
}
