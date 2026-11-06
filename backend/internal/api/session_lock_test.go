package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/webauthn/webauthntest"
)

type lockedSession struct {
	Locked      bool
	Passkey     bool
	LockMinutes *int
	Version     string
	User        *struct{ Name, Role string }
}

func (ts *testServer) sessionInfo(client *http.Client) lockedSession {
	ts.t.Helper()
	_, body := ts.do(client, "GET", "/api/v1/session", nil)
	var info lockedSession
	if err := json.Unmarshal(body, &info); err != nil {
		ts.t.Fatalf("session: %s", body)
	}
	return info
}

func TestSessionLockAnswers423(t *testing.T) {
	ts := newTestServer(t)
	log := activity.New(ts.store)
	ts.server.opts.Activity = log
	ts.setup()
	other := ts.newClient()
	ts.do(other, "POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword})

	if res, body := ts.request("POST", "/api/v1/session/lock", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("lock: %d %s", res.StatusCode, body)
	}
	// Locking again changes nothing and is no new entry.
	if res, _ := ts.request("POST", "/api/v1/session/lock", nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("lock again: %d", res.StatusCode)
	}
	for _, route := range []struct{ method, path string }{{"GET", "/api/v1/system"}, {"GET", "/api/v1/preferences"}, {"PUT", "/api/v1/system/hostname"}, {"GET", "/api/v1/account/passkeys"}, {"GET", "/api/v1/terminal/shell"}, {"GET", "/api/v1/account/sessions"}} {
		if res, body := ts.request(route.method, route.path, map[string]any{}); res.StatusCode != http.StatusLocked || errorCode(t, body) != "locked" {
			t.Errorf("%s %s while locked: %d %s", route.method, route.path, res.StatusCode, body)
		}
	}
	info := ts.sessionInfo(ts.client)
	if !info.Locked || info.User == nil || info.User.Name != "alex" || info.Version != "" || info.LockMinutes == nil || *info.LockMinutes != defaultLockMinutes {
		t.Errorf("session while locked: %+v", info)
	}
	// The other browser of the same account is not locked.
	if res, _ := ts.do(other, "GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK {
		t.Errorf("other session: %d", res.StatusCode)
	}
	if info := ts.sessionInfo(other); info.Locked || info.Version == "" {
		t.Errorf("other session info: %+v", info)
	}
	// API tokens of the account are no sessions and stay usable.
	owner, _ := ts.users.Get("alex")
	_, plain, err := ts.server.tokens.Create(owner, "monitor", []string{"system:read"})
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("GET", ts.url+"/api/v1/system", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode != http.StatusOK {
		t.Errorf("token while a session is locked: %v %v", res, err)
	} else {
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
	}

	if res, body := ts.request("POST", "/api/v1/session/unlock", map[string]string{"password": "wrong password"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Errorf("wrong password: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusLocked {
		t.Error("a wrong password unlocked")
	}
	if res, body := ts.request("POST", "/api/v1/session/unlock", map[string]string{"password": testPassword}); res.StatusCode != http.StatusOK {
		t.Fatalf("unlock: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK {
		t.Errorf("after unlock: %d", res.StatusCode)
	}
	if info := ts.sessionInfo(ts.client); info.Locked || info.Version == "" {
		t.Errorf("session after unlock: %+v", info)
	}

	rows := activityEntries(t, log)
	count := func(action string) int {
		n := 0
		for _, e := range rows {
			if e.Action == action {
				n++
			}
		}
		return n
	}
	if count("session_lock") != 1 || count("session_unlock") != 1 || count("session_unlock_failed") != 1 {
		t.Errorf("activity: %+v", rows)
	}
	if count("POST /api/v1/session/lock") != 0 || count("POST /api/v1/session/unlock") != 0 {
		t.Errorf("lock recorded twice: %+v", rows)
	}
	if e, _ := findActivity(rows, "session_unlock"); e.User != "alex" || !e.OK || e.Target != "password" {
		t.Errorf("unlock entry: %+v", e)
	}

	// Logging out works while locked.
	ts.request("POST", "/api/v1/session/lock", nil)
	if res, _ := ts.request("POST", "/api/v1/logout", nil); res.StatusCode != http.StatusNoContent {
		t.Errorf("logout while locked: %d", res.StatusCode)
	}
	if info := ts.sessionInfo(ts.client); info.User != nil {
		t.Errorf("still logged in: %+v", info)
	}
	if res, body := ts.request("POST", "/api/v1/session/unlock", map[string]string{"password": testPassword}); res.StatusCode != http.StatusUnauthorized {
		t.Errorf("unlock without a session: %d %s", res.StatusCode, body)
	}
}

func TestSessionUnlockIsRateLimitedLikeTheLogin(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	ts.request("POST", "/api/v1/session/lock", nil)
	var last *http.Response
	var body []byte
	for range 8 {
		last, body = ts.request("POST", "/api/v1/session/unlock", map[string]string{"password": "wrong password"})
		if last.StatusCode == http.StatusTooManyRequests {
			break
		}
	}
	if last.StatusCode != http.StatusTooManyRequests || errorCode(t, body) != "rate_limited" {
		t.Fatalf("no limit after failures: %d %s", last.StatusCode, body)
	}
	var parsed map[string]apiError
	json.Unmarshal(body, &parsed)
	if parsed["error"].RetryAfter <= 0 {
		t.Errorf("retryAfter = %d", parsed["error"].RetryAfter)
	}
	// Even the right password waits.
	if res, _ := ts.request("POST", "/api/v1/session/unlock", map[string]string{"password": testPassword}); res.StatusCode != http.StatusTooManyRequests {
		t.Errorf("right password during the wait: %d", res.StatusCode)
	}
	// The address is blocked for the login as well: the same limiter.
	if res, _ := ts.do(ts.newClient(), "POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword}); res.StatusCode != http.StatusTooManyRequests {
		t.Errorf("login from the same address during the wait: %d", res.StatusCode)
	}
}

func TestSessionUnlockAsksForTheCode(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	_, body := ts.request("POST", "/api/v1/account/2fa/begin", nil)
	var begun struct{ Secret string }
	json.Unmarshal(body, &begun)
	if res, body := ts.request("POST", "/api/v1/account/2fa/enable", map[string]string{"code": auth.CodeForTest(begun.Secret, 0)}); res.StatusCode != http.StatusOK {
		t.Fatalf("enable: %d %s", res.StatusCode, body)
	}
	ts.request("POST", "/api/v1/session/lock", nil)
	if res, body := ts.request("POST", "/api/v1/session/unlock", map[string]string{"password": testPassword}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "second_factor_required" {
		t.Fatalf("without a code: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/session/unlock", map[string]string{"password": testPassword, "code": "000000"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "invalid_code" {
		t.Fatalf("wrong code: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/session/unlock", map[string]string{"password": testPassword, "code": auth.CodeForTest(begun.Secret, 1)}); res.StatusCode != http.StatusOK {
		t.Fatalf("with the code: %d %s", res.StatusCode, body)
	}
}

func TestSessionUnlockWithPasskey(t *testing.T) {
	ts := newTestServer(t)
	ts.url = strings.Replace(ts.url, "127.0.0.1", "localhost", 1)
	origin := strings.ToLower(ts.url)
	ts.client = ts.newClient()
	ts.setup()
	_, body := ts.request("POST", "/api/v1/account/passkeys/begin", map[string]string{"password": testPassword})
	var begun struct {
		ChallengeID string `json:"challengeId"`
		PublicKey   struct {
			Challenge string
			User      struct{ ID string }
		} `json:"publicKey"`
	}
	json.Unmarshal(body, &begun)
	key := webauthntest.New()
	key.Handle, _ = decodeB64(begun.PublicKey.User.ID)
	cd, att := key.Create(begun.PublicKey.Challenge, origin, "localhost")
	if res, body := ts.request("POST", "/api/v1/account/passkeys", map[string]string{"challengeId": begun.ChallengeID, "name": "Laptop", "clientDataJSON": cd, "attestationObject": att}); res.StatusCode != http.StatusOK {
		t.Fatalf("register: %d %s", res.StatusCode, body)
	}
	ts.request("POST", "/api/v1/session/lock", nil)
	if info := ts.sessionInfo(ts.client); !info.Locked || !info.Passkey {
		t.Fatalf("session: %+v", info)
	}
	unlock := func(tamper bool) (*http.Response, []byte) {
		res, body := ts.request("POST", "/api/v1/account/passkeys/confirm", nil)
		if res.StatusCode != http.StatusOK {
			t.Fatalf("confirm while locked: %d %s", res.StatusCode, body)
		}
		var c struct {
			ChallengeID string                     `json:"challengeId"`
			PublicKey   struct{ Challenge string } `json:"publicKey"`
		}
		json.Unmarshal(body, &c)
		challenge := c.PublicKey.Challenge
		if tamper {
			challenge = strings.Repeat("A", len(challenge))
		}
		id, cd, ad, sig, handle := key.Get(challenge, origin, "localhost")
		return ts.request("POST", "/api/v1/session/unlock", map[string]any{"passkey": map[string]string{"challengeId": c.ChallengeID, "id": id, "clientDataJSON": cd, "authenticatorData": ad, "signature": sig, "userHandle": handle}})
	}
	if res, body := unlock(true); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "passkey_failed" {
		t.Fatalf("tampered passkey: %d %s", res.StatusCode, body)
	}
	if res, body := unlock(false); res.StatusCode != http.StatusOK {
		t.Fatalf("passkey unlock: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK {
		t.Errorf("after passkey unlock: %d", res.StatusCode)
	}
}

func TestSessionLocksAfterInactivity(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	now := time.Now()
	ts.server.sessions.SetClockForTest(func() time.Time { return now })
	ts.request("GET", "/api/v1/system", nil)
	// The account chooses 5 minutes; other fields of the desktop stay.
	if res, _ := ts.request("PUT", "/api/v1/preferences/desktop", map[string]any{"lockMinutes": 5, "icons": []string{"files"}}); res.StatusCode != http.StatusNoContent {
		t.Fatalf("preferences: %d", res.StatusCode)
	}
	if info := ts.sessionInfo(ts.client); *info.LockMinutes != 5 {
		t.Fatalf("lockMinutes: %+v", info)
	}
	now = now.Add(4 * time.Minute)
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("locked before the time: %d", res.StatusCode)
	}
	now = now.Add(6 * time.Minute)
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusLocked {
		t.Fatalf("not locked after 6 idle minutes: %d", res.StatusCode)
	}
	if res, _ := ts.request("POST", "/api/v1/session/unlock", map[string]string{"password": testPassword}); res.StatusCode != http.StatusOK {
		t.Fatal("unlock")
	}
	// 0 switches the automatic lock off.
	ts.request("PUT", "/api/v1/preferences/desktop", map[string]any{"lockMinutes": 0})
	now = now.Add(48 * time.Hour)
	if res, _ := ts.request("GET", "/api/v1/system", nil); res.StatusCode != http.StatusOK {
		t.Errorf("locked with the automatic lock off: %d", res.StatusCode)
	}
}

func TestConsoleEndsWithTheSessionLock(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	alex, _ := ts.users.Get("alex")
	var token string
	for _, c := range ts.client.Jar.Cookies(mustURL(t, ts.url)) {
		if c.Name == ts.server.cookieName() {
			token = c.Value
		}
	}
	r := httptest.NewRequest("GET", "/api/v1/terminal/shell", nil)
	r.AddCookie(&http.Cookie{Name: ts.server.cookieName(), Value: token})
	if !ts.server.consoleAllowed(r, alex) {
		t.Fatal("console refused")
	}
	ts.request("POST", "/api/v1/session/lock", nil)
	if ts.server.consoleAllowed(r, alex) {
		t.Error("console of a locked session still allowed")
	}
}

func TestClampLockMinutes(t *testing.T) {
	for in, want := range map[float64]int{-3: 0, 0: 0, 0.4: 0, 1: 1, 15: 15, 9999: maxLockMinutes} {
		if got := clampLockMinutes(in); got != want {
			t.Errorf("clamp(%v) = %d, want %d", in, got, want)
		}
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
