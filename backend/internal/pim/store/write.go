package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Collection describes a calendar or address book.
type Collection struct {
	Kind        Kind
	ID          string
	Name        string
	Color       string
	Order       int
	Description string
	CTag        string
	SyncToken   string
	// Count is the number of files (group vCards included).
	Count   int
	Virtual bool
	Created time.Time
}

func info(kind Kind, c *collection) Collection {
	st := c.st
	return Collection{Kind: kind, ID: st.ID, Name: st.Name, Color: st.Color, Order: st.Order, Description: st.Description,
		CTag: st.CTag(), SyncToken: st.SyncToken(), Count: len(st.Index), Virtual: c.virtual, Created: st.Created}
}

// Meta are the properties of a new collection.
type Meta struct {
	Name        string
	Color       string
	Order       int
	Description string
}

// MetaPatch changes properties of a collection; nil fields stay.
type MetaPatch struct {
	Name        *string
	Color       *string
	Order       *int
	Description *string
}

var colorRE = regexp.MustCompile(`^#[0-9A-Fa-f]{6}([0-9A-Fa-f]{2})?$`)

func validText(s string, limit int) bool {
	if len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if r < 0x20 && r != '\n' && r != '\t' || r == 0x7f {
			return false
		}
	}
	return true
}

func (m MetaPatch) valid() bool {
	return (m.Name == nil || (validText(*m.Name, 256) && !strings.ContainsAny(*m.Name, "\n\t"))) &&
		(m.Color == nil || *m.Color == "" || colorRE.MatchString(*m.Color)) &&
		(m.Description == nil || validText(*m.Description, 4096)) &&
		(m.Order == nil || (*m.Order >= -1_000_000 && *m.Order <= 1_000_000))
}

// Valid reports whether the set fields are acceptable values (the rules
// of UpdateCollection): davsync checks each property of a PROPPATCH on its
// own to answer per property.
func (m MetaPatch) Valid() bool { return m.valid() }

func (m MetaPatch) apply(st *State) {
	if m.Name != nil {
		st.Name = *m.Name
	}
	if m.Color != nil {
		st.Color = *m.Color
	}
	if m.Order != nil {
		st.Order = *m.Order
	}
	if m.Description != nil {
		st.Description = *m.Description
	}
}

