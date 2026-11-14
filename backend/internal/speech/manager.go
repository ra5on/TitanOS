package speech

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const (
	settingsFile = "system/speech.json"
	feature      = "speech"
	// Folders below .titanos/speech on the volume.
	whisperDir = "whisper"
	piperDir   = "piper"

	whisperService = docker.SpeechNetwork + "-whisper"
	piperService   = docker.SpeechNetwork + "-piper"
)

// The images, one checked version per TitanOS release, pinned by digest
// (the index of the tag as of 14.11.2026; change version and digest
// together). They are pulled when speech is switched on, not shipped.
const (
	WhisperRepository = "lscr.io/linuxserver/faster-whisper"
	WhisperVersion    = "v3.8.1-ls13"
	WhisperDigest     = "sha256:619615d262b677b52bdca3ec126096dab096df465bf86f7b6fd8316825e3a6b2"
	// The build with CUDA for NVIDIA GPUs (tag gpu, amd64 only).
	WhisperGPUDigest = "sha256:5451c1dae4ac75e147e61444fb045cfe29c413f5054421b80838182572da07ec"
	PiperRepository  = "lscr.io/linuxserver/piper"
	PiperVersion     = "2.5.3-ls138"
	PiperDigest      = "sha256:f0d63b705b6f345866a4db9adf1b2f3b7cefeed6d681694df643a28dab2f4fed"
)

// Engine is what speech needs of Docker (docker.Manager).
type Engine interface {
	EngineReady() bool
	SpeechInspect(ctx context.Context, name string) (docker.SpeechState, error)
	SpeechCreate(ctx context.Context, spec docker.SpeechSpec) error
	SpeechStart(ctx context.Context, name string) error
	SpeechRemove(ctx context.Context, name string) error
	SpeechNetworkEnsure(ctx context.Context) error
	SpeechLogs(ctx context.Context, name string, tail int) (string, error)
	NVRImageID(ctx context.Context, ref string) (string, error)
	NVRPull(ctx context.Context, repository, digest string, progress func(done, total int64)) error
}

// Errors the API turns into answers.
var (
	ErrNoVolume   = errors.New("speech: the volume of the models is not there")
	ErrUnknown    = errors.New("speech: no such model or voice")
	ErrBusy       = errors.New("speech: this is being downloaded already")
	ErrActive     = errors.New("speech: the active model or voice cannot be removed")
	ErrNotRunning = errors.New("speech: the service is not running")
	ErrNotThere   = errors.New("speech: download it first")
)

// Manager runs the two services.
type Manager struct {
	MountRoot string
	// Mounted are the volumes that are there right now.
	Mounted func() []string
	// Engine is Docker; nil in development mode (nothing is started).
	Engine Engine
	// Devices are the devices TitanOS offers to containers.
	Devices func(context.Context) []hardware.ContainerDevice
	// HomeAssistant are the addresses of the Home Assistants set up.
	HomeAssistant func(context.Context) []net.IP

	store *config.Store
	hub   *hub

	mu       sync.Mutex
	settings Settings
	loaded   bool
	voices   []Voice
	voicesAt time.Time
	jobs     map[string]*job
	services map[string]*service
	pulls    map[string]*pull
	ha       []net.IP
	haAt     time.Time
	kick     chan struct{}
	whisper  *gate
	piper    *gate
}

type job struct {
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
	Error string `json:"error,omitempty"`
	stop  context.CancelFunc
}

type pull struct {
	done, total int64
	err         string
	running     bool
	at          time.Time
}

// service is what is known about one container.
type service struct {
	// Phase: off, model (none chosen or downloaded), pulling, starting,
	// running, error.
	phase   string
	message string
	address string
	// starts counts the starts of a container that keeps ending.
	starts  int
	lastTry time.Time
}

// New makes a manager; Load reads its settings.
func New(store *config.Store) *Manager {
	m := &Manager{store: store, MountRoot: "/mnt", hub: newHub(), settings: Defaults(), jobs: map[string]*job{},
		services: map[string]*service{whisperService: {phase: "off"}, piperService: {phase: "off"}}, pulls: map[string]*pull{}, kick: make(chan struct{}, 1)}
	m.whisper = &gate{port: WhisperPort, target: func() string { return m.target(whisperService, WhisperPort) }, allow: m.allowed}
	m.piper = &gate{port: PiperPort, target: func() string { return m.target(piperService, PiperPort) }, allow: m.allowed}
	return m
}

