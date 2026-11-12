package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// grant gives a normal account rights on cameras: "live", "recordings"
// or "both".
func grant(t *testing.T, ts *testServer, user string, rights map[string]string) {
	t.Helper()
	cameras := map[string]any{}
	for id, right := range rights {
		cameras[id] = map[string]any{"live": right == "live" || right == "both", "recordings": right == "recordings" || right == "both"}
	}
	if res, body := ts.request("PUT", "/api/v1/users/"+user+"/cameras", map[string]any{"cameras": cameras}); res.StatusCode != 200 {
		t.Fatalf("grant: %d %s", res.StatusCode, body)
	}
}

type eventList struct {
	Events []nvr.Event `json:"events"`
	Next   *float64    `json:"next"`
}

func getJSON(t *testing.T, ts *testServer, client *http.Client, path string, into any) int {
	t.Helper()
	res, body := ts.do(client, "GET", "/api/v1"+path, nil)
	if res.StatusCode == 200 && into != nil {
		if err := json.Unmarshal(body, into); err != nil {
			t.Fatalf("%s: %v: %s", path, err, body)
		}
	}
	return res.StatusCode
}

func TestNVREventsFollowTheCameraRights(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	guest := ts.guest("gast")
	var all, mine eventList
	if getJSON(t, ts, ts.client, "/nvr/events?limit=200", &all); len(all.Events) < 20 {
		t.Fatalf("owner: %d events", len(all.Events))
	}
	// No right: no events, and every event of the NAS is "not there".
	if getJSON(t, ts, guest, "/nvr/events", &mine); len(mine.Events) != 0 {
		t.Errorf("without rights: %d events", len(mine.Events))
	}
	// Live alone shows no events; recordings do, for that camera only.
	grant(t, ts, "gast", map[string]string{"haustuer": "live", "einfahrt": "recordings"})
	getJSON(t, ts, guest, "/nvr/events?limit=200", &mine)
	if len(mine.Events) == 0 {
		t.Fatal("no events with the right")
	}
	for _, e := range mine.Events {
		if e.Camera != "einfahrt" {
			t.Errorf("event of %s", e.Camera)
		}
	}
	// Asking for another camera does not widen the list.
	getJSON(t, ts, guest, "/nvr/events?camera=haustuer,garten", &mine)
	if len(mine.Events) != 0 {
		t.Errorf("asked for cameras without right: %d events", len(mine.Events))
	}
	var own, foreign nvr.Event
	for _, e := range all.Events {
		if e.Camera == "einfahrt" && own.ID == "" && e.HasSnap {
			own = e
		}
		if e.Camera == "haustuer" && foreign.ID == "" && e.HasSnap {
			foreign = e
		}
	}
	for _, suffix := range []string{"", "/thumbnail.jpg", "/snapshot.jpg", "/clip.mp4"} {
		if res, body := ts.do(guest, "GET", "/api/v1/nvr/events/"+foreign.ID+suffix, nil); res.StatusCode != 404 || errorCode(t, body) != "nvr_event_not_found" {
			t.Errorf("foreign event%s: %d %s", suffix, res.StatusCode, body)
		}
		if res, _ := ts.do(guest, "GET", "/api/v1/nvr/events/"+own.ID+suffix, nil); res.StatusCode != 200 {
			t.Errorf("own event%s: %d", suffix, res.StatusCode)
		}
	}
	if res, _ := ts.do(guest, "POST", "/api/v1/nvr/events/"+foreign.ID+"/reviewed", map[string]any{"reviewed": true}); res.StatusCode != 404 {
		t.Errorf("mark a foreign event: %d", res.StatusCode)
	}
	res, body := ts.do(guest, "POST", "/api/v1/nvr/events/"+own.ID+"/reviewed", map[string]any{"reviewed": true})
	if res.StatusCode != 200 || !strings.Contains(string(body), `"reviewed":true`) {
		t.Errorf("mark: %d %s", res.StatusCode, body)
	}
	var again nvr.Event
	if getJSON(t, ts, guest, "/nvr/events/"+own.ID, &again); !again.Reviewed {
		t.Error("not marked")
	}
	// Deleting is for administrators.
	if res, _ := ts.do(guest, "DELETE", "/api/v1/nvr/events/"+own.ID, nil); res.StatusCode != http.StatusForbidden {
		t.Errorf("user deletes: %d", res.StatusCode)
	}
	if res, _ := ts.request("DELETE", "/api/v1/nvr/events/"+own.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("owner deletes: %d", res.StatusCode)
	}
	if res, _ := ts.request("GET", "/api/v1/nvr/events/"+own.ID, nil); res.StatusCode != 404 {
		t.Errorf("deleted event: %d", res.StatusCode)
	}
	// Media: content types, the clip with ranges, the download name.
	res, body = ts.request("GET", "/api/v1/nvr/events/"+foreign.ID+"/thumbnail.jpg", nil)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" || res.Header.Get("Cache-Control") != "private, no-store" || len(body) < 100 {
		t.Errorf("thumbnail: %d %v", res.StatusCode, res.Header)
	}
	req, _ := http.NewRequest("GET", ts.url+"/api/v1/nvr/events/"+foreign.ID+"/clip.mp4?download=1", nil)
	req.Header.Set("Range", "bytes=10-109")
	part, err := ts.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer part.Body.Close()
	if part.StatusCode != http.StatusPartialContent || part.Header.Get("Content-Range") == "" || part.ContentLength != 100 || part.Header.Get("Content-Type") != "video/mp4" ||
		!strings.HasPrefix(part.Header.Get("Content-Disposition"), `attachment; filename="haustuer-`) {
		t.Errorf("clip range: %d %v", part.StatusCode, part.Header)
	}
	// The event that is going on has no snapshot yet.
	if res, body := ts.request("GET", "/api/v1/nvr/events/"+all.Events[0].ID+"/snapshot.jpg", nil); res.StatusCode != 404 || errorCode(t, body) != "nvr_no_picture" {
		t.Errorf("no snapshot: %d %s", res.StatusCode, body)
	}
	// Filters are checked; pages have a cursor.
	for _, query := range []string{"limit=0", "limit=500", "label=dragon", "from=abc", "reviewed=maybe", "before=-1"} {
		if res, body := ts.request("GET", "/api/v1/nvr/events?"+query, nil); res.StatusCode != 400 || errorCode(t, body) != "nvr_invalid" {
			t.Errorf("%s: %d %s", query, res.StatusCode, body)
		}
	}
	var page eventList
	getJSON(t, ts, ts.client, "/nvr/events?limit=5&label=person", &page)
	if len(page.Events) != 5 || page.Next == nil {
		t.Errorf("page: %d events, next %v", len(page.Events), page.Next)
	}
	// Path tricks never reach Frigate.
	for _, id := range []string{"..%2Fconfig", "a%2Fb", "%2e%2e"} {
		if res, _ := ts.request("GET", "/api/v1/nvr/events/"+id+"/clip.mp4", nil); res.StatusCode != 404 {
			t.Errorf("event id %q: %d", id, res.StatusCode)
		}
	}
}

