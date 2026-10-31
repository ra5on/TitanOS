package plugins

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// shareFile is a plugin found in a share or on a volume. Installing it checks
// everything again; this is only what the list shows.
type shareFile struct {
	manifest Manifest
	path     string
	official bool
	imageOK  bool // a regular image of the size the manifest names lies next to it
}

// ScanShares looks for plugins on the mounted volumes, outside private and
// app/VM folders, reading at most 50,000 entries. The status poll reuses the
// result instead of reading the disks every few seconds (and waking them);
// the page asks for a new search when it opens and on refresh.
func (m *Manager) ScanShares(mounted []string) {
	var found []shareFile
	visited := 0
	for _, volume := range mounted {
		root := filepath.Join(m.mountRoot, volume)
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error { // the callback collects what it can; unreadable entries are skipped
			visited++
			if err != nil || visited > 50000 {
				return fs.SkipDir
			}
			if entry.IsDir() {
				depth := strings.Count(strings.TrimPrefix(path, root), "/")
				if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "appdata" || entry.Name() == "vms" || depth > 4) {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), manifestSuffix) {
				return nil
			}
			if manifest, err := readManifest(path); err == nil {
				file := shareFile{manifest: manifest, path: path, official: m.official(path)}
				image, err := os.Stat(filepath.Join(filepath.Dir(path), manifest.Name+".raw"))
				file.imageOK = err == nil && image.Mode().IsRegular() && image.Size() == manifest.Size
				found = append(found, file)
			}
			return nil
		})
	}
	m.mu.Lock()
	m.shareFiles, m.sharesFor, m.sharesLoaded = found, volumesKey(mounted), true
	m.mu.Unlock()
}

// sharePlugins returns the last search, searching again only if there was
// none yet or other volumes are mounted now.
func (m *Manager) sharePlugins(mounted []string) []shareFile {
	m.mu.Lock()
	fresh := m.sharesLoaded && m.sharesFor == volumesKey(mounted)
	m.mu.Unlock()
	if !fresh {
		m.ScanShares(mounted)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shareFiles
}

func volumesKey(mounted []string) string {
	return strings.Join(slices.Sorted(slices.Values(mounted)), "\x00")
}

// compatible reports whether a plugin fits the running kernel.
func (m *Manager) compatible(manifest Manifest) bool {
	return manifest.Kernel == "" || manifest.Kernel == m.kernel
}
