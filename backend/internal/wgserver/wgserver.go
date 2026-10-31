// Package wgserver runs a plain WireGuard server on the NAS (Settings ›
// WireGuard, docs/WIREGUARD.md): one switch, devices added by name with a
// QR code. TitanOS picks port, tunnel network and keys itself; the private
// key of a device is made on the NAS, shown once and never stored.
//
// The interface titanoswg is set up from /config at every start with ip and
// wg (wireguard-tools, kernel module of the Debian kernel). Devices reach
// the NAS and, by choice, the home network or everything through it; the
// NAS masquerades their traffic so the router needs no routes back.
package wgserver

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/run"
)

const (
	// Interface is the name of the tunnel; the firewall counts it as VPN.
	Interface    = "titanoswg"
	settingsFile = "wireguard/server.json"
	keyFile      = "wireguard/server.key"
	// DefaultPort is the port the firewall already allows from the internet
	// (service "VPN tunnel").
	DefaultPort = 51820
	maxPeers    = 50
	natChain    = "TITANOS-WG"
)

var (
	ErrInvalid  = errors.New("wireguard: invalid settings")
	ErrNotFound = errors.New("wireguard: no such device")
	ErrTooMany  = errors.New("wireguard: too many devices")
	ErrNameUsed = errors.New("wireguard: name in use")
	// defaultNetworks are tried in turn for the tunnel; the first that
	// overlaps no network of the NAS is taken.
	defaultNetworks = []string{"10.253.0.0/24", "10.254.13.0/24", "172.31.253.0/24", "192.168.253.0/24"}
)

// Settings are kept in /config/wireguard/server.json.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Endpoint is the name or address devices dial, without port.
	Endpoint string `json:"endpoint"`
	Port     int    `json:"port"`
	// Network is the tunnel network (IPv4); the NAS has its first address.
	Network string `json:"network"`
	// FullTunnel sends all traffic of the devices through the NAS; else
	// only the NAS and the home network.
	FullTunnel bool `json:"fullTunnel"`
	// DNS is handed to devices; empty leaves theirs.
	DNS   string `json:"dns,omitempty"`
	Peers []Peer `json:"peers"`
}

// Peer is a device. Its private key is not kept.
type Peer struct {
	Name      string    `json:"name"`
	PublicKey string    `json:"publicKey"`
	Preshared string    `json:"presharedKey"`
	Address   string    `json:"address"`
	Created   time.Time `json:"created"`
}

// PeerStatus is a device as the page shows it, without its preshared key.
type PeerStatus struct {
	Name          string     `json:"name"`
	PublicKey     string     `json:"publicKey"`
	Address       string     `json:"address"`
	Created       time.Time  `json:"created"`
	LastHandshake *time.Time `json:"lastHandshake,omitempty"`
	Received      int64      `json:"received"`
	Sent          int64      `json:"sent"`
	Endpoint      string     `json:"endpoint,omitempty"`
}

// Status is what the page shows.
type Status struct {
	Enabled    bool         `json:"enabled"`
	Running    bool         `json:"running"`
	Endpoint   string       `json:"endpoint"`
	Port       int          `json:"port"`
	Network    string       `json:"network"`
	FullTunnel bool         `json:"fullTunnel"`
	DNS        string       `json:"dns"`
	PublicKey  string       `json:"publicKey,omitempty"`
	Peers      []PeerStatus `json:"peers"`
	Error      string       `json:"error,omitempty"`
	Disabled   bool         `json:"disabled,omitempty"`
}

// Manager keeps the settings and the interface in line.
type Manager struct {
	store  *config.Store
	runner run.Runner
	runDir string
	// Disabled stores settings but touches no interface (development mode).
	Disabled bool
	// HomeNetworks are the networks of the NAS devices may reach.
	HomeNetworks func() []string
	// SuggestEndpoint proposes the name devices dial (dynamic DNS).
	SuggestEndpoint func() string
	now             func() time.Time
	forward         func() error

	mu       sync.Mutex
	loaded   bool
	settings Settings
	private  string
	running  bool
	err      string
}

