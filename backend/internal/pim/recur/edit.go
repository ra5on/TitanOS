package recur

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// Scope is what a change of a series applies to (TECHNIK 2.4).
type Scope int

const (
	// ScopeThis changes or removes one occurrence.
	ScopeThis Scope = iota + 1
	// ScopeFollowing changes or removes this occurrence and all after it.
	ScopeFollowing
	// ScopeAll changes or removes the whole series.
	ScopeAll
)

// EditOptions carry what an edit must not take from globals.
type EditOptions struct {
	// Local is the zone of the NAS (floating times, dates).
	Local *time.Location
	// Now stamps DTSTAMP, LAST-MODIFIED and CREATED.
	Now time.Time
	// NewUID is the UID of the new series made by ScopeFollowing.
	NewUID string
	// ProdID is written when the file has none.
	ProdID string
}

// EditResult is the outcome of Update or Delete.
type EditResult struct {
	// Data is the rewritten file; nil when Delete is set.
	Data []byte
	// Delete means the file has no event left and goes as a whole.
	Delete bool
	// New is the file of the new series made by ScopeFollowing on update.
	New    []byte
	NewUID string
	// Scope is the scope applied: "this and following" at the first
	// occurrence is "all", and a single event knows only "all".
	Scope Scope
	// DroppedExceptions counts overrides, EXDATE and RDATE values that a
	// change of time or duration made meaningless and that were left out.
	DroppedExceptions int
}

// Change edits one VEVENT in place: summary, location, DTSTART, DTEND,
// RRULE … It works on a copy; returning an error leaves the file as is.
type Change func(ev *ical.Component) error

var (
	// ErrNoSeries means the UID is not in the file.
	ErrNoSeries = errors.New("event not in this file")
	// ErrNoOccurrence means the occurrence is not part of the series.
	ErrNoOccurrence = errors.New("occurrence not in the series")
)

// Update applies change to the occurrence (an id from Occurrence.ID; empty
// for a single event or ScopeAll) of the series uid ("" = the only one) and
// returns the rewritten file.
func Update(b []byte, uid, occurrence string, scope Scope, change Change, opt EditOptions) (EditResult, error) {
	return edit(b, uid, occurrence, scope, change, opt)
}

// Delete removes the occurrence, the occurrence and those after it, or the
// whole series.
func Delete(b []byte, uid, occurrence string, scope Scope, opt EditOptions) (EditResult, error) {
	return edit(b, uid, occurrence, scope, nil, opt)
}

type editor struct {
	f      *File
	s      *Series
	opt    EditOptions
	result EditResult
}

func edit(b []byte, uid, occurrence string, scope Scope, change Change, opt EditOptions) (res EditResult, err error) {
	// Walking the series calls rrule-go; a panic there fails this edit,
	// never titand.
	defer func() {
		if r := recover(); r != nil {
			res, err = EditResult{}, fmt.Errorf("%w: the series could not be expanded", ErrInvalid)
		}
	}()
	if opt.Local == nil {
		opt.Local = time.UTC
	}
	if opt.Now.IsZero() {
		return EditResult{}, errors.New("edit without a time")
	}
	f, err := Read(b, Options{Local: opt.Local})
	if err != nil {
		return EditResult{}, err
	}
	e := &editor{f: f, opt: opt}
	for _, s := range f.Series {
		if uid == "" && len(f.Series) == 1 || s.UID == uid {
			e.s = s
		}
	}
	if e.s == nil {
		return EditResult{}, ErrNoSeries
	}
	m := e.s.Master
	recurring := m != nil && (m.Recurring() || len(e.s.Overrides) > 0)
	if !recurring {
		scope = ScopeAll
	}
	if scope != ScopeAll && occurrence == "" {
		return EditResult{}, fmt.Errorf("%w: no occurrence named", ErrNoOccurrence)
	}
	if m == nil && scope != ScopeThis {
		// Only overrides in the file: each stands alone.
		scope = ScopeThis
	}
	var rid tz.Value
	var override *Event
	if scope != ScopeAll {
		id, err := ParseOccurrenceID(occurrence)
		if err != nil {
			return EditResult{}, err
		}
		rid, override, err = e.find(id)
		if err != nil {
			return EditResult{}, err
		}
		if scope == ScopeFollowing && e.s.toInstance(rid).Equal(e.f.expansionStart(m.Start)) {
			scope = ScopeAll
		}
	}
	e.result.Scope = scope
	switch scope {
	case ScopeThis:
		err = e.this(rid, override, change)
	case ScopeFollowing:
		err = e.following(rid, change)
	default:
		err = e.all(change)
	}
	if err != nil {
		return EditResult{}, err
	}
	if e.result.Delete {
		e.result.Data = nil
		return e.result, nil
	}
	data, err := e.encode(f.Calendar)
	if err != nil {
		return EditResult{}, err
	}
	e.result.Data = data
	return e.result, nil
}

