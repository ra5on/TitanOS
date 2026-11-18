package calweb

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

// BirthdaysID is the virtual calendar of the contacts' birthdays: only in
// TitanOS (never under /caldav/, iOS makes its own), read-only, computed
// from the index of the contacts on every query (TECHNIK 5.6).
const BirthdaysID = "geburtstage"

// omitYear is the year iOS writes for a birthday without year
// (X-APPLE-OMIT-YEAR=1604); the index keeps only the value.
const omitYear = 1604

// ParseBDay reads a BDAY value: 1991-11-12, 19911112, a date-time (its
// date counts), --1112 or --11-12 (vCard 4 without year). year is 0
// without a year (also for 1604, iOS).
func ParseBDay(raw string) (year int, month time.Month, day int, ok bool) {
	s := strings.TrimSpace(raw)
	if i := strings.IndexAny(s, "Tt"); i >= 0 {
		s = s[:i]
	}
	noYear := strings.HasPrefix(s, "--")
	s = strings.ReplaceAll(strings.TrimPrefix(s, "--"), "-", "")
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, 0, 0, false
		}
	}
	switch {
	case noYear && len(s) == 4:
	case !noYear && len(s) == 8:
		year, _ = strconv.Atoi(s[:4])
		s = s[4:]
	default:
		return 0, 0, 0, false
	}
	m, _ := strconv.Atoi(s[:2])
	d, _ := strconv.Atoi(s[2:])
	if m < 1 || m > 12 || d < 1 || d > time.Date(2000, time.Month(m)+1, 0, 0, 0, 0, 0, time.UTC).Day() {
		return 0, 0, 0, false
	}
	if year == omitYear || year < 1 {
		year = 0
	}
	return year, time.Month(m), d, true
}

// Birthdays are the birthdays of the contacts (groups left out) that fall
// into [from, to), as all-day occurrences in the zone of the NAS. 29
// February shows on 28 February in other years.
func Birthdays(contacts []pimstore.ObjectInfo, from, to time.Time, local *time.Location) []View {
	var out []View
	first, last := from.In(local).Year(), to.In(local).Year()
	for _, c := range contacts {
		if c.Group || c.BDay == "" {
			continue
		}
		year, month, day, ok := ParseBDay(c.BDay)
		if !ok {
			continue
		}
		for y := first; y <= last; y++ {
			if year > 0 && y < year {
				continue
			}
			d := day
			if month == time.February && d == 29 && time.Date(y, 3, 0, 0, 0, 0, 0, time.UTC).Day() != 29 {
				d = 28
			}
			start := time.Date(y, month, d, 0, 0, 0, 0, local)
			end := start.AddDate(0, 0, 1)
			if !start.Before(to) || !end.After(from) {
				continue
			}
			b := &Birthday{Name: c.FN}
			if year > 0 {
				age := y - year
				b.Year, b.Age = year, &age
			}
			out = append(out, View{Kind: "birthday", ID: c.Name, Occurrence: start.Format("20060102"), Calendar: BirthdaysID,
				UID: c.UID, Title: c.FN, Start: start.Format(dateLayout), End: end.Format(dateLayout), AllDay: true,
				Recurring: true, Alarms: []int{}, ETag: c.ETag, ReadOnly: true, Birthday: b})
		}
	}
	slices.SortStableFunc(out, func(a, b View) int {
		return cmp.Or(strings.Compare(a.Start, b.Start), strings.Compare(a.Title, b.Title))
	})
	return out
}

// CountBirthdays is the number of contacts with a readable birthday.
func CountBirthdays(contacts []pimstore.ObjectInfo) int {
	n := 0
	for _, c := range contacts {
		if _, _, _, ok := ParseBDay(c.BDay); ok && !c.Group {
			n++
		}
	}
	return n
}

// CalendarID makes the id of a new calendar from its name ([a-z0-9-],
// umlauts spelled out, at most 56 characters so a number fits behind);
// taken reports ids in use (BirthdaysID always is). An id no name gives
// is "kalender".
func CalendarID(name string, taken func(string) bool) string {
	var b strings.Builder
	dash := false
	for _, r := range norm.NFD.String(strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "Ä", "ae", "Ö", "oe", "Ü", "ue", "ß", "ss").Replace(name)) {
		r = unicode.ToLower(r)
		switch {
		case unicode.Is(unicode.Mn, r):
			continue
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
		if b.Len() >= 56 {
			break
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "kalender"
	}
	used := func(id string) bool { return id == BirthdaysID || taken(id) }
	if !used(base) {
		return base
	}
	for n := 2; n < 1000; n++ {
		if id := base + "-" + strconv.Itoa(n); !used(id) {
			return id
		}
	}
	return base + "-" + NewUID()[:8]
}
