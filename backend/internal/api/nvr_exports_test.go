package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

func TestNVRExportsBelongToTheirAccount(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	anna, ben := ts.guest("anna"), ts.guest("ben")
	grant(t, ts, "anna", map[string]string{"haustuer": "recordings", "einfahrt": "live"})
	grant(t, ts, "ben", map[string]string{"haustuer": "both"})
	now := float64(time.Now().Unix())
	var list struct {
		Exports []nvr.Export `json:"exports"`
	}
	// The owner sees the three examples, the others nothing.
	if getJSON(t, ts, ts.client, "/nvr/exports", &list); len(list.Exports) != 3 {
		t.Fatalf("owner: %d exports", len(list.Exports))
	}
	if getJSON(t, ts, anna, "/nvr/exports", &list); len(list.Exports) != 0 {
		t.Errorf("anna: %d exports", len(list.Exports))
	}
	// Making one needs "recordings" on the camera.
	for name, body := range map[string]map[string]any{
		"live only": {"camera": "einfahrt", "from": now - 600, "to": now - 500},
		"no right":  {"camera": "garten", "from": now - 600, "to": now - 500},
		"unknown":   {"camera": "keller", "from": now - 600, "to": now - 500},
	} {
		if res, data := ts.do(anna, "POST", "/api/v1/nvr/exports", body); res.StatusCode != 404 || errorCode(t, data) != "nvr_camera_not_found" {
			t.Errorf("%s: %d %s", name, res.StatusCode, data)
		}
	}
	if res, data := ts.do(anna, "POST", "/api/v1/nvr/exports", map[string]any{"camera": "haustuer", "from": now - 900000, "to": now - 899000}); res.StatusCode != 409 || errorCode(t, data) != "nvr_export_failed" {
		t.Errorf("nothing recorded: %d %s", res.StatusCode, data)
	}
	res, data := ts.do(anna, "POST", "/api/v1/nvr/exports", map[string]any{"camera": "haustuer", "from": now - 600, "to": now - 500, "name": "Paketbote"})
	var made nvr.Export
	json.Unmarshal(data, &made)
	if res.StatusCode != 201 || made.State != "running" || made.Owner != "anna" || made.Name != "Paketbote" {
		t.Fatalf("create: %d %s", res.StatusCode, data)
	}
	if res, data := ts.do(anna, "GET", "/api/v1/nvr/exports/"+made.ID+"/download", nil); res.StatusCode != 409 || errorCode(t, data) != "nvr_export_running" {
		t.Errorf("download while running: %d %s", res.StatusCode, data)
	}
	// Asking again shows it done.
	var export nvr.Export
	for range 5 {
		getJSON(t, ts, anna, "/nvr/exports/"+made.ID, &export)
	}
	if export.State != "done" || export.SizeBytes == nil || export.From != now-600 {
		t.Fatalf("export: %+v", export)
	}
	// Ben has the camera but not the export; the owner has all.
	for _, path := range []string{"", "/download", "/thumbnail.jpg"} {
		if res, data := ts.do(ben, "GET", "/api/v1/nvr/exports/"+made.ID+path, nil); res.StatusCode != 404 || errorCode(t, data) != "nvr_export_not_found" {
			t.Errorf("ben %s: %d %s", path, res.StatusCode, data)
		}
	}
	if res, _ := ts.do(ben, "DELETE", "/api/v1/nvr/exports/"+made.ID, nil); res.StatusCode != 404 {
		t.Errorf("ben deletes: %d", res.StatusCode)
	}
	if getJSON(t, ts, ts.client, "/nvr/exports", &list); len(list.Exports) != 4 {
		t.Errorf("owner: %d exports", len(list.Exports))
	}
	// Download: an attachment under the export's name, ranges work.
	req, _ := http.NewRequest("GET", ts.url+"/api/v1/nvr/exports/"+made.ID+"/download", nil)
	req.Header.Set("Range", "bytes=0-99")
	part, err := anna.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	part.Body.Close()
	if part.StatusCode != 206 || part.ContentLength != 100 || part.Header.Get("Content-Disposition") != `attachment; filename="Paketbote.mp4"` {
		t.Errorf("download: %d %v", part.StatusCode, part.Header)
	}
	if res, _ := ts.do(anna, "GET", "/api/v1/nvr/exports/"+made.ID+"/thumbnail.jpg", nil); res.StatusCode != 200 {
		t.Errorf("thumbnail: %d", res.StatusCode)
	}
	if res, data := ts.do(anna, "PUT", "/api/v1/nvr/exports/"+made.ID, map[string]any{"name": "Paket von heute"}); res.StatusCode != 200 || !strings.Contains(string(data), "Paket von heute") {
		t.Errorf("rename: %d %s", res.StatusCode, data)
	}
	if res, data := ts.do(anna, "PUT", "/api/v1/nvr/exports/"+made.ID, map[string]any{"name": ""}); res.StatusCode != 400 {
		t.Errorf("empty name: %d %s", res.StatusCode, data)
	}
	// Anna loses the camera: her export of it is no longer handed out.
	grant(t, ts, "anna", map[string]string{"haustuer": "none"})
	if res, _ := ts.do(anna, "GET", "/api/v1/nvr/exports/"+made.ID+"/download", nil); res.StatusCode != 404 {
		t.Errorf("after losing the camera: %d", res.StatusCode)
	}
	grant(t, ts, "anna", map[string]string{"haustuer": "recordings"})
	if res, _ := ts.do(anna, "DELETE", "/api/v1/nvr/exports/"+made.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", res.StatusCode)
	}
	if res, _ := ts.do(anna, "GET", "/api/v1/nvr/exports/"+made.ID, nil); res.StatusCode != 404 {
		t.Errorf("deleted: %d", res.StatusCode)
	}
	// The storage figures count exports.
	var status nvr.Status
	getJSON(t, ts, ts.client, "/nvr/status", &status)
	if status.Storage.ExportsBytes == nil || status.Storage.RecordingsBytes == nil || status.Storage.SpanDays == nil || status.Storage.Cap != "off" {
		t.Errorf("storage: %+v", status.Storage)
	}
}