// find resolves an occurrence id to the RECURRENCE-ID in the form of the
// master's DTSTART, and the override standing for it, if any.
func (e *editor) find(id tz.Value) (tz.Value, *Event, error) {
	local := e.opt.Local
	for _, o := range e.s.Overrides {
		r := *o.RecurrenceID
		if r.IsDate() == id.IsDate() && r.InZone(local).Equal(id.InZone(local)) {
			return r, o, nil
		}
	}
	m := e.s.Master
	if m == nil {
		return tz.Value{}, nil, ErrNoOccurrence
	}
	if id.IsDate() != m.Start.IsDate() {
		return tz.Value{}, nil, ErrNoOccurrence
	}
	rid := e.s.ridOf(instance{start: e.s.toInstance(id)})
	if !e.s.isInstance(rid) {
		return tz.Value{}, nil, ErrNoOccurrence
	}
	return rid, nil, nil
}

func (e *editor) this(rid tz.Value, override *Event, change Change) error {
	cal := e.f.Calendar
	m := e.s.Master
	if change == nil {
		if override != nil {
			removeComponent(cal, override.Component)
		}
		if m != nil && e.s.isInstance(rid) {
			m.Component.Props.Add(rid.Prop(ical.PropExceptionDates))
			e.touch(m.Component, true)
		}
		e.result.Delete = !hasEvents(cal)
		return nil
	}
	if override != nil {
		c := cloneComponent(override.Component)
		if err := change(c); err != nil {
			return err
		}
		e.touch(c, true)
		replaceComponent(cal, override.Component, c)
		return nil
	}
	c := cloneComponent(m.Component)
	for _, name := range []string{ical.PropRecurrenceRule, ical.PropRecurrenceDates, ical.PropExceptionDates, "EXRULE"} {
		c.Props.Del(name)
	}
	c.Props.Set(rid.Prop(ical.PropRecurrenceID))
	if err := e.moveStart(c, m, rid); err != nil {
		return err
	}
	if err := change(c); err != nil {
		return err
	}
	c.Props.Del(ical.PropSequence)
	setSequence(c, sequence(m.Component)+1)
	e.touch(c, false)
	insertAfter(cal, m.Component, c)
	return nil
}

// moveStart sets DTSTART (and DTEND, keeping the duration) of c, a copy
// of the master, to the instance at.
func (e *editor) moveStart(c *ical.Component, m *Event, at tz.Value) error {
	c.Props.Set(at.Prop(ical.PropDateTimeStart))
	if p := m.Component.Props.Get(ical.PropDateTimeEnd); p != nil {
		like, err := e.f.tz.Parse(p)
		if err != nil {
			return err
		}
		_, end := span(at, m.Duration, e.opt.Local)
		v := valueLike(end, like, e.opt.Local)
		np := cloneProp(p)
		v.SetProp(np)
		c.Props.Set(np)
	}
	return nil
}

// valueLike expresses the instant t in the form of like.
func valueLike(t time.Time, like tz.Value, local *time.Location) tz.Value {
	v := tz.Value{Kind: like.Kind, TZID: like.TZID}
	switch like.Kind {
	case tz.KindDate:
		l := t.In(local)
		v.Time = time.Date(l.Year(), l.Month(), l.Day(), 0, 0, 0, 0, time.UTC)
	case tz.KindUTC:
		v.Time = t.UTC()
	case tz.KindZoned:
		v.Time = t.In(like.Time.Location())
	default:
		v.Time = t.In(local)
	}
	return v
}

