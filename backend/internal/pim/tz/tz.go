// Package tz reads the DATE and DATE-TIME values of iCalendar (RFC 5545)
// the way TitanOS shows them: IANA names through the embedded zone data,
// Windows names (Outlook, Exchange) through the CLDR table, unknown names
// through the VTIMEZONE sent along, floating times in the zone of the NAS,
// and dates as dates. The bytes of an entry are never changed here.
package tz

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // the image may lack /usr/share/zoneinfo

	"github.com/emersion/go-ical"
)

// Kind tells how a value was written.
type Kind uint8

const (
	// KindDate is VALUE=DATE: a calendar date without a time of day.
	KindDate Kind = iota + 1
	// KindUTC is a DATE-TIME with the suffix Z.
	KindUTC
	// KindZoned is a DATE-TIME with a TZID parameter.
	KindZoned
	// KindFloating is a DATE-TIME without Z and without TZID; it is read
	// in the zone of the NAS.
	KindFloating
)

// ErrInvalid marks a value that is not a DATE or DATE-TIME of RFC 5545.
var ErrInvalid = errors.New("invalid date or date-time")

// MaxListValues bounds the values read from one property; a longer list
// makes ParseList fail.
const MaxListValues = 1000

// Value is one parsed DATE or DATE-TIME.
//
// For KindDate, Time is 00:00 UTC of the date, so that day arithmetic never
// meets a missing local midnight; use InZone to place it on the timeline of
// a zone. For the other kinds Time is the instant, in the zone it was
// written in (UTC, the TZID's zone or the NAS zone).
type Value struct {
	Time time.Time
	Kind Kind
	// TZID is the parameter as written, for KindZoned.
	TZID string
	// Guess is true when the zone could not be resolved exactly and a fixed
	// offset or the NAS zone stands in for it.
	Guess bool
	// End is the end of a VALUE=PERIOD value (RDATE only), else zero.
	End time.Time
}

// IsDate reports whether v is a date without time of day.
func (v Value) IsDate() bool { return v.Kind == KindDate }

// InZone returns the instant v stands for when dates and floating times are
// read in loc: a date is 00:00 of that day in loc.
func (v Value) InZone(loc *time.Location) time.Time {
	switch v.Kind {
	case KindDate:
		y, m, d := v.Time.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, loc)
	case KindFloating:
		y, m, d := v.Time.Date()
		h, mi, s := v.Time.Clock()
		return time.Date(y, m, d, h, mi, s, 0, loc)
	}
	return v.Time
}

// Format returns the value text in the same form v was written in: a date,
// a UTC time with Z, or a wall-clock time without Z (TZID or floating).
func (v Value) Format() string {
	switch v.Kind {
	case KindDate:
		return v.Time.Format(dateLayout)
	case KindUTC:
		return v.Time.UTC().Format(utcLayout)
	}
	return v.Time.Format(localLayout)
}

// Prop builds a property named name carrying v in its own form, with TZID
// and VALUE=DATE parameters as needed.
func (v Value) Prop(name string) *ical.Prop {
	p := ical.NewProp(name)
	v.SetProp(p)
	return p
}

// SetProp writes v into p, replacing value, TZID and VALUE; other
// parameters stay.
func (v Value) SetProp(p *ical.Prop) {
	p.Params.Del(ical.ParamTimezoneID)
	p.Params.Del(ical.ParamValue)
	if v.Kind == KindDate {
		p.Params.Set(ical.ParamValue, string(ical.ValueDate))
	}
	if v.Kind == KindZoned && v.TZID != "" {
		p.Params.Set(ical.ParamTimezoneID, v.TZID)
	}
	p.Value = v.Format()
}

// Equal reports whether a and b name the same moment: dates by date,
// everything else by UTC instant. A date never equals a date-time.
func Equal(a, b Value) bool {
	return a.IsDate() == b.IsDate() && a.Time.Equal(b.Time)
}

// Date returns a date value.
func Date(year int, month time.Month, day int) Value {
	return Value{Time: time.Date(year, month, day, 0, 0, 0, 0, time.UTC), Kind: KindDate}
}

const (
	dateLayout  = "20060102"
	localLayout = "20060102T150405"
	utcLayout   = "20060102T150405Z"
)

// Resolver resolves TZIDs for one calendar file. It is not safe for
// concurrent use.
type Resolver struct {
	local *time.Location
	vtz   map[string]*ical.Component
	cache map[string]resolved
}

