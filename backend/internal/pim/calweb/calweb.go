// Package calweb is what the web API of T-Kalender does with events
// (TECHNIK 7.1): the JSON form of an event and of an occurrence, building a
// new iCalendar file, editing one on its component tree (with pim/recur),
// the preview of a rule, the birthdays of the contacts, and splitting and
// joining .ics files for import and export. It holds no state and touches
// no disk; api/calendar*.go call it with what pim/store gives.
package calweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// ProdID names TitanOS in the files it writes.
const ProdID = "-//TitanOS//T-Kalender 1.11//DE"

// Bounds of the fields of an event.
const (
	MaxTitle    = 2000
	MaxLocation = 2000
	MaxNotes    = 32 << 10
	// MaxAlarmMinutes is four weeks.
	MaxAlarmMinutes = 4 * 7 * 24 * 60
)

// ErrInvalid marks an event, rule or time the API refuses (400).
var ErrInvalid = errors.New("calweb: invalid event")

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Opt is a field that may be left out (unchanged) or set, null included.
type Opt[T any] struct {
	Set   bool
	Value *T
}

// UnmarshalJSON is called for a present field, null included.
func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// Input is an event as the web UI sends it: the state it wants for the
// occurrence it opened (PUT) or for a new event (POST). A field left out
// stays as it is on PUT. Times: start and end are given together; a
// timed one is a wall clock "2026-11-17T19:00" (seconds and an RFC 3339
// offset allowed) in timezone, an all-day one a date "2026-11-17" with end
// the day after the last day (exclusive, as DTEND).
type Input struct {
	Title    *string `json:"title"`
	Start    string  `json:"start"`
	End      string  `json:"end"`
	AllDay   bool    `json:"allDay"`
	Timezone string  `json:"timezone"`
	Location *string `json:"location"`
	Notes    *string `json:"notes"`
	// Repeat: null = no repetition, a rule with custom:true = keep the
	// rule the event has.
	Repeat Opt[recur.Simple] `json:"repeat"`
	// Alarms: minutes before the start; null or [] = none.
	Alarms Opt[[]int] `json:"alarms"`
}

// RuleView is the rule of a series for the UI: the simple form and its
// text in German and English.
type RuleView struct {
	recur.Simple
	Text map[string]string `json:"text"`
}

// Birthday is what a birthday occurrence says; the UI words it
// ("Geburtstag: Anna (35)").
type Birthday struct {
	Name string `json:"name"`
	// Year of birth and age in that year, when the contact has a year.
	Year int  `json:"year,omitempty"`
	Age  *int `json:"age,omitempty"`
}

// View is one occurrence (the event list) or one event (GET with etag
// and notes). Start/End: RFC 3339 in the zone of the event for timed
// ones, dates (end exclusive) for all-day ones. Texts are plain text.
type View struct {
	Kind       string `json:"kind"` // "event" or "birthday"
	ID         string `json:"id"`   // the file name of the entry (URL-encode it)
	Occurrence string `json:"occurrence"`
	Calendar   string `json:"calendar"`
	UID        string `json:"uid"`
	Title      string `json:"title"`
	Start      string `json:"start"`
	End        string `json:"end"`
	AllDay     bool   `json:"allDay"`
	// Timezone of the start: an IANA name, "UTC", or "" for all-day and
	// floating times (those follow the zone of the NAS).
	Timezone string `json:"timezone"`
	Floating bool   `json:"floating,omitempty"`
	Location string `json:"location"`
	// Clipped (lists only): title or location is longer than the list
	// keeps (256 bytes); GET of the one event has the whole text.
	Clipped bool `json:"clipped,omitempty"`
	// Notes are only in the answer for one event.
	Notes            *string   `json:"notes,omitempty"`
	Recurring        bool      `json:"recurring"`
	Rule             *RuleView `json:"rule"`
	Alarms           []int     `json:"alarms"`
	Exception        bool      `json:"exception"`
	ETag             string    `json:"etag"`
	TZGuess          bool      `json:"tzGuess"`
	RecurUnsupported bool      `json:"recurUnsupported"`
	ReadOnly         bool      `json:"readOnly"`
	Birthday         *Birthday `json:"birthday,omitempty"`
}

// ruleView describes a rule read in zone loc.
func ruleView(r *recur.Rule, loc *time.Location) *RuleView {
	if r == nil {
		return nil
	}
	return &RuleView{Simple: r.Simple(loc), Text: map[string]string{"de": r.Describe("de", loc), "en": r.Describe("en", loc)}}
}

// RuleText is the rule of a Simple in words for a start, de and en.
func RuleText(r *recur.Rule, loc *time.Location) map[string]string {
	return ruleView(r, loc).Text
}

const (
	dateLayout = "2006-01-02"
	timeLayout = time.RFC3339
)

// formatValue writes a start or end of the file for the UI.
func formatValue(v tz.Value) string {
	if v.IsDate() {
		return v.Time.Format(dateLayout)
	}
	return v.Time.Format(timeLayout)
}

