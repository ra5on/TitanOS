// Package activity keeps a bounded log of who changed what on the NAS. Entries
// are written as daily JSON lines to a data volume, never to the stick; without
// a volume only the most recent entries are kept in memory.
package activity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/ra5on/TitanOS/backend/internal/config"
	"github.com/ra5on/TitanOS/backend/internal/naming"
	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

const (
	settingsFile = "system/activity.json"
	// recentKept is what stays in memory for the page while no volume is there.
	recentKept = 500
	// pendingKept bounds the entries waiting for a volume; older ones are dropped.
	pendingKept = 5000
	maxLine     = 16384
)

// Settings choose whether and where the log is kept. An empty Volume means
// the first mounted volume.
type Settings struct {
	Enabled bool   `json:"enabled"`
	Volume  string `json:"volume"`
	Days    int    `json:"days"`
	MaxMiB  int    `json:"maxMiB"`
}

// Entry is one recorded action. Action is the API route ("PUT /api/v1/users/{name}")
// or an event such as "login"; Target names what it was done to. Request bodies
// are never recorded, so passwords and keys cannot end up here.
type Entry struct {
	At     time.Time `json:"at"`
	User   string    `json:"user"`
	Via    string    `json:"via"`
	IP     string    `json:"ip,omitempty"`
	Action string    `json:"action"`
	Target string    `json:"target,omitempty"`
	Status int       `json:"status,omitempty"`
	OK     bool      `json:"ok"`
}

// Via says how a request came in.
const (
	ViaWeb    = "web"
	ViaToken  = "token"
	ViaAgent  = "agent"
	ViaSystem = "system"
)

type Status struct {
	Settings Settings `json:"settings"`
	// Volume is where entries go now; empty while they stay in memory.
	Volume  string `json:"volume"`
	Pending int    `json:"pending"`
	Dropped int    `json:"dropped"`
	Error   string `json:"error"`
}

// Filter narrows a query. Before pages backwards through older entries.
type Filter struct {
	Days   int
	User   string
	Text   string
	Failed bool
	Before time.Time
	Limit  int
}

type Manager struct {
	store     *config.Store
	MountRoot string
	Mounted   func() []string
	mu        sync.Mutex
	ioMu      sync.Mutex
	settings  Settings
	recent    []Entry
	pending   []Entry
	dropped   int
	problem   string
	wake      chan struct{}
	now       func() time.Time
}

func New(store *config.Store) *Manager {
	return &Manager{store: store, MountRoot: "/mnt", settings: Defaults(), wake: make(chan struct{}, 1), now: time.Now}
}

func Defaults() Settings { return Settings{Enabled: true, Days: 90, MaxMiB: 64} }

func (m *Manager) Load() error {
	var s Settings
	if err := m.store.ReadJSON(settingsFile, &s); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := validate(s); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	m.mu.Unlock()
	return nil
}

func validate(s Settings) error {
	if (s.Volume != "" && !naming.ValidVolume(s.Volume)) || !slices.Contains([]int{30, 90, 365}, s.Days) || s.MaxMiB < 8 || s.MaxMiB > 256 {
		return errors.New("invalid activity settings")
	}
	return nil
}

func (m *Manager) Set(s Settings) error {
	if err := validate(s); err != nil {
		return err
	}
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	if err := m.store.WriteJSON(settingsFile, s); err != nil {
		return err
	}
	m.mu.Lock()
	m.settings = s
	m.problem = ""
	m.mu.Unlock()
	m.poke()
	return nil
}

func (m *Manager) Status() Status {
	m.mu.Lock()
	s := m.settings
	st := Status{Settings: s, Pending: len(m.pending), Dropped: m.dropped, Error: m.problem}
	m.mu.Unlock()
	if s.Enabled {
		st.Volume = m.volume(s)
	}
	return st
}

// volume is the volume entries go to, or "" if none is mounted.
func (m *Manager) volume(s Settings) string {
	if m.Mounted == nil {
		return ""
	}
	mounted := m.Mounted()
	if s.Volume != "" {
		if slices.Contains(mounted, s.Volume) {
			return s.Volume
		}
		return ""
	}
	sorted := slices.Sorted(slices.Values(mounted))
	for _, v := range sorted {
		if naming.ValidVolume(v) {
			return v
		}
	}
	return ""
}

