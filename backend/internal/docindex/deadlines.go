package docindex

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Kinds of deadlines.
const (
	KindCancel   = "cancel"   // Kündigung, Vertragsende
	KindWarranty = "warranty" // Garantie, Gewährleistung
	KindPayment  = "payment"  // Zahlung, Abbuchung
	KindDue      = "due"      // other Fälligkeit, Frist
)

// Deadline is a date in a document that someone has to act by.
type Deadline struct {
	Kind    string    `json:"kind"`
	Date    time.Time `json:"date"`
	Snippet string    `json:"snippet"`
	Page    int       `json:"page,omitempty"`
	// Cents of a payment, when the sentence names a sum.
	Cents int64 `json:"cents,omitempty"`
	// Debit: the sum is taken by direct debit (Lastschrift, abgebucht).
	Debit bool `json:"debit,omitempty"`
}

var (
	cancelWords   = regexp.MustCompile(`(?i)kündig|kuendig|vertragsende|vertragsablauf|mindestlaufzeit|läuft\s.{0,40}\saus|endet\sam|cancel|terminat|notice period|expires?|renewal`)
	warrantyWords = regexp.MustCompile(`(?i)garantie|gewährleistung|gewaehrleistung|warranty|guarantee`)
	paymentWords  = regexp.MustCompile(`(?i)fällig|faellig|zahlbar|zahlungsziel|zu zahlen|überweis|ueberweis|abgebucht|abbuchung|eingezogen|lastschrift|payable|payment due|due date|pay by|amount due|bitte zahlen`)
	dueWords      = regexp.MustCompile(`(?i)\bfrist\b|fristende|einzureichen|einreichen bis|spätestens|spaetestens|deadline|\bdue\b|bis zum`)
	debitWords    = regexp.MustCompile(`(?i)lastschrift|abgebucht|abbuchung|eingezogen|direct debit|sepa`)

	beforeEnd = regexp.MustCompile(`(?i)(\d+|eine[nm]?|zwei|drei|vier|sechs|zwölf)\s+(monate?n?|wochen?|tage?n?|months?|weeks?|days?)\s+(?:zum|vor|before)\s+(?:dem\s+|the\s+)?(?:ablauf|vertragsende|ende|end|expiry|laufzeitende)`)
	withinDays = regexp.MustCompile(`(?i)(?:innerhalb|binnen|within)\s+(?:von\s+)?(\d+)\s+(tage?n?|days?|wochen|weeks)`)
	spanYears  = regexp.MustCompile(`(?i)(\d+|ein|eine|zwei|drei|fünf|one|two|three|five)[\s-]+(jahre?n?|years?|monate?n?|months?)\s+(?:\w+\s+)?(?:garantie|gewährleistung|gewaehrleistung|herstellergarantie|warranty|guarantee)|(?:garantie|gewährleistung|warranty|guarantee)\s*(?:von|:|of)?\s*(\d+|ein|eine|zwei|drei|fünf|one|two|three|five)\s+(jahre?n?|years?|monate?n?|months?)`)
	docDateWords = regexp.MustCompile(`(?i)rechnungsdatum|kaufdatum|lieferdatum|belegdatum|\bdatum\b|invoice date|\bdate\b|\bvom\b`)
)

var smallNumbers = map[string]int{"ein": 1, "eine": 1, "einen": 1, "einem": 1, "one": 1, "zwei": 2, "two": 2, "drei": 3, "three": 3, "vier": 4, "fünf": 5, "five": 5, "sechs": 6, "zwölf": 12}

func number(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return smallNumbers[strings.ToLower(s)]
}

// sentences splits text at line ends and sentence ends, keeping offsets.
func sentences(text string) [][2]int {
	var out [][2]int
	start := 0
	for i := 0; i < len(text); i++ {
		c := text[i]
		end := c == '\n' || c == '!' || c == '?' || c == ';'
		// A full stop ends a sentence only before a space and a capital
		// or the end: "31.12." and "z. B." stay inside.
		if c == '.' && i+2 < len(text) && text[i+1] == ' ' && text[i+2] >= 'A' && text[i+2] <= 'Z' && i > 0 && !isDigit(text[i-1]) {
			end = true
		}
		if end {
			if strings.TrimSpace(text[start:i]) != "" {
				out = append(out, [2]int{start, i + 1})
			}
			start = i + 1
		}
	}
	if strings.TrimSpace(text[start:]) != "" {
		out = append(out, [2]int{start, len(text)})
	}
	return out
}