func TestNVRRecordingsAndPlayback(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	guest := ts.guest("gast")
	grant(t, ts, "gast", map[string]string{"haustuer": "recordings", "einfahrt": "live"})
	now := time.Now().Unix()
	var summary struct {
		Days []nvr.RecordedDay `json:"days"`
	}
	if code := getJSON(t, ts, guest, "/nvr/cameras/haustuer/recordings/summary", &summary); code != 200 || len(summary.Days) < 3 || len(summary.Days[0].Hours) == 0 {
		t.Fatalf("summary: %d %+v", code, summary)
	}
	var segments struct {
		From, To float64
		Segments []nvr.Segment `json:"segments"`
	}
	path := "/nvr/cameras/haustuer/recordings?from=" + itoa(now-3600) + "&to=" + itoa(now)
	if code := getJSON(t, ts, guest, path, &segments); code != 200 || len(segments.Segments) < 300 || segments.From != float64(now-3600) {
		t.Fatalf("segments: %d, %d", code, len(segments.Segments))
	}
	// Live alone does not open the recordings; no right, no camera.
	for _, p := range []string{"/nvr/cameras/einfahrt/recordings/summary", "/nvr/cameras/garten/recordings?from=1&to=2", "/nvr/cameras/einfahrt/vod/1/2/master.m3u8", "/nvr/cameras/garten/recordings/frame.jpg?at=5"} {
		if res, body := ts.do(guest, "GET", "/api/v1"+p, nil); res.StatusCode != 404 || errorCode(t, body) != "nvr_camera_not_found" {
			t.Errorf("%s: %d %s", p, res.StatusCode, body)
		}
	}
	if res, body := ts.do(guest, "GET", "/api/v1/nvr/cameras/haustuer/recordings?from=1&to=999999", nil); res.StatusCode != 400 {
		t.Errorf("more than a day: %d %s", res.StatusCode, body)
	}
	// Playback: the master names the playlist, the playlist its files,
	// all relative: every one of them comes through the same route.
	vod := "/api/v1/nvr/cameras/haustuer/vod/" + itoa(now-600) + "/" + itoa(now-300) + "/"
	res, body := ts.do(guest, "GET", vod+"master.m3u8", nil)
	if res.StatusCode != 200 || !strings.Contains(res.Header.Get("Content-Type"), "mpegurl") || !strings.Contains(string(body), "\nindex-v1.m3u8") {
		t.Fatalf("master: %d %s %s", res.StatusCode, res.Header.Get("Content-Type"), body)
	}
	res, body = ts.do(guest, "GET", vod+"index-v1.m3u8", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `URI="init-v1.mp4"`) || !strings.Contains(string(body), "\nseg-0-v1.m4s\n") {
		t.Fatalf("index: %d %s", res.StatusCode, body)
	}
	for _, file := range []string{"init-v1.mp4", "seg-0-v1.m4s", "seg-1-v1.m4s"} {
		if res, body := ts.do(guest, "GET", vod+file, nil); res.StatusCode != 200 || res.Header.Get("Content-Type") != "video/mp4" || len(body) < 500 {
			t.Errorf("%s: %d %d bytes", file, res.StatusCode, len(body))
		}
	}
	// Nothing but the files of a playback, and never another camera's:
	// the camera is the one of the path that was checked.
	for _, p := range []string{
		vod + "..%2F..%2F..%2Feinfahrt%2Fstart%2F1%2Fend%2F2%2Fmaster.m3u8",
		vod + "master.m3u8%2F..%2F..%2Fx",
		vod + "config.yml",
		vod + ".m3u8",
		"/api/v1/nvr/cameras/haustuer/vod/abc/5/master.m3u8",
		"/api/v1/nvr/cameras/haustuer/vod/5/1/master.m3u8",
		"/api/v1/nvr/cameras/haustuer/vod/1/999999/master.m3u8",
		"/api/v1/nvr/cameras/haustuer/vod/1%2F2/3/master.m3u8",
	} {
		if res, _ := ts.do(guest, "GET", p, nil); res.StatusCode != 400 && res.StatusCode != 404 {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/nvr/cameras/haustuer%2F..%2Feinfahrt/vod/1/2/master.m3u8", nil); res.StatusCode != 404 {
		t.Errorf("camera with a path in it: %d", res.StatusCode)
	}
	// A still at a time; a time without recording has none.
	res, body = ts.do(guest, "GET", "/api/v1/nvr/cameras/haustuer/recordings/frame.jpg?at="+itoa(now-600)+"&height=180", nil)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" || len(body) < 100 {
		t.Errorf("frame: %d", res.StatusCode)
	}
	if res, body := ts.do(guest, "GET", "/api/v1/nvr/cameras/haustuer/recordings/frame.jpg?at=1000", nil); res.StatusCode != 404 || errorCode(t, body) != "nvr_no_picture" {
		t.Errorf("frame in a gap: %d %s", res.StatusCode, body)
	}
}

