package api

import (
	"bytes"
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

// TestNVRRealExtras runs PLAN 88 against the real Frigate 0.18.2: a camera
// with two zones, a motion and an object mask, events only in zones and
// days apart must run (no safe mode) with exactly those keys in Frigate's
// own config; "Behalten" must set Frigate's retain flag on a real event
// and save its video as an export of its span; un-keeping clears both;
// "gesehen" per account must leave Frigate's own review flag alone. The
// test picture shows nothing Frigate detects, so the event is a manual one
// (POST /api/events/<camera>/<label>/create). Not part of the normal run:
//
//	TITANOS_NVR_REAL=<empty folder> go test ./internal/api -run TestNVRRealExtras -v -timeout 40m
func TestNVRRealExtras(t *testing.T) {
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
	frigate := func(method, path string, body any) (int, []byte) {
		var reader io.Reader
		if body != nil {
			data, _ := json.Marshal(body)
			reader = bytes.NewReader(data)
		}
		req, _ := http.NewRequest(method, "http://"+address+":8971"+path, reader)
		req.Header.Set("X-Proxy-Secret", secret)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, []byte(err.Error())
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, data
	}

	square := func(x, y, w, h float64) [][]float64 {
		return [][]float64{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}
	}
	res, body := ts.request("POST", "/api/v1/nvr/cameras", map[string]any{
		"name": "Hof", "source": map[string]any{"kind": "rtsp", "mainURL": stream},
		"detect": map[string]any{"enabled": true, "objects": []string{"person"}},
		"record": map[string]any{"mode": "continuous", "retainDays": 2, "recordDays": 1, "eventDays": 2, "preSeconds": 5, "postSeconds": 5},
	})
	if res.StatusCode != 201 {
		t.Fatalf("add camera: %d %s", res.StatusCode, body)
	}
	// Zones and masks need a picture to draw on: added once it records.
	files := func() int {
		out := run("exec", "titanos-nvr", "sh", "-c", "find /media/frigate/recordings -path '*/hof/*' -type f | wc -l")
		n, _ := strconv.Atoi(strings.TrimSpace(out))
		return n
	}
	until("the camera records", 10*time.Minute, func() bool { return status().State == nvr.PhaseRunning && files() >= 2 })
	res, body = ts.request("PUT", "/api/v1/nvr/cameras/hof", map[string]any{
		"zones": []map[string]any{
			{"name": "Weg", "points": [][]float64{{0, 0.5}, {0.6, 0.45}, {1, 1}, {0, 1}}},
			{"name": "Beet", "points": square(0.7, 0.1, 0.25, 0.3)},
		},
		"zonesOnly": true,
		"masks":     map[string]any{"motion": []map[string]any{{"points": square(0, 0, 0.3, 0.06)}}, "objects": []map[string]any{{"points": square(0.5, 0, 0.5, 0.2)}}},
	})
	if res.StatusCode != 200 {
		t.Fatalf("zones: %d %s", res.StatusCode, body)
	}
	time.Sleep(5 * time.Second) // the restart begins
	until("Frigate runs with zones and masks", 5*time.Minute, func() bool { return status().State == nvr.PhaseRunning })
	var cfg struct {
		SafeMode bool `json:"safe_mode"`
		Cameras  map[string]struct {
			Zones  map[string]struct{ Coordinates any } `json:"zones"`
			Motion struct {
				Mask map[string]struct{ Coordinates any } `json:"mask"`
			} `json:"motion"`
			Objects struct {
				Mask map[string]struct{ Coordinates any } `json:"mask"`
			} `json:"objects"`
			Review struct {
				Alerts struct {
					RequiredZones []string `json:"required_zones"`
				} `json:"alerts"`
			} `json:"review"`
			Record struct {
				Continuous struct{ Days float64 } `json:"continuous"`
				Alerts     struct {
					Retain struct{ Days float64 } `json:"retain"`
				} `json:"alerts"`
			} `json:"record"`
		} `json:"cameras"`
	}
	if code, data := frigate("GET", "/api/config", nil); code != 200 || json.Unmarshal(data, &cfg) != nil {
		t.Fatalf("Frigate's config: %d", code)
	}
	hof := cfg.Cameras["hof"]
	t.Logf("Frigate: zones %v, motion masks %v, object masks %v, required %v, days %v/%v",
		hof.Zones, hof.Motion.Mask, hof.Objects.Mask, hof.Review.Alerts.RequiredZones, hof.Record.Continuous.Days, hof.Record.Alerts.Retain.Days)
	if cfg.SafeMode || len(hof.Zones) != 2 || hof.Zones["z-weg"].Coordinates == nil || len(hof.Motion.Mask) != 1 || len(hof.Objects.Mask) != 1 ||
		len(hof.Review.Alerts.RequiredZones) != 2 || hof.Record.Continuous.Days != 1 || hof.Record.Alerts.Retain.Days != 2 {
		t.Errorf("Frigate runs another config: safe mode %v, %+v", cfg.SafeMode, hof)
	}

	// A real event (manual: the test picture holds no person), ten seconds.
	code, data := frigate("POST", "/api/events/hof/person/create", map[string]any{"duration": 10, "include_recording": true})
	var created struct {
		ID string `json:"event_id"`
	}
	if code != 200 || json.Unmarshal(data, &created) != nil || created.ID == "" {
		t.Fatalf("manual event: %d %s", code, data)
	}
	var event nvr.Event
	until("the event ended and its last seconds are written", 3*time.Minute, func() bool {
		if getJSON(t, ts, ts.client, "/nvr/events/"+created.ID, &event) != 200 || event.End == nil {
			return false
		}
		return float64(time.Now().Unix()) > *event.End+5+25
	})
	if res, body := ts.request("POST", "/api/v1/nvr/events/"+created.ID+"/keep", nil); res.StatusCode != 200 {
		t.Fatalf("keep: %d %s", res.StatusCode, body)
	} else if json.Unmarshal(body, &event) != nil || !event.Kept || event.KeptExport == "" {
		t.Fatalf("kept: %s", body)
	}
	retained := func() bool {
		var raw struct {
			Retain bool `json:"retain_indefinitely"`
		}
		code, data := frigate("GET", "/api/events/"+created.ID, nil)
		return code == 200 && json.Unmarshal(data, &raw) == nil && raw.Retain
	}
	// Frigate writes through a queue: the flag shows a moment later.
	until("Frigate keeps the event", time.Minute, retained)
	var export nvr.Export
	until("the kept video is saved", 3*time.Minute, func() bool {
		return getJSON(t, ts, ts.client, "/nvr/exports/"+event.KeptExport, &export) == 200 && export.State == nvr.ExportDone
	})
	t.Logf("kept video %s: %v–%v (event %v–%v), %v bytes", export.ID, export.From, export.To, event.Start, *event.End, *export.SizeBytes)
	if export.Event != created.ID || export.From > event.Start-5 || export.To < *event.End+5 || export.SizeBytes == nil || *export.SizeBytes < 10000 {
		t.Errorf("kept video %+v", export)
	}
	if res, _ := ts.request("GET", "/api/v1/nvr/exports/"+export.ID+"/download", nil); res.StatusCode != 200 {
		t.Errorf("kept video download: %d", res.StatusCode)
	}
	// Deleting it asks first.
	if res, body := ts.request("DELETE", "/api/v1/nvr/events/"+created.ID, nil); res.StatusCode != http.StatusConflict {
		t.Errorf("delete a kept event without asking: %d %s", res.StatusCode, body)
	}
	// "Seen" is the account's: Frigate's own flag stays as it was.
	if res, body := ts.request("POST", "/api/v1/nvr/events/"+created.ID+"/reviewed", map[string]any{"reviewed": true}); res.StatusCode != 200 {
		t.Errorf("seen: %d %s", res.StatusCode, body)
	}
	code, data = frigate("GET", "/api/review/event/"+created.ID, nil)
	var review struct {
		Reviewed bool `json:"has_been_reviewed"`
	}
	t.Logf("Frigate's review of the event: %d %s", code, data)
	if code == 200 && json.Unmarshal(data, &review) == nil && review.Reviewed {
		t.Error("titand set Frigate's NVR-wide review flag")
	}
	// Un-keep: the flag goes, the saved video goes.
	if res, body := ts.request("DELETE", "/api/v1/nvr/events/"+created.ID+"/keep", nil); res.StatusCode != 200 {
		t.Fatalf("unkeep: %d %s", res.StatusCode, body)
	}
	until("Frigate no longer keeps the event", time.Minute, func() bool { return !retained() })
	if res, _ := ts.request("GET", "/api/v1/nvr/exports/"+export.ID, nil); res.StatusCode != http.StatusNotFound {
		t.Errorf("the saved video stayed: %d", res.StatusCode)
	}
	out := run("exec", "titanos-nvr", "sh", "-c", "ls /media/frigate/exports | wc -l")
	t.Logf("files in /media/frigate/exports after un-keeping: %s", out)
	if out != "0" {
		t.Errorf("export files left: %s", out)
	}
}
