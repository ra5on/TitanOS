package main

import "github.com/ra5on/TitanOS/backend/internal/hwcheck"

// hardwareCheck is the compatibility check. It only reads sysfs, procfs and
// the kernel log, so development mode uses it as well; plugin hints come
// from the plugin catalog (image table, release catalog, installed).
func (w *titand) hardwareCheck() *hwcheck.Checker {
	checker := hwcheck.New()
	checker.Plugins = func(aliases []string) map[string][]hwcheck.PluginHint {
		result := map[string][]hwcheck.PluginHint{}
		for alias, matches := range w.extensions.PluginsFor(aliases, w.volumes.Mounted()) {
			for _, match := range matches {
				result[alias] = append(result[alias], hwcheck.PluginHint{Plugin: match.Plugin, Title: match.Title, Installed: match.Installed})
			}
		}
		return result
	}
	return checker
}
