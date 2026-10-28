package firewall

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestAppZonesAndBlocklistForIPv64(t *testing.T) {
	m, nft, store := newTestManager(t)
	ctx := context.Background()
	if err := m.Load(ctx); err != nil {
		t.Fatal(err)
	}
	// Off: nothing changes, nothing is stored.
	if ok, err := m.SetAppZones(ctx, "immich", Zones{Home: true, VPN: true, Internet: true}); ok || err != nil {
		t.Fatalf("firewall off: %v %v", ok, err)
	}
	if err := m.SetBlocklist(ctx, []netip.Prefix{netip.MustParsePrefix("203.0.113.9/32")}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(nft.last(), "203.0.113.9") {
		t.Fatal("blocklist loaded while the firewall is off")
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
	if !strings.Contains(nft.last(), "elements = { 203.0.113.9 }") || !strings.Contains(nft.last(), `"ipv64wg"`) {
		t.Fatal("blocklist or WireGuard interface missing", nft.last())
	}
	if ok, err := m.SetAppZones(ctx, "immich", Zones{Home: true, VPN: true, Internet: true}); !ok || err != nil {
		t.Fatalf("on: %v %v", ok, err)
	}
	if !m.AppZones("immich").Internet {
		t.Fatal("zones not kept")
	}
	var stored Config
	store.ReadJSON(settingsFile, &stored)
	if !stored.Apps["immich"].Internet {
		t.Fatal("zones not stored")
	}
	if _, err := m.SetAppZones(ctx, "bad key!", Zones{}); !errors.Is(err, ErrInvalid) {
		t.Fatal("bad key accepted")
	}
	if m.Status("192.168.1.20").Blocklist != 1 {
		t.Fatal("blocklist count")
	}
}