func itoa(n int64) string {
	data, _ := json.Marshal(n)
	return string(data)
}

func TestNVRFeed(t *testing.T) {
	recheck := consoleRecheck.Swap(int64(20 * time.Millisecond))
	t.Cleanup(func() { consoleRecheck.Store(recheck) })
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	guest := ts.guest("gast")
	grant(t, ts, "gast", map[string]string{"einfahrt": "live", "haustuer": "recordings"})
	read := func(conn *websocket.Conn, ctx context.Context) map[string]any {
		t.Helper()
		_, data, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("feed: %v", err)
		}
		var msg map[string]any
		json.Unmarshal(data, &msg)
		return msg
	}
	conn, ctx := liveDial(t, ts, guest, "/nvr/feed")
	// The owner's feed starts with what goes on at the front door; the
	// guest (recordings only there) gets no word of it.
	owner, octx := liveDial(t, ts, ts.client, "/nvr/feed")
	deadline := time.Now().Add(5 * time.Second)
	sawHello, sawDoor := false, false
	for !(sawHello && sawDoor) && time.Now().Before(deadline) {
		msg := read(owner, octx)
		sawHello = sawHello || (msg["t"] == "hello" && msg["running"] == true)
		sawDoor = sawDoor || (msg["t"] == "activity" && msg["camera"] == "haustuer" && msg["motion"] == true)
	}
	if !sawHello || !sawDoor {
		t.Fatalf("owner's feed: hello %v, front door %v", sawHello, sawDoor)
	}
	// The guest: hello, then only the drive.
	for {
		msg := read(conn, ctx)
		if msg["t"] == "hello" && msg["running"] == true {
			break
		}
		if msg["camera"] == "haustuer" {
			t.Fatalf("guest got %v", msg)
		}
	}
	// Logging out ends the feed.
	ts.do(guest, "POST", "/api/v1/logout", nil)
	for {
		msg := read(conn, ctx)
		if msg["camera"] == "haustuer" || (msg["event"] != nil && msg["event"].(map[string]any)["camera"] == "haustuer") {
			t.Fatalf("guest got %v", msg)
		}
		if msg["t"] == "bye" {
			if msg["reason"] != "logout" {
				t.Errorf("%v", msg)
			}
			break
		}
	}
	// Not logged in: no feed.
	if res, _ := ts.do(ts.newClient(), "GET", "/api/v1/nvr/feed", nil); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("anonymous: %d", res.StatusCode)
	}
}
