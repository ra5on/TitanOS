package files

import (
	"context"
	"io/fs"
	"os"
	"sort"
)

type DuplicateGroup struct {
	Versions map[string]string `json:"versions"`
	SHA256   string            `json:"sha256"`
	Size     int64             `json:"size"`
	Paths    []string          `json:"paths"`
}
type DuplicateResult struct {
	Groups  []DuplicateGroup `json:"groups"`
	Scanned int              `json:"scanned"`
	Hashed  int              `json:"hashed"`
}

// Only equal-sized candidates are read. No background index and no deletions.
func (m *Manager) Duplicates(ctx context.Context, root Root, dir string, showHidden bool) (DuplicateResult, error) {
	result := DuplicateResult{Groups: []DuplicateGroup{}}
	sizes := map[int64][]string{}
	err := m.scan(ctx, root, dir, showHidden, func(p string, info fs.FileInfo) error {
		if info.Mode().IsRegular() {
			sizes[info.Size()] = append(sizes[info.Size()], p)
			result.Scanned++
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	for size, paths := range sizes {
		if len(paths) < 2 {
			continue
		}
		hashes := map[string][]string{}
		versions := map[string]string{}
		for _, p := range paths {
			v, e := m.Properties(ctx, root, p, true)
			if e != nil {
				return result, e
			}
			if v.Size != size {
				return result, ErrTextConflict
			}
			hashes[v.SHA256] = append(hashes[v.SHA256], p)
			result.Hashed++
			versions[p] = v.Version
		}
		for hash, items := range hashes {
			if len(items) > 1 {
				sort.Strings(items)
				groupVersions := map[string]string{}
				for _, p := range items {
					groupVersions[p] = versions[p]
				}
				result.Groups = append(result.Groups, DuplicateGroup{Versions: groupVersions, SHA256: hash, Size: size, Paths: items})
			}
		}
	}
	for _, g := range result.Groups {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := m.CheckVersions(root, g.Paths, g.Versions); err != nil {
			return result, err
		}
	}
	sort.Slice(result.Groups, func(i, j int) bool {
		a, b := result.Groups[i], result.Groups[j]
		if a.Size == b.Size {
			return a.Paths[0] < b.Paths[0]
		}
		return a.Size > b.Size
	})
	return result, nil
}

func (m *Manager) CheckVersions(root Root, paths []string, versions map[string]string) error {
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return mapError(err)
	}
	defer r.Close()
	for _, p := range paths {
		rel, e := clean(p)
		if e != nil || root.secret(rel) || m.protected(root, rel) {
			return ErrInvalid
		}
		if versions[p] == "" {
			return ErrTextConflict
		}
		if e = checkChoice(r, rel, versions[p]); e != nil {
			return e
		}
	}
	return nil
}
