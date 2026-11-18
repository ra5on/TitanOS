package calweb

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// NewUID returns a random UID in the form of a UUID (lower case).
func NewUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand does not fail on Linux
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}

func stamp(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

func setStamp(c *ical.Component, name string, now time.Time) {
	p := ical.NewProp(name)
	p.Value = stamp(now)
	c.Props.Set(p)
}

// Build makes the file of a new event with the UID given: VCALENDAR with
// PRODID, the VTIMEZONE of its zone, and one VEVENT with DTSTAMP,
// DTSTART/DTEND (TZID, UTC or VALUE=DATE), RRULE, LOCATION, DESCRIPTION
// and a VALARM (ACTION:DISPLAY) per alarm.
func Build(in Input, uid string, now time.Time, local *time.Location) ([]byte, error) {
	t, err := parseTimes(in, local, zoneHint{})
	if err != nil {
		return nil, err
	}
	ev := ical.NewComponent(ical.CompEvent)
	ev.Props.SetText(ical.PropUID, uid)
	for _, name := range []string{ical.PropDateTimeStamp, ical.PropCreated, ical.PropLastModified} {
		setStamp(ev, name, now)
	}
	seq := ical.NewProp(ical.PropSequence)
	seq.Value = "0"
	ev.Props.Set(seq)
	setTimes(ev, t)
	for _, f := range []struct {
		prop  string
		value *string
		limit int
		multi bool
	}{{ical.PropSummary, in.Title, MaxTitle, false}, {ical.PropLocation, in.Location, MaxLocation, false}, {ical.PropDescription, in.Notes, MaxNotes, true}} {
		if f.value == nil {
			continue
		}
		text, err := checkText(f.prop, *f.value, f.limit, f.multi)
		if err != nil {
			return nil, err
		}
		if text != "" {
			ev.Props.SetText(f.prop, text)
		}
	}
	if in.Repeat.Value != nil {
		if err := setRepeat(ev, *in.Repeat.Value, t.start); err != nil {
			return nil, err
		}
	}
	if in.Alarms.Value != nil {
		alarms, err := checkAlarms(*in.Alarms.Value)
		if err != nil {
			return nil, err
		}
		applyAlarms(ev, alarms)
	}
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, ProdID)
	cal.Props.SetText(ical.PropCalendarScale, "GREGORIAN")
	cal.Children = append(cal.Children, ev)
	ensureZones(cal)
	return encode(cal)
}

func encode(cal *ical.Calendar) ([]byte, error) {
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return buf.Bytes(), nil
}

// setTimes writes DTSTART and DTEND (a DURATION goes).
func setTimes(c *ical.Component, t times) {
	c.Props.Set(t.start.Prop(ical.PropDateTimeStart))
	c.Props.Del(ical.PropDuration)
	c.Props.Set(t.end.Prop(ical.PropDateTimeEnd))
}

// setRepeat writes the RRULE of a simple rule for a start.
func setRepeat(c *ical.Component, s recur.Simple, start tz.Value) error {
	if s.Custom {
		return invalid("a custom rule cannot be written from its simple form")
	}
	r, err := s.Rule(start)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	p := ical.NewProp(ical.PropRecurrenceRule)
	if old := c.Props.Get(ical.PropRecurrenceRule); old != nil {
		p.Params = old.Params
	}
	p.Value = r.String()
	c.Props.Set(p)
	return nil
}

func newAlarm(minutes int) *ical.Component {
	a := ical.NewComponent(ical.CompAlarm)
	a.Props.SetText(ical.PropAction, "DISPLAY")
	trig := ical.NewProp(ical.PropTrigger)
	// Minutes before the start; negative: after it (1.13, "am Tag um 9:00").
	after := minutes < 0
	if after {
		minutes = -minutes
	}
	trig.Value = tz.Duration{Negative: !after && minutes > 0, Exact: time.Duration(minutes) * time.Minute}.String()
	a.Props.Set(trig)
	a.Props.SetText(ical.PropDescription, "Reminder")
	a.Props.SetText(ical.PropUID, NewUID())
	return a
}

// applyAlarms makes the editable alarms of c (recur.AlarmMinutes) those of
// want: an alarm whose minutes stay is kept as it is (its UID, ACTION and
// X- properties), one whose minutes went is removed, a new one is a
// DISPLAY alarm. Alarms the UI cannot show stay untouched.
func applyAlarms(c *ical.Component, want []int) {
	var keep []*ical.Component
	have := map[int]bool{}
	for _, ch := range c.Children {
		if m, ok := recur.AlarmMinutes(ch); ok {
			if slices.Contains(want, m) && !have[m] {
				have[m] = true
				keep = append(keep, ch)
			}
			continue
		}
		keep = append(keep, ch)
	}
	for _, m := range want {
		if !have[m] {
			keep = append(keep, newAlarm(m))
		}
	}
	c.Children = keep
}

// ensureZones adds a VTIMEZONE for each TZID the events use without one
// (RFC 5545 3.6.5), for names TitanOS knows (IANA, Windows). It reports
// whether it added any.
func ensureZones(cal *ical.Calendar) bool {
	have := map[string]bool{}
	var events []*ical.Component
	for _, c := range cal.Children {
		if c.Name == ical.CompTimezone {
			if p := c.Props.Get(ical.PropTimezoneID); p != nil {
				have[p.Value] = true
			}
			continue
		}
		events = append(events, c)
	}
	var zones []*ical.Component
	var walk func(c *ical.Component)
	walk = func(c *ical.Component) {
		for _, props := range c.Props {
			for _, p := range props {
				id := p.Params.Get(ical.ParamTimezoneID)
				if id == "" || have[id] {
					continue
				}
				have[id] = true
				loc := tz.Known(id)
				if loc == nil {
					continue
				}
				year := time.Now().Year()
				if len(p.Value) >= 4 {
					if y, err := strconv.Atoi(p.Value[:4]); err == nil && y >= 1900 && y <= 2200 {
						year = y
					}
				}
				// From the year before: the offset before the first change
				// of the event's year is described too.
				zones = append(zones, tz.VTimezone(id, loc, year-1, year+10))
			}
		}
		for _, ch := range c.Children {
			walk(ch)
		}
	}
	for _, c := range events {
		walk(c)
	}
	if len(zones) == 0 {
		return false
	}
	at := 0
	for at < len(cal.Children) && cal.Children[at].Name == ical.CompTimezone {
		at++
	}
	cal.Children = slices.Insert(cal.Children, at, zones...)
	return true
}

// EnsureZones adds missing VTIMEZONEs to a file written by an edit; the
// bytes stay as they are when none is missing.
func EnsureZones(data []byte) ([]byte, error) {
	if data == nil {
		return nil, nil
	}
	cal, err := ical.NewDecoder(bytes.NewReader(data)).Decode()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if !ensureZones(cal) {
		return data, nil
	}
	return encode(cal)
}
