package nvr

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

// The review of PLAN 82 (13.11.2026): each finding as a test.

// cdiRig is a NAS with an NPU, an iGPU and an NVIDIA GPU whose CDI device
// Docker refuses at the start, as Docker 26.1.5 does (measured: the create
// succeeds, the start answers 500).
func cdiRig(t *testing.T, s Settings) *rig {
	t.Helper()
	r, _, _ := volumeRig(t, "a")
	r.m.Devices = func(context.Context) []hardware.ContainerDevice {
		return []hardware.ContainerDevice{devNPU, devIGPU, devNV}
	}
	r.engine.RefuseCDI = true
	s.Enabled, s.Volume = true, "a"
	if err := r.m.SetSettings(context.Background(), s, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	return r
}

// settle looks n times, 20 s apart.
func (r *rig) settle(n int) {
	for range n {
		r.look()
		r.advance(20 * time.Second)
	}
}

// cdiState is what the tests of a refused CDI device check: the NVR runs
// on the CPU decoder, the detection was not blamed.
func (r *rig) expectCPUDecoding(what, detector string) {
	r.t.Helper()
	if got := r.phase(); got != PhaseRunning {
		r.t.Errorf("%s: phase %s, want running (the cameras record)", what, got)
	}
	if spec := r.engine.Spec(); len(spec.CDIDevices) != 0 {
		r.t.Errorf("%s: the container still has %v", what, spec.CDIDevices)
	}
	r.m.mu.Lock()
	crashes, failover, stopped, active := len(r.m.rt.crashes), r.m.state.Failover, r.m.state.Stopped, r.m.activeLocked()
	r.m.mu.Unlock()
	if crashes != 0 || failover != nil || stopped || active != detector {
		r.t.Errorf("%s: the detector was blamed: crashes %d, failover %+v, stopped %v, detector %s", what, crashes, failover, stopped, active)
	}
	for _, note := range r.noted() {
		if strings.Contains(note, "nvr_failover") || strings.Contains(note, "nvr_stopped") {
			r.t.Errorf("%s: message %q", what, note)
		}
	}
	st := r.m.Status(context.Background())
	if st.Decoder.Active != DecoderCPU || st.Decoder.Reason != "driver" || st.Decoder.Setting != devNV.ID || st.Hardware.VideoDecode {
		r.t.Errorf("%s: status decoder %+v (videoDecode %v)", what, st.Decoder, st.Hardware.VideoDecode)
	}
	if !strings.Contains(r.configOn("a"), `hwaccel_args: ""`) {
		r.t.Errorf("%s: the CPU does not decode in config.yml", what)
	}
}

// Finding 1: the NVIDIA GPU is chosen on a NAS whose Docker refuses its CDI
// device at the start: the container is made at once with the CPU
// decoding, Frigate's restarts do not count as crashes, the detector is
// not moved; "Wieder versuchen" tries the GPU again and falls back again.
func TestCDIRefusedAtStartFallsBackToTheCPU(t *testing.T) {
	r := cdiRig(t, Settings{Detector: DetectorSetting{Kind: DetectorNPU}, Decoder: devNV.ID})
	r.settle(4)
	if r.engine.Creates != 2 {
		t.Errorf("%d creates, want 2 (the GPU refused at the start, then the CPU)", r.engine.Creates)
	}
	r.expectCPUDecoding("first start", DetectorNPU)
	// More looks change nothing.
	r.settle(10)
	if r.engine.Creates != 2 {
		t.Errorf("made anew %d times while it runs", r.engine.Creates)
	}
	// "Wieder versuchen" while Docker still refuses: tried, refused, back
	// on the CPU.
	if err := r.m.RetryDetector(); err != nil {
		t.Fatal(err)
	}
	r.settle(4)
	r.expectCPUDecoding("after trying again", DetectorNPU)
	// Docker has the descriptions now.
	r.engine.RefuseCDI = false
	if err := r.m.RetryDetector(); err != nil {
		t.Fatal(err)
	}
	r.settle(3)
	if spec := r.engine.Spec(); !slices.Equal(spec.CDIDevices, []string{"nvidia.com/gpu=GPU-1"}) || r.phase() != PhaseRunning {
		t.Errorf("with Docker's descriptions: %v, phase %s", spec.CDIDevices, r.phase())
	}
	if st := r.m.Status(context.Background()); st.Decoder.Active != devNV.ID || st.Decoder.Reason != "" || !st.Hardware.VideoDecode {
		t.Errorf("status decoder %+v", st.Decoder)
	}
}

// Finding 1, from a running NVR: the CPU decodes, the NPU detects, the
// owner chooses the NVIDIA GPU.
func TestCDIRefusedWhenChosenWhileRunning(t *testing.T) {
	r := cdiRig(t, Settings{Detector: DetectorSetting{Kind: DetectorAuto}, Decoder: DecoderCPU})
	r.settle(3)
	if r.phase() != PhaseRunning {
		t.Fatalf("phase %s", r.phase())
	}
	s := r.m.Settings()
	s.Decoder = devNV.ID
	if err := r.m.SetSettings(context.Background(), s, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.settle(20)
	r.expectCPUDecoding("chosen while running", DetectorNPU)
}

// Finding 1, the start in ended(): the container ran with the NVIDIA GPU;
// after a restart of Docker (or of the NAS) the plugin's descriptions are
// missing, and after a crash of Frigate the driver is gone. Neither counts
// as a crash; the CPU decodes.
func TestCDIRefusedWhenStartedAgain(t *testing.T) {
	for _, how := range []string{"engine restarted", "frigate ended"} {
		t.Run(how, func(t *testing.T) {
			r := cdiRig(t, Settings{Detector: DetectorSetting{Kind: DetectorNPU}, Decoder: devNV.ID})
			r.engine.RefuseCDI = false
			r.settle(3)
			if spec := r.engine.Spec(); len(spec.CDIDevices) != 1 || r.phase() != PhaseRunning {
				t.Fatalf("the GPU is not used: %v, %s", spec.CDIDevices, r.phase())
			}
			r.engine.RefuseCDI = true
			if how == "engine restarted" {
				r.engine.RestartEngine()
			} else {
				r.engine.Exit(1)
			}
			r.settle(4)
			r.expectCPUDecoding(how, DetectorNPU)
		})
	}
}

// Finding 4: only a refusal of the CDI device marks the GPU; a timeout or
// a full disk at the create is an error of the engine, retried with the
// GPU.
func TestCDIOnlyCDIErrorsMarkTheGPU(t *testing.T) {
	cdiCreate := func(spec docker.NVRSpec) error {
		if len(spec.CDIDevices) > 0 {
			return errors.New(`docker: failed to inject CDI devices: unresolvable CDI devices nvidia.com/gpu=GPU-1 (HTTP 500)`)
		}
		return nil
	}
	r := cdiRig(t, Settings{Detector: DetectorSetting{Kind: DetectorNPU}, Decoder: devNV.ID})
	r.engine.RefuseCDI = false
	r.engine.CreateError = cdiCreate
	r.settle(3)
	r.expectCPUDecoding("refused at the create", DetectorNPU)

	for _, err := range []error{context.DeadlineExceeded, errors.New("docker: write /var/lib/docker/containers/x: no space left on device (HTTP 500)")} {
		r := cdiRig(t, Settings{Detector: DetectorSetting{Kind: DetectorNPU}, Decoder: devNV.ID})
		r.engine.RefuseCDI = false
		r.engine.CreateError = func(docker.NVRSpec) error { return err }
		r.look()
		r.m.mu.Lock()
		broken, phase := len(r.m.rt.brokenDecoders), r.m.rt.phase
		r.m.mu.Unlock()
		if broken != 0 || phase != PhaseError {
			t.Errorf("%v: GPU marked (%d) or phase %s", err, broken, phase)
		}
		// The next try, after the pause, is with the GPU again.
		r.engine.CreateError = nil
		r.advance(errorBackoff + time.Second)
		r.look()
		if spec := r.engine.Spec(); len(spec.CDIDevices) != 1 {
			t.Errorf("%v: the GPU is not tried again: %+v", err, spec)
		}
	}
	for _, text := range []string{
		`could not select device driver "cdi" with capabilities: []`,
		`failed to inject CDI devices: unresolvable CDI devices nvidia.com/gpu=GPU-1`,
		`error gathering device information while adding custom device "nvidia.com/gpu=GPU-1": no such file or directory`,
	} {
		if !docker.IsCDIError(errors.New(text)) {
			t.Errorf("not a CDI error: %s", text)
		}
	}
	for _, text := range []string{"context deadline exceeded", "no space left on device", "conflict: the container name is in use", "cdimage",
		`invalid mount config: bind source path does not exist: /mnt/cdi/frigate`} {
		if docker.IsCDIError(errors.New(text)) {
			t.Errorf("a CDI error: %s", text)
		}
	}
}

// Finding 1: the status names the decoder the container has (as the
// diagnosis), not only what the setting would give: an iGPU that went to
// a VM decodes until the container is made anew.
func TestStatusDecoderIsTheContainers(t *testing.T) {
	r, _, _ := volumeRig(t, "a")
	leased := map[string]bool{}
	r.m.Devices = func(context.Context) []hardware.ContainerDevice { return []hardware.ContainerDevice{devIGPU} }
	r.m.Leased = func() map[string]bool { return leased }
	if err := r.m.SetSettings(context.Background(), Settings{Enabled: true, Volume: "a", Detector: DetectorSetting{Kind: DetectorCPU}}, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	r.settle(2)
	if st := r.m.Status(context.Background()); st.Decoder.Active != devIGPU.ID {
		t.Fatalf("decoder %+v", st.Decoder)
	}
	leased[devIGPU.ID] = true
	r.m.hardwareCached(context.Background(), true)
	if st := r.m.Status(context.Background()); st.Decoder.Active != devIGPU.ID || !st.Hardware.VideoDecode {
		t.Errorf("the container still has the iGPU: status %+v", st.Decoder)
	}
	r.settle(2)
	if st := r.m.Status(context.Background()); st.Decoder.Active != DecoderCPU || st.Hardware.VideoDecode {
		t.Errorf("made anew without the iGPU: status %+v", st.Decoder)
	}
	if d := r.m.Diagnosis(context.Background()); d.Decoder.Active != DecoderCPU {
		t.Errorf("diagnosis %+v", d.Decoder)
	}
}

// Finding 2: "auto" never takes an NVIDIA GPU; an NVIDIA-only NAS
// decodes on the CPU as with 0.78, and its container gets no CDI device.
func TestAutoNeverTakesNVIDIA(t *testing.T) {
	m := New(config.Open(t.TempDir()))
	s := Settings{Enabled: true, Volume: "tank", Detector: DetectorSetting{Kind: DetectorCPU}, ProxySecret: testSecret}
	for _, devices := range [][]hardware.ContainerDevice{{devNV}, {devNV, devNV2}, {devNPU, devNV}} {
		hw := probeHardware(devices, nil, fullHost)
		if hw.VideoDecode {
			t.Errorf("%d devices: auto would decode on a GPU", len(devices))
		}
		p, err := m.makePlan(s, []Camera{testCamera("cam", "Cam", nil)}, nil, hw, "sha256:x", "/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.spec.CDIDevices) != 0 || p.decoder.Active != DecoderCPU || !strings.Contains(string(p.config), `hwaccel_args: ""`) {
			t.Errorf("auto on an NVIDIA NAS: %+v %+v", p.spec, p.decoder)
		}
	}
}

// Finding 3: with a GPU decoding, a stream that is recorded and detected
// is two inputs of the same restream (Frigate then records without the
// GPU); with the CPU it stays one input.
func TestRenderSplitsASharedStreamForTheGPU(t *testing.T) {
	for _, tc := range []struct {
		name   string
		use    StreamUse
		sub    bool
		decode Decode
		want   []string
	}{
		{"one stream, VA-API", StreamUse{}, false, Decode{Kind: DecodeVAAPI}, []string{"cam record", "cam detect"}},
		{"one stream, two VA-API nodes", StreamUse{}, false, Decode{Kind: DecodeVAAPI, Node: "/dev/dri/renderD129"}, []string{"cam record", "cam detect"}},
		{"one stream, NVIDIA", StreamUse{}, false, Decode{Kind: DecodeNVIDIA}, []string{"cam record", "cam detect"}},
		{"one stream, CPU", StreamUse{}, false, Decode{}, []string{"cam record,detect"}},
		{"both on main, GPU", StreamUse{Detect: StreamMain}, true, Decode{Kind: DecodeVAAPI}, []string{"cam record", "cam detect"}},
		{"both on sub, GPU", StreamUse{Record: StreamSub}, true, Decode{Kind: DecodeNVIDIA}, []string{"cam_sub record", "cam_sub detect"}},
		{"both on main, CPU", StreamUse{Detect: StreamMain}, true, Decode{}, []string{"cam record,detect"}},
		{"apart, GPU", StreamUse{}, true, Decode{Kind: DecodeVAAPI}, []string{"cam record", "cam_sub detect"}},
		{"swapped, GPU", StreamUse{Record: StreamSub, Detect: StreamMain}, true, Decode{Kind: DecodeVAAPI}, []string{"cam detect", "cam_sub record"}},
	} {
		c := testCamera("cam", "Cam", func(c *Camera) {
			c.Streams = tc.use
			if !tc.sub {
				c.Source.SubURL = ""
			}
		})
		if got := render(t, []Camera{c}, nil, RenderHardware{Decode: tc.decode}).inputs("cam"); !slices.Equal(got, tc.want) {
			t.Errorf("%s: inputs %v, want %v", tc.name, got, tc.want)
		}
	}
}

// Finding 6: a NAS without devices for the container (CPU detecting and
// decoding, or a Coral and no GPU) keeps the container of 0.78 after the
// update: the same spec, the same hash ("Devices":null). The hashes were
// made with makePlan of v0.78.0 from the same input.
func TestDevicesHashAsIn078(t *testing.T) {
	m := New(config.Open(t.TempDir()))
	for _, tc := range []struct {
		name     string
		devices  []hardware.ContainerDevice
		detector string
		hash     string
	}{
		{"cpu", nil, DetectorCPU, "8152b6fe7ebb723673fe4c1301f8e553"},
		{"coral", []hardware.ContainerDevice{devCoral}, DetectorCoral, "7b19396bc3ad7708cf3f55927181ad9b"},
	} {
		s := Settings{Enabled: true, Volume: "tank", Detector: DetectorSetting{Kind: tc.detector}, ProxySecret: testSecret}
		p, err := m.makePlan(s, []Camera{testCamera("cam", "Cam", nil)}, nil, probeHardware(tc.devices, nil, fullHost), "sha256:x", "/x", nil)
		if err != nil {
			t.Fatal(err)
		}
		if p.spec.Devices != nil || p.spec.Hash != tc.hash {
			t.Errorf("%s: devices %#v, hash %s, want 0.78's %s (the container would be made anew)", tc.name, p.spec.Devices, p.spec.Hash, tc.hash)
		}
	}
}

// cutArm ends the round's context at the ask after the first `per` ones
// of a round, as the deadline of a round does when cameras do not answer
// (each ask may take ten seconds): that ask is cut short.
type cutArm struct {
	*DevEngine
	per    int
	left   *int
	cancel *context.CancelFunc
}

func (e cutArm) NVRExec(ctx context.Context, cmd []string, limit int) (string, int, error) {
	if slices.Contains(cmd, "PUT") {
		if *e.left == 0 {
			(*e.cancel)()
			return "", -1, ctx.Err()
		}
		*e.left--
	}
	return e.DevEngine.NVRExec(ctx, cmd, limit)
}

// Finding 5: more missing streams than one round can ask: a stream is
// asked for when its turn comes, not marked before; the next round starts
// with those not asked yet, so every stream gets its turn.
func TestPreloadEveryMissingStreamGetsItsTurn(t *testing.T) {
	r := newRig(t, true)
	r.up()
	want := []string{}
	for i := range 10 {
		want = append(want, fmt.Sprintf("cam%02d", i))
	}
	r.engine.Away(want...)
	left, cancel := 0, context.CancelFunc(func() {})
	r.m.Engine = cutArm{r.engine, 3, &left, &cancel}
	round := func() []string {
		before := len(r.engine.Execs())
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		defer cancel()
		left = 3
		r.m.preloadRound(ctx, want)
		return puts(r.engine.Execs()[before:])
	}
	first := round()
	if !slices.Equal(first, want[:3]) {
		t.Fatalf("first round asked %v", first)
	}
	r.m.mu.Lock()
	marked := len(r.m.rt.preload.tried)
	r.m.mu.Unlock()
	if marked != len(first) {
		t.Errorf("%d streams marked as asked, %d asked: the others (and the one cut short) would wait five minutes for nothing", marked, len(first))
	}
	// Two minutes later the next round goes on where the first stopped
	// (the asked ones wait their five minutes); a stream asked before
	// comes again only after every other had its turn.
	order := slices.Clone(first)
	asked := map[string]bool{}
	for _, name := range first {
		asked[name] = true
	}
	for range 10 {
		if len(asked) == len(want) {
			break
		}
		r.advance(preloadEvery)
		for _, name := range round() {
			if asked[name] && len(asked) < len(want) {
				t.Errorf("%s asked again before every stream had its turn (order %v)", name, append(order, name))
			}
			asked[name] = true
			order = append(order, name)
		}
	}
	if len(asked) != len(want) {
		t.Errorf("not every stream was asked for: %v", order)
	}
}

// longAnswer answers the question for the open streams with more than
// the limit (cut, no JSON).
type longAnswer struct{ *DevEngine }

func (e longAnswer) NVRExec(ctx context.Context, cmd []string, limit int) (string, int, error) {
	if !slices.Contains(cmd, "PUT") {
		return `{"cam":` + strings.Repeat(" ", limit), 0, nil
	}
	return e.DevEngine.NVRExec(ctx, cmd, limit)
}

// The answer about the open streams may be long (64 cameras): it is read
// whole; one that reaches the limit anyway is said once, not every round,
// and asks for nothing.
func TestPreloadAnswerOf64Cameras(t *testing.T) {
	if preloadOutputLimit < 64*2*1500 {
		t.Errorf("limit %d bytes is too close to 128 streams of go2rtc's answer", preloadOutputLimit)
	}
	r := newRig(t, true)
	r.up()
	r.engine.Away("haustuer")
	r.m.Engine = longAnswer{r.engine}
	r.m.preloadRound(context.Background(), []string{"haustuer"})
	r.m.mu.Lock()
	tooLong := r.m.rt.preload.tooLong
	r.m.mu.Unlock()
	if !tooLong || len(puts(r.engine.Execs())) != 0 {
		t.Errorf("too long: said %v, asked %v", tooLong, puts(r.engine.Execs()))
	}
	r.m.Engine = r.engine
	r.m.preloadRound(context.Background(), []string{"haustuer"})
	r.m.mu.Lock()
	tooLong = r.m.rt.preload.tooLong
	r.m.mu.Unlock()
	if tooLong || len(puts(r.engine.Execs())) != 1 {
		t.Errorf("a readable answer again: said %v, asked %v", tooLong, puts(r.engine.Execs()))
	}
}

// dockerRestart is Docker's restart: a stop and a start, so that a start
// it refuses leaves the container ended (DevEngine.NVRRestart keeps it
// running).
type dockerRestart struct{ *DevEngine }

func (e dockerRestart) NVRRestart(ctx context.Context) error {
	e.DevEngine.Exit(0)
	return e.DevEngine.NVRStart(ctx)
}

// Re-check of the review: the NVIDIA card's CDI description goes stale
// while Frigate runs with it, and a change of the configuration restarts
// Frigate. The restart's start is refused: the CPU decodes at the next
// look, not only after the pause of an engine error, and no crash or
// failover is counted.
func TestCDIRefusedOnRestartFallsBackAtOnce(t *testing.T) {
	r, _, _ := volumeRig(t, "a")
	r.m.Engine = dockerRestart{r.engine}
	r.m.Devices = func(context.Context) []hardware.ContainerDevice { return []hardware.ContainerDevice{devNPU, devNV} }
	if err := r.m.SetSettings(context.Background(), Settings{Enabled: true, Volume: "a", Detector: DetectorSetting{Kind: DetectorAuto}, Decoder: devNV.ID}, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10 && r.phase() != PhaseRunning; i++ {
		r.look()
		r.advance(10 * time.Second)
	}
	if r.phase() != PhaseRunning || len(r.engine.Spec().CDIDevices) != 1 {
		t.Fatalf("not running with the NVIDIA card: %s %+v", r.phase(), r.engine.Spec().CDIDevices)
	}
	r.engine.RefuseCDI = true
	c, _ := r.m.Camera(r.m.Cameras()[0].ID)
	c.Name += " x"
	c.Source.Password = ""
	c.Detect.Objects = []string{"person"}
	if _, err := r.m.UpdateCamera(context.Background(), c.ID, c, CameraOptions{}); err != nil {
		t.Fatal(err)
	}
	looks := 0
	for ; looks < 6 && (r.phase() != PhaseRunning || len(r.engine.Spec().CDIDevices) != 0); looks++ {
		r.look()
		r.advance(5 * time.Second)
	}
	if r.phase() != PhaseRunning || len(r.engine.Spec().CDIDevices) != 0 {
		t.Fatalf("after %d looks: %s with %+v, want the CPU", looks, r.phase(), r.engine.Spec().CDIDevices)
	}
	if looks*5 >= int(errorBackoff/time.Second) {
		t.Errorf("the CPU took %d s, as long as an engine error's pause", looks*5)
	}
	r.m.mu.Lock()
	crashes, stopped, active := len(r.m.rt.crashes), r.m.state.Stopped, r.m.activeLocked()
	r.m.mu.Unlock()
	if crashes != 0 || stopped || active != DetectorNPU {
		t.Errorf("crashes %d, stopped %v, detector %s: the refusal is no crash and no failover", crashes, stopped, active)
	}
}
