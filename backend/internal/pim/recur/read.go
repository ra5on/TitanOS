package recur

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// Limits against hostile files. A file over a limit is refused as a whole.
const (
	// MaxSize is the largest .ics entry (TECHNIK 5.3).
	MaxSize = 1 << 20
	// MaxEvents bounds the VEVENT components of one file.
	MaxEvents = 1000
	// MaxComponents bounds all components (VALARM, VTIMEZONE … included).
	MaxComponents = 5000
	// MaxDepth bounds the nesting of BEGIN/END; go-ical decodes recursively.
	MaxDepth = 8
	// MaxDates bounds the EXDATE and the RDATE values of one VEVENT (each).
	MaxDates = 1000
	// maxParamBytes bounds the parameter part of a content line; go-ical
	// builds parameter values byte by byte, which is quadratic.
	maxParamBytes = 8 << 10
)

var (
	// ErrInvalid marks bytes that are no readable iCalendar object.
	ErrInvalid = errors.New("invalid calendar data")
	// ErrLimit marks a file over one of the limits above.
	ErrLimit = errors.New("calendar data over the limits")
)

// Options configure Read.
type Options struct {
	// Local is the zone of the NAS: floating times and dates are read in it.
	Local *time.Location
	// Account and Name label log lines (a panic inside rrule-go); never
	// the content.
	Account, Name string
	// MaxSize overrides MaxSize, for imports that split a big file
	// afterwards; 0 = MaxSize.
	MaxSize int
	// MaxEvents overrides MaxEvents; 0 = MaxEvents.
	MaxEvents int
}

// File is a parsed calendar file.
type File struct {
	Calendar *ical.Calendar
	// Series holds one entry per UID in the order of first appearance.
	Series []*Series
	opt    Options
	tz     *tz.Resolver
}

// Series is the master VEVENT of one UID and its overrides.
type Series struct {
	UID string
	// Master is the VEVENT without RECURRENCE-ID; nil when the file only
	// holds overrides (an invitation to single occurrences).
	Master    *Event
	Overrides []*Event
	file      *File
}

// Event is one VEVENT with its times read.
type Event struct {
	Component *ical.Component
	UID       string
	Start     tz.Value
	// Duration of each occurrence: exact for DTEND with a time, nominal for
	// DURATION and for dates (RFC 5545 3.8.5.3).
	Duration tz.Duration
	// Rule is nil without RRULE or when the rule is unsupported.
	Rule          *Rule
	RDates        []tz.Value
	ExDates       []tz.Value
	RecurrenceID  *tz.Value
	ThisAndFuture bool
	// Unsupported is recurUnsupported: the event has a recurrence TitanOS
	// does not expand; it shows once. Reason says why (for logs and tests).
	Unsupported bool
	Reason      string
	// TZGuess is true when one of its zones was guessed (tz.Value.Guess).
	TZGuess bool
}

// Recurring reports whether the event has a rule or extra dates TitanOS
// expands.
func (e *Event) Recurring() bool {
	return e != nil && !e.Unsupported && (e.Rule != nil || len(e.RDates) > 0)
}

// AllDay reports whether the event starts on a date.
func (e *Event) AllDay() bool { return e.Start.IsDate() }

