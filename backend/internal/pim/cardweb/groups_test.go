package cardweb

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/card"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

var groupNow = time.Date(2026, 11, 18, 9, 30, 0, 0, time.UTC)

// iOS's group as the iPhone sends it (davsync/testdata/ios26).
func iosGroup(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../davsync/testdata/ios26/fixtures/group-family.vcf")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReadGroupIOS(t *testing.T) {
	g, err := ReadGroup(iosGroup(t))
	if err != nil {
		t.Fatal(err)
	}
	if g.Name != "Familie" || !slices.Equal(g.UIDs, []string{"A5B7C9D1-E3F5-4789-9ABC-DEF012345678"}) || g.Other != 0 {
		t.Fatalf("%+v", g)
	}
	if _, err := ReadGroup(iosCard); err == nil {
		t.Fatal("a contact read as a group")
	}
}

func TestReadGroupV4(t *testing.T) {
	v4 := crlf(`
BEGIN:VCARD
VERSION:4.0
KIND:group
FN:Verein
MEMBER:urn:uuid:aaa-1
MEMBER:mailto:info@verein.example
MEMBER:urn:uuid:aaa-1
MEMBER:bbb-2
UID:urn:uuid:grp
END:VCARD
`)
	g, err := ReadGroup(v4)
	if err != nil {
		t.Fatal(err)
	}
	if g.Name != "Verein" || !slices.Equal(g.UIDs, []string{"aaa-1", "bbb-2"}) || g.Other != 1 {
		t.Fatalf("%+v", g)
	}
	// Adding writes MEMBER (the card's form); the mailto: member stays.
	out, changed, err := EditGroup(v4, GroupEdit{Add: []string{"ccc-3"}, Remove: []string{"bbb-2"}}, groupNow)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	s := string(out)
	if !strings.Contains(s, "MEMBER:urn:uuid:ccc-3\r\n") || strings.Contains(s, "bbb-2") ||
		!strings.Contains(s, "MEMBER:mailto:info@verein.example\r\n") || strings.Contains(s, "X-ADDRESSBOOKSERVER") {
		t.Fatalf("%s", s)
	}
}

