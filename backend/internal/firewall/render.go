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
	// blocklist are the addresses of the v64 blocklist of IPv64 (package
	// ipv64), dropped in the internet zone only.
	blocklist []netip.Prefix
	// admin is let in completely while a change is on trial.
	admin netip.Addr
	// networks of the NAS the zones are made of.
	networks []Network
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
	l4, l6 := split(rs.blocklist)
	writeSet(&b, "blocklist4", "ipv4_addr", l4)
	writeSet(&b, "blocklist6", "ipv6_addr", l6)

	mems := members(rs.cfg, rs.networks)
	// The zones with chains: the three made of addresses and those with
	// networks or sources, in the order of the settings.
	var zones []Zone
	for _, z := range rs.cfg.Zones {
		if slices.Contains(legacyZones, z.ID) || slices.ContainsFunc(mems, func(m member) bool { return m.zone == z.ID }) {
			zones = append(zones, z.ID)
		}
	}

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
	writeZoneJumps(&b, mems, "")
	b.WriteString("\t}\n")

	apps := slices.Clone(rs.apps)
	sort.Slice(apps, func(i, j int) bool { return apps[i].Key < apps[j].Key })
	services := Services(&rs.web)
	for _, zone := range zones {
		// Packets to the NAS itself.
		cell := rs.cfg.Cell(zone, ToNAS)
		drop := "goto drop_" + string(zone)
		fmt.Fprintf(&b, "\tchain %s {\n", zone)
		if zone == Internet {
			b.WriteString("\t\tip saddr @blocklist4 goto drop_internet comment \"v64 blocklist\"\n\t\tip6 saddr @blocklist6 goto drop_internet comment \"v64 blocklist\"\n")
		}
		for _, p := range cell.Policies {
			for _, line := range nasLines(p, services, drop) {
				b.WriteString("\t\t" + line + "\n")
			}
		}
		// Ports Docker's userland proxy answers on the NAS itself (IPv6,
		// and IPv4 where Docker does not DNAT).
		appCell := rs.cfg.Cell(zone, ToApps)
		for _, app := range apps {
			if !appAllowed(appCell, app.Key) {
				continue
			}
			for _, proto := range []string{"tcp", "udp"} {
				if ports := appPorts(app, proto); len(ports) > 0 {
					fmt.Fprintf(&b, "\t\t%s dport %s accept comment %q\n", proto, portSet(ports), comment("app "+app.Key))
				}
			}
		}
		if cell.Default == Allow {
			b.WriteString("\t\taccept\n\t}\n")
		} else {
			fmt.Fprintf(&b, "\t\t%s\n\t}\n", drop)
		}
		// Broadcasts and multicasts of other devices are dropped quietly:
		// they would fill the list of blocked packets.
		fmt.Fprintf(&b, "\tchain drop_%s {\n\t\tfib daddr type { broadcast, multicast } drop\n", zone)
		fmt.Fprintf(&b, "\t\tlimit rate 5/second burst 20 packets log prefix \"%s%s \" level info\n\t\tdrop\n\t}\n", logPrefix, zone)
	}

	// Published ports of Docker apps: Docker's DNAT has rewritten the
	// destination, ct original proto-dst is still the port on the NAS.
	// New connections out of networks behind the NAS (Docker, VM/LXC NAT
	// bridges in a zone) follow the cells between zones; everything else
	// that is forwarded (VMs and LXC on br0 with br_netfilter, Tailscale
	// routes) is left alone.
	routed := slices.ContainsFunc(mems, func(m member) bool { return len(m.hostedDevices()) > 0 })
	b.WriteString("\tchain forward {\n\t\ttype filter hook forward priority filter; policy accept;\n")
	b.WriteString("\t\tct status dnat ip saddr @blocked4 jump blocked\n\t\tct status dnat ip6 saddr @blocked6 jump blocked\n")
	b.WriteString("\t\tct state established,related accept\n")
	b.WriteString("\t\tct state new ct status dnat jump published\n")
	if routed {
		b.WriteString("\t\tct state new jump routed\n")
	}
	b.WriteString("\t}\n")
	b.WriteString("\tchain published {\n")
	writeZoneJumps(&b, mems, "app_")
	b.WriteString("\t}\n")
	for _, zone := range zones {
		cell := rs.cfg.Cell(zone, ToApps)
		drop := "goto drop_app_" + string(zone)
		fmt.Fprintf(&b, "\tchain app_%s {\n", zone)
		if zone == Internet {
			b.WriteString("\t\tip saddr @blocklist4 goto drop_app_internet comment \"v64 blocklist\"\n\t\tip6 saddr @blocklist6 goto drop_app_internet comment \"v64 blocklist\"\n")
		}
		for _, p := range cell.Policies {
			for _, line := range appLines(p, apps, drop) {
				b.WriteString("\t\t" + line + "\n")
			}
		}
		// Apps without a policy of their own follow the default; listed so
		// that the rule set says which ports it saw.
		verdict := "accept"
		if cell.Default != Allow {
			verdict = drop
		}
		for _, app := range apps {
			if slices.ContainsFunc(cell.Policies, func(p Policy) bool { return !p.Disabled && p.App == app.Key }) {
				continue
			}
			for _, proto := range []string{"tcp", "udp"} {
				if ports := appPorts(app, proto); len(ports) > 0 {
					fmt.Fprintf(&b, "\t\tmeta l4proto %s ct original proto-dst %s %s comment %q\n", proto, portSet(ports), verdict, comment("app "+app.Key))
				}
			}
		}
		if cell.Default == Allow {
			b.WriteString("\t\taccept comment \"new apps\"\n")
		} else {
			fmt.Fprintf(&b, "\t\t%s\n", drop)
		}
		b.WriteString("\t}\n")
		fmt.Fprintf(&b, "\tchain drop_app_%s {\n\t\tlimit rate 5/second burst 20 packets log prefix \"%sapp-%s \" level info\n\t\tdrop\n\t}\n", zone, logPrefix, zone)
	}
	if routed {
		writeRouted(&b, rs.cfg, mems)
	}
	b.WriteString("}\n")
	return b.String()
}

