package appproxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appaccess"
	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestRewriteFrameHeaders(t *testing.T) {
	origins := []string{"https://nas.local", "https://192.168.1.20"}
	want := "frame-ancestors 'self' https://nas.local https://192.168.1.20"
	for _, tc := range []struct {
		name       string
		csp        []string
		reportOnly []string
		wantCSP    []string
		wantReport []string
	}{
		{"none", nil, nil, []string{want}, nil},
		{"replace none", []string{"default-src 'self'; frame-ancestors 'none'; img-src *"}, nil,
			[]string{"default-src 'self'; " + want + "; img-src *"}, nil},
		{"other directives only", []string{"default-src 'self'"}, nil, []string{"default-src 'self'", want}, nil},
		{"two lines", []string{"default-src 'self'", "Frame-Ancestors https://evil.example"}, nil,
			[]string{"default-src 'self'", want}, nil},
		{"comma joined", []string{"script-src 'self', frame-ancestors 'none'; object-src 'none'"}, nil,
			[]string{"script-src 'self', " + want + "; object-src 'none'"}, nil},
		{"report only", nil, []string{"frame-ancestors 'none'", "default-src 'self'"},
			[]string{want}, []string{want, "default-src 'self'"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			h.Set("X-Frame-Options", "DENY")
			for _, v := range tc.csp {
				h.Add("Content-Security-Policy", v)
			}
			for _, v := range tc.reportOnly {
				h.Add("Content-Security-Policy-Report-Only", v)
			}
			rewriteFrameHeaders(h, origins)
			if h.Get("X-Frame-Options") != "" {
				t.Error("X-Frame-Options kept")
			}
			if got := h.Values("Content-Security-Policy"); !slices.Equal(got, tc.wantCSP) {
				t.Errorf("CSP = %q, want %q", got, tc.wantCSP)
			}
			if got := h.Values("Content-Security-Policy-Report-Only"); !slices.Equal(got, tc.wantReport) {
				t.Errorf("report-only = %q, want %q", got, tc.wantReport)
			}
			for _, v := range h.Values("Content-Security-Policy") {
				if strings.Contains(v, "*") && strings.Contains(v, "frame-ancestors *") {
					t.Error("frame-ancestors must never be open")
				}
			}
		})
	}
}

func TestFrameOrigins(t *testing.T) {
	m, _ := newTestManager(t)
	m.HTTPSPort = 8443
	m.UIHosts = func() []string { return []string{"nas.example.org", "nas.tail1.ts.net.", "bad name", "*.x"} }
	m.addresses = func() []netip.Addr {
		return []netip.Addr{netip.MustParseAddr("192.168.1.20"), netip.MustParseAddr("fd00::20")}
	}
	got := m.frameOrigins()
	want := []string{"https://nas:8443", "https://nas.local:8443", "https://nas.example.org:8443", "https://nas.tail1.ts.net:8443", "https://192.168.1.20:8443", "https://[fd00::20]:8443"}
	if !slices.Equal(got, want) {
		t.Errorf("origins = %q, want %q", got, want)
	}
	if o := m.uiOrigin("immich.nas.local"); o != "https://nas.local:8443" {
		t.Errorf("ui origin for .local = %s", o)
	}
	if o := m.uiOrigin("immich.example.org"); o != "https://nas.example.org:8443" {
		t.Errorf("ui origin for domain = %s", o)
	}
}

func TestPageLoad(t *testing.T) {
	for _, tc := range []struct {
		method string
		hdr    map[string]string
		want   bool
	}{
		{"GET", map[string]string{"Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "document"}, true},
		{"GET", map[string]string{"Sec-Fetch-Mode": "navigate", "Sec-Fetch-Dest": "iframe"}, true},
		{"POST", map[string]string{"Sec-Fetch-Mode": "navigate"}, true},
		{"GET", map[string]string{"Sec-Fetch-Mode": "cors", "Sec-Fetch-Dest": "empty", "Accept": "text/html"}, false},
		{"GET", map[string]string{"Sec-Fetch-Mode": "no-cors", "Sec-Fetch-Dest": "image"}, false},
		{"GET", map[string]string{"Sec-Fetch-Mode": "websocket"}, false},
		{"GET", map[string]string{"Accept": "text/html,application/xhtml+xml"}, true},
		{"GET", map[string]string{"Accept": "application/json"}, false},
		{"POST", map[string]string{"Accept": "text/html"}, false},
		{"GET", nil, false},
	} {
		r := httptest.NewRequest(tc.method, "/", nil)
		for k, v := range tc.hdr {
			r.Header.Set(k, v)
		}
		if got := pageLoad(r); got != tc.want {
			t.Errorf("%s %v: page load = %v, want %v", tc.method, tc.hdr, got, tc.want)
		}
	}
}

type gateFixture struct {
	m        *Manager
	access   *appaccess.Manager
	accounts map[string]appaccess.Account
	now      time.Time
	h        http.Handler
}

func newGateFixture(t *testing.T) *gateFixture {
	t.Helper()
	f := &gateFixture{now: time.Now()}
	f.m, _ = newTestManager(t)
	f.m.addresses = func() []netip.Addr { return []netip.Addr{netip.MustParseAddr("192.168.1.20")} }
	f.accounts = map[string]appaccess.Account{"alex": {Name: "alex", Admin: true}, "anna": {Name: "anna"}}
	f.access = appaccess.New(config.Open(t.TempDir()))
	f.access.Lookup = func(name string) (appaccess.Account, bool) { a, ok := f.accounts[name]; return a, ok }
	f.access.Now = func() time.Time { return f.now }
	f.m.Access = f.access
	port := upstreamPort(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
		io.WriteString(w, "app page cookie="+r.Header.Get("Cookie"))
	})
	if err := f.m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: port}); err != nil {
		t.Fatal(err)
	}
	f.h = f.m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "web ui") }))
	return f
}

