package system

import (
	"os"
	"path/filepath"
	"testing"
)

// The bridge/veth values reproduce Linux 6.12's ethtool behaviour. All
// topology and device files are temporary; no host network is changed.
func TestPhysicalLinkSpeed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		iface string
		files map[string]string
		links map[string]string
		want  int
	}{
		{name: "physical negotiated link", iface: "eth0", want: 2500},
		{name: "bridge ignores virtual 10G port", iface: "br0", want: 2500,
			links: map[string]string{"br0/brif/eth0": "eth0", "br0/brif/veth0": "veth0"}},
		{name: "bridge with only virtual ports", iface: "br0",
			links: map[string]string{"br0/brif/veth0": "veth0"}},
		{name: "virtual port has no physical link", iface: "veth0"},
		{name: "VLAN follows its lower device", iface: "vlan20", want: 2500,
			files: map[string]string{"vlan20/carrier": "1", "vlan20/speed": "10000"},
			links: map[string]string{"vlan20/lower_eth0": "eth0"}},
		{name: "bridge over VLAN ignores VM port", iface: "br0", want: 2500,
			files: map[string]string{"vlan20/carrier": "1"},
			links: map[string]string{"br0/brif/vlan20": "vlan20", "br0/brif/veth0": "veth0", "vlan20/lower_eth0": "eth0"}},
		{name: "several physical uplinks are ambiguous", iface: "br0",
			files: map[string]string{"eth1/device/present": "", "eth1/carrier": "1", "eth1/speed": "1000"},
			links: map[string]string{"br0/brif/eth0": "eth0", "br0/brif/eth1": "eth1"}},
		{name: "unknown second physical uplink remains ambiguous", iface: "br0",
			files: map[string]string{"eth1/device/present": "", "eth1/carrier": "1", "eth1/speed": "-1"},
			links: map[string]string{"br0/brif/eth0": "eth0", "br0/brif/eth1": "eth1"}},
		{name: "same physical uplink is counted once", iface: "br0", want: 2500,
			files: map[string]string{"vlan20/carrier": "1"},
			links: map[string]string{"br0/brif/eth0": "eth0", "br0/brif/vlan20": "vlan20", "vlan20/lower_eth0": "eth0"}},
		{name: "bond retains driver aggregate", iface: "br0", want: 5000,
			files: map[string]string{"bond0/bonding/mode": "802.3ad 4", "bond0/carrier": "1", "bond0/speed": "5000", "eth1/device/present": "", "eth1/carrier": "1", "eth1/speed": "2500"},
			links: map[string]string{"br0/brif/bond0": "bond0", "br0/brif/veth0": "veth0", "bond0/lower_eth0": "eth0", "bond0/lower_eth1": "eth1"}},
		{name: "active backup is not added", iface: "bond0", want: 2500,
			files: map[string]string{"bond0/bonding/mode": "active-backup 1", "bond0/carrier": "1", "bond0/speed": "2500", "eth1/device/present": "", "eth1/carrier": "1", "eth1/speed": "2500"},
			links: map[string]string{"bond0/lower_eth0": "eth0", "bond0/lower_eth1": "eth1"}},
		{name: "bond without physical device stays unknown", iface: "bond0",
			files: map[string]string{"bond0/bonding/mode": "active-backup 1", "bond0/carrier": "1", "bond0/speed": "10000"},
			links: map[string]string{"bond0/lower_veth0": "veth0"}},
		{name: "unplugged physical link", iface: "eth0", files: map[string]string{"eth0/carrier": "0"}},
		{name: "unknown signed speed", iface: "eth0", files: map[string]string{"eth0/speed": "-1"}},
		{name: "unknown unsigned speed", iface: "eth0", files: map[string]string{"eth0/speed": "4294967295"}},
		{name: "unreadable speed", iface: "eth0", files: map[string]string{"eth0/speed": "invalid"}},
		{name: "cycle is bounded", iface: "br0", files: map[string]string{"vlan20/carrier": "1"},
			links: map[string]string{"br0/brif/vlan20": "vlan20", "vlan20/lower_br0": "br0"}},
		{name: "missing device", iface: "missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			files := map[string]string{
				"eth0/device/present": "", "eth0/carrier": "1", "eth0/speed": "2500",
				"br0/carrier": "1", "br0/speed": "10000",
				"veth0/carrier": "1", "veth0/speed": "10000",
			}
			for name, value := range tc.files {
				files[name] = value
			}
			for name, value := range files {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(value+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			for name, target := range tc.links {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(filepath.Join(root, target), path); err != nil {
					t.Fatal(err)
				}
			}
			if got := readLinkSpeed(root, tc.iface); got != tc.want {
				t.Fatalf("%s: speed = %d Mbit/s, want %d", tc.iface, got, tc.want)
			}
		})
	}
}
