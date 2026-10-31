// Package fileindex keeps an optional full-text index of documents per data
// volume, so that the file manager can search inside files.
//
// The index is off by default. When switched on for chosen volumes or shares
// it lives in /mnt/<volume>/.titanos/fileindex/index.db (bbolt), never on the
// stick or in RAM. A pass walks the chosen folders, reads the text of new or
// changed files (by size, modification time and inode) and forgets deleted
// ones. Passes only start while the volume is in use anyway, at an optional
// daily time or on request, so that the index does not keep disks awake.
// A pass can stop anywhere: committed batches stay, the next pass continues.
package fileindex

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	bolt "go.etcd.io/bbolt"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/office"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const settingsFile = "files/index.json"

var (
	ErrInvalid     = errors.New("fileindex: invalid settings")
	ErrUnavailable = errors.New("fileindex: no index for this place")
	ErrRunning     = errors.New("fileindex: a pass is running")
)

// Settings are kept in /config/files/index.json. Older versions do not know
// the file and simply have no full-text search.
type Settings struct {
	Enabled bool `json:"enabled"`
	// Volumes are indexed as a whole, Shares only within their folder.
	Volumes []string `json:"volumes,omitempty"`
	Shares  []string `json:"shares,omitempty"`
	// OCR reads scanned PDFs without a text layer (slow, first five pages).
	OCR bool `json:"ocr,omitempty"`
	// Time ("HH:MM", NAS time zone) adds a daily pass that may wake sleeping
	// disks. Empty: passes only start while the volume is in use.
	Time string `json:"time,omitempty"`
}

// VolumeStatus is what titand knows about the index of a volume since it
// started; it is kept in RAM only.
type VolumeStatus struct {
	Volume string `json:"volume"`
	// State: idle, running or unavailable (volume not mounted).
	State     string    `json:"state"`
	Files     int       `json:"files"`
	Known     bool      `json:"known"`
	Scanned   int       `json:"scanned"`
	Updated   int       `json:"updated"`
	Bytes     int64     `json:"bytes"`
	LastRun   time.Time `json:"lastRun,omitzero"`
	LastError string    `json:"lastError,omitempty"`
	Truncated bool      `json:"truncated,omitempty"`
}

type Status struct {
	Settings
	Volumes      []VolumeStatus `json:"indexes"`
	OCRAvailable bool           `json:"ocrAvailable"`
}

type activityState struct {
	counter  uint64
	known    bool
	activeAt time.Time
	passEnd  time.Time
	timeDay  string
}

// Manager runs the index passes and answers searches.
type Manager struct {
	MountRoot string
	Mounted   func() []string
	// ShareVolume returns the volume of a share; its folder is <volume>/<share>.
	ShareVolume func(name string) (string, bool)
	// Protected reports files that belong to a virtual machine.
	Protected func(string) bool
	// Timezone names the time zone of the NAS for the daily time.
	Timezone func() string
	// Disabled keeps the index from running (development mode).
	Disabled bool

	store   *config.Store
	io      ioCounter
	extract extractor
	now     func() time.Time
	pause   time.Duration
	minGap  time.Duration
	maxWalk int

	mu       sync.Mutex
	settings Settings
	status   map[string]*VolumeStatus
	handles  map[string]*handle
	activity map[string]*activityState
	manual   map[string]bool
	running  string
	cancel   context.CancelFunc
	wake     chan struct{}
	ocr      bool
}

func New(store *config.Store) *Manager {
	return &Manager{store: store, io: ioCounter{proc: "/proc", sys: "/sys"}, extract: extractor{pdf: office.ReadPDF, ocr: office.OCRPDF},
		now: time.Now, pause: 50 * time.Millisecond, minGap: 30 * time.Minute, maxWalk: 2_000_000,
		status: map[string]*VolumeStatus{}, handles: map[string]*handle{}, activity: map[string]*activityState{}, manual: map[string]bool{}, wake: make(chan struct{}, 1)}
}

