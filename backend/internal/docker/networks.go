package docker

import (
	"context"
	"net/netip"
	"sort"
)

// EngineNetwork is a network of the Docker engine as the firewall needs
// it: its bridge on the NAS (or the parent of a macvlan/ipvlan network)
// and its subnets.
type EngineNetwork struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Driver string `json:"driver"`
	// Device is the bridge interface on the NAS (docker0, br-<id>);
	// empty for other drivers.
	Device string `json:"device,omitempty"`
	// Parent is the interface a macvlan/ipvlan network hangs on.
	Parent   string   `json:"parent,omitempty"`
	Subnets  []string `json:"subnets"`
	Internal bool     `json:"internal,omitempty"`
}

type apiNetwork struct {
	ID       string `json:"Id"`
	Name     string
	Driver   string
	Internal bool
	Options  map[string]string
	IPAM     struct {
		Config []struct{ Subnet string }
	}
}

// bridgeDevice is the interface Docker gives a bridge network: the one
// named in its options, docker0 for the default network, else br- and the
// first twelve characters of its ID.
func bridgeDevice(n apiNetwork) string {
	if name := n.Options["com.docker.network.bridge.name"]; name != "" {
		return name
	}
	if n.Name == "bridge" {
		return "docker0"
	}
	if len(n.ID) < 12 {
		return ""
	}
	return "br-" + n.ID[:12]
}

// Networks lists the bridge, macvlan and ipvlan networks of the engine
// (not host, none or overlay). It fails while the engine does not answer.
func (m *Manager) Networks(ctx context.Context) ([]EngineNetwork, error) {
	var list []apiNetwork
	if err := m.api.getJSON(ctx, "/networks", nil, &list); err != nil {
		return nil, err
	}
	out := make([]EngineNetwork, 0, len(list))
	for _, n := range list {
		e := EngineNetwork{ID: n.ID, Name: n.Name, Driver: n.Driver, Internal: n.Internal, Subnets: []string{}}
		switch n.Driver {
		case "bridge":
			e.Device = bridgeDevice(n)
		case "macvlan", "ipvlan":
			e.Parent = n.Options["parent"]
		default:
			continue
		}
		for _, c := range n.IPAM.Config {
			if p, err := netip.ParsePrefix(c.Subnet); err == nil {
				e.Subnets = append(e.Subnets, p.Masked().String())
			}
		}
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
