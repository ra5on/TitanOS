package store

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// withReadHook sets readHook for one test.
func withReadHook(t *testing.T, hook func()) {
	t.Helper()
	readHook = hook
	t.Cleanup(func() { readHook = nil })
}

// within fails the test when fn does not end in a second.
func within(t *testing.T, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("%s waited for a read of the disk", what)
	}
}

// A read of an entry or a picture that waits for the disk holds up no
// listing, ctag, Stat or other read of the collection (1.13).
func TestReadsDoNotHoldTheLock(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Contacts, DefaultBook, "a.vcf", vcf("a", "Anna"))
	etag := mustPut(t, s, Contacts, DefaultBook, "b.vcf", vcf("b", "Bert", "PHOTO;ENCODING=b;TYPE=JPEG:/9j/")).ETag
	if err := s.SavePhoto("alex", "b.vcf", etag, []byte("jpeg")); err != nil {
		t.Fatal(err)
	}
	mustPut(t, s, Calendar, DefaultCalendar, "x.ics", ics("x", "X", "20261110T080000Z"))
	for _, slow := range []struct {
		name string
		read func() error
	}{
		{"Get", func() error { _, err := s.Get(Contacts, "alex", DefaultBook, "a.vcf"); return err }},
		{"Photo", func() error { _, err := s.Photo("alex", "b.vcf"); return err }},
		{"Get of an event", func() error { _, err := s.Get(Calendar, "alex", DefaultCalendar, "x.ics"); return err }},
	} {
		entered, release := make(chan struct{}), make(chan struct{})
		var once sync.Once
		withReadHook(t, func() {
			once.Do(func() { close(entered) })
			<-release
		})
		errc := make(chan error, 1)
		go func() { errc <- slow.read() }()
		<-entered
		within(t, slow.name+": Objects", func() {
			if list, err := s.Objects(Contacts, "alex", DefaultBook); err != nil || len(list) != 2 {
				t.Errorf("objects: %v %v", list, err)
			}
		})
		within(t, slow.name+": GetCollection", func() { _, _ = s.GetCollection(Contacts, "alex", DefaultBook) })
		within(t, slow.name+": Collections", func() { _, _ = s.Collections(Calendar, "alex") })
		within(t, slow.name+": Stat", func() { _, _ = s.Stat(Contacts, "alex", DefaultBook, "a.vcf") })
		within(t, slow.name+": ChangesLimit", func() { _, _ = s.ChangesLimit(Calendar, "alex", DefaultCalendar, "", 0) })
		within(t, slow.name+": SuggestAddresses", func() { _ = s.SuggestAddresses("alex", "", 5) })
		close(release)
		if err := <-errc; err != nil {
			t.Fatalf("%s: %v", slow.name, err)
		}
		readHook = nil
	}
}

// A write that lands while a file is read without the lock: the answer
// is the bytes of one version with the ETag of that same version.
func TestGetOvertakenByAWrite(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	old := mustPut(t, s, Contacts, DefaultBook, "a.vcf", vcf("a", "Anna"))
	newer := vcf("a", "Anna Neu")
	var once sync.Once
	withReadHook(t, func() {
		once.Do(func() { mustPut(t, s, Contacts, DefaultBook, "a.vcf", newer) })
	})
	obj, err := s.Get(Contacts, "alex", DefaultBook, "a.vcf")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(obj.Data, newer) || obj.ETag != ETag(newer) || obj.ETag == old.ETag {
		t.Fatalf("got %q with %s", obj.Data, obj.ETag)
	}
	// Deleted while it was read: not found, never the old bytes.
	once = sync.Once{}
	withReadHook(t, func() {
		once.Do(func() {
			if err := s.DeleteObject(Contacts, "alex", DefaultBook, "a.vcf", ""); err != nil {
				t.Error(err)
			}
		})
	})
	if _, err := s.Get(Contacts, "alex", DefaultBook, "a.vcf"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted meanwhile: %v", err)
	}
	// A file changed behind titand's back still counts with its own ETag.
	readHook = nil
	mustPut(t, s, Contacts, DefaultBook, "b.vcf", vcf("b", "Bert"))
	outside := vcf("b", "Bert von außen")
	if err := writeFile(e, "contacts", "alex/kontakte/o/b.vcf", outside); err != nil {
		t.Fatal(err)
	}
	if obj, err := s.Get(Contacts, "alex", DefaultBook, "b.vcf"); err != nil || obj.ETag != ETag(outside) {
		t.Fatalf("changed outside: %v %+v", err, obj)
	}
}

