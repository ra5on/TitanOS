package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
)

// env is a volume in a temp folder with a counting Volume hook.
type env struct {
	t       *testing.T
	dir     string
	present atomic.Bool
	opens   atomic.Int32
	creates atomic.Int32
	volume  atomic.Value // string: the name Current returns
	mu      sync.Mutex
	created map[string]time.Time
}

var (
	t1 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 = time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
)

func newEnv(t *testing.T) *env {
	e := &env{t: t, dir: t.TempDir(), created: map[string]time.Time{"alex": t1, "bea": t1}}
	e.present.Store(true)
	e.volume.Store("data")
	return e
}

func (e *env) store() *Store {
	s := New()
	s.Local = func() *time.Location { return time.UTC }
	s.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		if !e.present.Load() {
			return nil, nil, ErrUnavailable
		}
		e.opens.Add(1)
		dir := filepath.Join(e.dir, ".titanos", feature)
		if create {
			e.creates.Add(1)
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, nil, err
			}
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			return nil, nil, err
		}
		return root, func() {}, nil
	}
	s.Current = func(string) string {
		if !e.present.Load() {
			return ""
		}
		return e.volume.Load().(string)
	}
	s.Account = func(name string) (time.Time, bool) {
		e.mu.Lock()
		defer e.mu.Unlock()
		c, ok := e.created[name]
		return c, ok
	}
	return s
}

func (e *env) setCreated(name string, at time.Time) {
	e.mu.Lock()
	e.created[name] = at
	e.mu.Unlock()
}