// Load reads the settings from /config. Without the file speech is off.
func (m *Manager) Load() error {
	s := Defaults()
	err := m.store.ReadJSON(settingsFile, &s)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := s.Check(); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings, m.loaded = s, true
	m.mu.Unlock()
	return nil
}

// Settings returns the current settings.
func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// SetSettings checks and saves; the services follow with the next look.
func (m *Manager) SetSettings(s Settings) error {
	if err := s.Check(); err != nil {
		return err
	}
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return errors.New("speech: the settings could not be read from /config; nothing was changed")
	}
	m.mu.Unlock()
	if s.Enabled && s.Volume == "" {
		return ErrNoVolume
	}
	if s.Volume != "" && m.Mounted != nil && !slices.Contains(m.Mounted(), s.Volume) {
		return ErrNoVolume
	}
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	// A changed setting gets a fresh try.
	for _, svc := range m.services {
		svc.starts = 0
	}
	m.mu.Unlock()
	m.Kick()
	return nil
}

// Kick asks for a look right now.
func (m *Manager) Kick() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Run looks after the services until ctx ends.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		m.tick(ctx)
		select {
		case <-ctx.Done():
			m.whisper.close()
			m.piper.close()
			return
		case <-ticker.C:
		case <-m.kick:
		}
	}
}

// root opens .titanos/speech on the volume of the settings.
func (m *Manager) root() (*os.Root, error) {
	s := m.Settings()
	if s.Volume == "" || m.Mounted == nil {
		return nil, ErrNoVolume
	}
	root, err := volumedata.Open(m.MountRoot, s.Volume, feature, m.Mounted())
	if err != nil {
		return nil, ErrNoVolume
	}
	for _, dir := range []string{whisperDir, piperDir} {
		if err := root.Mkdir(dir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
			root.Close()
			return nil, err
		}
	}
	return root, nil
}

func (m *Manager) hostPath(parts ...string) string {
	return filepath.Join(append([]string{m.MountRoot, m.Settings().Volume, ".titanos", feature}, parts...)...)
}

// ---------- models and voices ----------

// Voices is the catalog of Piper voices: read from Hugging Face at most
// once a day, else from the copy on the volume or in memory.
func (m *Manager) Voices(ctx context.Context) ([]Voice, error) {
	m.mu.Lock()
	list, at := m.voices, m.voicesAt
	m.mu.Unlock()
	if list != nil && time.Since(at) < 24*time.Hour {
		return list, nil
	}
	fresh, raw, err := m.hub.voices(ctx)
	if err == nil {
		if root, rerr := m.root(); rerr == nil {
			// The copy lets the list show while the internet is away.
			if werr := volumedata.Write(root, "voices.json", raw); werr != nil {
				slog.Warn("speech: catalog of voices not kept", "err", werr)
			}
			root.Close()
		}
	} else {
		if list != nil {
			return list, nil
		}
		root, rerr := m.root()
		if rerr != nil {
			return nil, err
		}
		defer root.Close()
		raw, rerr := root.ReadFile("voices.json")
		if rerr != nil {
			return nil, err
		}
		if fresh, rerr = parseVoices(raw); rerr != nil {
			return nil, err
		}
	}
	m.mu.Lock()
	m.voices, m.voicesAt = fresh, time.Now()
	m.mu.Unlock()
	return fresh, nil
}

func (m *Manager) voice(ctx context.Context, id string) (Voice, error) {
	list, err := m.Voices(ctx)
	if err != nil {
		return Voice{}, err
	}
	for _, v := range list {
		if v.ID == id {
			return v, nil
		}
	}
	return Voice{}, ErrUnknown
}

// Installed lists what is on the volume: models and voices by ID.
func (m *Manager) Installed() (models, voices []string) {
	models, voices = []string{}, []string{}
	root, err := m.root()
	if err != nil {
		return
	}
	defer root.Close()
	return installed(root)
}

