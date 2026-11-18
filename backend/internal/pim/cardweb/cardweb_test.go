package cardweb

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/pim/card"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

func crlf(s string) []byte {
	return []byte(strings.ReplaceAll(strings.TrimPrefix(s, "\n"), "\n", "\r\n"))
}

// iosCard is a card as iOS 26 writes it: labels in item groups, params of
// its own, IMPP, X-APPLE-*, a birthday without year, a long folded line.
var iosCard = crlf(`
BEGIN:VCARD
VERSION:3.0
PRODID:-//Apple Inc.//iPhone OS 26.1//EN
N:Muster;Ben;;;
FN:Ben Muster
ORG:Beispiel GmbH;Vertrieb
item1.EMAIL;type=INTERNET;type=pref:ben@example.com
item1.X-ABLabel:_$!<Other>!$_
EMAIL;type=INTERNET;type=WORK:b.muster@firma.example
TEL;type=CELL;type=VOICE;type=pref:+49 151 7654321
item2.TEL:+49 221 555 0142
item2.X-ABLabel:Werkstatt
item3.ADR;type=HOME;type=pref:;;Lindenallee 7\nHinterhaus;Köln;;50667;Deutschland
item3.X-ABADR:de
IMPP;X-SERVICE-TYPE=Skype;type=HOME;type=pref:skype:ben.muster
X-APPLE-PHONETIC-FIRST:Ben
BDAY;X-APPLE-OMIT-YEAR=1604:1604-11-20
NOTE:Spielt mittwochs Fußball\, allergisch gegen Nüsse\; beim Grillen dran de
 nken.
X-SOCIALPROFILE;type=twitter;x-user=ben:https://twitter.example/ben
URL;type=HOME:https://ben.example
REV:2026-11-10T07:45:12Z
UID:A5B7C9D1-E3F5-4789-9ABC-DEF012345678
END:VCARD
`)

var now = time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)

func input(t *testing.T, js string) Input {
	t.Helper()
	var in Input
	if err := json.Unmarshal([]byte(js), &in); err != nil {
		t.Fatal(err)
	}
	return in
}

func lines(b []byte) []string {
	return strings.Split(strings.TrimSuffix(string(b), "\r\n"), "\r\n")
}

