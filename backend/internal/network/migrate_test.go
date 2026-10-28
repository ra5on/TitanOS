package network

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMigrateLegacyConfigurations(t *testing.T) {
	cases := []struct {
		name   string
		legacy Config
		want   LogicalNetwork
		bonds  []Bond
	}{
		{"bridge with DHCP", Config{Hostname: "nas", Ports: []string{"enp1s0"}, Bridge: true, DHCP: true, DNS: []string{}},
			LogicalNetwork{ID: "admin", Name: "LAN", Purpose: "management", Parent: "enp1s0", Bridge: true, Management: true, IPv4: "dhcp", IPv6: "dhcp", DNS: []string{}, Routes: []Route{}}, []Bond{}},
		{"bond, VLAN and fixed address", Config{Hostname: "nas", Ports: []string{"enp1s0", "enp2s0"}, BondMode: "802.3ad", VLAN: 20, Bridge: true, Address: "192.168.20.5/24", Gateway: "192.168.20.1", DNS: []string{"1.1.1.1"}},
			LogicalNetwork{ID: "admin", Name: "LAN", Purpose: "management", Parent: "bond:uplink", VLAN: 20, Bridge: true, Management: true, IPv4: "static", Address: "192.168.20.5/24", Gateway: "192.168.20.1", IPv6: "auto", DNS: []string{"1.1.1.1"}, Routes: []Route{}},
			[]Bond{{ID: "uplink", Ports: []string{"enp1s0", "enp2s0"}, Mode: "802.3ad"}}},
		{"no port: every port on DHCP", Config{Hostname: "nas", DHCP: true, Ports: []string{}, DNS: []string{"9.9.9.9"}},
			LogicalNetwork{ID: "admin", Name: "LAN", Purpose: "management", Parent: "enp1s0", Management: true, IPv4: "dhcp", IPv6: "dhcp", DNS: []string{"9.9.9.9"}, Routes: []Route{}}, []Bond{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := migrateLegacy(c.legacy, "enp1s0")
			if len(got.Networks) != 1 || !reflect.DeepEqual(got.Networks[0], c.want) || !reflect.DeepEqual(got.Bonds, c.bonds) {
				t.Fatalf("got %+v %+v", got.Networks, got.Bonds)
			}
			// The flat fields stay readable for older readers of the status.
			if got.Bridge != c.legacy.Bridge || !reflect.DeepEqual(got.Ports, c.legacy.Ports) || got.Hostname != "nas" {
				t.Fatal("legacy fields lost", got)
			}
			if again := migrateLegacy(got, "enp2s0"); !reflect.DeepEqual(again, got) {
				t.Fatal("a topology must not be migrated again")
			}
		})
	}
}

func TestUplinkPrefersTheDefaultRoute(t *testing.T) {
	ports := []Interface{{Name: "enp1s0"}, {Name: "enp2s0", Carrier: true}, {Name: "enp3s0", Carrier: true}}
	if got := uplink(ports, "enp3s0"); got != "enp3s0" {
		t.Fatal(got)
	}
	if got := uplink(ports, "br0"); got != "enp2s0" {
		t.Fatal(got)
	}
	if got := uplink(ports[:1], ""); got != "enp1s0" {
		t.Fatal(got)
	}
	if got := uplink(nil, ""); got != "" {
		t.Fatal(got)
	}
}

// The migrated form must keep what workloads and the router see: br0 with
// the MAC of the first port, the same address, and legacy VMs on br0 must
// accept it.
func TestMigratedTopologyKeepsBridgeAndAddress(t *testing.T) {
	m, _ := newTestManager(t)
	legacy := normal(Config{Hostname: "nas", Ports: []string{"enp1s0"}, Bridge: true, Address: "192.168.1.20/24", Gateway: "192.168.1.1", DNS: []string{"192.168.1.1"}})
	migrated := normal(migrateLegacy(legacy, ""))
	if err := m.validate(migrated); err != nil {
		t.Fatal(err)
	}
	old, next := m.render(legacy), m.render(migrated)
	if old[prefix+"br0.netdev"] != next[prefix+"br0.netdev"] || !strings.Contains(next[prefix+"br0.netdev"], "MACAddress=52:54:00:aa:bb:01") {
		t.Fatalf("bridge differs:\n%s\n%s", old[prefix+"br0.netdev"], next[prefix+"br0.netdev"])
	}
	for _, line := range []string{"Address=192.168.1.20/24", "Gateway=192.168.1.1", "DNS=192.168.1.1", "Bridge=br0"} {
		if !strings.Contains(next[prefix+"br0.network"]+next[prefix+"enp1s0.network"], line) {
			t.Fatalf("%s missing: %v", line, next)
		}
	}
	if err := ProtectAttachments(legacy, migrated, map[string]bool{}, true); err != nil {
		t.Fatal("legacy VM on br0 blocks the migration:", err)
	}
	if device(legacy) != networkDevice(migrated.Networks[0]) {
		t.Fatal("management device renamed")
	}
	// A bond keeps its ports and mode for legacy VMs as well.
	bonded := normal(Config{Hostname: "nas", Ports: []string{"enp1s0", "enp2s0"}, BondMode: "active-backup", Bridge: true, DHCP: true})
	if err := ProtectAttachments(bonded, normal(migrateLegacy(bonded, "")), map[string]bool{}, true); err != nil {
		t.Fatal(err)
	}
	if len(migrated.Ports) != 0 || migrated.Bridge || migrated.Address != "" {
		t.Fatal("legacy fields are written with a topology", migrated)
	}
}

