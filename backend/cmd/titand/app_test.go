package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/app"
	"github.com/ra5on/TitanOS/backend/internal/minisign/minisigntest"
)

// appOnTrial puts app 0.8.1 onto a stick, on trial, and starts it as the
// base would.
func appOnTrial(t *testing.T) *app.Stick {
	t.Helper()
	root := t.TempDir()
	signer := minisigntest.New()
	os.MkdirAll(filepath.Join(root, "keys"), 0o755)
	os.WriteFile(filepath.Join(root, "keys/test.pub"), []byte(signer.PublicFile()), 0o644)
	stick := &app.Stick{Dir: filepath.Join(root, "app"), KeyDir: filepath.Join(root, "keys"), Base: "0.8.0", RunDir: filepath.Join(root, "run")}
	content := []byte("titand 0.8.1")
	sum := sha256.Sum256(content)
	manifest, _ := json.Marshal(map[string]any{"version": "0.8.1", "app": app.Info{Base: "0.8.0", Asset: "a", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))}})
	binary := filepath.Join(root, "titand")
	os.WriteFile(binary, content, 0o755)
	if err := stick.Install("0.8.1", manifest, []byte(signer.Sign(manifest)), binary); err != nil {
		t.Fatal(err)
	}
	if _, version, err := stick.Launch(); err != nil || version != "0.8.1" {
		t.Fatalf("launch: %q %v", version, err)
	}
	return stick
}

func healthServer(t *testing.T, ready bool) string {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"ready": ready})
	}))
	t.Cleanup(server.Close)
	address, _ := url.Parse(server.URL)
	return address.Host
}

func TestCheckKeepsAReadyApp(t *testing.T) {
	stick := appOnTrial(t)
	restarted := false
	restartTitand = func() error { restarted = true; return nil }
	if err := checkAppUpdate(stick, healthServer(t, true), time.Minute); err != nil {
		t.Fatal(err)
	}
	state, _ := stick.ReadState()
	if state.Trial || state.Current != "0.8.1" || state.Result != app.ResultInstalled || restarted {
		t.Errorf("state %+v, restarted %v", state, restarted)
	}
	// Nothing on trial: nothing to do.
	if err := checkAppUpdate(stick, "127.0.0.1:1", time.Minute); err != nil {
		t.Error(err)
	}
}

func TestCheckGivesUpOnAnAppThatIsNotReady(t *testing.T) {
	stick := appOnTrial(t)
	restarted := false
	restartTitand = func() error { restarted = true; return nil }
	if err := checkAppUpdate(stick, healthServer(t, false), 4*time.Second); err == nil {
		t.Fatal("an app that is not ready passed")
	}
	if state, _ := stick.ReadState(); state.Current != "" || state.Trial || state.Result != app.ResultFailed || state.ResultVersion != "0.8.1" {
		t.Errorf("state: %+v", state)
	}
	if !restarted {
		t.Error("titand was not restarted with the system image's titand")
	}
}
