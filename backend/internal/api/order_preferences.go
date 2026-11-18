package api

import (
	"encoding/json"
	"regexp"
	"unicode"
	"unicode/utf8"

	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// 1.13 (Baustand step 4, owner 18 Nov 2026): the account's own order of
// the entries in the side bars of Mail, Kontakte and Kalender, the
// preference kind "order":
//
//	{"mail": {"<mailbox id>": ["INBOX", "Rechnungen", …]},
//	 "contacts": ["<address book id>", …],
//	 "calendar": ["<calendar id>", …]}
//
// Each list names entries in the account's order; the web UI puts entries
// it does not name (new folders, a new calendar) after them and ignores
// names that are gone. A PUT replaces only the keys it sends (null drops
// one), so that two programs open in two windows never undo each other's
// order. titand 1.12 knows no kind "order": it neither lists nor accepts
// it, and the file users/<name>/order.json does not disturb it.

const (
	// orderMailboxes is tmail.MaxMailboxes.
	orderMailboxes = 20
	// orderFolders is the most folders titand lists per mailbox (tmail maxFolders).
	orderFolders = 500
	// orderFolderBytes bounds one folder's path as IMAP names it.
	orderFolderBytes = 1024
	// orderCollections bounds the address books and calendars (and the birthdays).
	orderCollections = 100
)

// orderMailboxID is the form of a mailbox's id (tmail idRE).
var orderMailboxID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// orderKeys are the keys the kind holds.
var orderKeys = map[string]bool{"mail": true, "contacts": true, "calendar": true}

// validOrderPrefs checks a PUT of the kind "order": only the known keys,
// each null or of its shape, within the bounds above, no name twice.
func validOrderPrefs(value json.RawMessage) bool {
	var top map[string]json.RawMessage
	if json.Unmarshal(value, &top) != nil {
		return false
	}
	for key, raw := range top {
		if !orderKeys[key] {
			return false
		}
		if string(raw) == "null" {
			continue
		}
		if key == "mail" {
			var boxes map[string][]string
			if json.Unmarshal(raw, &boxes) != nil || boxes == nil || len(boxes) > orderMailboxes {
				return false
			}
			for id, folders := range boxes {
				if !orderMailboxID.MatchString(id) || !orderList(folders, orderFolders, validOrderFolder) {
					return false
				}
			}
			continue
		}
		var ids []string
		if json.Unmarshal(raw, &ids) != nil || !orderList(ids, orderCollections, pimstore.ValidCollection) {
			return false
		}
	}
	return true
}

// orderList checks a list: present, at most max names, each valid, none twice.
func orderList(list []string, max int, valid func(string) bool) bool {
	if list == nil || len(list) > max {
		return false
	}
	seen := make(map[string]bool, len(list))
	for _, name := range list {
		if seen[name] || !valid(name) {
			return false
		}
		seen[name] = true
	}
	return true
}

// validOrderFolder is a folder's path as titand lists it: UTF-8 of at most
// orderFolderBytes, without control characters.
func validOrderFolder(path string) bool {
	if path == "" || len(path) > orderFolderBytes || !utf8.ValidString(path) {
		return false
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// mergeOrderPrefs lays a valid PUT over what is stored: the keys sent
// replace theirs, null drops a key, the others stay. A damaged stored value
// counts as empty.
func mergeOrderPrefs(stored, put json.RawMessage) (json.RawMessage, error) {
	merged := map[string]json.RawMessage{}
	if stored != nil {
		if json.Unmarshal(stored, &merged) != nil || merged == nil {
			merged = map[string]json.RawMessage{}
		}
	}
	var change map[string]json.RawMessage
	if err := json.Unmarshal(put, &change); err != nil {
		return nil, err
	}
	for key, raw := range change {
		if string(raw) == "null" {
			delete(merged, key)
		} else {
			merged[key] = raw
		}
	}
	// Only known keys stay (an older, hand-edited or damaged file).
	for key := range merged {
		if !orderKeys[key] {
			delete(merged, key)
		}
	}
	return json.Marshal(merged)
}

// update reads, changes and writes one kind, one update at a time, so that
// two requests at once cannot drop one another's change.
func (p *preferences) update(user, kind string, change func(json.RawMessage) (json.RawMessage, error)) error {
	p.updateMu.Lock()
	defer p.updateMu.Unlock()
	current, err := p.get(user, kind)
	if err != nil {
		return err
	}
	next, err := change(current)
	if err != nil {
		return err
	}
	return p.set(user, kind, next)
}