func (f *gateFixture) do(path string, browser bool, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", "https://immich.nas.local"+path, nil)
	if browser {
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.Header.Set("Sec-Fetch-Dest", "iframe")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	return w
}

func (f *gateFixture) enter(t *testing.T, user string) *http.Cookie {
	t.Helper()
	token, _ := f.access.EnterToken(user, "immich")
	w := f.do(EnterPath+"?t="+url.QueryEscape(token), true, nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/" {
		t.Fatalf("enter: %d %s", w.Code, w.Header().Get("Location"))
	}
	res := w.Result()
	cookies := res.Cookies()
	if len(cookies) != 1 || cookies[0].Name != gateCookie || !cookies[0].HttpOnly || !cookies[0].Secure || cookies[0].SameSite != http.SameSiteNoneMode || !cookies[0].Partitioned || cookies[0].Path != "/" {
		t.Fatalf("gate cookie = %+v", cookies)
	}
	return cookies[0]
}

func TestGate(t *testing.T) {
	f := newGateFixture(t)
	// Without a pass a page load is refused with a page that leads to TitanOS.
	w := f.do("/", true, nil)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "Diese App über TitanOS öffnen") || !strings.Contains(w.Body.String(), "https://nas.local/api/v1/apps/immich/open?host=immich.nas.local") {
		t.Fatalf("no pass: %d %s", w.Code, w.Body.String())
	}
	if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'self' https://nas") {
		t.Errorf("refusal page cannot show in a window: %s", csp)
	}
	// Requests that are no page loads pass (apps on phones and TVs).
	if w := f.do("/api/server/ping", false, nil); w.Code != http.StatusOK {
		t.Errorf("API call blocked: %d", w.Code)
	}
	cookie := f.enter(t, "alex")
	w = f.do("/", true, cookie)
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "app page") {
		t.Fatalf("with pass: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "titanos") {
		t.Error("the gate cookie reached the app")
	}
	if w.Header().Get("X-Frame-Options") != "" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'self' https://nas https://nas.local https://192.168.1.20") {
		t.Errorf("frame headers not rewritten: %v", w.Header())
	}
	// A pass is for one app's name.
	if err := f.m.SetRoute("other", Route{Enabled: true, Name: "other", Port: 1}); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "https://other.nas.local/", nil)
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	r.AddCookie(cookie)
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, r)
	if rec.Code != http.StatusForbidden {
		t.Errorf("pass of immich let into another app: %d", rec.Code)
	}
}