// Writes and reads at once (go test -race): every answer is consistent.
func TestGetWhileWriting(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	versions := [][]byte{vcf("a", "Anna Eins"), vcf("a", "Anna Zwei"), vcf("a", "Anna Drei")}
	mustPut(t, s, Contacts, DefaultBook, "a.vcf", versions[0])
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 1; ; i++ {
			select {
			case <-stop:
				return
			default:
			}
			if _, err := s.Put(Contacts, "alex", DefaultBook, "a.vcf", versions[i%3], PutOptions{}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	r, err := s.Reader(Contacts, "alex", DefaultBook)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for i := range 300 {
		get := s.Get
		if i%2 == 1 {
			get = func(Kind, string, string, string) (Object, error) { return r.Get("a.vcf") }
		}
		obj, err := get(Contacts, "alex", DefaultBook, "a.vcf")
		if err != nil {
			t.Fatal(err)
		}
		if ETag(obj.Data) != obj.ETag {
			t.Fatalf("bytes of one version with the ETag of another: %s", obj.ETag)
		}
	}
	close(stop)
	wg.Wait()
}

// A picture read while its card changes belongs to the ETag given.
func TestPhotoFollowsItsCard(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	first := mustPut(t, s, Contacts, DefaultBook, "b.vcf", vcf("b", "Bert", "PHOTO;ENCODING=b;TYPE=JPEG:/9j/")).ETag
	if err := s.SavePhoto("alex", "b.vcf", first, []byte("old picture")); err != nil {
		t.Fatal(err)
	}
	var second string
	var once sync.Once
	withReadHook(t, func() {
		once.Do(func() {
			second = mustPut(t, s, Contacts, DefaultBook, "b.vcf", vcf("b", "Bert", "PHOTO;ENCODING=b;TYPE=JPEG:/9j/4A")).ETag
			if err := s.SavePhoto("alex", "b.vcf", second, []byte("new picture")); err != nil {
				t.Error(err)
			}
		})
	})
	data, etag, err := s.PhotoTag("alex", "b.vcf")
	if err != nil || string(data) != "new picture" || etag != second {
		t.Fatalf("picture %q of %s (card %s): %v", data, etag, second, err)
	}
}

// writeFile writes a file below the volume as another program would.
func writeFile(e *env, feature, rel string, data []byte) error {
	root, release, err := e.store().Volume(feature, false)
	if err != nil {
		return err
	}
	defer release()
	defer root.Close()
	f, err := root.Create(rel)
	if err != nil {
		return fmt.Errorf("create %s: %w", rel, err)
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// Unabhängige Durchsicht 1.13, A-6: a reader of an account that is deleted
// and made anew under the same name before its first read never gives the
// new account's bytes (the folder is opened by name; owner.json decides).
func TestReaderAfterForgetAndNewAccount(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "Alt", "20261110T080000Z"))
	mustPut(t, s, Contacts, "kontakte", "c.vcf", vcf("c", "Alt"))
	r, err := s.Reader(Calendar, "alex", "privat")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	// For recurring entries and pictures the same path (readObject, readPhoto).
	acc, err := s.accountFor(Calendar, "alex")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ForgetUser("alex"); err != nil {
		t.Fatal(err)
	}
	e.setCreated("alex", t2)
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "Neu", "20261110T080000Z"))
	obj, err := r.Get("a.ics")
	if err == nil || strings.Contains(string(obj.Data), "Neu") {
		t.Fatalf("old reader gave the new account's entry: %v %q", err, obj.Data)
	}
	if data, err := s.readObject(acc, "privat", "a.ics"); err == nil || strings.Contains(string(data), "Neu") {
		t.Fatalf("readObject of the old account: %v %q", err, data)
	}
	// The new account reads its own.
	if obj, err := s.Get(Calendar, "alex", "privat", "a.ics"); err != nil || !strings.Contains(string(obj.Data), "Neu") {
		t.Fatalf("new account: %v %q", err, obj.Data)
	}
}

// The same without a known creation time of accounts: the reader of the
// forgotten account reads nothing of the folder made anew.
func TestReaderAfterForgetWithoutCreationTime(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	s.Account = nil
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "Alt", "20261110T080000Z"))
	r, err := s.Reader(Calendar, "alex", "privat")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := s.ForgetUser("alex"); err != nil {
		t.Fatal(err)
	}
	mustPut(t, s, Calendar, "privat", "a.ics", ics("a", "Neu", "20261110T080000Z"))
	if obj, err := r.Get("a.ics"); err == nil || strings.Contains(string(obj.Data), "Neu") {
		t.Fatalf("old reader gave the new entry: %v %q", err, obj.Data)
	}
}