// Zone resolves a zone name of the UI or the index: "UTC", an IANA or
// Windows name; nil for anything else.
func Zone(name string) *time.Location {
	if name == "UTC" {
		return time.UTC
	}
	return tz.Known(name)
}

// checkText bounds a text field; lines are allowed only where multi is
// set (notes). CR LF becomes LF.
func checkText(field string, s string, limit int, multi bool) (string, error) {
	if len(s) > limit || !utf8.ValidString(s) {
		return "", invalid("%s too long or not UTF-8", field)
	}
	if multi {
		s = strings.ReplaceAll(s, "\r\n", "\n")
	}
	for _, r := range s {
		if r == 0x7f || (r < 0x20 && r != '\t' && !(multi && r == '\n')) {
			return "", invalid("%s holds a control character", field)
		}
	}
	return s, nil
}

func checkAlarms(v []int) ([]int, error) {
	if len(v) > recur.MaxAlarms {
		return nil, invalid("at most %d alarms", recur.MaxAlarms)
	}
	out := []int{}
	for _, m := range v {
		if m < 0 || m > MaxAlarmMinutes {
			return nil, invalid("alarm %d minutes before", m)
		}
		if !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	slices.Sort(out)
	return out, nil
}

// times are the start and end the input asks for, in the form they will
// be written.
type times struct {
	start, end tz.Value
}

// zoneHint is the zone of the event as it is: used when the input names
// none, and its TZID text is kept when the input names the same zone
// (a Windows name keeps its VTIMEZONE).
type zoneHint struct {
	kind tz.Kind // KindZoned, KindUTC or KindFloating; 0 = none
	name string  // ZoneName (View.Timezone)
	tzid string  // the TZID parameter as written
	loc  *time.Location
}

// parseTimes reads start/end of an input. local is the zone of the NAS;
// hint the zone of the existing event (zero for a new one).
func parseTimes(in Input, local *time.Location, hint zoneHint) (times, error) {
	if in.Start == "" {
		return times{}, invalid("start missing")
	}
	if in.AllDay {
		day := func(s string) (time.Time, error) {
			if len(s) > 10 {
				s = s[:10]
			}
			return time.ParseInLocation(dateLayout, s, time.UTC)
		}
		st, err := day(in.Start)
		if err != nil {
			return times{}, invalid("start %q is no date", in.Start)
		}
		en := st.AddDate(0, 0, 1)
		if in.End != "" {
			if en, err = day(in.End); err != nil {
				return times{}, invalid("end %q is no date", in.End)
			}
		}
		if !en.After(st) {
			return times{}, invalid("end must be after the start (all-day: the day after the last day)")
		}
		if y := st.Year(); y < 1900 || y > 2200 || en.Sub(st) > 10000*24*time.Hour {
			return times{}, invalid("date out of range")
		}
		return times{start: tz.Value{Time: st, Kind: tz.KindDate}, end: tz.Value{Time: en, Kind: tz.KindDate}}, nil
	}
	kind, tzid, loc := tz.KindZoned, "", local
	switch {
	case in.Timezone == "" && hint.kind != 0:
		kind, tzid, loc = hint.kind, hint.tzid, hint.loc
	case in.Timezone == "":
		tzid = local.String()
		if tzid == "UTC" || tzid == "Local" || Zone(tzid) == nil {
			kind, tzid, loc = tz.KindUTC, "", time.UTC
		}
	case hint.kind != 0 && in.Timezone == hint.name:
		kind, tzid, loc = hint.kind, hint.tzid, hint.loc
	case in.Timezone == "UTC":
		kind, loc = tz.KindUTC, time.UTC
	default:
		loc = Zone(in.Timezone)
		if loc == nil {
			return times{}, invalid("unknown time zone %q", in.Timezone)
		}
		tzid = loc.String()
	}
	if kind == tz.KindZoned && tzid == "" {
		tzid = loc.String()
	}
	if kind == tz.KindFloating && loc == nil {
		loc = local
	}
	wall := func(s string) (time.Time, error) {
		for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
			if t, err := time.ParseInLocation(layout, s, loc); err == nil {
				return t, nil
			}
		}
		t, err := time.Parse(time.RFC3339, s)
		return t.In(loc), err
	}
	st, err := wall(in.Start)
	if err != nil {
		return times{}, invalid("start %q is no time", in.Start)
	}
	en := st.Add(time.Hour)
	if in.End != "" {
		if en, err = wall(in.End); err != nil {
			return times{}, invalid("end %q is no time", in.End)
		}
	}
	if en.Before(st) {
		return times{}, invalid("end before the start")
	}
	if y := st.Year(); y < 1900 || y > 2200 || en.Sub(st) > 10000*24*time.Hour {
		return times{}, invalid("time out of range")
	}
	v := func(t time.Time) tz.Value {
		val := tz.Value{Time: t, Kind: kind}
		if kind == tz.KindZoned {
			val.TZID = tzid
		}
		if kind == tz.KindUTC {
			val.Time = t.UTC()
		}
		return val
	}
	return times{start: v(st), end: v(en)}, nil
}
