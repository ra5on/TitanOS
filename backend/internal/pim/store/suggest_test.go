package store

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// T-Mail asks for addresses of contacts (TECHNIK 7.4): FN and each EMAIL
// one entry, groups left out, each address once, a word start first.
func TestSuggestAddresses(t *testing.T) {
	e := newEnv(t)
	s := e.store()
	// Nothing stored yet: empty, and nothing made on the volume.
	if got := s.SuggestAddresses("alex", "", 20); len(got) != 0 {
		t.Fatalf("empty book: %v", got)
	}
	if tree := e.tree(); len(tree) != 0 {
		t.Fatalf("reading made %v", tree)
	}
	mustPut(t, s, Contacts, DefaultBook, "erika.vcf", vcf("erika", "Erika Mustermann", "N:Mustermann;Erika;;;", "EMAIL;TYPE=HOME:erika@example.org", "EMAIL;TYPE=WORK:E.Mustermann@firma.example"))
	mustPut(t, s, Contacts, DefaultBook, "ben.vcf", vcf("ben", "Ben Abend", "N:Abend;Ben;;;", "EMAIL:ben@example.org", "EMAIL:erika@EXAMPLE.org"))
	mustPut(t, s, Contacts, DefaultBook, "nomail.vcf", vcf("nomail", "Erich Ohnepost"))
	mustPut(t, s, Contacts, DefaultBook, "group.vcf", vcf("group", "Erika Gruppe", "X-ADDRESSBOOKSERVER-KIND:group", "EMAIL:gruppe@example.org"))

	got := s.SuggestAddresses("alex", "eri", 20)
	want := []Suggestion{
		// Ben's second address is Erika's (case aside): it comes once,
		// with the first contact by sort key.
		{Name: "Ben Abend", Addr: "erika@EXAMPLE.org"},
		{Name: "Erika Mustermann", Addr: "E.Mustermann@firma.example"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("eri: %+v", got)
	}
	// A name matches with all its addresses; a word start comes before a
	// match inside ("firma" is inside Erika's work address only).
	got = s.SuggestAddresses("alex", "BEN", 20)
	if len(got) != 2 || got[0].Addr != "ben@example.org" || got[1].Addr != "erika@EXAMPLE.org" {
		t.Fatalf("ben: %+v", got)
	}
	got = s.SuggestAddresses("alex", "firma", 20)
	if len(got) != 1 || got[0].Addr != "E.Mustermann@firma.example" {
		t.Fatalf("inside: %+v", got)
	}
	got = s.SuggestAddresses("alex", "e", 20)
	// ben@example.org has "e" only inside: last.
	if len(got) != 3 || got[2].Addr != "ben@example.org" {
		t.Fatalf("e: %+v", got)
	}
	all := s.SuggestAddresses("alex", "", 20)
	if len(all) != 3 || slices.ContainsFunc(all, func(x Suggestion) bool { return strings.Contains(x.Addr, "gruppe") }) {
		t.Fatalf("all: %+v", all)
	}
	if got := s.SuggestAddresses("alex", "", 2); len(got) != 2 {
		t.Fatalf("limit: %+v", got)
	}
	// Another account sees none of them; without a volume there are none.
	if got := s.SuggestAddresses("bea", "", 20); len(got) != 0 {
		t.Fatalf("bea: %+v", got)
	}
	e.present.Store(false)
	if got := s.SuggestAddresses("alex", "", 20); got != nil {
		t.Fatalf("no volume: %+v", got)
	}
}
