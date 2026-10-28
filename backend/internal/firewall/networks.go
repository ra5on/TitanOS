package firewall

import (
	"net/netip"
	"slices"
	"sort"
	"strings"
)

// Kinds of networks of the NAS.
const (
	// KindNetwork is a logical network, VLAN or bridge of Settings ›
	// Network (br0, tn<id>, tv<id>, tb<id>).
	KindNetwork = "network"
	// KindDocker is a Docker bridge network (docker0, br-<id>).
	KindDocker = "docker"
	// KindVM is a NAT bridge of VMs or LXC (virbr*, lxcbr*).
	KindVM = "vm"
	// KindDockerLAN is a macvlan/ipvlan Docker network: its containers have
	// addresses of their own in the LAN and do not pass the NAS's own
	// stack. Shown, but not assignable.
	KindDockerLAN = "dockerlan"
)

// Network is a network the NAS has (or had: Missing).
type Network struct {
	// ID is net:<id>, docker:<name> or vm:<device>; what zones refer to.
	ID   string `json:"id"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	// Device is the interface on the NAS; empty for macvlan/ipvlan.
	Device string `json:"device,omitempty"`
	// Parent is the interface a macvlan/ipvlan network hangs on.
	Parent  string   `json:"parent,omitempty"`
	Subnets []string `json:"subnets"`
	// Missing is a network an own zone refers to that is not there now; it
	// stays in the zone.
	Missing bool `json:"missing,omitempty"`
}

// assignable networks can be put into a zone.
func (n Network) assignable() bool { return n.Kind != KindDockerLAN && deviceName(n.Device) }

func (n Network) prefixes() []netip.Prefix {
	var out []netip.Prefix
	for _, s := range n.Subnets {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, netip.PrefixFrom(p.Addr().Unmap(), p.Bits()).Masked())
		}
	}
	return out
}

func sameNetworks(a, b []Network) bool {
	return slices.EqualFunc(a, b, func(x, y Network) bool {
		return x.ID == y.ID && x.Kind == y.Kind && x.Name == y.Name && x.Device == y.Device && x.Parent == y.Parent && slices.Equal(x.Subnets, y.Subnets)
	})
}

func sortNetworks(list []Network) {
	order := map[string]int{KindNetwork: 0, KindDocker: 1, KindDockerLAN: 2, KindVM: 3}
	sort.SliceStable(list, func(i, j int) bool {
		if order[list[i].Kind] != order[list[j].Kind] {
			return order[list[i].Kind] < order[list[j].Kind]
		}
		return list[i].ID < list[j].ID
	})
}

// binding is a network of a zone as the rules match it: by interface,
// and by its subnets where known (so that titand, which only sees the
// address, puts a sender into the same zone as the rules).
type binding struct {
	device  string
	subnets []netip.Prefix
	hosted  bool
}

// member is what makes up a zone, in the order zones are matched.
type member struct {
	zone     Zone
	bindings []binding
	sources  []netip.Prefix
}

// members lists the own zones (in their order) and then the zone Docker,
// with their networks and sources; zones without either are left out.
// The built-in zones home network, VPN and internet come after them and
// are made of addresses.
func members(cfg Config, networks []Network) []member {
	byID := map[string]Network{}
	for _, n := range networks {
		if n.assignable() {
			byID[n.ID] = n
		}
	}
	taken := map[string]bool{}
	var out []member
	for _, z := range cfg.Zones {
		if z.Builtin {
			continue
		}
		m := member{zone: z.ID}
		for _, id := range z.Networks {
			taken[id] = true
			if n, ok := byID[id]; ok {
				m.bindings = append(m.bindings, binding{device: n.Device, subnets: n.prefixes(), hosted: isHosted(id)})
			}
		}
		for _, s := range z.Sources {
			if p, err := parseSource(s); err == nil {
				m.sources = append(m.sources, p)
			}
		}
		if len(m.bindings) > 0 || len(m.sources) > 0 {
			out = append(out, m)
		}
	}
	docker := member{zone: Docker}
	for _, n := range networks {
		if n.Kind == KindDocker && n.assignable() && !taken[n.ID] {
			docker.bindings = append(docker.bindings, binding{device: n.Device, subnets: n.prefixes(), hosted: true})
		}
	}
	if len(docker.bindings) > 0 {
		out = append(out, docker)
	}
	return out
}

// hostedDevices are the interfaces of networks behind the NAS in a zone.
func (m member) hostedDevices() []string {
	var out []string
	for _, b := range m.bindings {
		if b.hosted && !slices.Contains(out, b.device) {
			out = append(out, b.device)
		}
	}
	return out
}

// devices are all interfaces of a zone.
func (m member) devices() []string {
	var out []string
	for _, b := range m.bindings {
		if !slices.Contains(out, b.device) {
			out = append(out, b.device)
		}
	}
	return out
}

// classifier puts a sender into its zone the way the rules do.
type classifier struct {
	members []member
	home    []netip.Prefix
}

func newClassifier(cfg Config, networks []Network, home []netip.Prefix) classifier {
	return classifier{members: members(cfg, networks), home: home}
}

// zone of an address: own zones (subnets of their networks, sources),
// Docker, VPN, home network, internet. A network known only by its
// interface (no subnet) cannot be told by address; it is matched by the
// rules alone.
func (c classifier) zone(addr netip.Addr) Zone {
	addr = addr.Unmap().WithZone("")
	if addr.IsLoopback() {
		// The NAS itself; always let in before any zone.
		return Home
	}
	for _, m := range c.members {
		for _, b := range m.bindings {
			for _, p := range b.subnets {
				if p.Contains(addr) {
					return m.zone
				}
			}
		}
		for _, p := range m.sources {
			if p.Contains(addr) {
				return m.zone
			}
		}
	}
	return classify(addr, c.home)
}

// deviceName is a safe interface name for the rules.
func deviceName(name string) bool {
	if name == "" || len(name) > 15 {
		return false
	}
	return !strings.ContainsAny(name, "\"\\ \t\n/{}*")
}
