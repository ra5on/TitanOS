package cardweb

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// Item is a contact in the list, from the index only (no disk).
type Item struct {
	ID       string `json:"id"`
	Book     string `json:"book"`
	Name     string `json:"name"`
	SortKey  string `json:"sortKey"`
	Org      string `json:"org"`
	Initials string `json:"initials"`
	// Email is the first e-mail address (the second line of a row when
	// the company says nothing new).
	Email    string `json:"email,omitempty"`
	HasPhoto bool   `json:"hasPhoto"`
	// BirthdaySoon: the birthday falls into this week (Monday to Sunday in
	// the zone of the NAS, days gone included); the page words it.
	BirthdaySoon *SoonBirthday `json:"birthdaySoon,omitempty"`
}

// SoonBirthday is the day of this week's birthday and the age reached.
type SoonBirthday struct {
	Date string `json:"date"`
	Age  *int   `json:"age,omitempty"`
}

// Fold is the sort and search form of a text: lower case, accents and
// umlauts on their base letter, ß as ss (DIN 5007-1: ä sorts as a). The
// collation tables of x/text/collate would cost titand 1.5 MB; this uses
// what titand links anyway.
func Fold(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == 'ß':
			b.WriteString("ss")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Initials are the first letters of first and last word of the name, one
// letter for a company.
func Initials(fn, org string) string {
	words := strings.Fields(fn)
	if len(words) == 0 {
		return "?"
	}
	first := func(s string) string {
		for _, r := range s {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return strings.ToUpper(string(r))
			}
		}
		return ""
	}
	if fn == org || len(words) == 1 {
		if s := first(words[0]); s != "" {
			return s
		}
		return "?"
	}
	s := first(words[0]) + first(words[len(words)-1])
	if s == "" {
		return "?"
	}
	return s
}

// WeekOf is Monday 00:00 of the week of now and the Monday after (the
// week of birthdaySoon).
func WeekOf(now time.Time, local *time.Location) (time.Time, time.Time) { return weekOf(now, local) }

// weekOf is Monday 00:00 of the week of now and the Monday after.
func weekOf(now time.Time, local *time.Location) (time.Time, time.Time) {
	t := now.In(local)
	back := (int(t.Weekday()) + 6) % 7
	mon := time.Date(t.Year(), t.Month(), t.Day()-back, 0, 0, 0, 0, local)
	return mon, mon.AddDate(0, 0, 7)
}

// List makes the rows of the address book: groups never, q matching
// (Matches), sorted by the folded sort key (last name, first name; for a
// company its name), then name and id.
func List(book string, objects []pimstore.ObjectInfo, q string, now time.Time, local *time.Location) []Item {
	return NewListIndex(book, objects).List(q, now, local)
}

// ListIndex is the address book ready for its list: rows sorted, the
// texts of the search folded and the birthdays parsed once (1.13). The
// pages ask for the list again and again; the book changes rarely, so
// the API keeps one per ctag.
type ListIndex struct {
	book      string
	rows      []listRow
	birthdays *calweb.BirthdayIndex
}

type listRow struct {
	item Item
	// The folded texts Matches compares.
	fn, org string
	emails  []string // lower case
	adr     []string
	phones  []string // without a leading +
}

// NewListIndex prepares the rows of one book.
func NewListIndex(book string, objects []pimstore.ObjectInfo) *ListIndex {
	ix := &ListIndex{book: book, birthdays: calweb.NewBirthdayIndex(objects)}
	type sorted struct {
		listRow
		fold, name string
	}
	rows := make([]sorted, 0, len(objects))
	for _, o := range objects {
		if o.Group {
			continue
		}
		it := Item{ID: o.Name, Book: book, Name: o.FN, SortKey: o.SortKey, Org: o.Org, Initials: Initials(o.FN, o.Org), HasPhoto: o.HasPhoto}
		if len(o.Emails) > 0 {
			it.Email = o.Emails[0]
		}
		r := listRow{item: it, fn: Fold(o.FN), org: Fold(o.Org)}
		for _, v := range o.Emails {
			r.emails = append(r.emails, strings.ToLower(v))
		}
		for _, v := range o.Addresses {
			r.adr = append(r.adr, Fold(v))
		}
		for _, p := range o.Phones {
			r.phones = append(r.phones, strings.TrimPrefix(p, "+"))
		}
		rows = append(rows, sorted{r, Fold(o.SortKey), r.fn})
	}
	slices.SortFunc(rows, func(a, b sorted) int {
		return cmp.Or(strings.Compare(a.fold, b.fold), strings.Compare(a.name, b.name), strings.Compare(a.item.ID, b.item.ID))
	})
	ix.rows = make([]listRow, len(rows))
	for i, r := range rows {
		ix.rows[i] = r.listRow
	}
	return ix
}

