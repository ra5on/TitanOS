package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const topologyFile = "network/topology.json"
const lastGoodFile = "network/last-good.json"

var idRE = regexp.MustCompile(`^[a-z][a-z0-9-]{0,9}$`)

type Bond struct {
	ID    string   `json:"id"`
	Ports []string `json:"ports"`
	Mode  string   `json:"mode"`
}
type Route struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Metric      int    `json:"metric"`
}
type LogicalNetwork struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Purpose    string   `json:"purpose"`
	Parent     string   `json:"parent"`
	VLAN       int      `json:"vlan"`
	Bridge     bool     `json:"bridge"`
	Management bool     `json:"management"`
	IPv4       string   `json:"ipv4"`
	Address    string   `json:"address"`
	Gateway    string   `json:"gateway"`
	IPv6       string   `json:"ipv6"`
	Address6   string   `json:"address6"`
	Gateway6   string   `json:"gateway6"`
	DNS        []string `json:"dns"`
	Routes     []Route  `json:"routes,omitempty"`
}

func cloneTopology(cfg Config) Config {
	cfg.Ports = slices.Clone(cfg.Ports)
	cfg.DNS = slices.Clone(cfg.DNS)
	cfg.Networks = slices.Clone(cfg.Networks)
	cfg.Bonds = slices.Clone(cfg.Bonds)
	for i := range cfg.Networks {
		cfg.Networks[i].DNS = slices.Clone(cfg.Networks[i].DNS)
		cfg.Networks[i].Routes = slices.Clone(cfg.Networks[i].Routes)
	}
	for i := range cfg.Bonds {
		cfg.Bonds[i].Ports = slices.Clone(cfg.Bonds[i].Ports)
	}
	return cfg
}
func networkDevice(n LogicalNetwork) string {
	if n.Bridge {
		if n.Management {
			return Bridge
		}
		return "tn" + n.ID
	}
	if n.VLAN > 0 {
		return "tv" + n.ID
	}
	if strings.HasPrefix(n.Parent, "bond:") {
		return "tb" + strings.TrimPrefix(n.Parent, "bond:")
	}
	return n.Parent
}
func topologyHasBridge(cfg Config) bool {
	for _, n := range cfg.Networks {
		if n.Management && n.Bridge {
			return true
		}
	}
	return false
}

