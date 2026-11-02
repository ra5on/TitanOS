package nvr

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"syscall"
	"time"
)

// Prober talks to cameras. Every connection is checked at the moment it is
// made, so a name that changes its address between check and connect (DNS
// rebinding) still cannot reach beyond the home network.
type Prober struct {
	// Allow decides which addresses may be contacted; nil means AllowedAddr.
	Allow    func(netip.Addr) bool
	Resolver Resolver
	Timeout  time.Duration
	// ONVIFPorts and Table replace the vendor table; for tests only.
	ONVIFPorts []int
	Table      []Candidate
}

func (p *Prober) allow(addr netip.Addr) bool {
	if p.Allow != nil {
		return p.Allow(addr.Unmap())
	}
	return AllowedAddr(addr)
}

func (p *Prober) timeout() time.Duration {
	if p.Timeout > 0 {
		return p.Timeout
	}
	return 4 * time.Second
}

// resolve returns the addresses of host, all of which must be allowed.
func (p *Prober) resolve(ctx context.Context, host string) error {
	if p.Allow == nil {
		_, err := Resolve(ctx, p.Resolver, host)
		return err
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if !p.allow(addr) {
			return fmt.Errorf("%w: %s", ErrTargetNotAllowed, addr)
		}
	}
	return nil
}

func (p *Prober) dialer() *net.Dialer {
	return &net.Dialer{
		Timeout: p.timeout(),
		Control: func(_, address string, _ syscall.RawConn) error {
			ap, err := netip.ParseAddrPort(address)
			if err != nil || !p.allow(ap.Addr()) {
				return ErrTargetNotAllowed
			}
			return nil
		},
	}
}

func (p *Prober) dial(ctx context.Context, host string, port int) (net.Conn, error) {
	if err := p.resolve(ctx, host); err != nil {
		return nil, err
	}
	return p.dialer().DialContext(ctx, "tcp", hostPort(host, port))
}

type netConn = net.Conn

func (p *Prober) dialAddr(ctx context.Context, addr string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if err := p.resolve(ctx, host); err != nil {
		return nil, err
	}
	return p.dialer().DialContext(ctx, "tcp", net.JoinHostPort(host, portStr))
}
