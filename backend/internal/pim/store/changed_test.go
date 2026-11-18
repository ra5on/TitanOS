package store

import (
	"context"
	"slices"
	"sync"
	"testing"
)

// OnChanged hears every change once, with the ctag after it, and only
// after the locks are given back (the callback reads the store).
func TestOnChanged(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	var mu sync.Mutex
	var got []Changed
	s.OnChanged = func(c Changed) {
		// Reading in the callback must not wait for the write's locks.
		if c.CTag != "" {
			col, err := s.GetCollection(c.Kind, c.Account, c.Collection)
			if err != nil || col.CTag != c.CTag {
				t.Errorf("callback reads %+v: %v %+v", c, err, col)
			}
			_, _ = s.Objects(c.Kind, c.Account, c.Collection)
		}
		_, _ = s.Collections(c.Kind, c.Account)
		mu.Lock()
		got = append(got, c)
		mu.Unlock()
	}
	take := func() []Changed {
		mu.Lock()
		defer mu.Unlock()
		out := got
		got = nil
		return out
	}
	ctag := func(kind Kind, col string) string {
		c, err := s.GetCollection(kind, "alex", col)
		if err != nil {
			t.Fatal(err)
		}
		return c.CTag
	}
	mustPut(t, s, Contacts, DefaultBook, "a.vcf", vcf("a", "Anna"))
	if ev := take(); len(ev) != 1 || ev[0] != (Changed{Kind: Contacts, Account: "alex", Collection: DefaultBook, CTag: ctag(Contacts, DefaultBook)}) {
		t.Fatalf("put: %+v", ev)
	}
	// The same bytes again change nothing and tell nothing.
	mustPut(t, s, Contacts, DefaultBook, "a.vcf", vcf("a", "Anna"))
	if ev := take(); len(ev) != 0 {
		t.Fatalf("unchanged put: %+v", ev)
	}
	if err := s.DeleteObject(Contacts, "alex", DefaultBook, "a.vcf", ""); err != nil {
		t.Fatal(err)
	}
	if ev := take(); len(ev) != 1 || ev[0].CTag != ctag(Contacts, DefaultBook) {
		t.Fatalf("delete: %+v", ev)
	}
	// An import is one change.
	if _, err := s.Import(context.Background(), Contacts, "alex", DefaultBook, []Item{{"b.vcf", vcf("b", "B")}, {"c.vcf", vcf("c", "C")}}); err != nil {
		t.Fatal(err)
	}
	if ev := take(); len(ev) != 1 || ev[0].CTag != ctag(Contacts, DefaultBook) {
		t.Fatalf("import: %+v", ev)
	}
	// A new calendar makes the virtual "privat" real as well: both told.
	if _, err := s.CreateCollection(Calendar, "alex", "arbeit", Meta{Name: "Arbeit"}); err != nil {
		t.Fatal(err)
	}
	ev := take()
	var cols []string
	for _, c := range ev {
		cols = append(cols, c.Collection)
		if c.Kind != Calendar || c.CTag != ctag(Calendar, c.Collection) {
			t.Errorf("create: %+v", c)
		}
	}
	slices.Sort(cols)
	if !slices.Equal(cols, []string{"arbeit", "privat"}) {
		t.Fatalf("create: %+v", ev)
	}
	name := "Büro"
	if _, err := s.UpdateCollection(Calendar, "alex", "arbeit", MetaPatch{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if ev := take(); len(ev) != 1 || ev[0].Collection != "arbeit" || ev[0].CTag != ctag(Calendar, "arbeit") {
		t.Fatalf("update: %+v", ev)
	}
	if err := s.DeleteCollection(Calendar, "alex", "arbeit", ""); err != nil {
		t.Fatal(err)
	}
	if ev := take(); len(ev) != 1 || ev[0] != (Changed{Kind: Calendar, Account: "alex", Collection: "arbeit"}) {
		t.Fatalf("delete collection: %+v", ev)
	}
	// A refused write tells nothing.
	if _, err := s.Put(Contacts, "alex", DefaultBook, "b.vcf", vcf("b", "B2"), PutOptions{IfNoneMatch: true}); err == nil {
		t.Fatal("precondition")
	}
	if ev := take(); len(ev) != 0 {
		t.Fatalf("refused: %+v", ev)
	}
}
