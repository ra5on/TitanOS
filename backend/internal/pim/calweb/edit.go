package calweb

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
	pimstore "github.com/ra5on/TitanOS/backend/internal/pim/store"
	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// ErrNoOccurrence: the occurrence named is not part of the event (404).
var ErrNoOccurrence = recur.ErrNoOccurrence

// Views turns the occurrences of a calendar (pim/store Query) into views.
// local is the zone of the NAS.
func Views(calendar string, occs []pimstore.Occurrence, local *time.Location) []View {
	out := make([]View, 0, len(occs))
	rules := map[string]*RuleView{} // one text per file version
	for _, o := range occs {
		v := View{Kind: "event", ID: o.Name, Occurrence: o.ID, Calendar: calendar, UID: o.UID, Title: o.Summary,
			AllDay: o.AllDay, Timezone: o.TZID, Floating: o.Floating, Location: o.Location, Recurring: o.Recurs,
			Alarms: append([]int{}, o.Alarms...), Exception: o.Override, ETag: o.ETag, TZGuess: o.TZGuess,
			RecurUnsupported: o.Unsupported}
		if o.AllDay {
			v.Start, v.End = o.Start.In(local).Format(dateLayout), o.End.In(local).Format(dateLayout)
		} else {
			loc := local
			if !o.Floating {
				if l := Zone(o.TZID); l != nil {
					loc = l
				}
			}
			v.Start, v.End = o.Start.In(loc).Format(timeLayout), o.End.In(loc).Format(timeLayout)
		}
		if o.Rule != nil {
			key := o.Name + "\x00" + o.ETag
			r, ok := rules[key]
			if !ok {
				r = ruleView(o.Rule, o.RuleZone)
				rules[key] = r
			}
			v.Rule = r
		}
		out = append(out, v)
	}
	return out
}

// opened is the occurrence the UI opened, read from the current file.
type opened struct {
	file       *recur.File
	series     *recur.Series
	ev         *recur.Event // the master or the override standing for it
	start, end tz.Value
	view       View
	hint       zoneHint
	rule       *recur.Simple
}

// zoneLoc is the zone a value is read in: its own, UTC, or the NAS zone.
func zoneLoc(v tz.Value, local *time.Location) *time.Location {
	switch v.Kind {
	case tz.KindUTC:
		return time.UTC
	case tz.KindZoned:
		return v.Time.Location()
	}
	return local
}

// endOf is the end of an occurrence that starts at start.
func endOf(start tz.Value, d tz.Duration) tz.Value {
	if start.IsDate() {
		t := start.Time.AddDate(0, 0, d.Days).Add(d.Exact)
		return tz.Value{Time: time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), Kind: tz.KindDate}
	}
	return tz.Value{Time: d.AddTo(start.Time), Kind: start.Kind, TZID: start.TZID}
}

func open(data []byte, occurrence string, local *time.Location) (*opened, error) {
	f, err := recur.Read(data, recur.Options{Local: local})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if len(f.Series) != 1 {
		return nil, invalid("a file holds the events of one UID")
	}
	sr := f.Series[0]
	op := &opened{file: f, series: sr}
	m := sr.Master
	switch {
	case occurrence == "" && m != nil:
		op.ev, op.start = m, m.Start
	case occurrence == "":
		op.ev, op.start = sr.Overrides[0], sr.Overrides[0].Start
	default:
		id, err := recur.ParseOccurrenceID(occurrence)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrNoOccurrence, err)
		}
		for _, o := range sr.Overrides {
			if o.RecurrenceID.IsDate() == id.IsDate() && o.RecurrenceID.InZone(local).Equal(id.InZone(local)) {
				op.ev, op.start = o, o.Start
				break
			}
		}
		if op.ev == nil {
			if m == nil {
				return nil, ErrNoOccurrence
			}
			if !m.Recurring() {
				if recur.OccurrenceID(m.Start) != occurrence {
					return nil, ErrNoOccurrence
				}
				op.ev, op.start = m, m.Start
				break
			}
			at := id.InZone(local)
			x, err := sr.Expand(at, at.Add(24*time.Hour+time.Second))
			if err != nil {
				return nil, ErrNoOccurrence
			}
			for _, o := range x.Occurrences {
				if !o.Override && o.ID() == occurrence {
					op.ev, op.start = m, o.RecurrenceID
					break
				}
			}
			if op.ev == nil {
				return nil, ErrNoOccurrence
			}
		}
	}
	op.end = endOf(op.start, op.ev.Duration)
	if op.ev.Start.Kind != tz.KindDate {
		sp := op.ev.Component.Props.Get(ical.PropDateTimeStart)
		op.hint = zoneHint{kind: op.ev.Start.Kind, name: pimstore.ZoneName(op.ev.Start), tzid: sp.Params.Get(ical.ParamTimezoneID), loc: zoneLoc(op.ev.Start, local)}
	}
	c := op.ev.Component
	title, _ := c.Props.Text(ical.PropSummary)
	location, _ := c.Props.Text(ical.PropLocation)
	notes, _ := c.Props.Text(ical.PropDescription)
	id := ""
	if op.ev.RecurrenceID != nil {
		id = recur.OccurrenceID(*op.ev.RecurrenceID)
	} else {
		id = recur.OccurrenceID(op.start)
	}
	op.view = View{Kind: "event", Occurrence: id, UID: sr.UID, Title: title, Start: formatValue(op.start), End: formatValue(op.end),
		AllDay: op.start.IsDate(), Timezone: pimstore.ZoneName(op.start), Floating: op.start.Kind == tz.KindFloating,
		Location: location, Notes: &notes, Recurring: m == nil || len(sr.Overrides) > 0 || m.Recurring(),
		Alarms: append([]int{}, recur.Alarms(c)...), Exception: op.ev.RecurrenceID != nil, TZGuess: op.ev.TZGuess,
		RecurUnsupported: m != nil && m.Unsupported}
	if m != nil && m.Rule != nil {
		loc := zoneLoc(m.Start, local)
		op.view.Rule = ruleView(m.Rule, loc)
		s := m.Rule.Simple(loc)
		op.rule = &s
	}
	return op, nil
}

