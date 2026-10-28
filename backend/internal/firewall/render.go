package firewall

import (
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Table is the nftables table titand owns. NetBird's tables, Docker's
// iptables-nft tables and anything else are never touched.
const Table = "titanos"

// logPrefix starts every kernel log line of a dropped packet.
const logPrefix = "titanos-fw "

// App is a Docker app with the ports it publishes on the NAS.
type App struct {
	// Key is the Compose project or container:<name>.
	Key   string    `json:"key"`
	Name  string    `json:"name"`
	Ports []AppPort `json:"ports"`
	// HostNetwork apps publish nothing; their ports are the NAS's own and
	// need own rules.
	HostNetwork bool `json:"hostNetwork,omitempty"`
}

// AppPort is a published port on the NAS.
type AppPort struct {
	Port     int    `json:"port"`
	Protocol string `json:"protocol"`
}

// ruleset is everything the generated rules depend on.
type ruleset struct {
	cfg     Config
	web     WebPorts
	home    []netip.Prefix
	apps    []App
	blocked []netip.Prefix
	// admin is let in completely while a change is on trial.
	admin netip.Addr
}

// offScript removes the table (and creates it first, so that removing a
// table that is not there is no error).
func offScript() string {
	return "add table inet " + Table + "\ndelete table inet " + Table + "\n"
}

// render returns an nft script that replaces the table in one transaction.
func render(rs ruleset) string {
	if !rs.cfg.Enabled && len(rs.blocked) == 0 {
		return offScript()
	}
	var b strings.Builder
	b.WriteString("# Written by titand from /config/firewall/settings.json.\n")
	b.WriteString(offScript())
	b.WriteString("table inet " + Table + " {\n")
	v4, v6 := split(rs.blocked)
	writeSet(&b, "blocked4", "ipv4_addr", v4)
	writeSet(&b, "blocked6", "ipv6_addr", v6)
	b.WriteString("\tchain blocked {\n")
	b.WriteString("\t\tlimit rate 5/second burst 20 packets log prefix \"" + logPrefix + "blocked \" level info\n")
	b.WriteString("\t\tdrop\n\t}\n")

	if !rs.cfg.Enabled {
		// Only the automatic blocking: everything else stays open.
		b.WriteString("\tchain input {\n\t\ttype filter hook input priority filter; policy accept;\n")
		b.WriteString("\t\tiif \"lo\" accept\n")
		b.WriteString("\t\tip saddr @blocked4 jump blocked\n\t\tip6 saddr @blocked6 jump blocked\n\t}\n")
		b.WriteString("\tchain forward {\n\t\ttype filter hook forward priority filter; policy accept;\n")
		b.WriteString("\t\tct status dnat ip saddr @blocked4 jump blocked\n\t\tct status dnat ip6 saddr @blocked6 jump blocked\n\t}\n")
		b.WriteString("}\n")
		return b.String()
	}

	h4, h6 := split(rs.home)
	writeSet(&b, "home4", "ipv4_addr", h4)
	writeSet(&b, "home6", "ipv6_addr", h6)

	b.WriteString("\tchain input {\n\t\ttype filter hook input priority filter; policy accept;\n")
	for _, line := range []string{
		`iif "lo" accept`,
		"ip saddr @blocked4 jump blocked",
		"ip6 saddr @blocked6 jump blocked",
		"ct state established,related accept",
		"ct state invalid drop",
		// What IPv4/IPv6 need to work: errors (path MTU), neighbour and
		// router discovery, multicast listener queries.
		"meta l4proto icmp icmp type { destination-unreachable, time-exceeded, parameter-problem } accept",
		"meta l4proto ipv6-icmp icmpv6 type { destination-unreachable, packet-too-big, time-exceeded, parameter-problem, nd-router-solicit, nd-router-advert, nd-neighbor-solicit, nd-neighbor-advert, mld-listener-query } accept",
		// DHCP and DHCPv6 answers to this NAS as a client.
		"udp sport 67 udp dport 68 accept",
		"ip6 saddr fe80::/10 udp sport 547 udp dport 546 accept",
	} {
		b.WriteString("\t\t" + line + "\n")
	}
	// NetBird redirects DNS of its peers to its own forwarder on this NAS
	// (a routing peer); its own tables decide about those packets.
	b.WriteString("\t\tiifname \"wt0\" ct status dnat accept comment \"netbird\"\n")
	if rs.admin.IsValid() {
		fmt.Fprintf(&b, "\t\t%s saddr %s accept comment \"admin on trial\"\n", family(rs.admin), rs.admin)
	}
	writeZoneJumps(&b, "")
	b.WriteString("\t}\n")

	apps := slices.Clone(rs.apps)
	sort.Slice(apps, func(i, j int) bool { return apps[i].Key < apps[j].Key })
	services := Services(&rs.web)
	for _, zone := range AllZones {
		// Packets to the NAS itself.
		fmt.Fprintf(&b, "\tchain %s {\n", zone)
		for _, r := range rs.cfg.Rules {
			if line := ruleLine(r, zone, false); line != "" {
				b.WriteString("\t\t" + line + "\n")
			}
		}
		for _, s := range services {
			if !rs.cfg.Services[s.ID].Has(zone) {
				continue
			}
			for _, set := range s.Ports {
				if set.Protocol == "icmp" {
					fmt.Fprintf(&b, "\t\tmeta l4proto icmp icmp type echo-request accept comment %q\n", s.ID)
					fmt.Fprintf(&b, "\t\tmeta l4proto ipv6-icmp icmpv6 type echo-request accept comment %q\n", s.ID)
					continue
				}
				fmt.Fprintf(&b, "\t\t%s dport %s accept comment %q\n", set.Protocol, portSet(set.Ports), s.ID)
			}
		}
		// Ports Docker's userland proxy answers on the NAS itself (IPv6,
		// and IPv4 where Docker does not DNAT).
		for _, app := range apps {
			if !appZones(rs.cfg, app.Key).Has(zone) {
				continue
			}
			for _, proto := range []string{"tcp", "udp"} {
				if ports := appPorts(app, proto); len(ports) > 0 {
					fmt.Fprintf(&b, "\t\t%s dport %s accept comment %q\n", proto, portSet(ports), comment("app "+app.Key))
				}
			}
		}
		fmt.Fprintf(&b, "\t\tgoto drop_%s\n\t}\n", zone)
		// Broadcasts and multicasts of other devices are dropped quietly:
		// they would fill the list of blocked packets.
		fmt.Fprintf(&b, "\tchain drop_%s {\n\t\tfib daddr type { broadcast, multicast } drop\n", zone)
		fmt.Fprintf(&b, "\t\tlimit rate 5/second burst 20 packets log prefix \"%s%s \" level info\n\t\tdrop\n\t}\n", logPrefix, zone)
	}

	// Published ports of Docker apps: Docker's DNAT has rewritten the
	// destination, ct original proto-dst is still the port on the NAS.
	// Everything else that is forwarded (containers going out, VMs and
	// LXC on br0 with br_netfilter, Tailscale routes) is left alone.
	b.WriteString("\tchain forward {\n\t\ttype filter hook forward priority filter; policy accept;\n")
	b.WriteString("\t\tct status dnat ip saddr @blocked4 jump blocked\n\t\tct status dnat ip6 saddr @blocked6 jump blocked\n")
	b.WriteString("\t\tct state established,related accept\n")
	b.WriteString("\t\tct state new ct status dnat jump published\n\t}\n")
	b.WriteString("\tchain published {\n")
	writeZoneJumps(&b, "app_")
	b.WriteString("\t}\n")
	for _, zone := range AllZones {
		fmt.Fprintf(&b, "\tchain app_%s {\n", zone)
		for _, r := range rs.cfg.Rules {
			if line := ruleLine(r, zone, true); line != "" {
				b.WriteString("\t\t" + line + "\n")
			}
		}
		for _, app := range apps {
			verdict := "accept"
			if !appZones(rs.cfg, app.Key).Has(zone) {
				verdict = "goto drop_app_" + string(zone)
			}
			for _, proto := range []string{"tcp", "udp"} {
				if ports := appPorts(app, proto); len(ports) > 0 {
					fmt.Fprintf(&b, "\t\tmeta l4proto %s ct original proto-dst %s %s comment %q\n", proto, portSet(ports), verdict, comment("app "+app.Key))
				}
			}
		}
		if rs.cfg.NewApps.Has(zone) {
			b.WriteString("\t\taccept comment \"new apps\"\n")
		} else {
			fmt.Fprintf(&b, "\t\tgoto drop_app_%s\n", zone)
		}
		b.WriteString("\t}\n")
		fmt.Fprintf(&b, "\tchain drop_app_%s {\n\t\tlimit rate 5/second burst 20 packets log prefix \"%sapp-%s \" level info\n\t\tdrop\n\t}\n", zone, logPrefix, zone)
	}
	b.WriteString("}\n")
	return b.String()
}

// writeZoneJumps sends a packet on to the chain of its zone.
func writeZoneJumps(b *strings.Builder, prefix string) {
	quoted := make([]string, len(vpnInterfaces))
	for i, name := range vpnInterfaces {
		quoted[i] = strconv.Quote(name)
	}
	fmt.Fprintf(b, "\t\tiifname { %s } goto %s%s\n", strings.Join(quoted, ", "), prefix, VPN)
	for _, p := range vpnPrefixes {
		fmt.Fprintf(b, "\t\t%s saddr %s goto %s%s\n", family(p.Addr()), p, prefix, VPN)
	}
	fmt.Fprintf(b, "\t\tip saddr @home4 goto %s%s\n\t\tip6 saddr @home6 goto %s%s\n", prefix, Home, prefix, Home)
	fmt.Fprintf(b, "\t\tgoto %s%s\n", prefix, Internet)
}

// ruleLine is an own rule in the chain of a zone; "" if it does not apply
// there. forward rules match the original (published) port.
func ruleLine(r Rule, zone Zone, forward bool) string {
	if r.Disabled || (r.Zone != "" && r.Zone != zone) {
		return ""
	}
	var parts []string
	if r.Source != "" {
		source, err := parseSource(r.Source)
		if err != nil {
			return ""
		}
		parts = append(parts, family(source.Addr())+" saddr "+prefixText(source))
	}
	protocols := r.Protocol
	if protocols == "any" {
		protocols = "{ tcp, udp }"
	}
	switch {
	case r.Ports != "" && forward:
		parts = append(parts, "meta l4proto "+protocols+" ct original proto-dst "+r.Ports)
	case r.Ports != "" && r.Protocol == "any":
		parts = append(parts, "meta l4proto "+protocols+" th dport "+r.Ports)
	case r.Ports != "":
		parts = append(parts, r.Protocol+" dport "+r.Ports)
	case r.Protocol != "any":
		parts = append(parts, "meta l4proto "+r.Protocol)
	}
	verdict := "accept"
	if r.Action == "deny" {
		verdict = "goto drop_" + string(zone)
		if forward {
			verdict = "goto drop_app_" + string(zone)
		}
	}
	parts = append(parts, verdict)
	if r.Comment != "" {
		parts = append(parts, "comment "+strconv.Quote(r.Comment))
	}
	return strings.Join(parts, " ")
}

func appZones(cfg Config, key string) Zones {
	if zones, ok := cfg.Apps[key]; ok {
		return zones
	}
	return cfg.NewApps
}

func appPorts(app App, protocol string) []string {
	var ports []int
	for _, p := range app.Ports {
		if p.Protocol == protocol && p.Port > 0 && p.Port <= 65535 && !slices.Contains(ports, p.Port) {
			ports = append(ports, p.Port)
		}
	}
	slices.Sort(ports)
	out := make([]string, len(ports))
	for i, p := range ports {
		out[i] = strconv.Itoa(p)
	}
	return out
}

func portSet(ports []string) string {
	if len(ports) == 1 {
		return ports[0]
	}
	return "{ " + strings.Join(ports, ", ") + " }"
}

func comment(text string) string {
	if len(text) > 100 {
		text = text[:100]
	}
	return text
}

func family(addr netip.Addr) string {
	if addr.Is4() {
		return "ip"
	}
	return "ip6"
}

// prefixText writes single addresses without /32 or /128.
func prefixText(p netip.Prefix) string {
	if p.IsSingleIP() {
		return p.Addr().String()
	}
	return p.String()
}

func split(list []netip.Prefix) (v4, v6 []string) {
	for _, p := range minimal(slices.Clone(list)) {
		if p.Addr().Is4() {
			v4 = append(v4, prefixText(p))
		} else {
			v6 = append(v6, prefixText(p))
		}
	}
	return v4, v6
}

func writeSet(b *strings.Builder, name, kind string, elements []string) {
	fmt.Fprintf(b, "\tset %s {\n\t\ttype %s\n\t\tflags interval\n\t\tauto-merge\n", name, kind)
	if len(elements) > 0 {
		fmt.Fprintf(b, "\t\telements = { %s }\n", strings.Join(elements, ", "))
	}
	b.WriteString("\t}\n")
}
