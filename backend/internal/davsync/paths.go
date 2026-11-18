package davsync

import (
	"net/url"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// level is the kind of a path.
type level int

const (
	levelRoot       level = iota // /caldav/
	levelPrincipal               // /caldav/<account>/
	levelHome                    // /caldav/<account>/calendars/
	levelCollection              // /caldav/<account>/calendars/<collection>/
	levelEntry                   // /caldav/<account>/calendars/<collection>/<name>.ics
)

// levels is a set of levels.
type levels uint8

func on(ls ...level) levels {
	var s levels
	for _, l := range ls {
		s |= 1 << l
	}
	return s
}

func (s levels) has(l level) bool { return s&(1<<l) != 0 }

var allLevels = on(levelRoot, levelPrincipal, levelHome, levelCollection, levelEntry)

// target is a parsed path. account, collection and name are decoded.
type target struct {
	kind       store.Kind
	level      level
	account    string
	collection string
	name       string
}

// homeSegment is the name of the home set below the principal.
func homeSegment(kind store.Kind) string {
	if kind == store.Contacts {
		return "addressbooks"
	}
	return "calendars"
}

func prefix(kind store.Kind) string {
	if kind == store.Contacts {
		return CardDAVPrefix
	}
	return CalDAVPrefix
}

// parsePath parses the escaped path of a request or href. Each segment is
// decoded exactly once; a segment that is empty (but the last), "." or
// "..", or that decodes to hold "/", is no path of davsync. Names are
// checked with the rules of the store; the last segment of an entry has
// no trailing slash.
func parsePath(escaped string) (target, bool) {
	if !strings.HasPrefix(escaped, "/") {
		return target{}, false
	}
	parts := strings.Split(escaped[1:], "/")
	if n := len(parts); n > 1 && parts[n-1] == "" {
		parts = parts[:n-1]
	}
	segs := make([]string, len(parts))
	for i, p := range parts {
		s, err := url.PathUnescape(p)
		if err != nil || s == "" || s == "." || s == ".." || strings.Contains(s, "/") {
			return target{}, false
		}
		segs[i] = s
	}
	var t target
	switch segs[0] {
	case CalDAVPrefix[1:]:
		t.kind = store.Calendar
	case CardDAVPrefix[1:]:
		t.kind = store.Contacts
	default:
		return target{}, false
	}
	trailing := strings.HasSuffix(escaped, "/")
	switch len(segs) {
	case 1:
		t.level = levelRoot
	case 2:
		t.level = levelPrincipal
	case 3:
		t.level = levelHome
	case 4:
		t.level = levelCollection
	case 5:
		t.level = levelEntry
		if trailing {
			return target{}, false
		}
	default:
		return target{}, false
	}
	if len(segs) > 1 {
		t.account = segs[1]
		if !store.ValidAccount(t.account) {
			return target{}, false
		}
	}
	if len(segs) > 2 && segs[2] != homeSegment(t.kind) {
		return target{}, false
	}
	if len(segs) > 3 {
		t.collection = segs[3]
		if !store.ValidCollection(t.collection) {
			return target{}, false
		}
	}
	if len(segs) > 4 {
		t.name = segs[4]
		if !store.ValidName(t.kind, t.name) {
			return target{}, false
		}
	}
	return t, true
}

// href is the canonical path of a target: segments encoded, collections
// with a trailing slash.
func (t target) href() string {
	b := prefix(t.kind) + "/"
	if t.level == levelRoot {
		return b
	}
	b += escape(t.account) + "/"
	if t.level == levelPrincipal {
		return b
	}
	b += homeSegment(t.kind) + "/"
	if t.level == levelHome {
		return b
	}
	b += escape(t.collection) + "/"
	if t.level == levelCollection {
		return b
	}
	return b + escape(t.name)
}

// at returns the target of another level of the same account.
func (t target) at(l level) target {
	out := target{kind: t.kind, level: l, account: t.account}
	if l >= levelCollection {
		out.collection = t.collection
	}
	if l == levelEntry {
		out.name = t.name
	}
	return out
}

// principal is the href of the account's principal of the kind.
func principalHref(kind store.Kind, account string) string {
	return target{kind: kind, level: levelPrincipal, account: account}.href()
}

func homeHref(kind store.Kind, account string) string {
	return target{kind: kind, level: levelHome, account: account}.href()
}

// escape encodes one path segment (also "/", so a name stays one segment).
func escape(s string) string { return url.PathEscape(s) }

// entryOf resolves an href of a multiget (a path or an absolute URL) to
// the name of an entry in the collection c: the same kind, account and
// collection exactly, one name segment. Anything else is "not here"
// (404 for that href); nothing outside the collection is ever read.
func entryOf(c target, href string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(href))
	if err != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	t, ok := parsePath(u.EscapedPath())
	if !ok || t.level != levelEntry || t.kind != family(c.kind) || t.account != c.account || t.collection != c.collection {
		return "", false
	}
	return t.name, true
}
