package nvr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// The phases of the NVR (Status.State).
const (
	PhaseOff            = "off"             // switched off
	PhaseDockerOff      = "docker_off"      // Docker is switched off
	PhaseDockerStarting = "docker_starting" // Docker is on, its engine not up yet
	PhaseNoVolume       = "no_volume"       // the NVR's volume is not mounted
	PhasePulling        = "pulling"         // the image is being fetched
	PhaseStarting       = "starting"        // the container starts, Frigate does not answer yet
	PhaseRunning        = "running"         // Frigate answers
	PhaseStopped        = "stopped"         // nothing worked; waits for the owner
	PhaseError          = "error"           // the engine refused something; tried again later
)

// ImageStatus is where Frigate's image stands.
type ImageStatus struct {
	// State is "missing" (not looked for yet), "downloading", "ready" or
	// "error".
	State   string `json:"state"`
	Version string `json:"version"`
	// Bytes of Size are downloaded (Size grows while the pull starts);
	// Progress is Bytes/Size (0–1).
	Bytes    int64   `json:"bytes"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
	Error    string  `json:"error,omitempty"`
}

// LastError is the last thing that went wrong, for status and diagnosis.
type LastError struct {
	// Code is a reason of classify.go or "engine" (Docker refused).
	Code   string    `json:"code"`
	Detail string    `json:"detail"`
	At     time.Time `json:"at"`
}

// cameraRuntime is what the watch knows of one camera.
type cameraRuntime struct {
	fps, processFPS, detectionFPS, skippedFPS float64
	// offlineSince is when the camera stopped sending pictures (zero
	// while it sends); reported is set once the message went out.
	offlineSince time.Time
	reported     bool
	seen         bool
}

// runtime is the state of the watch, guarded by Manager.mu. Only the loop
// (Run) changes the container; requests set dirty and wake it.
type runtime struct {
	phase string
	// dirty: settings, cameras or the device changed; config and
	// container are compared with what they should be.
	dirty bool
	// stateLoaded: state.json of the volume was read; stateDirty: it is
	// to be written.
	stateLoaded, stateDirty bool
	// configHash is the hash of config.yml as last written or read.
	configHash string
	// retryAt holds the loop back after the engine refused something.
	retryAt   time.Time
	lastError *LastError

	image       ImageStatus
	imageID     string
	pulling     bool
	pullChecked time.Time

	hardware   HardwareInfo
	hardwareAt time.Time
	candidates []string

	// absent: the watch removed the container and did not create one
	// since (false at the start: one may be left from before).
	absent bool
	// The container as last seen.
	container docker.NVRState
	address   string
	// startedAt is when the watch last started the container; healthy
	// once Frigate answered after that.
	startedAt time.Time
	healthy   bool
	// answeredAt is when Frigate's API last answered.
	answeredAt time.Time
	version    string
	// crashes are the times the container was found ended.
	crashes   []time.Time
	lastCrash time.Time
	// detectorBad counts looks in a row without a living detector.
	detectorBad int

	inferenceMs *float64
	detectorPID *int
	stats       *frigateStats
	statsAt     time.Time
	usedBytes   *uint64
	usedAt      time.Time
	cameras     map[string]*cameraRuntime
}

// errorBackoff is the pause after the engine refused something.
const errorBackoff = time.Minute

// Run keeps the container in line with the settings until ctx is done.
// It looks every Tick (Docker's socket and Frigate's API over the
// container network: no disk, never /config) and at once when a request
// changed something.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(m.Tick)
	defer ticker.Stop()
	for {
		m.reconcile(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.kick:
		}
	}
}

func (m *Manager) setPhase(phase string) {
	m.mu.Lock()
	if m.rt.phase != phase {
		slog.Info("NVR", "state", phase)
	}
	m.rt.phase = phase
	m.mu.Unlock()
}

// engineError notes that the engine refused something and holds the loop
// back for errorBackoff.
func (m *Manager) engineError(what string, err error) {
	slog.Warn("NVR: "+what, "err", err)
	m.mu.Lock()
	m.rt.phase = PhaseError
	m.rt.lastError = &LastError{Code: "engine", Detail: clip(Redact(what+": "+err.Error()), 500), At: m.now()}
	m.rt.retryAt = m.now().Add(errorBackoff)
	m.mu.Unlock()
}

// reconcile is one look: it brings image, config and container to what
// the settings ask for and checks on Frigate.
func (m *Manager) reconcile(ctx context.Context) {
	m.mu.Lock()
	s, loaded, retryAt := m.settings, m.loaded, m.rt.retryAt
	m.mu.Unlock()
	if !loaded || m.Engine == nil {
		return
	}
	ready := m.Dev || m.Engine.EngineReady()
	if !s.Enabled {
		if ready {
			m.removeContainer(ctx)
		}
		m.setPhase(PhaseOff)
		return
	}
	if !ready {
		if m.DockerEnabled != nil && m.DockerEnabled() {
			m.setPhase(PhaseDockerStarting)
		} else {
			m.setPhase(PhaseDockerOff)
		}
		return
	}
	// A volume that is being unmounted or locked no longer counts as
	// mounted: the container lets go of it first (as Docker does for its
	// own volume).
	if !slices.Contains(m.mounted(), s.Volume) {
		m.removeContainer(ctx)
		m.mu.Lock()
		m.rt.stateLoaded = false
		m.mu.Unlock()
		m.setPhase(PhaseNoVolume)
		return
	}
	if m.now().Before(retryAt) {
		return
	}
	if err := m.syncState(s.Volume); err != nil {
		m.engineError("state of the NVR on the volume", err)
		return
	}
	m.mu.Lock()
	stopped := m.state.Stopped
	m.mu.Unlock()
	if stopped {
		m.removeContainer(ctx)
		m.setPhase(PhaseStopped)
		return
	}
	imageID, ok := m.readyImage(ctx)
	if !ok {
		return
	}
	if err := m.converge(ctx, s, imageID); err != nil {
		m.engineError("container of the NVR", err)
	}
}

// removeContainer stops and removes the container if the watch knows or
// has to assume one.
func (m *Manager) removeContainer(ctx context.Context) {
	m.mu.Lock()
	absent := m.rt.absent
	m.mu.Unlock()
	if absent {
		return // removed before, and not created since
	}
	rctx, cancel := context.WithTimeout(ctx, (docker.NVRStopSeconds+30)*time.Second)
	defer cancel()
	if err := m.Engine.NVRRemove(rctx); err != nil {
		slog.Warn("NVR: container not removed", "err", err)
		return
	}
	m.mu.Lock()
	m.rt.absent = true
	m.rt.container, m.rt.address, m.rt.healthy, m.rt.startedAt = docker.NVRState{}, "", false, time.Time{}
	m.rt.stats, m.rt.inferenceMs, m.rt.detectorPID = nil, nil, nil
	m.mu.Unlock()
}

// privateRoot opens .titanos/nvr on the NVR's volume (owner root, not
// writable by others, no links) and returns it with its path. In
// development mode it is a temporary folder.
func (m *Manager) privateRoot(volume string) (*os.Root, string, error) {
	if m.Dev {
		m.mu.Lock()
		dir := m.devDir
		m.mu.Unlock()
		if dir == "" {
			var err error
			if dir, err = os.MkdirTemp("", "titanos-nvr-dev-"); err != nil {
				return nil, "", err
			}
			m.mu.Lock()
			m.devDir = dir
			m.mu.Unlock()
		}
		root, err := os.OpenRoot(dir)
		return root, dir, err
	}
	root, err := volumedata.Open(m.MountRoot, volume, "nvr", m.mounted())
	if err != nil {
		if errors.Is(err, volumedata.ErrUnavailable) {
			return nil, "", ErrNoVolume
		}
		return nil, "", err
	}
	return root, filepath.Join(m.MountRoot, volume, ".titanos", "nvr"), nil
}

const stateFile = "state.json"

// syncState reads state.json once per volume and writes it when it
// changed. After a restart of the NAS a failover is forgotten: the chosen
// device is tried again (PLAN 79-7).
func (m *Manager) syncState(volume string) error {
	m.mu.Lock()
	loaded, dirty := m.rt.stateLoaded, m.rt.stateDirty
	m.mu.Unlock()
	if loaded && !dirty {
		return nil
	}
	root, _, err := m.privateRoot(volume)
	if err != nil {
		return err
	}
	defer root.Close()
	boot := m.bootID()
	if !loaded {
		var stored State
		err := volumedata.ReadJSON(root, stateFile, &stored, 64<<10)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			slog.Warn("NVR: state on the volume not readable, starting afresh", "err", err)
		}
		m.mu.Lock()
		// A request that reset the state meanwhile wins.
		if !m.rt.stateDirty {
			if stored.Boot != boot {
				if stored.Active != "" || stored.Stopped {
					slog.Info("NVR: the NAS was restarted, the chosen detection device is tried again", "was", stored.Active)
					m.rt.stateDirty = true
				}
				stored = State{}
			}
			m.state = stored
		}
		m.state.Boot = boot
		m.rt.stateLoaded = true
		dirty = m.rt.stateDirty
		m.mu.Unlock()
	}
	if !dirty {
		return nil
	}
	m.mu.Lock()
	state := m.state
	state.Boot = boot
	m.mu.Unlock()
	if err := volumedata.WriteJSON(root, stateFile, state); err != nil {
		return err
	}
	m.mu.Lock()
	m.rt.stateDirty = false
	m.mu.Unlock()
	return nil
}

// readyImage returns the ID of Frigate's image; while it is not there it
// starts the pull (once at a time, after a failure at most once a minute).
func (m *Manager) readyImage(ctx context.Context) (string, bool) {
	m.mu.Lock()
	id, pulling, checked, state := m.rt.imageID, m.rt.pulling, m.rt.pullChecked, m.rt.image.State
	m.mu.Unlock()
	if id != "" {
		return id, true
	}
	if pulling {
		m.setPhase(PhasePulling)
		return "", false
	}
	ictx, cancel := context.WithTimeout(ctx, 30*time.Second)
	found, err := m.Engine.NVRImageID(ictx, frigateImage)
	cancel()
	if err != nil {
		m.engineError("image of the NVR", err)
		return "", false
	}
	if found != "" {
		m.mu.Lock()
		m.rt.imageID, m.rt.image = found, ImageStatus{State: "ready"}
		m.mu.Unlock()
		return found, true
	}
	if state == "error" && m.now().Sub(checked) < errorBackoff {
		m.setPhase(PhaseError)
		return "", false
	}
	m.mu.Lock()
	m.rt.pulling = true
	m.rt.image = ImageStatus{State: "downloading"}
	m.mu.Unlock()
	m.setPhase(PhasePulling)
	go m.pull(ctx)
	return "", false
}

// pull fetches the image (about 1.5 GB) and wakes the loop when done.
func (m *Manager) pull(ctx context.Context) {
	pctx, cancel := context.WithTimeout(ctx, 2*time.Hour)
	defer cancel()
	slog.Info("NVR: fetching Frigate", "version", FrigateVersion)
	err := m.Engine.NVRPull(pctx, FrigateRepository, FrigateDigest, func(done, total int64) {
		m.mu.Lock()
		m.rt.image.Bytes, m.rt.image.Size = done, total
		m.mu.Unlock()
	})
	id := ""
	if err == nil {
		if id, err = m.Engine.NVRImageID(pctx, frigateImage); err == nil && id == "" {
			err = errors.New("nvr: the image is not there after the pull")
		}
	}
	m.mu.Lock()
	m.rt.pulling, m.rt.pullChecked = false, m.now()
	if err != nil {
		m.rt.image = ImageStatus{State: "error", Error: clip(err.Error(), 300)}
		m.rt.lastError = &LastError{Code: "image", Detail: clip(err.Error(), 500), At: m.now()}
	} else {
		m.rt.imageID, m.rt.image = id, ImageStatus{State: "ready"}
	}
	m.mu.Unlock()
	if err != nil {
		slog.Warn("NVR: Frigate's image not fetched", "err", err)
	} else {
		slog.Info("NVR: Frigate's image is ready")
	}
	m.wake()
}

// shmBytes is the size of /dev/shm for a number of cameras. Frigate keeps
// decoded frames of the detect stream there: about 1.6 MB per frame at
// 1280×720 and at best 50 frames per camera, plus about 50 MB for its
// logs and the nginx cache; it reserves room for two cameras added while
// it runs. 128 MB + 64 MB per camera gives every camera at least 30 frames
// at 720p (Frigate warns below 20) and stays small on a NAS; never less
// than 256 MB. A sub stream above 720p needs more per frame: the
// diagnosis shows how full /dev/shm is.
func shmBytes(cameras int) int64 {
	return max(256<<20, int64(128+64*cameras)<<20)
}

// cacheBytes is the tmpfs /tmp/cache: the segments of the recordings
// before they are moved to the volume (Frigate's documentation: 1 GB).
const cacheBytes = 1 << 30

// plan is what config and container should be right now.
type plan struct {
	config []byte
	spec   docker.NVRSpec
	active string
}

// makePlan resolves the device, renders the config and describes the
// container.
func (m *Manager) makePlan(s Settings, cameras []Camera, hw HardwareInfo, imageID, dir string, candidates []string) (plan, error) {
	m.mu.Lock()
	active := m.state.Active
	m.mu.Unlock()
	chain := hw.chain(s.Detector.Kind)
	if active == "" || (!slices.Contains(chain, active) && active != DetectorCPU) {
		active = chain[0]
	}
	nodes, usb := hw.containerDevices(active)
	config, err := Render(s, cameras, RenderHardware{Detector: active, VAAPI: hw.VideoDecode, Candidates: candidates})
	if err != nil {
		return plan{}, err
	}
	enabled := 0
	for _, c := range cameras {
		if c.Enabled {
			enabled++
		}
	}
	spec := docker.NVRSpec{
		Image: imageID, Config: filepath.Join(dir, "config"), Media: filepath.Join(dir, "media"),
		ShmBytes: shmBytes(enabled), CacheBytes: cacheBytes, Devices: nodes, USBBus: usb,
		// WebRTC (talking back) needs a port browsers reach directly; the
		// restream only when the owner switched it on. Frigate's API
		// (5000), its login port (8971) and go2rtc (1984) are never
		// published.
		Ports: []docker.NVRPort{{Port: 8555, Proto: "tcp"}, {Port: 8555, Proto: "udp"}},
	}
	if s.RTSPOut {
		spec.Ports = append(spec.Ports, docker.NVRPort{Port: 8554, Proto: "tcp"})
	}
	described, err := json.Marshal(spec)
	if err != nil {
		return plan{}, err
	}
	sum := sha256.Sum256(described)
	spec.Hash = hex.EncodeToString(sum[:16])
	return plan{config: config, spec: spec, active: active}, nil
}

func hashOf(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// writeConfig makes the folders of the container and puts config.yml in
// place (0600, atomically) if its content differs. It reports whether the
// file changed.
func (m *Manager) writeConfig(volume string, config []byte) (changed bool, err error) {
	want := hashOf(config)
	m.mu.Lock()
	have := m.rt.configHash
	m.mu.Unlock()
	if have == want {
		return false, nil
	}
	root, _, err := m.privateRoot(volume)
	if err != nil {
		return false, err
	}
	defer root.Close()
	for _, dir := range []string{"config", "media"} {
		if err := root.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return false, err
		}
		if info, err := root.Lstat(dir); err != nil || !info.IsDir() {
			return false, fmt.Errorf("nvr: %s is not a folder", dir)
		}
	}
	folder, err := root.OpenRoot("config")
	if err != nil {
		return false, err
	}
	defer folder.Close()
	// After a start of titand the hash is not known: the file may be
	// what it should be already (Frigate then needs no restart).
	if have == "" {
		if file, err := folder.Open("config.yml"); err == nil {
			var current bytes.Buffer
			_, rerr := current.ReadFrom(file)
			file.Close()
			if rerr == nil && bytes.Equal(current.Bytes(), config) {
				m.mu.Lock()
				m.rt.configHash = want
				m.mu.Unlock()
				return false, nil
			}
		}
	}
	if err := volumedata.Write(folder, "config.yml", config); err != nil {
		return false, err
	}
	m.mu.Lock()
	m.rt.configHash = want
	m.mu.Unlock()
	return true, nil
}

// converge creates, replaces, restarts or starts the container as
// needed and then checks on Frigate.
func (m *Manager) converge(ctx context.Context, s Settings, imageID string) error {
	ictx, cancel := context.WithTimeout(ctx, 30*time.Second)
	st, err := m.Engine.NVRInspect(ictx)
	cancel()
	if err != nil {
		return err
	}
	m.mu.Lock()
	dirty := m.rt.dirty || m.rt.configHash == ""
	m.rt.container = st
	m.rt.absent = m.rt.absent && !st.Exists
	m.mu.Unlock()

	if dirty || !st.Exists {
		hw := m.hardwareCached(ctx, true)
		dir, err := m.privateDir(s.Volume)
		if err != nil {
			return err
		}
		p, err := m.makePlan(s, m.Cameras(), hw, imageID, dir, m.candidates())
		if err != nil {
			return err
		}
		changed, err := m.writeConfig(s.Volume, p.config)
		if err != nil {
			return err
		}
		m.mu.Lock()
		m.rt.dirty = false
		if m.state.Active != p.active && m.state.Failover != nil {
			// The device of the failover is gone too; the plan chose anew.
			m.state.Active, m.rt.stateDirty = p.active, true
		}
		m.mu.Unlock()
		switch {
		case !st.Exists || st.Hash != p.spec.Hash || st.Image != imageID:
			return m.recreate(ctx, p.spec)
		case changed && st.Running:
			slog.Info("NVR: configuration changed, restarting Frigate")
			rctx, cancel := context.WithTimeout(ctx, (docker.NVRStopSeconds+60)*time.Second)
			defer cancel()
			if err := m.Engine.NVRRestart(rctx); err != nil {
				return err
			}
			m.started()
			return nil
		}
	}
	if !st.Running {
		return m.ended(ctx, st)
	}
	m.mu.Lock()
	if m.rt.startedAt.IsZero() {
		// Found running after a start of titand (an app update).
		m.rt.startedAt = st.StartedAt
	}
	m.rt.address = st.Address
	m.mu.Unlock()
	m.check(ctx, st)
	return nil
}

// privateDir is privateRoot without keeping the root open.
func (m *Manager) privateDir(volume string) (string, error) {
	root, dir, err := m.privateRoot(volume)
	if err != nil {
		return "", err
	}
	root.Close()
	return dir, nil
}

// candidates are the WebRTC addresses, read when the config is made (not
// on every look: a changed address alone never restarts Frigate).
func (m *Manager) candidates() []string {
	out := []string{}
	for _, address := range m.addresses() {
		out = append(out, address+":8555")
	}
	return out
}

// recreate replaces the container by one made from spec and starts it.
func (m *Manager) recreate(ctx context.Context, spec docker.NVRSpec) error {
	rctx, cancel := context.WithTimeout(ctx, (docker.NVRStopSeconds+90)*time.Second)
	defer cancel()
	if err := m.Engine.NVRRemove(rctx); err != nil {
		return err
	}
	if err := m.Engine.NVRNetwork(rctx); err != nil {
		return err
	}
	if err := m.Engine.NVRCreate(rctx, spec); err != nil {
		return err
	}
	if err := m.Engine.NVRStart(rctx); err != nil {
		return err
	}
	slog.Info("NVR: container started", "devices", strings.Join(spec.Devices, " "), "shmMB", spec.ShmBytes>>20)
	m.started()
	return nil
}

// started notes a start by the watch.
func (m *Manager) started() {
	m.mu.Lock()
	m.rt.startedAt, m.rt.healthy, m.rt.detectorBad, m.rt.absent = m.now(), false, 0, false
	m.rt.answeredAt = time.Time{}
	m.rt.phase = PhaseStarting
	m.rt.stats, m.rt.inferenceMs, m.rt.detectorPID = nil, nil, nil
	m.mu.Unlock()
}

// ended handles a container that is not running: Frigate ended (it does
// so when its detector dies). It is started again; CrashLimit ends within
// CrashWindow are a loop and move the detection to the next device.
func (m *Manager) ended(ctx context.Context, st docker.NVRState) error {
	now := m.now()
	m.mu.Lock()
	if m.rt.startedAt.IsZero() {
		// Not started by this titand: left stopped by a restart of the
		// NAS or of Docker. No fault of Frigate.
		m.mu.Unlock()
		sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		if err := m.Engine.NVRStart(sctx); err != nil {
			return err
		}
		m.started()
		return nil
	}
	// The same end is counted once.
	if !st.FinishedAt.Equal(m.rt.lastCrash) || st.FinishedAt.IsZero() {
		m.rt.lastCrash = st.FinishedAt
		m.rt.crashes = append(m.rt.crashes, now)
	}
	m.rt.crashes = slices.DeleteFunc(m.rt.crashes, func(t time.Time) bool { return now.Sub(t) > m.CrashWindow })
	crashes, first := len(m.rt.crashes), now
	if crashes > 0 {
		first = m.rt.crashes[0]
	}
	m.rt.healthy = false
	m.mu.Unlock()
	slog.Warn("NVR: Frigate ended", "exitCode", st.ExitCode, "endsInWindow", crashes)
	if crashes >= m.CrashLimit {
		return m.fail(ctx, "", first)
	}
	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if err := m.Engine.NVRStart(sctx); err != nil {
		return err
	}
	m.started()
	return nil
}

// check asks Frigate how it is: up, with the config TitanOS wrote, and
// with a living detector. It also reads what status and diagnosis show.
func (m *Manager) check(ctx context.Context, st docker.NVRState) {
	base := ""
	if st.Address != "" {
		base = m.FrigateBase(st.Address)
	}
	hctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	version, err := m.frigate.version(hctx, base)
	now := m.now()
	m.mu.Lock()
	startedAt, wasHealthy, answeredAt := m.rt.startedAt, m.rt.healthy, m.rt.answeredAt
	if err == nil {
		m.rt.answeredAt = now
	}
	m.mu.Unlock()
	if err != nil {
		if wasHealthy && now.Sub(answeredAt) > m.HealthyWithin {
			// Ran, and now hangs without ending: started afresh, counted
			// like an end.
			slog.Warn("NVR: Frigate does not answer any more, restarting it")
			m.mu.Lock()
			m.rt.crashes = append(m.rt.crashes, now)
			m.mu.Unlock()
			rctx, cancel := context.WithTimeout(ctx, (docker.NVRStopSeconds+60)*time.Second)
			defer cancel()
			if rerr := m.Engine.NVRRestart(rctx); rerr != nil {
				m.engineError("container of the NVR", rerr)
				return
			}
			m.started()
			return
		}
		if !wasHealthy && now.Sub(startedAt) > m.HealthyWithin {
			slog.Warn("NVR: Frigate did not answer after its start", "after", now.Sub(startedAt).Round(time.Second))
			if ferr := m.fail(ctx, ReasonNotHealthy, startedAt); ferr != nil {
				m.engineError("container of the NVR", ferr)
			}
			return
		}
		if !wasHealthy {
			m.setPhase(PhaseStarting)
		}
		return
	}
	// A refused config.yml does not end Frigate: it starts empty ("safe
	// mode") and answers. That is not the device's fault.
	if safe, err := m.frigate.safeMode(hctx, base); err == nil && safe {
		if ferr := m.fail(ctx, ReasonConfigInvalid, startedAt); ferr != nil {
			m.engineError("container of the NVR", ferr)
		}
		return
	}
	stats, err := m.frigate.stats(hctx, base)
	if err != nil {
		// Frigate's API answers before its statistics are there.
		m.mu.Lock()
		m.rt.version = version
		m.mu.Unlock()
		m.setPhase(PhaseStarting)
		return
	}
	alive := len(stats.Detectors) > 0
	for _, d := range stats.Detectors {
		alive = alive && d.PID != nil
	}
	m.mu.Lock()
	m.rt.version, m.rt.stats, m.rt.statsAt = version, &stats, now
	m.rt.inferenceMs, m.rt.detectorPID = nil, nil
	for _, d := range stats.Detectors {
		speed := d.InferenceSpeed
		m.rt.inferenceMs, m.rt.detectorPID = &speed, d.PID
		break
	}
	if alive {
		m.rt.detectorBad = 0
		if !m.rt.healthy {
			m.rt.healthy = true
			slog.Info("NVR: Frigate runs", "version", version, "detector", m.activeLocked())
		}
		m.rt.phase = PhaseRunning
	} else {
		m.rt.detectorBad++
	}
	bad := m.rt.detectorBad
	m.mu.Unlock()
	// Three looks in a row without a detector process: it is gone and
	// Frigate did not notice (it normally ends itself).
	if bad >= 3 {
		if ferr := m.fail(ctx, "", startedAt); ferr != nil {
			m.engineError("container of the NVR", ferr)
		}
		return
	}
	m.watchCameras(stats, now)
	m.readUsage(hctx, base, now)
}

// activeLocked is the device in use (m.mu held).
func (m *Manager) activeLocked() string {
	if m.state.Active != "" {
		return m.state.Active
	}
	if chain := m.rt.hardware.chain(m.settings.Detector.Kind); len(chain) > 0 {
		return chain[0]
	}
	return DetectorCPU
}

// usageEvery is how often the size of the recordings is asked.
const usageEvery = 5 * time.Minute

func (m *Manager) readUsage(ctx context.Context, base string, now time.Time) {
	m.mu.Lock()
	due := now.Sub(m.rt.usedAt) >= usageEvery
	m.mu.Unlock()
	if !due {
		return
	}
	used, err := m.frigate.recordingsUsage(ctx, base)
	m.mu.Lock()
	m.rt.usedAt = now
	if err == nil {
		m.rt.usedBytes = &used
	}
	m.mu.Unlock()
}

// watchCameras notes which cameras send pictures and reports one that has
// sent none for OfflineAfter (once, until it sends again).
func (m *Manager) watchCameras(stats frigateStats, now time.Time) {
	var report []string
	m.mu.Lock()
	for _, c := range m.cameras {
		entry, known := stats.Cameras[c.ID]
		rt := m.rt.cameras[c.ID]
		if rt == nil {
			rt = &cameraRuntime{}
			m.rt.cameras[c.ID] = rt
		}
		rt.seen = known
		rt.fps, rt.processFPS, rt.detectionFPS, rt.skippedFPS = entry.CameraFPS, entry.ProcessFPS, entry.DetectionFPS, entry.SkippedFPS
		if !c.Enabled || !known || entry.CameraFPS > 0 {
			rt.offlineSince, rt.reported = time.Time{}, false
			continue
		}
		if rt.offlineSince.IsZero() {
			rt.offlineSince = now
		}
		if !rt.reported && now.Sub(rt.offlineSince) >= m.OfflineAfter {
			rt.reported = true
			if c.Notify.Titan && slices.Contains(c.Notify.Kinds, "offline") {
				report = append(report, c.Name)
			}
		}
	}
	// Cameras that were deleted.
	for id := range m.rt.cameras {
		if !slices.ContainsFunc(m.cameras, func(c Camera) bool { return c.ID == id }) {
			delete(m.rt.cameras, id)
		}
	}
	m.mu.Unlock()
	for _, name := range report {
		m.notify("warning", "nvr_camera_offline", name)
	}
}

func (m *Manager) notify(severity, kind, subject string) {
	if m.Notify != nil {
		m.Notify(severity, kind, subject)
	}
}

// deviceTitle is how a device is called in messages.
func deviceTitle(kind string) string {
	switch kind {
	case DetectorNPU:
		return "NPU"
	case DetectorIGPU:
		return "iGPU"
	case DetectorCoral:
		return "Coral"
	case DetectorHailo:
		return "Hailo"
	}
	return "CPU"
}

// fail is called when Frigate keeps ending, never became healthy or lost
// its detector. It reads the cause from the container's log and moves the
// detection to the next device of the chain (NPU → iGPU → CPU; a Coral or
// Hailo → NPU → iGPU → CPU). At the end of the chain, or when the cause is
// not the device (a refused config), the NVR stops and waits for the
// owner. reason "" lets the log decide; since bounds the log to this
// failure.
func (m *Manager) fail(ctx context.Context, reason string, since time.Time) error {
	lctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	log, err := m.Engine.NVRLogs(lctx, 400, since.Add(-10*time.Second))
	cancel()
	if err != nil {
		slog.Warn("NVR: log of the container not readable", "err", err)
	}
	hw := m.hardwareCached(ctx, true)
	m.mu.Lock()
	setting, active := m.settings.Detector.Kind, m.activeLocked()
	m.mu.Unlock()
	found, detail := Classify(log, active, hw.AccelDevice)
	switch {
	case found == ReasonConfigInvalid || reason == ReasonConfigInvalid:
		reason = ReasonConfigInvalid
		if found != ReasonConfigInvalid {
			detail = ""
		}
	case reason == "" || found != ReasonDetectorUnknown:
		reason = found
	}
	next := hw.next(setting, active)
	now := m.now()
	if reason == ReasonConfigInvalid || next == "" {
		m.mu.Lock()
		m.state.Stopped, m.state.StoppedReason, m.state.StoppedDetail = true, reason, detail
		m.rt.stateDirty = true
		m.rt.lastError = &LastError{Code: reason, Detail: detail, At: now}
		m.rt.crashes = nil
		m.mu.Unlock()
		slog.Error("NVR stopped: nothing works", "reason", reason, "detector", active, "detail", detail)
		m.removeContainer(ctx)
		m.setPhase(PhaseStopped)
		m.notify("error", "nvr_stopped", deviceTitle(active))
		m.wake() // stores the state
		return nil
	}
	m.mu.Lock()
	m.state.Active = next
	m.state.Failover = &Failover{From: active, To: next, At: now, Reason: reason, Detail: detail}
	m.rt.stateDirty, m.rt.dirty = true, true
	m.rt.lastError = &LastError{Code: reason, Detail: detail, At: now}
	m.rt.crashes, m.rt.detectorBad = nil, 0
	m.mu.Unlock()
	slog.Warn("NVR: detection moved to another device", "from", active, "to", next, "reason", reason, "detail", detail)
	m.notify("warning", "nvr_failover", deviceTitle(active)+" → "+deviceTitle(next))
	m.wake() // the next look writes the config for the new device
	return nil
}