// Collections lists the collections of an account, the virtual default
// while it has none of its own; sorted by order, then id.
func (s *Store) Collections(kind Kind, account string) ([]Collection, error) {
	acc, err := s.accountFor(kind, account)
	if err != nil {
		return nil, err
	}
	var out []Collection
	for _, c := range acc.collections() {
		c.mu.Lock()
		if !c.deleted {
			out = append(out, info(kind, c))
		}
		c.mu.Unlock()
	}
	slices.SortFunc(out, func(a, b Collection) int {
		if a.Order != b.Order {
			return a.Order - b.Order
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

// GetCollection returns one collection.
func (s *Store) GetCollection(kind Kind, account, id string) (Collection, error) {
	if !ValidCollection(id) {
		return Collection{}, ErrInvalidCollection
	}
	acc, err := s.accountFor(kind, account)
	if err != nil {
		return Collection{}, err
	}
	c, err := acc.lookup(id)
	if err != nil {
		return Collection{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted {
		return Collection{}, ErrNotFound
	}
	return info(kind, c), nil
}

// CreateCollection makes a calendar (MKCALENDAR). Contacts keep the one
// book "kontakte" (ErrFixedBook). A name used before gets a new epoch.
//
// While the account still shows the virtual "privat", the first own
// calendar makes it real as well (empty, default name and color, in the
// same lock): a device that synced "privat" keeps it, and a PUT it made
// offline still lands. Creating "privat" itself makes the virtual one real
// with the properties given. The virtual object is made real in place, so
// a PUT waiting on its lock goes on into the real collection.
func (s *Store) CreateCollection(kind Kind, account, id string, m Meta) (Collection, error) {
	if kind == Contacts {
		return Collection{}, ErrFixedBook
	}
	if !ValidCollection(id) {
		return Collection{}, ErrInvalidCollection
	}
	patch := MetaPatch{Name: &m.Name, Color: &m.Color, Order: &m.Order, Description: &m.Description}
	if !patch.valid() {
		return Collection{}, ErrInvalidMeta
	}
	// CalDAV shows calendars and reminder lists side by side: an id is
	// one or the other (tasks.go).
	if err := s.idFree(kind, account, id); err != nil {
		return Collection{}, err
	}
	acc, done, err := s.beginWrite(kind, account)
	if err != nil {
		return Collection{}, err
	}
	var made []*collection // told after every lock is given back
	defer func() {
		for _, c := range made {
			s.changed(kind, account, c.id, c)
		}
	}()
	defer done()
	// Lock order: the collection first, then the account.
	var virtual *collection
	if v, err := acc.lookup(defaultID(kind)); err == nil && v.virtual {
		v.mu.Lock()
		defer v.mu.Unlock()
		virtual = v
	}
	acc.mu.Lock()
	defer acc.mu.Unlock()
	if acc.cols[id] != nil {
		return Collection{}, ErrExists
	}
	if len(acc.cols) >= maxCollections {
		return Collection{}, ErrTooManyCollections
	}
	if virtual != nil && (len(acc.cols) > 0 || !virtual.virtual || virtual.deleted || acc.virtual != virtual) {
		virtual = nil // made real (or replaced) meanwhile
	}
	ar, release, err := s.openWriteLocked(acc)
	if err != nil {
		return Collection{}, err
	}
	defer release()
	if virtual != nil {
		if err := s.materializeLocked(acc, ar, virtual); err != nil {
			return Collection{}, err
		}
		if id == virtual.id {
			if m.Name == "" {
				m.Name = virtual.st.Name
			}
			patch.apply(virtual.st)
		}
		vr, err := ar.OpenRoot(virtual.id)
		if err != nil {
			return Collection{}, err
		}
		err = s.writeState(vr, virtual.st)
		vr.Close()
		if err != nil {
			return Collection{}, err
		}
		acc.virtual = nil
		made = append(made, virtual)
		if id == virtual.id {
			return info(kind, virtual), nil
		}
	}
	st := defaultState(kind, id, newEpoch(), s.now().UTC())
	if m.Name == "" {
		m.Name = st.Name
	}
	patch.apply(st)
	if err := makeCollection(ar, id); err != nil {
		return Collection{}, err
	}
	cr, err := ar.OpenRoot(id)
	if err != nil {
		return Collection{}, err
	}
	defer cr.Close()
	if err := s.writeState(cr, st); err != nil {
		return Collection{}, err
	}
	c := &collection{id: id, st: st}
	acc.cols[id] = c
	if acc.virtual != nil && acc.virtual.virtual {
		acc.virtual = nil
	}
	s.noteData(acc)
	made = append(made, c)
	return info(kind, c), nil
}

// makeCollection makes <id>/ and <id>/o/ (an existing folder of a
// collection that is not known is a leftover and is replaced).
func makeCollection(ar *os.Root, id string) error {
	if err := ar.Mkdir(id, 0o700); errors.Is(err, fs.ErrExist) {
		if err := ar.RemoveAll(id); err != nil {
			return err
		}
		err = ar.Mkdir(id, 0o700)
		if err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if err := ar.Mkdir(id+"/"+objectsDir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	return syncDir(ar)
}

func syncDir(root *os.Root) error {
	d, err := root.Open(".")
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// UpdateCollection changes name, color, order or description (PROPPATCH);
// seq goes up. The virtual default becomes an own collection.
func (s *Store) UpdateCollection(kind Kind, account, id string, m MetaPatch) (Collection, error) {
	if !ValidCollection(id) {
		return Collection{}, ErrInvalidCollection
	}
	if !m.valid() {
		return Collection{}, ErrInvalidMeta
	}
	acc, done, err := s.beginWrite(kind, account)
	if err != nil {
		return Collection{}, err
	}
	var told *collection
	defer func() {
		if told != nil {
			s.changed(kind, account, id, told)
		}
	}()
	defer done()
	c, err := acc.lookup(id)
	if err != nil {
		return Collection{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted {
		return Collection{}, ErrNotFound
	}
	ar, release, err := s.openWrite(acc)
	if err != nil {
		return Collection{}, err
	}
	defer release()
	if err := s.materialize(acc, ar, c); err != nil {
		return Collection{}, err
	}
	cr, err := ar.OpenRoot(id)
	if err != nil {
		return Collection{}, err
	}
	defer cr.Close()
	m.apply(c.st)
	c.st.change("", false)
	told = c
	if err := s.writeState(cr, c.st); err != nil {
		return Collection{}, err
	}
	return info(kind, c), nil
}

// materialize turns the virtual default into an own collection (c.mu
// held): folder, new epoch; state.json follows with the write.
func (s *Store) materialize(acc *account, ar *os.Root, c *collection) error {
	if !c.virtual {
		return nil
	}
	acc.mu.Lock()
	defer acc.mu.Unlock()
	return s.materializeLocked(acc, ar, c)
}

// materializeLocked is materialize with acc.mu held as well.
func (s *Store) materializeLocked(acc *account, ar *os.Root, c *collection) error {
	if !c.virtual {
		return nil
	}
	if acc.cols[c.id] != nil {
		// Made meanwhile by CreateCollection: the virtual one is gone.
		return ErrExists
	}
	if len(acc.cols) >= maxCollections {
		return ErrTooManyCollections
	}
	if err := makeCollection(ar, c.id); err != nil {
		return err
	}
	c.st = defaultState(acc.kind, c.id, newEpoch(), s.now().UTC())
	c.virtual = false
	acc.cols[c.id] = c
	// An own collection is on the volume now: the account keeps data.
	s.noteData(acc)
	return nil
}

// DeleteCollection deletes a collection with all its entries – never an
// entry (DeleteObject is that). ifMatch, if set, must be the ctag. The
// last own collection stays (ErrLastCollection), as does a virtual one.
func (s *Store) DeleteCollection(kind Kind, account, id, ifMatch string) error {
	if !ValidCollection(id) {
		return ErrInvalidCollection
	}
	acc, done, err := s.beginWrite(kind, account)
	if err != nil {
		return err
	}
	gone := false
	defer func() {
		if gone {
			s.changed(kind, account, id, nil)
		}
	}()
	defer done()
	c, err := acc.lookup(id)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted {
		return ErrNotFound
	}
	if ifMatch != "" && unquote(ifMatch) != "*" && unquote(ifMatch) != c.st.CTag() {
		return ErrPrecondition
	}
	acc.mu.Lock()
	if c.virtual || len(acc.cols) <= 1 {
		acc.mu.Unlock()
		return ErrLastCollection
	}
	ar, release, err := s.openWriteLocked(acc)
	if err != nil {
		acc.mu.Unlock()
		return err
	}
	defer release()
	// Renamed first: a removal cut short never brings half a collection back.
	trash := deletedPfx + id + "-" + randomHex(6)
	if err := ar.Rename(id, trash); err != nil && !errors.Is(err, fs.ErrNotExist) {
		acc.mu.Unlock()
		return err
	}
	if err := syncDir(ar); err != nil {
		acc.mu.Unlock()
		return err
	}
	delete(acc.cols, id)
	c.deleted = true
	gone = true
	acc.mu.Unlock()
	s.parsed.dropCollection(kind, account, id)
	_ = ar.RemoveAll(trash) // a leftover goes with the next load
	return nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b) // crypto/rand does not fail on Linux
	return hex.EncodeToString(b)
}

// unquote takes an entity tag with or without its quotes; a weak one
// never matches.
func unquote(tag string) string {
	tag = strings.TrimSpace(tag)
	if strings.HasPrefix(tag, "W/") {
		return "\x00weak"
	}
	if len(tag) >= 2 && tag[0] == '"' && tag[len(tag)-1] == '"' {
		return tag[1 : len(tag)-1]
	}
	return tag
}

// PutOptions are the conditions of a PUT. IfMatch is an ETag (quotes
// allowed) or "*"; IfNoneMatch is If-None-Match: *.
type PutOptions struct {
	// BeforeCommit runs under the collection lock after all native object
	// preconditions and immediately before writing. It must not open PIM.
	BeforeCommit func() error
	IfMatch      string
	IfNoneMatch  bool
}

// PutResult says what a PUT did.
type PutResult struct {
	ETag    string
	Created bool
}

// Put stores an entry with the bytes as they came: checked, then the
// file, then state.json, under the lock of the collection.
func (s *Store) Put(kind Kind, account, col, name string, data []byte, opt PutOptions) (PutResult, error) {
	if !ValidCollection(col) {
		return PutResult{}, ErrInvalidCollection
	}
	if !kind.valid() || !ValidName(kind, name) {
		return PutResult{}, ErrInvalidName
	}
	if int64(len(data)) > maxSize(kind) {
		return PutResult{}, ErrTooLarge
	}
	entry, err := s.inspect(kind, account, name, data)
	if err != nil {
		return PutResult{}, err
	}
	acc, done, err := s.beginWrite(kind, account)
	if err != nil {
		return PutResult{}, err
	}
	var told *collection
	defer func() {
		if told != nil {
			s.changed(kind, account, col, told)
		}
	}()
	defer done()
	c, err := acc.lookup(col)
	if err != nil {
		return PutResult{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted {
		return PutResult{}, ErrNotFound
	}
	cur := c.st.Index[name]
	if err := precondition(cur, opt); err != nil {
		return PutResult{}, err
	}
	if cur != nil && cur.ETag == entry.ETag {
		return PutResult{ETag: cur.ETag}, nil // the same bytes: nothing to write
	}
	if cur != nil && cur.UID != "" && entry.UID != cur.UID {
		// RFC 4791 5.3.2.1, RFC 6352 6.3.2.1: an entry is not overwritten
		// with another UID (an old card without UID may get one).
		return PutResult{}, &UIDConflictError{Name: name}
	}
	if err := uidFree(c.st, name, entry.UID); err != nil {
		return PutResult{}, err
	}
	if cur == nil && len(c.st.Index) >= maxEntries {
		return PutResult{}, ErrCollectionFull
	}
	ar, release, err := s.openWrite(acc)
	if err != nil {
		return PutResult{}, err
	}
	defer release()
	if err := s.materialize(acc, ar, c); err != nil {
		return PutResult{}, err
	}
	cr, or, err := openCollection(ar, col)
	if err != nil {
		return PutResult{}, err
	}
	defer cr.Close()
	defer or.Close()
	file := encodeName(name)
	if opt.BeforeCommit != nil {
		if err := opt.BeforeCommit(); err != nil {
			return PutResult{}, err
		}
	}
	if err := volumedata.Write(or, file, data); err != nil {
		return PutResult{}, err
	}
	if err := stamp(or, file, entry); err != nil {
		return PutResult{}, err
	}
	if kind == Contacts {
		removePhoto(cr, name)
	}
	c.st.Index[name] = entry
	c.st.change(name, false)
	told = c
	// A failed state.json leaves RAM right (the file is there); the next
	// write writes it again, and a restart before that rebuilds the index
	// with a new epoch.
	if err := s.writeState(cr, c.st); err != nil {
		return PutResult{}, err
	}
	s.parsed.dropEntry(kind, account, col, name)
	s.noteData(acc) // once; a mark lost earlier is made up for here
	return PutResult{ETag: entry.ETag, Created: cur == nil}, nil
}

func precondition(cur *Entry, opt PutOptions) error {
	if opt.IfNoneMatch && cur != nil {
		return ErrPrecondition
	}
	if opt.IfMatch != "" {
		tag := unquote(opt.IfMatch)
		if cur == nil || (tag != "*" && tag != cur.ETag) {
			return ErrPrecondition
		}
	}
	return nil
}

func uidFree(st *State, name, uid string) error {
	if uid == "" {
		return nil
	}
	for other, e := range st.Index {
		if other != name && e.UID == uid {
			return &UIDConflictError{Name: other}
		}
	}
	return nil
}

func openCollection(ar *os.Root, col string) (*os.Root, *os.Root, error) {
	cr, err := ar.OpenRoot(col)
	if err != nil {
		return nil, nil, err
	}
	if err := cr.Mkdir(objectsDir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		cr.Close()
		return nil, nil, err
	}
	or, err := cr.OpenRoot(objectsDir)
	if err != nil {
		cr.Close()
		return nil, nil, err
	}
	return cr, or, nil
}

// stamp notes size and modification time of the written file.
func stamp(or *os.Root, file string, e *Entry) error {
	info, err := or.Lstat(file)
	if err != nil {
		return err
	}
	e.Size, e.MTime = info.Size(), info.ModTime()
	return nil
}

// DeleteObject deletes one entry – never a collection (DeleteCollection
// is that). ifMatch, if set, must be its ETag.
func (s *Store) DeleteObject(kind Kind, account, col, name, ifMatch string) error {
	if !ValidCollection(col) {
		return ErrInvalidCollection
	}
	if !kind.valid() || !ValidName(kind, name) {
		return ErrInvalidName
	}
	acc, done, err := s.beginWrite(kind, account)
	if err != nil {
		return err
	}
	var told *collection
	defer func() {
		if told != nil {
			s.changed(kind, account, col, told)
		}
	}()
	defer done()
	c, err := acc.lookup(col)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	cur := c.st.Index[name]
	if c.deleted || cur == nil {
		return ErrNotFound
	}
	if ifMatch != "" && unquote(ifMatch) != "*" && unquote(ifMatch) != cur.ETag {
		return ErrPrecondition
	}
	ar, release, err := s.openWrite(acc)
	if err != nil {
		return err
	}
	defer release()
	cr, or, err := openCollection(ar, col)
	if err != nil {
		return err
	}
	defer cr.Close()
	defer or.Close()
	if err := or.Remove(encodeName(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := syncDir(or); err != nil {
		return err
	}
	if kind == Contacts {
		removePhoto(cr, name)
	}
	delete(c.st.Index, name)
	c.st.change(name, true)
	told = c
	s.parsed.dropEntry(kind, account, col, name)
	return s.writeState(cr, c.st)
}

// Item is one entry of an import.
type Item struct {
	Name string
	Data []byte
}

// ImportResult is the outcome of one item.
type ImportResult struct {
	Name    string
	ETag    string
	Created bool
	Err     error
}

// Import writes many entries into one collection: each file without its
// own sync of the folder, then one sync, one log entry per file and
// state.json once at the end. An existing name is overwritten (not with
// another UID: *UIDConflictError, as Put). Items
// that fail carry their error; ctx ends the import between files.
func (s *Store) Import(ctx context.Context, kind Kind, account, col string, items []Item) ([]ImportResult, error) {
	return s.importItems(ctx, kind, account, col, items, false)
}

func (s *Store) importItems(ctx context.Context, kind Kind, account, col string, items []Item, missingOnly bool) ([]ImportResult, error) {
	if !ValidCollection(col) {
		return nil, ErrInvalidCollection
	}
	results := make([]ImportResult, len(items))
	entries := make([]*Entry, len(items))
	for i, it := range items {
		results[i].Name = it.Name
		switch {
		case !kind.valid() || !ValidName(kind, it.Name):
			results[i].Err = ErrInvalidName
		case int64(len(it.Data)) > maxSize(kind):
			results[i].Err = ErrTooLarge
		default:
			entries[i], results[i].Err = s.inspect(kind, account, it.Name, it.Data)
		}
	}
	acc, done, err := s.beginWrite(kind, account)
	if err != nil {
		return nil, err
	}
	var told *collection
	defer func() {
		if told != nil {
			s.changed(kind, account, col, told)
		}
	}()
	defer done()
	c, err := acc.lookup(col)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted {
		return nil, ErrNotFound
	}
	ar, release, err := s.openWrite(acc)
	if err != nil {
		return nil, err
	}
	defer release()
	fresh := c.virtual
	if err := s.materialize(acc, ar, c); err != nil {
		return nil, err
	}
	if fresh {
		told = c // a new epoch even when nothing is written
	}
	cr, or, err := openCollection(ar, col)
	if err != nil {
		return nil, err
	}
	defer cr.Close()
	defer or.Close()
	written := 0
	for i, it := range items {
		if results[i].Err != nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			results[i].Err = err
			continue
		}
		e := entries[i]
		cur := c.st.Index[it.Name]
		if cur != nil && cur.ETag == e.ETag {
			results[i].ETag = cur.ETag
			continue
		}
		if cur != nil && missingOnly {
			results[i].Err = ErrPrecondition
			continue
		}
		if cur != nil && cur.UID != "" && e.UID != cur.UID {
			// As Put: an entry is not overwritten with another UID.
			results[i].Err = &UIDConflictError{Name: it.Name}
			continue
		}
		if err := uidFree(c.st, it.Name, e.UID); err != nil {
			results[i].Err = err
			continue
		}
		if cur == nil && len(c.st.Index) >= maxEntries {
			results[i].Err = ErrCollectionFull
			continue
		}
		file := encodeName(it.Name)
		if err := writeNoDirSync(or, file, it.Data); err != nil {
			results[i].Err = err
			continue
		}
		if err := stamp(or, file, e); err != nil {
			results[i].Err = err
			continue
		}
		if kind == Contacts {
			removePhoto(cr, it.Name)
		}
		c.st.Index[it.Name] = e
		c.st.change(it.Name, false)
		s.parsed.dropEntry(kind, account, col, it.Name)
		results[i].ETag, results[i].Created = e.ETag, cur == nil
		written++
		told = c
	}
	if written == 0 {
		if fresh {
			return results, s.writeState(cr, c.st)
		}
		return results, nil
	}
	if err := syncDir(or); err != nil {
		return results, err
	}
	return results, s.writeState(cr, c.st)
}

// writeNoDirSync is volumedata.Write without the sync of the folder (the
// import syncs it once).
func writeNoDirSync(root *os.Root, name string, data []byte) error {
	tmp := pendingPfx + randomHex(12)
	f, err := root.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer root.Remove(tmp) // gone after the rename
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return root.Rename(tmp, name)
}
