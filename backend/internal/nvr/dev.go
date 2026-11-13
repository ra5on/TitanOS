package nvr

// Development mode (titand -dev) and tests: an engine without Docker and
// a stand-in for Frigate's API, so that the NVR's pages can be built and
// tested without the 1.5 GB image, a GPU or a camera. Nothing in this
// file is used on a NAS: wiring_nvr.go only takes it with -dev.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/jpeg"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"go.yaml.in/yaml/v3"

	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// devVolume is the only "volume" in development mode (a temporary folder).
const devVolume = "dev"

// demoCameras are the cameras of development mode while the development
// /config has none. They exist in RAM only until one is changed.
func demoCameras() []Camera {
	camera := func(order int, id, name, host string, edit func(*Camera)) Camera {
		c := NewCamera()
		c.ID, c.Name, c.Order = id, name, order
		c.Source = Source{Kind: "rtsp", Host: host, Port: 554, User: "viewer", Password: "demo",
			MainURL: "rtsp://" + host + ":554/main", SubURL: "rtsp://" + host + ":554/sub"}
		if edit != nil {
			edit(&c)
		}
		return c
	}
	return []Camera{
		camera(1, "haustuer", "Haustür", "192.168.1.61", func(c *Camera) {
			c.Detect.Objects, c.Detect.Faces = []string{"person", "dog"}, true
			c.Audio = Audio{Enabled: true, TwoWay: true}
		}),
		camera(2, "einfahrt", "Einfahrt", "192.168.1.62", func(c *Camera) {
			c.Detect.Objects, c.Detect.Plates = []string{"person", "car"}, true
			c.Record.Mode = RecordMotion
		}),
		camera(3, "garten", "Garten", "192.168.1.63", func(c *Camera) {
			c.Source.Kind, c.Source.ONVIFPort, c.Source.Port, c.PTZ = "onvif", 8000, 8000, true
			c.Detect.Objects = []string{"person", "cat", "dog", "bird"}
			c.Record = Record{Mode: RecordContinuous, RetainDays: 7, PreSeconds: 5, PostSeconds: 5}
		}),
		camera(4, "garage", "Garage", "192.168.1.64", func(c *Camera) {
			c.Source.SubURL = ""
			c.Enabled = false
		}),
	}
}

// devHardware is the hardware of development mode: a NAS with NPU and
// iGPU, no stick; the iGPU decodes, an NVIDIA card without its driver is
// offered but cannot.
func devHardware() HardwareInfo {
	h := probeHardware(nil, nil, hostFacts{accel: true, avx2: true, memory: 16 << 30}).with(
		Device{Kind: DetectorNPU, Title: "Intel NPU (Demo)", Present: true, Available: true},
		Device{Kind: DetectorIGPU, Title: "Intel iGPU (Demo)", Present: true, Available: true, nodes: []string{"/dev/dri/renderD128"}},
	)
	h.Decoders = []Decoder{
		{ID: "0000:00:02.0", Title: "Intel · 0000:00:02.0", Kind: "igpu", Available: true, nodes: []string{"/dev/dri/renderD128"}},
		{ID: "0000:01:00.0", Title: "NVIDIA · 0000:01:00.0", Kind: "nvidia", Reason: "driver"},
	}
	h.VideoDecode = true
	return h
}

// with puts devices in front of the CPU (development mode and tests).
func (h HardwareInfo) with(devices ...Device) HardwareInfo {
	h.Devices = append(append([]Device{}, devices...), h.Devices...)
	h.Auto = h.chain(DetectorAuto)[0]
	return h
}

