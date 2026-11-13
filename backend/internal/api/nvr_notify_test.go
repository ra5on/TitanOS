package api

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"

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
	n.NotifyTo(notify.Info, "nvr_event_person", "Haustür", []notify.Target{{User: "alex", Bell: true}}, false, "haustuer")
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
	_ = nvr.KindPerson
}
