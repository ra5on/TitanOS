package store

import (
	"errors"
	"io/fs"
	"log/slog"
	"os"
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
	c, err := s.collectionFor(kind, account, col)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted {
		return nil, ErrNotFound
	}
	out := make([]ObjectInfo, 0, len(c.st.Index))
	for _, name := range sortedNames(c.st.Index) {
		out = append(out, ObjectInfo{Name: name, Entry: copyEntry(c.st.Index[name])})
	}
	return out, nil
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
func (s *Store) Get(kind Kind, account, col, name string) (Object, error) {
	if !kind.valid() || !ValidName(kind, name) {
		return Object{}, ErrInvalidName
	}
	acc, err := s.accountFor(kind, account)
	if err != nil {
		return Object{}, err
	}
	c, err := acc.lookup(col)
	if err != nil {
		return Object{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.st.Index[name]
	if c.deleted || e == nil {
		return Object{}, ErrNotFound
	}
	data, err := s.readObject(acc, col, name)
	if err != nil {
		return Object{}, err
	}
	etag := e.ETag
	if got := ETag(data); got != etag {
		// Changed behind titand's back: the bytes count.
		slog.Warn("Calendar: entry differs from its index", "kind", kind, "account", account, "collection", col)
		etag = got
	}
	return Object{Name: name, Data: data, ETag: etag, MTime: e.MTime}, nil
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
// may be missing) of the book kontakte.
func (s *Store) SavePhoto(account, name string, jpeg []byte) error {
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
	if !ValidName(Contacts, name) {
		return nil, ErrInvalidName
	}
	acc, err := s.accountFor(Contacts, account)
	if err != nil {
		return nil, err
	}
	c, err := acc.lookup(DefaultBook)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.deleted || c.st.Index[name] == nil {
		return nil, ErrNotFound
	}
	ar, release, err := s.openRead(acc)
	if err != nil {
		return nil, err
	}
	defer release()
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