func (e *editor) following(rid tz.Value, change Change) error {
	cal := e.f.Calendar
	m := e.s.Master
	cut := e.s.toInstance(rid)
	original := cloneComponent(m.Component)

	// The old series ends before the cut.
	var carried []*Event
	for _, o := range e.s.Overrides {
		if !e.s.toInstance(*o.RecurrenceID).Before(cut) {
			carried = append(carried, o)
			removeComponent(cal, o.Component)
		}
	}
	before := func(v tz.Value) bool { return e.s.toInstance(v).Before(cut) }
	e.filterDates(m.Component, ical.PropExceptionDates, before)
	e.filterDates(m.Component, ical.PropRecurrenceDates, before)
	if m.Rule != nil {
		r := m.Rule.Clone()
		r.Count = 0
		u := untilBefore(cut, m.Start, e.opt.Local)
		r.Until = &u
		setRule(m.Component, r)
	}
	e.touch(m.Component, true)
	if change == nil {
		return nil
	}

	// The new series starts at the cut, with a new UID.
	if e.opt.NewUID == "" || e.opt.NewUID == m.UID {
		return errors.New("this and following needs a new UID")
	}
	n := original
	n.Props.SetText(ical.PropUID, e.opt.NewUID)
	if err := e.moveStart(n, m, rid); err != nil {
		return err
	}
	if m.Rule != nil && m.Rule.Count > 0 {
		left := m.Rule.Count - e.ruleInstancesBefore(cut)
		if left <= 0 {
			n.Props.Del(ical.PropRecurrenceRule)
		} else {
			r := m.Rule.Clone()
			r.Count = left
			setRule(n, r)
		}
	}
	notBefore := func(v tz.Value) bool { return !before(v) }
	e.filterDates(n, ical.PropExceptionDates, notBefore)
	e.filterDates(n, ical.PropRecurrenceDates, notBefore)
	startBefore, durBefore := timing(n)
	if err := change(n); err != nil {
		return err
	}
	e.fixUntil(n)
	n.Props.Del(ical.PropSequence)
	n.Props.Del(ical.PropCreated)
	created := ical.NewProp(ical.PropCreated)
	created.Value = e.opt.Now.UTC().Format("20060102T150405Z")
	n.Props.Set(created)
	e.touch(n, false)

	events := []*ical.Component{n}
	startAfter, durAfter := timing(n)
	if startAfter == startBefore && durAfter == durBefore {
		for _, o := range carried {
			c := cloneComponent(o.Component)
			c.Props.SetText(ical.PropUID, e.opt.NewUID)
			events = append(events, c)
		}
	} else {
		ex := countDates(e.f, n, ical.PropExceptionDates) + countDates(e.f, n, ical.PropRecurrenceDates)
		n.Props.Del(ical.PropExceptionDates)
		n.Props.Del(ical.PropRecurrenceDates)
		e.result.DroppedExceptions = len(carried) + ex
	}
	data, err := e.encode(newCalendar(cal, events))
	if err != nil {
		return err
	}
	e.result.New, e.result.NewUID = data, e.opt.NewUID
	return nil
}

// ruleInstancesBefore counts the instances of the RRULE alone before cut,
// those removed by EXDATE included: COUNT counts them (RFC 5545 3.3.10).
func (e *editor) ruleInstancesBefore(cut time.Time) int {
	m := e.s.Master
	next, err := newIterator(m.Rule.option(e.f.expansionStart(m.Start), m.Start, e.opt.Local))
	if err != nil {
		return 0
	}
	n := 0
	for n < MaxOccurrences {
		t, ok := next()
		if !ok || !t.Before(cut) {
			break
		}
		n++
	}
	return n
}

// untilBefore is UNTIL one second before the instance cut, in the form RFC
// 5545 3.3.10 asks: a date (the day before) for a date series, UTC with Z
// for a zoned or UTC series, a floating time for a floating one.
func untilBefore(cut time.Time, start tz.Value, local *time.Location) tz.Value {
	switch start.Kind {
	case tz.KindDate:
		return tz.Value{Time: cut.AddDate(0, 0, -1), Kind: tz.KindDate}
	case tz.KindFloating:
		w := cut.In(local).Add(-time.Second)
		return tz.Value{Time: time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, time.UTC), Kind: tz.KindFloating}
	}
	return tz.Value{Time: cut.Add(-time.Second).UTC(), Kind: tz.KindUTC}
}

