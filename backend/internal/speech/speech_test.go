package speech

import (
	"bufio"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
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

func TestSettingsCheck(t *testing.T) {
	s := Settings{}
	if err := s.Check(); err != nil || s.Device != DeviceCPU || s.Language != "auto" || s.Speed != 1 || s.Reach != ReachHA {
		t.Fatalf("defaults: %v %+v", err, s)
	}
	for name, bad := range map[string]Settings{
		"model":    {WhisperModel: "riesig"},
		"language": {Language: "deutsch"},
		"voice":    {Voice: "../../etc/passwd"},
		"voice2":   {Voice: "de_DE-thorsten"},
		"speed":    {Speed: 3},
		"reach":    {Reach: "internet"},
		"device":   {Device: "nvidia.com/gpu=all; rm"},
		"volume":   {Volume: "../x"},
	} {
		if err := bad.Check(); err == nil {
			t.Errorf("%s accepted: %+v", name, bad)
		}
	}
	ok := Settings{Device: "0000:01:00.0", WhisperModel: "large-v3-turbo", Language: "de", Voice: "de_DE-thorsten-high", Speed: 1.2, Reach: ReachLAN, Volume: "Data"}
	if err := ok.Check(); err != nil {
		t.Fatalf("good settings: %v", err)
	}
}

const voicesSample = `{
 "de_DE-thorsten-high": {"key":"de_DE-thorsten-high","name":"thorsten","language":{"code":"de_DE","name_native":"Deutsch","country_english":"Germany"},"quality":"high","num_speakers":1,
  "files":{"de/de_DE/thorsten/high/de_DE-thorsten-high.onnx":{"size_bytes":11,"md5_digest":"%s"},
           "de/de_DE/thorsten/high/de_DE-thorsten-high.onnx.json":{"size_bytes":2,"md5_digest":"%s"},
           "de/de_DE/thorsten/high/MODEL_CARD":{"size_bytes":5,"md5_digest":"00000000000000000000000000000000"}}},
 "en_US-amy-medium": {"name":"amy","language":{"code":"en_US","name_native":"English","country_english":"United States"},"quality":"medium","num_speakers":1,
  "files":{"en/en_US/amy/medium/en_US-amy-medium.onnx":{"size_bytes":3,"md5_digest":"11111111111111111111111111111111"}}},
 "../evil": {"name":"x","language":{"code":"xx_XX"},"quality":"low","files":{}}
}`

func sum(kind string, data string) string {
	if kind == "md5" {
		h := md5.Sum([]byte(data))
		return hex.EncodeToString(h[:])
	}
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:])
}

func TestParseVoices(t *testing.T) {
	list, err := parseVoices([]byte(fmt.Sprintf(voicesSample, sum("md5", "onnx-binary"), sum("md5", "{}"))))
	if err != nil || len(list) != 1 {
		t.Fatalf("voices: %v %+v", err, list)
	}
	v := list[0]
	if v.ID != "de_DE-thorsten-high" || v.LanguageName != "Deutsch" || v.Quality != "high" || v.Bytes != 13 || len(v.remote()) != 2 {
		t.Fatalf("voice: %+v", v)
	}
	if _, err := parseVoices([]byte(`{}`)); err == nil {
		t.Fatal("an empty catalog is accepted")
	}
}

