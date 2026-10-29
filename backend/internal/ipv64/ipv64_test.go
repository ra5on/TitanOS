package ipv64

import (
	"context"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider/ipv64fake"
)

type fakeRunner struct {
	mu    sync.Mutex
	calls []string
}

func (f *fakeRunner) Run(_ context.Context, stdin, name string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	return "", nil
}

func (f *fakeRunner) all() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.calls, "\n")
}

type fakeFirewall struct {
	list []netip.Prefix
}

func (f *fakeFirewall) SetBlocklist(_ context.Context, list []netip.Prefix) error {
	f.list = list
	return nil
}

func testManager(t *testing.T) (*Manager, *ipv64fake.Fake, *fakeRunner, *config.Store) {
	t.Helper()
	fake := ipv64fake.New(t)
	store := config.Open(t.TempDir())
	runner := &fakeRunner{}
	m := NewManager(store)
	m.runner = runner
	m.runDir = t.TempDir()
	m.Options = dnsprovider.Options{BaseURL: fake.Server.URL}
	m.Fallback = func() (string, string) { return ipv64fake.Key, "" }
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	return m, fake, runner, store
}

func TestParseBlocklist(t *testing.T) {
	list, truncated, err := ParseBlocklist(strings.NewReader("# comment\n198.51.100.7\n198.51.100.7 ; again\n203.0.113.0/24\n10.0.0.1\n192.168.0.0/16\n127.0.0.1\n100.100.1.1\n2001:db8::/32\n8.0.0.0/8\nbogus\n::ffff:1.2.3.4\n"))
	if err != nil || truncated {
		t.Fatal(err, truncated)
	}
	got := []string{}
	for _, p := range list {
		got = append(got, p.String())
	}
	if strings.Join(got, ",") != "198.51.100.7/32,203.0.113.0/24,2001:db8::/32,1.2.3.4/32" {
		t.Fatalf("parsed %v", got)
	}
	if _, _, err := ParseBlocklist(strings.NewReader("<html>nope</html>\n")); err == nil {
		t.Fatal("an HTML page was taken as a list")
	}
	if _, err := listAddress("http://ipv64.net/x/", ""); err == nil {
		t.Fatal("plain HTTP accepted")
	}
	if _, err := listAddress("https://evil.example/x/", ""); err == nil {
		t.Fatal("other host accepted")
	}
	if a, err := listAddress("https://ipv64.net/lists/abc", ""); err != nil || a != "https://ipv64.net/lists/abc/ipv64_blocklist_v64.txt" {
		t.Fatal(a, err)
	}
}

func TestBlocklistFetchedAtMostDaily(t *testing.T) {
	m, fake, _, store := testManager(t)
	fw := &fakeFirewall{}
	m.Firewall = fw
	ctx := context.Background()
	m.refreshList(ctx)
	if fake.Called("GET /honeypot/list-token") {
		t.Fatal("fetched while off")
	}
	if err := m.SetBlocklist(ctx, Blocklist{Enabled: true}); err != nil {
		t.Fatal(err)
	}
	m.refreshList(ctx)
	if len(fw.list) != 2 {
		t.Fatalf("firewall got %v", fw.list)
	}
	st := m.Status().List
	if st.Entries != 2 || st.Live || st.Next == nil || st.Next.Sub(*st.Fetched) != BlocklistEvery {
		t.Fatalf("status %+v", st)
	}
	before := len(fake.Calls)
	m.refreshList(ctx)
	if len(fake.Calls) != before {
		t.Fatal("fetched again within a day")
	}
	if err := m.RefreshBlocklist(ctx); err == nil {
		t.Fatal("manual refresh within a minute")
	}
	// The list stays in RAM; only the switch is on the stick.
	data, _ := store.Read(settingsFile)
	if strings.Contains(string(data), "198.51.100.7") || strings.Contains(string(data), "token123") {
		t.Fatal("list or token on the stick")
	}
	if err := m.SetBlocklist(ctx, Blocklist{}); err != nil || fw.list != nil {
		t.Fatal("switching off keeps the list", err)
	}
}

