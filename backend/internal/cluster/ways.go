package cluster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

// The ways between the nodes. Every address of a node has one; the
// cluster (and, per node, an exception) picks the way to try first, or the
// only one to use. Automatic keeps the order the node gave its addresses
// in: added ones, home network, VPNs, internet.
const (
	WayAuto      = ""
	WayHome      = "home"
	WayTailscale = "tailscale"
	WayNetBird   = "netbird"
	WayWireGuard = "wireguard"
	// WayTunnel is the WireGuard tunnel TitanOS builds between the nodes
	// itself (tunnel.go).
	WayTunnel   = "tunnel"
	WayExtra    = "extra"
	WayInternet = "internet"
)

var ways = []string{WayHome, WayTunnel, WayWireGuard, WayTailscale, WayNetBird, WayExtra, WayInternet}

func validWay(way string) bool { return way == WayAuto || slices.Contains(ways, way) }

// Ways are the settings of the cluster for the connections between nodes.
// The zero value is what clusters from before had: automatic, fallback
// allowed, back to the first way every minute, a notice after 10 minutes.
type Ways struct {
	// Manage is the way tried first for managing; Only forbids the others.
	Manage string `json:"manage,omitempty"`
	Only   bool   `json:"only,omitempty"`
	// Backup is the only way for backups between nodes; empty: as Manage.
	Backup string `json:"backup,omitempty"`
	// Return is the minutes between tries of the preferred way while a node
	// is reached on another one; 0 is 1, -1 never.
	Return int `json:"return,omitempty"`
	// Alert is the minutes a node may be unreachable before a notice; 0 is
	// 10, -1 never.
	Alert int `json:"alert,omitempty"`
	// ClockOff stops the comparison of the clocks.
	ClockOff bool `json:"clockOff,omitempty"`
}

// Route is the exception for the way to one node.
type Route struct {
	Manage string `json:"manage,omitempty"`
	Only   bool   `json:"only,omitempty"`
	Backup string `json:"backup,omitempty"`
}

func (w Ways) check() error {
	if !validWay(w.Manage) || !validWay(w.Backup) || w.Return < -1 || w.Return > 60 || w.Alert < -1 || w.Alert > 24*60 {
		return fmt.Errorf("%w: ways", ErrInvalid)
	}
	return nil
}

func (r Route) check() error {
	if !validWay(r.Manage) || !validWay(r.Backup) {
		return fmt.Errorf("%w: route", ErrInvalid)
	}
	return nil
}

func (w Ways) returnEvery() time.Duration {
	switch {
	case w.Return < 0:
		return 0
	case w.Return == 0:
		return time.Minute
	}
	return time.Duration(w.Return) * time.Minute
}

func (w Ways) alertAfter() time.Duration {
	switch {
	case w.Alert < 0:
		return 0
	case w.Alert == 0:
		return 10 * time.Minute
	}
	return time.Duration(w.Alert) * time.Minute
}

// wayOfInterface names the way of an address on the network interface
// iface of this node.
func wayOfInterface(iface string, ip netip.Addr) string {
	switch {
	case iface == TunnelInterface:
		return WayTunnel
	case strings.HasPrefix(iface, "tailscale"):
		return WayTailscale
	case iface == "wt0" || strings.HasPrefix(iface, "nb-") || strings.HasPrefix(iface, "netbird"):
		return WayNetBird
	case strings.HasPrefix(iface, "wg") || iface == "titanoswg" || iface == "ipv64wg":
		return WayWireGuard
	}
	return wayOfIP(ip)
}

// wayOfIP guesses the way from the address alone, for nodes that send no
// ways (older versions).
func wayOfIP(ip netip.Addr) string {
	ip = ip.Unmap()
	switch {
	case cgnat.Contains(ip) || tailscale6.Contains(ip):
		return WayTailscale
	case netguard.Local(ip):
		return WayHome
	}
	return WayInternet
}

// wayOf returns the way of address on node n.
func wayOf(n Node, address string) string {
	if w, ok := n.Ways[address]; ok && validWay(w) && w != WayAuto {
		return w
	}
	if slices.Contains(n.Extra, address) {
		return WayExtra
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}
	ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
	if err != nil {
		return WayExtra
	}
	return wayOfIP(ip)
}

// purpose of a connection: managing (and everything else) or backups.
type purpose int

const (
	forManage purpose = iota
	forBackup
)

func (p purpose) key(id string) string {
	if p == forBackup {
		return id + "/backup"
	}
	return id
}

// routeLocked returns the way to try first for node id and whether it is
// the only one. m.mu is held.
func (m *Manager) routeLocked(id string, p purpose) (string, bool) {
	if m.state == nil {
		return WayAuto, false
	}
	w := m.state.Ways
	r, exception := m.state.Routes[id]
	way, only := w.Manage, w.Only
	if exception && (r.Manage != WayAuto || r.Only) {
		way, only = r.Manage, r.Only
	}
	if p == forBackup {
		backup := w.Backup
		if exception && r.Backup != WayAuto {
			backup = r.Backup
		}
		if backup != WayAuto {
			return backup, true
		}
	}
	return way, only && way != WayAuto
}

