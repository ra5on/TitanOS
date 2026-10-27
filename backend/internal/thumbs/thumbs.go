// Package thumbs makes small previews of pictures, PDFs and videos for the
// file manager and keeps them on the data volume of the file.
//
// The cache lives under /mnt/<volume>/.titanos/thumbs (volumedata), never in
// RAM or on the stick. Nothing is made in the background: a thumbnail is
// made when a browser asks for it, so sleeping disks are not woken up.
package thumbs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Sizes are the edge lengths a thumbnail may have; requests are rounded up.
var Sizes = []int{160, 320, 640, 1280}

// cacheVersion changes the cache key when the rendering changes.
const cacheVersion = "1"

// Manager makes and caches thumbnails.
type Manager struct {
	// MountRoot is where volumes are mounted ("/mnt"); empty disables the cache.
	MountRoot string
	Mounted   func() []string
	// Limit is the cache size per volume in bytes.
	Limit int64

	slots    chan struct{}
	lookPath func(string) (string, error)
	now      func() time.Time

	mu       sync.Mutex
	inflight map[string]*call
	used     map[string]int64 // bytes per volume, -1 = unknown
}

type call struct {
	done        chan struct{}
	data        []byte
	contentType string
	err         error
}

// New returns a manager that makes at most two thumbnails at once.
func New() *Manager {
	return &Manager{Limit: 512 << 20, slots: make(chan struct{}, 2), lookPath: exec.LookPath, now: time.Now,
		inflight: map[string]*call{}, used: map[string]int64{}}
}

// Source is the file a thumbnail is asked for. Volume and Rel name it on
// its data volume (shared by the share and the volume view); without a
// volume (USB drives) nothing is cached.
type Source struct {
	Volume string
	Rel    string
	Name   string
	File   *os.File
	Info   fs.FileInfo
}

// FitSize rounds a requested edge length up to a supported one.
func FitSize(requested int) int {
	for _, size := range Sizes {
		if requested <= size {
			return size
		}
	}
	return Sizes[len(Sizes)-1]
}

// Key identifies one rendering of one version of a file: path, size,
// modification time and inode. Any change to the file makes a new key.
func Key(rel string, info fs.FileInfo, box int) string {
	var inode uint64
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		inode = st.Ino
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d\x00%d\x00%d\x00%d", cacheVersion, rel, info.Size(), info.ModTime().UnixNano(), inode, box))
	return hex.EncodeToString(sum[:16])
}

// Get returns the thumbnail of a file, from the cache or freshly made.
func (m *Manager) Get(ctx context.Context, src Source, box int) ([]byte, string, error) {
	if Kind(src.Name) == "" || !src.Info.Mode().IsRegular() {
		return nil, "", ErrUnsupported
	}
	box = FitSize(box)
	key := Key(src.Rel, src.Info, box)
	if data, ok := m.cached(src.Volume, key); ok {
		return data, contentType(data), nil
	}
	m.mu.Lock()
	if c, ok := m.inflight[key]; ok {
		m.mu.Unlock()
		select {
		case <-c.done:
			return c.data, c.contentType, c.err
		case <-ctx.Done():
			return nil, "", ctx.Err()
		}
	}
	c := &call{done: make(chan struct{})}
	m.inflight[key] = c
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.inflight, key)
		m.mu.Unlock()
		close(c.done)
	}()
	wait, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	select {
	case m.slots <- struct{}{}:
	case <-wait.Done():
		c.err = ErrBusy
		if ctx.Err() != nil {
			c.err = ctx.Err()
		}
		return nil, "", c.err
	}
	data, err := m.render(ctx, src.File, src.Name, src.Info.Size(), box)
	<-m.slots
	if err != nil {
		c.err = err
		return nil, "", err
	}
	c.data, c.contentType = data, contentType(data)
	m.store(src.Volume, key, data)
	return c.data, c.contentType, nil
}

func contentType(data []byte) string {
	if len(data) > 3 && string(data[1:4]) == "PNG" {
		return "image/png"
	}
	return "image/jpeg"
}