// writeRouted writes the cells between zones for traffic out of networks
// behind the NAS: routed sends it to the chain of its source zone, from_
// <zone> finds the destination zone (by outgoing interface, then by
// address), fwd_<source>_<destination> holds the policies of the cell.
// Within one network (same bridge) nothing is filtered.
func writeRouted(b *strings.Builder, cfg Config, mems []member) {
	var same []string
	for _, m := range mems {
		for _, dev := range m.hostedDevices() {
			same = append(same, fmt.Sprintf("%q . %q", dev, dev))
		}
	}
	b.WriteString("\tchain routed {\n")
	fmt.Fprintf(b, "\t\tiifname . oifname { %s } accept comment \"same network\"\n", strings.Join(same, ", "))
	var sources []member
	for _, m := range mems {
		if devs := m.hostedDevices(); len(devs) > 0 {
			fmt.Fprintf(b, "\t\tiifname %s goto from_%s\n", nameSet(devs), m.zone)
			sources = append(sources, m)
		}
	}
	b.WriteString("\t}\n")
	// Destinations in the order a destination address is matched.
	dests := []Zone{}
	for _, m := range mems {
		dests = append(dests, m.zone)
	}
	dests = append(dests, VPN, Home, Internet)
	for _, src := range sources {
		fmt.Fprintf(b, "\tchain from_%s {\n", src.zone)
		for _, d := range mems {
			target := fmt.Sprintf("goto fwd_%s_%s", src.zone, d.zone)
			if devs := d.devices(); len(devs) > 0 {
				fmt.Fprintf(b, "\t\toifname %s %s\n", nameSet(devs), target)
			}
			v4, v6 := split(d.sources)
			if len(v4) > 0 {
				fmt.Fprintf(b, "\t\tip daddr %s %s\n", portSet(v4), target)
			}
			if len(v6) > 0 {
				fmt.Fprintf(b, "\t\tip6 daddr %s %s\n", portSet(v6), target)
			}
		}
		fmt.Fprintf(b, "\t\toifname %s goto fwd_%s_%s\n", nameSet(vpnInterfaces), src.zone, VPN)
		for _, p := range vpnPrefixes {
			fmt.Fprintf(b, "\t\t%s daddr %s goto fwd_%s_%s\n", family(p.Addr()), p, src.zone, VPN)
		}
		fmt.Fprintf(b, "\t\tip daddr @home4 goto fwd_%s_%s\n\t\tip6 daddr @home6 goto fwd_%s_%s\n", src.zone, Home, src.zone, Home)
		fmt.Fprintf(b, "\t\tgoto fwd_%s_%s\n\t}\n", src.zone, Internet)
		drop := "goto drop_fwd_" + string(src.zone)
		for _, dst := range dests {
			cell := cfg.Cell(src.zone, string(dst))
			fmt.Fprintf(b, "\tchain fwd_%s_%s {\n", src.zone, dst)
			for _, p := range cell.Policies {
				if line := forwardLine(p, drop); line != "" {
					b.WriteString("\t\t" + line + "\n")
				}
			}
			if cell.Default == Allow {
				b.WriteString("\t\taccept\n\t}\n")
			} else {
				fmt.Fprintf(b, "\t\t%s\n\t}\n", drop)
			}
		}
		fmt.Fprintf(b, "\tchain drop_fwd_%s {\n\t\tlimit rate 5/second burst 20 packets log prefix \"%sfwd-%s \" level info\n\t\tdrop\n\t}\n", src.zone, logPrefix, src.zone)
	}
}

