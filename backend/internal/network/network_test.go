package network

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type fakeRunner struct {
	mu       sync.Mutex
	commands []string
	fail     string
}

func (r *fakeRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	line := name + " " + strings.Join(args, " ")
	r.commands = append(r.commands, line)
	if r.fail != "" && strings.Contains(line, r.fail) {
		return "", errors.New("failed: " + line)
	}
	return "", nil
}

func newTestManager(t *testing.T) (*Manager, *fakeRunner) {
	t.Helper()
	root := t.TempDir()
	m := NewManager(config.Open(filepath.Join(root, "config")))
	m.dir, m.sys = filepath.Join(root, "network"), filepath.Join(root, "sys")
	runner := &fakeRunner{}
	m.runner = runner
	for name, files := range map[string]map[string]string{
		"enp1s0": {"address": "52:54:00:aa:bb:01", "type": "1", "carrier": "1", "speed": "2500", "device/x": ""},
		"enp2s0": {"address": "52:54:00:aa:bb:02", "type": "1", "carrier": "0", "speed": "-1", "device/x": ""},
		"lo":     {"address": "00:00:00:00:00:00", "type": "772"},
		"br0":    {"address": "52:54:00:aa:bb:01", "type": "1"},
		"veth12": {"address": "aa:aa:aa:aa:aa:aa", "type": "1"},
	} {
		for file, content := range files {
			path := filepath.Join(m.sys, "class/net", name, file)
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, []byte(content+"\n"), 0o644)
		}
	}
	return m, runner
}

func files(t *testing.T, m *Manager) map[string]string {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	found := map[string]string{}
	entries, _ := os.ReadDir(m.dir)
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		found[entry.Name()] = string(data)
	}
	return found
}

func TestInterfaces(t *testing.T) {
	m, _ := newTestManager(t)
	ports := m.Interfaces()
	if len(ports) != 2 || ports[0].Name != "enp1s0" || !ports[0].Carrier || ports[0].SpeedMbps != 2500 || ports[1].Carrier || ports[1].SpeedMbps != 0 {
		t.Errorf("ports = %+v", ports)
	}
}