// Read parses an .ics file under the limits and groups its VEVENTs by UID.
// Other components (VTODO, VJOURNAL) are kept in Calendar but not read.
func Read(b []byte, opt Options) (f *File, err error) {
	if opt.Local == nil {
		opt.Local = time.UTC
	}
	maxSize := opt.MaxSize
	if maxSize <= 0 {
		maxSize = MaxSize
	}
	maxEvents := opt.MaxEvents
	if maxEvents <= 0 {
		maxEvents = MaxEvents
	}
	if len(b) > maxSize {
		return nil, fmt.Errorf("%w: larger than %d bytes", ErrLimit, maxSize)
	}
	if err := prescan(b, maxEvents); err != nil {
		return nil, err
	}
	cal, err := decode(b)
	if err != nil {
		return nil, err
	}
	f = &File{Calendar: cal, opt: opt, tz: tz.NewResolver(opt.Local, cal)}
	byUID := map[string]*Series{}
	for _, c := range cal.Children {
		if c.Name != ical.CompEvent {
			continue
		}
		ev, err := f.readEvent(c)
		if err != nil {
			return nil, err
		}
		s := byUID[ev.UID]
		if s == nil {
			s = &Series{UID: ev.UID, file: f}
			byUID[ev.UID] = s
			f.Series = append(f.Series, s)
		}
		if ev.RecurrenceID == nil {
			if s.Master != nil {
				return nil, fmt.Errorf("%w: two VEVENTs with the same UID and no RECURRENCE-ID", ErrInvalid)
			}
			s.Master = ev
			continue
		}
		for _, o := range s.Overrides {
			if tz.Equal(*o.RecurrenceID, *ev.RecurrenceID) {
				return nil, fmt.Errorf("%w: two overrides with the same RECURRENCE-ID", ErrInvalid)
			}
		}
		s.Overrides = append(s.Overrides, ev)
	}
	return f, nil
}

// decode runs go-ical with a recover: a panic on hostile bytes is an
// invalid file, not a crash of titand.
func decode(b []byte) (cal *ical.Calendar, err error) {
	defer func() {
		if r := recover(); r != nil {
			cal, err = nil, fmt.Errorf("%w: decoder failed", ErrInvalid)
		}
	}()
	dec := ical.NewDecoder(bytes.NewReader(b))
	cal, err = dec.Decode()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if _, err := dec.Decode(); err != io.EOF {
		return nil, fmt.Errorf("%w: more than one VCALENDAR", ErrInvalid)
	}
	return cal, nil
}

// prescan walks the unfolded content lines once before go-ical sees them:
// nesting depth, number of components and VEVENTs, length of parameters.
func prescan(b []byte, maxEvents int) error {
	depth, comps, events := 0, 0, 0
	var line []byte // first bytes of the logical line, enough for BEGIN:VEVENT
	paramLen, inQuote, paramDone := 0, false, false
	flush := func() error {
		name := strings.ToUpper(string(line))
		switch {
		case strings.HasPrefix(name, "BEGIN:"):
			depth++
			comps++
			if depth > MaxDepth {
				return fmt.Errorf("%w: components nested deeper than %d", ErrLimit, MaxDepth)
			}
			if comps > MaxComponents {
				return fmt.Errorf("%w: more than %d components", ErrLimit, MaxComponents)
			}
			if strings.TrimSpace(name[6:]) == ical.CompEvent {
				events++
				if events > maxEvents {
					return fmt.Errorf("%w: more than %d VEVENTs", ErrLimit, maxEvents)
				}
			}
		case strings.HasPrefix(name, "END:"):
			depth--
		}
		line = line[:0]
		paramLen, inQuote, paramDone = 0, false, false
		return nil
	}
	feed := func(seg []byte) error {
		for _, c := range seg {
			if len(line) < 32 {
				line = append(line, c)
			}
			if paramDone {
				continue
			}
			switch {
			case c == '"':
				inQuote = !inQuote
			case c == ':' && !inQuote:
				paramDone = true
			}
			paramLen++
			if paramLen > maxParamBytes {
				return fmt.Errorf("%w: parameters longer than %d bytes", ErrLimit, maxParamBytes)
			}
		}
		return nil
	}
	started := false
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		var seg []byte
		if i < 0 {
			seg, b = b, nil
		} else {
			seg, b = b[:i], b[i+1:]
		}
		seg = bytes.TrimSuffix(seg, []byte{'\r'})
		if len(seg) > 0 && (seg[0] == ' ' || seg[0] == '\t') && started {
			if err := feed(seg[1:]); err != nil {
				return err
			}
			continue
		}
		if started {
			if err := flush(); err != nil {
				return err
			}
		}
		started = true
		if err := feed(seg); err != nil {
			return err
		}
	}
	if started {
		return flush()
	}
	return nil
}

