package viewer

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/hardware"
)

// fakeEngine records what the viewer asks of Docker.
type fakeEngine struct {
	mu         sync.Mutex
	viewers    []docker.ViewerContainer
	removed    []string
	created    []docker.ViewerSpec
	started    []string
	attached   map[string]bool // network|app container → carries the marker
	detached   []string
	images     map[string]string
	loaded     int
	endpoints  []docker.AppEndpoint
	listErr    error
	imageList  []docker.ViewerImage
	networks   []string
	netRemoved []string
	stale      map[string]bool
	memory     uint64
	inUse      map[string]bool
	removedIm  []string
}

func newFakeEngine() *fakeEngine {
	return &fakeEngine{attached: map[string]bool{}, images: map[string]string{}, endpoints: []docker.AppEndpoint{{
		ID: "app1", Name: "immich-server", State: "running", NetworkMode: "immich_default", WebUI: "2283/photos",
		Ports: []docker.Port{{Host: 2283, Container: 3001, Protocol: "tcp"}},
	}}}
}

func (f *fakeEngine) Viewers(context.Context) ([]docker.ViewerContainer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.viewers), f.listErr
}
func (f *fakeEngine) ViewerCreate(_ context.Context, spec docker.ViewerSpec) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created = append(f.created, spec)
	id := fmt.Sprintf("v%d", len(f.created))
	f.viewers = append(f.viewers, docker.ViewerContainer{ID: id, Name: spec.Name, User: spec.User, App: spec.App})
	return id, nil
}
func (f *fakeEngine) ViewerStart(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started = append(f.started, id)
	return nil
}
func (f *fakeEngine) ViewerRemove(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removed = append(f.removed, id)
	f.viewers = slices.DeleteFunc(f.viewers, func(v docker.ViewerContainer) bool { return v.ID == id })
	return nil
}
func (f *fakeEngine) ViewerState(_ context.Context, id, network string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stale[id] {
		return "172.30.0.9", true, nil
	}
	return "172.30.0.5", true, nil
}
func (f *fakeEngine) ViewerMemory(context.Context, string) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.memory != 0 {
		return f.memory, nil
	}
	return 300 << 20, nil
}
func (f *fakeEngine) ViewerImageID(_ context.Context, ref string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.images[ref], nil
}
func (f *fakeEngine) ViewerLoadImage(_ context.Context, r io.Reader) error {
	data, _ := io.ReadAll(r)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.loaded++
	if string(data) == "image" {
		f.images[testImageID] = testImageID
	}
	return nil
}
func (f *fakeEngine) ViewerNetwork(_ context.Context, name, bridge, user, app string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(bridge) > 15 {
		return errors.New("bridge name too long")
	}
	f.networks = append(f.networks, name)
	return nil
}
func (f *fakeEngine) ViewerNetworks(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.networks), nil
}
func (f *fakeEngine) ViewerRemoveNetwork(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.networks = slices.DeleteFunc(f.networks, func(n string) bool { return n == name })
	f.netRemoved = append(f.netRemoved, name)
	return nil
}
func (f *fakeEngine) AppEndpoints(context.Context, string) ([]docker.AppEndpoint, error) {
	return f.endpoints, nil
}
func (f *fakeEngine) ViewerAttach(_ context.Context, network, id, _ string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attached[network+"|"+id] = true
	return "172.30.0.2", true, nil
}
func (f *fakeEngine) ViewerDetach(_ context.Context, network, id, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.attached[network+"|"+id] {
		delete(f.attached, network+"|"+id)
		f.detached = append(f.detached, id)
	}
	return nil
}
func (f *fakeEngine) ViewerAttached(_ context.Context, network, _ string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := []string{}
	for key := range f.attached {
		if n, id, _ := strings.Cut(key, "|"); n == network {
			out = append(out, id)
		}
	}
	return out, nil
}

func (f *fakeEngine) ViewerImages(context.Context) ([]docker.ViewerImage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.imageList), nil
}
func (f *fakeEngine) ViewerRemoveImage(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.inUse[id] {
		return errors.New("conflict: image is being used")
	}
	f.removedIm = append(f.removedIm, id)
	return nil
}

const testImageID = "sha256:" + "ab0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcd"

