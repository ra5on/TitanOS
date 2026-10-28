package firewall

import (
	"context"
	"errors"
	"flag"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// fakeNft records the scripts and answers like nft.
type fakeNft struct {
	mu      sync.Mutex
	scripts []string
	fail    bool
	listErr bool
}

func (f *fakeNft) Run(_ context.Context, stdin string, name string, args ...string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name != "nft" {
		return "", errors.New("unexpected command " + name)
	}
	if len(args) > 0 && args[0] == "-t" {
		if f.listErr {
			return "", errors.New("no such table")
		}
		return "", nil
	}
	if f.fail {
		return "", errors.New("nft: syntax error")
	}
	f.scripts = append(f.scripts, stdin)
	return "", nil
}

func (f *fakeNft) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.scripts) == 0 {
		return ""
	}
	return f.scripts[len(f.scripts)-1]
}

func newTestManager(t *testing.T) (*Manager, *fakeNft, *config.Store) {
	t.Helper()
	store := config.Open(t.TempDir())
	nft := &fakeNft{}
	m := NewManager(store)
	m.runner = nft
	m.trial = time.Hour
	m.runFile = filepath.Join(t.TempDir(), "blocks.json")
	m.HomeNetworks = func() []string { return []string{"192.168.1.0/24", "2001:db8:1:2::/64", "100.100.1.0/24"} }
	return m, nft, store
}

func sampleRuleset() ruleset {
	cfg := Defaults()
	cfg.Enabled = true
	cfg.Apps["jellyfin"] = Zones{Home: true, VPN: true, Internet: true}
	cfg.Apps["container:pihole"] = Zones{Home: true}
	cfg.Rules = []Rule{
		{Action: "allow", Protocol: "tcp", Ports: "8443", Source: "203.0.113.0/24", Comment: "office"},
		{Action: "deny", Protocol: "any", Source: "192.168.1.66", Zone: Home},
		{Action: "allow", Protocol: "udp", Ports: "60000-60100", Zone: Internet},
		{Action: "allow", Protocol: "any", Ports: "9000", Disabled: true},
	}
	return ruleset{
		cfg:  normal(cfg),
		web:  WebPorts{HTTP: 80, HTTPS: 443},
		home: homeNetworks([]string{"192.168.1.0/24", "2001:db8:1:2::/64"}),
		apps: []App{
			{Key: "jellyfin", Name: "jellyfin", Ports: []AppPort{{8096, "tcp"}, {7359, "udp"}}},
			{Key: "immich", Name: "immich", Ports: []AppPort{{2283, "tcp"}}},
			{Key: "container:pihole", Name: "pihole", Ports: []AppPort{{53, "tcp"}, {53, "udp"}, {8053, "tcp"}}},
		},
		blocked: []netip.Prefix{netip.MustParsePrefix("198.51.100.7/32"), netip.MustParsePrefix("2001:db8:bad::/64")},
		admin:   netip.MustParseAddr("192.168.1.20"),
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the generated rules:\n%s", name, got)
	}
}

func TestRenderGolden(t *testing.T) {
	golden(t, "enabled.nft", render(sampleRuleset()))

	blocksOnly := sampleRuleset()
	blocksOnly.cfg.Enabled = false
	golden(t, "blocks-only.nft", render(blocksOnly))

	off := blocksOnly
	off.blocked = nil
	if got := render(off); got != offScript() {
		t.Errorf("off without blocks: %q", got)
	}
}

