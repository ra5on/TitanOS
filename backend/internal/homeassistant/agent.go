package homeassistant

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"slices"
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
		var status *StatusError
		if errors.As(err, &status) && status.Code == http.StatusNotFound {
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
//
// All releases every entity of the connection (owner 15.11.2026: a way to
// let the agent see everything without picking each device). A single
// entity can be released whatever its domain; it can then be read. What
// can be called stays the domains titand knows services for (Level), and
// the agent's rules still decide whether it asks first.
type Release struct {
	All      bool     `json:"all,omitempty"`
	Domains  []string `json:"domains,omitempty"`
	Entities []string `json:"entities,omitempty"`
}

// agentOnlyDomains are kinds of entities that only the KI-Agent can be
// given, to look at (1.09, owner 17 Nov 2026: "wieviele Automationen habe
// ich" answered 0, because automations could not be released under
// "Auswahl"). Calls there stay impossible (no Level); saving automations is
// the tool ha_automation_save with its own right.
var agentOnlyDomains = []string{"automation"}

// AgentDomains are the domains the agent's release can list: those of MCP
// and the agent's own.
func AgentDomains() []string {
	return slices.Sorted(slices.Values(append(Domains(), agentOnlyDomains...)))
}

// releaseJSON is a release as it is written. Domains that versions before
// 1.09 do not know go to extraDomains: the previous app reads the file
// unchanged and can still save its settings (it would refuse an unknown
// domain); it only drops the extra ones (like extraGroups of MQTT).
type releaseJSON struct {
	All          bool     `json:"all,omitempty"`
	Domains      []string `json:"domains,omitempty"`
	ExtraDomains []string `json:"extraDomains,omitempty"`
	Entities     []string `json:"entities,omitempty"`
}

// MarshalJSON writes the domains known before 1.09 under domains, the
// others under extraDomains.
func (r Release) MarshalJSON() ([]byte, error) {
	out := releaseJSON{All: r.All, Entities: r.Entities}
	for _, d := range r.Domains {
		if slices.Contains(agentOnlyDomains, d) {
			out.ExtraDomains = append(out.ExtraDomains, d)
		} else {
			out.Domains = append(out.Domains, d)
		}
	}
	return json.Marshal(out)
}

// UnmarshalJSON takes both lists into Domains.
func (r *Release) UnmarshalJSON(data []byte) error {
	var in releaseJSON
	if err := json.Unmarshal(data, &in); err != nil {
		return err
	}
	*r = Release{All: in.All, Entities: in.Entities, Domains: in.Domains}
	for _, d := range in.ExtraDomains {
		if !slices.Contains(r.Domains, d) {
			r.Domains = append(r.Domains, d)
		}
	}
	return nil
}

// Empty reports whether the list releases nothing at all.
func (r Release) Empty() bool { return !r.All && len(r.Domains) == 0 && len(r.Entities) == 0 }

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
	return r.All || slices.Contains(r.Domains, domainOf(entity)) || slices.Contains(r.Entities, entity)
}

// DomainReleased reports whether every entity of a domain is visible:
// released as a whole (or "Alles") in the agent's own list, or among the
// domains released to MCP. Single released entities do not count; the
// rest of their domain stays hidden.
func (m *Manager) DomainReleased(ctx context.Context, domain string) bool {
	if r, ok := releaseOf(ctx, m.id); ok {
		return r.All || slices.Contains(r.Domains, domain)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, known := domainLevels[domain]
	return known && slices.Contains(m.settings.Domains, domain)
}

// OwnRelease reports whether the context carries the agent's own release
// list (rather than the release to MCP).
func (m *Manager) OwnRelease(ctx context.Context) bool {
	_, ok := releaseOf(ctx, m.id)
	return ok
}

// ReleasesNothing reports whether the context carries a release list of
// its own for this connection and that list is empty: then no entity is
// visible, and the answer should say where to change that.
func (m *Manager) ReleasesNothing(ctx context.Context) bool {
	r, ok := releaseOf(ctx, m.id)
	return ok && r.Empty()
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

// PolicyCtx is Policy, except for a context with a release of its own (the
// agent): the policies of MCP do not apply there.
func (m *Manager) PolicyCtx(ctx context.Context, entity string) EntityPolicy {
	if _, ok := releaseOf(ctx, m.id); ok {
		return EntityPolicy{}
	}
	return m.Policy(entity)
}

// Areas maps entity IDs to the name of their area (room).
func (m *Manager) Areas(ctx context.Context) (map[string]string, error) {
	return m.areas(ctx)
}