// Load reads the settings once at start; later changes come through Set.
func (m *Manager) Load() error {
	var settings Settings
	if err := m.store.ReadJSON(settingsFile, &settings); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if validate(&settings) != nil {
		return fmt.Errorf("%w in %s", ErrInvalid, settingsFile)
	}
	_, err := exec.LookPath("tesseract")
	m.mu.Lock()
	m.settings, m.ocr = settings, err == nil
	m.mu.Unlock()
	return nil
}

var clock = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)

func validate(s *Settings) error {
	if len(s.Volumes) > 32 || len(s.Shares) > 256 || s.Time != "" && !clock.MatchString(s.Time) {
		return ErrInvalid
	}
	for _, v := range s.Volumes {
		if !naming.ValidVolume(v) {
			return ErrInvalid
		}
	}
	for _, name := range s.Shares {
		if name == "" || len(name) > 64 || strings.ContainsAny(name, "/\\\x00") || strings.HasPrefix(name, ".") {
			return ErrInvalid
		}
	}
	slices.Sort(s.Volumes)
	s.Volumes = slices.Compact(s.Volumes)
	slices.Sort(s.Shares)
	s.Shares = slices.Compact(s.Shares)
	return nil
}

func (m *Manager) Settings() Settings {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.settings
}

// Set saves new settings. A running pass stops and starts over with them.
func (m *Manager) Set(settings Settings) error {
	if err := validate(&settings); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.store.WriteJSON(settingsFile, settings); err != nil {
		return err
	}
	m.settings = settings
	if m.cancel != nil {
		m.cancel()
	}
	return nil
}

// scope returns the folders (relative to the volume, "" = all) to index.
func (m *Manager) scope(settings Settings) map[string][]string {
	out := map[string][]string{}
	if !settings.Enabled {
		return out
	}
	for _, v := range settings.Volumes {
		out[v] = []string{""}
	}
	for _, share := range settings.Shares {
		if m.ShareVolume == nil {
			break
		}
		volume, ok := m.ShareVolume(share)
		if !ok || slices.Contains(out[volume], "") {
			continue
		}
		out[volume] = append(out[volume], share)
	}
	return out
}

// Covers reports whether a folder of a volume (relative path) has any
// indexed content, so that searching it makes sense.
func (m *Manager) Covers(volume, rel string) bool {
	if m.Disabled {
		return false
	}
	for _, prefix := range m.scope(m.Settings())[volume] {
		if prefix == "" || rel == "" || rel == prefix || strings.HasPrefix(rel, prefix+"/") || strings.HasPrefix(prefix, rel+"/") {
			return true
		}
	}
	return false
}

// Status lists the volumes in scope with what is known about their index.
func (m *Manager) Status() Status {
	settings := m.Settings()
	mounted := m.mounted()
	m.mu.Lock()
	defer m.mu.Unlock()
	out := Status{Settings: settings, Volumes: []VolumeStatus{}, OCRAvailable: m.ocr}
	scope := m.scope(settings)
	names := make([]string, 0, len(scope))
	for v := range scope {
		names = append(names, v)
	}
	sort.Strings(names)
	for _, v := range names {
		st := VolumeStatus{Volume: v, State: "idle"}
		if known := m.status[v]; known != nil {
			st = *known
		}
		if m.running == v {
			st.State = "running"
		} else if !slices.Contains(mounted, v) {
			st.State = "unavailable"
		} else {
			st.State = "idle"
		}
		out.Volumes = append(out.Volumes, st)
	}
	return out
}

// Start asks for a pass of one volume (or all in scope) as soon as possible.
// This may wake sleeping disks; it is an explicit request.
func (m *Manager) Start(volume string) error {
	settings := m.Settings()
	scope := m.scope(settings)
	if !settings.Enabled || m.Disabled {
		return ErrUnavailable
	}
	m.mu.Lock()
	if volume == "" {
		for v := range scope {
			m.manual[v] = true
		}
	} else if _, ok := scope[volume]; ok {
		m.manual[volume] = true
	} else {
		m.mu.Unlock()
		return ErrUnavailable
	}
	m.mu.Unlock()
	select {
	case m.wake <- struct{}{}:
	default:
	}
	return nil
}

