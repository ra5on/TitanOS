package system

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// A bridge's speed is the fastest member, including virtual VM/container
// ports. It does not describe the physical uplink. Follow the sysfs topology
// instead; several independent uplinks have no single unambiguous speed.
func readLinkSpeed(root, iface string) int {
	links := physicalLinkSpeeds(root, iface, make(map[string]bool))
	if len(links) != 1 {
		return 0
	}
	for _, speed := range links {
		return speed
	}
	return 0
}

func physicalLinkSpeeds(root, iface string, visiting map[string]bool) map[string]int {
	if iface == "" || iface == "." || iface == ".." || filepath.Base(iface) != iface || visiting[iface] {
		return nil
	}
	visiting[iface] = true
	defer delete(visiting, iface)
	base := filepath.Join(root, iface)
	if readTrimmed(filepath.Join(base, "carrier")) != "1" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(base, "device")); err == nil {
		return map[string]int{iface: linkSpeedValue(base)}
	}

	var children []string
	if ports, err := os.ReadDir(filepath.Join(base, "brif")); err == nil {
		for _, port := range ports {
			children = append(children, port.Name())
		}
	} else {
		entries, _ := os.ReadDir(base)
		for _, entry := range entries {
			if child, ok := strings.CutPrefix(entry.Name(), "lower_"); ok {
				children = append(children, child)
			}
		}
	}
	links := make(map[string]int)
	for _, child := range children {
		for name, speed := range physicalLinkSpeeds(root, child, visiting) {
			links[name] = speed
		}
	}
	if _, err := os.Stat(filepath.Join(base, "bonding")); err == nil && len(links) > 0 {
		// Unlike a bridge, the bond driver accounts for the active mode and
		// transmitting members. Preserve its negotiated aggregate value.
		if speed := linkSpeedValue(base); speed > 0 {
			return map[string]int{iface: speed}
		}
		return nil
	}
	return links
}

func linkSpeedValue(base string) int {
	speed, err := strconv.ParseUint(readTrimmed(filepath.Join(base, "speed")), 10, 32)
	if err != nil || speed == 0 || speed == uint64(^uint32(0)) {
		return 0
	}
	return int(speed)
}
