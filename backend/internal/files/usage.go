package files

import (
	"context"
	"io/fs"
	"path"
	"sort"
	"time"
)

type UsageItem struct {
	Path  string `json:"path"`
	Bytes int64  `json:"bytes"`
	Files int64  `json:"files"`
	Dir   bool   `json:"dir"`
	// Modified is the modification time of a file (RFC 3339), empty for folders.
	Modified string `json:"modified,omitempty"`
	// Sub are the largest entries one level below a child folder (PLAN 72:
	// folders as areas), at most usageSubs; only on Children.
	Sub []UsageItem `json:"sub,omitempty"`
}
type DirectoryUsage struct {
	Bytes    int64       `json:"bytes"`
	Files    int64       `json:"files"`
	Folders  int64       `json:"folders"`
	Largest  []UsageItem `json:"largest"`
	Children []UsageItem `json:"children"`
	// Old are the largest files not changed since OlderThan (PLAN 72).
	Old []UsageItem `json:"old"`
	// OlderThan is the cut-off used for Old (RFC 3339).
	OlderThan string `json:"olderThan"`
}

// UsageOptions tune a scan. Zero values take the defaults.
type UsageOptions struct {
	ShowHidden bool
	// OlderThanDays: files changed before this many days count as old
	// (default 1095, three years; at most 36500).
	OlderThanDays int
	// MinOldBytes: smaller old files are left out of Old.
	MinOldBytes int64
}

const (
	usageList = 50 // entries in Largest
	usageOld  = 100
	usageSubs = 12
)

// keepLargest inserts an item into a list sorted by size (largest first),
// keeping at most limit entries.
func keepLargest(list []UsageItem, item UsageItem, limit int) []UsageItem {
	if len(list) == limit && list[limit-1].Bytes >= item.Bytes {
		return list
	}
	at := sort.Search(len(list), func(i int) bool {
		a := list[i]
		return a.Bytes < item.Bytes || a.Bytes == item.Bytes && a.Path > item.Path
	})
	list = append(list, UsageItem{})
	copy(list[at+1:], list[at:])
	list[at] = item
	if len(list) > limit {
		list = list[:limit]
	}
	return list
}

func sortBySize(list []UsageItem) {
	sort.Slice(list, func(i, j int) bool {
		a, b := list[i], list[j]
		return a.Bytes > b.Bytes || a.Bytes == b.Bytes && a.Path < b.Path
	})
}

// DirectoryUsage scans explicitly, without following links or reading contents.
// Failure/limits never return misleading partial folder totals.
func (m *Manager) DirectoryUsage(ctx context.Context, root Root, dir string, showHidden bool) (DirectoryUsage, error) {
	return m.DirectoryUsageWith(ctx, root, dir, UsageOptions{ShowHidden: showHidden})
}

// DirectoryUsageWith is DirectoryUsage with options for old files.
func (m *Manager) DirectoryUsageWith(ctx context.Context, root Root, dir string, options UsageOptions) (DirectoryUsage, error) {
	days := options.OlderThanDays
	if days <= 0 {
		days = 1095
	}
	if days > 36500 {
		days = 36500
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	result := DirectoryUsage{Largest: []UsageItem{}, Children: []UsageItem{}, Old: []UsageItem{}, OlderThan: cutoff.UTC().Format(time.RFC3339)}
	dir, err := clean(dir)
	if err != nil {
		return result, err
	}
	children := map[string]*UsageItem{}
	subs := map[string]map[string]*UsageItem{}
	err = m.scanUpTo(ctx, root, dir, options.ShowHidden, usageScanLimit, func(p string, info fs.FileInfo) error {
		top, second := p, ""
		for path.Dir(top) != dir && path.Dir(top) != "." {
			second = top
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
		size := info.Size()
		result.Files++
		result.Bytes += size
		item.Bytes += size
		item.Files++
		if second != "" {
			below := subs[top]
			if below == nil {
				below = map[string]*UsageItem{}
				subs[top] = below
			}
			sub := below[second]
			if sub == nil {
				sub = &UsageItem{Path: second, Dir: second != p}
				below[second] = sub
			}
			sub.Bytes += size
			sub.Files++
		}
		file := UsageItem{Path: p, Bytes: size, Files: 1, Modified: info.ModTime().UTC().Format(time.RFC3339)}
		result.Largest = keepLargest(result.Largest, file, usageList)
		if info.ModTime().Before(cutoff) && size >= options.MinOldBytes {
			result.Old = keepLargest(result.Old, file, usageOld)
		}
		return nil
	})
	if err != nil {
		return DirectoryUsage{}, err
	}
	for top, item := range children {
		for _, sub := range subs[top] {
			item.Sub = append(item.Sub, *sub)
		}
		sortBySize(item.Sub)
		if len(item.Sub) > usageSubs {
			item.Sub = item.Sub[:usageSubs]
		}
		result.Children = append(result.Children, *item)
	}
	sortBySize(result.Children)
	return result, nil
}