// Remove deletes the index of a volume. It is made again by the next pass
// if the volume is still chosen.
func (m *Manager) Remove(volume string) error {
	if !naming.ValidVolume(volume) {
		return ErrInvalid
	}
	m.mu.Lock()
	if m.running == volume {
		m.mu.Unlock()
		return ErrRunning
	}
	h := m.handles[volume]
	m.mu.Unlock()
	if h != nil {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.users > 0 {
			return ErrRunning
		}
	}
	root, err := volumedata.Open(m.MountRoot, volume, "fileindex", m.mounted())
	if err != nil {
		return ErrUnavailable
	}
	defer root.Close()
	if err := root.Remove(dbName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	m.mu.Lock()
	delete(m.status, volume)
	m.mu.Unlock()
	return nil
}

func (m *Manager) mounted() []string {
	if m.Mounted == nil {
		return nil
	}
	return m.Mounted()
}

func (m *Manager) acquire(volume string, create bool) (*handle, error) {
	m.mu.Lock()
	h := m.handles[volume]
	if h == nil {
		h = &handle{}
		m.handles[volume] = h
	}
	m.mu.Unlock()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.db == nil {
		root, err := volumedata.Open(m.MountRoot, volume, "fileindex", m.mounted())
		if err != nil {
			return nil, ErrUnavailable
		}
		if !create {
			if _, err := root.Lstat(dbName); err != nil {
				root.Close()
				return nil, ErrUnavailable
			}
		}
		db, err := openDB(root)
		if err != nil {
			root.Close()
			return nil, err
		}
		h.db, h.root = db, root
	}
	h.users++
	return h, nil
}

func (m *Manager) release(h *handle) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.users--
	if h.users == 0 && h.db != nil {
		_ = h.db.Close() // cleanup, best effort: nothing more can be done if it fails
		h.root.Close()
		h.db, h.root = nil, nil
	}
}

// Search finds files under the given folders of a volume (relative paths,
// "" = whole volume) whose text contains every word of the query, also as
// the beginning of a longer word. The caller decides the folders from the
// rights of the account; hits outside them are never returned.
func (m *Manager) Search(ctx context.Context, volume string, prefixes []string, text string, limit int) ([]Hit, bool, error) {
	terms := Tokens(text)
	if len(terms) == 0 || len(terms) > 16 {
		return nil, false, ErrInvalid
	}
	if !slices.ContainsFunc(prefixes, func(p string) bool { return m.Covers(volume, p) }) {
		return nil, false, ErrUnavailable
	}
	h, err := m.acquire(volume, false)
	if err != nil {
		return nil, false, err
	}
	defer m.release(h)
	var hits []Hit
	var truncated bool
	err = h.db.View(func(tx *bolt.Tx) error {
		hits, truncated = query(tx, terms, prefixes, limit)
		return ctx.Err()
	})
	return hits, truncated, err
}

// Run starts passes while titand runs. It checks once a minute, reading
// only /proc and /sys, and never wakes a disk on its own unless a daily
// time is configured.
func (m *Manager) Run(ctx context.Context) {
	if m.Disabled {
		return
	}
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		m.tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-m.wake:
		}
	}
}

