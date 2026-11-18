package store

import (
	"cmp"
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
	// Location (clipped like Summary), the zone of DTSTART (an IANA name,
	// "UTC"; empty for dates and floating times), whether it was guessed,
	// and the alarms as minutes before the start (recur.Alarms): what the
	// lists of the web UI show without reading the file.
	Location string `json:"location,omitempty"`
	TZID     string `json:"tzid,omitempty"`
	TZGuess  bool   `json:"tzGuess,omitempty"`
	Alarms   []int  `json:"alarms,omitempty"`

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
	// Truncated: more changes follow (RFC 6578 3.6); Token continues
	// right after the ones given.
	Truncated bool
}

// syncToken is a parsed sync token: <epoch>/<seq> ("the state at seq"),
// or <epoch>/<seq>/<hex name> while a listing of the whole collection
// goes on in pages: the client has the entries up to the name (by name),
// and nothing it was told before seq is stale.
type syncToken struct {
	epoch     string
	seq       int64
	cursor    string
	hasCursor bool
}

func (t syncToken) String() string {
	s := SyncTokenPrefix + t.epoch + "/" + strconv.FormatInt(t.seq, 10)
	if t.hasCursor {
		s += "/" + hex.EncodeToString([]byte(t.cursor))
	}
	return s
}

func parseSyncToken(token string) (syncToken, bool) {
	rest, ok := strings.CutPrefix(token, SyncTokenPrefix)
	if !ok {
		return syncToken{}, false
	}
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || len(parts) > 3 || parts[0] == "" {
		return syncToken{}, false
	}
	seq, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || seq < 0 || strconv.FormatInt(seq, 10) != parts[1] {
		return syncToken{}, false
	}
	t := syncToken{epoch: parts[0], seq: seq}
	if len(parts) == 3 {
		name, err := hex.DecodeString(parts[2])
		if err != nil || len(name) == 0 || len(name) > MaxNameBytes {
			return syncToken{}, false
		}
		t.cursor, t.hasCursor = string(name), true
	}
	return t, true
}

// changes answers sync-collection: everything for an empty token, else
// the latest state of each name changed after it. limit > 0 cuts the
// answer after that many (RFC 6578 3.6/3.7) with a token that goes on
// right after them:
//   - A listing of everything (empty token, or a cursor token) goes by
//     name; cut after name X, the token is <seq>/<X> with the seq of the
//     listing's start, so later changes to the names already given come
//     again.
//   - Then (or for a plain token) the names changed after the token, by
//     the seq of their latest change; cut after the n-th, the token is
//     that seq: every name left out changed after it.
//
// The page itself is sorted by name.
func (st *State) changes(token string, limit int) (ChangeSet, error) {
	from := syncToken{epoch: st.Epoch, seq: st.Seq, hasCursor: true}
	if token != "" {
		t, ok := parseSyncToken(token)
		if !ok || t.epoch != st.Epoch || t.seq > st.Seq {
			return ChangeSet{}, ErrInvalidSyncToken
		}
		if t.seq < st.Seq && (len(st.Log) == 0 || t.seq < st.Log[0].Seq-1) {
			return ChangeSet{}, ErrInvalidSyncToken // older than the log
		}
		from = t
	}
	out := ChangeSet{Token: st.SyncToken(), Changes: []Change{}}
	listed := map[string]bool{}
	if from.hasCursor {
		for _, name := range sortedNames(st.Index) {
			if name <= from.cursor {
				continue
			}
			if limit > 0 && len(out.Changes) == limit {
				out.Truncated = true
				out.Token = syncToken{epoch: st.Epoch, seq: from.seq, cursor: out.Changes[len(out.Changes)-1].Name, hasCursor: true}.String()
				return out, nil
			}
			out.Changes = append(out.Changes, Change{Name: name, ETag: st.Index[name].ETag})
			listed[name] = true
		}
	}
	// The latest change of each name after the token, oldest first.
	latest := map[string]int64{}
	for _, l := range st.Log {
		if l.Seq > from.seq && l.Name != "" {
			latest[l.Name] = l.Seq
		}
	}
	names := slices.SortedFunc(maps.Keys(latest), func(a, b string) int { return cmp.Compare(latest[a], latest[b]) })
	for _, name := range names {
		if listed[name] {
			continue
		}
		if limit > 0 && len(out.Changes) == limit {
			out.Truncated = true
			last := out.Changes[len(out.Changes)-1].Name
			out.Token = syncToken{epoch: st.Epoch, seq: latest[last]}.String()
			if listed[last] {
				// Only the listing fits: go on from its start.
				out.Token = syncToken{epoch: st.Epoch, seq: from.seq, cursor: last, hasCursor: true}.String()
			}
			break
		}
		if e := st.Index[name]; e != nil {
			out.Changes = append(out.Changes, Change{Name: name, ETag: e.ETag})
		} else {
			out.Changes = append(out.Changes, Change{Name: name, Deleted: true})
		}
	}
	slices.SortFunc(out.Changes, func(a, b Change) int { return strings.Compare(a.Name, b.Name) })
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
