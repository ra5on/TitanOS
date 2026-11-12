package nvr

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// DetectorStatus is the chosen device and the one in use.
type DetectorStatus struct {
	// Kind is the setting (auto, npu, …), Active the device in use now.
	Kind   string `json:"kind"`
	Active string `json:"active"`
	// Failover is the last move to another device; null while the chosen
	// one runs.
	Failover *Failover `json:"failover"`
	// InferenceMs is the time per picture; null while not known.
	InferenceMs *float64 `json:"inferenceMs"`
}

// The states of the storage cap.
const (
	CapOff = "off" // no cap is set
	CapOK  = "ok"  // below the cap
	// CapCleaning: above the cap; the oldest recordings are being deleted
	// (or nothing old enough is left to delete).
	CapCleaning = "cleaning"
	// CapExportsExceed: the exports alone are above the cap. Exports are
	// never deleted by TitanOS, so nothing is deleted.
	CapExportsExceed = "exports_exceed"
)

// StorageStatus is the space of the recordings.
type StorageStatus struct {
	// UsedBytes is recordings plus exports; null until Frigate was asked.
	UsedBytes       *uint64 `json:"usedBytes"`
	RecordingsBytes *uint64 `json:"recordingsBytes"`
	ExportsBytes    *uint64 `json:"exportsBytes"`
	LimitGB         int     `json:"limitGB"`
	// SpanDays is how far back the recordings reach; null without any.
	SpanDays *float64 `json:"spanDays"`
	// Cap is the state of the storage cap (CapOff, …).
	Cap string `json:"cap"`
	// LastCleanup is when the cap last deleted recordings (Unix seconds;
	// null if not since titand started).
	LastCleanup *int64 `json:"lastCleanup"`
}

// CameraCounts counts cameras.
type CameraCounts struct {
	Total   int `json:"total"`
	Enabled int `json:"enabled"`
	// Online are enabled cameras that send pictures now.
	Online int `json:"online"`
}

// Status is GET /api/v1/nvr/status.
type Status struct {
	Enabled bool `json:"enabled"`
	// State is a phase of watch.go: off, docker_off, docker_starting,
	// no_volume, pulling, starting, running, stopped, error.
	State string `json:"state"`
	// Error is what last went wrong; null if nothing did.
	Error  *LastError  `json:"error"`
	Image  ImageStatus `json:"image"`
	Volume string      `json:"volume"`
	// VolumeMounted is false while the chosen volume is not there.
	VolumeMounted bool           `json:"volumeMounted"`
	Detector      DetectorStatus `json:"detector"`
	Storage       StorageStatus  `json:"storage"`
	Cameras       CameraCounts   `json:"cameras"`
	// PrivacyNoticeDismissed: the notice on filming people is hidden.
	PrivacyNoticeDismissed bool `json:"privacyNoticeDismissed"`
	RTSPOut                bool `json:"rtspOut"`
	// FrigateVersion is the pinned version.
	FrigateVersion string       `json:"frigateVersion"`
	Hardware       HardwareInfo `json:"hardware"`
}

// CameraState is what the watch knows of a camera.
type CameraState struct {
	// Online: Frigate gets pictures. Known is false while the NVR does
	// not run or has not looked yet.
	Online bool `json:"online"`
	Known  bool `json:"known"`
	// FPS is what the camera sends, DetectionFPS what is examined.
	FPS          float64 `json:"fps"`
	DetectionFPS float64 `json:"detectionFps"`
	// OfflineSince is null while the camera sends.
	OfflineSince *time.Time `json:"offlineSince"`
}

// CameraStates returns the state of every camera by ID.
func (m *Manager) CameraStates() map[string]CameraState {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cameraStatesLocked()
}

func (m *Manager) cameraStatesLocked() map[string]CameraState {
	out := map[string]CameraState{}
	running := m.rt.phase == PhaseRunning
	for _, c := range m.cameras {
		state := CameraState{}
		if rt := m.rt.cameras[c.ID]; rt != nil && running && rt.seen {
			state.Known, state.Online = true, rt.fps > 0
			state.FPS, state.DetectionFPS = rt.fps, rt.detectionFPS
			if !rt.offlineSince.IsZero() {
				since := rt.offlineSince
				state.OfflineSince = &since
			}
		}
		out[c.ID] = state
	}
	return out
}

