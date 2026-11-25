// Package store keeps calendars and address books on the system data volume
// (TECHNIK 5): one file per entry with the bytes as they came, one
// state.json per collection with counter, epoch, change log and index. It
// is the one contract for CalDAV/CardDAV, the web API and the agent.
//
// Layout below .titanos/calendar and .titanos/contacts:
//
//	<account>/owner.json            {"name","created"}
//	<account>/devices-seen.json     (calendar only, SaveSeen)
//	<account>/<collection>/state.json
//	<account>/<collection>/o/<file> percent-encoded entry name
//	<account>/kontakte/photos/<file>.jpg (contacts only, a cache)
//
// Without a volume every call fails with ErrUnavailable, never with an
// empty collection (iOS would delete its copy). Reading never creates
// anything; the state of each collection stays in RAM after the first
// read, so an unchanged listing touches no disk (disk sleep).
package store

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// Kind is calendar or contacts; its value is the folder below .titanos.
// Tasks (1.13) are the reminder lists (VTODO): calendar collections of
// CalDAV, kept apart in .titanos/tasks so that 1.12 never sees them
// (tasks.go).
type Kind string

const (
	Calendar Kind = "calendar"
	Contacts Kind = "contacts"
	Tasks    Kind = "tasks"
)

// Features are the folders below .titanos the store uses.
var Features = []string{string(Calendar), string(Contacts), string(Tasks)}

func (k Kind) valid() bool { return k == Calendar || k == Contacts || k == Tasks }

func (k Kind) suffix() string {
	if k == Contacts {
		return ".vcf"
	}
	return ".ics"
}

// The collections that exist virtually until the first write.
const (
	DefaultCalendar     = "privat"
	DefaultCalendarName = "Privat"
	// DefaultColor is --icon-square-violet of web/src/app.css.
	DefaultColor    = "#7c3aed"
	DefaultBook     = "kontakte"
	DefaultBookName = "Kontakte"
)

// Limits of TECHNIK 5.3 and 5.4.
const (
	MaxICS         = 1 << 20
	MaxVCF         = 2 << 20
	MaxEntries     = 20000
	MaxCollections = 50
	MaxLog         = 2000
	MaxPhoto       = 512 << 10
)

// Variables only so that tests reach the limits.
var (
	maxEntries     = MaxEntries
	maxCollections = MaxCollections
)

var (
	// ErrUnavailable: no volume (503, never an empty collection).
	ErrUnavailable = volumedata.ErrUnavailable
	ErrNotFound    = errors.New("pim: not found")
	// ErrInvalidAccount, ErrInvalidName, ErrInvalidCollection: a name
	// that may not be used (400).
	ErrInvalidAccount    = errors.New("pim: invalid account name")
	ErrInvalidName       = errors.New("pim: invalid entry name")
	ErrInvalidCollection = errors.New("pim: invalid collection name")
	// ErrNoAccount: the account does not exist.
	ErrNoAccount = errors.New("pim: no such account")
	// ErrInvalidData: no valid iCalendar/vCard (valid-calendar-data).
	ErrInvalidData = errors.New("pim: invalid data")
	// ErrUnsupportedComponent: a component other than VEVENT/VTIMEZONE
	// (supported-calendar-component).
	ErrUnsupportedComponent = errors.New("pim: only VEVENT is supported")
	// ErrTooLarge: over 1 MiB .ics or 2 MiB .vcf (max-resource-size, 413).
	ErrTooLarge = errors.New("pim: entry too large")
	// ErrCollectionFull: 20 000 entries (507).
	ErrCollectionFull = errors.New("pim: collection full")
	// ErrTooManyCollections: 50 collections (507).
	ErrTooManyCollections = errors.New("pim: too many collections")
	// ErrPrecondition: If-Match / If-None-Match failed (412).
	ErrPrecondition = errors.New("pim: precondition failed")
	// ErrUIDConflict: another entry of the collection has the UID, or a
	// PUT overwrites an entry with another UID (no-uid-conflict);
	// errors.As gives *UIDConflictError with the name holding the UID.
	ErrUIDConflict = errors.New("pim: uid already used in the collection")
	// ErrLastCollection: the last own collection is not deleted (409).
	ErrLastCollection = errors.New("pim: last collection")
	// ErrExists: the collection exists already.
	ErrExists = errors.New("pim: collection exists")
	// ErrFixedBook: contacts have the one book "kontakte".
	ErrFixedBook = errors.New("pim: only the address book kontakte")
	// ErrInvalidSyncToken: other epoch or older than the log (403
	// DAV:valid-sync-token).
	ErrInvalidSyncToken = errors.New("pim: invalid sync token")
	// ErrInvalidMeta: a name, color or description that cannot be stored.
	ErrInvalidMeta = errors.New("pim: invalid collection properties")
)

