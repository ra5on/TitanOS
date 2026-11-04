package files

import (
	"context"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

type ConflictInfo struct {
	// Identical is set for two files of the same size and modification
	// time (within two seconds): "missing or changed" leaves them alone.
	Identical     bool   `json:"identical"`
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
	// Counted over all conflicts, also beyond the listed 200: files that
	// are identical at the target, files that differ, and entries where a
	// file meets a folder.
	Identical      int64 `json:"identical"`
	IdenticalBytes int64 `json:"identicalBytes"`
	Different      int64 `json:"different"`
	DifferentBytes int64 `json:"differentBytes"`
	TypeMismatch   int64 `json:"typeMismatch"`
	// Unreadable lists (the first 20 of) the folders that could not be
	// read; UnreadableCount counts them all. The job reports them again.
	Unreadable      []string `json:"unreadable"`
	UnreadableCount int64    `json:"unreadableCount"`
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
	result.Unreadable = []string{}
	unreadable := func(p string) {
		result.UnreadableCount++
		if len(result.Unreadable) < 20 {
			result.Unreadable = append(result.Unreadable, p)
		}
	}
	entry := func(i fs.FileInfo) Entry {
		return Entry{Name: i.Name(), Dir: i.IsDir(), Size: i.Size(), Modified: i.ModTime().UTC().Truncate(time.Second)}
	}
	var visit func(rel, p string, info fs.FileInfo) error
	visit = func(rel, p string, info fs.FileInfo) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if hidden(path.Base(p)) || src.secret(p) || m.protected(src, p) || info.Mode()&os.ModeSymlink != 0 {
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
			// A target that cannot be read counts as missing; the job decides again.
			// The source itself (copying into its own folder) is no conflict.
			if existing, e := dr.Lstat(target); e == nil && !(info.IsDir() && existing.IsDir()) && !os.SameFile(info, existing) {
				identical := sameContent(info, existing)
				switch {
				case identical:
					result.Identical++
					result.IdenticalBytes += info.Size()
				case info.IsDir() != existing.IsDir():
					result.TypeMismatch++
				case info.Mode().IsRegular():
					result.Different++
					result.DifferentBytes += info.Size()
				}
				if len(result.Conflicts) < 200 {
					result.Conflicts = append(result.Conflicts, ConflictInfo{Identical: identical, SourcePath: p, SourceVersion: entryVersion(info), TargetVersion: entryVersion(existing), Path: target, Source: entry(info), Target: entry(existing)})
				} else {
					result.Truncated = true
				}
				if info.IsDir() {
					return nil // a folder that meets a file is one conflict
				}
			}
		}
		if !info.IsDir() {
			return nil
		}
		var inner error
		err := eachChild(sr, p, func(child fs.DirEntry) error {
			c := path.Join(p, child.Name())
			ci, e := sr.Lstat(c)
			if e != nil {
				return nil // vanished in between
			}
			inner = visit(rel, c, ci)
			return inner
		})
		if inner != nil {
			return inner
		}
		if err != nil {
			unreadable(p)
		}
		return nil
	}
	last := ""
	for _, rel := range selectedPaths {
		if last != "" && (rel == last || strings.HasPrefix(rel, last+"/")) {
			continue
		}
		last = rel
		info, e := sr.Lstat(rel)
		if e != nil {
			return result, mapError(e)
		}
		if err := visit(rel, rel, info); err != nil {
			return result, mapError(err)
		}
	}
	return result, nil
}