type resolved struct {
	loc   *time.Location
	guess bool
}

// NewResolver returns a resolver that reads floating times in local (the
// zone of the NAS, passed in by the caller) and consults the VTIMEZONE
// components of cal for unknown TZIDs. cal may be nil.
func NewResolver(local *time.Location, cal *ical.Calendar) *Resolver {
	if local == nil {
		local = time.UTC
	}
	r := &Resolver{local: local, vtz: map[string]*ical.Component{}, cache: map[string]resolved{}}
	if cal != nil {
		for _, c := range cal.Children {
			if c.Name != ical.CompTimezone {
				continue
			}
			if p := c.Props.Get(ical.PropTimezoneID); p != nil {
				r.vtz[p.Value] = c
			}
		}
	}
	return r
}

// Local returns the zone floating times are read in.
func (r *Resolver) Local() *time.Location { return r.local }

// Location resolves tzid for a value in year. Order: IANA name, Windows
// name, IANA name behind a path prefix (/mozilla.org/…/Europe/Berlin), then
// the VTIMEZONE of the file (X-LIC-LOCATION, an IANA zone with the same
// offsets and transition days in year, else the fixed TZOFFSETTO of its
// last STANDARD with guess). Without any of these the NAS zone stands in,
// with guess.
func (r *Resolver) Location(tzid string, year int) (loc *time.Location, guess bool) {
	if l := Known(tzid); l != nil {
		return l, false
	}
	comp := r.vtz[tzid]
	if comp == nil {
		return r.local, true
	}
	key := tzid + "\x00" + strconv.Itoa(year)
	if c, ok := r.cache[key]; ok {
		return c.loc, c.guess
	}
	loc, guess = fromVTimezone(tzid, comp, year)
	if loc == nil {
		loc, guess = r.local, true
	}
	r.cache[key] = resolved{loc, guess}
	return loc, guess
}

// Known resolves tzid without a VTIMEZONE: IANA, Windows, or IANA behind a
// path prefix. It returns nil for anything else.
func Known(tzid string) *time.Location {
	tzid = strings.TrimSpace(tzid)
	if tzid == "" {
		return nil
	}
	if l := LoadIANA(tzid); l != nil {
		return l
	}
	if name, ok := windowsZones[tzid]; ok {
		return LoadIANA(name)
	}
	// Older Thunderbird, Citadel and others write "/mozilla.org/20050126_1/
	// Europe/Berlin"; the IANA name is the tail.
	rest := tzid
	for {
		i := strings.IndexByte(rest, '/')
		if i < 0 {
			return nil
		}
		rest = rest[i+1:]
		if strings.Contains(rest, "/") {
			if l := LoadIANA(rest); l != nil {
				return l
			}
		}
	}
}

// LoadIANA loads an IANA zone by name and returns nil when there is none.
// "Local" and paths are refused, so a TZID never reads the system zone or
// the file system.
func LoadIANA(name string) *time.Location {
	if name == "" || name == "Local" || strings.HasPrefix(name, "/") || strings.Contains(name, "..") || len(name) > 64 {
		return nil
	}
	l, err := loadCached(name)
	if err != nil {
		return nil
	}
	return l
}

// WindowsToIANA returns the IANA zone CLDR names for a Windows zone.
func WindowsToIANA(name string) (string, bool) {
	z, ok := windowsZones[name]
	return z, ok
}

// Parse reads one DATE or DATE-TIME property (DTSTART, DTEND,
// RECURRENCE-ID, a single EXDATE …).
func (r *Resolver) Parse(p *ical.Prop) (Value, error) {
	vs, err := r.ParseList(p)
	if err != nil {
		return Value{}, err
	}
	if len(vs) != 1 {
		return Value{}, fmt.Errorf("%w: %s holds %d values", ErrInvalid, p.Name, len(vs))
	}
	return vs[0], nil
}

