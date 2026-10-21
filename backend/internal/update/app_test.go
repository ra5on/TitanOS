package update

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/app"
)

// appEnv is newEnv with a system image of base 1.0.0 that knows apps.
type appEnv struct {
	*env
	restarts atomic.Int32
	working  atomic.Bool
}

func newAppEnv(t *testing.T) *appEnv {
	t.Helper()
	e := &appEnv{env: newEnv(t)}
	e.m = e.appManager(t, "1.0.0")
	return e
}

func (e *appEnv) stick(slot string) *app.Stick {
	return &app.Stick{Dir: filepath.Join(e.boot, "titanos", slot, "app"), KeyDir: filepath.Join(filepath.Dir(e.boot), "keys"), Base: "1.0.0", RunDir: filepath.Join(filepath.Dir(e.boot), "run")}
}

func (e *appEnv) appManager(t *testing.T, version string) *Manager {
	t.Helper()
	m := NewManager(e.store, version, "a")
	m.bootDir, m.keyDir, m.bootID, m.stall, m.retryPause = e.boot, filepath.Join(filepath.Dir(e.boot), "keys"), "boot-1", 5*time.Second, 10*time.Millisecond
	m.OnEvent = func(kind, subject string) { e.events <- kind + ":" + subject }
	m.Base, m.Apps = "1.0.0", e.stick("a")
	m.appFor = e.stick
	m.restartDelay = 0
	m.Restart = func() error { e.restarts.Add(1); return nil }
	m.Working = e.working.Load
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m
}

// publishApp publishes version as a release whose app runs on base.
func (e *appEnv) publishApp(t *testing.T, version, base string) []byte {
	t.Helper()
	binary := make([]byte, 50_000)
	rand.Read(binary)
	sum := sha256.Sum256(binary)
	asset := "titanos-" + version + "-titand"
	e.publish(t, version, ChannelStable, func(m *Manifest) {
		m.App = &app.Info{Base: base, Asset: asset, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(binary))}
	})
	e.release.mu.Lock()
	e.release.assets[asset] = binary
	e.release.mu.Unlock()
	return binary
}

