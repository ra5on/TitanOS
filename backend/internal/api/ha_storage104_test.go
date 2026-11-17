package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// N-102-36: an entry whose setup cannot be saved leaves neither the new
// account nor the new share behind; the next try works.
func TestHAStorageThatCannotBeSavedLeavesNothing(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	dir := filepath.Join(ts.store.Dir(), "homeassistant")
	if err := os.MkdirAll(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o500)
	t.Cleanup(func() { os.Chmod(dir, 0o700) })
	entry := map[string]any{"volume": "tank", "share": "homeassistant", "account": "homeassistant", "usage": "backup"}
	if res, body := ts.request("POST", "/api/v1/homeassistant/storage", entry); res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("save on a stick that cannot be written: %d %s", res.StatusCode, body)
	}
	if _, ok := ts.users.Get("homeassistant"); ok {
		t.Error("the account was left behind")
	}
	if findShare(ts, "homeassistant").Name != "" {
		t.Error("the share was left behind")
	}
	os.Chmod(dir, 0o700)
	if res, body := ts.request("POST", "/api/v1/homeassistant/storage", entry); res.StatusCode != http.StatusCreated {
		t.Fatalf("next try: %d %s", res.StatusCode, body)
	}
}

// N-102-36: entries added at the same time are all kept.
func TestHAStorageEntriesAddedAtOnceAreAllKept(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.withShare()
	if res, body := ts.request("POST", "/api/v1/homeassistant/storage", map[string]any{"volume": "tank", "share": "homeassistant", "account": "homeassistant", "usage": "backup"}); res.StatusCode != http.StatusCreated {
		t.Fatalf("first: %d %s", res.StatusCode, body)
	}
	var wg sync.WaitGroup
	for i := range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			share := fmt.Sprintf("media%d", i)
			if res, body := ts.request("POST", "/api/v1/homeassistant/storage", map[string]any{"volume": "tank", "share": share, "usage": "media"}); res.StatusCode != http.StatusCreated {
				t.Errorf("%s: %d %s", share, res.StatusCode, body)
			}
		}()
	}
	wg.Wait()
	setup, err := ts.server.haArea().haStorage("")
	if err != nil || setup == nil || len(setup.Entries) != 7 {
		t.Fatalf("entries after adding at once: %+v %v", setup, err)
	}
}
