package recur

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// Simple is the form of a rule the calendar window and the agent work
// with ("repeat" in calendar_create, "rule" in the event list). Custom is
// true when the rule says more than these fields can hold; such a rule is
// shown as text only and never rebuilt from this form.
type Simple struct {
	Freq       string   `json:"freq"` // daily, weekly, monthly, yearly
	Interval   int      `json:"interval"`
	ByDay      []string `json:"byDay,omitempty"`      // "MO", "3TU", "-1FR"
	ByMonthDay []int    `json:"byMonthDay,omitempty"` // 1…31, -1 = last day
	Until      string   `json:"until,omitempty"`      // YYYY-MM-DD, that day included
	Count      int      `json:"count,omitempty"`
	Custom     bool     `json:"custom,omitempty"`
}

// ErrNotSimple marks a Simple that cannot become a rule.
var ErrNotSimple = errors.New("repeat rule not valid")

// Simple returns the simple form of r. loc is the zone of DTSTART; it
// gives the date of a UTC UNTIL.
func (r *Rule) Simple(loc *time.Location) Simple {
	s := Simple{Freq: strings.ToLower(r.Freq.String()), Interval: max(r.Interval, 1), Count: r.Count}
	for _, d := range r.ByDay {
		s.ByDay = append(s.ByDay, d.String())
	}
	s.ByMonthDay = slices.Clone(r.ByMonthDay)
	if r.Until != nil {
		s.Until = untilDate(*r.Until, loc).Format("2006-01-02")
	}
	s.Custom = !r.isSimple()
	return s
}

func untilDate(u tz.Value, loc *time.Location) time.Time {
	t := u.Time
	if u.Kind == tz.KindUTC && loc != nil {
		t = t.In(loc)
	}
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// isSimple reports whether the fields of Simple say all of r.
func (r *Rule) isSimple() bool {
	if len(r.ByMonth)+len(r.ByYearDay)+len(r.ByWeekNo)+len(r.BySetPos)+len(r.ByHour)+len(r.ByMinute)+len(r.BySecond) > 0 {
		return false
	}
	// WKST changes the result only for a weekly rule with INTERVAL > 1 and
	// more than one day.
	if r.WkstSet && r.Wkst != time.Monday && r.Freq == Weekly && r.Interval > 1 && len(r.ByDay) > 1 {
		return false
	}
	ordinals := 0
	for _, d := range r.ByDay {
		if d.N != 0 {
			ordinals++
		}
	}
	switch r.Freq {
	case Daily, Weekly:
		return ordinals == 0 && len(r.ByMonthDay) == 0
	case Monthly:
		if len(r.ByDay) > 0 {
			// "on the third Tuesday": one ordinal day, nothing else.
			return len(r.ByDay) == 1 && ordinals == 1 && len(r.ByMonthDay) == 0
		}
		return true
	case Yearly:
		return len(r.ByDay) == 0 && len(r.ByMonthDay) == 0
	}
	return false
}

// Rule builds the rule for s, with UNTIL in the form RFC 5545 asks for the
// kind of dtstart: a date for an all-day series, the end of that day in
// UTC for a zoned or UTC start, the end of that day floating otherwise.
func (s Simple) Rule(dtstart tz.Value) (*Rule, error) {
	if s.Custom {
		return nil, fmt.Errorf("%w: a custom rule is kept as it is", ErrNotSimple)
	}
	r := &Rule{Interval: s.Interval, Count: s.Count}
	switch strings.ToLower(s.Freq) {
	case "daily":
		r.Freq = Daily
	case "weekly":
		r.Freq = Weekly
	case "monthly":
		r.Freq = Monthly
	case "yearly":
		r.Freq = Yearly
	default:
		return nil, fmt.Errorf("%w: freq %q", ErrNotSimple, s.Freq)
	}
	if r.Interval == 0 {
		r.Interval = 1
	}
	if r.Interval < 1 || r.Interval > 1000 {
		return nil, fmt.Errorf("%w: interval %d", ErrNotSimple, s.Interval)
	}
	if s.Count < 0 || s.Count > MaxOccurrences {
		return nil, fmt.Errorf("%w: count %d", ErrNotSimple, s.Count)
	}
	if s.Count > 0 && s.Until != "" {
		return nil, fmt.Errorf("%w: count and until together", ErrNotSimple)
	}
	if len(s.ByDay) > 7 || len(s.ByMonthDay) > 31 {
		return nil, fmt.Errorf("%w: too many days", ErrNotSimple)
	}
	for _, d := range s.ByDay {
		days, err := parseByDay(d)
		if err != nil || len(days) != 1 {
			return nil, fmt.Errorf("%w: day %q", ErrNotSimple, d)
		}
		r.ByDay = append(r.ByDay, days[0])
	}
	r.ByMonthDay = slices.Clone(s.ByMonthDay)
	for _, d := range r.ByMonthDay {
		if d == 0 || d > 31 || d < -31 {
			return nil, fmt.Errorf("%w: day of month %d", ErrNotSimple, d)
		}
	}
	if s.Until != "" {
		u, err := time.ParseInLocation("2006-01-02", s.Until, time.UTC)
		if err != nil {
			return nil, fmt.Errorf("%w: until %q", ErrNotSimple, s.Until)
		}
		var v tz.Value
		switch dtstart.Kind {
		case tz.KindDate:
			v = tz.Value{Time: u, Kind: tz.KindDate}
		case tz.KindFloating:
			v = tz.Value{Time: u.Add(24*time.Hour - time.Second), Kind: tz.KindFloating}
		default:
			loc := dtstart.Time.Location()
			end := time.Date(u.Year(), u.Month(), u.Day(), 23, 59, 59, 0, loc)
			v = tz.Value{Time: end.UTC(), Kind: tz.KindUTC}
		}
		r.Until = &v
	}
	if !r.isSimple() {
		return nil, fmt.Errorf("%w: days do not fit freq %s", ErrNotSimple, s.Freq)
	}
	// Through Parse once: the same whitelist as a rule from a client.
	back, err := Parse(r.String())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotSimple, err)
	}
	return back, nil
}