func (e *appEnv) waitPhase(t *testing.T, phases ...string) Progress {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		progress := e.m.CurrentProgress()
		for _, phase := range phases {
			if progress.Phase == phase {
				return progress
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("still at %+v", e.m.CurrentProgress())
	return Progress{}
}

func (e *appEnv) waitRestarts(t *testing.T, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for e.restarts.Load() < want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := e.restarts.Load(); got != want {
		t.Fatalf("titand restarted %d times, want %d", got, want)
	}
}

func TestAppUpdateReplacesOnlyTitand(t *testing.T) {
	e := newAppEnv(t)
	binary := e.publishApp(t, "1.0.1", "1.0.0")
	if err := e.m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if status := e.m.Status(); status.Kind != KindApp || status.Base != "1.0.0" || status.App.CanRollback {
		t.Fatalf("status: kind %q base %q app %+v", status.Kind, status.Base, status.App)
	}

	// Not while titand is at work a restart would break off.
	e.working.Store(true)
	if err := e.m.Install(context.Background()); !errors.Is(err, ErrWorking) {
		t.Fatalf("install while working: %v", err)
	}
	e.working.Store(false)

	if err := e.m.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitPhase(t, "restart", "failed")
	e.waitRestarts(t, 1)
	if progress := e.m.CurrentProgress(); progress.Phase != "restart" {
		t.Fatalf("progress %+v", progress)
	}
	for _, request := range e.release.requests {
		if strings.Contains(request, "squashfs") || strings.Contains(request, "vmlinuz") {
			t.Errorf("an app update loaded a system file: %s", request)
		}
	}
	state, _ := e.stick("a").ReadState()
	if state.Current != "1.0.1" || !state.Trial {
		t.Errorf("app state: %+v", state)
	}
	if data, _ := os.ReadFile(filepath.Join(e.boot, "titanos/a/app/titand-1.0.1")); string(data) != string(binary) {
		t.Error("the app on the stick is not the one of the release")
	}
	// Neither the other slot nor the boot entries were touched.
	if _, err := os.Stat(filepath.Join(e.boot, "titanos/b")); !errors.Is(err, os.ErrNotExist) {
		t.Error("an app update wrote into the other slot")
	}
	if got := entries(e.env); got != "titanos-a.conf[titanos-a]" {
		t.Errorf("entries: %s", got)
	}
	status := e.m.Status()
	if !status.App.Trial || status.Pending != nil || status.Available != nil {
		t.Errorf("status after install: %+v", status)
	}
	// Nothing else while the app is on trial.
	e.m.setProgress(func(p *Progress) { *p = Progress{} })
	if err := e.m.AppRollback(); !errors.Is(err, ErrBusy) {
		t.Errorf("rollback during the trial: %v", err)
	}

	// The check after the restart found it ready; the app tells once.
	if _, err := e.stick("a").Confirm(true); err != nil {
		t.Fatal(err)
	}
	e.event(t) // update_available
	restarted := e.appManager(t, "1.0.1")
	if got := e.event(t); got != "update_installed:1.0.1" {
		t.Errorf("event after the check: %q", got)
	}
	if status := restarted.Status(); status.Version != "1.0.1" || status.App.Trial || !status.App.CanRollback || status.App.Current != "1.0.1" {
		t.Errorf("status of the app: %+v", status)
	}

	// Back to the titand of the system image, without a restart of the NAS.
	e.m = restarted
	if err := e.m.AppRollback(); err != nil {
		t.Fatal(err)
	}
	e.waitRestarts(t, 2)
	if state, _ := e.stick("a").ReadState(); state.Current != "" || state.Previous != "1.0.1" {
		t.Errorf("after going back: %+v", state)
	}
	if got := e.event(t); got != "update_rolled_back:1.0.0" {
		t.Errorf("event after going back: %q", got)
	}
}

func TestAppThatFailsIsReported(t *testing.T) {
	e := newAppEnv(t)
	e.publishApp(t, "1.0.1", "1.0.0")
	e.m.Check(context.Background())
	if err := e.m.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitPhase(t, "restart", "failed")
	e.event(t)
	// The check gave up on it; titand runs the base again and says so.
	e.stick("a").Confirm(false)
	e.appManager(t, "1.0.0")
	if got := e.event(t); got != "update_failed:1.0.1" {
		t.Errorf("event: %q", got)
	}
}

func TestReleaseForAnotherImageIsASystemUpdate(t *testing.T) {
	e := newAppEnv(t)
	e.publishApp(t, "1.1.1", "1.1.0")
	var release string
	e.m.PreparePlugins = func(_ context.Context, version, _, _ string, _ map[string]string, _ func(context.Context, string, int64) ([]byte, error), _ func(context.Context, string, string, string, int64) error) error {
		release = version
		return nil
	}
	// Apps the other slot had do not fit the system that comes.
	old := e.stick("b")
	os.MkdirAll(old.Dir, 0o755)
	os.WriteFile(filepath.Join(old.Dir, "titand-0.9.9"), []byte("old"), 0o755)

	if err := e.m.Check(context.Background()); err != nil {
		t.Fatal(err)
	}
	if kind := e.m.Status().Kind; kind != KindSystem {
		t.Fatalf("kind: %q", kind)
	}
	if err := e.m.Install(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.waitPhase(t, "done", "failed")
	if progress := e.m.CurrentProgress(); progress.Phase != "done" {
		t.Fatalf("progress: %+v", progress)
	}
	// Plugins belong to the image the release was made on.
	if release != "1.1.0" {
		t.Errorf("plugins prepared for %q", release)
	}
	if _, err := os.Stat(old.Dir); !errors.Is(err, os.ErrNotExist) {
		t.Error("apps of the old system stayed in the slot")
	}
	if e.restarts.Load() != 0 {
		t.Error("a system update restarted titand")
	}
}

func TestImageWithoutBaseTakesNoApps(t *testing.T) {
	e := newAppEnv(t)
	e.m.Base = ""
	e.publishApp(t, "1.0.1", "1.0.0")
	e.m.Check(context.Background())
	if kind := e.m.Status().Kind; kind != KindSystem {
		t.Errorf("an image without base version is offered an app: %q", kind)
	}
}