// Changed says that a collection changed: an entry written or deleted,
// its properties, or the collection made or deleted (CTag "" then).
type Changed struct {
	Kind       Kind
	Account    string
	Collection string
	// CTag is the collection's ctag after the change (<epoch>-<seq>): the
	// generation the pages compare with what they read.
	CTag string
}

// changed tells OnChanged.
func (s *Store) changed(kind Kind, account, col string, c *collection) {
	if s.OnChanged == nil {
		return
	}
	ev := Changed{Kind: kind, Account: account, Collection: col}
	if c != nil {
		c.mu.Lock()
		if !c.deleted {
			ev.CTag = c.st.CTag()
		}
		c.mu.Unlock()
	}
	s.OnChanged(ev)
}

// UIDConflictError names the entry that already has the UID.
type UIDConflictError struct{ Name string }

func (e *UIDConflictError) Error() string {
	return fmt.Sprintf("%v: %s", ErrUIDConflict, e.Name)
}

func (e *UIDConflictError) Is(target error) bool { return target == ErrUIDConflict }

// Store is the calendar and contacts store. Set the hooks before use.
type Store struct {
	// Volume opens .titanos/<feature> on the system data volume: with
	// create the folder is made, without it fs.ErrNotExist says there is
	// none (nothing kept yet), ErrUnavailable that there is no volume.
	// release ends the use of the volume.
	Volume func(feature string, create bool) (root *os.Root, release func(), err error)
	// Current names the volume the feature lives on right now, from RAM
	// only (no disk access); "" while there is none. The state in RAM
	// belongs to that volume: another name drops it. Nil: one volume.
	Current func(feature string) string
	// Account returns the creation time of an account; ok is false when
	// it does not exist. A folder written for an earlier account of the
	// same name is not read. Nil: every valid name, without a time.
	Account func(name string) (created time.Time, ok bool)
	// Local is the zone of the NAS (floating times, dates); nil = time.Local.
	// Set it before the store is used; SetLocal changes it later.
	Local func() *time.Location
	zone  atomic.Pointer[func() *time.Location]
	// Now is the clock; nil = time.Now.
	Now func() time.Time
	// ReadMark and WriteMark read and write /config/users/<account>/
	// pim.json (config.Store, atomic): the mark that an account kept data
	// of a feature (marks.go). ReadMark gives fs.ErrNotExist without one.
	// Nil: no mark, a missing folder shows the virtual default.
	ReadMark  func(account string) ([]byte, error)
	WriteMark func(account string, data []byte) error
	// Mounted lists the mounted volumes (RAM only), Peek opens
	// .titanos/<feature> on one of them without making anything: where
	// the folder of an account went (journal hint). Nil: no hint.
	Mounted func() []string
	Peek    func(volume, feature string) (*os.Root, error)
	// OnChanged hears every change of a collection or its entries, from
	// any side (web API, CalDAV/CardDAV, the agent, an import), after the
	// locks are given back (1.13, the pages' live updates). It must not
	// block. Nil: nobody listens.
	OnChanged func(Changed)

	mu         sync.Mutex
	features   map[Kind]*featureCache
	ctxs       map[string]*accountCtx
	forgetting map[string]int
	restoring  map[string]bool

	parsed parsedCache

	// markMu guards marks, hints and logged (marks.go); taken last.
	markMu sync.Mutex
	marks  map[string]*knownMark
	hints  map[Kind]*hintCache
	logged map[string]string

	// failState, in tests, fails the write of state.json.
	failState func(collection string) error
}

type accountCtx struct {
	ctx    context.Context
	cancel context.CancelFunc
}

// featureCache is what RAM knows of one feature on one volume.
type featureCache struct {
	volume   string
	accounts map[string]*account
}

type account struct {
	kind    Kind
	name    string
	created time.Time

	// gate: writes hold it shared, ForgetUser alone; gone is then set.
	gate sync.RWMutex
	gone bool
	// forgotten is gone for readers, which do not take the gate (a reader
	// may write too, and a second RLock while ForgetUser waits would hang):
	// set by ForgetUser before the folder goes (openRead).
	forgotten atomic.Bool

	// mu guards the fields below. Lock order: a collection's mu may be
	// held when taking mu, never the other way round.
	mu      sync.Mutex
	loaded  bool
	onDisk  bool // the folder with this account's owner.json exists
	foreign bool // a folder of an earlier account (or none): removed on the first write
	cols    map[string]*collection
	virtual *collection
	// away: the account kept data, but the folder is not on this volume
	// (ErrElsewhere); read again when the mounted volumes change (awaySig).
	away    *ElsewhereError
	awaySig string
}