// ordered returns the addresses of n for p: those of the preferred way
// first, or only those.
func (m *Manager) ordered(n Node, p purpose) []string {
	m.mu.Lock()
	way, only := m.routeLocked(n.ID, p)
	m.mu.Unlock()
	if way == WayAuto {
		return slices.Clone(n.Addresses)
	}
	var first, rest []string
	for _, a := range n.Addresses {
		if wayOf(n, a) == way {
			first = append(first, a)
		} else if !only {
			rest = append(rest, a)
		}
	}
	return append(first, rest...)
}

// returnToPreferred tries, for every node reached on a way behind the
// preferred one, the addresses before it, as often as the settings say.
func (m *Manager) returnToPreferred(ctx context.Context) {
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return
	}
	every := m.state.Ways.returnEvery()
	m.mu.Unlock()
	if every == 0 {
		return
	}
	for _, n := range m.others() {
		for _, p := range []purpose{forManage, forBackup} {
			key := p.key(n.ID)
			m.mu.Lock()
			last, tried := m.reach[key], m.returnTried[key]
			m.mu.Unlock()
			order := m.ordered(n, p)
			i := slices.Index(order, last)
			if last == "" || i <= 0 || wayOf(n, order[i]) == wayOf(n, order[0]) || time.Since(tried) < every {
				continue
			}
			m.mu.Lock()
			m.returnTried[key] = time.Now()
			m.mu.Unlock()
			for _, a := range order[:i] {
				if wayOf(n, a) == wayOf(n, last) {
					continue
				}
				res := m.probe(ctx, n, a)
				if res.OK {
					m.reached(key, a)
					slog.Info("cluster: back on the preferred way", "node", n.Name, "way", wayOf(n, a))
					break
				}
			}
		}
	}
}

// Probe is the answer of one address of a node.
type Probe struct {
	Way     string `json:"way"`
	Address string `json:"address"`
	OK      bool   `json:"ok"`
	// Millis is the time for the answer.
	Millis int64 `json:"millis"`
	// Problem says why it did not answer: timeout, refused, blocked (the
	// settings forbid the address) or failed.
	Problem string `json:"problem,omitempty"`
	// Used is the address the node is reached at now.
	Used bool `json:"used"`
}

// NodeProbes are the answers of all addresses of one node.
type NodeProbes struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Probes []Probe `json:"probes"`
}

// probe asks node n for its status on address alone.
func (m *Manager) probe(ctx context.Context, n Node, address string) Probe {
	res := Probe{Way: wayOf(n, address), Address: address}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	start := time.Now()
	err := m.tryAddresses(ctx, n, []string{address}, "", func(ctx context.Context, address string) (*http.Response, error) {
		return m.sendSigned(ctx, n, address, "GET", PeerPrefix+"status", "", nil)
	}, nil)
	res.Millis = time.Since(start).Milliseconds()
	switch {
	case err == nil:
		res.OK = true
	case refused(err):
		res.Problem = "refused"
	case strings.Contains(err.Error(), "no address a node may be reached at"):
		res.Problem = "blocked"
	case ctx.Err() != nil || isTimeout(err):
		res.Problem = "timeout"
	default:
		res.Problem = "failed"
	}
	return res
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

// Probes asks every other node on each of its addresses, all at once.
func (m *Manager) Probes(ctx context.Context) []NodeProbes {
	nodes := m.others()
	out := make([]NodeProbes, len(nodes))
	done := make(chan struct{})
	pending := 0
	for i, n := range nodes {
		m.mu.Lock()
		used := m.reach[n.ID]
		m.mu.Unlock()
		addrs := m.ordered(n, forManage)
		for _, a := range n.Addresses {
			if !slices.Contains(addrs, a) {
				addrs = append(addrs, a)
			}
		}
		out[i] = NodeProbes{ID: n.ID, Name: n.Name, Probes: make([]Probe, len(addrs))}
		for j, a := range addrs {
			pending++
			go func() {
				p := m.probe(ctx, n, a)
				p.Used = a == used
				out[i].Probes[j] = p
				done <- struct{}{}
			}()
		}
	}
	for ; pending > 0; pending-- {
		<-done
	}
	return out
}

// SetWays changes the way settings of the cluster and tells the others.
func (m *Manager) SetWays(ctx context.Context, w Ways) error {
	if err := w.check(); err != nil {
		return err
	}
	return m.changeState(ctx, func(s *State) { s.Ways = w })
}

// SetRoute sets the exception for the way to node id; the zero Route
// removes it.
func (m *Manager) SetRoute(ctx context.Context, id string, r Route) error {
	if err := r.check(); err != nil {
		return err
	}
	if _, err := m.node(id); err != nil {
		return err
	}
	return m.changeState(ctx, func(s *State) {
		routes := map[string]Route{}
		for k, v := range s.Routes {
			if k != id {
				routes[k] = v
			}
		}
		if r != (Route{}) {
			routes[id] = r
		}
		s.Routes = routes
	})
}

// changeState applies change to the cluster on a node that may manage and
// tells the others.
func (m *Manager) changeState(ctx context.Context, change func(*State)) error {
	if err := m.managing(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return ErrNoCluster
	}
	next := cloneState(m.state)
	change(next)
	if err := m.saveLocked(next, m.secrets); err != nil {
		m.mu.Unlock()
		return err
	}
	// Other ways may apply now: look for each node again.
	clear(m.reach)
	m.mu.Unlock()
	m.announce(ctx, "")
	return nil
}