func (m *Manager) tick(ctx context.Context) {
	settings := m.Settings()
	scope := m.scope(settings)
	mounted := m.mounted()
	names := make([]string, 0, len(scope))
	for v := range scope {
		names = append(names, v)
	}
	sort.Strings(names)
	for _, volume := range names {
		if ctx.Err() != nil {
			return
		}
		if !slices.Contains(mounted, volume) {
			continue
		}
		point := filepath.Join(m.MountRoot, volume)
		now := m.now()
		m.mu.Lock()
		st := m.activity[volume]
		if st == nil {
			st = &activityState{}
			m.activity[volume] = st
		}
		counter, known := m.io.read(point)
		if known && st.known && counter != st.counter {
			st.activeAt = now
		}
		st.counter, st.known = counter, known
		manual := m.manual[volume]
		delete(m.manual, volume)
		timed := false
		if settings.Time != "" {
			local := now
			if m.Timezone != nil {
				if loc, err := time.LoadLocation(m.Timezone()); err == nil {
					local = now.In(loc)
				}
			}
			at, _ := time.ParseInLocation("15:04", settings.Time, local.Location())
			start := time.Date(local.Year(), local.Month(), local.Day(), at.Hour(), at.Minute(), 0, 0, local.Location())
			day := local.Format(time.DateOnly)
			if !local.Before(start) && local.Sub(start) < time.Hour && st.timeDay != day {
				timed, st.timeDay = true, day
			}
		}
		// In use: other I/O within the last three minutes, and the last pass
		// long enough ago. The counters after a pass exclude its own I/O.
		active := !st.activeAt.IsZero() && now.Sub(st.activeAt) <= 3*time.Minute && (st.passEnd.IsZero() || now.Sub(st.passEnd) >= m.minGap)
		m.mu.Unlock()
		if !manual && !timed && !active {
			continue
		}
		m.pass(ctx, volume, scope[volume], settings.OCR)
		m.mu.Lock()
		st.passEnd, st.activeAt = m.now(), time.Time{}
		st.counter, st.known = m.io.read(point)
		m.mu.Unlock()
	}
}

func (m *Manager) setStatus(volume string, change func(*VolumeStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.status[volume]
	if st == nil {
		st = &VolumeStatus{Volume: volume}
		m.status[volume] = st
	}
	change(st)
}

// pass brings the index of one volume up to date with the given folders.
func (m *Manager) pass(parent context.Context, volume string, prefixes []string, ocr bool) {
	ctx, cancel := context.WithCancel(parent)
	m.mu.Lock()
	m.running, m.cancel = volume, cancel
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		m.running, m.cancel = "", nil
		m.mu.Unlock()
	}()
	m.setStatus(volume, func(st *VolumeStatus) { st.Scanned, st.Updated, st.Truncated = 0, 0, false })
	err := m.update(ctx, volume, prefixes, ocr)
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("file index pass", "volume", volume, "err", err)
	}
	m.setStatus(volume, func(st *VolumeStatus) {
		st.LastRun, st.LastError = m.now(), ""
		if err != nil {
			st.LastError = err.Error()
			if errors.Is(err, context.Canceled) {
				st.LastError = "cancelled"
			}
		}
	})
}