// fakeHub serves a model repository and the voices like Hugging Face.
func fakeHub(t *testing.T, model string) *hub {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/models/Systran/faster-whisper-tiny":
			fmt.Fprintf(w, `{"siblings":[{"rfilename":"README.md","size":9},{"rfilename":"config.json","size":2},
				{"rfilename":"model.bin","size":%d,"lfs":{"sha256":"%s"}},{"rfilename":"tokenizer.json","size":2},{"rfilename":"vocabulary.txt","size":3},
				{"rfilename":"../../escape","size":4}]}`, len("the-model"), sum("sha", "the-model"))
		case "/Systran/faster-whisper-tiny/resolve/main/model.bin":
			w.Write([]byte(model))
		case "/Systran/faster-whisper-tiny/resolve/main/config.json", "/Systran/faster-whisper-tiny/resolve/main/tokenizer.json":
			w.Write([]byte("{}"))
		case "/Systran/faster-whisper-tiny/resolve/main/vocabulary.txt":
			w.Write([]byte("a b"))
		case "/" + voicesIndex:
			fmt.Fprintf(w, voicesSample, sum("md5", "onnx-binary"), sum("md5", "{}"))
		case "/rhasspy/piper-voices/resolve/main/de/de_DE/thorsten/high/de_DE-thorsten-high.onnx":
			w.Write([]byte("onnx-binary"))
		case "/rhasspy/piper-voices/resolve/main/de/de_DE/thorsten/high/de_DE-thorsten-high.onnx.json":
			w.Write([]byte("{}"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &hub{base: srv.URL, client: srv.Client()}
}

// testManager has a volume "Data" in a temporary folder.
func testManager(t *testing.T, h *hub) *Manager {
	t.Helper()
	mnt := t.TempDir()
	if err := os.Mkdir(filepath.Join(mnt, "Data"), 0755); err != nil {
		t.Fatal(err)
	}
	m := New(config.Open(t.TempDir()))
	m.MountRoot, m.hub = mnt, h
	m.Mounted = func() []string { return []string{"Data"} }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m
}

func waitJobs(t *testing.T, m *Manager) map[string]job {
	t.Helper()
	for i := 0; i < 200; i++ {
		jobs := m.Jobs()
		busy := false
		for _, j := range jobs {
			if j.Error == "" {
				busy = true
			}
		}
		if !busy {
			return jobs
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("download did not end")
	return nil
}

func TestWyomingRoundTrip(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				r := bufio.NewReader(conn)
				var pcm []byte
				for {
					ev, err := readEvent(r)
					if err != nil {
						return
					}
					switch ev.Type {
					case "synthesize":
						var req struct {
							Text  string
							Voice struct{ Name string }
						}
						json.Unmarshal(ev.Data, &req)
						writeEvent(conn, "audio-start", map[string]int{"rate": 16000, "width": 2, "channels": 1}, nil)
						writeEvent(conn, "audio-chunk", map[string]int{"rate": 16000, "width": 2, "channels": 1}, []byte(req.Voice.Name+":"+req.Text+"!"))
						writeEvent(conn, "audio-stop", nil, nil)
						return
					case "audio-chunk":
						pcm = append(pcm, ev.Payload...)
					case "audio-stop":
						writeEvent(conn, "transcript", map[string]string{"text": " " + string(pcm) + " "}, nil)
						return
					}
				}
			}()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	audio, err := Synthesize(ctx, l.Addr().String(), "Hallo", "de_DE-thorsten-high")
	if err != nil || audio.Rate != 16000 || audio.Width != 2 || audio.Channels != 1 || string(audio.PCM) != "de_DE-thorsten-high:Hallo!" {
		t.Fatalf("synthesize: %v %+v", err, audio)
	}
	big := Audio{PCM: []byte(strings.Repeat("ab", 10000)), Rate: 16000, Width: 2, Channels: 1}
	text, err := Transcribe(ctx, l.Addr().String(), big, "de")
	if err != nil || strings.TrimSpace(text) != string(big.PCM) {
		t.Fatalf("transcribe: %v %d", err, len(text))
	}
	// WAV there and back.
	back, err := ParseWAV(big.WAV())
	if err != nil || back.Rate != 16000 || back.Width != 2 || back.Channels != 1 || string(back.PCM) != string(big.PCM) {
		t.Fatalf("wav: %v %+v", err, back.Rate)
	}
	for _, bad := range [][]byte{nil, []byte("RIFFxxxxWAVE"), []byte("not a wav file at all")} {
		if _, err := ParseWAV(bad); err == nil {
			t.Fatalf("%q accepted as WAV", bad)
		}
	}
}

func TestHomeNetwork(t *testing.T) {
	for addr, want := range map[string]bool{
		"127.0.0.1": true, "192.168.10.40": true, "10.1.2.3": true, "172.17.0.2": true, "100.101.102.103": true,
		"fd00::1": true, "fe80::1": true, "8.8.8.8": false, "2001:db8::1": false,
	} {
		if got := homeNetwork(net.ParseIP(addr)); got != want {
			t.Errorf("%s: %v", addr, got)
		}
	}
}

// fakeEngine is Docker for the manager.
type fakeEngine struct {
	mu      sync.Mutex
	images  map[string]string
	boxes   map[string]docker.SpeechSpec
	running map[string]bool
	pulled  []string
	removed []string
}

func (f *fakeEngine) EngineReady() bool { return true }
func (f *fakeEngine) SpeechInspect(_ context.Context, name string) (docker.SpeechState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	spec, ok := f.boxes[name]
	if !ok {
		return docker.SpeechState{}, nil
	}
	return docker.SpeechState{Exists: true, ID: name, Running: f.running[name], Address: "172.30.0.2", Hash: spec.Hash, Image: spec.Image, StartedAt: time.Now()}, nil
}
func (f *fakeEngine) SpeechCreate(_ context.Context, spec docker.SpeechSpec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.boxes[spec.Name], f.running[spec.Name] = spec, true
	return nil
}
func (f *fakeEngine) SpeechStart(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.running[name] = true
	return nil
}
func (f *fakeEngine) SpeechRemove(_ context.Context, name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.boxes[name]; ok {
		f.removed = append(f.removed, name)
	}
	delete(f.boxes, name)
	delete(f.running, name)
	return nil
}
func (f *fakeEngine) SpeechNetworkEnsure(context.Context) error { return nil }
func (f *fakeEngine) SpeechLogs(context.Context, string, int) (string, error) {
	return "ValueError: no model", nil
}
func (f *fakeEngine) NVRImageID(_ context.Context, ref string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.images[ref], nil
}
func (f *fakeEngine) NVRPull(_ context.Context, repository, digest string, progress func(done, total int64)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	progress(5, 10)
	f.pulled = append(f.pulled, repository+"@"+digest)
	f.images[repository+"@"+digest] = "sha256:" + strings.Repeat("a", 64)
	return nil
}

func TestManagerDownloadsAndServices(t *testing.T) {
	m := testManager(t, fakeHub(t, "the-model"))
	if _, err := os.Stat(filepath.Join(m.MountRoot, "Data")); err != nil {
		t.Fatal(err)
	}
	engine := &fakeEngine{images: map[string]string{}, boxes: map[string]docker.SpeechSpec{}, running: map[string]bool{}}
	m.Engine = engine
	m.Devices = func(context.Context) []hardware.ContainerDevice {
		return []hardware.ContainerDevice{{ID: "0000:01:00.0", Kind: "nvidia", Available: true, Mappings: []string{"nvidia.com/gpu=GPU-1234"}}}
	}
	ctx := context.Background()
	// Switching on needs a volume.
	if err := m.SetSettings(Settings{Enabled: true}); err == nil {
		t.Fatal("switched on without a volume")
	}
	if err := m.SetSettings(Settings{Volume: "Data"}); err != nil {
		t.Fatal(err)
	}

	// A voice and a model are downloaded with their checksums.
	if err := m.Download(ctx, "voice", "de_DE-thorsten-high"); err != nil {
		t.Fatal(err)
	}
	if err := m.Download(ctx, "model", "tiny"); err != nil {
		t.Fatal(err)
	}
	if jobs := waitJobs(t, m); len(jobs) != 0 {
		t.Fatalf("jobs left: %+v", jobs)
	}
	models, voices := m.Installed()
	if !slices.Equal(models, []string{"tiny"}) || !slices.Equal(voices, []string{"de_DE-thorsten-high"}) {
		t.Fatalf("installed: %v %v", models, voices)
	}
	if data, _ := os.ReadFile(m.hostPath(whisperDir, "tiny", "model.bin")); string(data) != "the-model" {
		t.Fatalf("model file: %q", data)
	}
	if _, err := os.Stat(m.hostPath(whisperDir, "tiny", "README.md")); err == nil {
		t.Fatal("a file outside the list was taken")
	}
	for _, id := range []string{"gibtsnicht", "../x"} {
		if err := m.Download(ctx, "model", id); err != ErrUnknown {
			t.Fatalf("model %s: %v", id, err)
		}
		if err := m.Download(ctx, "voice", id); err != ErrUnknown {
			t.Fatalf("voice %s: %v", id, err)
		}
	}

	// Switched on with both chosen: images pulled, containers made.
	s := Settings{Enabled: true, Volume: "Data", WhisperModel: "tiny", Language: "de", Voice: "de_DE-thorsten-high", Speed: 1.25}
	if err := m.SetSettings(s); err != nil {
		t.Fatal(err)
	}
	defer m.whisper.close()
	defer m.piper.close()
	for i := 0; i < 100 && m.Status().Piper.Phase != "running"; i++ {
		m.tick(ctx)
		time.Sleep(10 * time.Millisecond)
	}
	st := m.Status()
	if st.Whisper.Phase != "running" || st.Piper.Phase != "running" {
		t.Fatalf("status: %+v", st)
	}
	w, p := engine.boxes[whisperService], engine.boxes[piperService]
	if !slices.Contains(w.Env, "WHISPER_MODEL=/config/tiny") || !slices.Contains(w.Env, "WHISPER_LANG=de") || !slices.Contains(w.Env, "LOCAL_ONLY=1") || len(w.CDIDevices) != 0 {
		t.Fatalf("whisper: %+v", w)
	}
	if !slices.Contains(p.Env, "PIPER_VOICE=de_DE-thorsten-high") || !slices.Contains(p.Env, "PIPER_LENGTH=0.80") || !slices.Contains(p.Env, "LOCAL_ONLY=1") {
		t.Fatalf("piper: %+v", p)
	}
	if !slices.Contains(engine.pulled, WhisperRepository+"@"+WhisperDigest) || slices.Contains(engine.pulled, WhisperRepository+"@"+WhisperGPUDigest) {
		t.Fatalf("pulled: %v", engine.pulled)
	}
	if addr, err := m.Address("whisper"); err != nil || addr != "172.30.0.2:10300" {
		t.Fatalf("address: %v %s", err, addr)
	}

	// The GPU: its CDI name and the CUDA image; the container is replaced.
	s.Device = "0000:01:00.0"
	m.SetSettings(s)
	for i := 0; i < 100 && len(engine.boxes[whisperService].CDIDevices) == 0; i++ {
		m.tick(ctx)
		time.Sleep(10 * time.Millisecond)
	}
	if got := engine.boxes[whisperService]; !slices.Equal(got.CDIDevices, []string{"nvidia.com/gpu=GPU-1234"}) || !slices.Contains(engine.pulled, WhisperRepository+"@"+WhisperGPUDigest) {
		t.Fatalf("gpu: %+v %v", got, engine.pulled)
	}
	// A GPU that is gone is said, nothing runs on something else silently.
	s.Device = "0000:09:00.0"
	m.SetSettings(s)
	m.tick(ctx)
	if st := m.Status().Whisper; st.Phase != "error" || st.Message != "device" {
		t.Fatalf("missing gpu: %+v", st)
	}

	// The active model stays; others go.
	if err := m.Remove("model", "tiny"); err != ErrActive {
		t.Fatalf("remove active: %v", err)
	}
	// Without a model the service is removed and says so.
	s.Device, s.WhisperModel = DeviceCPU, ""
	m.SetSettings(s)
	m.tick(ctx)
	if st := m.Status().Whisper; st.Phase != "model" {
		t.Fatalf("no model: %+v", st)
	}
	if err := m.Remove("model", "tiny"); err != nil {
		t.Fatal(err)
	}
	// Switched off: both containers go, the ports close.
	s.Enabled = false
	m.SetSettings(s)
	m.tick(ctx)
	if len(engine.boxes) != 0 || m.Status().Piper.Phase != "off" || m.Status().Piper.Listening {
		t.Fatalf("after off: %v %+v", engine.boxes, m.Status())
	}
}

func TestDownloadRefusesWrongChecksum(t *testing.T) {
	m := testManager(t, fakeHub(t, "another-model-of-the-same-size"[:9]))
	if err := m.SetSettings(Settings{Volume: "Data"}); err != nil {
		t.Fatal(err)
	}
	if err := m.Download(context.Background(), "model", "tiny"); err != nil {
		t.Fatal(err)
	}
	jobs := waitJobs(t, m)
	if j, ok := jobs["model/tiny"]; !ok || !strings.Contains(j.Error, "checksum") {
		t.Fatalf("jobs: %+v", jobs)
	}
	if models, _ := m.Installed(); len(models) != 0 {
		t.Fatalf("a model with a wrong checksum is installed: %v", models)
	}
	entries, _ := os.ReadDir(m.hostPath(whisperDir))
	if len(entries) != 0 {
		t.Fatalf("parts left behind: %v", entries)
	}
}
