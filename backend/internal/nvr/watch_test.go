package nvr

import (
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// rig is a manager on a DevEngine and a DevFrigate, with a clock the test
// moves and the messages it raised.
type rig struct {
	t       *testing.T
	m       *Manager
	engine  *DevEngine
	frigate *DevFrigate
	store   *config.Store
	dir     string
	now     time.Time
	mu      sync.Mutex
	notes   []string
	routed  []Notice
}

func newRig(t *testing.T, image bool) *rig {
	t.Helper()
	r := &rig{t: t, store: config.Open(t.TempDir()), dir: t.TempDir(), now: time.Date(2026, 11, 12, 8, 0, 0, 0, time.UTC)}
	r.engine = NewDevEngine(image)
	r.frigate = NewDevFrigate(nil)
	r.engine.Serve(r.frigate)
	srv := httptest.NewServer(r.frigate)
	t.Cleanup(srv.Close)
	r.m = r.manager(srv.URL)
	if err := r.m.Load(); err != nil {
		t.Fatal(err)
	}
	r.frigate.SetCameras(r.m.Cameras())
	return r
}

// manager makes a manager on the rig's store, folder and engine: a second
// one stands for titand after a restart.
func (r *rig) manager(base string) *Manager {
	m := New(r.store)
	m.Dev, m.Engine, m.devDir = true, r.engine, r.dir
	m.Access = NewAccess(r.store)
	m.FrigateBase = func(string) string { return base }
	m.Now = func() time.Time { r.mu.Lock(); defer r.mu.Unlock(); return r.now }
	m.bootID = func() string { return "boot-1" }
	m.Route = func(n Notice) {
		r.mu.Lock()
		r.notes = append(r.notes, n.Severity+" "+n.Message+" "+n.Subject)
		r.routed = append(r.routed, n)
		r.mu.Unlock()
	}
	m.Notify = func(severity, kind, subject string) {
		r.mu.Lock()
		r.notes = append(r.notes, severity+" "+kind+" "+subject)
		r.mu.Unlock()
	}
	return m
}

func (r *rig) advance(d time.Duration) {
	r.mu.Lock()
	r.now = r.now.Add(d)
	r.mu.Unlock()
}

func (r *rig) look() { r.m.reconcile(context.Background()) }

func (r *rig) phase() string {
	r.m.mu.Lock()
	defer r.m.mu.Unlock()
	return r.m.rt.phase
}

func (r *rig) config() string {
	data, err := os.ReadFile(filepath.Join(r.dir, "config", "config.yml"))
	if err != nil {
		r.t.Fatalf("config.yml: %v", err)
	}
	return string(data)
}

func (r *rig) enable() {
	r.t.Helper()
	if err := r.m.Enable(context.Background(), "", SetOptions{}); err != nil {
		r.t.Fatal(err)
	}
}

// up enables the NVR and lets it reach "running".
func (r *rig) up() {
	r.t.Helper()
	r.enable()
	r.look()
	r.look()
	if got := r.phase(); got != PhaseRunning {
		r.t.Fatalf("phase %q, want running", got)
	}
}

// crashLoop lets Frigate end CrashLimit times with log as its last words.
func (r *rig) crashLoop(log string) {
	r.engine.SetLog(log)
	for range r.m.CrashLimit {
		r.engine.Exit(1)
		r.advance(20 * time.Second)
		r.look()
	}
}

func (r *rig) noted() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.notes)
}

func TestEnableStartsFrigate(t *testing.T) {
	r := newRig(t, true)
	r.look()
	if r.phase() != PhaseOff || r.engine.Creates != 0 {
		t.Fatalf("off: phase %s, %d containers", r.phase(), r.engine.Creates)
	}
	r.enable()
	r.look()
	if r.phase() != PhaseStarting || r.engine.Creates != 1 || r.engine.Starts != 1 {
		t.Fatalf("after enabling: phase %s creates %d starts %d", r.phase(), r.engine.Creates, r.engine.Starts)
	}
	spec := r.engine.Spec()
	// Three of the four demo cameras are on: 128 + 3·64 MB.
	if spec.ShmBytes != 320<<20 || spec.CacheBytes != 1<<30 || spec.Config != filepath.Join(r.dir, "config") || spec.Media != filepath.Join(r.dir, "media") {
		t.Errorf("spec: %+v", spec)
	}
	// WebRTC only; never Frigate's API, its login port or go2rtc.
	for _, p := range spec.Ports {
		if p.Port != 8555 {
			t.Errorf("published port %d/%s", p.Port, p.Proto)
		}
	}
	if len(spec.Ports) != 2 {
		t.Errorf("ports: %+v", spec.Ports)
	}
	if cfg := r.config(); !strings.Contains(cfg, "device: NPU") || !strings.Contains(cfg, "haustuer_twoway") {
		t.Errorf("config:\n%s", cfg)
	}
	// config.yml holds the cameras' passwords: only root reads it.
	if info, err := os.Stat(filepath.Join(r.dir, "config", "config.yml")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("config.yml mode: %v %v", info.Mode(), err)
	}
	r.look()
	st := r.m.Status(context.Background())
	if st.State != PhaseRunning || st.Detector.Active != DetectorNPU || st.Detector.Failover != nil || st.Detector.InferenceMs == nil || *st.Detector.InferenceMs != 11.4 {
		t.Errorf("status: %+v", st)
	}
	if st.Cameras.Total != 4 || st.Cameras.Enabled != 3 || st.Cameras.Online != 3 || st.Storage.UsedBytes == nil {
		t.Errorf("counts: %+v storage %+v", st.Cameras, st.Storage)
	}
	// Nothing changes: more looks neither restart nor rewrite anything.
	for range 5 {
		r.advance(10 * time.Second)
		r.look()
	}
	if r.engine.Creates != 1 || r.engine.Starts != 1 {
		t.Errorf("idle looks: creates %d starts %d", r.engine.Creates, r.engine.Starts)
	}
}