// TestRenderDefaults checks the owner's defaults line by line: home and VPN
// get every service, discovery only home, the internet only the tunnels.
func TestRenderDefaults(t *testing.T) {
	cfg := Defaults()
	cfg.Enabled = true
	script := render(ruleset{cfg: normal(cfg), home: homeNetworks(nil)})
	zone := func(name string) string {
		start := strings.Index(script, "\tchain "+name+" {")
		end := strings.Index(script[start:], "\n\t}\n")
		return script[start : start+end]
	}
	home, vpn, internet := zone("home"), zone("vpn"), zone("internet")
	for _, want := range []string{`tcp dport { 80, 443 } accept comment "webui"`, `tcp dport { 139, 445 } accept comment "smb"`, `tcp dport 22 accept comment "ssh"`, `udp dport { 3702, 5353, 5355 } accept comment "discovery"`, `tcp dport { 111, 2049, 20048, 32765-32766, 32768 } accept comment "nfs"`} {
		if !strings.Contains(home, want) {
			t.Errorf("home lacks %s", want)
		}
	}
	if !strings.Contains(vpn, `tcp dport 22 accept`) || strings.Contains(vpn, "discovery") {
		t.Errorf("vpn zone:\n%s", vpn)
	}
	if strings.Contains(internet, "webui") || strings.Contains(internet, "ssh") || !strings.Contains(internet, `udp dport { 41641, 51820 } accept comment "vpn"`) {
		t.Errorf("internet zone:\n%s", internet)
	}
	if !strings.Contains(zone("app_internet"), "goto drop_app_internet") || !strings.Contains(zone("app_home"), `accept comment "new apps"`) {
		t.Error("new apps should be allowed from home only")
	}
}

// TestRealNft loads the generated rules into the kernel in a network
// namespace of its own, if this machine allows that without root.
func TestRealNft(t *testing.T) {
	nft, err := exec.LookPath("nft")
	if err != nil {
		for _, p := range []string{"/usr/sbin/nft", "/sbin/nft"} {
			if _, err := os.Stat(p); err == nil {
				nft = p
			}
		}
	}
	unshare, uerr := exec.LookPath("unshare")
	if nft == "" || uerr != nil {
		t.Skip("nft or unshare not installed")
	}
	if out, err := exec.Command(unshare, "-rn", nft, "list", "ruleset").CombinedOutput(); err != nil {
		t.Skipf("no user and network namespace here: %v %s", err, out)
	}
	blocksOnly := sampleRuleset()
	blocksOnly.cfg.Enabled = false
	for name, script := range map[string]string{"enabled": render(sampleRuleset()), "blocks-only": render(blocksOnly), "off": offScript(), "defaults": render(ruleset{cfg: func() Config { c := Defaults(); c.Enabled = true; return c }(), home: homeNetworks(nil)})} {
		cmd := exec.Command(unshare, "-rn", nft, "-f", "-")
		cmd.Stdin = strings.NewReader(script)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Errorf("%s: nft refused the rules: %v\n%s", name, err, out)
		}
	}
}

func TestClassify(t *testing.T) {
	home := homeNetworks([]string{"192.168.1.0/24", "2001:db8:1:2::/64", "100.100.1.0/24", "0.0.0.0/0", "fe80::/64"})
	for addr, want := range map[string]Zone{
		"192.168.1.5":         Home,
		"10.1.2.3":            Home,
		"172.20.0.4":          Home, // Docker networks
		"169.254.3.3":         Home,
		"fd00::12":            Home,
		"fe80::1":             Home,
		"2001:db8:1:2::77":    Home, // public prefix of the NAS's own LAN
		"2001:db8:1:3::77":    Internet,
		"2a00:1450:4001::200": Internet, // public IPv6 elsewhere
		"100.101.102.103":     VPN,
		"fd7a:115c:a1e0::9":   VPN,
		"203.0.113.9":         Internet,
		"::ffff:192.168.1.5":  Home,
		"127.0.0.1":           Home,
		"::1":                 Home,
	} {
		if got := classify(netip.MustParseAddr(addr), home); got != want {
			t.Errorf("%s: %s, want %s", addr, got, want)
		}
	}
	for _, p := range home {
		if p.Bits() == 0 || p.String() == "100.100.1.0/24" || p.String() == "fe80::/64" {
			t.Errorf("home networks should not contain %s", p)
		}
	}
}

