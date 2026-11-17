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
	mu sync.Mutex
	// known: account and address:port of a server a login went through
	// (the address after DNS, never a name that may point elsewhere).
	known map[string]bool
	// loaded: account and host:port of the mailboxes with a password as
	// they were stored at the start, each with the address it was first
	// resolved to in this run (zero until then). Only that address is no
	// new target: a name stored before a start must not lead anywhere
	// its DNS points later (third review of 1.05).
	loaded map[string]netip.AddrPort
	// tried: account → attempts of the last minute (an address after DNS
	// with its port, or one check of the settings for IMAP and SMTP
	// together) → first time.
	tried map[string]map[string]time.Time
}

func targetKey(user string, s Server) string {
	return user + "\x00" + s.Host + ":" + strconv.Itoa(s.Port)
}

// attemptKey names one check of the settings in a context: its IMAP and
// SMTP connections count as one attempt.
type attemptKey struct{}

// succeeded notes that an account logged in to the server at addr.
func (m *Manager) succeeded(user string, addr net.Addr) {
	ap, err := netip.ParseAddrPort(addr.String())
	if err != nil {
		return
	}
	t := &m.targets
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.known == nil {
		t.known = map[string]bool{}
	}
	t.known[user+"\x00"+netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port()).String()] = true
}

// loadedLocked notes the servers of the stored mailboxes with a password
// at a start (m.mu held): unchanged since then, they are no new targets
// and need no attempt of their own after every start. A server changed
// later counts until a login there goes through.
func (m *Manager) loadedLocked() {
	t := &m.targets
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.loaded == nil {
		t.loaded = map[string]netip.AddrPort{}
	}
	for name, f := range m.users {
		for _, mb := range f.Mailboxes {
			if mb.Password != "" {
				t.loaded[targetKey(name, mb.IMAP)] = netip.AddrPort{}
				t.loaded[targetKey(name, mb.SMTP)] = netip.AddrPort{}
			}
		}
	}
}

// mayTry tells whether an account may connect to a server at target now:
// the first address a server stored at the start resolves to, or one a
// login went through, always; else at most NewTargetLimit different
// attempts a minute (the same address and port, or one check of the
// settings, counts once). Attempts count by the address after DNS, not
// by the name: a name whose DNS changes from try to try is a new target
// each time (third review of 1.05).
func (m *Manager) mayTry(ctx context.Context, user string, s Server, target netip.AddrPort) bool {
	t := &m.targets
	t.mu.Lock()
	defer t.mu.Unlock()
	addr := netip.AddrPortFrom(target.Addr().Unmap(), target.Port())
	if bound, ok := t.loaded[targetKey(user, s)]; ok {
		if !bound.IsValid() {
			t.loaded[targetKey(user, s)] = addr
			return true
		}
		if bound == addr {
			return true
		}
	}
	if t.known[user+"\x00"+addr.String()] {
		return true
	}
	attempt := addr.String()
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
	for k := range t.loaded {
		if strings.HasPrefix(k, user+"\x00") {
			delete(t.loaded, k)
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
		if err := m.checkTarget(target); err != nil {
			return err
		}
		if !m.mayTry(ctx, user, s, target) {
			return ErrRateLimited
		}
		return nil
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
	for _, known := range []error{ErrRateLimited, ErrBusy, ErrAuthFailed, ErrUnreachable, ErrTLS, ErrHostForbidden, ErrTimeout, ErrTooLarge, ErrNotFound, ErrFolderExists, ErrSendFailed, ErrInvalid, ErrNoPassword, ErrDisabled} {
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
