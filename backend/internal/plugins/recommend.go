package plugins

// Hardware recommendations: which plugin fits the hardware of this NAS.
//
// A manifest may list modalias patterns (PCI, USB, DMI) in "hardware",
// the same patterns the kernel module carries (modinfo alias). TitanOS
// compares them with the devices in /sys at start, on udev "add" events and
// when the catalog is refreshed — never in the status poll. A plugin is
// recommended for a device when the plugin is not installed and, for rules
// marked "unbound", no kernel driver has taken the device. Recommendations
// only point at the plugin; installing it stays an explicit action.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

const (
	recommendationsFile = "plugins/recommendations.json"
	// The image carries the catalog information of the official plugins of
	// its release, so recommendations work without a network.
	catalogTableDir  = "/usr/share/titanos/plugin-catalog"
	maxHardwareRules = 1000
)

var categoryRE = regexp.MustCompile(`^[a-z][a-z-]{0,31}$`)

// Catalog is the descriptive part of a manifest. Every field is optional.
type Catalog struct {
	// Category groups plugins on the page: network, graphics, sensors,
	// storage, tools, remote. Unknown categories are shown as "other".
	Category string `json:"category,omitempty"`
	// License is an SPDX expression of what the plugin ships.
	License  string    `json:"license,omitempty"`
	Upstream *Upstream `json:"upstream,omitempty"`
	// Details maps a language to the long description.
	Details  map[string]Details `json:"details,omitempty"`
	Hardware []HardwareRule     `json:"hardware,omitempty"`
	// Sources are links to the exact sources of what the plugin ships
	// (driver plugins: Debian source package and the sources archive).
	Sources []string `json:"sources,omitempty"`
}

// Upstream names where the plugin's software comes from, pinned.
type Upstream struct {
	Name    string `json:"name,omitempty"`
	URL     string `json:"url,omitempty"`
	Version string `json:"version,omitempty"`
}

// Details is the long description of a plugin in one language.
type Details struct {
	// When answers "do I need this?".
	When string `json:"when,omitempty"`
	// Hardware lists supported chips, cards or boards.
	Hardware []string `json:"hardware,omitempty"`
	// Changes says what the plugin changes on the system.
	Changes string `json:"changes,omitempty"`
	// Limits are known limits and risks.
	Limits []string `json:"limits,omitempty"`
	// Footprint describes RAM use; the disk size is the image size.
	Footprint string `json:"footprint,omitempty"`
}

// HardwareRule matches the modalias of a device, as fnmatch(3) does.
type HardwareRule struct {
	Modalias string `json:"modalias"`
	// Unbound: only devices without a bound kernel driver count. Right for
	// drivers; tools that work next to an existing driver leave it out.
	Unbound bool `json:"unbound,omitempty"`
}

func (c Catalog) validate() error {
	if c.Category != "" && !categoryRE.MatchString(c.Category) {
		return errors.New("odd category")
	}
	if len(c.Hardware) > maxHardwareRules {
		return errors.New("too many hardware rules")
	}
	// Rules for other buses are kept but never match, so that a newer kind
	// of rule does not make a signed manifest unusable for this titand.
	for _, rule := range c.Hardware {
		if rule.Modalias == "" || len(rule.Modalias) > 512 {
			return errors.New("odd hardware rule")
		}
	}
	return nil
}

// ruleBus is the bus a pattern is for: "pci:v…", "usb:v…", and "dmi*:…" as
// the kernel writes DMI aliases. Empty if there is none.
func ruleBus(pattern string) string {
	bus, _, ok := strings.Cut(pattern, ":")
	if !ok {
		return ""
	}
	return strings.TrimSuffix(bus, "*")
}

// Device is a piece of present hardware, as shown with a recommendation.
type Device struct {
	// Key identifies the kind of device (vendor and product), stable across
	// boots and slots; dismissed recommendations are remembered by it.
	Key     string `json:"key"`
	Bus     string `json:"bus"`
	Address string `json:"address,omitempty"`
	Name    string `json:"name"`
	// Driver is the kernel driver bound to the device, if any.
	Driver   string `json:"driver,omitempty"`
	modalias string
}

// Recommendation is a plugin for present hardware that nothing handles yet.
type Recommendation struct {
	Plugin   string   `json:"plugin"`
	Title    string   `json:"title"`
	Category string   `json:"category,omitempty"`
	Devices  []Device `json:"devices"`
}

