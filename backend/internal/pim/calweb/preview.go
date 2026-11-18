package calweb

import (
	"time"

	"github.com/ra5on/TitanOS/backend/internal/pim/recur"
)

// PreviewInput is what the window "Neuer Termin" shows the rule of.
type PreviewInput struct {
	Start    string        `json:"start"`
	End      string        `json:"end"`
	Timezone string        `json:"timezone"`
	AllDay   bool          `json:"allDay"`
	Repeat   *recur.Simple `json:"repeat"`
}

// PreviewTime is one occurrence of the preview.
type PreviewTime struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// PreviewResult: the next occurrences from the start, the rule in words
// (de, en; null without rule), the number of occurrences when the rule
// ends (null when it does not, or when it ends too far away to count),
// and whether there are more than shown.
type PreviewResult struct {
	Occurrences []PreviewTime     `json:"occurrences"`
	Text        map[string]string `json:"text"`
	Total       *int              `json:"total"`
	More        bool              `json:"more"`
}

// previewWindows bounds the windows of 400 days counted for Total.
const previewWindows = 30

// Preview resolves a rule the way the calendar will (one resolution, on
// the server): the first n occurrences.
func Preview(in PreviewInput, n int, local *time.Location) (PreviewResult, error) {
	repeat := Opt[recur.Simple]{Set: true, Value: in.Repeat}
	data, err := Build(Input{Start: in.Start, End: in.End, Timezone: in.Timezone, AllDay: in.AllDay, Repeat: repeat}, "preview", time.Now(), local)
	if err != nil {
		return PreviewResult{}, err
	}
	f, err := recur.Read(data, recur.Options{Local: local})
	if err != nil || len(f.Series) != 1 || f.Series[0].Master == nil {
		return PreviewResult{}, invalid("rule not readable")
	}
	sr := f.Series[0]
	m := sr.Master
	loc := zoneLoc(m.Start, local)
	out := PreviewResult{Occurrences: []PreviewTime{}}
	if m.Rule != nil {
		out.Text = RuleText(m.Rule, loc)
	}
	total, counted, err := walkSeries(sr, local, func(o recur.Occurrence) bool {
		if len(out.Occurrences) < n {
			t := PreviewTime{Start: o.Start.In(loc).Format(timeLayout), End: o.End.In(loc).Format(timeLayout)}
			if o.AllDay {
				t = PreviewTime{Start: o.Start.In(local).Format(dateLayout), End: o.End.In(local).Format(dateLayout)}
			}
			out.Occurrences = append(out.Occurrences, t)
			return true
		}
		out.More = true
		return false
	})
	if err != nil {
		return PreviewResult{}, err
	}
	if counted {
		out.Total = &total
	}
	return out, nil
}

// walkSeries goes through the occurrences of a series from the start of
// its master in windows of 400 days (at most previewWindows, at most
// recur.MaxOccurrences); visit gets each one and returns whether it wants
// more. A rule that ends (COUNT, UNTIL) or no rule is walked to its end
// regardless, so that total counts all: counted is true then. A rule
// without end stops when visit has enough (counted false).
func walkSeries(sr *recur.Series, local *time.Location, visit func(recur.Occurrence) bool) (total int, counted bool, err error) {
	m := sr.Master
	finite := m.Rule == nil || m.Rule.Count > 0 || m.Rule.Until != nil
	from := m.Start.InZone(local)
	for range previewWindows {
		to := from.AddDate(0, 0, recur.MaxWindowDays)
		x, err := sr.Expand(from, to)
		if err != nil || x.Unsupported || x.Failed {
			return 0, false, invalid("rule not expandable")
		}
		for _, o := range x.Occurrences {
			if o.Start.Before(from) {
				continue // a long occurrence of the window before
			}
			total++
			if !visit(o) && !finite {
				return total, false, nil
			}
		}
		switch {
		case x.Truncated || total >= recur.MaxOccurrences:
			return total, false, nil
		case m.Rule == nil || m.Rule.Count > 0 && total >= m.Rule.Count:
			return total, true, nil
		case m.Rule.Until != nil && !to.Before(m.Rule.Until.InZone(local)):
			return total, true, nil
		}
		from = to
	}
	return total, false, nil
}

// occurrences is the number of occurrences of a series whose rule ends
// (COUNT or UNTIL): the instances of the rule, those an EXDATE takes away
// left out, RDATEs added; an override moves an occurrence and counts as
// it. What "Alle 12 Termine" in the delete dialog says. nil for a single
// event, a rule without end, or more than recur.MaxOccurrences (or 30
// windows of 400 days).
func occurrences(sr *recur.Series, local *time.Location) *int {
	m := sr.Master
	if m == nil || m.Rule == nil || m.Unsupported || m.Rule.Count == 0 && m.Rule.Until == nil {
		return nil
	}
	// The rule alone (COUNT counts the instances before EXDATE, RFC 5545
	// 3.3.10): a copy of the series without dates and overrides.
	plain := *sr
	master := *m
	master.ExDates, master.RDates = nil, nil
	plain.Master, plain.Overrides = &master, nil
	ids := map[string]bool{}
	total, counted, err := walkSeries(&plain, local, func(o recur.Occurrence) bool {
		ids[o.ID()] = true
		return true
	})
	if err != nil || !counted {
		return nil
	}
	for _, x := range m.ExDates {
		if id := recur.OccurrenceID(x); ids[id] {
			delete(ids, id)
			total--
		}
	}
	for _, r := range m.RDates {
		if id := recur.OccurrenceID(r); !ids[id] {
			ids[id] = true
			total++
		}
	}
	return &total
}
