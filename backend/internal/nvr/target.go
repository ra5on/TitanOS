package nvr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

// ErrTargetNotAllowed is an address outside the home network that the
// administrator did not confirm, or one that is never a camera (this
// host, multicast).
var ErrTargetNotAllowed = errors.New("nvr: the address is not in the home network")

// overlay are the ranges of Tailscale and NetBird; a camera at another
// site is reached through them.
var overlay = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")}

// HomeAddr tells whether an address is in the home network: private
// addresses and the Tailscale/NetBird ranges. Never this host (loopback),
// link-local, multicast or the public internet: a probe that carries a
// password must not become a scanner or a way to services of this NAS.
func HomeAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsUnspecified() || addr.IsMulticast() || addr.IsLinkLocalUnicast() {
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

// Prober talks to cameras for the search and the check of an address.
// Every connection is checked at the moment it is made, so a name that
// changes its address between check and connect (DNS rebinding) still
// cannot reach beyond what is allowed.
type Prober struct {
	// AllowPublic: the administrator confirmed addresses outside the home
	// network (netguard.LAN then decides: still never this host).
	AllowPublic bool
	// Allow replaces the rule (tests reach 127.0.0.1).
	Allow func(netip.Addr) bool
	// Timeout bounds one connection and its answers; 4 s if zero.
	Timeout time.Duration
}

func (p *Prober) allowed(addr netip.Addr) bool {
	addr = addr.Unmap()
	switch {
	case p.Allow != nil:
		return p.Allow(addr)
	case p.AllowPublic:
		return netguard.LAN(addr) && !addr.IsLinkLocalUnicast()
	}
	return HomeAddr(addr)
}

func (p *Prober) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 4 * time.Second
}

// dial connects to host:port; the address actually connected to must be
// allowed.
func (p *Prober) dial(ctx context.Context, host string, port int) (net.Conn, error) {
	if addr, err := netip.ParseAddr(host); err == nil && !p.allowed(addr) {
		return nil, fmt.Errorf("%w: %s", ErrTargetNotAllowed, host)
	}
	dialer := &net.Dialer{
		Timeout: p.timeout(),
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !p.allowed(ap.Addr()) {
				return ErrTargetNotAllowed
			}
			return nil
		},
	}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, fmt.Sprint(port)))
	if err != nil && errors.Is(err, ErrTargetNotAllowed) {
		return nil, fmt.Errorf("%w: %s", ErrTargetNotAllowed, host)
	}
	return conn, err
}
