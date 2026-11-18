package store

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Object is a stored entry with its bytes.
type Object struct {
	Name  string
	Data  []byte
	ETag  string
	MTime time.Time
}

// ObjectInfo is an entry as the index knows it.
type ObjectInfo struct {
	Name string
	Entry
}

func copyEntry(e *Entry) Entry {
	out := *e
	out.Emails = append([]string(nil), e.Emails...)
	out.Phones = append([]string(nil), e.Phones...)
	out.Alarms = append([]int(nil), e.Alarms...)
	return out
}

// Objects lists the entries of a collection from RAM (names, ETags and the
// index), sorted by name. Group vCards are included (Group set).
func (s *Store) Objects(kind Kind, account, col string) ([]ObjectInfo, error) {
	out, _, err := s.ObjectsTag(kind, account, col)
	return out, err
}

// ObjectsTag is Objects with the ctag of the same moment: what a cache
// of the list keeps under that ctag (1.13).
func (s *Store) ObjectsTag(kind Kind, account, col string) ([]ObjectInfo, string, error) {
	c, err := s.collectionFor(kind, account, col)
	if err != nil {
		return nil, "", err
	}
	c.mu.Lock()
	if c.deleted {
		c.mu.Unlock()
		return nil, "", ErrNotFound
	}
	out := make([]ObjectInfo, 0, len(c.st.Index))
	for name, e := range c.st.Index {
		out = append(out, ObjectInfo{Name: name, Entry: copyEntry(e)})
	}
	ctag := c.st.CTag()
	c.mu.Unlock()
	// Sorted outside the lock: the copy is the caller's.
	slices.SortFunc(out, func(a, b ObjectInfo) int { return strings.Compare(a.Name, b.Name) })
	return out, ctag, nil
}

