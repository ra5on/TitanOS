package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/config"
)

func TestMatchLikeFnmatch(t *testing.T) {
	for _, c := range []struct {
		pattern, name string
		want          bool
	}{
		{"pci:v00008088d00001001sv*sd*bc*sc*i*", "pci:v00008088d00001001sv00008088sd00000000bc02sc00i00", true},
		{"pci:v00008088d00001001sv*sd*bc*sc*i*", "pci:v00008088d00002001sv00008088sd00000000bc02sc00i00", false},
		{"pci:v000010DEd00001[EF]*sv*sd*bc03sc*i*", "pci:v000010DEd00001E84sv00001043sd00008700bc03sc00i00", true},
		{"pci:v000010DEd00001[EF]*sv*sd*bc03sc*i*", "pci:v000010DEd00001DB4sv000010DEsd00001212bc03sc02i00", false},
		{"pci:v000010DEd00001[EF]*sv*sd*bc03sc*i*", "pci:v000010DEd00001E84sv00001043sd00008700bc04sc03i00", false},
		{"pci:v*d0000[!1]*", "pci:v1d00002", true},
		{"pci:v*d0000[^1]*", "pci:v1d00001", false},
		{"usb:v1A6Ep089A*", "usb:v1A6Ep089Ad0100dc00dsc00dp00icFFiscFFipFFin00", true},
		// DMI modaliases contain '/'; '*' must span it, unlike path.Match.
		{"dmi*:rvn*GigabyteTechnologyCo.,Ltd.*:rn*X670EAORUSMASTER*:", "dmi:bvnAmericanMegatrendsInc.:bvrF2:bd06/01/2025:rvnGigabyteTechnologyCo.,Ltd.:rnX670EAORUSMASTER:rvrx.x:", true},
		{"a?c", "abc", true},
		{"a?c", "ac", false},
		{"a\\*", "a*", true},
		{"a\\*", "ab", false},
		{"[", "[", true},
		{"[]]", "]", true},
		{"*", "", true},
		{"", "x", false},
	} {
		if got := Match(c.pattern, c.name); got != c.want {
			t.Errorf("Match(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
	}
}

// fakeSys builds a sysfs below a temporary directory.
func fakeSys(t *testing.T) string {
	t.Helper()
	sys := t.TempDir()
	write := func(path, value string) {
		full := filepath.Join(sys, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A Wangxun 10G card without driver, an Intel NIC with one.
	write("bus/pci/devices/0000:03:00.0/modalias", "pci:v00008088d00001001sv00008088sd00000000bc02sc00i00")
	write("bus/pci/devices/0000:03:00.0/vendor", "0x8088")
	write("bus/pci/devices/0000:03:00.0/device", "0x1001")
	write("bus/pci/devices/0000:04:00.0/modalias", "pci:v00008086d0000125Csv00008086sd00000000bc02sc00i00")
	write("bus/pci/devices/0000:04:00.0/vendor", "0x8086")
	write("bus/pci/devices/0000:04:00.0/device", "0x125c")
	if err := os.Symlink("../../../bus/pci/drivers/igc", filepath.Join(sys, "bus/pci/devices/0000:04:00.0/driver")); err != nil {
		t.Fatal(err)
	}
	// A Coral USB accelerator before its firmware is loaded.
	write("bus/usb/devices/2-1/idVendor", "1a6e")
	write("bus/usb/devices/2-1/idProduct", "089a")
	write("bus/usb/devices/2-1:1.0/modalias", "usb:v1A6Ep089Ad0100dc00dsc00dp00icFFiscFFipFFin00")
	write("class/dmi/id/modalias", "dmi:bvnAmericanMegatrendsInc.:bvrF2:bd06/01/2025:rvnGigabyteTechnologyCo.,Ltd.:rnX670EAORUSMASTER:rvrx.x:")
	write("class/dmi/id/board_vendor", "Gigabyte Technology Co., Ltd.")
	write("class/dmi/id/board_name", "X670E AORUS MASTER")
	return sys
}

func TestScanAndRecommend(t *testing.T) {
	sys := fakeSys(t)
	ids := filepath.Join(t.TempDir(), "pci.ids")
	os.WriteFile(ids, []byte("8088  Beijing Wangxun Technology Co., Ltd.\n\t1001  Ethernet Controller RP1000 for 10GbE SFP+\n8086  Intel Corporation\n\t125c  Ethernet Controller I226-V\nC 00  Unclassified device\n"), 0o644)
	devices := scanDevices(sys, ids)
	if len(devices) != 4 {
		t.Fatalf("devices: %+v", devices)
	}
	if devices[0].Name != "Beijing Wangxun Technology Co., Ltd. Ethernet Controller RP1000 for 10GbE SFP+" || devices[0].Key != "pci:8088:1001" || devices[0].Driver != "" {
		t.Errorf("first device: %+v", devices[0])
	}
	if devices[1].Driver != "igc" {
		t.Errorf("bound driver not read: %+v", devices[1])
	}
	defs := []Manifest{
		{Name: "wangxun-nic", Title: "Wangxun", Catalog: Catalog{Hardware: []HardwareRule{{Modalias: "pci:v00008088d00001001sv*sd*bc*sc*i*", Unbound: true}}}},
		// A driver plugin for a device a driver already handles: not recommended.
		{Name: "intel-other", Title: "Other", Catalog: Catalog{Hardware: []HardwareRule{{Modalias: "pci:v00008086d0000125Csv*sd*bc*sc*i*", Unbound: true}}}},
		// A tool that works beside the driver: recommended despite the driver.
		{Name: "intel-tool", Title: "Tool", Catalog: Catalog{Hardware: []HardwareRule{{Modalias: "pci:v00008086d*sv*sd*bc02sc*i*"}}}},
		{Name: "coral-usb", Title: "Coral", Catalog: Catalog{Hardware: []HardwareRule{{Modalias: "usb:v1A6Ep089A*"}}}},
		{Name: "it87-extended", Title: "IT87", Catalog: Catalog{Hardware: []HardwareRule{{Modalias: "dmi*:rvn*GigabyteTechnologyCo.,Ltd.*:rn*X670EAORUSMASTER*:"}}}},
		{Name: "nothing", Title: "No hardware"},
	}
	got := recommend(defs, map[string]bool{}, devices, nil)
	var names []string
	for _, r := range got {
		names = append(names, r.Plugin)
	}
	if !slices.Equal(names, []string{"coral-usb", "intel-tool", "it87-extended", "wangxun-nic"}) {
		t.Fatalf("recommended: %v", names)
	}
	if got[3].Devices[0].Address != "0000:03:00.0" {
		t.Errorf("devices of the recommendation: %+v", got[3].Devices)
	}
	// Installed plugins and dismissed devices are not recommended.
	got = recommend(defs, map[string]bool{"wangxun-nic": true, "intel-tool": true}, devices, []string{"coral-usb usb:1a6e:089a"})
	names = nil
	for _, r := range got {
		names = append(names, r.Plugin)
	}
	if !slices.Equal(names, []string{"it87-extended"}) {
		t.Fatalf("after install/dismiss: %v", names)
	}
}

func TestCatalogValidation(t *testing.T) {
	ok := Catalog{Category: "network", Hardware: []HardwareRule{{Modalias: "pci:v*"}, {Modalias: "acpi:PNP0C0A:*"}}}
	if ok.validate() != nil {
		t.Fatal("valid catalog rejected")
	}
	// A rule for a bus this titand does not know never matches.
	if found := matching(ok.Hardware[1:], []Device{{Bus: "pci", modalias: "acpi:PNP0C0A:"}}, false); len(found) != 0 {
		t.Fatalf("unknown bus matched: %+v", found)
	}
	for _, bad := range []Catalog{
		{Category: "Network!"},
		{Hardware: []HardwareRule{{Modalias: ""}}},
		{Hardware: []HardwareRule{{Modalias: "pci:" + strings.Repeat("v", 600)}}},
	} {
		if bad.validate() == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
	// A manifest of an older TitanOS without catalog fields stays valid, and
	// a new one keeps its fields through parsing.
	data, _ := json.Marshal(map[string]any{"name": "x", "version": "1", "sha256": "0000000000000000000000000000000000000000000000000000000000000000", "size": 1,
		"category": "sensors", "license": "GPL-2.0-or-later", "details": map[string]any{"de": map[string]any{"when": "w"}}, "hardware": []map[string]any{{"modalias": "dmi*:rvn*", "unbound": false}}})
	manifest, err := parseManifest(data, "x.plugin.json")
	if err != nil || manifest.Category != "sensors" || manifest.Details["de"].When != "w" || len(manifest.Hardware) != 1 {
		t.Fatalf("parsed %+v, %v", manifest, err)
	}
}

func TestDismissAndNotifyOnce(t *testing.T) {
	sys := fakeSys(t)
	table := t.TempDir()
	os.WriteFile(filepath.Join(table, "wangxun-nic.json"), []byte(`{"title":"Wangxun","category":"network","hardware":[{"modalias":"pci:v00008088d00001001sv*sd*bc*sc*i*","unbound":true}]}`), 0o644)
	os.WriteFile(filepath.Join(table, "broken.json"), []byte(`{`), 0o644)
	store := config.Open(t.TempDir())
	m := &Manager{store: store, sysRoot: sys, catalogTable: table, active: map[string]Manifest{}, resident: map[string]Manifest{}, problems: map[string]string{}, applied: "-"}
	if err := m.Load(); err != nil {
		t.Fatal(err)
	}
	var told []string
	m.Notify = func(title string) { told = append(told, title) }
	m.DetectHardware()
	m.notifyRecommendations(nil)
	m.notifyRecommendations(nil)
	if !slices.Equal(told, []string{"Wangxun"}) {
		t.Fatalf("notifications: %v", told)
	}
	// A new scan (a udev event, a new start) does not tell again.
	m.DetectHardware()
	m.notifyRecommendations(nil)
	if len(told) != 1 {
		t.Fatalf("notified again: %v", told)
	}
	status := m.Status(nil)
	if len(status.Recommendations) != 1 || status.Recommendations[0].Plugin != "wangxun-nic" {
		t.Fatalf("status: %+v", status.Recommendations)
	}
	if err := m.Dismiss("wangxun-nic", nil); err != nil {
		t.Fatal(err)
	}
	if status = m.Status(nil); len(status.Recommendations) != 0 || status.Dismissed != 1 {
		t.Fatalf("after dismissing: %+v", status)
	}
	if err := m.Dismiss("wangxun-nic", nil); err != ErrNotFound {
		t.Fatalf("dismissing nothing: %v", err)
	}
	// Remembered in /config, across a restart of titand.
	var saved recommendationState
	if err := store.ReadJSON(recommendationsFile, &saved); err != nil || !slices.Equal(saved.Dismissed, []string{"wangxun-nic pci:8088:1001"}) || !slices.Equal(saved.Notified, []string{"wangxun-nic pci:8088:1001"}) {
		t.Fatalf("saved %+v, %v", saved, err)
	}
	again := &Manager{store: store, sysRoot: sys, catalogTable: table, active: map[string]Manifest{}, resident: map[string]Manifest{}, problems: map[string]string{}, applied: "-"}
	again.Load()
	again.DetectHardware()
	if status = again.Status(nil); len(status.Recommendations) != 0 {
		t.Fatalf("dismissal forgotten: %+v", status.Recommendations)
	}
	if err := again.RestoreDismissed(); err != nil {
		t.Fatal(err)
	}
	if status = again.Status(nil); len(status.Recommendations) != 1 || len(status.Recommendations[0].Devices) != 1 {
		t.Fatalf("restored: %+v", status.Recommendations)
	}
}

func TestParseUevent(t *testing.T) {
	action, subsystem := parseUevent([]byte("add@/devices/pci0000:00/0000:00:1c.0\x00ACTION=add\x00DEVPATH=/devices/pci0000:00\x00SUBSYSTEM=pci\x00SEQNUM=1\x00"))
	if action != "add" || subsystem != "pci" {
		t.Fatalf("%q %q", action, subsystem)
	}
}
