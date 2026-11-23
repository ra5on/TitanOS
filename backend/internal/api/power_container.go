package api

import (
	"net"
	"regexp"
)

// dockerBridgeName matches the bridges Docker makes: docker0 and br-<id>
// of its own networks. TitanOS's home bridge br0, libvirt's virbr0 and
// LXC's bridge are not among them.
var dockerBridgeName = regexp.MustCompile(`^(docker0|br-[0-9a-f]{12})$`)

// containerNetworks lists the networks of Docker's bridges on this NAS.
// A variable for the tests.
var containerNetworks = func() []*net.IPNet {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var nets []*net.IPNet
	for _, iface := range ifaces {
		if !dockerBridgeName.MatchString(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if n, ok := addr.(*net.IPNet); ok {
				nets = append(nets, n)
			}
		}
	}
	return nets
}

// viaContainer says whether a request reached titand from a Docker
// container, typically a reverse proxy the browser goes through. Going
// down stops Docker first, and with it the way back to the web UI: the
// restart dialog warns before (owner decision of 23 Nov 2026).
func viaContainer(ip string) bool {
	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}
	for _, n := range containerNetworks() {
		if n.Contains(addr) {
			return true
		}
	}
	return false
}
