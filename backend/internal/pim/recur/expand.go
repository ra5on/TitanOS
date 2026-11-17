package recur

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/teambition/rrule-go"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

const (
	// MaxWindowDays bounds the window of one expansion.
	MaxWindowDays = 400
	// MaxOccurrences bounds the occurrences of one entry in one expansion.
	MaxOccurrences = 5000
)

// maxSteps bounds the instances walked per expansion, those before the
// window included (a daily rule from 1900 to 2200 needs about 110 000). A
// variable only so that tests can reach it.
var maxSteps = 200000

// occurrenceCap is MaxOccurrences, a variable only for tests: no rule of
// the whitelist reaches 5000 occurrences within 400 days.
var occurrenceCap = MaxOccurrences

// ErrWindow marks a window that is empty or longer than MaxWindowDays.
var ErrWindow = errors.New("expansion window empty or longer than 400 days")

// Occurrence is one occurrence of a series in a window.
type Occurrence struct {
	// Start and End (exclusive) on the timeline; an all-day occurrence runs
	// from 00:00 to 00:00 in the NAS zone.
	Start, End time.Time
	AllDay     bool
	// RecurrenceID is the original start, in the form of the master's
	// DTSTART (for a single event: its DTSTART).
	RecurrenceID tz.Value
	// Event supplies title, location and the rest: the master, or the
	// override that replaces this occurrence.
	Event    *Event
	Override bool
	// Orphan marks an override whose RECURRENCE-ID is no occurrence of the
	// master (still shown, RFC 5545 3.8.4.4).
	Orphan bool
}

// ID names the occurrence the way the agent and the API do: its
// RECURRENCE-ID in UTC ("20261110T080000Z"), or the date ("20261110").
func (o Occurrence) ID() string { return OccurrenceID(o.RecurrenceID) }

// OccurrenceID formats a RECURRENCE-ID value as an occurrence id.
func OccurrenceID(v tz.Value) string {
	if v.IsDate() {
		return v.Time.Format("20060102")
	}
	return v.Time.UTC().Format("20060102T150405Z")
}

// ParseOccurrenceID reads an id made by OccurrenceID.
func ParseOccurrenceID(s string) (tz.Value, error) {
	if len(s) == 8 && digits(s) {
		t, err := time.ParseInLocation("20060102", s, time.UTC)
		if err == nil {
			return tz.Value{Time: t, Kind: tz.KindDate}, nil
		}
	}
	t, err := time.ParseInLocation("20060102T150405Z", s, time.UTC)
	if err != nil || len(s) != 16 {
		return tz.Value{}, fmt.Errorf("invalid occurrence id %q", s)
	}
	return tz.Value{Time: t, Kind: tz.KindUTC}, nil
}

// Expansion is the result of Expand.
type Expansion struct {
	Occurrences []Occurrence
	// Truncated is set when a cap ended the expansion early.
	Truncated bool
	// Unsupported is recurUnsupported: the master shows once.
	Unsupported bool
	// Failed is set when rrule-go panicked; the entry then shows once and
	// the panic is logged with account and file name.
	Failed bool
}

// Expand returns the occurrences that overlap [from, to), sorted by start.
// The window may be at most MaxWindowDays long.
func (s *Series) Expand(from, to time.Time) (Expansion, error) {
	if !to.After(from) || to.After(from.AddDate(0, 0, MaxWindowDays)) {
		return Expansion{}, ErrWindow
	}
	var x Expansion
	collect := func(o Occurrence) bool {
		if len(x.Occurrences) >= occurrenceCap {
			x.Truncated = true
			return false
		}
		x.Occurrences = append(x.Occurrences, o)
		return true
	}
	truncated, failed := s.safeWalk(from, to, collect)
	if failed {
		x = Expansion{Failed: true}
		truncated, _ = s.safeWalk(from, to, collect)
		x.Failed = true
	}
	x.Truncated = x.Truncated || truncated
	x.Unsupported = s.Master != nil && s.Master.Unsupported
	slices.SortStableFunc(x.Occurrences, func(a, b Occurrence) int { return a.Start.Compare(b.Start) })
	return x, nil
}

// Overlaps reports whether any occurrence overlaps [from, to); a zero from
// or to is open. It stops at the first match, so a series without end
// costs one walk up to from. When a cap ends the walk it answers true:
// the client filters again.
func (s *Series) Overlaps(from, to time.Time) bool {
	found := false
	truncated, failed := s.safeWalk(from, to, func(Occurrence) bool {
		found = true
		return false
	})
	if failed {
		truncated, _ = s.safeWalk(from, to, func(Occurrence) bool {
			found = true
			return false
		})
	}
	return found || truncated
}