// Open reads one event of a file for the UI, with its notes: the
// occurrence named, or the event itself when occurrence is empty.
func Open(data []byte, occurrence string, local *time.Location) (View, error) {
	op, err := open(data, occurrence, local)
	if err != nil {
		return View{}, err
	}
	return op.view, nil
}

// EditOptions carry what an edit needs from outside.
type EditOptions struct {
	Local *time.Location
	Now   time.Time
	// NewUID is the UID of the new series of "this and following".
	NewUID string
}

// sameRule compares two simple rules in their meaning.
func sameRule(a, b recur.Simple) bool {
	norm := func(s recur.Simple) string {
		s.Freq = strings.ToLower(s.Freq)
		s.Interval = max(s.Interval, 1)
		s.Custom = false
		for i := range s.ByDay {
			s.ByDay[i] = strings.ToUpper(s.ByDay[i])
		}
		if len(s.ByDay) == 0 {
			s.ByDay = nil
		}
		if len(s.ByMonthDay) == 0 {
			s.ByMonthDay = nil
		}
		b, _ := json.Marshal(s)
		return string(b)
	}
	a.ByDay, b.ByDay = slices.Clone(a.ByDay), slices.Clone(b.ByDay)
	return norm(a) == norm(b)
}

// Edit applies the input to the occurrence of the file (scope this,
// following or all, pim/recur) on its component tree: only the fields
// that differ from the occurrence as read are written, so unknown
// properties, other VALARMs and an unchanged rule (COUNT already cut by
// "following") stay. For "all" a moved time moves the series by the same
// amount, from wherever the occurrence was. changed is false when the
// input asks for nothing new (nothing to write).
func Edit(data []byte, occurrence string, scope recur.Scope, in Input, opt EditOptions) (res recur.EditResult, changed bool, err error) {
	local := opt.Local
	if local == nil {
		local = time.UTC
	}
	op, err := open(data, occurrence, local)
	if err != nil {
		return recur.EditResult{}, false, err
	}
	var want *times
	if in.Start != "" {
		t, err := parseTimes(in, local, op.hint)
		if err != nil {
			return recur.EditResult{}, false, err
		}
		if formatValue(t.start) != op.view.Start || formatValue(t.end) != op.view.End || t.start.Kind != op.start.Kind ||
			t.start.TZID != op.start.TZID && pimstore.ZoneName(t.start) != op.view.Timezone {
			want = &t
		}
	}
	type text struct {
		prop  string
		value string
	}
	var texts []text
	for _, f := range []struct {
		prop  string
		value *string
		old   string
		limit int
		multi bool
	}{{ical.PropSummary, in.Title, op.view.Title, MaxTitle, false}, {ical.PropLocation, in.Location, op.view.Location, MaxLocation, false},
		{ical.PropDescription, in.Notes, *op.view.Notes, MaxNotes, true}} {
		if f.value == nil {
			continue
		}
		v, err := checkText(f.prop, *f.value, f.limit, f.multi)
		if err != nil {
			return recur.EditResult{}, false, err
		}
		if v != f.old {
			texts = append(texts, text{f.prop, v})
		}
	}
	removeRule := false
	var newRule *recur.Simple
	if in.Repeat.Set {
		switch r := in.Repeat.Value; {
		case r == nil:
			removeRule = op.rule != nil
		case r.Custom:
			if op.rule == nil {
				return recur.EditResult{}, false, invalid("custom rule without a rule to keep")
			}
		case op.rule == nil || !sameRule(*op.rule, *r):
			newRule = r
		}
	}
	var alarms []int
	if in.Alarms.Set {
		var v []int
		if in.Alarms.Value != nil {
			v = *in.Alarms.Value
		}
		if alarms, err = checkAlarms(v); err != nil {
			return recur.EditResult{}, false, err
		}
		if slices.Equal(alarms, op.view.Alarms) {
			alarms = nil
		} else if alarms == nil {
			alarms = []int{}
		}
	}
	if want == nil && len(texts) == 0 && !removeRule && newRule == nil && alarms == nil {
		return recur.EditResult{}, false, nil
	}
	if newRule != nil {
		if _, err := newRule.Rule(op.start); err != nil {
			return recur.EditResult{}, false, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
	}
	resolver := tz.NewResolver(local, op.file.Calendar)
	change := func(c *ical.Component) error {
		master := c.Props.Get(ical.PropRecurrenceID) == nil
		uid, _ := c.Props.Text(ical.PropUID)
		all := master && uid == op.series.UID
		for _, t := range texts {
			if t.value == "" {
				c.Props.Del(t.prop)
			} else {
				c.Props.SetText(t.prop, t.value)
			}
		}
		if want != nil {
			if all && op.series.Master != nil {
				setTimes(c, shiftMaster(op, *want))
			} else {
				setTimes(c, *want)
			}
		}
		if master {
			if removeRule {
				c.Props.Del(ical.PropRecurrenceRule)
				if c.Props.Get(ical.PropRecurrenceDates) == nil {
					c.Props.Del(ical.PropExceptionDates)
				}
			}
			if newRule != nil {
				start, err := resolver.Parse(c.Props.Get(ical.PropDateTimeStart))
				if err != nil {
					return fmt.Errorf("%w: %v", ErrInvalid, err)
				}
				if err := setRepeat(c, *newRule, start); err != nil {
					return err
				}
			}
		}
		if alarms != nil {
			applyAlarms(c, alarms)
		}
		return nil
	}
	res, err = recur.Update(data, "", occurrence, scope, change, recur.EditOptions{Local: local, Now: opt.Now, NewUID: opt.NewUID, ProdID: ProdID})
	if err != nil {
		if errors.Is(err, recur.ErrNoOccurrence) {
			return recur.EditResult{}, false, ErrNoOccurrence
		}
		if errors.Is(err, recur.ErrInvalid) || errors.Is(err, ErrInvalid) {
			return recur.EditResult{}, false, fmt.Errorf("%w: %v", ErrInvalid, err)
		}
		return recur.EditResult{}, false, err
	}
	if res.Data, err = EnsureZones(res.Data); err != nil {
		return recur.EditResult{}, false, err
	}
	if res.New, err = EnsureZones(res.New); err != nil {
		return recur.EditResult{}, false, err
	}
	return res, true, nil
}

// wallDate is the calendar date a value shows on its own clock.
func wallDate(v tz.Value) time.Time {
	t := v.Time
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// shiftMaster is the new DTSTART/DTEND of the master when the occurrence
// op opened moves to want: the master moves by the same number of days and
// takes the new time of day, zone and kind; the length is that of want.
func shiftMaster(op *opened, want times) times {
	m := op.series.Master
	days := int(wallDate(want.start).Sub(wallDate(op.start)).Hours() / 24)
	if want.start.IsDate() != op.start.IsDate() {
		// From all-day to timed or back: the date of the occurrence counts.
		days = int(wallDate(want.start).Sub(wallDate(op.start)).Round(24*time.Hour).Hours() / 24)
	}
	date := wallDate(m.Start).AddDate(0, 0, days)
	var start tz.Value
	if want.start.IsDate() {
		start = tz.Value{Time: date, Kind: tz.KindDate}
		length := int(want.end.Time.Sub(want.start.Time).Hours() / 24)
		return times{start: start, end: tz.Value{Time: date.AddDate(0, 0, length), Kind: tz.KindDate}}
	}
	w := want.start.Time
	t := time.Date(date.Year(), date.Month(), date.Day(), w.Hour(), w.Minute(), w.Second(), 0, w.Location())
	start = tz.Value{Time: t, Kind: want.start.Kind, TZID: want.start.TZID}
	end := tz.Value{Time: t.Add(want.end.Time.Sub(want.start.Time)), Kind: want.end.Kind, TZID: want.end.TZID}
	if start.Kind == tz.KindUTC {
		start.Time, end.Time = start.Time.UTC(), end.Time.UTC()
	}
	return times{start: start, end: end}
}
