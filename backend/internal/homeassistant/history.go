package homeassistant

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

type History struct {
	Entity     string    `json:"entity"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
	States     []State   `json:"states"`
	Cut        bool      `json:"cut"`
	NextOffset int       `json:"nextOffset,omitempty"`
	Source     string    `json:"source"`
}

// History reads one expressly released entity, at most 24 hours per request.
// HA recorder retention determines completeness; empty data is not proof that
// nothing happened. No history is copied onto the boot stick.
func (m *Manager) History(ctx context.Context, entity string, start, end time.Time, offset int) (History, error) {
	out := History{Entity: entity, Start: start, End: end, States: []State{}, Source: "Home Assistant recorder"}
	if !m.Allowed(entity) {
		return out, ErrNotAllowed
	}
	if start.IsZero() || !end.After(start) || end.Sub(start) > 24*time.Hour || end.After(time.Now().Add(time.Minute)) || offset < 0 || offset > 100000 {
		return out, invalid("history: select at most 24 hours and a valid offset")
	}
	query := url.Values{"filter_entity_id": {entity}, "end_time": {end.UTC().Format(time.RFC3339)}, "minimal_response": {""}, "no_attributes": {""}}
	var groups [][]State
	if err := m.do(ctx, http.MethodGet, "/api/history/period/"+url.PathEscape(start.UTC().Format(time.RFC3339))+"?"+query.Encode(), nil, &groups); err != nil {
		return out, err
	}
	// Minimal responses may omit entity_id on subsequent states.
	var states []State
	for _, group := range groups {
		if len(group) == 0 || group[0].EntityID != entity {
			continue
		}
		for _, state := range group {
			if state.EntityID != "" && state.EntityID != entity {
				continue
			}
			state.EntityID = entity
			states = append(states, state)
		}
	}
	first := min(offset, len(states))
	last := min(first+200, len(states))
	out.States = states[first:last]
	out.Cut = last < len(states)
	if out.Cut {
		out.NextOffset = last
	}
	return out, nil
}