// DevEngine is an Engine without Docker: it remembers one container.
type DevEngine struct {
	mu    sync.Mutex
	image bool
	state docker.NVRState
	spec  docker.NVRSpec
	// Log is what NVRLogs returns.
	Log string
	// PullError makes the pull fail; PullTime is how long it takes.
	PullError error
	PullTime  time.Duration
	// Starts counts NVRStart and NVRRestart, Creates NVRCreate.
	Starts, Creates int
	// Prunes counts NVRPruneImages.
	Prunes int
	// RefuseCDI makes the start of a container with CDI devices fail, as
	// Docker 26.1.5 does (measured) without the NVIDIA plugin's
	// descriptions: the create succeeds, the start answers 500.
	RefuseCDI bool
	// CreateError, if set, is asked before each create: what it returns
	// for the spec NVRCreate answers instead of creating (nil: it creates).
	CreateError func(docker.NVRSpec) error
	// frigate is told which config.yml the container started with.
	frigate *DevFrigate
	// engineStarted is when the engine started (zero: long ago);
	// engineStartedErr what NVREngineStarted says instead.
	engineStarted    time.Time
	engineStartedErr error
	// preloads are the streams go2rtc keeps open: those of config.yml's
	// preload from the start of the container on, and those asked for
	// again (NVRExec); away are streams of cameras that do not answer.
	preloads, away map[string]bool
	// execs are the commands run in the container.
	execs [][]string
}

// NewDevEngine returns an engine; with image the image counts as pulled.
func NewDevEngine(image bool) *DevEngine { return &DevEngine{image: image} }

// Serve has the engine tell f, at every start of the container, which
// cameras the config.yml in the container's config folder names: what
// Frigate reports in /api/config. Without the file Frigate writes its own,
// without cameras.
func (e *DevEngine) Serve(f *DevFrigate) {
	e.mu.Lock()
	e.frigate = f
	e.mu.Unlock()
}

func (e *DevEngine) EngineReady() bool { return true }

func (e *DevEngine) NVRInspect(context.Context) (docker.NVRState, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.state, nil
}

func (e *DevEngine) NVRCreate(_ context.Context, spec docker.NVRSpec) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state.Exists {
		return errors.New("dev engine: the container exists")
	}
	if e.CreateError != nil {
		if err := e.CreateError(spec); err != nil {
			return err
		}
	}
	e.spec, e.Creates = spec, e.Creates+1
	e.state = docker.NVRState{Exists: true, ID: "dev", Hash: spec.Hash, Image: spec.Image}
	return nil
}

func (e *DevEngine) NVRStart(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.state.Exists {
		return docker.ErrNotFound
	}
	if e.RefuseCDI && len(e.spec.CDIDevices) > 0 {
		return errors.New(`docker: could not select device driver "cdi" with capabilities: [] (HTTP 500)`)
	}
	e.state.Running, e.state.StartedAt, e.state.Address = true, time.Now(), "127.0.0.1"
	e.Starts++
	if e.frigate != nil {
		e.frigate.readConfig(filepath.Join(e.spec.Config, "config.yml"))
	}
	// go2rtc connects to the streams of its preload, except those whose
	// camera does not answer; it never tries those again by itself.
	e.preloads = map[string]bool{}
	var file struct {
		Go2RTC struct {
			Preload map[string]string `yaml:"preload"`
		} `yaml:"go2rtc"`
	}
	if data, err := os.ReadFile(filepath.Join(e.spec.Config, "config.yml")); err == nil && yaml.Unmarshal(data, &file) == nil {
		for name := range file.Go2RTC.Preload {
			if !e.away[name] {
				e.preloads[name] = true
			}
		}
	}
	return nil
}

// NVRExec answers the questions the watch asks go2rtc in the container:
// the open streams (GET /api/preload) and to open one again (PUT).
func (e *DevEngine) NVRExec(_ context.Context, cmd []string, limit int) (string, int, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.state.Running {
		return "", 0, docker.ErrNVRNotRunning
	}
	e.execs = append(e.execs, slices.Clone(cmd))
	target := cmd[len(cmd)-1]
	if !strings.HasPrefix(target, go2rtcAPI) {
		return "", 127, nil
	}
	if slices.Contains(cmd, "PUT") {
		if src, err := url.ParseQuery(strings.TrimPrefix(target, go2rtcAPI+"?")); err == nil && !e.away[src.Get("src")] {
			e.preloads[src.Get("src")] = true
		}
		return "", 0, nil
	}
	open := map[string]map[string]string{}
	for name := range e.preloads {
		open[name] = map[string]string{"query": ""}
	}
	data, _ := json.Marshal(open) // a map of strings always marshals
	if len(data) > limit {
		data = data[:limit]
	}
	return string(data), 0, nil
}

