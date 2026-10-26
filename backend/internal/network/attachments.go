package network

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

// ProtectAttachments also counts stopped, configured workloads.
func ProtectAttachments(before, after Config, refs map[string]bool, legacyBridge bool) error {
	find := func(cfg Config, id string) (LogicalNetwork, bool) {
		for _, n := range cfg.Networks {
			if n.ID == id {
				return n, true
			}
		}
		return LogicalNetwork{}, false
	}
	for id, bridgeRequired := range refs {
		old, had := find(before, id)
		next, ok := find(after, id)
		if !ok || (bridgeRequired && !next.Bridge) {
			return fmt.Errorf("%w: network %s is still used by a container or VM", ErrInvalid, id)
		}
		if had && (old.Parent != next.Parent || old.VLAN != next.VLAN || old.Bridge != next.Bridge || (old.Bridge && old.Management != next.Management)) {
			return fmt.Errorf("%w: move workloads off network %s before changing its connection", ErrInvalid, id)
		}
		if had && strings.HasPrefix(old.Parent, "bond:") {
			bondID := strings.TrimPrefix(old.Parent, "bond:")
			var a, b Bond
			for _, v := range before.Bonds {
				if v.ID == bondID {
					a = v
				}
			}
			for _, v := range after.Bonds {
				if v.ID == bondID {
					b = v
				}
			}
			if !reflect.DeepEqual(a, b) {
				return fmt.Errorf("%w: bond %s is in use", ErrInvalid, bondID)
			}
		}
	}
	if legacyBridge {
		if len(after.Networks) == 0 {
			if !after.Bridge || before.VLAN != after.VLAN || before.BondMode != after.BondMode || !slices.Equal(before.Ports, after.Ports) {
				return fmt.Errorf("%w: legacy VM bridge is in use", ErrInvalid)
			}
		} else {
			found := false
			for _, n := range after.Networks {
				if networkDevice(n) == Bridge && n.Bridge {
					found = true
					if len(before.Networks) > 0 {
						for _, previous := range before.Networks {
							if networkDevice(previous) == Bridge && (previous.Parent != n.Parent || previous.VLAN != n.VLAN || !reflect.DeepEqual(before.Bonds, after.Bonds)) {
								return fmt.Errorf("%w: move legacy VMs off br0 before changing its connection", ErrInvalid)
							}
						}
					} else {
						ports := []string{n.Parent}
						mode := ""
						for _, bond := range after.Bonds {
							if n.Parent == "bond:"+bond.ID {
								ports = bond.Ports
								mode = bond.Mode
							}
						}
						if n.VLAN != before.VLAN || !slices.Equal(ports, before.Ports) || len(ports) > 1 && mode != before.BondMode {
							return fmt.Errorf("%w: keep the existing br0 uplink when migrating legacy VMs", ErrInvalid)
						}
					}
				}
			}
			if !found {
				return fmt.Errorf("%w: management bridge br0 is still used by a VM", ErrInvalid)
			}
		}
	}
	return nil
}