func TestChangesRestartOnlyWhenNeeded(t *testing.T) {
	r := newRig(t, true)
	r.up()
	// A changed camera: new config, Frigate restarts in the same container.
	c, _ := r.m.Camera("einfahrt")
	c.Name = "Einfahrt Nord"
	if _, err := r.m.UpdateCamera("einfahrt", c, false); err != nil {
		t.Fatal(err)
	}
	r.look()
	if r.engine.Creates != 1 || r.engine.Starts != 2 || !strings.Contains(r.config(), "Einfahrt Nord") {
		t.Errorf("rename: creates %d starts %d", r.engine.Creates, r.engine.Starts)
	}
	// Storing the same camera again changes nothing.
	if _, err := r.m.UpdateCamera("einfahrt", c, false); err != nil {
		t.Fatal(err)
	}
	r.look()
	if r.engine.Creates != 1 || r.engine.Starts != 2 {
		t.Errorf("same camera: creates %d starts %d", r.engine.Creates, r.engine.Starts)
	}
	// The restream on port 8554 changes the container itself.
	s := r.m.Settings()
	s.RTSPOut = true
	if err := r.m.SetSettings(context.Background(), s, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	spec := r.engine.Spec()
	if r.engine.Creates != 2 || len(spec.Ports) != 3 || spec.Ports[2].Port != 8554 {
		t.Errorf("rtsp out: creates %d ports %+v", r.engine.Creates, spec.Ports)
	}
	password := r.m.Settings().RTSPPassword
	if len(password) < 16 || !strings.Contains(r.config(), "password: "+password) {
		t.Errorf("restream password %q not in the config", password)
	}
	// The password stays when the settings are stored again.
	s = r.m.Settings()
	s.RTSPPassword = "something else"
	if err := r.m.SetSettings(context.Background(), s, SetOptions{}); err != nil || r.m.Settings().RTSPPassword != password {
		t.Errorf("restream password changed: %v", err)
	}
	// Switching off removes the container and keeps the data.
	if err := r.m.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.look()
	if st, _ := r.engine.NVRInspect(context.Background()); st.Exists || r.phase() != PhaseOff {
		t.Errorf("disabled: container %+v phase %s", st, r.phase())
	}
	if _, err := os.Stat(filepath.Join(r.dir, "config", "config.yml")); err != nil {
		t.Errorf("data removed with the container: %v", err)
	}
}

func TestTitandRestartAdoptsRunningFrigate(t *testing.T) {
	r := newRig(t, true)
	r.up()
	// titand restarts (an app update): Frigate keeps running.
	old := r.m
	r.m = r.manager(old.FrigateBase(""))
	if err := r.m.Load(); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	if r.phase() != PhaseRunning || r.engine.Creates != 1 || r.engine.Starts != 1 {
		t.Errorf("after a restart of titand: phase %s creates %d starts %d", r.phase(), r.engine.Creates, r.engine.Starts)
	}
}

func TestFailoverChain(t *testing.T) {
	r := newRig(t, true)
	r.up()
	// The NPU's firmware is too old: Frigate ends again and again.
	r.crashLoop("frigate.app INFO starting\nRuntimeError: [ GENERAL_ERROR ] MAPPED_INFERENCE_VERSION is NOT compatible with the ELF Expected: 7.0 received: 11.0\n")
	if r.engine.Starts != 3 {
		t.Errorf("two ends are answered by a new start, the third by the failover: %d starts", r.engine.Starts)
	}
	st := r.m.Status(context.Background())
	if st.Detector.Kind != DetectorAuto || st.Detector.Active != DetectorIGPU || st.Detector.Failover == nil {
		t.Fatalf("after the loop: %+v", st.Detector)
	}
	f := st.Detector.Failover
	if f.From != DetectorNPU || f.To != DetectorIGPU || f.Reason != ReasonNPUFirmware || !strings.Contains(f.Detail, "MAPPED_INFERENCE_VERSION") || !f.At.Equal(r.now) {
		t.Errorf("failover: %+v", f)
	}
	if got := r.noted(); len(got) != 1 || got[0] != "warning nvr_failover NPU → iGPU" {
		t.Errorf("messages: %v", got)
	}
	// The next look writes the config for the iGPU and starts Frigate.
	r.look()
	if !strings.Contains(r.config(), "device: GPU") || r.engine.Starts != 4 {
		t.Errorf("iGPU config not applied (starts %d):\n%s", r.engine.Starts, r.config())
	}
	r.look()
	if r.phase() != PhaseRunning {
		t.Errorf("phase %s", r.phase())
	}
	// The failover is on the volume, not on the stick.
	var stored State
	root, _ := os.OpenRoot(r.dir)
	defer root.Close()
	if err := volumedata.ReadJSON(root, stateFile, &stored, 1<<20); err != nil || stored.Active != DetectorIGPU || stored.Failover == nil || stored.Boot != "boot-1" {
		t.Errorf("state.json: %+v %v", stored, err)
	}
	if data, _ := r.store.Read(settingsFile); strings.Contains(string(data), "igpu") {
		t.Errorf("the failover was written to the stick: %s", data)
	}

	// The iGPU fails too (cause unknown), then the CPU: the NVR stops.
	r.advance(10 * time.Minute)
	r.crashLoop("Bus error (core dumped)")
	r.look()
	if st := r.m.Status(context.Background()); st.Detector.Active != DetectorCPU || st.Detector.Failover.From != DetectorIGPU || st.Detector.Failover.Reason != ReasonDetectorUnknown {
		t.Fatalf("second failover: %+v", st.Detector)
	}
	if !strings.Contains(r.config(), "device: CPU") {
		t.Error("CPU config not applied")
	}
	r.advance(10 * time.Minute)
	r.crashLoop("Bus error (core dumped)")
	st = r.m.Status(context.Background())
	if st.State != PhaseStopped || st.Error == nil || st.Error.Code != ReasonDetectorUnknown {
		t.Fatalf("at the end of the chain: %+v error %+v", st.State, st.Error)
	}
	if c, _ := r.engine.NVRInspect(context.Background()); c.Exists {
		t.Error("the container stays after the NVR stopped")
	}
	if got := r.noted(); len(got) != 3 || got[1] != "warning nvr_failover iGPU → CPU" || got[2] != "error nvr_stopped CPU" {
		t.Errorf("messages: %v", got)
	}
	// It stays stopped: no loop of its own.
	starts := r.engine.Starts
	for range 3 {
		r.advance(time.Minute)
		r.look()
	}
	if r.engine.Starts != starts || r.phase() != PhaseStopped {
		t.Errorf("stopped NVR started again (%d → %d)", starts, r.engine.Starts)
	}

	// "Try again" goes back to the chosen device.
	if err := r.m.RetryDetector(); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	st = r.m.Status(context.Background())
	if st.State != PhaseRunning || st.Detector.Active != DetectorNPU || st.Detector.Failover != nil || st.Error != nil || !strings.Contains(r.config(), "device: NPU") {
		t.Errorf("after retry: %+v error %+v", st.Detector, st.Error)
	}
}

func TestTwoEndsAreNoLoop(t *testing.T) {
	r := newRig(t, true)
	r.up()
	r.engine.SetLog("MAPPED_INFERENCE_VERSION is NOT compatible")
	// Two ends, then quiet for longer than the window, then two more.
	for range 2 {
		for range 2 {
			r.engine.Exit(1)
			r.advance(20 * time.Second)
			r.look()
		}
		r.advance(6 * time.Minute)
		r.look()
	}
	if st := r.m.Status(context.Background()); st.Detector.Failover != nil || st.State != PhaseRunning {
		t.Errorf("failover without a loop: %+v", st.Detector)
	}
}

func TestFailoverIsForgottenAfterARestartOfTheNAS(t *testing.T) {
	r := newRig(t, true)
	r.up()
	r.crashLoop("Unsupported configuration key: NPU_TURBO")
	r.look()
	r.look()
	if st := r.m.Status(context.Background()); st.Detector.Active != DetectorIGPU || st.Detector.Failover.Reason != ReasonNPUTurbo {
		t.Fatalf("%+v", st.Detector)
	}
	// titand alone restarts: the failover stays.
	base := r.m.FrigateBase("")
	r.m = r.manager(base)
	_ = r.m.Load()
	r.look()
	if st := r.m.Status(context.Background()); st.Detector.Active != DetectorIGPU || st.Detector.Failover == nil {
		t.Errorf("after a restart of titand: %+v", st.Detector)
	}
	// The NAS restarts: the NPU is tried again.
	r.engine.Exit(0)
	r.m = r.manager(base)
	r.m.bootID = func() string { return "boot-2" }
	_ = r.m.Load()
	r.look()
	r.look()
	st := r.m.Status(context.Background())
	if st.Detector.Active != DetectorNPU || st.Detector.Failover != nil || st.State != PhaseRunning || !strings.Contains(r.config(), "device: NPU") {
		t.Errorf("after a restart of the NAS: %+v state %s", st.Detector, st.State)
	}
	// A container left stopped by the restart is no crash.
	r.m.mu.Lock()
	crashes := len(r.m.rt.crashes)
	r.m.mu.Unlock()
	if crashes != 0 {
		t.Errorf("%d ends counted for a container the restart stopped", crashes)
	}
}

func TestChoosingADeviceResetsTheFailover(t *testing.T) {
	r := newRig(t, true)
	r.up()
	r.crashLoop("Unsupported configuration key: NPU_TURBO")
	r.look()
	s := r.m.Settings()
	s.Detector.Kind = DetectorCPU
	if err := r.m.SetSettings(context.Background(), s, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	if st := r.m.Status(context.Background()); st.Detector.Active != DetectorCPU || st.Detector.Failover != nil || !strings.Contains(r.config(), "device: CPU") {
		t.Errorf("%+v", st.Detector)
	}
}

func TestRefusedConfigStopsWithoutFailover(t *testing.T) {
	r := newRig(t, true)
	r.enable()
	r.frigate.Set(func(f *DevFrigate) { f.SafeMode = true })
	r.engine.SetLog("***    Config Validation Errors    ***\nKey     : cameras -> x\nMessage : Extra inputs are not permitted\nStarting Frigate in safe mode.")
	r.look()
	r.look()
	st := r.m.Status(context.Background())
	if st.State != PhaseStopped || st.Error == nil || st.Error.Code != ReasonConfigInvalid || st.Detector.Failover != nil || st.Detector.Active != DetectorNPU {
		t.Errorf("state %s error %+v detector %+v", st.State, st.Error, st.Detector)
	}
	if got := r.noted(); len(got) != 1 || !strings.HasPrefix(got[0], "error nvr_stopped") {
		t.Errorf("messages: %v", got)
	}
}

func TestNeverHealthyFailsOver(t *testing.T) {
	r := newRig(t, true)
	r.enable()
	r.frigate.Set(func(f *DevFrigate) { f.Down = true })
	r.look()
	for range 4 {
		r.advance(time.Minute)
		r.look()
	}
	if st := r.m.Status(context.Background()); st.State != PhaseStarting || st.Detector.Failover != nil {
		t.Fatalf("within the grace time: %s %+v", st.State, st.Detector)
	}
	r.advance(2 * time.Minute)
	r.look()
	st := r.m.Status(context.Background())
	if st.Detector.Active != DetectorIGPU || st.Detector.Failover == nil || st.Detector.Failover.Reason != ReasonNotHealthy {
		t.Errorf("%+v", st.Detector)
	}
}

func TestDeadDetectorFailsOver(t *testing.T) {
	r := newRig(t, true)
	r.up()
	r.frigate.Set(func(f *DevFrigate) { f.NoDetector = true })
	r.engine.SetLog("frigate.watchdog INFO : Detection appears to be stuck. Restarting detection process...")
	for i := range 3 {
		if st := r.m.Status(context.Background()); st.Detector.Failover != nil {
			t.Fatalf("failover after %d looks", i)
		}
		r.advance(10 * time.Second)
		r.look()
	}
	if st := r.m.Status(context.Background()); st.Detector.Active != DetectorIGPU || st.Detector.Failover == nil {
		t.Errorf("%+v", st.Detector)
	}
}

func TestHangingFrigateIsRestarted(t *testing.T) {
	r := newRig(t, true)
	r.up()
	starts := 1
	// hang lets Frigate stop answering for six minutes, looked at like the
	// watch does, and answer again.
	hang := func() {
		t.Helper()
		r.frigate.Set(func(f *DevFrigate) { f.Down = true })
		r.advance(time.Minute)
		r.look()
		if r.engine.Starts != starts {
			t.Fatalf("restarted after a minute of silence")
		}
		// Two looks without an answer after five minutes are not enough.
		r.advance(5 * time.Minute)
		r.look()
		if r.engine.Starts != starts {
			t.Fatalf("restarted after two missed looks")
		}
		r.advance(10 * time.Second)
		r.look()
		r.frigate.Set(func(f *DevFrigate) { f.Down = false })
	}
	hang()
	starts++
	if r.engine.Starts != starts || r.phase() != PhaseStarting {
		t.Fatalf("starts %d phase %s", r.engine.Starts, r.phase())
	}
	r.look()
	if r.phase() != PhaseRunning {
		t.Fatalf("phase %s", r.phase())
	}
	hang()
	starts++
	r.look()
	// The third hang within the window counts like the third end: the
	// detection moves on.
	hang()
	st := r.m.Status(context.Background())
	if st.Detector.Active != DetectorIGPU || st.Detector.Failover == nil || r.engine.Starts != starts {
		t.Errorf("after three hangs: starts %d, detector %+v", r.engine.Starts, st.Detector)
	}
	r.look()
	if r.engine.Starts != starts+1 || !strings.Contains(r.config(), "device: GPU") {
		t.Errorf("not moved: %d starts", r.engine.Starts)
	}
}

// Frigate runs other cameras than config.yml names (it read no file):
// titand writes the file again and restarts Frigate instead of calling it
// running; when that keeps happening the NVR stops.
func TestOtherCamerasRestartFrigate(t *testing.T) {
	r := newRig(t, true)
	r.enable()
	r.look()
	r.frigate.Set(func(f *DevFrigate) { f.config = map[string]bool{} })
	r.look()
	if r.phase() == PhaseRunning || r.engine.Starts != 1 {
		t.Fatalf("Frigate without cameras counts as running: %s", r.phase())
	}
	r.look()
	if r.engine.Starts != 2 {
		t.Fatalf("not restarted: %d starts", r.engine.Starts)
	}
	r.look()
	if r.phase() != PhaseRunning {
		t.Fatalf("phase %s after the restart", r.phase())
	}
	// It keeps happening: stopped, the config is to blame, not the device.
	r.frigate.Set(func(f *DevFrigate) { f.IgnoreConfig, f.config = true, map[string]bool{} })
	r.m.mu.Lock()
	r.m.rt.healthy = false
	r.m.mu.Unlock()
	for range 8 {
		r.advance(10 * time.Second)
		r.look()
	}
	st := r.m.Status(context.Background())
	if st.State != PhaseStopped || st.Error == nil || st.Error.Code != ReasonConfigInvalid || st.Detector.Failover != nil {
		t.Errorf("state %s error %+v detector %+v", st.State, st.Error, st.Detector)
	}
}

func TestImagePull(t *testing.T) {
	r := newRig(t, false)
	r.engine.PullTime = 50 * time.Millisecond
	r.enable()
	r.look()
	if st := r.m.Status(context.Background()); st.State != PhasePulling || st.Image.State != "downloading" {
		t.Fatalf("%s %+v", st.State, st.Image)
	}
	deadline := time.Now().Add(5 * time.Second)
	for r.m.Status(context.Background()).Image.State != "ready" {
		if time.Now().After(deadline) {
			t.Fatal("the pull did not end")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if st := r.m.Status(context.Background()); st.Image.Progress != 1 || st.Image.Version != FrigateVersion {
		t.Errorf("%+v", st.Image)
	}
	r.look()
	r.look()
	if r.phase() != PhaseRunning {
		t.Errorf("phase %s", r.phase())
	}
}

func TestImagePullFailureIsRetriedLater(t *testing.T) {
	r := newRig(t, false)
	r.engine.PullError = errors.New("registry unreachable")
	r.enable()
	r.look()
	deadline := time.Now().Add(5 * time.Second)
	for r.m.Status(context.Background()).Image.State != "error" {
		if time.Now().After(deadline) {
			t.Fatal("the pull did not fail")
		}
		time.Sleep(10 * time.Millisecond)
	}
	st := r.m.Status(context.Background())
	if st.Image.Error == "" || st.Error == nil || st.Error.Code != "image" {
		t.Errorf("%+v %+v", st.Image, st.Error)
	}
	// Not at once again …
	r.look()
	if r.m.Status(context.Background()).Image.State != "error" {
		t.Error("pulled again at once")
	}
	// … but after a minute.
	r.engine.mu.Lock()
	r.engine.PullError = nil
	r.engine.mu.Unlock()
	r.advance(2 * time.Minute)
	r.look()
	if got := r.m.Status(context.Background()).Image.State; got != "downloading" && got != "ready" {
		t.Errorf("image state %s", got)
	}
}

func TestOfflineCameraIsReportedOnce(t *testing.T) {
	r := newRig(t, true)
	r.up()
	r.frigate.Set(func(f *DevFrigate) { f.Cameras["haustuer"] = 0; f.Cameras["einfahrt"] = 0 })
	// einfahrt does not want the message.
	c, _ := r.m.Camera("einfahrt")
	c.Notify.Kinds = []string{"person"}
	if _, err := r.m.UpdateCamera("einfahrt", c, false); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	for range 4 {
		r.advance(time.Minute)
		r.look()
	}
	if got := r.noted(); len(got) != 0 {
		t.Fatalf("reported before five minutes: %v", got)
	}
	states := r.m.CameraStates()
	if states["haustuer"].Online || !states["haustuer"].Known || states["haustuer"].OfflineSince == nil || !states["garten"].Online {
		t.Errorf("states: %+v", states)
	}
	for range 5 {
		r.advance(time.Minute)
		r.look()
	}
	if got := r.noted(); len(got) != 1 || got[0] != "warning nvr_camera_offline Haustür" {
		t.Errorf("messages: %v", got)
	}
	// Back, and gone again: a new message.
	r.frigate.Set(func(f *DevFrigate) { f.Cameras["haustuer"] = 15 })
	r.look()
	r.frigate.Set(func(f *DevFrigate) { f.Cameras["haustuer"] = 0 })
	for range 7 {
		r.advance(time.Minute)
		r.look()
	}
	if got := r.noted(); len(got) != 2 {
		t.Errorf("messages: %v", got)
	}
	if st := r.m.Status(context.Background()); st.Cameras.Online != 1 {
		t.Errorf("online: %+v", st.Cameras)
	}
}

func TestCameraCRUD(t *testing.T) {
	r := newRig(t, true)
	// The demo cameras are in RAM only.
	if _, err := r.store.Read(camerasFile); err == nil {
		t.Error("demo cameras were written to /config")
	}
	c := NewCamera()
	c.Name = "Haustür"
	c.Source = Source{Kind: "rtsp", MainURL: "rtsp://192.168.1.80/live", User: "u", Password: "p"}
	added, err := r.m.AddCamera(c)
	if err != nil || added.ID != "haustuer_2" || added.Order != 5 {
		t.Fatalf("%+v %v", added, err)
	}
	c.ID = "haustuer"
	if _, err := r.m.AddCamera(c); !errors.Is(err, ErrExists) {
		t.Errorf("double ID: %v", err)
	}
	// An edit without a password keeps it …
	edit := added
	edit.Source.Password = ""
	edit.Name = "Seiteneingang"
	got, err := r.m.UpdateCamera(added.ID, edit, false)
	if err != nil || got.Source.Password != "p" {
		t.Errorf("%+v %v", got.Source, err)
	}
	// … but never sends it to another address or user.
	edit.Source.MainURL = "rtsp://192.168.1.99/live"
	if _, err := r.m.UpdateCamera(added.ID, edit, false); !errors.Is(err, ErrInvalid) {
		t.Errorf("another host kept the password: %v", err)
	}
	edit.Source.MainURL, edit.Source.User = "rtsp://192.168.1.80/live", "other"
	if _, err := r.m.UpdateCamera(added.ID, edit, false); !errors.Is(err, ErrInvalid) {
		t.Errorf("another user kept the password: %v", err)
	}
	edit.Source.User = ""
	if got, err := r.m.UpdateCamera(added.ID, edit, true); err != nil || got.Source.Password != "" {
		t.Errorf("clearing: %+v %v", got.Source, err)
	}
	if _, err := r.m.UpdateCamera("nope", edit, false); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown camera: %v", err)
	}
	// Deleting a camera deletes its rights.
	r.m.Access.Lookup = func(name string) (Account, bool) { return Account{Name: name}, true }
	if err := r.m.Access.SetUser("anna", UserRights{added.ID: {Live: true}}, r.m.CameraIDs()); err != nil {
		t.Fatal(err)
	}
	if err := r.m.RemoveCamera(added.ID); err != nil || r.m.Access.Sees("anna", added.ID) {
		t.Errorf("remove: %v", err)
	}
	if err := r.m.RemoveCamera(added.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove twice: %v", err)
	}
	// The cameras survive a restart, in order.
	again := r.manager("")
	if err := again.Load(); err != nil || !slices.Equal(again.CameraIDs(), []string{"haustuer", "einfahrt", "garten", "garage"}) {
		t.Errorf("after Load: %v %v", again.CameraIDs(), err)
	}
}

func TestSettingsGuards(t *testing.T) {
	r := newRig(t, true)
	m := r.m
	m.Dev = false
	m.Mounted = func() []string { return []string{"tank", "sleepy"} }
	m.Sleeps = func(context.Context) (map[string]bool, bool) { return map[string]bool{"sleepy": true}, true }
	docker, dockerVolume := false, ""
	m.DockerEnabled = func() bool { return docker }
	m.EnableDocker = func(volume string) error { docker, dockerVolume = true, volume; return nil }
	ctx := context.Background()
	if err := m.Enable(ctx, "", SetOptions{}); !errors.Is(err, ErrNoVolume) {
		t.Errorf("without a volume: %v", err)
	}
	if err := m.Enable(ctx, "gone", SetOptions{}); !errors.Is(err, ErrNoVolume) {
		t.Errorf("volume not mounted: %v", err)
	}
	// Recordings keep the disks awake: the administrator is told first.
	if err := m.Enable(ctx, "sleepy", SetOptions{EnableDocker: true}); !errors.Is(err, ErrVolumeSleeps) || docker {
		t.Errorf("sleeping volume: %v", err)
	}
	// Docker is off: only with the administrator's yes.
	if err := m.Enable(ctx, "tank", SetOptions{}); !errors.Is(err, ErrDockerOff) || m.Settings().Enabled {
		t.Errorf("Docker off: %v", err)
	}
	if err := m.Enable(ctx, "tank", SetOptions{EnableDocker: true}); err != nil || !docker || dockerVolume != "tank" || !m.Settings().Enabled {
		t.Errorf("with confirmation: %v docker=%v on %q", err, docker, dockerVolume)
	}
	if err := m.Enable(ctx, "sleepy", SetOptions{ConfirmSleepingVolume: true}); err != nil || m.Settings().Volume != "sleepy" {
		t.Errorf("confirmed sleeping volume: %v", err)
	}
	if err := m.RetryDetector(); err != nil {
		t.Error(err)
	}
	_ = m.Disable(ctx)
	if err := m.RetryDetector(); !errors.Is(err, ErrDisabled) {
		t.Errorf("retry while off: %v", err)
	}
	// The same settings again are not written again.
	writes := r.store.Writes()
	if err := m.SetSettings(ctx, m.Settings(), SetOptions{}); err != nil || r.store.Writes() != writes {
		t.Errorf("unchanged settings written again: %v", err)
	}
}

func TestVolumeGoneStopsTheContainer(t *testing.T) {
	r := newRig(t, true)
	r.up()
	// The volume is being unmounted: it no longer counts as mounted.
	r.m.Dev = false
	r.m.Mounted = func() []string { return nil }
	r.m.Engine = readyEngine{r.engine}
	r.look()
	if st, _ := r.engine.NVRInspect(context.Background()); st.Exists || r.phase() != PhaseNoVolume {
		t.Errorf("container %+v phase %s", st, r.phase())
	}
}

// readyEngine is a DevEngine outside development mode.
type readyEngine struct{ *DevEngine }

// An NVR that is on without a secret (settings of an earlier build) gets
// one, once; Frigate only runs with it.
func TestSecretIsMadeOnce(t *testing.T) {
	r := newRig(t, true)
	if err := r.store.WriteJSON(settingsFile, Settings{Enabled: true, Volume: devVolume, Detector: DetectorSetting{Kind: DetectorAuto}}); err != nil {
		t.Fatal(err)
	}
	if err := r.m.Load(); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	secret := r.m.Settings().ProxySecret
	var stored Settings
	if err := r.store.ReadJSON(settingsFile, &stored); err != nil || stored.ProxySecret != secret || len(secret) < 40 || r.phase() != PhaseRunning {
		t.Fatalf("secret %q, stored %q, phase %s", secret, stored.ProxySecret, r.phase())
	}
	if !strings.Contains(r.config(), "auth_secret: "+secret) {
		t.Error("config.yml without the secret")
	}
	r.look()
	if r.m.Settings().ProxySecret != secret {
		t.Error("the secret changed")
	}
	// Switching off and on keeps it.
	if err := r.m.Disable(context.Background()); err != nil {
		t.Fatal(err)
	}
	r.enable()
	if r.m.Settings().ProxySecret != secret {
		t.Error("the secret changed with switching on")
	}
}

func TestDiagnosis(t *testing.T) {
	r := newRig(t, true)
	r.m.KernelLog = func(context.Context) string {
		return "usb 1-1: new device\nintel_vpu 0000:00:0b.0: [drm] Firmware: intel/vpu/vpu_40xx_v0.0.bin, version: 20250115*MTL_CLIENT_SILICON-release*1905\nintel_vpu 0000:00:0b.0: enabling device\n"
	}
	r.up()
	secret := r.m.Settings().ProxySecret
	r.frigate.Set(func(f *DevFrigate) {
		f.Log = append(f.Log, "ffmpeg.haustuer.detect ERROR : rtsp://viewer:demo@192.168.1.61:554/sub: Connection refused")
		f.Log = append(f.Log, "nginx: X-Proxy-Secret "+secret)
	})
	d := r.m.Diagnosis(context.Background())
	if d.State != PhaseRunning || d.Container.State != "running" || d.Container.StartedAt == nil || !d.ImagePresent || d.ImageVersion != FrigateVersion ||
		!strings.Contains(d.ImageReference, FrigateDigest) || d.RunningVersion == "" {
		t.Errorf("%+v", d)
	}
	if d.Detector.Active != DetectorNPU || d.DetectorPID == nil || *d.DetectorPID != 4711 || d.Detector.InferenceMs == nil {
		t.Errorf("detector: %+v pid %v", d.Detector, d.DetectorPID)
	}
	if d.LogSource != "frigate" || len(d.Log) != 5 || len(d.NPUFirmware) != 2 || !d.AccelDevice {
		t.Errorf("log %d lines from %q, firmware %v", len(d.Log), d.LogSource, d.NPUFirmware)
	}
	// No password, wherever it comes from.
	for _, line := range d.Log {
		if strings.Contains(line, "demo@") || strings.Contains(line, secret) {
			t.Errorf("password in the diagnosis: %s", line)
		}
	}
	if d.Shm.TotalMB == nil || *d.Shm.TotalMB != 384 || len(d.GPUUsage) != 1 || len(d.NPUUsage) != 1 {
		t.Errorf("shm %+v gpu %v npu %v", d.Shm, d.GPUUsage, d.NPUUsage)
	}
	if len(d.Cameras) != 4 || d.Cameras[0].ID != "haustuer" || !d.Cameras[0].Online || d.Cameras[0].FPS != 15 || d.Cameras[3].Known {
		t.Errorf("cameras: %+v", d.Cameras)
	}
	// Frigate is down: the container's own log stands in.
	r.frigate.Set(func(f *DevFrigate) { f.Down = true })
	r.engine.SetLog("s6-rc: info: service frigate started\nrtsp://a:b@192.168.1.61/x failed\n")
	d = r.m.Diagnosis(context.Background())
	if d.LogSource != "container" || len(d.Log) != 2 || strings.Contains(d.Log[1], "a:b@") {
		t.Errorf("fallback log: %q %v", d.LogSource, d.Log)
	}
}

func TestLiveAndSnapshot(t *testing.T) {
	r := newRig(t, true)
	if _, err := r.m.Snapshot(context.Background(), "haustuer", 0); !errors.Is(err, ErrNotRunning) {
		t.Errorf("snapshot while off: %v", err)
	}
	r.up()
	picture, err := r.m.Snapshot(context.Background(), "haustuer", 180)
	if err != nil || len(picture) < 100 || picture[0] != 0xFF || picture[1] != 0xD8 {
		t.Errorf("snapshot: %d bytes, %v", len(picture), err)
	}
	if _, err := r.m.Snapshot(context.Background(), "nope", 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown camera: %v", err)
	}
	// garage is switched off.
	if _, err := r.m.Snapshot(context.Background(), "garage", 0); !errors.Is(err, ErrNotRunning) {
		t.Errorf("disabled camera: %v", err)
	}
	live, err := r.m.Live("haustuer", true)
	if err != nil || !strings.HasPrefix(live, "ws://127.0.0.1:") || !strings.HasSuffix(live, "/live/mse/api/ws?src=haustuer_sub") {
		t.Errorf("live: %q %v", live, err)
	}
	if live, _ := r.m.Live("haustuer", false); !strings.HasSuffix(live, "?src=haustuer") {
		t.Errorf("main: %q", live)
	}
}

// A titand started while Frigate briefly does not answer takes the
// container over without moving the detection (review of 12.11.2026).
func TestAdoptionIsNoFailover(t *testing.T) {
	r := newRig(t, true)
	r.up()
	old := r.m
	r.m = r.manager(old.FrigateBase(""))
	if err := r.m.Load(); err != nil {
		t.Fatal(err)
	}
	r.advance(48 * time.Hour) // the container started long before this titand
	r.frigate.Set(func(f *DevFrigate) { f.Down = true })
	r.look()
	r.frigate.Set(func(f *DevFrigate) { f.Down = false })
	r.look()
	r.m.mu.Lock()
	st := r.m.state
	r.m.mu.Unlock()
	if st.Failover != nil || st.Stopped || r.phase() != PhaseRunning {
		t.Errorf("one missed answer after a titand restart: phase %s, failover %+v, stopped %v", r.phase(), st.Failover, st.Stopped)
	}
}

// Moving the NVR to another volume writes config.yml there before the new
// container starts (review of 12.11.2026).
func TestVolumeChangeWritesTheConfig(t *testing.T) {
	r := newRig(t, true)
	mnt := t.TempDir()
	for _, v := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(mnt, v), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	m := r.m
	m.Dev = false
	m.MountRoot = mnt
	m.Mounted = func() []string { return []string{"a", "b"} }
	if err := m.Enable(context.Background(), "a", SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	if _, err := os.Stat(filepath.Join(mnt, "a", ".titanos", "nvr", "config", "config.yml")); err != nil {
		t.Fatalf("volume a: %v (phase %s)", err, r.phase())
	}
	s := m.Settings()
	s.Volume = "b"
	if err := m.SetSettings(context.Background(), s, SetOptions{ConfirmSleepingVolume: true}); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	spec := r.engine.Spec()
	if spec.Config != filepath.Join(mnt, "b", ".titanos", "nvr", "config") {
		t.Errorf("container mounts %s", spec.Config)
	}
	if _, err := os.Stat(filepath.Join(spec.Config, "config.yml")); err != nil {
		t.Errorf("after the move to volume b there is no config.yml: %v (phase %s)", err, r.phase())
	}
}