func TestValidate(t *testing.T) {
	m, _ := newTestManager(t)
	good := Config{Hostname: "nas", Ports: []string{"enp1s0"}, Address: "192.168.1.20/24", Gateway: "192.168.1.1", DNS: []string{"192.168.1.1"}}
	if err := m.validate(good); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*Config){
		"hostname with a dot":      func(c *Config) { c.Hostname = "nas.local" },
		"hostname in capitals":     func(c *Config) { c.Hostname = "NAS" },
		"unknown port":             func(c *Config) { c.Ports = []string{"eth7"} },
		"a bridge as a port":       func(c *Config) { c.Ports = []string{"br0"} },
		"the same port twice":      func(c *Config) { c.Ports = []string{"enp1s0", "enp1s0"}; c.BondMode = "active-backup" },
		"two ports without a mode": func(c *Config) { c.Ports = []string{"enp1s0", "enp2s0"} },
		"VLAN out of range":        func(c *Config) { c.VLAN = 5000 },
		"fixed address, no port":   func(c *Config) { c.Ports = nil },
		"address without a length": func(c *Config) { c.Address = "192.168.1.20" },
		"network address":          func(c *Config) { c.Address = "192.168.1.0/24" },
		"broadcast address":        func(c *Config) { c.Address = "192.168.1.255/24" },
		"IPv6 address":             func(c *Config) { c.Address = "fd00::2/64" },
		"gateway elsewhere":        func(c *Config) { c.Gateway = "10.0.0.1" },
		"gateway is the NAS":       func(c *Config) { c.Gateway = "192.168.1.20" },
		"name server is a name":    func(c *Config) { c.DNS = []string{"dns.example.org"} },
		"four name servers":        func(c *Config) { c.DNS = []string{"1.1.1.1", "8.8.8.8", "9.9.9.9", "1.0.0.1"} },
	} {
		bad := good
		bad.Ports, bad.DNS = slices.Clone(good.Ports), slices.Clone(good.DNS)
		change(&bad)
		if err := m.validate(bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if err := m.validate(Config{Hostname: "nas", DHCP: true}); err != nil {
		t.Errorf("DHCP on every port: %v", err)
	}
	if err := m.validate(Config{Hostname: "nas", DHCP: true, Bridge: true}); !errors.Is(err, ErrInvalid) {
		t.Errorf("a bridge without a port: %v", err)
	}
}

func TestRender(t *testing.T) {
	m, _ := newTestManager(t)
	if got := m.render(Config{Hostname: "nas", DHCP: true}); len(got) != 0 {
		t.Errorf("without ports the image's own file applies: %v", got)
	}

	plain := m.render(Config{Ports: []string{"enp1s0"}, Address: "192.168.1.20/24", Gateway: "192.168.1.1", DNS: []string{"192.168.1.1", "1.1.1.1"}})
	if len(plain) != 1 || plain["10-titanos-enp1s0.network"] != "[Match]\nName=enp1s0\n\n[Network]\nIPv6AcceptRA=yes\nKeepConfiguration=no\nDHCP=no\nAddress=192.168.1.20/24\nGateway=192.168.1.1\nDNS=192.168.1.1\nDNS=1.1.1.1\n" {
		t.Errorf("fixed address on one port: %v", plain)
	}

	// Everything at once: two ports as a bond, a VLAN on it, the bridge on top.
	all := m.render(Config{Ports: []string{"enp1s0", "enp2s0"}, BondMode: "802.3ad", VLAN: 20, Bridge: true, DHCP: true, DNS: []string{"9.9.9.9"}})
	want := map[string][]string{
		"10-titanos-bond0.netdev":   {"Kind=bond", "Mode=802.3ad"},
		"10-titanos-enp1s0.network": {"Name=enp1s0", "Bond=bond0", "DHCP=no"},
		"10-titanos-enp2s0.network": {"Name=enp2s0", "Bond=bond0"},
		"10-titanos-vlan20.netdev":  {"Kind=vlan", "Id=20"},
		"10-titanos-bond0.network":  {"Name=bond0", "VLAN=vlan20", "LinkLocalAddressing=no"},
		"10-titanos-br0.netdev":     {"Kind=bridge", "MACAddress=52:54:00:aa:bb:01"},
		"10-titanos-vlan20.network": {"Name=vlan20", "Bridge=br0"},
		"10-titanos-br0.network":    {"Name=br0", "DHCP=yes", "DNS=9.9.9.9", "ClientIdentifier=mac", "UseDNS=no"},
	}
	if len(all) != len(want) {
		t.Errorf("files = %v", all)
	}
	for name, parts := range want {
		for _, part := range parts {
			if !strings.Contains(all[name], part) {
				t.Errorf("%s lacks %q:\n%s", name, part, all[name])
			}
		}
	}
}

func TestFirewallBeforeNetworkChange(t *testing.T) {
	m, runner := newTestManager(t)
	ctx := context.Background()
	cfg := Config{Hostname: "nas", Ports: []string{"enp1s0"}, DHCP: true}
	if err := m.apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	before := files(t, m)
	runner.commands = nil
	runner.fail = "titanos-firewall-open"
	cfg.Bridge = true
	if err := m.apply(ctx, cfg); err == nil {
		t.Fatal("bridge must not be configured when forwarding setup fails")
	}
	if got := files(t, m); len(got) != len(before) || got["10-titanos-enp1s0.network"] != before["10-titanos-enp1s0.network"] {
		t.Fatalf("failed preparation changed networkd files: %v", got)
	}
	if len(runner.commands) != 1 || !strings.Contains(runner.commands[0], "titanos-firewall-open") {
		t.Fatalf("networkd touched despite failed preparation: %v", runner.commands)
	}
	runner.commands, runner.fail = nil, ""
	if err := m.apply(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(runner.commands[0], "titanos-firewall-open") || !strings.Contains(files(t, m)["10-titanos-enp1s0.network"], "Bridge=br0") {
		t.Fatalf("forwarding must be ready before br0 is created: %v", runner.commands)
	}
}

func TestChangeOnTrial(t *testing.T) {
	m, runner := newTestManager(t)
	ctx := context.Background()
	m.trial = 150 * time.Millisecond
	bridge := Config{Hostname: "nas", Ports: []string{"enp1s0"}, Bridge: true, DHCP: true}

	if err := m.Change(ctx, bridge); !errors.Is(err, ErrNotLoaded) {
		t.Fatalf("before Load: %v", err)
	}
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 0 {
		t.Errorf("without a configuration nothing is touched: %v", runner.commands)
	}
	if err := m.Confirm(); !errors.Is(err, ErrNoPending) {
		t.Errorf("confirm without a change: %v", err)
	}

	// Not confirmed: the previous state comes back by itself.
	if err := m.Change(ctx, bridge); err != nil {
		t.Fatal(err)
	}
	if err := m.Change(ctx, bridge); !errors.Is(err, ErrPending) {
		t.Errorf("a second change while one is on trial: %v", err)
	}
	if status := m.Status(ctx); status.Pending == nil || !status.Pending.Bridge || status.PendingSeconds < 1 || status.Config.Bridge {
		t.Errorf("on trial: %+v", status)
	}
	if got := files(t, m); len(got) != 3 || !strings.Contains(got["10-titanos-enp1s0.network"], "Bridge=br0") {
		t.Fatalf("files on trial: %v", got)
	}
	time.Sleep(400 * time.Millisecond)
	if got := files(t, m); len(got) != 0 {
		t.Errorf("files after the trial ran out: %v", got)
	}
	if !slices.Contains(runner.commands, "networkctl delete br0") {
		t.Errorf("the bridge was not taken away: %v", runner.commands)
	}
	if m.PendingSeconds() != 0 || m.store.ReadJSON(configFile, &Config{}) == nil {
		t.Error("an unconfirmed change was kept")
	}

	// Confirmed: it stays, also over a restart.
	if err := m.Change(ctx, bridge); err != nil {
		t.Fatal(err)
	}
	if err := m.Confirm(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if got := files(t, m); len(got) != 3 {
		t.Errorf("a confirmed change was taken back: %v", got)
	}
	again := NewManager(m.store)
	again.dir, again.sys, again.runner = filepath.Join(t.TempDir(), "network"), m.sys, runner
	if err := again.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if got := files(t, again); len(got) != 3 || !again.Status(ctx).Config.Bridge {
		t.Errorf("after a restart: %v", got)
	}

	// A new name applies at once and is kept, with everything else as it was.
	if err := m.SetHostname(ctx, "Kein Name"); !errors.Is(err, ErrInvalid) {
		t.Errorf("odd hostname: %v", err)
	}
	if err := m.SetHostname(ctx, "keller"); err != nil {
		t.Fatal(err)
	}
	var stored Config
	if err := m.store.ReadJSON(configFile, &stored); err != nil || stored.Hostname != "keller" || !stored.Bridge || m.PendingSeconds() != 0 {
		t.Errorf("after renaming: %+v, %v", stored, err)
	}
	if !slices.Contains(runner.commands, "hostnamectl hostname keller") {
		t.Errorf("the name was not set: %v", runner.commands)
	}

	// Given up by hand; and a change that cannot be applied leaves things as they were.
	fixed := Config{Hostname: "nas", Ports: []string{"enp1s0"}, Address: "192.168.1.20/24", Gateway: "192.168.1.1"}
	if err := m.Change(ctx, fixed); err != nil {
		t.Fatal(err)
	}
	if err := m.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if got := files(t, m); len(got) != 3 || m.PendingSeconds() != 0 {
		t.Errorf("after reverting: %v", got)
	}
	runner.fail = "networkctl reload"
	if err := m.Change(ctx, fixed); err == nil {
		t.Error("a change that could not be applied was accepted")
	}
	runner.fail = ""
	if got := files(t, m); len(got) != 3 || m.PendingSeconds() == 0 {
		t.Errorf("after a failed change: %v", got)
	}
	if err := m.Confirm(); !errors.Is(err, ErrReverting) {
		t.Errorf("a failed rollback must not be confirmable: %v", err)
	}
	if err := m.Revert(ctx); err != nil || m.PendingSeconds() != 0 {
		t.Errorf("retry rollback: %v", err)
	}
}

func TestAddressOwnership(t *testing.T) {
	m, _ := newTestManager(t)
	for _, dhcp := range []bool{true, false} {
		cfg := Config{Ports: []string{"enp1s0"}, Bridge: true, DHCP: dhcp, Address: "192.168.10.5/24", Gateway: "192.168.10.1"}
		got := m.render(cfg)
		port, bridge := got[prefix+"enp1s0.network"], got[prefix+"br0.network"]
		for _, setting := range []string{"DHCP=no", "IPv6AcceptRA=no", "LinkLocalAddressing=no", "KeepConfiguration=no", "Bridge=br0"} {
			if !strings.Contains(port, setting) {
				t.Errorf("bridge port lacks %s: %s", setting, port)
			}
		}
		if strings.Contains(port, "\nAddress=") || strings.Contains(port, "\nGateway=") || !strings.Contains(bridge, "IPv6AcceptRA=yes") {
			t.Fatalf("addresses/routes must belong to br0: %v", got)
		}
		if dhcp && !strings.Contains(bridge, "ClientIdentifier=mac") || !dhcp && !strings.Contains(bridge, "Address=192.168.10.5/24") {
			t.Errorf("bridge address configuration: %s", bridge)
		}
	}
}

func TestReconfigureFailureAndRetiredBridge(t *testing.T) {
	m, runner := newTestManager(t)
	ctx := context.Background()
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	bridge := Config{Hostname: "nas", Ports: []string{"enp1s0"}, Bridge: true, DHCP: true}
	if err := m.Change(ctx, bridge); err != nil {
		t.Fatal(err)
	}
	if err := m.Confirm(); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(runner.commands, "networkctl reconfigure br0 enp1s0 enp2s0") {
		t.Fatalf("existing br0 was not reconfigured: %v", runner.commands)
	}
	runner.fail = "networkctl reconfigure"
	fixed := bridge
	fixed.DHCP, fixed.Address = false, "192.168.10.5/24"
	if err := m.Change(ctx, fixed); err == nil {
		t.Fatal("reconfigure failure was ignored")
	}
	runner.fail = ""
	if err := m.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Change(ctx, Config{Hostname: "nas", DHCP: true}); err != nil {
		t.Fatal(err)
	}
	if err := m.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	// The old definition must be forgotten before the bridge is removed.
	runner.commands = nil
	if err := m.Change(ctx, Config{Hostname: "nas", DHCP: true}); err != nil {
		t.Fatal(err)
	}
	reload, remove := slices.Index(runner.commands, "networkctl reload"), slices.Index(runner.commands, "networkctl delete br0")
	if reload < 0 || remove <= reload {
		t.Fatalf("delete before reload: %v", runner.commands)
	}
	if err := m.Revert(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestChangeSurvivesLostRequest(t *testing.T) {
	m, runner := newTestManager(t)
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := m.Change(ctx, Config{Hostname: "nas", Ports: []string{"enp1s0"}, Bridge: true, DHCP: true}); err != nil {
		t.Fatal(err)
	}
	if m.PendingSeconds() == 0 {
		t.Fatal("missing rollback timer")
	}
	if err := m.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) == 0 {
		t.Fatal("configuration not applied")
	}
}

func TestRollbackRetriesRetiredBridge(t *testing.T) {
	m, runner := newTestManager(t)
	ctx := context.Background()
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if err := m.Change(ctx, Config{Hostname: "nas", Ports: []string{"enp1s0"}, Bridge: true, DHCP: true}); err != nil {
		t.Fatal(err)
	}
	runner.fail = "networkctl delete br0"
	if err := m.Revert(ctx); err == nil {
		t.Fatal("bridge deletion failure was ignored")
	}
	if err := m.Confirm(); !errors.Is(err, ErrReverting) {
		t.Fatalf("failed rollback confirmable: %v", err)
	}
	if len(files(t, m)) != 0 {
		t.Fatal("networkd still has the obsolete definition")
	}
	runner.commands, runner.fail = nil, ""
	if err := m.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(runner.commands, "networkctl delete br0") || m.PendingSeconds() != 0 {
		t.Fatalf("retired bridge was forgotten on retry: %v", runner.commands)
	}
}

func TestRestartOfTitandLeavesTheNetworkAlone(t *testing.T) {
	m, runner := newTestManager(t)
	ctx := context.Background()
	cfg := Config{Hostname: "nas", Ports: []string{"enp1s0"}, Bridge: true, DHCP: true}
	if err := m.store.WriteJSON(configFile, cfg); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(runner.commands, "networkctl reload") {
		t.Fatalf("the first start did not put the configuration in place: %v", runner.commands)
	}

	// titand starts again, as after an app update: the files are there.
	again := NewManager(m.store)
	again.dir, again.sys, again.runner = m.dir, m.sys, runner
	runner.commands = nil
	if err := again.Load(ctx); err != nil {
		t.Fatal(err)
	}
	for _, command := range runner.commands {
		if strings.HasPrefix(command, "networkctl") {
			t.Errorf("the network was reconfigured although nothing changed: %v", runner.commands)
		}
	}
}
