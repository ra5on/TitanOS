package wgserver

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type fakeRunner struct {
	calls []string
	stdin []string
	// conf is what wg syncconf read.
	conf string
	dump string
	// noLink makes "ip link show" fail (the interface does not exist).
	noLink bool
}

func (f *fakeRunner) Run(_ context.Context, stdin, name string, args ...string) (string, error) {
	call := name + " " + strings.Join(args, " ")
	f.calls = append(f.calls, call)
	f.stdin = append(f.stdin, stdin)
	switch {
	case strings.HasPrefix(call, "ip link show") && f.noLink:
		return "", errors.New("does not exist")
	case strings.HasPrefix(call, "wg syncconf"):
		data, _ := os.ReadFile(args[2])
		f.conf = string(data)
	case strings.HasPrefix(call, "wg show"):
		return f.dump, nil
	case strings.HasPrefix(call, "iptables -t nat -C"):
		return "", errors.New("no such rule")
	}
	return "", nil
}

func fixture(t *testing.T) (*Manager, *fakeRunner, *config.Store) {
	store := config.Open(t.TempDir())
	m := New(store)
	f := &fakeRunner{noLink: true}
	m.runner, m.runDir = f, t.TempDir()
	m.forward = func() error { return nil }
	m.HomeNetworks = func() []string { return []string{"192.168.178.0/24", "10.253.0.0/16", "2001:db8::/64"} }
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	return m, f, store
}

