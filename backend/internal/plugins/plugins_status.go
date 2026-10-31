package plugins

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// installed returns the plugins in the plugin folder of the volume.
func (m *Manager) installed(volume string) map[string]Manifest {
	found := map[string]Manifest{}
	entries, _ := os.ReadDir(m.dir(volume))
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), manifestSuffix) {
			continue
		}
		manifest, err := readManifest(filepath.Join(m.dir(volume), entry.Name()))
		if err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(m.dir(volume), manifest.Name+".raw")); err == nil {
			found[manifest.Name] = manifest
		}
	}
	return found
}

// Status lists the installed plugins and those found on the volumes.
func (m *Manager) Status(mounted []string) Status {
	shares := m.sharePlugins(mounted) // before the lock: a new search may take a while
	m.actions.Lock()
	defer m.actions.Unlock()
	settings := m.current()
	status := Status{Volume: settings.Volume, Kernel: m.kernel, Plugins: []Plugin{}}
	names := map[string]bool{}
	installedAt := map[string]int{}
	if slices.Contains(mounted, settings.Volume) {
		for name, manifest := range m.installed(settings.Volume) {
			plugin := Plugin{Manifest: manifest, Installed: true, Enabled: slices.Contains(settings.Enabled, name)}
			plugin.Official = m.official(filepath.Join(m.dir(settings.Volume), name+manifestSuffix))
			if name == "nvidia" {
				_, err := m.localDriver(m.dir(settings.Volume))
				plugin.Local = err == nil
			}
			_, plugin.Active = m.active[name]
			plugin.Problem = m.problems[name]
			if plugin.Enabled && !m.compatible(manifest) {
				plugin.Problem = "kernel"
			}
			names[name] = true
			installedAt[name] = len(status.Plugins)
			status.Plugins = append(status.Plugins, plugin)
		}
	}
	// Verified release downloads take precedence over unrelated loose files.
	// An identical signed local copy can still be installed without a network.
	catalog, available := m.catalogSnapshot()
	status.Catalog = catalog
	status.DriverUpdate = m.driverSnapshot(settings)
	if status.DriverUpdate.CanRestore {
		bundled, e := readManifest(filepath.Join(m.releaseDir(settings.Volume, m.key()), "nvidia"+manifestSuffix))
		status.DriverUpdate.CanRestore = e == nil && bundled.SHA256 != m.installed(settings.Volume)["nvidia"].SHA256
	}
	downloads := map[string]int{}
	for _, plugin := range available {
		if !names[plugin.Name] {
			names[plugin.Name] = true
			downloads[plugin.Name] = len(status.Plugins)
			status.Plugins = append(status.Plugins, plugin)
		}
	}
	// A newer signed version in the catalog, mostly from the plugin repository.
	for _, plugin := range available {
		if index, ok := installedAt[plugin.Name]; ok {
			installed := &status.Plugins[index]
			if installed.Official && updatable(installed.Manifest, plugin.Manifest) && m.compatible(plugin.Manifest) {
				installed.Update = &PluginUpdate{Version: plugin.Version, Official: true, Download: true}
			}
		}
	}
	// Community plugins from sources: never in place of an official one.
	for _, entry := range m.sourceSnapshot() {
		plugin := entry.plugin
		if index, ok := installedAt[plugin.Name]; ok {
			installed := &status.Plugins[index]
			if installed.Update == nil && !installed.Official && updatable(installed.Manifest, plugin.Manifest) && m.compatible(plugin.Manifest) {
				installed.Update = &PluginUpdate{Version: plugin.Version, Official: plugin.Official, Download: true, Source: plugin.Source}
			}
			continue
		}
		if !names[plugin.Name] {
			names[plugin.Name] = true
			status.Plugins = append(status.Plugins, plugin)
		}
	}
	// Plugins someone put into a share or onto a volume, ready to install.
	for _, file := range shares {
		if index, ok := installedAt[file.manifest.Name]; ok {
			installed := &status.Plugins[index]
			if installed.Update == nil && updatable(installed.Manifest, file.manifest) && file.imageOK && m.compatible(file.manifest) {
				installed.Update = &PluginUpdate{Path: file.path, Version: file.manifest.Version, Official: file.official}
			}
			continue
		}
		if !names[file.manifest.Name] {
			names[file.manifest.Name] = true
			status.Plugins = append(status.Plugins, Plugin{Manifest: file.manifest, Path: file.path, Official: file.official})
		} else if index, ok := downloads[file.manifest.Name]; ok && reflect.DeepEqual(status.Plugins[index].Manifest, file.manifest) && file.official && file.imageOK {
			status.Plugins[index].Download, status.Plugins[index].Path = false, file.path
			delete(downloads, file.manifest.Name)
		}
	}
	sort.Slice(status.Plugins, func(i, j int) bool { return status.Plugins[i].Name < status.Plugins[j].Name })
	m.addHardware(&status)
	status.Sources = m.Sources()
	return status
}

// updatable reports whether found can replace the installed plugin from a
// share. NVIDIA is updated through its driver preparation, with a reboot and a
// way back, never by swapping the image underneath it.
// Only a newer version counts: the old file usually still lies in the share.
func updatable(installed, found Manifest) bool {
	return installed.Name != "nvidia" && !installed.LocalBuild && !found.LocalBuild && found.SHA256 != installed.SHA256 &&
		newerVersion(found.Version, installed.Version)
}

var versionNumbers = regexp.MustCompile(`[0-9]+`)

// newerVersion compares the numbers in two version strings in order
// ("1.10.2" > "1.9"); without numbers, only a different text counts.
func newerVersion(found, installed string) bool {
	a, b := versionNumbers.FindAllString(found, -1), versionNumbers.FindAllString(installed, -1)
	if len(a) == 0 || len(b) == 0 {
		return found != installed
	}
	for i := range max(len(a), len(b)) {
		var x, y string
		if i < len(a) {
			x = strings.TrimLeft(a[i], "0")
		}
		if i < len(b) {
			y = strings.TrimLeft(b[i], "0")
		}
		if len(x) != len(y) {
			return len(x) > len(y)
		}
		if x != y {
			return x > y
		}
	}
	return false
}
