package store

import (
	"errors"
	"slices"
	"strings"
	"sync"
	"time"
	"unsafe"

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

	// What lists show besides the title: location (clipped), zone of the
	// start (ZoneName), guessed zone, alarms (minutes before), and for a
	// series the rule of its master (a copy) with the zone it is read in.
	Floating bool
	Location string
	TZID     string
	TZGuess  bool
	Alarms   []int
	// Clipped: Summary or Location was cut to the length of the index.
	Clipped  bool
	Rule     *recur.Rule
	RuleZone *time.Location
}

// MaxQueryOccurrences bounds the occurrences of one Query (and of the
// event list of the web API): 20 000 daily series over 400 days would be
// 8 million (S-20).
const MaxQueryOccurrences = 10000

// Bounds of the cache of parsed recurring entries (recur.File is not safe
// for concurrent use: each cached file has its own lock). The bytes count
// each file and the occurrences of its last window (S-22).
const maxParsed = 1024

// maxParsedBytes is a variable only for tests.
var maxParsedBytes = 32 << 20

// occurrenceBytes is what one cached occurrence holds (the event it
// points to belongs to the parsed file).
const occurrenceBytes = int(unsafe.Sizeof(recur.Occurrence{}))

type parsedCache struct {
	mu    sync.Mutex
	files map[string]*parsedFile
	bytes int
}

type parsedFile struct {
	mu   sync.Mutex
	key  string
	etag string
	size int
	// cost is size plus the occurrences of last (guarded by the cache's
	// lock, not mu).
	cost int
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
		p.bytes -= old.cost
		delete(p.files, key)
	}
	f.key, f.cost = key, f.size
	for k, v := range p.files {
		if len(p.files) < maxParsed && p.bytes+f.cost <= maxParsedBytes {
			break
		}
		p.bytes -= v.cost
		delete(p.files, k)
	}
	p.files[key] = f
	p.bytes += f.cost
}

// resize sets the cost of a cached file after its last window changed
// and evicts others while the cache is over its bytes; a file evicted
// meanwhile counts nothing. The caller holds f.mu (lock order: f.mu,
// then p.mu; nothing takes them the other way round).
func (p *parsedCache) resize(f *parsedFile, cost int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.files[f.key] != f {
		return
	}
	p.bytes += cost - f.cost
	f.cost = cost
	for k, v := range p.files {
		if p.bytes <= maxParsedBytes {
			break
		}
		if v != f {
			p.bytes -= v.cost
			delete(p.files, k)
		}
	}
}

func (p *parsedCache) drop(match func(key string) bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, v := range p.files {
		if match(k) {
			p.bytes -= v.cost
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
// most 400 days, recur.ErrWindow otherwise), sorted by start: the first
// limit of them (at most MaxQueryOccurrences; <= 0 is that), truncated
// when there were more. Single events come from the index (no disk);
// recurring ones are expanded with pim/recur, their parsed file and last
// window cached per ETag. Once more than limit are held the list is cut
// to the earliest limit and later entries are expanded only up to the
// start of the last one kept, so neither RAM nor time grows with the
// number of series (S-20).
func (s *Store) Query(account, col string, from, to time.Time, limit int) ([]Occurrence, bool, error) {
	if !to.After(from) || to.After(from.AddDate(0, 0, recur.MaxWindowDays)) {
		return nil, false, recur.ErrWindow
	}
	if limit <= 0 || limit > MaxQueryOccurrences {
		limit = MaxQueryOccurrences
	}
	acc, items, err := s.snapshot(account, col)
	if err != nil {
		return nil, false, err
	}
	loc := s.local()
	out := []Occurrence{}
	// end is to, or once cut the start of the last occurrence kept: what
	// starts there or later is not among the first limit.
	end, cut := to, false
	trim := func() {
		slices.SortStableFunc(out, func(a, b Occurrence) int { return a.Start.Compare(b.Start) })
		if len(out) > limit {
			clear(out[limit:])
			out, cut = out[:limit], true
			end = out[limit-1].Start
		}
	}
	for _, it := range items {
		if len(out) >= 2*limit {
			trim()
		}
		e := &it.entry
		if !e.Recurs {
			if e.Start.IsZero() && e.ETag != "" && e.UID == "" {
				continue // a file the index could not read
			}
			st, en := e.span(loc)
			if overlaps(st, en, from, end) {
				out = append(out, Occurrence{Name: it.name, ETag: e.ETag, UID: e.UID, ID: entryID(e), Start: st, End: en,
					AllDay: e.AllDay, Summary: e.Summary, Unsupported: e.Unsupported, Floating: e.Floating,
					Location: e.Location, TZID: e.TZID, TZGuess: e.TZGuess, Alarms: e.Alarms, Clipped: e.Clipped})
			}
			continue
		}
		if !end.After(from) {
			continue // cut at the window's start: no occurrence fits
		}
		pf, err := s.parsedFor(acc, col, it)
		if errors.Is(err, ErrUnavailable) {
			return nil, false, err
		}
		if err != nil {
			continue // gone meanwhile or unreadable: the next query sees it
		}
		pf.mu.Lock() // the events of a cached file are read under its lock
		x, err := s.expandCached(pf, from, end)
		if err != nil {
			pf.mu.Unlock()
			continue
		}
		var rule *recur.Rule
		var ruleZone *time.Location
		if sr := pf.file.Series; len(sr) > 0 && sr[0].Master != nil && sr[0].Master.Rule != nil {
			m := sr[0].Master
			rule, ruleZone = m.Rule.Clone(), zoneOf(m.Start, loc)
		}
		for _, o := range x.Occurrences {
			oc := Occurrence{Name: it.name, ETag: pf.etag, UID: e.UID, ID: o.ID(), Start: o.Start, End: o.End,
				AllDay: o.AllDay, Recurs: true, Override: o.Override, Orphan: o.Orphan,
				Unsupported: x.Unsupported || x.Failed, Truncated: x.Truncated, Rule: rule, RuleZone: ruleZone}
			if o.Event != nil {
				if t, err := o.Event.Component.Props.Text(ical.PropSummary); err == nil {
					oc.Summary, oc.Clipped = clip(t), clipped(t)
				}
				oc.Floating = o.Event.Start.Kind == tz.KindFloating
				var cut bool
				oc.Location, oc.TZID, oc.TZGuess, oc.Alarms, cut = eventDetails(o.Event)
				oc.Clipped = oc.Clipped || cut
			}
			out = append(out, oc)
		}
		pf.mu.Unlock()
	}
	trim()
	return out, cut, nil
}

// zoneOf is the zone a start is read in: its own, UTC, or the NAS zone
// for dates and floating times.
func zoneOf(v tz.Value, local *time.Location) *time.Location {
	switch v.Kind {
	case tz.KindUTC:
		return time.UTC
	case tz.KindZoned:
		return v.Time.Location()
	}
	return local
}

// expandCached is expandLocked with the occurrences of the new last
// window counted in the cache (pf.mu held).
func (s *Store) expandCached(pf *parsedFile, from, to time.Time) (recur.Expansion, error) {
	if pf.hasLast && pf.from.Equal(from) && pf.to.Equal(to) {
		return pf.last, nil
	}
	x, err := pf.expandLocked(from, to)
	if err == nil {
		s.parsed.resize(pf, pf.size+len(x.Occurrences)*occurrenceBytes)
	}
	return x, err
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
