package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

// What the KI-Agent of TitanOS needs beyond the tools of MCP (PLAN 44):
// long-term statistics (more than the 24 hours of the recorder) and saving
// automations and scripts through the configuration API that Home
// Assistant's own editor uses.

var configID = regexp.MustCompile(`^[A-Za-z0-9_-]{1,80}$`)

// ErrInvalidConfig is a configuration the agent may not send.
var ErrInvalidConfig = errors.New("homeassistant: invalid automation or script")

// Statistics returns the long-term statistics of entities, e.g. energy per
// day, between start and end with the period "5minute", "hour", "day",
// "week" or "month".
func (m *Manager) Statistics(ctx context.Context, ids []string, start, end time.Time, period string) (json.RawMessage, error) {
	for _, id := range ids {
		if !ValidEntity(id) {
			return nil, invalid("entity")
		}
	}
	switch period {
	case "5minute", "hour", "day", "week", "month":
	default:
		return nil, invalid("period")
	}
	return m.command(ctx, 30*time.Second, map[string]any{
		"type": "recorder/statistics_during_period", "statistic_ids": ids,
		"start_time": start.UTC().Format(time.RFC3339), "end_time": end.UTC().Format(time.RFC3339),
		"period": period, "types": []string{"mean", "min", "max", "sum", "change"},
	})
}

// Config reads an automation or script ("automation" or "script") by id;
// nil if there is none.
func (m *Manager) Config(ctx context.Context, kind, id string) (json.RawMessage, error) {
	if (kind != "automation" && kind != "script") || !configID.MatchString(id) {
		return nil, ErrInvalidConfig
	}
	var out json.RawMessage
	err := m.do(ctx, http.MethodGet, "/api/config/"+kind+"/config/"+id, nil, &out)
	if err != nil {
		// Home Assistant answers 404 for an id it does not know.
		if strings.Contains(err.Error(), "404") {
			return nil, nil
		}
		return nil, err
	}
	return out, nil
}

// SaveConfig creates or replaces an automation or script and reloads them.
func (m *Manager) SaveConfig(ctx context.Context, kind, id string, config map[string]any) error {
	if (kind != "automation" && kind != "script") || !configID.MatchString(id) || config == nil {
		return ErrInvalidConfig
	}
	if err := m.do(ctx, http.MethodPost, "/api/config/"+kind+"/config/"+id, config, nil); err != nil {
		return err
	}
	return m.do(ctx, http.MethodPost, "/api/services/"+kind+"/reload", map[string]any{}, nil)
}

// Release is a list of released domains and entities of its own (PLAN 44,
// decision 5.11.2026: the KI-Agent has its own list, apart from "Geräte für
// MCP freigeben"). A context carrying one is checked against it instead of
// the MCP release; the policies of MCP (read-only, service lists) do not
// apply then, the agent has rules of its own.
type Release struct {
	Domains  []string `json:"domains,omitempty"`
	Entities []string `json:"entities,omitempty"`
}

type releaseKey struct{}

// WithRelease returns a context whose calls to the first connection use r
// as the release; other connections release nothing.
func WithRelease(ctx context.Context, r Release) context.Context {
	return WithReleases(ctx, map[string]Release{PrimaryID: r})
}

// WithReleases returns a context whose calls use the release of their
// connection by its ID; a connection without one releases nothing.
func WithReleases(ctx context.Context, releases map[string]Release) context.Context {
	return context.WithValue(ctx, releaseKey{}, releases)
}

// releaseOf is the release of the context for connection id.
func releaseOf(ctx context.Context, id string) (Release, bool) {
	releases, ok := ctx.Value(releaseKey{}).(map[string]Release)
	if !ok {
		return Release{}, false
	}
	if id == "" {
		id = PrimaryID
	}
	return releases[id], true
}

func (r Release) allows(entity string) bool {
	if !ValidEntity(entity) {
		return false
	}
	if _, known := domainLevels[domainOf(entity)]; !known {
		return false
	}
	return slices.Contains(r.Domains, domainOf(entity)) || slices.Contains(r.Entities, entity)
}

// AllowedCtx is Allowed with the release of the context, if it has one.
func (m *Manager) AllowedCtx(ctx context.Context, entity string) bool {
	if r, ok := releaseOf(ctx, m.id); ok {
		return r.allows(entity)
	}
	return m.Allowed(entity)
}

// CanCallCtx is CanCall with the release of the context, if it has one.
func (m *Manager) CanCallCtx(ctx context.Context, entity, service string) bool {
	if r, ok := releaseOf(ctx, m.id); ok {
		return r.allows(entity)
	}
	return m.CanCall(entity, service)
}

// Areas maps entity IDs to the name of their area (room).
func (m *Manager) Areas(ctx context.Context) (map[string]string, error) {
	return m.areas(ctx)
}
