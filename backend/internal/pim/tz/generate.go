package tz

import (
	"fmt"
	"strconv"
	"time"

	"github.com/emersion/go-ical"
)

// VTimezone builds the VTIMEZONE component for a zone TitanOS writes into
// an event (RFC 5545 3.6.5: every TZID used needs one). It describes the
// years from to to: a STANDARD and a DAYLIGHT with a yearly RRULE when the
// changes of all those years follow one weekday rule (Europe, the US …),
// else one observance per change; a zone without changes gets a single
// STANDARD. X-LIC-LOCATION names the zone for readers that know IANA.
func VTimezone(tzid string, loc *time.Location, from, to int) *ical.Component {
	if to < from {
		to = from
	}
	c := ical.NewComponent(ical.CompTimezone)
	c.Props.SetText(ical.PropTimezoneID, tzid)
	c.Props.SetText("X-LIC-LOCATION", loc.String())
	var all []change
	for y := from; y <= to; y++ {
		all = append(all, changesIn(loc, y)...)
	}
	if len(all) == 0 {
		jan := time.Date(from, 1, 1, 0, 0, 0, 0, loc)
		name, off := jan.Zone()
		c.Children = append(c.Children, observanceComp(true, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), off, off, name, ""))
		return c
	}
	if rules, ok := yearlyRules(all, from, to); ok {
		for _, r := range rules {
			c.Children = append(c.Children, observanceComp(!r.first.dst, r.first.wall, r.first.from, r.first.to, r.first.name, r.rrule))
		}
		return c
	}
	for _, ch := range all {
		c.Children = append(c.Children, observanceComp(!ch.dst, ch.wall, ch.from, ch.to, ch.name, ""))
	}
	return c
}

// change is one change of offset: the wall clock just before it (in the
// old offset), the offsets and the name after it.
type change struct {
	wall     time.Time // read in UTC
	from, to int
	dst      bool
	name     string
}

func changesIn(loc *time.Location, year int) []change {
	ts, before := ianaTransitions(loc, year)
	out := make([]change, 0, len(ts))
	prev := before
	for _, t := range ts {
		after := t.at.In(loc)
		name, _ := after.Zone()
		wall := t.at.Add(time.Duration(prev) * time.Second).UTC()
		out = append(out, change{wall: wall, from: prev, to: t.offset, dst: after.IsDST(), name: name})
		prev = t.offset
	}
	return out
}

type yearly struct {
	first change
	rrule string
}

// yearlyRules finds one RRULE per kind (standard, daylight) that gives
// every change of the years: same month, weekday, nth (or last) weekday,
// time of day and offsets, exactly one per year.
func yearlyRules(all []change, from, to int) ([]yearly, bool) {
	var out []yearly
	for _, dst := range []bool{true, false} {
		var mine []change
		for _, ch := range all {
			if ch.dst == dst {
				mine = append(mine, ch)
			}
		}
		if len(mine) != to-from+1 {
			return nil, false
		}
		f := mine[0]
		last := f.wall.Day()+7 > daysIn(f.wall.Year(), f.wall.Month())
		nth := (f.wall.Day()-1)/7 + 1
		for i, ch := range mine {
			if ch.wall.Year() != from+i || ch.wall.Month() != f.wall.Month() || ch.wall.Weekday() != f.wall.Weekday() ||
				ch.from != f.from || ch.to != f.to || ch.name != f.name {
				return nil, false
			}
			h, m, s := ch.wall.Clock()
			fh, fm, fs := f.wall.Clock()
			if h != fh || m != fm || s != fs {
				return nil, false
			}
			chLast := ch.wall.Day()+7 > daysIn(ch.wall.Year(), ch.wall.Month())
			chNth := (ch.wall.Day()-1)/7 + 1
			if last && !chLast {
				last = false
			}
			if chNth != nth {
				nth = 0
			}
		}
		var by string
		switch {
		case last:
			by = "-1"
		case nth > 0:
			by = strconv.Itoa(nth)
		default:
			return nil, false
		}
		day := [...]string{"SU", "MO", "TU", "WE", "TH", "FR", "SA"}[f.wall.Weekday()]
		out = append(out, yearly{first: f, rrule: fmt.Sprintf("FREQ=YEARLY;BYMONTH=%d;BYDAY=%s%s", int(f.wall.Month()), by, day)})
	}
	return out, true
}

func observanceComp(standard bool, wall time.Time, from, to int, name, rrule string) *ical.Component {
	kind := ical.CompTimezoneDaylight
	if standard {
		kind = ical.CompTimezoneStandard
	}
	o := ical.NewComponent(kind)
	start := ical.NewProp(ical.PropDateTimeStart)
	start.Value = wall.Format(localLayout)
	o.Props.Set(start)
	for _, p := range []struct {
		name string
		off  int
	}{{ical.PropTimezoneOffsetFrom, from}, {ical.PropTimezoneOffsetTo, to}} {
		prop := ical.NewProp(p.name)
		prop.Value = formatOffset(p.off)
		o.Props.Set(prop)
	}
	if name != "" {
		o.Props.SetText(ical.PropTimezoneName, name)
	}
	if rrule != "" {
		r := ical.NewProp(ical.PropRecurrenceRule)
		r.Value = rrule
		o.Props.Set(r)
	}
	return o
}

// formatOffset writes seconds east of UTC as UTC-OFFSET (+0100, -0330,
// +053128 with seconds).
func formatOffset(sec int) string {
	sign := "+"
	if sec < 0 {
		sign, sec = "-", -sec
	}
	s := fmt.Sprintf("%s%02d%02d", sign, sec/3600, sec%3600/60)
	if sec%60 != 0 {
		s += fmt.Sprintf("%02d", sec%60)
	}
	return s
}
