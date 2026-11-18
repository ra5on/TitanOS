package recur

import (
	"slices"
	"strings"
	"time"

	"github.com/emersion/go-ical"

	"github.com/ra5on/TitanOS/backend/internal/pim/tz"
)

// MaxAlarms bounds the alarms listed for one event.
const MaxAlarms = 20

// AlarmMinutes reads a VALARM that fires a whole number of minutes before
// (or at) the start of its event: TRIGGER a duration ≤ 0 related to START.
// ok is false for any other alarm (an absolute time, related to the end,
// after the start, seconds): those are kept but not offered for editing.
func AlarmMinutes(a *ical.Component) (minutes int, ok bool) {
	if a == nil || a.Name != ical.CompAlarm {
		return 0, false
	}
	p := a.Props.Get(ical.PropTrigger)
	if p == nil {
		return 0, false
	}
	if v := p.Params.Get(ical.ParamValue); v != "" && !strings.EqualFold(v, "DURATION") {
		return 0, false
	}
	if r := p.Params.Get(ical.ParamRelated); r != "" && !strings.EqualFold(r, "START") {
		return 0, false
	}
	d, err := tz.ParseDuration(p.Value)
	if err != nil || (!d.Negative && !d.IsZero()) || d.Exact%time.Minute != 0 {
		return 0, false
	}
	return d.Days*24*60 + int(d.Exact/time.Minute), true
}

// Alarms lists the editable alarms of an event (AlarmMinutes) as minutes
// before the start, sorted, each once, at most MaxAlarms.
func Alarms(ev *ical.Component) []int {
	var out []int
	for _, c := range ev.Children {
		if m, ok := AlarmMinutes(c); ok && !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	slices.Sort(out)
	if len(out) > MaxAlarms {
		out = out[:MaxAlarms]
	}
	return out
}