// Describe puts r into words, German ("de") or English (anything else),
// e.g. "jeden Monat am dritten Dienstag, endet nach 10 Terminen". loc is
// the zone of DTSTART, for the date of UNTIL.
func (r *Rule) Describe(lang string, loc *time.Location) string {
	w := words(lang)
	var b strings.Builder
	n := max(r.Interval, 1)
	weekdays := r.weekdaysOnly()
	switch {
	case weekdays && (r.Freq == Daily || r.Freq == Weekly) && n == 1:
		b.WriteString(w.everyWeekday)
	case n == 1:
		b.WriteString(w.every[r.Freq])
	default:
		b.WriteString(fmt.Sprintf(w.everyN[r.Freq], n))
	}
	if len(r.ByWeekNo) > 0 {
		b.WriteString(" " + fmt.Sprintf(w.inWeek, joinInts(r.ByWeekNo, w)))
	}
	if len(r.ByMonth) > 0 {
		names := make([]string, len(r.ByMonth))
		for i, m := range r.ByMonth {
			names[i] = w.months[m-1]
		}
		b.WriteString(" " + w.in + " " + w.join(names))
	}
	if len(r.ByYearDay) > 0 {
		b.WriteString(" " + fmt.Sprintf(w.onYearDay, joinInts(r.ByYearDay, w)))
	}
	if len(r.ByMonthDay) > 0 {
		parts := make([]string, len(r.ByMonthDay))
		for i, d := range r.ByMonthDay {
			parts[i] = w.monthDay(d)
		}
		b.WriteString(" " + w.on + " " + w.join(parts))
	}
	if len(r.ByDay) > 0 && !(weekdays && n == 1 && (r.Freq == Daily || r.Freq == Weekly)) {
		parts := make([]string, len(r.ByDay))
		for i, d := range r.ByDay {
			if d.N == 0 {
				parts[i] = w.days[d.Day]
			} else {
				parts[i] = w.nth(d.N) + " " + w.days[d.Day]
			}
		}
		b.WriteString(" " + w.onDay(r.ByDay) + " " + w.join(parts))
	}
	if len(r.BySetPos) > 0 {
		parts := make([]string, len(r.BySetPos))
		for i, p := range r.BySetPos {
			parts[i] = w.setPosName(p)
		}
		b.WriteString(" " + fmt.Sprintf(w.setPos, w.join(parts)))
	}
	switch {
	case r.Count == 1:
		b.WriteString(", " + w.afterOne)
	case r.Count > 1:
		b.WriteString(", " + fmt.Sprintf(w.afterN, r.Count))
	case r.Until != nil:
		b.WriteString(", " + w.until + " " + w.date(untilDate(*r.Until, loc)))
	}
	return b.String()
}

func (r *Rule) weekdaysOnly() bool {
	if len(r.ByDay) != 5 {
		return false
	}
	seen := map[time.Weekday]bool{}
	for _, d := range r.ByDay {
		if d.N != 0 || d.Day == time.Saturday || d.Day == time.Sunday {
			return false
		}
		seen[d.Day] = true
	}
	return len(seen) == 5
}

func joinInts(v []int, w *lang) string {
	s := make([]string, len(v))
	for i, n := range v {
		s[i] = strconv.Itoa(n)
	}
	return w.join(s)
}

