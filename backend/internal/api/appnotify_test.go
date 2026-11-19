package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appnotify"
	"github.com/ra5on/TitanOS/backend/internal/webpush"
)

type appNotifyList struct {
	Epoch, Latest string
	Reset         bool
	Items         []appnotify.Item
}

func (ts *testServer) appNotifications(phone *http.Client, since string) appNotifyList {
	ts.t.Helper()
	res, body := ts.do(phone, "GET", "/api/v1/app/notifications?since="+since, nil)
	var got appNotifyList
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &got) != nil {
		ts.t.Fatalf("list: %d %s", res.StatusCode, body)
	}
	return got
}

func TestAppNotifications(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	app := newTestApp(t)
	user, device := ts.pairApp(app, "Pixel 9 von Alex")
	phone, _, _ := ts.appSession(app, user, device)
	push := ts.server.opts.WebPush

	empty := ts.appNotifications(phone, "")
	if len(empty.Items) != 0 || empty.Reset || empty.Epoch == "" {
		t.Fatalf("empty: %+v", empty)
	}
	// alex has no subscribed browser: the app gets the messages anyway.
	push.Broadcast(webpush.Message{Title: "[nas] Platte wird warm", Body: "WD Red Plus 52 °C", Tag: "disk_hot:sda", URL: "/", Severity: "warning"})
	push.SendMail(user, webpush.Message{Title: "Sam Christensen", Body: "Elternabend", Tag: "mail:Privat", URL: "/mail"})
	push.SendTo("someone-else", webpush.Message{Title: "not for alex", Tag: "agent-9"})
	got := ts.appNotifications(phone, empty.Latest)
	if len(got.Items) != 2 || got.Items[0].Category != appnotify.NAS || got.Items[0].Severity != "warning" ||
		got.Items[1].Category != appnotify.Mail || got.Items[1].Link != "/mail" || got.Reset {
		t.Fatalf("list: %+v", got)
	}
	if again := ts.appNotifications(phone, got.Latest); len(again.Items) != 0 {
		t.Fatalf("after latest: %+v", again)
	}
	// A marker of an earlier start of titand: everything kept, with reset.
	if old := ts.appNotifications(phone, "1-1"); !old.Reset || len(old.Items) != 2 {
		t.Fatalf("old epoch: %+v", old)
	}

	// The browser is not the app; without a login nothing.
	for _, path := range []string{"/api/v1/app/notifications", "/api/v1/app/notifications/stream"} {
		if res, body := ts.request("GET", path, nil); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "app_session_required" {
			t.Fatalf("browser %s: %d %s", path, res.StatusCode, body)
		}
		if res, _ := ts.do(ts.newClient(), "GET", path, nil); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("anonymous %s: %d", path, res.StatusCode)
		}
	}
}

func TestAppNotificationStream(t *testing.T) {
	prev := appNotifyHeartbeat.Load()
	appNotifyHeartbeat.Store(int64(50 * time.Millisecond))
	t.Cleanup(func() { appNotifyHeartbeat.Store(prev) })
	ts := newTestServer(t)
	ts.setup()
	app := newTestApp(t)
	user, device := ts.pairApp(app, "Pixel 9 von Alex")
	phone, _, _ := ts.appSession(app, user, device)
	push := ts.server.opts.WebPush
	start := ts.appNotifications(phone, "")
	push.SendTo(user, webpush.Message{Title: "KI-Agent: fertig", Tag: "agent-1", URL: "/agent?chat=1"})

	req, _ := http.NewRequestWithContext(t.Context(), "GET", ts.url+"/api/v1/app/notifications/stream?since="+start.Latest, nil)
	res, err := phone.Do(req)
	if err != nil || res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("stream: %v %v", err, res)
	}
	defer res.Body.Close()
	lines := make(chan string, 64)
	go func() {
		scan := bufio.NewScanner(res.Body)
		for scan.Scan() {
			lines <- scan.Text()
		}
		close(lines)
	}()
	next := func(prefix string) string {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case line, ok := <-lines:
				if !ok {
					t.Fatalf("stream ended before %q", prefix)
				}
				if strings.HasPrefix(line, prefix) {
					return line
				}
			case <-deadline:
				t.Fatalf("no %q", prefix)
			}
		}
	}
	next("event: ready")
	// What came after since, then what comes now, each with its marker.
	if id := next("id: "); !strings.HasPrefix(id, "id: "+start.Epoch+"-") {
		t.Fatalf("id %q", id)
	}
	if data := next("data: "); !strings.Contains(data, `"category":"agent"`) || !strings.Contains(data, "KI-Agent: fertig") {
		t.Fatalf("backlog %q", data)
	}
	push.Broadcast(webpush.Message{Title: "Sicherung fertig", Tag: "backup_done:Data"})
	if data := next("data: "); !strings.Contains(data, "Sicherung fertig") || !strings.Contains(data, `"category":"nas"`) {
		t.Fatalf("live %q", data)
	}
	next(": keep-alive")
}