func (e *editor) all(change Change) error {
	cal := e.f.Calendar
	m := e.s.Master
	if change == nil {
		for _, ev := range append([]*Event{m}, e.s.Overrides...) {
			if ev != nil {
				removeComponent(cal, ev.Component)
			}
		}
		e.result.Delete = !hasEvents(cal)
		return nil
	}
	c := cloneComponent(m.Component)
	if err := change(c); err != nil {
		return err
	}
	sp := c.Props.Get(ical.PropDateTimeStart)
	if sp == nil {
		return fmt.Errorf("%w: change removed DTSTART", ErrInvalid)
	}
	newStart, err := e.f.tz.Parse(sp)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	old := m.Start
	overrides := e.s.Overrides
	switch {
	case newStart.IsDate() != old.IsDate():
		// All-day became timed or back: old ids name nothing any more.
		dropped := countDates(e.f, c, ical.PropExceptionDates) + countDates(e.f, c, ical.PropRecurrenceDates) + len(overrides)
		c.Props.Del(ical.PropExceptionDates)
		c.Props.Del(ical.PropRecurrenceDates)
		for _, o := range overrides {
			removeComponent(cal, o.Component)
		}
		e.result.DroppedExceptions = dropped
	default:
		shift := e.shifter(old, newStart)
		if shift != nil {
			e.mapDates(c, ical.PropExceptionDates, shift)
			e.mapDates(c, ical.PropRecurrenceDates, shift)
			if r := ruleOf(c); r != nil && r.Until != nil && !r.Until.IsDate() {
				u := shift(*r.Until)
				r.Until = &u
				setRule(c, r)
			}
			for _, o := range overrides {
				oc := cloneComponent(o.Component)
				p := oc.Props.Get(ical.PropRecurrenceID)
				nv := shift(*o.RecurrenceID)
				np := cloneProp(p)
				nv.SetProp(np)
				oc.Props.Set(np)
				replaceComponent(cal, o.Component, oc)
			}
		}
	}
	e.fixUntil(c)
	e.touch(c, true)
	replaceComponent(cal, m.Component, c)
	return nil
}

// shifter returns the move from old to new DTSTART as a function on
// dates and date-times, or nil when DTSTART did not move. Date-times move
// by the wall-clock difference in the zone of DTSTART, so an EXDATE after
// a DST change still meets its occurrence; dates move by days.
func (e *editor) shifter(old, moved tz.Value) func(tz.Value) tz.Value {
	local := e.opt.Local
	if old.IsDate() {
		days := int(moved.Time.Sub(old.Time).Hours() / 24)
		if days == 0 {
			return nil
		}
		return func(v tz.Value) tz.Value {
			v.Time = v.Time.AddDate(0, 0, days)
			return v
		}
	}
	oldLoc := old.InZone(local).Location()
	wall := func(t time.Time) time.Time {
		return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC)
	}
	sameZone := old.Kind == moved.Kind && old.TZID == moved.TZID
	delta := wall(moved.InZone(local).In(oldLoc)).Sub(wall(old.InZone(local).In(oldLoc)))
	if !sameZone {
		delta = moved.InZone(local).Sub(old.InZone(local))
	}
	if delta == 0 {
		return nil
	}
	return func(v tz.Value) tz.Value {
		if v.IsDate() {
			return v
		}
		var at time.Time
		if sameZone {
			w := wall(v.InZone(local).In(oldLoc)).Add(delta)
			at = time.Date(w.Year(), w.Month(), w.Day(), w.Hour(), w.Minute(), w.Second(), 0, oldLoc)
		} else {
			at = v.InZone(local).Add(delta)
		}
		return valueLike(at, v, local)
	}
}