// Status reports the NVR for administrators; the API narrows it for other
// accounts.
func (m *Manager) Status(ctx context.Context) Status {
	hw := m.hardwareCached(ctx, false)
	mounted := m.mounted()
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	st := Status{
		Enabled: s.Enabled, State: m.rt.phase, Error: m.rt.lastError, Image: m.imageLocked(),
		Volume: s.Volume, VolumeMounted: s.Volume != "" && slices.Contains(mounted, s.Volume),
		Detector:               DetectorStatus{Kind: s.Detector.Kind, Active: m.activeLocked(), Failover: m.state.Failover, InferenceMs: m.rt.inferenceMs},
		Storage:                m.storageStatusLocked(),
		PrivacyNoticeDismissed: s.PrivacyNoticeDismissed, RTSPOut: s.RTSPOut,
		FrigateVersion: FrigateVersion, Hardware: hw,
	}
	if m.state.Stopped && st.Error == nil {
		st.Error = &LastError{Code: m.state.StoppedReason, Detail: m.state.StoppedDetail}
	}
	states := m.cameraStatesLocked()
	for _, c := range m.cameras {
		st.Cameras.Total++
		if c.Enabled {
			st.Cameras.Enabled++
			if states[c.ID].Online {
				st.Cameras.Online++
			}
		}
	}
	return st
}

func (m *Manager) imageLocked() ImageStatus {
	image := m.rt.image
	image.Version = FrigateVersion
	if image.State == "" {
		image.State = "missing"
	}
	if image.Size > 0 {
		image.Progress = min(1, float64(image.Bytes)/float64(image.Size))
	}
	if image.State == "ready" {
		image.Progress = 1
	}
	return image
}

// ContainerDiagnosis is the container as Docker reports it.
type ContainerDiagnosis struct {
	// State is "missing", "running" or "exited".
	State      string     `json:"state"`
	ExitCode   int        `json:"exitCode"`
	StartedAt  *time.Time `json:"startedAt"`
	FinishedAt *time.Time `json:"finishedAt"`
	// RecentEnds counts the ends within the watch's window and the
	// restarts it forced (Frigate hung or ran other cameras).
	RecentEnds int `json:"recentEnds"`
}

// CameraDiagnosis is one camera in the diagnosis.
type CameraDiagnosis struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	CameraState
	ProcessFPS float64 `json:"processFps"`
	SkippedFPS float64 `json:"skippedFps"`
}

// ShmDiagnosis is /dev/shm of the container in MB; null values while
// Frigate does not answer.
type ShmDiagnosis struct {
	TotalMB *float64 `json:"totalMB"`
	UsedMB  *float64 `json:"usedMB"`
	FreeMB  *float64 `json:"freeMB"`
}

// Diagnosis is GET /api/v1/nvr/diagnosis: everything needed to see why
// the detection does not run, without a terminal. It holds no password.
type Diagnosis struct {
	State     string             `json:"state"`
	Container ContainerDiagnosis `json:"container"`
	// ImageReference is what is pulled; ImagePresent whether it is there.
	ImageReference string `json:"imageReference"`
	ImageVersion   string `json:"imageVersion"`
	ImagePresent   bool   `json:"imagePresent"`
	// RunningVersion is what Frigate says of itself ("" while down).
	RunningVersion string         `json:"runningVersion"`
	Detector       DetectorStatus `json:"detector"`
	DetectorPID    *int           `json:"detectorPid"`
	// LastError is the last classified failure with the line it was read
	// from.
	LastError *LastError `json:"lastError"`
	// Log are the last 200 lines of Frigate (of the container while
	// Frigate does not answer); LogSource is "frigate", "container" or "".
	Log       []string `json:"log"`
	LogSource string   `json:"logSource"`
	// AccelDevice: /dev/accel exists. NPUFirmware are the kernel's lines
	// about the NPU (driver intel_vpu, with the firmware's build date).
	AccelDevice bool     `json:"accelDevice"`
	NPUFirmware []string `json:"npuFirmware"`
	// GPUUsage and NPUUsage are Frigate's own figures, as it gives them.
	GPUUsage map[string]json.RawMessage `json:"gpuUsage"`
	NPUUsage map[string]json.RawMessage `json:"npuUsage"`
	Cameras  []CameraDiagnosis          `json:"cameras"`
	Shm      ShmDiagnosis               `json:"shm"`
	Hardware HardwareInfo               `json:"hardware"`
}

// diagnosisLogLines is how much of the log the diagnosis shows.
const diagnosisLogLines = 200

