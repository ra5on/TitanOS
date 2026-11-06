package appproxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// portManager is a manager with a small port range, a listen check that
// only refuses busy, and fake listeners.
func portManager(t *testing.T, busy ...int) *Manager {
	t.Helper()
	m, _ := newTestManager(t)
	m.portRange = [2]int{20000, 20009}
	m.portFree = func(port int) bool { return !slices.Contains(busy, port) }
	m.addresses = func() []netip.Addr {
		return []netip.Addr{netip.MustParseAddr("192.168.1.20"), netip.MustParseAddr("fd00::20")}
	}
	return m
}

func TestPortAssignment(t *testing.T) {
	m := portManager(t, 20001)
	if err := m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: 2283}); err != nil {
		t.Fatal(err)
	}
	// Before Docker told its ports, no port is given.
	if p := m.Settings().Ports["immich"]; p != 0 {
		t.Fatalf("port given before Docker answered: %d", p)
	}
	// 20000 is published by a container, 20001 busy on the host.
	m.SetPublishedPorts([]int{20000, 2283, 8096})
	m.SetAutoApps(map[string]int{"jellyfin": 8096})
	ports := m.Settings().Ports
	if ports["immich"] != 20002 || ports["jellyfin"] != 20003 {
		t.Fatalf("ports = %v", ports)
	}
	// Stored on the stick, read back the same after a restart.
	m2 := portManager(t)
	m2.store = m.store
	if err := m2.Load(); err != nil {
		t.Fatal(err)
	}
	m2.SetPublishedPorts(nil)
	m2.SetAutoApps(map[string]int{"jellyfin": 8096})
	if got := m2.Settings().Ports; got["immich"] != 20002 || got["jellyfin"] != 20003 {
		t.Errorf("ports after restart = %v", got)
	}
	// A stored port is never checked or given anew, even if Docker now
	// publishes it; a stopped app (no automatic route) keeps its port.
	m2.SetPublishedPorts([]int{20002})
	m2.SetAutoApps(map[string]int{})
	if got := m2.Settings().Ports; got["immich"] != 20002 || got["jellyfin"] != 20003 {
		t.Errorf("ports changed: %v", got)
	}
	// Taking a name away keeps the port; removing the app frees it.
	if err := m2.RemoveRoute("immich"); err != nil {
		t.Fatal(err)
	}
	if m2.Settings().Ports["immich"] != 20002 {
		t.Error("removing the name freed the port")
	}
	if err := m2.ForgetApp("jellyfin"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m2.Settings().Ports["jellyfin"]; ok {
		t.Error("removed app kept its port")
	}
	// The old file without ports is still read, and the new one by a
	// version that knows nothing of them (unknown fields are ignored).
	var old struct {
		Apps map[string]Route `json:"apps"`
	}
	if err := m2.store.ReadJSON(configFile, &old); err != nil {
		t.Fatal(err)
	}
}

func TestPortAssignmentSkipsReservedAndFixed(t *testing.T) {
	m := portManager(t)
	m.portRange = [2]int{20046, 20050}
	m.Reserved = append(m.Reserved, 20046)
	m.SetPublishedPorts([]int{20047})
	m.SetAutoApps(map[string]int{"a": 1001, "b": 1002, "c": 1003})
	got := m.Settings().Ports
	// 20046 titand, 20047 a container, 20048 NFS mountd.
	if got["a"] != 20049 || got["b"] != 20050 {
		t.Errorf("ports = %v", got)
	}
	if _, ok := got["c"]; ok {
		t.Errorf("port outside the range: %v", got)
	}
	// With the range full, looking at Docker again writes nothing.
	changes := m.store.Changes()
	m.SetAutoApps(map[string]int{"a": 1001, "b": 1002, "c": 1003})
	m.SetAutoApps(map[string]int{"a": 1001, "b": 1002, "c": 1003})
	if m.store.Changes() != changes {
		t.Error("full range wrote to the stick on every look")
	}
	// No route may point at an app port: the proxy would call itself.
	if err := m.SetRoute("d", Route{Enabled: true, Name: "d", Port: 20049}); !errors.Is(err, ErrInvalid) {
		t.Errorf("route to an app port: %v", err)
	}
}

func TestPortAssignmentDevelopmentMode(t *testing.T) {
	m := portManager(t)
	m.Disabled = true
	m.SetPublishedPorts(nil)
	m.SetAutoApps(map[string]int{"a": 1001})
	if len(m.Settings().Ports) != 0 {
		t.Error("development mode gave ports")
	}
	m.ServePorts(context.Background(), &http.Server{}) // returns at once
}

