package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/activity"
	"github.com/ra5on/TitanOS/backend/internal/auth"
	"github.com/ra5on/TitanOS/backend/internal/pim/profile"
)

type profileAnswer struct {
	Link   profile.Link `json:"link"`
	URL    string       `json:"url"`
	Path   string       `json:"path"`
	Signed bool         `json:"signed"`
}

// newProfileLink asks for a link in the session of client.
func (ts *testServer) newProfileLink(client *http.Client, body map[string]any) profileAnswer {
	ts.t.Helper()
	if _, ok := body["password"]; !ok {
		body["password"] = testPassword
	}
	res, data := ts.do(client, "POST", "/api/v1/account/devices/profile", body)
	var a profileAnswer
	if res.StatusCode != http.StatusOK || json.Unmarshal(data, &a) != nil || a.Path == "" {
		ts.t.Fatalf("new link: %d %s", res.StatusCode, data)
	}
	if res.Header.Get("Cache-Control") != "no-store" {
		ts.t.Fatal("link without no-store")
	}
	return a
}

// fetch is the iPhone: no cookie, no token of the API.
func (ts *testServer) fetchProfile(path string, header map[string]string) (*http.Response, []byte) {
	ts.t.Helper()
	req, _ := http.NewRequest("GET", ts.url+path, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		ts.t.Fatal(err)
	}
	defer res.Body.Close()
	var b strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := res.Body.Read(buf)
		b.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return res, []byte(b.String())
}

// profilePassword is the device password a profile carries.
func profilePassword(t *testing.T, body []byte) string {
	t.Helper()
	s := string(body)
	_, rest, ok := strings.Cut(s, "<key>CalDAVPassword</key><string>")
	if !ok {
		t.Fatalf("no password in %s", s)
	}
	pw, _, _ := strings.Cut(rest, "</string>")
	return pw
}

func TestProfileLinkFetchedOnce(t *testing.T) {
	var log *activity.Manager
	ts := newTestServerWith(t, func(o *Options) {
		log = activity.New(o.Store)
		o.Activity = log
	})
	ts.setup()
	// A session alone makes none; a bad name costs no attempt.
	if res, body := ts.request("POST", "/api/v1/account/devices/profile", map[string]any{"name": "iPhone"}); res.StatusCode != http.StatusForbidden || errorCode(t, body) != "wrong_password" {
		t.Fatalf("without password: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("POST", "/api/v1/account/devices/profile", map[string]any{"name": "", "password": "wrong"}); res.StatusCode != http.StatusBadRequest || errorCode(t, body) != "invalid_device_name" {
		t.Fatalf("bad name: %d %s", res.StatusCode, body)
	}
	for _, bad := range []map[string]any{{"server": "evil.example.org"}, {"holidays": "de-xx"}, {"holidays": "at"}} {
		bad["name"], bad["password"] = "iPhone", testPassword
		if res, body := ts.request("POST", "/api/v1/account/devices/profile", bad); res.StatusCode != http.StatusBadRequest {
			t.Fatalf("%v: %d %s", bad, res.StatusCode, body)
		}
	}
	a := ts.newProfileLink(ts.client, map[string]any{"name": "iPhone von Alex", "holidays": "de-by"})
	token := strings.TrimPrefix(a.Path, profilePath)
	if !profile.ValidToken(token) || !strings.HasSuffix(a.URL, a.Path) || !strings.HasPrefix(a.URL, "https://") || a.Link.State != profile.StatePending || a.Signed {
		t.Fatalf("answer %+v", a)
	}
	// No device before the fetch, nothing on the stick.
	if n := len(ts.deviceList(ts.client, "/api/v1/account/devices")); n != 0 {
		t.Fatalf("%d devices before the fetch", n)
	}
	if _, err := os.Stat(filepath.Join(ts.store.Dir(), "users", "alex", "devices.json")); err == nil {
		t.Fatal("devices.json written before the fetch")
	}
	res, body := ts.fetchProfile(a.Path, nil)
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != profile.ContentType || res.Header.Get("Cache-Control") != "no-store" ||
		res.Header.Get("Content-Disposition") != `attachment; filename="TitanOS.mobileconfig"` || res.Header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("fetch: %d %v %s", res.StatusCode, res.Header, body)
	}
	for _, want := range []string{"com.apple.caldav.account", "com.apple.carddav.account", "com.apple.subscribedcalendar.account",
		"<key>CalDAVUsername</key><string>alex</string>", "/api/v1/holidays/de-by.ics", "Feiertage (Bayern)"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("profile without %s", want)
		}
	}
	password := profilePassword(t, body)
	if id, err := ts.calAuth("alex", password); err != nil || id.Name != "iPhone von Alex" {
		t.Fatalf("device password of the profile: %v %v", id, err)
	}
	// Once only.
	if res, body := ts.fetchProfile(a.Path, nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "profile_link_invalid" {
		t.Fatalf("second fetch: %d %s", res.StatusCode, body)
	}
	_, st := ts.request("GET", "/api/v1/account/devices/profile/"+a.Link.ID, nil)
	var link profile.Link
	if json.Unmarshal(st, &link); link.State != profile.StateFetched {
		t.Fatalf("state %s", st)
	}
	// The activity log names account and device, never the token or the password.
	rows := activityEntries(t, log)
	if e, ok := findActivity(rows, "iphone_profile"); !ok || e.User != "alex" || e.Target != "iPhone von Alex" || !e.OK {
		t.Fatalf("activity %+v", rows)
	}
	if e, ok := findActivity(rows, "POST /api/v1/account/devices/profile"); !ok || e.Target != "iPhone von Alex" {
		t.Fatalf("activity of the link %+v", rows)
	}
	all, _ := json.Marshal(rows)
	if strings.Contains(string(all), token) || strings.Contains(string(all), password) || strings.Contains(string(all), testPassword) {
		t.Fatalf("secret in the activity log: %s", all)
	}
}

