// Package holidays computes the public holidays of Germany, nationwide and
// per state, and writes them as an iCalendar file for a subscription on
// the iPhone (1.13, the switch "Feiertage" of the profile). Everything is
// computed here (Easter by the Gregorian computus); nothing is fetched,
// stored or read from a volume, and no account's data is in it.
//
// Source of the table: the holiday laws of the states as summarized in
// "Gesetzliche Feiertage in Deutschland" (de.wikipedia.org, read
// 18 Nov 2026). Days that hold only in parts of a state are left out:
// Fronleichnam in parts of Saxony and Thuringia, Mariä Himmelfahrt in the
// Catholic municipalities of Bavaria, the Augsburger Friedensfest, and
// one-off days (Tag der Befreiung in Berlin 2020 and 2025, Reformationstag
// nationwide 2017). Hessen counts every Sunday as a holiday; Easter Sunday
// and Whit Sunday are listed only for Brandenburg, which names them.
package holidays

import (
	"slices"
	"time"
)

// States are the codes of the 16 states (ISO 3166-2:DE without "DE-").
var States = []string{"bw", "by", "be", "bb", "hb", "hh", "he", "mv", "ni", "nw", "rp", "sl", "sn", "st", "sh", "th"}

// Holiday is one day.
type Holiday struct {
	Date time.Time // midnight UTC of the day
	Key  string
}

// day is a holiday of the table: a fixed date or days after Easter Sunday,
// in the given states (nil: all), from a year on (0: always).
type day struct {
	key          string
	month, mday  int // fixed date, or
	easterOffset int // with month 0
	states       []string
	since        int
}

var table = []day{
	{key: "newYear", month: 1, mday: 1},
	{key: "epiphany", month: 1, mday: 6, states: []string{"bw", "by", "st"}},
	{key: "womensDay", month: 3, mday: 8, states: []string{"be"}, since: 2019},
	{key: "womensDayMV", month: 3, mday: 8, states: []string{"mv"}, since: 2023},
	{key: "goodFriday", easterOffset: -2},
	{key: "easterSunday", easterOffset: 0, states: []string{"bb"}},
	{key: "easterMonday", easterOffset: 1},
	{key: "labourDay", month: 5, mday: 1},
	{key: "ascension", easterOffset: 39},
	{key: "whitSunday", easterOffset: 49, states: []string{"bb"}},
	{key: "whitMonday", easterOffset: 50},
	{key: "corpusChristi", easterOffset: 60, states: []string{"bw", "by", "he", "nw", "rp", "sl"}},
	{key: "assumption", month: 8, mday: 15, states: []string{"sl"}},
	{key: "childrensDay", month: 9, mday: 20, states: []string{"th"}, since: 2019},
	{key: "unity", month: 10, mday: 3, since: 1990},
	{key: "reformation", month: 10, mday: 31, states: []string{"bb", "mv", "sn", "st", "th"}},
	{key: "reformationNorth", month: 10, mday: 31, states: []string{"hb", "hh", "ni", "sh"}, since: 2018},
	{key: "allSaints", month: 11, mday: 1, states: []string{"bw", "by", "nw", "rp", "sl"}},
	{key: "repentance", month: 11, states: []string{"sn"}}, // mday 0: Buß- und Bettag
	{key: "christmas", month: 12, mday: 25},
	{key: "boxingDay", month: 12, mday: 26},
}

// Easter is Easter Sunday of a Gregorian year (anonymous Gregorian
// algorithm, Meeus/Jones/Butcher).
func Easter(year int) time.Time {
	a := year % 19
	b, c := year/100, year%100
	d, e := b/4, b%4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i, k := c/4, c%4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	mday := (h+l-7*m+114)%31 + 1
	return time.Date(year, time.Month(month), mday, 0, 0, 0, 0, time.UTC)
}

// repentance is Buß- und Bettag: the Wednesday before 23 November.
func repentance(year int) time.Time {
	d := time.Date(year, time.November, 22, 0, 0, 0, 0, time.UTC)
	back := (int(d.Weekday()) - int(time.Wednesday) + 7) % 7
	return d.AddDate(0, 0, -back)
}

// ValidState reports whether s is one of States, or "" (nationwide only).
func ValidState(s string) bool { return s == "" || slices.Contains(States, s) }

// For returns the holidays of a year: the nationwide ones, and with a
// state also those of that state, in the order of the year.
func For(year int, state string) []Holiday {
	out := []Holiday{}
	easter := Easter(year)
	for _, d := range table {
		if d.since > year || d.states != nil && !slices.Contains(d.states, state) {
			continue
		}
		var date time.Time
		switch {
		case d.key == "repentance":
			date = repentance(year)
		case d.month == 0:
			date = easter.AddDate(0, 0, d.easterOffset)
		default:
			date = time.Date(year, time.Month(d.month), d.mday, 0, 0, 0, 0, time.UTC)
		}
		out = append(out, Holiday{Date: date, Key: d.key})
	}
	slices.SortStableFunc(out, func(a, b Holiday) int { return a.Date.Compare(b.Date) })
	return out
}