func testManager(t *testing.T, engine *fakeEngine) *Manager {
	t.Helper()
	m := New(config.Open(t.TempDir()))
	m.Engine = engine
	m.Version = "0.60.0"
	m.MountRoot = t.TempDir()
	os.Mkdir(filepath.Join(m.MountRoot, "data"), 0o755)
	m.Mounted = func() []string { return []string{"data"} }
	m.DockerVolume = func() string { return "data" }
	m.MemAvailable = func() (uint64, error) { return 8 << 30, nil }
	m.Linger = time.Hour
	m.PolicyRoot = t.TempDir()
	checkSpace = func(string, int64) error { return nil }
	profileUID = os.Getuid()
	t.Cleanup(func() { checkSpace, profileUID = spaceFor, 1000 })
	// The image is there already.
	m.image.status.State, m.image.id = "ready", testImageID
	engine.images[testImageID] = testImageID
	return m
}

func TestValidateSettings(t *testing.T) {
	gpus := []GPU{{ID: "/dev/dri/renderD128", Kind: "vaapi", Available: true}}
	ok := Defaults()
	if err := Validate(ok, gpus, nil); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Settings){
		"unknown device": func(s *Settings) { s.GPU = "/dev/sda" },
		"quality":        func(s *Settings) { s.Quality = "ultra" },
		"fps":            func(s *Settings) { s.FPS = 25 },
		"volume":         func(s *Settings) { s.Volume = "gone" },
		"volume path":    func(s *Settings) { s.Volume = "../etc" },
	} {
		s := Defaults()
		change(&s)
		if err := Validate(s, gpus, []string{"data"}); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	s := Defaults()
	s.GPU, s.FPS, s.Volume, s.Quality = "/dev/dri/renderD128", 30, "data", "low"
	if err := Validate(s, gpus, []string{"data"}); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsLoadKeepsDefaultsForMissingFields(t *testing.T) {
	store := config.Open(t.TempDir())
	store.Write(settingsFile, []byte(`{"fps":30}`))
	m := New(store)
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	got := m.Settings()
	if got.FPS != 30 || got.GPU != "auto" || got.Quality != "auto" || !got.Audio {
		t.Fatalf("settings: %+v", got)
	}
	// Without a file: defaults, no error.
	empty := New(config.Open(t.TempDir()))
	if err := empty.Load(); err != nil || empty.Settings() != Defaults() {
		t.Fatalf("no file: %+v %v", empty.Settings(), err)
	}
}

func manifest(viewer string) []byte {
	return []byte(`{"version":"0.60.0","tag":"v0.60.0","viewer":` + viewer + `}`)
}

const goodEntry = `{"asset":"titanos-viewer-0.60.0.tar.zst","sha256":"` + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" + `","size":5,"imageId":"` + testImageID + `"}`

func TestParseReleaseImage(t *testing.T) {
	img, err := ParseReleaseImage(manifest(goodEntry), "0.60.0")
	if err != nil || img.ImageID != testImageID || img.Size != 5 {
		t.Fatalf("good entry: %+v %v", img, err)
	}
	if _, err := ParseReleaseImage([]byte(`{"version":"0.60.0","tag":"v0.60.0"}`), "0.60.0"); !errors.Is(err, ErrNoImage) {
		t.Fatalf("release without viewer: %v", err)
	}
	if _, err := ParseReleaseImage(manifest(goodEntry), "0.61.0"); err == nil {
		t.Fatal("manifest of another release accepted")
	}
	for _, bad := range []string{
		strings.Replace(goodEntry, "titanos-viewer-0.60.0", "../evil", 1),
		strings.Replace(goodEntry, `"sha256":"0123`, `"sha256":"zz23`, 1),
		strings.Replace(goodEntry, `"size":5`, `"size":0`, 1),
		strings.Replace(goodEntry, `"size":5`, `"size":99999999999`, 1),
		strings.Replace(goodEntry, testImageID, "titanos-viewer:latest", 1),
	} {
		if _, err := ParseReleaseImage(manifest(bad), "0.60.0"); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

// fakeRelease stands for the signed release; signature and hash checks
// are in plugins (release_assets.go) and fail as errors here.
type fakeRelease struct {
	manifest    []byte
	manifestErr error
	downloadErr error
	body        string
}

func (f fakeRelease) ReleaseManifest(context.Context, string) ([]byte, error) {
	return f.manifest, f.manifestErr
}
func (f fakeRelease) DownloadReleaseAsset(_ context.Context, _, _, dest, _ string, _ int64) error {
	if f.downloadErr != nil {
		return f.downloadErr
	}
	return os.WriteFile(dest, []byte(f.body), 0o600)
}

func waitImage(t *testing.T, m *Manager) ImageStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st := m.ImageStatus(); st.State == "ready" || st.State == "error" {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("image never settled")
	return ImageStatus{}
}

func TestImageIsVerifiedAndLoaded(t *testing.T) {
	for name, tc := range map[string]struct {
		release fakeRelease
		state   string
	}{
		"good":          {fakeRelease{manifest: manifest(goodEntry), body: "image"}, "ready"},
		"bad signature": {fakeRelease{manifestErr: errors.New("plugins: invalid")}, "error"},
		"bad hash":      {fakeRelease{manifest: manifest(goodEntry), downloadErr: errors.New("plugins: checksum")}, "error"},
		"other image":   {fakeRelease{manifest: manifest(goodEntry), body: "not the image"}, "error"},
		"no viewer":     {fakeRelease{manifest: []byte(`{"version":"0.60.0","tag":"v0.60.0"}`)}, "error"},
	} {
		t.Run(name, func(t *testing.T) {
			engine := newFakeEngine()
			m := testManager(t, engine)
			m.image = imageState{}
			delete(engine.images, testImageID)
			m.Release = tc.release
			if _, err := m.Open(t.Context(), "anna", "immich"); !errors.Is(err, ErrImageNotReady) {
				t.Fatalf("open before the image: %v", err)
			}
			st := waitImage(t, m)
			if st.State != tc.state {
				t.Fatalf("state %s, want %s (%s)", st.State, tc.state, st.Error)
			}
			if tc.state == "ready" {
				dir := filepath.Join(m.MountRoot, "data/.titanos/viewer/image")
				if entries, _ := os.ReadDir(dir); len(entries) != 0 {
					t.Errorf("archive kept after loading: %v", entries)
				}
				if _, err := m.Open(t.Context(), "anna", "immich"); err != nil {
					t.Fatalf("open with the image: %v", err)
				}
			}
			if len(engine.created) != 0 && tc.state != "ready" {
				t.Fatal("container without image")
			}
		})
	}
}

func TestOlderImagesAreRemoved(t *testing.T) {
	engine := newFakeEngine()
	old, used := "sha256:"+strings.Repeat("1", 64), "sha256:"+strings.Repeat("2", 64)
	engine.imageList = []docker.ViewerImage{{ID: old, Tags: []string{"titanos-viewer:0.59.9"}}, {ID: used}, {ID: testImageID}}
	engine.inUse = map[string]bool{used: true}
	m := testManager(t, engine)
	m.image = imageState{}
	m.Release = fakeRelease{manifest: manifest(goodEntry), body: "image"}
	m.readyImage()
	if st := waitImage(t, m); st.State != "ready" {
		t.Fatalf("state %+v", st)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		engine.mu.Lock()
		removed := slices.Clone(engine.removedIm)
		engine.mu.Unlock()
		if len(removed) > 0 {
			// Only the old one; the new image and one in use stay.
			if !slices.Equal(removed, []string{old}) {
				t.Fatalf("removed %v", removed)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("older image not removed")
}

func TestImageAlreadyLoadedIsNotDownloaded(t *testing.T) {
	engine := newFakeEngine()
	engine.images[testImageID] = testImageID
	m := testManager(t, engine)
	m.image = imageState{}
	m.Release = fakeRelease{manifest: manifest(goodEntry), downloadErr: errors.New("must not download")}
	m.readyImage()
	if st := waitImage(t, m); st.State != "ready" || engine.loaded != 0 {
		t.Fatalf("state %+v, loads %d", st, engine.loaded)
	}
}

func TestLostImageIsFetchedAgainAndErrorsBackOff(t *testing.T) {
	engine := newFakeEngine()
	m := testManager(t, engine)
	delete(engine.images, testImageID)
	calls := 0
	m.Release = countingRelease{&calls, fakeRelease{manifestErr: errors.New("offline")}}
	if _, err := m.Open(t.Context(), "anna", "immich"); !errors.Is(err, ErrImageNotReady) {
		t.Fatalf("lost image: %v", err)
	}
	if st := waitImage(t, m); st.State != "error" {
		t.Fatalf("state %+v", st)
	}
	// Reconnecting windows do not ask the release again within a minute.
	for range 3 {
		m.Open(t.Context(), "anna", "immich")
	}
	if calls != 1 || len(engine.created) != 0 {
		t.Fatalf("manifest fetched %d times, %d containers", calls, len(engine.created))
	}
}

type countingRelease struct {
	calls *int
	fakeRelease
}

func (c countingRelease) ReleaseManifest(ctx context.Context, v string) ([]byte, error) {
	*c.calls++
	return c.fakeRelease.ReleaseManifest(ctx, v)
}

func TestContainerSpec(t *testing.T) {
	engine := newFakeEngine()
	m := testManager(t, engine)
	m.CPUs = 8
	m.Policy = func(origin string) ([]byte, error) { return []byte("origin=" + origin), nil }
	m.store.WriteJSON(settingsFile, Settings{GPU: "off", Quality: "auto", FPS: 60})
	m.Load()
	lease, err := m.Open(t.Context(), "anna", "immich")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close(false)
	if len(engine.created) != 1 {
		t.Fatalf("created %d", len(engine.created))
	}
	spec := engine.created[0]
	// Its own network, named like the container; the policy file from titand.
	if spec.Image != testImageID || spec.Network != spec.Name || spec.User != "anna" || spec.App != "immich" {
		t.Fatalf("spec: %+v", spec)
	}
	if spec.NanoCPUs != 2e9 || spec.CPUShares != 512 || spec.MemoryBytes != PerView {
		t.Fatalf("CPU/memory: %+v", spec)
	}
	if !slices.Equal(engine.networks, []string{spec.Name}) || !engine.attached[spec.Name+"|app1"] {
		t.Fatalf("network %v attached %v", engine.networks, engine.attached)
	}
	policy, err := os.ReadFile(spec.Policy)
	if err != nil || string(policy) != "origin=http://172.30.0.2:3001" {
		t.Fatalf("policy %q %v", policy, err)
	}
	if st, _ := os.Stat(spec.Policy); st.Mode().Perm() != 0o444 {
		t.Fatalf("policy mode %v", st.Mode())
	}
	if len(spec.Devices) != 0 || len(spec.CDIDevices) != 0 {
		t.Fatalf("GPU off but devices: %+v", spec)
	}
	if spec.MemoryBytes == 0 || spec.PidsLimit == 0 || spec.ShmBytes != 512<<20 {
		t.Fatalf("limits: %+v", spec)
	}
	env := strings.Join(spec.Env, "\n")
	// The container port behind the published one, on the app's address
	// in the internal network, with the path of the WebUI label.
	for _, want := range []string{"VIEWER_URL=http://172.30.0.2:3001/photos", "VIEWER_GPU=cpu", "VIEWER_AUDIO=0"} {
		if !strings.Contains(env, want) {
			t.Errorf("env lacks %s:\n%s", want, env)
		}
	}
	if !strings.Contains(env, "VIEWER_KEY=") || len(lease.s.secret) != 64 {
		t.Fatal("no session key")
	}
	profile := filepath.Join(m.MountRoot, "data/.titanos/viewer/profiles/anna/immich")
	if spec.Profile != profile {
		t.Fatalf("profile %s", spec.Profile)
	}
	for _, dir := range []string{"data/.titanos/viewer", "data/.titanos/viewer/profiles", "data/.titanos/viewer/profiles/anna"} {
		st, err := os.Stat(filepath.Join(m.MountRoot, dir))
		if err != nil || st.Mode().Perm() != 0o700 {
			t.Errorf("%s: %v %v", dir, st.Mode(), err)
		}
	}
	if lease.AgentURL() != "ws://172.30.0.5:7300/stream" || lease.Key() != lease.s.secret {
		t.Fatal("agent address")
	}
}

func TestContainerSpecWithGPU(t *testing.T) {
	engine := newFakeEngine()
	m := testManager(t, engine)
	m.Devices = func(context.Context) []hardware.ContainerDevice {
		return []hardware.ContainerDevice{
			{ID: "0000:00:02.0", Kind: "igpu", Vendor: "8086", Available: true, Mappings: []string{"/dev/dri/renderD128:/dev/dri/renderD128"}, Groups: []string{"105"}},
			{ID: "0000:01:00.0", Kind: "nvidia", Vendor: "10de", Available: true, Mappings: []string{"nvidia.com/gpu=GPU-1"}},
		}
	}
	lease, err := m.Open(t.Context(), "anna", "immich")
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close(false)
	spec := engine.created[0]
	if !slices.Equal(spec.Devices, []string{"/dev/dri/renderD128"}) || !slices.Equal(spec.GroupAdd, []string{"105"}) || lease.GPU() != "vaapi" {
		t.Fatalf("auto takes the iGPU: %+v", spec)
	}
	// A device with a VM is not used; auto falls to NVIDIA.
	m.Leased = func() map[string]bool { return map[string]bool{"0000:00:02.0": true} }
	m.gpus = nil
	if g := chooseGPU("auto", m.GPUs(t.Context())); g == nil || g.Kind != "nvenc" || g.cdi != "nvidia.com/gpu=GPU-1" {
		t.Fatalf("leased iGPU: %+v", g)
	}
	if g := chooseGPU("/dev/dri/renderD128", m.GPUs(t.Context())); g != nil {
		t.Fatal("chosen device with a VM used")
	}
}

func TestPickTarget(t *testing.T) {
	host := []docker.AppEndpoint{{ID: "h", State: "running", NetworkMode: "host", WebUI: "8123"}}
	if _, err := pickTarget(host, 0); !errors.Is(err, ErrHostNetwork) {
		t.Fatalf("host network: %v", err)
	}
	if _, err := pickTarget([]docker.AppEndpoint{{ID: "x", State: "running", NetworkMode: "bridge"}}, 0); !errors.Is(err, ErrNoWebUI) {
		t.Fatalf("no webui: %v", err)
	}
	stopped := []docker.AppEndpoint{{ID: "s", State: "exited", NetworkMode: "bridge", WebUI: "80", Ports: []docker.Port{{Host: 80, Container: 8080, Protocol: "tcp"}}}}
	if _, err := pickTarget(stopped, 0); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("stopped: %v", err)
	}
	// The port of the HTTPS name wins over the label, on whichever
	// container publishes it.
	two := []docker.AppEndpoint{
		{ID: "db", State: "running", NetworkMode: "bridge", WebUI: "5432", Ports: []docker.Port{{Host: 5432, Container: 5432, Protocol: "tcp"}}},
		{ID: "web", State: "running", NetworkMode: "bridge", Ports: []docker.Port{{Host: 8443, Container: 443, Protocol: "tcp"}}},
	}
	got, err := pickTarget(two, 8443)
	if err != nil || got.id != "web" || got.port != 443 || got.path != "/" {
		t.Fatalf("route port: %+v %v", got, err)
	}
	if p := webUIPath("http://[host]:8096/web/index.html"); p != "/web/index.html" {
		t.Fatalf("path %s", p)
	}
}

func TestMemoryRefusal(t *testing.T) {
	engine := newFakeEngine()
	m := testManager(t, engine)
	m.MemAvailable = func() (uint64, error) { return 900 << 20, nil }
	if _, err := m.Open(t.Context(), "anna", "immich"); !errors.Is(err, ErrMemory) {
		t.Fatalf("open with little memory: %v", err)
	}
	if len(engine.created) != 0 || len(engine.attached) != 0 {
		t.Fatal("something was started")
	}
	if est := m.MemoryEstimate(); est.More != 0 {
		t.Fatalf("estimate: %+v", est)
	}
	m.MemAvailable = func() (uint64, error) { return 700<<20 + 3*PerView, nil }
	if est := m.MemoryEstimate(); est.More != 3 {
		t.Fatalf("estimate: %+v", est)
	}
}

func TestLingerReplaceAndStop(t *testing.T) {
	engine := newFakeEngine()
	m := testManager(t, engine)
	m.Linger = 50 * time.Millisecond
	first, err := m.Open(t.Context(), "anna", "immich")
	if err != nil {
		t.Fatal(err)
	}
	// A second window takes over the same container.
	second, err := m.Open(t.Context(), "anna", "immich")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-first.Ended:
	default:
		t.Fatal("first window not told")
	}
	if !first.Replaced() || len(engine.created) != 1 {
		t.Fatalf("replaced %v, created %d", first.Replaced(), len(engine.created))
	}
	first.Close(true)
	second.Close(true)
	// The container lingers, then goes with the app's network.
	engine.mu.Lock()
	early := len(engine.removed)
	engine.mu.Unlock()
	if early != 0 {
		t.Fatal("removed at once")
	}
	time.Sleep(200 * time.Millisecond)
	engine.mu.Lock()
	removed, detached := slices.Clone(engine.removed), slices.Clone(engine.detached)
	engine.mu.Unlock()
	if !slices.Equal(removed, []string{"v1"}) || !slices.Equal(detached, []string{"app1"}) {
		t.Fatalf("after linger: removed %v detached %v", removed, detached)
	}
	// Reopened within the linger time, the container is reused.
	m.Linger = time.Hour
	third, _ := m.Open(t.Context(), "anna", "immich")
	third.Close(true)
	again, _ := m.Open(t.Context(), "anna", "immich")
	if len(engine.created) != 2 {
		t.Fatalf("not reused: %d", len(engine.created))
	}
	// Two accounts on one app: the app leaves the network with the last.
	other, err := m.Open(t.Context(), "ben", "immich")
	if err != nil {
		t.Fatal(err)
	}
	// Each view has its own network: ben's keeps the app.
	again.Close(false)
	if len(engine.detached) != 2 || !engine.attached[m.ContainerName("ben", "immich")+"|app1"] {
		t.Fatalf("detached %v, attached %v", engine.detached, engine.attached)
	}
	other.Close(false)
	if len(engine.detached) != 3 || len(engine.attached) != 0 || len(engine.networks) != 0 {
		t.Fatalf("left: detached %v attached %v networks %v", engine.detached, engine.attached, engine.networks)
	}
}

func TestCleanupTouchesOnlyLabelledViewers(t *testing.T) {
	engine := newFakeEngine()
	// Viewers lists only containers with the label (docker/viewer.go).
	engine.viewers = []docker.ViewerContainer{{ID: "old1", Name: "titanos-viewer-aaaa"}}
	engine.networks = []string{"titanos-viewer-bbbb"}
	engine.attached["titanos-viewer-bbbb|app9"] = true
	m := testManager(t, engine)
	engine.listErr = errors.New("docker not running")
	if err := m.cleanup(t.Context()); err == nil {
		t.Fatal("cleanup without Docker")
	}
	engine.listErr = nil
	if err := m.cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(engine.removed, []string{"old1"}) || !slices.Equal(engine.detached, []string{"app9"}) || !slices.Equal(engine.netRemoved, []string{"titanos-viewer-bbbb"}) {
		t.Fatalf("removed %v detached %v networks %v", engine.removed, engine.detached, engine.netRemoved)
	}
	// Once only: views opened later are never cleaned away.
	lease, _ := m.Open(t.Context(), "anna", "immich")
	m.cleanup(t.Context())
	if len(engine.removed) != 1 {
		t.Fatal("cleanup ran twice")
	}
	lease.Close(false)
}

func TestNamesAreSafe(t *testing.T) {
	m := testManager(t, newFakeEngine())
	if m.ContainerName("a-b", "c") == m.ContainerName("a", "b-c") {
		t.Fatal("names collide")
	}
	for _, pair := range [][2]string{{"../x", "immich"}, {"anna", "../x"}, {"anna", "Immich"}, {".", "immich"}} {
		if _, err := m.Open(t.Context(), pair[0], pair[1]); !errors.Is(err, ErrName) {
			t.Errorf("%v: %v", pair, err)
		}
	}
}

func TestParseMemAvailable(t *testing.T) {
	got, err := parseMemAvailable([]byte("MemTotal: 100 kB\nMemAvailable:    2048 kB\n"))
	if err != nil || got != 2048<<10 {
		t.Fatalf("%d %v", got, err)
	}
}
