package dnsprovider

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
)

// --- DNS failover ---

// IPv64FailoverModes and the other enums of a policy.
var (
	IPv64FailoverModes   = []string{"auto_failover", "auto_failover_and_fallback", "manual"}
	IPv64FailoverLinking = []string{"and", "or"}
	IPv64FailoverStates  = []string{"pending", "active", "failed_over", "paused", "failover_requested", "fallback_requested"}
)

// IPv64Policy is a DNS failover policy. Monitor healthchecks are IDs.
type IPv64Policy struct {
	ID                   string     `json:"id"`
	Name                 string     `json:"name"`
	Status               string     `json:"status"`
	Manual               bool       `json:"manual"`
	Mode                 string     `json:"mode"`
	FallbackRequiresAll  bool       `json:"fallback_requires_all"`
	Linking              string     `json:"linking"`
	NotificationsEnabled bool       `json:"notifications_enabled"`
	Integrations         []int      `json:"integrations"`
	MonitorsDown         int        `json:"monitors_down"`
	LastCheckAt          *time.Time `json:"last_check_at"`
	Monitors             []struct {
		Healthcheck       string  `json:"healthcheck"`
		Name              string  `json:"name"`
		Negate            bool    `json:"negate"`
		HealthcheckStatus *string `json:"healthcheck_status"`
	} `json:"monitors"`
	Rules []struct {
		Record     string  `json:"record"`
		FQDN       *string `json:"fqdn"`
		RecordType string  `json:"record_type"`
		State      string  `json:"state"`
		Target     struct {
			Kind        string `json:"kind"`
			IP          string `json:"ip,omitempty"`
			Record      string `json:"record,omitempty"`
			OfflinePage string `json:"offline_page,omitempty"`
		} `json:"target"`
	} `json:"rules"`
}

// hide turns the healthcheck tokens of the monitors into IDs.
func (p *IPv64Policy) hide() {
	for i := range p.Monitors {
		p.Monitors[i].Healthcheck = HealthcheckID(p.Monitors[i].Healthcheck)
	}
	if p.Integrations == nil {
		p.Integrations = []int{}
	}
}

// MonitorTokens are the healthcheck tokens of the monitors (for titand).
func (c *IPv64) policy(ctx context.Context, method, path string, body any) (IPv64Policy, []string, error) {
	var p IPv64Policy
	if err := c.call(ctx, method, path, nil, body, &p); err != nil {
		return p, nil, err
	}
	tokens := make([]string, len(p.Monitors))
	for i, m := range p.Monitors {
		tokens[i] = m.Healthcheck
	}
	p.hide()
	return p, tokens, nil
}

// PolicyInput is the body of creating or changing a policy.
type PolicyInput struct {
	Name                 *string `json:"name,omitempty"`
	Mode                 *string `json:"mode,omitempty"`
	FallbackRequiresAll  *bool   `json:"fallback_requires_all,omitempty"`
	Linking              *string `json:"linking,omitempty"`
	NotificationsEnabled *bool   `json:"notifications_enabled,omitempty"`
	Integrations         *[]int  `json:"integrations,omitempty"`
}

// Validate checks the enums.
func (in PolicyInput) Validate() error {
	if in.Name != nil && (strings.TrimSpace(*in.Name) == "" || len(*in.Name) > 100) {
		return invalid("name")
	}
	if in.Mode != nil && !slices.Contains(IPv64FailoverModes, *in.Mode) {
		return invalid("mode")
	}
	if in.Linking != nil && !slices.Contains(IPv64FailoverLinking, *in.Linking) {
		return invalid("linking")
	}
	return nil
}

// Policies lists the failover policies.
func (c *IPv64) Policies(ctx context.Context) ([]IPv64Policy, error) {
	list := []IPv64Policy{}
	if err := c.call(ctx, http.MethodGet, "/failover/policies", nil, nil, &list); err != nil {
		return nil, err
	}
	for i := range list {
		list[i].hide()
	}
	return list, nil
}

// CreatePolicy makes a policy (monitors and rules come separately).
func (c *IPv64) CreatePolicy(ctx context.Context, in PolicyInput) (IPv64Policy, error) {
	if err := in.Validate(); err != nil {
		return IPv64Policy{}, err
	}
	p, _, err := c.policy(ctx, http.MethodPost, "/failover/policies", in)
	return p, err
}