// recommendationState is /config/plugins/recommendations.json. Both lists
// hold "<plugin> <device key>"; the file is written only when they change.
type recommendationState struct {
	Dismissed []string `json:"dismissed"`
	Notified  []string `json:"notified"`
}

// Match reports whether name matches the fnmatch(3) pattern without flags:
// '*', '?' and bracket expressions with ranges and '!' or '^'. Unlike
// path.Match a '/' is an ordinary character (DMI dates contain it).
func Match(pattern, name string) bool {
	px, nx := 0, 0
	starP, starN := -1, 0
	for nx < len(name) {
		if px < len(pattern) {
			switch c := pattern[px]; c {
			case '*':
				starP, starN = px, nx
				px++
				continue
			case '?':
				px++
				nx++
				continue
			case '[':
				if end, ok := matchClass(pattern[px:], name[nx]); end > 0 {
					if ok {
						px += end
						nx++
						continue
					}
				} else if name[nx] == '[' { // unterminated: a literal '['
					px++
					nx++
					continue
				}
			default:
				if c == '\\' && px+1 < len(pattern) {
					if pattern[px+1] == name[nx] {
						px += 2
						nx++
						continue
					}
				} else if c == name[nx] {
					px++
					nx++
					continue
				}
			}
		}
		if starP < 0 {
			return false
		}
		starN++
		px, nx = starP+1, starN
	}
	for px < len(pattern) && pattern[px] == '*' {
		px++
	}
	return px == len(pattern)
}

// matchClass matches c against the bracket expression at the start of
// pattern. It returns the length of the expression (0 if unterminated).
func matchClass(pattern string, c byte) (int, bool) {
	i := 1
	negate := i < len(pattern) && (pattern[i] == '!' || pattern[i] == '^')
	if negate {
		i++
	}
	matched := false
	for first := true; i < len(pattern); first = false {
		if pattern[i] == ']' && !first {
			return i + 1, matched != negate
		}
		lo := pattern[i]
		if i+2 < len(pattern) && pattern[i+1] == '-' && pattern[i+2] != ']' {
			if lo <= c && c <= pattern[i+2] {
				matched = true
			}
			i += 3
			continue
		}
		if lo == c {
			matched = true
		}
		i++
	}
	return 0, false
}

// scanDevices lists PCI devices, USB interfaces (one entry per USB device)
// and the mainboard (DMI) below sys, normally /sys. PCI names come from
// pciIDs (pci.ids), read only for the devices present.
func scanDevices(sys, pciIDs string) []Device {
	var devices []Device
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(data))
	}
	driver := func(dir string) string {
		target, err := os.Readlink(filepath.Join(dir, "driver"))
		if err != nil {
			return ""
		}
		return filepath.Base(target)
	}
	pci, _ := os.ReadDir(filepath.Join(sys, "bus/pci/devices"))
	for _, entry := range pci {
		dir := filepath.Join(sys, "bus/pci/devices", entry.Name())
		alias := read(filepath.Join(dir, "modalias"))
		if !strings.HasPrefix(alias, "pci:") {
			continue
		}
		vendor := strings.ToLower(strings.TrimPrefix(read(filepath.Join(dir, "vendor")), "0x"))
		product := strings.ToLower(strings.TrimPrefix(read(filepath.Join(dir, "device")), "0x"))
		devices = append(devices, Device{Key: "pci:" + vendor + ":" + product, Bus: "pci", Address: entry.Name(),
			Driver: driver(dir), modalias: alias})
	}
	wanted := map[string]bool{}
	for _, device := range devices {
		vendor, product, _ := strings.Cut(strings.TrimPrefix(device.Key, "pci:"), ":")
		wanted[vendor], wanted[vendor+":"+product] = true, true
	}
	names := readPCIIDs(pciIDs, wanted)
	for i := range devices {
		vendor, product, _ := strings.Cut(strings.TrimPrefix(devices[i].Key, "pci:"), ":")
		devices[i].Name = names.name(vendor, product)
	}
	usb, _ := os.ReadDir(filepath.Join(sys, "bus/usb/devices"))
	seen := map[string]int{}
	for _, entry := range usb {
		parent, _, isInterface := strings.Cut(entry.Name(), ":")
		if !isInterface {
			continue
		}
		dir := filepath.Join(sys, "bus/usb/devices", entry.Name())
		alias := read(filepath.Join(dir, "modalias"))
		if !strings.HasPrefix(alias, "usb:") {
			continue
		}
		owner := filepath.Join(sys, "bus/usb/devices", parent)
		vendor, product := read(filepath.Join(owner, "idVendor")), read(filepath.Join(owner, "idProduct"))
		name := strings.TrimSpace(read(filepath.Join(owner, "manufacturer")) + " " + read(filepath.Join(owner, "product")))
		if name == "" {
			name = "USB " + vendor + ":" + product
		}
		device := Device{Key: "usb:" + vendor + ":" + product, Bus: "usb", Address: parent, Name: name, Driver: driver(dir), modalias: alias}
		// A USB device has one entry per interface; keep one per device,
		// but each interface still counts for matching.
		if index, ok := seen[parent+"\x00"+alias]; ok {
			devices[index] = device
			continue
		}
		seen[parent+"\x00"+alias] = len(devices)
		devices = append(devices, device)
	}
	dmi := filepath.Join(sys, "class/dmi/id")
	if alias := read(filepath.Join(dmi, "modalias")); strings.HasPrefix(alias, "dmi:") {
		vendor, board := read(filepath.Join(dmi, "board_vendor")), read(filepath.Join(dmi, "board_name"))
		devices = append(devices, Device{Key: "dmi:" + vendor + "/" + board, Bus: "dmi", Name: strings.TrimSpace(vendor + " " + board), modalias: alias})
	}
	return devices
}

