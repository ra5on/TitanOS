package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/auth"
	pa "github.com/ra5on/TitanOS/backend/internal/personalagent"
	store "github.com/ra5on/TitanOS/backend/internal/pim/store"
)

func (p personalPIM) free(ctx context.Context, u auth.User, r pa.Request) (pa.Preview, error) {
	var out pa.Preview
	if err := p.available(u); err != nil {
		return out, err
	}
	if len(r.Sources) != 1 || r.Sources[0].Kind != "calendar.range" {
		return out, errPersonalSource
	}
	src := r.Sources[0]
	col, err := p.area.store.GetCollection(store.Calendar, u.Name, src.Ref["collection"])
	if err != nil {
		return out, errPersonalSource
	}
	zone := p.area.store.Zone()
	from, ok := parseInstant(src.Ref["start"], zone)
	to, endOK := parseInstant(src.Ref["end"], zone)
	if !ok || !endOK || !to.After(from) || to.Sub(from) > 7*24*time.Hour {
		return out, errors.New("select a calendar window of at most seven days")
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	entries, cut, err := p.area.store.Query(u.Name, col.ID, from, to, store.MaxQueryOccurrences)
	if err != nil || cut {
		return out, errors.New("calendar availability could not be checked completely")
	}
	type interval struct {
		Start string `json:"start"`
		End   string `json:"end"`
	}
	slots := []interval{}
	var lines []string
	next := from
	add := func(a, b time.Time) {
		if b.After(a) {
			slots = append(slots, interval{a.Format(time.RFC3339), b.Format(time.RFC3339)})
			lines = append(lines, a.In(zone).Format("2006-01-02 15:04")+" – "+b.In(zone).Format("2006-01-02 15:04"))
		}
	}
	for _, entry := range entries {
		if entry.Unsupported || entry.Truncated {
			return out, errors.New("calendar recurrence could not be checked completely")
		}
		if entry.Start.After(next) {
			add(next, entry.Start)
		}
		if entry.End.After(next) {
			next = entry.End
		}
	}
	add(next, to)
	src.Label = col.Name
	text := fmt.Sprintf("%s · %s\n", col.Name, zone.String())
	if len(lines) == 0 {
		text += "Kein freies Zeitfenster im gewählten Zeitraum."
	} else {
		text += strings.Join(lines, "\n")
	}
	return pa.Preview{Title: "Freie Zeiten", Text: text, Sources: []pa.Source{src}, Data: jsonPIM(map[string]any{"free": slots, "calendar": col.ID, "timezone": zone.String()})}, nil
}
func init() {
	registerPersonalTools(func(s *Server) []personalTool {
		p := personalPIM{s.pimDataArea(), store.Calendar, "calendar"}
		return []personalTool{{ID: "calendar.free", Title: "Freie Zeiten im ausgewählten Kalender", App: "calendar", Default: pa.Ask, SourceKinds: []string{"calendar.range"}, Available: p.available, Check: func(_ context.Context, u auth.User, r pa.Request) error {
			if err := p.available(u); err != nil {
				return err
			}
			if len(r.Sources) != 1 || r.Sources[0].Kind != "calendar.range" {
				return errPersonalSource
			}
			if _, err := p.area.store.GetCollection(store.Calendar, u.Name, r.Sources[0].Ref["collection"]); err != nil {
				return errPersonalSource
			}
			return nil
		}, Prepare: p.free}}
	})
}