// Away stands for cameras that do not answer: go2rtc loses the streams
// and does not open them again until they answer (Back).
func (e *DevEngine) Away(streams ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.away == nil {
		e.away = map[string]bool{}
	}
	for _, name := range streams {
		e.away[name] = true
		delete(e.preloads, name)
	}
}

// Back lets the streams' cameras answer again.
func (e *DevEngine) Back(streams ...string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, name := range streams {
		delete(e.away, name)
	}
}

// Execs are the commands NVRExec ran.
func (e *DevEngine) Execs() [][]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return slices.Clone(e.execs)
}

// Preloads are the streams go2rtc keeps open now, sorted.
func (e *DevEngine) Preloads() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := []string{}
	for name := range e.preloads {
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func (e *DevEngine) NVRRestart(ctx context.Context) error { return e.NVRStart(ctx) }

func (e *DevEngine) NVRRemove(context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state = docker.NVRState{}
	return nil
}

func (e *DevEngine) NVRLogs(context.Context, int, time.Time) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.Log, nil
}

func (e *DevEngine) NVRNetwork(context.Context) error { return nil }

func (e *DevEngine) NVRImageID(context.Context, string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.image {
		return "sha256:" + strings.Repeat("d", 64), nil
	}
	return "", nil
}

func (e *DevEngine) NVRPull(ctx context.Context, _, _ string, progress func(done, total int64)) error {
	e.mu.Lock()
	wait, fail := e.PullTime, e.PullError
	e.mu.Unlock()
	const total = 1500 << 20
	for step := range 10 {
		if progress != nil {
			progress(int64(step)*total/10, total)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait / 10):
		}
	}
	if fail != nil {
		return fail
	}
	e.mu.Lock()
	e.image = true
	e.mu.Unlock()
	return nil
}

func (e *DevEngine) NVRPruneImages(context.Context, string, string) ([]string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Prunes++
	return nil, nil
}

// Exit ends the container as a crash of Frigate would.
func (e *DevEngine) Exit(code int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.state.Running, e.state.ExitCode, e.state.FinishedAt, e.state.Address = false, code, time.Now(), ""
}

// NVREngineStarted tells when the engine started.
func (e *DevEngine) NVREngineStarted(context.Context) (time.Time, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.engineStarted, e.engineStartedErr
}

// RestartEngine stands for a restart of Docker's engine: it stops the
// container, as dockerd does when it ends, and starts anew.
func (e *DevEngine) RestartEngine() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.state.Running {
		e.state.Running, e.state.ExitCode, e.state.FinishedAt, e.state.Address = false, 0, time.Now(), ""
	}
	e.engineStarted = time.Now()
}

// Spec is what the container was last created from.
func (e *DevEngine) Spec() docker.NVRSpec {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.spec
}

// SetLog sets what NVRLogs returns.
func (e *DevEngine) SetLog(log string) {
	e.mu.Lock()
	e.Log = log
	e.mu.Unlock()
}

// DevFrigate stands in for Frigate's API on port 5000: the routes the
// NVR uses, with answers shaped like Frigate 0.18.
type DevFrigate struct {
	mu sync.Mutex
	// Down makes every route fail; SafeMode answers as after a refused
	// config; NoDetector reports a detector without a process.
	Down, SafeMode, NoDetector bool
	// FailKept answers the question for kept events with an error
	// (tests of the storage cap); IgnoreConfig starts without the
	// cameras of config.yml.
	FailKept, IgnoreConfig bool
	// Cameras are the cameras Frigate "runs" with their pictures per
	// second (0: offline).
	Cameras map[string]float64
	// config are the cameras of the config.yml Frigate started with (ID →
	// enabled); nil until a DevEngine told it. secret is its
	// proxy.auth_secret: requests without it are refused, as on port 8971.
	config map[string]bool
	secret string
	// Log are the lines of /api/logs/frigate.
	Log []string
	// data are events, recordings, exports and faces (dev_data.go).
	data devData
}

