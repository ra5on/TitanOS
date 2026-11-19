package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// testApp is the key of a phone in the tests.
type testApp struct {
	key *ecdsa.PrivateKey
}

func newTestApp(t *testing.T) testApp {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return testApp{key: key}
}

func (a testApp) public() string {
	der, _ := x509.MarshalPKIXPublicKey(&a.key.PublicKey)
	return base64.StdEncoding.EncodeToString(der)
}

func (a testApp) sign(text string) string {
	sum := sha256.Sum256([]byte(text))
	sig, _ := ecdsa.SignASN1(rand.Reader, a.key, sum[:])
	return base64.StdEncoding.EncodeToString(sig)
}

type pairStart struct {
	ID, Code, ManualCode, Link string
	Addresses                  []appAddress
}

// pairApp runs a pairing to the end: start in the browser, claim and
// wait in the app, confirm with the password. It returns the account and
// the device the app keeps.
func (ts *testServer) pairApp(app testApp, name string) (user, device string) {
	ts.t.Helper()
	res, body := ts.request("POST", "/api/v1/account/apps/pair", nil)
	var start pairStart
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &start) != nil || start.Code == "" {
		ts.t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	phone := ts.newClient()
	if res, body := ts.do(phone, "POST", "/api/v1/app/pair/claim", map[string]string{"code": start.Code, "name": name, "app": "titanos", "publicKey": app.public()}); res.StatusCode != http.StatusOK {
		ts.t.Fatalf("claim: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/apps/pair/"+start.ID+"/confirm", map[string]string{"password": testPassword}); res.StatusCode != http.StatusOK {
		ts.t.Fatalf("confirm: %d %s", res.StatusCode, body)
	}
	res, body = ts.do(phone, "POST", "/api/v1/app/pair/wait", map[string]string{"code": start.Code, "signature": app.sign("titanos-pair\n" + start.Code)})
	var waited struct{ State, Device, User string }
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &waited) != nil || waited.State != "confirmed" || waited.Device == "" {
		ts.t.Fatalf("wait: %d %s", res.StatusCode, body)
	}
	return waited.User, waited.Device
}

// appSession signs a challenge and returns a client with the session.
func (ts *testServer) appSession(app testApp, user, device string) (*http.Client, *http.Response, []byte) {
	ts.t.Helper()
	phone := ts.newClient()
	res, body := ts.do(phone, "POST", "/api/v1/app/challenge", map[string]string{"user": user, "device": device})
	var ch struct{ Nonce string }
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &ch) != nil || ch.Nonce == "" {
		ts.t.Fatalf("challenge: %d %s", res.StatusCode, body)
	}
	res, body = ts.do(phone, "POST", "/api/v1/app/session", map[string]string{"user": user, "device": device, "nonce": ch.Nonce,
		"signature": app.sign("titanos-app-session\n" + user + "\n" + device + "\n" + ch.Nonce)})
	if res.StatusCode != http.StatusOK {
		return nil, res, body
	}
	var got struct{ Cookie, Token string }
	json.Unmarshal(body, &got)
	// A native app has no cookie jar of the browser: it sends the header.
	client := &http.Client{Transport: cookieHeader{name: got.Cookie, value: got.Token}}
	return client, res, body
}

type cookieHeader struct{ name, value string }

func (c cookieHeader) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Cookie", c.name+"="+c.value)
	return http.DefaultTransport.RoundTrip(r)
}