type collection struct {
	mu      sync.Mutex
	id      string
	st      *State
	virtual bool
	deleted bool
}

// New returns a store; the hooks are set afterwards.
func New() *Store {
	return &Store{}
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) local() *time.Location {
	if f := s.zone.Load(); f != nil {
		if loc := (*f)(); loc != nil {
			return loc
		}
	}
	if s.Local != nil {
		if loc := s.Local(); loc != nil {
			return loc
		}
	}
	return time.Local
}

// SetLocal replaces the zone of the NAS on a running store (tests set it
// after the server started, while its reminders already read it); it
// wins over Local.
func (s *Store) SetLocal(f func() *time.Location) { s.zone.Store(&f) }

// Zone is the zone of the NAS the store reads dates and floating times in.
func (s *Store) Zone() *time.Location { return s.local() }

// Context ends when the account is forgotten (ForgetUser): running DAV
// requests of the account watch it.
func (s *Store) Context(name string) context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctxs == nil {
		s.ctxs = map[string]*accountCtx{}
	}
	c := s.ctxs[name]
	if c == nil {
		ctx, cancel := context.WithCancel(context.Background())
		c = &accountCtx{ctx: ctx, cancel: cancel}
		s.ctxs[name] = c
	}
	return c.ctx
}

// Moved drops what RAM knows of a feature: its folder went to another
// volume (volumedata.Home OnMoved) and is read again from there.
func (s *Store) Moved(feature string) {
	s.mu.Lock()
	delete(s.features, Kind(feature))
	s.mu.Unlock()
	s.parsed.dropKind(Kind(feature))
}

// open runs the Volume hook.
func (s *Store) open(kind Kind, create bool) (*os.Root, func(), error) {
	if s.Volume == nil {
		return nil, nil, ErrUnavailable
	}
	root, release, err := s.Volume(string(kind), create)
	if err != nil {
		return nil, nil, err
	}
	if release == nil {
		release = func() {}
	}
	return root, func() { _ = root.Close(); release() }, nil // closing a directory changes nothing on disk
}

// accountFor returns the loaded state of an account: from RAM, else read
// once from the volume (creating nothing).
func (s *Store) accountFor(kind Kind, name string) (*account, error) {
	if !kind.valid() {
		return nil, ErrInvalidCollection
	}
	if !accountRE.MatchString(name) {
		return nil, ErrInvalidAccount
	}
	var created time.Time
	if s.Account != nil {
		c, ok := s.Account(name)
		if !ok {
			return nil, ErrNoAccount
		}
		created = c
	}
	volume := "."
	if s.Current != nil {
		volume = s.Current(string(kind))
	}
	s.mu.Lock()
	if s.forgetting[name] > 0 {
		s.mu.Unlock()
		return nil, ErrUnavailable
	}
	if s.features == nil {
		s.features = map[Kind]*featureCache{}
	}
	fc := s.features[kind]
	if volume == "" {
		// What RAM knew belongs to a volume that is gone.
		delete(s.features, kind)
		s.mu.Unlock()
		if fc != nil {
			s.parsed.dropKind(kind)
		}
		return nil, ErrUnavailable
	}
	if fc == nil || fc.volume != volume {
		dropped := fc != nil
		fc = &featureCache{volume: volume, accounts: map[string]*account{}}
		s.features[kind] = fc
		if dropped {
			defer s.parsed.dropKind(kind)
		}
	}
	acc := fc.accounts[name]
	if acc == nil || !acc.created.Equal(created) {
		acc = &account{kind: kind, name: name, created: created}
		fc.accounts[name] = acc
	}
	s.mu.Unlock()
	acc.mu.Lock()
	defer acc.mu.Unlock()
	if acc.loaded && acc.away != nil && acc.awaySig != s.mountSig() {
		// Volumes came or went: look again (once per mount set, so an
		// iPhone that asks every 15 minutes wakes no disk).
		acc.loaded = false
	}
	if !acc.loaded {
		if err := s.loadLocked(acc, volume); err != nil {
			return nil, err
		}
	}
	if acc.away != nil {
		return nil, acc.away
	}
	return acc, nil
}

// owner is owner.json: the account a folder belongs to.
type owner struct {
	Name    string    `json:"name"`
	Created time.Time `json:"created,omitzero"`
}

const (
	ownerFile    = "owner.json"
	seenFile     = "devices-seen.json"
	stateFile    = "state.json"
	objectsDir   = "o"
	photosDir    = "photos"
	maxStateFile = 64 << 20
	maxSeenFile  = 1 << 20
	deletedPfx   = ".deleted-"
	pendingPfx   = ".pending-"
)