// fakeListener is a listener nobody connects to.
type fakeListener struct {
	port   int
	closed chan struct{}
}

func (l *fakeListener) Accept() (net.Conn, error) { <-l.closed; return nil, net.ErrClosed }
func (l *fakeListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}
func (l *fakeListener) Addr() net.Addr { return &net.TCPAddr{Port: l.port} }

func servePorts(t *testing.T, m *Manager) *[]int {
	t.Helper()
	var reported []int
	m.OnPorts = func(p []int) { reported = p }
	m.ports.listen = func(port int) (net.Listener, error) {
		return &fakeListener{port: port, closed: make(chan struct{})}, nil
	}
	m.ports.serve = func(_ *http.Server, ln net.Listener) error { _, err := ln.Accept(); return err }
	t.Cleanup(m.closePorts)
	return &reported
}

// portRequest is a request that came in on an app port of the NAS.
func portRequest(method, target string, port int) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.RemoteAddr = "192.168.1.5:50000"
	return r.WithContext(context.WithValue(r.Context(), http.LocalAddrContextKey, &net.TCPAddr{IP: net.ParseIP("192.168.1.20"), Port: port}))
}

func TestPortRoutingAndCookies(t *testing.T) {
	f := newGateFixture(t)
	m := f.m
	m.portRange = [2]int{20000, 20009}
	m.portFree = func(int) bool { return true }
	reported := servePorts(t, m)
	port := upstreamPort(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "app two host="+r.Host+" cookie="+r.Header.Get("Cookie"))
	})
	if err := m.SetRoute("two", Route{Enabled: true, Name: "two", Port: port}); err != nil {
		t.Fatal(err)
	}
	m.SetPublishedPorts(nil)
	m.SetAutoApps(nil)
	ports := m.Settings().Ports
	if ports["immich"] != 20000 || ports["two"] != 20001 {
		t.Fatalf("ports = %v", ports)
	}
	m.syncPorts(&http.Server{})
	if !slices.Equal(*reported, []int{20000, 20001}) {
		t.Errorf("firewall told %v", *reported)
	}
	h := m.PortHandler()

	// Without a pass, the page leads back with the host and port.
	w := httptest.NewRecorder()
	r := portRequest("GET", "https://192.168.1.20:20000/", 20000)
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "/api/v1/apps/immich/open?host=192.168.1.20%3A20000") {
		t.Fatalf("no pass: %d %s", w.Code, w.Body.String())
	}

	// Enter immich on its port: the pass is named after the port.
	token, _ := f.access.EnterToken("alex", "immich", auth.SessionID(f.logins["alex"]))
	w = httptest.NewRecorder()
	r = portRequest("GET", "https://192.168.1.20:20000"+EnterPath+"?t="+url.QueryEscape(token), 20000)
	r.Header.Set("Sec-Fetch-Mode", "navigate")
	h.ServeHTTP(w, r)
	cookies := w.Result().Cookies()
	if w.Code != http.StatusSeeOther || len(cookies) != 1 || cookies[0].Name != "__Host-titanos_app_20000" || !cookies[0].Partitioned || !cookies[0].HttpOnly {
		t.Fatalf("enter on port: %d %+v", w.Code, cookies)
	}
	pass := cookies[0]

	// Any Host header reaches the app of the port; TitanOS's cookies and
	// the passes of other apps never reach it.
	for _, host := range []string{"192.168.1.20:20000", "nas.local:20000", "whatever.example"} {
		w = httptest.NewRecorder()
		r = portRequest("GET", "https://"+host+"/", 20000)
		r.Host = host
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.AddCookie(pass)
		r.AddCookie(&http.Cookie{Name: "__Host-titanos_session", Value: "secret"})
		r.AddCookie(&http.Cookie{Name: "__Host-titanos_device", Value: "secret"})
		r.AddCookie(&http.Cookie{Name: "__Host-titanos_app_20001", Value: "secret"})
		r.AddCookie(&http.Cookie{Name: "own", Value: "1"})
		h.ServeHTTP(w, r)
		body := w.Body.String()
		if w.Code != http.StatusOK || !strings.HasPrefix(body, "app page") {
			t.Fatalf("%s: %d %s", host, w.Code, body)
		}
		if strings.Contains(body, "titanos") || strings.Contains(body, "secret") || !strings.Contains(body, "own=1") {
			t.Errorf("%s: cookies to the app: %s", host, body)
		}
		if w.Header().Get("X-Frame-Options") != "" || !strings.Contains(w.Header().Get("Content-Security-Policy"), "https://192.168.1.20") {
			t.Errorf("frame headers not rewritten: %v", w.Header())
		}
	}

	// The pass of immich's port does not open the app on the next port,
	// not even under the name of that port's cookie.
	for _, c := range []*http.Cookie{pass, {Name: "__Host-titanos_app_20001", Value: pass.Value}} {
		w = httptest.NewRecorder()
		r = portRequest("GET", "https://192.168.1.20:20001/", 20001)
		r.Header.Set("Sec-Fetch-Mode", "navigate")
		r.AddCookie(c)
		h.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Errorf("pass of immich (%s) let into app two: %d %s", c.Name, w.Code, w.Body.String())
		}
	}

	// A port without an app answers 404; a switched-off route closes it.
	w = httptest.NewRecorder()
	h.ServeHTTP(w, portRequest("GET", "https://192.168.1.20:20005/", 20005))
	if w.Code != http.StatusNotFound {
		t.Errorf("port without app: %d", w.Code)
	}
	if err := m.SetRoute("two", Route{Enabled: false, Name: "two", Port: port}); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, portRequest("GET", "https://192.168.1.20:20001/", 20001))
	if w.Code != http.StatusNotFound {
		t.Errorf("switched-off app still answers on its port: %d", w.Code)
	}
	m.syncPorts(&http.Server{})
	if !slices.Equal(*reported, []int{20000}) {
		t.Errorf("firewall told %v after switching off", *reported)
	}
	m.ports.mu.Lock()
	_, open := m.ports.open[20001]
	m.ports.mu.Unlock()
	if open {
		t.Error("listener of a switched-off app still open")
	}
}