func TestBuildGroupLikeIOS(t *testing.T) {
	data, err := BuildGroup("Fußball, Verein; 1. Mannschaft", "G-1", []string{"U1", "U2", "U1", ""}, groupNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := card.Parse(data); err != nil {
		t.Fatalf("not a valid card: %v\n%s", err, data)
	}
	s := string(data)
	for _, want := range []string{
		"VERSION:3.0\r\n", "X-ADDRESSBOOKSERVER-KIND:group\r\n",
		"FN:Fußball\\, Verein\\; 1. Mannschaft\r\n", "N:Fußball\\, Verein\\; 1. Mannschaft;;;;\r\n",
		"X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:U1\r\nX-ADDRESSBOOKSERVER-MEMBER:urn:uuid:U2\r\n",
		"UID:G-1\r\n", "REV:20261118T093000Z\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	g, err := ReadGroup(data)
	if err != nil || g.Name != "Fußball, Verein; 1. Mannschaft" || !slices.Equal(g.UIDs, []string{"U1", "U2"}) {
		t.Fatalf("%+v %v", g, err)
	}
	// The store indexes it as a group (hidden in lists).
	if _, err := BuildGroup("  ", "G-2", nil, groupNow); err == nil {
		t.Fatal("a group without a name")
	}
}

func TestEditGroupKeepsLines(t *testing.T) {
	in := iosGroup(t)
	name := "Familie Muster"
	out, changed, err := EditGroup(in, GroupEdit{Name: &name, Add: []string{"NEW-UID", "A5B7C9D1-E3F5-4789-9ABC-DEF012345678"}}, groupNow)
	if err != nil || !changed {
		t.Fatal(err, changed)
	}
	inLines := strings.Split(string(in), "\r\n")
	outLines := strings.Split(string(out), "\r\n")
	// Every line but N, FN and REV as it came; one member more.
	for _, l := range inLines {
		if strings.HasPrefix(l, "N:") || strings.HasPrefix(l, "FN:") || strings.HasPrefix(l, "REV:") {
			continue
		}
		if !slices.Contains(outLines, l) {
			t.Errorf("line %q lost", l)
		}
	}
	for _, want := range []string{"FN:Familie Muster", "N:Familie Muster;;;;", "X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:NEW-UID", "REV:20261118T093000Z"} {
		if !slices.Contains(outLines, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	// Nothing to do: the bytes as they were.
	same, changed, err := EditGroup(in, GroupEdit{Add: []string{"A5B7C9D1-E3F5-4789-9ABC-DEF012345678"}}, groupNow)
	if err != nil || changed || !bytes.Equal(same, in) {
		t.Fatal("unchanged group rewritten", err, changed)
	}
	// Removing the only member.
	out, _, err = EditGroup(in, GroupEdit{Remove: []string{"A5B7C9D1-E3F5-4789-9ABC-DEF012345678"}}, groupNow)
	if err != nil || strings.Contains(string(out), "MEMBER") {
		t.Fatalf("%v\n%s", err, out)
	}
	if _, _, err := EditGroup(iosCard, GroupEdit{Add: []string{"x"}}, groupNow); err == nil {
		t.Fatal("a contact edited as a group")
	}
}

func TestDuplicates(t *testing.T) {
	obj := func(name, fn string, emails, phones []string) pimstore.ObjectInfo {
		return pimstore.ObjectInfo{Name: name, Entry: pimstore.Entry{FN: fn, Emails: emails, Phones: phones}}
	}
	objects := []pimstore.ObjectInfo{
		obj("a.vcf", "Anna Beispiel", []string{"Anna@Beispiel.example"}, nil),
		obj("b.vcf", "A. Beispiel", []string{"anna@beispiel.example"}, nil),
		obj("c.vcf", "Ben Muster", nil, []string{"+491701234567"}),
		obj("d.vcf", "Benjamin M.", nil, []string{"01701234567"}),
		obj("e.vcf", "Muster, Carla", nil, nil),
		obj("f.vcf", "Carla Muster", nil, nil),
		obj("g.vcf", "Einzeln", []string{"solo@example.com"}, []string{"0221123456"}),
		// A switchboard shared by five: no sign of a duplicate.
		obj("s1.vcf", "Kollege Eins", nil, []string{"+49221300400"}),
		obj("s2.vcf", "Kollege Zwei", nil, []string{"+49221300400"}),
		obj("s3.vcf", "Kollege Drei", nil, []string{"+49221300400"}),
		obj("s4.vcf", "Kollege Vier", nil, []string{"+49221300400"}),
		obj("s5.vcf", "Kollege Fünf", nil, []string{"+49221300400"}),
		// A group with the same name is never a duplicate.
		{Name: "grp.vcf", Entry: pimstore.Entry{FN: "Anna Beispiel", Group: true}},
	}
	sets := Duplicates(objects)
	got := map[string][]string{}
	for _, s := range sets {
		var kinds []string
		for _, m := range s.Matches {
			kinds = append(kinds, m.Kind)
		}
		got[strings.Join(s.IDs, ",")] = kinds
	}
	want := map[string][]string{
		"b.vcf,a.vcf": {MatchEmail},
		"c.vcf,d.vcf": {MatchPhone},
		"f.vcf,e.vcf": {MatchName},
	}
	if len(got) != len(want) {
		t.Fatalf("sets %v", got)
	}
	for ids, kinds := range want {
		if !slices.Equal(got[ids], kinds) {
			t.Errorf("%s: %v, want %v (all %v)", ids, got[ids], kinds, got)
		}
	}
	if Duplicates(nil) == nil {
		t.Fatal("nil instead of an empty list")
	}
}

func TestPhoneKeys(t *testing.T) {
	meet := func(a, b string) bool {
		for _, x := range PhoneKeys(a) {
			if slices.Contains(PhoneKeys(b), x) {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"+49 170 1234567", "0170 1234567", true},
		{"0049 170 1234567", "+49 170 1234567", true},
		{"+1 555 123 4567", "555 123 4567", true},
		{"0170 1234567", "0170 1234568", false},
		{"112", "112", false},
	} {
		if got := meet(c.a, c.b); got != c.want {
			t.Errorf("%s ~ %s: %v", c.a, c.b, got)
		}
	}
}

func TestMergeKeepsUnknown(t *testing.T) {
	keep := crlf(`
BEGIN:VCARD
VERSION:3.0
PRODID:-//Apple Inc.//iPhone OS 26.1//EN
N:Muster;Ben;;;
FN:Ben Muster
TEL;type=CELL:+49 151 7654321
item1.URL:https://ben.example
item1.X-ABLabel:_$!<HomePage>!$_
X-APPLE-PHONETIC-FIRST:Ben
UID:KEEP
END:VCARD
`)
	drop := crlf(`
BEGIN:VCARD
VERSION:3.0
N:Muster;Benjamin;;;
FN:Benjamin Muster
EMAIL;type=INTERNET:ben@mail.example
item1.X-SOCIALPROFILE;type=twitter:x-apple:benm
item1.X-ABLabel:twitter
item2.TEL:+49 221 555 0142
item2.X-ABLabel:Werkstatt
X-APPLE-PHONETIC-FIRST:Benjamin
NICKNAME:Benni
X-CUSTOM-FIELD:eigener Wert
UID:DROP
END:VCARD
`)
	first, last := "Ben", "Muster"
	in := Input{}
	in.First.Set, in.First.Value = true, &first
	in.Last.Set, in.Last.Value = true, &last
	phones := []Value{{Type: "mobile", Value: "+49 151 7654321"}, {Type: "other", Value: "+49 221 555 0142"}}
	emails := []Value{{Type: "other", Value: "ben@mail.example"}}
	in.Phones.Set, in.Phones.Value = true, &phones
	in.Emails.Set, in.Emails.Value = true, &emails
	out, err := Merge(keep, drop, in, "keep", groupNow)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := card.Parse(out); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{
		"UID:KEEP", "FN:Ben Muster", "item1.URL:https://ben.example", "X-APPLE-PHONETIC-FIRST:Ben\r\n",
		"item2.X-SOCIALPROFILE;type=twitter:x-apple:benm", "item2.X-ABLabel:twitter",
		"NICKNAME:Benni", "X-CUSTOM-FIELD:eigener Wert", "EMAIL", "ben@mail.example", "+49 221 555 0142",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	for _, not := range []string{"UID:DROP", "Benjamin", "Werkstatt"} {
		if strings.Contains(s, not) {
			t.Errorf("%q taken over in\n%s", not, s)
		}
	}
	if _, err := Merge(keep, iosGroup(t), in, "", groupNow); err == nil {
		t.Fatal("a group merged")
	}
}

// Unabhängige Durchsicht 1.13, A-3: a group of the other card with a line
// the merge decides itself (UID, members of a group, numbers …) is not
// taken over at all – not the line, not its companions.
func TestMergeDropsGroupsWithManagedLines(t *testing.T) {
	keep := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:keep-1\r\nFN:Anna\r\nN:;Anna;;;\r\nEND:VCARD\r\n")
	drop := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nUID:drop-1\r\nFN:Anna B\r\nN:B;Anna;;;\r\n" +
		"item1.URL:https://smuggle.example\r\nitem1.UID:smuggled\r\nitem1.X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:other\r\n" +
		"item2.URL:https://ok.example\r\nitem2.X-ABLabel:Blog\r\nEND:VCARD\r\n")
	out, err := Merge(keep, drop, Input{}, "keep", groupNow)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"smuggled", "urn:uuid:other", "smuggle.example"} {
		if strings.Contains(s, bad) {
			t.Errorf("%q taken over:\n%s", bad, s)
		}
	}
	if !strings.Contains(s, "UID:keep-1") || strings.Count(s, "UID") != 1 {
		t.Errorf("UID of the kept card:\n%s", s)
	}
	// A group without such a line still comes along with its label.
	if !strings.Contains(s, "URL:https://ok.example") || !strings.Contains(s, "X-ABLabel:Blog") {
		t.Errorf("ordinary group lost:\n%s", s)
	}
}
