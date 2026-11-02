package nvr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
)

// ErrTargetNotAllowed is an address the camera search must not touch.
var ErrTargetNotAllowed = errors.New("nvr: address is not in the home network")

// overlay are the ranges of Tailscale and NetBird; a camera at another site
// is reached through them.
var overlay = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")}

// AllowedAddr tells whether the camera search may connect to an address:
// private home-network addresses and the Tailscale/NetBird ranges. Never
// this host (loopback), link-local, multicast, unspecified or the public
// internet: a search with a password must not become a scanner or a way to
// reach services of this NAS or of other people.
func AllowedAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsUnspecified() || addr.IsMulticast() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return false
	}
	if addr.IsPrivate() {
		return true
	}
	for _, prefix := range overlay {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// Resolver looks up a name; net.DefaultResolver satisfies it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

// Resolve returns the addresses of a host name or IP literal. Every address
// must be allowed: a name that has one public or loopback address next to a
// private one is refused as a whole (DNS rebinding).
func Resolve(ctx context.Context, resolver Resolver, host string) ([]netip.Addr, error) {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !AllowedAddr(addr) {
			return nil, fmt.Errorf("%w: %s", ErrTargetNotAllowed, addr)
		}
		return []netip.Addr{addr.Unmap()}, nil
	}
	addrs, err := resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("nvr: no address for %q", host)
	}
	for _, addr := range addrs {
		if !AllowedAddr(addr) {
			return nil, fmt.Errorf("%w: %s", ErrTargetNotAllowed, addr)
		}
	}
	return addrs, nil
}
