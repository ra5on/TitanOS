package cardweb

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/card"
)

// Merging two contacts (1.13, owner 18 Nov 2026): one card stays (keep,
// its UID, file name and what devices know of it), the other is taken
// into it and then deleted by the caller. The fields the page shows come
// as the person chose them in the preview (an Input: names, company,
// birthday and note chosen, numbers, e-mail and postal addresses joined);
// everything else of the other card – what TitanOS does not show (an
// iPhone's own fields, social profiles, related names, dates, web
// addresses) – is taken over unless the kept card has it already.

// managedMerge are the properties the Input decides (or that belong to
// the card itself): never copied from the other card.
var managedMerge = map[string]bool{
	"BEGIN": true, "END": true, "VERSION": true, "PRODID": true, "UID": true, "REV": true,
	"FN": true, "N": true, "ORG": true, "TEL": true, "EMAIL": true, "ADR": true,
	"BDAY": true, "NOTE": true, "PHOTO": true, "KIND": true,
	"X-ADDRESSBOOKSERVER-KIND": true, "X-ADDRESSBOOKSERVER-MEMBER": true, "MEMBER": true,
}

// multiMerge are properties a card holds several of: the other card's
// lines join the kept card's. Any other property the kept card has wins.
var multiMerge = map[string]bool{
	"URL": true, "IMPP": true, "X-SOCIALPROFILE": true, "RELATED": true, "X-ABRELATEDNAMES": true,
	"X-ABDATE": true, "CATEGORIES": true, "NICKNAME": true, "X-AIM": true, "X-JABBER": true,
	"X-MSN": true, "X-YAHOO": true, "X-ICQ": true, "X-SKYPE": true, "X-TWITTER": true,
}

var itemGroup = regexp.MustCompile(`(?i)^item(\d+)$`)

// mainLine is the line of a group (item1.) that is not one of Apple's
// companions (X-AB…: label, address format); nil when there is none.
func mainLine(lines []*card.Line) *card.Line {
	for _, l := range lines {
		if !strings.HasPrefix(l.Name, "X-AB") || l.Name == "X-ABRELATEDNAMES" || l.Name == "X-ABDATE" {
			return l
		}
	}
	return nil
}

func sameLine(a, b *card.Line) bool {
	return a.Name == b.Name && strings.EqualFold(strings.TrimSpace(card.UnescapeText(a.Value)), strings.TrimSpace(card.UnescapeText(b.Value)))
}

// portable: a line of one version that the other version reads the same
// (no inline data in the form of the other version).
func portable(l *card.Line) bool {
	v := strings.ToLower(strings.TrimSpace(l.Value))
	return !l.HasParam("ENCODING") && !strings.HasPrefix(v, "data:")
}

// Merge takes the card drop into keep and applies the chosen fields. With
// photo "drop" the picture of drop replaces keep's (when drop has one);
// "keep" (or "") leaves keep's, "none" removes the picture.
func Merge(keep, drop []byte, in Input, photo string, now time.Time) ([]byte, error) {
	kd, err := card.ParseDoc(keep)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	dd, err := card.ParseDoc(drop)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if isGroupDoc(kd) || isGroupDoc(dd) {
		return nil, invalid("a group is not merged")
	}
	sameVersion := kd.Version() == dd.Version()

	// Lines without a group, then whole groups (item1.URL with its label).
	var plain []*card.Line
	groups := map[string][]*card.Line{}
	var groupOrder []string
	for _, l := range dd.Lines {
		if managedMerge[l.Name] && l.Group == "" {
			continue
		}
		if l.Group == "" {
			plain = append(plain, l)
			continue
		}
		g := strings.ToLower(l.Group)
		if _, ok := groups[g]; !ok {
			groupOrder = append(groupOrder, g)
		}
		groups[g] = append(groups[g], l)
	}
	has := func(l *card.Line) bool {
		for _, o := range kd.Lines {
			if sameLine(o, l) {
				return true
			}
		}
		return false
	}
	hasName := func(name string) bool { return kd.First(name) != nil }
	for _, l := range plain {
		if (!sameVersion && !portable(l)) || has(l) || (hasName(l.Name) && !multiMerge[l.Name]) {
			continue
		}
		kd.Insert(l.Copy(""))
	}
	// The next free itemN of the kept card.
	next := 0
	for _, l := range kd.Lines {
		if m := itemGroup.FindStringSubmatch(l.Group); m != nil {
			if n, _ := strconv.Atoi(m[1]); n > next {
				next = n
			}
		}
	}
	for _, g := range groupOrder {
		lines := groups[g]
		main := mainLine(lines)
		// A group with any line the merge decides (item1.TEL with its label,
		// but also item1.UID or item1.X-ADDRESSBOOKSERVER-MEMBER next to a
		// web address) is not taken over at all (unabhängige Durchsicht
		// 1.13, A-3).
		if main == nil || slices.ContainsFunc(lines, func(l *card.Line) bool { return managedMerge[l.Name] }) || has(main) || (!sameVersion && !portable(main)) {
			continue
		}
		if hasName(main.Name) && !multiMerge[main.Name] {
			continue
		}
		next++
		name := "item" + strconv.Itoa(next)
		for _, l := range lines {
			kd.Insert(l.Copy(name))
		}
	}

	switch photo {
	case "drop":
		if pic, ok := dd.PhotoData(); ok {
			setPicture(kd, pic)
		}
	case "none":
		kd.Remove(kd.Find("PHOTO")...)
	}
	kd.Set("REV", now.UTC().Format("20060102T150405Z"))
	out, _, err := Edit(kd.Bytes(), in, now)
	return out, err
}

// setPicture puts an inline picture of JPEG or PNG into a card, in the
// form of its version; another format becomes nothing.
func setPicture(d *card.Doc, pic []byte) {
	kind := ""
	switch {
	case bytes.HasPrefix(pic, []byte{0xff, 0xd8, 0xff}):
		kind = "JPEG"
	case bytes.HasPrefix(pic, []byte("\x89PNG\r\n\x1a\n")):
		kind = "PNG"
	default:
		return
	}
	if kind == "JPEG" {
		d.SetPhoto(pic)
		return
	}
	d.Remove(d.Find("PHOTO")...)
	b64 := base64.StdEncoding.EncodeToString(pic)
	if d.Version() == "4.0" {
		d.Add(&card.Line{Name: "PHOTO", Value: "data:image/png;base64," + b64})
		return
	}
	d.Add(&card.Line{Name: "PHOTO", Params: []card.Param{{Name: "ENCODING", Values: []string{"b"}}, {Name: "TYPE", Values: []string{"PNG"}}}, Value: b64})
}
