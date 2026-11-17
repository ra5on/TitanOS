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
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/netguard"
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

// ErrConfined: accounts of the role "user" reach only mail servers on the
// internet (owner 17.11.2026, as for their own notification channels):
// no home network, no Tailscale/CGNAT, no private or reserved ranges.
// Owners and administrators may use a mail server in the home network.
var ErrConfined = fmt.Errorf("%w: accounts of the role user may only use mail servers on the internet, not in the home network", ErrHostForbidden)

// confined tells whether an account reaches only public addresses: every
// account that is not an owner or administrator (also an unknown one).
func (m *Manager) confined(user string) bool {
	account, ok := m.lookup(user)
	return !ok || !account.Admin
}

// CheckTargetFor is CheckTarget for an account; confined accounts reach
// public addresses only, outside the networks of the NAS (own addresses
// counted as /56 for IPv6) and never the NAS itself on any port.
func CheckTargetFor(target netip.AddrPort, own []netip.Addr, ports []int, confined bool) error {
	if confined && !netguard.Confined(target.Addr(), ownPrefixes(own, nil)) {
		return ErrConfined
	}
	return CheckTarget(target, own, ports)
}

// ownPrefixes are the networks of the NAS: those of its interfaces and
// each own address (IPv4 alone, IPv6 widened by netguard).
func ownPrefixes(own []netip.Addr, nets []netip.Prefix) []netip.Prefix {
	out := append([]netip.Prefix{}, nets...)
	for _, a := range own {
		a = a.Unmap()
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out
}

// confinedTarget tells whether a confined account may reach an address.
func (m *Manager) confinedTarget(addr netip.Addr) bool {
	own := m.OwnAddrs
	if own == nil {
		own = interfaceAddrs
	}
	nets := m.OwnNets
	if nets == nil {
		nets = netguard.OwnNetworks
	}
	return netguard.Confined(addr, ownPrefixes(own(), nets()))
}

// NewTargetLimit is how many connections an account may open a minute to
// mail servers it never logged in to (all roles): a stored mailbox whose
// port is changed again and again must not become a port scanner
// (security review of 1.05).
const NewTargetLimit = 10

// targetState remembers the servers an account logged in to (RAM only)
// and counts its tries of others.
type targetState struct {
	mu    sync.Mutex
	known map[string]bool
	// tried: account → attempts of the last minute (a host:port, or one
	// check of the settings for IMAP and SMTP together) → first time.
	tried map[string]map[string]time.Time
}

func targetKey(user string, s Server) string {
	return user + "\x00" + s.Host + ":" + strconv.Itoa(s.Port)
}

// attemptKey names one check of the settings in a context: its IMAP and
// SMTP connections count as one attempt.
type attemptKey struct{}

// succeeded notes that an account logged in to a server.
func (m *Manager) succeeded(user string, s Server) {
	t := &m.targets
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.known == nil {
		t.known = map[string]bool{}
	}
	t.known[targetKey(user, s)] = true
}

// knownLocked marks the servers of the stored mailboxes with a password
// as known (m.mu held), at a start: they need no attempt of their own
// after every start (follow-up of the security review). Servers changed
// later count until a login there goes through.
func (m *Manager) knownLocked() {
	for name, f := range m.users {
		for _, mb := range f.Mailboxes {
			if mb.Password != "" {
				m.succeeded(name, mb.IMAP)
				m.succeeded(name, mb.SMTP)
			}
		}
	}
}

// mayTry tells whether an account may open a connection to a server now:
// a known one always, else at most NewTargetLimit different attempts a
// minute (the same host:port, or one check of the settings, counts once).
func (m *Manager) mayTry(ctx context.Context, user string, s Server) bool {
	t := &m.targets
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.known[targetKey(user, s)] {
		return true
	}
	attempt := s.Host + ":" + strconv.Itoa(s.Port)
	if id, ok := ctx.Value(attemptKey{}).(string); ok {
		attempt = "check:" + id
	}
	now := m.clock()
	if t.tried == nil {
		t.tried = map[string]map[string]time.Time{}
	}
	recent := t.tried[user]
	if recent == nil {
		recent = map[string]time.Time{}
		t.tried[user] = recent
	}
	for k, at := range recent {
		if now.Sub(at) >= time.Minute {
			delete(recent, k)
		}
	}
	if _, counted := recent[attempt]; counted {
		return true
	}
	if len(recent) >= NewTargetLimit {
		return false
	}
	recent[attempt] = now
	return true
}

// forgetTargets drops what is known of an account's servers.
func (m *Manager) forgetTargets(user string) {
	t := &m.targets
	t.mu.Lock()
	defer t.mu.Unlock()
	for k := range t.known {
		if strings.HasPrefix(k, user+"\x00") {
			delete(t.known, k)
		}
	}
	delete(t.tried, user)
}

// dial opens a TCP connection to a server of a mailbox of an account,
// checked at the moment of connecting, after DNS.
func (m *Manager) dial(ctx context.Context, user string, s Server) (net.Conn, error) {
	if !validHost(s.Host) || s.Port < 1 || s.Port > 65535 {
		return nil, ErrInvalid
	}
	if !m.mayTry(ctx, user, s) {
		return nil, ErrRateLimited
	}
	confined := m.confined(user)
	dialer := net.Dialer{Timeout: connectTimeout, ControlContext: func(_ context.Context, _, address string, _ syscall.RawConn) error {
		target, err := netip.ParseAddrPort(address)
		if err != nil {
			return errForbiddenTarget
		}
		// The role is checked before the hook of the tests, so that
		// tests reach their servers on loopback only as administrators.
		if confined && !m.confinedTarget(target.Addr()) {
			return ErrConfined
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
