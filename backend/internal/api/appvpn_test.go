package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/wgserver"
)

type appVPNTestAnswer struct {
	Available    bool
	Reason       string
	MayCreate    bool
	PresharedKey string
	Peer         *struct {
		Name, Address, PublicKey string
		Active                   bool
	}
	Server *struct {
		PublicKey, Address, Network string
		Port, Keepalive             int
		Endpoints                   []string
		NASAddresses                []string
		NAS                         struct{ Server, SPKI string }
	}
}

func (ts *testServer) appVPN(phone *http.Client, method string, body any) (int, appVPNTestAnswer, []byte) {
	ts.t.Helper()
	res, raw := ts.do(phone, method, "/api/v1/app/vpn", body)
	var got appVPNTestAnswer
	_ = json.Unmarshal(raw, &got)
	if res.StatusCode == http.StatusOK && res.Header.Get("Cache-Control") != "no-store" {
		ts.t.Fatalf("%s without no-store", method)
	}
	return res.StatusCode, got, raw
}

// pairAppWith pairs an app for the account logged in at browser.
func (ts *testServer) pairAppWith(browser *http.Client, app testApp, name string) (user, device string) {
	ts.t.Helper()
	res, body := ts.do(browser, "POST", "/api/v1/account/apps/pair", nil)
	var start pairStart
	if res.StatusCode != http.StatusOK || json.Unmarshal(body, &start) != nil {
		ts.t.Fatalf("start: %d %s", res.StatusCode, body)
	}
	phone := ts.newClient()
	if res, body := ts.do(phone, "POST", "/api/v1/app/pair/claim", map[string]string{"code": start.Code, "name": name, "app": "titanos", "publicKey": app.public()}); res.StatusCode != http.StatusOK {
		ts.t.Fatalf("claim: %d %s", res.StatusCode, body)
	}
	if res, body := ts.do(browser, "POST", "/api/v1/account/apps/pair/"+start.ID+"/confirm", map[string]string{"password": testPassword}); res.StatusCode != http.StatusOK {
		ts.t.Fatalf("confirm: %d %s", res.StatusCode, body)
	}
	res, body = ts.do(phone, "POST", "/api/v1/app/pair/wait", map[string]string{"code": start.Code, "signature": app.sign("titanos-pair\n" + start.Code)})
	var waited struct{ State, Device, User string }
	if json.Unmarshal(body, &waited) != nil || waited.State != "confirmed" {
		ts.t.Fatalf("wait: %d %s", res.StatusCode, body)
	}
	return waited.User, waited.Device
}

func wgPublic(t *testing.T) string {
	t.Helper()
	_, public, err := wgserver.NewKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	return public
}