// tree lists every path below the volume.
func (e *env) tree() []string {
	var out []string
	_ = filepath.WalkDir(e.dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && p != e.dir {
			rel, _ := filepath.Rel(e.dir, p)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

func ics(uid, summary, start string, extra ...string) []byte {
	lines := []string{"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:-//test//EN", "BEGIN:VEVENT", "UID:" + uid,
		"DTSTAMP:20261101T000000Z", "DTSTART:" + start, "DURATION:PT1H", "SUMMARY:" + summary}
	lines = append(lines, extra...)
	lines = append(lines, "END:VEVENT", "END:VCALENDAR", "")
	return []byte(strings.Join(lines, "\r\n"))
}

func vcf(uid, fn string, extra ...string) []byte {
	lines := []string{"BEGIN:VCARD", "VERSION:3.0", "UID:" + uid, "FN:" + fn}
	if !slices.ContainsFunc(extra, func(l string) bool { return strings.HasPrefix(l, "N:") }) {
		lines = append(lines, "N:"+fn+";;;;")
	}
	lines = append(lines, extra...)
	lines = append(lines, "END:VCARD", "")
	return []byte(strings.Join(lines, "\r\n"))
}

func mustPut(t *testing.T, s *Store, kind Kind, col, name string, data []byte) PutResult {
	t.Helper()
	r, err := s.Put(kind, "alex", col, name, data, PutOptions{})
	if err != nil {
		t.Fatalf("put %s: %v", name, err)
	}
	return r
}

func TestVirtualDefaults(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	cals, err := s.Collections(Calendar, "alex")
	if err != nil {
		t.Fatal(err)
	}
	if len(cals) != 1 || cals[0].ID != "privat" || cals[0].Name != "Privat" || cals[0].Color != "#7c3aed" || !cals[0].Virtual || cals[0].CTag != "0-0" {
		t.Fatalf("virtual calendar: %+v", cals)
	}
	books, err := s.Collections(Contacts, "alex")
	if err != nil || len(books) != 1 || books[0].ID != "kontakte" || books[0].Name != "Kontakte" || books[0].CTag != "0-0" {
		t.Fatalf("virtual book: %+v %v", books, err)
	}
	if _, err := s.GetCollection(Calendar, "alex", "arbeit"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown collection: %v", err)
	}
	if len(e.tree()) != 0 || e.creates.Load() != 0 {
		t.Fatalf("virtual collections made something: %v", e.tree())
	}
	// The first write makes it real; a new own calendar hides the virtual one later only
	// while none exists.
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	cals, _ = s.Collections(Calendar, "alex")
	if len(cals) != 1 || cals[0].Virtual || cals[0].CTag == "0-0" || cals[0].Count != 1 {
		t.Fatalf("after first write: %+v", cals)
	}
	if err := s.DeleteCollection(Calendar, "alex", "privat", ""); !errors.Is(err, ErrLastCollection) {
		t.Fatalf("last collection: %v", err)
	}
	if err := s.DeleteCollection(Contacts, "alex", "kontakte", ""); !errors.Is(err, ErrLastCollection) {
		t.Fatalf("virtual book deleted: %v", err)
	}
	if _, err := s.CreateCollection(Contacts, "alex", "zwei", Meta{Name: "Zwei"}); !errors.Is(err, ErrFixedBook) {
		t.Fatalf("second book: %v", err)
	}
}

// Another calendar while the default is virtual: only that one is shown.
// The first own calendar makes the virtual "privat" real as well: a device
// that synced it keeps it, and a PUT made offline still lands there.
func TestVirtualKeptByOwnCalendar(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	if _, err := s.CreateCollection(Calendar, "alex", "arbeit", Meta{Name: "Arbeit", Color: "#112233"}); err != nil {
		t.Fatal(err)
	}
	cals, _ := s.Collections(Calendar, "alex")
	if len(cals) != 2 || cals[0].ID != "arbeit" && cals[1].ID != "arbeit" {
		t.Fatalf("%+v", cals)
	}
	for _, c := range cals {
		switch c.ID {
		case "arbeit":
			if c.Color != "#112233" || c.Virtual {
				t.Fatalf("arbeit %+v", c)
			}
		case "privat":
			if c.Virtual || c.Name != DefaultCalendarName || c.Color != DefaultColor || c.Count != 0 || c.CTag == "0-0" {
				t.Fatalf("privat %+v", c)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".titanos/calendar/alex/privat", stateFile)); err != nil {
		t.Fatalf("privat not on the volume: %v", err)
	}
	if _, err := s.Put(Calendar, "alex", "privat", "a.ics", ics("a", "A", "20261110T080000Z"), PutOptions{IfNoneMatch: true}); err != nil {
		t.Fatalf("put into privat after the first own calendar: %v", err)
	}
	// Read again from the volume: both stay.
	if cals, err := e.store().Collections(Calendar, "alex"); err != nil || len(cals) != 2 {
		t.Fatalf("after reload: %+v %v", cals, err)
	}
	// Creating "privat" itself while it is virtual takes the properties.
	if _, err := s.CreateCollection(Calendar, "bea", "privat", Meta{Name: "Mein", Color: "#445566"}); err != nil {
		t.Fatal(err)
	}
	if cals, _ := s.Collections(Calendar, "bea"); len(cals) != 1 || cals[0].Name != "Mein" || cals[0].Color != "#445566" || cals[0].Virtual {
		t.Fatalf("bea %+v", cals)
	}
	if _, err := s.CreateCollection(Calendar, "alex", "arbeit", Meta{}); !errors.Is(err, ErrExists) {
		t.Fatalf("twice: %v", err)
	}
	if _, err := s.CreateCollection(Calendar, "alex", "Bad.Name", Meta{}); !errors.Is(err, ErrInvalidCollection) {
		t.Fatalf("bad id: %v", err)
	}
	if _, err := s.CreateCollection(Calendar, "alex", "6F2A61B8-1C9E-4D5A-9C3B-7E8F00112233", Meta{Name: "iOS"}); err != nil {
		t.Fatalf("iOS uuid: %v", err)
	}
	if _, err := s.CreateCollection(Calendar, "alex", "x", Meta{Color: "violet"}); !errors.Is(err, ErrInvalidMeta) {
		t.Fatalf("bad color: %v", err)
	}
}

// An entry is not overwritten with another UID (RFC 4791 5.3.2.1, RFC
// 6352 6.3.2.1); a card without UID may get one.
func TestPutKeepsUID(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	first := mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	_, err := s.Put(Calendar, "alex", "privat", "a.ics", ics("b", "B", "20261110T080000Z"), PutOptions{})
	var uc *UIDConflictError
	if !errors.As(err, &uc) || uc.Name != "a.ics" {
		t.Fatalf("other uid: %v", err)
	}
	if info, _ := s.Stat(Calendar, "alex", "privat", "a.ics"); info.ETag != first.ETag || info.UID != "a" {
		t.Fatalf("changed: %+v", info)
	}
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A2", "20261110T080000Z"))
	noUID := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Erika\r\nN:Erika;;;;\r\nEND:VCARD\r\n")
	mustPut(t, s, Contacts, "kontakte", "e.vcf", noUID)
	mustPut(t, s, Contacts, "kontakte", "e.vcf", vcf("e", "Erika"))
	if _, err := s.Put(Contacts, "alex", "kontakte", "e.vcf", vcf("f", "Erika"), PutOptions{}); !errors.As(err, &uc) {
		t.Fatalf("card with another uid: %v", err)
	}
	if _, err := s.Put(Contacts, "alex", "kontakte", "e.vcf", noUID, PutOptions{}); !errors.As(err, &uc) {
		t.Fatalf("card without its uid: %v", err)
	}
}

func TestVolumeMissing(t *testing.T) {
	e := newEnv(t)
	e.present.Store(false)
	s := e.store()
	check := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, ErrUnavailable) {
			t.Fatalf("%s without volume: %v", what, err)
		}
	}
	_, err := s.Collections(Calendar, "alex")
	check("collections", err)
	_, err = s.Collections(Contacts, "alex")
	check("books", err)
	_, err = s.Objects(Calendar, "alex", "privat")
	check("objects", err)
	_, err = s.Get(Calendar, "alex", "privat", "a.ics")
	check("get", err)
	_, err = s.Changes(Calendar, "alex", "privat", "")
	check("changes", err)
	_, err = s.Put(Calendar, "alex", "privat", "a.ics", ics("a", "A", "20261110T080000Z"), PutOptions{})
	check("put", err)
	_, err = s.CreateCollection(Calendar, "alex", "arbeit", Meta{})
	check("create", err)
	check("delete", s.DeleteObject(Calendar, "alex", "privat", "a.ics", ""))
	_, _, err = s.Counts("alex")
	check("counts", err)
	check("save seen", s.SaveSeen("alex", map[string]string{}))
	_, err = s.Query("alex", "privat", t1, t1.AddDate(0, 1, 0))
	check("query", err)
	if len(e.tree()) != 0 {
		t.Fatalf("made without a volume: %v", e.tree())
	}

	// A warm cache does not outlive its volume.
	e.present.Store(true)
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	e.present.Store(false)
	_, err = s.Collections(Calendar, "alex")
	check("cached collections", err)
	_, err = s.Objects(Calendar, "alex", "privat")
	check("cached objects", err)
	// Back, but on another volume (the default moved without a move):
	// read again from there.
	e.present.Store(true)
	e.volume.Store("other")
	before := e.opens.Load()
	if _, err := s.Collections(Calendar, "alex"); err != nil {
		t.Fatal(err)
	}
	if e.opens.Load() == before {
		t.Fatal("state of another volume served from RAM")
	}
	before = e.opens.Load()
	if _, err := s.Collections(Calendar, "alex"); err != nil || e.opens.Load() != before {
		t.Fatalf("second read touched the volume: %v", err)
	}
}

func TestReadCreatesNothing(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	reads := func() {
		_, _ = s.Collections(Calendar, "alex")
		_, _ = s.Collections(Contacts, "alex")
		_, _ = s.GetCollection(Calendar, "alex", "privat")
		_, _ = s.Objects(Calendar, "alex", "privat")
		_, _ = s.Objects(Contacts, "alex", "kontakte")
		_, _ = s.Get(Calendar, "alex", "privat", "a.ics")
		_, _ = s.Stat(Calendar, "alex", "privat", "a.ics")
		_, _ = s.Changes(Calendar, "alex", "privat", "")
		_, _ = s.Query("alex", "privat", from, from.AddDate(0, 1, 0))
		_, _ = s.Overlapping("alex", "privat", from, time.Time{})
		_, _, _ = s.Counts("alex")
		var v map[string]any
		if err := s.LoadSeen("alex", &v); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("load seen: %v", err)
		}
		_, _ = s.Photo("alex", "a.vcf")
	}
	reads()
	if e.creates.Load() != 0 || len(e.tree()) != 0 {
		t.Fatalf("reading made %v (%d create opens)", e.tree(), e.creates.Load())
	}
	// With a folder of another account: still nothing new for alex.
	if _, err := s.Put(Calendar, "bea", "privat", "b.ics", ics("b", "B", "20261110T080000Z"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	before := e.tree()
	e.creates.Store(0)
	reads()
	s = e.store()
	reads()
	if e.creates.Load() != 0 || !slices.Equal(before, e.tree()) {
		t.Fatalf("reading changed the tree: %v → %v", before, e.tree())
	}
}

func TestNoVolumeAccessWithoutChange(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	mustPut(t, s, Contacts, "kontakte", "c.vcf", vcf("c", "Carla"))
	if _, err := s.CreateCollection(Calendar, "alex", "arbeit", Meta{Name: "Arbeit"}); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	before := e.opens.Load()
	for range 5 {
		if _, err := s.Collections(Calendar, "alex"); err != nil {
			t.Fatal(err)
		}
		_, _ = s.Collections(Contacts, "alex")
		_, _ = s.GetCollection(Calendar, "alex", "privat")
		_, _ = s.Objects(Calendar, "alex", "privat")
		_, _ = s.Objects(Contacts, "alex", "kontakte")
		_, _ = s.Stat(Calendar, "alex", "privat", "a.ics")
		_, _ = s.Changes(Calendar, "alex", "privat", "")
		if q, err := s.Query("alex", "privat", from, from.AddDate(0, 1, 0)); err != nil || len(q) != 1 {
			t.Fatalf("query: %v %v", q, err)
		}
		_, _ = s.Overlapping("alex", "privat", from, time.Time{})
		_, _, _ = s.Counts("alex")
	}
	if n := e.opens.Load() - before; n != 0 {
		t.Fatalf("unchanged state opened the volume %d times", n)
	}
	// A fresh start reads once, then RAM again.
	s = e.store()
	if _, err := s.Collections(Calendar, "alex"); err != nil {
		t.Fatal(err)
	}
	before = e.opens.Load()
	for range 3 {
		_, _ = s.Collections(Calendar, "alex")
		_, _ = s.Objects(Calendar, "alex", "privat")
	}
	if e.opens.Load() != before {
		t.Fatal("reads after the first opened the volume")
	}
}

var diskNameRE = regexp.MustCompile(`^[A-Za-z0-9._~%-]+$`)

var etagRE = regexp.MustCompile(`^[0-9a-f]{32}$`)

func TestPutGetAndPreconditions(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	data := ics("a", "A", "20261110T080000Z")
	r, err := s.Put(Calendar, "alex", "privat", "a.ics", data, PutOptions{IfNoneMatch: true})
	if err != nil || !r.Created || !etagRE.MatchString(r.ETag) || r.ETag != ETag(data) {
		t.Fatalf("create: %+v %v", r, err)
	}
	obj, err := s.Get(Calendar, "alex", "privat", "a.ics")
	if err != nil || string(obj.Data) != string(data) || obj.ETag != r.ETag || obj.MTime.IsZero() {
		t.Fatalf("get: %+v %v", obj, err)
	}
	v2 := ics("a", "A2", "20261110T080000Z")
	cases := []struct {
		name string
		opt  PutOptions
		data []byte
		err  error
	}{
		{"if-none-match on existing", PutOptions{IfNoneMatch: true}, v2, ErrPrecondition},
		{"if-match wrong", PutOptions{IfMatch: `"0000"`}, v2, ErrPrecondition},
		{"if-match weak", PutOptions{IfMatch: `W/"` + r.ETag + `"`}, v2, ErrPrecondition},
		{"if-match right quoted", PutOptions{IfMatch: `"` + r.ETag + `"`}, v2, nil},
		{"if-match star", PutOptions{IfMatch: "*"}, v2, nil},
	}
	for _, c := range cases {
		res, err := s.Put(Calendar, "alex", "privat", "a.ics", c.data, c.opt)
		if !errors.Is(err, c.err) {
			t.Fatalf("%s: %v", c.name, err)
		}
		if err == nil && res.Created {
			t.Fatalf("%s: overwrite reported as created", c.name)
		}
	}
	if _, err := s.Put(Calendar, "alex", "privat", "new.ics", ics("n", "N", "20261110T080000Z"), PutOptions{IfMatch: "*"}); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("if-match on missing: %v", err)
	}
	// Deleting with If-Match.
	cur, _ := s.Stat(Calendar, "alex", "privat", "a.ics")
	if err := s.DeleteObject(Calendar, "alex", "privat", "a.ics", "deadbeef"); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("delete wrong etag: %v", err)
	}
	if err := s.DeleteObject(Calendar, "alex", "privat", "a.ics", `"`+cur.ETag+`"`); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteObject(Calendar, "alex", "privat", "a.ics", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if _, err := s.Get(Calendar, "alex", "privat", "a.ics"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get deleted: %v", err)
	}
	// Same bytes again: nothing written, seq unchanged.
	mustPut(t, s, Calendar, "privat", "b.ics", ics("b", "B", "20261110T080000Z"))
	c1, _ := s.GetCollection(Calendar, "alex", "privat")
	r2 := mustPut(t, s, Calendar, "privat", "b.ics", ics("b", "B", "20261110T080000Z"))
	c2, _ := s.GetCollection(Calendar, "alex", "privat")
	if c1.CTag != c2.CTag || r2.Created {
		t.Fatalf("identical put changed the collection: %s → %s", c1.CTag, c2.CTag)
	}
	if _, err := s.Put(Calendar, "nobody", "privat", "b.ics", ics("b", "B", "20261110T080000Z"), PutOptions{}); !errors.Is(err, ErrNoAccount) {
		t.Fatalf("unknown account: %v", err)
	}
}

func TestUIDConflict(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("same", "A", "20261110T080000Z"))
	_, err := s.Put(Calendar, "alex", "privat", "b.ics", ics("same", "B", "20261110T080000Z"), PutOptions{})
	var conflict *UIDConflictError
	if !errors.Is(err, ErrUIDConflict) || !errors.As(err, &conflict) || conflict.Name != "a.ics" {
		t.Fatalf("uid conflict: %v", err)
	}
	// The same UID in another calendar is fine, and the same file may keep its UID.
	if _, err := s.CreateCollection(Calendar, "alex", "arbeit", Meta{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put(Calendar, "alex", "arbeit", "b.ics", ics("same", "B", "20261110T080000Z"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	mustPut(t, s, Calendar, "privat", "a.ics", ics("same", "A changed", "20261110T080000Z"))
	mustPut(t, s, Contacts, "kontakte", "c.vcf", vcf("u1", "Carla"))
	if _, err := s.Put(Contacts, "alex", "kontakte", "d.vcf", vcf("u1", "Dora"), PutOptions{}); !errors.Is(err, ErrUIDConflict) {
		t.Fatalf("vcard uid conflict: %v", err)
	}
}

func TestValidation(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	todo := []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VTODO\r\nUID:t\r\nDTSTAMP:20261101T000000Z\r\nSUMMARY:x\r\nEND:VTODO\r\nEND:VCALENDAR\r\n")
	if _, err := s.Put(Calendar, "alex", "privat", "t.ics", todo, PutOptions{}); !errors.Is(err, ErrUnsupportedComponent) {
		t.Fatalf("VTODO: %v", err)
	}
	two := []byte(strings.Replace(string(ics("a", "A", "20261110T080000Z")), "END:VCALENDAR", "BEGIN:VEVENT\r\nUID:b\r\nDTSTAMP:20261101T000000Z\r\nDTSTART:20261111T080000Z\r\nEND:VEVENT\r\nEND:VCALENDAR", 1))
	if _, err := s.Put(Calendar, "alex", "privat", "two.ics", two, PutOptions{}); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("two UIDs: %v", err)
	}
	if _, err := s.Put(Calendar, "alex", "privat", "x.ics", []byte("hello"), PutOptions{}); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("garbage: %v", err)
	}
	if _, err := s.Put(Contacts, "alex", "kontakte", "x.vcf", []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nEND:VCARD\r\n"), PutOptions{}); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("card without FN: %v", err)
	}
	if _, err := s.Put(Contacts, "alex", "kontakte", "x.ics", vcf("x", "X"), PutOptions{}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("wrong suffix: %v", err)
	}
	if len(e.tree()) != 0 {
		t.Fatalf("refused puts made %v", e.tree())
	}
}

func TestLimits(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	big := append(ics("a", "A", "20261110T080000Z"), make([]byte, MaxICS)...)
	if _, err := s.Put(Calendar, "alex", "privat", "big.ics", big, PutOptions{}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("1 MiB: %v", err)
	}
	photo := "PHOTO;ENCODING=b;TYPE=JPEG:" + strings.Repeat("A", MaxVCF)
	if _, err := s.Put(Contacts, "alex", "kontakte", "big.vcf", vcf("b", "B", photo), PutOptions{}); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("2 MiB: %v", err)
	}
	okPhoto := "PHOTO;ENCODING=b;TYPE=JPEG:" + strings.Repeat("A", 1<<20)
	if _, err := s.Put(Contacts, "alex", "kontakte", "ok.vcf", vcf("ok", "Ok", okPhoto), PutOptions{}); err != nil {
		t.Fatalf("1 MiB card: %v", err)
	}
	if info, _ := s.Stat(Contacts, "alex", "kontakte", "ok.vcf"); !info.HasPhoto {
		t.Fatal("photo not noted")
	}
	oldE, oldC := maxEntries, maxCollections
	t.Cleanup(func() { maxEntries, maxCollections = oldE, oldC })
	maxEntries, maxCollections = 2, 2
	mustPut(t, s, Calendar, "privat", "1.ics", ics("1", "1", "20261110T080000Z"))
	mustPut(t, s, Calendar, "privat", "2.ics", ics("2", "2", "20261110T080000Z"))
	if _, err := s.Put(Calendar, "alex", "privat", "3.ics", ics("3", "3", "20261110T080000Z"), PutOptions{}); !errors.Is(err, ErrCollectionFull) {
		t.Fatalf("full: %v", err)
	}
	mustPut(t, s, Calendar, "privat", "2.ics", ics("2", "2b", "20261110T080000Z")) // overwriting is fine
	if _, err := s.CreateCollection(Calendar, "alex", "b", Meta{}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCollection(Calendar, "alex", "c", Meta{}); !errors.Is(err, ErrTooManyCollections) {
		t.Fatalf("too many: %v", err)
	}
}

func TestBadNamesNeverEscape(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	bad := []string{"", ".", "..", "../x.ics", "..ics", ".ics", ".hidden.ics", "a/b.ics", "/a.ics", "a\x00.ics", "a\n.ics", "a\x7f.ics",
		"a.ICS", "a.txt", strings.Repeat("a", 197) + ".ics", "\xff.ics", strings.Repeat("ä", 64) + ".ics"}
	for _, name := range bad {
		if ValidName(Calendar, name) {
			t.Errorf("valid: %q", name)
		}
		if _, err := s.Put(Calendar, "alex", "privat", name, ics("x", "X", "20261110T080000Z"), PutOptions{}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("put %q: %v", name, err)
		}
		if err := s.DeleteObject(Calendar, "alex", "privat", name, ""); !errors.Is(err, ErrInvalidName) {
			t.Errorf("delete %q: %v", name, err)
		}
	}
	for _, col := range []string{"..", "../alex", "a/b", ".x", "", strings.Repeat("a", 65), "Privat"} {
		if _, err := s.Put(Calendar, "alex", col, "a.ics", ics("x", "X", "20261110T080000Z"), PutOptions{}); !errors.Is(err, ErrInvalidCollection) {
			t.Errorf("collection %q: %v", col, err)
		}
	}
	for _, acc := range []string{"..", "Alex", "a/b", ""} {
		if _, err := s.Collections(Calendar, acc); !errors.Is(err, ErrInvalidAccount) {
			t.Errorf("account %q: %v", acc, err)
		}
	}
	// Odd but valid names stay inside o/, encoded.
	odd := []string{"%2F.ics", "%2e%2e.ics", "a b.ics", "ä.ics", "x..ics", strings.Repeat("a", 196) + ".ics"}
	for i, name := range odd {
		mustPut(t, s, Calendar, "privat", name, ics(fmt.Sprint("u", i), "X", "20261110T080000Z"))
	}
	for _, p := range e.tree() {
		if strings.Count(p, string(filepath.Separator)) > 5 {
			t.Errorf("deep path %s", p)
		}
		parts := strings.Split(p, string(filepath.Separator))
		if parts[0] != ".titanos" || len(parts) > 2 && parts[2] != "alex" {
			t.Errorf("outside the account: %s", p)
		}
		if len(parts) == 6 && (parts[4] != "o" || !diskNameRE.MatchString(parts[5])) {
			t.Errorf("unexpected file %s", p)
		}
	}
	objs, _ := s.Objects(Calendar, "alex", "privat")
	var names []string
	for _, o := range objs {
		names = append(names, o.Name)
	}
	want := slices.Clone(odd)
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("names %q, want %q", names, want)
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".titanos/calendar/alex/privat/o/%252F.ics")); err != nil {
		t.Fatalf("encoded name: %v", err)
	}
	// Survives a restart without a rebuild.
	c1, _ := s.GetCollection(Calendar, "alex", "privat")
	c2, err := e.store().GetCollection(Calendar, "alex", "privat")
	if err != nil || c1.CTag != c2.CTag {
		t.Fatalf("encoded names rebuilt the index: %s → %s (%v)", c1.CTag, c2.CTag, err)
	}
}

func TestDeleteObjectAndCollectionApart(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Calendar, "privat", "arbeit.ics", ics("a", "A", "20261110T080000Z"))
	if _, err := s.CreateCollection(Calendar, "alex", "arbeit", Meta{Name: "Arbeit"}); err != nil {
		t.Fatal(err)
	}
	mustPut(t, s, Calendar, "arbeit", "x.ics", ics("x", "X", "20261110T080000Z"))
	// An entry name is never a collection and the other way round.
	if err := s.DeleteObject(Calendar, "alex", "privat", "arbeit", ""); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("delete object by collection name: %v", err)
	}
	if err := s.DeleteCollection(Calendar, "alex", "arbeit.ics", ""); !errors.Is(err, ErrInvalidCollection) {
		t.Fatalf("delete collection by entry name: %v", err)
	}
	if err := s.DeleteCollection(Calendar, "alex", "arbeit", "1-1"); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("collection if-match: %v", err)
	}
	col, _ := s.GetCollection(Calendar, "alex", "arbeit")
	if err := s.DeleteCollection(Calendar, "alex", "arbeit", col.CTag); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(Calendar, "alex", "privat", "arbeit.ics"); err != nil {
		t.Fatalf("entry of the same name went with the collection: %v", err)
	}
	if _, err := s.Objects(Calendar, "alex", "arbeit"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted collection: %v", err)
	}
	for _, p := range e.tree() {
		if strings.Contains(p, "arbeit") && !strings.HasSuffix(p, "arbeit.ics") || strings.Contains(p, deletedPfx) {
			t.Fatalf("left over: %s", p)
		}
	}
	if err := s.DeleteCollection(Calendar, "alex", "privat", ""); !errors.Is(err, ErrLastCollection) {
		t.Fatalf("last: %v", err)
	}
	// A cut-short removal is cleaned on the next load and not brought back.
	if err := os.MkdirAll(filepath.Join(e.dir, ".titanos/calendar/alex", deletedPfx+"alt-00/o"), 0o700); err != nil {
		t.Fatal(err)
	}
	cols, err := e.store().Collections(Calendar, "alex")
	if err != nil || len(cols) != 1 || cols[0].ID != "privat" {
		t.Fatalf("after leftover: %+v %v", cols, err)
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".titanos/calendar/alex", deletedPfx+"alt-00")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("leftover stays: %v", err)
	}
}

