package cluster

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/run"
)

// The WireGuard tunnel TitanOS builds between the nodes itself, as another
// way besides the home network, Tailscale and NetBird (ways.go). It is off
// by default. The cluster holds the tunnel network and the address of
// every node; every node its own key pair, the public key travels with its
// entry. Peers are dialled at the address the node is reached at (or the
// first other one), so the tunnel needs some way to start with; for nodes
// at other places that is the internet (port forwarding of TunnelPort) or
// a DDNS name added as an address.
//
// Like the WireGuard server of the NAS (package wgserver) it is set up
// with ip and wg; the keys go to wg in a file in /run, never as arguments.
const (
	// TunnelInterface is the tunnel; the firewall counts it as VPN.
	TunnelInterface = "titanoscl"
	// TunnelPort is the UDP port of the tunnel on every node.
	TunnelPort = 51821
	keepalive  = 25
)

// tunnelDir holds the configuration for wg for a moment (RAM, root only).
var tunnelDir = "/run/titanos-cluster"

// tunnelNetworks are tried in turn; the first no address of this node lies
// in is taken.
var tunnelNetworks = []string{"10.77.0.0/24", "10.78.77.0/24", "172.30.77.0/24", "192.168.177.0/24"}

// Runner runs ip and wg (run.Exec on a NAS).
type Runner = run.Runner

// Tunnel is the tunnel of the cluster.
type Tunnel struct {
	Enabled bool   `json:"enabled,omitempty"`
	Network string `json:"network,omitempty"`
	Port    int    `json:"port,omitempty"`
	// Addresses is the address of each node in Network.
	Addresses map[string]string `json:"addresses,omitempty"`
}

func (t Tunnel) check() error {
	if !t.Enabled {
		return nil
	}
	p, err := netip.ParsePrefix(t.Network)
	if err != nil || !p.Addr().Is4() || p.Bits() < 16 || p.Bits() > 28 || t.Port < 1 || t.Port > 65535 || len(t.Addresses) > MaxNodes {
		return fmt.Errorf("%w: tunnel", ErrInvalid)
	}
	for _, a := range t.Addresses {
		ip, err := netip.ParseAddr(a)
		if err != nil || !p.Contains(ip) {
			return fmt.Errorf("%w: tunnel", ErrInvalid)
		}
	}
	return nil
}

// TunnelView is the tunnel with its state on this node.
type TunnelView struct {
	Tunnel
	// Address is the address of this node in the tunnel.
	Address string `json:"address,omitempty"`
	Running bool   `json:"running"`
	Error   string `json:"error,omitempty"`
}

func (m *Manager) tunnelViewLocked() TunnelView {
	v := TunnelView{Tunnel: m.state.Tunnel, Running: m.tunnelUp != "", Error: m.tunnelErr}
	v.Addresses = nil
	v.Address = m.state.Tunnel.Addresses[m.self.ID]
	if v.Port == 0 {
		v.Port = TunnelPort
	}
	return v
}

func newTunnelKey() (string, error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key.Bytes()), nil
}