// sameAccount: a folder without a creation time does not belong to an
// account with one (as in tmail); only an account without a known
// creation time goes by its name.
func sameAccount(o owner, name string, created time.Time) bool {
	if o.Name == "" || o.Name != name {
		return false
	}
	if created.IsZero() {
		return true
	}
	return !o.Created.IsZero() && o.Created.Equal(created)
}

// loadLocked reads the account's folder (acc.mu held): owner.json and the
// state of every collection, checked against the files (crash recovery).
// An account that kept data (pim.json) but has no own collection here is
// away (ErrElsewhere), never shown the virtual default.
func (s *Store) loadLocked(acc *account, volume string) error {
	if err := s.loadFolderLocked(acc); err != nil {
		return err
	}
	acc.away, acc.awaySig = nil, ""
	if len(acc.cols) > 0 {
		return nil
	}
	marked, err := s.marked(acc)
	if err != nil {
		acc.loaded = false // not known now: asked again, never served empty
		return err
	}
	if marked {
		acc.awaySig = s.mountSig()
		acc.away = s.elsewhere(acc, volume)
	}
	return nil
}

// loadFolderLocked is the reading part of loadLocked.
func (s *Store) loadFolderLocked(acc *account) error {
	acc.cols = map[string]*collection{}
	acc.virtual = nil
	acc.onDisk, acc.foreign = false, false
	root, release, err := s.open(acc.kind, false)
	if errors.Is(err, fs.ErrNotExist) {
		acc.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	defer release()
	st, err := root.Lstat(acc.name)
	if errors.Is(err, fs.ErrNotExist) {
		acc.loaded = true
		return nil
	}
	if err != nil {
		return err
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("pim: unsafe folder of account %s", acc.name)
	}
	ar, err := root.OpenRoot(acc.name)
	if err != nil {
		return err
	}
	defer ar.Close()
	var o owner
	if err := volumedata.ReadJSON(ar, ownerFile, &o, 4096); err != nil && !errors.Is(err, fs.ErrNotExist) {
		slog.Warn("Calendar: owner.json not readable; the folder is not used", "kind", acc.kind, "account", acc.name, "err", err)
	}
	if !sameAccount(o, acc.name, acc.created) {
		// Of an earlier account of the name: not read, removed on the
		// first write.
		acc.foreign = true
		acc.loaded = true
		return nil
	}
	acc.onDisk = true
	entries, err := ar.Open(".")
	if err != nil {
		return err
	}
	list, err := entries.ReadDir(-1)
	entries.Close()
	if err != nil {
		return err
	}
	var unlock func()
	for _, e := range list {
		name := e.Name()
		if len(name) > len(deletedPfx) && name[:len(deletedPfx)] == deletedPfx && e.IsDir() {
			// A collection whose removal was cut short.
			if unlock == nil {
				if unlock, err = volumedata.Share(ar); err != nil {
					unlock = nil
					continue
				}
			}
			if err := ar.RemoveAll(name); err != nil {
				slog.Warn("Calendar: leftover of a deleted collection not removed", "kind", acc.kind, "account", acc.name, "err", err)
			}
			continue
		}
		if !e.IsDir() || !ValidCollection(name) || (acc.kind == Contacts && name != DefaultBook) {
			continue
		}
		if unlock == nil {
			// Recovery may write state.json.
			if unlock, err = volumedata.Share(ar); err != nil {
				return err
			}
		}
		state, err := s.loadCollection(acc, ar, name)
		if err != nil {
			unlock()
			return fmt.Errorf("pim: collection %s of %s: %w", name, acc.name, err)
		}
		acc.cols[name] = &collection{id: name, st: state}
	}
	if unlock != nil {
		unlock()
	}
	acc.loaded = true
	return nil
}

// defaultMeta is the state of a collection that has none yet.
func defaultState(kind Kind, id string, epoch string, created time.Time) *State {
	st := &State{ID: id, Name: id, Epoch: epoch, Created: created, Index: map[string]*Entry{}}
	switch {
	case kind == Contacts:
		st.Name = DefaultBookName
	case kind == Tasks:
		st.Color = DefaultTaskColor
		if id == DefaultTaskList {
			st.Name = DefaultTaskListName
		}
	case id == DefaultCalendar:
		st.Name, st.Color = DefaultCalendarName, DefaultColor
	default:
		st.Color = DefaultColor
	}
	return st
}

// virtualState is the collection that exists until the first write.
func virtualState(kind Kind) *State {
	return defaultState(kind, defaultID(kind), virtualEpoch, time.Time{})
}

// lookup returns a collection of the account: an own one, or the default
// one while it is virtual. A deleted one is ErrNotFound.
func (acc *account) lookup(id string) (*collection, error) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	if c := acc.cols[id]; c != nil {
		return c, nil
	}
	if len(acc.cols) == 0 && id == defaultID(acc.kind) {
		if acc.virtual == nil || acc.virtual.deleted {
			acc.virtual = &collection{id: id, st: virtualState(acc.kind), virtual: true}
		}
		return acc.virtual, nil
	}
	return nil, ErrNotFound
}

