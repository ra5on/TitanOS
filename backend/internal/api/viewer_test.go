package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/viewer"
)

type fakeViewerOpen struct {
	err   error
	calls atomic.Int32
}

func (f *fakeViewerOpen) Open(context.Context, string, string) (*viewer.Lease, error) {
	f.calls.Add(1)
	return nil, f.err
}

// viewerMessages opens the stream with the cookies of client and returns
// the messages until the server closes it.
func viewerMessages(t *testing.T, ts *testServer, client *http.Client, app string) []map[string]any {
	t.Helper()
	base, _ := url.Parse(ts.url)
	header := http.Header{}
	for _, cookie := range client.Jar.Cookies(base) {
		header.Add("Cookie", cookie.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(ts.url, "http")+"/api/v1/viewer/"+app+"/stream", &websocket.DialOptions{HTTPHeader: header})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var out []map[string]any
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return out
		}
		var msg map[string]any
		json.Unmarshal(data, &msg)
		out = append(out, msg)
	}
}

func lastCode(msgs []map[string]any) string {
	if len(msgs) == 0 {
		return ""
	}
	code, _ := msgs[len(msgs)-1]["code"].(string)
	return code
}

func TestViewerStreamChecksRightAndMemory(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	fake := &fakeViewerOpen{err: viewer.ErrMemory}
	ts.server.viewerOpen = fake
	// Without a right on the app (no app rights: administrators only) the
	// view never starts.
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if code := lastCode(viewerMessages(t, ts, guest, "immich")); code != "app_forbidden" {
		t.Fatalf("guest: %q", code)
	}
	if fake.calls.Load() != 0 {
		t.Fatal("opened without the right")
	}
	msgs := viewerMessages(t, ts, ts.client, "immich")
	if len(msgs) != 2 || msgs[0]["t"] != "starting" || msgs[1]["t"] != "error" || lastCode(msgs) != "viewer_memory" {
		t.Fatalf("owner without memory: %v", msgs)
	}
	for err, want := range map[error]string{viewer.ErrLimit: "viewer_limit", viewer.ErrHostNetwork: "viewer_hostnet", viewer.ErrImageNotReady: "viewer_image", viewer.ErrNoWebUI: "viewer_no_webui"} {
		fake.err = err
		if code := lastCode(viewerMessages(t, ts, ts.client, "immich")); code != want {
			t.Errorf("%v: %q", err, code)
		}
	}
	// Not logged in: no WebSocket at all.
	res, _ := ts.do(ts.newClient(), "GET", "/api/v1/viewer/immich/stream", nil)
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}
}

