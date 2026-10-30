package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/netguard"
)

// foundLocked returns the addresses of this node on its network
// interfaces, home network first, then the VPNs: Tailscale and NetBird
// (100.64.0.0/10), WireGuard and other tunnels with private addresses.
// m.mu is held.
func (m *Manager) foundLocked() []string {
	if m.Local != nil {
		return m.Local()
	}
	internet := m.state != nil && m.state.Internet
	var private, vpn, public []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		// Docker and VM bridges are no way to another NAS.
		if strings.HasPrefix(iface.Name, "docker") || strings.HasPrefix(iface.Name, "br-") ||
			strings.HasPrefix(iface.Name, "veth") || strings.HasPrefix(iface.Name, "virbr") || strings.HasPrefix(iface.Name, "lxcbr") {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			prefix, err := netip.ParsePrefix(a.String())
			if err != nil {
				continue
			}
			ip := prefix.Addr().Unmap()
			if ip.IsLinkLocalUnicast() || ip.IsLoopback() || ip.IsMulticast() {
				continue
			}
			host := net.JoinHostPort(ip.String(), strconv.Itoa(m.Port))
			switch {
			case cgnat.Contains(ip) || tailscale6.Contains(ip):
				vpn = append(vpn, host)
			case netguard.Local(ip):
				private = append(private, host)
			case internet && netguard.Public(ip):
				public = append(public, host)
			}
		}
	}
	// IPv4 before IPv6 within each kind: home routers hand out changing
	// IPv6 addresses more often.
	for _, list := range [][]string{private, vpn, public} {
		slices.SortStableFunc(list, func(a, b string) int {
			return boolInt(strings.Contains(a, "[")) - boolInt(strings.Contains(b, "["))
		})
	}
	out := append(append(private, vpn...), public...)
	if len(out) > maxAddresses {
		out = out[:maxAddresses]
	}
	return out
}

var (
	cgnat      = netip.MustParsePrefix("100.64.0.0/10")
	tailscale6 = netip.MustParsePrefix("fd7a:115c:a1e0::/48")
)

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ownAddressesLocked returns the addresses of this node as the others see
// them: the added ones first, then the found ones. m.mu is held.
func (m *Manager) ownAddressesLocked() []string {
	out := slices.Clone(m.self.Extra)
	for _, a := range m.foundLocked() {
		if !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	if len(out) > maxAddresses {
		out = out[:maxAddresses]
	}
	return out
}

// selfNodeLocked returns this node as a member. m.mu is held.
func (m *Manager) selfNodeLocked() Node {
	name := "TitanOS"
	if m.Name != nil {
		name = m.Name()
	}
	return Node{ID: m.self.ID, Name: name, Addresses: m.ownAddressesLocked(), Extra: slices.Clone(m.self.Extra), Cert: m.cert.fingerprint}
}

// NormalizeAddress checks an address the administrator adds: a host name
// or IP address, with a port or with the HTTPS port of titand.
func (m *Manager) NormalizeAddress(address string) (string, error) {
	address = strings.TrimSpace(address)
	address = strings.TrimPrefix(strings.TrimPrefix(address, "https://"), "http://")
	address = strings.TrimSuffix(address, "/")
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		host, port = strings.Trim(address, "[]"), strconv.Itoa(m.Port)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 || host == "" || len(host) > 253 ||
		strings.ContainsAny(host, " /?#@") {
		return "", fmt.Errorf("%w: address", ErrInvalid)
	}
	return net.JoinHostPort(host, strconv.Itoa(n)), nil
}

// SetExtra sets the addresses added for this node and passes them on.
func (m *Manager) SetExtra(ctx context.Context, addresses []string) error {
	clean := []string{}
	for _, a := range addresses {
		if strings.TrimSpace(a) == "" {
			continue
		}
		n, err := m.NormalizeAddress(a)
		if err != nil {
			return err
		}
		if !slices.Contains(clean, n) {
			clean = append(clean, n)
		}
	}
	if len(clean) > 8 {
		return fmt.Errorf("%w: too many addresses", ErrInvalid)
	}
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return ErrNotLoaded
	}
	next := m.self
	next.Extra = clean
	if err := m.store.WriteJSON(nodeFile, next); err != nil {
		m.mu.Unlock()
		return err
	}
	m.self = next
	m.mu.Unlock()
	return m.refreshAddresses(ctx)
}

// refreshAddresses updates the entry of this node in the cluster when its
// addresses changed, and tells the others.
func (m *Manager) refreshAddresses(ctx context.Context) error {
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return nil
	}
	me := m.selfNodeLocked()
	i := slices.IndexFunc(m.state.Nodes, func(n Node) bool { return n.ID == me.ID })
	if i < 0 {
		m.mu.Unlock()
		return nil
	}
	old := m.state.Nodes[i]
	if slices.Equal(old.Addresses, me.Addresses) && slices.Equal(old.Extra, me.Extra) && old.Name == me.Name && old.Cert == me.Cert {
		m.mu.Unlock()
		return nil
	}
	me.Joined, me.Updated = old.Joined, time.Now().UTC().Truncate(time.Second)
	next := cloneState(m.state)
	next.Nodes[i] = me
	if err := m.saveLocked(next, m.secrets); err != nil {
		m.mu.Unlock()
		return err
	}
	m.mu.Unlock()
	for _, n := range m.others() {
		if err := m.call(ctx, n, "POST", "/api/v1/cluster/peer/node", "", me, nil); err != nil {
			slog.Info("cluster: node not told of new addresses", "node", n.Name, "err", err)
		}
	}
	return nil
}

// updateNode takes over the new entry another node sent of itself.
func (m *Manager) updateNode(from string, n Node) error {
	if n.ID != from || len(n.Addresses) > maxAddresses || !validName(n.Name) || len(n.Cert) != 64 {
		return ErrInvalid
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == nil {
		return ErrNoCluster
	}
	i := slices.IndexFunc(m.state.Nodes, func(x Node) bool { return x.ID == n.ID })
	if i < 0 {
		return ErrUnknownNode
	}
	if !n.Updated.After(m.state.Nodes[i].Updated) {
		return nil
	}
	n.Joined = m.state.Nodes[i].Joined
	next := cloneState(m.state)
	next.Nodes[i] = n
	return m.saveLocked(next, m.secrets)
}
