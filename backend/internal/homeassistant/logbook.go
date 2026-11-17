package homeassistant

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// LogbookEntry is one line of the logbook of Home Assistant: what changed
// or ran, when (an automation was triggered, a light went on).
type LogbookEntry struct {
	When    time.Time `json:"when"`
	Entity  string    `json:"entity"`
	Name    string    `json:"name,omitempty"`
	Message string    `json:"message,omitempty"`
	State   string    `json:"state,omitempty"`
}

// Logbook is a time window of the logbook, only released entities.
type Logbook struct {
	Start   time.Time      `json:"start"`
	End     time.Time      `json:"end"`
	Entries []LogbookEntry `json:"entries"`
	// Hidden: entries of entities that are not released (or of no entity)
	// were left out; how many is not said (1.09: no count of hidden ones).
	Hidden bool `json:"hidden,omitempty"`
}

// Logbook reads at most 24 hours of the logbook (1.10, "was lief heute
// Nacht?"). Lines of entities that are not released, and lines that name
// no entity, are left out. entity narrows it to one released entity.
func (m *Manager) Logbook(ctx context.Context, start, end time.Time, entity string) (Logbook, error) {
	out := Logbook{Start: start, End: end, Entries: []LogbookEntry{}}
	if start.IsZero() || !end.After(start) || end.Sub(start) > 24*time.Hour {
		return out, invalid("logbook: select at most 24 hours")
	}
	if entity != "" && !m.AllowedCtx(ctx, entity) {
		return out, ErrNotAllowed
	}
	query := url.Values{"end_time": {end.UTC().Format(time.RFC3339)}}
	if entity != "" {
		query.Set("entity", entity)
	}
	var lines []struct {
		When     time.Time `json:"when"`
		EntityID string    `json:"entity_id"`
		Name     string    `json:"name"`
		Message  string    `json:"message"`
		State    string    `json:"state"`
	}
	if err := m.do(ctx, http.MethodGet, "/api/logbook/"+url.PathEscape(start.UTC().Format(time.RFC3339))+"?"+query.Encode(), nil, &lines); err != nil {
		return out, err
	}
	for _, l := range lines {
		if l.EntityID == "" || !m.AllowedCtx(ctx, l.EntityID) {
			out.Hidden = true
			continue
		}
		out.Entries = append(out.Entries, LogbookEntry{When: l.When, Entity: l.EntityID, Name: l.Name, Message: l.Message, State: l.State})
	}
	return out, nil
}
