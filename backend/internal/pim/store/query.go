package store

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// Occurrence is one occurrence of an event in a window (Query).
type Occurrence struct {
	Name string // the file
	ETag string
	UID  string
	// ID is the occurrence id (recur.OccurrenceID): RECURRENCE-ID in UTC
	// or the date.
	ID         string
	Start, End time.Time
	AllDay     bool
	Summary    string
	Recurs     bool
	Override   bool
	Orphan     bool
	// Unsupported is recurUnsupported: the rule is not expanded, the
	// event shows once.
	Unsupported bool
	Truncated   bool
}

// Bounds of the cache of parsed recurring entries (recur.File is not safe
// for concurrent use: each cached file has its own lock).
const (
	maxParsed      = 1024
	maxParsedBytes = 32 << 20
)

type parsedCache struct {
	mu    sync.Mutex
	files map[string]*parsedFile
	bytes int
}

type parsedFile struct {
	mu   sync.Mutex
	etag string
	size int
	file *recur.File
	// The last window and its result: a month view asks again and again,
	// and a rare rule can cost ~45 ms.
	from, to time.Time
	last     recur.Expansion
	hasLast  bool
}

func parsedKey(kind Kind, account, col, name string) string {
	return string(kind) + "\x00" + account + "\x00" + col + "\x00" + name
}

func (p *parsedCache) get(key, etag string) *parsedFile {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f := p.files[key]; f != nil && f.etag == etag {
		return f
	}
	return nil
}

func (p *parsedCache) put(key string, f *parsedFile) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.files == nil {
		p.files = map[string]*parsedFile{}
	}
	if old := p.files[key]; old != nil {
		p.bytes -= old.size
		delete(p.files, key)
	}
	for k, v := range p.files {
		if len(p.files) < maxParsed && p.bytes+f.size <= maxParsedBytes {
			break
		}
		p.bytes -= v.size
		delete(p.files, k)
	}
	p.files[key] = f
	p.bytes += f.size
}

func (p *parsedCache) drop(match func(key string) bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, v := range p.files {
		if match(k) {
			p.bytes -= v.size
			delete(p.files, k)
		}
	}
}

func (p *parsedCache) dropKind(kind Kind) {
	p.drop(func(k string) bool { return strings.HasPrefix(k, string(kind)+"\x00") })
}

func (p *parsedCache) dropAccount(name string) {
	p.drop(func(k string) bool { return strings.Split(k, "\x00")[1] == name })
}

func (p *parsedCache) dropCollection(kind Kind, account, col string) {
	prefix := parsedKey(kind, account, col, "")
	p.drop(func(k string) bool { return strings.HasPrefix(k, prefix) })
}

func (p *parsedCache) dropEntry(kind Kind, account, col, name string) {
	key := parsedKey(kind, account, col, name)
	p.drop(func(k string) bool { return k == key })
}

type queryItem struct {
	name  string
	entry Entry
}

