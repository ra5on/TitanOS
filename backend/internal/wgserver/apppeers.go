package wgserver

import (
	"context"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Devices of paired Android apps (docs/ANDROID-APP.md "VPN", PLAN 116):
// the app makes its key pair on the phone and hands in only the public
// key; the NAS adds a preshared key and the next address, as for a device
// of the page. One device per app; it ends with the app.

// maxAppName is the length of the name of an app device (appdevices.MaxName).
const maxAppName = 64

// AppServer is what an app needs besides its own keys to set up the
// tunnel: the server's key, port and tunnel network and the address of
// the NAS in it, the networks of the NAS and the server's DNS setting.
type AppServer struct {
	PublicKey string `json:"publicKey"`
	Port      int    `json:"port"`
	Network   string `json:"network"`
	// Address is the NAS inside the tunnel (first address of Network).
	Address      string   `json:"address"`
	HomeNetworks []string `json:"homeNetworks"`
	DNS          []string `json:"dns"`
	// FullTunnel is the choice of the page for its devices; the app has
	// its own switch and only shows it.
	FullTunnel bool `json:"fullTunnel"`
	// Keepalive is what the configurations of the page carry (seconds).
	Keepalive int `json:"keepalive"`
	// Endpoint is the address of the page (or the suggestion), may be "".
	Endpoint string `json:"endpoint"`
}

// Ready reports whether a device can be added now: the server is set up
// and switched on.
func (m *Manager) Ready() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.loaded && m.settings.Enabled && m.settings.Network != "" && validKey(m.private)
}

// AppServer returns the server's side for an app; false while it is off.
func (m *Manager) AppServer() (AppServer, bool) {
	m.mu.Lock()
	s, private, ok := m.settings, m.private, m.loaded && m.settings.Enabled && m.settings.Network != "" && validKey(m.private)
	m.mu.Unlock()
	if !ok {
		return AppServer{}, false
	}
	public, _ := publicOf(private)
	out := AppServer{PublicKey: public, Port: s.Port, Network: s.Network, Address: serverAddress(s.Network).Addr().String(),
		HomeNetworks: []string{}, DNS: []string{}, FullTunnel: s.FullTunnel, Keepalive: 25, Endpoint: s.Endpoint}
	if out.Port == 0 {
		out.Port = DefaultPort
	}
	if out.Endpoint == "" {
		out.Endpoint = m.SuggestEndpoint()
	}
	for _, network := range m.HomeNetworks() {
		if p, err := netip.ParsePrefix(network); err == nil && p.Addr().Is4() && p.Addr().IsPrivate() && !slices.Contains(out.HomeNetworks, p.Masked().String()) {
			out.HomeNetworks = append(out.HomeNetworks, p.Masked().String())
		}
	}
	for _, part := range strings.Split(s.DNS, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out.DNS = append(out.DNS, part)
		}
	}
	return out, true
}

// AppPeer returns the device of an app, false if it has none.
func (m *Manager) AppPeer(account, device string) (Peer, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i := slices.IndexFunc(m.settings.Peers, func(p Peer) bool { return p.Account == account && p.Device == device })
	if i < 0 || account == "" || device == "" {
		return Peer{}, false
	}
	return m.settings.Peers[i], true
}

// appName is the name an app device shows on the page: the name of the
// phone, without characters wg-quick files cannot carry; " (App)" and a
// number when a device of the page has that name already.
func appName(name string, taken func(string) bool) string {
	name = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\\' || r == '"' {
			return -1
		}
		return r
	}, name))
	if name == "" {
		name = "Android"
	}
	if r := []rune(name); len(r) > maxAppName {
		name = strings.TrimSpace(string(r[:maxAppName]))
	}
	if !taken(name) {
		return name
	}
	for i := 1; ; i++ {
		suffix := " (App)"
		if i > 1 {
			suffix = " (App " + strconv.Itoa(i) + ")"
		}
		candidate := name + suffix
		if !taken(candidate) {
			return candidate
		}
	}
}