func TestProfileLinkExpiresAndIsForeign(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	a := ts.newProfileLink(ts.client, map[string]any{"name": "iPhone"})
	ts.server.profileLinks.Now = func() time.Time { return time.Now().Add(profile.TTL + time.Second) }
	if res, body := ts.fetchProfile(a.Path, nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "profile_link_invalid" {
		t.Fatalf("expired: %d %s", res.StatusCode, body)
	}
	ts.server.profileLinks.Now = nil
	if n := len(ts.deviceList(ts.client, "/api/v1/account/devices")); n != 0 {
		t.Fatalf("%d devices after an expired link", n)
	}
	// Another account neither sees nor cancels a link of alex.
	b := ts.newProfileLink(ts.client, map[string]any{"name": "iPad"})
	bea := ts.guest("bea")
	for _, m := range []string{"GET", "DELETE"} {
		if res, body := ts.do(bea, m, "/api/v1/account/devices/profile/"+b.Link.ID, nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "profile_link_not_found" {
			t.Fatalf("bea %s: %d %s", m, res.StatusCode, body)
		}
	}
	// Bea's own session in the browser that fetches does not matter: the
	// profile is alex's.
	res, body := ts.do(bea, "GET", b.Path, nil)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), "<key>CardDAVUsername</key><string>alex</string>") {
		t.Fatalf("fetch with bea's cookie: %d %s", res.StatusCode, body)
	}
	// Cancelled: gone.
	c := ts.newProfileLink(ts.client, map[string]any{"name": "Mac"})
	if res, _ := ts.request("DELETE", "/api/v1/account/devices/profile/"+c.Link.ID, nil); res.StatusCode != http.StatusNoContent {
		t.Fatalf("cancel: %d", res.StatusCode)
	}
	if res, _ := ts.fetchProfile(c.Path, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("cancelled link fetched: %d", res.StatusCode)
	}
}

