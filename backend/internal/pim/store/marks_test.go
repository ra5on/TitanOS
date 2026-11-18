package store

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// volumes are several volumes in temp folders, one of them current, with
// pim.json in a map in place of the stick.
type volumes struct {
	t       *testing.T
	base    string
	current atomic.Value // string
	mu      sync.Mutex
	mounted []string
	marks   map[string][]byte
	readErr error
	created map[string]time.Time
	reads   atomic.Int32
	writes  atomic.Int32
	peeks   atomic.Int32
	opens   atomic.Int32
	failW   atomic.Bool
}

func newVolumes(t *testing.T, names ...string) *volumes {
	v := &volumes{t: t, base: t.TempDir(), mounted: names, marks: map[string][]byte{}, created: map[string]time.Time{"alex": t1, "bea": t1}}
	v.current.Store(names[0])
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(v.base, n), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

func (v *volumes) dir(volume, rel string) string {
	return filepath.Join(v.base, volume, ".titanos", rel)
}

func (v *volumes) store() *Store {
	s := New()
	s.Local = func() *time.Location { return time.UTC }
	s.Current = func(string) string { return v.current.Load().(string) }
	s.Volume = func(feature string, create bool) (*os.Root, func(), error) {
		v.opens.Add(1)
		dir := v.dir(v.current.Load().(string), feature)
		if create {
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
	s.Account = func(name string) (time.Time, bool) {
		v.mu.Lock()
		defer v.mu.Unlock()
		c, ok := v.created[name]
		return c, ok
	}
	s.ReadMark = func(account string) ([]byte, error) {
		v.reads.Add(1)
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.readErr != nil {
			return nil, v.readErr
		}
		data, ok := v.marks[account]
		if !ok {
			return nil, fs.ErrNotExist
		}
		return data, nil
	}
	s.WriteMark = func(account string, data []byte) error {
		v.writes.Add(1)
		if v.failW.Load() {
			return errors.New("stick away")
		}
		v.mu.Lock()
		defer v.mu.Unlock()
		v.marks[account] = data
		return nil
	}
	s.Mounted = func() []string {
		v.mu.Lock()
		defer v.mu.Unlock()
		return slices.Clone(v.mounted)
	}
	s.Peek = func(volume, feature string) (*os.Root, error) {
		v.peeks.Add(1)
		return os.OpenRoot(v.dir(volume, feature))
	}
	return s
}

func (v *volumes) mark(account string) mark {
	v.mu.Lock()
	defer v.mu.Unlock()
	var m mark
	if data, ok := v.marks[account]; ok {
		if err := json.Unmarshal(data, &m); err != nil {
			v.t.Fatal(err)
		}
	}
	return m
}

func wantElsewhere(t *testing.T, what string, err error, volume string) {
	t.Helper()
	var away *ElsewhereError
	if !errors.Is(err, ErrElsewhere) || !errors.Is(err, ErrUnavailable) || !errors.As(err, &away) || away.Volume != volume {
		t.Fatalf("%s: %v, want ErrElsewhere on %q", what, err, volume)
	}
}

func TestMarkWrittenOnceAndBoundToTheAccount(t *testing.T) {
	v := newVolumes(t, "a")
	s := v.store()
	// Reading marks nothing.
	if _, err := s.Collections(Calendar, "alex"); err != nil {
		t.Fatal(err)
	}
	if v.writes.Load() != 0 {
		t.Fatal("a read wrote the mark")
	}
	// Device times are no data of their own.
	if err := s.SaveSeen("alex", map[string]string{}); err != nil {
		t.Fatal(err)
	}
	if v.writes.Load() != 0 {
		t.Fatal("SaveSeen wrote the mark")
	}
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	mustPut(t, s, Calendar, "privat", "b.ics", ics("b", "B", "20261110T080000Z"))
	if _, err := s.CreateCollection(Calendar, "alex", "arbeit", Meta{Name: "Arbeit"}); err != nil {
		t.Fatal(err)
	}
	m := v.mark("alex")
	if v.writes.Load() != 1 || m.Calendar.IsZero() || !m.Contacts.IsZero() || m.Owner.Name != "alex" || !m.Owner.Created.Equal(t1) {
		t.Fatalf("after three writes: %d writes, %+v", v.writes.Load(), m)
	}
	mustPut(t, s, Contacts, "kontakte", "c.vcf", vcf("c", "Carla"))
	if m := v.mark("alex"); v.writes.Load() != 2 || m.Calendar.IsZero() || m.Contacts.IsZero() {
		t.Fatalf("contacts: %d writes, %+v", v.writes.Load(), m)
	}
	// Read once into RAM, never again per request.
	reads := v.reads.Load()
	for range 5 {
		mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A2", "20261110T080000Z"))
		if _, err := s.Collections(Calendar, "alex"); err != nil {
			t.Fatal(err)
		}
	}
	if v.reads.Load() != reads || v.writes.Load() != 2 {
		t.Fatalf("mark read %d / written %d times more", v.reads.Load()-reads, v.writes.Load()-2)
	}
}

func TestElsewhereInsteadOfEmpty(t *testing.T) {
	v := newVolumes(t, "a", "b")
	s := v.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	// The default volume changed without a move: b has nothing (not even
	// .titanos/calendar).
	v.current.Store("b")
	_, err := s.Collections(Calendar, "alex")
	wantElsewhere(t, "collections", err, "a")
	_, err = s.Objects(Calendar, "alex", "privat")
	wantElsewhere(t, "objects", err, "a")
	_, err = s.Changes(Calendar, "alex", "privat", "")
	wantElsewhere(t, "changes", err, "a")
	// Nothing is made on b by writes either.
	_, err = s.Put(Calendar, "alex", "privat", "n.ics", ics("n", "N", "20261110T080000Z"), PutOptions{})
	wantElsewhere(t, "put", err, "a")
	_, err = s.CreateCollection(Calendar, "alex", "neu", Meta{})
	wantElsewhere(t, "create", err, "a")
	wantElsewhere(t, "save seen", s.SaveSeen("alex", map[string]string{}), "a")
	_, err = s.Import(context.Background(), Calendar, "alex", "privat", []Item{{Name: "i.ics", Data: ics("i", "I", "20261110T080000Z")}})
	wantElsewhere(t, "import", err, "a")
	if _, err := os.Stat(v.dir("b", "calendar/alex")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("made on b: %v", err)
	}
	// Contacts never kept data: the virtual book as before; another
	// account without a mark sees its virtual calendar.
	if cols, err := s.Collections(Contacts, "alex"); err != nil || len(cols) != 1 || !cols[0].Virtual {
		t.Fatalf("contacts: %v %v", cols, err)
	}
	if cols, err := s.Collections(Calendar, "bea"); err != nil || len(cols) != 1 || !cols[0].Virtual {
		t.Fatalf("bea: %v %v", cols, err)
	}

	// The other volumes are looked at once per mount set, and the volume
	// is not opened again for each request.
	peeks, opens := v.peeks.Load(), v.opens.Load()
	for range 5 {
		_, err := s.Collections(Calendar, "alex")
		wantElsewhere(t, "again", err, "a")
	}
	if v.peeks.Load() != peeks || v.opens.Load() != opens {
		t.Fatalf("repeated: %d peeks, %d opens more", v.peeks.Load()-peeks, v.opens.Load()-opens)
	}
	// Volume a unplugged: looked at again, now on no mounted volume.
	v.mu.Lock()
	v.mounted = []string{"b"}
	v.mu.Unlock()
	_, err = s.Collections(Calendar, "alex")
	wantElsewhere(t, "a unplugged", err, "")

	// The folder of alex on b with owner.json but without a collection
	// (only device times, say) is no data either.
	if err := os.MkdirAll(v.dir("b", "calendar/alex"), 0o700); err != nil {
		t.Fatal(err)
	}
	mine, _ := json.Marshal(owner{Name: "alex", Created: t1})
	if err := os.WriteFile(v.dir("b", "calendar/alex/owner.json"), mine, 0o600); err != nil {
		t.Fatal(err)
	}
	s.Moved("calendar")
	_, err = s.Collections(Calendar, "alex")
	wantElsewhere(t, "owner.json only", err, "")
	// A folder of an earlier account of the name on b: not alex's data.
	old, _ := json.Marshal(owner{Name: "alex", Created: t2})
	if err := os.WriteFile(v.dir("b", "calendar/alex/owner.json"), old, 0o600); err != nil {
		t.Fatal(err)
	}
	s.Moved("calendar")
	_, err = s.Collections(Calendar, "alex")
	wantElsewhere(t, "foreign folder", err, "")

	// The move comes through: the data is served from b.
	if err := os.RemoveAll(v.dir("b", "calendar/alex")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(v.dir("a", "calendar/alex"), v.dir("b", "calendar/alex")); err != nil {
		t.Fatal(err)
	}
	s.Moved("calendar")
	if objs, err := s.Objects(Calendar, "alex", "privat"); err != nil || len(objs) != 1 {
		t.Fatalf("after the move: %v %v", objs, err)
	}
}

func TestMarkOfAnEarlierAccountIsIgnored(t *testing.T) {
	v := newVolumes(t, "a", "b")
	s := v.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	// alex deleted while pim.json stayed (another node), made again.
	v.mu.Lock()
	v.created["alex"] = t2
	v.mu.Unlock()
	v.current.Store("b")
	if cols, err := s.Collections(Calendar, "alex"); err != nil || len(cols) != 1 || !cols[0].Virtual {
		t.Fatalf("new alex: %v %v", cols, err)
	}
	// Its own first write marks it anew, for it alone.
	mustPut(t, s, Calendar, "privat", "n.ics", ics("n", "N", "20261110T080000Z"))
	if m := v.mark("alex"); !m.Owner.Created.Equal(t2) || m.Calendar.IsZero() {
		t.Fatalf("mark: %+v", m)
	}
	// And on a, the old folder is not the new alex's.
	v.current.Store("a")
	if objs, err := s.Objects(Calendar, "alex", "privat"); err == nil {
		t.Fatalf("new alex sees on a: %v", objs)
	}
}

func TestMarkNotReadableIsNeverEmpty(t *testing.T) {
	v := newVolumes(t, "a")
	v.mu.Lock()
	v.readErr = errors.New("stick away")
	v.mu.Unlock()
	s := v.store()
	_, err := s.Collections(Calendar, "alex")
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrElsewhere) {
		t.Fatalf("unreadable mark: %v", err)
	}
	v.mu.Lock()
	v.readErr = nil
	v.mu.Unlock()
	if cols, err := s.Collections(Calendar, "alex"); err != nil || !cols[0].Virtual {
		t.Fatalf("readable again: %v %v", cols, err)
	}
}

func TestMarkWriteFailureKeepsTheData(t *testing.T) {
	v := newVolumes(t, "a", "b")
	s := v.store()
	v.failW.Store(true)
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	if v.mark("alex").has(Calendar) {
		t.Fatal("marked although the write failed")
	}
	v.failW.Store(false)
	failed := v.writes.Load()
	mustPut(t, s, Calendar, "privat", "b.ics", ics("b", "B", "20261110T080000Z"))
	if !v.mark("alex").has(Calendar) || v.writes.Load() != failed+1 {
		t.Fatalf("not tried again: %d writes", v.writes.Load()-failed)
	}
}

func TestForgetUserDropsTheMark(t *testing.T) {
	v := newVolumes(t, "a")
	s := v.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	if err := s.ForgetUser("alex"); err != nil {
		t.Fatal(err)
	}
	// The file goes with users/alex (auth.Users.Delete).
	v.mu.Lock()
	delete(v.marks, "alex")
	v.mu.Unlock()
	reads := v.reads.Load()
	if cols, err := s.Collections(Calendar, "alex"); err != nil || !cols[0].Virtual {
		t.Fatalf("after ForgetUser: %v %v", cols, err)
	}
	if v.reads.Load() == reads {
		t.Fatal("the mark in RAM outlived ForgetUser")
	}
}

func TestAbandonStartsAnew(t *testing.T) {
	v := newVolumes(t, "a", "b")
	s := v.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "A", "20261110T080000Z"))
	mustPut(t, s, Contacts, "kontakte", "c.vcf", vcf("c", "Carla"))
	v.current.Store("b")
	_, err := s.Collections(Calendar, "alex")
	wantElsewhere(t, "before", err, "a")
	if err := s.Abandon(Calendar, "alex"); err != nil {
		t.Fatal(err)
	}
	if cols, err := s.Collections(Calendar, "alex"); err != nil || !cols[0].Virtual {
		t.Fatalf("after Abandon: %v %v", cols, err)
	}
	if m := v.mark("alex"); m.has(Calendar) || !m.has(Contacts) {
		t.Fatalf("mark: %+v", m)
	}
}
