package firewall

import (
	"net/netip"
	"slices"
)

// VPN: the interfaces and address ranges of Tailscale and NetBird.
var (
	vpnInterfaces = []string{"tailscale0", "wt0"}
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

// ruleMatches tells whether an own rule applies to a connection.
func ruleMatches(r Rule, addr netip.Addr, protocol string, port int) bool {
	if r.Source != "" {
		source, err := parseSource(r.Source)
		if err != nil || !source.Contains(addr) {
			return false
		}
	}
	if r.Protocol != "any" && r.Protocol != protocol {
		return false
	}
	if r.Ports != "" {
		if protocol != "tcp" && protocol != "udp" {
			return false
		}
		low, high, err := parsePorts(r.Ports)
		if err != nil || port < low || port > high {
			return false
		}
	}
	return true
}

// decide evaluates the own rules and then the service the way the rule
// set does for a new connection from addr in zone.
func decide(cfg Config, zone Zone, addr netip.Addr, protocol string, port int, service string) bool {
	for _, r := range cfg.Rules {
		if r.Disabled || (r.Zone != "" && r.Zone != zone) {
			continue
		}
		if ruleMatches(r, addr, protocol, port) {
			return r.Action == "allow"
		}
	}
	return cfg.Services[service].Has(zone)
}
