package tmail

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"syscall"
	"time"
)

// Where titand may connect for a mailbox (docs/TMAIL.md "Ziele"): any host
// and port, the home network included, but never loopback, link-local,
// unspecified or multicast addresses, and not the NAS's own addresses on
// the ports titand listens on. The check runs on the address the dialer
// is about to connect to, after DNS, so that a name cannot point
// somewhere else between check and connect.

// Time limits (docs/TMAIL.md "Zeitgrenzen").
const (
	connectTimeout = 15 * time.Second
	commandTimeout = 60 * time.Second
	fetchTimeout   = 120 * time.Second
	sendTimeout    = 120 * time.Second
)

var errForbiddenTarget = fmt.Errorf("%w", ErrHostForbidden)

// CheckTarget tells whether a mailbox may connect to an address: own are
// the NAS's addresses, ports the ports of titand.
func CheckTarget(target netip.AddrPort, own []netip.Addr, ports []int) error {
	addr := target.Addr().Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() ||
		addr.IsUnspecified() || addr.IsMulticast() || addr.IsInterfaceLocalMulticast() {
		return errForbiddenTarget
	}
	if slices.Contains(ports, int(target.Port())) {
		for _, a := range own {
			if a.Unmap() == addr {
				return errForbiddenTarget
			}
		}
	}
	return nil
}

// interfaceAddrs are the addresses of this host.
func interfaceAddrs() []netip.Addr {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	var out []netip.Addr
	for _, a := range addrs {
		if prefix, err := netip.ParsePrefix(a.String()); err == nil {
			out = append(out, prefix.Addr())
		}
	}
	return out
}

func (m *Manager) checkTarget(target netip.AddrPort) error {
	if m.Targets != nil {
		return m.Targets(target)
	}
	own := m.OwnAddrs
	if own == nil {
		own = interfaceAddrs
	}
	return CheckTarget(target, own(), m.OwnPorts)
}

// dial opens a TCP connection to a server of a mailbox, checked at the
// moment of connecting.
func (m *Manager) dial(ctx context.Context, s Server) (net.Conn, error) {
	if !validHost(s.Host) || s.Port < 1 || s.Port > 65535 {
		return nil, ErrInvalid
	}
	dialer := net.Dialer{Timeout: connectTimeout, ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
		target, err := netip.ParseAddrPort(address)
		if err != nil {
			return errForbiddenTarget
		}
		return m.checkTarget(target)
	}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(s.Host, strconv.Itoa(s.Port)))
	if err != nil {
		return nil, classify(err)
	}
	return conn, nil
}

// tlsConfig checks the certificate of host against the trusted roots.
func (m *Manager) tlsConfig(host string) *tls.Config {
	return &tls.Config{ServerName: host, RootCAs: m.RootCAs, MinVersion: tls.VersionTLS12}
}

// classify turns an error of a connection into one of the errors of the
// package, keeping the first.
func classify(err error) error {
	if err == nil {
		return nil
	}
	for _, known := range []error{ErrAuthFailed, ErrUnreachable, ErrTLS, ErrHostForbidden, ErrTimeout, ErrTooLarge, ErrNotFound, ErrFolderExists, ErrSendFailed, ErrInvalid, ErrNoPassword, ErrDisabled} {
		if errors.Is(err, known) {
			return err
		}
	}
	var certErr *tls.CertificateVerificationError
	var recordErr tls.RecordHeaderError
	var alert tls.AlertError
	var netErr net.Error
	switch {
	case errors.As(err, &certErr), errors.As(err, &recordErr), errors.As(err, &alert):
		return fmt.Errorf("%w: %s", ErrTLS, safeText(err.Error()))
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, syscall.ETIMEDOUT), errors.As(err, &netErr) && netErr.Timeout():
		return ErrTimeout
	}
	return fmt.Errorf("%w: %s", ErrUnreachable, safeText(err.Error()))
}

// safeText shortens a text of a server or the network for an answer or
// the log: one line, at most 300 characters.
func safeText(s string) string {
	out := []rune{}
	for _, r := range s {
		if r == '\r' || r == '\n' || r == '\t' {
			r = ' '
		}
		if r < 0x20 || r == 0x7f {
			continue
		}
		out = append(out, r)
		if len(out) >= 300 {
			break
		}
	}
	return string(out)
}
