package cardweb

import (
	"testing"
	"time"

	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// The prepared rows of ListIndex match exactly as Matches does on the
// index entries (1.13: folded once per ctag, not per request).
func TestListIndexMatchesLikeMatches(t *testing.T) {
	entries := []pimstore.ObjectInfo{
		{Name: "a.vcf", Entry: pimstore.Entry{FN: "Jürgen Groß", SortKey: "groß jürgen", Org: "Bäckerei Ölmann", Emails: []string{"Juergen@Example.org"}, Phones: []string{"+491701234567"}, Addresses: []string{"Hauptstraße 1, 80331 München, Deutschland"}}},
		{Name: "b.vcf", Entry: pimstore.Entry{FN: "Erika Muster", SortKey: "muster erika", Phones: []string{"01701234567", "089123"}, BDay: "1980-11-17"}},
		{Name: "c.vcf", Entry: pimstore.Entry{FN: "Firma", Org: "Firma", Emails: []string{"info@firma.example", "b@x"}}},
		{Name: "g.vcf", Entry: pimstore.Entry{FN: "Gruppe", Group: true}},
	}
	ix := NewListIndex("kontakte", entries)
	byID := map[string]*listRow{}
	for i := range ix.rows {
		byID[ix.rows[i].item.ID] = &ix.rows[i]
	}
	if len(ix.rows) != 3 || byID["g.vcf"] != nil {
		t.Fatalf("rows: %+v", ix.rows)
	}
	for _, q := range []string{"", "jürgen", "JURGEN", "gross", "ölm", "oelm", "example", "EXAMPLE.ORG", "münchen", "muenchen",
		"0170 12", "+49 170", "0049170", "089", "12", "muster", "firma", "b@x", "x", "+", "0170-1234567"} {
		query := NewQuery(q)
		for _, e := range entries {
			if e.Group {
				continue
			}
			if want, got := query.Matches(e.Entry), query.matchesRow(byID[e.Name]); want != got {
				t.Errorf("%q on %s: Matches %v, row %v", q, e.Name, want, got)
			}
		}
	}
	// The list of the index is the list of List, with this week's birthday.
	now := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	got, want := ix.List("", now, time.UTC), List("kontakte", entries, "", now, time.UTC)
	if len(got) != 3 || len(want) != 3 {
		t.Fatalf("list %+v", got)
	}
	for i := range got {
		if got[i].ID != want[i].ID || (got[i].BirthdaySoon == nil) != (want[i].BirthdaySoon == nil) {
			t.Fatalf("%d: %+v / %+v", i, got[i], want[i])
		}
	}
	if got[2].ID != "b.vcf" || got[2].BirthdaySoon == nil || got[2].BirthdaySoon.Date != "2026-11-17" {
		t.Fatalf("order or birthday: %+v", got)
	}
	// A row handed out is a copy: changing it changes nothing kept.
	got[0].Name = "x"
	if ix.List("", now, time.UTC)[0].Name == "x" {
		t.Fatal("the list hands out the kept rows")
	}
}
