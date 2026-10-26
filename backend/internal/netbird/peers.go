package netbird

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Peer is one member of the NetBird network as the dashboard shows it.
// Values the daemon does not report stay nil or empty instead of being
// guessed; `netbird status` reports no operating system.
type Peer struct {
	// ID is the peer's WireGuard public key, NetBird's stable identity.
	// Never an address: addresses can move between peers.
	ID   string `json:"id"`
	Name string `json:"name"`
	FQDN string `json:"fqdn"`
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
	// Online: a tunnel to the peer is up. Peers NetBird connects only on
	// demand (lazy connections) count as not connected while idle.
	Online *bool `json:"online"`
	// LastSeen is the last WireGuard handshake with the peer.
	LastSeen *time.Time `json:"lastSeen"`
	// Connection is "p2p" or "relayed" while connected, otherwise empty.
	Connection string   `json:"connection,omitempty"`
	Networks   []string `json:"networks"`
	Self       bool     `json:"self"`
}
type Peers struct {
	Active  bool      `json:"active"`
	State   string    `json:"state"`
	Peers   []Peer    `json:"peers"`
	Updated time.Time `json:"updated"`
}

// peersTTL bounds how often the dashboard runs `netbird status`.
const peersTTL = 5 * time.Second

type peerCache struct {
	mu    sync.Mutex
	value Peers
	at    time.Time
}

type peerJSON struct {
	FQDN          string    `json:"fqdn"`
	IP            string    `json:"netbirdIp"`
	IPv6          string    `json:"netbirdIpv6"`
	PubKey        string    `json:"publicKey"`
	Status        string    `json:"status"`
	ConnType      string    `json:"connectionType"`
	LastHandshake time.Time `json:"lastWireguardHandshake"`
	Networks      []string  `json:"networks"`
}

// WireGuard public keys are base64; nothing else is accepted as an ID.
var peerID = regexp.MustCompile(`^[A-Za-z0-9+/=_-]{1,128}$`)

func nameOf(fqdn string) string {
	name, _, _ := strings.Cut(fqdn, ".")
	return name
}

// parsePeers turns `netbird status --json` into the dashboard list.
func parsePeers(raw []byte) (Peers, bool) {
	var status statusJSON
	if json.Unmarshal(raw, &status) != nil || status.DaemonStatus == "" {
		return Peers{}, false
	}
	result := Peers{Active: true, State: status.DaemonStatus, Peers: []Peer{}}
	connected := status.DaemonStatus == "Connected"
	add := func(p Peer) {
		if !peerID.MatchString(p.ID) || slices.ContainsFunc(result.Peers, func(q Peer) bool { return q.ID == p.ID }) {
			return
		}
		result.Peers = append(result.Peers, p)
	}
	if status.PubKey != "" {
		fqdn := strings.TrimSuffix(status.FQDN, ".")
		self := Peer{ID: status.PubKey, Name: nameOf(fqdn), FQDN: fqdn, IPv4: address(status.IP), IPv6: address(status.IPv6), Networks: cleanNetworks(status.Networks), Self: true}
		online := connected
		self.Online = &online
		add(self)
	}
	for _, p := range status.Peers.Details {
		fqdn := strings.TrimSuffix(p.FQDN, ".")
		peer := Peer{ID: p.PubKey, Name: nameOf(fqdn), FQDN: fqdn, IPv4: address(p.IP), IPv6: address(p.IPv6), Networks: cleanNetworks(p.Networks)}
		// Without a running engine the peer states are stale.
		if connected {
			online := p.Status == "Connected"
			peer.Online = &online
			if online {
				switch p.ConnType {
				case "P2P":
					peer.Connection = "p2p"
				case "Relayed":
					peer.Connection = "relayed"
				}
			}
		}
		// The daemon reports the zero time when there was no handshake.
		if p.LastHandshake.Year() > 2000 {
			seen := p.LastHandshake.UTC()
			peer.LastSeen = &seen
		}
		add(peer)
	}
	slices.SortStableFunc(result.Peers[min(1, len(result.Peers)):], func(a, b Peer) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return result, true
}

// cleanNetworks keeps only the routed networks; "-" means none.
func cleanNetworks(list []string) []string {
	result := []string{}
	for _, text := range list {
		if text = strings.TrimSpace(text); text != "" && text != "-" && len(text) <= 256 {
			result = append(result, text)
		}
	}
	return result
}

// Peers lists the NetBird network. It only runs the local CLI, at most once
// per peersTTL, and never reads /config.
func (m *Manager) Peers() Peers {
	m.peers.mu.Lock()
	defer m.peers.mu.Unlock()
	now := time.Now()
	if !m.peers.at.IsZero() && now.Sub(m.peers.at) < peersTTL {
		return m.peers.value
	}
	value := Peers{State: "inactive", Peers: []Peer{}, Updated: now.UTC()}
	if !m.Disabled {
		// A missing binary or daemon means the plugin is not active: no error.
		if raw, err := m.command(10*time.Second, "", "status", "--json"); err == nil {
			if parsed, ok := parsePeers([]byte(raw)); ok {
				value = parsed
				value.Updated = now.UTC()
			} else {
				value.State = "unknown"
			}
		}
	}
	m.peers.value, m.peers.at = value, now
	return value
}
