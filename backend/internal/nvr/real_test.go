package nvr

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/docker"
)

// realEngine is the Docker of this machine, whatever TitanOS's own
// settings say.
type realEngine struct{ *docker.Manager }

func (realEngine) EngineReady() bool { return true }

// TestRealFrigate runs the manager against a real Docker engine and the
// real image: pull (if it is not there), config, container, health, a
// camera added while it runs, and removal. It is not part of the normal
// test run (the image is 1.5 GB to download and it starts a container
// named titanos-nvr on this machine):
//
//	TITANOS_NVR_REAL=<empty folder> go test ./internal/nvr -run TestRealFrigate -v -timeout 30m
//
// The folder stands in for /mnt and gets a "volume" named tank; Frigate
// writes into it as root.
func TestRealFrigate(t *testing.T) {
	root := os.Getenv("TITANOS_NVR_REAL")
	if root == "" {
		t.Skip("set TITANOS_NVR_REAL to a folder to run Frigate for real")
	}
	if err := os.MkdirAll(filepath.Join(root, "tank"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := config.Open(t.TempDir())
	m := New(store)
	m.Engine = realEngine{docker.NewManager(store, store.Dir())}
	m.MountRoot = root
	m.Mounted = func() []string { return []string{"tank"} }
	m.Tick = 2 * time.Second
	var notes []string
	m.Notify = func(severity, kind, subject string) { notes = append(notes, severity+" "+kind+" "+subject) }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() {
		// Whatever happened: no container stays behind.
		_ = m.Disable(context.Background())
		waitFor(t, m, 2*time.Minute, func(s Status) bool { return s.State == PhaseOff })
		cancel()
		<-done
		if st, err := m.Engine.NVRInspect(context.Background()); err != nil || st.Exists {
			t.Errorf("the container stays: %+v %v", st, err)
		}
	}()

	s := Defaults()
	s.Enabled, s.Volume, s.Detector.Kind = true, "tank", DetectorCPU
	if err := m.SetSettings(context.Background(), s, SetOptions{}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, m, 25*time.Minute, func(s Status) bool {
		if s.State == PhaseStopped || s.Image.State == "error" {
			t.Fatalf("the NVR gave up: %+v image %+v", s.Error, s.Image)
		}
		return s.State == PhaseRunning
	})
	d := m.Diagnosis(context.Background())
	t.Logf("Frigate %s runs: detector %s, %v ms, pid %v, shm %v MB, %d log lines (%s)", d.RunningVersion, d.Detector.Active, deref(d.Detector.InferenceMs), d.DetectorPID, deref(d.Shm.TotalMB), len(d.Log), d.LogSource)
	if !strings.HasPrefix(d.RunningVersion, FrigateVersion) {
		t.Errorf("Frigate says it is %q, pinned is %s", d.RunningVersion, FrigateVersion)
	}
	if d.Detector.Active != DetectorCPU || d.DetectorPID == nil || d.LogSource != "frigate" || len(d.Log) == 0 || d.Shm.TotalMB == nil || *d.Shm.TotalMB < 255 {
		t.Errorf("diagnosis: %+v", d)
	}
	st, _ := m.Engine.NVRInspect(context.Background())
	t.Logf("container: %+v", st)

	// A camera that does not answer: Frigate must take the config all the
	// same (a refused one would start it in safe mode and stop the NVR).
	c := NewCamera()
	c.Name = "Haustür"
	c.Source = Source{Kind: "onvif", Host: "192.168.250.250", ONVIFPort: 8000, User: "admin", Password: "p@ss/w:rd {x}",
		MainURL: "rtsp://192.168.250.250:554/main", SubURL: "rtsp://192.168.250.250:554/sub"}
	// Plates and faces are left out here: Frigate would download their
	// models. The validator of Frigate took a config with both (render_test).
	c.PTZ, c.Audio = true, Audio{Enabled: true, TwoWay: true}
	if _, err := m.AddCamera(context.Background(), c, CameraOptions{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * m.Tick)
	waitFor(t, m, 10*time.Minute, func(s Status) bool {
		if s.State == PhaseStopped {
			t.Fatalf("the NVR stopped with a camera: %+v", s.Error)
		}
		states := m.CameraStates()
		return s.State == PhaseRunning && states["haustuer"].Known
	})
	d = m.Diagnosis(context.Background())
	t.Logf("with a camera: state %s, cameras %+v, last error %+v", d.State, d.Cameras, d.LastError)
	for _, line := range d.Log {
		if strings.Contains(line, "p@ss") || strings.Contains(line, "p%40ss") {
			t.Errorf("the camera's password is in the log shown: %s", line)
		}
	}
	if len(notes) != 0 {
		t.Errorf("messages: %v", notes)
	}
}

func deref(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

func waitFor(t *testing.T, m *Manager, limit time.Duration, ok func(Status) bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	last := ""
	for {
		s := m.Status(context.Background())
		if line := s.State + " " + s.Image.State; line != last {
			t.Logf("%s: state %s, image %s (%d of %d MB)", time.Now().Format("15:04:05"), s.State, s.Image.State, s.Image.Bytes>>20, s.Image.Size>>20)
			last = line
		}
		if ok(s) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("not reached within %v: %+v", limit, s)
		}
		time.Sleep(time.Second)
	}
}