func installed(root *os.Root) (models, voices []string) {
	models, voices = []string{}, []string{}
	if dir, err := root.Open(whisperDir); err == nil {
		names, _ := dir.Readdirnames(-1)
		dir.Close()
		for _, n := range names {
			if WhisperModelByID(n) == nil {
				continue
			}
			if st, err := root.Stat(whisperDir + "/" + n + "/model.bin"); err == nil && st.Mode().IsRegular() {
				models = append(models, n)
			}
		}
	}
	if dir, err := root.Open(piperDir); err == nil {
		names, _ := dir.Readdirnames(-1)
		dir.Close()
		for _, n := range names {
			id, ok := strings.CutSuffix(n, ".onnx")
			if !ok || !voiceRE.MatchString(id) {
				continue
			}
			if st, err := root.Stat(piperDir + "/" + n + ".json"); err == nil && st.Mode().IsRegular() {
				voices = append(voices, id)
			}
		}
	}
	slices.Sort(models)
	slices.Sort(voices)
	return
}

// Jobs are the downloads that run or failed, by "model/<id>" or "voice/<id>".
func (m *Manager) Jobs() map[string]job {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]job{}
	for k, j := range m.jobs {
		out[k] = job{Done: j.Done, Total: j.Total, Error: j.Error}
	}
	return out
}

// Download fetches a model ("model") or a voice ("voice") in the
// background; Jobs shows how far it is.
func (m *Manager) Download(ctx context.Context, kind, id string) error {
	var files []remoteFile
	var dir, tmp string
	switch kind {
	case "model":
		model := WhisperModelByID(id)
		if model == nil {
			return ErrUnknown
		}
		list, err := m.hub.modelFiles(ctx, *model)
		if err != nil {
			return err
		}
		// Into a folder beside its place, moved there when complete.
		files, tmp, dir = list, whisperDir+"/.new-"+id, whisperDir+"/"+id
	case "voice":
		v, err := m.voice(ctx, id)
		if err != nil {
			return err
		}
		files, dir = v.remote(), piperDir
	default:
		return ErrUnknown
	}
	root, err := m.root()
	if err != nil {
		return err
	}
	key := kind + "/" + id
	m.mu.Lock()
	if j := m.jobs[key]; j != nil && j.Error == "" {
		m.mu.Unlock()
		root.Close()
		return ErrBusy
	}
	run, stop := context.WithCancel(context.Background())
	j := &job{stop: stop}
	for _, f := range files {
		j.Total += f.bytes
	}
	m.jobs[key] = j
	m.mu.Unlock()
	go func() {
		defer root.Close()
		defer stop()
		err := m.fetch(run, root, files, dir, tmp, j)
		m.mu.Lock()
		if err != nil && !errors.Is(err, context.Canceled) {
			j.Error = err.Error()
			slog.Warn("speech: download failed", "what", key, "err", err)
		} else {
			delete(m.jobs, key)
		}
		m.mu.Unlock()
		m.Kick()
	}()
	return nil
}

func (m *Manager) fetch(ctx context.Context, root *os.Root, files []remoteFile, dir, tmp string, j *job) error {
	into := dir
	if tmp != "" {
		root.RemoveAll(tmp) // what an interrupted download left
		if err := root.Mkdir(tmp, 0700); err != nil {
			return err
		}
		into = tmp
		defer root.RemoveAll(tmp)
	}
	for _, f := range files {
		if err := m.hub.download(ctx, root, into, f, func(n int64) {
			m.mu.Lock()
			j.Done += n
			m.mu.Unlock()
		}); err != nil {
			return err
		}
	}
	if tmp != "" {
		root.RemoveAll(dir)
		return root.Rename(tmp, dir)
	}
	return nil
}

// Cancel stops a download; a failed one is forgotten.
func (m *Manager) Cancel(kind, id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j := m.jobs[kind+"/"+id]; j != nil {
		j.stop()
		delete(m.jobs, kind+"/"+id)
	}
}

// Remove deletes a model or voice from the volume; not the active one.
func (m *Manager) Remove(kind, id string) error {
	s := m.Settings()
	root, err := m.root()
	if err != nil {
		return err
	}
	defer root.Close()
	switch kind {
	case "model":
		if WhisperModelByID(id) == nil {
			return ErrUnknown
		}
		if s.WhisperModel == id {
			return ErrActive
		}
		return root.RemoveAll(whisperDir + "/" + id)
	case "voice":
		if !voiceRE.MatchString(id) {
			return ErrUnknown
		}
		if s.Voice == id {
			return ErrActive
		}
		for _, name := range []string{id + ".onnx", id + ".onnx.json"} {
			if err := root.Remove(piperDir + "/" + name); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
		}
		return nil
	}
	return ErrUnknown
}

