package files

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

type ConflictInfo struct {
	SourcePath    string `json:"sourcePath"`
	SourceVersion string `json:"sourceVersion"`
	TargetVersion string `json:"targetVersion"`
	Path          string `json:"path"`
	Source        Entry  `json:"source"`
	Target        Entry  `json:"target"`
}
type Inspection struct {
	Files     int64          `json:"files"`
	Bytes     int64          `json:"bytes"`
	Conflicts []ConflictInfo `json:"conflicts"`
	Truncated bool           `json:"truncated"`
}

// Inspect walks only on explicit request, confines all reads to authorized
// roots, skips links/internal files, and stops when the request expires.
func (m *Manager) Inspect(ctx context.Context, src Root, paths []string, dst *Root, dir string) (Inspection, error) {
	result := Inspection{Conflicts: []ConflictInfo{}}
	sr, err := os.OpenRoot(src.Dir)
	if err != nil {
		return result, mapError(err)
	}
	defer sr.Close()
	var dr *os.Root
	if dst != nil {
		if !dst.Writable {
			return result, ErrReadOnly
		}
		dir, err = clean(dir)
		if err != nil {
			return result, err
		}
		if dst.secret(dir) {
			return result, ErrNotFound
		}
		dr, err = os.OpenRoot(dst.Dir)
		if err != nil {
			return result, mapError(err)
		}
		defer dr.Close()
		info, e := dr.Stat(dir)
		if e != nil || !info.IsDir() {
			return result, ErrNotFound
		}
	}
	// Only the explicitly selected paths are held in RAM. Remove duplicates
	// and descendants of selected directories before streaming their trees.
	selectedPaths := make([]string, 0, len(paths))
	for _, selected := range paths {
		rel, e := clean(selected)
		if e != nil || rel == "." || src.secret(rel) {
			return result, ErrInvalid
		}
		selectedPaths = append(selectedPaths, rel)
	}
	sort.Strings(selectedPaths)
	last := ""
	for _, rel := range selectedPaths {
		if last != "" && (rel == last || strings.HasPrefix(rel, last+"/")) {
			continue
		}
		last = rel
		err = walkEntries(sr, rel, func(p string, info fs.FileInfo) error {
			if e := ctx.Err(); e != nil {
				return e
			}
			if hidden(path.Base(p)) || src.secret(p) || m.protected(src, p) {
				if info.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return nil
			}
			if info.Mode().IsRegular() {
				result.Files++
				result.Bytes += info.Size()
			}
			if dr != nil {
				target := path.Join(dir, path.Base(rel), strings.TrimPrefix(strings.TrimPrefix(p, rel), "/"))
				if dst.secret(target) {
					return ErrNotFound
				}
				existing, e := dr.Lstat(target)
				if e != nil && !errors.Is(e, fs.ErrNotExist) {
					return e
				}
				if e == nil && !(info.IsDir() && existing.IsDir()) {
					if len(result.Conflicts) >= 200 {
						result.Truncated = true
						if info.IsDir() && !existing.IsDir() {
							return fs.SkipDir
						}
						return nil
					}
					entry := func(i fs.FileInfo) Entry {
						return Entry{Name: i.Name(), Dir: i.IsDir(), Size: i.Size(), Modified: i.ModTime().UTC().Truncate(time.Second)}
					}
					result.Conflicts = append(result.Conflicts, ConflictInfo{SourcePath: p, SourceVersion: entryVersion(info), TargetVersion: entryVersion(existing), Path: target, Source: entry(info), Target: entry(existing)})
					if info.IsDir() && !existing.IsDir() {
						return fs.SkipDir
					}
				}
			}
			return nil
		})
		if err != nil {
			return result, mapError(err)
		}
	}
	return result, nil
}