// publicKey returns the public key of a private WireGuard key.
func publicKey(private string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(private)
	if err != nil {
		return "", err
	}
	key, err := ecdh.X25519().NewPrivateKey(raw)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func validKey(key string) bool {
	raw, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(raw) == 32
}

// SetTunnel switches the tunnel of the cluster on or off for all nodes.
func (m *Manager) SetTunnel(ctx context.Context, on bool) error {
	if err := m.managing(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.state == nil {
		m.mu.Unlock()
		return ErrNoCluster
	}
	t := m.state.Tunnel
	found, _ := m.foundLocked()
	m.mu.Unlock()
	if on && !t.Enabled {
		network := pickTunnelNetwork(found)
		if network == "" {
			return fmt.Errorf("%w: no free network for the tunnel", ErrInvalid)
		}
		t = Tunnel{Enabled: true, Network: network, Port: TunnelPort, Addresses: map[string]string{}}
	} else if !on {
		t = Tunnel{}
	}
	return m.changeState(ctx, func(s *State) {
		s.Tunnel = t
		assignTunnelAddresses(s)
	})
}

// pickTunnelNetwork returns the first tunnel network no address in own
// lies in.
func pickTunnelNetwork(own []string) string {
	for _, text := range tunnelNetworks {
		p := netip.MustParsePrefix(text)
		used := slices.ContainsFunc(own, func(a string) bool {
			host, _, err := net.SplitHostPort(a)
			if err != nil {
				return false
			}
			ip, err := netip.ParseAddr(strings.Trim(host, "[]"))
			return err == nil && p.Contains(ip.Unmap())
		})
		if !used {
			return text
		}
	}
	return ""
}

// assignTunnelAddresses gives every node of s without one an address in
// the tunnel and drops those of nodes that left. It reports a change.
func assignTunnelAddresses(s *State) bool {
	t := &s.Tunnel
	if !t.Enabled {
		return false
	}
	prefix, err := netip.ParsePrefix(t.Network)
	if err != nil {
		return false
	}
	changed := false
	next := map[string]string{}
	taken := map[string]bool{}
	for _, n := range s.Nodes {
		if a, ok := t.Addresses[n.ID]; ok {
			next[n.ID], taken[a] = a, true
		}
	}
	if len(next) != len(t.Addresses) {
		changed = true
	}
	for _, n := range s.Nodes {
		if _, ok := next[n.ID]; ok {
			continue
		}
		for ip := prefix.Masked().Addr().Next(); prefix.Contains(ip); ip = ip.Next() {
			if !taken[ip.String()] {
				next[n.ID], taken[ip.String()], changed = ip.String(), true, true
				break
			}
		}
	}
	t.Addresses = next
	return changed
}

// applyTunnel brings the tunnel on this node in line with the cluster:
// makes the key on first use, sets peers and endpoints, or takes it down.
// The admin node also gives new nodes their address.
func (m *Manager) applyTunnel(ctx context.Context) {
	if m.Runner == nil {
		return
	}
	m.mu.Lock()
	if m.state != nil && m.state.Tunnel.Enabled && m.adminLocked() == m.self.ID {
		next := cloneState(m.state)
		if assignTunnelAddresses(next) {
			if err := m.saveLocked(next, m.secrets); err == nil {
				m.mu.Unlock()
				m.announce(ctx, "")
				m.mu.Lock()
			}
		}
	}
	on := m.state != nil && m.state.Tunnel.Enabled && m.state.Tunnel.Addresses[m.self.ID] != ""
	m.mu.Unlock()
	if !on {
		m.tunnelDown(ctx)
		return
	}
	if err := m.ensureTunnelKey(ctx); err != nil {
		m.setTunnelErr(err)
		return
	}
	m.mu.Lock()
	conf, address := m.tunnelConfigLocked()
	up := m.tunnelUp
	m.mu.Unlock()
	if conf+address == up {
		return
	}
	err := m.tunnelSet(ctx, conf, address)
	m.mu.Lock()
	if err == nil {
		m.tunnelUp, m.tunnelErr = conf+address, ""
	} else {
		m.tunnelUp, m.tunnelErr = "", err.Error()
	}
	m.mu.Unlock()
	if err != nil {
		slog.Warn("cluster: tunnel not set up", "err", err)
	}
}

func (m *Manager) setTunnelErr(err error) {
	m.mu.Lock()
	m.tunnelErr = err.Error()
	m.mu.Unlock()
	slog.Warn("cluster: tunnel not set up", "err", err)
}

// ensureTunnelKey makes the key pair of this node on first use and tells
// the others its public key.
func (m *Manager) ensureTunnelKey(ctx context.Context) error {
	m.mu.Lock()
	if m.self.TunnelKey != "" {
		m.mu.Unlock()
		return nil
	}
	key, err := newTunnelKey()
	if err != nil {
		m.mu.Unlock()
		return err
	}
	next := m.self
	next.TunnelKey = key
	if err := m.store.WriteJSON(nodeFile, next); err != nil {
		m.mu.Unlock()
		return err
	}
	m.self = next
	m.mu.Unlock()
	return m.refreshAddresses(ctx)
}

// tunnelConfigLocked returns what `wg syncconf` takes and the address of
// this node with the prefix length. m.mu is held.
func (m *Manager) tunnelConfigLocked() (string, string) {
	t := m.state.Tunnel
	port := t.Port
	if port == 0 {
		port = TunnelPort
	}
	prefix, _ := netip.ParsePrefix(t.Network)
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nListenPort = %d\n", m.self.TunnelKey, port)
	for _, n := range m.state.Nodes {
		ip := t.Addresses[n.ID]
		if n.ID == m.self.ID || ip == "" || !validKey(n.TunnelKey) {
			continue
		}
		fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nAllowedIPs = %s/32\nPersistentKeepalive = %d\n", n.TunnelKey, ip, keepalive)
		if host := m.tunnelEndpointLocked(n, ip); host != "" {
			fmt.Fprintf(&b, "Endpoint = %s\n", net.JoinHostPort(host, strconv.Itoa(port)))
		}
	}
	return b.String(), fmt.Sprintf("%s/%d", t.Addresses[m.self.ID], prefix.Bits())
}

// tunnelEndpointLocked returns the host to dial node n at for the tunnel:
// the one it is reached at, else its first address outside the tunnel.
// m.mu is held.
func (m *Manager) tunnelEndpointLocked(n Node, tunnelIP string) string {
	candidates := append([]string{m.reach[n.ID]}, n.Addresses...)
	for _, a := range candidates {
		if a == "" || wayOf(n, a) == WayTunnel {
			continue
		}
		host, _, err := net.SplitHostPort(a)
		if err != nil || strings.Trim(host, "[]") == tunnelIP {
			continue
		}
		return strings.Trim(host, "[]")
	}
	return ""
}

func (m *Manager) tunnelSet(ctx context.Context, conf, address string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := os.MkdirAll(tunnelDir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(tunnelDir, TunnelInterface+".conf")
	if err := os.WriteFile(path, []byte(conf), 0o600); err != nil {
		return err
	}
	defer os.Remove(path)
	if _, err := m.Runner.Run(ctx, "", "ip", "link", "show", "dev", TunnelInterface); err != nil {
		if _, err := m.Runner.Run(ctx, "", "ip", "link", "add", "dev", TunnelInterface, "type", "wireguard"); err != nil {
			return fmt.Errorf("tunnel: interface: %w", err)
		}
	}
	if _, err := m.Runner.Run(ctx, "", "wg", "syncconf", TunnelInterface, path); err != nil {
		return fmt.Errorf("tunnel: wg: %w", err)
	}
	if _, err := m.Runner.Run(ctx, "", "ip", "address", "flush", "dev", TunnelInterface); err != nil {
		return fmt.Errorf("tunnel: address: %w", err)
	}
	if _, err := m.Runner.Run(ctx, "", "ip", "address", "add", address, "dev", TunnelInterface); err != nil {
		return fmt.Errorf("tunnel: address: %w", err)
	}
	if _, err := m.Runner.Run(ctx, "", "ip", "link", "set", "up", "dev", TunnelInterface); err != nil {
		return fmt.Errorf("tunnel: link up: %w", err)
	}
	slog.Info("cluster: tunnel set up", "address", address)
	return nil
}

// tunnelDown removes the tunnel if this node set it up.
func (m *Manager) tunnelDown(ctx context.Context) {
	m.mu.Lock()
	up := m.tunnelUp
	m.tunnelUp, m.tunnelErr = "", ""
	m.mu.Unlock()
	if up == "" {
		return
	}
	_, _ = m.Runner.Run(ctx, "", "ip", "link", "del", "dev", TunnelInterface) // cleanup, best effort: nothing more can be done if it fails
	slog.Info("cluster: tunnel taken down")
}

// StopTunnel takes the tunnel down when titand stops or leaves the cluster.
func (m *Manager) StopTunnel(ctx context.Context) {
	if m.Runner != nil {
		m.tunnelDown(ctx)
	}
}