// NewDevFrigate returns a stand-in that runs the given cameras.
func NewDevFrigate(cameras []Camera) *DevFrigate {
	f := &DevFrigate{Cameras: map[string]float64{}, Log: []string{
		"[2026-11-12 08:00:01] frigate.app                    INFO    : Starting Frigate (" + FrigateVersion + ")",
		"[2026-11-12 08:00:04] frigate.detectors.plugins.openvino INFO    : OpenVINO detector started on NPU",
		"[2026-11-12 08:00:05] frigate.app                    INFO    : Camera processor started for haustuer: 512",
	}}
	f.SetCameras(cameras)
	return f
}

// SetCameras tells the stand-in which cameras run (enabled ones send 15
// pictures per second, except "garten": it shows an offline camera).
func (f *DevFrigate) SetCameras(cameras []Camera) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Cameras = map[string]float64{}
	for _, c := range cameras {
		if !c.Enabled {
			continue
		}
		f.Cameras[c.ID] = 15
	}
}

// readConfig takes the cameras from a config.yml as Frigate does when it
// starts; a missing file is Frigate's own config without cameras.
func (f *DevFrigate) readConfig(path string) {
	var file struct {
		Proxy struct {
			Secret string `yaml:"auth_secret"`
		} `yaml:"proxy"`
		Cameras map[string]struct {
			Enabled *bool `yaml:"enabled"`
		} `yaml:"cameras"`
	}
	data, err := os.ReadFile(path)
	if err == nil {
		err = yaml.Unmarshal(data, &file)
	}
	config := map[string]bool{}
	f.mu.Lock()
	ignore := f.IgnoreConfig
	f.mu.Unlock()
	if err == nil && !ignore {
		for id, c := range file.Cameras {
			config[id] = c.Enabled == nil || *c.Enabled
		}
	}
	f.mu.Lock()
	f.config, f.secret = config, file.Proxy.Secret
	f.mu.Unlock()
}

// Set changes the stand-in under its lock.
func (f *DevFrigate) Set(edit func(*DevFrigate)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	edit(f)
}

