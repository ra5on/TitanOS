package homeassistant

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
)

// Capabilities reports the server's actual service fields and current entity
// features. A domain service existing does not prove a device supports it.
func (m *Manager) Capabilities(ctx context.Context, entity string) (any, error) {
	if !m.Allowed(entity) {
		return nil, ErrNotAllowed
	}
	state, err := m.State(ctx, entity)
	if err != nil {
		return nil, err
	}
	var domains []struct {
		Domain   string                     `json:"domain"`
		Services map[string]json.RawMessage `json:"services"`
	}
	if err = m.do(ctx, http.MethodGet, "/api/services", nil, &domains); err != nil {
		return nil, err
	}
	services := []map[string]any{}
	for _, domain := range domains {
		if domain.Domain != domainOf(entity) {
			continue
		}
		names := []string{}
		for name := range domain.Services {
			if m.CanCall(entity, name) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			services = append(services, map[string]any{"service": name, "schema": domain.Services[name]})
		}
	}
	return map[string]any{"entity": state, "policy": m.Policy(entity), "services": services, "notice": "Service schemas describe domain capabilities. Check supported_features, supported_color_modes, min/max and current availability before choosing parameters."}, nil
}
