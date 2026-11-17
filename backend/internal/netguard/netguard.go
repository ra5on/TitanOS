// Package netguard decides which network addresses titand may connect to on
// behalf of an account. Accounts with the role "user" only reach the public
// internet; owners and administrators may also reach their home network.
package netguard

import (
	"net"
	"net/netip"
)

// nonPublic are ranges that are not the internet beyond what Go knows:
// shared address space (Tailscale), benchmarking, documentation, reserved.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("fd7a:115c:a1e0::/48"),
	// 6to4 and local-use NAT64 carry IPv4 addresses inside, the home
	// network's among them (security review of 1.05).
	netip.MustParsePrefix("2002::/16"), netip.MustParsePrefix("64:ff9b:1::/48"),
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

// overlay are the ranges of Tailscale and NetBird.
var overlay = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")}

// Local reports whether a client is in the home network or comes over
// Tailscale or NetBird: private, link-local and loopback addresses and the
// overlay ranges, never a public address.
func Local(addr netip.Addr) bool {
	addr = addr.Unmap()
	if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() {
		return true
	}
	for _, prefix := range overlay {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// ownPrefixBits widens the IPv6 networks of the NAS for Confined: a home
// network usually gets a /56 from the provider, every device in it a
// public address (security review of 1.05).
const ownPrefixBits = 56

// OwnNetworks are the networks of this host's interfaces (address with
// its mask), IPv6 widened to at least a /56. Loopback and link-local are
// left out: no rule needs them.
func OwnNetworks() []netip.Prefix {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, a := range addrs {
		prefix, err := netip.ParsePrefix(a.String())
		if err != nil {
			continue
		}
		out = append(out, widen(prefix))
	}
	return out
}

// widen makes an IPv6 network at most ownPrefixBits long and masks it.
func widen(prefix netip.Prefix) netip.Prefix {
	addr := prefix.Addr().Unmap()
	bits := prefix.Bits()
	if addr.Is6() && bits > ownPrefixBits {
		bits = ownPrefixBits
	}
	if addr.Is4() && bits > 32 {
		bits = 32
	}
	p, err := addr.Prefix(bits)
	if err != nil {
		return netip.PrefixFrom(addr, addr.BitLen())
	}
	return p
}

// Confined reports whether an account limited to the internet may connect
// to an address: Public, and not in a network of the NAS itself (own) –
// with IPv6 the devices of the home network have public addresses, and so
// has the NAS, on every port. own nil reads the interfaces now.
func Confined(addr netip.Addr, own []netip.Prefix) bool {
	addr = addr.Unmap()
	if !Public(addr) {
		return false
	}
	if own == nil {
		own = OwnNetworks()
	}
	for _, p := range own {
		if widen(p).Contains(addr) {
			return false
		}
	}
	return true
}
