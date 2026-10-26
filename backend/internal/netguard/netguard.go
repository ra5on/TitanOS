// Package netguard decides which network addresses titand may connect to on
// behalf of an account. Accounts with the role "user" only reach the public
// internet; owners and administrators may also reach their home network.
package netguard

import "net/netip"

// nonPublic are ranges that are not the internet beyond what Go knows:
// shared address space (Tailscale), benchmarking, documentation, reserved.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
}

// Public reports whether an address is on the public internet: no loopback,
// private, link-local, Tailscale/CGNAT or reserved range.
func Public(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || addr.IsPrivate() {
		return false
	}
	for _, prefix := range nonPublic {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// LAN reports whether an owner or administrator may connect to an address:
// public and home-network addresses, but never this host's loopback, an
// unspecified or a multicast address.
func LAN(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() && !addr.IsLoopback() && !addr.IsUnspecified() && !addr.IsMulticast() && !addr.IsInterfaceLocalMulticast()
}
