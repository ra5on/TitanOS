package tasks

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/calweb"
	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	"github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

const stampLayout = "20060102T150405Z"

func setStamp(c *ical.Component, name string, now time.Time) {
	p := ical.NewProp(name)
	p.Value = now.UTC().Format(stampLayout)
	c.Props.Set(p)
}

// touch marks a changed reminder: DTSTAMP, LAST-MODIFIED, SEQUENCE+1.
func touch(c *ical.Component, now time.Time) {
	setStamp(c, ical.PropDateTimeStamp, now)
	setStamp(c, ical.PropLastModified, now)
	n := 0
	if p := c.Props.Get(ical.PropSequence); p != nil {
		n, _ = strconv.Atoi(strings.TrimSpace(p.Value))
	}
	seq := ical.NewProp(ical.PropSequence)
	seq.Value = strconv.Itoa(max(n, 0) + 1)
	c.Props.Set(seq)
}

func checkText(field, s string, limit int, multi bool) (string, error) {
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

// zoneOf is the zone an input time is read in: the one named, else that
// of the value the reminder has (hint), else the NAS zone.
func zoneOf(name string, hint *tz.Value, local *time.Location) (tz.Kind, string, *time.Location, error) {
	switch {
	case name == "" && hint != nil && !hint.IsDate():
		switch hint.Kind {
		case tz.KindUTC:
			return tz.KindUTC, "", time.UTC, nil
		case tz.KindFloating:
			return tz.KindFloating, "", local, nil
		}
		return tz.KindZoned, hint.TZID, hint.Time.Location(), nil
	case name == "":
		id := local.String()
		if id == "UTC" || id == "Local" || calweb.Zone(id) == nil {
			return tz.KindUTC, "", time.UTC, nil
		}
		return tz.KindZoned, id, local, nil
	case name == "UTC":
		return tz.KindUTC, "", time.UTC, nil
	case hint != nil && hint.Kind == tz.KindZoned && store.ZoneName(*hint) == name:
		return tz.KindZoned, hint.TZID, hint.Time.Location(), nil
	}
	loc := calweb.Zone(name)
	if loc == nil {
		return 0, "", nil, invalid("unknown time zone %q", name)
	}
	return tz.KindZoned, loc.String(), loc, nil
}

// parseWhen reads a date or a time of the input.
func parseWhen(field, s, zone string, hint *tz.Value, local *time.Location) (tz.Value, error) {
	s = strings.TrimSpace(s)
	if len(s) == len(time.DateOnly) {
		d, err := time.ParseInLocation(time.DateOnly, s, time.UTC)
		if err != nil {
			return tz.Value{}, invalid("%s %q is no date", field, s)
		}
		if y := d.Year(); y < 1900 || y > 2200 {
			return tz.Value{}, invalid("%s out of range", field)
		}
		return tz.Value{Time: d, Kind: tz.KindDate}, nil
	}
	kind, tzid, loc, err := zoneOf(zone, hint, local)
	if err != nil {
		return tz.Value{}, err
	}
	var at time.Time
	ok := false
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			at, ok = t, true
			break
		}
	}
	if !ok {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return tz.Value{}, invalid("%s %q is no time", field, s)
		}
		at = t.In(loc)
	}
	if y := at.Year(); y < 1900 || y > 2200 {
		return tz.Value{}, invalid("%s out of range", field)
	}
	at = at.Truncate(time.Second)
	if kind == tz.KindUTC {
		at = at.UTC()
	}
	return tz.Value{Time: at, Kind: kind, TZID: tzid}, nil
}

// parsed is a stored reminder, read for editing.
type parsed struct {
	cal    *ical.Calendar
	master *recur.Todo
	file   *recur.TodoFile
}