func TestAppVPN(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	app := newTestApp(t)
	user, device := ts.pairApp(app, "Pixel 10 Pro XL von Alex")
	phone, _, _ := ts.appSession(app, user, device)

	// Not for the browser, not without a login.
	if res, body := ts.request("GET", "/api/v1/app/vpn", nil); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "app_session_required" {
		t.Fatalf("browser: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.do(ts.newClient(), "POST", "/api/v1/app/vpn", map[string]string{"publicKey": wgPublic(t)}); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", res.StatusCode)
	}

	// WireGuard off: said in GET, refused with its own code.
	code, got, raw := ts.appVPN(phone, "GET", nil)
	if code != 200 || got.Available || got.Reason != "wireguard_off" || got.Peer != nil || !got.MayCreate {
		t.Fatalf("off: %d %s", code, raw)
	}
	if code, _, raw := ts.appVPN(phone, "POST", map[string]string{"publicKey": wgPublic(t)}); code != http.StatusConflict || errorCode(t, raw) != "wireguard_off" {
		t.Fatalf("create while off: %d %s", code, raw)
	}
	// On, but no address from the internet (no DDNS in the test either).
	if res, body := ts.request("PUT", "/api/v1/network/wireguard", map[string]any{"enabled": true}); res.StatusCode != 200 {
		t.Fatalf("switch on: %d %s", res.StatusCode, body)
	}
	if code, _, raw := ts.appVPN(phone, "POST", map[string]string{"publicKey": wgPublic(t)}); code != http.StatusConflict || errorCode(t, raw) != "wireguard_no_endpoint" {
		t.Fatalf("no endpoint: %d %s", code, raw)
	}
	if res, body := ts.request("PUT", "/api/v1/network/wireguard", map[string]any{"enabled": true, "endpoint": "nas.example.org"}); res.StatusCode != 200 {
		t.Fatalf("endpoint: %d %s", res.StatusCode, body)
	}
	if code, _, raw := ts.appVPN(phone, "POST", map[string]string{"publicKey": "kein Schlüssel"}); code != http.StatusBadRequest || errorCode(t, raw) != "invalid_key" {
		t.Fatalf("bad key: %d %s", code, raw)
	}

	key := wgPublic(t)
	code, made, raw := ts.appVPN(phone, "POST", map[string]string{"publicKey": key})
	if code != 200 || !made.Available || made.Peer == nil || made.Peer.PublicKey != key || made.PresharedKey == "" || made.Server == nil ||
		made.Server.Endpoints[0] != "nas.example.org" || made.Server.Port != wgserver.DefaultPort || made.Server.Keepalive != 25 ||
		!strings.HasPrefix(made.Server.Address, "10.") || made.Server.NAS.Server == "" {
		t.Fatalf("create: %d %s", code, raw)
	}
	if strings.Contains(string(raw), "PrivateKey") || strings.Contains(string(raw), "privateKey") {
		t.Fatalf("a private key in the answer: %s", raw)
	}
	// GET never repeats the preshared key.
	code, got, raw = ts.appVPN(phone, "GET", nil)
	if code != 200 || got.Peer == nil || got.Peer.Address != made.Peer.Address || got.PresharedKey != "" || strings.Contains(string(raw), made.PresharedKey) {
		t.Fatalf("get: %d %s", code, raw)
	}
	// Shown under WireGuard with the account, without secrets.
	_, status := ts.request("GET", "/api/v1/network/wireguard", nil)
	if !strings.Contains(string(status), `"account":"alex"`) || !strings.Contains(string(status), "Pixel 10 Pro XL von Alex") || strings.Contains(string(status), made.PresharedKey) {
		t.Fatalf("status: %s", status)
	}
	// Make anew: the same address, the old key gone.
	again := wgPublic(t)
	if code, renewed, raw := ts.appVPN(phone, "POST", map[string]string{"publicKey": again}); code != 200 || renewed.Peer.Address != made.Peer.Address || renewed.PresharedKey == made.PresharedKey {
		t.Fatalf("anew: %d %s", code, raw)
	}
	if n := len(ts.server.opts.WireGuard.Settings().Peers); n != 1 {
		t.Fatalf("%d devices after making anew", n)
	}
	// Remove.
	if code, _, raw := ts.appVPN(phone, "DELETE", nil); code != 200 || !strings.Contains(string(raw), `"removed":true`) {
		t.Fatalf("remove: %d %s", code, raw)
	}
	if code, _, raw := ts.appVPN(phone, "DELETE", nil); code != 200 || !strings.Contains(string(raw), `"removed":false`) {
		t.Fatalf("remove again: %d %s", code, raw)
	}

	// Ending the app in Mein Konto › Geräte takes the device along.
	if code, _, raw := ts.appVPN(phone, "POST", map[string]string{"publicKey": wgPublic(t)}); code != 200 {
		t.Fatalf("create for ending: %d %s", code, raw)
	}
	if res, body := ts.request("DELETE", "/api/v1/account/apps/"+device, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("end app: %d %s", res.StatusCode, body)
	}
	if peers := ts.server.opts.WireGuard.Settings().Peers; len(peers) != 0 {
		t.Fatalf("device stayed after the app ended: %+v", peers)
	}
}

