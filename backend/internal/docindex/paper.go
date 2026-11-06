package docindex

import (
	"regexp"
	"strings"
	"time"
	"unicode"
)

// Types of paperwork, for the overview by type.
const (
	TypeEnergy    = "energy"    // Strom und Gas
	TypeTelecom   = "telecom"   // Internet und Handy
	TypeInsurance = "insurance" // Versicherungen
	TypeShopping  = "shopping"  // Einkäufe
	TypeHealth    = "health"    // Gesundheit
	TypeHousing   = "housing"   // Wohnen
	TypeTax       = "tax"       // Steuern und Ämter
	TypeOther     = "other"
)

// Types lists every type in the order of the overview.
var Types = []string{TypeEnergy, TypeTelecom, TypeInsurance, TypeShopping, TypeHealth, TypeHousing, TypeTax, TypeOther}

var typeWords = map[string]*regexp.Regexp{
	TypeEnergy:    regexp.MustCompile(`(?i)stadtwerke|\bstrom|erdgas|\bgas\b|abschlag|kwh|energie|fernwärme|wasserversorg|e\.on|eon\b|vattenfall|enbw|rwe\b|yello`),
	TypeTelecom:   regexp.MustCompile(`(?i)telekom|vodafone|congstar|\bo2\b|telefónica|telefonica|1&1|mobilfunk|\bdsl\b|glasfaser|internet|handy|rufnummer|kabel deutschland`),
	TypeInsurance: regexp.MustCompile(`(?i)versicherung|\bhuk\b|huk-coburg|allianz|\baxa\b|ergo\b|devk|haftpflicht|police|versicherungsschein|beitragsrechnung`),
	TypeShopping:  regexp.MustCompile(`(?i)mediamarkt|media markt|saturn|amazon|otto\b|zalando|ikea|bestellung|bestellnummer|artikel|kassenbon|lieferschein|warenkorb|order number|order #`),
	TypeHealth:    regexp.MustCompile(`(?i)zahnarzt|arztpraxis|\bpraxis\b|patient|goä|\bgoz\b|behandlung|apotheke|krankenkasse|klinik|dr\. med`),
	TypeHousing:   regexp.MustCompile(`(?i)\bmiete\b|mietvertrag|nebenkosten|hausverwaltung|vermieter|hausgeld|grundsteuer`),
	TypeTax:       regexp.MustCompile(`(?i)finanzamt|steuerbescheid|einkommensteuer|bundeszentralamt|rundfunkbeitrag|stadtverwaltung|bürgeramt|landratsamt`),
}

// typeOrder breaks ties: a Stadtwerke letter that mentions "Internet" is
// still about energy.
var typeOrder = []string{TypeEnergy, TypeTelecom, TypeInsurance, TypeHealth, TypeTax, TypeHousing, TypeShopping}

var knownSenders = []struct {
	re   *regexp.Regexp
	name string
}{
	{regexp.MustCompile(`(?i)\bstadtwerke\s+([A-ZÄÖÜ][\wäöüß-]+)`), "Stadtwerke $1"},
	{regexp.MustCompile(`(?i)\bstadtwerke\b`), "Stadtwerke"},
	{regexp.MustCompile(`(?i)\btelekom\b`), "Telekom"},
	{regexp.MustCompile(`(?i)\bvodafone\b`), "Vodafone"},
	{regexp.MustCompile(`(?i)\bcongstar\b`), "congstar"},
	{regexp.MustCompile(`(?i)\bhuk[- ]?coburg\b|\bhuk\b`), "HUK"},
	{regexp.MustCompile(`(?i)\ballianz\b`), "Allianz"},
	{regexp.MustCompile(`(?i)\bmedia\s?markt\b`), "MediaMarkt"},
	{regexp.MustCompile(`(?i)\bamazon\b`), "Amazon"},
	{regexp.MustCompile(`(?i)\bikea\b`), "IKEA"},
	{regexp.MustCompile(`(?i)\bfinanzamt\s+([A-ZÄÖÜ][\wäöüß-]+)`), "Finanzamt $1"},
	{regexp.MustCompile(`(?i)\bfinanzamt\b`), "Finanzamt"},
	{regexp.MustCompile(`(?i)\bARD ZDF|rundfunkbeitrag\b`), "Rundfunkbeitrag"},
}

