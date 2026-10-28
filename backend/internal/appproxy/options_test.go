package appproxy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

// status answers a request for host from remote through the proxy.
func status(m *Manager, host, remote string) int {
	req := httptest.NewRequest("GET", "/", nil)
	req.Host = host
	if remote != "" {
		req.RemoteAddr = remote
	}
	rec := httptest.NewRecorder()
	m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(299) })).ServeHTTP(rec, req)
	return rec.Code
}

func ptr[T any](v T) *T { return &v }

func TestLocalNameAndMDNS(t *testing.T) {
	m, _ := newTestManager(t)
	m.reload = func() error { return nil }
	port := upstreamPort(t, echo)
	if err := m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: port}); err != nil {
		t.Fatal(err)
	}
	if err := m.SetGeneral(General{LocalName: ptr(" Apps.local. ")}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(m.Suffixes(), ","); got != "apps.local" {
		t.Fatalf("suffixes %q", got)
	}
	if status(m, "immich.apps.local", "") != 200 || status(m, "immich.nas.local", "") != 299 {
		t.Error("local name not used for routing")
	}
	if info := m.Info(); info.LocalName != "apps" || info.DefaultLocalName != "nas" || !info.MDNS {
		t.Errorf("info %+v", info)
	}
	m.Publish(context.Background())
	if data, _ := os.ReadFile(m.hostsFile); !strings.Contains(string(data), "192.168.1.20 immich.apps.local\n") {
		t.Errorf("hosts file:\n%s", data)
	}
	for _, bad := range []string{"a b", "local", "a.b.c.d", "-x"} {
		if err := m.SetGeneral(General{LocalName: ptr(bad)}); !errors.Is(err, ErrInvalid) {
			t.Errorf("local name %q: %v", bad, err)
		}
	}
	// Empty is the NAS's name again.
	m.SetGeneral(General{LocalName: ptr("")})
	if got := strings.Join(m.Suffixes(), ","); got != "nas.local" {
		t.Fatalf("suffixes %q", got)
	}

	// Without mDNS no .local name is left, nothing is published.
	if err := m.SetGeneral(General{MDNS: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if len(m.Suffixes()) != 0 || m.Publishing() || m.URL("immich") != "" || status(m, "immich.nas.local", "") != 299 {
		t.Error("names left without mDNS")
	}
	if m.hostsContent() != "# Made by TitanOS from /config/docker/proxy.json; changes are overwritten.\n" {
		t.Errorf("hosts without mDNS:\n%s", m.hostsContent())
	}
	// The domain still works; it and the other fields are kept when only one is sent.
	m.SetGeneral(General{Domain: ptr("home.example.org")})
	if got := strings.Join(m.Suffixes(), ","); got != "home.example.org" || m.Info().MDNS {
		t.Errorf("suffixes %q", got)
	}
	if status(m, "immich.home.example.org", "") != 200 {
		t.Error("domain does not route without mDNS")
	}
}

func TestShortNamesAndAliases(t *testing.T) {
	m, _ := newTestManager(t)
	port := upstreamPort(t, echo)
	if err := m.SetRoute("immich", Route{Enabled: true, Name: "immich", Aliases: []string{"Fotos", "fotos", "immich", ""}, Short: true, Port: port}); err != nil {
		t.Fatal(err)
	}
	if got := m.Settings().Apps["immich"].Aliases; strings.Join(got, ",") != "fotos" {
		t.Errorf("aliases %v", got)
	}
	for _, host := range []string{"immich.nas.local", "fotos.nas.local", "immich.local", "fotos.local"} {
		if status(m, host, "") != 200 {
			t.Errorf("%s does not reach the app", host)
		}
	}
	if status(m, "nas.local", "") != 299 || status(m, "other.local", "") != 299 {
		t.Error("foreign .local names reach an app")
	}
	if got := strings.Join(m.CertificateNames("fotos.local"), ","); got != "*.nas.local,fotos.local,immich.local" {
		t.Errorf("certificate names %q", got)
	}
	if got := m.Info().Apps[0].URLs; strings.Join(got, ",") != "https://immich.nas.local,https://immich.local" {
		t.Errorf("urls %v", got)
	}
	hosts := m.hostsContent()
	for _, want := range []string{"192.168.1.20 fotos.local\n", "192.168.1.20 fotos.nas.local\n", "192.168.1.20 immich.local\n", "192.168.1.20 immich.nas.local\n"} {
		if !strings.Contains(hosts, want) {
			t.Errorf("hosts lacks %q:\n%s", want, hosts)
		}
	}

	// A short name may not be the NAS's own name, now or after renaming.
	if err := m.SetRoute("x", Route{Enabled: true, Name: "nas", Short: true, Port: 8080}); !errors.Is(err, ErrInvalid) {
		t.Errorf("short name of the NAS: %v", err)
	}
	if err := m.SetGeneral(General{LocalName: ptr("fotos")}); !errors.Is(err, ErrInvalid) {
		t.Errorf("local name taken by a short name: %v", err)
	}
	// Names are unique over names and aliases, apps and targets.
	if err := m.SetRoute("other", Route{Name: "fotos", Port: 8081}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("alias of another app: %v", err)
	}
	if err := m.SetTarget("router", Route{Name: "box", Aliases: []string{"immich"}, Host: "192.168.1.1", Port: 80}); !errors.Is(err, ErrNameTaken) {
		t.Errorf("target with an app's name: %v", err)
	}
	if err := m.SetRoute("other", Route{Name: "o", Aliases: []string{"a_b"}, Port: 8081}); !errors.Is(err, ErrInvalid) {
		t.Errorf("bad alias: %v", err)
	}
	if err := m.SetRoute("other", Route{Name: "o", Aliases: strings.Split("a,b,c,d,e,f,g,h,i", ","), Port: 8081}); !errors.Is(err, ErrInvalid) {
		t.Errorf("nine aliases: %v", err)
	}
	// Without mDNS there are no short names either.
	m.SetGeneral(General{MDNS: ptr(false), Domain: ptr("home.example.org")})
	if status(m, "immich.local", "") != 299 || len(m.shortNames()) != 0 {
		t.Error("short name without mDNS")
	}
}

// oldSettings is /config/docker/proxy.json as titand 0.29.0 reads it.
type oldSettings struct {
	Domain string `json:"domain"`
	Apps   map[string]struct {
		Enabled   bool   `json:"enabled"`
		Name      string `json:"name"`
		Port      int    `json:"port"`
		LocalOnly bool   `json:"localOnly"`
	} `json:"apps"`
}

func oldView(t *testing.T, m *Manager) oldSettings {
	t.Helper()
	data, err := m.store.Read(configFile)
	if err != nil {
		t.Fatal(err)
	}
	var old oldSettings
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatal(err)
	}
	return old
}

func TestMasterSwitch(t *testing.T) {
	m, store := newTestManager(t)
	port := upstreamPort(t, echo)
	m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: port})
	m.SetRoute("idle", Route{Enabled: false, Name: "idle", Port: port})
	m.SetTarget("ha", Route{Enabled: true, Name: "ha", Host: "127.0.0.1", Port: port})
	if err := m.SetGeneral(General{Off: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if status(m, "immich.nas.local", "") != 299 || status(m, "ha.nas.local", "") != 299 || m.Publishing() || m.URL("immich") != "" {
		t.Error("names answer while all are off")
	}
	if info := m.Info(); !info.Off || !info.Apps[1].Enabled || info.Apps[0].Enabled {
		t.Errorf("each route keeps its own switch: %+v", info.Apps)
	}
	// An older version sees every route as off.
	for app, route := range oldView(t, m).Apps {
		if route.Enabled {
			t.Errorf("%s enabled for an older version", app)
		}
	}
	// This version reads them back as they were.
	again := NewManager(store)
	again.Hostname = m.Hostname
	if err := again.Load(); err != nil {
		t.Fatal(err)
	}
	if s := again.Settings(); !s.Off || !s.Apps["immich"].Enabled || s.Apps["idle"].Enabled || !s.Targets["ha"].Enabled {
		t.Errorf("read back %+v", s)
	}
	again.SetGeneral(General{Off: ptr(false)})
	if status(again, "immich.nas.local", "") != 200 || status(again, "ha.nas.local", "") != 200 {
		t.Error("names do not answer after switching back on")
	}
	if data, _ := store.Read(configFile); strings.Contains(string(data), "paused") {
		t.Errorf("paused left on the stick:\n%s", data)
	}
}

func TestOwnNetworks(t *testing.T) {
	m, _ := newTestManager(t)
	port := upstreamPort(t, echo)
	if err := m.SetRoute("vault", Route{Enabled: true, Name: "vault", Port: port, Networks: []string{"10.1.2.3/8", " 203.0.113.7 ", "", "10.0.0.0/8"}}); err != nil {
		t.Fatal(err)
	}
	if got := m.Settings().Apps["vault"].Networks; strings.Join(got, ",") != "10.0.0.0/8,203.0.113.7/32" {
		t.Errorf("networks %v", got)
	}
	for remote, want := range map[string]int{"10.9.8.7:1": 200, "203.0.113.7:1": 200, "192.168.1.5:1": 403, "8.8.8.8:1": 403, "[::ffff:10.0.0.1]:1": 200} {
		if got := status(m, "vault.nas.local", remote); got != want {
			t.Errorf("%s: %d, want %d", remote, got, want)
		}
	}
	// An older version only knows "home network only": the stricter rule there.
	if !oldView(t, m).Apps["vault"].LocalOnly {
		t.Error("own networks not stored as local only for older versions")
	}
	for _, bad := range [][]string{{"10.0.0.0/33"}, {"nonsense"}} {
		if err := m.SetRoute("vault", Route{Name: "vault", Port: port, Networks: bad}); !errors.Is(err, ErrInvalid) {
			t.Errorf("networks %v: %v", bad, err)
		}
	}
	many := make([]string, 33)
	for i := range many {
		many[i] = "10.0." + strconv.Itoa(i) + ".0/24"
	}
	if err := m.SetRoute("vault", Route{Name: "vault", Port: port, Networks: many}); !errors.Is(err, ErrInvalid) {
		t.Errorf("33 networks: %v", err)
	}
}

func TestTargets(t *testing.T) {
	m, store := newTestManager(t)
	port := upstreamPort(t, echo)
	if err := m.SetTarget("homeassistant", Route{Enabled: true, Name: "ha", Host: "127.0.0.1", Port: port}); err != nil {
		t.Fatal(err)
	}
	if status(m, "ha.nas.local", "") != 200 {
		t.Error("target does not answer")
	}
	if info := m.Info(); len(info.Targets) != 1 || info.Targets[0].Host != "127.0.0.1" || len(info.Apps) != 0 {
		t.Errorf("info %+v", info)
	}
	// Older versions never see a target as an app (they would send it to 127.0.0.1).
	if len(oldView(t, m).Apps) != 0 {
		t.Error("target stored among the apps")
	}
	if data, _ := store.Read(configFile); !strings.Contains(string(data), `"targets"`) {
		t.Errorf("stored %s", data)
	}
	for _, c := range []Route{
		{Name: "x", Host: "8.8.8.8", Port: 80},        // not in the home network
		{Name: "x", Host: "localhost", Port: 443},     // titand itself
		{Name: "x", Host: "bad host", Port: 80},       // no host name
		{Name: "x", Host: "", Port: 80},               // no address
		{Name: "x", Host: "192.168.1.1", Port: 70000}, // port
	} {
		if err := m.SetTarget("x", c); !errors.Is(err, ErrInvalid) {
			t.Errorf("target %+v: %v", c, err)
		}
	}
	if err := m.SetTarget("router", Route{Name: "router", Host: "Fritz.Box.", Port: 80}); err != nil {
		t.Errorf("host name: %v", err)
	}
	if err := m.SetRoute("app", Route{Name: "app", Host: "192.168.1.1", Port: 80}); !errors.Is(err, ErrInvalid) {
		t.Errorf("app with a host: %v", err)
	}
	if err := m.RemoveTarget("homeassistant"); err != nil || status(m, "ha.nas.local", "") != 299 {
		t.Errorf("remove: %v", err)
	}
	if err := m.RemoveTarget("never"); err != nil {
		t.Fatal(err)
	}

	// Every connection to a target is checked where it goes.
	reserved := []int{443, 80}
	for address, ok := range map[string]bool{"192.168.1.9:8123": true, "127.0.0.1:8080": true, "[fd7a:115c:a1e0::9]:80": true, "8.8.8.8:80": false, "127.0.0.1:443": false, "[::1]:80": false} {
		if err := checkTarget(address, reserved); (err == nil) != ok {
			t.Errorf("%s: %v", address, err)
		}
	}
}

func TestVerifyTLS(t *testing.T) {
	m, _ := newTestManager(t)
	srv := httptest.NewTLSServer(http.HandlerFunc(echo))
	t.Cleanup(srv.Close)
	u, _ := url.Parse(srv.URL)
	port, _ := strconv.Atoi(u.Port())
	m.SetRoute("vault", Route{Enabled: true, Name: "vault", Port: port, Scheme: "https"})
	if got := status(m, "vault.nas.local", ""); got != 200 {
		t.Fatalf("without check: %d", got)
	}
	// The app's own certificate is not trusted by anyone: checked, it fails.
	m.SetRoute("vault", Route{Enabled: true, Name: "vault", Port: port, Scheme: "https", VerifyTLS: true})
	if got := status(m, "vault.nas.local", ""); got != http.StatusBadGateway {
		t.Fatalf("with check: %d", got)
	}
	// Plain HTTP has nothing to check; the flag is not kept.
	m.SetRoute("vault", Route{Enabled: true, Name: "vault", Port: port, VerifyTLS: true})
	if m.Settings().Apps["vault"].VerifyTLS {
		t.Error("certificate check stored for HTTP")
	}
}
