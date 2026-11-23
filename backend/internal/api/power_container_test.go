package api

import (
	"net"
	"testing"
)

func TestViaContainer(t *testing.T) {
	old := containerNetworks
	defer func() { containerNetworks = old }()
	_, docker0, _ := net.ParseCIDR("172.17.0.1/16")
	_, proxy, _ := net.ParseCIDR("172.18.0.1/16")
	containerNetworks = func() []*net.IPNet { return []*net.IPNet{docker0, proxy} }
	for ip, want := range map[string]bool{
		"172.18.0.2":     true, // the reverse proxy of the owner's NAS (23 Nov 2026)
		"172.17.0.5":     true,
		"192.168.178.20": false,
		"100.64.1.2":     false,
		"::1":            false,
		"":               false,
		"not an ip":      false,
	} {
		if got := viaContainer(ip); got != want {
			t.Errorf("viaContainer(%q) = %v, want %v", ip, got, want)
		}
	}
}

func TestDockerBridgeNames(t *testing.T) {
	for name, want := range map[string]bool{
		"docker0": true, "br-398ea6b154ac": true,
		"br0": false, "virbr0": false, "lxcbr0": false, "br-lan": false, "tailscale0": false,
	} {
		if got := dockerBridgeName.MatchString(name); got != want {
			t.Errorf("%s: %v, want %v", name, got, want)
		}
	}
}