func open(data []byte, local *time.Location) (*parsed, error) {
	f, err := recur.ReadTodos(data, recur.Options{Local: local})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", store.ErrInvalidData, err)
	}
	p := &parsed{cal: f.Calendar, file: f}
	for _, t := range f.Todos {
		if t.RecurrenceID == nil {
			p.master = t
			break
		}
	}
	if p.master == nil {
		if len(f.Todos) == 0 {
			return nil, fmt.Errorf("%w: no VTODO", store.ErrInvalidData)
		}
		p.master = f.Todos[0]
	}
	return p, nil
}

func encode(cal *ical.Calendar) ([]byte, error) {
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return calweb.EnsureZones(buf.Bytes())
}

// alarmAt is the time an alarm fires, when TitanOS can place it: an
// absolute TRIGGER, or a duration related to DTSTART (else DUE) or, with
// RELATED=END, to DUE.
func alarmAt(a *ical.Component, t *recur.Todo, local *time.Location) (time.Time, bool) {
	p := a.Props.Get(ical.PropTrigger)
	if p == nil {
		return time.Time{}, false
	}
	if strings.EqualFold(p.Params.Get(ical.ParamValue), "DATE-TIME") {
		at, err := time.Parse(stampLayout, strings.TrimSpace(p.Value))
		return at, err == nil
	}
	d, err := tz.ParseDuration(p.Value)
	if err != nil {
		return time.Time{}, false
	}
	base := t.Start
	if strings.EqualFold(p.Params.Get(ical.ParamRelated), "END") || base == nil {
		base = t.Due
	}
	if base == nil {
		return time.Time{}, false
	}
	at := base.InZone(local)
	if d.Negative {
		return at.AddDate(0, 0, -d.Days).Add(-d.Exact), true
	}
	return at.AddDate(0, 0, d.Days).Add(d.Exact), true
}

func newUID() string { return calweb.NewUID() }

func randomID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand does not fail on Linux
	return strings.ToUpper(hex.EncodeToString(b))
}

func newAlarm(at time.Time) *ical.Component {
	a := ical.NewComponent(ical.CompAlarm)
	a.Props.SetText(ical.PropAction, "DISPLAY")
	a.Props.SetText(ical.PropDescription, "Reminder")
	trig := ical.NewProp(ical.PropTrigger)
	trig.Params.Set(ical.ParamValue, "DATE-TIME")
	trig.Value = at.UTC().Format(stampLayout)
	a.Props.Set(trig)
	a.Props.SetText(ical.PropUID, newUID())
	return a
}

// applyAlarms makes the alarms TitanOS can place those of want: one whose
// time stays is kept as it is (UID, X- properties), one whose time went
// is removed, a new one is an absolute DISPLAY alarm. Alarms it cannot
// place stay.
func applyAlarms(c *ical.Component, t *recur.Todo, want []time.Time, local *time.Location) bool {
	var keep []*ical.Component
	have := map[int64]bool{}
	changed := false
	for _, ch := range c.Children {
		if ch.Name != ical.CompAlarm {
			keep = append(keep, ch)
			continue
		}
		at, ok := alarmAt(ch, t, local)
		if !ok {
			keep = append(keep, ch)
			continue
		}
		if slices.ContainsFunc(want, at.Equal) && !have[at.Unix()] {
			have[at.Unix()] = true
			keep = append(keep, ch)
			continue
		}
		changed = true
	}
	for _, at := range want {
		if !have[at.Unix()] {
			have[at.Unix()] = true
			keep = append(keep, newAlarm(at))
			changed = true
		}
	}
	c.Children = keep
	return changed
}

func (in Input) alarms(t *recur.Todo, local *time.Location) ([]time.Time, error) {
	if in.Alarms.Value == nil {
		return nil, nil
	}
	if len(*in.Alarms.Value) > MaxAlarms {
		return nil, invalid("at most %d alarms", MaxAlarms)
	}
	var hint *tz.Value
	if t != nil {
		if hint = t.Due; hint == nil {
			hint = t.Start
		}
	}
	var out []time.Time
	for _, s := range *in.Alarms.Value {
		v, err := parseWhen("alarm", s, in.Timezone, hint, local)
		if err != nil {
			return nil, err
		}
		at := v.InZone(local).UTC()
		if !slices.ContainsFunc(out, at.Equal) {
			out = append(out, at)
		}
	}
	return out, nil
}