func TestOpenIOSCard(t *testing.T) {
	c, err := Open(iosCard)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(c)
	want := `{"id":"","book":"","etag":"","first":"Ben","last":"Muster","name":"Ben Muster","org":"Beispiel GmbH",` +
		`"phones":[{"type":"mobile","value":"+49 151 7654321"},{"type":"other","value":"+49 221 555 0142","label":"Werkstatt"}],` +
		`"emails":[{"type":"other","value":"ben@example.com"},{"type":"work","value":"b.muster@firma.example"}],` +
		`"addresses":[{"type":"home","street":"Lindenallee 7\nHinterhaus","city":"Köln","postcode":"50667","region":"","country":"Deutschland"}],` +
		`"birthday":"--11-20","notes":"Spielt mittwochs Fußball, allergisch gegen Nüsse; beim Grillen dran denken.","hasPhoto":false,"url":"https://ben.example"}`
	if string(got) != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

// What the page sends back unchanged writes nothing; a change of the notes
// changes only NOTE and REV, every other line stays byte for byte.
func TestEditKeepsWhatThePageDoesNotShow(t *testing.T) {
	c, _ := Open(iosCard)
	same, _ := json.Marshal(map[string]any{"first": c.First, "last": c.Last, "org": c.Org, "phones": c.Phones, "emails": c.Emails,
		"addresses": c.Addresses, "birthday": c.Birthday, "notes": c.Notes})
	out, changed, err := Edit(iosCard, input(t, string(same)), now)
	if err != nil || changed || !bytes.Equal(out, iosCard) {
		t.Fatalf("unchanged input: changed=%v err=%v\n%s", changed, err, out)
	}
	out, changed, err = Edit(iosCard, input(t, `{"notes":"Neue Notiz"}`), now)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	before, after := lines(iosCard), lines(out)
	for _, l := range before {
		if strings.HasPrefix(l, "NOTE:") || strings.HasPrefix(l, " nken") || strings.HasPrefix(l, "REV:") {
			continue
		}
		if !strings.Contains(string(out), l+"\r\n") {
			t.Errorf("line lost: %q", l)
		}
	}
	if !strings.Contains(string(out), "NOTE:Neue Notiz\r\n") || !strings.Contains(string(out), "REV:20261118T100000Z\r\n") || len(after) != len(before)-1 {
		t.Fatalf("edited:\n%s", out)
	}
	if _, err := card.Parse(out); err != nil {
		t.Fatal(err)
	}
}

// Phones, e-mail addresses and addresses: a value kept keeps its line; a
// kind changed keeps the other params; a removed item takes its label
// along; new values come as new lines.
func TestEditValues(t *testing.T) {
	in := input(t, `{"phones":[{"type":"work","value":"+49 151 7654321"},{"type":"mobile","value":"0170 1112223"}],
		"emails":[{"type":"work","value":"b.muster@firma.example"}],
		"addresses":[{"type":"work","street":"Lindenallee 7\nHinterhaus","city":"Köln","postcode":"50667","country":"Deutschland"},
		             {"type":"home","street":"Am Weiher 3","city":"Köln; Porz","postcode":"51147"}]}`)
	out, changed, err := Edit(iosCard, in, now)
	if err != nil || !changed {
		t.Fatal(changed, err)
	}
	s := strings.ReplaceAll(string(out), "\r\n ", "")
	for _, want := range []string{
		"TEL;TYPE=VOICE;TYPE=pref;TYPE=WORK:+49 151 7654321\r\n",
		"TEL;TYPE=CELL:0170 1112223\r\n",
		"EMAIL;type=INTERNET;type=WORK:b.muster@firma.example\r\n",
		"item3.ADR;TYPE=pref;TYPE=WORK:;;Lindenallee 7\\nHinterhaus;Köln;;50667;Deutschland\r\n",
		"item3.X-ABADR:de\r\n",
		"ADR;TYPE=HOME:;;Am Weiher 3;Köln\\; Porz;;51147;\r\n",
		"IMPP;X-SERVICE-TYPE=Skype;type=HOME;type=pref:skype:ben.muster\r\n",
		"X-SOCIALPROFILE;type=twitter;x-user=ben:https://twitter.example/ben\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, gone := range []string{"item1.", "item2.", "ben@example.com", "Werkstatt"} {
		if strings.Contains(s, gone) {
			t.Errorf("still there: %q", gone)
		}
	}
	c, _ := Open(out)
	if len(c.Addresses) != 2 || c.Addresses[1].City != "Köln; Porz" || c.Addresses[0].Type != "work" || c.Phones[0].Type != "work" {
		t.Fatalf("read back: %+v", c)
	}
	if _, _, err := Edit(iosCard, input(t, `{"emails":[{"type":"mobile","value":"x@y"}]}`), now); err == nil {
		t.Fatal("an e-mail address of type mobile")
	}
	if _, _, err := Edit(iosCard, input(t, `{"phones":[{"type":"home","value":"1\n2"}]}`), now); err == nil {
		t.Fatal("a number with a line break")
	}
}

// A birthday without a year: --MM-DD in the API, X-APPLE-OMIT-YEAR=1604
// in vCard 3 (iOS), --MMDD in vCard 4.
func TestBirthdayWithoutYear(t *testing.T) {
	out, _, err := Edit(iosCard, input(t, `{"birthday":"1986-11-20"}`), now)
	if err != nil || !strings.Contains(string(out), "BDAY;VALUE=date:1986-11-20\r\n") || strings.Contains(string(out), "1604") {
		t.Fatalf("with year: %v\n%s", err, out)
	}
	back, _, err := Edit(out, input(t, `{"birthday":"--02-29"}`), now)
	if err != nil || !strings.Contains(string(back), "BDAY;X-APPLE-OMIT-YEAR=1604:1604-02-29\r\n") {
		t.Fatalf("without year: %v\n%s", err, back)
	}
	if c, _ := Open(back); c.Birthday != "--02-29" {
		t.Fatalf("read back %q", c.Birthday)
	}
	v4 := crlf("\nBEGIN:VCARD\nVERSION:4.0\nFN:Anna\nUID:a\nBDAY:--1112\nEND:VCARD\n")
	if c, _ := Open(v4); c.Birthday != "--11-12" {
		t.Fatalf("vCard 4 %q", c.Birthday)
	}
	out, _, err = Edit(v4, input(t, `{"birthday":"--12-24"}`), now)
	if err != nil || !strings.Contains(string(out), "BDAY:--1224\r\n") {
		t.Fatalf("vCard 4 without year: %v\n%s", err, out)
	}
	out, _, _ = Edit(v4, input(t, `{"birthday":""}`), now)
	if strings.Contains(string(out), "BDAY") {
		t.Fatalf("cleared:\n%s", out)
	}
	for _, bad := range []string{"1986-13-01", "--02-30", "20.11.1986", "--1120"} {
		if _, _, err := Edit(iosCard, input(t, `{"birthday":"`+bad+`"}`), now); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestBuild(t *testing.T) {
	data, err := Build(input(t, `{"first":"Lena","last":"Beispiel","phones":[{"type":"mobile","value":"+49 172 9988776"}],
		"emails":[{"type":"home","value":"lena@example.org"}],"notes":"Zeile 1\nZeile 2, mit Komma"}`), "u-1", now)
	if err != nil {
		t.Fatal(err)
	}
	c, err := card.Parse(data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if c.PreferredValue("FN") != "Lena Beispiel" || c.Value("UID") != "u-1" || c.Value("VERSION") != "3.0" {
		t.Fatalf("%s", data)
	}
	for _, want := range []string{"N:Beispiel;Lena;;;\r\n", "TEL;TYPE=CELL:+49 172 9988776\r\n", "EMAIL;TYPE=INTERNET;TYPE=HOME:lena@example.org\r\n",
		"NOTE:Zeile 1\\nZeile 2\\, mit Komma\r\n", "PRODID:" + ProdID + "\r\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in\n%s", want, data)
		}
	}
	// A company; only an address; nothing at all.
	data, _ = Build(input(t, `{"org":"Autohaus Beispiel"}`), "u-2", now)
	if v, _ := Open(data); v.Name != "Autohaus Beispiel" {
		t.Fatalf("company: %+v", v)
	}
	data, _ = Build(input(t, `{"emails":[{"type":"other","value":"trainer@fc-sued.example"}]}`), "u-3", now)
	if v, _ := Open(data); v.Name != "trainer@fc-sued.example" {
		t.Fatalf("address only: %+v", v)
	}
	if _, err := Build(input(t, `{"first":"  "}`), "u-4", now); err == nil {
		t.Fatal("a contact without anything")
	}
	// The name changes: N keeps its other components, FN follows.
	edited, _, err := Edit(crlf("\nBEGIN:VCARD\nVERSION:3.0\nN:Muster;Ben;Karl;Dr.;\nFN:Dr. Ben Karl Muster\nUID:x\nEND:VCARD\n"), input(t, `{"last":"Meier"}`), now)
	if err != nil || !strings.Contains(string(edited), "N:Meier;Ben;Karl;Dr.;\r\n") || !strings.Contains(string(edited), "FN:Ben Meier\r\n") {
		t.Fatalf("renamed: %v\n%s", err, edited)
	}
}

// Long lines are folded at 75 octets, never inside a character; reading
// them back gives the value.
func TestFold(t *testing.T) {
	long := strings.Repeat("Grüße aus Köln – ", 30)
	data, err := Build(input(t, `{"first":"A","notes":"`+long+`"}`), "u", now)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines(data) {
		if len(l) > 75 {
			t.Fatalf("line of %d octets", len(l))
		}
		if !utf8.ValidString(l) {
			t.Fatal("cut inside a character")
		}
	}
	if c, _ := Open(data); c.Notes != long {
		t.Fatalf("read back %q", c.Notes)
	}
}

func TestPhotoLine(t *testing.T) {
	jpeg := bytes.Repeat([]byte{0xff, 0xd8, 1, 2, 3}, 100)
	out, err := SetPhoto(iosCard, jpeg, now)
	if err != nil || !strings.Contains(string(out), "PHOTO;ENCODING=b;TYPE=JPEG:/9g") {
		t.Fatalf("%v\n%s", err, out)
	}
	if p, ok := Photo(out); !ok || !bytes.Equal(p, jpeg) {
		t.Fatal("photo not read back")
	}
	if c, _ := Open(out); !c.HasPhoto {
		t.Fatal("hasPhoto")
	}
	v4, _ := SetPhoto(crlf("\nBEGIN:VCARD\nVERSION:4.0\nFN:A\nUID:a\nEND:VCARD\n"), jpeg, now)
	if !strings.Contains(string(v4), "PHOTO:data:image/jpeg;base64,/9g") {
		t.Fatalf("vCard 4:\n%s", v4)
	}
	if p, ok := Photo(v4); !ok || !bytes.Equal(p, jpeg) {
		t.Fatal("vCard 4 photo not read back")
	}
	gone, _ := SetPhoto(out, nil, now)
	if strings.Contains(string(gone), "PHOTO") {
		t.Fatal("photo not removed")
	}
	// A picture by reference is never fetched: no photo.
	if _, ok := Photo(crlf("\nBEGIN:VCARD\nVERSION:3.0\nFN:A\nPHOTO;VALUE=uri:https://evil.example/x.jpg\nEND:VCARD\n")); ok {
		t.Fatal("a photo by reference")
	}
}

func entry(fn, sortKey, org string, emails, phones, adr []string) pimstore.ObjectInfo {
	return pimstore.ObjectInfo{Name: strings.ToLower(strings.ReplaceAll(fn, " ", "")) + ".vcf",
		Entry: pimstore.Entry{FN: fn, SortKey: sortKey, Org: org, Emails: emails, Phones: phones, Addresses: adr}}
}

func TestListSearchAndSort(t *testing.T) {
	berlin, _ := time.LoadLocation("Europe/Berlin")
	objs := []pimstore.ObjectInfo{
		entry("Ben Muster", "muster ben", "", []string{"ben.muster@mail.example"}, []string{"+491517654321"}, []string{"Lindenallee 7, 50667 Köln"}),
		entry("Eva Krüger", "krüger eva", "", nil, nil, nil),
		entry("Anna Ärger", "ärger anna", "Praxis am Markt", nil, []string{"01701234567"}, nil),
		entry("Autohaus Beispiel", "autohaus beispiel", "Autohaus Beispiel", nil, nil, nil),
		entry("Zoe Bach", "bach zoe", "", nil, nil, nil),
		{Name: "g.vcf", Entry: pimstore.Entry{FN: "Familie", SortKey: "familie", Group: true}},
	}
	objs[1].BDay = "1979-11-20"
	objs[4].BDay = "--11-16"
	names := func(items []Item) string {
		var s []string
		for _, it := range items {
			s = append(s, it.Name)
		}
		return strings.Join(s, ",")
	}
	all := List("kontakte", objs, "", now, berlin)
	if got := names(all); got != "Anna Ärger,Autohaus Beispiel,Zoe Bach,Eva Krüger,Ben Muster" {
		t.Fatalf("sorted: %s", got)
	}
	if all[1].Initials != "A" || all[0].Initials != "AÄ" || all[4].Email != "ben.muster@mail.example" {
		t.Fatalf("items: %+v", all)
	}
	// 18.11.2026 is a Wednesday: the week runs 16.–22.11.
	if b := all[3].BirthdaySoon; b == nil || b.Date != "2026-11-20" || b.Age == nil || *b.Age != 47 {
		t.Fatalf("birthday this week: %+v", all[3].BirthdaySoon)
	}
	if b := all[2].BirthdaySoon; b == nil || b.Date != "2026-11-16" || b.Age != nil {
		t.Fatalf("birthday without year on Monday: %+v", all[2].BirthdaySoon)
	}
	if all[4].BirthdaySoon != nil {
		t.Fatal("no birthday")
	}
	for q, want := range map[string]string{
		"0151 765":        "Ben Muster",
		"+49 151 7654321": "Ben Muster",
		"0049151":         "Ben Muster",
		"+49 170 123":     "Anna Ärger",
		"1234567":         "Anna Ärger",
		"KRUGER":          "Eva Krüger",
		"praxis":          "Anna Ärger",
		"mail.example":    "Ben Muster",
		"lindenallee":     "Ben Muster",
		"50667":           "Ben Muster",
		"koln":            "Ben Muster",
		"familie":         "",
		"12":              "",
		"x9":              "",
	} {
		if got := names(List("kontakte", objs, q, now, berlin)); got != want {
			t.Errorf("q=%q: %q, want %q", q, got, want)
		}
	}
}

func TestSplitVCF(t *testing.T) {
	a := crlf("\nBEGIN:VCARD\nVERSION:3.0\nUID:a\nFN:A\nEND:VCARD\n")
	b := []byte("begin:vcard\nversion:4.0\nfn:B\nemail:b@example.org\nend:vcard\n")
	broken := []byte("BEGIN:VCARD\r\nVERSION:3.0\r\nFN:C\r\n")
	file := append(append(append([]byte("junk\r\n"), a...), b...), broken...)
	got := SplitVCF(file)
	if len(got) != 3 || !bytes.Equal(got[0].Data, a) || !bytes.Equal(got[1].Data, b) || !bytes.Equal(got[2].Data, broken) {
		t.Fatalf("%q", got)
	}
	if got[0].UID != "a" || got[1].UID != "" || got[1].FN != "B" || got[1].Email != "b@example.org" {
		t.Fatalf("%+v", got)
	}
	if Key("B", "B@example.org") != Key(" b ", "b@EXAMPLE.org") || Key("", "x") != "" {
		t.Fatal("key")
	}
	if w := WithUID(b, "new"); !strings.Contains(string(w), "UID:new\r\n") {
		t.Fatalf("%s", w)
	}
}