func TestAppPairingAndSession(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	app := newTestApp(t)

	// The link carries the code and every address.
	res, body := ts.request("POST", "/api/v1/account/apps/pair", nil)
	var start pairStart
	json.Unmarshal(body, &start)
	link, err := url.Parse(start.Link)
	if res.StatusCode != http.StatusOK || err != nil || link.Scheme != "titanos" || link.Host != "pair" || link.Query().Get("c") != start.Code ||
		len(start.ManualCode) != 9 || start.ManualCode[4] != '-' {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	ts.request("DELETE", "/api/v1/account/apps/pair/"+start.ID, nil)

	user, device := ts.pairApp(app, "Pixel 9 von Alex")
	if user != "alex" {
		t.Fatalf("user %q", user)
	}
	// The stick holds the public key, nothing secret, and the session list
	// shows the app.
	stored, _ := os.ReadFile(filepath.Join(ts.store.Dir(), "users", "alex", "apps.json"))
	if !strings.Contains(string(stored), "Pixel 9 von Alex") || !strings.Contains(string(stored), app.public()) {
		t.Fatalf("stored %s", stored)
	}
	phone, res, body := ts.appSession(app, user, device)
	if phone == nil {
		t.Fatalf("session: %d %s", res.StatusCode, body)
	}
	if res, body := ts.do(phone, "GET", "/api/v1/session", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"alex"`) {
		t.Fatalf("session of the app: %d %s", res.StatusCode, body)
	}
	// A route only for the browser takes the app (it is a session, not a
	// token).
	if res, body := ts.do(phone, "GET", "/api/v1/account/apps", nil); res.StatusCode != http.StatusOK || !strings.Contains(string(body), device) {
		t.Fatalf("apps from the app: %d %s", res.StatusCode, body)
	}
	_, listed := ts.request("GET", "/api/v1/account/sessions", nil)
	if !strings.Contains(string(listed), `"device":"`+device+`"`) || !strings.Contains(string(listed), "TitanOS-Android/titanos Pixel 9 von Alex") {
		t.Fatalf("sessions %s", listed)
	}

	// Ending the app ends its session and its key.
	if res, body := ts.request("DELETE", "/api/v1/account/apps/"+device, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("end: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(phone, "GET", "/api/v1/session", nil); res.StatusCode == http.StatusOK {
		if _, b := ts.do(phone, "GET", "/api/v1/session", nil); strings.Contains(string(b), `"alex"`) {
			t.Fatalf("session of an ended app still works: %s", b)
		}
	}
	if client, res, body := ts.appSession(app, user, device); client != nil || res.StatusCode != http.StatusUnauthorized || errorCode(t, body) != "app_unknown" {
		t.Fatalf("session after end: %d %s", res.StatusCode, body)
	}
}

func TestAppPairingRefusals(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	app, other := newTestApp(t), newTestApp(t)
	res, body := ts.request("POST", "/api/v1/account/apps/pair", nil)
	var start pairStart
	json.Unmarshal(body, &start)
	phone := ts.newClient()
	claim := func(code, name, key string) (*http.Response, []byte) {
		return ts.do(phone, "POST", "/api/v1/app/pair/claim", map[string]string{"code": code, "name": name, "app": "titanos", "publicKey": key})
	}
	// Confirming before a claim costs no password attempt.
	if res, body := ts.request("POST", "/api/v1/account/apps/pair/"+start.ID+"/confirm", map[string]string{"password": "wrong"}); res.StatusCode != http.StatusConflict {
		t.Fatalf("confirm unclaimed: %d %s", res.StatusCode, body)
	}
	if res, body := claim("nope", "Pixel", app.public()); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "pairing_code" {
		t.Fatalf("bad code: %d %s", res.StatusCode, body)
	}
	if res, body := claim(start.Code, "a​b", app.public()); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_device_name" {
		t.Fatalf("bad name: %d %s", res.StatusCode, body)
	}
	if res, body := claim(start.Code, "Pixel", "AAAA"); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_key" {
		t.Fatalf("bad key: %d %s", res.StatusCode, body)
	}
	// The manual code, typed in lower case without the dash.
	manual := strings.ToLower(strings.ReplaceAll(start.ManualCode, "-", ""))
	if res, body := claim(manual, "Pixel", app.public()); res.StatusCode != http.StatusOK {
		t.Fatalf("manual code: %d %s", res.StatusCode, body)
	}
	// Once only, and the long code is spent with it.
	if res, _ := claim(start.Code, "Fremd", other.public()); res.StatusCode != http.StatusNotFound {
		t.Fatalf("second claim: %d", res.StatusCode)
	}
	// The browser sees who asks; a wrong password confirms nothing.
	_, status := ts.request("GET", "/api/v1/account/apps/pair/"+start.ID, nil)
	if !strings.Contains(string(status), `"claimed"`) || !strings.Contains(string(status), "Pixel") {
		t.Fatalf("status %s", status)
	}
	if res, _ := ts.request("POST", "/api/v1/account/apps/pair/"+start.ID+"/confirm", map[string]string{"password": "wrong"}); res.StatusCode == http.StatusOK {
		t.Fatal("confirmed with a wrong password")
	}
	// Another key cannot read the answer.
	if res, _ := ts.do(phone, "POST", "/api/v1/app/pair/wait", map[string]string{"code": manual, "signature": other.sign("titanos-pair\n" + strings.ToUpper(manual))}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("wait with another key: %d", res.StatusCode)
	}
	if res, body := ts.request("POST", "/api/v1/account/apps/pair/"+start.ID+"/reject", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("reject: %d %s", res.StatusCode, body)
	}
	// The app signs the code as typed (the Android app sends it with the
	// dash, as the browser shows it).
	res, body = ts.do(phone, "POST", "/api/v1/app/pair/wait", map[string]string{"code": start.ManualCode, "signature": app.sign("titanos-pair\n" + start.ManualCode)})
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"rejected"`) || strings.Contains(string(body), "device") {
		t.Fatalf("wait rejected: %d %s", res.StatusCode, body)
	}

	// The app routes take no API token's place: an API token cannot start
	// a pairing.
	token := ts.mcpToken("alex")
	req, _ := http.NewRequest("POST", ts.url+"/api/v1/account/apps/pair", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	if res, err := http.DefaultClient.Do(req); err != nil || res.StatusCode == http.StatusOK {
		t.Fatalf("pairing with a token: %v %d", err, res.StatusCode)
	}
}

func TestAppSessionRefusals(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	app, other := newTestApp(t), newTestApp(t)
	user, device := ts.pairApp(app, "Pixel")
	phone := ts.newClient()
	nonce := func() string {
		_, body := ts.do(phone, "POST", "/api/v1/app/challenge", map[string]string{"user": user, "device": device})
		var ch struct{ Nonce string }
		json.Unmarshal(body, &ch)
		return ch.Nonce
	}
	try := func(n, sig string) int {
		res, _ := ts.do(phone, "POST", "/api/v1/app/session", map[string]string{"user": user, "device": device, "nonce": n, "signature": sig})
		return res.StatusCode
	}
	n := nonce()
	if code := try(n, other.sign("titanos-app-session\n"+user+"\n"+device+"\n"+n)); code != http.StatusUnauthorized {
		t.Fatalf("other key: %d", code)
	}
	// The challenge was used up by the failure.
	if code := try(n, app.sign("titanos-app-session\n"+user+"\n"+device+"\n"+n)); code != http.StatusUnauthorized {
		t.Fatalf("reused nonce: %d", code)
	}
	// A challenge for another device does not open this one.
	_, body := ts.do(phone, "POST", "/api/v1/app/challenge", map[string]string{"user": user, "device": "other"})
	var ch struct{ Nonce string }
	json.Unmarshal(body, &ch)
	if code := try(ch.Nonce, app.sign("titanos-app-session\n"+user+"\n"+device+"\n"+ch.Nonce)); code != http.StatusUnauthorized {
		t.Fatalf("nonce of another device: %d", code)
	}
	// The right signature works.
	n = nonce()
	if code := try(n, app.sign("titanos-app-session\n"+user+"\n"+device+"\n"+n)); code != http.StatusOK {
		t.Fatalf("right signature: %d", code)
	}
	// A new password ends the app, on the stick too.
	if _, err := ts.users.SetPassword(user, "another long password"); err != nil {
		t.Fatal(err)
	}
	ts.server.devicesRevokeAll(user)
	n = nonce()
	if code := try(n, app.sign("titanos-app-session\n"+user+"\n"+device+"\n"+n)); code != http.StatusUnauthorized {
		t.Fatalf("after new password: %d", code)
	}
}

func TestAppDeviceEndsWithStay(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	app := newTestApp(t)
	user, device := ts.pairApp(app, "Pixel")
	now := time.Now()
	// "Stay logged in" off: 30 days after the pairing, like a browser.
	ts.server.appDevices.SetClockForTest(func() time.Time { return now.Add(auth.SessionMaxAge + time.Hour) })
	if client, res, _ := ts.appSession(app, user, device); client != nil || res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after 30 days: %d", res.StatusCode)
	}
	// With "no end" it stays.
	if err := ts.server.sessions.SetStay(auth.StayForever); err != nil {
		t.Fatal(err)
	}
	if client, res, body := ts.appSession(app, user, device); client == nil {
		t.Fatalf("with no end: %d %s", res.StatusCode, body)
	}
}
