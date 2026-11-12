package firewall

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// The zones of one app, as "public through IPv64" sets them.
func TestAppZonesOfOneApp(t *testing.T) {
	m, nft, store := newTestManager(t)
	ctx := context.Background()
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	// Off: nothing changes, nothing is stored.
	if ok, err := m.SetAppZones(ctx, "immich", Zones{Home: true, VPN: true, Internet: true}); ok || err != nil {
		t.Fatalf("firewall off: %v %v", ok, err)
	}
	cfg := Defaults()
	cfg.Enabled = true
	if err := m.Change(ctx, cfg, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetAppZones(ctx, "immich", Zones{Internet: true}); !errors.Is(err, ErrPending) {
		t.Fatalf("on trial: %v", err)
	}
	if err := m.Confirm(ctx); err != nil {
		t.Fatal(err)
	}
	// Nothing of the removed IPv64 account area is in the rules.
	if strings.Contains(nft.last(), "blocklist") || strings.Contains(nft.last(), "ipv64wg") {
		t.Fatal("v64 blocklist or the tunnel of the Cloud Router still in the rules", nft.last())
	}
	if ok, err := m.SetAppZones(ctx, "immich", Zones{Home: true, VPN: true, Internet: true}); !ok || err != nil {
		t.Fatalf("on: %v %v", ok, err)
	}
	if !m.AppZones("immich").Internet {
		t.Fatal("zones not kept")
	}
	var stored fileConfig
	store.ReadJSON(settingsFile, &stored)
	if stored.V2 == nil || !appAllowed(fromFile(stored).Cell(Internet, ToApps), "immich") {
		t.Fatal("zones not stored")
	}
	if !stored.Apps["immich"].Internet {
		t.Fatal("the view for older titand lacks the app")
	}
	if _, err := m.SetAppZones(ctx, "bad key!", Zones{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad key accepted")
	}
}

// Zones whose rules nft refuses are neither kept nor stored on the stick.
func TestAppZonesNotStoredWhenRulesFail(t *testing.T) {
	m, nft, store := newTestManager(t)
	ctx := context.Background()
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	cfg := Defaults()
	cfg.Enabled = true
	if err := m.Change(ctx, cfg, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	if err := m.Confirm(ctx); err != nil {
		t.Fatal(err)
	}
	nft.mu.Lock()
	nft.fail = true
	nft.mu.Unlock()
	if _, err := m.SetAppZones(ctx, "immich", Zones{Internet: true}); err == nil {
		t.Fatal("failing rules reported as applied")
	}
	if m.AppZones("immich").Internet {
		t.Fatal("zones kept although their rules failed")
	}
	var stored fileConfig
	if err := store.ReadJSON(settingsFile, &stored); err != nil {
		t.Fatal(err)
	}
	if appAllowed(fromFile(stored).Cell(Internet, ToApps), "immich") {
		t.Fatal("zones stored on the stick although their rules failed")
	}
}
