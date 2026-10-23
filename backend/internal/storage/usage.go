package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"syscall"
	"time"
)

type UsageEntry struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Bytes      uint64 `json:"bytes"`
	Allocated  uint64 `json:"allocated"`
	Files      int    `json:"files"`
	Incomplete bool   `json:"incomplete"`
	Error      string `json:"error"`
}
type UsageReport struct {
	At         time.Time    `json:"at"`
	Entries    []UsageEntry `json:"entries"`
	Incomplete bool         `json:"incomplete"`
	Scanned    int          `json:"scanned"`
}

// Usage is explicitly requested; it never indexes data in the background. Allocated
// bytes may share extents (reflinks/snapshots), so these figures are not additive physical usage.
func (m *Manager) Usage(ctx context.Context, volume string) (UsageReport, error) {
	out := UsageReport{At: time.Now().UTC(), Entries: []UsageEntry{}}
	if !containsMounted(m.Mounted(), volume) {
		return out, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	root, err := os.OpenRoot(m.MountPoint(volume))
	if err != nil {
		return out, err
	}
	defer root.Close()
	dir, err := root.Open(".")
	if err != nil {
		return out, err
	}
	top, err := dir.ReadDir(10001)
	dir.Close()
	if err != nil && !errors.Is(err, io.EOF) && len(top) == 0 {
		return out, err
	}
	if len(top) > 10000 {
		top = top[:10000]
		out.Incomplete = true
	}
	// Scan only public top-level trees and the dedicated snapshot directory. Private
	// chat, credential and operational state directories are deliberately excluded.
	paths := []string{}
	for _, e := range top {
		if e.Name() == ".titanos" {
			paths = append(paths, ".titanos/snapshots")
			continue
		}
		paths = append(paths, e.Name())
	}
	sort.Strings(paths)
	for index := 0; index < len(paths); index++ {
		name := paths[index]
		sum := sha256.Sum256([]byte(name))
		kind := "other"
		switch {
		case name == "vms":
			kind = "vms"
		case name == "appdata" || name == "docker":
			kind = "apps"
		case name == ".titanos/snapshots":
			kind = "snapshots"
		case path.Base(name) == ".recycle":
			kind = "recycle"
		}
		entry := UsageEntry{ID: hex.EncodeToString(sum[:12]), Name: name, Kind: kind}
		seen := map[[2]uint64]bool{}
		err := usageWalk(root, name, name, func(p string, e fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if out.Scanned >= 200000 {
				return errors.New("scan entry limit reached")
			}
			out.Scanned++
			if walkErr != nil {
				entry.Incomplete = true
				entry.Error = walkErr.Error()
				if e != nil && e.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if e.Type()&os.ModeSymlink != 0 {
				return nil
			}
			if p != name && e.IsDir() && e.Name() == ".recycle" && kind != "snapshots" {
				paths = append(paths, p)
				return fs.SkipDir
			}
			if p != name && (e.Name() == ".titanos" || strings.Contains(e.Name(), ".titanos-")) {
				if e.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			info, err := e.Info()
			if err != nil {
				entry.Incomplete = true
				entry.Error = err.Error()
				return nil
			}
			if !info.Mode().IsRegular() {
				return nil
			}
			st, ok := info.Sys().(*syscall.Stat_t)
			if !ok {
				return errors.New("file identity unavailable")
			}
			key := [2]uint64{uint64(st.Dev), st.Ino}
			if seen[key] {
				return nil
			}
			seen[key] = true
			entry.Files++
			entry.Bytes += uint64(info.Size())
			entry.Allocated += uint64(max(st.Blocks, 0)) * 512
			return nil
		})
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && name == ".titanos/snapshots" {
				continue
			}
			entry.Incomplete = true
			entry.Error = err.Error()
		}
		out.Incomplete = out.Incomplete || entry.Incomplete
		out.Entries = append(out.Entries, entry)
		if ctx.Err() != nil || out.Scanned >= 200000 {
			out.Incomplete = true
			break
		}
	}
	return out, nil
}
func containsMounted(list []string, name string) bool {
	for _, v := range list {
		if v == name {
			return true
		}
	}
	return false
}

// Read directory entries in bounded batches. WalkDir would first allocate every
// name in a large directory, before its visitor could enforce the scan limit.
func usageWalk(root *os.Root, relative, logical string, visit fs.WalkDirFunc) error {
	info, err := root.Lstat(relative)
	if err != nil {
		return visit(logical, nil, err)
	}
	entry := fs.FileInfoToDirEntry(info)
	if err := visit(logical, entry, nil); err != nil {
		if err == fs.SkipDir {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return nil
	}
	child, err := root.OpenRoot(relative)
	if err != nil {
		return visit(logical, entry, err)
	}
	defer child.Close()
	dir, err := child.Open(".")
	if err != nil {
		return visit(logical, entry, err)
	}
	defer dir.Close()
	pinned, err := dir.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(info, pinned) {
		return errors.New("directory changed during scan")
	}
	for {
		entries, err := dir.ReadDir(256)
		if err != nil && !errors.Is(err, io.EOF) {
			return visit(logical, entry, err)
		}
		for _, e := range entries {
			if err := usageWalk(child, e.Name(), path.Join(logical, e.Name()), visit); err != nil {
				return err
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	return nil
}