func has(calls []string, prefix string) bool {
	for _, c := range calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestSwitchOnAddDeviceAndRemove(t *testing.T) {
	m, f, store := fixture(t)
	ctx := context.Background()
	if err := m.Set(ctx, Settings{Enabled: true, Endpoint: "NAS.example.org"}); err != nil {
		t.Fatal(err)
	}
	s := m.Settings()
	// 10.253.0.0/24 overlaps a home network, so the next one is taken.
	if s.Network != "10.254.13.0/24" || s.Port != DefaultPort || s.Endpoint != "nas.example.org" {
		t.Fatalf("settings %+v", s)
	}
	for _, want := range []string{"ip link add dev titanoswg type wireguard", "wg syncconf titanoswg", "ip address replace 10.254.13.1/24 dev titanoswg", "ip link set up dev titanoswg", "iptables-restore --noflush", "iptables -t nat -I POSTROUTING -j TITANOS-WG"} {
		if !has(f.calls, want) {
			t.Fatalf("missing %q in %v", want, f.calls)
		}
	}
	if !strings.Contains(strings.Join(f.stdin, ""), "-A TITANOS-WG -s 10.254.13.0/24 ! -o titanoswg -j MASQUERADE") {
		t.Fatal("no masquerade rule")
	}
	for _, c := range f.calls {
		if strings.Contains(c, "PrivateKey") || strings.Contains(c, m.private) {
			t.Fatal("private key on a command line")
		}
	}
	if entries, _ := os.ReadDir(m.runDir); len(entries) != 0 {
		t.Fatal("configuration with the key left in /run")
	}
	key, err := store.Read(keyFile)
	if err != nil || !validKey(strings.TrimSpace(string(key))) {
		t.Fatal("server key not stored")
	}

	peer, text, err := m.AddPeer(ctx, "iPhone von Alex")
	if err != nil {
		t.Fatal(err)
	}
	if peer.Address != "10.254.13.2/32" || !validKey(peer.PublicKey) || !validKey(peer.Preshared) {
		t.Fatalf("peer %+v", peer)
	}
	for _, want := range []string{"PrivateKey = ", "Address = 10.254.13.2/32", "Endpoint = nas.example.org:51820", "AllowedIPs = 10.254.13.0/24, 192.168.178.0/24, 10.253.0.0/16", "PresharedKey = " + peer.Preshared, "PersistentKeepalive = 25"} {
		if !strings.Contains(text, want) {
			t.Fatalf("client config lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "2001:db8") {
		t.Fatal("IPv6 home network in a split tunnel")
	}
	if !strings.Contains(f.conf, "PublicKey = "+peer.PublicKey) || !strings.Contains(f.conf, "AllowedIPs = 10.254.13.2/32") {
		t.Fatalf("server config:\n%s", f.conf)
	}
	// The private key of the device is nowhere on the NAS.
	stored, _ := store.Read(settingsFile)
	private := strings.TrimPrefix(strings.Split(strings.Split(text, "PrivateKey = ")[1], "\n")[0], "")
	if strings.Contains(string(stored), private) || strings.Contains(f.conf, private) {
		t.Fatal("device key stored")
	}
	second, _, err := m.AddPeer(ctx, "Laptop")
	if err != nil || second.Address != "10.254.13.3/32" {
		t.Fatalf("second peer %+v %v", second, err)
	}
	if _, _, err := m.AddPeer(ctx, "laptop"); !errors.Is(err, ErrNameUsed) {
		t.Fatal("same name twice")
	}
	if _, _, err := m.AddPeer(ctx, "bad\nname"); !errors.Is(err, ErrInvalid) {
		t.Fatal("control characters in the name")
	}
	// The network cannot move under existing devices.
	if err := m.Set(ctx, Settings{Enabled: true, Network: "192.168.253.0/24"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("network changed with devices")
	}

	f.dump = "privkey\tpubkey\t51820\toff\n" + peer.PublicKey + "\tpsk\t203.0.113.5:4500\t10.254.13.2/32\t1793200000\t1024\t2048\t25\n"
	st := m.Status()
	if !st.Running || len(st.Peers) != 2 || st.Peers[0].LastHandshake == nil || st.Peers[0].Received != 1024 || st.Peers[0].Endpoint != "203.0.113.5:4500" || st.Peers[1].LastHandshake != nil {
		t.Fatalf("status %+v", st)
	}

	if err := m.RemovePeer(ctx, peer.PublicKey); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(f.conf, peer.PublicKey) {
		t.Fatal("removed device still in the server config")
	}
	if err := m.RemovePeer(ctx, peer.PublicKey); !errors.Is(err, ErrNotFound) {
		t.Fatal("removed twice")
	}

	// Switching off removes interface and NAT rules; devices stay.
	f.calls = nil
	if err := m.Set(ctx, Settings{Enabled: false, Endpoint: "nas.example.org"}); err != nil {
		t.Fatal(err)
	}
	if !has(f.calls, "ip link del dev titanoswg") || !has(f.calls, "iptables -t nat -X TITANOS-WG") || len(m.Settings().Peers) != 1 {
		t.Fatalf("switch off: %v", f.calls)
	}
	// A restart reads everything back.
	again := New(store)
	again.Load()
	if s := again.Settings(); s.Network != "10.254.13.0/24" || len(s.Peers) != 1 || again.private != m.private {
		t.Fatalf("after restart %+v", s)
	}
}

func TestFullTunnelAndValidation(t *testing.T) {
	m, _, _ := fixture(t)
	ctx := context.Background()
	if err := m.Set(ctx, Settings{Enabled: true, Endpoint: "2001:db8::1", FullTunnel: true, DNS: "192.168.178.1"}); err != nil {
		t.Fatal(err)
	}
	_, text, err := m.AddPeer(ctx, "Handy")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "AllowedIPs = 0.0.0.0/0, ::/0") || !strings.Contains(text, "Endpoint = [2001:db8::1]:51820") || !strings.Contains(text, "DNS = 192.168.178.1") {
		t.Fatalf("full tunnel config:\n%s", text)
	}
	for _, bad := range []Settings{
		{Endpoint: "nas example"}, {Endpoint: "nas.example.org:51820"}, {Port: 80}, {Network: "8.8.8.0/24"},
		{Network: "10.0.0.0/8"}, {Network: "10.1.2.3/24"}, {DNS: "dns.example.org"}, {Endpoint: "-x;rm"},
	} {
		if _, err := normalize(withDefaults(bad)); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
	}
}

func withDefaults(s Settings) Settings {
	if s.Network == "" {
		s.Network = "10.253.0.0/24"
	}
	return s
}

func TestDevelopmentModeTouchesNothing(t *testing.T) {
	m, f, _ := fixture(t)
	m.Disabled = true
	if err := m.Set(context.Background(), Settings{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("calls in development mode: %v", f.calls)
	}
}
