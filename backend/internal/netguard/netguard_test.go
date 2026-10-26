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
