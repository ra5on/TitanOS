package appproxy

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// fakeTailscale plays systemctl, systemd-run and the tailscale CLI of
// every device; it records what titand asked.
type fakeTailscale struct {
	mu      sync.Mutex
	calls   []string
	active  map[string]bool // unit → running
	login   map[string]bool // socket → logged in
	certs   bool
	keySeen string
}

func newFakeTailscale() *fakeTailscale {
	return &fakeTailscale{active: map[string]bool{}, login: map[string]bool{}, certs: true}
}

func (f *fakeTailscale) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	switch name {
	case "systemctl":
		switch args[0] {
		case "is-active":
			if f.active[strings.TrimSuffix(args[1], ".service")] {
				return "active\n", nil
			}
			return "inactive\n", errors.New("exit 3")
		case "stop":
			delete(f.active, strings.TrimSuffix(args[1], ".service"))
		case "list-units":
			var out strings.Builder
			for unit := range f.active {
				out.WriteString(unit + ".service loaded active running x\n")
			}
			return out.String(), nil
		}
		return "", nil
	case "systemd-run":
		unit := strings.TrimPrefix(args[0], "--unit=")
		f.active[unit] = true
		return "", nil
	case "tailscale":
		socket := strings.TrimPrefix(args[0], "--socket=")
		id := strings.TrimSuffix(filepath.Base(socket), ".sock")
		host := strings.TrimPrefix(id, "app-")
		host = strings.TrimPrefix(host, "target-")
		switch args[1] {
		case "status":
			state := "NeedsLogin"
			if f.login[socket] {
				state = "Running"
			}
			certs := []string{}
			if f.certs {
				certs = []string{host + ".tail1234.ts.net"}
			}
			out, _ := json.Marshal(map[string]any{"BackendState": state, "CertDomains": certs, "Self": map[string]any{"DNSName": host + ".tail1234.ts.net.", "HostName": host, "TailscaleIPs": []string{"fd7a:115c:a1e0::5", "100.101.102.103"}}})
			return string(out), nil
		case "up":
			for _, a := range args {
				if file, ok := strings.CutPrefix(a, "--auth-key=file:"); ok {
					data, _ := os.ReadFile(file)
					f.keySeen = string(data)
				}
			}
			f.login[socket] = true
		case "logout":
			delete(f.login, socket)
		}
		return "", nil
	}
	return "", nil
}

func (f *fakeTailscale) called(prefix string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func newTailnetManager(t *testing.T) (*Manager, *fakeTailscale, string) {
	t.Helper()
	m, _ := newTestManager(t)
	fake := newFakeTailscale()
	mount := t.TempDir()
	os.MkdirAll(filepath.Join(mount, "data"), 0o755)
	binary := filepath.Join(t.TempDir(), "tailscaled")
	os.WriteFile(binary, nil, 0o755)
	m.Tailnet.runner = fake
	m.Tailnet.mountRoot = mount
	m.Tailnet.runDir = t.TempDir()
	m.Tailnet.binary = binary
	m.Tailnet.Volume = func() (string, bool) { return "data", true }
	return m, fake, mount
}

func TestTailnetDeviceLifecycle(t *testing.T) {
	m, fake, mount := newTailnetManager(t)
	ctx := context.Background()
	if err := m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: 2283, Tailscale: true}); err != nil {
		t.Fatal(err)
	}

	// Without a key the device waits for one and never logs in.
	m.reconcileTailnet(ctx)
	if n := m.tailnetNode("app:immich"); n == nil || n.State != "login" {
		t.Fatalf("without key: %+v", n)
	}
	if len(fake.called("tailscale --socket")) == 0 || len(fake.called("tailscale --socket="+m.Tailnet.socket("app-immich")+" up")) != 0 {
		t.Fatal("logged in without a key")
	}

	if err := m.SetTailnetKey("not-a-key"); !errors.Is(err, ErrTailnetKey) {
		t.Fatalf("bad key: %v", err)
	}
	if err := m.SetTailnetKey("tskey-auth-abc123"); err != nil {
		t.Fatal(err)
	}
	m.reconcileTailnet(ctx)
	n := m.tailnetNode("app:immich")
	if n == nil || n.State != "running" || n.URL != "https://immich.tail1234.ts.net" {
		t.Fatalf("running: %+v", n)
	}
	run := fake.called("systemd-run")
	if len(run) != 1 || !strings.Contains(run[0], "--unit=titanos-ts-app-immich") || !strings.Contains(run[0], "--tun=userspace-networking") ||
		!strings.Contains(run[0], "--statedir="+filepath.Join(mount, "data", tailnetState, "app-immich")) {
		t.Fatalf("systemd-run: %v", run)
	}
	// The key reaches tailscale only as a file, which is gone afterwards.
	if fake.keySeen != "tskey-auth-abc123" {
		t.Fatalf("key seen %q", fake.keySeen)
	}
	for _, c := range fake.calls {
		if strings.Contains(c, "tskey-") {
			t.Fatalf("key on a command line: %s", c)
		}
	}
	if _, err := os.Stat(filepath.Join(m.Tailnet.runDir, "authkey")); !os.IsNotExist(err) {
		t.Fatal("key file left behind")
	}
	serve := fake.called("tailscale --socket=" + m.Tailnet.socket("app-immich") + " serve")
	if len(serve) != 1 || !strings.HasSuffix(serve[0], "--https=443 https+insecure://127.0.0.1:443") {
		t.Fatalf("serve: %v", serve)
	}
	st, err := os.Stat(filepath.Join(mount, "data", tailnetState, "app-immich"))
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Fatalf("state folder: %v %v", st, err)
	}

	// A second round changes nothing that runs.
	m.reconcileTailnet(ctx)
	if len(fake.called("systemd-run")) != 1 || len(fake.called("tailscale --socket="+m.Tailnet.socket("app-immich")+" serve")) != 1 {
		t.Fatal("restarted or served again")
	}
	info := m.Info()
	if info.Apps[0].Tailnet == nil || info.Apps[0].Tailnet.URL != "https://immich.tail1234.ts.net" || !info.Tailnet.KeySet || !info.Tailnet.Available {
		t.Fatalf("info %+v %+v", info.Apps[0].Tailnet, info.Tailnet)
	}

	// Switched off: stopped, the key stays for later.
	m.SetGeneral(General{Off: ptr(true)})
	m.reconcileTailnet(ctx)
	if len(fake.called("systemctl stop titanos-ts-app-immich")) == 0 || m.tailnetNode("app:immich").State != "stopped" {
		t.Fatal("not stopped")
	}
	if _, err := os.Stat(filepath.Join(mount, "data", tailnetState, "app-immich")); err != nil {
		t.Fatal("state removed when only switched off")
	}

	// Without Tailscale: the device leaves the tailnet and forgets its key.
	m.SetGeneral(General{Off: ptr(false)})
	m.reconcileTailnet(ctx)
	m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: 2283})
	m.reconcileTailnet(ctx)
	if len(fake.called("tailscale --socket="+m.Tailnet.socket("app-immich")+" logout")) != 1 {
		t.Fatal("no logout")
	}
	if _, err := os.Stat(filepath.Join(mount, "data", tailnetState, "app-immich")); !os.IsNotExist(err) {
		t.Fatal("state not removed")
	}
	if m.tailnetNode("app:immich") != nil {
		t.Fatal("node kept")
	}
}