// Record keeps an entry. It never blocks on the disk; Run writes it out.
func (m *Manager) Record(e Entry) {
	if m == nil {
		return
	}
	if e.At.IsZero() {
		e.At = m.now()
	}
	e.At = e.At.UTC()
	e.User = clean(e.User, 64)
	e.Via = clean(e.Via, 16)
	e.IP = clean(e.IP, 64)
	e.Action = clean(e.Action, 160)
	e.Target = clean(e.Target, 512)
	m.mu.Lock()
	if !m.settings.Enabled {
		m.mu.Unlock()
		return
	}
	m.recent = append(m.recent, e)
	if len(m.recent) > recentKept {
		m.recent = slices.Clone(m.recent[len(m.recent)-recentKept:])
	}
	m.pending = append(m.pending, e)
	if over := len(m.pending) - pendingKept; over > 0 {
		m.dropped += over
		m.pending = slices.Clone(m.pending[over:])
	}
	m.mu.Unlock()
	m.poke()
}

func (m *Manager) poke() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// clean drops control characters, so an entry stays one line and cannot
// fake other lines in an export.
func clean(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, s)
	if len(s) > limit {
		cut := limit
		for cut > 0 && !utf8Start(s[cut]) {
			cut--
		}
		s = s[:cut] + "…"
	}
	return s
}

func utf8Start(b byte) bool { return b&0xC0 != 0x80 }

// Run writes waiting entries a few seconds after they come in.
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.Flush()
			return
		case <-ticker.C:
		case <-m.wake:
			// Collect what else comes with the same click.
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
		m.Flush()
	}
}

// Flush appends the waiting entries to the volume.
func (m *Manager) Flush() {
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	m.mu.Lock()
	s := m.settings
	batch := slices.Clone(m.pending)
	m.mu.Unlock()
	if len(batch) == 0 || !s.Enabled {
		return
	}
	volume := m.volume(s)
	if volume == "" {
		return
	}
	err := m.write(s, volume, batch)
	m.mu.Lock()
	defer m.mu.Unlock()
	if err != nil {
		m.problem = err.Error()
		return
	}
	m.problem = ""
	// Entries recorded meanwhile stay; dropped ones may have shifted the start.
	done := len(batch)
	for done > 0 && (len(m.pending) < done || m.pending[done-1] != batch[len(batch)-1]) {
		done--
	}
	m.pending = slices.Clone(m.pending[done:])
}

