package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The viewer image (PLAN 63) trusts the release manifest only with a valid
// signature for the exact version, and the asset only with its hash.
func TestReleaseManifestAndAsset(t *testing.T) {
	f := newCatalogFixture(t)
	m := f.m
	ctx := context.Background()
	asset := []byte("viewer image")
	sum := sha256.Sum256(asset)
	raw, _ := json.Marshal(map[string]any{"version": m.Release, "tag": "v" + m.Release, "viewer": map[string]any{"asset": "titanos-viewer-" + m.Release + ".tar.zst"}})
	f.write(t, "manifest.json", raw)
	f.sign(t, "manifest.json")
	f.write(t, "titanos-viewer-"+m.Release+".tar.zst", asset)
	if data, err := m.ReleaseManifest(ctx, m.Release); err != nil || string(data) != string(raw) {
		t.Fatalf("signed manifest: %v", err)
	}
	if _, err := m.ReleaseManifest(ctx, "dev-123"); err == nil {
		t.Fatal("development version fetched a release")
	}
	dest := filepath.Join(t.TempDir(), "image.part")
	if err := m.DownloadReleaseAsset(ctx, m.Release, "titanos-viewer-"+m.Release+".tar.zst", dest, hex.EncodeToString(sum[:]), int64(len(asset))); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); string(got) != string(asset) {
		t.Fatal("asset content")
	}
	wrong := sha256.Sum256([]byte("other"))
	other := filepath.Join(t.TempDir(), "other.part")
	if err := m.DownloadReleaseAsset(ctx, m.Release, "titanos-viewer-"+m.Release+".tar.zst", other, hex.EncodeToString(wrong[:]), int64(len(asset))); err == nil {
		t.Fatal("asset with the wrong hash accepted")
	}
	// A manifest naming another version, and a forged signature.
	raw2, _ := json.Marshal(map[string]any{"version": "9.9.9", "tag": "v9.9.9"})
	f.write(t, "manifest.json", raw2)
	f.sign(t, "manifest.json")
	if _, err := m.ReleaseManifest(ctx, m.Release); err == nil {
		t.Fatal("manifest of another version accepted")
	}
	f.write(t, "manifest.json", raw)
	f.write(t, "manifest.json.minisig", []byte("forged"))
	if _, err := m.ReleaseManifest(ctx, m.Release); err == nil {
		t.Fatal("forged manifest accepted")
	}
}