func TestPollReportsChangesOnly(t *testing.T) {
	m, fake, _, _ := testManager(t)
	var mu sync.Mutex
	var got []string
	m.Notify = func(severity, kind, subject string) {
		mu.Lock()
		got = append(got, kind+":"+subject)
		mu.Unlock()
	}
	now := time.Unix(10_000, 0)
	m.now = func() time.Time { return now }
	ctx := context.Background()
	m.pollOnce(ctx)
	if len(fake.Calls) != 0 {
		t.Fatal("polled while off")
	}
	m.SetNotify(Notify{Enabled: true, Failover: true, Healthchecks: true, CDNTraffic: true})
	fake.Checks["hc1"] = map[string]any{"token": "hc1", "name": "nas", "type": "push", "status": "up", "notify": map[string]any{}}
	fake.Policies["p1"] = map[string]any{"id": "p1", "name": "web", "status": "active", "monitors": []any{}, "rules": []any{}}
	m.pollOnce(ctx)
	first := len(fake.Calls)
	m.pollOnce(ctx)
	if len(fake.Calls) != first {
		t.Fatal("polled again within five minutes")
	}
	fake.Checks["hc1"]["status"] = "down"
	fake.Policies["p1"]["status"] = "failed_over"
	now = now.Add(PollEvery)
	m.pollOnce(ctx)
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	joined := strings.Join(got, ",")
	mu.Unlock()
	if !strings.Contains(joined, "ipv64_check_down:nas") || !strings.Contains(joined, "ipv64_failover:web") || strings.Contains(joined, "cdn") {
		t.Fatalf("notifications %q", joined)
	}
}

func TestWireGuardConnectKeepsPrivateKeyHome(t *testing.T) {
	m, fake, runner, store := testManager(t)
	ctx := context.Background()
	client, err := m.Client()
	if err != nil {
		t.Fatal(err)
	}
	router, err := client.CreateRouter(ctx, "home")
	if err != nil {
		t.Fatal(err)
	}
	gateway := router.Gateways[0].ID
	if err := m.Connect(ctx, client, router.ID, gateway, "titanos"); err != nil {
		t.Fatal(err)
	}
	if !fake.Called("PUT /peers/" + m.Settings().WireGuard.Peer + "/keys") {
		t.Fatal("public key not set")
	}
	key, _ := store.Read(privateKeyFile)
	if !validKey(strings.TrimSpace(string(key))) {
		t.Fatal("no private key on /config")
	}
	status := m.Status()
	data := strings.Join([]string{status.Settings.WireGuard.PublicKey, strings.Join(status.Settings.WireGuard.AllowedIPs, ",")}, " ")
	if strings.Contains(data, strings.TrimSpace(string(key))) {
		t.Fatal("private key in the status")
	}
	calls := runner.all()
	for _, want := range []string{"ip link add dev ipv64wg type wireguard", "wg set ipv64wg private-key ", "preshared-key ", "endpoint vpn1.ipv64.net:51820", "ip address add 10.64.0.5/32 dev ipv64wg", "ip route replace 10.64.0.0/24 dev ipv64wg"} {
		if !strings.Contains(calls, want) {
			t.Fatalf("missing %q in\n%s", want, calls)
		}
	}
	if strings.Contains(calls, strings.TrimSpace(string(key))) || strings.Contains(calls, "route replace 0.0.0.0/0") {
		t.Fatal("private key on a command line or default route set", calls)
	}
	if !status.Tunnel.Up {
		t.Fatal("tunnel not up", status.Tunnel)
	}
	// After a restart the interface comes from /config without IPv64.
	m2 := NewManager(store)
	runner2 := &fakeRunner{}
	m2.runner, m2.runDir = runner2, t.TempDir()
	m2.Load(ctx)
	if !strings.Contains(runner2.all(), "ip link add dev ipv64wg") {
		t.Fatal("not set up at the start")
	}
	if err := m.Disconnect(ctx, client, true); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Read(privateKeyFile); err == nil {
		t.Fatal("private key kept after disconnecting")
	}
	if m.Settings().WireGuard.Enabled || !strings.Contains(runner.all(), "ip link del dev ipv64wg") {
		t.Fatal("still connected")
	}
}

func TestParsePeerConfigRejectsBrokenOnes(t *testing.T) {
	for _, text := range []string{"", "[Peer]\nPublicKey = x\n", "[Interface]\nAddress = 10.0.0.2/32\n[Peer]\nPublicKey = GatewayPublicKeyGatewayPublicKeyGatewayPubA=\nEndpoint = nohost\n"} {
		if _, _, err := ParsePeerConfig(text); err == nil {
			t.Fatalf("accepted %q", text)
		}
	}
	if got := routedNetworks([]string{"0.0.0.0/0", "::/0", "10.1.0.0/16"}); strings.Join(got, ",") != "10.1.0.0/16" {
		t.Fatal(got)
	}
}

func TestKeyFallbackAndSecretStatus(t *testing.T) {
	m, _, _, _ := testManager(t)
	if key, source := m.Key(); key != ipv64fake.Key || source != "ddns" {
		t.Fatal(key, source)
	}
	m.SetKey("own-key", false)
	if key, source := m.Key(); key != "own-key" || source != "own" {
		t.Fatal(key, source)
	}
	st := m.Status()
	if !st.KeySet || st.Settings.APIKey != "" {
		t.Fatal("key in the status")
	}
	m.SetKey("", true)
	if _, source := m.Key(); source != "ddns" {
		t.Fatal(source)
	}
}
