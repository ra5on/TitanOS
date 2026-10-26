package tailscale

import (
	"os"
	"strings"
	"testing"
	"time"
)

// testdata/status.json is shaped like `tailscale status --json` of 1.104.
func TestParseDevicesFromRecordedStatus(t *testing.T) {
	raw, err := os.ReadFile("testdata/status.json")
	if err != nil {
		t.Fatal(err)
	}
	got, ok := parseDevices(raw)
	if !ok || !got.Active || got.State != "Running" || len(got.Devices) != 4 {
		t.Fatalf("parse %+v %v", got, ok)
	}
	self, laptop, phone, router := got.Devices[0], got.Devices[1], got.Devices[2], got.Devices[3]
	if !self.Self || self.ID != "nSELF1CNTRL" || self.Name != "titan" || self.LastSeen != nil || len(self.Routes) != 1 || self.Routes[0] != "192.168.10.0/24" {
		t.Fatalf("self %+v", self)
	}
	if laptop.ID != "nLAPTOP2CNTRL" || laptop.Name != "alex-laptop" || laptop.HostName != "Alex-Laptop" || laptop.DNSName != "alex-laptop.example-tailnet.ts.net" || laptop.OS != "macOS" || laptop.Online == nil || !*laptop.Online {
		t.Fatalf("laptop %+v", laptop)
	}
	if len(laptop.IPv4) != 1 || laptop.IPv4[0] != "100.101.1.20" || len(laptop.IPv6) != 1 || laptop.IPv6[0] != "fd7a:115c:a1e0::a01:114" {
		t.Fatalf("addresses %+v", laptop)
	}
	if laptop.LastSeen == nil || !laptop.LastSeen.Equal(time.Date(2026, 10, 26, 9, 58, 0, 0, time.UTC)) {
		t.Fatalf("last seen %v", laptop.LastSeen)
	}
	if !router.ExitNode || !router.ExitNodeOption || *router.Online || len(router.IPv6) != 0 || len(router.Routes) != 1 || router.Routes[0] != "10.0.0.0/16" {
		t.Fatalf("router %+v", router)
	}
	// Without a stable ID the public key identifies the node, never the IP;
	// unknown values stay unknown.
	if !strings.HasPrefix(phone.ID, "nodekey:") || phone.Name != "phone" || phone.OS != "" || phone.LastSeen != nil {
		t.Fatalf("phone %+v", phone)
	}
}

func TestDevicesWithoutRunningBackendHaveUnknownOnlineState(t *testing.T) {
	got, ok := parseDevices([]byte(`{"BackendState":"Stopped","Peer":{"k":{"ID":"n1","DNSName":"a.ts.net.","Online":true}}}`))
	if !ok || len(got.Devices) != 1 || got.Devices[0].Online != nil {
		t.Fatalf("stale online flag %+v", got)
	}
	if _, ok := parseDevices([]byte(`<html>`)); ok {
		t.Fatal("invalid status accepted")
	}
	got, _ = parseDevices([]byte(`{"BackendState":"Running","Peer":{"a":{"ID":"bad id\n"},"b":{"ID":"n2"},"c":{"ID":"n2"}}}`))
	if len(got.Devices) != 1 {
		t.Fatalf("unsafe or duplicate IDs kept: %+v", got.Devices)
	}
}

func TestDevicesInactiveAndCached(t *testing.T) {
	m, f := fixture(t)
	f.fail = true
	if d := m.Devices(); d.Active || d.State != "inactive" || d.Devices == nil {
		t.Fatalf("inactive %+v", d)
	}
	f.fail = false
	f.status = `{"BackendState":"Running","Self":{"ID":"n1","DNSName":"nas.ts.net.","Online":true}}`
	calls := len(f.calls)
	if d := m.Devices(); d.Active {
		t.Fatal("cache bypassed")
	}
	m.devices.at = time.Now().Add(-devicesTTL)
	if d := m.Devices(); !d.Active || len(d.Devices) != 1 || !d.Devices[0].Self {
		t.Fatalf("refreshed %+v", d)
	}
	m.Devices()
	if len(f.calls) != calls+1 || f.calls[calls] != "tailscale status --json" {
		t.Fatalf("calls %v", f.calls[calls:])
	}
	if m.store.Changes() != 0 {
		t.Fatal("listing devices wrote config")
	}
	f.fail = true
	m.devices.at = time.Time{}
	m.Disabled = true
	if d := m.Devices(); d.Active || len(f.calls) != calls+1 {
		t.Fatal("development mode ran tailscale")
	}
}