// fixUntil puts UNTIL into the form the (possibly changed) DTSTART needs.
func (e *editor) fixUntil(c *ical.Component) {
	r := ruleOf(c)
	sp := c.Props.Get(ical.PropDateTimeStart)
	if r == nil || r.Until == nil || sp == nil {
		return
	}
	start, err := e.f.tz.Parse(sp)
	if err != nil {
		return
	}
	u := *r.Until
	local := e.opt.Local
	var fixed tz.Value
	switch start.Kind {
	case tz.KindDate:
		if u.IsDate() {
			return
		}
		fixed = tz.Value{Time: untilDate(u, local), Kind: tz.KindDate}
	case tz.KindFloating:
		if u.Kind == tz.KindFloating {
			return
		}
		t := untilIn(u, start.InZone(local), false, local)
		fixed = tz.Value{Time: time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC), Kind: tz.KindFloating}
	default:
		if u.Kind == tz.KindUTC {
			return
		}
		fixed = tz.Value{Time: untilIn(u, start.Time, false, local).UTC(), Kind: tz.KindUTC}
	}
	r.Until = &fixed
	setRule(c, r)
}

// filterDates keeps the EXDATE/RDATE values for which keep is true and
// returns how many it removed.
func (e *editor) filterDates(c *ical.Component, name string, keep func(tz.Value) bool) int {
	removed := 0
	e.rewriteDates(c, name, func(v tz.Value) (tz.Value, bool) {
		if keep(v) {
			return v, true
		}
		removed++
		return v, false
	})
	return removed
}

func (e *editor) mapDates(c *ical.Component, name string, fn func(tz.Value) tz.Value) {
	e.rewriteDates(c, name, func(v tz.Value) (tz.Value, bool) {
		end := v.End
		n := fn(v)
		if !end.IsZero() {
			n.End = end.Add(n.InZone(e.opt.Local).Sub(v.InZone(e.opt.Local)))
		}
		return n, true
	})
}

// rewriteDates rewrites each value of the list properties name, keeping
// their parameters; a property left empty goes.
func (e *editor) rewriteDates(c *ical.Component, name string, fn func(tz.Value) (tz.Value, bool)) {
	props := c.Props.Values(name)
	if len(props) == 0 {
		return
	}
	var out []ical.Prop
	for i := range props {
		p := props[i]
		vs, err := e.f.tz.ParseList(&p)
		if err != nil {
			out = append(out, p)
			continue
		}
		var texts []string
		for _, v := range vs {
			n, keep := fn(v)
			if !keep {
				continue
			}
			t := n.Format()
			if !n.End.IsZero() {
				t += "/" + tz.Duration{Exact: n.End.Sub(n.InZone(e.opt.Local))}.String()
			}
			texts = append(texts, t)
		}
		if len(texts) == 0 {
			continue
		}
		np := cloneProp(&p)
		np.Value = strings.Join(texts, ",")
		out = append(out, *np)
	}
	if len(out) == 0 {
		c.Props.Del(name)
		return
	}
	c.Props[name] = out
}

func countDates(f *File, c *ical.Component, name string) int {
	n := 0
	for i := range c.Props.Values(name) {
		vs, _ := f.tz.ParseList(&c.Props.Values(name)[i])
		n += len(vs)
	}
	return n
}

// timing is the text of DTSTART and of the duration (DTEND or DURATION) of
// c, to tell whether a change moved the time.
func timing(c *ical.Component) (start, dur string) {
	if p := c.Props.Get(ical.PropDateTimeStart); p != nil {
		start = p.Params.Get(ical.ParamTimezoneID) + "|" + p.Params.Get(ical.ParamValue) + "|" + p.Value
	}
	if p := c.Props.Get(ical.PropDateTimeEnd); p != nil {
		dur = "end|" + p.Params.Get(ical.ParamTimezoneID) + "|" + p.Value
	}
	if p := c.Props.Get(ical.PropDuration); p != nil {
		dur = "dur|" + p.Value
	}
	return start, dur
}

func ruleOf(c *ical.Component) *Rule {
	p := c.Props.Get(ical.PropRecurrenceRule)
	if p == nil {
		return nil
	}
	r, err := Parse(p.Value)
	if err != nil {
		return nil
	}
	return r
}

func setRule(c *ical.Component, r *Rule) {
	p := ical.NewProp(ical.PropRecurrenceRule)
	if old := c.Props.Get(ical.PropRecurrenceRule); old != nil {
		p = cloneProp(old)
	}
	p.Value = r.String()
	c.Props.Set(p)
}

func sequence(c *ical.Component) int {
	if p := c.Props.Get(ical.PropSequence); p != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(p.Value)); err == nil && n >= 0 {
			return n
		}
	}
	return 0
}

