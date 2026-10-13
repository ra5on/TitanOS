package network

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

type fakeRunner struct {
	commands []string
	fail     string
}

func (r *fakeRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
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
	if len(plain) != 1 || plain["10-titanos-enp1s0.network"] != "[Match]\nName=enp1s0\n\n[Network]\nDHCP=no\nAddress=192.168.1.20/24\nGateway=192.168.1.1\nDNS=192.168.1.1\nDNS=1.1.1.1\n" {
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
	if got := files(t, m); len(got) != 3 || m.PendingSeconds() != 0 {
		t.Errorf("after a failed change: %v", got)
	}
}