// A changed password, an ended login or a deleted account between the
// link and its fetch: no device.
func TestProfileLinkBoundToAccountAndLogin(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	a := ts.newProfileLink(ts.client, map[string]any{"name": "iPhone"})
	// Changed underneath (the routes would also call Forget; here the
	// check of the fetch itself).
	if _, err := ts.users.SetPassword("alex", testPassword+"x"); err != nil {
		t.Fatal(err)
	}
	if res, body := ts.fetchProfile(a.Path, nil); res.StatusCode != http.StatusGone || errorCode(t, body) != "profile_link_void" {
		t.Fatalf("after a new password: %d %s", res.StatusCode, body)
	}
	if _, err := ts.users.SetPassword("alex", testPassword); err != nil {
		t.Fatal(err)
	}
	client := ts.newClient()
	if res, _ := ts.do(client, "POST", "/api/v1/login", credentials{Name: "alex", Password: testPassword}); res.StatusCode != http.StatusOK {
		t.Fatal("login")
	}
	b := ts.newProfileLink(client, map[string]any{"name": "iPhone"})
	if res, _ := ts.do(client, "POST", "/api/v1/logout", nil); res.StatusCode >= 400 {
		t.Fatal("logout")
	}
	if res, body := ts.fetchProfile(b.Path, nil); res.StatusCode != http.StatusGone || errorCode(t, body) != "profile_link_void" {
		t.Fatalf("after logout: %d %s", res.StatusCode, body)
	}
	_, st := ts.do(ts.client, "GET", "/api/v1/account/devices", nil)
	if strings.Contains(string(st), "iPhone") {
		t.Fatalf("device made: %s", st)
	}
	// The password changed through the page: the link is forgotten at once.
	c := ts.newProfileLink(ts.client, map[string]any{"name": "iPhone"})
	ts.server.devicesRevokeAll("alex")
	if res, _ := ts.fetchProfile(c.Path, nil); res.StatusCode != http.StatusNotFound {
		t.Fatalf("after revoke all: %d", res.StatusCode)
	}
}

// HEAD, prefetch, an API token and the cluster never use a link up.
func TestProfileLinkNotUsedUp(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	a := ts.newProfileLink(ts.client, map[string]any{"name": "iPhone"})
	req, _ := http.NewRequest("HEAD", ts.url+a.Path, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("HEAD: %v %v", res, err)
	}
	res.Body.Close()
	for _, h := range []map[string]string{{"Sec-Purpose": "prefetch"}, {"Purpose": "prefetch"}, {"X-Purpose": "preview"}} {
		if res, _ := ts.fetchProfile(a.Path, h); res.StatusCode != http.StatusForbidden {
			t.Fatalf("%v: %d", h, res.StatusCode)
		}
	}
	token := ts.mcpToken("alex", "system:read", "system:write", "files:read", "files:write")
	if res, _ := ts.fetchProfile(a.Path, map[string]string{"Authorization": "Bearer " + token}); res.StatusCode != http.StatusForbidden && res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("with an API token: %d", res.StatusCode)
	}
	if res, _ := ts.fetchProfile(a.Path, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("after the refusals: %d", res.StatusCode)
	}
}

func TestProfileLinkGuessingIsLimited(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	a := ts.newProfileLink(ts.client, map[string]any{"name": "iPhone"})
	wrong := profilePath + strings.Repeat("A", 43)
	for i := range 5 {
		if res, body := ts.fetchProfile(wrong, nil); res.StatusCode != http.StatusNotFound || errorCode(t, body) != "profile_link_invalid" {
			t.Fatalf("guess %d: %d %s", i, res.StatusCode, body)
		}
	}
	// Malformed and unknown: the same answer.
	res, body := ts.fetchProfile(profilePath+"x", nil)
	if res.StatusCode != http.StatusTooManyRequests || res.Header.Get("Retry-After") == "" {
		t.Fatalf("after five guesses: %d %s", res.StatusCode, body)
	}
	// Blocked: not even the right token is looked at (and not used up).
	if res, _ := ts.fetchProfile(a.Path, nil); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("right token while blocked: %d", res.StatusCode)
	}
	ts.server.limiter = auth.NewLimiter()
	if res, _ := ts.fetchProfile(a.Path, nil); res.StatusCode != http.StatusOK {
		t.Fatalf("after the block: %d", res.StatusCode)
	}
}

// selfSigned is a certificate for name with an ECDSA key.
func selfSigned(t *testing.T, name string, notAfter time.Time) (*tls.Certificate, *x509.Certificate) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(7), Subject: pkix.Name{CommonName: name}, DNSNames: []string{name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return &tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, leaf
}