// Diagnosis gathers the diagnosis; it asks Frigate and Docker now.
func (m *Manager) Diagnosis(ctx context.Context) Diagnosis {
	hw := m.hardwareCached(ctx, true)
	m.mu.Lock()
	d := Diagnosis{
		State: m.rt.phase, ImageReference: frigateImage, ImageVersion: FrigateVersion, ImagePresent: m.rt.imageID != "",
		RunningVersion: m.rt.version,
		Detector:       DetectorStatus{Kind: m.settings.Detector.Kind, Active: m.activeLocked(), Failover: m.state.Failover, InferenceMs: m.rt.inferenceMs},
		DetectorPID:    m.rt.detectorPID, LastError: m.rt.lastError,
		Log: []string{}, NPUFirmware: []string{}, Cameras: []CameraDiagnosis{},
		AccelDevice: hw.AccelDevice, Hardware: hw,
		GPUUsage: map[string]json.RawMessage{}, NPUUsage: map[string]json.RawMessage{},
	}
	if m.state.Stopped && d.LastError == nil {
		d.LastError = &LastError{Code: m.state.StoppedReason, Detail: m.state.StoppedDetail}
	}
	container, crashes := m.rt.container, len(m.rt.crashes)+len(m.rt.hangs)
	address, running := m.rt.address, m.rt.phase == PhaseRunning || m.rt.phase == PhaseStarting
	states := m.cameraStatesLocked()
	for _, c := range m.cameras {
		entry := CameraDiagnosis{ID: c.ID, Name: c.Name, CameraState: states[c.ID]}
		if rt := m.rt.cameras[c.ID]; rt != nil {
			entry.ProcessFPS, entry.SkippedFPS = rt.processFPS, rt.skippedFPS
		}
		d.Cameras = append(d.Cameras, entry)
	}
	if stats := m.rt.stats; stats != nil {
		if stats.GPUUsages != nil {
			d.GPUUsage = stats.GPUUsages
		}
		if stats.NPUUsages != nil {
			d.NPUUsage = stats.NPUUsages
		}
		if shm, ok := stats.Service.Storage["/dev/shm"]; ok {
			total, used, free := shm.Total, shm.Used, shm.Free
			d.Shm = ShmDiagnosis{TotalMB: &total, UsedMB: &used, FreeMB: &free}
		}
	}
	m.mu.Unlock()

	d.Container = ContainerDiagnosis{State: "missing", RecentEnds: crashes}
	if container.Exists {
		d.Container.State, d.Container.ExitCode = "exited", container.ExitCode
		if container.Running {
			d.Container.State = "running"
		}
		if !container.StartedAt.IsZero() {
			d.Container.StartedAt = &container.StartedAt
		}
		if !container.FinishedAt.IsZero() && !container.Running {
			d.Container.FinishedAt = &container.FinishedAt
		}
	}
	lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if running && address != "" {
		if lines, err := m.frigate.logs(lctx, m.FrigateBase(address), diagnosisLogLines); err == nil {
			for i := range lines {
				lines[i] = m.redact(lines[i])
			}
			d.Log, d.LogSource = lines, "frigate"
		}
	}
	if d.LogSource == "" && container.Exists && m.Engine != nil {
		if text, err := m.Engine.NVRLogs(lctx, diagnosisLogLines, time.Time{}); err == nil {
			for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
				if line != "" {
					d.Log = append(d.Log, m.redact(line))
				}
			}
			d.LogSource = "container"
		}
	}
	if m.KernelLog != nil {
		d.NPUFirmware = npuLines(m.KernelLog(lctx))
	}
	return d
}

