package store

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-ical"
	"github.com/emersion/go-vcard"

	"github.com/ra5on/TitanOS/backend/internal/pim/card"
	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// Bounds of what the index keeps of one entry.
const (
	maxIndexText   = 256
	maxIndexValues = 20
)

// inspect checks the bytes of an entry as a PUT does and returns its index
// entry (without size and time): one UID per .ics file and only VEVENT
// besides VTIMEZONE; one vCard per .vcf file.
func (s *Store) inspect(kind Kind, account, name string, data []byte) (*Entry, error) {
	if kind == Contacts {
		return inspectCard(data)
	}
	if len(data) > MaxICS {
		return nil, ErrTooLarge
	}
	// A write: one VTIMEZONE at most MaxZoneBytes (S-21).
	f, err := recur.Read(data, recur.Options{Local: s.local(), Account: account, Name: name, MaxZone: recur.MaxZoneBytes})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidData, err)
	}
	return calendarEntry(f, data)
}

func calendarEntry(f *recur.File, data []byte) (*Entry, error) {
	for _, c := range f.Calendar.Children {
		switch c.Name {
		case ical.CompEvent, ical.CompTimezone:
		default:
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedComponent, c.Name)
		}
	}
	if len(f.Series) != 1 {
		return nil, fmt.Errorf("%w: a file holds the events of exactly one UID", ErrInvalidData)
	}
	sr := f.Series[0]
	if sr.UID == "" {
		return nil, fmt.Errorf("%w: UID missing", ErrInvalidData)
	}
	ev := sr.Master
	if ev == nil {
		ev = sr.Overrides[0]
	}
	e := &Entry{ETag: ETag(data), UID: sr.UID}
	if summary, err := ev.Component.Props.Text(ical.PropSummary); err == nil {
		e.Summary, e.Clipped = clip(summary), clipped(summary)
	}
	var cut bool
	e.Location, e.TZID, e.TZGuess, e.Alarms, cut = eventDetails(ev)
	e.Clipped = e.Clipped || cut
	e.Recurs = sr.Master == nil || len(sr.Overrides) > 0 || sr.Master.Recurring()
	e.Unsupported = sr.Master != nil && sr.Master.Unsupported
	e.AllDay = ev.Start.Kind == tz.KindDate
	e.Floating = ev.Start.Kind == tz.KindFloating
	if e.AllDay || e.Floating {
		y, m, d := ev.Start.Time.Date()
		h, mi, sec := ev.Start.Time.Clock()
		e.Start = time.Date(y, m, d, h, mi, sec, 0, time.UTC)
		if e.AllDay {
			e.End = e.Start.AddDate(0, 0, ev.Duration.Days).Add(ev.Duration.Exact)
		} else {
			e.End = ev.Duration.AddTo(e.Start)
		}
	} else {
		e.Start = ev.Start.Time.UTC()
		e.End = ev.Duration.AddTo(ev.Start.Time).UTC()
	}
	if e.Recurs {
		e.End = time.Time{}
	}
	return e, nil
}

// eventDetails are what lists show of an event besides title and time:
// location (clipped), the zone of DTSTART, whether a zone was guessed, the
// alarms.
func eventDetails(ev *recur.Event) (location, tzid string, guess bool, alarms []int, cut bool) {
	if l, err := ev.Component.Props.Text(ical.PropLocation); err == nil {
		location, cut = clip(l), clipped(l)
	}
	return location, ZoneName(ev.Start), ev.TZGuess, recur.Alarms(ev.Component), cut
}

// ZoneName names the zone of a start: its IANA name, "UTC", or "" for a
// date or a floating time (they follow the zone of the NAS).
func ZoneName(v tz.Value) string {
	switch v.Kind {
	case tz.KindUTC:
		return "UTC"
	case tz.KindZoned:
		return v.Time.Location().String()
	}
	return ""
}

// span places an index entry on the timeline with the NAS zone.
func (e *Entry) span(loc *time.Location) (time.Time, time.Time) {
	if !e.AllDay && !e.Floating {
		return e.Start, e.End
	}
	wall := func(t time.Time) time.Time {
		y, m, d := t.Date()
		h, mi, s := t.Clock()
		return time.Date(y, m, d, h, mi, s, 0, loc)
	}
	return wall(e.Start), wall(e.End)
}

func inspectCard(data []byte) (*Entry, error) {
	c, err := card.Parse(data)
	if errors.Is(err, card.ErrTooLarge) {
		return nil, ErrTooLarge
	}
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidData, err)
	}
	e := &Entry{ETag: ETag(data), UID: clip(c.Value(vcard.FieldUID))}
	e.FN = clip(c.PreferredValue(vcard.FieldFormattedName))
	sort := ""
	if n := c.Name(); n != nil {
		sort = strings.TrimSpace(n.FamilyName + " " + n.GivenName)
	}
	if sort == "" {
		sort = e.FN
	}
	e.SortKey = clip(strings.ToLower(sort))
	if org := c.Value(vcard.FieldOrganization); org != "" {
		first, _, _ := strings.Cut(org, ";")
		e.Org = clip(strings.TrimSpace(first))
	}
	for _, v := range c.Values(vcard.FieldEmail) {
		if v = strings.TrimSpace(v); v != "" && len(e.Emails) < maxIndexValues {
			e.Emails = append(e.Emails, clip(v))
		}
	}
	for _, v := range c.Values(vcard.FieldTelephone) {
		if d := phoneDigits(v); d != "" && len(e.Phones) < maxIndexValues {
			e.Phones = append(e.Phones, d)
		}
	}
	e.BDay = clip(c.Value(vcard.FieldBirthday))
	for _, a := range c.Addresses() {
		var parts []string
		for _, p := range []string{a.StreetAddress, strings.TrimSpace(a.PostalCode + " " + a.Locality), a.Region, a.Country} {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
		if len(parts) > 0 && len(e.Addresses) < maxIndexValues {
			e.Addresses = append(e.Addresses, clip(strings.Join(parts, ", ")))
		}
	}
	e.Group = strings.EqualFold(strings.TrimSpace(c.Value("X-ADDRESSBOOKSERVER-KIND")), "group") ||
		strings.EqualFold(strings.TrimSpace(c.Value(vcard.FieldKind)), "group")
	e.HasPhoto = len(c[vcard.FieldPhoto]) > 0
	return e, nil
}

// PhoneDigits keeps the digits of a number and a leading + (the form of
// Entry.Phones; addressbook-query compares a TEL text with it).
func PhoneDigits(v string) string { return phoneDigits(v) }

// phoneDigits keeps the digits of a number and a leading +.
func phoneDigits(v string) string {
	v = strings.TrimPrefix(strings.TrimSpace(v), "tel:")
	var b strings.Builder
	for i, r := range v {
		if r >= '0' && r <= '9' || (r == '+' && i == 0) {
			b.WriteRune(r)
		}
		if b.Len() >= 32 {
			break
		}
	}
	if b.String() == "+" {
		return ""
	}
	return b.String()
}

// clipped reports whether clip cuts s.
func clipped(s string) bool { return len(strings.TrimSpace(s)) > maxIndexText }

// clip bounds a text of the index to maxIndexText bytes of whole runes.
func clip(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxIndexText {
		return s
	}
	cut := maxIndexText
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
