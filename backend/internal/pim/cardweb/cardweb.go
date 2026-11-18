// Package cardweb is what the web API of Kontakte does with vCards
// (TECHNIK 7.2): the JSON form of a contact, building a new card, editing
// one on its content lines (pim/card.Doc: what the page does not show
// stays byte for byte), the list with search and sort, and splitting .vcf
// files for the import. It holds no state and touches no disk;
// api/contacts*.go call it with what pim/store gives.
package cardweb

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/card"
)

// ProdID names TitanOS in the cards it makes.
const ProdID = "-//TitanOS//Kontakte 1.11//DE"

// Bounds of the fields of a contact.
const (
	MaxName   = 2000
	MaxValue  = 1000
	MaxNotes  = 32 << 10
	MaxValues = 100
)

// ErrInvalid marks a contact the API refuses (400).
var ErrInvalid = errors.New("cardweb: invalid contact")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Value is a phone number or an e-mail address. Type is "mobile" (phones
// only), "home", "work" or "other"; Label is a label of its own from the
// iPhone (item1.X-ABLabel), shown as it is, never sent back.
type Value struct {
	Type  string `json:"type"`
	Value string `json:"value"`
	Label string `json:"label,omitempty"`
}

// Address is a postal address (ADR without post office box and extended
// address, which stay on the line as long as it is not changed).
type Address struct {
	Type     string `json:"type"`
	Label    string `json:"label,omitempty"`
	Street   string `json:"street"`
	City     string `json:"city"`
	Postcode string `json:"postcode"`
	Region   string `json:"region"`
	Country  string `json:"country"`
}

func (a Address) key() [5]string {
	return [5]string{strings.TrimSpace(a.Street), strings.TrimSpace(a.City), strings.TrimSpace(a.Postcode), strings.TrimSpace(a.Region), strings.TrimSpace(a.Country)}
}

// Contact is one contact as the page shows it.
type Contact struct {
	ID    string `json:"id"`
	Book  string `json:"book"`
	ETag  string `json:"etag"`
	First string `json:"first"`
	Last  string `json:"last"`
	// Name is FN as stored (what lists and T-Mail show).
	Name      string    `json:"name"`
	Org       string    `json:"org"`
	Phones    []Value   `json:"phones"`
	Emails    []Value   `json:"emails"`
	Addresses []Address `json:"addresses"`
	// Birthday is YYYY-MM-DD, --MM-DD without a year (also iOS's 1604), or
	// "" (none, or a value TitanOS cannot read – it stays in the card).
	Birthday string `json:"birthday"`
	Notes    string `json:"notes"`
	HasPhoto bool   `json:"hasPhoto"`
	// URL is the first URL when it is http or https.
	URL string `json:"url,omitempty"`
	// Modified is when the file was last written (RFC 3339).
	Modified string `json:"modified,omitempty"`
}

// Input is a contact from the page. A field left out (or null) stays as it
// is; an empty string or list clears it.
type Input struct {
	First     calweb.Opt[string]    `json:"first"`
	Last      calweb.Opt[string]    `json:"last"`
	Org       calweb.Opt[string]    `json:"org"`
	Phones    calweb.Opt[[]Value]   `json:"phones"`
	Emails    calweb.Opt[[]Value]   `json:"emails"`
	Addresses calweb.Opt[[]Address] `json:"addresses"`
	Birthday  calweb.Opt[string]    `json:"birthday"`
	Notes     calweb.Opt[string]    `json:"notes"`
}

func set[T any](o calweb.Opt[T]) (T, bool) {
	var zero T
	if !o.Set || o.Value == nil {
		return zero, false
	}
	return *o.Value, true
}

// checkText bounds a text field; lines only where multi is set.
func checkText(field, s string, limit int, multi bool) (string, error) {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if len(s) > limit || !utf8.ValidString(s) {
		return "", invalid("%s too long or not UTF-8", field)
	}
	for _, r := range s {
		if r == 0x7f || (r < 0x20 && r != '\t' && !(multi && r == '\n')) {
			return "", invalid("%s holds a control character", field)
		}
	}
	if !multi {
		s = strings.TrimSpace(s)
	}
	return s, nil
}

// Open reads a card for the page.
func Open(data []byte) (Contact, error) {
	d, err := card.ParseDoc(data)
	if err != nil {
		return Contact{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return view(d), nil
}

func view(d *card.Doc) Contact {
	c := Contact{Phones: []Value{}, Emails: []Value{}, Addresses: []Address{}}
	if l := d.First("FN"); l != nil {
		c.Name = strings.TrimSpace(card.UnescapeText(l.Value))
	}
	if l := d.First("N"); l != nil {
		n := card.Structured(l.Value)
		c.Last = strings.TrimSpace(n[0])
		if len(n) > 1 {
			c.First = strings.TrimSpace(n[1])
		}
	}
	if l := d.First("ORG"); l != nil {
		c.Org = strings.TrimSpace(card.Structured(l.Value)[0])
	}
	for _, l := range d.Find("TEL") {
		t, label := kindOf(d, l, true)
		c.Phones = append(c.Phones, Value{Type: t, Label: label, Value: telValue(l.Value)})
	}
	for _, l := range d.Find("EMAIL") {
		t, label := kindOf(d, l, false)
		c.Emails = append(c.Emails, Value{Type: t, Label: label, Value: strings.TrimSpace(card.UnescapeText(l.Value))})
	}
	for _, l := range d.Find("ADR") {
		t, label := kindOf(d, l, false)
		c.Addresses = append(c.Addresses, addressOf(l, t, label))
	}
	if l := d.First("BDAY"); l != nil {
		c.Birthday = birthdayOf(l)
	}
	if l := d.First("NOTE"); l != nil {
		c.Notes = card.UnescapeText(l.Value)
	}
	if _, ok := d.PhotoData(); ok {
		c.HasPhoto = true
	}
	if l := d.First("URL"); l != nil {
		u := strings.TrimSpace(card.UnescapeText(l.Value))
		if lower := strings.ToLower(u); strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
			c.URL = u
		}
	}
	return c
}

func telValue(v string) string {
	v = strings.TrimSpace(card.UnescapeText(v))
	if len(v) >= 4 && strings.EqualFold(v[:4], "tel:") {
		v = v[4:]
	}
	return v
}

func addressOf(l *card.Line, t, label string) Address {
	p := card.Structured(l.Value)
	for len(p) < 7 {
		p = append(p, "")
	}
	tr := strings.TrimSpace
	return Address{Type: t, Label: label, Street: tr(p[2]), City: tr(p[3]), Region: tr(p[4]), Postcode: tr(p[5]), Country: tr(p[6])}
}

// Apple's labels in item1.X-ABLabel.
var appleLabels = map[string]string{
	"_$!<mobile>!$_": "mobile", "_$!<home>!$_": "home", "_$!<work>!$_": "work", "_$!<other>!$_": "other",
	"_$!<main>!$_": "other", "_$!<homefax>!$_": "home", "_$!<workfax>!$_": "work", "_$!<pager>!$_": "other",
}

// kindOf is the type of a TEL, EMAIL or ADR line: Apple's label first,
// then TYPE. A label of its own comes as label (type "other").
func kindOf(d *card.Doc, l *card.Line, phone bool) (string, string) {
	if c := d.Companion(l, "X-ABLABEL"); c != nil {
		v := strings.TrimSpace(card.UnescapeText(c.Value))
		if t, ok := appleLabels[strings.ToLower(v)]; ok {
			if t == "mobile" && !phone {
				t = "other"
			}
			return t, ""
		}
		if v != "" && !strings.HasPrefix(v, "_$!<") {
			return "other", v
		}
	}
	has := map[string]bool{}
	for _, v := range l.Param("TYPE") {
		has[strings.ToUpper(strings.TrimSpace(v))] = true
	}
	switch {
	case phone && (has["CELL"] || has["IPHONE"] || has["MOBILE"]):
		return "mobile", ""
	case has["WORK"]:
		return "work", ""
	case has["HOME"]:
		return "home", ""
	}
	return "other", ""
}

// kindTypes are the TYPE values that say the kind (replaced on a change).
var kindTypes = map[string]bool{"CELL": true, "IPHONE": true, "MOBILE": true, "HOME": true, "WORK": true, "OTHER": true}

func birthdayOf(l *card.Line) string {
	year, month, day, ok := calweb.ParseBDay(l.Value)
	if !ok {
		return ""
	}
	if year == 0 || l.HasParam("X-APPLE-OMIT-YEAR") {
		return fmt.Sprintf("--%02d-%02d", month, day)
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

// checkBirthday reads YYYY-MM-DD or --MM-DD (29 February allowed without a
// year).
func checkBirthday(s string) (year int, month time.Month, day int, err error) {
	s = strings.TrimSpace(s)
	if rest, ok := strings.CutPrefix(s, "--"); ok {
		t, e := time.Parse("01-02", rest)
		if e != nil && rest != "02-29" {
			return 0, 0, 0, invalid("birthday is YYYY-MM-DD or --MM-DD")
		}
		if rest == "02-29" {
			return 0, time.February, 29, nil
		}
		return 0, t.Month(), t.Day(), nil
	}
	t, e := time.Parse("2006-01-02", s)
	if e != nil || t.Year() < 1 {
		return 0, 0, 0, invalid("birthday is YYYY-MM-DD or --MM-DD")
	}
	return t.Year(), t.Month(), t.Day(), nil
}

// Build makes a new vCard 3.0 of an input (UID uid, REV now).
func Build(in Input, uid string, now time.Time) ([]byte, error) {
	skeleton := "BEGIN:VCARD\r\nVERSION:3.0\r\nPRODID:" + ProdID + "\r\nUID:" + card.EscapeText(uid) + "\r\nEND:VCARD\r\n"
	out, _, err := Edit([]byte(skeleton), in, now)
	return out, err
}

// Edit applies the input to a card on its lines: only the fields that
// differ from the card as read are written. A phone, address or address
// whose value is unchanged keeps its line (and its parameters, its Apple
// label); one whose type changed keeps its other parameters. FN is made
// anew from first and last name (else company, else the first e-mail
// address or number) only when one of these changed. Nothing changed:
// changed is false and the bytes are the old ones.
func Edit(data []byte, in Input, now time.Time) ([]byte, bool, error) {
	d, err := card.ParseDoc(data)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	cur := view(d)
	v4 := d.Version() == "4.0"
	changed := false

	first, last, org := cur.First, cur.Last, cur.Org
	for _, f := range []struct {
		opt  calweb.Opt[string]
		to   *string
		name string
	}{{in.First, &first, "first"}, {in.Last, &last, "last"}, {in.Org, &org, "org"}} {
		if f.opt.Set {
			v, _ := set(f.opt)
			if *f.to, err = checkText(f.name, v, MaxName, false); err != nil {
				return nil, false, err
			}
		}
	}
	nameChanged := first != cur.First || last != cur.Last
	if nameChanged || d.First("N") == nil {
		parts := []string{last, first, "", "", ""}
		if l := d.First("N"); l != nil {
			old := card.Structured(l.Value)
			copy(parts[2:], old[min(2, len(old)):])
			parts[0], parts[1] = last, first
		}
		d.Set("N", card.JoinStructured(parts))
		changed = true
	}
	if org != cur.Org {
		if org == "" {
			d.Remove(d.Find("ORG")...)
		} else {
			parts := []string{org}
			if l := d.First("ORG"); l != nil {
				parts = append(parts, card.Structured(l.Value)[1:]...)
			}
			d.Set("ORG", card.JoinStructured(parts))
		}
		changed = true
	}

	if in.Phones.Set {
		list, _ := set(in.Phones)
		c, err := editValues(d, "TEL", list, true, v4)
		if err != nil {
			return nil, false, err
		}
		changed = changed || c
	}
	if in.Emails.Set {
		list, _ := set(in.Emails)
		c, err := editValues(d, "EMAIL", list, false, v4)
		if err != nil {
			return nil, false, err
		}
		changed = changed || c
	}
	if in.Addresses.Set {
		list, _ := set(in.Addresses)
		c, err := editAddresses(d, list, v4)
		if err != nil {
			return nil, false, err
		}
		changed = changed || c
	}
	if in.Birthday.Set {
		b, _ := set(in.Birthday)
		if strings.TrimSpace(b) != cur.Birthday {
			d.Remove(d.Find("BDAY")...)
			if strings.TrimSpace(b) != "" {
				year, month, day, err := checkBirthday(b)
				if err != nil {
					return nil, false, err
				}
				d.Add(birthdayLine(year, month, day, v4))
			}
			changed = true
		}
	}
	if in.Notes.Set {
		n, _ := set(in.Notes)
		n, err := checkText("notes", n, MaxNotes, true)
		if err != nil {
			return nil, false, err
		}
		if n != cur.Notes {
			if strings.TrimSpace(n) == "" {
				d.Remove(d.Find("NOTE")...)
			} else {
				d.Set("NOTE", card.EscapeText(n))
			}
			changed = true
		}
	}
	if nameChanged || org != cur.Org || d.First("FN") == nil {
		fn := strings.TrimSpace(strings.Join(nonEmpty(first, last), " "))
		after := view(d)
		switch {
		case fn != "":
		case org != "":
			fn = org
		case len(after.Emails) > 0:
			fn = after.Emails[0].Value
		case len(after.Phones) > 0:
			fn = after.Phones[0].Value
		}
		if fn == "" {
			return nil, false, invalid("a contact needs a name, a company, an e-mail address or a number")
		}
		if l := d.First("FN"); l == nil || card.UnescapeText(l.Value) != fn {
			d.Set("FN", card.EscapeText(fn))
			changed = true
		}
	}
	if !changed {
		return data, false, nil
	}
	d.Set("REV", now.UTC().Format("20060102T150405Z"))
	return d.Bytes(), true, nil
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, x := range s {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func birthdayLine(year int, month time.Month, day int, v4 bool) *card.Line {
	switch {
	case v4 && year == 0:
		return &card.Line{Name: "BDAY", Value: fmt.Sprintf("--%02d%02d", month, day)}
	case v4:
		return &card.Line{Name: "BDAY", Value: fmt.Sprintf("%04d%02d%02d", year, month, day)}
	case year == 0:
		// What iOS writes for a birthday without a year.
		return &card.Line{Name: "BDAY", Params: []card.Param{{Name: "X-APPLE-OMIT-YEAR", Values: []string{"1604"}}},
			Value: fmt.Sprintf("1604-%02d-%02d", month, day)}
	}
	return &card.Line{Name: "BDAY", Params: []card.Param{{Name: "VALUE", Values: []string{"date"}}}, Value: fmt.Sprintf("%04d-%02d-%02d", year, month, day)}
}

func checkKind(t string, phone bool) error {
	switch t {
	case "home", "work", "other":
		return nil
	case "mobile":
		if phone {
			return nil
		}
	}
	return invalid("type %q unknown", t)
}

// typeValue is the TYPE of a kind ("" for other) in the case of the version.
func typeValue(t string, v4 bool) string {
	v := map[string]string{"mobile": "CELL", "home": "HOME", "work": "WORK"}[t]
	if v4 {
		return strings.ToLower(v)
	}
	return v
}

// retype gives a line another kind: the kind's TYPE values and Apple's
// label go, the other parameters stay.
func retype(d *card.Doc, l *card.Line, t string, v4 bool) {
	if lab := d.Companion(l, "X-ABLABEL"); lab != nil {
		d.Remove(lab)
	}
	var params []card.Param
	for _, p := range l.Params {
		if p.Name != "TYPE" {
			params = append(params, p)
			continue
		}
		var keep []string
		for _, v := range p.Values {
			if !kindTypes[strings.ToUpper(strings.TrimSpace(v))] {
				keep = append(keep, v)
			}
		}
		if len(keep) > 0 {
			params = append(params, card.Param{Name: "TYPE", Values: keep})
		}
	}
	if tv := typeValue(t, v4); tv != "" {
		params = append(params, card.Param{Name: "TYPE", Values: []string{tv}})
	}
	l.Params = params
	l.Touch()
}

// editValues makes the TEL or EMAIL lines match the list: a line whose
// value is in the list stays (retyped when its kind changed), others go,
// new values come as new lines.
func editValues(d *card.Doc, name string, list []Value, phone, v4 bool) (bool, error) {
	if len(list) > MaxValues {
		return false, invalid("at most %d values", MaxValues)
	}
	lines := d.Find(name)
	used := map[*card.Line]bool{}
	changed := false
	var add []*card.Line
	for _, v := range list {
		val, err := checkText(strings.ToLower(name), v.Value, MaxValue, false)
		if err != nil {
			return false, err
		}
		if err := checkKind(v.Type, phone); err != nil {
			return false, err
		}
		if val == "" {
			continue
		}
		var match *card.Line
		for _, l := range lines {
			cur := strings.TrimSpace(card.UnescapeText(l.Value))
			if phone {
				cur = telValue(l.Value)
			}
			if !used[l] && cur == val {
				match = l
				break
			}
		}
		if match != nil {
			used[match] = true
			if t, _ := kindOf(d, match, phone); t != v.Type {
				retype(d, match, v.Type, v4)
				changed = true
			}
			continue
		}
		l := &card.Line{Name: name, Value: card.EscapeText(val)}
		if name == "EMAIL" && !v4 {
			l.Params = append(l.Params, card.Param{Name: "TYPE", Values: []string{"INTERNET"}})
		}
		if tv := typeValue(v.Type, v4); tv != "" {
			l.Params = append(l.Params, card.Param{Name: "TYPE", Values: []string{tv}})
		}
		add = append(add, l)
	}
	var gone []*card.Line
	for _, l := range lines {
		if !used[l] {
			gone = append(gone, l)
		}
	}
	if len(gone) > 0 {
		d.Remove(gone...)
		changed = true
	}
	for _, l := range add {
		d.Add(l)
		changed = true
	}
	return changed, nil
}

func editAddresses(d *card.Doc, list []Address, v4 bool) (bool, error) {
	if len(list) > MaxValues {
		return false, invalid("at most %d addresses", MaxValues)
	}
	lines := d.Find("ADR")
	used := map[*card.Line]bool{}
	changed := false
	var add []*card.Line
	for _, a := range list {
		if err := checkKind(a.Type, false); err != nil {
			return false, err
		}
		for _, f := range []*string{&a.Street, &a.City, &a.Postcode, &a.Region, &a.Country} {
			v, err := checkText("address", *f, MaxValue, f == &a.Street)
			if err != nil {
				return false, err
			}
			*f = strings.TrimSpace(v)
		}
		if a.key() == ([5]string{}) {
			continue
		}
		var match *card.Line
		for _, l := range lines {
			if !used[l] && addressOf(l, "", "").key() == a.key() {
				match = l
				break
			}
		}
		if match != nil {
			used[match] = true
			if t, _ := kindOf(d, match, false); t != a.Type {
				retype(d, match, a.Type, v4)
				changed = true
			}
			continue
		}
		l := &card.Line{Name: "ADR", Value: card.JoinStructured([]string{"", "", a.Street, a.City, a.Region, a.Postcode, a.Country})}
		if tv := typeValue(a.Type, v4); tv != "" {
			l.Params = []card.Param{{Name: "TYPE", Values: []string{tv}}}
		}
		add = append(add, l)
	}
	var gone []*card.Line
	for _, l := range lines {
		if !used[l] {
			gone = append(gone, l)
		}
	}
	if len(gone) > 0 {
		d.Remove(gone...)
		changed = true
	}
	for _, l := range add {
		d.Add(l)
		changed = true
	}
	return changed, nil
}

// SetPhoto puts a JPEG into the card (PHOTO;ENCODING=b;TYPE=JPEG in vCard
// 3, a data: URI in 4) or, with nil, removes the picture; REV now.
func SetPhoto(data, jpeg []byte, now time.Time) ([]byte, error) {
	d, err := card.ParseDoc(data)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if jpeg == nil {
		if len(d.Find("PHOTO")) == 0 {
			return data, nil
		}
		d.Remove(d.Find("PHOTO")...)
	} else {
		d.SetPhoto(jpeg)
	}
	d.Set("REV", now.UTC().Format("20060102T150405Z"))
	return d.Bytes(), nil
}

// Photo is the inline picture of a card (never one by reference).
func Photo(data []byte) ([]byte, bool) {
	d, err := card.ParseDoc(data)
	if err != nil {
		return nil, false
	}
	return d.PhotoData()
}