// safeWalk runs walk under recover. A panic marks the master unsupported,
// so a second walk shows it once without calling rrule-go again.
func (s *Series) safeWalk(from, to time.Time, emit func(Occurrence) bool) (truncated, failed bool) {
	defer func() {
		if r := recover(); r != nil {
			failed = true
			if s.Master != nil {
				s.Master.Unsupported, s.Master.Rule, s.Master.RDates = true, nil, nil
				s.Master.Reason = "expansion failed"
			}
			slog.Error("Calendar: a recurrence rule could not be expanded; the event shows once",
				"account", s.file.opt.Account, "name", s.file.opt.Name)
		}
	}()
	return s.walk(from, to, emit), false
}

// overlaps applies the time-range rule of RFC 4791 9.9: an occurrence with
// length overlaps when it starts before to and ends after from; one
// without length when it starts in [from, to). Zero from/to are open.
func overlaps(start, end, from, to time.Time) bool {
	if !end.After(start) {
		return (from.IsZero() || !start.Before(from)) && (to.IsZero() || start.Before(to))
	}
	return (to.IsZero() || start.Before(to)) && (from.IsZero() || end.After(from))
}

// span places an event start and duration on the timeline.
func span(v tz.Value, d tz.Duration, local *time.Location) (time.Time, time.Time) {
	if v.IsDate() {
		e := v.Time.AddDate(0, 0, d.Days)
		start := v.InZone(local)
		end := time.Date(e.Year(), e.Month(), e.Day(), 0, 0, 0, 0, local).Add(d.Exact)
		return start, end
	}
	start := v.InZone(local)
	return start, d.AddTo(start)
}

// instance is one member of the master's recurrence set, in the zone of
// expansion (00:00 UTC for dates).
type instance struct {
	start time.Time
	end   time.Time // only for RDATE periods
}

type thisAndFuture struct {
	at    time.Time // the RECURRENCE-ID as an instance
	shift time.Duration
	days  int // shift of a date series
	ev    *Event
}

// walk emits the occurrences overlapping [from, to) until emit returns
// false; it reports whether a cap ended it.
func (s *Series) walk(from, to time.Time, emit func(Occurrence) bool) bool {
	local := s.file.opt.Local
	m := s.Master
	for _, o := range s.Overrides {
		st, en := span(o.Start, o.Duration, local)
		if !overlaps(st, en, from, to) {
			continue
		}
		occ := Occurrence{Start: st, End: en, AllDay: o.Start.IsDate(), RecurrenceID: *o.RecurrenceID, Event: o, Override: true}
		occ.Orphan = m == nil || !s.isInstance(*o.RecurrenceID)
		if !emit(occ) {
			return false
		}
	}
	if m == nil {
		return false
	}
	exact, tf := s.overrideIndex()
	// An override for this and following may move later instances
	// earlier; instances up to that far after the window still count.
	var back time.Duration
	for _, t := range tf {
		back = max(back, -(t.shift + time.Duration(t.days)*24*time.Hour))
	}
	stop := time.Time{}
	if !to.IsZero() {
		stop = to.Add(back + time.Hour)
	}
	truncated := s.instances(func(in instance) bool {
		if !stop.IsZero() && in.start.After(stop) {
			return false
		}
		if exact.has(in.start, m.Start, local) {
			return true // the override stands for it
		}
		occ := Occurrence{AllDay: m.Start.IsDate(), RecurrenceID: s.ridOf(in), Event: m}
		dur := m.Duration
		v := s.ridOf(in)
		for i := len(tf) - 1; i >= 0; i-- {
			if !in.start.Before(tf[i].at) {
				t := tf[i]
				occ.Event, dur = t.ev, t.ev.Duration
				if v.IsDate() {
					v.Time = v.Time.AddDate(0, 0, t.days)
				} else {
					v.Time = v.Time.Add(t.shift)
				}
				break
			}
		}
		occ.Start, occ.End = span(v, dur, local)
		if !in.end.IsZero() && occ.Event == m {
			occ.End = in.end
		}
		if !overlaps(occ.Start, occ.End, from, to) {
			return true
		}
		return emit(occ)
	})
	return truncated
}