// ---------- who may connect ----------

func (m *Manager) allowed(ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	if m.Settings().Reach == ReachLAN {
		return homeNetwork(ip)
	}
	for _, n := range dockerNetworks() {
		if n.Contains(ip) {
			return true
		}
	}
	m.mu.Lock()
	list, at := m.ha, m.haAt
	m.mu.Unlock()
	if m.HomeAssistant != nil && time.Since(at) > time.Minute {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		list = m.HomeAssistant(ctx)
		cancel()
		m.mu.Lock()
		m.ha, m.haAt = list, time.Now()
		m.mu.Unlock()
	}
	for _, a := range list {
		if a.Equal(ip) {
			return true
		}
	}
	return false
}

func (m *Manager) target(name string, port int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if svc := m.services[name]; svc.phase == "running" && svc.address != "" {
		return net.JoinHostPort(svc.address, strconv.Itoa(port))
	}
	return ""
}

// Address is where titand reaches a running service ("whisper", "piper").
func (m *Manager) Address(which string) (string, error) {
	name, port := whisperService, WhisperPort
	if which == "piper" {
		name, port = piperService, PiperPort
	}
	if addr := m.target(name, port); addr != "" {
		return addr, nil
	}
	return "", ErrNotRunning
}

// ---------- the containers ----------

func (m *Manager) set(name, phase, message, address string) {
	m.mu.Lock()
	svc := m.services[name]
	svc.phase, svc.message, svc.address = phase, message, address
	m.mu.Unlock()
}

// cdi are the CDI names of the chosen GPU, nil for the processor; ok is
// false when the chosen GPU is not there (driver, a VM has it).
func (m *Manager) cdi(ctx context.Context, device string) (names []string, ok bool) {
	if device == DeviceCPU || device == "" {
		return nil, true
	}
	if m.Devices == nil {
		return nil, false
	}
	for _, d := range m.Devices(ctx) {
		if d.ID != device || d.Kind != "nvidia" || !d.Available {
			continue
		}
		for _, mapping := range d.Mappings {
			if strings.HasPrefix(mapping, "nvidia.com/gpu=") {
				names = append(names, mapping)
			}
		}
		return names, len(names) > 0
	}
	return nil, false
}

func (m *Manager) tick(ctx context.Context) {
	s := m.Settings()
	if m.Engine == nil {
		return
	}
	if !s.Enabled {
		m.whisper.close()
		m.piper.close()
		if m.Engine.EngineReady() {
			for _, name := range []string{whisperService, piperService} {
				rctx, cancel := context.WithTimeout(ctx, 40*time.Second)
				if err := m.Engine.SpeechRemove(rctx, name); err != nil {
					slog.Warn("speech: container not removed", "name", name, "err", err)
				}
				cancel()
				m.set(name, "off", "", "")
			}
		}
		return
	}
	root, err := m.root()
	if err != nil || !m.Engine.EngineReady() {
		message := "docker"
		if err != nil {
			message = "volume"
		}
		for _, name := range []string{whisperService, piperService} {
			if m.Engine.EngineReady() {
				rctx, cancel := context.WithTimeout(ctx, 40*time.Second)
				m.Engine.SpeechRemove(rctx, name) // the models went away with the volume; a container must not keep the folder
				cancel()
			}
			m.set(name, "error", message, "")
		}
		return
	}
	models, voices := installed(root)
	root.Close()
	m.whisper.open()
	m.piper.open()
	nctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	err = m.Engine.SpeechNetworkEnsure(nctx)
	cancel()
	if err != nil {
		m.set(whisperService, "error", err.Error(), "")
		m.set(piperService, "error", err.Error(), "")
		return
	}

	// The services read the models as the user titand is (root on the NAS):
	// the folder is private to it (.titanos, 0700).
	owner := [2]string{"PUID=" + strconv.Itoa(os.Geteuid()), "PGID=" + strconv.Itoa(os.Getegid())}

	// Speech to text.
	if s.WhisperModel == "" || !slices.Contains(models, s.WhisperModel) {
		m.remove(ctx, whisperService, "model")
	} else if names, ok := m.cdi(ctx, s.Device); !ok {
		m.remove(ctx, whisperService, "error")
		m.set(whisperService, "error", "device", "")
	} else {
		digest := WhisperDigest
		if len(names) > 0 {
			digest = WhisperGPUDigest
		}
		env := []string{owner[0], owner[1], "LOCAL_ONLY=1", "WHISPER_MODEL=/config/" + s.WhisperModel, "WHISPER_LANG=" + s.Language, "WHISPER_BEAM=1"}
		m.ensure(ctx, docker.SpeechSpec{Name: whisperService, Data: m.hostPath(whisperDir), Env: env, CDIDevices: names}, WhisperRepository, digest)
	}

	// Text to speech (on the processor: the GPU build of Piper is not
	// checked yet, PLAN 92).
	if s.Voice == "" || !slices.Contains(voices, s.Voice) {
		m.remove(ctx, piperService, "model")
	} else {
		// Piper's length scale is the inverse of the speed.
		length := strconv.FormatFloat(1/s.Speed, 'f', 2, 64)
		env := []string{owner[0], owner[1], "LOCAL_ONLY=1", "PIPER_VOICE=" + s.Voice, "PIPER_LENGTH=" + length}
		m.ensure(ctx, docker.SpeechSpec{Name: piperService, Data: m.hostPath(piperDir), Env: env}, PiperRepository, PiperDigest)
	}
}