func TestLegacyStatusMigratesWithoutTouchingTheSystem(t *testing.T) {
	m, runner := newTestManager(t)
	ctx := context.Background()
	legacy := Config{Hostname: "nas", Ports: []string{"enp1s0"}, Bridge: true, DHCP: true, DNS: []string{}}
	if err := m.store.WriteJSON(configFile, legacy); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	before := files(t, m)
	runner.commands = nil
	status := m.Status(ctx)
	if !status.Migrated || len(status.Topology.Networks) != 1 || status.Topology.Networks[0].Parent != "enp1s0" || !status.Config.Bridge {
		t.Fatalf("%+v", status)
	}
	if live := status.Live["admin"]; live.Device != "br0" {
		t.Fatalf("live device %+v", status.Live)
	}
	for _, command := range runner.commands {
		if strings.HasPrefix(command, "networkctl") || strings.HasPrefix(command, "hostnamectl") {
			t.Fatal("reading the status changed the network:", command)
		}
	}
	if !reflect.DeepEqual(before, files(t, m)) {
		t.Fatal("networkd files changed")
	}
	if _, err := os.Stat(filepath.Join(m.store.Dir(), topologyFile)); !os.IsNotExist(err) {
		t.Fatal("migration was stored before an apply")
	}
	// Only the derived view is a topology; workloads still see legacy.
	if m.Connections() != nil && len(m.Connections()) != 0 {
		t.Fatal("legacy configuration exposed as topology to workloads")
	}
	if _, ok := m.Device("admin", true); ok {
		t.Fatal("derived network handed to workloads before apply")
	}
	checks, err := m.Diagnostics(ctx, "admin")
	if err != nil || len(checks) == 0 {
		t.Fatal(checks, err)
	}
	if !strings.Contains(strings.Join(runner.commands, "\n"), "ip -json address show dev br0") {
		t.Fatal("diagnostics did not look at br0", runner.commands)
	}
}

func TestApplyingTheMigrationIsOnTrialAndRevertible(t *testing.T) {
	m, _ := newTestManager(t)
	ctx := context.Background()
	m.trial = time.Minute
	legacy := Config{Hostname: "nas", Ports: []string{"enp1s0"}, Bridge: true, Address: "192.168.1.20/24", Gateway: "192.168.1.1", DNS: []string{}}
	if err := m.store.WriteJSON(configFile, legacy); err != nil {
		t.Fatal(err)
	}
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	legacyFiles := files(t, m)

	// What the page sends back is the status' topology, with networks[];
	// config itself stays in the stored flat form for older clients.
	status := m.Status(ctx)
	if len(status.Config.Networks) != 0 {
		t.Fatalf("config rewritten: %+v", status.Config)
	}
	draft := status.Topology
	if err := m.Change(ctx, draft); err != nil {
		t.Fatal(err)
	}
	if err := m.Revert(ctx); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(legacyFiles, files(t, m)) {
		t.Fatal("revert did not restore the legacy files")
	}

	if err := m.Change(ctx, draft); err != nil {
		t.Fatal(err)
	}
	if err := m.ConfirmFrom(ctx, &net.TCPAddr{IP: net.ParseIP("192.168.1.20"), Port: 443}); err != nil {
		t.Fatal(err)
	}
	var stored, good, old map[string]any
	read := func(name string, into *map[string]any) {
		data, err := os.ReadFile(filepath.Join(m.store.Dir(), name))
		if err != nil {
			t.Fatal(err)
		}
		json.Unmarshal(data, into)
	}
	read(topologyFile, &stored)
	read(lastGoodFile, &good)
	read(configFile, &old)
	if ports, _ := stored["ports"].([]any); len(ports) != 0 || stored["bridge"] == true {
		t.Fatal("legacy fields written to topology.json", stored)
	}
	if good["bridge"] != true || old["bridge"] != true || old["networks"] != nil {
		t.Fatal("older slots lost their legacy configuration", good, old)
	}
	if dev, ok := m.Device("admin", true); !ok || dev != "br0" {
		t.Fatal(dev, ok)
	}
	if status := m.Status(ctx); status.Migrated {
		t.Fatal("confirmed topology still marked as migrated")
	}
	// The bridge counts as there for the VM page's "set up home network".
	if err := m.EnsureBridge(ctx, "", false); err != nil {
		t.Fatal(err)
	}
}