// New returns a manager for the configuration in store.
func New(store *config.Store) *Manager {
	return &Manager{store: store, runner: run.Exec{}, runDir: "/run/titanos-wireguard", now: time.Now, forward: enableForwarding,
		HomeNetworks: func() []string { return nil }, SuggestEndpoint: func() string { return "" }}
}

// enableForwarding lets the NAS pass on packets of the devices (Docker,
// Tailscale and NetBird want the same; it is never switched off).
func enableForwarding() error {
	return os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0o644)
}

// NewKeyPair makes a WireGuard key pair (X25519), base64 as wg writes it.
func NewKeyPair() (private, public string, err error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(key.Bytes()), base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func publicOf(private string) (string, error) {
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

func newPreshared() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

func validKey(key string) bool {
	raw, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(raw) == 32
}

// Load reads settings and key from /config.
func (m *Manager) Load() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Settings{Port: DefaultPort, Peers: []Peer{}}
	if err := m.store.ReadJSON(settingsFile, &s); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if s.Peers == nil {
		s.Peers = []Peer{}
	}
	key, err := m.store.Read(keyFile)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.settings, m.private, m.loaded = s, strings.TrimSpace(string(key)), true
	return nil
}

// pickNetwork chooses a tunnel network that no network of the NAS overlaps.
func (m *Manager) pickNetwork() string {
	var home []netip.Prefix
	for _, text := range m.HomeNetworks() {
		if p, err := netip.ParsePrefix(text); err == nil {
			home = append(home, p)
		}
	}
	for _, candidate := range defaultNetworks {
		p := netip.MustParsePrefix(candidate)
		if !slices.ContainsFunc(home, p.Overlaps) {
			return candidate
		}
	}
	return defaultNetworks[0]
}

func validEndpoint(host string) bool {
	if host == "" {
		return true
	}
	if a, err := netip.ParseAddr(host); err == nil {
		return a.Zone() == ""
	}
	if len(host) > 253 || strings.HasPrefix(host, ".") || strings.HasSuffix(host, ".") || strings.Contains(host, "..") {
		return false
	}
	return !strings.ContainsFunc(host, func(r rune) bool {
		return !(r == '.' || r == '-' || r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)))
	})
}

// normalize checks settings that came from the page; peers stay as stored.
func normalize(s Settings) (Settings, error) {
	s.Endpoint = strings.ToLower(strings.TrimSpace(s.Endpoint))
	s.DNS = strings.TrimSpace(s.DNS)
	if !validEndpoint(s.Endpoint) {
		return s, fmt.Errorf("%w: endpoint", ErrInvalid)
	}
	if s.Port == 0 {
		s.Port = DefaultPort
	}
	if s.Port < 1024 || s.Port > 65535 {
		return s, fmt.Errorf("%w: port", ErrInvalid)
	}
	p, err := netip.ParsePrefix(s.Network)
	if err != nil || !p.Addr().Is4() || !p.Addr().IsPrivate() || p.Bits() < 16 || p.Bits() > 28 || p.Masked() != p {
		return s, fmt.Errorf("%w: network", ErrInvalid)
	}
	if s.DNS != "" {
		var list []string
		for _, part := range strings.FieldsFunc(s.DNS, func(r rune) bool { return r == ',' || r == ' ' }) {
			a, err := netip.ParseAddr(part)
			if err != nil || a.Zone() != "" {
				return s, fmt.Errorf("%w: dns", ErrInvalid)
			}
			list = append(list, a.String())
		}
		if len(list) > 3 {
			return s, fmt.Errorf("%w: dns", ErrInvalid)
		}
		s.DNS = strings.Join(list, ", ")
	}
	return s, nil
}

// serverAddress is the first address of the tunnel network.
func serverAddress(network string) netip.Prefix {
	p := netip.MustParsePrefix(network)
	return netip.PrefixFrom(p.Addr().Next(), p.Bits())
}

