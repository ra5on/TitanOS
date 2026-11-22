package api

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/appdevices"
	"github.com/ra5on/TitanOS/backend/internal/tlscert"
)

// watchStart runs start and reveal of a watch and returns its id and the
// digits it shows.
func watchStart(t *testing.T, ts *testServer, client *http.Client, watch testApp, secret, spki string) (string, string) {
	t.Helper()
	res, body := ts.do(client, "POST", "/api/v1/app/pair/watch", map[string]string{"name": "Pixel Watch 3", "publicKey": watch.public(),
		"commit": appdevices.WatchCommit(watch.public(), spki, false, secret)})
	var started struct{ ID, Nonce string }
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &started) != nil || started.ID == "" || started.Nonce == "" {
		t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	if res, body := ts.do(client, "POST", "/api/v1/app/pair/watch/"+started.ID+"/reveal", map[string]any{"secret": secret, "spki": spki}); res.StatusCode != http.StatusOK {
		t.Fatalf("reveal: %d %s", res.StatusCode, body)
	}
	return started.ID, appdevices.WatchCode(started.ID, watch.public(), started.Nonce, secret, spki, false)
}

// The watch shows six digits, the paired phone app confirms them, the
// watch gets its own device and session (docs/ANDROID-APP.md "Die Uhr").
func TestAppWatchPairing(t *testing.T) {
	var certs *tlscert.Manager
	ts := newTestServerWith(t, func(o *Options) {
		m, err := tlscert.NewManager(o.Store, "nas")
		if err != nil {
			t.Fatal(err)
		}
		certs = m
		o.Certificate = m
	})
	ts.setup()
	cert, err := certs.GetCertificate(&tls.ClientHelloInfo{})
	if err != nil {
		t.Fatal(err)
	}
	spki := spkiOf(cert)
	titan, watch := newTestApp(t), newTestApp(t)
	user, device := ts.pairApp(titan, "Pixel 10 Pro XL")
	phone, res, body := ts.appSession(titan, user, device)
	if phone == nil {
		t.Fatalf("session: %d %s", res.StatusCode, body)
	}
	wclient := ts.newClient()

	// Another certificate than the NAS serves: someone in between.
	res, body = ts.do(wclient, "POST", "/api/v1/app/pair/watch", map[string]string{"name": "W", "publicKey": watch.public(), "commit": appdevices.WatchCommit(watch.public(), "b3RoZXI", false, "s1")})
	var bad struct{ ID string }
	json.Unmarshal(body, &bad)
	if res, body := ts.do(wclient, "POST", "/api/v1/app/pair/watch/"+bad.ID+"/reveal", map[string]any{"secret": "s1", "spki": "b3RoZXI"}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "watch_certificate" {
		t.Fatalf("other certificate: %d %s", res.StatusCode, body)
	}

	// A "public certificate" counts only for a name with a valid public
	// certificate; the test server has none.
	res, body = ts.do(wclient, "POST", "/api/v1/app/pair/watch", map[string]string{"name": "W", "publicKey": watch.public(), "commit": appdevices.WatchCommit(watch.public(), "b3RoZXI", true, "s2")})
	json.Unmarshal(body, &bad)
	if res, body := ts.do(wclient, "POST", "/api/v1/app/pair/watch/"+bad.ID+"/reveal", map[string]any{"secret": "s2", "spki": "b3RoZXI", "publicCert": true}); res.StatusCode != http.StatusConflict || errorCode(t, body) != "watch_certificate" {
		t.Fatalf("public claimed: %d %s", res.StatusCode, body)
	}

	id, code := watchStart(t, ts, wclient, watch, "secret-of-the-watch", spki)
	sign := map[string]string{"signature": watch.sign("titanos-watch-wait\n" + id)}
	if res, body := ts.do(wclient, "POST", "/api/v1/app/pair/watch/"+id+"/wait", sign); res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"revealed"`) {
		t.Fatalf("wait before: %d %s", res.StatusCode, body)
	}

	// Only a paired app confirms; the browser has no say.
	if res, body := ts.request("POST", "/api/v1/app/pair/watch/confirm", map[string]string{"code": code}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "app_session_required" {
		t.Fatalf("from the browser: %d %s", res.StatusCode, body)
	}
	if res, body := ts.do(wclient, "POST", "/api/v1/app/pair/watch/confirm", map[string]string{"code": code}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("without login: %d %s", res.StatusCode, body)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "000001"
	}
	if res, body := ts.do(phone, "POST", "/api/v1/app/pair/watch/confirm", map[string]string{"code": wrong}); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "pairing_code" {
		t.Fatalf("wrong code: %d %s", res.StatusCode, body)
	}
	res, body = ts.do(phone, "POST", "/api/v1/app/pair/watch/confirm", map[string]string{"code": code[:3] + " " + code[3:]})
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"app":"watch"`) {
		t.Fatalf("confirm: %d %s", res.StatusCode, body)
	}
	res, body = ts.do(wclient, "POST", "/api/v1/app/pair/watch/"+id+"/wait", sign)
	var waited struct {
		State, Device, User string
		Addresses           []appAddress
	}
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &waited) != nil || waited.State != "confirmed" || waited.User != user || waited.Device == "" {
		t.Fatalf("wait: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(wclient, "POST", "/api/v1/app/pair/watch/"+id+"/wait", sign); res.StatusCode != http.StatusNotFound {
		t.Fatalf("told twice: %d", res.StatusCode)
	}
	wsession, res, body := ts.appSession(watch, waited.User, waited.Device)
	if wsession == nil {
		t.Fatalf("watch session: %d %s", res.StatusCode, body)
	}
	if res, body := ts.do(wsession, "GET", "/api/v1/app/notifications", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("notifications from the watch: %d %s", res.StatusCode, body)
	}
	_, listed := ts.request("GET", "/api/v1/account/apps", nil)
	if !strings.Contains(string(listed), `"app":"watch"`) || !strings.Contains(string(listed), `"Pixel Watch 3"`) {
		t.Fatalf("apps %s", listed)
	}

	// Least rights (security review 22.11.2026): the watch gets what it
	// needs, nothing else; the phone app keeps everything.
	for _, ok := range []string{"/api/v1/system", "/api/v1/storage", "/api/v1/agent/chats", "/api/v1/nvr/cameras"} {
		if res, body := ts.do(wsession, "GET", ok, nil); res.StatusCode == http.StatusForbidden && errorCode(t, body) == "watch_forbidden" {
			t.Fatalf("watch kept from %s", ok)
		}
	}
	for _, c := range []struct{ method, path string }{
		{"GET", "/api/v1/app/vpn"}, {"POST", "/api/v1/app/vpn"}, {"GET", "/api/v1/app/pim/contacts"},
		{"POST", "/api/v1/app/photos/check"}, {"GET", "/api/v1/files/roots"}, {"GET", "/api/v1/account/apps"},
		{"GET", "/api/v1/account/sessions"}, {"PUT", "/api/v1/agent/settings"}, {"GET", "/api/v1/users"},
		{"POST", "/api/v1/docker/containers/x/remove"}, {"POST", "/api/v1/vms/x/poweroff"},
	} {
		if res, body := ts.do(wsession, c.method, c.path, map[string]any{}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "watch_forbidden" {
			t.Fatalf("watch %s %s: %d %s", c.method, c.path, res.StatusCode, body)
		}
	}
	// Home Assistant: switching and scenes yes, a lock no.
	if res, body := ts.do(wsession, "POST", "/api/v1/homeassistant/call", map[string]any{"domain": "lock", "service": "unlock", "entities": []string{"lock.door"}}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "watch_forbidden" {
		t.Fatalf("watch unlocks: %d %s", res.StatusCode, body)
	}
	if res, body := ts.do(wsession, "POST", "/api/v1/homeassistant/call", map[string]any{"domain": "light", "service": "turn_on", "entities": []string{"light.kitchen"}}); res.StatusCode == http.StatusForbidden && errorCode(t, body) == "watch_forbidden" {
		t.Fatalf("watch may not switch a light: %s", body)
	}
	if res, body := ts.do(phone, "GET", "/api/v1/files/roots", nil); res.StatusCode != http.StatusOK {
		t.Fatalf("phone kept from files: %d %s", res.StatusCode, body)
	}

	// The watch confirms no other watch and asks for no sibling code.
	other := newTestApp(t)
	_, code2 := watchStart(t, ts, ts.newClient(), other, "second", spki)
	if res, body := ts.do(wsession, "POST", "/api/v1/app/pair/watch/confirm", map[string]string{"code": code2}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "watch_forbidden" {
		t.Fatalf("watch confirms watch: %d %s", res.StatusCode, body)
	}
	if res, body := ts.do(wsession, "POST", "/api/v1/app/pair/sibling", map[string]string{"app": "titanos"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "watch_forbidden" {
		t.Fatalf("sibling from the watch: %d %s", res.StatusCode, body)
	}
	if res, body := ts.do(phone, "POST", "/api/v1/app/pair/sibling", map[string]string{"app": "watch"}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_app" {
		t.Fatalf("sibling code for a watch: %d %s", res.StatusCode, body)
	}

	// Wrong codes count: after a few the phone waits.
	limited := false
	for i := 0; i < 20 && !limited; i++ {
		res, _ := ts.do(phone, "POST", "/api/v1/app/pair/watch/confirm", map[string]string{"code": "99999" + string(rune('0'+i%10))})
		limited = res.StatusCode == http.StatusTooManyRequests
	}
	if !limited {
		t.Fatal("wrong codes are not limited")
	}
}

func TestWatchAddressCountsIPv6AsPrefix(t *testing.T) {
	if watchAddress("192.0.2.7") != "192.0.2.7" {
		t.Fatal("IPv4")
	}
	if a, b := watchAddress("2001:db8:1:2::1"), watchAddress("2001:db8:1:2:ffff::9"); a != b || a != "2001:db8:1:2::/64" {
		t.Fatalf("IPv6 %s %s", a, b)
	}
}
