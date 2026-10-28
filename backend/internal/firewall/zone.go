package firewall

import (
	"net/netip"
	"slices"
)

// VPN: the interfaces and address ranges of Tailscale and NetBird, and the
// WireGuard server of the NAS (package wgserver).
var (
	vpnInterfaces = []string{"tailscale0", "wt0", "titanoswg"}
	vpnPrefixes   = []netip.Prefix{netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("fd7a:115c:a1e0::/48")}
	// privatePrefixes always count as home network: RFC 1918, link-local,
	// unique local IPv6.
	privatePrefixes = []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("172.16.0.0/12"),
		netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("169.254.0.0/16"),
		netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10"),
	}
)

// homeNetworks are the private ranges plus the networks of the NAS's own
// interfaces (which carry its public IPv6 prefix), without ranges that lie
// inside another and without the VPN ranges.
func homeNetworks(own []string) []netip.Prefix {
	all := slices.Clone(privatePrefixes)
	for _, text := range own {
		prefix, err := netip.ParsePrefix(text)
		if err != nil {
			continue
		}
		prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()).Masked()
		if prefix.Addr().Is4() && prefix.Bits() > 32 {
			continue
		}
		if slices.ContainsFunc(vpnPrefixes, func(v netip.Prefix) bool { return v.Overlaps(prefix) }) {
			continue
		}
		// A default route or a whole address family is no home network.
		if prefix.Bits() < 8 {
			continue
		}
		all = append(all, prefix)
	}
	return minimal(all)
}

// minimal drops duplicates and prefixes inside others and sorts the rest.
func minimal(list []netip.Prefix) []netip.Prefix {
	slices.SortFunc(list, func(a, b netip.Prefix) int {
		if c := a.Addr().Compare(b.Addr()); c != 0 {
			return c
		}
		return a.Bits() - b.Bits()
	})
	out := []netip.Prefix{}
	for _, p := range list {
		if slices.ContainsFunc(out, func(o netip.Prefix) bool { return o.Bits() <= p.Bits() && o.Contains(p.Addr()) }) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// classify puts a source address into its zone, the way the rules do
// (without the interface: the VPN interfaces only carry VPN addresses).
func classify(addr netip.Addr, home []netip.Prefix) Zone {
	addr = addr.Unmap().WithZone("")
	if addr.IsLoopback() {
		// The NAS itself; always let in before any zone.
		return Home
	}
	for _, p := range vpnPrefixes {
		if p.Contains(addr) {
			return VPN
		}
	}
	for _, p := range home {
		if p.Contains(addr) {
			return Home
		}
	}
	return Internet
}

// sharedByName are the services on the same port (443) that titand tells
// apart by the name asked for (appproxy.Manager.Gate → AllowHTTP).
var sharedByName = []string{"webui", "apps"}

// policyPortSets are the ports a service policy stands for in the rules.
// A deny of the web UI or of the HTTPS names leaves out the port both
// share: nftables lets it in, titand refuses the name that is denied.
func policyPortSets(p Policy, services []Service) []PortSet {
	s, ok := serviceByID(services, p.Service)
	if !ok {
		return nil
	}
	if p.Action != Deny || !slices.Contains(sharedByName, p.Service) {
		return s.Ports
	}
	var shared []PortSet
	for _, other := range sharedByName {
		if o, ok := serviceByID(services, other); ok && other != p.Service {
			shared = append(shared, o.Ports...)
		}
	}
	var out []PortSet
	for _, set := range s.Ports {
		var ports []string
		for _, port := range set.Ports {
			if !slices.ContainsFunc(shared, func(o PortSet) bool { return o.Protocol == set.Protocol && slices.Contains(o.Ports, port) }) {
				ports = append(ports, port)
			}
		}
		if len(ports) > 0 {
			out = append(out, PortSet{Protocol: set.Protocol, Ports: ports})
		}
	}
	return out
}

// policyMatchesNAS tells whether a policy of a cell to the NAS applies to
// a connection. service is the service asked for by titand (webui, apps) or
// empty for a plain port: a policy for one of the services on port 443
// then only matches its own name.
func policyMatchesNAS(p Policy, services []Service, addr netip.Addr, protocol string, port int, service string) bool {
	if p.Disabled {
		return false
	}
	if p.Source != "" {
		source, err := parseSource(p.Source)
		if err != nil || !source.Contains(addr) {
			return false
		}
	}
	if p.Service != "" {
		if service != "" && slices.Contains(sharedByName, service) && slices.Contains(sharedByName, p.Service) {
			return p.Service == service
		}
		for _, set := range policyPortSets(p, services) {
			if set.Protocol != protocol {
				continue
			}
			for _, ports := range set.Ports {
				if low, high, err := parsePorts(ports); err == nil && port >= low && port <= high {
					return true
				}
			}
		}
		return false
	}
	if p.Protocol != "any" && p.Protocol != protocol {
		return false
	}
	if p.Ports != "" {
		if protocol != "tcp" && protocol != "udp" {
			return false
		}
		low, high, err := parsePorts(p.Ports)
		if err != nil || port < low || port > high {
			return false
		}
	}
	return true
}

// decide evaluates the cell of a zone to the NAS the way the rule set
// does for a new connection from addr: policies in order, then the
// default.
func decide(cfg Config, services []Service, zone Zone, addr netip.Addr, protocol string, port int, service string) bool {
	c := cfg.Cell(zone, ToNAS)
	for _, p := range c.Policies {
		if policyMatchesNAS(p, services, addr, protocol, port, service) {
			return p.Action == Allow
		}
	}
	return c.Default == Allow
}