// Set stores the settings of the server (not the devices) and applies them.
func (m *Manager) Set(ctx context.Context, s Settings) error {
	m.mu.Lock()
	if !m.loaded {
		m.mu.Unlock()
		return errors.New("wireguard: not loaded")
	}
	if s.Network == "" {
		s.Network = m.settings.Network
	}
	if s.Network == "" {
		s.Network = m.pickNetwork()
	}
	s, err := normalize(s)
	if err != nil {
		m.mu.Unlock()
		return err
	}
	if len(m.settings.Peers) > 0 && s.Network != m.settings.Network {
		m.mu.Unlock()
		return fmt.Errorf("%w: the network cannot change while devices exist", ErrInvalid)
	}
	s.Peers = m.settings.Peers
	if m.private == "" {
		private, _, err := NewKeyPair()
		if err != nil {
			m.mu.Unlock()
			return err
		}
		if err := m.store.Write(keyFile, []byte(private+"\n")); err != nil {
			m.mu.Unlock()
			return err
		}
		m.private = private
	}
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		m.mu.Unlock()
		return err
	}
	m.settings = s
	m.mu.Unlock()
	return m.Apply(ctx)
}

// Settings returns the current settings.
func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := m.settings
	s.Peers = slices.Clone(s.Peers)
	return s
}

func validName(name string) bool {
	return name != "" && len([]rune(name)) <= 40 && !strings.ContainsFunc(name, func(r rune) bool { return unicode.IsControl(r) || r == '\\' || r == '"' })
}

// AddPeer makes a device: keys, the next free address. It returns the
// configuration for the device, the only place its private key appears.
func (m *Manager) AddPeer(ctx context.Context, name string) (Peer, string, error) {
	name = strings.TrimSpace(name)
	if !validName(name) {
		return Peer{}, "", fmt.Errorf("%w: name", ErrInvalid)
	}
	m.mu.Lock()
	s := m.settings
	if !m.loaded || s.Network == "" || m.private == "" {
		m.mu.Unlock()
		return Peer{}, "", fmt.Errorf("%w: switch the server on first", ErrInvalid)
	}
	if len(s.Peers) >= maxPeers {
		m.mu.Unlock()
		return Peer{}, "", ErrTooMany
	}
	if slices.ContainsFunc(s.Peers, func(p Peer) bool { return strings.EqualFold(p.Name, name) }) {
		m.mu.Unlock()
		return Peer{}, "", ErrNameUsed
	}
	address, ok := nextAddress(s)
	if !ok {
		m.mu.Unlock()
		return Peer{}, "", ErrTooMany
	}
	private, public, err := NewKeyPair()
	if err != nil {
		m.mu.Unlock()
		return Peer{}, "", err
	}
	psk, err := newPreshared()
	if err != nil {
		m.mu.Unlock()
		return Peer{}, "", err
	}
	peer := Peer{Name: name, PublicKey: public, Preshared: psk, Address: address.String() + "/32", Created: m.now().UTC()}
	next := s
	next.Peers = append(slices.Clone(s.Peers), peer)
	if err := m.store.WriteJSON(settingsFile, next); err != nil {
		m.mu.Unlock()
		return Peer{}, "", err
	}
	m.settings = next
	serverPublic, _ := publicOf(m.private)
	m.mu.Unlock()
	text := ClientConfig(next, peer, private, serverPublic, m.HomeNetworks())
	if err := m.Apply(ctx); err != nil {
		slog.Warn("WireGuard device added but the server is not up", "err", err)
	}
	return peer, text, nil
}

// nextAddress finds the lowest free address after the server's.
func nextAddress(s Settings) (netip.Addr, bool) {
	p := netip.MustParsePrefix(s.Network)
	used := map[netip.Addr]bool{serverAddress(s.Network).Addr(): true}
	for _, peer := range s.Peers {
		if a, err := netip.ParsePrefix(peer.Address); err == nil {
			used[a.Addr()] = true
		}
	}
	for a := serverAddress(s.Network).Addr().Next(); p.Contains(a); a = a.Next() {
		if !p.Contains(a.Next()) {
			break // the broadcast address
		}
		if !used[a] {
			return a, true
		}
	}
	return netip.Addr{}, false
}