func (m *Manager) remove(ctx context.Context, name, phase string) {
	rctx, cancel := context.WithTimeout(ctx, 40*time.Second)
	defer cancel()
	if err := m.Engine.SpeechRemove(rctx, name); err != nil {
		m.set(name, "error", err.Error(), "")
		return
	}
	m.set(name, phase, "", "")
}

// ensure brings one container to its spec: pulls the image, replaces a
// container made from other settings, starts one that ended.
func (m *Manager) ensure(ctx context.Context, spec docker.SpeechSpec, repository, digest string) {
	ref := repository + "@" + digest
	ictx, cancel := context.WithTimeout(ctx, 20*time.Second)
	image, err := m.Engine.NVRImageID(ictx, ref)
	cancel()
	if err != nil {
		m.set(spec.Name, "error", err.Error(), "")
		return
	}
	if image == "" {
		m.pull(ref, repository, digest)
		m.mu.Lock()
		p := m.pulls[ref]
		phase, message := "pulling", ""
		if p != nil && !p.running && p.err != "" {
			phase, message = "error", p.err
		}
		m.mu.Unlock()
		m.set(spec.Name, phase, message, "")
		return
	}
	spec.Image = image
	sum := sha256.New()
	json.NewEncoder(sum).Encode(spec)
	spec.Hash = hex.EncodeToString(sum.Sum(nil))[:32]

	cctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	st, err := m.Engine.SpeechInspect(cctx, spec.Name)
	if err != nil {
		m.set(spec.Name, "error", err.Error(), "")
		return
	}
	m.mu.Lock()
	svc := m.services[spec.Name]
	starts, last := svc.starts, svc.lastTry
	m.mu.Unlock()
	switch {
	case st.Exists && st.Hash == spec.Hash && st.Running:
		// Up for a while: earlier ends no longer count.
		if time.Since(st.StartedAt) > 2*time.Minute {
			m.mu.Lock()
			svc.starts = 0
			m.mu.Unlock()
		}
		m.set(spec.Name, "running", "", st.Address)
		return
	case st.Exists && st.Hash == spec.Hash && starts >= 3:
		// It keeps ending: say why instead of starting it for ever.
		logs, _ := m.Engine.SpeechLogs(cctx, spec.Name, 20)
		m.set(spec.Name, "error", lastLines(logs, 600), "")
		return
	case st.Exists && st.Hash == spec.Hash:
		if time.Since(last) < 10*time.Second {
			return
		}
		err = m.Engine.SpeechStart(cctx, spec.Name)
	default:
		if st.Exists {
			if err = m.Engine.SpeechRemove(cctx, spec.Name); err != nil {
				break
			}
		}
		err = m.Engine.SpeechCreate(cctx, spec)
	}
	m.mu.Lock()
	svc.starts, svc.lastTry = starts+1, time.Now()
	m.mu.Unlock()
	if err != nil {
		message := err.Error()
		if docker.IsCDIError(err) {
			message = "device"
		}
		m.set(spec.Name, "error", message, "")
		return
	}
	m.set(spec.Name, "starting", "", "")
	// Look again soon: the address is known once it runs.
	time.AfterFunc(3*time.Second, m.Kick)
}