type lang struct {
	de                     bool
	every, everyN          map[Freq]string
	everyWeekday           string
	on, in                 string
	days                   [7]string
	months                 [12]string
	inWeek, onYearDay      string
	setPos                 string
	afterOne, afterN       string
	until                  string
	and                    string
	ordinals               []string
	last, secondLast       string
	lastDay, secondLastDay string
}

var german = &lang{
	de:           true,
	every:        map[Freq]string{Daily: "jeden Tag", Weekly: "jede Woche", Monthly: "jeden Monat", Yearly: "jedes Jahr"},
	everyN:       map[Freq]string{Daily: "alle %d Tage", Weekly: "alle %d Wochen", Monthly: "alle %d Monate", Yearly: "alle %d Jahre"},
	everyWeekday: "jeden Werktag",
	on:           "am", in: "im",
	days:      [7]string{"Sonntag", "Montag", "Dienstag", "Mittwoch", "Donnerstag", "Freitag", "Samstag"},
	months:    [12]string{"Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", "September", "Oktober", "November", "Dezember"},
	inWeek:    "in Kalenderwoche %s",
	onYearDay: "am %s. Tag des Jahres",
	setPos:    "(davon jeweils der %s)",
	afterOne:  "endet nach 1 Termin", afterN: "endet nach %d Terminen",
	until:    "bis",
	and:      "und",
	ordinals: []string{"ersten", "zweiten", "dritten", "vierten", "fünften"},
	last:     "letzten", secondLast: "vorletzten",
	lastDay: "letzten Tag", secondLastDay: "vorletzten Tag",
}

var english = &lang{
	every:        map[Freq]string{Daily: "every day", Weekly: "every week", Monthly: "every month", Yearly: "every year"},
	everyN:       map[Freq]string{Daily: "every %d days", Weekly: "every %d weeks", Monthly: "every %d months", Yearly: "every %d years"},
	everyWeekday: "every weekday",
	on:           "on the", in: "in",
	days:      [7]string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"},
	months:    [12]string{"January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"},
	inWeek:    "in week %s",
	onYearDay: "on day %s of the year",
	setPos:    "(the %s of them)",
	afterOne:  "ends after 1 occurrence", afterN: "ends after %d occurrences",
	until:    "until",
	and:      "and",
	ordinals: []string{"first", "second", "third", "fourth", "fifth"},
	last:     "last", secondLast: "second-to-last",
	lastDay: "last day", secondLastDay: "second-to-last day",
}

func words(code string) *lang {
	if strings.HasPrefix(strings.ToLower(code), "de") {
		return german
	}
	return english
}

func (w *lang) join(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + " " + w.and + " " + parts[len(parts)-1]
}

// onDay is the preposition before weekdays: "am" / "on" for plain days,
// "am" / "on the" for "the third Tuesday".
func (w *lang) onDay(days []WeekdayNum) string {
	if w.de {
		return "am"
	}
	for _, d := range days {
		if d.N != 0 {
			return "on the"
		}
	}
	return "on"
}

func (w *lang) nth(n int) string {
	switch {
	case n >= 1 && n <= len(w.ordinals):
		return w.ordinals[n-1]
	case n == -1:
		return w.last
	case n == -2:
		return w.secondLast
	case w.de && n > 0:
		return strconv.Itoa(n) + "."
	case w.de:
		return strconv.Itoa(-n) + ".-letzten"
	case n > 0:
		return ordinalEN(n)
	}
	return ordinalEN(-n) + "-to-last"
}

// setPosName names a BYSETPOS entry: German as a number ("der 2.", "der
// letzte"), English as an ordinal.
func (w *lang) setPosName(n int) string {
	switch {
	case !w.de:
		return w.nth(n)
	case n == -1:
		return "letzte"
	case n < 0:
		return strconv.Itoa(-n) + ".-letzte"
	}
	return strconv.Itoa(n) + "."
}

func (w *lang) monthDay(d int) string {
	switch {
	case d == -1:
		return w.lastDay
	case d == -2:
		return w.secondLastDay
	case d < 0 && w.de:
		return strconv.Itoa(-d) + ".-letzten Tag"
	case d < 0:
		return ordinalEN(-d) + "-to-last day"
	case w.de:
		return strconv.Itoa(d) + "."
	}
	return ordinalEN(d)
}

func ordinalEN(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return strconv.Itoa(n) + suffix
}

func (w *lang) date(t time.Time) string {
	if w.de {
		return t.Format("02.01.2006")
	}
	return w.months[t.Month()-1] + " " + strconv.Itoa(t.Day()) + ", " + strconv.Itoa(t.Year())
}