func TestSyncChanges(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	all, err := s.Changes(Calendar, "alex", "privat", "")
	if err != nil || len(all.Changes) != 0 || all.Token != SyncTokenPrefix+"0/0" {
		t.Fatalf("virtual: %+v %v", all, err)
	}
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	mustPut(t, s, Calendar, "privat", "b.ics", ics("b", "B", "20261110T080000Z"))
	if _, err := s.Changes(Calendar, "alex", "privat", all.Token); !errors.Is(err, ErrInvalidSyncToken) {
		t.Fatalf("token of the virtual calendar: %v", err)
	}
	first, _ := s.Changes(Calendar, "alex", "privat", "")
	if len(first.Changes) != 2 {
		t.Fatalf("full: %+v", first)
	}
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A2", "20261110T080000Z"))
	mustPut(t, s, Calendar, "privat", "c.ics", ics("c", "C", "20261110T080000Z"))
	if err := s.DeleteObject(Calendar, "alex", "privat", "c.ics", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteObject(Calendar, "alex", "privat", "b.ics", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateCollection(Calendar, "alex", "privat", MetaPatch{Color: ptr("#00ff00")}); err != nil {
		t.Fatal(err)
	}
	ch, err := s.Changes(Calendar, "alex", "privat", first.Token)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Stat(Calendar, "alex", "privat", "a.ics")
	want := []Change{{Name: "a.ics", ETag: a.ETag}, {Name: "b.ics", Deleted: true}, {Name: "c.ics", Deleted: true}}
	if !slices.Equal(ch.Changes, want) {
		t.Fatalf("changes %+v, want %+v", ch.Changes, want)
	}
	if again, err := s.Changes(Calendar, "alex", "privat", ch.Token); err != nil || len(again.Changes) != 0 || again.Token != ch.Token {
		t.Fatalf("no change: %+v %v", again, err)
	}
	for _, bad := range []string{"x", SyncTokenPrefix + "zz/1", SyncTokenPrefix + "/1", strings.Replace(ch.Token, "/7", "/99", 1), "https://ns.titanos.invalid/sync/abc"} {
		if _, err := s.Changes(Calendar, "alex", "privat", bad); !errors.Is(err, ErrInvalidSyncToken) {
			t.Fatalf("token %q: %v", bad, err)
		}
	}
	// The log keeps 2000 changes: an older token is invalid, a recent one not.
	items := make([]Item, 0, 2005)
	for i := range 2005 {
		items = append(items, Item{Name: fmt.Sprintf("i%d.ics", i), Data: ics(fmt.Sprint("i", i), "I", "20261110T080000Z")})
	}
	res, err := s.Import(context.Background(), Calendar, "alex", "privat", items)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Err != nil || !r.Created {
			t.Fatalf("import %s: %v", r.Name, r.Err)
		}
	}
	if _, err := s.Changes(Calendar, "alex", "privat", ch.Token); !errors.Is(err, ErrInvalidSyncToken) {
		t.Fatalf("token older than the log: %v", err)
	}
	st := loadState(t, e, Calendar, "privat")
	if len(st.Log) != MaxLog || st.Log[len(st.Log)-1].Seq != st.Seq || st.Log[0].Seq != st.Seq-MaxLog+1 {
		t.Fatalf("log %d entries, seq %d, first %d", len(st.Log), st.Seq, st.Log[0].Seq)
	}
	recent := fmt.Sprintf("%s%s/%d", SyncTokenPrefix, st.Epoch, st.Seq-MaxLog)
	if got, err := s.Changes(Calendar, "alex", "privat", recent); err != nil || len(got.Changes) != MaxLog {
		t.Fatalf("token at the start of the log: %d %v", len(got.Changes), err)
	}
	if _, err := s.Changes(Calendar, "alex", "privat", fmt.Sprintf("%s%s/%d", SyncTokenPrefix, st.Epoch, st.Seq-MaxLog-1)); !errors.Is(err, ErrInvalidSyncToken) {
		t.Fatalf("token just before the log: %v", err)
	}
}

func ptr[T any](v T) *T { return &v }

func loadState(t *testing.T, e *env, kind Kind, col string) State {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.dir, ".titanos", string(kind), "alex", col, stateFile))
	if err != nil {
		t.Fatal(err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

func TestImportWritesStateOnce(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Calendar, "privat", "dup.ics", ics("dup", "D", "20261110T080000Z"))
	writes := 0
	s.failState = func(string) error { writes++; return nil }
	items := []Item{
		{Name: "1.ics", Data: ics("1", "1", "20261110T080000Z")},
		{Name: "2.ics", Data: ics("2", "2", "20261110T080000Z")},
		{Name: "bad", Data: ics("3", "3", "20261110T080000Z")},
		{Name: "4.ics", Data: ics("dup", "4", "20261110T080000Z")},
		{Name: "5.ics", Data: []byte("x")},
		{Name: "6.ics", Data: ics("1", "again", "20261110T080000Z")},
	}
	before, _ := s.GetCollection(Calendar, "alex", "privat")
	res, err := s.Import(context.Background(), Calendar, "alex", "privat", items)
	if err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("state.json written %d times", writes)
	}
	wantErr := []error{nil, nil, ErrInvalidName, ErrUIDConflict, ErrInvalidData, ErrUIDConflict}
	for i, r := range res {
		if !errors.Is(r.Err, wantErr[i]) || (wantErr[i] == nil) != (r.ETag != "") {
			t.Fatalf("item %d: %+v", i, r)
		}
	}
	after, _ := s.GetCollection(Calendar, "alex", "privat")
	if after.Count != 3 || after.CTag == before.CTag {
		t.Fatalf("after import: %+v", after)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, _ = s.Import(ctx, Calendar, "alex", "privat", []Item{{Name: "7.ics", Data: ics("7", "7", "20261110T080000Z")}})
	if !errors.Is(res[0].Err, context.Canceled) {
		t.Fatalf("cancelled import: %+v", res)
	}
}

func TestRecreatedCollectionNewEpoch(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	if _, err := s.CreateCollection(Calendar, "alex", "arbeit", Meta{Name: "Arbeit"}); err != nil {
		t.Fatal(err)
	}
	mustPut(t, s, Calendar, "arbeit", "x.ics", ics("x", "X", "20261110T080000Z"))
	old, _ := s.Changes(Calendar, "alex", "arbeit", "")
	if err := s.DeleteCollection(Calendar, "alex", "arbeit", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateCollection(Calendar, "alex", "arbeit", Meta{Name: "Arbeit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Changes(Calendar, "alex", "arbeit", old.Token); !errors.Is(err, ErrInvalidSyncToken) {
		t.Fatalf("token of the old collection: %v", err)
	}
	if objs, _ := s.Objects(Calendar, "alex", "arbeit"); len(objs) != 0 {
		t.Fatalf("recreated collection has %v", objs)
	}
}

func TestMetadataUpdate(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	c, err := s.UpdateCollection(Calendar, "alex", "privat", MetaPatch{Name: ptr("Familie"), Color: ptr("#FF0000FF"), Order: ptr(3)})
	if err != nil || c.Virtual || c.Name != "Familie" || c.Color != "#FF0000FF" || c.Order != 3 || !strings.HasSuffix(c.CTag, "-1") {
		t.Fatalf("update of the virtual calendar: %+v %v", c, err)
	}
	c2, err := s.UpdateCollection(Calendar, "alex", "privat", MetaPatch{Description: ptr("Zuhause")})
	if err != nil || !strings.HasSuffix(c2.CTag, "-2") || c2.Name != "Familie" || c2.Description != "Zuhause" {
		t.Fatalf("second update: %+v %v", c2, err)
	}
	if _, err := s.UpdateCollection(Calendar, "alex", "privat", MetaPatch{Name: ptr("a\nb")}); !errors.Is(err, ErrInvalidMeta) {
		t.Fatalf("bad name: %v", err)
	}
	c3, err := e.store().GetCollection(Calendar, "alex", "privat")
	if err != nil || c3.CTag != c2.CTag || c3.Description != "Zuhause" {
		t.Fatalf("after restart: %+v %v", c3, err)
	}
}

// titand stopped between a file and its state.json: the next start
// rebuilds the index with a new epoch; nothing is lost or doubled.
func TestCrashBetweenEntryAndState(t *testing.T) {
	for _, step := range []string{"new", "overwrite", "delete"} {
		t.Run(step, func(t *testing.T) {
			e := newEnv(t)
			s := e.store()
			mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
			mustPut(t, s, Calendar, "privat", "b.ics", ics("b", "B", "20261110T080000Z"))
			before, _ := s.Changes(Calendar, "alex", "privat", "")
			s.failState = func(string) error { return errors.New("power gone") }
			var err error
			want := []string{"a.ics", "b.ics"}
			switch step {
			case "new":
				_, err = s.Put(Calendar, "alex", "privat", "c.ics", ics("c", "C", "20261110T080000Z"), PutOptions{})
				want = append(want, "c.ics")
			case "overwrite":
				time.Sleep(10 * time.Millisecond)
				_, err = s.Put(Calendar, "alex", "privat", "a.ics", ics("a", "A2", "20261110T080000Z"), PutOptions{})
			case "delete":
				err = s.DeleteObject(Calendar, "alex", "privat", "b.ics", "")
				want = want[:1]
			}
			if err == nil {
				t.Fatal("state.json failure not reported")
			}
			// A pending file of an interrupted write too.
			if err := os.WriteFile(filepath.Join(e.dir, ".titanos/calendar/alex/privat/o", pendingPfx+"abc"), []byte("half"), 0o600); err != nil {
				t.Fatal(err)
			}
			s2 := e.store()
			after, err := s2.Changes(Calendar, "alex", "privat", "")
			if err != nil {
				t.Fatal(err)
			}
			var names []string
			for _, c := range after.Changes {
				names = append(names, c.Name)
			}
			if !slices.Equal(names, want) {
				t.Fatalf("after recovery %v, want %v", names, want)
			}
			if after.Token == before.Token || strings.Split(after.Token, "/")[4] == strings.Split(before.Token, "/")[4] {
				t.Fatalf("no new epoch: %s → %s", before.Token, after.Token)
			}
			if _, err := s2.Changes(Calendar, "alex", "privat", before.Token); !errors.Is(err, ErrInvalidSyncToken) {
				t.Fatalf("old token after recovery: %v", err)
			}
			if step == "overwrite" {
				obj, _ := s2.Get(Calendar, "alex", "privat", "a.ics")
				if obj.ETag != ETag(ics("a", "A2", "20261110T080000Z")) {
					t.Fatal("index not rebuilt from the file")
				}
			}
			if _, err := os.Stat(filepath.Join(e.dir, ".titanos/calendar/alex/privat/o", pendingPfx+"abc")); !errors.Is(err, fs.ErrNotExist) {
				t.Fatal("pending file stays")
			}
			// Written back: a third start keeps the epoch.
			if third, _ := e.store().Changes(Calendar, "alex", "privat", ""); third.Token != after.Token {
				t.Fatalf("rebuilt state not kept: %s → %s", after.Token, third.Token)
			}
		})
	}
}

func TestConcurrentPuts(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	const n = 24
	var wg sync.WaitGroup
	errs := make(chan error, n*2)
	for i := range n {
		wg.Go(func() {
			_, err := s.Put(Calendar, "alex", "privat", fmt.Sprintf("e%d.ics", i), ics(fmt.Sprint("e", i), "E", "20261110T080000Z"), PutOptions{IfNoneMatch: true})
			errs <- err
			// The same name from two devices: one wins, the other gets 412.
			_, err = s.Put(Calendar, "alex", "privat", "same.ics", ics("same", fmt.Sprint(i), "20261110T080000Z"), PutOptions{IfNoneMatch: true})
			if err != nil && !errors.Is(err, ErrPrecondition) {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	col, _ := s.GetCollection(Calendar, "alex", "privat")
	if col.Count != n+1 || !strings.HasSuffix(col.CTag, fmt.Sprintf("-%d", n+1)) {
		t.Fatalf("after concurrent puts: %+v", col)
	}
	again, err := e.store().GetCollection(Calendar, "alex", "privat")
	if err != nil || again.CTag != col.CTag {
		t.Fatalf("state.json differs from RAM: %s → %s (%v)", col.CTag, again.CTag, err)
	}
}

func TestNewAccountInheritsNothing(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	mustPut(t, s, Contacts, "kontakte", "c.vcf", vcf("c", "Carla"))
	if err := s.SaveSeen("alex", map[string]string{"iphone": "x"}); err != nil {
		t.Fatal(err)
	}
	// Deleted while the volume was away, made again with the same name.
	e.setCreated("alex", t2)
	for _, st := range []*Store{s, e.store()} {
		cols, err := st.Collections(Calendar, "alex")
		if err != nil || len(cols) != 1 || !cols[0].Virtual {
			t.Fatalf("new account sees %+v %v", cols, err)
		}
		if objs, _ := st.Objects(Contacts, "alex", "kontakte"); len(objs) != 0 {
			t.Fatalf("new account sees contacts %v", objs)
		}
		if _, err := st.Get(Calendar, "alex", "privat", "a.ics"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("old entry readable: %v", err)
		}
		var seen map[string]string
		if err := st.LoadSeen("alex", &seen); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("old seen times readable: %v %v", seen, err)
		}
		if ev, co, err := st.Counts("alex"); err != nil || ev != 0 || co != 0 {
			t.Fatalf("counts %d %d %v", ev, co, err)
		}
	}
	// The first write removes the old folder.
	s = e.store()
	mustPut(t, s, Calendar, "privat", "n.ics", ics("n", "N", "20261110T080000Z"))
	if _, err := os.Stat(filepath.Join(e.dir, ".titanos/calendar/alex/privat/o/a.ics")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("old entry stays: %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".titanos/calendar/alex", seenFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("old seen file stays: %v", err)
	}
	if objs, _ := e.store().Objects(Calendar, "alex", "privat"); len(objs) != 1 || objs[0].Name != "n.ics" {
		t.Fatalf("after first write: %+v", objs)
	}
	// A folder without owner.json is no one's.
	if err := os.MkdirAll(filepath.Join(e.dir, ".titanos/contacts/bea/kontakte/o"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.dir, ".titanos/contacts/bea/kontakte/o/x.vcf"), vcf("x", "X"), 0o600); err != nil {
		t.Fatal(err)
	}
	if objs, _ := e.store().Objects(Contacts, "bea", "kontakte"); len(objs) != 0 {
		t.Fatalf("folder without owner read: %v", objs)
	}
}

func TestForgetUser(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	mustPut(t, s, Contacts, "kontakte", "c.vcf", vcf("c", "Carla"))
	if _, err := s.Put(Calendar, "bea", "privat", "b.ics", ics("b", "B", "20261110T080000Z"), PutOptions{}); err != nil {
		t.Fatal(err)
	}
	ctx := s.Context("alex")
	if err := s.ForgetUser("alex"); err != nil {
		t.Fatal(err)
	}
	if ctx.Err() == nil {
		t.Fatal("context of the account not cancelled")
	}
	if s.Context("alex").Err() != nil {
		t.Fatal("new context cancelled")
	}
	for _, kind := range []string{"calendar", "contacts"} {
		if _, err := os.Stat(filepath.Join(e.dir, ".titanos", kind, "alex")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s folder stays: %v", kind, err)
		}
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".titanos/calendar/bea/privat/o/b.ics")); err != nil {
		t.Fatalf("other account touched: %v", err)
	}
	cols, err := s.Collections(Calendar, "alex")
	if err != nil || len(cols) != 1 || !cols[0].Virtual {
		t.Fatalf("RAM kept: %+v %v", cols, err)
	}
	if err := s.ForgetUser("../x"); !errors.Is(err, ErrInvalidAccount) {
		t.Fatalf("bad name: %v", err)
	}
	// An account that never used the store: nothing made.
	e2 := newEnv(t)
	s2 := e2.store()
	if err := s2.ForgetUser("alex"); err != nil || len(e2.tree()) != 0 || e2.creates.Load() != 0 {
		t.Fatalf("forget made %v", e2.tree())
	}
}

func TestForgetUserWithoutVolume(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	e.present.Store(false)
	if err := s.ForgetUser("alex"); err != nil {
		t.Fatal(err)
	}
	e.present.Store(true)
	if _, err := os.Stat(filepath.Join(e.dir, ".titanos/calendar/alex/privat/o/a.ics")); err != nil {
		t.Fatalf("folder went without a volume: %v", err)
	}
	// The account was deleted; a new one of the name sees nothing.
	e.setCreated("alex", t2)
	if objs, err := s.Objects(Calendar, "alex", "privat"); err != nil || len(objs) != 0 {
		t.Fatalf("new account inherits %v %v", objs, err)
	}
}

func TestContactsIndexAndGroups(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Contacts, "kontakte", "c.vcf", vcf("c", "Carla Muster", "N:Muster;Carla;;;", "ORG:Firma;Abteilung",
		"EMAIL;TYPE=INTERNET:carla@example.org", "TEL;TYPE=CELL:+49 (170) 123-45", "BDAY;X-APPLE-OMIT-YEAR=1604:1604-03-01",
		"ADR;TYPE=HOME:;;Lindenallee 7;Köln;;50667;Deutschland"))
	group := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:g\r\nFN:Familie\r\nN:Familie;;;;\r\nX-ADDRESSBOOKSERVER-KIND:group\r\nX-ADDRESSBOOKSERVER-MEMBER:urn:uuid:c\r\nEND:VCARD\r\n")
	mustPut(t, s, Contacts, "kontakte", "g.vcf", group)
	objs, err := s.Objects(Contacts, "alex", "kontakte")
	if err != nil || len(objs) != 2 {
		t.Fatalf("%+v %v", objs, err)
	}
	c, g := objs[0], objs[1]
	if c.FN != "Carla Muster" || c.Org != "Firma" || c.SortKey != "muster carla" || !slices.Equal(c.Emails, []string{"carla@example.org"}) ||
		!slices.Equal(c.Phones, []string{"+4917012345"}) || c.BDay != "1604-03-01" || c.Group ||
		!slices.Equal(c.Addresses, []string{"Lindenallee 7, 50667 Köln, Deutschland"}) {
		t.Fatalf("contact index: %+v", c)
	}
	if !g.Group {
		t.Fatalf("group not flagged: %+v", g)
	}
	obj, err := s.Get(Contacts, "alex", "kontakte", "g.vcf")
	if err != nil || string(obj.Data) != string(group) {
		t.Fatalf("group not returned verbatim: %v", err)
	}
	if ev, co, err := s.Counts("alex"); err != nil || ev != 0 || co != 1 {
		t.Fatalf("counts %d %d %v", ev, co, err)
	}
	// Photos: a cache next to the book, gone with a new version.
	cinfo, _ := s.Stat(Contacts, "alex", "kontakte", "c.vcf")
	if err := s.SavePhoto("alex", "c.vcf", "0123", []byte("jpeg")); !errors.Is(err, ErrPrecondition) {
		t.Fatalf("photo of another version: %v", err)
	}
	if err := s.SavePhoto("alex", "c.vcf", cinfo.ETag, []byte("jpeg")); err != nil {
		t.Fatal(err)
	}
	if p, err := s.Photo("alex", "c.vcf"); err != nil || string(p) != "jpeg" {
		t.Fatalf("photo: %q %v", p, err)
	}
	mustPut(t, s, Contacts, "kontakte", "c.vcf", vcf("c", "Carla M"))
	if _, err := s.Photo("alex", "c.vcf"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale photo: %v", err)
	}
	if err := s.SavePhoto("alex", "none.vcf", "", []byte("x")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("photo of nothing: %v", err)
	}
}

func TestQueryAndOverlapping(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Calendar, "privat", "single.ics", ics("s", "Einmal", "20261110T080000Z"))
	mustPut(t, s, Calendar, "privat", "weekly.ics", ics("w", "Woche", "20261102T090000Z", "RRULE:FREQ=WEEKLY"))
	mustPut(t, s, Calendar, "privat", "day.ics", []byte("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//t//EN\r\nBEGIN:VEVENT\r\nUID:d\r\nDTSTAMP:20261101T000000Z\r\nDTSTART;VALUE=DATE:20261112\r\nSUMMARY:Ganztag\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"))
	mustPut(t, s, Calendar, "privat", "old.ics", ics("o", "Alt", "20250101T080000Z"))
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 30)
	occ, err := s.Query("alex", "privat", from, to)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, o := range occ {
		counts[o.Name]++
	}
	if counts["single.ics"] != 1 || counts["weekly.ics"] != 5 || counts["day.ics"] != 1 || counts["old.ics"] != 0 {
		t.Fatalf("occurrences %v", counts)
	}
	for i := 1; i < len(occ); i++ {
		if occ[i].Start.Before(occ[i-1].Start) {
			t.Fatal("not sorted")
		}
	}
	for _, o := range occ {
		if o.Name == "day.ics" && (!o.AllDay || o.ID != "20261112" || !o.End.Equal(o.Start.AddDate(0, 0, 1))) {
			t.Fatalf("all-day: %+v", o)
		}
		if o.Name == "single.ics" && (o.ID != "20261110T080000Z" || o.Summary != "Einmal") {
			t.Fatalf("single: %+v", o)
		}
	}
	// Cached by ETag: asking again reads nothing.
	before := e.opens.Load()
	if _, err := s.Query("alex", "privat", from, to); err != nil || e.opens.Load() != before {
		t.Fatalf("second query opened the volume: %v", err)
	}
	if _, err := s.Query("alex", "privat", from, from.AddDate(2, 0, 0)); !errors.Is(err, recur.ErrWindow) {
		t.Fatalf("window: %v", err)
	}
	names, err := s.Overlapping("alex", "privat", time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil || !slices.Equal(names, []string{"weekly.ics"}) {
		t.Fatalf("open end: %v %v", names, err)
	}
	names, _ = s.Overlapping("alex", "privat", time.Time{}, time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	if !slices.Equal(names, []string{"old.ics"}) {
		t.Fatalf("open start: %v", names)
	}
	// A new version is read again.
	mustPut(t, s, Calendar, "privat", "weekly.ics", ics("w", "Woche", "20261102T090000Z", "RRULE:FREQ=WEEKLY;COUNT=2"))
	occ, _ = s.Query("alex", "privat", from, to)
	counts = map[string]int{}
	for _, o := range occ {
		counts[o.Name]++
	}
	if counts["weekly.ics"] != 2 {
		t.Fatalf("after change %v", counts)
	}
}

func TestSeen(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	if err := s.SaveSeen("alex", map[string]string{"iphone": "2026-11-17T10:00:00Z"}); err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := e.store().LoadSeen("alex", &got); err != nil || got["iphone"] == "" {
		t.Fatalf("%v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".titanos/calendar/alex", seenFile)); err != nil {
		t.Fatal(err)
	}
}

// Paged sync (RFC 6578 3.6/3.7): a client that applies each page and goes
// on with its token ends with exactly the server's entries and ETags, also
// when entries change between the pages (initial listing and later
// syncs, every limit).
func TestSyncChangesPaged(t *testing.T) {
	for _, limit := range []int{1, 2, 3, 7} {
		e := newEnv(t)
		s := e.store()
		n := 0
		edit := func(i int) {
			name := fmt.Sprintf("e%02d.ics", i%23)
			n++
			if i%5 == 4 {
				_ = s.DeleteObject(Calendar, "alex", "privat", name, "")
				return
			}
			mustPut(t, s, Calendar, "privat", name, ics("u"+name, fmt.Sprint("S", n), "20261110T080000Z"))
		}
		for i := range 30 {
			edit(i)
		}
		client := map[string]string{}
		apply := func(cs ChangeSet) {
			for _, c := range cs.Changes {
				if c.Deleted {
					delete(client, c.Name)
				} else {
					client[c.Name] = c.ETag
				}
			}
		}
		token, round := "", 0
		for pass := range 4 {
			for {
				cs, err := s.ChangesLimit(Calendar, "alex", "privat", token, limit)
				if err != nil {
					t.Fatalf("limit %d pass %d: %v", limit, pass, err)
				}
				if len(cs.Changes) > limit {
					t.Fatalf("limit %d: %d changes", limit, len(cs.Changes))
				}
				apply(cs)
				token = cs.Token
				round++
				if round%4 != 0 {
					edit(round * 3) // a change between the pages (not always: the queue must drain)
				}
				if !cs.Truncated {
					break
				}
			}
			// Settle: sync without changes in between.
			for {
				cs, err := s.ChangesLimit(Calendar, "alex", "privat", token, limit)
				if err != nil {
					t.Fatal(err)
				}
				apply(cs)
				token = cs.Token
				if !cs.Truncated {
					break
				}
			}
			objs, _ := s.Objects(Calendar, "alex", "privat")
			want := map[string]string{}
			for _, o := range objs {
				want[o.Name] = o.ETag
			}
			if !maps.Equal(client, want) {
				t.Fatalf("limit %d pass %d: client %v, server %v", limit, pass, client, want)
			}
		}
	}
	// A cut initial listing: the first page, a 507-able cut, the token
	// goes on by name; a bad cursor is an invalid token.
	e := newEnv(t)
	s := e.store()
	for _, n := range []string{"a.ics", "b.ics", "c.ics"} {
		mustPut(t, s, Calendar, "privat", n, ics("u"+n, "X", "20261110T080000Z"))
	}
	first, err := s.ChangesLimit(Calendar, "alex", "privat", "", 1)
	if err != nil || !first.Truncated || len(first.Changes) != 1 || first.Changes[0].Name != "a.ics" {
		t.Fatalf("first page: %+v %v", first, err)
	}
	rest, err := s.ChangesLimit(Calendar, "alex", "privat", first.Token, 0)
	if err != nil || rest.Truncated || len(rest.Changes) != 2 || rest.Token != loadToken(t, s) {
		t.Fatalf("rest: %+v %v", rest, err)
	}
	// A name already listed changes before the next page: it comes again.
	mustPut(t, s, Calendar, "privat", "a.ics", ics("ua.ics", "Y", "20261110T080000Z"))
	second, err := s.ChangesLimit(Calendar, "alex", "privat", first.Token, 1)
	if err != nil || !second.Truncated || len(second.Changes) != 1 || second.Changes[0].Name != "b.ics" {
		t.Fatalf("second page: %+v %v", second, err)
	}
	var seen []string
	for tok := second.Token; ; {
		cs, err := s.ChangesLimit(Calendar, "alex", "privat", tok, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cs.Changes {
			seen = append(seen, c.Name)
		}
		tok = cs.Token
		if !cs.Truncated {
			break
		}
	}
	if !slices.Equal(seen, []string{"c.ics", "a.ics"}) {
		t.Fatalf("after the second page: %v", seen)
	}
	for _, bad := range []string{first.Token + "zz", first.Token + "/x", strings.TrimSuffix(first.Token, "/"+strings.Split(first.Token, "/")[len(strings.Split(first.Token, "/"))-1]) + "/"} {
		if _, err := s.ChangesLimit(Calendar, "alex", "privat", bad, 1); !errors.Is(err, ErrInvalidSyncToken) {
			t.Fatalf("token %q: %v", bad, err)
		}
	}
}

func loadToken(t *testing.T, s *Store) string {
	t.Helper()
	c, err := s.GetCollection(Calendar, "alex", "privat")
	if err != nil {
		t.Fatal(err)
	}
	return c.SyncToken
}

// Import refuses to overwrite an entry with another UID, as Put does
// (BAUSTAND, step 8 open point); a card without UID may get one.
func TestImportKeepsUID(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	first := mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	res, err := s.Import(context.Background(), Calendar, "alex", "privat", []Item{
		{Name: "a.ics", Data: ics("b", "B", "20261110T080000Z")},
		{Name: "c.ics", Data: ics("c", "C", "20261110T080000Z")},
	})
	var uc *UIDConflictError
	if err != nil || !errors.As(res[0].Err, &uc) || uc.Name != "a.ics" || res[1].Err != nil {
		t.Fatalf("import: %v %+v", err, res)
	}
	if info, _ := s.Stat(Calendar, "alex", "privat", "a.ics"); info.ETag != first.ETag {
		t.Fatalf("overwritten: %+v", info)
	}
	mustPut(t, s, Contacts, "kontakte", "e.vcf", []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Erika\r\nN:Erika;;;;\r\nEND:VCARD\r\n"))
	res, err = s.Import(context.Background(), Contacts, "alex", "kontakte", []Item{{Name: "e.vcf", Data: vcf("e", "Erika")}})
	if err != nil || res[0].Err != nil {
		t.Fatalf("card gets a uid: %v %+v", err, res)
	}
}

// The index keeps what the lists show without reading the file: location,
// zone, alarms; Query hands them out, with the rule of a series.
func TestQueryDetails(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	alarm := []string{"LOCATION:Praxis\\, Raum 2", "BEGIN:VALARM", "ACTION:DISPLAY", "TRIGGER:-PT30M", "END:VALARM",
		"BEGIN:VALARM", "ACTION:AUDIO", "TRIGGER;VALUE=DATE-TIME:20261110T070000Z", "END:VALARM"}
	single := []byte(strings.Replace(string(ics("s", "Einmal", "20261110T080000Z", alarm...)), "DTSTART:20261110T080000Z", "DTSTART;TZID=W. Europe Standard Time:20261110T090000", 1))
	mustPut(t, s, Calendar, "privat", "s.ics", single)
	mustPut(t, s, Calendar, "privat", "w.ics", ics("w", "Woche", "20261102T090000Z", append([]string{"RRULE:FREQ=WEEKLY;COUNT=3"}, alarm...)...))
	before := e.opens.Load()
	occ, err := s.Query("alex", "privat", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range occ {
		if o.Location != "Praxis, Raum 2" || !slices.Equal(o.Alarms, []int{30}) {
			t.Fatalf("%s: %+v", o.Name, o)
		}
		switch o.Name {
		case "s.ics":
			if o.TZID != "Europe/Berlin" || o.Rule != nil || !o.Start.Equal(time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)) {
				t.Fatalf("single: %+v", o)
			}
		case "w.ics":
			if o.TZID != "UTC" || o.Rule == nil || o.Rule.Count != 3 || o.RuleZone != time.UTC {
				t.Fatalf("series: %+v", o)
			}
		}
	}
	if len(occ) != 4 {
		t.Fatalf("occurrences: %d", len(occ))
	}
	if opened := e.opens.Load() - before; opened != 1 {
		t.Fatalf("the single event read the volume: %d opens", opened)
	}
}