func TestViewerSettingsAndStatus(t *testing.T) {
	ts := newTestServer(t)
	m := viewer.New(ts.store)
	ts.server.opts.Viewer = m
	ts.setup()
	if res, body := ts.request("PUT", "/api/v1/viewer/settings", map[string]any{"gpu": "/dev/sda", "quality": "auto", "fps": 60}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "viewer_settings_invalid" {
		t.Fatalf("arbitrary device: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/viewer/settings", map[string]any{"gpu": "off", "quality": "low", "fps": 30, "audio": false}); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"fps":30`) {
		t.Fatalf("settings: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/viewer/status", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"image":{"state":"missing"`) || !strings.Contains(string(body), `"gpu":"cpu"`) {
		t.Fatalf("status: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("GET", "/api/v1/viewer/status", nil); !strings.Contains(string(body), `"idleMinutes":15`) {
		t.Fatalf("idle minutes: %d %s", res.StatusCode, body)
	}
	ts.users.Create("gast", testPassword, auth.RoleUser)
	guest := ts.newClient()
	ts.do(guest, "POST", "/api/v1/login", credentials{Name: "gast", Password: testPassword})
	if res, _ := ts.do(guest, "POST", "/api/v1/viewer/image", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("guest image: %d", res.StatusCode)
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/viewer/settings", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("guest settings: %d", res.StatusCode)
	}
	if res, body := ts.do(guest, "GET", "/api/v1/viewer/status", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"gpus":[]`) {
		t.Fatalf("guest status: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/viewer/immich/downloads/d1", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("guest download: %d", res.StatusCode)
	}
	if res, _ := ts.request("GET", "/api/v1/viewer/immich/downloads/..%2f..", nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("odd download id: %d", res.StatusCode)
	}
	if res, body := ts.request("GET", "/api/v1/viewer/immich/downloads/d1", nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "viewer_no_session" {
		t.Fatalf("download without view: %d %s", res.StatusCode, body)
	}
}

// ownerRequest is a request with the cookie of the owner's login.
func ownerRequest(t *testing.T, ts *testServer, client *http.Client) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", "/", nil)
	base, _ := url.Parse(ts.url)
	for _, cookie := range client.Jar.Cookies(base) {
		r.AddCookie(cookie)
	}
	return r
}

// blockingRelay stands for a relay that runs until its context ends.
func blockingRelay(input func(), every time.Duration) func(context.Context, func()) viewer.End {
	return func(ctx context.Context, in func()) viewer.End {
		if every > 0 {
			ticker := time.NewTicker(every)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return viewer.EndContext
				case <-ticker.C:
					in()
				}
			}
		}
		<-ctx.Done()
		return viewer.EndContext
	}
}

func TestViewerEndsLikeAConsole(t *testing.T) {
	recheck := consoleRecheck.Swap(int64(20 * time.Millisecond))
	idle := consoleIdle.Swap(int64(150 * time.Millisecond))
	t.Cleanup(func() { consoleRecheck.Store(recheck); consoleIdle.Store(idle) })
	ts := newTestServer(t)
	ts.setup()
	owner, _ := ts.users.Get(ownerName(t, ts))
	run := func(r *http.Request, user auth.User, every time.Duration, within time.Duration) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), within)
		defer cancel()
		return ts.server.watchViewer(ctx, r, user, "immich", nil, blockingRelay(nil, every))
	}
	// No input: idle after the console idle time.
	if why := run(ownerRequest(t, ts, ts.client), owner, 0, 2*time.Second); why != "idle" {
		t.Fatalf("without input: %s", why)
	}
	// Input keeps it open (the context ends it here).
	if why := run(ownerRequest(t, ts, ts.client), owner, 30*time.Millisecond, 400*time.Millisecond); why != "closed" {
		t.Fatalf("with input: %s", why)
	}
	// An administrator who loses the role loses the right on the app
	// (app rights: administrators only without a tick).
	ts.users.Create("admin2", testPassword, auth.RoleAdmin)
	other := ts.newClient()
	ts.do(other, "POST", "/api/v1/login", credentials{Name: "admin2", Password: testPassword})
	admin2, _ := ts.users.Get("admin2")
	done := make(chan string, 1)
	go func() { done <- run(ownerRequest(t, ts, other), admin2, 30*time.Millisecond, 3*time.Second) }()
	time.Sleep(60 * time.Millisecond)
	ts.users.SetRole("admin2", auth.RoleUser)
	if why := <-done; why != "forbidden" && why != "logout" {
		t.Fatalf("role taken: %s", why)
	}
	// Locking the session ends the view like a console.
	go func() { done <- run(ownerRequest(t, ts, ts.client), owner, 30*time.Millisecond, 3*time.Second) }()
	time.Sleep(60 * time.Millisecond)
	if res, _ := ts.request("POST", "/api/v1/session/lock", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("lock: %d", res.StatusCode)
	}
	if why := <-done; why != "logout" {
		t.Fatalf("locked: %s", why)
	}
}

func TestAttachmentName(t *testing.T) {
	got := attachmentName("../Bericht \"Q3\";\r\n.pdf")
	if strings.ContainsAny(got, "\r\n/") || !strings.HasPrefix(got, "attachment;") || !strings.Contains(got, "filename*=UTF-8''") {
		t.Fatalf("%s", got)
	}
}

func TestDeletedAccountLosesItsViewerProfiles(t *testing.T) {
	ts := newTestServer(t)
	m := viewer.New(ts.store)
	m.MountRoot = t.TempDir()
	m.Mounted = func() []string { return []string{"data"} }
	m.DockerVolume = func() string { return "data" }
	ts.server.opts.Viewer = m
	ts.setup()
	ts.users.Create("gast", testPassword, auth.RoleUser)
	profile := filepath.Join(m.MountRoot, "data/.titanos/viewer/profiles/gast/immich")
	os.MkdirAll(profile, 0o700)
	other := filepath.Join(m.MountRoot, "data/.titanos/viewer/profiles/anna")
	os.MkdirAll(other, 0o700)
	if res, body := ts.request("DELETE", "/api/v1/users/gast", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(filepath.Dir(profile)); os.IsNotExist(err) {
			if _, err := os.Stat(other); err != nil {
				t.Fatal("another account's profiles removed")
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("profiles of the deleted account kept")
}
