package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/notify"
)

// The findings of the review of PLAN 82-3 (13.11.2026), each a test that
// failed before its fix.

// A refused change must not reach the camera in RAM: the pause is a
// pointer that encoding/json wrote through into the stored camera.
func TestReview82PauseNotShared(t *testing.T) {
	ts, manager := nvrServer(t)
	ts.setup()
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"pauseSeconds": 300}}); res.StatusCode != http.StatusOK {
		t.Fatalf("store 300: %d %s", res.StatusCode, body)
	}
	for _, change := range []map[string]any{
		{"pauseSeconds": 7},
		{"pauseSeconds": 3600, "recipients": []map[string]any{{"user": "niemand", "push": true}}},
	} {
		res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": change})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("change %v: %d %s, want 400", change, res.StatusCode, body)
		}
		c, _ := manager.Camera("haustuer")
		if got := *c.Notify.PauseSeconds; got != 300 {
			t.Errorf("refused change %v reached RAM: pause %d, stored 300", change, got)
		}
	}
}

// Recipients in a request replace the stored list as a whole: an entry
// left partly out takes no ways of whoever stood at its place.
func TestReview82RecipientsReplaced(t *testing.T) {
	ts, manager := nvrServer(t)
	ts.setup()
	if _, err := ts.users.Create("mara", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": []map[string]any{{"user": "alex", "bell": true, "push": true, "email": true}}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": []map[string]any{{"user": "mara", "push": true}}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	c, _ := manager.Camera("haustuer")
	if len(c.Notify.Recipients) != 1 || c.Notify.Recipients[0].User != "mara" || c.Notify.Recipients[0].Bell || c.Notify.Recipients[0].Email || !c.Notify.Recipients[0].Push {
		t.Errorf("stored %+v, want mara with push only", c.Notify.Recipients)
	}
	// A request without recipients keeps them.
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"pauseSeconds": 300}}); res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if c, _ = manager.Camera("haustuer"); len(c.Notify.Recipients) != 1 {
		t.Errorf("a change without recipients lost them: %+v", c.Notify.Recipients)
	}
}

// An administrator chosen with the bell and later made a plain account
// loses the bell at the next save of the camera; the form, which shows
// such an account without a bell, can then add and remove its ways.
func TestReview82DemotedAdminLosesBell(t *testing.T) {
	ts, manager := nvrServer(t)
	ts.setup()
	if _, err := ts.users.Create("bob", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": []map[string]any{{"user": "bob", "bell": true, "push": true, "email": false}}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if _, err := ts.users.SetRole("bob", auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	grant(t, ts, "bob", map[string]string{"haustuer": "live"})
	// What the form sends when e-mail is ticked for bob: the entry as it
	// was (with the bell) plus e-mail.
	res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": []map[string]any{{"user": "bob", "bell": true, "push": true, "email": true}}}})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("tick e-mail for the demoted bob: %d %s", res.StatusCode, body)
	}
	c, _ := manager.Camera("haustuer")
	if len(c.Notify.Recipients) != 1 || c.Notify.Recipients[0].Bell || !c.Notify.Recipients[0].Email || c.Notify.Titan {
		t.Errorf("stored %+v (titan %v): bob keeps push and e-mail, not the bell", c.Notify.Recipients, c.Notify.Titan)
	}
	// A bell asked for anew is still refused for a plain account.
	res, _ = ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": []map[string]any{{"user": "bob", "bell": true, "push": true, "email": true}}}})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("a new bell for a plain account: %d, want 400", res.StatusCode)
	}
}