// DocDate is the date a document is from: the one after "Rechnungsdatum",
// "Datum" or "vom", else the first full date of its beginning.
func DocDate(text string, loc *time.Location) (time.Time, bool) {
	head := text
	if len(head) > 4000 {
		head = head[:4000]
	}
	dates := FindDates(head, loc)
	for _, m := range docDateWords.FindAllStringIndex(head, -1) {
		for _, d := range dates {
			if !d.NoYear && d.Start >= m[1] && d.Start-m[1] < 30 {
				return d.Time, true
			}
		}
	}
	for _, d := range dates {
		if !d.NoYear {
			return d.Time, true
		}
	}
	return time.Time{}, false
}

// Deadlines finds the deadlines of the pages of a document, from rules (no
// model): a word for a kind of deadline and a date in the same sentence,
// "einen Monat vor Ablauf (31.12.)", "zahlbar innerhalb von 14 Tagen" from
// the date of the document, "2 Jahre Gewährleistung" from the date of
// purchase. Only deadlines on or after now are returned.
func Deadlines(pages []Page, now time.Time) []Deadline {
	loc := now.Location()
	var all strings.Builder
	for _, p := range pages {
		all.WriteString(p.Text)
		all.WriteByte('\n')
	}
	docDate, haveDocDate := DocDate(all.String(), loc)
	ref := now
	if haveDocDate {
		ref = docDate
	}
	var out []Deadline
	add := func(d Deadline) {
		if d.Date.Before(dayOf(now)) {
			return
		}
		if slices.ContainsFunc(out, func(o Deadline) bool { return o.Kind == d.Kind && o.Date.Equal(d.Date) }) {
			return
		}
		out = append(out, d)
	}
	for _, p := range pages {
		text := p.Text
		for _, se := range sentences(text) {
			s := text[se[0]:se[1]]
			kind := ""
			switch {
			case warrantyWords.MatchString(s):
				kind = KindWarranty
			case cancelWords.MatchString(s):
				kind = KindCancel
			case paymentWords.MatchString(s):
				kind = KindPayment
			case dueWords.MatchString(s):
				kind = KindDue
			default:
				continue
			}
			snippet := clean(s, 200)
			dates := FindDates(s, loc)
			// A sentence that names a date of the document itself is no
			// deadline ("Rechnungsdatum 03.11.2026").
			dates = slices.DeleteFunc(dates, func(d DateAt) bool {
				before := strings.ToLower(s[max(0, d.Start-18):d.Start])
				return docDateWords.MatchString(before) && !paymentWords.MatchString(before) && !cancelWords.MatchString(before)
			})
			d := Deadline{Kind: kind, Snippet: snippet, Page: p.N}
			if kind == KindPayment {
				d.Debit = debitWords.MatchString(s)
				if am := FindAmounts(s); len(am) > 0 {
					d.Cents = am[0].Cents
				}
			}
			switch {
			case kind == KindWarranty:
				if m := spanYears.FindStringSubmatch(s); m != nil && haveDocDate {
					n, unit := number(m[1]), m[2]
					if n == 0 {
						n, unit = number(m[3]), m[4]
					}
					if n > 0 {
						if strings.HasPrefix(strings.ToLower(unit), "j") || strings.HasPrefix(strings.ToLower(unit), "y") {
							d.Date = addMonths(docDate, 12*n)
						} else {
							d.Date = addMonths(docDate, n)
						}
						add(d)
						continue
					}
				}
				if len(dates) > 0 {
					d.Date = resolveYear(dates[len(dates)-1], ref)
					add(d)
				}
			case kind == KindCancel && beforeEnd.MatchString(s) && len(dates) > 0:
				m := beforeEnd.FindStringSubmatch(s)
				n, unit := number(m[1]), strings.ToLower(m[2])
				end := resolveYear(dates[len(dates)-1], now)
				switch {
				case strings.HasPrefix(unit, "mon"):
					d.Date = addMonths(end, -n)
				case strings.HasPrefix(unit, "w"):
					d.Date = end.AddDate(0, 0, -7*n)
				default:
					d.Date = end.AddDate(0, 0, -n)
				}
				add(d)
			case len(dates) > 0:
				d.Date = resolveYear(dates[0], ref)
				add(d)
			case kind == KindPayment && haveDocDate:
				if m := withinDays.FindStringSubmatch(s); m != nil {
					n := number(m[1])
					if strings.HasPrefix(strings.ToLower(m[2]), "w") {
						n *= 7
					}
					if n > 0 && n <= 120 {
						d.Date = docDate.AddDate(0, 0, n)
						add(d)
					}
				}
			}
		}
	}
	slices.SortFunc(out, func(a, b Deadline) int { return a.Date.Compare(b.Date) })
	return out
}

// clean folds whitespace and cuts to n bytes.
func clean(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		s = cutUTF8(s, n-3) + "…"
	}
	return s
}
