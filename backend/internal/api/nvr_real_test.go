package api

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// realNVREngine is the Docker of this machine, whatever TitanOS's own
// settings say.
type realNVREngine struct{ *docker.Manager }

func (realNVREngine) EngineReady() bool { return true }

// TestNVRRealFrigate runs the whole way for real: titand's routes, the
// manager, a real Docker engine, the pinned Frigate image and a camera.
// The camera is a second container of the same image whose go2rtc serves
// a moving test picture over RTSP (nothing is installed on the host).
// Not part of the normal test run:
//
//	TITANOS_NVR_REAL=<empty folder> go test ./internal/api -run TestNVRRealFrigate -v -timeout 30m
//
// It starts the containers titanos-nvr and titanos-nvr-testsrc and
// removes them again; Frigate writes into the folder as root.
func TestNVRRealFrigate(t *testing.T) {
	root := os.Getenv("TITANOS_NVR_REAL")
	if root == "" {
		t.Skip("set TITANOS_NVR_REAL to a folder to run Frigate for real")
	}
	for _, volume := range []string{"tank", "tank2"} {
		if err := os.MkdirAll(filepath.Join(root, volume), 0o755); err != nil {
			t.Fatal(err)
		}
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
		manager.Mounted = func() []string { return []string{"tank", "tank2"} }
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
		run("rm", "-f", "titanos-nvr-probe")
		run("network", "rm", "titanos-nvr-probe")
		run("network", "rm", "titanos-nvr")
	}()

	if res, body := ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"enabled": true, "volume": "tank", "detector": map[string]any{"kind": "cpu"}}); res.StatusCode != 200 {
		t.Fatalf("enable: %d %s", res.StatusCode, body)
	}
	until("Frigate runs", 25*time.Minute, func() bool { return status().State == nvr.PhaseRunning })
	secret := manager.Settings().ProxySecret
	address := run("inspect", "-f", `{{(index .NetworkSettings.Networks "titanos-nvr").IPAddress}}`, "titanos-nvr")
	frigate := func(path string, withSecret bool) (int, []byte) {
		req, _ := http.NewRequest("GET", "http://"+address+":8971"+path, nil)
		if withSecret {
			req.Header.Set("X-Proxy-Secret", secret)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return 0, []byte(err.Error())
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return res.StatusCode, data
	}
	t.Logf("Frigate at %s, secret of %d characters", address, len(secret))
	// effective reads what Frigate made of config.yml (global record block
	// merged into each camera field by field) and checks that the global
	// retention is the floor for unknown cameras and that no camera
	// inherits it: every camera sets both days itself.
	effective := func(where string, want map[string][2]float64) {
		t.Helper()
		type days struct {
			Continuous struct {
				Days float64 `json:"days"`
			} `json:"continuous"`
			Motion struct {
				Days float64 `json:"days"`
			} `json:"motion"`
		}
		var cfg struct {
			Record  days `json:"record"`
			Cameras map[string]struct {
				Enabled bool `json:"enabled"`
				Record  days `json:"record"`
			} `json:"cameras"`
		}
		code, body := frigate("/api/config", true)
		if code != 200 || json.Unmarshal(body, &cfg) != nil {
			t.Errorf("%s: Frigate's config: %d", where, code)
			return
		}
		got := map[string][2]float64{}
		for id, c := range cfg.Cameras {
			got[id] = [2]float64{c.Record.Continuous.Days, c.Record.Motion.Days}
		}
		t.Logf("%s: Frigate's effective record days: global %v/%v, cameras %v", where, cfg.Record.Continuous.Days, cfg.Record.Motion.Days, got)
		if cfg.Record.Continuous.Days < 30 || cfg.Record.Motion.Days < 30 {
			t.Errorf("%s: global record days %v/%v, want at least 30", where, cfg.Record.Continuous.Days, cfg.Record.Motion.Days)
		}
		if len(got) != len(want) {
			t.Errorf("%s: cameras %v, want %v", where, got, want)
		}
		for id, w := range want {
			if got[id] != w {
				t.Errorf("%s: camera %s keeps continuous/motion %v days, want %v (inherited the global floor?)", where, id, got[id], w)
			}
		}
	}
	effective("no camera", map[string][2]float64{})

	// Nothing of Frigate answers without the secret: 5000, go2rtc's API
	// and its RTSP server listen on the container's loopback only, 8971
	// wants the secret. From the host:
	for _, port := range []string{"5000", "1984", "8554", "8971"} {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(address, port), 3*time.Second)
		if conn != nil {
			conn.Close()
		}
		t.Logf("host → %s:%s: %v", address, port, err)
		if (port == "8971") != (err == nil) {
			t.Errorf("port %s from the host: %v", port, err)
		}
	}
	if code, body := frigate("/api/config/raw", false); code != 401 {
		t.Errorf("8971 without the secret: %d %s", code, body[:min(len(body), 200)])
	} else {
		t.Logf("host → :8971/api/config/raw without the secret: %d", code)
	}
	if code, _ := frigate("/api/config/raw", true); code != 200 {
		t.Errorf("8971 with the secret: %d", code)
	}
	if code, body := frigate("/api/recordings/storage", true); code != 200 {
		t.Errorf("an admin route with the secret: %d %s", code, body)
	}
	// From a container on another Docker network, and from one on the
	// NVR's own network (stands for a host that routes to it).
	run("network", "create", "titanos-nvr-probe")
	for _, network := range []string{"titanos-nvr-probe", "titanos-nvr"} {
		for _, port := range []string{"5000", "8971"} {
			out := run("run", "--rm", "--network", network, "--entrypoint", "curl", image, "-s", "-o", "/dev/null", "-w", "%{http_code} %{errormsg}", "--max-time", "5", "http://"+address+":"+port+"/api/config/raw")
			t.Logf("container on %s → %s:%s/api/config/raw: %s", network, address, port, out)
			if strings.HasPrefix(out, "200") {
				t.Errorf("%s reaches Frigate's config on %s: %s", network, port, out)
			}
		}
	}
	if out := run("exec", "titanos-nvr", "curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "http://127.0.0.1:5000/api/version"); out != "200" {
		t.Errorf("port 5000 inside the container (Frigate's own export uses it): %s", out)
	}

	// The camera: checked with titand's own RTSP probe, then added.
	res, body := ts.request("POST", "/api/v1/nvr/cameras/probe", map[string]any{"kind": "rtsp", "mainURL": stream})
	var probe nvr.ProbeResult
	json.Unmarshal(body, &probe)
	t.Logf("probe: %s", body)
	if res.StatusCode != 200 || probe.Outcome != "ok" || len(probe.Streams) != 1 || probe.Streams[0].Codec != "H264" {
		t.Errorf("probe of the test camera: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/nvr/cameras", map[string]any{
		"name": "Test", "source": map[string]any{"kind": "rtsp", "mainURL": stream},
		"record": map[string]any{"mode": "continuous", "retainDays": 1, "preSeconds": 5, "postSeconds": 5},
	})
	if res.StatusCode != 201 {
		t.Fatalf("add camera: %d %s", res.StatusCode, body)
	}
	// A second camera, switched off, recording on events: Frigate merges
	// the global 30 days into it unless it sets both days itself.
	res, body = ts.request("POST", "/api/v1/nvr/cameras", map[string]any{
		"name": "Aus", "enabled": false, "source": map[string]any{"kind": "rtsp", "mainURL": stream},
		"record": map[string]any{"mode": "events", "retainDays": 5, "preSeconds": 5, "postSeconds": 5},
	})
	if res.StatusCode != 201 {
		t.Fatalf("add camera: %d %s", res.StatusCode, body)
	}
	started := time.Now()
	until("Frigate runs both cameras", 5*time.Minute, func() bool {
		code, body := frigate("/api/config", true)
		return code == 200 && strings.Contains(string(body), `"aus":{`) && status().State == nvr.PhaseRunning
	})
	effective("two cameras", map[string][2]float64{"test": {1, 1}, "aus": {0, 0}})
	// A narrower recording is asked first; nothing changes without the yes.
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/test", map[string]any{"record": map[string]any{"mode": "events", "retainDays": 1, "preSeconds": 5, "postSeconds": 5}}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "nvr_recordings_shrink" {
		t.Errorf("continuous → events without confirmation: %d %s", res.StatusCode, body)
	}
	until("camera online", 5*time.Minute, func() bool {
		var view nvr.CameraView
		getJSON(t, ts, ts.client, "/nvr/cameras/test", &view)
		return view.State.Online && view.State.FPS > 0
	})
	// A still and the live stream (go2rtc's MSE protocol through titand).
	res, body = ts.request("GET", "/api/v1/nvr/cameras/test/snapshot.jpg?height=180", nil)
	if res.StatusCode != 200 || len(body) < 1000 || body[0] != 0xFF {
		t.Errorf("snapshot: %d, %d bytes", res.StatusCode, len(body))
	}
	conn, lctx := liveDial(t, ts, ts.client, "/nvr/cameras/test/live")
	if err := conn.Write(lctx, websocket.MessageText, []byte(`{"type":"mse","value":"avc1.640029,avc1.64002A,avc1.640033,mp4a.40.2,mp4a.40.5,flac,opus"}`)); err != nil {
		t.Fatal(err)
	}
	conn.SetReadLimit(16 << 20)
	text, binary := "", 0
	for binary < 3 {
		kind, data, err := conn.Read(lctx)
		if err != nil {
			t.Errorf("live: %v (after %q and %d binary messages)", err, text, binary)
			break
		}
		if kind == websocket.MessageText {
			text = string(data)
		} else {
			binary += 1
		}
	}
	t.Logf("live: %s, then video (through 8971 with the secret)", text)
	if !strings.Contains(text, `"type":"mse"`) || !strings.Contains(text, "avc1") {
		t.Errorf("live answer: %s", text)
	}
	conn.CloseNow()
	// The feed: Frigate's WebSocket through titand.
	feed, fctx := liveDial(t, ts, ts.client, "/nvr/feed")
	sawHello, sawCamera := false, false
	for i := 0; i < 20 && !(sawHello && sawCamera); i++ {
		_, data, err := feed.Read(fctx)
		if err != nil {
			break
		}
		t.Logf("feed: %s", data)
		sawHello = sawHello || strings.Contains(string(data), `"t":"hello","running":true`)
		sawCamera = sawCamera || strings.Contains(string(data), `"camera":"test"`)
	}
	if !sawHello || !sawCamera {
		t.Errorf("feed: hello %v, camera %v", sawHello, sawCamera)
	}
	feed.CloseNow()

	// Recordings: a segment appears, the summary names the hour.
	var segments struct {
		Segments []nvr.Segment `json:"segments"`
	}
	until("a recording segment", 4*time.Minute, func() bool {
		now := time.Now().Unix()
		getJSON(t, ts, ts.client, "/nvr/cameras/test/recordings?from="+itoa(now-3600)+"&to="+itoa(now), &segments)
		return len(segments.Segments) >= 3
	})
	t.Logf("segments: %+v", segments.Segments)
	var summary struct {
		Days []nvr.RecordedDay `json:"days"`
	}
	if getJSON(t, ts, ts.client, "/nvr/cameras/test/recordings/summary", &summary); len(summary.Days) == 0 || len(summary.Days[0].Hours) == 0 || summary.Days[0].Hours[0].Seconds == 0 {
		t.Errorf("summary: %+v", summary)
	}
	t.Logf("summary: %+v", summary.Days)
	// Playback: master → index → init and a segment, all through titand,
	// following the names in the playlists as a player does.
	from, to := int64(segments.Segments[0].Start), int64(segments.Segments[len(segments.Segments)-1].End)+1
	vod := "/api/v1/nvr/cameras/test/vod/" + itoa(from) + "/" + itoa(to) + "/"
	res, body = ts.request("GET", vod+"master.m3u8", nil)
	t.Logf("master.m3u8 (%d, %s):\n%s", res.StatusCode, res.Header.Get("Content-Type"), body)
	if res.StatusCode != 200 {
		t.Fatalf("master: %d %s", res.StatusCode, body)
	}
	index := ""
	for _, line := range strings.Split(string(body), "\n") {
		if line = strings.TrimSpace(line); line != "" && !strings.HasPrefix(line, "#") {
			index = line
		}
	}
	if strings.ContainsAny(index, "/:") {
		t.Fatalf("the master names its playlist not relative: %q", index)
	}
	res, body = ts.request("GET", vod+index, nil)
	t.Logf("%s (%d):\n%s", index, res.StatusCode, body)
	if res.StatusCode != 200 {
		t.Fatalf("index: %d", res.StatusCode)
	}
	var files []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if _, rest, ok := strings.Cut(line, `URI="`); ok {
			files = append(files, strings.TrimSuffix(rest, `"`))
		} else if line != "" && !strings.HasPrefix(line, "#") {
			files = append(files, line)
		}
	}
	if len(files) < 2 {
		t.Fatalf("playlist names %v", files)
	}
	for _, file := range files[:2] {
		if strings.ContainsAny(file, "/:") {
			t.Errorf("the playlist names a file not relative: %q", file)
			continue
		}
		res, body := ts.request("GET", vod+file, nil)
		t.Logf("%s: %d, %s, %d bytes", file, res.StatusCode, res.Header.Get("Content-Type"), len(body))
		if res.StatusCode != 200 || len(body) < 200 {
			t.Errorf("%s: %d, %d bytes", file, res.StatusCode, len(body))
		}
	}
	// A range of a segment.
	req, _ := http.NewRequest("GET", ts.url+vod+files[1], nil)
	req.Header.Set("Range", "bytes=0-99")
	if part, err := ts.client.Do(req); err != nil || part.StatusCode != 206 {
		t.Errorf("range of a segment: %v %v", part, err)
	} else {
		t.Logf("range of %s: %d %s", files[1], part.StatusCode, part.Header.Get("Content-Range"))
		part.Body.Close()
	}
	res, body = ts.request("GET", "/api/v1/nvr/cameras/test/recordings/frame.jpg?at="+itoa(from+5), nil)
	t.Logf("frame: %d, %s, %d bytes", res.StatusCode, res.Header.Get("Content-Type"), len(body))
	if res.StatusCode != 200 || len(body) < 1000 {
		t.Errorf("frame at a recorded time: %d %s", res.StatusCode, body[:min(len(body), 200)])
	}

	// An export: made, finished, downloaded with a range, renamed, deleted.
	res, body = ts.request("POST", "/api/v1/nvr/exports", map[string]any{"camera": "test", "from": from, "to": from + 20, "name": "Probe"})
	var export nvr.Export
	json.Unmarshal(body, &export)
	if res.StatusCode != 201 {
		t.Fatalf("export: %d %s", res.StatusCode, body)
	}
	until("export done", 3*time.Minute, func() bool {
		getJSON(t, ts, ts.client, "/nvr/exports/"+export.ID, &export)
		return export.State == nvr.ExportDone && export.SizeBytes != nil
	})
	t.Logf("export: %+v, %d bytes", export, *export.SizeBytes)
	req, _ = http.NewRequest("GET", ts.url+"/api/v1/nvr/exports/"+export.ID+"/download", nil)
	req.Header.Set("Range", "bytes=0-1023")
	part, err := ts.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(part.Body)
	part.Body.Close()
	t.Logf("download: %d, %s, %s, %d bytes", part.StatusCode, part.Header.Get("Content-Type"), part.Header.Get("Content-Range"), len(data))
	if part.StatusCode != 206 || len(data) != 1024 || !strings.Contains(string(data[:64]), "ftyp") {
		t.Errorf("download: %d, %d bytes", part.StatusCode, len(data))
	}
	if res, _ := ts.request("GET", "/api/v1/nvr/exports/"+export.ID+"/thumbnail.jpg", nil); res.StatusCode != 200 {
		t.Logf("export thumbnail: %d (%s)", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/exports/"+export.ID, map[string]any{"name": "Probe 2"}); res.StatusCode != 200 {
		t.Errorf("rename: %d %s", res.StatusCode, body)
	}
	// Events (the test picture shows no person: the list may be empty).
	var events eventList
	if code := getJSON(t, ts, ts.client, "/nvr/events?limit=10", &events); code != 200 {
		t.Errorf("events: %d", code)
	}
	t.Logf("events: %d", len(events.Events))
	var ptz nvr.PTZInfo
	if code := getJSON(t, ts, ts.client, "/nvr/cameras/test/ptz", &ptz); code != 200 || ptz.Supported {
		t.Errorf("ptz of a camera without: %d %+v", code, ptz)
	}

	pids := run("stats", "--no-stream", "--format", "{{.PIDs}} {{.MemUsage}}", "titanos-nvr")
	t.Logf("container with one camera: PIDs and memory %s (limit %d)", pids, docker.NVRPidsLimit)
	t.Logf("published: %s", run("port", "titanos-nvr"))
	if ss, err := exec.Command("ss", "-ltnuH").CombinedOutput(); err == nil {
		for _, line := range strings.Split(string(ss), "\n") {
			if strings.Contains(line, ":8555") {
				t.Logf("listening: %s", strings.Join(strings.Fields(line), " "))
			}
		}
	}

	// Talking back: go2rtc's WebRTC signalling through titand and 8971.
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/test", map[string]any{"audio": map[string]any{"enabled": true, "twoWay": true}}); res.StatusCode != 200 {
		t.Fatalf("talk on: %d %s", res.StatusCode, body)
	}
	time.Sleep(5 * time.Second) // the restart begins
	until("camera online with talking back", 5*time.Minute, func() bool {
		var view nvr.CameraView
		getJSON(t, ts, ts.client, "/nvr/cameras/test", &view)
		return status().State == nvr.PhaseRunning && view.State.Online
	})
	talk, tctx := liveDial(t, ts, ts.client, "/nvr/cameras/test/talk")
	offer := "v=0\r\no=- 0 0 IN IP4 127.0.0.1\r\ns=-\r\nt=0 0\r\na=group:BUNDLE 0\r\nm=audio 9 UDP/TLS/RTP/SAVPF 111\r\nc=IN IP4 0.0.0.0\r\na=mid:0\r\na=sendrecv\r\na=rtpmap:111 opus/48000/2\r\na=ice-ufrag:abcd\r\na=ice-pwd:abcdefghijklmnopqrstuvwx\r\na=fingerprint:sha-256 00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF\r\na=setup:actpass\r\n"
	data, _ = json.Marshal(map[string]string{"type": "webrtc/offer", "value": offer})
	if err := talk.Write(tctx, websocket.MessageText, data); err != nil {
		t.Errorf("talk offer: %v", err)
	} else if _, answer, err := talk.Read(tctx); err != nil {
		t.Errorf("talk: no answer from go2rtc: %v", err)
	} else {
		t.Logf("talk: go2rtc answered %s", answer[:min(len(answer), 120)])
	}
	talk.CloseNow()

	// The NVR moves to another volume: config.yml is written there before
	// the new container starts, and Frigate runs the camera there (titand
	// checks /api/config after the start); then back.
	for _, volume := range []string{"tank2", "tank"} {
		if res, body := ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"volume": volume}); res.StatusCode != 200 {
			t.Fatalf("move to %s: %d %s", volume, res.StatusCode, body)
		}
		time.Sleep(5 * time.Second) // the restart begins
		until("Frigate runs on "+volume, 5*time.Minute, func() bool {
			var view nvr.CameraView
			getJSON(t, ts, ts.client, "/nvr/cameras/test", &view)
			return status().State == nvr.PhaseRunning && view.State.Online
		})
		file := filepath.Join(root, volume, ".titanos", "nvr", "config", "config.yml")
		if info, err := os.Stat(file); err != nil {
			t.Errorf("config.yml on %s: %v", volume, err)
		} else {
			t.Logf("config.yml on %s: %d bytes", volume, info.Size())
		}
		address = run("inspect", "-f", `{{(index .NetworkSettings.Networks "titanos-nvr").IPAddress}}`, "titanos-nvr")
		if code, body := frigate("/api/config", true); code != 200 || !strings.Contains(string(body), `"test":{`) {
			t.Errorf("Frigate on %s runs without the camera: %d", volume, code)
		}
	}

	// The camera is deleted: its recordings stay (a stub that is switched
	// off keeps Frigate's cleanup from deleting them), counted and
	// playable for an administrator.
	var before struct {
		Segments []nvr.Segment `json:"segments"`
	}
	now := time.Now().Unix()
	getJSON(t, ts, ts.client, "/nvr/cameras/test/recordings?from="+itoa(now-3600)+"&to="+itoa(now), &before)
	if res, body := ts.request("DELETE", "/api/v1/nvr/cameras/test", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete camera: %d %s", res.StatusCode, body)
	}
	time.Sleep(5 * time.Second) // the restart begins
	until("Frigate runs with the deleted camera", 5*time.Minute, func() bool { return status().State == nvr.PhaseRunning })
	restarted := time.Now()
	// Frigate's cleanup runs a minute after its start.
	time.Sleep(75 * time.Second)
	var after struct {
		Segments []nvr.Segment `json:"segments"`
	}
	getJSON(t, ts, ts.client, "/nvr/cameras/test/recordings?from="+itoa(now-3600)+"&to="+itoa(now), &after)
	t.Logf("segments of the deleted camera %v after Frigate's restart: %d before, %d after", time.Since(restarted).Round(time.Second), len(before.Segments), len(after.Segments))
	if len(after.Segments) == 0 || len(after.Segments) < len(before.Segments) {
		t.Errorf("recordings of the deleted camera: %d before, %d after", len(before.Segments), len(after.Segments))
	}
	if code, body := frigate("/api/recordings/storage", true); code != 200 || !strings.Contains(string(body), `"test":{`) {
		t.Errorf("Frigate does not count the deleted camera: %d %s", code, body)
	} else {
		t.Logf("Frigate's storage: %s", body)
	}
	var running struct {
		Cameras map[string]struct {
			Enabled bool `json:"enabled"`
		} `json:"cameras"`
	}
	if code, body := frigate("/api/config", true); code != 200 || json.Unmarshal(body, &running) != nil {
		t.Errorf("Frigate's config: %d", code)
	} else if stub, ok := running.Cameras["test"]; !ok || stub.Enabled {
		t.Errorf("the deleted camera in Frigate's config: %+v", running.Cameras)
	}
	effective("deleted camera", map[string][2]float64{"test": {1, 1}, "aus": {0, 0}})
	if len(after.Segments) > 0 {
		vod := "/api/v1/nvr/cameras/test/vod/" + itoa(int64(after.Segments[0].Start)) + "/" + itoa(int64(after.Segments[len(after.Segments)-1].End)+1) + "/master.m3u8"
		if res, body := ts.request("GET", vod, nil); res.StatusCode != 200 {
			t.Errorf("playback of the deleted camera: %d %s", res.StatusCode, body)
		}
	}
	var listed struct {
		Cameras []nvr.CameraView `json:"cameras"`
	}
	getJSON(t, ts, ts.client, "/nvr/cameras", &listed)
	if len(listed.Cameras) != 2 || listed.Cameras[1].ID != "test" || !listed.Cameras[1].Deleted {
		t.Errorf("administrator's list: %+v", listed.Cameras)
	}
	guest := ts.guest("gast")
	if code := getJSON(t, ts, guest, "/nvr/cameras/test/recordings/summary", &summary); code != 404 {
		t.Errorf("a user reaches the deleted camera: %d", code)
	}
	if strings.Contains(run("logs", "--tail", "3000", "titanos-nvr"), secret) {
		t.Error("the secret is in the container's log")
	}

	// The deleted camera is noted on the stick, not on the volume: the NVR
	// moves to tank2 and back, and both volumes' config.yml keep it (on
	// tank its recordings are).
	if got := manager.DeletedCameras(); len(got) != 1 || got[0].ID != "test" {
		t.Errorf("deleted cameras: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(root, "tank", ".titanos", "nvr", "deleted-cameras.json")); err == nil {
		t.Error("deleted cameras noted on the volume")
	}
	for _, volume := range []string{"tank2", "tank"} {
		if res, body := ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"volume": volume}); res.StatusCode != 200 {
			t.Fatalf("move to %s: %d %s", volume, res.StatusCode, body)
		}
		time.Sleep(5 * time.Second) // the restart begins
		until("Frigate runs on "+volume+" with the deleted camera", 5*time.Minute, func() bool { return status().State == nvr.PhaseRunning })
		data, err := os.ReadFile(filepath.Join(root, volume, ".titanos", "nvr", "config", "config.yml"))
		if err != nil || !strings.Contains(string(data), "\n  test:\n    enabled: false\n") {
			t.Errorf("the deleted camera in %s's config.yml: %v", volume, err)
		}
		address = run("inspect", "-f", `{{(index .NetworkSettings.Networks "titanos-nvr").IPAddress}}`, "titanos-nvr")
		effective("deleted camera on "+volume, map[string][2]float64{"test": {1, 1}, "aus": {0, 0}})
	}

	// The storage figures move (the watch looks every five minutes); the
	// recordings now are those of the deleted camera.
	until("storage figures", 7*time.Minute, func() bool {
		st := status().Storage
		return st.RecordingsBytes != nil && *st.RecordingsBytes > 0 && st.ExportsBytes != nil && *st.ExportsBytes > 0 && st.SpanDays != nil
	})
	st := status()
	t.Logf("storage after %v: recordings %d bytes, exports %d bytes, span %.4f days, cap %s", time.Since(started).Round(time.Second), *st.Storage.RecordingsBytes, *st.Storage.ExportsBytes, *st.Storage.SpanDays, st.Storage.Cap)
	if res, _ := ts.request("DELETE", "/api/v1/nvr/exports/"+export.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("delete export: %d", res.StatusCode)
	}
	d := manager.Diagnosis(context.Background())
	t.Logf("diagnosis: state %s, detector %s %v ms, cameras %+v, shm %v/%v MB, last error %+v", d.State, d.Detector.Active, d.Detector.InferenceMs, d.Cameras, d.Shm.UsedMB, d.Shm.TotalMB, d.LastError)
	for _, line := range d.Log {
		if strings.Contains(line, "18554") && strings.Contains(line, "@") {
			t.Errorf("credentials in the log: %s", line)
		}
	}
}