// writeZoneJumps sends a packet on to the chain of its zone: own zones
// and Docker by interface (and subnet) or source, then VPN, home network,
// internet.
func writeZoneJumps(b *strings.Builder, mems []member, prefix string) {
	for _, m := range mems {
		target := "goto " + prefix + string(m.zone)
		for _, bind := range m.bindings {
			v4, v6 := split(bind.subnets)
			if len(v4) == 0 && len(v6) == 0 {
				fmt.Fprintf(b, "\t\tiifname %q %s\n", bind.device, target)
				continue
			}
			if len(v4) > 0 {
				fmt.Fprintf(b, "\t\tiifname %q ip saddr %s %s\n", bind.device, portSet(v4), target)
			}
			if len(v6) > 0 {
				fmt.Fprintf(b, "\t\tiifname %q ip6 saddr %s %s\n", bind.device, portSet(v6), target)
			}
		}
		v4, v6 := split(m.sources)
		if len(v4) > 0 {
			fmt.Fprintf(b, "\t\tip saddr %s %s\n", portSet(v4), target)
		}
		if len(v6) > 0 {
			fmt.Fprintf(b, "\t\tip6 saddr %s %s\n", portSet(v6), target)
		}
	}
	fmt.Fprintf(b, "\t\tiifname %s goto %s%s\n", nameSet(vpnInterfaces), prefix, VPN)
	for _, p := range vpnPrefixes {
		fmt.Fprintf(b, "\t\t%s saddr %s goto %s%s\n", family(p.Addr()), p, prefix, VPN)
	}
	fmt.Fprintf(b, "\t\tip saddr @home4 goto %s%s\n\t\tip6 saddr @home6 goto %s%s\n", prefix, Home, prefix, Home)
	fmt.Fprintf(b, "\t\tgoto %s%s\n", prefix, Internet)
}

func nameSet(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = strconv.Quote(name)
	}
	if len(quoted) == 1 {
		return quoted[0]
	}
	return "{ " + strings.Join(quoted, ", ") + " }"
}

// sourcePart is the match of a source (or destination) address; ok false
// if the text is no address.
func addressPart(text, dir string) (string, bool) {
	if text == "" {
		return "", true
	}
	p, err := parseSource(text)
	if err != nil {
		return "", false
	}
	return family(p.Addr()) + " " + dir + " " + prefixText(p), true
}

