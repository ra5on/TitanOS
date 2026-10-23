package homeassistant

import (
	"slices"
	"strings"
)

// Policies narrow existing releases; they never release an entity themselves.
// Nil services inherits the domain policy, an empty list permits no actions.
type EntityPolicy struct {
	Aliases     []string `json:"aliases,omitempty"`
	ReadOnly    bool     `json:"readOnly,omitempty"`
	Services    []string `json:"services"`
	Description string   `json:"description,omitempty"`
}

func clonePolicies(in map[string]EntityPolicy) map[string]EntityPolicy {
	if in == nil {
		return nil
	}
	out := make(map[string]EntityPolicy, len(in))
	for id, p := range in {
		p.Aliases = slices.Clone(p.Aliases)
		p.Services = slices.Clone(p.Services)
		out[id] = p
	}
	return out
}

func validatePolicies(policies map[string]EntityPolicy) error {
	if len(policies) > 500 {
		return invalid("at most 500 entity policies")
	}
	for id, p := range policies {
		if !ValidEntity(id) {
			return invalid("policy entity")
		}
		if _, ok := domainLevels[domainOf(id)]; !ok {
			return ErrNotAllowed
		}
		if len(p.Aliases) > 20 || len(p.Services) > 50 || len(p.Description) > 1000 {
			return invalid("policy too large")
		}
		for _, alias := range p.Aliases {
			if strings.TrimSpace(alias) == "" || len(alias) > 120 {
				return invalid("alias")
			}
		}
		for _, service := range p.Services {
			if !ValidService(service) {
				return invalid("policy service")
			}
		}
	}
	return nil
}

func callAllowed(s Settings, entity, service string) bool {
	if !allowed(s, entity) {
		return false
	}
	p, ok := s.Policies[entity]
	return !ok || !p.ReadOnly && (p.Services == nil || slices.Contains(p.Services, service))
}

func (m *Manager) CanCall(entity, service string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return callAllowed(m.settings, entity, service)
}

func (m *Manager) Policy(entity string) EntityPolicy {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.settings.Policies[entity]
	p.Aliases, p.Services = slices.Clone(p.Aliases), slices.Clone(p.Services)
	return p
}