func (m *Manager) update(ctx context.Context, volume string, prefixes []string, ocr bool) error {
	vol, err := volumedata.OpenVolume(m.MountRoot, volume, m.mounted())
	if err != nil {
		return ErrUnavailable
	}
	defer vol.Close()
	h, err := m.acquire(volume, true)
	if err != nil {
		return err
	}
	defer m.release(h)
	if err := volumedata.SameMount(vol, h.root); err != nil {
		return err
	}
	var seen []uint64
	var batch []doc
	lastCommit := time.Now()
	complete := true
	commit := func() error {
		if len(batch) == 0 {
			return nil
		}
		err := h.db.Update(func(tx *bolt.Tx) error {
			for _, p := range batch {
				id, err := put(tx, p)
				if err != nil {
					return err
				}
				seen = append(seen, id)
			}
			return nil
		})
		m.setStatus(volume, func(st *VolumeStatus) { st.Updated += len(batch) })
		batch, lastCommit = batch[:0], time.Now()
		return err
	}
	walked := 0
	for _, prefix := range prefixes {
		dir := prefix
		if dir == "" {
			dir = "."
		}
		err := fs.WalkDir(vol.FS(), dir, func(p string, d fs.DirEntry, walkErr error) error {
			if err := ctx.Err(); err != nil {
				return err
			}
			if walkErr != nil {
				if p == dir && errors.Is(walkErr, fs.ErrNotExist) {
					return fs.SkipDir // a chosen share without a folder yet
				}
				complete = false // never forget files below a folder that could not be read
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if p != dir && (strings.HasPrefix(d.Name(), ".") || strings.Contains(d.Name(), ".titanos-snapshot-")) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if m.Protected != nil && m.Protected(filepath.Join(m.MountRoot, volume, filepath.FromSlash(p))) {
				if d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if walked++; walked > m.maxWalk {
				return errLimit
			}
			if d.IsDir() || !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if err != nil {
				complete = false
				return nil
			}
			rel := path.Clean(p)
			if !Indexable(rel, info.Size()) {
				return nil
			}
			m.setStatus(volume, func(st *VolumeStatus) { st.Scanned++ })
			var inode uint64
			if st, ok := info.Sys().(*syscall.Stat_t); ok {
				inode = st.Ino
			}
			modified := info.ModTime().UnixNano()
			var known uint64
			unchanged := false
			_ = h.db.View(func(tx *bolt.Tx) error { // a failed lookup indexes the file anew
				id, old, ok := lookup(tx, rel)
				if ok && old.same(info.Size(), modified, inode) {
					known, unchanged = id, true
				}
				return nil
			})
			if unchanged {
				seen = append(seen, known)
				return nil
			}
			d2 := doc{Path: rel, Size: info.Size(), Modified: modified, Inode: inode}
			text, err := m.read(ctx, vol, rel, info, ocr)
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err != nil {
				d2.Failed = true
			}
			d2.Terms = Tokens(path.Base(rel) + "\n" + text)
			batch = append(batch, d2)
			if len(batch) >= 64 || time.Since(lastCommit) > 5*time.Second {
				if err := commit(); err != nil {
					return err
				}
			}
			if m.pause > 0 {
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(m.pause):
				}
			}
			return nil
		})
		if errors.Is(err, errLimit) {
			complete = false
			m.setStatus(volume, func(st *VolumeStatus) { st.Truncated = true })
			break
		}
		if err != nil {
			_ = commit() // the walk error is returned; what was committed stays
			return err
		}
	}
	if err := commit(); err != nil {
		return err
	}
	// Forget files that are gone or no longer chosen, but only after a
	// complete walk: a folder that could not be read keeps its entries.
	slices.Sort(seen)
	var files int
	var size int64
	err = h.db.Update(func(tx *bolt.Tx) error {
		if complete {
			var stale []uint64
			c := tx.Bucket(filesBucket).Cursor()
			for k, _ := c.First(); k != nil; k, _ = c.Next() {
				if len(k) != 8 {
					continue
				}
				id := uint64(0)
				for _, b := range k {
					id = id<<8 | uint64(b)
				}
				if _, found := slices.BinarySearch(seen, id); !found {
					stale = append(stale, id)
				}
			}
			for _, id := range stale {
				if d, ok := getDoc(tx, id); ok {
					if err := remove(tx, id, d); err != nil {
						return err
					}
				} else if err := tx.Bucket(filesBucket).Delete(idKey(id)); err != nil {
					return err
				}
			}
		}
		size = tx.Size()
		return nil
	})
	if err == nil {
		// Counted afterwards: bucket statistics miss deletions of an open
		// write transaction.
		_ = h.db.View(func(tx *bolt.Tx) error { files = tx.Bucket(filesBucket).Stats().KeyN; return nil }) // the count is for display only
	}
	m.setStatus(volume, func(st *VolumeStatus) { st.Files, st.Bytes, st.Known = files, size, true })
	return err
}

var errLimit = errors.New("fileindex: too many entries")

// read opens a file through the pinned volume root without following links
// and returns its text. A file that changed meanwhile is read next time.
func (m *Manager) read(ctx context.Context, vol *os.Root, rel string, info fs.FileInfo, ocr bool) (string, error) {
	f, err := vol.OpenFile(rel, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() || !os.SameFile(st, info) {
		return "", errSkip
	}
	return m.extract.text(ctx, f, rel, st.Size(), ocr)
}
