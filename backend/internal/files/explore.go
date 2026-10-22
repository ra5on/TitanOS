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

var ErrScanLimit = errors.New("files: scan limit reached")

const scanLimit = 50000

type FoundEntry struct {
	Version string `json:"version,omitempty"`
	Entry
	Path string `json:"path"`
}
type SearchOptions struct {
	Path, Query, Extension string
	Extensions             []string
	MinSize, MaxSize       int64
	After, Before          time.Time
	ShowHidden             bool
}
type SearchResult struct {
	Entries   []FoundEntry `json:"entries"`
	Scanned   int          `json:"scanned"`
	Truncated bool         `json:"truncated"`
}

// scan is demand-driven, confined to the selected root and does not follow links.
func (m *Manager) scan(ctx context.Context, root Root, dir string, showHidden bool, visit func(string, fs.FileInfo) error) error {
	dir, err := clean(dir)
	if err != nil || root.secret(dir) || m.protected(root, dir) {
		return ErrInvalid
	}
	r, err := os.OpenRoot(root.Dir)
	if err != nil {
		return mapError(err)
	}
	defer r.Close()
	count := 0
	return fs.WalkDir(r.FS(), dir, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return mapError(e)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if p != dir && (hidden(d.Name()) || root.secret(p) || (!showHidden && strings.HasPrefix(d.Name(), ".")) || m.protected(root, p)) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		count++
		if count > scanLimit {
			return ErrScanLimit
		}
		info, e := d.Info()
		if e != nil {
			return mapError(e)
		}
		if p == dir && info.IsDir() {
			return nil
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return nil
		}
		return visit(p, info)
	})
}
func (m *Manager) Search(ctx context.Context, root Root, opt SearchOptions) (SearchResult, error) {
	result := SearchResult{Entries: []FoundEntry{}}
	if opt.MinSize < 0 || opt.MaxSize < 0 || (opt.MaxSize > 0 && opt.MinSize > opt.MaxSize) || (!opt.Before.IsZero() && !opt.After.IsZero() && opt.After.After(opt.Before)) {
		return result, ErrInvalid
	}
	query := strings.ToLower(opt.Query)
	extension := strings.ToLower(strings.TrimPrefix(strings.TrimSpace(opt.Extension), "."))
	extensions := map[string]bool{}
	if extension != "" {
		extensions[extension] = true
	}
	for _, ext := range opt.Extensions {
		extensions[strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))] = true
	}
	err := m.scan(ctx, root, opt.Path, opt.ShowHidden, func(p string, info fs.FileInfo) error {
		result.Scanned++
		if info.IsDir() {
			return nil
		}
		if !strings.Contains(strings.ToLower(info.Name()), query) || len(extensions) > 0 && !extensions[strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))] || info.Size() < opt.MinSize || opt.MaxSize > 0 && info.Size() > opt.MaxSize || !opt.After.IsZero() && info.ModTime().Before(opt.After) || !opt.Before.IsZero() && info.ModTime().After(opt.Before) {
			return nil
		}
		if len(result.Entries) >= 1000 {
			return ErrScanLimit
		}
		result.Entries = append(result.Entries, FoundEntry{Entry: Entry{Name: info.Name(), Size: info.Size(), Modified: info.ModTime()}, Path: p})
		return nil
	})
	if errors.Is(err, ErrScanLimit) {
		result.Truncated = true
		err = nil
	}
	return result, err
}

type Difference struct {
	Path         string `json:"path"`
	State        string `json:"state"`
	LeftVersion  string `json:"leftVersion,omitempty"`
	RightVersion string `json:"rightVersion,omitempty"`
	Left, Right  *Entry
}
type Comparison struct {
	Entries  []Difference `json:"entries"`
	Same     int          `json:"same"`
	Checksum bool         `json:"checksum"`
}

func (m *Manager) Compare(ctx context.Context, left Root, leftDir string, right Root, rightDir string, checksum, showHidden bool) (Comparison, error) {
	result := Comparison{Entries: []Difference{}, Checksum: checksum}
	read := func(root Root, dir string) (map[string]FoundEntry, error) {
		items := map[string]FoundEntry{}
		dir, err := clean(dir)
		if err != nil {
			return nil, err
		}
		err = m.scan(ctx, root, dir, showHidden, func(p string, info fs.FileInfo) error {
			relative := p
			if dir != "." {
				relative = strings.TrimPrefix(p, dir+"/")
			}
			items[relative] = FoundEntry{Version: entryVersion(info), Entry: Entry{Name: info.Name(), Dir: info.IsDir(), Size: info.Size(), Modified: info.ModTime()}, Path: p}
			return nil
		})
		return items, err
	}
	a, err := read(left, leftDir)
	if err != nil {
		return result, err
	}
	b, err := read(right, rightDir)
	if err != nil {
		return result, err
	}
	names := []string{}
	for name := range a {
		names = append(names, name)
	}
	for name := range b {
		if _, ok := a[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		x, xok := a[name]
		y, yok := b[name]
		row := Difference{Path: name, State: "same"}
		if xok {
			row.Left = &x.Entry
			row.LeftVersion = x.Version
		}
		if yok {
			row.Right = &y.Entry
			row.RightVersion = y.Version
		}
		switch {
		case !xok:
			row.State = "right"
		case !yok:
			row.State = "left"
		case x.Dir != y.Dir:
			row.State = "different"
		case !x.Dir:
			if x.Size != y.Size {
				row.State = "different"
			} else if checksum {
				xsum, e := m.Properties(ctx, left, x.Path, true)
				if e != nil {
					return result, e
				}
				ysum, e := m.Properties(ctx, right, y.Path, true)
				if e != nil {
					return result, e
				}
				if xsum.SHA256 != ysum.SHA256 {
					row.State = "different"
				}
			} else if !x.Modified.Equal(y.Modified) {
				row.State = "different"
			}
		}
		if row.State == "same" {
			result.Same++
		} else {
			result.Entries = append(result.Entries, row)
		}
	}
	return result, nil
}
