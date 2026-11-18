package holidays

import (
	"strings"
	"time"
)

// names of the holidays (iCalendar SUMMARY), German and English.
var names = map[string][2]string{
	"newYear":          {"Neujahr", "New Year's Day"},
	"epiphany":         {"Heilige Drei Könige", "Epiphany"},
	"womensDay":        {"Internationaler Frauentag", "International Women's Day"},
	"womensDayMV":      {"Internationaler Frauentag", "International Women's Day"},
	"goodFriday":       {"Karfreitag", "Good Friday"},
	"easterSunday":     {"Ostersonntag", "Easter Sunday"},
	"easterMonday":     {"Ostermontag", "Easter Monday"},
	"labourDay":        {"Tag der Arbeit", "Labour Day"},
	"ascension":        {"Christi Himmelfahrt", "Ascension Day"},
	"whitSunday":       {"Pfingstsonntag", "Whit Sunday"},
	"whitMonday":       {"Pfingstmontag", "Whit Monday"},
	"corpusChristi":    {"Fronleichnam", "Corpus Christi"},
	"assumption":       {"Mariä Himmelfahrt", "Assumption Day"},
	"childrensDay":     {"Weltkindertag", "World Children's Day"},
	"unity":            {"Tag der Deutschen Einheit", "German Unity Day"},
	"reformation":      {"Reformationstag", "Reformation Day"},
	"reformationNorth": {"Reformationstag", "Reformation Day"},
	"allSaints":        {"Allerheiligen", "All Saints' Day"},
	"repentance":       {"Buß- und Bettag", "Day of Repentance and Prayer"},
	"christmas":        {"1. Weihnachtstag", "Christmas Day"},
	"boxingDay":        {"2. Weihnachtstag", "Boxing Day"},
}

// stateNames in German and English.
var stateNames = map[string][2]string{
	"bw": {"Baden-Württemberg", "Baden-Württemberg"},
	"by": {"Bayern", "Bavaria"},
	"be": {"Berlin", "Berlin"},
	"bb": {"Brandenburg", "Brandenburg"},
	"hb": {"Bremen", "Bremen"},
	"hh": {"Hamburg", "Hamburg"},
	"he": {"Hessen", "Hesse"},
	"mv": {"Mecklenburg-Vorpommern", "Mecklenburg-Western Pomerania"},
	"ni": {"Niedersachsen", "Lower Saxony"},
	"nw": {"Nordrhein-Westfalen", "North Rhine-Westphalia"},
	"rp": {"Rheinland-Pfalz", "Rhineland-Palatinate"},
	"sl": {"Saarland", "Saarland"},
	"sn": {"Sachsen", "Saxony"},
	"st": {"Sachsen-Anhalt", "Saxony-Anhalt"},
	"sh": {"Schleswig-Holstein", "Schleswig-Holstein"},
	"th": {"Thüringen", "Thuringia"},
}

func lang(l string) int {
	if l == "en" {
		return 1
	}
	return 0
}

// Region is what a subscription names: "de" (nationwide only) or
// "de-<state>". ParseRegion returns the state ("" for "de").
func ParseRegion(region string) (string, bool) {
	if region == "de" {
		return "", true
	}
	state, ok := strings.CutPrefix(region, "de-")
	if !ok || state == "" || !ValidState(state) {
		return "", false
	}
	return state, true
}

// Title is the name of the calendar on the iPhone.
func Title(state, l string) string {
	if state == "" {
		return [2]string{"Feiertage in Deutschland", "Public holidays in Germany"}[lang(l)]
	}
	return [2]string{"Feiertage (", "Public holidays ("}[lang(l)] + stateNames[state][lang(l)] + ")"
}

// escape is an iCalendar TEXT value (RFC 5545 3.3.11).
func escape(s string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`).Replace(s)
}

// fold breaks a content line after 75 octets, never inside a UTF-8
// sequence (RFC 5545 3.1).
func fold(line string) string {
	var b strings.Builder
	n := 0
	for i, r := range line {
		size := len(string(r))
		if n+size > 75 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteString(line[i : i+size])
		n += size
	}
	b.WriteString("\r\n")
	return b.String()
}

// stamp is the DTSTAMP of every event: the table changes only with a new
// version of TitanOS, so the events stay the same between fetches.
const stamp = "20261118T000000Z"

// ICS writes the holidays of a state ("" nationwide) for the years from
// first to last as an iCalendar file: all-day events, free time.
func ICS(state, l string, first, last int) []byte {
	region := "de"
	if state != "" {
		region += "-" + state
	}
	var b strings.Builder
	line := func(s string) { b.WriteString(fold(s)) }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//TitanOS//Holidays//" + strings.ToUpper([2]string{"de", "en"}[lang(l)]))
	line("CALSCALE:GREGORIAN")
	line("METHOD:PUBLISH")
	line("X-WR-CALNAME:" + escape(Title(state, l)))
	line("X-PUBLISHED-TTL:P7D")
	line("REFRESH-INTERVAL;VALUE=DURATION:P7D")
	for year := first; year <= last; year++ {
		for _, h := range For(year, state) {
			date := h.Date.Format("20060102")
			line("BEGIN:VEVENT")
			line("UID:" + date + "-" + h.Key + "-" + region + "@holidays.titanos")
			line("DTSTAMP:" + stamp)
			line("DTSTART;VALUE=DATE:" + date)
			line("DTEND;VALUE=DATE:" + h.Date.AddDate(0, 0, 1).Format("20060102"))
			line("SUMMARY:" + escape(names[h.Key][lang(l)]))
			line("TRANSP:TRANSPARENT")
			line("CATEGORIES:" + [2]string{"Feiertag", "Holiday"}[lang(l)])
			line("END:VEVENT")
		}
	}
	line("END:VCALENDAR")
	return []byte(b.String())
}

// Years are the years a subscription holds: the last one, this one and the
// next two.
func Years(now time.Time) (int, int) {
	y := now.Year()
	return y - 1, y + 2
}

// FileName is the last segment of the route: "de-by.ics".
func FileName(state string) string {
	if state == "" {
		return "de.ics"
	}
	return "de-" + state + ".ics"
}
