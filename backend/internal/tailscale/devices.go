package tailscale

import (
	"encoding/json"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Device is one node of the tailnet as the dashboard shows it. Values the
// daemon does not report stay nil instead of being guessed.
type Device struct {
	// ID is Tailscale's stable node ID (public node key as fallback). Never
	// an address: addresses can move between nodes.
	ID       string     `json:"id"`
	Name     string     `json:"name"`
	DNSName  string     `json:"dnsName"`
	HostName string     `json:"hostName"`
	IPv4     []string   `json:"ipv4"`
	IPv6     []string   `json:"ipv6"`
	Online   *bool      `json:"online"`
	LastSeen *time.Time `json:"lastSeen"`
	OS       string     `json:"os,omitempty"`
	// ExitNodeOption: the node offers itself as exit node.
	// ExitNode: this NAS currently sends its traffic through it.
	ExitNodeOption bool     `json:"exitNodeOption"`
	ExitNode       bool     `json:"exitNode"`
	Routes         []string `json:"routes"`
	Self           bool     `json:"self"`
}
type Devices struct {
	Active  bool      `json:"active"`
	State   string    `json:"state"`
	Devices []Device  `json:"devices"`
	Updated time.Time `json:"updated"`
}

// devicesTTL bounds how often the dashboard runs `tailscale status`.
const devicesTTL = 5 * time.Second

type deviceCache struct {
	mu    sync.Mutex
	value Devices
	at    time.Time
}

type peerStatus struct {
	ID, PublicKey, HostName, DNSName, OS string
	TailscaleIPs                         []string
	PrimaryRoutes                        []string
	LastSeen                             *time.Time
	Online, ExitNode, ExitNodeOption     bool
}

var deviceID = regexp.MustCompile(`^[A-Za-z0-9:._-]{1,128}$`)

// parseDevices turns `tailscale status --json` into the dashboard list.
func parseDevices(raw []byte) (Devices, bool) {
	var status struct {
		BackendState string
		Self         *peerStatus
		Peer         map[string]*peerStatus
	}
	if json.Unmarshal(raw, &status) != nil {
		return Devices{}, false
	}
	result := Devices{Active: true, State: status.BackendState, Devices: []Device{}}
	running := status.BackendState == "Running"
	add := func(p *peerStatus, self bool) {
		if p == nil {
			return
		}
		id := p.ID
		if id == "" {
			id = p.PublicKey
		}
		if !deviceID.MatchString(id) || slices.ContainsFunc(result.Devices, func(d Device) bool { return d.ID == id }) {
			return
		}
		dns := strings.TrimSuffix(p.DNSName, ".")
		name, _, _ := strings.Cut(dns, ".")
		if name == "" {
			name = p.HostName
		}
		d := Device{ID: id, Name: name, DNSName: dns, HostName: p.HostName, IPv4: []string{}, IPv6: []string{}, OS: p.OS, ExitNodeOption: p.ExitNodeOption, ExitNode: p.ExitNode, Routes: []string{}, Self: self}
		for _, text := range p.TailscaleIPs {
			if a, err := netip.ParseAddr(text); err == nil && a.Is4() {
				d.IPv4 = append(d.IPv4, a.String())
			} else if err == nil {
				d.IPv6 = append(d.IPv6, a.String())
			}
		}
		for _, text := range p.PrimaryRoutes {
			if prefix, err := netip.ParsePrefix(text); err == nil {
				d.Routes = append(d.Routes, prefix.String())
			}
		}
		// Without a running backend the online flags are stale.
		if running {
			online := p.Online
			d.Online = &online
		}
		// The daemon reports the zero time when it never saw the node.
		if p.LastSeen != nil && p.LastSeen.Year() > 2000 {
			seen := p.LastSeen.UTC()
			d.LastSeen = &seen
		}
		result.Devices = append(result.Devices, d)
	}
	add(status.Self, true)
	for _, p := range status.Peer {
		add(p, false)
	}
	slices.SortStableFunc(result.Devices[min(1, len(result.Devices)):], func(a, b Device) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return result, true
}

// Devices lists the nodes of the tailnet. It only runs the local CLI,
// at most once per devicesTTL, and never reads /config.
func (m *Manager) Devices() Devices {
	m.devices.mu.Lock()
	defer m.devices.mu.Unlock()
	now := time.Now()
	if !m.devices.at.IsZero() && now.Sub(m.devices.at) < devicesTTL {
		return m.devices.value
	}
	value := Devices{State: "inactive", Devices: []Device{}, Updated: now.UTC()}
	if !m.Disabled {
		// A missing binary means the plugin is not active: no error.
		if raw, err := m.command("status", "--json"); err == nil {
			if parsed, ok := parseDevices([]byte(raw)); ok {
				value = parsed
				value.Updated = now.UTC()
			} else {
				value.State = "unknown"
			}
		}
	}
	m.devices.value, m.devices.at = value, now
	return value
}