// ridOf is the RECURRENCE-ID of an instance, in the form of DTSTART.
func (s *Series) ridOf(in instance) tz.Value {
	m := s.Master
	v := tz.Value{Time: in.start, Kind: m.Start.Kind, TZID: m.Start.TZID, Guess: m.Start.Guess}
	return v
}

// isInstance reports whether rid is a member of the master's set.
func (s *Series) isInstance(rid tz.Value) bool {
	local := s.file.opt.Local
	m := s.Master
	want := s.toInstance(rid)
	idx := keyIndex{}
	idx.add(rid, m.Start, local, nil)
	found := false
	s.instances(func(in instance) bool {
		if idx.has(in.start, m.Start, local) {
			found = true
			return false
		}
		return !in.start.After(want.Add(24 * time.Hour))
	})
	return found
}

// keyIndex finds instances by RECURRENCE-ID or EXDATE: by instant for a
// date-time series, by date for a date series or a date value.
type keyIndex struct {
	instants map[int64]*Event
	dates    map[int]*Event
}

func ymd(t time.Time) int {
	y, m, d := t.Date()
	return y*10000 + int(m)*100 + d
}

func (k *keyIndex) add(v, master tz.Value, local *time.Location, ev *Event) {
	if k.instants == nil {
		k.instants, k.dates = map[int64]*Event{}, map[int]*Event{}
	}
	switch {
	case v.IsDate():
		k.dates[ymd(v.Time)] = ev
	case master.IsDate():
		k.dates[ymd(v.InZone(local).In(local))] = ev
	default:
		k.instants[v.InZone(local).Unix()] = ev
	}
}

func (k *keyIndex) has(in time.Time, master tz.Value, local *time.Location) bool {
	if k.instants == nil {
		return false
	}
	if master.IsDate() {
		_, ok := k.dates[ymd(in)]
		return ok
	}
	if _, ok := k.instants[in.Unix()]; ok {
		return true
	}
	_, ok := k.dates[ymd(in)]
	return ok
}

func (s *Series) overrideIndex() (keyIndex, []thisAndFuture) {
	local := s.file.opt.Local
	var exact keyIndex
	var tf []thisAndFuture
	for _, o := range s.Overrides {
		exact.add(*o.RecurrenceID, s.Master.Start, local, o)
		if !o.ThisAndFuture {
			continue
		}
		t := thisAndFuture{at: s.toInstance(*o.RecurrenceID), ev: o}
		if o.Start.IsDate() && o.RecurrenceID.IsDate() {
			t.days = int(o.Start.Time.Sub(o.RecurrenceID.Time).Hours() / 24)
		} else {
			t.shift = o.Start.InZone(local).Sub(o.RecurrenceID.InZone(local))
		}
		tf = append(tf, t)
	}
	slices.SortFunc(tf, func(a, b thisAndFuture) int { return a.at.Compare(b.at) })
	return exact, tf
}

// zone is where rrule-go expands: UTC for dates, the NAS zone for floating
// times, else the zone of DTSTART.
func (s *Series) zone() *time.Location {
	switch s.Master.Start.Kind {
	case tz.KindDate, tz.KindUTC:
		return time.UTC
	case tz.KindFloating:
		return s.file.opt.Local
	}
	return s.Master.Start.Time.Location()
}