// Len is the number of contacts (groups not counted).
func (ix *ListIndex) Len() int { return len(ix.rows) }

// Birthdays are the parsed birthdays of the book.
func (ix *ListIndex) Birthdays() *calweb.BirthdayIndex { return ix.birthdays }

// List makes the rows matching q, with this week's birthdays (List).
func (ix *ListIndex) List(q string, now time.Time, local *time.Location) []Item {
	from, to := weekOf(now, local)
	soon := map[string]SoonBirthday{}
	for _, b := range ix.birthdays.Between(from, to, local) {
		s := SoonBirthday{Date: b.Start}
		if b.Birthday != nil && b.Birthday.Age != nil {
			s.Age = b.Birthday.Age
		}
		soon[b.ID] = s
	}
	query := NewQuery(q)
	out := []Item{}
	for i := range ix.rows {
		r := &ix.rows[i]
		if !query.matchesRow(r) {
			continue
		}
		it := r.item
		if s, ok := soon[it.ID]; ok {
			it.BirthdaySoon = &s
		}
		out = append(out, it)
	}
	return out
}

// Query is a search of the list.
type Query struct {
	text   string
	phones []string
}

// NewQuery reads q: text over name, company, e-mail addresses and postal
// addresses (folded, contained); a q of digits (and + - / ( ) . space)
// with at least three digits also over the numbers, digits compared:
// the query without leading zeros, and without + or 00 and a country code
// of 1 to 3 digits, is contained in a number without its +. So "0170 12"
// finds "+49 170 1234567" and "+49 170" finds "0170 1234567".
func NewQuery(q string) Query {
	q = strings.TrimSpace(q)
	out := Query{text: Fold(q)}
	digits := ""
	phoneLike := true
	for _, r := range q {
		switch {
		case r >= '0' && r <= '9':
			digits += string(r)
		case strings.ContainsRune("+-/(). ", r):
		default:
			phoneLike = false
		}
	}
	if !phoneLike || len(digits) < 3 {
		return out
	}
	international := strings.HasPrefix(q, "+") || strings.HasPrefix(digits, "00")
	add := func(s string) {
		if len(s) >= 3 && !slices.Contains(out.phones, s) {
			out.phones = append(out.phones, s)
		}
	}
	add(strings.TrimLeft(digits, "0"))
	if international {
		rest := strings.TrimLeft(digits, "0")
		for cc := 1; cc <= 3 && len(rest)-cc >= 3; cc++ {
			add("0" + rest[cc:])
			if len(rest)-cc >= 6 {
				add(rest[cc:])
			}
		}
	}
	return out
}

// matchesRow is Matches on the folded texts of a row.
func (q Query) matchesRow(r *listRow) bool {
	if q.text == "" {
		return true
	}
	if strings.Contains(r.fn, q.text) || strings.Contains(r.org, q.text) {
		return true
	}
	for _, v := range r.emails {
		if strings.Contains(v, q.text) {
			return true
		}
	}
	for _, v := range r.adr {
		if strings.Contains(v, q.text) {
			return true
		}
	}
	for _, p := range r.phones {
		for _, x := range q.phones {
			if strings.Contains(p, x) {
				return true
			}
		}
	}
	return false
}

// Matches reports whether an entry of the index matches (an empty query
// matches all).
func (q Query) Matches(e pimstore.Entry) bool {
	if q.text == "" {
		return true
	}
	if strings.Contains(Fold(e.FN), q.text) || strings.Contains(Fold(e.Org), q.text) {
		return true
	}
	for _, v := range e.Emails {
		if strings.Contains(strings.ToLower(v), q.text) {
			return true
		}
	}
	for _, v := range e.Addresses {
		if strings.Contains(Fold(v), q.text) {
			return true
		}
	}
	for _, p := range e.Phones {
		p = strings.TrimPrefix(p, "+")
		for _, x := range q.phones {
			if strings.Contains(p, x) {
				return true
			}
		}
	}
	return false
}
