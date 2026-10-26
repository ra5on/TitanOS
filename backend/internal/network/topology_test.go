package network

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func exampleTopology() Config {
	return Config{Hostname: "nas", Networks: []LogicalNetwork{
		{ID: "admin", Name: "Verwaltung", Purpose: "management", Parent: "enp1s0", Management: true, Bridge: true, IPv4: "static", Address: "192.168.1.20/24", Gateway: "192.168.1.1", IPv6: "auto", DNS: []string{"192.168.1.1"}},
		{ID: "storage", Name: "Speicher", Purpose: "storage", Parent: "enp2s0", VLAN: 20, Bridge: true, IPv4: "static", Address: "192.168.20.20/24", IPv6: "static", Address6: "fd12:3456:20::20/64"},
		{ID: "apps", Name: "Anwendungen", Purpose: "apps", Parent: "enp2s0", VLAN: 30, Bridge: true, IPv4: "dhcp", IPv6: "auto"},
	}}
}

func TestTopologyVLANsAddressesAndRoutes(t *testing.T) {
	m, _ := newTestManager(t)
	cfg := exampleTopology()
	if err := m.validate(cfg); err != nil {
		t.Fatal(err)
	}
	f := m.render(cfg)
	if !strings.Contains(f[prefix+"enp2s0.network"], "VLAN=tvstorage\nVLAN=tvapps") {
		t.Fatal(f)
	}
	if !strings.Contains(f[prefix+"br0.network"], "Address=192.168.1.20/24") || !strings.Contains(f[prefix+"tnstorage.network"], "Address=fd12:3456:20::20/64") {
		t.Fatal(f)
	}
	if !strings.Contains(f[prefix+"tnapps.network"], "UseRoutes=no") || !strings.Contains(f[prefix+"tnapps.network"], "UseGateway=no") {
		t.Fatal("secondary DHCP replaced preferred routes")
	}
	for name, v := range f {
		if strings.Contains(v, "IPForward=") || strings.Contains(v, "IPMasquerade=") || strings.Contains(v, "DHCPServer=yes") {
			t.Fatalf("router/firewall unexpected in %s", name)
		}
	}
	for _, mutate := range []func(*Config){func(c *Config) { c.Networks[1].Address = "192.168.1.40/24" }, func(c *Config) { c.Networks[1].VLAN = 30 }, func(c *Config) { c.Networks[0].Management = false }, func(c *Config) { c.Networks[1].Address = "192.168.20.0/24" }, func(c *Config) { c.Networks[0].DNS = []string{"1.1.1.1\nIPForward=yes"} }} {
		c := cloneTopology(cfg)
		mutate(&c)
		if m.validate(c) == nil {
			t.Fatal("invalid topology accepted", c)
		}
	}
}

func TestTopologyConfirmationRequiresNewManagementSocket(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := exampleTopology()
	m.trial = time.Second
	if err := m.Change(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	defer m.Revert(context.Background())
	if m.Confirm() == nil {
		t.Fatal("legacy confirm accepted topology")
	}
	if err := m.ConfirmFrom(context.Background(), &net.TCPAddr{IP: net.ParseIP("192.168.9.20"), Port: 443}); err == nil {
		t.Fatal("old connection confirmed change")
	}
	if err := m.ConfirmFrom(context.Background(), &net.TCPAddr{IP: net.ParseIP("192.168.1.20"), Port: 443}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(m.store.Dir(), topologyFile)); err != nil {
		t.Fatal(err)
	}
	if dev, ok := m.Device("storage", true); !ok || dev != "tnstorage" {
		t.Fatal(dev, ok)
	}
	if err := m.Change(context.Background(), Config{Hostname: "nas", DHCP: true}); err == nil {
		t.Fatal("old request silently overwrote topology")
	}
}

func TestBondTopologyAndTrialRestartRollback(t *testing.T) {
	m, _ := newTestManager(t)
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	cfg := exampleTopology()
	cfg.Bonds = []Bond{{ID: "uplink", Ports: []string{"enp1s0", "enp2s0"}, Mode: "802.3ad"}}
	for i := range cfg.Networks {
		cfg.Networks[i].Parent = "bond:uplink"
	}
	cfg.Networks[0].VLAN = 10
	if err := m.validate(cfg); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.render(cfg)[prefix+"tbuplink.netdev"], "Mode=802.3ad") {
		t.Fatal("bond missing")
	}
	m.trial = 20 * time.Millisecond
	if err := m.Change(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for m.PendingSeconds() > 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if m.PendingSeconds() != 0 {
		t.Fatal("trial not reverted")
	}
	for name := range files(t, m) {
		if strings.HasPrefix(name, prefix) {
			t.Fatal("previous automatic configuration not restored", name)
		}
	}
}

func TestDiagnosticsDoesNotReportDownOrAddresslessLinksAsHealthy(t *testing.T) {
	for _, out := range []string{`[]`, `[{"flags":["UP"],"addr_info":[]}]`, `[{"flags":["UP","LOWER_UP"],"addr_info":[{"local":"fe80::1","scope":"link"}]}]`} {
		if addressCheck(out, nil).OK {
			t.Fatal("false healthy link", out)
		}
	}
	if !addressCheck(`[{"flags":["UP","LOWER_UP"],"addr_info":[{"local":"192.168.1.20","scope":"global"}]}]`, nil).OK {
		t.Fatal("usable link rejected")
	}
}

func TestLegacyBridgeMigrationKeepsUplink(t *testing.T) {
	before := Config{Bridge: true, Ports: []string{"enp1s0"}, DHCP: true}
	after := exampleTopology()
	if err := ProtectAttachments(before, after, nil, true); err != nil {
		t.Fatal(err)
	}
	after.Networks[0].VLAN = 10
	if err := ProtectAttachments(before, after, nil, true); err == nil {
		t.Fatal("live br0 moved to another VLAN")
	}
}

type discoveringRunner struct {
	m    *Manager
	base *fakeRunner
}

func (r discoveringRunner) Run(ctx context.Context, stdin, name string, args ...string) (string, error) {
	if name == "networkctl" && len(args) > 0 && args[0] == "reload" {
		os.MkdirAll(filepath.Join(r.m.sys, "class/net/tnfresh"), 0755)
	}
	if name == "networkctl" && len(args) > 0 && args[0] == "reconfigure" && strings.Contains(strings.Join(args, " "), "tnfresh") {
		return "", fmt.Errorf("link not yet known to networkd")
	}
	return r.base.Run(ctx, stdin, name, args...)
}
func TestReloadDoesNotReconfigureNewLinksBeforeNetworkdDiscovery(t *testing.T) {
	m, r := newTestManager(t)
	if err := m.Load(context.Background()); err != nil {
		t.Fatal(err)
	}
	m.runner = discoveringRunner{m, r}
	cfg := exampleTopology()
	cfg.Networks[1].ID = "fresh"
	if err := m.Change(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	defer m.Revert(context.Background())
}