func setSequence(c *ical.Component, n int) {
	p := ical.NewProp(ical.PropSequence)
	p.Value = strconv.Itoa(n)
	c.Props.Set(p)
}

// touch stamps a changed component: DTSTAMP and LAST-MODIFIED now, and
// SEQUENCE + 1 when bump is set.
func (e *editor) touch(c *ical.Component, bump bool) {
	now := e.opt.Now.UTC().Format("20060102T150405Z")
	for _, name := range []string{ical.PropDateTimeStamp, ical.PropLastModified} {
		p := ical.NewProp(name)
		p.Value = now
		c.Props.Set(p)
	}
	if bump {
		setSequence(c, sequence(c)+1)
	}
}

func (e *editor) encode(cal *ical.Calendar) ([]byte, error) {
	now := e.opt.Now.UTC().Format("20060102T150405Z")
	if cal.Props.Get(ical.PropProductID) == nil {
		prod := e.opt.ProdID
		if prod == "" {
			prod = "-//TitanOS//Calendar//EN"
		}
		cal.Props.SetText(ical.PropProductID, prod)
	}
	if cal.Props.Get(ical.PropVersion) == nil {
		cal.Props.SetText(ical.PropVersion, "2.0")
	}
	for _, c := range cal.Children {
		if c.Name == ical.CompEvent && c.Props.Get(ical.PropDateTimeStamp) == nil {
			p := ical.NewProp(ical.PropDateTimeStamp)
			p.Value = now
			c.Props.Set(p)
		}
	}
	var buf bytes.Buffer
	if err := ical.NewEncoder(&buf).Encode(cal); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return buf.Bytes(), nil
}

// newCalendar builds the file of a new series: the calendar properties of
// old, the VTIMEZONEs its events use, and events.
func newCalendar(old *ical.Calendar, events []*ical.Component) *ical.Calendar {
	cal := ical.NewCalendar()
	for _, name := range []string{ical.PropProductID, ical.PropVersion, ical.PropCalendarScale} {
		if p := old.Props.Get(name); p != nil {
			cal.Props.Set(cloneProp(p))
		}
	}
	used := map[string]bool{}
	for _, ev := range events {
		for _, props := range ev.Props {
			for _, p := range props {
				if id := p.Params.Get(ical.ParamTimezoneID); id != "" {
					used[id] = true
				}
			}
		}
	}
	for _, c := range old.Children {
		if c.Name != ical.CompTimezone {
			continue
		}
		if p := c.Props.Get(ical.PropTimezoneID); p != nil && used[p.Value] {
			cal.Children = append(cal.Children, cloneComponent(c))
		}
	}
	cal.Children = append(cal.Children, events...)
	return cal
}

func hasEvents(cal *ical.Calendar) bool {
	return slices.ContainsFunc(cal.Children, func(c *ical.Component) bool { return c.Name == ical.CompEvent })
}

func removeComponent(cal *ical.Calendar, c *ical.Component) {
	cal.Children = slices.DeleteFunc(cal.Children, func(x *ical.Component) bool { return x == c })
}

func replaceComponent(cal *ical.Calendar, old, c *ical.Component) {
	for i, x := range cal.Children {
		if x == old {
			cal.Children[i] = c
			return
		}
	}
}

func insertAfter(cal *ical.Calendar, after, c *ical.Component) {
	for i, x := range cal.Children {
		if x == after {
			cal.Children = slices.Insert(cal.Children, i+1, c)
			return
		}
	}
	cal.Children = append(cal.Children, c)
}

func cloneProp(p *ical.Prop) *ical.Prop {
	n := &ical.Prop{Name: p.Name, Value: p.Value, Params: make(ical.Params, len(p.Params))}
	for k, v := range p.Params {
		n.Params[k] = slices.Clone(v)
	}
	return n
}

func cloneComponent(c *ical.Component) *ical.Component {
	n := &ical.Component{Name: c.Name, Props: make(ical.Props, len(c.Props))}
	for name, props := range c.Props {
		out := make([]ical.Prop, len(props))
		for i := range props {
			out[i] = *cloneProp(&props[i])
		}
		n.Props[name] = out
	}
	for _, child := range c.Children {
		n.Children = append(n.Children, cloneComponent(child))
	}
	return n
}
