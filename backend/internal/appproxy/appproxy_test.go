package appproxy

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func newTestManager(t *testing.T) (*Manager, *config.Store) {
	t.Helper()
	store := config.Open(t.TempDir())
	m := NewManager(store)
	m.Hostname = func() string { return "NAS" }
	m.Reserved = []int{443, 80}
	m.hostsFile = filepath.Join(t.TempDir(), "avahi", "hosts")
	m.address = func() string { return "192.168.1.20" }
	return m, store
}

// upstreamPort starts an app that answers with what it received.
func upstreamPort(t *testing.T, handler http.HandlerFunc) int {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	return port
}

func echo(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "app", Value: "1"})
	http.SetCookie(w, &http.Cookie{Name: "__Host-titanos_session", Value: "evil", Path: "/", Secure: true})
	http.SetCookie(w, &http.Cookie{Name: "titanos_session", Value: "evil", Domain: "nas.local"})
	json.NewEncoder(w).Encode(map[string]string{
		"host": r.Host, "cookie": r.Header.Get("Cookie"), "path": r.URL.RequestURI(),
		"for": r.Header.Get("X-Forwarded-For"), "fhost": r.Header.Get("X-Forwarded-Host"), "proto": r.Header.Get("X-Forwarded-Proto"),
	})
}

func TestRoutingByHost(t *testing.T) {
	m, _ := newTestManager(t)
	port := upstreamPort(t, echo)
	if err := m.SetRoute("immich", Route{Enabled: true, Name: "Immich", Port: port}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRoute("off", Route{Enabled: false, Name: "off", Port: port}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetDomain("Home.Example.org."); err != nil {
		t.Fatal(err)
	}
	ui := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "web ui") })
	h := m.Handler(ui)

	for host, proxied := range map[string]bool{
		"immich.nas.local":          true,
		"IMMICH.NAS.LOCAL.":         true,
		"immich.nas.local:443":      true,
		"immich.home.example.org":   true,
		"nas.local":                 false,
		"192.168.1.20":              false,
		"other.nas.local":           false,
		"off.nas.local":             false, // switched off
		"immich.other.local":        false,
		"x.immich.nas.local":        false,
		"immich.example.org":        false,
		"immich.home.example.org.x": false,
	} {
		req := httptest.NewRequest("GET", "/photos?x=1", nil)
		req.Host = host
		req.RemoteAddr = "192.168.1.5:5000"
		req.Header.Set("Cookie", "__Host-titanos_session=secret; app=1; titanos_session=also")
		req.Header.Set("X-Forwarded-For", "1.2.3.4")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if !proxied {
			if rec.Body.String() != "web ui" {
				t.Errorf("%s went to an app: %q", host, rec.Body.String())
			}
			continue
		}
		var got map[string]string
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("%s: %v %q", host, err, rec.Body.String())
		}
		if got["host"] != host || got["path"] != "/photos?x=1" {
			t.Errorf("%s: app was asked %v", host, got)
		}
		// The TitanOS login never reaches the app; its own cookies do.
		if got["cookie"] != "app=1" {
			t.Errorf("%s: app got cookies %q", host, got["cookie"])
		}
		// The client's own X-Forwarded-For is not passed on.
		if got["for"] != "192.168.1.5" || got["fhost"] != host || got["proto"] != "http" {
			t.Errorf("%s: forwarded headers %v", host, got)
		}
		setCookies := rec.Result().Header.Values("Set-Cookie")
		if len(setCookies) != 1 || !strings.HasPrefix(setCookies[0], "app=") {
			t.Errorf("%s: app may set %v", host, setCookies)
		}
	}
}

func TestLocalOnly(t *testing.T) {
	m, _ := newTestManager(t)
	port := upstreamPort(t, echo)
	if err := m.SetRoute("vault", Route{Enabled: true, Name: "vault", Port: port, LocalOnly: true}); err != nil {
		t.Fatal(err)
	}
	h := m.Handler(http.NotFoundHandler())
	for remote, want := range map[string]int{"192.168.1.5:1": 200, "100.101.1.2:1": 200, "[fd7a:115c:a1e0::5]:1": 200, "8.8.8.8:1": 403, "[2001:db8::1]:1": 403} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host = "vault.nas.local"
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("%s: %d, want %d", remote, rec.Code, want)
		}
	}
}

