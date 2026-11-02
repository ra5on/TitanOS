package nvr

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
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
		start, err := time.ParseInLocation("2006-01-02/15-04-05", strings.TrimSuffix(filepath.ToSlash(rel), ".mp4"), time.UTC)
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
	r := PruneAll(root, cameras, defaultKeep, reserve, now, free)
	return r.Files, r.Bytes
}

// PruneResult tells what a prune did.
type PruneResult struct {
	Files int
	Bytes int64
	// Shortened names cameras whose recordings had to go before their
	// retention because space ran short, with the days that are left.
	Shortened map[string]int
}

// eventGrace is how long a recording of a camera in mode "events" waits for
// an event before it goes; eventMargin is kept before and after each event.
const (
	eventGrace  = 15 * time.Minute
	eventMargin = time.Minute
)

// PruneAll is Prune with the details.
func PruneAll(root string, cameras []Camera, defaultKeep int, reserve int64, now time.Time, free func(string) (int64, error)) PruneResult {
	res := PruneResult{Shortened: map[string]int{}}
	if free == nil {
		free = FreeBytes
	}
	type item struct {
		path   string
		seg    Segment
		camera string
		keep   int
	}
	var all []item
	known := map[string]bool{}
	for _, c := range cameras {
		known[c.ID] = true
	}
	// Folders of removed cameras age out with the general retention.
	orphan := map[string]bool{}
	if entries, err := os.ReadDir(root); err == nil {
		for _, e := range entries {
			if e.IsDir() && !known[e.Name()] && strings.HasPrefix(e.Name(), "cam-") {
				cameras = append(cameras[:len(cameras):len(cameras)], Camera{ID: e.Name()})
				orphan[e.Name()] = true
			}
		}
	}
	remove := func(path string, s Segment) bool {
		if os.Remove(path) != nil {
			return false
		}
		res.Files++
		res.Bytes += s.Size
		return true
	}
	for _, c := range cameras {
		keep := c.KeepDays
		if keep <= 0 {
			keep = defaultKeep
		}
		var events []Event
		if c.Mode() == "events" {
			events = Events(root, c.ID, 0)
		}
		segs := segments(root, c.ID)
		for i, s := range segs {
			path := filepath.Join(root, c.ID, filepath.FromSlash(s.Name))
			// Nothing writes for a removed camera, so its newest file may go too.
			newest := i == len(segs)-1 && !orphan[c.ID]
			if newest {
				continue
			}
			if now.Sub(s.Start) > time.Duration(keep)*24*time.Hour {
				remove(path, s)
				continue
			}
			end := s.Start.Add(SegmentSeconds * time.Second)
			if events != nil && now.Sub(end) > eventGrace && !nearEvent(s.Start, end, events, now) {
				remove(path, s)
				continue
			}
			all = append(all, item{path, s, c.ID, keep})
		}
		removeEmptyDays(filepath.Join(root, c.ID))
	}
	if reserve > 0 {
		// The deficit is measured once and covered by adding up file sizes:
		// on Btrfs freed space shows in statfs only after the next commit,
		// so asking again after each file would delete nearly everything.
		var deficit int64
		if f, err := free(root); err == nil && f < reserve {
			deficit = reserve - f
		}
		sort.Slice(all, func(i, j int) bool { return all[i].seg.Start.Before(all[j].seg.Start) })
		for _, it := range all {
			if deficit <= 0 {
				break
			}
			if remove(it.path, it.seg) {
				deficit -= it.seg.Size
				if !orphan[it.camera] {
					res.Shortened[it.camera] = it.keep
				}
			}
		}
		// Say how many days are left for the cameras that lost recordings.
		for id := range res.Shortened {
			left := 0
			if segs := segments(root, id); len(segs) > 0 {
				left = int(now.Sub(segs[0].Start).Hours() / 24)
			}
			res.Shortened[id] = left
		}
	}
	return res
}

// nearEvent reports whether [start, end) touches an event with its margin.
func nearEvent(start, end time.Time, events []Event, now time.Time) bool {
	for _, e := range events {
		evEnd := e.End
		if evEnd.IsZero() {
			evEnd = now
		}
		if start.Before(evEnd.Add(eventMargin)) && e.Start.Add(-eventMargin).Before(end) {
			return true
		}
	}
	return false
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
	if _, err := os.Lstat(dst); err == nil {
		return errNotSegment
	}
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
	if _, err := os.Lstat(dst); err == nil {
		return errNotSegment // never overwrite a recording of the same name
	}
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
		start, err := time.ParseInLocation("2006-01-02/15-04-05", strings.TrimSuffix(filepath.ToSlash(rel), ".mp4"), time.UTC)
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

// LatestWrite is when the newest recording of a camera was last written.
func LatestWrite(root, camera string) time.Time {
	segs := segments(root, camera)
	if len(segs) == 0 {
		return time.Time{}
	}
	info, err := os.Stat(filepath.Join(root, camera, filepath.FromSlash(segs[len(segs)-1].Name)))
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// Usage is what lies on disk for one camera.
type Usage struct {
	Segments int
	Bytes    int64
	DayBytes int64 // of the last 24 hours
	Latest   time.Time
}

type usageCache struct {
	mu   sync.Mutex
	root string
	at   time.Time
	data map[string]Usage
}

var cachedUsage usageCache

// CameraUsage sums the recordings of each camera. The answer is kept for
// 30 seconds: the page and Home Assistant ask often, and walking every
// recording file of every camera each time is costly.
func CameraUsage(root string, cameras []Camera, now time.Time) map[string]Usage {
	c := &cachedUsage
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.root == root && now.Sub(c.at) < 30*time.Second && c.data != nil {
		return c.data
	}
	out := map[string]Usage{}
	dayAgo := now.Add(-24 * time.Hour)
	for _, cam := range cameras {
		var u Usage
		segs := segments(root, cam.ID)
		u.Segments = len(segs)
		for _, s := range segs {
			u.Bytes += s.Size
			if s.Start.After(dayAgo) {
				u.DayBytes += s.Size
			}
		}
		if len(segs) > 0 {
			u.Latest = segs[len(segs)-1].Start
		}
		out[cam.ID] = u
	}
	c.root, c.at, c.data = root, now, out
	return out
}
