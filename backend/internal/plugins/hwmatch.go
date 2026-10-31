package plugins

import "slices"

// HardwareMatch is a plugin whose hardware rules match a device.
type HardwareMatch struct {
	Plugin    string
	Title     string
	Installed bool
}

// PluginsFor returns, per modalias, the known plugins whose hardware rules
// match it, whether or not the device has a driver: the image's catalog
// table, the loaded release catalog and the installed plugins. It reads
// the installed manifests from the plugin volume, so it is meant for a
// button, not for polling. While a plugin action runs, installed plugins
// are not looked up (Installed stays false).
func (m *Manager) PluginsFor(aliases []string, mounted []string) map[string][]HardwareMatch {
	var list []Plugin
	if m.actions.TryLock() {
		settings := m.current()
		if settings.Volume != "" && slices.Contains(mounted, settings.Volume) {
			for _, manifest := range m.installed(settings.Volume) {
				list = append(list, Plugin{Manifest: manifest, Installed: true})
			}
		}
		m.actions.Unlock()
	}
	_, available := m.catalogSnapshot()
	table, _, _ := m.hardwareSnapshot()
	installed := map[string]bool{}
	for _, plugin := range list {
		installed[plugin.Name] = true
	}
	result := map[string][]HardwareMatch{}
	for _, definition := range definitions(table, append(list, available...)) {
		for _, alias := range aliases {
			if alias == "" || slices.ContainsFunc(result[alias], func(match HardwareMatch) bool { return match.Plugin == definition.Name }) {
				continue
			}
			for _, rule := range definition.Hardware {
				if ruleBus(rule.Modalias) == ruleBus(alias) && Match(rule.Modalias, alias) {
					result[alias] = append(result[alias], HardwareMatch{Plugin: definition.Name, Title: definition.Title, Installed: installed[definition.Name]})
					break
				}
			}
		}
	}
	return result
}