// matching returns the devices a plugin's rules match. With unboundOnly,
// devices a rule wants unbound but that have a driver are left out.
func matching(rules []HardwareRule, devices []Device, unboundOnly bool) []Device {
	var found []Device
	for _, device := range devices {
		for _, rule := range rules {
			if ruleBus(rule.Modalias) != device.Bus || !Match(rule.Modalias, device.modalias) {
				continue
			}
			if unboundOnly && rule.Unbound && device.Driver != "" {
				continue
			}
			if !slices.ContainsFunc(found, func(d Device) bool { return d.Key == device.Key && d.Address == device.Address }) {
				found = append(found, device)
			}
			break
		}
	}
	return found
}

// recommend lists the plugins of definitions for which present, unhandled,
// not dismissed hardware exists. Installed plugins are never recommended.
func recommend(definitions []Manifest, installed map[string]bool, devices []Device, dismissed []string) []Recommendation {
	result := []Recommendation{}
	for _, definition := range definitions {
		if installed[definition.Name] || len(definition.Hardware) == 0 {
			continue
		}
		var open []Device
		for _, device := range matching(definition.Hardware, devices, true) {
			if !slices.Contains(dismissed, definition.Name+" "+device.Key) {
				open = append(open, device)
			}
		}
		if len(open) > 0 {
			result = append(result, Recommendation{Plugin: definition.Name, Title: definition.Title, Category: definition.Category, Devices: open})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Plugin < result[j].Plugin })
	return result
}

// readCatalogTable reads the catalog information the image carries for the
// official plugins of its release: <name>.json, a manifest without image.
func readCatalogTable(dir string) []Manifest {
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var table []Manifest
	for _, entry := range entries {
		name, ok := strings.CutSuffix(entry.Name(), ".json")
		if !ok || !nameRE.MatchString(name) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil || len(data) > 64*1024 {
			continue
		}
		var manifest Manifest
		if json.Unmarshal(data, &manifest) != nil || manifest.Catalog.validate() != nil {
			slog.Warn("plugin catalog entry not readable", "file", entry.Name())
			continue
		}
		manifest.Name = name
		if manifest.Title == "" {
			manifest.Title = name
		}
		table = append(table, manifest)
	}
	return table
}

// definitions merges what is known about each plugin: the image's table,
// then the signed catalog of the release, then the installed manifest.
func definitions(table []Manifest, plugins []Plugin) []Manifest {
	byName := map[string]Manifest{}
	var order []string
	add := func(manifest Manifest) {
		if _, ok := byName[manifest.Name]; !ok {
			order = append(order, manifest.Name)
		}
		if len(manifest.Hardware) == 0 {
			// An older manifest without rules keeps those of the table.
			manifest.Catalog = mergeCatalog(byName[manifest.Name].Catalog, manifest.Catalog)
		}
		byName[manifest.Name] = manifest
	}
	for _, manifest := range table {
		add(manifest)
	}
	for _, plugin := range plugins {
		if plugin.Download || plugin.Installed {
			add(plugin.Manifest)
		}
	}
	result := make([]Manifest, 0, len(order))
	for _, name := range order {
		result = append(result, byName[name])
	}
	return result
}