func (f *File) readEvent(c *ical.Component) (*Event, error) {
	ev := &Event{Component: c}
	uid := c.Props.Get(ical.PropUID)
	if uid == nil || strings.TrimSpace(uid.Value) == "" {
		return nil, fmt.Errorf("%w: VEVENT without UID", ErrInvalid)
	}
	ev.UID = uid.Value
	sp := c.Props.Get(ical.PropDateTimeStart)
	if sp == nil {
		return nil, fmt.Errorf("%w: VEVENT without DTSTART", ErrInvalid)
	}
	start, err := f.tz.Parse(sp)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	ev.Start = start
	ev.TZGuess = start.Guess

	ends := c.Props.Values(ical.PropDateTimeEnd)
	durs := c.Props.Values(ical.PropDuration)
	switch {
	case len(ends) > 0 && len(durs) > 0:
		return nil, fmt.Errorf("%w: DTEND and DURATION together", ErrInvalid)
	case len(ends) > 0:
		end, err := f.tz.Parse(&ends[0])
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		ev.TZGuess = ev.TZGuess || end.Guess
		ev.Duration = between(start, end, f.opt.Local)
	case len(durs) > 0:
		d, err := tz.ParseDuration(durs[0].Value)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		if d.Negative {
			d = tz.Duration{}
		}
		ev.Duration = d
	case start.IsDate():
		ev.Duration = tz.Duration{Days: 1}
	}

	if rp := c.Props.Get(ical.PropRecurrenceID); rp != nil {
		rid, err := f.tz.Parse(rp)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		ev.RecurrenceID = &rid
		ev.ThisAndFuture = strings.EqualFold(rp.Params.Get("RANGE"), "THISANDFUTURE")
		// Overrides are single occurrences; a rule on them is not
		// expanded (one expansion per UID).
		return ev, nil
	}

	for _, name := range []string{ical.PropExceptionDates, ical.PropRecurrenceDates} {
		var list []tz.Value
		for i := range c.Props.Values(name) {
			vs, err := f.tz.ParseList(&c.Props.Values(name)[i])
			if err != nil {
				return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
			}
			list = append(list, vs...)
			if len(list) > MaxDates {
				return nil, fmt.Errorf("%w: more than %d %s values", ErrLimit, MaxDates, name)
			}
		}
		for _, v := range list {
			ev.TZGuess = ev.TZGuess || v.Guess
		}
		if name == ical.PropExceptionDates {
			ev.ExDates = list
		} else {
			ev.RDates = list
		}
	}

	rules := c.Props.Values(ical.PropRecurrenceRule)
	switch {
	case len(rules) > 1:
		ev.Unsupported, ev.Reason = true, "more than one RRULE"
	case len(rules) == 1:
		r, err := Parse(rules[0].Value)
		if err == nil {
			err = r.Check(start, f.expansionStart(start))
		}
		if err != nil {
			ev.Unsupported, ev.Reason = true, err.Error()
		} else {
			ev.Rule = r
		}
	}
	if !ev.Unsupported && len(ev.RDates) > 0 {
		if y := start.Time.Year(); y < minYear || y > maxYear {
			ev.Unsupported, ev.Reason = true, "DTSTART out of range"
		}
	}
	return ev, nil
}

// between is the duration from start to end: whole days between two
// dates, else the exact time. An end before the start counts as none.
func between(start, end tz.Value, local *time.Location) tz.Duration {
	if start.IsDate() && end.IsDate() {
		days := int(end.Time.Sub(start.Time).Hours() / 24)
		if days < 0 {
			days = 0
		}
		return tz.Duration{Days: days}
	}
	d := end.InZone(local).Sub(start.InZone(local))
	if d < 0 {
		d = 0
	}
	return tz.Duration{Exact: d}
}

// expansionStart is DTSTART in the zone rrule-go works in: UTC for dates
// (no missing midnights), the NAS zone for floating times, else its own.
func (f *File) expansionStart(v tz.Value) time.Time {
	if v.Kind == tz.KindFloating {
		return v.InZone(f.opt.Local)
	}
	return v.Time
}