func (m *Manager) validateTopology(cfg Config) error {
	invalid := func(s string) error { return fmt.Errorf("%w: %s", ErrInvalid, s) }
	if !hostnameRE.MatchString(cfg.Hostname) || len(cfg.Networks) > 32 || len(cfg.Bonds) > 8 {
		return invalid("hostname or topology size")
	}
	ports := map[string]bool{}
	for _, p := range m.Interfaces() {
		ports[p.Name] = true
	}
	used := map[string]bool{}
	bonds := map[string]bool{}
	for _, b := range cfg.Bonds {
		if !idRE.MatchString(b.ID) || bonds[b.ID] || len(b.Ports) < 2 || !slices.Contains(BondModes, b.Mode) {
			return invalid("bond")
		}
		bonds[b.ID] = true
		for _, p := range b.Ports {
			if !ports[p] || used[p] {
				return invalid("bond member")
			}
			used[p] = true
		}
	}
	ids := map[string]bool{}
	links := map[string]bool{}
	management := 0
	prefixes := []netip.Prefix{}
	for _, n := range cfg.Networks {
		if !idRE.MatchString(n.ID) || ids[n.ID] || strings.TrimSpace(n.Name) == "" || len(n.Name) > 80 || strings.ContainsAny(n.Name, "\r\n\x00") || !slices.Contains([]string{"management", "storage", "apps", "custom"}, n.Purpose) {
			return invalid("network identity")
		}
		ids[n.ID] = true
		if strings.HasPrefix(n.Parent, "bond:") {
			if !bonds[strings.TrimPrefix(n.Parent, "bond:")] {
				return invalid("unknown bond")
			}
		} else if !ports[n.Parent] || used[n.Parent] {
			return invalid("unknown or bonded port")
		}
		key := n.Parent + ":" + strconv.Itoa(n.VLAN)
		if links[key] || n.VLAN < 0 || n.VLAN > 4094 {
			return invalid("duplicate network or invalid VLAN")
		}
		links[key] = true
		if !slices.Contains([]string{"off", "dhcp", "static"}, n.IPv4) || !slices.Contains([]string{"off", "auto", "dhcp", "static"}, n.IPv6) {
			return invalid("address mode")
		}
		if n.Management {
			management++
			if n.IPv4 == "off" && n.IPv6 == "off" {
				return invalid("management needs an address")
			}
		}
		for i, v := range []struct {
			mode, address, gateway string
			v4                     bool
		}{{n.IPv4, n.Address, n.Gateway, true}, {n.IPv6, n.Address6, n.Gateway6, false}} {
			if v.mode != "static" {
				if v.address != "" || v.gateway != "" {
					return invalid("automatic/disabled address has static values")
				}
				continue
			}
			p, err := netip.ParsePrefix(v.address)
			if err != nil || p.Addr().Is4() != v.v4 || p.Addr().IsUnspecified() || p.Addr().IsLoopback() || p.Addr().IsMulticast() {
				return invalid("static address")
			}
			if i == 0 && (p.Bits() < 8 || p.Bits() > 30 || p.Addr() == p.Masked().Addr() || !p.Contains(p.Addr().Next())) {
				return invalid("IPv4 host address")
			}
			for _, old := range prefixes {
				if old.Overlaps(p) {
					return invalid("overlapping static networks")
				}
			}
			prefixes = append(prefixes, p)
			if v.gateway != "" {
				g, err := netip.ParseAddr(v.gateway)
				if err != nil || g.Is4() != v.v4 || g == p.Addr() || g.IsUnspecified() || g.IsMulticast() || (!p.Contains(g) && !(!v.v4 && g.IsLinkLocalUnicast())) {
					return invalid("gateway")
				}
			}
		}
		if len(n.DNS) > 3 || len(n.Routes) > 32 {
			return invalid("DNS or route count")
		}
		for _, dns := range n.DNS {
			a, e := netip.ParseAddr(dns)
			if e != nil || a.IsUnspecified() || a.IsMulticast() {
				return invalid("DNS server")
			}
		}
		for _, r := range n.Routes {
			p, e := netip.ParsePrefix(r.Destination)
			g, ge := netip.ParseAddr(r.Gateway)
			if e != nil || ge != nil || g.Is4() != p.Addr().Is4() || g.IsUnspecified() || g.IsMulticast() || r.Metric < 0 || r.Metric > 65535 {
				return invalid("route")
			}
			addr := n.Address
			if !g.Is4() {
				addr = n.Address6
			}
			local, le := netip.ParsePrefix(addr)
			if le != nil || !local.Contains(g) {
				return invalid("route gateway must be on a static local network")
			}
		}
	}
	if management != 1 {
		return invalid("exactly one preferred management network is required")
	}
	return nil
}

