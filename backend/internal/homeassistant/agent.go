package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
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
