package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// nvrServer is a test server with the NVR of development mode: no
// Docker, a stand-in for Frigate, four demo cameras.
func nvrServer(t *testing.T) (*testServer, *nvr.Manager) {
	t.Helper()
	var manager *nvr.Manager
	ts := newTestServerWith(t, func(opts *Options) {
		manager = nvr.New(opts.Store)
		if err := manager.StartDev(t.Context()); err != nil {
			t.Fatal(err)
		}
		users := opts.Users
		manager.Access.Lookup = func(name string) (nvr.Account, bool) {
			user, ok := users.Get(name)
			return nvr.Account{Name: user.Name, Admin: user.Role.CanAdminister(), Disabled: user.Disabled}, ok
		}
		manager.Tick = 20 * time.Millisecond
		if err := manager.Load(); err != nil {
			t.Fatal(err)
		}
		opts.NVR = manager
	})
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() { manager.Run(ctx); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	return ts, manager
}

func (ts *testServer) guest(name string) *http.Client {
	ts.t.Helper()
	if _, err := ts.users.Create(name, testPassword, auth.RoleUser); err != nil {
		ts.t.Fatal(err)
	}
	client := ts.newClient()
	if res, _ := ts.do(client, "POST", "/api/v1/login", credentials{Name: name, Password: testPassword}); res.StatusCode != http.StatusOK {
		ts.t.Fatalf("login of %s: %d", name, res.StatusCode)
	}
	return client
}

// nvrRunning switches the NVR on and waits until Frigate "runs".
func nvrRunning(t *testing.T, ts *testServer) {
	t.Helper()
	if res, body := ts.request("POST", "/api/v1/nvr/enable", map[string]any{}); res.StatusCode != http.StatusOK {
		t.Fatalf("enable: %d %s", res.StatusCode, body)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		_, body := ts.request("GET", "/api/v1/nvr/status", nil)
		var status nvr.Status
		json.Unmarshal(body, &status)
		if status.State == nvr.PhaseRunning && status.Cameras.Online > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the NVR did not start: %s", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestNVRRoutesNeedTheRightRole(t *testing.T) {
	ts, _ := nvrServer(t)
	admin := []struct{ method, path string }{
		{"GET", "/nvr/settings"}, {"PUT", "/nvr/settings"}, {"POST", "/nvr/enable"}, {"POST", "/nvr/disable"}, {"GET", "/nvr/diagnosis"},
		{"POST", "/nvr/detector/retry"}, {"POST", "/nvr/cameras"}, {"POST", "/nvr/cameras/discover"}, {"POST", "/nvr/cameras/probe"},
		{"PUT", "/nvr/cameras/haustuer"}, {"DELETE", "/nvr/cameras/haustuer"}, {"GET", "/users/gast/cameras"}, {"PUT", "/users/gast/cameras"},
	}
	user := []struct{ method, path string }{
		{"GET", "/nvr/status"}, {"GET", "/nvr/cameras"}, {"GET", "/nvr/cameras/haustuer"}, {"GET", "/nvr/cameras/haustuer/snapshot.jpg"}, {"GET", "/nvr/cameras/haustuer/live"},
	}
	for _, route := range append(admin, user...) {
		if res, _ := ts.request(route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	ts.setup()
	guest := ts.guest("gast")
	for _, route := range admin {
		if res, _ := ts.do(guest, route.method, "/api/v1"+route.path, map[string]any{}); res.StatusCode != http.StatusForbidden {
			t.Errorf("user %s %s: %d", route.method, route.path, res.StatusCode)
		}
	}
	// No route of the NVR is open to API tokens.
	for _, route := range publicRoutes {
		if strings.Contains(route.Path, "/nvr") || strings.HasSuffix(route.Path, "/cameras") {
			t.Errorf("NVR route in the public API: %s %s", route.Method, route.Path)
		}
	}
}

func TestNVRStatusAndSettings(t *testing.T) {
	ts, manager := nvrServer(t)
	ts.setup()
	res, body := ts.request("GET", "/api/v1/nvr/status", nil)
	var status nvr.Status
	if err := json.Unmarshal(body, &status); err != nil || res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if status.Enabled || status.State != nvr.PhaseOff || status.Detector.Kind != "auto" || status.Detector.Active != "npu" || status.Hardware.Auto != "npu" ||
		len(status.Hardware.Devices) != 3 || status.Cameras.Total != 4 || status.FrigateVersion != nvr.FrigateVersion || status.Image.State == "" {
		t.Errorf("status: %s", body)
	}
	nvrRunning(t, ts)
	// The settings: a partial change keeps the rest.
	res, body = ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"storage": map[string]any{"limitGB": 200}, "privacyNoticeDismissed": true})
	var settings nvrSettingsView
	json.Unmarshal(body, &settings)
	if res.StatusCode != 200 || !settings.Enabled || settings.Storage.LimitGB != 200 || !settings.PrivacyNoticeDismissed || settings.Detector.Kind != "auto" || settings.RTSPPassword != "" {
		t.Errorf("settings: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"detector": map[string]any{"kind": "quantum"}}); res.StatusCode != 400 || errorCode(t, body) != "nvr_invalid" {
		t.Errorf("unknown detector: %d %s", res.StatusCode, body)
	}
	// The restream gets a login; only the settings (administrators) show it.
	_, body = ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"rtspOut": true})
	json.Unmarshal(body, &settings)
	if settings.RTSPUser != "titanos" || len(settings.RTSPPassword) < 16 {
		t.Errorf("restream login: %s", body)
	}
	if _, body := ts.request("GET", "/api/v1/nvr/status", nil); strings.Contains(string(body), settings.RTSPPassword) {
		t.Error("the status shows the restream password")
	}
	// The secret between titand and Frigate is in no answer, and no
	// request changes it.
	secret := manager.Settings().ProxySecret
	if len(secret) < 40 {
		t.Fatalf("secret %q", secret)
	}
	ts.request("PUT", "/api/v1/nvr/settings", map[string]any{"proxySecret": "chosen-by-the-page-0123456789"})
	if manager.Settings().ProxySecret != secret {
		t.Error("a request changed the secret")
	}
	for _, path := range []string{"/nvr/settings", "/nvr/status", "/nvr/diagnosis", "/nvr/cameras"} {
		if _, body := ts.request("GET", "/api/v1"+path, nil); strings.Contains(string(body), secret) || strings.Contains(string(body), "proxySecret") {
			t.Errorf("%s shows the secret: %s", path, body)
		}
	}
	// A normal account sees that the NVR runs and nothing of the machine.
	guest := ts.guest("gast")
	_, body = ts.do(guest, "GET", "/api/v1/nvr/status", nil)
	for _, secret := range []string{"hardware", "detector", "image", "storage", "volume", "error"} {
		if strings.Contains(string(body), `"`+secret+`"`) {
			t.Errorf("a user's status holds %q: %s", secret, body)
		}
	}
	var view nvrStatusView
	json.Unmarshal(body, &view)
	if !view.Enabled || view.Cameras.Total != 0 || !view.PrivacyNoticeDismissed {
		t.Errorf("user status: %s", body)
	}
	// Diagnosis, retry, switching off.
	res, body = ts.request("GET", "/api/v1/nvr/diagnosis", nil)
	var diagnosis nvr.Diagnosis
	json.Unmarshal(body, &diagnosis)
	if res.StatusCode != 200 || diagnosis.Container.State != "running" || diagnosis.Detector.Active != "npu" || len(diagnosis.Log) == 0 || len(diagnosis.Cameras) != 4 || strings.Contains(string(body), "demo@") {
		t.Errorf("diagnosis: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/nvr/detector/retry", nil); res.StatusCode != 200 {
		t.Errorf("retry: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/nvr/disable", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"enabled":false`) {
		t.Errorf("disable: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/nvr/detector/retry", nil); res.StatusCode != 409 || errorCode(t, body) != "nvr_disabled" {
		t.Errorf("retry while off: %d %s", res.StatusCode, body)
	}
}

func TestNVRCameras(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	_, body := ts.request("GET", "/api/v1/nvr/cameras", nil)
	var list struct {
		Cameras []nvr.CameraView `json:"cameras"`
		Objects []string         `json:"objects"`
	}
	json.Unmarshal(body, &list)
	if len(list.Cameras) != 4 || list.Cameras[0].ID != "haustuer" || !list.Cameras[0].Source.HasPassword || len(list.Objects) == 0 {
		t.Fatalf("cameras: %s", body)
	}
	// Never a password, in whatever form.
	if strings.Contains(string(body), `"password"`) || strings.Contains(string(body), `"demo"`) {
		t.Errorf("a password in the list: %s", body)
	}
	// A pasted address with its login: the login goes to its fields.
	res, body := ts.request("POST", "/api/v1/nvr/cameras", map[string]any{
		"name": "Carport", "source": map[string]any{"kind": "rtsp", "mainURL": "rtsp://admin:s3cr3t@192.168.1.90:554/h264"},
	})
	var added nvr.CameraView
	json.Unmarshal(body, &added)
	if res.StatusCode != 201 || added.ID != "carport" || added.Source.User != "admin" || !added.Source.HasPassword || added.Source.MainURL != "rtsp://192.168.1.90:554/h264" ||
		added.Source.Host != "192.168.1.90" || added.Record.Mode != "events" || !added.Detect.Enabled || added.Audio.Enabled || strings.Contains(string(body), "s3cr3t") {
		t.Fatalf("added: %d %s", res.StatusCode, body)
	}
	// A partial change keeps everything else, the password too.
	res, body = ts.request("PUT", "/api/v1/nvr/cameras/carport", map[string]any{"name": "Carport Süd", "record": map[string]any{"mode": "motion", "retainDays": 30, "preSeconds": 5, "postSeconds": 5}})
	var changed nvr.CameraView
	json.Unmarshal(body, &changed)
	if res.StatusCode != 200 || changed.Name != "Carport Süd" || changed.Record.Mode != "motion" || !changed.Source.HasPassword || changed.Source.User != "admin" {
		t.Errorf("changed: %d %s", res.StatusCode, body)
	}
	for name, tc := range map[string]struct {
		method, path string
		body         map[string]any
		status       int
		code         string
	}{
		"same id":            {"POST", "/nvr/cameras", map[string]any{"id": "carport", "name": "X", "source": map[string]any{"kind": "rtsp", "mainURL": "rtsp://192.168.1.91/x"}}, 409, "nvr_camera_exists"},
		"no stream":          {"POST", "/nvr/cameras", map[string]any{"name": "X"}, 400, "nvr_invalid"},
		"public address":     {"POST", "/nvr/cameras", map[string]any{"name": "X", "source": map[string]any{"kind": "rtsp", "mainURL": "rtsp://203.0.113.4/x"}}, 409, "nvr_target_public"},
		"this NAS":           {"POST", "/nvr/cameras", map[string]any{"name": "X", "source": map[string]any{"kind": "rtsp", "mainURL": "rtsp://127.0.0.1:8554/x"}}, 400, "nvr_invalid"},
		"unknown camera":     {"PUT", "/nvr/cameras/nope", map[string]any{"name": "X"}, 404, "nvr_camera_not_found"},
		"password elsewhere": {"PUT", "/nvr/cameras/carport", map[string]any{"source": map[string]any{"kind": "rtsp", "mainURL": "rtsp://192.168.1.99/x", "user": "admin"}}, 400, "nvr_invalid"},
		"delete unknown":     {"DELETE", "/nvr/cameras/nope", nil, 404, "nvr_camera_not_found"},
	} {
		if res, body := ts.request(tc.method, "/api/v1"+tc.path, tc.body); res.StatusCode != tc.status || errorCode(t, body) != tc.code {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
	}
	// With the administrator's yes a camera outside the home network is taken.
	if res, body := ts.request("POST", "/api/v1/nvr/cameras", map[string]any{"name": "Ferienhaus", "source": map[string]any{"kind": "rtsp", "mainURL": "rtsp://203.0.113.4/x", "allowPublic": true}}); res.StatusCode != 201 {
		t.Errorf("confirmed public address: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("DELETE", "/api/v1/nvr/cameras/carport", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("delete: %d", res.StatusCode)
	}
	// The search and the check of a camera (answers of development mode).
	res, body = ts.request("POST", "/api/v1/nvr/cameras/discover", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"address":"192.168.1.65"`) || !strings.Contains(string(body), `"known":true`) {
		t.Errorf("discover: %d %s", res.StatusCode, body)
	}
	res, body = ts.request("POST", "/api/v1/nvr/cameras/probe", map[string]any{"kind": "onvif", "host": "192.168.1.65", "user": "admin", "password": "x"})
	var probe nvr.ProbeResult
	json.Unmarshal(body, &probe)
	if res.StatusCode != 200 || probe.Outcome != "ok" || probe.MainURL == "" || len(probe.Streams) != 2 || !probe.PTZ || probe.Device == nil {
		t.Errorf("probe: %d %s", res.StatusCode, body)
	}
	if _, body := ts.request("POST", "/api/v1/nvr/cameras/probe", map[string]any{"kind": "rtsp", "mainURL": "rtsp://192.168.1.65/x", "user": "admin", "password": "wrong"}); !strings.Contains(string(body), `"outcome":"unauthorized"`) {
		t.Errorf("wrong password: %s", body)
	}
	// The stored login of a camera is only used for that camera.
	if res, body := ts.request("POST", "/api/v1/nvr/cameras/probe", map[string]any{"kind": "rtsp", "camera": "haustuer", "mainURL": "rtsp://192.168.1.250/x"}); res.StatusCode != 400 {
		t.Errorf("stored login for another address: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/nvr/cameras/probe", map[string]any{"kind": "rtsp", "camera": "haustuer", "mainURL": "rtsp://192.168.1.61:554/main"}); res.StatusCode != 200 || !strings.Contains(string(body), `"outcome":"ok"`) {
		t.Errorf("stored login: %d %s", res.StatusCode, body)
	}
}

func TestNVRCameraRights(t *testing.T) {
	ts, manager := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	guest := ts.guest("gast")
	// A new account sees no camera.
	if _, body := ts.do(guest, "GET", "/api/v1/nvr/cameras", nil); strings.TrimSpace(string(body)) != `{"cameras":[]}` {
		t.Errorf("new account: %s", body)
	}
	for _, path := range []string{"/nvr/cameras/haustuer", "/nvr/cameras/haustuer/snapshot.jpg"} {
		if res, body := ts.do(guest, "GET", "/api/v1"+path, nil); res.StatusCode != 404 || errorCode(t, body) != "nvr_camera_not_found" {
			t.Errorf("%s without right: %d %s", path, res.StatusCode, body)
		}
	}
	res, body := ts.request("GET", "/api/v1/users/gast/cameras", nil)
	var rights nvrUserCameras
	json.Unmarshal(body, &rights)
	if res.StatusCode != 200 || rights.Admin || len(rights.Cameras) != 4 || rights.Cameras[0].Live || rights.Cameras[0].Name != "Haustür" {
		t.Errorf("rights of a new account: %s", body)
	}
	// Live on the front door, recordings only of the drive.
	res, body = ts.request("PUT", "/api/v1/users/gast/cameras", map[string]any{"cameras": map[string]any{
		"haustuer": map[string]any{"live": true}, "einfahrt": map[string]any{"recordings": true},
	}})
	json.Unmarshal(body, &rights)
	if res.StatusCode != 200 || !rights.Cameras[0].Live || rights.Cameras[0].Recordings || rights.Cameras[1].Live || !rights.Cameras[1].Recordings {
		t.Fatalf("set rights: %d %s", res.StatusCode, body)
	}
	_, body = ts.do(guest, "GET", "/api/v1/nvr/cameras", nil)
	var list struct {
		Cameras []nvrCameraBrief `json:"cameras"`
	}
	json.Unmarshal(body, &list)
	if len(list.Cameras) != 2 || list.Cameras[0].ID != "haustuer" || !list.Cameras[0].Rights.Live || list.Cameras[0].Rights.Recordings || !list.Cameras[0].State.Online {
		t.Errorf("cameras of the account: %s", body)
	}
	// Nothing of where the camera is or how it is reached.
	for _, secret := range []string{`"source"`, "192.168", "rtsp://", "viewer", `"detect"`, `"record"`, `"notify"`} {
		if strings.Contains(string(body), secret) {
			t.Errorf("a user's camera list holds %q: %s", secret, body)
		}
	}
	if res, body := ts.do(guest, "GET", "/api/v1/nvr/cameras/haustuer", nil); res.StatusCode != 200 || strings.Contains(string(body), "source") {
		t.Errorf("own camera: %d %s", res.StatusCode, body)
	}
	if _, body := ts.do(guest, "GET", "/api/v1/nvr/status", nil); !strings.Contains(string(body), `"total":2`) {
		t.Errorf("counts of a user: %s", body)
	}
	// The picture needs "live".
	res, body = ts.do(guest, "GET", "/api/v1/nvr/cameras/haustuer/snapshot.jpg?height=180", nil)
	if res.StatusCode != 200 || res.Header.Get("Content-Type") != "image/jpeg" || res.Header.Get("Cache-Control") != "no-store" || len(body) < 100 {
		t.Errorf("snapshot: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/nvr/cameras/einfahrt/snapshot.jpg", nil); res.StatusCode != 404 {
		t.Errorf("snapshot with recordings only: %d", res.StatusCode)
	}
	if res, body := ts.do(guest, "GET", "/api/v1/nvr/cameras/haustuer/snapshot.jpg?height=9999", nil); res.StatusCode != 400 {
		t.Errorf("height: %d %s", res.StatusCode, body)
	}
	// A camera that is switched off has no picture.
	if res, body := ts.request("GET", "/api/v1/nvr/cameras/garage/snapshot.jpg", nil); res.StatusCode != 409 || errorCode(t, body) != "nvr_not_running" {
		t.Errorf("disabled camera: %d %s", res.StatusCode, body)
	}
	// Refused: administrators, unknown accounts and cameras.
	owner := ownerName(t, ts)
	for name, tc := range map[string]struct {
		path   string
		body   map[string]any
		status int
		code   string
	}{
		"administrator":  {"/users/" + owner + "/cameras", map[string]any{"cameras": map[string]any{}}, 400, "nvr_access_admin"},
		"unknown user":   {"/users/niemand/cameras", map[string]any{"cameras": map[string]any{}}, 404, "user_not_found"},
		"unknown camera": {"/users/gast/cameras", map[string]any{"cameras": map[string]any{"keller": map[string]any{"live": true}}}, 400, "nvr_invalid"},
	} {
		if res, body := ts.request("PUT", "/api/v1"+tc.path, tc.body); res.StatusCode != tc.status || errorCode(t, body) != tc.code {
			t.Errorf("%s: %d %s", name, res.StatusCode, body)
		}
	}
	if _, body := ts.request("GET", "/api/v1/users/"+owner+"/cameras", nil); !strings.Contains(string(body), `"admin":true`) || strings.Contains(string(body), `"live":false`) {
		t.Errorf("rights of the owner: %s", body)
	}
	// Deleting the account takes it off the cameras: a new account of
	// the same name starts with nothing.
	if res, _ := ts.request("DELETE", "/api/v1/users/gast", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete account: %d", res.StatusCode)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	if manager.Access.Sees("gast", "haustuer") || manager.Access.Sees("gast", "einfahrt") {
		t.Error("a deleted account keeps its cameras")
	}
}

// liveMessages opens the live stream and returns the text messages until
// the server closes it or stop fires.
func liveDial(t *testing.T, ts *testServer, client *http.Client, path string) (*websocket.Conn, context.Context) {
	t.Helper()
	base, _ := url.Parse(ts.url)
	header := http.Header{}
	for _, cookie := range client.Jar.Cookies(base) {
		header.Add("Cookie", cookie.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.url, "http")+"/api/v1"+path, &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn, ctx
}

func liveMessage(t *testing.T, conn *websocket.Conn, ctx context.Context) map[string]string {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("live stream: %v", err)
	}
	var msg map[string]string
	json.Unmarshal(data, &msg)
	return msg
}

func TestNVRLive(t *testing.T) {
	recheck := consoleRecheck.Swap(int64(20 * time.Millisecond))
	t.Cleanup(func() { consoleRecheck.Store(recheck) })
	ts, manager := nvrServer(t)
	ts.setup()
	guest := ts.guest("gast")
	// The NVR is off.
	conn, ctx := liveDial(t, ts, ts.client, "/nvr/cameras/haustuer/live")
	if msg := liveMessage(t, conn, ctx); msg["t"] != "error" || msg["code"] != "nvr_not_running" {
		t.Errorf("while off: %v", msg)
	}
	nvrRunning(t, ts)
	// Without the right the camera does not exist.
	conn, ctx = liveDial(t, ts, guest, "/nvr/cameras/haustuer/live")
	if msg := liveMessage(t, conn, ctx); msg["code"] != "nvr_camera_not_found" {
		t.Errorf("without right: %v", msg)
	}
	// Recordings alone are not live.
	ts.request("PUT", "/api/v1/users/gast/cameras", map[string]any{"cameras": map[string]any{"haustuer": map[string]any{"recordings": true}}})
	conn, ctx = liveDial(t, ts, guest, "/nvr/cameras/haustuer/live")
	if msg := liveMessage(t, conn, ctx); msg["code"] != "nvr_camera_not_found" {
		t.Errorf("recordings only: %v", msg)
	}
	// With the right the stream of Frigate is passed through, both ways.
	var asked, secret string
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked, secret = r.URL.String(), r.Header.Get("X-Proxy-Secret")
		up, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer up.CloseNow()
		for {
			kind, data, err := up.Read(r.Context())
			if err != nil {
				return
			}
			if err := up.Write(r.Context(), kind, append([]byte("frigate:"), data...)); err != nil {
				return
			}
		}
	})
	dial := nvrLiveDial
	t.Cleanup(func() { nvrLiveDial = dial })
	upstream := newLocalServer(t, echo)
	nvrLiveDial = func(ctx context.Context, address string, header http.Header) (*websocket.Conn, error) {
		u, _ := url.Parse(address)
		return dial(ctx, "ws"+strings.TrimPrefix(upstream, "http")+u.RequestURI(), header)
	}
	ts.request("PUT", "/api/v1/users/gast/cameras", map[string]any{"cameras": map[string]any{"haustuer": map[string]any{"live": true}}})
	conn, ctx = liveDial(t, ts, guest, "/nvr/cameras/haustuer/live?stream=sub")
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"type":"mse"}`)); err != nil {
		t.Fatal(err)
	}
	if kind, data, err := conn.Read(ctx); err != nil || kind != websocket.MessageText || string(data) != `frigate:{"type":"mse"}` {
		t.Fatalf("relay: %v %s %v", kind, data, err)
	}
	if err := conn.Write(ctx, websocket.MessageBinary, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if kind, data, err := conn.Read(ctx); err != nil || kind != websocket.MessageBinary || len(data) != 11 {
		t.Fatalf("binary relay: %v %v %v", kind, data, err)
	}
	if asked != "/live/mse/api/ws?src=haustuer_sub" {
		t.Errorf("asked Frigate for %q", asked)
	}
	// Frigate's port 8971 answers nothing without the secret.
	if want := manager.Settings().ProxySecret; secret == "" || secret != want {
		t.Errorf("secret %q sent to Frigate, want %q", secret, want)
	}
	// The right is taken away: the open stream ends within the recheck.
	ts.request("PUT", "/api/v1/users/gast/cameras", map[string]any{"cameras": map[string]any{"haustuer": map[string]any{"live": false}}})
	if msg := liveMessage(t, conn, ctx); msg["t"] != "bye" || msg["reason"] != "forbidden" {
		t.Errorf("after losing the right: %v", msg)
	}
	// Logging out ends it as well.
	conn, ctx = liveDial(t, ts, ts.client, "/nvr/cameras/haustuer/live")
	if err := conn.Write(ctx, websocket.MessageText, []byte(`x`)); err != nil {
		t.Fatal(err)
	}
	if _, data, err := conn.Read(ctx); err != nil || string(data) != "frigate:x" {
		t.Fatalf("owner relay: %s %v", data, err)
	}
	if res, _ := ts.request("POST", "/api/v1/logout", nil); res.StatusCode >= 400 {
		t.Fatalf("logout: %d", res.StatusCode)
	}
	if msg := liveMessage(t, conn, ctx); msg["t"] != "bye" || msg["reason"] != "logout" {
		t.Errorf("after logout: %v", msg)
	}
}

func TestNVRLiveRefusesOtherSites(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	base, _ := url.Parse(ts.url)
	header := http.Header{"Origin": {"https://evil.example"}}
	for _, cookie := range ts.client.Jar.Cookies(base) {
		header.Add("Cookie", cookie.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, res, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.url, "http")+"/api/v1/nvr/cameras/haustuer/live", &websocket.DialOptions{HTTPHeader: header})
	if err == nil {
		conn.CloseNow()
		t.Fatal("a page of another site opened the live stream")
	}
	if res != nil && res.StatusCode != http.StatusForbidden {
		t.Errorf("status %d", res.StatusCode)
	}
}

func TestNVRWithoutManager(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("GET", "/api/v1/nvr/status", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"state":"off"`) {
		t.Errorf("status: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/nvr/cameras", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"cameras":[]`) {
		t.Errorf("cameras: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/nvr/enable", map[string]any{}); res.StatusCode != 404 || errorCode(t, body) != "nvr_unavailable" {
		t.Errorf("enable: %d %s", res.StatusCode, body)
	}
	// Deleting an account works without an NVR.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	if res, _ := ts.request("DELETE", "/api/v1/users/gast", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("delete account: %d", res.StatusCode)
	}
}

// newLocalServer serves handler on a port of this machine until the test ends.
func newLocalServer(t *testing.T, handler http.Handler) string {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv.URL
}