// Stat returns the index entry of one entry from RAM.
func (s *Store) Stat(kind Kind, account, col, name string) (ObjectInfo, error) {
	if !kind.valid() || !ValidName(kind, name) {
		return ObjectInfo{}, ErrInvalidName
	}
	c, err := s.collectionFor(kind, account, col)
	if err != nil {
		return ObjectInfo{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.st.Index[name]
	if c.deleted || e == nil {
		return ObjectInfo{}, ErrNotFound
	}
	return ObjectInfo{Name: name, Entry: copyEntry(e)}, nil
}

// Get reads an entry: its bytes as stored, ETag and modification time.
// The file is read without the lock of the collection (Reader), so a slow
// disk holds up no listing, ctag or other request (1.13).
func (s *Store) Get(kind Kind, account, col, name string) (Object, error) {
	if !kind.valid() || !ValidName(kind, name) {
		return Object{}, ErrInvalidName
	}
	r, err := s.Reader(kind, account, col)
	if err != nil {
		return Object{}, err
	}
	defer r.Close()
	return r.Get(name)
}

// Reader reads entries of one collection one after the other (GET, the
// multigets): the folder of the collection is opened once, on the first
// read, and each file is read without the collection's lock. Its ETag is
// checked against the index afterwards, so a write that lands meanwhile
// gives the new bytes with their ETag (or the old ones with theirs),
// never bytes of one version with the ETag of another. Close it.
type Reader struct {
	s    *Store
	acc  *account
	c    *collection
	kind Kind
	col  string

	or      *os.Root
	release func()
	openErr error
}

// Reader returns a reader of a collection; ErrNotFound for one that does
// not exist (from RAM, the volume is opened by the first Get).
func (s *Store) Reader(kind Kind, account, col string) (*Reader, error) {
	if !kind.valid() {
		return nil, ErrInvalidName
	}
	if !ValidCollection(col) {
		return nil, ErrInvalidCollection
	}
	acc, err := s.accountFor(kind, account)
	if err != nil {
		return nil, err
	}
	c, err := acc.lookup(col)
	if err != nil {
		return nil, err
	}
	return &Reader{s: s, acc: acc, c: c, kind: kind, col: col}, nil
}

// Close ends the use of the volume.
func (r *Reader) Close() {
	if r.release != nil {
		r.release()
		r.release, r.or = nil, nil
	}
}

// readFile reads the file of an entry; ErrNotFound when it is missing.
func (r *Reader) readFile(name string) ([]byte, error) {
	if r.or == nil && r.openErr == nil {
		ar, release, err := r.s.openRead(r.acc)
		if err == nil {
			cr, or, oerr := openCollectionRead(ar, r.col)
			if oerr != nil {
				release()
				err = oerr
			} else {
				r.or = or
				r.release = func() { or.Close(); cr.Close(); release() }
			}
		}
		r.openErr = err
	}
	if r.openErr != nil {
		return nil, r.openErr
	}
	if readHook != nil {
		readHook()
	}
	data, err := readLimited(r.or, encodeName(name), maxSize(r.kind))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

// indexed is the ETag and time of an entry in the index; ok is false when
// the entry or its collection is gone.
func (c *collection) indexed(name string) (etag string, mtime time.Time, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.st.Index[name]
	if c.deleted || e == nil {
		return "", time.Time{}, false
	}
	return e.ETag, e.MTime, true
}

// maxReadRetries is how often a read starts again when the entry changed
// while its file was read.
const maxReadRetries = 3

// Get reads one entry of the reader's collection.
func (r *Reader) Get(name string) (Object, error) {
	if !ValidName(r.kind, name) {
		return Object{}, ErrInvalidName
	}
	for attempt := 0; ; attempt++ {
		want, mtime, ok := r.c.indexed(name)
		if !ok {
			return Object{}, ErrNotFound
		}
		data, err := r.readFile(name)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return Object{}, err
		}
		got := ""
		if err == nil {
			if got = ETag(data); got == want {
				return Object{Name: name, Data: data, ETag: want, MTime: mtime}, nil
			}
		}
		// The bytes are not those of the index: a write ran meanwhile (it
		// holds the lock until its index is in; taking it waits for that),
		// or the file changed behind titand's back.
		cur, curMTime, ok := r.c.indexed(name)
		switch {
		case !ok:
			return Object{}, ErrNotFound
		case err == nil && cur == got:
			return Object{Name: name, Data: data, ETag: got, MTime: curMTime}, nil
		case cur != want && attempt < maxReadRetries:
			continue // changed again: read the new state
		case err != nil:
			return Object{}, err // indexed, but the file is missing
		}
		// Changed behind titand's back: the bytes count.
		slog.Warn("Calendar: entry differs from its index", "kind", r.kind, "account", r.acc.name, "collection", r.col)
		return Object{Name: name, Data: data, ETag: got, MTime: curMTime}, nil
	}
}

// readObject reads the bytes of an entry from the volume.
func (s *Store) readObject(acc *account, col, name string) ([]byte, error) {
	ar, release, err := s.openRead(acc)
	if err != nil {
		return nil, err
	}
	defer release()
	cr, or, err := openCollectionRead(ar, col)
	if err != nil {
		return nil, err
	}
	defer cr.Close()
	defer or.Close()
	data, err := readLimited(or, encodeName(name), maxSize(acc.kind))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

// readHook, in tests, runs before a file is read without a lock (a read
// that a write overtakes).
var readHook func()

func openCollectionRead(ar *os.Root, col string) (*os.Root, *os.Root, error) {
	cr, err := ar.OpenRoot(col)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			err = ErrNotFound
		}
		return nil, nil, err
	}
	or, err := cr.OpenRoot(objectsDir)
	if err != nil {
		cr.Close()
		if errors.Is(err, fs.ErrNotExist) {
			err = ErrNotFound
		}
		return nil, nil, err
	}
	return cr, or, nil
}

func (s *Store) collectionFor(kind Kind, account, col string) (*collection, error) {
	if !ValidCollection(col) {
		return nil, ErrInvalidCollection
	}
	acc, err := s.accountFor(kind, account)
	if err != nil {
		return nil, err
	}
	return acc.lookup(col)
}

// Changes answers sync-collection (RFC 6578): with an empty token every
// entry, else the latest state of each name changed since the token
// (deleted ones with Deleted). A token of another epoch or older than the
// log is ErrInvalidSyncToken.
func (s *Store) Changes(kind Kind, account, col, token string) (ChangeSet, error) {
	return s.ChangesLimit(kind, account, col, token, 0)
}

// ChangesLimit is Changes with at most limit changes (0 = all); a cut
// answer is Truncated and its token goes on after the changes given
// (RFC 6578 3.6, 3.7). The token and the list are one snapshot.
func (s *Store) ChangesLimit(kind Kind, account, col, token string, limit int) (ChangeSet, error) {
	c, err := s.collectionFor(kind, account, col)
	if err != nil {
		return ChangeSet{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted {
		return ChangeSet{}, ErrNotFound
	}
	return c.st.changes(token, limit)
}

// Counts are the events (calendar files) and contacts (without groups) of
// an account, for the dialog that deletes it.
func (s *Store) Counts(name string) (events, contacts int, err error) {
	for _, kind := range []Kind{Calendar, Contacts} {
		acc, err := s.accountFor(kind, name)
		if err != nil {
			return 0, 0, err
		}
		for _, c := range acc.collections() {
			c.mu.Lock()
			for _, e := range c.st.Index {
				switch {
				case kind == Calendar:
					events++
				case !e.Group:
					contacts++
				}
			}
			c.mu.Unlock()
		}
	}
	return events, contacts, nil
}

// SaveSeen writes devices-seen.json of an account (3.5). It makes the
// account's calendar folder (with owner.json) if there is none yet; the
// caller writes at most hourly per device and only when the volume is
// awake for a request anyway.
func (s *Store) SaveSeen(account string, v any) error {
	acc, done, err := s.beginWrite(Calendar, account)
	if err != nil {
		return err
	}
	defer done()
	ar, release, err := s.openWrite(acc)
	if err != nil {
		return err
	}
	defer release()
	return volumedata.WriteJSON(ar, seenFile, v)
}

// LoadSeen reads devices-seen.json; fs.ErrNotExist without one (also for
// a folder of an earlier account of the name).
func (s *Store) LoadSeen(account string, v any) error {
	acc, err := s.accountFor(Calendar, account)
	if err != nil {
		return err
	}
	acc.mu.Lock()
	onDisk := acc.onDisk
	acc.mu.Unlock()
	if !onDisk {
		return fs.ErrNotExist
	}
	ar, release, err := s.openRead(acc)
	if errors.Is(err, ErrNotFound) {
		return fs.ErrNotExist
	}
	if err != nil {
		return err
	}
	defer release()
	return volumedata.ReadJSON(ar, seenFile, v, maxSeenFile)
}

// SavePhoto keeps the reduced picture of a contact (photos/, a cache that
// may be missing) of the book kontakte, made of the card with the ETag
// etag: ErrPrecondition when the card changed meanwhile (the picture of
// the old card must not stay for the new one).
func (s *Store) SavePhoto(account, name, etag string, jpeg []byte) error {
	if !ValidName(Contacts, name) {
		return ErrInvalidName
	}
	if len(jpeg) > MaxPhoto {
		return ErrTooLarge
	}
	acc, done, err := s.beginWrite(Contacts, account)
	if err != nil {
		return err
	}
	defer done()
	c, err := acc.lookup(DefaultBook)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted || c.virtual || c.st.Index[name] == nil {
		return ErrNotFound
	}
	if c.st.Index[name].ETag != etag {
		return ErrPrecondition
	}
	ar, release, err := s.openWrite(acc)
	if err != nil {
		return err
	}
	defer release()
	cr, err := ar.OpenRoot(DefaultBook)
	if err != nil {
		return err
	}
	defer cr.Close()
	if err := cr.Mkdir(photosDir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return err
	}
	pr, err := cr.OpenRoot(photosDir)
	if err != nil {
		return err
	}
	defer pr.Close()
	return volumedata.Write(pr, photoName(name), jpeg)
}

// Photo reads the reduced picture of a contact; ErrNotFound without one.
func (s *Store) Photo(account, name string) ([]byte, error) {
	data, _, err := s.PhotoTag(account, name)
	return data, err
}

// PhotoTag reads the reduced picture of a contact and the ETag of the
// card it was made of; ErrNotFound without one. The file is read without
// the lock of the book; a card changed meanwhile is read again, so the
// picture always belongs to the ETag given.
func (s *Store) PhotoTag(account, name string) ([]byte, string, error) {
	if !ValidName(Contacts, name) {
		return nil, "", ErrInvalidName
	}
	acc, err := s.accountFor(Contacts, account)
	if err != nil {
		return nil, "", err
	}
	c, err := acc.lookup(DefaultBook)
	if err != nil {
		return nil, "", err
	}
	for attempt := 0; ; attempt++ {
		etag, _, ok := c.indexed(name)
		if !ok {
			return nil, "", ErrNotFound
		}
		data, err := s.readPhoto(acc, name)
		if err != nil && !errors.Is(err, ErrNotFound) {
			return nil, "", err
		}
		now, _, ok := c.indexed(name)
		switch {
		case !ok:
			return nil, "", ErrNotFound
		case now != etag && attempt < maxReadRetries:
			continue // the card changed while its picture was read
		case now != etag:
			return nil, "", ErrNotFound // keeps changing: made again from the card
		case err != nil:
			return nil, "", err
		}
		return data, etag, nil
	}
}

func (s *Store) readPhoto(acc *account, name string) ([]byte, error) {
	ar, release, err := s.openRead(acc)
	if err != nil {
		return nil, err
	}
	defer release()
	if readHook != nil {
		readHook()
	}
	data, err := readLimited(ar, DefaultBook+"/"+photosDir+"/"+photoName(name), MaxPhoto)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	return data, err
}

// removePhoto drops the cached picture of a contact (c.mu held).
func removePhoto(cr *os.Root, name string) {
	if err := cr.Remove(photosDir + "/" + photoName(name)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("Contacts: old picture not removed", "err", err)
	}
}