func TestValidate(t *testing.T) {
	ok := normal(Defaults())
	ok.Rules = []Rule{{Action: "ALLOW", Protocol: "tcp", Ports: "1-65535", Source: "2001:db8::/32"}}
	if err := validate(normal(ok)); err != nil {
		t.Fatal(err)
	}
	for name, r := range map[string]Rule{
		"action":  {Action: "reject", Protocol: "tcp"},
		"proto":   {Action: "allow", Protocol: "sctp"},
		"port":    {Action: "allow", Protocol: "tcp", Ports: "70000"},
		"range":   {Action: "allow", Protocol: "tcp", Ports: "90-80"},
		"source":  {Action: "allow", Protocol: "tcp", Source: "example.org"},
		"zone":    {Action: "allow", Protocol: "tcp", Zone: "dmz"},
		"comment": {Action: "allow", Protocol: "tcp", Comment: `a"b`},
	} {
		cfg := normal(Defaults())
		cfg.Rules = []Rule{r}
		if err := validate(normal(cfg)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	cfg := normal(Defaults())
	cfg.Apps["bad name;"] = Zones{}
	if !errors.Is(validate(cfg), ErrInvalid) {
		t.Error("app key not checked")
	}
}

func TestChangeOnTrialAndRollback(t *testing.T) {
	m, nft, store := newTestManager(t)
	reverted := make(chan struct{}, 1)
	m.OnRevert = func() { reverted <- struct{}{} }
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if nft.last() != offScript() {
		t.Fatalf("off by default must load no rules: %q", nft.last())
	}
	cfg := Defaults()
	cfg.Enabled = true
	m.trial = 50 * time.Millisecond
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nft.last(), `ip saddr 192.168.1.20 accept comment "admin on trial"`) {
		t.Fatal("the admin must be let in while on trial")
	}
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); !errors.Is(err, ErrPending) {
		t.Errorf("second change: %v", err)
	}
	select {
	case <-reverted:
	case <-time.After(2 * time.Second):
		t.Fatal("no rollback")
	}
	if nft.last() != offScript() {
		t.Errorf("rollback should bring back the open state: %q", nft.last())
	}
	if _, err := store.Read(settingsFile); err == nil {
		t.Error("an unconfirmed change must not be stored")
	}

	m.trial = time.Hour
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	if err := m.Confirm(context.Background()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(nft.last(), "admin on trial") || !strings.Contains(nft.last(), "chain home") {
		t.Error("confirmed rules should have no admin exception")
	}
	var saved Config
	if err := store.ReadJSON(settingsFile, &saved); err != nil || !saved.Enabled {
		t.Fatalf("not saved: %v %+v", err, saved)
	}
	if err := m.Confirm(context.Background()); !errors.Is(err, ErrNoPending) {
		t.Errorf("confirm twice: %v", err)
	}

	// Revert gives up at once; switching off needs no trial.
	cfg.Services["ssh"] = Zones{}
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	if err := m.Revert(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(nft.last(), `tcp dport 22 accept comment "ssh"`) {
		t.Error("revert should bring SSH back")
	}
	cfg.Enabled = false
	if err := m.Change(context.Background(), cfg, "203.0.113.5"); err != nil {
		t.Fatal(err)
	}
	if m.Status("").Pending != nil || nft.last() != offScript() {
		t.Error("switching off should apply at once")
	}
}

func TestChangeRefusesToLockOutTheAdmin(t *testing.T) {
	m, nft, _ := newTestManager(t)
	m.Load(context.Background())
	cfg := Defaults()
	cfg.Enabled = true
	// From the internet the web UI is closed by default.
	if err := m.Change(context.Background(), cfg, "203.0.113.5"); !errors.Is(err, ErrExcludesYou) {
		t.Errorf("internet admin: %v", err)
	}
	cfg.Rules = []Rule{{Action: "deny", Protocol: "tcp", Ports: "443", Source: "192.168.1.20"}}
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); !errors.Is(err, ErrExcludesYou) {
		t.Errorf("own deny rule: %v", err)
	}
	cfg.Rules = []Rule{{Action: "allow", Protocol: "tcp", Ports: "443", Source: "203.0.113.0/24"}}
	if err := m.Change(context.Background(), cfg, "203.0.113.5"); err != nil {
		t.Errorf("an own allow rule should do: %v", err)
	}
	m.Revert(context.Background())
	nft.fail = true
	cfg.Rules = nil
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); !errors.Is(err, ErrUnavailable) {
		t.Errorf("nft failing: %v", err)
	}
	if m.Status("").Pending != nil {
		t.Error("a change nft refused must not stay pending")
	}
}

