package recur

import (
	"fmt"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// Reminders (VTODO, 1.13). A VTODO has no DTEND: its times are DTSTART and
// DUE, either may be missing (RFC 5545 3.6.2). Its recurrence is read with
// the same rules as an event's (Parse, Check, the expansion of Series);
// the anchor of the rule is DTSTART, or DUE when a reminder has only that
// (iOS writes both for a dated reminder; a rule without either is not
// expanded). No other package imports rrule-go.

// Todo is one VTODO with its times read.
type Todo struct {
	Component *ical.Component
	UID       string
	// Start is DTSTART, Due is DUE; nil when missing.
	Start, Due *tz.Value
	// Completed is COMPLETED (a UTC time by RFC 5545); nil when missing.
	Completed *tz.Value
	// RecurrenceID is set on an override of one occurrence.
	RecurrenceID *tz.Value
	// Rule is nil without RRULE or when it is unsupported.
	Rule *Rule
	// Unsupported: the reminder has a recurrence TitanOS does not
	// expand (Reason says why); it is kept and shown once.
	Unsupported bool
	Reason      string
	TZGuess     bool
	series      *Series
}

// Recurring reports whether the reminder repeats in a way TitanOS
// expands.
func (t *Todo) Recurring() bool {
	return t != nil && t.series != nil && t.series.Master.Recurring()
}

// Anchor is the value the rule counts from: DTSTART, else DUE; nil
// without either.
func (t *Todo) Anchor() *tz.Value {
	if t.Start != nil {
		return t.Start
	}
	return t.Due
}

// TodoFile is a parsed calendar file of reminders.
type TodoFile struct {
	Calendar *ical.Calendar
	// Todos are the VTODOs in the order of the file.
	Todos []*Todo
	file  *File
}

// ReadTodos parses an .ics file under the limits of Read (size, depth,
// components, parameters, the size of a VTIMEZONE with opt.MaxZone) and
// reads its VTODOs; other components stay in Calendar unread. At most
// MaxEvents VTODOs.
func ReadTodos(b []byte, opt Options) (*TodoFile, error) {
	if opt.Local == nil {
		opt.Local = time.UTC
	}
	maxSize := opt.MaxSize
	if maxSize <= 0 {
		maxSize = MaxSize
	}
	if len(b) > maxSize {
		return nil, fmt.Errorf("%w: larger than %d bytes", ErrLimit, maxSize)
	}
	if err := prescan(b, MaxEvents, opt.MaxZone); err != nil {
		return nil, err
	}
	cal, err := decode(b)
	if err != nil {
		return nil, err
	}
	f := &File{Calendar: cal, opt: opt, tz: tz.NewResolver(opt.Local, cal)}
	out := &TodoFile{Calendar: cal, file: f}
	for _, c := range cal.Children {
		if c.Name != ical.CompToDo {
			continue
		}
		if len(out.Todos) >= MaxEvents {
			return nil, fmt.Errorf("%w: more than %d VTODOs", ErrLimit, MaxEvents)
		}
		t, err := f.readTodo(c)
		if err != nil {
			return nil, err
		}
		out.Todos = append(out.Todos, t)
	}
	return out, nil
}

// readTodo reads the times and the recurrence of one VTODO.
func (f *File) readTodo(c *ical.Component) (*Todo, error) {
	t := &Todo{Component: c}
	uid := c.Props.Get(ical.PropUID)
	if uid == nil || strings.TrimSpace(uid.Value) == "" {
		return nil, fmt.Errorf("%w: VTODO without UID", ErrInvalid)
	}
	t.UID = uid.Value
	read := func(name string) (*tz.Value, error) {
		vs := c.Props.Values(name)
		if len(vs) == 0 {
			return nil, nil
		}
		if len(vs) > 1 {
			return nil, fmt.Errorf("%w: more than one %s", ErrInvalid, name)
		}
		v, err := f.tz.Parse(&vs[0])
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		t.TZGuess = t.TZGuess || v.Guess
		return &v, nil
	}
	var err error
	if t.Start, err = read(ical.PropDateTimeStart); err != nil {
		return nil, err
	}
	if t.Due, err = read(ical.PropDue); err != nil {
		return nil, err
	}
	if len(c.Props.Values(ical.PropDue)) > 0 && len(c.Props.Values(ical.PropDuration)) > 0 {
		return nil, fmt.Errorf("%w: DUE and DURATION together", ErrInvalid)
	}
	if t.Completed, err = read(ical.PropCompleted); err != nil {
		return nil, err
	}
	if t.RecurrenceID, err = read(ical.PropRecurrenceID); err != nil {
		return nil, err
	}
	if t.RecurrenceID != nil {
		return t, nil // an override is one occurrence (as for events)
	}
	anchor := t.Anchor()
	ev := &Event{Component: c, UID: t.UID}
	if anchor != nil {
		ev.Start = *anchor
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
	case len(rules) == 1 && anchor == nil:
		ev.Unsupported, ev.Reason = true, "RRULE without DTSTART or DUE"
	case len(rules) == 1:
		r, err := Parse(rules[0].Value)
		if err == nil {
			err = r.Check(*anchor, f.expansionStart(*anchor))
		}
		if err != nil {
			ev.Unsupported, ev.Reason = true, err.Error()
		} else {
			ev.Rule = r
		}
	}
	if !ev.Unsupported && len(ev.RDates) > 0 {
		if anchor == nil {
			ev.Unsupported, ev.Reason = true, "RDATE without DTSTART or DUE"
		} else if y := anchor.Time.Year(); y < minYear || y > maxYear {
			ev.Unsupported, ev.Reason = true, "DTSTART out of range"
		}
	}
	t.Rule, t.Unsupported, t.Reason = ev.Rule, ev.Unsupported, ev.Reason
	if anchor != nil {
		t.series = &Series{UID: t.UID, Master: ev, file: f}
	}
	return t, nil
}

// maxNextWindows bounds the windows Next looks through (each at most
// MaxWindowDays): about 33 years, enough for every rule of the whitelist
// but a yearly one with a large INTERVAL, which then counts as ended.
const maxNextWindows = 30

// Next returns the first occurrence of the reminder's recurrence that
// starts after its anchor (the value of the next DTSTART, or DUE without
// one, in the form the anchor is written in); ok is false when the rule
// ends there, is not expanded, or failed (a panic of rrule-go is caught
// and logged like for events).
func (t *Todo) Next() (tz.Value, bool) {
	if !t.Recurring() {
		return tz.Value{}, false
	}
	s := t.series
	from := span0(s.Master.Start, s.file.opt.Local).Add(time.Second)
	for range maxNextWindows {
		to := from.AddDate(0, 0, MaxWindowDays)
		x, err := s.Expand(from, to)
		if err != nil || x.Failed || x.Unsupported {
			return tz.Value{}, false
		}
		if len(x.Occurrences) > 0 {
			return x.Occurrences[0].RecurrenceID, true
		}
		if x.Truncated {
			return tz.Value{}, false
		}
		from = to
	}
	return tz.Value{}, false
}

// span0 is the instant of a value on the timeline (dates at 00:00 and
// floating times in the NAS zone).
func span0(v tz.Value, local *time.Location) time.Time {
	start, _ := span(v, tz.Duration{}, local)
	return start
}
