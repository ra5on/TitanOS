package store

import (
	"cmp"
	"slices"
	"strings"
)

// Suggestion is an e-mail address of a contact, for the address field of
// T-Mail (TECHNIK 7.4): the name is FN.
type Suggestion struct {
	Name string
	Addr string
}

// SuggestAddresses lists the e-mail addresses of the account's contacts
// whose name (FN) or address contains q (case does not matter; an empty q
// matches all): one entry per address, group vCards left out. Those whose
// name has a word, or whose address, starting with q come first, then by
// the sort key of the contact; an address of two contacts comes once, with
// the first. At most limit.
//
// It answers from RAM after the first read of the address book and makes
// nothing on the volume. Without a volume, on another volume, or for an
// unknown account it is empty: T-Mail goes on with its learned addresses.
func (s *Store) SuggestAddresses(account, q string, limit int) []Suggestion {
	if limit <= 0 {
		return nil
	}
	acc, err := s.accountFor(Contacts, account)
	if err != nil {
		return nil
	}
	q = strings.ToLower(strings.TrimSpace(q))
	type found struct {
		Suggestion
		rank    int
		sortKey string
	}
	var list []found
	for _, c := range acc.collections() {
		c.mu.Lock()
		if !c.deleted {
			for _, e := range c.st.Index {
				if e.Group || len(e.Emails) == 0 {
					continue
				}
				name := strings.ToLower(e.FN)
				for _, addr := range e.Emails {
					addr = strings.TrimSpace(addr)
					key := strings.ToLower(addr)
					if addr == "" || q != "" && !strings.Contains(name, q) && !strings.Contains(key, q) {
						continue
					}
					rank := 1
					if q == "" || strings.HasPrefix(key, q) || wordStarts(name, q) {
						rank = 0
					}
					list = append(list, found{Suggestion{Name: e.FN, Addr: addr}, rank, e.SortKey})
				}
			}
		}
		c.mu.Unlock()
	}
	slices.SortFunc(list, func(a, b found) int {
		return cmp.Or(cmp.Compare(a.rank, b.rank), cmp.Compare(a.sortKey, b.sortKey),
			cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)), cmp.Compare(strings.ToLower(a.Addr), strings.ToLower(b.Addr)))
	})
	// An address of two contacts goes with the first in this order.
	out := []Suggestion{}
	seen := map[string]bool{}
	for _, f := range list {
		key := strings.ToLower(f.Addr)
		if seen[key] {
			continue
		}
		seen[key] = true
		if out = append(out, f.Suggestion); len(out) == limit {
			break
		}
	}
	return out
}

// wordStarts reports whether a word of s starts with prefix.
func wordStarts(s, prefix string) bool {
	for _, w := range strings.Fields(s) {
		if strings.HasPrefix(w, prefix) {
			return true
		}
	}
	return false
}
