package vm

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
)

// Medium is an installation medium found on a volume.
type Medium struct {
	Path      string `json:"path"`
	SizeBytes int64  `json:"sizeBytes"`
}

// walkLimit bounds the entries Media and Images look at on one volume.
var walkLimit = 50000

// Media looks for ISO files on the mounted volumes, a few folders deep.
func (m *Manager) Media(mounted []string) []Medium {
	media := []Medium{}
	for _, volume := range mounted {
		visited := 0 // each volume has its own walkLimit
		root := filepath.Join(m.mountRoot, volume)
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error { // the callback collects what it can; unreadable entries are skipped
			visited++
			if err != nil || visited > walkLimit || len(media) >= 200 {
				return fs.SkipDir
			}
			if entry.IsDir() {
				depth := strings.Count(strings.TrimPrefix(path, root), "/")
				// Docker's data, app data and recycle bins hold no media.
				if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "appdata" || depth > 4) {
					return fs.SkipDir
				}
				return nil
			}
			if strings.EqualFold(filepath.Ext(entry.Name()), ".iso") && entry.Type().IsRegular() {
				if info, err := entry.Info(); err == nil {
					media = append(media, Medium{Path: path, SizeBytes: info.Size()})
				}
			}
			return nil
		})
	}
	sort.Slice(media, func(i, j int) bool { return media[i].Path < media[j].Path })
	return media
}
