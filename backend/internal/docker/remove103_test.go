package docker

import (
	"context"
	"errors"
	"os"
	"testing"
)

// Removing an app while the engine does not run would only forget its
// compose file: its containers would come back unmanaged once the engine
// runs again. Refused, also without its data and with Docker switched off
// as long as the engine's data are on the volume.
func TestRemovingAnAppNeedsTheEngine(t *testing.T) {
	m, _, _ := newTestManager(t)
	ctx := context.Background()
	m.SetSettings(Settings{Enabled: true, Volume: "data"}, []string{"data"})
	m.store.Write("apps/whoami/compose.yaml", []byte("services:\n  web:\n    image: traefik/whoami\n"))
	kept := func(when string) {
		t.Helper()
		if _, err := m.Compose("whoami"); err != nil {
			t.Fatalf("%s: the app was forgotten: %v", when, err)
		}
	}
	// Switched on, the volume locked: the engine does not run.
	if err := m.RemoveApp(ctx, "whoami", false); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("without the engine: %v", err)
	}
	kept("volume locked")
	// Switched off, its data still on the volume.
	if err := os.MkdirAll(m.dataRoot("data"), 0o700); err != nil {
		t.Fatal(err)
	}
	m.SetSettings(Settings{Enabled: false, Volume: "data"}, []string{"data"})
	for _, purge := range []bool{false, true} {
		if err := m.RemoveApp(ctx, "whoami", purge); !errors.Is(err, ErrRemoveNeedsEngine) {
			t.Fatalf("Docker off with its data, purge %v: %v", purge, err)
		}
	}
	kept("Docker off")
	// No data of the engine (never switched on, volume gone): only forgotten.
	if err := os.RemoveAll(m.dataRoot("data")); err != nil {
		t.Fatal(err)
	}
	if err := m.RemoveApp(ctx, "whoami", false); err != nil {
		t.Fatalf("nothing to take down: %v", err)
	}
	if _, err := m.Compose("whoami"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("app kept: %v", err)
	}
}
