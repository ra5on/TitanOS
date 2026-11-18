package cardweb

import (
	"cmp"
	"slices"
	"strings"
	"unicode"

	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// Possible duplicates (1.13, owner 18 Nov 2026): contacts of one address
// book that share an e-mail address, a phone number or a name. Computed
// on the index alone (no disk), so the page and later the agent's tool
// contacts_duplicates call it as it is. Conservative on purpose: it only
// proposes; merging is always the person's choice.

// Kinds of a duplicate match.
const (
	MatchEmail = "email"
	MatchPhone = "phone"
	MatchName  = "name"
)

// sharedLimit: a value shared by more contacts than this (the switchboard
// of a company, a family's landline, info@) is no sign of a duplicate.
const sharedLimit = 4

// DuplicateMatch is what two or more contacts of a set share.
type DuplicateMatch struct {
	Kind string `json:"kind"`
	// Value as the first contact has it (an address, a number as the index
	// keeps it, a name).
	Value string `json:"value"`
	// IDs are the contacts sharing it.
	IDs []string `json:"ids"`
}

// DuplicateSet is a group of contacts that may be one person: every two
// are joined by a chain of matches.
type DuplicateSet struct {
	IDs     []string         `json:"ids"`
	Matches []DuplicateMatch `json:"matches"`
}

// PhoneKeys are the forms a number is compared in: its digits without
// leading zeros, and for a number with + or 00 also without a country
// code of 1 to 3 digits (so +49 170 1234567 meets 0170 1234567). Numbers
// with fewer than 6 digits are left out (extensions, short codes).
func PhoneKeys(v string) []string {
	international := strings.HasPrefix(strings.TrimSpace(v), "+")
	var b strings.Builder
	for _, r := range v {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if strings.HasPrefix(digits, "00") {
		international = true
	}
	rest := strings.TrimLeft(digits, "0")
	if len(rest) < 6 {
		return nil
	}
	out := []string{rest}
	if international {
		for cc := 1; cc <= 3 && len(rest)-cc >= 6; cc++ {
			out = append(out, "cc:"+rest[cc:])
		}
	} else {
		// A national number meets the international one without its code.
		out = append(out, "cc:"+rest)
	}
	return out
}

// NameKey is the compared form of a name: folded, letters and digits only,
// its words sorted (so "Muster, Ben" meets "Ben Muster"); "" for a name of
// fewer than 3 letters.
func NameKey(name string) string {
	words := strings.FieldsFunc(Fold(name), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	slices.Sort(words)
	key := strings.Join(words, " ")
	if len([]rune(strings.ReplaceAll(key, " ", ""))) < 3 {
		return ""
	}
	return key
}

// Duplicates finds the possible duplicates among the entries of one
// address book (groups left out), each set sorted by name, the sets by
// their first name.
func Duplicates(objects []pimstore.ObjectInfo) []DuplicateSet {
	type contact struct {
		id, name string
	}
	var people []contact
	byID := map[string]int{}
	buckets := map[string][]int{} // kind + "\x00" + key → people
	values := map[string]string{} // kind + "\x00" + key → value shown
	add := func(kind, key, value string, i int) {
		k := kind + "\x00" + key
		if l := buckets[k]; len(l) > 0 && l[len(l)-1] == i {
			return
		}
		buckets[k] = append(buckets[k], i)
		if _, ok := values[k]; !ok {
			values[k] = value
		}
	}
	for _, o := range objects {
		if o.Group {
			continue
		}
		i := len(people)
		people = append(people, contact{id: o.Name, name: o.FN})
		byID[o.Name] = i
		for _, e := range o.Emails {
			if e = strings.ToLower(strings.TrimSpace(e)); strings.Contains(e, "@") {
				add(MatchEmail, e, e, i)
			}
		}
		for _, p := range o.Phones {
			for _, k := range PhoneKeys(p) {
				add(MatchPhone, k, p, i)
			}
		}
		if k := NameKey(o.FN); k != "" {
			add(MatchName, k, strings.TrimSpace(o.FN), i)
		}
	}

	// Union–find over the buckets that join 2…sharedLimit contacts.
	parent := make([]int, len(people))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	type joined struct {
		kind, value string
		members     []int
	}
	var joins []joined
	keys := make([]string, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		members := buckets[k]
		if len(members) < 2 || len(members) > sharedLimit {
			continue
		}
		kind, _, _ := strings.Cut(k, "\x00")
		joins = append(joins, joined{kind: kind, value: values[k], members: members})
		for _, m := range members[1:] {
			if a, b := find(members[0]), find(m); a != b {
				parent[b] = a
			}
		}
	}
	sets := map[int]*DuplicateSet{}
	for i := range people {
		root := find(i)
		s := sets[root]
		if s == nil {
			s = &DuplicateSet{}
			sets[root] = s
		}
		s.IDs = append(s.IDs, people[i].id)
	}
	byName := func(a, b string) int {
		pa, pb := people[byID[a]], people[byID[b]]
		return cmp.Or(strings.Compare(Fold(pa.name), Fold(pb.name)), strings.Compare(a, b))
	}
	for _, j := range joins {
		s := sets[find(j.members[0])]
		ids := make([]string, len(j.members))
		for n, m := range j.members {
			ids[n] = people[m].id
		}
		slices.SortFunc(ids, byName)
		// One match per kind and the same contacts (a number seen with and
		// without its country code counts once).
		dup := false
		for _, m := range s.Matches {
			if m.Kind == j.kind && slices.Equal(m.IDs, ids) {
				dup = true
				break
			}
		}
		if !dup {
			s.Matches = append(s.Matches, DuplicateMatch{Kind: j.kind, Value: j.value, IDs: ids})
		}
	}
	out := []DuplicateSet{}
	for _, s := range sets {
		if len(s.IDs) < 2 {
			continue
		}
		slices.SortFunc(s.IDs, byName)
		slices.SortFunc(s.Matches, func(a, b DuplicateMatch) int {
			return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Value, b.Value))
		})
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b DuplicateSet) int { return byName(a.IDs[0], b.IDs[0]) })
	return out
}