func (m *Manager) renderTopology(cfg Config) map[string]string {
	files := map[string]string{}
	members := map[string][]string{}
	addressed := map[string]LogicalNetwork{}
	for _, b := range cfg.Bonds {
		name := "tb" + b.ID
		files[prefix+name+".netdev"] = "[NetDev]\nName=" + name + "\nKind=bond\n\n[Bond]\nMode=" + b.Mode + "\nMIIMonitorSec=1s\n"
		for _, p := range b.Ports {
			members[p] = append(members[p], "Bond="+name)
		}
	}
	for _, n := range cfg.Networks {
		lower := n.Parent
		if strings.HasPrefix(lower, "bond:") {
			lower = "tb" + strings.TrimPrefix(lower, "bond:")
		}
		if n.VLAN > 0 {
			vlan := "tv" + n.ID
			files[prefix+vlan+".netdev"] = "[NetDev]\nName=" + vlan + "\nKind=vlan\n\n[VLAN]\nId=" + strconv.Itoa(n.VLAN) + "\n"
			members[lower] = append(members[lower], "VLAN="+vlan)
			lower = vlan
		}
		if n.Bridge {
			br := networkDevice(n)
			physical := n.Parent
			if strings.HasPrefix(physical, "bond:") {
				for _, b := range cfg.Bonds {
					if b.ID == strings.TrimPrefix(physical, "bond:") && len(b.Ports) > 0 {
						physical = b.Ports[0]
					}
				}
			}
			mac, _ := os.ReadFile(filepath.Join(m.sys, "class/net", physical, "address"))
			files[prefix+br+".netdev"] = "[NetDev]\nName=" + br + "\nKind=bridge\nMACAddress=" + strings.TrimSpace(string(mac)) + "\n\n[Bridge]\nSTP=no\n"
			members[lower] = append(members[lower], "Bridge="+br)
			lower = br
		}
		addressed[lower] = n
	}
	for name := range addressed {
		if _, ok := members[name]; !ok {
			members[name] = nil
		}
	}
	for name, lines := range members {
		var b strings.Builder
		n, ok := addressed[name]
		b.WriteString("[Match]\nName=" + name + "\n\n[Link]\nRequiredForOnline=" + yesNoNetwork(ok && n.Management) + "\n\n[Network]\nKeepConfiguration=no\n")
		for _, line := range lines {
			b.WriteString(line + "\n")
		}
		if !ok {
			b.WriteString("DHCP=no\nIPv6AcceptRA=no\nLinkLocalAddressing=no\n")
		} else {
			dhcp := "no"
			if n.IPv4 == "dhcp" {
				dhcp = "ipv4"
			}
			if n.IPv6 == "dhcp" {
				if dhcp == "ipv4" {
					dhcp = "yes"
				} else {
					dhcp = "ipv6"
				}
			}
			ra := "no"
			linklocal := "no"
			if n.IPv6 != "off" {
				linklocal = "ipv6"
			}
			if n.IPv6 == "auto" || n.IPv6 == "dhcp" {
				ra = "yes"
			}
			b.WriteString("DHCP=" + dhcp + "\nIPv6AcceptRA=" + ra + "\nLinkLocalAddressing=" + linklocal + "\n")
			for _, addr := range []string{n.Address, n.Address6} {
				if addr != "" {
					b.WriteString("Address=" + addr + "\n")
				}
			}
			for _, dns := range n.DNS {
				b.WriteString("DNS=" + dns + "\n")
			}
			b.WriteString("DNSDefaultRoute=" + yesNoNetwork(n.Management) + "\n")
			for _, g := range []string{n.Gateway, n.Gateway6} {
				if g != "" {
					metric := 500
					if n.Management {
						metric = 100
					}
					b.WriteString("\n[Route]\nGateway=" + g + "\nMetric=" + strconv.Itoa(metric) + "\n")
				}
			}
			for _, r := range n.Routes {
				b.WriteString("\n[Route]\nDestination=" + r.Destination + "\nGateway=" + r.Gateway + "\nMetric=" + strconv.Itoa(r.Metric) + "\n")
			}
			b.WriteString("\n[DHCPv4]\nClientIdentifier=mac\nUseRoutes=" + yesNoNetwork(n.Management) + "\nUseDNS=" + yesNoNetwork(n.Management && len(n.DNS) == 0) + "\nRouteMetric=100\n")
			b.WriteString("\n[DHCPv6]\nUseDNS=" + yesNoNetwork(n.Management && len(n.DNS) == 0) + "\n")
			b.WriteString("\n[IPv6AcceptRA]\nUseGateway=" + yesNoNetwork(n.Management) + "\nUseDNS=" + yesNoNetwork(n.Management && len(n.DNS) == 0) + "\nRouteMetric=100\n")
		}
		files[prefix+name+".network"] = b.String()
	}
	// Unassigned physical ports must not acquire unexpected DHCP/default routes
	// from the image's fallback configuration after explicit topology opt-in.
	for _, port := range m.Interfaces() {
		name := prefix + port.Name + ".network"
		if files[name] == "" {
			files[name] = "[Match]\nName=" + port.Name + "\n\n[Link]\nRequiredForOnline=no\n\n[Network]\nDHCP=no\nIPv6AcceptRA=no\nLinkLocalAddressing=no\nKeepConfiguration=no\n"
		}
	}
	return files
}
func yesNoNetwork(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

// CurrentIPv4 is used when validating Docker IPAM against a DHCP NAS address.
// The caller holds an attachment lease, so the confirmed device cannot change.
func (m *Manager) CurrentIPv4(ctx context.Context, id string) (string, bool) {
	dev, ok := m.Device(id, false)
	if !ok {
		return "", false
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := m.runner.Run(ctx, "", "ip", "-j", "-4", "address", "show", "dev", dev)
	var links []struct {
		Addresses []struct {
			Local  string `json:"local"`
			Prefix int    `json:"prefixlen"`
			Scope  string `json:"scope"`
		} `json:"addr_info"`
	}
	if err != nil || json.Unmarshal([]byte(out), &links) != nil {
		return "", false
	}
	for _, link := range links {
		for _, addr := range link.Addresses {
			if addr.Scope == "global" {
				return addr.Local + "/" + strconv.Itoa(addr.Prefix), true
			}
		}
	}
	return "", false
}
func (m *Manager) saveConfig(cfg Config) error {
	file := configFile
	if len(cfg.Networks) > 0 {
		file = topologyFile
	}
	return m.store.WriteJSON(file, cfg)
}

// ConfirmFrom checks the socket's actual local address, never Host/Forwarded
// headers. An old management connection cannot commit a changed address.
func (m *Manager) ConfirmFrom(ctx context.Context, local net.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.pending == nil {
		return ErrNoPending
	}
	if m.reverting {
		return ErrReverting
	}
	if len(m.pending.Networks) == 0 {
		if err := m.saveConfig(*m.pending); err != nil {
			return err
		}
	} else {
		if local == nil {
			return fmt.Errorf("%w: local management connection is unknown", ErrInvalid)
		}
		host, _, err := net.SplitHostPort(local.String())
		ip, errIP := netip.ParseAddr(host)
		if err != nil || errIP != nil {
			return ErrInvalid
		}
		ip = ip.Unmap()
		allowed := false
		for _, n := range m.pending.Networks {
			if !n.Management {
				continue
			}
			for _, addr := range []string{n.Address, n.Address6} {
				if p, e := netip.ParsePrefix(addr); e == nil && p.Addr().Unmap() == ip {
					allowed = true
				}
			}
			if n.IPv4 == "dhcp" || n.IPv6 == "auto" || n.IPv6 == "dhcp" {
				out, e := m.runner.Run(ctx, "", "ip", "-j", "address", "show", "dev", networkDevice(n))
				if e == nil {
					var links []struct {
						Addresses []struct {
							Local string `json:"local"`
						} `json:"addr_info"`
					}
					if json.Unmarshal([]byte(out), &links) == nil {
						for _, link := range links {
							for _, a := range link.Addresses {
								if parsed, e := netip.ParseAddr(a.Local); e == nil && parsed.Unmap() == ip {
									allowed = true
								}
							}
						}
					}
				}
			}
		}
		if !allowed {
			return fmt.Errorf("%w: open the new management address before confirming", ErrInvalid)
		}
		if err := m.store.WriteJSON(lastGoodFile, m.before); err != nil {
			return err
		}
		if err := m.saveConfig(*m.pending); err != nil {
			return err
		}
	}
	if m.timer != nil {
		m.timer.Stop()
	}
	m.active, m.pending, m.configured = cloneTopology(*m.pending), nil, true
	return nil
}

// Connections returns only confirmed topology devices for workload attachment.
func (m *Manager) Connections() []LogicalNetwork {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded || m.pending != nil {
		return nil
	}
	return cloneTopology(m.active).Networks
}
func (m *Manager) Device(id string, needBridge bool) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.loaded || m.pending != nil {
		return "", false
	}
	for _, n := range m.active.Networks {
		if n.ID == id && (!needBridge || n.Bridge) {
			return networkDevice(n), true
		}
	}
	return "", false
}

func (m *Manager) AttachmentLease() (func(), error) {
	m.attachments.RLock()
	m.mu.Lock()
	ready := m.loaded && m.pending == nil
	m.mu.Unlock()
	if !ready {
		m.attachments.RUnlock()
		return nil, ErrPending
	}
	return m.attachments.RUnlock, nil
}

// Recover is called only by the authenticated local-console command while the
// daemon is stopped. It never changes Docker/Tailscale rules or data volumes.
func (m *Manager) Recover(ctx context.Context, port string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Disabled {
		return ErrDisabled
	}
	var cfg Config
	if port == "" {
		if err := m.store.ReadJSON(lastGoodFile, &cfg); err != nil {
			if err = m.store.ReadJSON(configFile, &cfg); err != nil {
				return err
			}
		}
	} else {
		cfg = defaults()
		cfg.Ports = []string{port}
	}
	if err := m.validate(cfg); err != nil {
		return err
	}
	if err := m.apply(ctx, cfg); err != nil {
		return err
	}
	if err := m.saveConfig(cfg); err != nil {
		return err
	}
	if len(cfg.Networks) == 0 {
		if err := os.Remove(filepath.Join(m.store.Dir(), topologyFile)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	m.active, m.pending, m.loaded, m.configured = cfg, nil, true, true
	return nil
}