// npuLines are the kernel's messages about the NPU, the last 20.
func npuLines(kernel string) []string {
	out := []string{}
	for _, line := range strings.Split(kernel, "\n") {
		if strings.Contains(line, "intel_vpu") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	if len(out) > 20 {
		out = out[len(out)-20:]
	}
	return out
}

// SourceView is a camera's source as the API answers it: never the
// password.
type SourceView struct {
	Kind        string `json:"kind"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	User        string `json:"user"`
	HasPassword bool   `json:"hasPassword"`
	MainURL     string `json:"mainURL"`
	SubURL      string `json:"subURL"`
	ONVIFPort   int    `json:"onvifPort"`
	AllowPublic bool   `json:"allowPublic"`
}

// CameraView is a camera as the API answers it to administrators.
type CameraView struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Enabled bool        `json:"enabled"`
	Source  SourceView  `json:"source"`
	Detect  Detect      `json:"detect"`
	Record  Record      `json:"record"`
	Audio   Audio       `json:"audio"`
	PTZ     bool        `json:"ptz"`
	Notify  Notify      `json:"notify"`
	Order   int         `json:"order"`
	State   CameraState `json:"state"`
	// Deleted: the camera was deleted and its recordings are kept until
	// ExpiresAt (Unix seconds, like DeletedAt); only administrators see
	// such cameras, and only their recordings and events.
	Deleted   bool   `json:"deleted"`
	DeletedAt *int64 `json:"deletedAt"`
	ExpiresAt *int64 `json:"expiresAt"`
}

// View is a deleted camera as the API lists it for administrators.
func (d DeletedCamera) View() CameraView {
	deleted, expires := d.DeletedAt.Unix(), d.ExpiresAt().Unix()
	return CameraView{
		ID: d.ID, Name: d.Name, Source: SourceView{},
		Detect: Detect{Objects: []string{}}, Record: Record{Mode: RecordOff, RetainDays: d.RetainDays},
		Notify: Notify{Kinds: []string{}}, Deleted: true, DeletedAt: &deleted, ExpiresAt: &expires,
	}
}

// View is the camera without its password.
func (c Camera) View(state CameraState) CameraView {
	s := c.Source
	return CameraView{
		ID: c.ID, Name: c.Name, Enabled: c.Enabled,
		Source: SourceView{Kind: s.Kind, Host: s.Host, Port: s.Port, User: s.User, HasPassword: s.Password != "",
			MainURL: s.MainURL, SubURL: s.SubURL, ONVIFPort: s.ONVIFPort, AllowPublic: s.AllowPublic},
		Detect: c.Detect, Record: c.Record, Audio: c.Audio, PTZ: c.PTZ, Notify: c.Notify, Order: c.Order, State: state,
	}
}

// Live returns the address of a camera's live stream in Frigate
// (WebSocket, MSE) for the proxy of the API; sub picks the small stream.
func (m *Manager) Live(id string, sub bool) (string, error) {
	c, ok := m.Camera(id)
	if !ok {
		return "", ErrNotFound
	}
	m.mu.Lock()
	address, running := m.rt.address, m.rt.phase == PhaseRunning
	m.mu.Unlock()
	if !running || address == "" || !c.Enabled {
		return "", ErrNotRunning
	}
	base := m.FrigateBase(address)
	return "ws" + strings.TrimPrefix(base, "http") + "/live/mse/api/ws?src=" + LiveStream(c, sub), nil
}

// snapshotAge is how long a still is handed out again: widgets and tiles
// of several windows ask for the same picture.
const snapshotAge = time.Second

type snapshotKey struct {
	camera string
	height int
}

type cachedSnapshot struct {
	at      time.Time
	picture []byte
}

// snapshotHeights are the heights stills are made in: a height asked for
// is taken up to the next of them, so that the cache holds a few pictures
// per camera, not one per pixel.
var snapshotHeights = []int{90, 180, 360, 720, 1080, 2160}

// snapshotHeight is the height a still of height pixels is made in (0:
// as Frigate keeps it).
func snapshotHeight(height int) int {
	if height <= 0 {
		return 0
	}
	for _, h := range snapshotHeights {
		if height <= h {
			return h
		}
	}
	return snapshotHeights[len(snapshotHeights)-1]
}

// Snapshot fetches the latest picture of a camera as JPEG, at least
// height pixels high (the next of snapshotHeights; 0: as Frigate keeps
// it). The same picture is handed out for snapshotAge; the caller must not
// change it.
func (m *Manager) Snapshot(ctx context.Context, id string, height int) ([]byte, error) {
	height = snapshotHeight(height)
	c, ok := m.Camera(id)
	if !ok {
		return nil, ErrNotFound
	}
	m.mu.Lock()
	address, running := m.rt.address, m.rt.phase == PhaseRunning
	key := snapshotKey{id, height}
	cached, hit := m.snapshots[key]
	m.mu.Unlock()
	if !running || address == "" || !c.Enabled {
		return nil, ErrNotRunning
	}
	now := time.Now()
	if hit && now.Sub(cached.at) < snapshotAge {
		return cached.picture, nil
	}
	picture, err := m.frigate.snapshot(ctx, m.FrigateBase(address), id, height)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	if m.snapshots == nil {
		m.snapshots = map[snapshotKey]cachedSnapshot{}
	}
	// Old pictures of other sizes and cameras do not pile up.
	for other, entry := range m.snapshots {
		if now.Sub(entry.at) > 10*snapshotAge {
			delete(m.snapshots, other)
		}
	}
	m.snapshots[key] = cachedSnapshot{at: now, picture: picture}
	m.mu.Unlock()
	return picture, nil
}