// RemovePeer takes a device away; its key stops working at once.
func (m *Manager) RemovePeer(ctx context.Context, publicKey string) error {
	m.mu.Lock()
	s := m.settings
	if !slices.ContainsFunc(s.Peers, func(p Peer) bool { return p.PublicKey == publicKey }) {
		m.mu.Unlock()
		return ErrNotFound
	}
	next := s
	next.Peers = slices.DeleteFunc(slices.Clone(s.Peers), func(p Peer) bool { return p.PublicKey == publicKey })
	if err := m.store.WriteJSON(settingsFile, next); err != nil {
		m.mu.Unlock()
		return err
	}
	m.settings = next
	m.mu.Unlock()
	return m.Apply(ctx)
}

// ClientConfig is the wg-quick configuration of a device.
func ClientConfig(s Settings, peer Peer, private, serverPublic string, home []string) string {
	allowed := []string{s.Network}
	if s.FullTunnel {
		allowed = []string{"0.0.0.0/0", "::/0"}
	} else {
		for _, network := range home {
			if p, err := netip.ParsePrefix(network); err == nil && p.Addr().Is4() && p.Addr().IsPrivate() && !slices.Contains(allowed, p.Masked().String()) {
				allowed = append(allowed, p.Masked().String())
			}
		}
	}
	endpoint := s.Endpoint
	if a, err := netip.ParseAddr(endpoint); err == nil && a.Is6() {
		endpoint = "[" + endpoint + "]"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\n# %s\nPrivateKey = %s\nAddress = %s\n", peer.Name, private, peer.Address)
	if s.DNS != "" {
		fmt.Fprintf(&b, "DNS = %s\n", s.DNS)
	}
	fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nEndpoint = %s:%d\nAllowedIPs = %s\nPersistentKeepalive = 25\n",
		serverPublic, peer.Preshared, endpoint, s.Port, strings.Join(allowed, ", "))
	return b.String()
}

// serverConfig is what `wg syncconf` takes (no Address lines).
func serverConfig(s Settings, private string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nListenPort = %d\n", private, s.Port)
	for _, peer := range s.Peers {
		fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nPresharedKey = %s\nAllowedIPs = %s\n", peer.PublicKey, peer.Preshared, peer.Address)
	}
	return b.String()
}

// Apply brings the interface in line with the settings: up with all
// devices while switched on, gone while off.
func (m *Manager) Apply(ctx context.Context) error {
	m.mu.Lock()
	s, private := m.settings, m.private
	m.mu.Unlock()
	if m.Disabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var err error
	if s.Enabled && s.Network != "" && validKey(private) {
		err = m.up(ctx, s, private)
	} else {
		m.down(ctx)
	}
	m.mu.Lock()
	m.running = err == nil && s.Enabled
	m.err = ""
	if err != nil {
		m.err = err.Error()
	}
	m.mu.Unlock()
	return err
}

func (m *Manager) up(ctx context.Context, s Settings, private string) error {
	if err := os.MkdirAll(m.runDir, 0o700); err != nil {
		return err
	}
	// The keys go to wg in a file in /run (RAM, root only), never as
	// arguments, and the file goes right after.
	confPath := filepath.Join(m.runDir, Interface+".conf")
	if err := os.WriteFile(confPath, []byte(serverConfig(s, private)), 0o600); err != nil {
		return err
	}
	defer os.Remove(confPath)
	if _, err := m.runner.Run(ctx, "", "ip", "link", "show", "dev", Interface); err != nil {
		if _, err := m.runner.Run(ctx, "", "ip", "link", "add", "dev", Interface, "type", "wireguard"); err != nil {
			return fmt.Errorf("wireguard: interface: %w", err)
		}
	}
	if _, err := m.runner.Run(ctx, "", "wg", "syncconf", Interface, confPath); err != nil {
		return fmt.Errorf("wireguard: wg: %w", err)
	}
	if _, err := m.runner.Run(ctx, "", "ip", "address", "replace", serverAddress(s.Network).String(), "dev", Interface); err != nil {
		return fmt.Errorf("wireguard: address: %w", err)
	}
	if _, err := m.runner.Run(ctx, "", "ip", "link", "set", "up", "dev", Interface); err != nil {
		return fmt.Errorf("wireguard: link up: %w", err)
	}
	if err := m.forward(); err != nil {
		slog.Warn("IPv4 forwarding not switched on", "err", err)
	}
	return m.masquerade(ctx, s.Network)
}

