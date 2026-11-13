package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// TestNVRRealStreams runs PLAN 82 for real: a camera whose streams are
// swapped (the small stream is recorded, the large one detects) with its
// streams kept open, and a second camera that is away when go2rtc starts
// and comes later: go2rtc never connects it again by itself, titand's
// watch asks it to (docker exec, curl in the image). Not part of the
// normal test run:
//
//	TITANOS_NVR_REAL=<empty folder> go test ./internal/api -run TestNVRRealStreams -v -timeout 30m
//
// It starts the containers titanos-nvr, titanos-nvr-testsrc and
// titanos-nvr-testlate and removes them again; Frigate writes into the
// folder as root.
func TestNVRRealStreams(t *testing.T) {
	root := os.Getenv("TITANOS_NVR_REAL")
	if root == "" {
		t.Skip("set TITANOS_NVR_REAL to a folder to run Frigate for real")
	}
	if err := os.MkdirAll(filepath.Join(root, "tank"), 0o755); err != nil {
		t.Fatal(err)
	}
	image := nvr.FrigateRepository + "@" + nvr.FrigateDigest
	run := func(args ...string) string {
		out, err := exec.Command("docker", args...).CombinedOutput()
		if err != nil {
			t.Logf("docker %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	source := func(name string, port string, streams map[string]string) {
		config := map[string]any{"api": map[string]any{"listen": ""}, "rtsp": map[string]any{"listen": ":8554"}, "webrtc": map[string]any{"listen": ""}, "streams": map[string]string{}}
		for stream, size := range streams {
			config["streams"].(map[string]string)[stream] = "exec:/usr/lib/ffmpeg/8.0/bin/ffmpeg -re -f lavfi -i testsrc2=size=" + size + ":rate=10 -c:v libx264 -preset ultrafast -tune zerolatency -pix_fmt yuv420p -g 20 -f rtsp {output}"
		}
		data, _ := json.Marshal(config)
		run("rm", "-f", name)
		run("run", "-d", "--rm", "--name", name, "-p", port+":8554", "--entrypoint", "/usr/local/go2rtc/bin/go2rtc", image, "-config", string(data))
	}
	// The camera: a large and a small stream.
	source("titanos-nvr-testsrc", "18554", map[string]string{"large": "1280x720", "small": "640x360"})
	defer run("rm", "-f", "titanos-nvr-testsrc")
	run("rm", "-f", "titanos-nvr-testlate")
	defer run("rm", "-f", "titanos-nvr-testlate")
	gateway := run("network", "inspect", "bridge", "-f", "{{(index .IPAM.Config 0).Gateway}}")
	large, small := "rtsp://"+gateway+":18554/large", "rtsp://"+gateway+":18554/small"
	late := "rtsp://" + gateway + ":18555/late"

	var manager *nvr.Manager
	ts := newTestServerWith(t, func(opts *Options) {
		manager = nvr.New(opts.Store)
		manager.Engine = realNVREngine{docker.NewManager(opts.Store, "")}
		manager.MountRoot = root
		manager.Mounted = func() []string { return []string{"tank"} }
		manager.Tick = 2 * time.Second
		// The camera rights need the accounts (the recordings route asks).
		users := opts.Users
		manager.Access.Lookup = func(name string) (nvr.Account, bool) {
			user, ok := users.Get(name)
			return nvr.Account{Name: user.Name, Admin: user.Role.CanAdminister(), Disabled: user.Disabled, Created: user.Created}, ok
		}
		if err := manager.Load(); err != nil {
			t.Fatal(err)
		}
		opts.NVR = manager
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { manager.Run(ctx); close(done) }()
	ts.setup()
	status := func() nvr.Status {
		var st nvr.Status
		getJSON(t, ts, ts.client, "/nvr/status", &st)
		return st
	}
	until := func(what string, limit time.Duration, ok func() bool) {
		t.Helper()
		deadline := time.Now().Add(limit)
		for !ok() {
			if time.Now().After(deadline) {
				d := manager.Diagnosis(context.Background())
				t.Fatalf("%s: not within %v; state %s, last error %+v\n%s", what, limit, d.State, d.LastError, strings.Join(d.Log[max(0, len(d.Log)-30):], "\n"))
			}
			time.Sleep(2 * time.Second)
		}
		t.Logf("%s: %s", time.Now().Format("15:04:05"), what)
	}
	defer func() {
		ts.request("POST", "/api/v1/nvr/disable", nil)
		until("NVR off, container removed", 2*time.Minute, func() bool { return status().State == nvr.PhaseOff })
		cancel()
		<-done
		run("network", "rm", "titanos-nvr")
	}()

	// Both cameras before Frigate's first start: the second one's camera
	// is away when go2rtc connects.
	for _, c := range []map[string]any{
		{"name": "Tor", "source": map[string]any{"kind": "rtsp", "mainURL": large, "subURL": small},
			"streams": map[string]any{"record": "sub", "detect": "main", "live": "auto"},
			"record":  map[string]any{"mode": "continuous", "retainDays": 1, "preSeconds": 5, "postSeconds": 5}},
		{"name": "Spaet", "source": map[string]any{"kind": "rtsp", "mainURL": late},
			"record": map[string]any{"mode": "continuous", "retainDays": 1, "preSeconds": 5, "postSeconds": 5}},
	} {
		if res, body := ts.request("POST", "/api/v1/nvr/cameras", c); res.StatusCode != 201 {
			t.Fatalf("add camera: %d %s", res.StatusCode, body)
		}
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"enabled": true, "volume": "tank", "detector": map[string]any{"kind": "cpu"}, "decoder": "auto"}); res.StatusCode != 200 {
		t.Fatalf("enable: %d %s", res.StatusCode, body)
	}
	until("Frigate runs", 25*time.Minute, func() bool { return status().State == nvr.PhaseRunning })
	started := time.Now()
	st := status()
	t.Logf("decoder: %+v (GPUs: %d)", st.Decoder, len(st.Hardware.Decoders))
	cfg, _ := os.ReadFile(filepath.Join(root, "tank", ".titanos", "nvr", "config", "config.yml"))
	for _, line := range strings.Split(string(cfg), "\n") {
		if strings.Contains(line, "preload") || strings.Contains(line, "hwaccel") {
			t.Logf("config.yml: %s", line)
		}
	}

	// What Frigate made of the swapped streams: it took the file (no safe
	// mode, both cameras), the large stream detects, the small one records.
	secret := manager.Settings().ProxySecret
	address := run("inspect", "-f", `{{(index .NetworkSettings.Networks "titanos-nvr").IPAddress}}`, "titanos-nvr")
	frigate := func(path string) (int, []byte) {
		req, _ := http.NewRequest("GET", "http://"+address+":8971"+path, nil)
		req.Header.Set("X-Proxy-Secret", secret)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, []byte(err.Error())
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, data
	}
	var running struct {
		SafeMode bool `json:"safe_mode"`
		Cameras  map[string]struct {
			FFmpeg struct {
				Inputs []struct {
					Path  string   `json:"path"`
					Roles []string `json:"roles"`
				} `json:"inputs"`
			} `json:"ffmpeg"`
			Detect struct {
				Width  int `json:"width"`
				Height int `json:"height"`
			} `json:"detect"`
		} `json:"cameras"`
	}
	if code, body := frigate("/api/config"); code != 200 || json.Unmarshal(body, &running) != nil {
		t.Fatalf("Frigate's config: %d", code)
	}
	if running.SafeMode || len(running.Cameras) != 2 {
		t.Errorf("Frigate refused config.yml: safe mode %v, cameras %d", running.SafeMode, len(running.Cameras))
	}
	roles := []string{}
	for _, in := range running.Cameras["tor"].FFmpeg.Inputs {
		roles = append(roles, strings.TrimPrefix(in.Path, "rtsp://127.0.0.1:8554/")+" "+strings.Join(in.Roles, ","))
	}
	t.Logf("Frigate's inputs of tor: %v; detect %d×%d", roles, running.Cameras["tor"].Detect.Width, running.Cameras["tor"].Detect.Height)
	if !slices.Equal(roles, []string{"tor detect", "tor_sub record"}) {
		t.Errorf("inputs of tor: %v", roles)
	}
	until("camera tor online", 5*time.Minute, func() bool {
		var view nvr.CameraView
		getJSON(t, ts, ts.client, "/nvr/cameras/tor", &view)
		return view.State.Online && view.State.FPS > 0
	})

	// go2rtc's open streams, asked through titand's own exec (the real
	// Engine API) and through the docker command.
	open := func() map[string]json.RawMessage {
		ectx, ecancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer ecancel()
		out, code, err := manager.Engine.NVRExec(ectx, []string{"curl", "-s", "-m", "5", "http://127.0.0.1:1984/api/preload"}, 64<<10)
		var list map[string]json.RawMessage
		if err != nil || code != 0 || json.Unmarshal([]byte(out), &list) != nil {
			t.Errorf("NVRExec: exit %d, %v, %d bytes", code, err, len(out))
		}
		return list
	}
	keys := func(m map[string]json.RawMessage) []string {
		out := []string{}
		for k := range m {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}
	first := open()
	t.Logf("go2rtc keeps open: %v", keys(first))
	if _, ok := first["tor"]; !ok {
		t.Errorf("tor is not kept open: %v", keys(first))
	}
	if _, ok := first["tor_sub"]; !ok {
		t.Errorf("tor_sub is not kept open: %v", keys(first))
	}
	if _, ok := first["spaet"]; ok {
		t.Errorf("spaet is open though its camera is away")
	}
	direct := run("exec", "titanos-nvr", "curl", "-s", "-m", "5", "http://127.0.0.1:1984/api/preload")
	t.Logf("docker exec curl /api/preload: %d bytes, password in it: %v", len(direct), strings.Contains(direct, "@"))

	// The late camera comes; go2rtc does not try it again by itself, the
	// watch asks it within about two minutes after Frigate's start.
	source("titanos-nvr-testlate", "18555", map[string]string{"late": "640x360"})
	until("go2rtc keeps spaet open (titand asked it again)", 10*time.Minute, func() bool {
		_, ok := open()["spaet"]
		return ok
	})
	t.Logf("spaet open %v after Frigate's start", time.Since(started).Round(time.Second))
	until("camera spaet online", 5*time.Minute, func() bool {
		var view nvr.CameraView
		getJSON(t, ts, ts.client, "/nvr/cameras/spaet", &view)
		return view.State.Online
	})
	// The recording of tor is of its small stream.
	var segments struct {
		Segments []nvr.Segment `json:"segments"`
	}
	deadline := time.Now().Add(4 * time.Minute)
	for {
		now := time.Now().Unix()
		code := getJSON(t, ts, ts.client, "/nvr/cameras/tor/recordings?from="+itoa(now-3600)+"&to="+itoa(now), &segments)
		if code != 200 {
			t.Fatalf("recordings of tor: %d", code)
		}
		if len(segments.Segments) >= 2 {
			t.Logf("%s: recording segments of tor: %d", time.Now().Format("15:04:05"), len(segments.Segments))
			break
		}
		if time.Now().After(deadline) {
			// What Frigate has: its cache, the folders of the recordings,
			// what it says per camera, its log lines about tor.
			t.Logf("cache: %s", run("exec", "titanos-nvr", "ls", "-la", "/tmp/cache"))
			t.Logf("recordings: %s", run("exec", "titanos-nvr", "sh", "-c", "find /media/frigate/recordings -type f | head -20"))
			for _, path := range []string{"/api/tor/recordings/summary", "/api/spaet/recordings/summary", "/api/stats"} {
				code, body := frigate(path)
				t.Logf("%s: %d %s", path, code, body[:min(len(body), 1500)])
			}
			logs := run("logs", "--tail", "400", "titanos-nvr")
			for _, line := range strings.Split(logs, "\n") {
				if strings.Contains(line, "tor") || strings.Contains(line, "record") {
					t.Logf("log: %s", line)
				}
			}
			t.Fatal("no recording segment of tor within 4 minutes")
		}
		time.Sleep(3 * time.Second)
	}
	pids := run("stats", "--no-stream", "--format", "{{.PIDs}} {{.MemUsage}} {{.CPUPerc}}", "titanos-nvr")
	t.Logf("container with two cameras: %s", pids)
}