func TestAppDown(t *testing.T) {
	m, _ := newTestManager(t)
	// A port nothing listens on.
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	if err := m.SetRoute("gone", Route{Enabled: true, Name: "gone", Port: port}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = "gone.nas.local"
	rec := httptest.NewRecorder()
	m.Handler(http.NotFoundHandler()).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestWebSocketUpgrade(t *testing.T) {
	m, _ := newTestManager(t)
	port := upstreamPort(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "websocket" || r.Header.Get("Cookie") != "" {
			http.Error(w, "no upgrade", http.StatusBadRequest)
			return
		}
		conn, rw, err := http.NewResponseController(w).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		rw.Flush()
		// Echo one line back, as frames would go both ways.
		line, _ := rw.ReadString('\n')
		rw.WriteString("echo " + line)
		rw.Flush()
	})
	if err := m.SetRoute("chat", Route{Enabled: true, Name: "chat", Port: port}); err != nil {
		t.Fatal(err)
	}
	front := httptest.NewServer(m.Handler(http.NotFoundHandler()))
	defer front.Close()

	conn, err := net.Dial("tcp", strings.TrimPrefix(front.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	io.WriteString(conn, "GET /ws HTTP/1.1\r\nHost: chat.nas.local\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nCookie: __Host-titanos_session=secret\r\n\r\n")
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", res.StatusCode)
	}
	io.WriteString(conn, "hello\n")
	line, err := reader.ReadString('\n')
	if err != nil || line != "echo hello\n" {
		t.Fatalf("got %q, %v", line, err)
	}
}

func TestValidation(t *testing.T) {
	m, store := newTestManager(t)
	for _, c := range []struct {
		app   string
		route Route
	}{
		{"../x", Route{Name: "x", Port: 8080}},
		{"app", Route{Name: "", Port: 8080}},
		{"app", Route{Name: "a.b", Port: 8080}},
		{"app", Route{Name: "-a", Port: 8080}},
		{"app", Route{Name: strings.Repeat("a", 64), Port: 8080}},
		{"app", Route{Name: "a", Port: 0}},
		{"app", Route{Name: "a", Port: 70000}},
		{"app", Route{Name: "a", Port: 443}}, // titand itself
		{"app", Route{Name: "a", Port: 8080, Scheme: "ftp"}},
	} {
		if err := m.SetRoute(c.app, c.route); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s %+v: %v", c.app, c.route, err)
		}
	}
	for _, domain := range []string{"example", "bad_label.org", "nas.local", "-a.org", strings.Repeat("a.", 101) + "org"} {
		if err := m.SetDomain(domain); !errors.Is(err, ErrInvalid) {
			t.Errorf("domain %q: %v", domain, err)
		}
	}
	if err := m.SetRoute("immich", Route{Enabled: true, Name: "photos", Port: 2283}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetRoute("other", Route{Enabled: true, Name: "photos", Port: 2284}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("same name twice: %v", err)
	}
	// Its own name again is fine.
	if err := m.SetRoute("immich", Route{Enabled: true, Name: "photos", Port: 2283, Scheme: "https", LocalOnly: true}); err != nil {
		t.Fatal(err)
	}

	// Only what is set is stored; a new manager reads it back.
	data, _ := store.Read(configFile)
	if strings.Contains(string(data), `"scheme": "http"`) || !strings.Contains(string(data), `"scheme": "https"`) {
		t.Errorf("stored %s", data)
	}
	again := NewManager(store)
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if got := again.Settings().Apps["immich"]; got.Name != "photos" || got.Scheme != "https" || !got.LocalOnly {
		t.Errorf("read back %+v", got)
	}
	if err := again.RemoveRoute("immich"); err != nil || len(again.Settings().Apps) != 0 {
		t.Fatalf("remove: %v %v", err, again.Settings())
	}
	if err := again.RemoveRoute("never"); err != nil {
		t.Fatal(err)
	}

	// No file at all: no routes, no error.
	empty := NewManager(config.Open(t.TempDir()))
	if err := empty.Load(); err != nil || empty.Settings().Apps == nil {
		t.Fatal(err)
	}
}

func TestURLsAndCertificateNames(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: 2283}); err != nil {
		t.Fatal(err)
	}
	if got := m.URL("immich"); got != "https://immich.nas.local" {
		t.Errorf("URL %q", got)
	}
	m.HTTPSPort = 8443
	m.SetDomain("home.example.org")
	if got := m.URL("immich"); got != "https://immich.home.example.org:8443" {
		t.Errorf("URL with domain %q", got)
	}
	if got := m.URL("missing"); got != "" {
		t.Errorf("URL of an app without route %q", got)
	}
	names := m.CertificateNames("immich.nas.local")
	if strings.Join(names, ",") != "*.nas.local,*.home.example.org" {
		t.Errorf("names %v", names)
	}
	if m.CertificateNames("nas.local") != nil || m.CertificateNames("") != nil {
		t.Error("the web UI's own name got app names")
	}
	apps := m.Info().Apps
	if len(apps) != 1 || len(apps[0].URLs) != 2 || apps[0].Scheme != "http" {
		t.Errorf("info %+v", apps)
	}
}

func TestPublishAvahiHosts(t *testing.T) {
	m, _ := newTestManager(t)
	reloads := 0
	m.reload = func() error { reloads++; return nil }
	m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: 2283})
	m.SetRoute("jellyfin", Route{Enabled: true, Name: "media", Port: 8096})
	m.SetRoute("off", Route{Enabled: false, Name: "off", Port: 8097})
	m.SetDomain("home.example.org")
	if !m.Publishing() {
		t.Fatal("not publishing")
	}
	ctx := context.Background()
	if err := m.Publish(ctx); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(m.hostsFile)
	if err != nil {
		t.Fatal(err)
	}
	want := "192.168.1.20 immich.nas.local\n192.168.1.20 media.nas.local\n"
	if !strings.HasSuffix(string(data), want) || strings.Contains(string(data), "off.") || strings.Contains(string(data), "example") {
		t.Errorf("hosts file:\n%s", data)
	}
	// Unchanged content: avahi is not bothered again.
	m.Publish(ctx)
	if reloads != 1 {
		t.Errorf("%d reloads", reloads)
	}
	m.address = func() string { return "192.168.1.21" }
	m.Publish(ctx)
	if reloads != 2 {
		t.Errorf("new address: %d reloads", reloads)
	}

	// Development mode leaves avahi alone.
	dev, _ := newTestManager(t)
	dev.Disabled = true
	dev.reload = func() error { t.Error("reload in development mode"); return nil }
	dev.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: 2283})
	if dev.Publishing() || dev.Publish(ctx) != nil {
		t.Error("development mode publishes")
	}
	if _, err := os.Stat(dev.hostsFile); !os.IsNotExist(err) {
		t.Errorf("hosts file written in development mode: %v", err)
	}
}
