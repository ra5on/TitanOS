package ipv64

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"
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

	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
)

// "Connect this NAS" to a Cloud Router of IPv64: TitanOS makes a peer at a
// gateway, creates the key pair itself and gives IPv64 only the public key
// (PUT /peers/{token}/keys without private_key, so the configuration at
// IPv64 holds no private key). The private key and a preshared key stay in
// /config/ipv64/ (files of their own, 0600) and are never sent anywhere,
// neither to IPv64 nor to the web UI. The interface ipv64wg is set up at
// every start of titand with ip and wg (wireguard-tools); the firewall
// counts it as VPN.

const (
	privateKeyFile = "ipv64/wireguard.key"
	presharedFile  = "ipv64/wireguard.psk"
	// Interface is the name of the tunnel.
	Interface = firewall.WireGuardInterface
)

// WireGuard is the tunnel of the NAS, as stored.
type WireGuard struct {
	Enabled bool   `json:"enabled"`
	Router  string `json:"router,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	Peer    string `json:"peer,omitempty"`
	Name    string `json:"name,omitempty"`
	// PublicKey is the key of the NAS; the private key is a file.
	PublicKey string `json:"publicKey,omitempty"`
	// From the configuration of the peer.
	Addresses  []string `json:"addresses,omitempty"`
	PeerKey    string   `json:"peerKey,omitempty"`
	Endpoint   string   `json:"endpoint,omitempty"`
	AllowedIPs []string `json:"allowedIPs,omitempty"`
	Keepalive  int      `json:"keepalive,omitempty"`
}

// TunnelStatus is the state of the interface.
type TunnelStatus struct {
	Up            bool       `json:"up"`
	LastHandshake *time.Time `json:"lastHandshake,omitempty"`
	Error         string     `json:"error,omitempty"`
	// Routes are the AllowedIPs routed into the tunnel; default routes
	// never are.
	Routes []string `json:"routes"`
}

type tunnelState struct {
	err string
	up  bool
	// key and psk are kept in RAM after the start, so that a failed setup
	// (no DNS yet at boot) is tried again without reading /config.
	key, psk string
	tried    time.Time
}

func (m *Manager) tunnelStatus() TunnelStatus {
	m.mu.Lock()
	wg := m.current.WireGuard
	st := TunnelStatus{Up: m.tunnel.up, Error: m.tunnel.err, Routes: routedNetworks(wg.AllowedIPs)}
	m.mu.Unlock()
	return st
}

// Handshake asks wg for the last handshake (on request only).
func (m *Manager) Handshake(ctx context.Context) *time.Time {
	if m.Disabled {
		return nil
	}
	out, err := m.runner.Run(ctx, "", "wg", "show", Interface, "latest-handshakes")
	if err != nil {
		return nil
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			if sec, err := strconv.ParseInt(fields[1], 10, 64); err == nil && sec > 0 {
				t := time.Unix(sec, 0)
				return &t
			}
		}
	}
	return nil
}

// NewKeyPair makes a WireGuard key pair (X25519), base64 as wg writes it.
func NewKeyPair() (private, public string, err error) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(key.Bytes()), base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

// ParsePeerConfig reads the configuration IPv64 gives for a peer.
func ParsePeerConfig(text string) (WireGuard, string, error) {
	var wg WireGuard
	var psk, section string
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = strings.ToLower(strings.Trim(line, "[]"))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.ToLower(strings.TrimSpace(key)), strings.TrimSpace(value)
		list := func() []string {
			var out []string
			for _, v := range strings.Split(value, ",") {
				if v = strings.TrimSpace(v); v != "" {
					out = append(out, v)
				}
			}
			return out
		}
		switch section + "." + key {
		case "interface.address":
			wg.Addresses = append(wg.Addresses, list()...)
		case "peer.publickey":
			wg.PeerKey = value
		case "peer.presharedkey":
			psk = value
		case "peer.endpoint":
			wg.Endpoint = value
		case "peer.allowedips":
			wg.AllowedIPs = append(wg.AllowedIPs, list()...)
		case "peer.persistentkeepalive":
			wg.Keepalive, _ = strconv.Atoi(value)
		}
	}
	for _, a := range wg.Addresses {
		if _, err := netip.ParsePrefix(a); err != nil {
			return WireGuard{}, "", fmt.Errorf("ipv64: address %q in the configuration", a)
		}
	}
	for _, a := range wg.AllowedIPs {
		if _, err := netip.ParsePrefix(a); err != nil {
			return WireGuard{}, "", fmt.Errorf("ipv64: AllowedIPs %q in the configuration", a)
		}
	}
	if !validKey(wg.PeerKey) || len(wg.Addresses) == 0 {
		return WireGuard{}, "", errors.New("ipv64: the configuration of the peer is incomplete")
	}
	if psk != "" && !validKey(psk) {
		return WireGuard{}, "", errors.New("ipv64: preshared key in the configuration")
	}
	if host, port, err := net.SplitHostPort(wg.Endpoint); err != nil || host == "" || port == "" || strings.ContainsAny(wg.Endpoint, " \t") {
		return WireGuard{}, "", errors.New("ipv64: endpoint in the configuration")
	}
	if wg.Keepalive < 0 || wg.Keepalive > 65535 {
		wg.Keepalive = 25
	}
	return wg, psk, nil
}

func validKey(key string) bool {
	data, err := base64.StdEncoding.DecodeString(key)
	return err == nil && len(data) == 32
}

// routedNetworks are the AllowedIPs that get a route: never a default
// route, which would send all traffic of the NAS through the tunnel.
func routedNetworks(allowed []string) []string {
	out := []string{}
	for _, a := range allowed {
		p, err := netip.ParsePrefix(a)
		if err != nil || p.Bits() == 0 {
			continue
		}
		out = append(out, p.Masked().String())
	}
	return out
}

// Connect makes a peer at the gateway with a key pair of the NAS and
// brings the tunnel up.
func (m *Manager) Connect(ctx context.Context, client *dnsprovider.IPv64, router, gateway, name string) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.mu.Lock()
	enabled := m.current.WireGuard.Enabled
	m.mu.Unlock()
	if enabled {
		return errors.New("ipv64: the NAS is already connected; disconnect first")
	}
	private, public, err := NewKeyPair()
	if err != nil {
		return err
	}
	peer, err := client.CreatePeer(ctx, gateway, name)
	if err != nil {
		return err
	}
	cleanup := func() { _ = client.DeletePeer(context.WithoutCancel(ctx), peer.ID) } // cleanup, best effort: nothing more can be done if it fails
	// The peer is set up in the background; changes answer 409 meanwhile.
	if err := retryBusy(ctx, func() error {
		_, err := client.SetPeerPublicKey(ctx, peer.ID, public)
		return err
	}); err != nil {
		cleanup()
		return err
	}
	var config string
	if err := retryBusy(ctx, func() error {
		var err error
		config, err = client.PeerConfig(ctx, peer.ID)
		return err
	}); err != nil {
		cleanup()
		return err
	}
	wg, psk, err := ParsePeerConfig(config)
	if err != nil {
		cleanup()
		return err
	}
	wg.Enabled, wg.Router, wg.Gateway, wg.Peer, wg.Name, wg.PublicKey = true, router, gateway, peer.ID, name, public
	if err := m.store.Write(privateKeyFile, []byte(private+"\n")); err != nil {
		cleanup()
		return err
	}
	if psk != "" {
		if err := m.store.Write(presharedFile, []byte(psk+"\n")); err != nil {
			cleanup()
			return err
		}
	} else {
		_ = m.store.Remove(presharedFile) // cleanup, best effort: nothing more can be done if it fails
	}
	if err := m.update(func(s *Settings) error {
		s.WireGuard = wg
		return nil
	}); err != nil {
		cleanup()
		return err
	}
	return m.up(ctx, wg, private, psk)
}

// retryBusy repeats a call while IPv64 answers 409 (still being set up),
// up to two minutes.
func retryBusy(ctx context.Context, call func() error) error {
	deadline := time.Now().Add(2 * time.Minute)
	for {
		err := call()
		var apiErr *dnsprovider.APIError
		if err == nil || !errors.As(err, &apiErr) || apiErr.Status != 409 || time.Now().After(deadline) {
			return err
		}
		wait := apiErr.RetryAfter
		if wait <= 0 || wait > 30*time.Second {
			wait = 3 * time.Second
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// Disconnect takes the tunnel down, forgets the keys and, with deletePeer,
// removes the peer at IPv64.
func (m *Manager) Disconnect(ctx context.Context, client *dnsprovider.IPv64, deletePeer bool) error {
	m.mu.Lock()
	wg := m.current.WireGuard
	m.mu.Unlock()
	if deletePeer && wg.Peer != "" {
		if client == nil {
			return ErrNoKey
		}
		err := client.DeletePeer(ctx, wg.Peer)
		var apiErr *dnsprovider.APIError
		if err != nil && !(errors.As(err, &apiErr) && apiErr.Status == 404) {
			return err
		}
	}
	m.down(ctx)
	_ = m.store.Remove(privateKeyFile) // cleanup, best effort: nothing more can be done if it fails
	_ = m.store.Remove(presharedFile)  // cleanup, best effort: nothing more can be done if it fails
	return m.update(func(s *Settings) error {
		s.WireGuard = WireGuard{}
		return nil
	})
}

func (m *Manager) readKeys() (string, string) {
	key, _ := m.store.Read(privateKeyFile)
	psk, _ := m.store.Read(presharedFile)
	return strings.TrimSpace(string(key)), strings.TrimSpace(string(psk))
}

// up creates the interface from the stored settings.
func (m *Manager) up(ctx context.Context, wg WireGuard, private, psk string) error {
	if m.Disabled {
		return nil
	}
	err := m.setUp(ctx, wg, private, psk)
	m.mu.Lock()
	m.tunnel.key, m.tunnel.psk, m.tunnel.tried = private, psk, m.now()
	m.tunnel.up = err == nil
	m.tunnel.err = ""
	if err != nil {
		m.tunnel.err = err.Error()
	}
	m.mu.Unlock()
	return err
}

func (m *Manager) setUp(ctx context.Context, wg WireGuard, private, psk string) error {
	if !validKey(private) {
		return errors.New("ipv64: the private key of the tunnel is missing on /config")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	// The keys go to wg as files in /run (RAM, root only), never as
	// arguments.
	if err := os.MkdirAll(m.runDir, 0o700); err != nil {
		return err
	}
	keyPath := filepath.Join(m.runDir, "private.key")
	if err := os.WriteFile(keyPath, []byte(private+"\n"), 0o600); err != nil {
		return err
	}
	defer os.Remove(keyPath)
	_, _ = m.runner.Run(ctx, "", "ip", "link", "del", "dev", Interface) // cleanup, best effort: nothing more can be done if it fails
	if _, err := m.runner.Run(ctx, "", "ip", "link", "add", "dev", Interface, "type", "wireguard"); err != nil {
		return fmt.Errorf("ipv64: interface: %w", err)
	}
	args := []string{"set", Interface, "private-key", keyPath, "peer", wg.PeerKey}
	if psk != "" {
		pskPath := filepath.Join(m.runDir, "preshared.key")
		if err := os.WriteFile(pskPath, []byte(psk+"\n"), 0o600); err != nil {
			return err
		}
		defer os.Remove(pskPath)
		args = append(args, "preshared-key", pskPath)
	}
	args = append(args, "endpoint", wg.Endpoint)
	if len(wg.AllowedIPs) > 0 {
		args = append(args, "allowed-ips", strings.Join(wg.AllowedIPs, ","))
	}
	if wg.Keepalive > 0 {
		args = append(args, "persistent-keepalive", strconv.Itoa(wg.Keepalive))
	}
	if _, err := m.runner.Run(ctx, "", "wg", args...); err != nil {
		_, _ = m.runner.Run(ctx, "", "ip", "link", "del", "dev", Interface) // cleanup, best effort: nothing more can be done if it fails
		return fmt.Errorf("ipv64: wg: %w", err)
	}
	for _, address := range wg.Addresses {
		if _, err := m.runner.Run(ctx, "", "ip", "address", "add", address, "dev", Interface); err != nil {
			return fmt.Errorf("ipv64: address: %w", err)
		}
	}
	if _, err := m.runner.Run(ctx, "", "ip", "link", "set", "up", "dev", Interface); err != nil {
		return fmt.Errorf("ipv64: link up: %w", err)
	}
	for _, network := range routedNetworks(wg.AllowedIPs) {
		if slices.ContainsFunc(wg.Addresses, func(a string) bool { p, _ := netip.ParsePrefix(a); return p.Masked().String() == network }) {
			continue
		}
		if _, err := m.runner.Run(ctx, "", "ip", "route", "replace", network, "dev", Interface); err != nil {
			slog.Warn("IPv64 tunnel route not set", "network", network, "err", err)
		}
	}
	slog.Info("IPv64 WireGuard tunnel up", "interface", Interface)
	return nil
}

// retryTunnel sets the tunnel up again while it is switched on but not up,
// at most every five minutes.
func (m *Manager) retryTunnel(ctx context.Context) {
	m.mu.Lock()
	wg, st := m.current.WireGuard, m.tunnel
	due := m.loaded && wg.Enabled && !st.up && st.key != "" && m.now().Sub(st.tried) >= 5*time.Minute
	m.mu.Unlock()
	if due {
		if err := m.up(ctx, wg, st.key, st.psk); err != nil {
			slog.Warn("IPv64 WireGuard tunnel still not up", "err", err)
		}
	}
}

func (m *Manager) down(ctx context.Context) {
	if !m.Disabled {
		_, _ = m.runner.Run(ctx, "", "ip", "link", "del", "dev", Interface) // cleanup, best effort: nothing more can be done if it fails
	}
	m.mu.Lock()
	m.tunnel = tunnelState{}
	m.mu.Unlock()
}