func defaultID(kind Kind) string {
	switch kind {
	case Contacts:
		return DefaultBook
	case Tasks:
		return DefaultTaskList
	}
	return DefaultCalendar
}

// collections lists the collections to show: the own ones, or the
// virtual default without any.
func (acc *account) collections() []*collection {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	if len(acc.cols) == 0 {
		if acc.virtual == nil || !acc.virtual.virtual || acc.virtual.deleted {
			acc.virtual = &collection{id: defaultID(acc.kind), st: virtualState(acc.kind), virtual: true}
		}
		return []*collection{acc.virtual}
	}
	out := make([]*collection, 0, len(acc.cols))
	for _, c := range acc.cols {
		out = append(out, c)
	}
	return out
}

// openWriteLocked opens the account's folder for a write (acc.mu held):
// makes it, removes a folder of an earlier account first, writes
// owner.json once, and holds volumedata.Share on it until release.
func (s *Store) openWriteLocked(acc *account) (*os.Root, func(), error) {
	root, release, err := s.open(acc.kind, true)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*os.Root, func(), error) {
		release()
		return nil, nil, err
	}
	if acc.foreign {
		if err := root.RemoveAll(acc.name); err != nil {
			return fail(err)
		}
		acc.foreign = false
		acc.onDisk = false
	}
	if err := root.Mkdir(acc.name, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return fail(err)
	}
	st, err := root.Lstat(acc.name)
	if err != nil {
		return fail(err)
	}
	if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
		return fail(fmt.Errorf("pim: unsafe folder of account %s", acc.name))
	}
	ar, err := root.OpenRoot(acc.name)
	if err != nil {
		return fail(err)
	}
	unlock, err := volumedata.Share(ar)
	if err != nil {
		ar.Close()
		return fail(err)
	}
	done := func() { unlock(); _ = ar.Close(); release() }
	if !acc.onDisk {
		if err := volumedata.WriteJSON(ar, ownerFile, owner{Name: acc.name, Created: acc.created}); err != nil {
			done()
			return nil, nil, err
		}
		acc.onDisk = true
	}
	return ar, done, nil
}

// openWrite is openWriteLocked for a caller that holds a collection lock.
func (s *Store) openWrite(acc *account) (*os.Root, func(), error) {
	acc.mu.Lock()
	defer acc.mu.Unlock()
	return s.openWriteLocked(acc)
}

// openRead opens the account's folder to read; ErrNotFound when the
// account keeps nothing.
//
// The folder is found by name. A reader that began before the account was
// deleted and made anew under the same name would otherwise read the new
// account's files (unabhängige Durchsicht 1.13, A-6): after opening, the
// folder must still belong to this account (owner.json with its creation
// time, read through the opened folder, which stays that folder even if
// the name is taken again meanwhile), and the account must not have been
// forgotten (for an account without a known creation time).
func (s *Store) openRead(acc *account) (*os.Root, func(), error) {
	root, release, err := s.open(acc.kind, false)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	ar, err := root.OpenRoot(acc.name)
	if err != nil {
		release()
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, err
	}
	done := func() { _ = ar.Close(); release() }
	if acc.forgotten.Load() {
		done()
		return nil, nil, ErrUnavailable
	}
	var o owner
	if err := volumedata.ReadJSON(ar, ownerFile, &o, 4096); err != nil && !errors.Is(err, fs.ErrNotExist) {
		done()
		return nil, nil, err
	}
	if !sameAccount(o, acc.name, acc.created) {
		done()
		return nil, nil, ErrNotFound
	}
	return ar, done, nil
}

// beginWrite takes the account for a write: the gate against ForgetUser.
func (s *Store) beginWrite(kind Kind, name string) (*account, func(), error) {
	acc, err := s.accountFor(kind, name)
	if err != nil {
		return nil, nil, err
	}
	acc.gate.RLock()
	if acc.gone {
		acc.gate.RUnlock()
		return nil, nil, ErrUnavailable
	}
	return acc, acc.gate.RUnlock, nil
}
