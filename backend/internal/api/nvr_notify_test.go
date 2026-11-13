package api

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/notify"
	"github.com/ra5on/TitanOS/backend/internal/nvr"
)

// PLAN 82-3: whom a camera can tell, and the recipients a camera may have.
func TestNVRRecipients(t *testing.T) {
	ts, manager := nvrServer(t)
	ts.setup()
	guest := ts.guest("gast")
	if _, err := ts.users.Create("mara", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.users.Create("weg", testPassword, auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	if _, err := ts.users.SetDisabled("weg", true, "alex"); err != nil {
		t.Fatal(err)
	}
	// gast may watch the front door; alex has an address of his own.
	if res, body := ts.request("PUT", "/api/v1/users/gast/cameras", map[string]any{"cameras": map[string]any{"haustuer": map[string]bool{"live": true}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("rights: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/account/notifications", map[string]any{"language": "de", "content": "summary", "email": map[string]any{"enabled": false, "to": "alex@example.org"}}); res.StatusCode != http.StatusOK {
		t.Fatalf("own address: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(guest, "GET", "/api/v1/nvr/recipients", nil); res.StatusCode != http.StatusForbidden {
		t.Fatalf("a plain account lists the recipients: %d", res.StatusCode)
	}
	res, body := ts.request("GET", "/api/v1/nvr/recipients", nil)
	var list struct {
		Accounts   []nvrRecipient `json:"accounts"`
		MailServer bool           `json:"mailServer"`
	}
	if err := json.Unmarshal(body, &list); err != nil || res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	names := []string{}
	for _, a := range list.Accounts {
		names = append(names, a.Name)
	}
	if !slices.Equal(names, []string{"alex", "gast", "mara"}) || list.MailServer {
		t.Fatalf("accounts %v (a locked one is left out), mail server %v: %s", names, list.MailServer, body)
	}
	alex, gast := list.Accounts[0], list.Accounts[1]
	if !alex.Admin || !alex.Email || len(alex.Cameras) != len(manager.CameraIDs()) || alex.PushDevices != 0 {
		t.Errorf("alex %+v", alex)
	}
	if gast.Admin || gast.Email || !slices.Equal(gast.Cameras, []string{"haustuer"}) {
		t.Errorf("gast %+v", gast)
	}

	// Saving a camera: the recipients are checked against the accounts.
	camera := func() map[string]any {
		_, body := ts.request("GET", "/api/v1/nvr/cameras/haustuer", nil)
		var c map[string]any
		json.Unmarshal(body, &c)
		return c
	}
	recipients := func(c map[string]any) any { return c["notify"].(map[string]any)["recipients"] }
	if recipients(camera()) != nil {
		t.Fatalf("a demo camera without a list answers %v, want null", recipients(camera()))
	}
	put := func(list ...map[string]any) (*http.Response, []byte) {
		c := camera()
		c["notify"].(map[string]any)["recipients"] = list
		delete(c, "state")
		return ts.request("PUT", "/api/v1/nvr/cameras/haustuer", c)
	}
	for _, tc := range []struct {
		list []map[string]any
		code string
	}{
		{[]map[string]any{{"user": "niemand", "push": true}}, "nvr_recipient_invalid"},
		{[]map[string]any{{"user": "weg", "push": true}}, "nvr_recipient_invalid"},
		{[]map[string]any{{"user": "gast", "bell": true}}, "nvr_recipient_bell"},
	} {
		res, body := put(tc.list...)
		if res.StatusCode != http.StatusBadRequest || errorCode(t, body) != tc.code {
			t.Errorf("%v: %d %s, want %s", tc.list, res.StatusCode, body, tc.code)
		}
	}
	// A refused change leaves the camera in RAM as it was.
	if recipients(camera()) != nil {
		t.Fatalf("a refused change reached the camera: %v", recipients(camera()))
	}
	res, body = put(map[string]any{"user": "alex", "bell": true, "push": true}, map[string]any{"user": "gast", "push": true, "email": true})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	stored, _ := manager.Camera("haustuer")
	if len(stored.Notify.Recipients) != 2 || stored.Notify.Recipients[0].Added.IsZero() || !stored.Notify.Titan || !stored.Notify.Email {
		t.Fatalf("stored %+v", stored.Notify)
	}
	// Another camera: gast may not see it.
	c := map[string]any{}
	_, body = ts.request("GET", "/api/v1/nvr/cameras/einfahrt", nil)
	json.Unmarshal(body, &c)
	delete(c, "state")
	c["notify"].(map[string]any)["recipients"] = []map[string]any{{"user": "gast", "push": true}}
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/einfahrt", c); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "nvr_recipient_no_right" {
		t.Errorf("%d %s", res.StatusCode, body)
	}
	// A refused change of a stored list (decoded over the stored camera)
	// leaves the list in RAM as it was.
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": []map[string]any{{"user": "gast", "bell": true}}}}); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if stored, _ := manager.Camera("haustuer"); stored.Notify.Recipients[0].User != "alex" || !stored.Notify.Recipients[0].Bell || stored.Notify.Recipients[1].User != "gast" {
		t.Fatalf("a refused change reached the stored list: %+v", stored.Notify.Recipients)
	}
	// gast loses the right: reordering the camera still works.
	if res, body := ts.request("PUT", "/api/v1/users/gast/cameras", map[string]any{"cameras": map[string]any{"haustuer": map[string]bool{"live": false}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("rights: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"order": 2}); res.StatusCode != http.StatusOK {
		t.Fatalf("reordering after a recipient lost the right: %d %s", res.StatusCode, body)
	}
	// Deleting an account takes it off the cameras.
	if res, body := ts.request("DELETE", "/api/v1/users/gast", nil); res.StatusCode >= 300 {
		t.Fatalf("delete gast: %d %s", res.StatusCode, body)
	}
	stored, _ = manager.Camera("haustuer")
	if len(stored.Notify.Recipients) != 1 || stored.Notify.Recipients[0].User != "alex" {
		t.Fatalf("gast stays a recipient: %+v", stored.Notify.Recipients)
	}
}

// A camera's bell message is shown and dismissed only by its recipients.
func TestBellAudience(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	if _, err := ts.users.Create("mara", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	mara := ts.newClient()
	if res, _ := ts.do(mara, "POST", "/api/v1/login", credentials{Name: "mara", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatal("login of mara")
	}
	n := ts.server.opts.Notify
	n.Notify(notify.Warning, "volume_degraded", "tank")
	n.NotifyTo(notify.Info, "nvr_event_person", "Haustür", []notify.Target{{User: "alex", Bell: true}}, false, notify.About{Ref: "haustuer"})
	kinds := func(client *http.Client, method, path string, body any) []string {
		_, data := ts.do(client, method, path, body)
		var res struct{ Messages []notify.Message }
		if err := json.Unmarshal(data, &res); err != nil {
			t.Fatalf("%s", data)
		}
		out := []string{}
		for _, m := range res.Messages {
			out = append(out, m.Kind)
		}
		return out
	}
	if got := kinds(ts.client, "GET", "/api/v1/notifications", nil); !slices.Equal(got, []string{"nvr_event_person", "volume_degraded"}) {
		t.Fatalf("alex sees %v", got)
	}
	if got := kinds(mara, "GET", "/api/v1/notifications", nil); !slices.Equal(got, []string{"volume_degraded"}) {
		t.Fatalf("mara sees %v", got)
	}
	var cameraID int
	for _, m := range n.MessagesFor("alex") {
		if m.Kind == "nvr_event_person" {
			cameraID = m.ID
		}
	}
	// mara cannot dismiss it; alex can.
	if got := kinds(mara, "POST", "/api/v1/notifications/dismiss", map[string]any{"ids": []int{cameraID}}); !slices.Equal(got, []string{"volume_degraded"}) {
		t.Fatalf("mara after dismissing: %v", got)
	}
	if got := kinds(ts.client, "GET", "/api/v1/notifications", nil); len(got) != 2 {
		t.Fatalf("mara dismissed alex's message: %v", got)
	}
	if got := kinds(ts.client, "POST", "/api/v1/notifications/dismiss", map[string]any{"ids": []int{cameraID}}); !slices.Equal(got, []string{"volume_degraded"}) {
		t.Fatalf("alex after dismissing: %v", got)
	}
}

// PLAN 82-3 (owner 13.11.2026): the names of recognised faces and known
// plates and the plates read are a right of their own per camera. Without
// it an account sees events and the feed without them.
func TestNVRNamesRight(t *testing.T) {
	recheck := consoleRecheck.Swap(int64(20 * time.Millisecond))
	t.Cleanup(func() { consoleRecheck.Store(recheck) })
	ts, manager := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	guest := ts.guest("gast")
	grant(t, ts, "gast", map[string]string{"einfahrt": "both"})
	named := func(client *http.Client) (names, plates int) {
		var list eventList
		getJSON(t, ts, client, "/nvr/events?limit=200&camera=einfahrt", &list)
		for _, e := range list.Events {
			if e.SubLabel != "" {
				names++
			}
			if e.Plate != "" {
				plates++
			}
		}
		return names, plates
	}
	if names, plates := named(ts.client); names == 0 || plates == 0 {
		t.Fatalf("the owner sees %d names, %d plates", names, plates)
	}
	if names, plates := named(guest); names != 0 || plates != 0 {
		t.Fatalf("without the right: %d names, %d plates", names, plates)
	}
	var plated nvr.Event
	var all eventList
	getJSON(t, ts, ts.client, "/nvr/events?limit=200&camera=einfahrt", &all)
	for _, e := range all.Events {
		if e.Plate != "" && plated.ID == "" {
			plated = e
		}
	}
	for _, check := range []struct {
		method, path string
		body         any
	}{{"GET", "/api/v1/nvr/events/" + plated.ID, nil}, {"POST", "/api/v1/nvr/events/" + plated.ID + "/reviewed", map[string]any{"reviewed": true}}} {
		res, body := ts.do(guest, check.method, check.path, check.body)
		var e nvr.Event
		json.Unmarshal(body, &e)
		if res.StatusCode != http.StatusOK || e.Plate != "" || e.SubLabel != "" {
			t.Errorf("%s %s: %d %s", check.method, check.path, res.StatusCode, body)
		}
	}
	// The feed: an event with a plate reaches the guest without it.
	conn, ctx := liveDial(t, ts, guest, "/nvr/feed")
	owner, octx := liveDial(t, ts, ts.client, "/nvr/feed")
	plateIn := func(conn *websocket.Conn, ctx context.Context, id string) (string, string) {
		t.Helper()
		for {
			_, data, err := conn.Read(ctx)
			if err != nil {
				t.Fatalf("feed: %v", err)
			}
			var msg nvr.FeedMessage
			json.Unmarshal(data, &msg)
			if msg.Event != nil && msg.Event.ID == id {
				return msg.Event.Plate, msg.Event.SubLabel
			}
		}
	}
	now := float64(time.Now().Unix())
	manager.DevStandIn().AddEvent("1731412345.1-plate", "einfahrt", "car", "Annas Auto", "M-AB 1234", now)
	if plate, name := plateIn(owner, octx, "1731412345.1-plate"); plate != "M-AB 1234" || name != "Annas Auto" {
		t.Errorf("owner's feed: %q %q", plate, name)
	}
	if plate, name := plateIn(conn, ctx, "1731412345.1-plate"); plate != "" || name != "" {
		t.Errorf("guest's feed without the right: %q %q", plate, name)
	}

	// Names without recordings are not given; with them they are.
	setRights := func(rights map[string]any) nvrUserCameras {
		t.Helper()
		res, body := ts.request("PUT", "/api/v1/users/gast/cameras", map[string]any{"cameras": map[string]any{"einfahrt": rights}})
		var view nvrUserCameras
		if err := json.Unmarshal(body, &view); err != nil || res.StatusCode != http.StatusOK {
			t.Fatalf("%d %s", res.StatusCode, body)
		}
		return view
	}
	names := func(view nvrUserCameras) bool {
		for _, c := range view.Cameras {
			if c.ID == "einfahrt" {
				return c.Names
			}
		}
		return false
	}
	if names(setRights(map[string]any{"live": true, "recordings": false, "names": true})) {
		t.Error("names without recordings")
	}
	if !names(setRights(map[string]any{"live": true, "recordings": true, "names": true})) {
		t.Fatal("names with recordings")
	}
	// A page that does not know the names (0.78) leaves them as they are.
	grant(t, ts, "gast", map[string]string{"einfahrt": "both"})
	var cams nvrUserCameras
	getJSON(t, ts, ts.client, "/users/gast/cameras", &cams)
	if !names(cams) {
		t.Fatal("a page of 0.78 took the names away")
	}
	if names, plates := named(guest); names == 0 || plates == 0 {
		t.Errorf("with the right: %d names, %d plates", names, plates)
	}
	manager.DevStandIn().AddEvent("1731412345.2-plate", "einfahrt", "car", "", "B-XY 987", now)
	if plate, _ := plateIn(conn, ctx, "1731412345.2-plate"); plate != "B-XY 987" {
		t.Errorf("guest's feed with the right: %q", plate)
	}
	// The camera list says the account's own rights; the recipients list
	// which cameras' names an account may see.
	res, body := ts.do(guest, "GET", "/api/v1/nvr/cameras/einfahrt", nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"names":true`) {
		t.Errorf("brief: %d %s", res.StatusCode, body)
	}
	_, body = ts.request("GET", "/api/v1/nvr/recipients", nil)
	var list struct {
		Accounts []nvrRecipient `json:"accounts"`
	}
	json.Unmarshal(body, &list)
	for _, a := range list.Accounts {
		switch a.Name {
		case "gast":
			if !slices.Equal(a.Names, []string{"einfahrt"}) {
				t.Errorf("gast names %v", a.Names)
			}
		case "alex":
			if len(a.Names) != len(manager.CameraIDs()) {
				t.Errorf("alex names %v", a.Names)
			}
		}
	}
}
