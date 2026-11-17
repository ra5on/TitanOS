package store

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/volumedata"
)

// State is state.json of a collection (TECHNIK 5.4).
type State struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Color       string    `json:"color"`
	Order       int       `json:"order"`
	Description string    `json:"description"`
	Epoch       string    `json:"epoch"`
	Seq         int64     `json:"seq"`
	Created     time.Time `json:"created"`
	// Index: entry name (decoded) → what lists, search and queries need.
	// It can be rebuilt from the files and is no source of truth.
	Index map[string]*Entry `json:"index"`
	// Log holds the last MaxLog changes with consecutive seq values
	// ending at Seq.
	Log []LogEntry `json:"log"`
}

// LogEntry is one change; Name is "" for a change of the collection's
// properties.
type LogEntry struct {
	Seq     int64  `json:"seq"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// Entry is the index entry of one file.
type Entry struct {
	ETag  string    `json:"etag"`
	UID   string    `json:"uid,omitempty"`
	Size  int64     `json:"size"`
	MTime time.Time `json:"mtime"`

	// Calendar. Start/End of an event that does not recur (of the master
	// otherwise, End then zero): an instant, or for AllDay and Floating
	// the wall clock written as UTC (converted with the NAS zone when
	// asked, so a new zone needs no rebuild).
	Start       time.Time `json:"start,omitzero"`
	End         time.Time `json:"end,omitzero"`
	AllDay      bool      `json:"allDay,omitempty"`
	Floating    bool      `json:"floating,omitempty"`
	Recurs      bool      `json:"recurs,omitempty"`
	Unsupported bool      `json:"recurUnsupported,omitempty"`
	Summary     string    `json:"summary,omitempty"`

	// Contacts.
	FN       string   `json:"fn,omitempty"`
	SortKey  string   `json:"sortKey,omitempty"`
	Org      string   `json:"org,omitempty"`
	Emails   []string `json:"emails,omitempty"`
	Phones   []string `json:"phones,omitempty"` // digits only (a leading + kept)
	BDay     string   `json:"bday,omitempty"`
	Group    bool     `json:"group,omitempty"` // a group vCard: hidden in lists and search
	HasPhoto bool     `json:"hasPhoto,omitempty"`
}

// ETag is the strong entity tag of stored bytes, without quotes: the first
// 16 bytes of SHA-256 in hex (32 characters).
func ETag(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:16])
}

// virtualEpoch is the epoch of a collection that exists only virtually.
const virtualEpoch = "0"

// SyncTokenPrefix starts every sync token (RFC 6578 wants a URI).
const SyncTokenPrefix = "https://ns.titanos.invalid/sync/"

// CTag is <epoch>-<seq>.
func (st *State) CTag() string { return st.Epoch + "-" + strconv.FormatInt(st.Seq, 10) }

// SyncToken is SyncTokenPrefix<epoch>/<seq>.
func (st *State) SyncToken() string {
	return SyncTokenPrefix + st.Epoch + "/" + strconv.FormatInt(st.Seq, 10)
}

func newEpoch() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand does not fail on Linux
	}
	return hex.EncodeToString(b[:])
}

// change counts one change: seq+1 and a log entry, the log trimmed to MaxLog.
func (st *State) change(name string, deleted bool) {
	st.Seq++
	st.Log = append(st.Log, LogEntry{Seq: st.Seq, Name: name, Deleted: deleted})
	if n := len(st.Log) - MaxLog; n > 0 {
		st.Log = slices.Delete(st.Log, 0, n)
	}
}

// Change is the latest state of one entry since a sync token.
type Change struct {
	Name    string
	ETag    string // "" when deleted
	Deleted bool
}

// ChangeSet is the answer of Changes.
type ChangeSet struct {
	Token   string
	Changes []Change
}

// changes answers sync-collection: everything for an empty token, else
// the latest state of each name changed after it.
func (st *State) changes(token string) (ChangeSet, error) {
	out := ChangeSet{Token: st.SyncToken(), Changes: []Change{}}
	if token == "" {
		for _, name := range sortedNames(st.Index) {
			out.Changes = append(out.Changes, Change{Name: name, ETag: st.Index[name].ETag})
		}
		return out, nil
	}
	rest, ok := strings.CutPrefix(token, SyncTokenPrefix)
	epoch, seqText, ok2 := strings.Cut(rest, "/")
	seq, err := strconv.ParseInt(seqText, 10, 64)
	if !ok || !ok2 || err != nil || epoch != st.Epoch || seq < 0 || seq > st.Seq {
		return ChangeSet{}, ErrInvalidSyncToken
	}
	if seq < st.Seq && (len(st.Log) == 0 || seq < st.Log[0].Seq-1) {
		return ChangeSet{}, ErrInvalidSyncToken // older than the log
	}
	latest := map[string]bool{}
	for _, l := range st.Log {
		if l.Seq > seq && l.Name != "" {
			latest[l.Name] = true
		}
	}
	for _, name := range slices.Sorted(maps.Keys(latest)) {
		if e := st.Index[name]; e != nil {
			out.Changes = append(out.Changes, Change{Name: name, ETag: e.ETag})
		} else {
			out.Changes = append(out.Changes, Change{Name: name, Deleted: true})
		}
	}
	return out, nil
}

func sortedNames(index map[string]*Entry) []string {
	names := make([]string, 0, len(index))
	for n := range index {
		names = append(names, n)
	}
	slices.Sort(names)
	return names
}

// writeState writes state.json of a collection.
func (s *Store) writeState(cr *os.Root, st *State) error {
	if s.failState != nil {
		if err := s.failState(st.ID); err != nil {
			return err
		}
	}
	return volumedata.WriteJSON(cr, stateFile, st)
}

// loadCollection reads state.json and checks its index against the files
// (size and modification time). A difference means titand stopped between
// a file and its state.json (or the state is lost): the index is rebuilt
// from the files and the collection gets a new epoch, so that every device
// syncs once in full. Nothing is lost and nothing doubles.
func (s *Store) loadCollection(acc *account, ar *os.Root, id string) (*State, error) {
	cr, err := ar.OpenRoot(id)
	if err != nil {
		return nil, err
	}
	defer cr.Close()
	var st State
	readErr := volumedata.ReadJSON(cr, stateFile, &st, maxStateFile)
	if readErr != nil && !errors.Is(readErr, fs.ErrNotExist) {
		slog.Warn("Calendar: state.json not readable; rebuilt from the entries", "kind", acc.kind, "account", acc.name, "collection", id, "err", readErr)
	}
	good := readErr == nil && st.ID == id && st.Epoch != "" && st.Epoch != virtualEpoch
	if st.Index == nil {
		st.Index = map[string]*Entry{}
	}
	or, err := cr.OpenRoot(objectsDir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := cr.Mkdir(objectsDir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		or, err = cr.OpenRoot(objectsDir)
	}
	if err != nil {
		return nil, err
	}
	defer or.Close()
	removePending(cr)
	removePending(or)
	files, err := listFiles(acc.kind, or)
	if err != nil {
		return nil, err
	}
	same := good && len(files) == len(st.Index)
	if same {
		for name, info := range files {
			e := st.Index[name]
			if e == nil || e.Size != info.Size() || !e.MTime.Equal(info.ModTime()) {
				same = false
				break
			}
		}
	}
	if same {
		return &st, nil
	}
	// Rebuild.
	fresh := defaultState(acc.kind, id, newEpoch(), s.now().UTC())
	if readErr == nil && st.ID == id {
		fresh.Name, fresh.Color, fresh.Order, fresh.Description = st.Name, st.Color, st.Order, st.Description
		if !st.Created.IsZero() {
			fresh.Created = st.Created
		}
		fresh.Seq = st.Seq + 1
	}
	for name, info := range files {
		if old := st.Index[name]; good && old != nil && old.Size == info.Size() && old.MTime.Equal(info.ModTime()) {
			fresh.Index[name] = old
			continue
		}
		data, err := readLimited(or, encodeName(name), maxSize(acc.kind))
		if err != nil {
			slog.Warn("Calendar: entry not readable while rebuilding the index", "kind", acc.kind, "account", acc.name, "collection", id, "err", err)
			continue
		}
		e, err := s.inspect(acc.kind, acc.name, name, data)
		if err != nil {
			// Kept and served as it is, with what can be said of it.
			e = &Entry{ETag: ETag(data)}
		}
		e.Size, e.MTime = info.Size(), info.ModTime()
		fresh.Index[name] = e
	}
	slog.Info("Calendar: index rebuilt from the entries; devices sync once in full", "kind", acc.kind, "account", acc.name, "collection", id, "entries", len(fresh.Index))
	if err := s.writeState(cr, fresh); err != nil {
		// The state in RAM is right; the next start rebuilds again.
		slog.Warn("Calendar: rebuilt state.json not written", "kind", acc.kind, "account", acc.name, "collection", id, "err", err)
	}
	return fresh, nil
}

func maxSize(kind Kind) int64 {
	if kind == Contacts {
		return MaxVCF
	}
	return MaxICS
}

// listFiles returns the entries in o/ by decoded name; other files are
// left alone and never served.
func listFiles(kind Kind, or *os.Root) (map[string]fs.FileInfo, error) {
	dir, err := or.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	out := map[string]fs.FileInfo{}
	for {
		batch, err := dir.ReadDir(1024)
		for _, e := range batch {
			if !e.Type().IsRegular() {
				continue
			}
			name, ok := decodeName(kind, e.Name())
			if !ok {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out[name] = info
		}
		if err == io.EOF || len(batch) == 0 {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
	}
}

// removePending removes what an interrupted volumedata.Write left.
func removePending(root *os.Root) {
	dir, err := root.Open(".")
	if err != nil {
		return
	}
	names, _ := dir.Readdirnames(-1)
	dir.Close()
	for _, n := range names {
		if strings.HasPrefix(n, pendingPfx) {
			_ = root.Remove(n) // a leftover; the next load tries again
		}
	}
}

// readLimited reads a regular file of at most limit bytes.
func readLimited(root *os.Root, name string, limit int64) ([]byte, error) {
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("pim: not a regular file")
	}
	if info.Size() > limit {
		return nil, ErrTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, ErrTooLarge
	}
	return data, nil
}