func TestWindowURLsUseRequestHost(t *testing.T) {
	f := newGateFixture(t)
	m := f.m
	m.addresses = func() []netip.Addr {
		return []netip.Addr{netip.MustParseAddr("192.168.1.20"), netip.MustParseAddr("fd00::20")}
	}
	m.portRange = [2]int{20000, 20009}
	m.portFree = func(int) bool { return true }
	servePorts(t, m)
	// No listener yet: only the name.
	if p, n := m.WindowURLs("immich", "192.168.1.20", "tok"); p != "" || n != "https://immich.nas.local/.titanos/enter?t=tok" {
		t.Errorf("before the port: %q %q", p, n)
	}
	m.SetPublishedPorts(nil)
	m.SetAutoApps(nil)
	m.syncPorts(&http.Server{})
	for host, want := range map[string]string{
		"192.168.1.20":       "https://192.168.1.20:20000",
		"192.168.1.20:443":   "https://192.168.1.20:20000",
		"NAS.local:8443":     "https://nas.local:20000",
		"[fd00::20]":         "https://[fd00::20]:20000",
		"[fd00::20]:443":     "https://[fd00::20]:20000",
		"evil.example":       "",
		"evil.example:443":   "",
		"192.168.1.99":       "",
		"immich.nas.local":   "",
		"":                   "",
		"192.168.1.20:abc":   "https://192.168.1.20:20000",
		"192.168.1.20:20000": "https://192.168.1.20:20000",
	} {
		p, n := m.WindowURLs("immich", host, "tok")
		wantP := ""
		if want != "" {
			wantP = want + "/.titanos/enter?t=tok"
		}
		if p != wantP || n != "https://immich.nas.local/.titanos/enter?t=tok" {
			t.Errorf("%q: port %q name %q, want %q", host, p, n, wantP)
		}
	}
	// /open with the host and port of the page without a pass.
	if u, ok := m.EnterURL("immich", "192.168.1.20:20000", "tok"); !ok || u != "https://192.168.1.20:20000/.titanos/enter?t=tok" {
		t.Errorf("enter url on port: %q", u)
	}
	// A foreign host, or the port of another app, never carries a token.
	for _, host := range []string{"evil.example:20000", "192.168.1.20:20001"} {
		if u, _ := m.EnterURL("immich", host, "tok"); u != "https://immich.nas.local/.titanos/enter?t=tok" {
			t.Errorf("%s: enter url %q", host, u)
		}
	}
}
