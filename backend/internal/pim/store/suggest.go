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

// PhotoAddresses maps the e-mail addresses (lower case) of the account's
// contacts that have a picture to the entry's name, for the sender
// pictures of T-Mail (1.12). An address of two contacts goes with the one
// first by sort key. At most limit addresses; from RAM after the first
// read, empty without a volume or for an unknown account.
func (s *Store) PhotoAddresses(account string, limit int) map[string]string {
	out := map[string]string{}
	if limit <= 0 {
		return out
	}
	acc, err := s.accountFor(Contacts, account)
	if err != nil {
		return out
	}
	type found struct{ addr, name, sortKey string }
	var list []found
	for _, c := range acc.collections() {
		c.mu.Lock()
		if !c.deleted {
			for name, e := range c.st.Index {
				if e.Group || !e.HasPhoto {
					continue
				}
				for _, addr := range e.Emails {
					if addr = strings.ToLower(strings.TrimSpace(addr)); addr != "" {
						list = append(list, found{addr, name, e.SortKey})
					}
				}
			}
		}
		c.mu.Unlock()
	}
	slices.SortFunc(list, func(a, b found) int {
		return cmp.Or(cmp.Compare(a.sortKey, b.sortKey), cmp.Compare(a.name, b.name), cmp.Compare(a.addr, b.addr))
	})
	for _, f := range list {
		if _, ok := out[f.addr]; ok {
			continue
		}
		if len(out) == limit {
			break
		}
		out[f.addr] = f.name
	}
	return out
}