// toInstance converts a date or date-time into the terms of the master's
// instances: a date series takes the (local) date, a date-time series
// places a date at DTSTART's time of day.
func (s *Series) toInstance(v tz.Value) time.Time {
	m := s.Master
	local := s.file.opt.Local
	loc := s.zone()
	if m.Start.IsDate() {
		if v.IsDate() {
			return v.Time
		}
		t := v.InZone(local).In(local)
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	if v.IsDate() {
		st := s.file.expansionStart(m.Start)
		return time.Date(v.Time.Year(), v.Time.Month(), v.Time.Day(), st.Hour(), st.Minute(), st.Second(), 0, loc)
	}
	return v.InZone(local).In(loc)
}

// newIterator is replaced in tests to provoke a panic.
var newIterator = func(o rrule.ROption) (func() (time.Time, bool), error) {
	r, err := rrule.NewRRule(o)
	if err != nil {
		return nil, err
	}
	return r.Iterator(), nil
}

// instances walks the recurrence set of the master in order: DTSTART, the
// rule, RDATE, minus EXDATE. fn returns false to stop. The result reports
// whether maxSteps ended the walk.
func (s *Series) instances(fn func(instance) bool) bool {
	m := s.Master
	local := s.file.opt.Local
	first := s.file.expansionStart(m.Start)
	var extra []instance
	extra = append(extra, instance{start: first})
	if m.Recurring() {
		for _, v := range m.RDates {
			in := instance{start: s.toInstance(v)}
			if !v.End.IsZero() {
				in.end = v.End
			}
			extra = append(extra, in)
		}
	}
	slices.SortStableFunc(extra, func(a, b instance) int { return a.start.Compare(b.start) })
	var ex keyIndex
	for _, v := range m.ExDates {
		ex.add(v, m.Start, local, nil)
	}

	var next func() (time.Time, bool)
	if m.Recurring() && m.Rule != nil {
		var err error
		next, err = newIterator(m.Rule.option(first, m.Start, local))
		if err != nil {
			next = nil
		}
	}
	steps := 0
	var pending *time.Time
	pull := func() *time.Time {
		if pending == nil && next != nil {
			steps++
			if t, ok := next(); ok {
				pending = &t
			} else {
				next = nil
			}
		}
		return pending
	}
	var last time.Time
	emitted := false
	for {
		if steps >= maxSteps {
			return true
		}
		var in instance
		p := pull()
		switch {
		case p == nil && len(extra) == 0:
			return false
		case p == nil || (len(extra) > 0 && !extra[0].start.After(*p)):
			in = extra[0]
			extra = extra[1:]
		default:
			in = instance{start: *p}
			pending = nil
		}
		if emitted && in.start.Equal(last) {
			continue
		}
		last, emitted = in.start, true
		if !m.Recurring() && !in.start.Equal(first) {
			continue
		}
		if ex.has(in.start, m.Start, local) {
			continue
		}
		if !fn(in) {
			return false
		}
		if !m.Recurring() {
			return false
		}
	}
}

var rruleDays = [7]rrule.Weekday{rrule.SU, rrule.MO, rrule.TU, rrule.WE, rrule.TH, rrule.FR, rrule.SA}

// option builds the rrule-go options. UNTIL is converted into the zone of
// DTSTART here (rrule-go would read every UNTIL as UTC, #67).
func (r *Rule) option(start time.Time, dtstart tz.Value, local *time.Location) rrule.ROption {
	freq := map[Freq]rrule.Frequency{Daily: rrule.DAILY, Weekly: rrule.WEEKLY, Monthly: rrule.MONTHLY, Yearly: rrule.YEARLY}[r.Freq]
	o := rrule.ROption{
		Freq:       freq,
		Dtstart:    start,
		Interval:   r.Interval,
		Count:      r.Count,
		Wkst:       rruleDays[r.Wkst],
		Bysetpos:   slices.Clone(r.BySetPos),
		Bymonth:    slices.Clone(r.ByMonth),
		Bymonthday: slices.Clone(r.ByMonthDay),
		Byyearday:  slices.Clone(r.ByYearDay),
		Byweekno:   slices.Clone(r.ByWeekNo),
	}
	if !r.WkstSet {
		o.Wkst = rrule.MO
	}
	for _, d := range r.ByDay {
		wd := rruleDays[d.Day]
		if d.N != 0 {
			wd = wd.Nth(d.N)
		}
		o.Byweekday = append(o.Byweekday, wd)
	}
	if r.Until != nil {
		o.Until = untilIn(*r.Until, start, dtstart.IsDate(), local)
	}
	return o
}

// untilIn places UNTIL on the timeline of the expansion. For a date series
// (expanded in UTC) it is a date: a UTC or floating UNTIL counts by its
// date in the NAS zone (Google writes a UTC time for all-day series). For a
// date-time series a UTC UNTIL keeps its instant, a floating one is read in
// the zone of DTSTART, and a date includes that whole day.
func untilIn(u tz.Value, start time.Time, dateSeries bool, local *time.Location) time.Time {
	loc := start.Location()
	if dateSeries {
		var t time.Time
		switch u.Kind {
		case tz.KindUTC:
			t = u.Time.In(local)
		default:
			t = u.Time
		}
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
	}
	switch u.Kind {
	case tz.KindUTC:
		return u.Time.In(loc)
	case tz.KindDate:
		return time.Date(u.Time.Year(), u.Time.Month(), u.Time.Day(), 23, 59, 59, 0, loc)
	}
	return time.Date(u.Time.Year(), u.Time.Month(), u.Time.Day(), u.Time.Hour(), u.Time.Minute(), u.Time.Second(), 0, loc)
}