func TestAllowHTTP(t *testing.T) {
	m, _, _ := newTestManager(t)
	m.Load(context.Background())
	if !m.AllowHTTP("203.0.113.5:4000", false) {
		t.Error("off: everything is allowed")
	}
	cfg := Defaults()
	cfg.Enabled = true
	cfg.Services["webui"] = Zones{Home: true}
	cfg.Services["apps"] = Zones{Home: true, VPN: true, Internet: true}
	if err := m.Change(context.Background(), cfg, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		addr string
		app  bool
		want bool
	}{
		{"192.168.1.30:1", false, true},
		{"[2001:db8:1:2::5]:1", false, true},
		{"100.64.0.8:1", false, false},
		{"100.64.0.8:1", true, true},
		{"203.0.113.5:1", true, true},
		{"203.0.113.5:1", false, false},
		{"127.0.0.1:1", false, true},
		{"garbage", false, false},
	} {
		if got := m.AllowHTTP(c.addr, c.app); got != c.want {
			t.Errorf("%s app=%v: %v", c.addr, c.app, got)
		}
	}
}

func TestRefreshFollowsInterfacesAndDocker(t *testing.T) {
	m, nft, store := newTestManager(t)
	cfg := Defaults()
	cfg.Enabled = true
	store.WriteJSON(settingsFile, cfg)
	apps := []App{{Key: "web", Name: "web", Ports: []AppPort{{8080, "tcp"}}}}
	m.Apps = func(context.Context) ([]App, bool) { return apps, true }
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	loads := len(nft.scripts)
	m.refresh(context.Background())
	if len(nft.scripts) != loads+1 || !strings.Contains(nft.last(), `tcp dport 8080 accept comment "app web"`) {
		t.Fatalf("new Docker port not taken over:\n%s", nft.last())
	}
	m.refresh(context.Background())
	if len(nft.scripts) != loads+1 {
		t.Error("nothing changed, nothing should be loaded")
	}
	nft.listErr = true
	m.refresh(context.Background())
	if len(nft.scripts) != loads+2 {
		t.Error("a missing table should be loaded again")
	}
	nft.listErr = false
	m.HomeNetworks = func() []string { return []string{"192.168.7.0/24", "2001:db8:9::/64"} }
	m.refresh(context.Background())
	if !strings.Contains(nft.last(), "2001:db8:9::/64") {
		t.Error("a new home prefix should be taken over")
	}
	// Docker not answering keeps the last known ports.
	m.Apps = func(context.Context) ([]App, bool) { return nil, false }
	m.refresh(context.Background())
	if !strings.Contains(nft.last(), "8080") {
		t.Error("ports lost while Docker did not answer")
	}
}

