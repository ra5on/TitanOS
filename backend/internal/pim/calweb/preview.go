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
	from := m.Start.InZone(local)
	total := 0
	counting := m.Rule != nil && (m.Rule.Count > 0 || m.Rule.Until != nil) || m.Rule == nil
	for w := 0; w < previewWindows; w++ {
		to := from.AddDate(0, 0, recur.MaxWindowDays)
		x, err := sr.Expand(from, to)
		if err != nil || x.Unsupported || x.Failed {
			return PreviewResult{}, invalid("rule not expandable")
		}
		for _, o := range x.Occurrences {
			if o.Start.Before(from) {
				continue // a long occurrence of the window before
			}
			total++
			if len(out.Occurrences) < n {
				t := PreviewTime{Start: o.Start.In(loc).Format(timeLayout), End: o.End.In(loc).Format(timeLayout)}
				if o.AllDay {
					t = PreviewTime{Start: o.Start.In(local).Format(dateLayout), End: o.End.In(local).Format(dateLayout)}
				}
				out.Occurrences = append(out.Occurrences, t)
			} else {
				out.More = true
			}
		}
		if x.Truncated || total >= recur.MaxOccurrences {
			counting = false
			break
		}
		if m.Rule == nil || m.Rule.Count > 0 && total >= m.Rule.Count {
			break
		}
		if m.Rule.Until != nil && !to.Before(m.Rule.Until.InZone(local)) {
			break
		}
		if !counting && len(out.Occurrences) >= n && out.More {
			break
		}
		from = to
		if w == previewWindows-1 {
			counting = false
		}
	}
	if counting {
		out.Total = &total
	}
	return out, nil
}
