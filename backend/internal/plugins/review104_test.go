package plugins

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// N-102-32: after the local build key was lost (a stick restored from
// before the first build), a build of the same image could never be
// published again: the cached copy under its hash no longer checks out.
// The fresh build takes its place.
func TestRebuildAfterALostKeyReplacesTheCachedCopy(t *testing.T) {
	f, _, dir := localFixture(t)
	m := f.m
	// The same build, made again with the key of now.
	result := filepath.Join(t.TempDir(), "result")
	if err := os.CopyFS(result, os.DirFS(dir)); err != nil {
		t.Fatal(err)
	}
	// The key the cached copy was made with is gone.
	m.buildKey = make([]byte, 32)
	rand.Read(m.buildKey)
	if _, err := m.cachedDriver(dir, true); err == nil {
		t.Fatal("the cached copy still checks out")
	}
	// titand signs the fresh result with the key of now.
	var r buildReceipt
	raw, _ := os.ReadFile(filepath.Join(result, localReceipt))
	json.Unmarshal(raw, &r)
	r.MAC = m.receiptMAC(r)
	raw, _ = json.Marshal(r)
	os.WriteFile(filepath.Join(result, localReceipt), raw, 0o600)
	if err := m.publishLocalBuild(result, dir); err != nil {
		t.Fatalf("publish after a lost key: %v", err)
	}
	if _, err := m.cachedDriver(dir, true); err != nil {
		t.Fatalf("published copy: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Dir(dir))
	if len(entries) != 1 {
		t.Fatalf("left in the cache: %v", entries)
	}
}

// N-102-33: a driver update asked for while another request waits is not
// dropped silently: the caller hears that the driver work is busy, as for
// a build or a choice of version.
func TestDriverUpdateRequestOnAFullQueueIsRefused(t *testing.T) {
	f, _ := driverFixture(t)
	m := f.m
	if err := m.RequestDriverUpdate("", []string{"data"}); err != nil {
		t.Fatal(err)
	}
	if err := m.RequestDriverUpdate("", []string{"data"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second request while the first waits: %v", err)
	}
}