// SetAppPeer gives the app device of account a tunnel with publicKey,
// made on the phone. An app that has one already gets the new key and a
// new preshared key at the same address ("make anew"): the old key stops
// at once. The answer carries the preshared key; it is stored for the
// server and handed out only here.
func (m *Manager) SetAppPeer(ctx context.Context, account, device, name, publicKey string) (Peer, error) {
	if account == "" || device == "" {
		return Peer{}, fmt.Errorf("%w: device", ErrInvalid)
	}
	if !validKey(publicKey) {
		return Peer{}, fmt.Errorf("%w: key", ErrInvalid)
	}
	psk, err := newPreshared()
	if err != nil {
		return Peer{}, err
	}
	m.mu.Lock()
	s := m.settings
	if !m.loaded || !s.Enabled || s.Network == "" || !validKey(m.private) {
		m.mu.Unlock()
		return Peer{}, ErrOff
	}
	serverPublic, _ := publicOf(m.private)
	mine := slices.IndexFunc(s.Peers, func(p Peer) bool { return p.Account == account && p.Device == device })
	if publicKey == serverPublic || slices.ContainsFunc(s.Peers, func(p Peer) bool {
		return p.PublicKey == publicKey && !(p.Account == account && p.Device == device)
	}) {
		m.mu.Unlock()
		return Peer{}, ErrKeyUsed
	}
	peers := slices.Clone(s.Peers)
	taken := func(n string) bool {
		return slices.ContainsFunc(peers, func(p Peer) bool {
			return strings.EqualFold(p.Name, n) && !(p.Account == account && p.Device == device)
		})
	}
	peer := Peer{Name: appName(name, taken), PublicKey: publicKey, Preshared: psk, Created: m.now().UTC(), Account: account, Device: device}
	if mine >= 0 {
		peer.Address = peers[mine].Address
		peers[mine] = peer
	} else {
		if len(peers) >= maxPeers {
			m.mu.Unlock()
			return Peer{}, ErrTooMany
		}
		address, ok := nextAddress(s)
		if !ok {
			m.mu.Unlock()
			return Peer{}, ErrTooMany
		}
		peer.Address = address.String() + "/32"
		peers = append(peers, peer)
	}
	next := s
	next.Peers = peers
	if err := m.store.WriteJSON(settingsFile, next); err != nil {
		m.mu.Unlock()
		return Peer{}, err
	}
	m.settings = next
	m.mu.Unlock()
	if err := m.Apply(ctx); err != nil {
		slog.Warn("WireGuard device of an app stored but the server is not up", "err", err)
	}
	return peer, nil
}

// RemoveAppPeer takes the device of an app away; false if it had none.
func (m *Manager) RemoveAppPeer(ctx context.Context, account, device string) (bool, error) {
	removed, err := m.removeApps(func(p Peer) bool { return p.Account == account && p.Device == device })
	if err != nil || removed == 0 {
		return false, err
	}
	return true, m.Apply(ctx)
}

// PruneAppPeers removes the devices of apps that ended (AppPeerLive says
// so for sure) in one write, and nothing when none did. It runs at start
// and when an app or account ends, never on a timer.
func (m *Manager) PruneAppPeers(ctx context.Context) (int, error) {
	if m.AppPeerLive == nil {
		return 0, nil
	}
	m.mu.Lock()
	candidates := slices.Clone(m.settings.Peers)
	m.mu.Unlock()
	gone := map[string]bool{}
	for _, p := range candidates {
		if !p.App() {
			continue
		}
		if live, known := m.AppPeerLive(p.Account, p.Device); known && !live {
			gone[p.PublicKey] = true
		}
	}
	if len(gone) == 0 {
		return 0, nil
	}
	removed, err := m.removeApps(func(p Peer) bool { return p.App() && gone[p.PublicKey] })
	if err != nil || removed == 0 {
		return removed, err
	}
	return removed, m.Apply(ctx)
}

func (m *Manager) removeApps(match func(Peer) bool) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	kept := slices.DeleteFunc(slices.Clone(s.Peers), match)
	removed := len(s.Peers) - len(kept)
	if removed == 0 {
		return 0, nil
	}
	next := s
	next.Peers = kept
	if err := m.store.WriteJSON(settingsFile, next); err != nil {
		return 0, err
	}
	m.settings = next
	return removed, nil
}

// Recheck applies again when the devices let in changed since the last
// Apply (an app ran out, an account was locked), judged from RAM only
// (AppPeerCached): /config is neither read nor written.
func (m *Manager) Recheck(ctx context.Context) error {
	if m.AppPeerCached == nil {
		return nil
	}
	m.mu.Lock()
	s, applied := m.settings, m.applied
	m.mu.Unlock()
	if activeKeys(s, m.AppPeerCached) == applied {
		return nil
	}
	return m.Apply(ctx)
}

// Watch calls Recheck every interval until ctx ends: a device whose app
// ran out by "stay logged in" leaves the tunnel within that time.
func (m *Manager) Watch(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := m.Recheck(ctx); err != nil {
				slog.Warn("WireGuard: devices of apps not rechecked", "err", err)
			}
		}
	}
}
