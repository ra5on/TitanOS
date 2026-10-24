package system

import (
	"path/filepath"
	"strings"
	"sync"
)

// DMI is immutable during a boot. Read once from sysfs, never from /config.
type Inventory struct {
	Mainboard string `json:"mainboard"`
	BIOS      string `json:"bios"`
}

var inventoryOnce sync.Once
var inventory Inventory

func systemInventory() Inventory {
	inventoryOnce.Do(func() { inventory = readInventory("/sys/class/dmi/id") })
	return inventory
}
func readInventory(root string) Inventory {
	read := func(name string) string {
		v := readTrimmed(filepath.Join(root, name))
		switch strings.ToLower(v) {
		case "to be filled by o.e.m.", "default string", "not specified", "none", "unknown":
			return ""
		}
		return v
	}
	join := func(values ...string) string {
		var parts []string
		for _, v := range values {
			if v != "" {
				parts = append(parts, v)
			}
		}
		return strings.Join(parts, " · ")
	}
	return Inventory{Mainboard: join(read("board_vendor"), read("board_name")), BIOS: join(read("bios_version"), read("bios_date"))}
}