func (m *Manager) write(s Settings, volume string, batch []Entry) error {
	root, err := volumedata.Open(m.MountRoot, volume, "activity", m.Mounted())
	if err != nil {
		return err
	}
	defer root.Close()
	days := map[string][]byte{}
	var order []string
	for _, e := range batch {
		line, err := json.Marshal(e)
		if err != nil {
			return err
		}
		if len(line) >= maxLine {
			continue
		}
		name := e.At.Format("2006-01-02") + ".jsonl"
		if _, ok := days[name]; !ok {
			order = append(order, name)
		}
		days[name] = append(append(days[name], line...), '\n')
	}
	for _, name := range order {
		if err := prune(root, s, m.now(), int64(len(days[name]))); err != nil {
			return err
		}
		if st, e := root.Lstat(name); e == nil && !st.Mode().IsRegular() {
			return errors.New("invalid activity file")
		}
		file, err := root.OpenFile(name, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0600)
		if err != nil {
			return err
		}
		if err = repairTail(file); err == nil {
			_, err = file.Write(days[name])
		}
		if err == nil {
			err = file.Sync()
		}
		if ce := file.Close(); err == nil {
			err = ce
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func records(root *os.Root) ([]fs.DirEntry, error) {
	dir, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	list, err := dir.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	list = slices.DeleteFunc(list, func(e fs.DirEntry) bool { _, err := time.Parse("2006-01-02.jsonl", e.Name()); return err != nil })
	sort.Slice(list, func(i, j int) bool { return list[i].Name() < list[j].Name() })
	return list, nil
}

// prune removes days past the retention and then the oldest days until the
// new lines fit the budget. Today's file is never removed.
func prune(root *os.Root, s Settings, now time.Time, reserve int64) error {
	list, err := records(root)
	if err != nil {
		return err
	}
	cutoff := now.UTC().AddDate(0, 0, -s.Days).Format("2006-01-02")
	today := now.UTC().Format("2006-01-02") + ".jsonl"
	var total int64
	var keep []fs.DirEntry
	for _, e := range list {
		st, err := e.Info()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return errors.New("invalid activity entry")
		}
		if e.Name()[:10] < cutoff {
			if err := root.Remove(e.Name()); err != nil {
				return err
			}
			continue
		}
		total += st.Size()
		keep = append(keep, e)
	}
	for total+reserve > int64(s.MaxMiB)<<20 && len(keep) > 0 && keep[0].Name() != today {
		st, err := keep[0].Info()
		if err != nil {
			return err
		}
		if err := root.Remove(keep[0].Name()); err != nil {
			return err
		}
		total -= st.Size()
		keep = keep[1:]
	}
	if total+reserve > int64(s.MaxMiB)<<20 {
		return errors.New("activity log budget exhausted for today")
	}
	return nil
}

func repairTail(file *os.File) error {
	st, err := file.Stat()
	if err != nil || st.Size() == 0 {
		return err
	}
	n := min(st.Size(), int64(maxLine))
	b := make([]byte, n)
	if _, err := file.ReadAt(b, st.Size()-n); err != nil {
		return err
	}
	if b[len(b)-1] == '\n' {
		return nil
	}
	i := bytes.LastIndexByte(b, '\n')
	if i < 0 && st.Size() > n {
		return errors.New("invalid activity tail")
	}
	return file.Truncate(st.Size() - n + int64(i+1))
}

func (f Filter) match(e Entry, cutoff time.Time) bool {
	if e.At.Before(cutoff) || (!f.Before.IsZero() && !e.At.Before(f.Before)) {
		return false
	}
	if f.User != "" && e.User != f.User {
		return false
	}
	if f.Failed && e.OK {
		return false
	}
	if f.Text != "" {
		text := strings.ToLower(f.Text)
		if !strings.Contains(strings.ToLower(e.Action+" "+e.Target+" "+e.IP+" "+e.User), text) {
			return false
		}
	}
	return true
}

// Query returns the newest matching entries first. More is true when older
// matches exist beyond Limit. Stored says whether a volume was read; without
// one only the entries in memory are there.
func (m *Manager) Query(ctx context.Context, f Filter) (entries []Entry, more bool, stored bool, err error) {
	if f.Days < 1 || f.Days > 365 || f.Limit < 1 || f.Limit > 50000 {
		return nil, false, false, errors.New("invalid activity query")
	}
	cutoff := m.now().UTC().AddDate(0, 0, -f.Days)
	m.mu.Lock()
	s := m.settings
	pending := slices.Clone(m.pending)
	recent := slices.Clone(m.recent)
	m.mu.Unlock()
	found := []Entry{}
	volume := ""
	if s.Enabled {
		volume = m.volume(s)
	}
	if volume == "" {
		for _, e := range recent {
			if f.match(e, cutoff) {
				found = append(found, e)
			}
		}
		return finish(found, f.Limit, false)
	}
	m.ioMu.Lock()
	defer m.ioMu.Unlock()
	// Entries written while we waited for the lock are on the volume now.
	m.mu.Lock()
	pending = slices.Clone(m.pending)
	m.mu.Unlock()
	for _, e := range pending {
		if f.match(e, cutoff) {
			found = append(found, e)
		}
	}
	root, err := volumedata.Open(m.MountRoot, volume, "activity", m.Mounted())
	if err != nil {
		return nil, false, false, err
	}
	defer root.Close()
	list, err := records(root)
	if err != nil {
		return nil, false, false, err
	}
	first := cutoff.Format("2006-01-02")
	last := ""
	if !f.Before.IsZero() {
		last = f.Before.UTC().Format("2006-01-02")
	}
	var read int64
	for i := len(list) - 1; i >= 0; i-- {
		name := list[i].Name()[:10]
		if name < first {
			break
		}
		if last != "" && name > last {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, false, false, err
		}
		st, err := list[i].Info()
		if err != nil {
			return nil, false, false, err
		}
		read += st.Size()
		if !st.Mode().IsRegular() || read > int64(s.MaxMiB)<<20+int64(maxLine) {
			return nil, false, false, errors.New("activity data exceeds budget")
		}
		day, err := readDay(root, list[i].Name(), f, cutoff)
		if err != nil {
			return nil, false, false, err
		}
		found = append(found, day...)
		// Days are read newest first; once enough is there, older days only add "more".
		if len(found) > f.Limit {
			break
		}
	}
	return finish(found, f.Limit, true)
}

func finish(found []Entry, limit int, stored bool) ([]Entry, bool, bool, error) {
	sort.SliceStable(found, func(i, j int) bool { return found[i].At.After(found[j].At) })
	more := len(found) > limit
	if more {
		found = found[:limit]
	}
	return found, more, stored, nil
}

func readDay(root *os.Root, name string, f Filter, cutoff time.Time) ([]Entry, error) {
	file, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var out []Entry
	reader := bufio.NewReaderSize(file, maxLine+1)
	for {
		line, err := reader.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			// Lines are bounded when written; skip anything longer.
			for errors.Is(err, bufio.ErrBufferFull) {
				_, err = reader.ReadSlice('\n')
			}
			continue
		}
		if len(line) > 0 && line[len(line)-1] == '\n' {
			var e Entry
			// A torn or foreign line is skipped instead of hiding the whole day.
			if json.Unmarshal(line, &e) == nil && f.match(e, cutoff) {
				out = append(out, e)
			}
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}