func (m *Manager) open(volume string) (*os.Root, error) {
	if m.MountRoot == "" || m.Mounted == nil || volume == "" {
		return nil, volumedata.ErrUnavailable
	}
	return volumedata.Open(m.MountRoot, volume, "thumbs", m.Mounted())
}

func cachePath(key string) string { return key[:2] + "/" + key + ".img" }

// cached reads a cached thumbnail. A hit refreshes the file time at most
// once a day; that time decides what the cleanup removes first.
func (m *Manager) cached(volume, key string) ([]byte, bool) {
	if m.MountRoot == "" || m.Mounted == nil || volume == "" || !slices.Contains(m.Mounted(), volume) {
		return nil, false
	}
	root, err := m.open(volume)
	if err != nil {
		return nil, false
	}
	defer root.Close()
	name := cachePath(key)
	st, err := root.Lstat(name)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxRendered {
		return nil, false
	}
	data, err := root.ReadFile(name)
	if err != nil || len(data) == 0 {
		return nil, false
	}
	if now := m.now(); now.Sub(st.ModTime()) > 24*time.Hour {
		root.Chtimes(name, now, now)
	}
	return data, true
}

func (m *Manager) store(volume, key string, data []byte) {
	root, err := m.open(volume)
	if err != nil {
		return
	}
	defer root.Close()
	dir := key[:2]
	if err := root.Mkdir(dir, 0700); err != nil && !errors.Is(err, fs.ErrExist) {
		return
	}
	if st, err := root.Lstat(dir); err != nil || !st.IsDir() {
		return
	}
	if err := volumedata.Write(root, cachePath(key), data); err != nil {
		slog.Warn("thumbnail cache", "volume", volume, "err", err)
		return
	}
	m.mu.Lock()
	used, known := m.used[volume]
	if known {
		used += int64(len(data))
		m.used[volume] = used
	}
	m.mu.Unlock()
	if !known || used > m.Limit {
		m.trim(volume, root)
	}
}

type cacheFile struct {
	name string
	size int64
	used time.Time
}

// trim counts the cache and, above the limit, removes the least recently
// used thumbnails until it is down to three quarters of the limit.
func (m *Manager) trim(volume string, root *os.Root) {
	var list []cacheFile
	var total int64
	dirs, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return
	}
	for _, dir := range dirs {
		if !dir.IsDir() || len(dir.Name()) != 2 {
			continue
		}
		entries, err := fs.ReadDir(root.FS(), dir.Name())
		if err != nil {
			continue
		}
		for _, entry := range entries {
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			name := dir.Name() + "/" + entry.Name()
			if strings.HasPrefix(entry.Name(), ".pending-") && m.now().Sub(info.ModTime()) > time.Hour {
				root.Remove(name)
				continue
			}
			list = append(list, cacheFile{name, info.Size(), info.ModTime()})
			total += info.Size()
		}
	}
	if total > m.Limit {
		sort.Slice(list, func(i, j int) bool { return list[i].used.Before(list[j].used) })
		for _, f := range list {
			if total <= m.Limit*3/4 {
				break
			}
			if root.Remove(f.name) == nil {
				total -= f.size
			}
		}
	}
	m.mu.Lock()
	m.used[volume] = total
	m.mu.Unlock()
}

// Clear removes the cache of a volume (all thumbnails are made again).
func (m *Manager) Clear(volume string) error {
	root, err := m.open(volume)
	if err != nil {
		return err
	}
	defer root.Close()
	dirs, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return err
	}
	for _, dir := range dirs {
		if dir.IsDir() && len(dir.Name()) == 2 {
			if err := root.RemoveAll(dir.Name()); err != nil {
				return err
			}
		}
	}
	m.mu.Lock()
	m.used[volume] = 0
	m.mu.Unlock()
	return nil
}

// VolumeRel returns the path of a file relative to its volume's mount point,
// or "" if the directory is not on that volume.
func VolumeRel(mountRoot, volume, dir, rel string) string {
	if mountRoot == "" || volume == "" {
		return ""
	}
	full := filepath.Join(dir, filepath.FromSlash(rel))
	out, err := filepath.Rel(filepath.Join(mountRoot, volume), full)
	if err != nil || out == ".." || strings.HasPrefix(out, "../") {
		return ""
	}
	return filepath.ToSlash(out)
}