// nasLines are the lines of a policy in the chain of a zone to the NAS.
func nasLines(p Policy, services []Service, drop string) []string {
	if p.Disabled {
		return nil
	}
	source, ok := addressPart(p.Source, "saddr")
	if !ok {
		return nil
	}
	verdict := "accept"
	if p.Action == Deny {
		verdict = drop
	}
	join := func(parts ...string) string {
		var out []string
		for _, part := range parts {
			if part != "" {
				out = append(out, part)
			}
		}
		return strings.Join(out, " ")
	}
	if p.Service != "" {
		text := p.Comment
		if text == "" {
			text = p.Service
		}
		var lines []string
		for _, set := range policyPortSets(p, services) {
			if set.Protocol == "icmp" {
				lines = append(lines,
					join(source, "meta l4proto icmp icmp type echo-request", verdict, "comment "+strconv.Quote(text)),
					join(source, "meta l4proto ipv6-icmp icmpv6 type echo-request", verdict, "comment "+strconv.Quote(text)))
				continue
			}
			lines = append(lines, join(source, set.Protocol+" dport "+portSet(set.Ports), verdict, "comment "+strconv.Quote(text)))
		}
		return lines
	}
	return []string{join(source, portMatch(p, false), verdict, commentPart(p.Comment))}
}

// appLines are the lines of a policy in the chain of a zone to the apps:
// an app's published ports, or own ports by their original destination.
func appLines(p Policy, apps []App, drop string) []string {
	if p.Disabled {
		return nil
	}
	source, ok := addressPart(p.Source, "saddr")
	if !ok {
		return nil
	}
	verdict := "accept"
	if p.Action == Deny {
		verdict = drop
	}
	prefix := ""
	if source != "" {
		prefix = source + " "
	}
	if p.App != "" {
		i := slices.IndexFunc(apps, func(a App) bool { return a.Key == p.App })
		if i < 0 {
			return nil
		}
		var lines []string
		for _, proto := range []string{"tcp", "udp"} {
			if ports := appPorts(apps[i], proto); len(ports) > 0 {
				lines = append(lines, fmt.Sprintf("%smeta l4proto %s ct original proto-dst %s %s comment %q", prefix, proto, portSet(ports), verdict, comment("app "+p.App)))
			}
		}
		return lines
	}
	line := prefix
	if match := portMatch(p, true); match != "" {
		line += match + " "
	}
	line += verdict
	if c := commentPart(p.Comment); c != "" {
		line += " " + c
	}
	return []string{line}
}

// forwardLine is a policy of a cell between zones.
func forwardLine(p Policy, drop string) string {
	if p.Disabled {
		return ""
	}
	source, ok := addressPart(p.Source, "saddr")
	if !ok {
		return ""
	}
	dest, ok := addressPart(p.Destination, "daddr")
	if !ok {
		return ""
	}
	if source != "" && dest != "" && strings.Fields(source)[0] != strings.Fields(dest)[0] {
		// IPv4 source and IPv6 destination never match together.
		return ""
	}
	var parts []string
	for _, part := range []string{source, dest, portMatch(p, false)} {
		if part != "" {
			parts = append(parts, part)
		}
	}
	verdict := "accept"
	if p.Action == Deny {
		verdict = drop
	}
	parts = append(parts, verdict)
	if c := commentPart(p.Comment); c != "" {
		parts = append(parts, c)
	}
	return strings.Join(parts, " ")
}

// portMatch is the protocol and ports of a policy; forward matches the
// original (published) port.
func portMatch(p Policy, forward bool) string {
	protocols := p.Protocol
	if protocols == "any" {
		protocols = "{ tcp, udp }"
	}
	switch {
	case p.Ports != "" && forward:
		return "meta l4proto " + protocols + " ct original proto-dst " + p.Ports
	case p.Ports != "" && p.Protocol == "any":
		return "meta l4proto " + protocols + " th dport " + p.Ports
	case p.Ports != "":
		return p.Protocol + " dport " + p.Ports
	case p.Protocol != "any":
		return "meta l4proto " + p.Protocol
	}
	return ""
}

func commentPart(text string) string {
	if text == "" {
		return ""
	}
	return "comment " + strconv.Quote(text)
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