func setRepeat(c *ical.Component, s recur.Simple, anchor *tz.Value) error {
	if anchor == nil {
		return invalid("a repetition needs a date (due or start)")
	}
	r, err := s.Rule(*anchor)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if err := r.Check(*anchor, anchor.InZone(time.UTC)); err != nil {
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

func checkPriority(p *int) (int, error) {
	if p == nil {
		return 0, nil
	}
	if *p < 0 || *p > 9 {
		return 0, invalid("priority %d (0–9)", *p)
	}
	return *p, nil
}

func checkParent(s string) (string, error) {
	s = strings.TrimSpace(s)
	return checkText("parent", s, MaxParent, false)
}

// setParent writes RELATED-TO (parameters of the one replaced stay);
// "" removes the parent relation (other relations stay).
func setParent(c *ical.Component, uid string) bool {
	if store.ParentUID(c) == uid {
		return false
	}
	var keep []ical.Prop
	var params ical.Params
	for _, p := range c.Props.Values(ical.PropRelatedTo) {
		rel := p.Params.Get(ical.ParamRelationshipType)
		if rel == "" || strings.EqualFold(rel, "PARENT") {
			if params == nil {
				params = p.Params
			}
			continue
		}
		keep = append(keep, p)
	}
	if uid != "" {
		p := ical.NewProp(ical.PropRelatedTo)
		if params != nil {
			p.Params = params
		}
		p.Value = uid
		keep = append(keep, *p)
	}
	if len(keep) == 0 {
		c.Props.Del(ical.PropRelatedTo)
	} else {
		c.Props[ical.PropRelatedTo] = keep
	}
	return true
}

// setText sets or (for "") removes a text property; whether it changed.
func setText(c *ical.Component, name, value string) bool {
	old, _ := c.Props.Text(name)
	if c.Props.Get(name) != nil && old == value {
		return false
	}
	if value == "" {
		if c.Props.Get(name) == nil {
			return false
		}
		c.Props.Del(name)
		return true
	}
	c.Props.SetText(name, value)
	return true
}

func setValue(c *ical.Component, name string, v *tz.Value) bool {
	old := c.Props.Get(name)
	if v == nil {
		if old == nil {
			return false
		}
		c.Props.Del(name)
		return true
	}
	p := v.Prop(name)
	if old != nil {
		for k, vs := range old.Params {
			if k != ical.ParamTimezoneID && k != ical.ParamValue {
				p.Params[k] = vs
			}
		}
		if old.Value == p.Value && old.Params.Get(ical.ParamTimezoneID) == p.Params.Get(ical.ParamTimezoneID) &&
			strings.EqualFold(old.Params.Get(ical.ParamValue), p.Params.Get(ical.ParamValue)) {
			return false
		}
	}
	c.Props.Set(p)
	return true
}

func setInt(c *ical.Component, name string, n int) bool {
	old := c.Props.Get(name)
	if n == 0 {
		if old == nil {
			return false
		}
		c.Props.Del(name)
		return true
	}
	if old != nil && strings.TrimSpace(old.Value) == strconv.Itoa(n) {
		return false
	}
	p := ical.NewProp(name)
	p.Value = strconv.Itoa(n)
	c.Props.Set(p)
	return true
}

// Build makes the file of a new reminder with the UID given: VCALENDAR
// with PRODID, one VTODO with DTSTAMP, CREATED, LAST-MODIFIED, SEQUENCE,
// SUMMARY, DESCRIPTION, DTSTART/DUE (TZID, UTC or VALUE=DATE; DTSTART
// follows DUE unless given), PRIORITY, STATUS:NEEDS-ACTION, RELATED-TO,
// RRULE and absolute alarms, and the VTIMEZONE of its zone.
func Build(in Input, uid string, now time.Time, local *time.Location) ([]byte, error) {
	if in.Title == nil || strings.TrimSpace(*in.Title) == "" {
		return nil, invalid("a reminder needs a title")
	}
	c := ical.NewComponent(ical.CompToDo)
	c.Props.SetText(ical.PropUID, uid)
	for _, name := range []string{ical.PropDateTimeStamp, ical.PropCreated, ical.PropLastModified} {
		setStamp(c, name, now)
	}
	seq := ical.NewProp(ical.PropSequence)
	seq.Value = "0"
	c.Props.Set(seq)
	title, err := checkText("title", strings.TrimSpace(*in.Title), MaxTitle, false)
	if err != nil {
		return nil, err
	}
	c.Props.SetText(ical.PropSummary, title)
	if in.Notes != nil {
		notes, err := checkText("notes", *in.Notes, MaxNotes, true)
		if err != nil {
			return nil, err
		}
		setText(c, ical.PropDescription, notes)
	}
	var due, start *tz.Value
	if in.Due.Value != nil {
		v, err := parseWhen("due", *in.Due.Value, in.Timezone, nil, local)
		if err != nil {
			return nil, err
		}
		due = &v
	}
	switch {
	case in.Start.Value != nil:
		v, err := parseWhen("start", *in.Start.Value, in.Timezone, due, local)
		if err != nil {
			return nil, err
		}
		start = &v
	case !in.Start.Set:
		start = due
	}
	setValue(c, ical.PropDateTimeStart, start)
	setValue(c, ical.PropDue, due)
	prio, err := checkPriority(in.Priority)
	if err != nil {
		return nil, err
	}
	setInt(c, ical.PropPriority, prio)
	c.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
	if in.Parent.Value != nil {
		parent, err := checkParent(*in.Parent.Value)
		if err != nil {
			return nil, err
		}
		setParent(c, parent)
	}
	if in.Repeat.Value != nil {
		anchor := start
		if anchor == nil {
			anchor = due
		}
		if err := setRepeat(c, *in.Repeat.Value, anchor); err != nil {
			return nil, err
		}
	}
	cal := ical.NewCalendar()
	cal.Props.SetText(ical.PropVersion, "2.0")
	cal.Props.SetText(ical.PropProductID, ProdID)
	cal.Props.SetText(ical.PropCalendarScale, "GREGORIAN")
	cal.Children = append(cal.Children, c)
	t := &recur.Todo{Component: c, Start: start, Due: due}
	want, err := in.alarms(t, local)
	if err != nil {
		return nil, err
	}
	applyAlarms(c, t, want, local)
	return encode(cal)
}

// Open is the reminder of a stored file for the page.
func Open(list, name, etag string, data []byte, local *time.Location) (Task, error) {
	p, err := open(data, local)
	if err != nil {
		return Task{}, err
	}
	return view(list, name, etag, p.master, len(p.file.Todos) > 1, local), nil
}

func view(list, name, etag string, t *recur.Todo, overrides bool, local *time.Location) Task {
	c := t.Component
	ti := store.TodoInfo(t, overrides)
	o := store.ObjectInfo{Name: name, Entry: store.Entry{ETag: etag, UID: t.UID, Task: ti}}
	o.Summary, _ = c.Props.Text(ical.PropSummary)
	out := Task{Item: itemOf(list, o, local)}
	out.Notes, _ = c.Props.Text(ical.PropDescription)
	out.Start = formatTime(ti.Start, ti.StartAllDay, ti.StartFloating, zoneName(t.Start), local)
	out.StartAllDay = ti.StartAllDay
	if t.Due != nil {
		out.Timezone = store.ZoneName(*t.Due)
	} else if t.Start != nil {
		out.Timezone = store.ZoneName(*t.Start)
	}
	out.Status, out.Percent, out.RecurUnsupported = ti.Status, ti.Percent, ti.RecurUnsupported
	zone := local
	if l := calweb.Zone(out.Timezone); l != nil {
		zone = l
	}
	for _, a := range c.Children {
		if a.Name != ical.CompAlarm {
			continue
		}
		if at, ok := alarmAt(a, t, local); ok {
			out.Alarms = append(out.Alarms, at.In(zone).Format(time.RFC3339))
		}
	}
	if t.Rule != nil {
		if anchor := t.Anchor(); anchor != nil {
			loc := local
			if anchor.Kind == tz.KindZoned || anchor.Kind == tz.KindUTC {
				loc = anchor.Time.Location()
			}
			out.Rule = &calweb.RuleView{Simple: t.Rule.Simple(loc), Text: calweb.RuleText(t.Rule, loc)}
		}
	}
	return out
}

func zoneName(v *tz.Value) string {
	if v == nil {
		return ""
	}
	return store.ZoneName(*v)
}

// Edit applies an input to a stored reminder (its master VTODO). Only the
// fields given change; everything else stays as it was, byte for byte in
// the properties. An input that changes nothing returns the bytes as
// they are (changed false).
func Edit(data []byte, in Input, now time.Time, local *time.Location) ([]byte, bool, error) {
	p, err := open(data, local)
	if err != nil {
		return nil, false, err
	}
	t := p.master
	c := t.Component
	changed := false
	if in.Title != nil {
		title, err := checkText("title", strings.TrimSpace(*in.Title), MaxTitle, false)
		if err != nil {
			return nil, false, err
		}
		if title == "" {
			return nil, false, invalid("a reminder needs a title")
		}
		changed = setText(c, ical.PropSummary, title) || changed
	}
	if in.Notes != nil {
		notes, err := checkText("notes", *in.Notes, MaxNotes, true)
		if err != nil {
			return nil, false, err
		}
		changed = setText(c, ical.PropDescription, notes) || changed
	}
	oldDue, oldStart := t.Due, t.Start
	due, start := oldDue, oldStart
	if in.Due.Set {
		due = nil
		if in.Due.Value != nil {
			v, err := parseWhen("due", *in.Due.Value, in.Timezone, oldDue, local)
			if err != nil {
				return nil, false, err
			}
			due = &v
		}
		// DTSTART that equals DUE (as iOS writes it) moves with it.
		if !in.Start.Set && oldStart != nil && oldDue != nil && tz.Equal(*oldStart, *oldDue) {
			start = due
		}
	}
	if in.Start.Set {
		start = nil
		if in.Start.Value != nil {
			hint := oldStart
			if hint == nil {
				hint = due
			}
			v, err := parseWhen("start", *in.Start.Value, in.Timezone, hint, local)
			if err != nil {
				return nil, false, err
			}
			start = &v
		}
	}
	dueMoved := setValue(c, ical.PropDue, due)
	startMoved := setValue(c, ical.PropDateTimeStart, start)
	changed = dueMoved || startMoved || changed
	if in.Priority != nil {
		prio, err := checkPriority(in.Priority)
		if err != nil {
			return nil, false, err
		}
		changed = setInt(c, ical.PropPriority, prio) || changed
	}
	if in.Parent.Set {
		parent := ""
		if in.Parent.Value != nil {
			if parent, err = checkParent(*in.Parent.Value); err != nil {
				return nil, false, err
			}
		}
		changed = setParent(c, parent) || changed
	}
	if in.Repeat.Set {
		switch {
		case in.Repeat.Value == nil:
			for _, name := range []string{ical.PropRecurrenceRule, ical.PropRecurrenceDates, ical.PropExceptionDates} {
				if c.Props.Get(name) != nil {
					c.Props.Del(name)
					changed = true
				}
			}
		case in.Repeat.Value.Custom:
			// The rule stays as it is.
		default:
			anchor := start
			if anchor == nil {
				anchor = due
			}
			before := ""
			if r := c.Props.Get(ical.PropRecurrenceRule); r != nil {
				before = r.Value
			}
			if err := setRepeat(c, *in.Repeat.Value, anchor); err != nil {
				return nil, false, err
			}
			changed = c.Props.Get(ical.PropRecurrenceRule).Value != before || changed
		}
	}
	now = now.UTC().Truncate(time.Second)
	moved := &recur.Todo{Component: c, Start: start, Due: due}
	switch {
	case in.Alarms.Set:
		want, err := in.alarms(moved, local)
		if err != nil {
			return nil, false, err
		}
		changed = applyAlarms(c, moved, want, local) || changed
	case dueMoved && oldDue != nil && due != nil:
		// An absolute alarm at the old due time (as iOS sets one) moves
		// with it; relative ones move by themselves.
		old, next := oldDue.InZone(local), due.InZone(local)
		for _, a := range c.Children {
			if a.Name != ical.CompAlarm {
				continue
			}
			if p := a.Props.Get(ical.PropTrigger); p != nil && strings.EqualFold(p.Params.Get(ical.ParamValue), "DATE-TIME") {
				if at, err := time.Parse(stampLayout, strings.TrimSpace(p.Value)); err == nil && at.Equal(old) {
					p.Value = next.UTC().Format(stampLayout)
				}
			}
		}
	}
	if !changed {
		return data, false, nil
	}
	if c.Props.Get(ical.PropRecurrenceRule) != nil && start == nil && due == nil {
		return nil, false, invalid("a repetition needs a date (due or start)")
	}
	touch(c, now)
	out, err := encode(p.cal)
	if err != nil {
		return nil, false, err
	}
	return out, true, nil
}

// Completion is the outcome of SetDone on the bytes.
type Completion struct {
	// Data is the reminder as it is to be stored (nil: no change).
	Data []byte
	// Copy is, for a series ticked off, the done copy of the occurrence:
	// its file and UID.
	Copy    []byte
	CopyUID string
}

// SetDone ticks a reminder off (done) or opens it again. A series that
// TitanOS expands and that has a next occurrence is ticked off like iOS
// does it: the occurrence becomes a reminder of its own (a copy without
// rule and alarms, done), and the series moves on – DTSTART to the next
// occurrence (DUE without DTSTART), DUE by the same distance, absolute
// alarms by it too, COUNT one less. A series whose rule ends there, and a
// rule TitanOS does not expand, are done as a whole.
func SetDone(data []byte, done bool, now time.Time, local *time.Location) (Completion, error) {
	p, err := open(data, local)
	if err != nil {
		return Completion{}, err
	}
	t := p.master
	c := t.Component
	now = now.UTC().Truncate(time.Second)
	ti := store.TodoInfo(t, false)
	if !done {
		if !ti.Done() && !ti.Cancelled() {
			return Completion{}, nil
		}
		c.Props.Del(ical.PropCompleted)
		c.Props.Del(ical.PropPercentComplete)
		c.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
		touch(c, now)
		out, err := encode(p.cal)
		return Completion{Data: out}, err
	}
	if ti.Done() {
		return Completion{}, nil
	}
	if next, ok := t.Next(); ok {
		return advance(p, next, now, local)
	}
	markDone(c, now)
	touch(c, now)
	out, err := encode(p.cal)
	return Completion{Data: out}, err
}

func markDone(c *ical.Component, now time.Time) {
	setStamp(c, ical.PropCompleted, now)
	c.Props.SetText(ical.PropStatus, "COMPLETED")
	pc := ical.NewProp(ical.PropPercentComplete)
	pc.Value = "100"
	c.Props.Set(pc)
}

// advance is SetDone of a series with a next occurrence.
func advance(p *parsed, next tz.Value, now time.Time, local *time.Location) (Completion, error) {
	t := p.master
	c := t.Component
	anchor := *t.Anchor()

	// The copy: this occurrence, done.
	cp := cloneComponent(c)
	for _, name := range []string{ical.PropRecurrenceRule, ical.PropRecurrenceDates, ical.PropExceptionDates, ical.PropSequence} {
		cp.Props.Del(name)
	}
	var children []*ical.Component
	for _, ch := range cp.Children {
		if ch.Name != ical.CompAlarm {
			children = append(children, ch)
		}
	}
	cp.Children = children
	uid := randomID()
	cp.Props.SetText(ical.PropUID, uid)
	setStamp(cp, ical.PropCreated, now)
	markDone(cp, now)
	touch(cp, now)
	copyCal := ical.NewCalendar()
	for name, props := range p.cal.Props {
		copyCal.Props[name] = slices.Clone(props)
	}
	for _, ch := range p.cal.Children {
		if ch.Name == ical.CompTimezone {
			copyCal.Children = append(copyCal.Children, ch)
		}
	}
	copyCal.Children = append(copyCal.Children, cp)
	copyData, err := encode(copyCal)
	if err != nil {
		return Completion{}, err
	}

	// The series moves on.
	shift := func(v tz.Value) tz.Value {
		if v.IsDate() && anchor.IsDate() && next.IsDate() {
			days := int(next.Time.Sub(anchor.Time).Hours() / 24)
			return tz.Value{Time: v.Time.AddDate(0, 0, days), Kind: tz.KindDate}
		}
		at := next.InZone(local).Add(v.InZone(local).Sub(anchor.InZone(local)))
		return valueLike(at, v, local)
	}
	if t.Start != nil {
		setValue(c, ical.PropDateTimeStart, &next)
		if t.Due != nil {
			due := shift(*t.Due)
			setValue(c, ical.PropDue, &due)
		}
	} else {
		setValue(c, ical.PropDue, &next)
	}
	delta := next.InZone(local).Sub(anchor.InZone(local))
	for _, a := range c.Children {
		if a.Name != ical.CompAlarm {
			continue
		}
		if pr := a.Props.Get(ical.PropTrigger); pr != nil && strings.EqualFold(pr.Params.Get(ical.ParamValue), "DATE-TIME") {
			if at, err := time.Parse(stampLayout, strings.TrimSpace(pr.Value)); err == nil {
				pr.Value = at.Add(delta).UTC().Format(stampLayout)
			}
		}
		// iOS marks a fired alarm acknowledged; the next one is new.
		a.Props.Del("ACKNOWLEDGED")
	}
	if rp := c.Props.Get(ical.PropRecurrenceRule); rp != nil && t.Rule != nil && t.Rule.Count > 0 {
		r := t.Rule.Clone()
		r.Count--
		rp.Value = r.String()
	}
	c.Props.Del(ical.PropCompleted)
	c.Props.Del(ical.PropPercentComplete)
	if c.Props.Get(ical.PropStatus) != nil {
		c.Props.SetText(ical.PropStatus, "NEEDS-ACTION")
	}
	touch(c, now)
	out, err := encode(p.cal)
	if err != nil {
		return Completion{}, err
	}
	return Completion{Data: out, Copy: copyData, CopyUID: uid}, nil
}

// valueLike writes an instant in the form of like: a date (of the NAS
// zone), UTC, floating (wall clock of the NAS zone) or its zone.
func valueLike(at time.Time, like tz.Value, local *time.Location) tz.Value {
	switch like.Kind {
	case tz.KindDate:
		l := at.In(local)
		return tz.Date(l.Year(), l.Month(), l.Day())
	case tz.KindUTC:
		return tz.Value{Time: at.UTC(), Kind: tz.KindUTC}
	case tz.KindFloating:
		return tz.Value{Time: at.In(local), Kind: tz.KindFloating}
	}
	return tz.Value{Time: at.In(like.Time.Location()), Kind: tz.KindZoned, TZID: like.TZID}
}

func cloneComponent(c *ical.Component) *ical.Component {
	out := &ical.Component{Name: c.Name, Props: ical.Props{}}
	for name, props := range c.Props {
		list := make([]ical.Prop, len(props))
		for i, p := range props {
			list[i] = ical.Prop{Name: p.Name, Value: p.Value, Params: ical.Params{}}
			for k, v := range p.Params {
				list[i].Params[k] = slices.Clone(v)
			}
		}
		out.Props[name] = list
	}
	for _, ch := range c.Children {
		out.Children = append(out.Children, cloneComponent(ch))
	}
	return out
}