func (m *Manager) pull(ref, repository, digest string) {
	m.mu.Lock()
	// One pull at a time; a failed one is tried again after five minutes.
	if p := m.pulls[ref]; p != nil && (p.running || time.Since(p.at) < 5*time.Minute) {
		m.mu.Unlock()
		return
	}
	p := &pull{running: true, at: time.Now()}
	m.pulls[ref] = p
	m.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		err := m.Engine.NVRPull(ctx, repository, digest, func(done, total int64) {
			m.mu.Lock()
			p.done, p.total = done, total
			m.mu.Unlock()
		})
		m.mu.Lock()
		p.running = false
		if err != nil {
			p.err = err.Error()
			slog.Warn("speech: image not pulled", "image", ref, "err", err)
		}
		m.mu.Unlock()
		m.Kick()
	}()
}

func lastLines(text string, limit int) string {
	text = strings.TrimSpace(text)
	if len(text) > limit {
		text = text[len(text)-limit:]
		if i := strings.IndexByte(text, '\n'); i >= 0 {
			text = text[i+1:]
		}
	}
	return text
}

// ---------- what the page shows ----------

// ServiceStatus is one service on the page.
type ServiceStatus struct {
	// Phase: off, model, pulling, starting, running, error.
	Phase string `json:"phase"`
	// Message with phase error: "volume", "docker", "device", or the
	// engine's or the service's own words.
	Message string `json:"message,omitempty"`
	// PullDone and PullTotal while the image is pulled.
	PullDone  int64  `json:"pullDone,omitempty"`
	PullTotal int64  `json:"pullTotal,omitempty"`
	Port      int    `json:"port"`
	Listening bool   `json:"listening"`
	PortError string `json:"portError,omitempty"`
	Seen      *Seen  `json:"seen,omitempty"`
	Version   string `json:"version"`
}

// Status is the state of both services.
type Status struct {
	Whisper ServiceStatus `json:"whisper"`
	Piper   ServiceStatus `json:"piper"`
	// Available is false in development mode: nothing can be started.
	Available bool `json:"available"`
}

// Status reads the state for the page.
func (m *Manager) Status() Status {
	s := m.Settings()
	one := func(name string, g *gate, port int, version, ref string) ServiceStatus {
		m.mu.Lock()
		svc := *m.services[name]
		var p pull
		if x := m.pulls[ref]; x != nil {
			p = *x
		}
		m.mu.Unlock()
		out := ServiceStatus{Phase: svc.phase, Message: svc.message, Port: port, Version: version}
		if svc.phase == "pulling" {
			out.PullDone, out.PullTotal = p.done, p.total
		}
		out.Listening, out.PortError, out.Seen = g.state()
		return out
	}
	whisperRef := WhisperRepository + "@" + WhisperDigest
	if s.Device != DeviceCPU {
		whisperRef = WhisperRepository + "@" + WhisperGPUDigest
	}
	return Status{Available: m.Engine != nil,
		Whisper: one(whisperService, m.whisper, WhisperPort, WhisperVersion, whisperRef),
		Piper:   one(piperService, m.piper, PiperPort, PiperVersion, PiperRepository+"@"+PiperDigest)}
}

// Say speaks a text with a voice that is downloaded.
func (m *Manager) Say(ctx context.Context, text, voice string) (Audio, error) {
	addr, err := m.Address("piper")
	if err != nil {
		return Audio{}, err
	}
	if voice != "" {
		_, voices := m.Installed()
		if !slices.Contains(voices, voice) {
			return Audio{}, ErrNotThere
		}
	}
	return Synthesize(ctx, addr, text, voice)
}

// Hear writes down what was said.
func (m *Manager) Hear(ctx context.Context, audio Audio, language string) (string, error) {
	addr, err := m.Address("whisper")
	if err != nil {
		return "", err
	}
	text, err := Transcribe(ctx, addr, audio, language)
	return strings.TrimSpace(text), err
}
