package cardweb

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/card"
)

// Groups of contacts (1.13, owner 18 Nov 2026): a group is a vCard of its
// own in the address book, the way iOS and iCloud keep them over CardDAV –
// vCard 3.0 with X-ADDRESSBOOKSERVER-KIND:group and one
// X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:<UID> per member
// (davsync/testdata/ios26/fixtures/group-family.vcf). vCard 4.0 groups
// (KIND:group, MEMBER) are read as well. Edits touch only the member
// lines, FN/N (a new name) and REV; a member TitanOS does not know (a
// UID of no contact here, a MEMBER:mailto:) stays as it is.

// MaxMembers bounds the members a group takes from the web page.
const MaxMembers = 5000

// GroupCard is a group as read from its card.
type GroupCard struct {
	Name string
	// UIDs are the members named by a UID (urn:uuid:…, or a bare value),
	// in the order of the card, each once.
	UIDs []string
	// Other counts member lines that name no UID (mailto:, other URIs).
	Other int
}

// isGroupDoc reports whether a card is a group (either form).
func isGroupDoc(d *card.Doc) bool {
	for _, name := range []string{"X-ADDRESSBOOKSERVER-KIND", "KIND"} {
		if l := d.First(name); l != nil && strings.EqualFold(strings.TrimSpace(card.UnescapeText(l.Value)), "group") {
			return true
		}
	}
	return false
}

// memberLines are the member lines of either form.
func memberLines(d *card.Doc) []*card.Line {
	var out []*card.Line
	for _, l := range d.Lines {
		if l.Name == "X-ADDRESSBOOKSERVER-MEMBER" || l.Name == "MEMBER" {
			out = append(out, l)
		}
	}
	return out
}

// MemberUID is the UID a member value names: urn:uuid:<UID>, or a bare
// value without a scheme (some clients write the UID alone); "" for
// another URI (mailto:, http:, a urn of another kind).
func MemberUID(value string) string {
	v := strings.TrimSpace(card.UnescapeText(value))
	if rest, ok := cutFold(v, "urn:uuid:"); ok {
		return strings.TrimSpace(rest)
	}
	if v == "" || strings.Contains(v, ":") {
		return ""
	}
	return v
}

func cutFold(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return s, false
}