func mergeCatalog(base, over Catalog) Catalog {
	if over.Category == "" {
		over.Category = base.Category
	}
	if over.License == "" {
		over.License = base.License
	}
	if over.Upstream == nil {
		over.Upstream = base.Upstream
	}
	if len(over.Details) == 0 {
		over.Details = base.Details
	}
	if len(over.Hardware) == 0 {
		over.Hardware = base.Hardware
	}
	return over
}

// pciNames maps "vvvv" and "vvvv:dddd" to names from pci.ids.
type pciNames map[string]string

// readPCIIDs keeps only the wanted entries of pci.ids; the whole list is
// large and needed only during a scan.
func readPCIIDs(path string, wanted map[string]bool) pciNames {
	names := pciNames{}
	file, err := os.Open(path)
	if err != nil {
		return names
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	vendor := ""
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "C "):
			return names // device classes follow; no more vendors
		case line == "" || line[0] == '#' || strings.HasPrefix(line, "\t\t"):
		case line[0] != '\t' && len(line) > 6 && isHex(line[:4]):
			vendor = strings.ToLower(line[:4])
			if wanted[vendor] {
				names[vendor] = strings.TrimSpace(line[4:])
			}
		case line[0] == '\t' && vendor != "" && wanted[vendor] && len(line) > 7 && isHex(line[1:5]):
			if id := vendor + ":" + strings.ToLower(line[1:5]); wanted[id] {
				names[id] = strings.TrimSpace(line[5:])
			}
		}
	}
	return names
}

func isHex(value string) bool {
	return strings.Trim(strings.ToLower(value), "0123456789abcdef") == ""
}

func (names pciNames) name(vendor, product string) string {
	device, maker := names[vendor+":"+product], names[vendor]
	switch {
	case device != "" && maker != "":
		return maker + " " + device
	case device != "":
		return device
	}
	return fmt.Sprintf("PCI %s:%s", vendor, product)
}

// hardwareState is what detection keeps in RAM.
type hardwareState struct {
	table     []Manifest
	devices   []Device
	notifyDue bool
	state     recommendationState
	stateRead bool
}

// DetectHardware looks at the devices again. Cheap (sysfs only), but meant
// for start, udev events and catalog refreshes, not for status polling.
func (m *Manager) DetectHardware() {
	m.hwMu.Lock()
	defer m.hwMu.Unlock()
	if m.sysRoot == "" {
		return
	}
	m.hw.devices = scanDevices(m.sysRoot, m.pciIDsPath)
	m.hw.notifyDue = true
}

func (m *Manager) hardwareSnapshot() ([]Manifest, []Device, recommendationState) {
	m.hwMu.Lock()
	defer m.hwMu.Unlock()
	state := recommendationState{Dismissed: slices.Clone(m.hw.state.Dismissed), Notified: slices.Clone(m.hw.state.Notified)}
	return m.hw.table, slices.Clone(m.hw.devices), state
}

// loadRecommendations reads the dismissed/notified lists once.
func (m *Manager) loadRecommendations() error {
	var state recommendationState
	err := m.store.ReadJSON(recommendationsFile, &state)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	table := readCatalogTable(m.catalogTable)
	m.hwMu.Lock()
	defer m.hwMu.Unlock()
	m.hw.state, m.hw.stateRead, m.hw.table = state, true, table
	return nil
}

// addHardware fills Devices of every plugin and the recommendations of the
// status. Called with the plugin list complete; reads only RAM.
func (m *Manager) addHardware(status *Status) {
	table, devices, state := m.hardwareSnapshot()
	defs := definitions(table, status.Plugins)
	installed := map[string]bool{}
	for i := range status.Plugins {
		plugin := &status.Plugins[i]
		installed[plugin.Name] = installed[plugin.Name] || plugin.Installed
		for _, definition := range defs {
			if definition.Name == plugin.Name {
				if len(plugin.Hardware) == 0 {
					plugin.Catalog = mergeCatalog(definition.Catalog, plugin.Catalog)
				}
				plugin.Devices = matching(definition.Hardware, devices, false)
			}
		}
	}
	status.Recommendations = recommend(defs, installed, devices, state.Dismissed)
	status.Dismissed = len(state.Dismissed)
}