func TestProfileSigned(t *testing.T) {
	ts := newTestServer(t)
	ts.setup()
	cert, _ := selfSigned(t, "titan.tail1a2b3.ts.net", time.Now().Add(time.Hour))
	ts.server.profileSigner = func() *tls.Certificate { return cert }
	_, info := ts.request("GET", "/api/v1/account/devices/profile", nil)
	if !strings.Contains(string(info), `"signed":true`) || !strings.Contains(string(info), `"signer":"titan.tail1a2b3.ts.net"`) {
		t.Fatalf("info %s", info)
	}
	a := ts.newProfileLink(ts.client, map[string]any{"name": "iPhone", "lang": "en"})
	if !a.Signed {
		t.Fatal("not signed")
	}
	res, body := ts.fetchProfile(a.Path, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("fetch %d", res.StatusCode)
	}
	content, _, err := profile.Verify(body)
	if err != nil || !strings.Contains(string(content), "TitanOS Calendar") {
		t.Fatalf("signed profile: %v %s", err, content)
	}
	// Without a trusted certificate: unsigned, and the page says so.
	ts.server.profileSigner = func() *tls.Certificate { return nil }
	_, info = ts.request("GET", "/api/v1/account/devices/profile", nil)
	if !strings.Contains(string(info), `"signed":false`) {
		t.Fatalf("info %s", info)
	}
}

func TestTrustedCert(t *testing.T) {
	now := time.Now()
	good, goodLeaf := selfSigned(t, "nas.example.org", now.Add(time.Hour))
	roots := x509.NewCertPool()
	roots.AddCert(goodLeaf)
	get := func(host string) (*tls.Certificate, error) { return good, nil }
	if trustedCert([]string{"nas.example.org"}, get, roots, now) != good {
		t.Fatal("trusted certificate not taken")
	}
	if trustedCert([]string{"other.example.org"}, get, roots, now) != nil {
		t.Fatal("certificate of another name taken")
	}
	if trustedCert([]string{"nas.example.org"}, get, x509.NewCertPool(), now) != nil {
		t.Fatal("untrusted certificate taken")
	}
	if trustedCert([]string{"nas.example.org"}, get, roots, now.Add(2*time.Hour)) != nil {
		t.Fatal("expired certificate taken")
	}
	noKey := &tls.Certificate{Certificate: good.Certificate}
	if trustedCert([]string{"nas.example.org"}, func(string) (*tls.Certificate, error) { return noKey, nil }, roots, now) != nil {
		t.Fatal("certificate without key taken")
	}
}

func TestHolidaysRoute(t *testing.T) {
	ts := newTestServer(t)
	res, body := ts.fetchProfile("/api/v1/holidays/de-by.ics?lang=en", nil)
	if res.StatusCode != http.StatusOK || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/calendar") || !strings.Contains(string(body), "SUMMARY:Corpus Christi") {
		t.Fatalf("holidays: %d %s", res.StatusCode, body)
	}
	for _, p := range []string{"/api/v1/holidays/de-xx.ics", "/api/v1/holidays/de-by", "/api/v1/holidays/..%2Fx.ics"} {
		if res, _ := ts.fetchProfile(p, nil); res.StatusCode != http.StatusNotFound {
			t.Errorf("%s: %d", p, res.StatusCode)
		}
	}
}

func TestSplitServer(t *testing.T) {
	for in, want := range map[string]string{"nas.example.org": "nas.example.org 443", "nas:8443": "nas 8443", "[fd7a::1]": "fd7a::1 443",
		"[fd7a::1]:8443": "fd7a::1 8443", "192.168.1.20": "192.168.1.20 443"} {
		host, port, ok := splitServer(in)
		if !ok || fmt.Sprintf("%s %d", host, port) != want {
			t.Errorf("%s: %s %d %v", in, host, port, ok)
		}
	}
	for _, in := range []string{"", "nas:0", "nas:x", "a/b", "evil@host"} {
		if _, _, ok := splitServer(in); ok {
			t.Errorf("%q accepted", in)
		}
	}
}
