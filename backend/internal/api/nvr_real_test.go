package api

import (
	"context"
	"encoding/json"
	"io"
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
			return nvr.Account{Name: user.Name, Admin: user.Role.CanAdminister(), Disabled: user.Disabled}, ok
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
	started := time.Now()
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
	t.Logf("live: %s, then video", text)
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
	req, _ := http.NewRequest("GET", ts.url+"/api/v1/nvr/exports/"+export.ID+"/download", nil)
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

	// The storage figures move (the watch looks every five minutes).
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