// ParseList reads a property that may hold a comma-separated list (EXDATE,
// RDATE), with TZID and VALUE=DATE, DATE-TIME or PERIOD. A period yields
// its start, with End set.
func (r *Resolver) ParseList(p *ical.Prop) ([]Value, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: missing", ErrInvalid)
	}
	valueType := strings.ToUpper(p.Params.Get(ical.ParamValue))
	tzid := p.Params.Get(ical.ParamTimezoneID)
	parts := strings.Split(strings.TrimSpace(p.Value), ",")
	if len(parts) > MaxListValues {
		return nil, fmt.Errorf("%w: %s holds more than %d values", ErrInvalid, p.Name, MaxListValues)
	}
	out := make([]Value, 0, len(parts))
	for _, s := range parts {
		s = strings.TrimSpace(s)
		var v Value
		var err error
		switch valueType {
		case "PERIOD":
			v, err = r.period(s, tzid)
		case "DATE":
			v, err = parseDate(s)
		case "", "DATE-TIME":
			if valueType == "" && len(s) == len(dateLayout) {
				v, err = parseDate(s)
			} else {
				v, err = r.dateTime(s, tzid)
			}
		default:
			err = fmt.Errorf("%w: VALUE=%s", ErrInvalid, valueType)
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Name, err)
		}
		out = append(out, v)
	}
	return out, nil
}

func (r *Resolver) period(s, tzid string) (Value, error) {
	startText, endText, ok := strings.Cut(s, "/")
	if !ok {
		return Value{}, fmt.Errorf("%w: period %q", ErrInvalid, s)
	}
	v, err := r.dateTime(startText, tzid)
	if err != nil {
		return Value{}, err
	}
	if strings.HasPrefix(endText, "P") || strings.HasPrefix(endText, "+P") {
		d, err := ParseDuration(endText)
		if err != nil || d.Negative {
			return Value{}, fmt.Errorf("%w: period %q", ErrInvalid, s)
		}
		v.End = d.AddTo(v.Time)
	} else {
		e, err := r.dateTime(endText, tzid)
		if err != nil {
			return Value{}, err
		}
		v.End = e.Time
	}
	if !v.End.After(v.Time) {
		return Value{}, fmt.Errorf("%w: period %q ends before it starts", ErrInvalid, s)
	}
	return v, nil
}

func parseDate(s string) (Value, error) {
	if len(s) != len(dateLayout) || !digits(s) {
		return Value{}, fmt.Errorf("%w: date %q", ErrInvalid, s)
	}
	t, err := time.ParseInLocation(dateLayout, s, time.UTC)
	if err != nil {
		return Value{}, fmt.Errorf("%w: date %q", ErrInvalid, s)
	}
	return Value{Time: t, Kind: KindDate}, nil
}

func (r *Resolver) dateTime(s, tzid string) (Value, error) {
	utc := strings.HasSuffix(s, "Z")
	body := strings.TrimSuffix(s, "Z")
	if len(body) != len(localLayout) || body[8] != 'T' || !digits(body[:8]) || !digits(body[9:]) {
		return Value{}, fmt.Errorf("%w: date-time %q", ErrInvalid, s)
	}
	wall, err := time.ParseInLocation(localLayout, body, time.UTC)
	if err != nil {
		return Value{}, fmt.Errorf("%w: date-time %q", ErrInvalid, s)
	}
	switch {
	case utc:
		return Value{Time: wall, Kind: KindUTC}, nil
	case tzid != "":
		loc, guess := r.Location(tzid, wall.Year())
		return Value{Time: wallIn(wall, loc), Kind: KindZoned, TZID: tzid, Guess: guess}, nil
	}
	return Value{Time: wallIn(wall, r.local), Kind: KindFloating}, nil
}

// wallIn places the wall clock of w (read in UTC) in loc the way RFC 5545
// 3.3.5 asks: a time in the gap of a spring transition is read with the
// offset before the gap (02:30 becomes 03:30 summer time), a time that
// occurs twice in autumn means the first of the two. time.Date leaves both
// cases unspecified and in practice differs between zones.
func wallIn(w time.Time, loc *time.Location) time.Time {
	u := time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, time.UTC)
	_, offBefore := u.Add(-26 * time.Hour).In(loc).Zone()
	_, offAfter := u.Add(26 * time.Hour).In(loc).Zone()
	var best time.Time
	for _, off := range []int{offBefore, offAfter} {
		t := u.Add(-time.Duration(off) * time.Second).In(loc)
		if t.Year() != u.Year() || t.YearDay() != u.YearDay() || t.Hour() != u.Hour() || t.Minute() != u.Minute() || t.Second() != u.Second() {
			continue
		}
		if best.IsZero() || t.Before(best) {
			best = t
		}
	}
	if best.IsZero() {
		// Not on the clock with either offset: the gap.
		best = u.Add(-time.Duration(offBefore) * time.Second).In(loc)
	}
	return best
}

func digits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return s != ""
}
