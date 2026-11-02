package nvr

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// Segment is one recording file.
type Segment struct {
	Camera string    `json:"camera"`
	Name   string    `json:"name"` // "2026-11-02/14-05-00.mp4"
	Start  time.Time `json:"start"`
	Size   int64     `json:"size"`
}

// segments lists the recordings of one camera, oldest first.
func segments(root, camera string) []Segment {
	var out []Segment
	base := filepath.Join(root, camera)
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && path != base && (d.Name() == ProtectedDir || d.Name() == "events") {
			return fs.SkipDir // protected clips and event pictures are not recordings to prune
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".mp4") {
			return nil
		}
		rel, _ := filepath.Rel(base, path)
		start, err := time.ParseInLocation("2006-01-02/15-04-05", strings.TrimSuffix(filepath.ToSlash(rel), ".mp4"), time.Local)
		if err != nil {
			return nil // not ours
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		out = append(out, Segment{Camera: camera, Name: filepath.ToSlash(rel), Start: start, Size: info.Size()})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// Segments lists the recordings of a camera, oldest first.
func Segments(root, camera string) []Segment { return segments(root, camera) }

// FreeBytes is the space free on the volume holding path.
func FreeBytes(path string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, err
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}

// Prune removes recordings older than their retention and then, while less
// than reserve bytes are free, the oldest ones of all cameras. It returns
// how many files and bytes went. The newest file of a camera is never
// removed here: it may be open.
func Prune(root string, cameras []Camera, defaultKeep int, reserve int64, now time.Time, free func(string) (int64, error)) (files int, bytes int64) {
	if free == nil {
		free = FreeBytes
	}
	type item struct {
		path string
		seg  Segment
	}
	var all []item
	known := map[string]bool{}
	// Folders of removed cameras age out with the general retention.
	for _, c := range cameras {
		known[c.ID] = true
	}
	orphan := map[string]bool{}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() && !known[e.Name()] && strings.HasPrefix(e.Name(), "cam-") {
				cameras = append(cameras[:len(cameras):len(cameras)], Camera{ID: e.Name()})
				orphan[e.Name()] = true
			}
		}
	}
	for _, c := range cameras {
		known[c.ID] = true
		keep := c.KeepDays
		if keep <= 0 {
			keep = defaultKeep
		}
		segs := segments(root, c.ID)
		for i, s := range segs {
			path := filepath.Join(root, c.ID, filepath.FromSlash(s.Name))
			// Nothing writes for a removed camera, so its newest file may go too.
			newest := i == len(segs)-1 && !orphan[c.ID]
			if !newest && now.Sub(s.Start) > time.Duration(keep)*24*time.Hour {
				if os.Remove(path) == nil {
					files++
					bytes += s.Size
				}
				continue
			}
			if !newest {
				all = append(all, item{path, s})
			}
		}
		removeEmptyDays(filepath.Join(root, c.ID))
	}
	if reserve > 0 {
		sort.Slice(all, func(i, j int) bool { return all[i].seg.Start.Before(all[j].seg.Start) })
		for _, it := range all {
			if f, err := free(root); err != nil || f >= reserve {
				break
			}
			if os.Remove(it.path) == nil {
				files++
				bytes += it.seg.Size
			}
		}
	}
	return files, bytes
}

func removeEmptyDays(camDir string) {
	entries, err := os.ReadDir(camDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			_ = os.Remove(filepath.Join(camDir, e.Name())) // fails while not empty
		}
	}
}

// DiskUse sums the size of all recordings.
func DiskUse(root string, cameras []Camera) int64 {
	var total int64
	for _, c := range cameras {
		for _, s := range segments(root, c.ID) {
			total += s.Size
		}
	}
	return total
}

// ProtectedDir holds clips that retention never removes.
const ProtectedDir = "protected"

var errNotSegment = errors.New("nvr: no such recording")

func segmentPath(root, camera, day, file string) string {
	return filepath.Join(root, camera, day, file)
}

// Protect moves one recording into the protected folder of its camera, where
// neither age nor lack of space removes it. The file being written right now
// (the newest) cannot be protected.
func Protect(root, camera, day, file string) error {
	src := segmentPath(root, camera, day, file)
	segs := segments(root, camera)
	if len(segs) == 0 || segs[len(segs)-1].Name == day+"/"+file {
		return errNotSegment
	}
	if info, err := os.Stat(src); err != nil || !info.Mode().IsRegular() {
		return errNotSegment
	}
	dst := filepath.Join(root, camera, ProtectedDir, day, file)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// Unprotect puts a protected clip back where retention applies again.
func Unprotect(root, camera, day, file string) error {
	src := filepath.Join(root, camera, ProtectedDir, day, file)
	if info, err := os.Stat(src); err != nil || !info.Mode().IsRegular() {
		return errNotSegment
	}
	dst := segmentPath(root, camera, day, file)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.Rename(src, dst)
}

// Protected lists the protected clips of a camera, oldest first.
func Protected(root, camera string) []Segment {
	var out []Segment
	base := filepath.Join(root, camera, ProtectedDir)
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".mp4") {
			return nil
		}
		rel, _ := filepath.Rel(base, path)
		start, err := time.ParseInLocation("2006-01-02/15-04-05", strings.TrimSuffix(filepath.ToSlash(rel), ".mp4"), time.Local)
		if err != nil {
			return nil
		}
		if info, err := d.Info(); err == nil {
			out = append(out, Segment{Camera: camera, Name: filepath.ToSlash(rel), Start: start, Size: info.Size()})
		}
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}
