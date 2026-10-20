package files

import (
	"context"
	"io/fs"
	"path"
	"sort"
)

type UsageItem struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int64  `json:"files"`
	Dir   bool   `json:"dir"`
}
type DirectoryUsage struct {
	Bytes    int64       `json:"bytes"`
	Files    int64       `json:"files"`
	Folders  int64       `json:"folders"`
	Largest  []UsageItem `json:"largest"`
	Children []UsageItem `json:"children"`
}

// DirectoryUsage scans explicitly, without following links or reading contents.
// Failure/limits never return misleading partial folder totals.
func (m *Manager) DirectoryUsage(ctx context.Context, root Root, dir string, showHidden bool) (DirectoryUsage, error) {
	result := DirectoryUsage{Largest: []UsageItem{}, Children: []UsageItem{}}
	dir, err := clean(dir)
	if err != nil {
		return result, err
	}
	children := map[string]*UsageItem{}
	err = m.scan(ctx, root, dir, showHidden, func(p string, info fs.FileInfo) error {
		top := p
		for path.Dir(top) != dir && path.Dir(top) != "." {
			top = path.Dir(top)
		}
		item := children[top]
		if item == nil {
			item = &UsageItem{Path: top, Dir: info.IsDir() || top != p}
			children[top] = item
		}
		if info.IsDir() {
			result.Folders++
			return nil
		}
		result.Files++
		result.Bytes += info.Size()
		item.Bytes += info.Size()
		item.Files++
		result.Largest = append(result.Largest, UsageItem{Path: p, Bytes: info.Size(), Files: 1})
		sort.Slice(result.Largest, func(i, j int) bool {
			a, b := result.Largest[i], result.Largest[j]
			return a.Bytes > b.Bytes || a.Bytes == b.Bytes && a.Path < b.Path
		})
		if len(result.Largest) > 50 {
			result.Largest = result.Largest[:50]
		}
		return nil
	})
	if err != nil {
		return DirectoryUsage{}, err
	}
	for _, item := range children {
		result.Children = append(result.Children, *item)
	}
	sort.Slice(result.Children, func(i, j int) bool {
		a, b := result.Children[i], result.Children[j]
		return a.Bytes > b.Bytes || a.Bytes == b.Bytes && a.Path < b.Path
	})
	return result, nil
}