func TestAutoBlock(t *testing.T) {
	m, nft, store := newTestManager(t)
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	var blocked []Block
	m.OnBlock = func(b Block) { blocked = append(blocked, b) }
	m.Load(context.Background())

	// Off by default: nothing is counted.
	for range 20 {
		m.Failed("203.0.113.9", SourceWeb)
	}
	if len(blocked) != 0 {
		t.Fatal("auto block is off by default")
	}
	a := Defaults().AutoBlock
	a.Enabled, a.Attempts, a.Within, a.Minutes = true, 3, 5, 30
	if err := m.SetAutoBlock(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	m.Failed("203.0.113.9", SourceSSH)
	now = now.Add(6 * time.Minute) // the first one is out of the window
	m.Failed("203.0.113.9", SourceSSH)
	m.Failed("203.0.113.9", SourceSSH)
	if len(blocked) != 0 {
		t.Fatal("blocked although only two attempts are in the window")
	}
	m.Failed("203.0.113.9", SourceSMB)
	if len(blocked) != 1 || blocked[0].Address != "203.0.113.9" || blocked[0].Source != SourceSMB || !blocked[0].Until.Equal(now.Add(30*time.Minute)) {
		t.Fatalf("not blocked: %+v", blocked)
	}
	if !strings.Contains(nft.last(), "elements = { 203.0.113.9 }") || strings.Contains(nft.last(), "chain home") {
		t.Errorf("firewall off: only the block should be loaded:\n%s", nft.last())
	}
	// Home network and VPN are exempt; so are listed networks.
	for range 5 {
		m.Failed("192.168.1.50", SourceWeb)
		m.Failed("100.64.1.1", SourceWeb)
		m.Failed("127.0.0.1", SourceWeb)
	}
	if len(blocked) != 1 {
		t.Errorf("local senders were blocked: %+v", blocked)
	}
	// IPv6 senders are blocked by their /64.
	for _, addr := range []string{"2001:db8:bad:1::1", "2001:db8:bad:1::2", "2001:db8:bad:1:ffff::3"} {
		m.Failed(addr, SourceWeb)
	}
	if len(blocked) != 2 || blocked[1].Address != "2001:db8:bad:1::/64" {
		t.Fatalf("IPv6 /64 not blocked: %+v", blocked)
	}
	// Expiry lifts temporary blocks.
	now = now.Add(31 * time.Minute)
	m.refresh(context.Background())
	if got := m.Status("").Blocked; len(got) != 0 {
		t.Errorf("expired blocks still there: %+v", got)
	}
	if nft.last() != offScript() {
		t.Error("without blocks and firewall off the table should go")
	}

	// Permanent blocks go to the stick and come back after a restart.
	a.Permanent = true
	a.Exempt = []string{"198.51.100.0/24", " "}
	if err := m.SetAutoBlock(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		m.Failed("198.51.100.4", SourceWeb)
		m.Failed("203.0.113.77", SourceWeb)
	}
	if len(blocked) != 3 || !blocked[2].Until.IsZero() {
		t.Fatalf("permanent block: %+v", blocked)
	}
	m.flushPermanent(now, true)
	var permanent []Block
	if err := store.ReadJSON(blockedFile, &permanent); err != nil || len(permanent) != 1 {
		t.Fatalf("permanent blocks on the stick: %v %+v", err, permanent)
	}
	again := NewManager(store)
	again.runner, again.runFile = nft, ""
	again.Load(context.Background())
	if got := again.Status("").Blocked; len(got) != 1 || got[0].Address != "203.0.113.77" {
		t.Errorf("after a restart: %+v", got)
	}
	if err := again.Unblock(context.Background(), "203.0.113.77"); err != nil {
		t.Fatal(err)
	}
	if err := again.Unblock(context.Background(), "203.0.113.77"); !errors.Is(err, ErrNotBlocked) {
		t.Errorf("unblock twice: %v", err)
	}
	store.ReadJSON(blockedFile, &permanent)
	if len(permanent) != 0 {
		t.Error("unblocking should remove the permanent block from the stick")
	}

	bad := a
	bad.Attempts = 1
	if err := m.SetAutoBlock(context.Background(), bad); !errors.Is(err, ErrInvalid) {
		t.Errorf("one attempt: %v", err)
	}
}

func TestTemporaryBlocksSurviveRestartOfTitand(t *testing.T) {
	m, nft, store := newTestManager(t)
	m.Load(context.Background())
	a := Defaults().AutoBlock
	a.Enabled, a.Attempts = true, 2
	m.SetAutoBlock(context.Background(), a)
	m.Failed("203.0.113.1", SourceWeb)
	m.Failed("203.0.113.1", SourceWeb)
	again := NewManager(store)
	again.runner, again.runFile = nft, m.runFile
	again.Load(context.Background())
	if got := again.Status("").Blocked; len(got) != 1 {
		t.Errorf("temporary block lost: %+v", got)
	}
}

func TestParseJournal(t *testing.T) {
	for line, want := range map[string]string{
		"Invalid user admin from 203.0.113.4 port 50122":                                            "203.0.113.4",
		"Failed publickey for alex from 2001:db8::5 port 22 ssh2: ED25519 SHA256:x":                 "2001:db8::5",
		"Failed password for invalid user root from 198.51.100.3 port 1 ssh2":                       "198.51.100.3",
		"User alex from 203.0.113.7 not allowed because not listed in AllowUsers":                   "203.0.113.7",
		"Connection closed by authenticating user alex 192.0.2.8 port 5 [preauth]":                  "192.0.2.8",
		"Disconnecting authenticating user root 192.0.2.9 port 5: Too many authentication failures": "192.0.2.9",
	} {
		addr, ok := parseSSHFailure(line)
		if !ok || addr.String() != want {
			t.Errorf("%q: %v %v", line, addr, ok)
		}
	}
	for _, line := range []string{"Accepted publickey for alex from 1.2.3.4 port 5 ssh2: ED25519 SHA256:x", "Connection closed by 1.2.3.4 port 5 [preauth]"} {
		if _, ok := parseSSHFailure(line); ok {
			t.Errorf("%q is no failure", line)
		}
	}
	smb := `Auth: [SMB2,(null)] user [WORKGROUP]\[alex] at [Wed, 28 Oct 2026 12:00:00.000000 CET] with [NTLMv2] status [NT_STATUS_WRONG_PASSWORD] workstation [PC] remote host [ipv4:192.0.2.44:50123] mapped to [WORKGROUP]\[alex]. local host [ipv4:192.168.1.2:445] `
	if addr, ok := parseSMBFailure(smb); !ok || addr.String() != "192.0.2.44" {
		t.Errorf("smb: %v %v", addr, ok)
	}
	if addr, ok := parseSMBFailure(strings.Replace(strings.Replace(smb, "ipv4:192.0.2.44", "ipv6:2001:db8::7", 1), "WRONG_PASSWORD", "NO_SUCH_USER", 1)); !ok || addr.String() != "2001:db8::7" {
		t.Errorf("smb v6: %v %v", addr, ok)
	}
	if _, ok := parseSMBFailure(strings.Replace(smb, "NT_STATUS_WRONG_PASSWORD", "NT_STATUS_OK", 1)); ok {
		t.Error("a successful login is no failure")
	}

	drop, ok := parseDrop("titanos-fw internet IN=br0 OUT= MAC=aa:bb SRC=203.0.113.9 DST=192.168.1.2 LEN=60 TOS=0x00 PROTO=TCP SPT=40000 DPT=22 WINDOW=1 SYN URGP=0")
	if !ok || drop.Zone != "internet" || drop.Source != "203.0.113.9" || drop.Protocol != "tcp" || drop.Port != 22 || drop.Interface != "br0" {
		t.Errorf("drop: %+v %v", drop, ok)
	}
	drop, ok = parseDrop("titanos-fw app-vpn IN=tailscale0 OUT=docker0 SRC=100.64.0.2 DST=172.17.0.2 PROTO=UDP SPT=1 DPT=53")
	if !ok || !drop.App || drop.Zone != "vpn" || drop.Port != 53 {
		t.Errorf("app drop: %+v", drop)
	}
	if _, ok := parseDrop("usb 1-1: new device"); ok {
		t.Error("other kernel lines are no drops")
	}

	m, _, _ := newTestManager(t)
	m.Load(context.Background())
	a := Defaults().AutoBlock
	a.Enabled, a.Attempts = true, 2
	m.SetAutoBlock(context.Background(), a)
	journal := strings.Join([]string{
		`{"MESSAGE":"titanos-fw home IN=br0 SRC=192.168.1.9 DST=192.168.1.2 PROTO=TCP DPT=8080","_TRANSPORT":"kernel","__REALTIME_TIMESTAMP":"1793188800000000"}`,
		`{"MESSAGE":"Invalid user x from 203.0.113.50 port 1","SYSLOG_IDENTIFIER":"sshd-session"}`,
		`{"MESSAGE":"Invalid user y from 203.0.113.50 port 2","SYSLOG_IDENTIFIER":"sshd"}`,
		`not json`,
	}, "\n")
	if err := m.readJournal(strings.NewReader(journal)); err != nil {
		t.Fatal(err)
	}
	status := m.Status("192.168.1.20")
	if len(status.Recent) != 1 || status.Recent[0].Port != 8080 || status.Recent[0].At.Year() != 2026 {
		t.Errorf("recent: %+v", status.Recent)
	}
	if len(status.Blocked) != 1 || status.Blocked[0].Source != SourceSSH || status.ClientZone != Home {
		t.Errorf("status: %+v", status)
	}
	m.ClearRecent()
	if len(m.Status("").Recent) != 0 {
		t.Error("recent not cleared")
	}
}

func TestLoadInvalidStaysOff(t *testing.T) {
	m, nft, store := newTestManager(t)
	store.Write(settingsFile, []byte(`{"enabled":true,"rules":[{"action":"maybe"}]}`))
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	if nft.last() != offScript() || m.Status("").Config.Enabled {
		t.Error("invalid settings must leave the firewall off")
	}
}