func TestGateRevokedAndExpired(t *testing.T) {
	f := newGateFixture(t)
	if err := f.access.Set(appaccess.Settings{Apps: map[string]appaccess.App{"immich": {Users: []string{"anna"}}}}); err != nil {
		t.Fatal(err)
	}
	cookie := f.enter(t, "anna")
	if w := f.do("/", true, cookie); w.Code != http.StatusOK {
		t.Fatalf("allowed user refused: %d", w.Code)
	}
	if err := f.access.Set(appaccess.Settings{Apps: map[string]appaccess.App{"immich": {}}}); err != nil {
		t.Fatal(err)
	}
	if w := f.do("/", true, cookie); w.Code != http.StatusForbidden {
		t.Errorf("revoked right still lets in: %d", w.Code)
	}
	if err := f.access.Set(appaccess.Settings{Apps: map[string]appaccess.App{"immich": {Users: []string{"anna"}}}}); err != nil {
		t.Fatal(err)
	}
	f.accounts["anna"] = appaccess.Account{Name: "anna", Disabled: true}
	if w := f.do("/", true, cookie); w.Code != http.StatusForbidden {
		t.Errorf("locked account still lets in: %d", w.Code)
	}
	f.accounts["anna"] = appaccess.Account{Name: "anna"}
	f.now = f.now.Add(appaccess.CookieTTL + time.Second)
	if w := f.do("/", true, cookie); w.Code != http.StatusForbidden {
		t.Errorf("expired pass still lets in: %d", w.Code)
	}
	// Old or used window tokens are refused at the door.
	token, _ := f.access.EnterToken("anna", "immich")
	f.now = f.now.Add(appaccess.EnterTTL)
	if w := f.do(EnterPath+"?t="+url.QueryEscape(token), true, nil); w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 {
		t.Errorf("expired token: %d", w.Code)
	}
	token, _ = f.access.EnterToken("anna", "immich")
	f.do(EnterPath+"?t="+url.QueryEscape(token), true, nil)
	if w := f.do(EnterPath+"?t="+url.QueryEscape(token), true, nil); w.Code != http.StatusForbidden {
		t.Errorf("token used twice: %d", w.Code)
	}
}

func TestGateSwitchedOff(t *testing.T) {
	f := newGateFixture(t)
	if err := f.access.Set(appaccess.Settings{Apps: map[string]appaccess.App{"immich": {NoGate: true}}}); err != nil {
		t.Fatal(err)
	}
	if w := f.do("/", true, nil); w.Code != http.StatusOK {
		t.Errorf("gate off still refuses: %d", w.Code)
	}
}

func TestAutoRoutes(t *testing.T) {
	m, store := newTestManager(t)
	port := upstreamPort(t, echo)
	if err := m.SetRoute("immich", Route{Enabled: false, Name: "photos", Port: port}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRoute("own", Route{Enabled: true, Name: "jellyfin", Port: port}); err != nil {
		t.Fatal(err)
	}
	changes := store.Changes()
	m.SetAutoApps(map[string]int{"immich": port, "jellyfin": port, "My_App": port, "web": 443, "paperless": port})
	if store.Changes() != changes {
		t.Error("automatic names written to the stick")
	}
	if _, ok := m.Route("immich"); ok {
		t.Error("an app's own route (switched off) must win over the automatic one")
	}
	r, ok := m.Route("jellyfin")
	if !ok || r.Name != "jellyfin-2" || !r.LocalOnly {
		t.Errorf("taken name not avoided: %+v %v", r, ok)
	}
	if _, ok := m.Route("My_App"); ok {
		t.Error("an invalid app name got a route")
	}
	if _, ok := m.Route("web"); ok {
		t.Error("a port of titand got a route")
	}
	if u := m.URL("paperless"); u != "https://paperless.nas.local" {
		t.Errorf("url %q", u)
	}
	h := m.Handler(http.NotFoundHandler())
	req := httptest.NewRequest("GET", "https://paperless.nas.local/", nil)
	req.RemoteAddr = "192.168.1.30:5000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("automatic name does not answer: %d", rec.Code)
	}
	info := m.Info()
	auto := 0
	for _, a := range info.Apps {
		if a.Auto {
			auto++
		}
	}
	if auto != 2 || !info.Auto {
		t.Errorf("info: %d automatic, switch %v", auto, info.Auto)
	}
	// Switched off: gone, and the switch is stored.
	off := false
	if err := m.SetGeneral(General{Auto: &off}); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Route("paperless"); ok {
		t.Error("automatic names still answer when switched off")
	}
	var stored Settings
	if err := store.ReadJSON(configFile, &stored); err != nil || !stored.NoAuto || len(stored.Apps) != 2 {
		t.Errorf("stored: %+v %v", stored, err)
	}
	on := true
	if err := m.SetGeneral(General{Auto: &on}); err != nil {
		t.Fatal(err)
	}
	// A removed app loses its name.
	m.SetAutoApps(map[string]int{"jellyfin": port})
	if _, ok := m.Route("paperless"); ok {
		t.Error("removed app keeps its automatic name")
	}
	// The whole proxy off: nothing.
	yes := true
	if err := m.SetGeneral(General{Off: &yes}); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Route("jellyfin"); ok {
		t.Error("automatic name with the proxy off")
	}
}

func TestWebUIPort(t *testing.T) {
	for label, want := range map[string]int{"8096": 8096, "8096/admin": 8096, "http://[host]:2283/": 2283, "https://example.org": 0, "": 0, "99999": 0} {
		if got := WebUIPort(label); got != want {
			t.Errorf("%q: %d, want %d", label, got, want)
		}
	}
}
