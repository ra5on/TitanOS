package ipv64

import (
	"context"
	"time"
)

// PollEvery is the shortest time between two looks at the account.
const PollEvery = 5 * time.Minute

// PollStatus tells when the account was looked at.
type PollStatus struct {
	Last  *time.Time `json:"last,omitempty"`
	Error string     `json:"error,omitempty"`
}

// pollState is RAM only: the states seen at the last look. A state seen
// for the first time sets the baseline and is not reported.
type pollState struct {
	last     time.Time
	err      string
	policies map[string]string
	checks   map[string]string
	traffic  map[string]bool
	seeded   bool
}

func (m *Manager) pollStatus() PollStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := PollStatus{Error: m.poll.err}
	if !m.poll.last.IsZero() {
		last := m.poll.last
		st.Last = &last
	}
	return st
}

// pollOnce looks at the account if notifications are on and the last look
// is at least PollEvery ago.
func (m *Manager) pollOnce(ctx context.Context) {
	m.mu.Lock()
	n := m.current.Notify
	due := m.loaded && n.Enabled && (n.Failover || n.CDNTraffic || n.Healthchecks) && m.now().Sub(m.poll.last) >= PollEvery
	if !n.Enabled {
		// Switched off: forget the baseline, so that switching on does
		// not report what happened meanwhile.
		m.poll = pollState{last: m.poll.last}
	}
	m.mu.Unlock()
	if !due {
		return
	}
	client, err := m.Client()
	if err != nil {
		m.mu.Lock()
		m.poll.err, m.poll.last = err.Error(), m.now()
		m.mu.Unlock()
		return
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	var events [][3]string
	policies, checks, traffic := map[string]string{}, map[string]string{}, map[string]bool{}
	names := map[string]string{}
	var firstErr error
	if n.Failover {
		list, err := client.Policies(ctx)
		if err != nil {
			firstErr = err
		}
		for _, p := range list {
			policies[p.ID], names["p"+p.ID] = p.Status, p.Name
		}
	}
	if n.Healthchecks {
		list, err := client.Checks(ctx)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		for _, c := range list {
			checks[c.ID], names["c"+c.ID] = c.Status, c.Name
		}
	}
	if n.CDNTraffic {
		list, err := client.CDNs(ctx)
		if err != nil && firstErr == nil {
			firstErr = err
		}
		for _, c := range list {
			name := c.ID
			if c.Domain != nil {
				name = *c.Domain
			}
			traffic[name] = c.Portmapper.State == "suspended_traffic_limit"
		}
		if me, err := client.Me(ctx); err == nil {
			if u, ok := me.Usage["cdn_traffic_gb"]; ok && u.Limit != nil && u.Used >= float64(*u.Limit) {
				traffic["account"] = true
			} else {
				traffic["account"] = false
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.poll.last = m.now()
	m.poll.err = ""
	if firstErr != nil {
		m.poll.err = firstErr.Error()
	}
	if m.poll.seeded {
		for id, state := range policies {
			before, seen := m.poll.policies[id]
			if seen && before != state && (state == "failed_over" || (before == "failed_over" && state == "active")) {
				kind := "ipv64_failover"
				if state == "active" {
					kind = "ipv64_fallback"
				}
				events = append(events, [3]string{"warning", kind, names["p"+id]})
			}
		}
		for id, state := range checks {
			before, seen := m.poll.checks[id]
			if seen && before != state && state == "down" {
				events = append(events, [3]string{"error", "ipv64_check_down", names["c"+id]})
			}
		}
		for name, limited := range traffic {
			if limited && !m.poll.traffic[name] {
				subject := name
				if name == "account" {
					subject = ""
				}
				events = append(events, [3]string{"warning", "ipv64_cdn_traffic", subject})
			}
		}
	}
	// Keep the old baseline of what could not be read this time.
	if n.Failover && (firstErr == nil || len(policies) > 0) {
		m.poll.policies = policies
	}
	if n.Healthchecks && (firstErr == nil || len(checks) > 0) {
		m.poll.checks = checks
	}
	if n.CDNTraffic && (firstErr == nil || len(traffic) > 0) {
		m.poll.traffic = traffic
	}
	m.poll.seeded = true
	if m.Notify != nil {
		for _, e := range events {
			go m.Notify(e[0], e[1], e[2])
		}
	}
}