// masquerade lets traffic of the devices leave with the NAS's address, so
// the router and the home network need no route back to the tunnel. Own
// chain, refilled each time; the jump from POSTROUTING is added once.
func (m *Manager) masquerade(ctx context.Context, network string) error {
	rules := "*nat\n:" + natChain + " - [0:0]\n-A " + natChain + " -s " + network + " ! -o " + Interface + " -j MASQUERADE\nCOMMIT\n"
	if _, err := m.runner.Run(ctx, rules, "iptables-restore", "--noflush"); err != nil {
		return fmt.Errorf("wireguard: nat: %w", err)
	}
	if _, err := m.runner.Run(ctx, "", "iptables", "-t", "nat", "-C", "POSTROUTING", "-j", natChain); err != nil {
		if _, err := m.runner.Run(ctx, "", "iptables", "-t", "nat", "-I", "POSTROUTING", "-j", natChain); err != nil {
			return fmt.Errorf("wireguard: nat: %w", err)
		}
	}
	return nil
}

func (m *Manager) down(ctx context.Context) {
	_, _ = m.runner.Run(ctx, "", "ip", "link", "del", "dev", Interface)                        // cleanup, best effort: nothing more can be done if it fails
	_, _ = m.runner.Run(ctx, "", "iptables", "-t", "nat", "-D", "POSTROUTING", "-j", natChain) // cleanup, best effort: nothing more can be done if it fails
	_, _ = m.runner.Run(ctx, "", "iptables", "-t", "nat", "-F", natChain)                      // cleanup, best effort: nothing more can be done if it fails
	_, _ = m.runner.Run(ctx, "", "iptables", "-t", "nat", "-X", natChain)                      // cleanup, best effort: nothing more can be done if it fails
}

// Status reads the devices' handshakes from wg (on request, never on a
// timer).
func (m *Manager) Status() Status {
	m.mu.Lock()
	s, private, running, problem := m.settings, m.private, m.running, m.err
	m.mu.Unlock()
	st := Status{Enabled: s.Enabled, Running: running, Endpoint: s.Endpoint, Port: s.Port, Network: s.Network, FullTunnel: s.FullTunnel, DNS: s.DNS, Error: problem, Disabled: m.Disabled, Peers: []PeerStatus{}}
	if st.Endpoint == "" {
		st.Endpoint = m.SuggestEndpoint()
	}
	if st.Port == 0 {
		st.Port = DefaultPort
	}
	if private != "" {
		st.PublicKey, _ = publicOf(private)
	}
	live := map[string]PeerStatus{}
	if running && !m.Disabled {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if out, err := m.runner.Run(ctx, "", "wg", "show", Interface, "dump"); err == nil {
			live = parseDump(out)
		}
	}
	for _, peer := range s.Peers {
		p := live[peer.PublicKey]
		p.Name, p.PublicKey, p.Address, p.Created = peer.Name, peer.PublicKey, peer.Address, peer.Created
		st.Peers = append(st.Peers, p)
	}
	return st
}

// parseDump reads the peer lines of `wg show <if> dump`: public key,
// preshared key, endpoint, allowed IPs, latest handshake, rx, tx, keepalive.
func parseDump(out string) map[string]PeerStatus {
	peers := map[string]PeerStatus{}
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		fields := strings.Split(line, "\t")
		if i == 0 || len(fields) < 8 {
			continue // the first line is the interface
		}
		p := PeerStatus{}
		if fields[2] != "(none)" {
			p.Endpoint = fields[2]
		}
		if seconds, err := strconv.ParseInt(fields[4], 10, 64); err == nil && seconds > 0 {
			t := time.Unix(seconds, 0).UTC()
			p.LastHandshake = &t
		}
		p.Received, _ = strconv.ParseInt(fields[5], 10, 64)
		p.Sent, _ = strconv.ParseInt(fields[6], 10, 64)
		peers[fields[0]] = p
	}
	return peers
}