func TestAppVPNRightsAndEnds(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	if res, body := ts.request("PUT", "/api/v1/network/wireguard", map[string]any{"enabled": true, "endpoint": "nas.example.org"}); res.StatusCode != 200 {
		t.Fatalf("switch on: %d %s", res.StatusCode, body)
	}
	login := func(name, role string) *http.Client {
		if res, body := ts.request("POST", "/api/v1/users", map[string]any{"name": name, "password": testPassword, "role": role}); res.StatusCode != http.StatusCreated {
			t.Fatalf("user %s: %d %s", name, res.StatusCode, body)
		}
		c := ts.newClient()
		if res, body := ts.do(c, "POST", "/api/v1/login", credentials{Name: name, Password: testPassword}); res.StatusCode != 200 {
			t.Fatalf("login %s: %d %s", name, res.StatusCode, body)
		}
		return c
	}
	// A user without administration: may not make a device, is told why.
	reader := login("reader", "user")
	readerApp := newTestApp(t)
	u, d := ts.pairAppWith(reader, readerApp, "Pixel von Reader")
	readerPhone, _, _ := ts.appSession(readerApp, u, d)
	// Without the right to add a device the server's key, networks and endpoints stay hidden.
	if code, got, raw := ts.appVPN(readerPhone, "GET", nil); code != 200 || got.Available || got.Reason != "wireguard_admin_only" || got.MayCreate || got.Server != nil {
		t.Fatalf("reader get: %d %s", code, raw)
	}
	if code, _, raw := ts.appVPN(readerPhone, "POST", map[string]string{"publicKey": wgPublic(t)}); code != http.StatusForbidden || errorCode(t, raw) != "wireguard_admin_only" {
		t.Fatalf("reader create: %d %s", code, raw)
	}

	// An administrator loses the device when the role goes, when locked
	// and when deleted.
	for _, end := range []string{"role", "lock", "delete"} {
		name := "bea" + end
		admin := login(name, "admin")
		app := newTestApp(t)
		u, d := ts.pairAppWith(admin, app, "Pixel von Bea")
		phone, _, _ := ts.appSession(app, u, d)
		if code, _, raw := ts.appVPN(phone, "POST", map[string]string{"publicKey": wgPublic(t)}); code != 200 {
			t.Fatalf("%s create: %d %s", end, code, raw)
		}
		if _, ok := ts.server.opts.WireGuard.AppPeer(name, d); !ok {
			t.Fatalf("%s: no device", end)
		}
		var res *http.Response
		var body []byte
		switch end {
		case "role":
			res, body = ts.request("PUT", "/api/v1/users/"+name, map[string]any{"role": "user"})
		case "lock":
			res, body = ts.request("POST", "/api/v1/users/"+name+"/lock", nil)
		case "delete":
			res, body = ts.request("DELETE", "/api/v1/users/"+name, map[string]any{})
		}
		if res.StatusCode >= 300 {
			t.Fatalf("%s: %d %s", end, res.StatusCode, body)
		}
		if _, ok := ts.server.opts.WireGuard.AppPeer(name, d); ok {
			t.Fatalf("%s: the device stayed", end)
		}
	}

	// A password change ends the apps and their devices.
	admin := login("carla", "admin")
	app := newTestApp(t)
	u, d = ts.pairAppWith(admin, app, "Pixel von Carla")
	phone, _, _ := ts.appSession(app, u, d)
	if code, _, raw := ts.appVPN(phone, "POST", map[string]string{"publicKey": wgPublic(t)}); code != 200 {
		t.Fatalf("carla create: %d %s", code, raw)
	}
	ts.server.devicesRevokeAll("carla")
	if _, ok := ts.server.opts.WireGuard.AppPeer("carla", d); ok {
		t.Fatal("device stayed after the credentials changed")
	}

	// Ran out by "stay logged in": out of the tunnel at the hourly check
	// (RAM only), removed from the stick at the next start.
	owner := newTestApp(t)
	u, d = ts.pairApp(owner, "Pixel von Alex")
	phone, _, _ = ts.appSession(owner, u, d)
	if code, _, raw := ts.appVPN(phone, "POST", map[string]string{"publicKey": wgPublic(t)}); code != 200 {
		t.Fatalf("alex create: %d %s", code, raw)
	}
	wg := ts.server.opts.WireGuard
	ts.server.appDevices.SetClockForTest(func() time.Time { return time.Now().Add(60 * 24 * time.Hour) })
	if err := wg.Recheck(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p, ok := wg.PeerStatus(mustAppPeer(t, wg, "alex", d).PublicKey); !ok || p.Active {
		t.Fatalf("ran out but active: %+v", p)
	}
	ts.server.appVPNPrune()
	if _, ok := wg.AppPeer("alex", d); ok {
		t.Fatal("ran out but stored")
	}
}

func mustAppPeer(t *testing.T, wg *wgserver.Manager, account, device string) wgserver.Peer {
	t.Helper()
	p, ok := wg.AppPeer(account, device)
	if !ok {
		t.Fatalf("no device of %s", account)
	}
	return p
}