// ChangePolicy changes the given fields of a policy.
func (c *IPv64) ChangePolicy(ctx context.Context, id string, in PolicyInput) (IPv64Policy, error) {
	if !validRef(id) {
		return IPv64Policy{}, invalid("policy")
	}
	if err := in.Validate(); err != nil {
		return IPv64Policy{}, err
	}
	p, _, err := c.policy(ctx, http.MethodPatch, "/failover/policies/"+url.PathEscape(id), in)
	return p, err
}

// DeletePolicy removes a policy (not while it has failed over).
func (c *IPv64) DeletePolicy(ctx context.Context, id string) error {
	if !validRef(id) {
		return invalid("policy")
	}
	return c.call(ctx, http.MethodDelete, "/failover/policies/"+url.PathEscape(id), nil, nil, nil)
}

// PolicyAction is pause, resume, failover or fallback (the last two are
// answered with 202 and carried out in the background).
func (c *IPv64) PolicyAction(ctx context.Context, id, action string) (IPv64Policy, error) {
	if !validRef(id) || !slices.Contains([]string{"pause", "resume", "failover", "fallback"}, action) {
		return IPv64Policy{}, invalid("policy action")
	}
	p, _, err := c.policy(ctx, http.MethodPost, "/failover/policies/"+url.PathEscape(id)+"/"+action, nil)
	return p, err
}

// AddMonitor makes a healthcheck (by token) a trigger of the policy.
func (c *IPv64) AddMonitor(ctx context.Context, id, healthcheck string, negate bool) (IPv64Policy, error) {
	if !validRef(id) || healthcheck == "" {
		return IPv64Policy{}, invalid("monitor")
	}
	p, _, err := c.policy(ctx, http.MethodPost, "/failover/policies/"+url.PathEscape(id)+"/monitors", map[string]any{"healthcheck": healthcheck, "negate": negate})
	return p, err
}

// RemoveMonitor takes a healthcheck (by token) off the policy.
func (c *IPv64) RemoveMonitor(ctx context.Context, id, healthcheck string) error {
	if !validRef(id) || healthcheck == "" {
		return invalid("monitor")
	}
	return c.call(ctx, http.MethodDelete, "/failover/policies/"+url.PathEscape(id)+"/monitors/"+url.PathEscape(healthcheck), nil, nil, nil)
}

// RuleInput protects an A or AAAA record; exactly one target is set.
type RuleInput struct {
	Record       string `json:"record"`
	TargetIP     string `json:"target_ip,omitempty"`
	TargetRecord string `json:"target_record,omitempty"`
	OfflinePage  string `json:"offline_page,omitempty"`
}

// Validate checks that the rule names one target.
func (in RuleInput) Validate() error {
	if !validRef(in.Record) {
		return invalid("record")
	}
	set := 0
	for _, v := range []string{in.TargetIP, in.TargetRecord, in.OfflinePage} {
		if v != "" {
			set++
		}
	}
	if set != 1 {
		return invalid("exactly one of target_ip, target_record and offline_page")
	}
	if in.TargetIP != "" {
		if _, err := netip.ParseAddr(in.TargetIP); err != nil {
			return invalid("target_ip")
		}
	}
	if in.TargetRecord != "" && !validRef(in.TargetRecord) {
		return invalid("target_record")
	}
	if in.OfflinePage != "" && in.OfflinePage != "default" && !validRef(in.OfflinePage) {
		return invalid("offline_page")
	}
	return nil
}

// AddRule adds a protected record to the policy.
func (c *IPv64) AddRule(ctx context.Context, id string, in RuleInput) (IPv64Policy, error) {
	if !validRef(id) {
		return IPv64Policy{}, invalid("policy")
	}
	if err := in.Validate(); err != nil {
		return IPv64Policy{}, err
	}
	p, _, err := c.policy(ctx, http.MethodPost, "/failover/policies/"+url.PathEscape(id)+"/rules", in)
	return p, err
}

// RemoveRule takes a record off the policy.
func (c *IPv64) RemoveRule(ctx context.Context, id, record string) error {
	if !validRef(id) || !validRef(record) {
		return invalid("rule")
	}
	return c.call(ctx, http.MethodDelete, "/failover/policies/"+url.PathEscape(id)+"/rules/"+url.PathEscape(record), nil, nil, nil)
}

// PolicyMonitorTokens returns the healthcheck tokens of a policy's monitors.
func (c *IPv64) PolicyMonitorTokens(ctx context.Context, id string) ([]string, error) {
	_, tokens, err := c.policy(ctx, http.MethodGet, "/failover/policies/"+url.PathEscape(id), nil)
	return tokens, err
}
