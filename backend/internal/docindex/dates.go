package docindex

import (
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DateAt is a date found in a text, with where it stands.
type DateAt struct {
	Time       time.Time
	Start, End int
	// NoYear: the text gave day and month only ("bis 31.12.").
	NoYear bool
}

var months = map[string]time.Month{
	"januar": 1, "jan": 1, "jänner": 1, "january": 1,
	"februar": 2, "feb": 2, "february": 2,
	"märz": 3, "maerz": 3, "mär": 3, "mrz": 3, "march": 3, "mar": 3,
	"april": 4, "apr": 4,
	"mai": 5, "may": 5,
	"juni": 6, "jun": 6, "june": 6,
	"juli": 7, "jul": 7, "july": 7,
	"august": 8, "aug": 8,
	"september": 9, "sept": 9, "sep": 9,
	"oktober": 10, "okt": 10, "october": 10, "oct": 10,
	"november": 11, "nov": 11,
	"dezember": 12, "dez": 12, "december": 12, "dec": 12,
}

const monthNames = `januar|jänner|january|jan|februar|february|feb|märz|maerz|march|mär|mrz|mar|april|apr|mai|may|juni|june|jun|juli|july|jul|august|aug|september|sept|sep|oktober|october|okt|oct|november|nov|dezember|december|dez|dec`

var (
	isoDate     = regexp.MustCompile(`\b(\d{4})-(\d{2})-(\d{2})\b`)
	numericDate = regexp.MustCompile(`\b(\d{1,2})\.\s?(\d{1,2})\.(?:\s?(\d{4}|\d{2})\b)?`)
	textDate    = regexp.MustCompile(`(?i)\b(\d{1,2})\.?\s+(` + monthNames + `)\.?(?:\s+(\d{4}))?\b`)
	englishDate = regexp.MustCompile(`(?i)\b(` + monthNames + `)\.?\s+(\d{1,2})(?:st|nd|rd|th)?,?\s+(\d{4})\b`)
)

// FindDates finds the dates of a text in German and English writing:
// 2026-11-03, 03.11.2026, 3.11.26, 31.12. (no year), 3. November 2026,
// 3 Nov 2026, November 3, 2026. Impossible dates are skipped.
func FindDates(text string, loc *time.Location) []DateAt {
	if loc == nil {
		loc = time.Local
	}
	var out []DateAt
	taken := func(s, e int) bool {
		for _, d := range out {
			if s < d.End && e > d.Start {
				return true
			}
		}
		return false
	}
	add := func(s, e, y, m, d int, noYear bool) {
		if taken(s, e) || m < 1 || m > 12 || d < 1 || d > 31 {
			return
		}
		if !noYear {
			if y < 100 {
				y += 2000
			}
			if y < 1950 || y > 2200 {
				return
			}
		} else {
			y = 2000 // a leap year, so 29.02. passes
		}
		t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, loc)
		if t.Day() != d {
			return
		}
		out = append(out, DateAt{Time: t, Start: s, End: e, NoYear: noYear})
	}
	atoi := func(s string) int { n, _ := strconv.Atoi(s); return n }
	for _, m := range isoDate.FindAllStringSubmatchIndex(text, -1) {
		add(m[0], m[1], atoi(text[m[2]:m[3]]), atoi(text[m[4]:m[5]]), atoi(text[m[6]:m[7]]), false)
	}
	for _, m := range textDate.FindAllStringSubmatchIndex(text, -1) {
		mon := months[strings.ToLower(text[m[4]:m[5]])]
		if m[6] < 0 {
			add(m[0], m[1], 0, int(mon), atoi(text[m[2]:m[3]]), true)
		} else {
			add(m[0], m[1], atoi(text[m[6]:m[7]]), int(mon), atoi(text[m[2]:m[3]]), false)
		}
	}
	for _, m := range englishDate.FindAllStringSubmatchIndex(text, -1) {
		mon := months[strings.ToLower(text[m[2]:m[3]])]
		add(m[0], m[1], atoi(text[m[6]:m[7]]), int(mon), atoi(text[m[4]:m[5]]), false)
	}
	for _, m := range numericDate.FindAllStringSubmatchIndex(text, -1) {
		// "1.284,37" or a version like 1.2.3 are no dates.
		if m[0] > 0 && (isDigit(text[m[0]-1]) || text[m[0]-1] == '.') {
			continue
		}
		if m[1] < len(text) && (isDigit(text[m[1]]) || text[m[1]] == ',') {
			continue
		}
		if m[6] < 0 {
			add(m[0], m[1], 0, atoi(text[m[4]:m[5]]), atoi(text[m[2]:m[3]]), true)
		} else {
			add(m[0], m[1], atoi(text[m[6]:m[7]]), atoi(text[m[4]:m[5]]), atoi(text[m[2]:m[3]]), false)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// resolveYear gives a date without year the next year in which it lies on
// or after ref.
func resolveYear(d DateAt, ref time.Time) time.Time {
	if !d.NoYear {
		return d.Time
	}
	t := time.Date(ref.Year(), d.Time.Month(), d.Time.Day(), 0, 0, 0, 0, d.Time.Location())
	if t.Month() != d.Time.Month() { // 29.02. in a year without it
		t = time.Date(ref.Year(), 3, 1, 0, 0, 0, 0, d.Time.Location())
	}
	if t.Before(dayOf(ref)) {
		t = t.AddDate(1, 0, 0)
	}
	return t
}

func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// addMonths moves a date by n months and keeps it in its month: 31.12.
// minus one month is 30.11., not 1.12.
func addMonths(t time.Time, n int) time.Time {
	first := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, t.Location()).AddDate(0, n, 0)
	last := first.AddDate(0, 1, -1).Day()
	return time.Date(first.Year(), first.Month(), min(t.Day(), last), 0, 0, 0, 0, t.Location())
}

// Amounts.

var amountRe = regexp.MustCompile(`(?i)(?:(€|eur|euro)\s?)?(-?\d{1,3}(?:[.\s']\d{3})+(?:,\d{2})|-?\d{1,3}(?:,\d{3})+\.\d{2}|-?\d+,\d{2}|-?\d+\.\d{2})(?:\s?(€|eur\b|euro\b))?`)

// Amount is a sum of money in cents with where it stands.
type Amount struct {
	Cents      int64
	Start, End int
	Currency   bool // € or EUR stood next to it
}

// FindAmounts finds sums of money: 1.284,37 €, 86,00 EUR, EUR 12.50,
// 1,284.37.
func FindAmounts(text string) []Amount {
	var out []Amount
	for _, m := range amountRe.FindAllStringSubmatchIndex(text, -1) {
		num := text[m[4]:m[5]]
		if m[4] > 0 && (isDigit(text[m[4]-1]) || text[m[4]-1] == '.') {
			continue
		}
		// "23.11.2026" and "1,284" go on: no sum.
		if m[5] < len(text) && (isDigit(text[m[5]]) || text[m[5]] == '.' && m[5]+1 < len(text) && isDigit(text[m[5]+1])) {
			continue
		}
		c, ok := parseCents(num)
		if !ok {
			continue
		}
		out = append(out, Amount{Cents: c, Start: m[0], End: m[1], Currency: m[2] >= 0 || m[6] >= 0})
	}
	return out
}

func parseCents(num string) (int64, bool) {
	neg := strings.HasPrefix(num, "-")
	num = strings.TrimPrefix(num, "-")
	var whole, frac string
	switch {
	case len(num) > 3 && num[len(num)-3] == ',':
		whole, frac = num[:len(num)-3], num[len(num)-2:]
		whole = strings.NewReplacer(".", "", " ", "", "'", "").Replace(whole)
	case len(num) > 3 && num[len(num)-3] == '.':
		whole, frac = num[:len(num)-3], num[len(num)-2:]
		whole = strings.ReplaceAll(whole, ",", "")
	default:
		return 0, false
	}
	w, err1 := strconv.ParseInt(whole, 10, 64)
	f, err2 := strconv.ParseInt(frac, 10, 64)
	if err1 != nil || err2 != nil || w > 10_000_000 {
		return 0, false
	}
	c := w*100 + f
	if neg {
		c = -c
	}
	return c, true
}

// FormatEuro writes cents the German way: 1.284,37 €.
func FormatEuro(c int64) string {
	neg := c < 0
	if neg {
		c = -c
	}
	whole := strconv.FormatInt(c/100, 10)
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	s := b.String() + "," + pad2(c%100) + " €"
	if neg {
		s = "-" + s
	}
	return s
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}
