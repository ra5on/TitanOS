package network

import (
	"context"
	"encoding/json"
	"slices"
	"strconv"
	"time"
)

// Names given to what a legacy configuration becomes in the multi-network
// model. They match what the web UI used to suggest for the manual switch.
const (
	migratedNetwork = "admin"
	migratedBond    = "uplink"
	migratedName    = "LAN"
)

// migrateLegacy returns the multi-network form of a configuration that only
// has the flat fields of network.json: one preferred management network on
// the same ports, bond, VLAN and bridge, with the same addresses and name
// servers. uplink is the port used when the legacy file names none (every
// port on DHCP). A topology is returned unchanged.
//
// Nothing here is stored or applied: the result is what the page edits, and
// it only takes effect through Change, on trial, like any other change. The
// management bridge keeps the name br0, so VMs and system containers on the
// home network keep their connection.
func migrateLegacy(cfg Config, uplink string) Config {
	if len(cfg.Networks) > 0 {
		return cloneTopology(cfg)
	}
	out := cloneTopology(cfg)
	ports := slices.Clone(cfg.Ports)
	if len(ports) == 0 && uplink != "" {
		ports = []string{uplink}
	}
	parent := ""
	if len(ports) == 1 {
		parent = ports[0]
	}
	out.Bonds = []Bond{}
	if len(ports) > 1 {
		mode := cfg.BondMode
		if !slices.Contains(BondModes, mode) {
			mode = BondModes[0]
		}
		out.Bonds = []Bond{{ID: migratedBond, Ports: ports, Mode: mode}}
		parent = "bond:" + migratedBond
	}
	n := LogicalNetwork{
		ID: migratedNetwork, Name: migratedName, Purpose: "management", Parent: parent,
		VLAN: cfg.VLAN, Bridge: cfg.Bridge, Management: true,
		DNS: slices.Clone(cfg.DNS), Routes: []Route{},
	}
	if n.DNS == nil {
		n.DNS = []string{}
	}
	// networkd's DHCP=yes of the legacy file asks for IPv4 and IPv6; a fixed
	// address kept router advertisements on.
	if cfg.DHCP {
		n.IPv4, n.IPv6 = "dhcp", "dhcp"
	} else {
		n.IPv4, n.IPv6 = "static", "auto"
		n.Address, n.Gateway = cfg.Address, cfg.Gateway
	}
	out.Networks = []LogicalNetwork{n}
	return out
}

// stripLegacy drops the flat fields from a topology. topology.json is only
// read by versions that know networks[]; network.json, which older system
// slots read, is left as it is.
func stripLegacy(cfg Config) Config {
	cfg.Ports, cfg.BondMode, cfg.VLAN, cfg.Bridge = []string{}, "", 0, false
	cfg.DHCP, cfg.Address, cfg.Gateway, cfg.DNS = false, "", "", []string{}
	return cfg
}

// Live is what one network looks like on the machine right now.
type Live struct {
	Device    string   `json:"device"`
	Up        bool     `json:"up"`
	Addresses []string `json:"addresses"`
}

// uplink picks the port a legacy configuration without ports runs on: the
// one with the default route, else the first with a cable, else the first.
func uplink(ports []Interface, routeDevice string) string {
	for _, p := range ports {
		if p.Name == routeDevice {
			return p.Name
		}
	}
	for _, p := range ports {
		if p.Carrier {
			return p.Name
		}
	}
	if len(ports) > 0 {
		return ports[0].Name
	}
	return ""
}

// liveDevices maps every network of cfg to the interface carrying it. For a
// legacy configuration that is the device the legacy files configure, which
// is not always the one the migrated topology would create.
func liveDevices(cfg Config, migrated Config, routeDevice string) map[string]string {
	devices := map[string]string{}
	if len(cfg.Networks) == 0 {
		dev := device(cfg)
		if dev == "" {
			dev = routeDevice
		}
		for _, n := range migrated.Networks {
			devices[n.ID] = dev
		}
		return devices
	}
	for _, n := range cfg.Networks {
		devices[n.ID] = networkDevice(n)
	}
	return devices
}

// readLive asks the kernel once for all links and their global addresses.
func (m *Manager) readLive(ctx context.Context, devices map[string]string) map[string]Live {
	live := map[string]Live{}
	bounded, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	out, err := m.runner.Run(bounded, "", "ip", "-j", "address", "show")
	var links []struct {
		Name      string   `json:"ifname"`
		Flags     []string `json:"flags"`
		Addresses []struct {
			Local     string `json:"local"`
			Prefix    int    `json:"prefixlen"`
			Scope     string `json:"scope"`
			Tentative bool   `json:"tentative"`
		} `json:"addr_info"`
	}
	if err == nil {
		_ = json.Unmarshal([]byte(out), &links)
	}
	for id, dev := range devices {
		state := Live{Device: dev, Addresses: []string{}}
		for _, link := range links {
			if link.Name != dev || dev == "" {
				continue
			}
			state.Up = slices.Contains(link.Flags, "UP") && slices.Contains(link.Flags, "LOWER_UP")
			for _, a := range link.Addresses {
				if a.Scope == "global" && a.Local != "" && !a.Tentative {
					state.Addresses = append(state.Addresses, a.Local+"/"+strconv.Itoa(a.Prefix))
				}
			}
		}
		live[id] = state
	}
	return live
}
