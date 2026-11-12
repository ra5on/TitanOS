package nvr

// Regression tests of the second review of the NVR (12.11.2026). Each was
// a probe that failed before its fix; the comments name the finding.

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

// hookEngine is a DevEngine whose engine can be down, and which runs a
// function once inside NVRInspect: a request of the API that lands while
// the loop waits for Docker.
type hookEngine struct {
	*DevEngine
	mu        sync.Mutex
	down      bool
	onInspect func()
}

func (h *hookEngine) EngineReady() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.down
}

func (h *hookEngine) setDown(down bool) {
	h.mu.Lock()
	h.down = down
	h.mu.Unlock()
}

func (h *hookEngine) NVRInspect(ctx context.Context) (docker.NVRState, error) {
	h.mu.Lock()
	f := h.onInspect
	h.onInspect = nil
	h.mu.Unlock()
	if f != nil {
		f()
	}
	return h.DevEngine.NVRInspect(ctx)
}

// volumeRig is a rig whose NVR works on real folders of volumes below a
// mount root (not development mode); mounted can be changed by the test.
func volumeRig(t *testing.T, volumes ...string) (r *rig, mnt string, mounted *[]string) {
	t.Helper()
	r = newRig(t, true)
	mnt = t.TempDir()
	for _, v := range volumes {
		if err := os.MkdirAll(filepath.Join(mnt, v), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	list := append([]string{}, volumes...)
	mounted = &list
	r.m.Dev, r.m.MountRoot = false, mnt
	r.m.Mounted = func() []string { return append([]string{}, *mounted...) }
	return r, mnt, mounted
}

func volumeConfig(t *testing.T, mnt, volume string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(mnt, volume, ".titanos", "nvr", "config", "config.yml"))
	if err != nil {
		t.Fatalf("config.yml on %s: %v", volume, err)
	}
	return string(data)
}

func globalDays(t *testing.T, cfg string) any {
	t.Helper()
	return dig(t, parse(t, []byte(cfg)), "record", "continuous", "days")
}

func longRetention(t *testing.T, r *rig, id string, days int) {
	t.Helper()
	c, ok := r.m.Camera(id)
	if !ok {
		t.Fatalf("no camera %s", id)
	}
	c.Record.RetainDays = days
	if _, err := r.m.UpdateCamera(context.Background(), id, c, CameraOptions{}); err != nil {
		t.Fatal(err)
	}
}

// C3 (A): recorded on a, moved to b, a camera deleted there, moved back:
// a's config.yml keeps the deleted camera, wherever it was deleted.
func TestDeletedCameraStubFollowsTheVolume(t *testing.T) {
	r, mnt, _ := volumeRig(t, "a", "b")
	m := r.m
	ctx := context.Background()
	longRetention(t, r, "garten", 60)
	move := func(v string) {
		s := m.Settings()
		s.Volume = v
		if err := m.SetSettings(ctx, s, SetOptions{ConfirmSleepingVolume: true}); err != nil {
			t.Fatal(err)
		}
		r.look()
		r.look()
	}
	if err := m.Enable(ctx, "a", SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	move("b")
	if err := m.RemoveCamera("garten"); err != nil {
		t.Fatal(err)
	}
	r.look()
	if cfg := volumeConfig(t, mnt, "b"); !strings.Contains(cfg, "garten:") {
		t.Error("b's config.yml lost the deleted camera")
	}
	move("a")
	cfg := volumeConfig(t, mnt, "a")
	if !strings.Contains(cfg, "garten:") {
		t.Errorf("garten recorded on a for 60 days, deleted while the NVR was on b: a's config.yml has neither camera nor stub")
	}
	if got := globalDays(t, cfg); got != 60 {
		t.Errorf("global days on a: %v", got)
	}
}

// C3 (B): the volume cleared while switched off, a camera deleted then,
// switched on again on the old volume: the deleted camera is a stub.
func TestDeletedCameraStubWithoutVolume(t *testing.T) {
	r, mnt, _ := volumeRig(t, "a")
	m := r.m
	ctx := context.Background()
	longRetention(t, r, "garten", 60)
	if err := m.Enable(ctx, "a", SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	s := m.Settings()
	s.Enabled, s.Volume = false, ""
	if err := m.SetSettings(ctx, s, SetOptions{}); err != nil {
		t.Fatalf("volume \"\" refused: %v", err)
	}
	if err := m.RemoveCamera("garten"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := m.Enable(ctx, "a", SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	cfg := volumeConfig(t, mnt, "a")
	if !strings.Contains(cfg, "garten:") {
		t.Errorf("camera deleted while the volume was \"\": no stub on a; Frigate expires its recordings with the global retention")
	}
	if got := globalDays(t, cfg); got != 60 {
		t.Errorf("global days: %v", got)
	}
}

// C2: titand restarts while the NVR's volume is locked; a new camera
// named like a deleted one never gets its ID.
func TestNewCameraNeverTakesAStubIDWhileTheVolumeIsAway(t *testing.T) {
	r, mnt, mounted := volumeRig(t, "a")
	m := r.m
	ctx := context.Background()
	if err := m.Enable(ctx, "a", SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	if err := m.RemoveCamera("einfahrt"); err != nil {
		t.Fatal(err)
	}
	*mounted = nil
	m2 := r.manager("http://127.0.0.1:1")
	m2.Dev, m2.MountRoot = false, mnt
	m2.Mounted = func() []string { return append([]string{}, *mounted...) }
	if err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	c := NewCamera()
	c.Name, c.Source = "Einfahrt", Source{Kind: "rtsp", MainURL: "rtsp://192.168.1.99/main"}
	added, err := m2.AddCamera(ctx, c, CameraOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if added.ID == "einfahrt" {
		t.Errorf("the new camera took the ID of the deleted one (and with it its recordings)")
	}
	if got := m2.DeletedCameras(); len(got) != 1 || got[0].ID != "einfahrt" {
		t.Errorf("deleted cameras without the volume: %+v", got)
	}
}

// C1: a change that lets Frigate delete recordings that exist now is
// only made when the administrator confirmed it.
func TestRecordingsShrinkNeedsConfirmation(t *testing.T) {
	for _, step := range []struct {
		from, to         string
		fromDays, toDays int
		shrinks          bool
	}{
		{RecordContinuous, RecordMotion, 14, 14, true},
		{RecordContinuous, RecordEvents, 14, 14, true},
		{RecordMotion, RecordEvents, 14, 14, true},
		{RecordOff, RecordEvents, 14, 14, true},
		{RecordOff, RecordMotion, 14, 14, true},
		{RecordContinuous, RecordContinuous, 14, 7, true},
		{RecordOff, RecordOff, 14, 7, true},
		{RecordContinuous, RecordOff, 14, 7, true},
		{RecordContinuous, RecordOff, 14, 14, false},
		{RecordEvents, RecordOff, 14, 14, false},
		{RecordMotion, RecordOff, 14, 14, false},
		{RecordOff, RecordContinuous, 14, 14, false},
		{RecordEvents, RecordMotion, 14, 14, false},
		{RecordMotion, RecordContinuous, 14, 14, false},
		{RecordEvents, RecordEvents, 14, 30, false},
	} {
		r := newRig(t, true)
		ctx := context.Background()
		c, _ := r.m.Camera("haustuer")
		c.Record.Mode, c.Record.RetainDays = step.from, step.fromDays
		if _, err := r.m.UpdateCamera(ctx, "haustuer", c, CameraOptions{}); err != nil {
			t.Fatalf("%+v: setting up: %v", step, err)
		}
		c.Record.Mode, c.Record.RetainDays = step.to, step.toDays
		_, err := r.m.UpdateCamera(ctx, "haustuer", c, CameraOptions{})
		if got := err != nil; got != step.shrinks {
			t.Errorf("%s %d → %s %d without confirmation: %v (want refused: %v)", step.from, step.fromDays, step.to, step.toDays, err, step.shrinks)
		}
	}
}

// C4: without cameras, or with short ones, the global retention (what
// Frigate keeps of cameras it does not know) is 30 days.
func TestGlobalRetentionFloor(t *testing.T) {
	s := Settings{ProxySecret: testSecret}
	days := func(cameras []Camera, deleted []DeletedCamera) (any, any) {
		out, err := Render(s, cameras, deleted, RenderHardware{Detector: DetectorCPU})
		if err != nil {
			t.Fatal(err)
		}
		cfg := parse(t, out)
		return dig(t, cfg, "record", "continuous", "days"), dig(t, cfg, "record", "motion", "days")
	}
	if c, mo := days(nil, nil); c != 30 || mo != 30 {
		t.Errorf("no cameras: %v %v", c, mo)
	}
	cam := NewCamera()
	cam.ID, cam.Name, cam.Source = "cam", "Cam", Source{Kind: "rtsp", MainURL: "rtsp://192.168.1.5/main"}
	cam.Record.RetainDays = 3
	if c, mo := days([]Camera{cam}, nil); c != 30 || mo != 30 {
		t.Errorf("short camera: %v %v", c, mo)
	}
	cam.Record.RetainDays = 45
	stub := DeletedCamera{ID: "alt", Name: "Alt", RetainDays: 90, DeletedAt: time.Unix(1_700_000_000, 0)}
	if c, mo := days([]Camera{cam}, []DeletedCamera{stub}); c != 90 || mo != 90 {
		t.Errorf("long stub: %v %v", c, mo)
	}
}

// C4: the global floor is safe only because no camera inherits it:
// Frigate merges the global record block into every camera field by
// field, so each camera block sets both days itself.
func TestEveryCameraSetsItsOwnDays(t *testing.T) {
	var cameras []Camera
	for i, mode := range []string{RecordContinuous, RecordMotion, RecordEvents, RecordOff} {
		c := NewCamera()
		c.ID, c.Name = "cam_"+mode, "Cam "+mode
		c.Source = Source{Kind: "rtsp", MainURL: "rtsp://192.168.1." + string(rune('5'+i)) + "/main"}
		c.Record = Record{Mode: mode, RetainDays: 7, PreSeconds: 5, PostSeconds: 5}
		c.Enabled = i%2 == 0
		cameras = append(cameras, c)
	}
	stub := DeletedCamera{ID: "alt", Name: "Alt", RetainDays: 5, DeletedAt: time.Unix(1_700_000_000, 0)}
	out, err := Render(Settings{ProxySecret: testSecret}, cameras, []DeletedCamera{stub}, RenderHardware{Detector: DetectorCPU})
	if err != nil {
		t.Fatal(err)
	}
	cams := dig(t, parse(t, out), "cameras").(map[string]any)
	if len(cams) != 5 {
		t.Fatalf("cameras: %v", cams)
	}
	// And every camera of the golden files (checked against Frigate's own
	// merge in the pinned image, docs/NVR.md).
	for name, tc := range renderCases() {
		golden, err := Render(tc.settings, tc.cameras, tc.deleted, tc.hw)
		if err != nil {
			t.Fatal(err)
		}
		if blocks, ok := dig(t, parse(t, golden), "cameras").(map[string]any); ok {
			for id, block := range blocks {
				cams[name+"/"+id] = block
			}
		}
	}
	for id, block := range cams {
		record, _ := block.(map[string]any)["record"].(map[string]any)
		for _, field := range []string{"continuous", "motion"} {
			if _, ok := record[field].(map[string]any)["days"]; !ok {
				t.Errorf("%s: record.%s.days not set (it would inherit the global floor)", id, field)
			}
		}
	}
}

// R1: a change of the settings while the loop is inside a look (between
// reading the settings and clearing dirty) is applied by the next look.
func TestSettingsChangeDuringALookIsApplied(t *testing.T) {
	r := newRig(t, true)
	r.up()
	hook := &hookEngine{DevEngine: r.engine}
	r.m.Engine = hook
	hook.onInspect = func() {
		s := r.m.Settings()
		s.RTSPOut = true
		if err := r.m.SetSettings(context.Background(), s, SetOptions{}); err != nil {
			t.Error(err)
		}
	}
	for range 4 {
		r.look()
	}
	if !strings.Contains(r.config(), "username: titanos") {
		t.Error("rtspOut is on in the settings, but config.yml never got it")
	}
	if ports := r.engine.Spec().Ports; len(ports) != 3 {
		t.Errorf("container ports: %+v", ports)
	}
}

// R1: a camera added while the loop plans is written by the next look.
func TestCameraAddedDuringALookIsApplied(t *testing.T) {
	r := newRig(t, true)
	r.up()
	r.m.mu.Lock()
	r.m.rt.dirty = true
	r.m.mu.Unlock()
	once := true
	r.m.Addresses = func() []string {
		if once {
			once = false
			c := NewCamera()
			c.Name, c.Source = "Neu", Source{Kind: "rtsp", MainURL: "rtsp://192.168.1.77/main"}
			if _, err := r.m.AddCamera(context.Background(), c, CameraOptions{}); err != nil {
				t.Error(err)
			}
		}
		return []string{"192.168.1.10"}
	}
	for range 4 {
		r.look()
	}
	if _, ok := r.m.Camera("neu"); !ok {
		t.Fatal("camera not stored")
	}
	if !strings.Contains(r.config(), "neu:") {
		t.Error("camera neu is stored on the stick but never reaches config.yml")
	}
}

// R1: a camera added while the loop waits for Docker is written by the
// next look.
func TestCameraAddedWhileTheLoopWaitsForDockerIsApplied(t *testing.T) {
	r := newRig(t, true)
	r.up()
	hook := &hookEngine{DevEngine: r.engine}
	r.m.Engine = hook
	hook.onInspect = func() {
		c := NewCamera()
		c.Name, c.Source = "Neu", Source{Kind: "rtsp", MainURL: "rtsp://192.168.1.77/main"}
		if _, err := r.m.AddCamera(context.Background(), c, CameraOptions{}); err != nil {
			t.Error(err)
		}
	}
	for range 3 {
		r.look()
	}
	if !strings.Contains(r.config(), "neu:") {
		t.Error("camera neu is stored on the stick but never reaches config.yml")
	}
}

// R1: a change of the volume while the loop waits for Docker moves the
// container to the new volume.
func TestVolumeChangeDuringALookIsApplied(t *testing.T) {
	r, mnt, _ := volumeRig(t, "a", "b")
	m := r.m
	ctx := context.Background()
	if err := m.Enable(ctx, "a", SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	hook := &hookEngine{DevEngine: r.engine}
	m.Engine = hook
	hook.onInspect = func() {
		s := m.Settings()
		s.Volume = "b"
		if err := m.SetSettings(ctx, s, SetOptions{ConfirmSleepingVolume: true}); err != nil {
			t.Error(err)
		}
	}
	for range 4 {
		r.look()
	}
	if spec := r.engine.Spec().Config; !strings.HasPrefix(spec, filepath.Join(mnt, "b")) {
		t.Errorf("settings name volume b, Frigate records on %s", spec)
	}
}

// R1 (not probed): after a change of the volume the container lets go of
// the old one even when the new one cannot be used yet.
func TestOldVolumeIsLetGoAfterAChange(t *testing.T) {
	r, mnt, _ := volumeRig(t, "a", "b")
	m := r.m
	ctx := context.Background()
	if err := m.Enable(ctx, "a", SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	if !strings.HasPrefix(r.engine.Spec().Config, filepath.Join(mnt, "a")) {
		t.Fatalf("not on a: %s", r.engine.Spec().Config)
	}
	// b's private folder cannot be made: the look fails on b.
	if err := os.WriteFile(filepath.Join(mnt, "b", ".titanos"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	s := m.Settings()
	s.Volume = "b"
	if err := m.SetSettings(ctx, s, SetOptions{ConfirmSleepingVolume: true}); err != nil {
		t.Fatal(err)
	}
	r.look()
	if st, _ := r.engine.NVRInspect(ctx); st.Exists && strings.HasPrefix(r.engine.Spec().Config, filepath.Join(mnt, "a")) {
		t.Errorf("the settings name b, the container still runs on a (phase %s)", r.phase())
	}
}

// R2: after a failover NPU → iGPU the iGPU goes to a VM: the detection
// goes on down the chain (CPU), never back to the NPU; when the iGPU
// comes back it is taken again.
func TestFailoverGoesOnWhenTheNextDeviceIsLeased(t *testing.T) {
	m := New(config.Open(t.TempDir()))
	m.facts = func() hostFacts { return hostFacts{accel: true, avx2: true, memory: 16 << 30} }
	devices := []hardware.ContainerDevice{
		{ID: "npu0", Kind: "npu", Vendor: "8086", Title: "NPU", Available: true, Mappings: []string{"/dev/accel/accel0:/dev/accel/accel0"}},
		{ID: "igpu0", Kind: "igpu", Vendor: "8086", Title: "iGPU", Available: true, Mappings: []string{"/dev/dri/renderD128:/dev/dri/renderD128"}},
	}
	s := Settings{Enabled: true, Volume: "tank", Detector: DetectorSetting{Kind: DetectorNPU}, ProxySecret: testSecret}
	m.state = State{Active: DetectorIGPU, Failover: &Failover{From: DetectorNPU, To: DetectorIGPU, Reason: ReasonNPUFirmware}}
	plan := func(leased map[string]bool) string {
		p, err := m.makePlan(s, nil, nil, probeHardware(devices, leased, m.facts()), "sha256:x", "/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		return p.active
	}
	if got := plan(nil); got != DetectorIGPU {
		t.Fatalf("iGPU free: %s", got)
	}
	if got := plan(map[string]bool{"igpu0": true}); got != DetectorCPU {
		t.Errorf("iGPU with a VM: plan runs on %s, want cpu (never back to the failed NPU)", got)
	}
	if got := plan(nil); got != DetectorIGPU {
		t.Errorf("iGPU back from the VM: plan runs on %s, want igpu", got)
	}
}

// R2 through the loop: the device the failover chose goes to a VM and
// comes back; the NVR runs on the CPU meanwhile, returns to it, never to
// the device that failed, and says "failover" once.
func TestFailoverFollowsTheLeasedDevice(t *testing.T) {
	r, _, _ := volumeRig(t, "a")
	m := r.m
	m.facts = func() hostFacts { return hostFacts{accel: true, avx2: true, memory: 16 << 30} }
	devices := []hardware.ContainerDevice{
		{ID: "npu0", Kind: "npu", Vendor: "8086", Title: "NPU", Available: true, Mappings: []string{"/dev/accel/accel0:/dev/accel/accel0"}},
		{ID: "igpu0", Kind: "igpu", Vendor: "8086", Title: "iGPU", Available: true, Mappings: []string{"/dev/dri/renderD128:/dev/dri/renderD128"}},
	}
	var lmu sync.Mutex
	leased := map[string]bool{}
	m.Devices = func(context.Context) []hardware.ContainerDevice { return devices }
	m.Leased = func() map[string]bool {
		lmu.Lock()
		defer lmu.Unlock()
		out := map[string]bool{}
		for k, v := range leased {
			out[k] = v
		}
		return out
	}
	lease := func(on bool) {
		lmu.Lock()
		leased["igpu0"] = on
		lmu.Unlock()
	}
	ctx := context.Background()
	s := m.Settings()
	s.Enabled, s.Volume, s.Detector.Kind = true, "a", DetectorNPU
	if err := m.SetSettings(ctx, s, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	r.crashLoop("RuntimeError: [ GENERAL_ERROR ] MAPPED_INFERENCE_VERSION is NOT compatible with the ELF Expected: 7.0 received: 11.0\n")
	r.look()
	r.look()
	active := func() string {
		m.mu.Lock()
		defer m.mu.Unlock()
		return m.activeLocked()
	}
	if got := active(); got != DetectorIGPU || !strings.Contains(strings.Join(r.engine.Spec().Devices, " "), "renderD128") {
		t.Fatalf("after the failover: %s, devices %v", got, r.engine.Spec().Devices)
	}
	lease(true)
	r.look()
	r.look()
	if got := active(); got != DetectorCPU || strings.Contains(strings.Join(r.engine.Spec().Devices, " "), "renderD128") {
		t.Errorf("iGPU with a VM: runs on %s with %v, want cpu without the iGPU", got, r.engine.Spec().Devices)
	}
	if m.Status(ctx).Detector.Active != DetectorCPU {
		t.Errorf("status shows %+v", m.Status(ctx).Detector)
	}
	lease(false)
	r.look()
	r.look()
	if got := active(); got != DetectorIGPU || !strings.Contains(strings.Join(r.engine.Spec().Devices, " "), "renderD128") {
		t.Errorf("iGPU back: runs on %s with %v, want igpu", got, r.engine.Spec().Devices)
	}
	failovers := 0
	for _, note := range r.noted() {
		if strings.Contains(note, "nvr_failover") {
			failovers++
		}
	}
	if failovers != 1 {
		t.Errorf("failover messages: %v", r.noted())
	}
}

// M1: camera rights that could not be read at the start are never
// replaced by a change.
func TestUnreadableRightsAreNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	store := config.Open(dir)
	if err := store.WriteJSON(accessFile, accessSettings{Cameras: map[string]CameraAccess{"haustuer": {Live: []string{"anna"}, Recordings: []string{"anna"}}}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, accessFile)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	a := NewAccess(store)
	a.Lookup = func(name string) (Account, bool) { return Account{Name: name, Created: time.Unix(1, 0)}, true }
	loadErr := a.Load()
	_ = os.Chmod(path, 0o600)
	if loadErr == nil {
		t.Skip("file readable despite mode 0 (running as root?)")
	}
	if err := a.SetUser("ben", UserRights{"garage": {Live: true}}, []string{"haustuer", "garage"}); err == nil {
		t.Error("a change of the rights was accepted although they could not be read")
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "anna") {
		t.Errorf("anna's rights were replaced: %s", data)
	}
}

// M1: exports.json that could not be read is never replaced by a change.
func TestUnreadableExportsAreNotOverwritten(t *testing.T) {
	r := newRig(t, true)
	r.up()
	path := filepath.Join(r.dir, exportsFile)
	if err := os.WriteFile(path, []byte(`{"x":{"owner":"anna","from":1,"to":2}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("file readable despite mode 0 (running as root?)")
	}
	// As after a start of titand: nothing read yet.
	r.m.mu.Lock()
	r.m.rt.exports = nil
	r.m.mu.Unlock()
	if _, err := r.m.CreateExport(context.Background(), "ben", "haustuer", 1_700_000_000, 1_700_000_600, ""); !errors.Is(err, ErrExportsUnreadable) {
		t.Errorf("export started although its owner could not be noted: %v", err)
	}
	err := r.m.changeExportRecords(func(records map[string]exportRecord) {
		records["y"] = exportRecord{Owner: "ben", From: 3, To: 4}
	})
	_ = os.Chmod(path, 0o600)
	if err == nil {
		t.Error("a change of the exports was accepted although they could not be read")
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "anna") {
		t.Errorf("exports.json was replaced: %s", data)
	}
}

// M2: Docker seen down by titand (or switched off and on) is no crash of
// Frigate: the container found ended afterwards is started again without
// counting.
func TestDockerDownIsNoCrash(t *testing.T) {
	r, _, _ := volumeRig(t, "a")
	hook := &hookEngine{DevEngine: r.engine}
	r.m.Engine = hook
	if err := r.m.Enable(context.Background(), "a", SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.look()
	r.look()
	if r.phase() != PhaseRunning {
		t.Fatalf("phase %s", r.phase())
	}
	for range r.m.CrashLimit + 1 {
		hook.setDown(true)
		r.look()
		r.engine.Exit(0)
		hook.setDown(false)
		r.advance(20 * time.Second)
		r.look()
		r.look()
	}
	r.m.mu.Lock()
	failover, stopped, crashes := r.m.state.Failover, r.m.state.Stopped, len(r.m.rt.crashes)
	r.m.mu.Unlock()
	if failover != nil || stopped || crashes != 0 || r.phase() != PhaseRunning {
		t.Errorf("Docker restarts counted as crashes: %d, failover %+v, stopped %v, phase %s", crashes, failover, stopped, r.phase())
	}
}

// M4: stills are cached for a few fixed heights only.
func TestSnapshotHeightsAreFew(t *testing.T) {
	r := newRig(t, true)
	r.up()
	ctx := context.Background()
	for h := 90; h <= 2160; h += 7 {
		if _, err := r.m.Snapshot(ctx, "haustuer", h); err != nil {
			t.Fatal(err)
		}
	}
	r.m.mu.Lock()
	cached := len(r.m.snapshots)
	r.m.mu.Unlock()
	if cached > 8 {
		t.Errorf("%d stills cached for one camera", cached)
	}
}

// M5: a library larger than a backup takes is left out as a whole: not
// the library as it was read before.
func TestTooLargeFaceLibraryIsLeftOut(t *testing.T) {
	r := newRig(t, true)
	r.up()
	writeFace(t, r.dir, "Anna", "Anna-1.webp", 100)
	if got := r.m.BackupFiles(); len(got) != 1 {
		t.Fatalf("small library: %d", len(got))
	}
	for i := range 7 {
		writeFace(t, r.dir, "Big", "Big-"+string(rune('a'+i))+".webp", maxBackupFaceFile)
	}
	if got := r.m.BackupFiles(); len(got) != 0 {
		t.Errorf("too large: %d files sent", len(got))
	}
	// The disks sleep now: still nothing of the old library.
	r.m.Asleep = func(string) bool { return true }
	if got := r.m.BackupFiles(); len(got) != 0 {
		t.Errorf("too large, asleep: %d files sent", len(got))
	}
}

// M6: a restored face that cannot be taken off the stick is tried once
// per start of titand, not at every look.
func TestRestoredFacesLeaveTheStickOncePerStart(t *testing.T) {
	r := newRig(t, true)
	if err := r.store.Write("volume-data/nvr/faces/Anna/Anna-1.webp", []byte("restored")); err != nil {
		t.Fatal(err)
	}
	r.m = r.manager(r.m.FrigateBase(""))
	if err := r.m.Load(); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(r.store.Dir(), "volume-data", "nvr", "faces", "Anna")
	if err := os.Chmod(folder, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(folder, 0o700) })
	r.enable()
	r.look()
	if _, err := os.Stat(filepath.Join(folder, "Anna-1.webp")); err != nil {
		t.Skip("the stick folder was writable despite mode 0500 (running as root?)")
	}
	picture := filepath.Join(r.dir, "media", "clips", "faces", "Anna", "Anna-1.webp")
	if err := os.Remove(picture); err != nil {
		t.Fatalf("not put back: %v", err)
	}
	r.look()
	r.look()
	if _, err := os.Stat(picture); err == nil {
		t.Error("the import ran again at a later look")
	}
}

// M7: changes of the settings are made one after the other.
func TestSettingsChangesAreSerialized(t *testing.T) {
	r, _, _ := volumeRig(t, "a")
	m := r.m
	ctx := context.Background()
	docker := false
	m.DockerEnabled = func() bool { return docker }
	var other chan error
	m.EnableDocker = func(string) error {
		other = make(chan error, 1)
		go func() {
			s := m.Settings()
			s.RTSPOut = true
			other <- m.SetSettings(ctx, s, SetOptions{})
		}()
		select {
		case <-other:
			t.Error("a second change went through while the first was being made")
			other = nil
		case <-time.After(200 * time.Millisecond):
		}
		docker = true
		return nil
	}
	if err := m.Enable(ctx, "a", SetOptions{EnableDocker: true}); err != nil {
		t.Fatal(err)
	}
	if other != nil {
		if err := <-other; err != nil {
			t.Fatal(err)
		}
	}
	var stored Settings
	if err := r.store.ReadJSON(settingsFile, &stored); err != nil {
		t.Fatal(err)
	}
	if got := m.Settings(); got.Enabled != stored.Enabled || got.RTSPOut != stored.RTSPOut || got.Volume != stored.Volume {
		t.Errorf("RAM %+v, stick %+v", got, stored)
	}
}

// M3: an error of the HTTP client never carries the path it asked for: a
// face's path holds the person's name, and such errors are logged.
func TestFrigateErrorsHoldNoPath(t *testing.T) {
	err := withoutURL(&url.Error{Op: "Post", URL: "http://172.22.0.2:8971/api/faces/Anna%20Beispiel/create", Err: context.DeadlineExceeded})
	if strings.Contains(err.Error(), "Anna") || strings.Contains(err.Error(), "/api/") || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("%v", err)
	}
	if withoutURL(nil) != nil {
		t.Error("no error became one")
	}
}
