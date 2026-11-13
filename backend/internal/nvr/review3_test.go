package nvr

// Regression tests of the third review of the NVR (13.11.2026): the
// review of the fixes. Each failed before its fix.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// N1: rights that could not be read at the start: deleting an account
// meanwhile cannot remove its ticks; once a later start reads the file, a
// new account of the same name must not inherit them (the probe of the
// review: a file without the times of the ticks).
func TestStaleRightsAfterAnUnreadableStart(t *testing.T) {
	dir := t.TempDir()
	store := config.Open(dir)
	if err := store.WriteJSON(accessFile, accessSettings{Cameras: map[string]CameraAccess{"einfahrt": {Live: []string{"anna"}, Recordings: []string{"anna"}}}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, accessFile)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	created := time.Unix(1000, 0)
	a := NewAccess(store)
	a.Lookup = func(name string) (Account, bool) { return Account{Name: name, Created: created}, true }
	if err := a.Load(); err == nil {
		t.Skip("readable despite mode 0 (running as root?)")
	}
	if err := a.ForgetUser("anna"); err == nil {
		t.Error("the rights were changed although they could not be read")
	}
	_ = os.Chmod(path, 0o600)
	// A later start; anna is a new account now.
	created = time.Unix(5000, 0)
	b := NewAccess(store)
	b.Lookup = func(name string) (Account, bool) { return Account{Name: name, Created: created}, true }
	if err := b.Load(); err != nil {
		t.Fatal(err)
	}
	if b.Allows("anna", "einfahrt", RightRecordings) || b.Allows("anna", "einfahrt", RightLive) {
		t.Error("a new account inherits the ticks of a deleted one of the same name")
	}
}

// N1, the ordinary way: ticks written for an account belong to it only;
// an account made after them, of the same name, sees nothing of them, and
// giving it ticks on one camera does not bring back the old ones.
func TestTicksBelongToTheAccountTheyWereGivenTo(t *testing.T) {
	dir := t.TempDir()
	store := config.Open(dir)
	created := time.Now().Add(-time.Hour)
	lookup := func(name string) (Account, bool) { return Account{Name: name, Created: created}, true }
	a := NewAccess(store)
	a.Lookup = lookup
	if err := a.SetUser("anna", UserRights{"einfahrt": {Live: true, Recordings: true}}, []string{"einfahrt", "garage"}); err != nil {
		t.Fatal(err)
	}
	if !a.Allows("anna", "einfahrt", RightLive) {
		t.Fatal("ticks not given")
	}
	// The account is deleted while the file cannot be read; a new anna is
	// made later; a later start reads the file.
	time.Sleep(5 * time.Millisecond)
	created = time.Now()
	time.Sleep(5 * time.Millisecond)
	b := NewAccess(store)
	b.Lookup = lookup
	if err := b.Load(); err != nil {
		t.Fatal(err)
	}
	if b.Allows("anna", "einfahrt", RightLive) {
		t.Error("the new anna sees the old anna's camera")
	}
	if got := b.User("anna", []string{"einfahrt", "garage"}); got["einfahrt"].Live || got["einfahrt"].Recordings {
		t.Errorf("the ticks shown for the new anna: %+v", got)
	}
	if err := b.SetUser("anna", UserRights{"garage": {Live: true}}, []string{"einfahrt", "garage"}); err != nil {
		t.Fatal(err)
	}
	if b.Allows("anna", "einfahrt", RightLive) || b.Allows("anna", "einfahrt", RightRecordings) || !b.Allows("anna", "garage", RightLive) {
		t.Errorf("after new ticks: einfahrt %v/%v, garage %v", b.Allows("anna", "einfahrt", RightLive), b.Allows("anna", "einfahrt", RightRecordings), b.Allows("anna", "garage", RightLive))
	}
	if data, _ := os.ReadFile(filepath.Join(dir, accessFile)); strings.Contains(string(data), `"einfahrt"`) {
		t.Errorf("the old ticks stay in the file: %s", data)
	}
}

// N1: a camera is not deleted while its rights cannot be removed: a
// camera added later with its ID would inherit them.
func TestCameraIsNotDeletedWhileTheRightsAreUnreadable(t *testing.T) {
	r := newRig(t, true)
	if err := r.store.WriteJSON(accessFile, accessSettings{Cameras: map[string]CameraAccess{"einfahrt": {Live: []string{"anna"}, Recordings: []string{}}}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(r.store.Dir(), accessFile)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if err := r.m.Access.Load(); err == nil {
		t.Skip("readable despite mode 0 (running as root?)")
	}
	_ = os.Chmod(path, 0o600)
	if err := r.m.RemoveCamera("einfahrt"); !errors.Is(err, ErrRightsUnreadable) {
		t.Fatalf("deleted with unreadable rights: %v", err)
	}
	if _, ok := r.m.Camera("einfahrt"); !ok {
		t.Error("the camera is gone")
	}
	if got := r.m.DeletedCameras(); len(got) != 0 {
		t.Errorf("noted as deleted: %+v", got)
	}
	// A start that reads the file: deleting works and takes the ticks.
	if err := r.m.Access.Load(); err != nil {
		t.Fatal(err)
	}
	if err := r.m.RemoveCamera("einfahrt"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); strings.Contains(string(data), "einfahrt") {
		t.Errorf("the deleted camera's ticks stay: %s", data)
	}
}

// restartEngine stops the container as dockerd does when it is restarted
// (by hand, after a crash of dockerd, by titand for a new data folder):
// the engine does not answer for one look, then is back.
type restartEngine struct {
	*DevEngine
	unreachable bool
}

func (e *restartEngine) NVRInspect(ctx context.Context) (docker.NVRState, error) {
	if e.unreachable {
		return docker.NVRState{}, errors.New("Cannot connect to the Docker daemon")
	}
	return e.DevEngine.NVRInspect(ctx)
}

// M2: a restart of Docker that titand did not see is no crash of Frigate:
// the container ended when Docker's engine stopped, and the engine started
// after the container did.
func TestDockerRestartByHandIsNoCrash(t *testing.T) {
	r := newRig(t, true)
	r.up()
	e := &restartEngine{DevEngine: r.engine}
	r.m.Engine = e
	for range r.m.CrashLimit + 1 {
		r.engine.RestartEngine()
		e.unreachable = true
		r.advance(10 * time.Second)
		r.look()
		e.unreachable = false
		r.advance(errorBackoff + time.Second)
		r.look()
		r.look()
	}
	r.m.mu.Lock()
	failover, stopped, crashes := r.m.state.Failover, r.m.state.Stopped, len(r.m.rt.crashes)
	r.m.mu.Unlock()
	if failover != nil || stopped || crashes != 0 || r.phase() != PhaseRunning {
		t.Errorf("restarts of Docker counted as crashes: %d, failover %+v, stopped %v, phase %s", crashes, failover, stopped, r.phase())
	}
	// Ends of Frigate while the engine runs still count.
	r.crashLoop("frigate.app INFO starting\nRuntimeError: [ GENERAL_ERROR ] MAPPED_INFERENCE_VERSION is NOT compatible with the ELF\n")
	r.m.mu.Lock()
	failover = r.m.state.Failover
	r.m.mu.Unlock()
	if failover == nil || failover.From != DetectorNPU {
		t.Errorf("a crash loop after Docker's restarts did not move the detection: %+v", failover)
	}
}

// M2: what the engine says about its start is read, not assumed: an
// engine that cannot tell counts the end, as before.
func TestEngineStartUnknownCounts(t *testing.T) {
	r := newRig(t, true)
	r.up()
	r.engine.mu.Lock()
	r.engine.engineStartedErr = errors.New("no pid file")
	r.engine.engineStarted = time.Now().Add(time.Hour)
	r.engine.mu.Unlock()
	r.engine.Exit(1)
	r.advance(20 * time.Second)
	r.look()
	r.m.mu.Lock()
	crashes := len(r.m.rt.crashes)
	r.m.mu.Unlock()
	if crashes != 1 {
		t.Errorf("crashes counted: %d", crashes)
	}
}

// docs: the still of a request without a height is one of the six sizes.
func TestSnapshotWithoutHeightIsOneOfTheSizes(t *testing.T) {
	if got := snapshotHeight(0); !containsInt(snapshotHeights, got) {
		t.Errorf("height 0 → %d", got)
	}
	r := newRig(t, true)
	r.up()
	ctx := context.Background()
	for _, h := range []int{0, 90, 100, 200, 400, 800, 1100, 2160} {
		if _, err := r.m.Snapshot(ctx, "haustuer", h); err != nil {
			t.Fatal(err)
		}
	}
	r.m.mu.Lock()
	cached := len(r.m.snapshots)
	r.m.mu.Unlock()
	if cached > len(snapshotHeights) {
		t.Errorf("%d stills cached for one camera", cached)
	}
}

func containsInt(list []int, v int) bool {
	for _, each := range list {
		if each == v {
			return true
		}
	}
	return false
}

// Names of persons and plates in the lines of Frigate's log (nginx writes
// every request line; the faces' paths name the person) never reach the
// diagnosis, from Frigate's API or from the container's log.
func TestDiagnosisLogHasNoNames(t *testing.T) {
	lines := []string{
		`172.21.0.1 - - [13/Nov/2026:01:00:00 +0000] "POST /api/faces/Anna%20Beispiel/create HTTP/1.1" 200 2 "-" "Go-http-client/1.1"`,
		`172.21.0.1 - - [13/Nov/2026:01:00:01 +0000] "POST /api/faces/train/Anna/classify HTTP/1.1" 200 2 "-" "-"`,
		`172.21.0.1 - - [13/Nov/2026:01:00:02 +0000] "GET /clips/faces/Anna/Anna-1700000000.1.webp HTTP/1.1" 200 2 "-" "-"`,
		`172.21.0.1 - - [13/Nov/2026:01:00:03 +0000] "GET /clips/faces/train/1700000000.1-abc-Anna-0.81.webp HTTP/1.1" 200 2 "-" "-"`,
		`172.21.0.1 - - [13/Nov/2026:01:00:04 +0000] "GET /api/events?sub_label=Anna&recognized_license_plate=M-AB1234&limit=5 HTTP/1.1" 200 2 "-" "-"`,
		`2026-11-13 01:00:05.000 INFO frigate.api faces listed: /api/faces`,
	}
	for _, source := range []string{"frigate", "container"} {
		r := newRig(t, true)
		r.up()
		r.frigate.Set(func(f *DevFrigate) { f.Log = lines })
		r.engine.SetLog(strings.Join(lines, "\n") + "\n")
		if source == "container" {
			r.engine.Exit(1)
			r.m.mu.Lock()
			r.m.rt.phase = PhaseError
			r.m.mu.Unlock()
		}
		d := r.m.Diagnosis(context.Background())
		if d.LogSource != source {
			t.Fatalf("log from %q, want %q", d.LogSource, source)
		}
		text := strings.Join(d.Log, "\n")
		for _, secret := range []string{"Anna", "Beispiel", "M-AB1234"} {
			if strings.Contains(text, secret) {
				t.Errorf("%s: the diagnosis names %q:\n%s", source, secret, text)
			}
		}
		if !strings.Contains(text, "/api/faces/") || !strings.Contains(text, "limit=5") || !strings.Contains(text, "faces listed: /api/faces") {
			t.Errorf("%s: more than the names went:\n%s", source, text)
		}
	}
}
