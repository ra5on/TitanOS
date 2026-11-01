package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
)

// foundLocked returns the addresses of this node on its network
// interfaces, home network first, then the VPNs: the tunnel of the
// cluster, WireGuard, Tailscale and NetBird; with the way of each. m.mu is
// held.
func (m *Manager) foundLocked() ([]string, map[string]string) {
	kinds := map[string]string{}
	if m.Local != nil {
		out := m.Local()
		for _, a := range out {
			kinds[a] = wayOf(Node{}, a)
		}
		return out, kinds
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
			way := wayOfInterface(iface.Name, ip)
			switch {
			case way != WayHome && way != WayInternet:
				vpn = append(vpn, host)
			case way == WayHome:
				private = append(private, host)
			case internet:
				public = append(public, host)
			default:
				continue
			}
			kinds[host] = way
		}
	}
	// IPv4 before IPv6 within each kind: home routers hand out changing
	// IPv6 addresses more often.
	for _, list := range [][]string{private, vpn, public} {
		slices.SortStableFunc(list, func(a, b string) int {
			return boolInt(strings.Contains(a, "[")) - boolInt(strings.Contains(b, "["))
		})
	}
	// The MagicDNS name of Tailscale outlives a new address: it comes
	// after the IPs, a node that cannot resolve it loses nothing.
	if m.TailscaleName != nil {
		if name := m.TailscaleName(); name != "" {
			host := net.JoinHostPort(name, strconv.Itoa(m.Port))
			vpn = append(vpn, host)
			kinds[host] = WayTailscale
		}
	}
	out := append(append(private, vpn...), public...)
	if len(out) > maxAddresses {
		out = out[:maxAddresses]
	}
	return out, kinds
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
// them: the added ones first, then the found ones; with their ways. m.mu
// is held.
func (m *Manager) ownAddressesLocked() ([]string, map[string]string) {
	out := slices.Clone(m.self.Extra)
	kinds := map[string]string{}
	for _, a := range out {
		kinds[a] = WayExtra
	}
	found, foundKinds := m.foundLocked()
	for _, a := range found {
		if !slices.Contains(out, a) {
			out = append(out, a)
			kinds[a] = foundKinds[a]
		}
	}
	if len(out) > maxAddresses {
		out = out[:maxAddresses]
	}
	for a := range kinds {
		if !slices.Contains(out, a) {
			delete(kinds, a)
		}
	}
	return out, kinds
}

// selfNodeLocked returns this node as a member. m.mu is held.
func (m *Manager) selfNodeLocked() Node {
	name := "TitanOS"
	if m.Name != nil {
		name = m.Name()
	}
	addrs, kinds := m.ownAddressesLocked()
	n := Node{ID: m.self.ID, Name: name, Addresses: addrs, Ways: kinds, Extra: slices.Clone(m.self.Extra), Cert: m.cert.fingerprint}
	if m.self.TunnelKey != "" {
		n.TunnelKey, _ = publicKey(m.self.TunnelKey)
	}
	return n
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
	if slices.Equal(old.Addresses, me.Addresses) && slices.Equal(old.Extra, me.Extra) && old.Name == me.Name && old.Cert == me.Cert &&
		maps.Equal(old.Ways, me.Ways) && old.TunnelKey == me.TunnelKey {
		m.mu.Unlock()
		return nil
	}
	me.Joined, me.Updated, me.Manage = old.Joined, time.Now().UTC().Truncate(time.Second), old.Manage
	if !me.Updated.After(old.Updated) {
		// Two changes in one second: the others take only a newer entry.
		me.Updated = old.Updated.Add(time.Second)
	}
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
	if n.ID != from || len(n.Addresses) > maxAddresses || len(n.Ways) > maxAddresses || !validName(n.Name) || len(n.Cert) != 64 ||
		(n.TunnelKey != "" && !validKey(n.TunnelKey)) {
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
	// Whether it may manage is the admin node's call, not its own.
	n.Joined, n.Manage = m.state.Nodes[i].Joined, m.state.Nodes[i].Manage
	next := cloneState(m.state)
	next.Nodes[i] = n
	return m.saveLocked(next, m.secrets)
}
