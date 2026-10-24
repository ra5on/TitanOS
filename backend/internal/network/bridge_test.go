package network

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"
	"time"
)

func TestEnsureBridgeDefaultUplinkAndPersistence(t *testing.T) {
	m, r := newTestManager(t)
	ctx := context.Background()
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if m.configured {
		t.Fatal("new network marked configured")
	}
	r.routeOut = "default via 192.168.1.1 dev enp1s0 proto dhcp\n"
	if err := m.EnsureBridge(ctx, "", true); err != nil {
		t.Fatal(err)
	}
	if m.BridgeReady() {
		t.Fatal("pending bridge ready for VMs")
	}
	if m.pending == nil || !m.pending.Bridge || !reflect.DeepEqual(m.pending.Ports, []string{"enp1s0"}) {
		t.Fatalf("pending: %+v", m.pending)
	}
	if err := m.EnsureBridge(ctx, "", false); !errors.Is(err, ErrPending) {
		t.Fatalf("overwrote pending: %v", err)
	}
	if err := m.Confirm(); err != nil {
		t.Fatal(err)
	}
	if !m.BridgeReady() {
		t.Fatal("confirmed bridge not ready")
	}
	commands := len(r.commands)
	if err := m.EnsureBridge(ctx, "", false); err != nil {
		t.Fatal(err)
	}
	if len(r.commands) != commands {
		t.Fatal("existing bridge reconfigured")
	}
	again := NewManager(m.store)
	again.sys, again.dir, again.runner = m.sys, m.dir, r
	if err := again.Load(ctx); err != nil {
		t.Fatal(err)
	}
	if !again.active.Bridge || !again.configured {
		t.Fatalf("lost confirmed network: %+v", again.active)
	}
	if slices.Contains(r.commands[commands:], "networkctl reload") {
		t.Fatal("service restart disrupted bridge")
	}
}

func TestEnsureBridgePreservesTopology(t *testing.T) {
	for _, dhcp := range []bool{true, false} {
		t.Run(map[bool]string{true: "DHCP", false: "static"}[dhcp], func(t *testing.T) {
			m, _ := newTestManager(t)
			ctx := context.Background()
			cfg := Config{Hostname: "nas", Ports: []string{"enp1s0", "enp2s0"}, BondMode: "active-backup", VLAN: 20, DHCP: dhcp, DNS: []string{"9.9.9.9"}}
			if !dhcp {
				cfg.Address = "192.168.1.20/24"
				cfg.Gateway = "192.168.1.1"
			}
			if err := m.store.WriteJSON(configFile, cfg); err != nil {
				t.Fatal(err)
			}
			if err := m.Load(ctx); err != nil {
				t.Fatal(err)
			}
			if err := m.EnsureBridge(ctx, "", true); err != nil || m.pending != nil {
				t.Fatalf("overrode explicit settings: %v", err)
			}
			if err := m.EnsureBridge(ctx, "", false); err != nil {
				t.Fatal(err)
			}
			expected := cfg
			expected.Bridge = true
			if !reflect.DeepEqual(*m.pending, expected) {
				t.Fatalf("changed topology: %+v", m.pending)
			}
			if err := m.Revert(ctx); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(m.active, cfg) {
				t.Fatal("rollback lost original settings")
			}
		})
	}
}

func TestEnsureBridgeAmbiguousAndInvalidPorts(t *testing.T) {
	for _, route := range []string{"", "default dev bond0", "default dev bond0\ndefault dev enp1s0", "default dev enp1s0\ndefault dev enp2s0"} {
		m, r := newTestManager(t)
		ctx := context.Background()
		m.Load(ctx)
		r.routeOut = route
		if err := m.EnsureBridge(ctx, "", false); !errors.Is(err, ErrBridgePort) {
			t.Fatalf("route %q: %v", route, err)
		}
		if len(files(t, m)) != 0 {
			t.Fatal("changed network without a port")
		}
		if err := m.EnsureBridge(ctx, "br0", false); !errors.Is(err, ErrBridgePort) {
			t.Fatalf("virtual port allowed: %v", err)
		}
		if err := m.EnsureBridge(ctx, "enp1s0", false); err != nil {
			t.Fatal(err)
		}
		m.Revert(ctx)
	}
}

func TestEnsureBridgeDisabledFailureAndTimeout(t *testing.T) {
	ctx := context.Background()
	m, r := newTestManager(t)
	if err := m.EnsureBridge(ctx, "enp1s0", false); !errors.Is(err, ErrNotLoaded) {
		t.Fatal(err)
	}
	m.Load(ctx)
	m.Disabled = true
	if err := m.EnsureBridge(ctx, "enp1s0", false); !errors.Is(err, ErrDisabled) {
		t.Fatal(err)
	}
	if len(r.commands) != 0 {
		t.Fatal("dev network changed")
	}
	m.Disabled = false
	r.fail = "titanos-firewall-open"
	if err := m.EnsureBridge(ctx, "enp1s0", false); err == nil {
		t.Fatal("apply failure ignored")
	}
	r.fail = ""
	if m.pending != nil {
		m.Revert(ctx)
	}
	m.trial = 25 * time.Millisecond
	if err := m.EnsureBridge(ctx, "enp1s0", false); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for m.PendingSeconds() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if m.PendingSeconds() != 0 || m.active.Bridge {
		t.Fatal("unconfirmed bridge survived")
	}
}