// The bell messages of a deleted account go with it: an account made
// later under the same name does not see them.
func TestReview82DeletedAccountLeavesTheBell(t *testing.T) {
	ts, _ := nvrServer(t)
	ts.setup()
	if _, err := ts.users.Create("bob", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	n := ts.server.opts.Notify
	n.NotifyTo(notify.Info, "nvr_event_face_known", "Anna · Haustür", []notify.Target{{User: "bob", Bell: true}, {User: "alex", Bell: true}}, false, notify.About{Ref: "haustuer"})
	if res, body := ts.request("DELETE", "/api/v1/users/bob", nil); res.StatusCode >= 300 {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	time.Sleep(10 * time.Millisecond)
	if _, err := ts.users.Create("bob", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	bob := ts.newClient()
	if res, _ := ts.do(bob, "POST", "/api/v1/login", credentials{Name: "bob", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatal("login")
	}
	_, data := ts.do(bob, "GET", "/api/v1/notifications", nil)
	var res struct{ Messages []notify.Message }
	if err := json.Unmarshal(data, &res); err != nil {
		t.Fatal(err)
	}
	for _, m := range res.Messages {
		if m.Kind == "nvr_event_face_known" {
			t.Errorf("the new bob sees the earlier bob's camera message: %q (To %v)", m.Text, m.To)
		}
	}
	// The other recipient keeps it.
	kept := false
	for _, m := range n.MessagesFor("alex") {
		kept = kept || m.Kind == "nvr_event_face_known"
	}
	if !kept {
		t.Error("alex lost the message when bob was deleted")
	}
}

// A change of the pause while messages are routed (run with -race: the
// pause was one int shared by the request and the routing).
func TestReview82PauseRace(t *testing.T) {
	ts, manager := nvrServer(t)
	ts.setup()
	nvrRunning(t, ts)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 40; i++ {
			manager.DevStandIn().AddEvent(fmt.Sprintf("1731412345.%d-race", i), "haustuer", "person", "", "", float64(time.Now().Unix()))
			time.Sleep(5 * time.Millisecond)
		}
	}()
	for i := 0; i < 40; i++ {
		pause := []int{0, 60}[i%2]
		ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"pauseSeconds": pause}})
		time.Sleep(5 * time.Millisecond)
	}
	<-done
}

// Re-check of the review (13.11.2026): an entry left without any way is
// no recipient; null does not turn a camera with recipients back into
// one of 0.78; an account made anew does not see the bell messages of an
// earlier one of its name even where ForgetBell could not act.
func TestReview82WaylessEntryGoes(t *testing.T) {
	ts, manager := nvrServer(t)
	ts.setup()
	if _, err := ts.users.Create("bob", testPassword, auth.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": []map[string]any{{"user": "bob", "bell": true, "push": true}, {"user": "alex", "bell": true}}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	if _, err := ts.users.SetRole("bob", auth.RoleUser); err != nil {
		t.Fatal(err)
	}
	grant(t, ts, "bob", map[string]string{"haustuer": "live"})
	// The form shows bob with push only and unticks it.
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": []map[string]any{{"user": "bob", "bell": true}, {"user": "alex", "bell": true}}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	c, _ := manager.Camera("haustuer")
	if len(c.Notify.Recipients) != 1 || c.Notify.Recipients[0].User != "alex" {
		t.Errorf("stored %+v, want alex only", c.Notify.Recipients)
	}
}

func TestReview82NullKeepsTheList(t *testing.T) {
	ts, manager := nvrServer(t)
	ts.setup()
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": []map[string]any{{"user": "alex", "push": true, "email": true}}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	res, _ := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": nil}})
	if res.StatusCode != http.StatusBadRequest {
		t.Errorf("null for a camera with recipients: %d, want 400", res.StatusCode)
	}
	if c, _ := manager.Camera("haustuer"); c.Notify.Recipients == nil {
		t.Error("the camera fell back to the ways of 0.78")
	}
	// [] is nobody and allowed.
	if res, body := ts.request("PUT", "/api/v1/nvr/cameras/haustuer", map[string]any{"notify": map[string]any{"recipients": []map[string]any{}}}); res.StatusCode != http.StatusOK {
		t.Fatalf("[]: %d %s", res.StatusCode, body)
	}
	if c, _ := manager.Camera("haustuer"); c.Notify.Recipients == nil || len(c.Notify.Recipients) != 0 {
		t.Errorf("[] stored as %#v", c.Notify.Recipients)
	}
}

func TestReview82OlderBellHiddenFromNewAccount(t *testing.T) {
	n := notify.NewManager(nil)
	created := time.Now().Add(time.Hour)
	n.AccountCreated = func(user string) time.Time {
		if user == "bob" {
			return created
		}
		return time.Time{}
	}
	n.NotifyTo(notify.Info, "nvr_event_face_known", "Anna · Haustür", []notify.Target{{User: "bob", Bell: true}}, false, notify.About{Ref: "haustuer"})
	if list := n.MessagesFor("bob"); len(list) != 0 {
		t.Errorf("an account made after the message sees it: %+v", list)
	}
	created = time.Time{}
	if list := n.MessagesFor("bob"); len(list) != 1 {
		t.Errorf("the account the message was for does not see it: %+v", list)
	}
}
