package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// TestNVRRealPurge deletes the recordings of a removed camera with the real
// Frigate (PLAN 85-3, owner 13.11.2026): two cameras record, one is
// removed, then its recordings are deleted now. Its files must be gone from
// the volume (not only from Frigate's database), the other camera keeps
// all of its own, and Frigate runs on without the removed camera. Not
// part of the normal test run:
//
//	TITANOS_NVR_REAL=<empty folder> go test ./internal/api -run TestNVRRealPurge -v -timeout 30m
func TestNVRRealPurge(t *testing.T) {
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
	run("rm", "-f", "titanos-nvr-testsrc")
	source := `{"api":{"listen":""},"rtsp":{"listen":":8554"},"webrtc":{"listen":""},"streams":{"test":"exec:/usr/lib/ffmpeg/8.0/bin/ffmpeg -re -f lavfi -i testsrc2=size=640x360:rate=10 -c:v libx264 -preset ultrafast -tune zerolatency -pix_fmt yuv420p -g 20 -f rtsp {output}"}}`
	run("run", "-d", "--rm", "--name", "titanos-nvr-testsrc", "-p", "18554:8554", "--entrypoint", "/usr/local/go2rtc/bin/go2rtc", image, "-config", source)
	defer run("rm", "-f", "titanos-nvr-testsrc")
	gateway := run("network", "inspect", "bridge", "-f", "{{(index .IPAM.Config 0).Gateway}}")
	stream := "rtsp://" + gateway + ":18554/test"

	var manager *nvr.Manager
	ts := newTestServerWith(t, func(opts *Options) {
		manager = nvr.New(opts.Store)
		manager.Engine = realNVREngine{docker.NewManager(opts.Store, "")}
		manager.MountRoot = root
		manager.Mounted = func() []string { return []string{"tank"} }
		manager.Tick = 2 * time.Second
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

	if res, body := ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"enabled": true, "volume": "tank", "detector": map[string]any{"kind": "cpu"}}); res.StatusCode != 200 {
		t.Fatalf("enable: %d %s", res.StatusCode, body)
	}
	until("Frigate runs", 25*time.Minute, func() bool { return status().State == nvr.PhaseRunning })
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

	for _, name := range []string{"Weg", "Bleibt"} {
		res, body := ts.request("POST", "/api/v1/nvr/cameras", map[string]any{
			"name": name, "source": map[string]any{"kind": "rtsp", "mainURL": stream},
			"record": map[string]any{"mode": "continuous", "retainDays": 1, "preSeconds": 5, "postSeconds": 5},
		})
		if res.StatusCode != 201 {
			t.Fatalf("add camera %s: %d %s", name, res.StatusCode, body)
		}
	}
	// The files of a camera on the volume, as Frigate lays them out
	// (recordings/<day>/<hour>/<camera>/<mm.ss>.mp4).
	files := func(camera string) int {
		out := run("exec", "titanos-nvr", "sh", "-c", "find /media/frigate/recordings -path '*/"+camera+"/*' -type f | wc -l")
		n, err := strconv.Atoi(strings.TrimSpace(out))
		if err != nil {
			t.Fatalf("counting the files of %s: %q", camera, out)
		}
		return n
	}
	until("both cameras have recorded files", 10*time.Minute, func() bool {
		return status().State == nvr.PhaseRunning && files("weg") >= 3 && files("bleibt") >= 3
	})

	if res, body := ts.request("DELETE", "/api/v1/nvr/cameras/weg", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("remove camera: %d %s", res.StatusCode, body)
	}
	time.Sleep(5 * time.Second) // the restart begins
	until("Frigate runs with the removed camera", 5*time.Minute, func() bool { return status().State == nvr.PhaseRunning })
	kept := files("weg")
	keptOther := files("bleibt")
	t.Logf("files before deleting: weg %d, bleibt %d", kept, keptOther)
	if kept == 0 {
		t.Fatal("the removed camera kept no files: nothing to test")
	}

	started := time.Now()
	if res, body := ts.request("DELETE", "/api/v1/nvr/cameras/weg/recordings", nil); res.StatusCode != http.StatusAccepted {
		t.Fatalf("delete the removed camera's recordings: %d %s", res.StatusCode, body)
	}
	// It runs in the background; the camera is listed until it is done.
	until("the removed camera is gone", 5*time.Minute, func() bool {
		var list struct {
			Cameras []nvr.CameraView `json:"cameras"`
		}
		getJSON(t, ts, ts.client, "/nvr/cameras", &list)
		for _, c := range list.Cameras {
			if c.ID == "weg" {
				return false
			}
		}
		return true
	})
	t.Logf("deleted in %v", time.Since(started).Round(time.Millisecond))
	if n := files("weg"); n != 0 {
		t.Errorf("files of the removed camera left on the volume: %d", n)
	}
	if n := files("bleibt"); n < keptOther {
		t.Errorf("the other camera lost files: %d, before %d", n, keptOther)
	}
	var list struct {
		Cameras []nvr.CameraView `json:"cameras"`
	}
	getJSON(t, ts, ts.client, "/nvr/cameras", &list)
	for _, c := range list.Cameras {
		if c.ID == "weg" {
			t.Errorf("still listed: %+v", c)
		}
	}
	if res, _ := ts.request("GET", "/api/v1/nvr/cameras/weg/recordings/summary", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("summary of the purged camera: %d", res.StatusCode)
	}
	// Once more: nothing left.
	if res, body := ts.request("DELETE", "/api/v1/nvr/cameras/weg/recordings", nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("second purge: %d %s", res.StatusCode, body)
	}
	// Frigate restarts once without the stub and runs on with the other camera.
	time.Sleep(5 * time.Second)
	until("Frigate runs again", 5*time.Minute, func() bool { return status().State == nvr.PhaseRunning })
	var cfg struct {
		Cameras map[string]json.RawMessage `json:"cameras"`
	}
	if code, body := frigate("/api/config"); code != 200 || json.Unmarshal(body, &cfg) != nil {
		t.Fatalf("Frigate's config: %d", code)
	}
	if _, ok := cfg.Cameras["weg"]; ok {
		t.Error("Frigate still runs the removed camera")
	}
	if _, ok := cfg.Cameras["bleibt"]; !ok {
		t.Error("Frigate lost the other camera")
	}
	until("the other camera records on", 3*time.Minute, func() bool { return files("bleibt") > keptOther })
	if n := files("weg"); n != 0 {
		t.Errorf("files of the removed camera came back: %d", n)
	}
}
