package backup

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// What a job saves. Sources are shares, whole unless Folders names the
// folders of a share to take instead. Skip leaves folders out ("share/dir")
// and Patterns leaves out whatever matches, such as *.tmp. Extras are places
// that belong to TitanOS rather than to a share:
//   - appdata: the data of the apps, /mnt/<volume>/appdata on every volume
//   - vms: the disks of the VMs, /mnt/<volume>/vms
//   - config: the settings on the stick, /config

// extras are the places a job may add to its shares.
var extras = []string{"appdata", "vms", "config"}

const (
	maxFolders  = 200
	maxPatterns = 50
)

// ErrFolder means a folder of a share could not be listed.
var ErrFolder = fmt.Errorf("backup: cannot list this folder")

// relative reports whether text is a clean path inside a share.
func relative(text string) bool {
	return text != "" && len(text) <= 500 && !path.IsAbs(text) && path.Clean(text) == text && text != "." &&
		text != ".." && !strings.HasPrefix(text, "../") && !strings.ContainsAny(text, "\x00\n\r")
}

// checkSelection tells whether what a job picks out makes sense.
func checkSelection(job Job) error {
	if len(job.Sources) == 0 && len(job.Extras) == 0 && len(job.Applications) == 0 {
		return invalid("no share chosen")
	}
	for _, extra := range job.Extras {
		if !slices.Contains(extras, extra) {
			return invalid("place to save")
		}
	}
	count := 0
	for share, folders := range job.Folders {
		if !slices.Contains(job.Sources, share) {
			return invalid("folder of a share that is not chosen")
		}
		for _, folder := range folders {
			count++
			if !relative(folder) {
				return invalid("folder")
			}
		}
	}
	for _, skip := range job.Skip {
		count++
		share, folder, ok := strings.Cut(skip, "/")
		if !ok || !slices.Contains(job.Sources, share) || !relative(folder) {
			return invalid("folder to leave out")
		}
	}
	if count > maxFolders || len(job.Patterns) > maxPatterns {
		return invalid("too many folders or patterns")
	}
	for _, pattern := range job.Patterns {
		if pattern == "" || len(pattern) > 200 || strings.ContainsAny(pattern, "\x00\n\r") || strings.HasPrefix(pattern, "-") {
			return invalid("pattern")
		}
	}
	return nil
}

// selection is what restic is given.
type selection struct {
	paths, excludes []string
	// notes are folders that were chosen but are not there.
	notes []string
	// missing are shares and places that are not there at all: their
	// volume is not mounted. Old backups are the only copy of them then.
	missing []string
}

// volumeOf returns the volume a path under the mount root is on.
func (m *Manager) volumeOf(p string) string {
	rel, err := filepath.Rel(m.mountRoot, p)
	if err != nil || strings.HasPrefix(rel, "..") {
		return ""
	}
	volume, _, _ := strings.Cut(rel, string(filepath.Separator))
	return volume
}

// selection works out the paths of a job. The volume a local backup goes to
// is never saved: a copy next to the original does not survive what kills it.
func (m *Manager) selection(job Job) selection {
	var sel selection
	shares := map[string]Source{}
	for _, source := range m.sources() {
		shares[source.Name] = source
	}
	add := func(p string) {
		if job.Destination.Type == "local" && m.volumeOf(p) == job.Destination.Volume {
			return
		}
		sel.paths = append(sel.paths, p)
	}
	for _, name := range job.Sources {
		source, ok := shares[name]
		if !ok {
			sel.missing = append(sel.missing, name)
			continue
		}
		folders := job.Folders[name]
		if len(folders) == 0 {
			add(source.Path)
			continue
		}
		for _, folder := range folders {
			full := filepath.Join(source.Path, filepath.FromSlash(folder))
			if info, err := os.Lstat(full); err != nil || !info.IsDir() {
				sel.notes = append(sel.notes, name+"/"+folder)
				continue
			}
			add(full)
		}
	}
	for _, skip := range job.Skip {
		share, folder, _ := strings.Cut(skip, "/")
		if source, ok := shares[share]; ok {
			sel.excludes = append(sel.excludes, filepath.Join(source.Path, filepath.FromSlash(folder)))
		}
	}
	sel.excludes = append(sel.excludes, job.Patterns...)
	for _, extra := range job.Extras {
		switch extra {
		case "appdata", "vms":
			volumes := m.mounted()
			sort.Strings(volumes)
			for _, volume := range volumes {
				full := filepath.Join(m.mountRoot, volume, extra)
				if info, err := os.Stat(full); err == nil && info.IsDir() {
					add(full)
				}
			}
		case "config":
			// /config is a link into the mounted stick; restic wants the folder.
			if real, err := filepath.EvalSymlinks(m.configDir); err == nil {
				add(real)
			} else {
				sel.missing = append(sel.missing, "config")
			}
		}
	}
	return sel
}

// homeVolume is the volume where a job keeps its cache and what it knows.
func (m *Manager) homeVolume(job Job) string {
	for _, source := range m.sources() {
		if slices.Contains(job.Sources, source.Name) {
			return source.Volume
		}
	}
	if len(job.Extras) > 0 || len(job.Applications) > 0 {
		mounted := m.mounted()
		sort.Strings(mounted)
		for _, volume := range mounted {
			if !(job.Destination.Type == "local" && volume == job.Destination.Volume) {
				return volume
			}
		}
	}
	return ""
}

// Folders lists the folders directly inside dir of a share, for choosing.
func (m *Manager) Folders(share, dir string) ([]string, error) {
	var source *Source
	for _, candidate := range m.sources() {
		if candidate.Name == share {
			source = &candidate
			break
		}
	}
	if source == nil {
		return nil, ErrNotFound
	}
	if dir != "" && !relative(dir) {
		return nil, ErrInvalid
	}
	root, err := os.OpenRoot(source.Path)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFolder, err)
	}
	defer root.Close()
	handle, err := root.Open(filepath.FromSlash(orDot(dir)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFolder, err)
	}
	defer handle.Close()
	entries, err := handle.ReadDir(-1)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFolder, err)
	}
	folders := []string{}
	for _, entry := range entries {
		// The recycle bin and half uploads are never saved; links are not followed.
		if entry.IsDir() && entry.Name() != ".recycle" && entry.Type()&os.ModeSymlink == 0 {
			folders = append(folders, entry.Name())
		}
	}
	sort.Strings(folders)
	return folders, nil
}

func orDot(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}