// snapshot copies the index of a calendar for a query.
func (s *Store) snapshot(account, col string) (*account, []queryItem, error) {
	if !ValidCollection(col) {
		return nil, nil, ErrInvalidCollection
	}
	acc, err := s.accountFor(Calendar, account)
	if err != nil {
		return nil, nil, err
	}
	c, err := acc.lookup(col)
	if err != nil {
		return nil, nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted {
		return nil, nil, ErrNotFound
	}
	items := make([]queryItem, 0, len(c.st.Index))
	for _, name := range sortedNames(c.st.Index) {
		items = append(items, queryItem{name: name, entry: copyEntry(c.st.Index[name])})
	}
	return acc, items, nil
}

// parsedFor returns the parsed file of a recurring entry: cached by ETag,
// else read from the volume once.
func (s *Store) parsedFor(acc *account, col string, it queryItem) (*parsedFile, error) {
	key := parsedKey(Calendar, acc.name, col, it.name)
	if f := s.parsed.get(key, it.entry.ETag); f != nil {
		return f, nil
	}
	data, err := s.readObject(acc, col, it.name)
	if err != nil {
		return nil, err
	}
	file, err := recur.Read(data, recur.Options{Local: s.local(), Account: acc.name, Name: it.name})
	if err != nil || len(file.Series) == 0 {
		return nil, ErrInvalidData
	}
	f := &parsedFile{etag: ETag(data), size: len(data), file: file}
	s.parsed.put(key, f)
	return f, nil
}

// overlaps is RFC 4791 9.9 (as recur): with length, start < to and end >
// from; without, start in [from, to). Zero from/to are open.
func overlaps(start, end, from, to time.Time) bool {
	if !end.After(start) {
		return (from.IsZero() || !start.Before(from)) && (to.IsZero() || start.Before(to))
	}
	return (to.IsZero() || start.Before(to)) && (from.IsZero() || end.After(from))
}

func entryID(e *Entry) string {
	v := tz.Value{Time: e.Start, Kind: tz.KindUTC}
	switch {
	case e.AllDay:
		v.Kind = tz.KindDate
	case e.Floating:
		v.Kind = tz.KindFloating
	}
	return recur.OccurrenceID(v)
}

// Query returns the occurrences of a calendar's events in [from, to) (at
// most 400 days, recur.ErrWindow otherwise), sorted by start. Single
// events come from the index (no disk); recurring ones are expanded with
// pim/recur, their parsed file and last window cached per ETag.
func (s *Store) Query(account, col string, from, to time.Time) ([]Occurrence, error) {
	if !to.After(from) || to.After(from.AddDate(0, 0, recur.MaxWindowDays)) {
		return nil, recur.ErrWindow
	}
	acc, items, err := s.snapshot(account, col)
	if err != nil {
		return nil, err
	}
	loc := s.local()
	out := []Occurrence{}
	for _, it := range items {
		e := &it.entry
		if !e.Recurs {
			if e.Start.IsZero() && e.ETag != "" && e.UID == "" {
				continue // a file the index could not read
			}
			st, en := e.span(loc)
			if overlaps(st, en, from, to) {
				out = append(out, Occurrence{Name: it.name, ETag: e.ETag, UID: e.UID, ID: entryID(e), Start: st, End: en,
					AllDay: e.AllDay, Summary: e.Summary, Unsupported: e.Unsupported})
			}
			continue
		}
		pf, err := s.parsedFor(acc, col, it)
		if errors.Is(err, ErrUnavailable) {
			return nil, err
		}
		if err != nil {
			continue // gone meanwhile or unreadable: the next query sees it
		}
		pf.mu.Lock() // the events of a cached file are read under its lock
		x, err := pf.expandLocked(from, to)
		if err != nil {
			pf.mu.Unlock()
			continue
		}
		for _, o := range x.Occurrences {
			summary := ""
			if o.Event != nil {
				if t, err := o.Event.Component.Props.Text(ical.PropSummary); err == nil {
					summary = clip(t)
				}
			}
			out = append(out, Occurrence{Name: it.name, ETag: pf.etag, UID: e.UID, ID: o.ID(), Start: o.Start, End: o.End,
				AllDay: o.AllDay, Summary: summary, Recurs: true, Override: o.Override, Orphan: o.Orphan,
				Unsupported: x.Unsupported || x.Failed, Truncated: x.Truncated})
		}
		pf.mu.Unlock()
	}
	slices.SortStableFunc(out, func(a, b Occurrence) int { return a.Start.Compare(b.Start) })
	return out, nil
}

// expandLocked expands every series of the file (pf.mu held).
func (pf *parsedFile) expandLocked(from, to time.Time) (recur.Expansion, error) {
	if pf.hasLast && pf.from.Equal(from) && pf.to.Equal(to) {
		return pf.last, nil
	}
	var all recur.Expansion
	for _, sr := range pf.file.Series {
		x, err := sr.Expand(from, to)
		if err != nil {
			return recur.Expansion{}, err
		}
		all.Occurrences = append(all.Occurrences, x.Occurrences...)
		all.Truncated = all.Truncated || x.Truncated
		all.Unsupported = all.Unsupported || x.Unsupported
		all.Failed = all.Failed || x.Failed
	}
	pf.from, pf.to, pf.last, pf.hasLast = from, to, all, true
	return all, nil
}

// Overlapping returns the names of a calendar's entries with an
// occurrence in [from, to) for calendar-query time-range; a zero from or
// to is open (RFC 4791 9.9). Entries that cannot be expanded match: the
// client filters again.
func (s *Store) Overlapping(account, col string, from, to time.Time) ([]string, error) {
	acc, items, err := s.snapshot(account, col)
	if err != nil {
		return nil, err
	}
	loc := s.local()
	out := []string{}
	for _, it := range items {
		e := &it.entry
		if !e.Recurs {
			st, en := e.span(loc)
			if (e.Start.IsZero() && e.UID == "") || overlaps(st, en, from, to) {
				out = append(out, it.name)
			}
			continue
		}
		pf, err := s.parsedFor(acc, col, it)
		if errors.Is(err, ErrUnavailable) {
			return nil, err
		}
		if err != nil {
			out = append(out, it.name)
			continue
		}
		pf.mu.Lock()
		match := false
		for _, sr := range pf.file.Series {
			if sr.Overlaps(from, to) {
				match = true
				break
			}
		}
		pf.mu.Unlock()
		if match {
			out = append(out, it.name)
		}
	}
	return out, nil
}
