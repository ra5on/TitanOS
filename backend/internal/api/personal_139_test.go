package api

import (
	"net/http"
	"testing"
	"time"
)

// 1.39 (owner 28.11.2026): while an Agent window of the account is in view
// on any device, a finished job sends no push.
func TestPersonalPresenceFromTheBrowser(t *testing.T) {
	ts := personalFixture(t)
	s := ts.server
	user, ok := ts.users.Get("alex")
	if !ok {
		t.Fatal("no account")
	}
	key := personalKey(user)
	if s.personalLooking(key, time.Now()) {
		t.Fatal("looking before any window said so")
	}
	resp, _ := ts.do(ts.client, "POST", personalBase+"/presence", map[string]any{"window": "w1", "visible": true})
	if resp.StatusCode != http.StatusNoContent || !s.personalLooking(key, time.Now()) {
		t.Fatalf("visible window not seen: %d", resp.StatusCode)
	}
	resp, _ = ts.do(ts.client, "POST", personalBase+"/presence", map[string]any{"window": "w1", "visible": false})
	if resp.StatusCode != http.StatusNoContent || s.personalLooking(key, time.Now()) {
		t.Fatal("hidden window still counts")
	}
	if resp, _ := ts.do(ts.client, "POST", personalBase+"/presence", map[string]any{"visible": true}); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("window without a name: %d", resp.StatusCode)
	}
}

func TestPersonalPresenceEndsAndCountsEveryWindow(t *testing.T) {
	s := &Server{}
	now := time.Now()
	s.personalLook("a", "desktop", true, now)
	s.personalLook("a", "phone", true, now)
	s.personalLook("a", "phone", false, now)
	if !s.personalLooking("a", now) {
		t.Fatal("the desktop window still looks")
	}
	if s.personalLooking("b", now) {
		t.Fatal("another account is not looking")
	}
	if s.personalLooking("a", now.Add(personalLookingFor+time.Second)) {
		t.Fatal("a window that stopped saying so still counts")
	}
	for i := range 100 {
		s.personalLook("a", string(rune('A'+i)), true, now)
	}
	if n := len(s.personal.seen["a"]); n > 32 {
		t.Fatalf("%d windows kept", n)
	}
}