// ReadGroup reads a group card; ErrInvalid when the card is no group.
func ReadGroup(data []byte) (GroupCard, error) {
	d, err := card.ParseDoc(data)
	if err != nil {
		return GroupCard{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if !isGroupDoc(d) {
		return GroupCard{}, invalid("not a group")
	}
	g := GroupCard{}
	if l := d.First("FN"); l != nil {
		g.Name = strings.TrimSpace(card.UnescapeText(l.Value))
	}
	if g.Name == "" {
		if l := d.First("N"); l != nil {
			g.Name = strings.TrimSpace(card.Structured(l.Value)[0])
		}
	}
	for _, l := range memberLines(d) {
		uid := MemberUID(l.Value)
		switch {
		case uid == "":
			g.Other++
		case !slices.Contains(g.UIDs, uid):
			g.UIDs = append(g.UIDs, uid)
		}
	}
	return g, nil
}

// checkGroupName bounds the name of a group (one line, not empty).
func checkGroupName(name string) (string, error) {
	n, err := checkText("name", name, MaxName, false)
	if err != nil {
		return "", err
	}
	if n == "" {
		return "", invalid("a group needs a name")
	}
	return n, nil
}

// BuildGroup makes a new group in the form iOS writes: vCard 3.0, N and FN
// the name, X-ADDRESSBOOKSERVER-KIND:group, a member line per UID.
func BuildGroup(name, uid string, members []string, now time.Time) ([]byte, error) {
	n, err := checkGroupName(name)
	if err != nil {
		return nil, err
	}
	if len(members) > MaxMembers {
		return nil, invalid("at most %d members", MaxMembers)
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\nPRODID:" + ProdID + "\r\n")
	b.WriteString("N:" + card.JoinStructured([]string{n, "", "", "", ""}) + "\r\n")
	b.WriteString("FN:" + card.EscapeText(n) + "\r\n")
	b.WriteString("X-ADDRESSBOOKSERVER-KIND:group\r\n")
	seen := map[string]bool{}
	for _, m := range members {
		if m = strings.TrimSpace(m); m == "" || seen[m] {
			continue
		}
		seen[m] = true
		b.WriteString("X-ADDRESSBOOKSERVER-MEMBER:urn:uuid:" + card.EscapeText(m) + "\r\n")
	}
	b.WriteString("REV:" + now.UTC().Format("20060102T150405Z") + "\r\n")
	b.WriteString("UID:" + card.EscapeText(uid) + "\r\nEND:VCARD\r\n")
	d, err := card.ParseDoc([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	// Written again through Doc: long lines folded like every other card.
	for _, l := range d.Lines {
		l.Touch()
	}
	return d.Bytes(), nil
}

// GroupEdit is a change of a group: a new name, members added and taken
// out (UIDs).
type GroupEdit struct {
	Name   *string
	Add    []string
	Remove []string
}

// EditGroup applies a change on the lines of a group card: FN (and the
// first part of N) for a name, member lines for the members in the form
// the card uses (MEMBER in a vCard 4.0 with KIND:group, else
// X-ADDRESSBOOKSERVER-MEMBER), REV now. Other lines – members TitanOS does
// not know included – stay byte for byte. Nothing changed: the old bytes
// and false.
func EditGroup(data []byte, e GroupEdit, now time.Time) ([]byte, bool, error) {
	d, err := card.ParseDoc(data)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if !isGroupDoc(d) {
		return nil, false, invalid("not a group")
	}
	if len(e.Add)+len(e.Remove) > MaxMembers {
		return nil, false, invalid("at most %d members", MaxMembers)
	}
	changed := false
	if e.Name != nil {
		n, err := checkGroupName(*e.Name)
		if err != nil {
			return nil, false, err
		}
		cur := ""
		if l := d.First("FN"); l != nil {
			cur = strings.TrimSpace(card.UnescapeText(l.Value))
		}
		if n != cur {
			d.Set("FN", card.EscapeText(n))
			if l := d.First("N"); l != nil {
				parts := card.Structured(l.Value)
				parts[0] = n
				d.Set("N", card.JoinStructured(parts))
			} else if d.Version() == "3.0" {
				d.Set("N", card.JoinStructured([]string{n, "", "", "", ""}))
			}
			changed = true
		}
	}
	remove := map[string]bool{}
	for _, uid := range e.Remove {
		if uid = strings.TrimSpace(uid); uid != "" {
			remove[uid] = true
		}
	}
	var gone []*card.Line
	have := map[string]bool{}
	for _, l := range memberLines(d) {
		uid := MemberUID(l.Value)
		if uid != "" && remove[uid] {
			gone = append(gone, l)
			continue
		}
		if uid != "" {
			have[uid] = true
		}
	}
	if len(gone) > 0 {
		d.Remove(gone...)
		changed = true
	}
	name := "X-ADDRESSBOOKSERVER-MEMBER"
	if d.Version() == "4.0" && d.First("X-ADDRESSBOOKSERVER-KIND") == nil {
		name = "MEMBER"
	}
	for _, uid := range e.Add {
		uid = strings.TrimSpace(uid)
		if uid == "" || have[uid] || remove[uid] {
			continue
		}
		if len(uid) > MaxValue || strings.ContainsAny(uid, "\r\n") {
			return nil, false, invalid("member UID too long")
		}
		have[uid] = true
		d.Add(&card.Line{Name: name, Value: "urn:uuid:" + card.EscapeText(uid)})
		changed = true
	}
	if !changed {
		return data, false, nil
	}
	d.Set("REV", now.UTC().Format("20060102T150405Z"))
	return d.Bytes(), true, nil
}