func TestTailnetUnavailableAndNames(t *testing.T) {
	m, fake, _ := newTailnetManager(t)
	m.Tailnet.Volume = func() (string, bool) { return "", false }
	m.SetTailnetKey("tskey-auth-x")
	m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: 2283, Tailscale: true, TailscaleName: "photos"})
	m.reconcileTailnet(context.Background())
	if len(fake.called("systemd-run")) != 0 || m.tailnetNode("app:immich").State != "stopped" || m.Info().Tailnet.Available {
		t.Fatal("started without the plugin")
	}
	// Names in the tailnet are unique, and only labels.
	if err := m.SetRoute("other", Route{Enabled: true, Name: "other", Port: 2284, Tailscale: true, TailscaleName: "photos"}); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("same tailnet name: %v", err)
	}
	if err := m.SetRoute("other", Route{Enabled: true, Name: "other", Port: 2284, Tailscale: true, TailscaleName: "a.b"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad tailnet name: %v", err)
	}
	// The name in the tailnet is only stored where it differs.
	m.SetRoute("same", Route{Enabled: true, Name: "same", Port: 2285, Tailscale: true, TailscaleName: "same"})
	if m.Settings().Apps["same"].TailscaleName != "" || m.Settings().Apps["immich"].TailscaleName != "photos" {
		t.Fatalf("stored %+v", m.Settings().Apps)
	}
	// The key survives a restart and is never part of the status.
	again := NewManager(m.store)
	again.Load()
	if !again.TailnetInfo().KeySet {
		t.Fatal("key not read back")
	}
	data, _ := json.Marshal(again.Info())
	if strings.Contains(string(data), "tskey") {
		t.Fatal("key in the status")
	}
}

func TestTailnetRequestsOnlyFromLoopback(t *testing.T) {
	m, _, _ := newTailnetManager(t)
	var got http.Header
	port := upstreamPort(t, func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		io.WriteString(w, "app")
	})
	m.SetTailnetKey("tskey-auth-x")
	m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: port, Tailscale: true, Networks: []string{"10.99.0.0/16"}})
	m.reconcileTailnet(context.Background())
	handler := m.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "webui") }))
	ask := func(host, remote string, header http.Header) (int, string) {
		req := httptest.NewRequest("GET", "/", nil)
		req.Host, req.RemoteAddr = host, remote
		for k, v := range header {
			req.Header[k] = v
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res.Code, res.Body.String()
	}
	// From the device's own Serve: the app, despite the own networks
	// (the tailnet's rules decide), with who asks.
	code, body := ask("immich.tail1234.ts.net", "127.0.0.1:50000", http.Header{"Tailscale-User-Login": {"alex@example.com"}})
	if code != 200 || body != "app" || got.Get("Tailscale-User-Login") != "alex@example.com" {
		t.Fatalf("via tailnet: %d %q %v", code, body, got)
	}
	// Anyone in the home network asking for the ts.net name directly is turned away.
	if code, _ := ask("immich.tail1234.ts.net", "192.168.1.5:50000", nil); code != http.StatusMisdirectedRequest {
		t.Fatalf("from the LAN: %d", code)
	}
	// On the home-network path nobody can claim a Tailscale identity.
	m.SetRoute("immich", Route{Enabled: true, Name: "immich", Port: port, Tailscale: true})
	got = nil
	if code, _ := ask("immich.nas.local", "192.168.1.5:50000", http.Header{"Tailscale-User-Login": {"evil@example.com"}}); code != 200 || got.Get("Tailscale-User-Login") != "" {
		t.Fatalf("forged identity passed: %d %v", code, got)
	}
	// Switched off: the name belongs to nobody.
	m.SetGeneral(General{Off: ptr(true)})
	if code, body := ask("immich.tail1234.ts.net", "127.0.0.1:50000", nil); code != 200 || body != "webui" {
		t.Fatalf("off: %d %q", code, body)
	}
}