// recommendations is what Status would recommend, without looking for
// plugin files in shares: installed plugins and the release catalog only.
func (m *Manager) recommendations(mounted []string) []Recommendation {
	m.actions.Lock()
	settings := m.current()
	var list []Plugin
	if settings.Volume != "" && slices.Contains(mounted, settings.Volume) {
		for _, manifest := range m.installed(settings.Volume) {
			list = append(list, Plugin{Manifest: manifest, Installed: true})
		}
	}
	m.actions.Unlock()
	_, available := m.catalogSnapshot()
	status := Status{Plugins: append(list, available...)}
	m.addHardware(&status)
	return status.Recommendations
}

// Dismiss stops recommending a plugin for the devices it is recommended for
// now. A different device later is recommended again.
func (m *Manager) Dismiss(name string, mounted []string) error {
	var keys []string
	for _, recommendation := range m.recommendations(mounted) {
		if recommendation.Plugin == name {
			for _, device := range recommendation.Devices {
				keys = append(keys, name+" "+device.Key)
			}
		}
	}
	if len(keys) == 0 {
		return ErrNotFound
	}
	return m.changeRecommendations(func(state *recommendationState) bool {
		changed := false
		for _, key := range keys {
			if !slices.Contains(state.Dismissed, key) {
				state.Dismissed = append(state.Dismissed, key)
				changed = true
			}
		}
		return changed
	})
}

// RestoreDismissed shows all dismissed recommendations again.
func (m *Manager) RestoreDismissed() error {
	return m.changeRecommendations(func(state *recommendationState) bool {
		changed := len(state.Dismissed) > 0
		state.Dismissed = nil
		return changed
	})
}

// changeRecommendations writes the file only when change reports a change.
func (m *Manager) changeRecommendations(change func(*recommendationState) bool) error {
	if m.Disabled {
		return ErrDisabled
	}
	m.hwMu.Lock()
	defer m.hwMu.Unlock()
	if !m.hw.stateRead {
		return ErrNotLoaded
	}
	state := recommendationState{Dismissed: slices.Clone(m.hw.state.Dismissed), Notified: slices.Clone(m.hw.state.Notified)}
	if !change(&state) {
		return nil
	}
	sort.Strings(state.Dismissed)
	sort.Strings(state.Notified)
	if err := m.store.WriteJSON(recommendationsFile, state); err != nil {
		return err
	}
	m.hw.state = state
	return nil
}

// notifyRecommendations tells Notify once about each new recommendation,
// after a scan, as soon as the installed plugins can be known (the plugin
// volume, if any, is mounted).
func (m *Manager) notifyRecommendations(mounted []string) {
	m.hwMu.Lock()
	due := m.hw.notifyDue && m.hw.stateRead
	m.hwMu.Unlock()
	if !due || m.Disabled || m.Notify == nil {
		return
	}
	settings := m.current()
	if settings.Volume != "" && !slices.Contains(mounted, settings.Volume) {
		return
	}
	current := m.recommendations(mounted)
	m.hwMu.Lock()
	m.hw.notifyDue = false
	m.hwMu.Unlock()
	var titles []string
	err := m.changeRecommendations(func(state *recommendationState) bool {
		changed := false
		for _, recommendation := range current {
			fresh := false
			for _, device := range recommendation.Devices {
				key := recommendation.Plugin + " " + device.Key
				if !slices.Contains(state.Notified, key) {
					state.Notified = append(state.Notified, key)
					fresh, changed = true, true
				}
			}
			if fresh {
				titles = append(titles, recommendation.Title)
			}
		}
		return changed
	})
	if err != nil {
		// Without remembering, the same notification would come again.
		slog.Warn("plugin recommendations not saved", "err", err)
		return
	}
	for _, title := range titles {
		m.Notify(title)
	}
}

// watchDevices rescans after udev "add" events of PCI and USB devices,
// bundled: a card or a hub brings many events at once.
func (m *Manager) watchDevices(ctx context.Context) {
	events := make(chan struct{}, 1)
	go func() {
		if err := listenUevents(ctx, func(action, subsystem string) {
			if action == "add" && (subsystem == "pci" || subsystem == "usb") {
				select {
				case events <- struct{}{}:
				default:
				}
			}
		}); err != nil && ctx.Err() == nil {
			slog.Warn("device events unavailable; hardware is checked at start and on catalog refresh", "err", err)
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-events:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
		// Drop the events that came in meanwhile; one scan covers them.
		select {
		case <-events:
		default:
		}
		m.DetectHardware()
	}
}