var (
	doctorRe  = regexp.MustCompile(`\b(Zahnarzt|Zahnärztin|Praxis|Arztpraxis|Tierarzt)\s+(Dr\.\s*(?:med\.\s*(?:dent\.\s*)?)?)?([A-ZÄÖÜ][a-zäöüß]+)`)
	drRe      = regexp.MustCompile(`\bDr\.\s*(?:med\.\s*(?:dent\.\s*)?)?([A-ZÄÖÜ][a-zäöüß]+)`)
	companyRe = regexp.MustCompile(`(?m)^\s*([A-ZÄÖÜ][\wÄÖÜäöüß&.\- ]{1,40}?)\s+(GmbH|AG|KG|SE|e\.\s?V\.|OHG|UG|Ltd\.?|Inc\.?)\b`)

	strongTotal = regexp.MustCompile(`(?i)gesamtbetrag|rechnungsbetrag|endbetrag|zu zahlen|zahlbetrag|bruttobetrag|gesamtsumme|amount due|total due|grand total|summe brutto|abschlagsbetrag|beitrag gesamt|zu überweisen`)
	weakTotal   = regexp.MustCompile(`(?i)\bbetrag\b|\bsumme\b|\btotal\b|\bgesamt\b|\babschlag\b|\bbeitrag\b|\bbrutto\b`)
)

var docKinds = []struct {
	re   *regexp.Regexp
	word string
}{
	{regexp.MustCompile(`(?i)mahnung|zahlungserinnerung|payment reminder`), "Mahnung"},
	{regexp.MustCompile(`(?i)abschlagsrechnung|abschlagszahlung|\babschlag\b`), "Abschlag"},
	{regexp.MustCompile(`(?i)beitragsrechnung`), "Beitragsrechnung"},
	{regexp.MustCompile(`(?i)gutschrift|credit note`), "Gutschrift"},
	{regexp.MustCompile(`(?i)steuerbescheid|\bbescheid\b`), "Bescheid"},
	{regexp.MustCompile(`(?i)kündigungsbestätigung|kündigung`), "Kündigung"},
	{regexp.MustCompile(`(?i)\bvertrag\b|vertragsbestätigung|contract`), "Vertrag"},
	{regexp.MustCompile(`(?i)rechnung|invoice|\bbill\b`), "Rechnung"},
	{regexp.MustCompile(`(?i)quittung|kassenbon|receipt|beleg`), "Beleg"},
}

// Hint is what is known of a document besides its text.
type Hint struct {
	Name     string // file name
	FromName string // display name of a mail's sender
	From     string // address of a mail's sender
	Subject  string
	Date     time.Time // of the mail, or the file's change
}

// Info is what the autopilot recognised in a letter or invoice.
type Info struct {
	Sender string    `json:"sender,omitempty"`
	Type   string    `json:"type"`
	Kind   string    `json:"kind"` // Rechnung, Abschlag, …
	Date   time.Time `json:"date,omitzero"`
	Cents  int64     `json:"cents,omitempty"`
	Due    time.Time `json:"due,omitzero"`
	Debit  bool      `json:"debit,omitempty"`
	// Sure: sender, type and date were found; else the person is asked.
	Sure bool `json:"sure"`
	// Doubt names the alternatives when two types fit about as well.
	Doubt []string `json:"doubt,omitempty"`
}

// Recognize reads sender, date, sum, due date and type of a letter.
func Recognize(text string, h Hint, now time.Time) Info {
	loc := now.Location()
	all := text + "\n" + h.Subject
	info := Info{Type: TypeOther, Kind: "Dokument"}
	// Type: the kind with most matches; two close ones are a doubt.
	best, second, bestN, secondN := "", "", 0, 0
	for _, t := range typeOrder {
		n := len(typeWords[t].FindAllStringIndex(all+" "+h.FromName+" "+h.From, -1))
		if n > bestN {
			second, secondN = best, bestN
			best, bestN = t, n
		} else if n > secondN {
			second, secondN = t, n
		}
	}
	if best != "" {
		info.Type = best
		if second != "" && secondN*2 > bestN && bestN < 4 {
			info.Doubt = []string{best, second}
		}
	}
	for _, k := range docKinds {
		if k.re.MatchString(h.Subject + "\n" + head(text, 1500)) {
			info.Kind = k.word
			break
		}
	}
	info.Sender = sender(text, h)
	if d, ok := DocDate(text, loc); ok {
		info.Date = d
	} else if !h.Date.IsZero() {
		info.Date = dayOf(h.Date.In(loc))
	}
	info.Cents = total(text)
	for _, d := range Deadlines([]Page{{N: 1, Text: text}}, now.AddDate(-1, 0, 0)) {
		if d.Kind == KindPayment {
			info.Due, info.Debit = d.Date, d.Debit
			if info.Cents == 0 {
				info.Cents = d.Cents
			}
			break
		}
	}
	info.Sure = info.Sender != "" && info.Type != TypeOther && !info.Date.IsZero() && len(info.Doubt) == 0
	return info
}

