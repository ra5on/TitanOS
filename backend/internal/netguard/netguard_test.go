package netguard

import (
	"net/netip"
	"testing"
)

func TestRules(t *testing.T) {
	for addr, want := range map[string][2]bool{
		"8.8.8.8":              {true, true},
		"2001:4860:4860::8888": {true, true},
		"192.168.1.10":         {false, true},
		"10.0.0.1":             {false, true},
		"100.100.1.1":          {false, true},
		"fd00::1":              {false, true},
		"169.254.169.254":      {false, true},
		"127.0.0.1":            {false, false},
		"::1":                  {false, false},
		"0.0.0.0":              {false, false},
		"224.0.0.1":            {false, false},
		"::ffff:127.0.0.1":     {false, false},
		"::ffff:8.8.8.8":       {true, true},
	} {
		a := netip.MustParseAddr(addr)
		if Public(a) != want[0] || LAN(a) != want[1] {
			t.Errorf("%s: Public=%v LAN=%v, want %v", addr, Public(a), LAN(a), want)
		}
	}
}

func TestLocal(t *testing.T) {
	for addr, want := range map[string]bool{
		"192.168.1.10":       true,
		"10.0.0.1":           true,
		"100.100.1.1":        true, // Tailscale, NetBird
		"fd7a:115c:a1e0::1":  true,
		"fd00::1":            true,
		"fe80::1":            true,
		"127.0.0.1":          true,
		"::ffff:192.168.0.2": true,
		"8.8.8.8":            false,
		"2001:db8::1":        false,
		"::ffff:8.8.8.8":     false,
	} {
		if got := Local(netip.MustParseAddr(addr)); got != want {
			t.Errorf("Local(%s) = %v, want %v", addr, got, want)
		}
	}
}

// Confined accounts (role user) reach neither 6to4/NAT64 wrappers nor any
// address in a network of the NAS, IPv6 counted as /56, on any port.
func TestConfined(t *testing.T) {
	own := []netip.Prefix{netip.MustParsePrefix("192.168.1.10/24"), netip.MustParsePrefix("2a02:8108:1234:5600::10/64"), netip.MustParsePrefix("203.0.113.5/29")}
	for addr, want := range map[string]bool{
		"8.8.8.8":                 true,
		"2001:4860:4860::8888":    true,
		"2a02:8108:1234:5600::10": false, // the NAS itself
		"2a02:8108:1234:5600::1":  false, // the router in the same /64
		"2a02:8108:1234:56ff::7":  false, // another /64 of the same /56
		"2a02:8108:1234:5700::7":  true,  // a neighbour's /56
		"203.0.113.6":             false, // public IPv4 on an interface
		"2002:c0a8:010a::1":       false, // 6to4 of 192.168.1.10
		"64:ff9b:1::c0a8:10a":     false, // local-use NAT64
		"64:ff9b::808:808":        false, // well-known NAT64 (as before)
		"192.168.1.20":            false,
	} {
		if got := Confined(netip.MustParseAddr(addr), own); got != want {
			t.Errorf("%s: %v, want %v", addr, got, want)
		}
	}
	if Public(netip.MustParseAddr("2002::1")) || Public(netip.MustParseAddr("64:ff9b:1::1")) {
		t.Error("6to4 or NAT64 public")
	}
}