func TestNVRPTZNeedsLive(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	guest := ts.guest("gast")
	grant(t, ts, "gast", map[string]string{"garten": "recordings", "haustuer": "live"})
	if res, _ := ts.do(guest, "GET", "/api/v1/nvr/cameras/garten/ptz", nil); res.StatusCode != 404 {
		t.Errorf("recordings only: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "POST", "/api/v1/nvr/cameras/garten/ptz", map[string]any{"action": "move_up"}); res.StatusCode != 404 {
		t.Errorf("recordings only: %d", res.StatusCode)
	}
	var info nvr.PTZInfo
	if getJSON(t, ts, guest, "/nvr/cameras/haustuer/ptz", &info); info.Supported {
		t.Errorf("front door: %+v", info)
	}
	if res, body := ts.do(guest, "POST", "/api/v1/nvr/cameras/haustuer/ptz", map[string]any{"action": "move_up"}); res.StatusCode != 409 || errorCode(t, body) != "nvr_ptz_unsupported" {
		t.Errorf("camera that cannot move: %d %s", res.StatusCode, body)
	}
	grant(t, ts, "gast", map[string]string{"garten": "live"})
	if getJSON(t, ts, guest, "/nvr/cameras/garten/ptz", &info); !info.Supported || len(info.Presets) != 3 {
		t.Errorf("garden: %+v", info)
	}
	// The feed to Frigate may need a moment after the start.
	deadline := time.Now().Add(5 * time.Second)
	for {
		res, body := ts.do(guest, "POST", "/api/v1/nvr/cameras/garten/ptz", map[string]any{"action": "preset", "preset": "tor"})
		if res.StatusCode == http.StatusNoContent {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("preset: %d %s", res.StatusCode, body)
		}
		time.Sleep(150 * time.Millisecond)
	}
	if res, body := ts.do(guest, "POST", "/api/v1/nvr/cameras/garten/ptz", map[string]any{"action": "fly"}); res.StatusCode != 400 || errorCode(t, body) != "nvr_invalid" {
		t.Errorf("unknown action: %d %s", res.StatusCode, body)
	}
	limited := 0
	for range 20 {
		if res, body := ts.do(guest, "POST", "/api/v1/nvr/cameras/garten/ptz", map[string]any{"action": "stop"}); res.StatusCode == http.StatusTooManyRequests && errorCode(t, body) == "rate_limited" {
			limited++
		}
	}
	if limited == 0 {
		t.Error("no limit on PTZ commands")
	}
	// Talking back: the right "live" and a camera set up for it.
	conn, ctx := liveDial(t, ts, guest, "/nvr/cameras/haustuer/talk")
	if err := conn.Write(ctx, 1, []byte(`{"type":"webrtc/offer","value":"v=0"}`)); err != nil {
		t.Fatal(err)
	}
	if _, data, err := conn.Read(ctx); err != nil || !strings.Contains(string(data), "haustuer_twoway") {
		t.Errorf("talk: %s %v", data, err)
	}
	conn, ctx = liveDial(t, ts, guest, "/nvr/cameras/einfahrt/talk")
	if _, _, err := conn.Read(ctx); err == nil || !strings.Contains(err.Error(), "nvr_camera_not_found") {
		t.Errorf("talk without right: %v", err)
	}
	conn, ctx = liveDial(t, ts, guest, "/nvr/cameras/garten/talk")
	if _, _, err := conn.Read(ctx); err == nil || !strings.Contains(err.Error(), "nvr_not_running") {
		t.Errorf("talk with a camera that has none: %v", err)
	}
}

func TestNVRFacesAreForAdministrators(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	guest := ts.guest("gast")
	grant(t, ts, "gast", map[string]string{"haustuer": "both"})
	for _, route := range []struct{ method, path string }{
		{"GET", "/nvr/faces"}, {"POST", "/nvr/faces"}, {"PUT", "/nvr/faces/Anna"}, {"DELETE", "/nvr/faces/Anna"}, {"POST", "/nvr/faces/Anna/images"},
		{"GET", "/nvr/faces/Anna/images/a.webp"}, {"DELETE", "/nvr/faces/Anna/images/a.webp"}, {"GET", "/nvr/face-attempts/a.webp"},
		{"POST", "/nvr/face-attempts/a.webp/assign"}, {"DELETE", "/nvr/face-attempts/a.webp"}, {"GET", "/nvr/plates/recognized"},
	} {
		if res, _ := ts.do(guest, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("user %s %s: %d", route.method, route.path, res.StatusCode)
		}
		if res, _ := ts.do(ts.newClient(), route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	var faces nvr.Faces
	if getJSON(t, ts, ts.client, "/nvr/faces", &faces); !faces.Enabled || len(faces.Faces) != 2 || len(faces.Attempts) != 2 {
		t.Fatalf("%+v", faces)
	}
	res, body := ts.request("POST", "/api/v1/nvr/faces", map[string]any{"name": "Clara"})
	if res.StatusCode != 201 || !strings.Contains(string(body), `"name":"Clara"`) {
		t.Errorf("create: %d %s", res.StatusCode, body)
	}
	for name, tc := range map[string]struct {
		method, path string
		body         map[string]any
		status       int
		code         string
	}{
		"exists":       {"POST", "/nvr/faces", map[string]any{"name": "Anna"}, 409, "nvr_face_exists"},
		"bad name":     {"POST", "/nvr/faces", map[string]any{"name": "../x"}, 400, "nvr_invalid"},
		"rename gone":  {"PUT", "/nvr/faces/Nobody", map[string]any{"name": "X"}, 404, "nvr_face_not_found"},
		"delete gone":  {"DELETE", "/nvr/faces/Nobody", nil, 404, "nvr_face_not_found"},
		"image gone":   {"GET", "/nvr/faces/Anna/images/nope.webp", nil, 404, "nvr_face_not_found"},
		"attempt gone": {"POST", "/nvr/face-attempts/nope.webp/assign", map[string]any{"name": "Anna"}, 404, "nvr_face_not_found"},
	} {
		if res, body := ts.request(tc.method, "/api/v1"+tc.path, tc.body); res.StatusCode != tc.status || errorCode(t, body) != tc.code {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
	}
	upload := func(size int, kind string) (*http.Response, []byte) {
		req, _ := http.NewRequest("POST", ts.url+"/api/v1/nvr/faces/Clara/images", bytes.NewReader(make([]byte, size)))
		req.Header.Set("Content-Type", kind)
		req.Header.Set("Sec-Fetch-Site", "same-origin")
		res, err := ts.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var buf bytes.Buffer
		buf.ReadFrom(res.Body)
		return res, buf.Bytes()
	}
	if res, body := upload(5000, "image/jpeg"); res.StatusCode != 201 || !strings.Contains(string(body), "Clara-") {
		t.Errorf("upload: %d %s", res.StatusCode, body)
	}
	if res, body := upload(100, "image/jpeg"); res.StatusCode != 400 || errorCode(t, body) != "nvr_face_rejected" {
		t.Errorf("no face: %d %s", res.StatusCode, body)
	}
	if res, body := upload(5000, "application/pdf"); res.StatusCode != 400 || errorCode(t, body) != "nvr_invalid" {
		t.Errorf("a PDF: %d %s", res.StatusCode, body)
	}
	if res, _ := upload(nvr.MaxFaceImage+10, "image/jpeg"); res.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: %d", res.StatusCode)
	}
	res, body = ts.request("GET", "/api/v1/nvr/faces/Anna/images/"+faces.Faces[0].Images[0], nil)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" || res.Header.Get("Cache-Control") != "private, no-store" || len(body) < 100 {
		t.Errorf("image: %d %v", res.StatusCode, res.Header)
	}
	attempt := faces.Attempts[0].File
	if res, _ := ts.request("GET", "/api/v1/nvr/face-attempts/"+attempt, nil); res.StatusCode != 200 {
		t.Errorf("attempt picture: %d", res.StatusCode)
	}
	if res, body := ts.request("POST", "/api/v1/nvr/face-attempts/"+attempt+"/assign", map[string]any{"name": "Clara"}); res.StatusCode != 200 {
		t.Errorf("assign: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("DELETE", "/api/v1/nvr/face-attempts/"+faces.Attempts[1].File, nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("discard: %d", res.StatusCode)
	}
	if res, _ := ts.request("PUT", "/api/v1/nvr/faces/Clara", map[string]any{"name": "Klara"}); res.StatusCode != 200 {
		t.Errorf("rename: %d", res.StatusCode)
	}
	getJSON(t, ts, ts.client, "/nvr/faces", &faces)
	if len(faces.Faces) != 3 || faces.Faces[2].Name != "Klara" || len(faces.Faces[2].Images) != 2 || len(faces.Attempts) != 0 {
		t.Errorf("%+v", faces)
	}
	if res, _ := ts.request("DELETE", "/api/v1/nvr/faces/Klara", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", res.StatusCode)
	}
	// Plates: the ones read, and the known ones in the settings.
	var plates struct {
		Plates []nvr.RecognizedPlate `json:"plates"`
	}
	if getJSON(t, ts, ts.client, "/nvr/plates/recognized", &plates); len(plates.Plates) != 2 || plates.Plates[0].Count != 1 {
		t.Errorf("%+v", plates)
	}
	res, body = ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"plates": []map[string]any{{"name": "Annas Auto", "plates": []string{"M-AB 1234", "MAB12*"}}}})
	if res.StatusCode != 200 || !strings.Contains(string(body), `"plates":[{"name":"Annas Auto","plates":["M-AB 1234","MAB12*"]}]`) {
		t.Errorf("known plates: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"plates": []map[string]any{{"name": "X", "plates": []string{"A[1"}}}}); res.StatusCode != 400 {
		t.Errorf("bad pattern: %d %s", res.StatusCode, body)
	}
	// A change that names no plates keeps them.
	if _, body := ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"privacyNoticeDismissed": true}); !strings.Contains(string(body), "Annas Auto") {
		t.Errorf("plates lost: %s", body)
	}
}