func head(s string, n int) string {
	if len(s) > n {
		return cutUTF8(s, n)
	}
	return s
}

func sender(text string, h Hint) string {
	top := head(text, 2500) + "\n" + h.Subject + "\n" + h.FromName
	for _, k := range knownSenders {
		if m := k.re.FindStringSubmatchIndex(top); m != nil {
			name := string(k.re.ExpandString(nil, k.name, top, m))
			return SafeName(name)
		}
	}
	if m := doctorRe.FindStringSubmatch(top); m != nil {
		title := m[1]
		if m[2] != "" {
			title += " Dr."
		}
		return SafeName(title + " " + m[3])
	}
	if m := companyRe.FindStringSubmatch(top); m != nil {
		return SafeName(strings.TrimSpace(m[1]))
	}
	if m := drRe.FindStringSubmatch(top); m != nil {
		return SafeName("Dr. " + m[1])
	}
	if n := strings.TrimSpace(h.FromName); n != "" {
		return SafeName(n)
	}
	if h.From != "" {
		if at := strings.LastIndex(h.From, "@"); at > 0 {
			domain := h.From[at+1:]
			if dot := strings.Index(domain, "."); dot > 0 {
				domain = domain[:dot]
			}
			if domain != "" && domain != "gmail" && domain != "web" && domain != "gmx" && domain != "outlook" {
				return SafeName(strings.ToUpper(domain[:1]) + domain[1:])
			}
		}
	}
	return ""
}

// total finds the sum of an invoice: an amount on a line with "Gesamt-
// betrag", "zu zahlen" and the like, else on one with "Betrag", "Summe",
// else the largest amount with a currency.
func total(text string) int64 {
	var strong, weak, any int64
	for line := range strings.SplitSeq(text, "\n") {
		am := FindAmounts(line)
		if len(am) == 0 {
			continue
		}
		last := am[len(am)-1].Cents
		for _, a := range am {
			if a.Currency {
				last = a.Cents // the sum with € on the line counts
			}
		}
		switch {
		case strongTotal.MatchString(line):
			if strong == 0 {
				strong = last
			}
		case weakTotal.MatchString(line):
			weak = max(weak, last)
		}
		for _, a := range am {
			if a.Currency {
				any = max(any, a.Cents)
			}
		}
	}
	switch {
	case strong != 0:
		return strong
	case weak != 0:
		return weak
	}
	return any
}

// SafeName makes a name fit for a file or folder: no slashes, no control
// characters, no leading dot, at most 60 characters.
func SafeName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|':
			b.WriteRune(' ')
		case unicode.IsControl(r):
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Join(strings.Fields(b.String()), " ")
	out = strings.TrimLeft(out, ". ")
	out = strings.TrimRight(out, ". ")
	if r := []rune(out); len(r) > 60 {
		out = strings.TrimSpace(string(r[:60]))
	}
	if out == ".." {
		return ""
	}
	return out
}

// FileName is the name a filed document gets: "2026-11-03 Stadtwerke
// Abschlag.pdf".
func FileName(info Info, ext string) string {
	parts := []string{}
	if !info.Date.IsZero() {
		parts = append(parts, info.Date.Format("2006-01-02"))
	}
	if info.Sender != "" {
		parts = append(parts, info.Sender)
	}
	if info.Kind != "" {
		parts = append(parts, info.Kind)
	}
	name := SafeName(strings.Join(parts, " "))
	if name == "" {
		name = "Dokument"
	}
	return name + strings.ToLower(ext)
}