// ServeHTTP answers like Frigate.
func (f *DevFrigate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	down, safe, noDetector, secret := f.Down, f.SafeMode, f.NoDetector, f.secret
	cameras := map[string]float64{}
	for id, fps := range f.Cameras {
		cameras[id] = fps
	}
	log := append([]string{}, f.Log...)
	f.mu.Unlock()
	if down {
		http.Error(w, "down", http.StatusBadGateway)
		return
	}
	if secret != "" && r.Header.Get(proxySecretHeader) != secret {
		http.Error(w, "", http.StatusUnauthorized)
		return
	}
	writeJSON := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v) // the client went away; nothing to do
	}
	switch path := r.URL.Path; {
	case path == "/api/version":
		fmt.Fprint(w, FrigateVersion+"-dev")
	case path == "/api/config":
		f.mu.Lock()
		cameras := map[string]any{}
		for id, enabled := range f.config {
			cameras[id] = map[string]any{"enabled": enabled}
		}
		f.mu.Unlock()
		if safe {
			cameras = map[string]any{}
		}
		writeJSON(map[string]any{"safe_mode": safe, "cameras": cameras})
	case path == "/api/stats":
		pid := any(4711)
		if noDetector {
			pid = nil
		}
		stats := map[string]any{
			"detectors":  map[string]any{"ov": map[string]any{"inference_speed": 11.4, "detection_start": 0.0, "pid": pid}},
			"cameras":    map[string]any{},
			"gpu_usages": map[string]any{"intel-gpu": map[string]any{"gpu": "7.0%", "mem": "-%"}},
			"npu_usages": map[string]any{"intel-npu": map[string]any{"npu": "21.0%", "mem": "-%"}},
			"service": map[string]any{"uptime": 3600, "version": FrigateVersion, "storage": map[string]any{
				"/dev/shm": map[string]any{"total": 384.0, "used": 142.5, "free": 241.5, "mount_type": "tmpfs"},
			}},
		}
		for id, fps := range cameras {
			detection := 0.0
			if fps > 0 {
				detection = 1.2
			}
			stats["cameras"].(map[string]any)[id] = map[string]any{"camera_fps": fps, "process_fps": min(fps, 5), "skipped_fps": 0.0, "detection_fps": detection}
		}
		writeJSON(stats)
	case path == "/api/logs/frigate":
		writeJSON(map[string]any{"totalLines": len(log), "lines": log})
	case strings.HasPrefix(path, "/api/") && strings.HasSuffix(path, "/latest.jpg"):
		id := strings.TrimSuffix(strings.TrimPrefix(path, "/api/"), "/latest.jpg")
		if _, ok := cameras[id]; !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(demoPicture(id)) // the client went away; nothing to do
	case path == "/live/mse/api/ws":
		// No video in development mode: the connection opens and stays
		// until the window closes it, so the player shows "connecting".
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			if _, _, err := conn.Read(r.Context()); err != nil {
				return
			}
		}
	default:
		if !f.serveData(w, r) {
			http.NotFound(w, r)
		}
	}
}

// demoPicture is a still for a demo camera: a gradient in a colour made
// from the camera's ID, so the tiles can be told apart.
func demoPicture(id string) []byte {
	hash := fnv.New32a()
	hash.Write([]byte(id))
	seed := hash.Sum32()
	base := color.RGBA{R: uint8(60 + seed%120), G: uint8(60 + (seed>>8)%120), B: uint8(60 + (seed>>16)%120), A: 255}
	const width, height = 640, 360
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		shade := uint8(y * 60 / height)
		for x := range width {
			img.SetRGBA(x, y, color.RGBA{R: base.R - shade/2, G: base.G - shade/2, B: base.B - shade/2, A: 255})
		}
	}
	var out bytes.Buffer
	_ = jpeg.Encode(&out, img, &jpeg.Options{Quality: 70}) // a bytes.Buffer does not fail
	return out.Bytes()
}

// DevStandIn is the stand-in for Frigate of development mode (nil
// otherwise); tests let it raise events.
func (m *Manager) DevStandIn() *DevFrigate { return m.devFrigate }

// StartDev makes m a manager of development mode: a DevEngine with the
// image "pulled" and a DevFrigate on a port of 127.0.0.1 that ends with
// ctx. Call it before Load.
func (m *Manager) StartDev(ctx context.Context) error {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	stand := NewDevFrigate(nil)
	m.devFrigate = stand
	server := &http.Server{Handler: stand, ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = server.Serve(listener) }() // ends with Close below
	context.AfterFunc(ctx, func() { _ = server.Close() })
	base := "http://" + listener.Addr().String()
	m.Dev = true
	engine := NewDevEngine(true)
	engine.Serve(stand)
	m.Engine = engine
	m.FrigateBase = func(string) string {
		// The stand-in runs what is configured now.
		stand.SetCameras(m.Cameras())
		stand.seedOnce(m.Cameras())
		stand.Set(func(f *DevFrigate) {
			if _, ok := f.Cameras["garten"]; ok {
				f.Cameras["garten"] = 0 // one demo camera is offline
			}
		})
		return base
	}
	return nil
}

// seedOnce fills the stand-in with example data the first time it is
// asked for its address.
func (f *DevFrigate) seedOnce(cameras []Camera) {
	f.mu.Lock()
	seeded := f.data.seeded
	f.mu.Unlock()
	if !seeded {
		f.Seed(cameras, time.Now())
	}
}
