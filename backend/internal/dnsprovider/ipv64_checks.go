package dnsprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// --- healthchecks ---

// IPv64Healthcheck is a push healthcheck, without its token.
type IPv64Healthcheck struct {
	// ID identifies the check towards the web UI: a hash of the token.
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Status       string     `json:"status"`
	LastSeen     *time.Time `json:"lastSeenAt,omitempty"`
	ExpectedBy   *time.Time `json:"expectedBy,omitempty"`
	Period       int        `json:"periodSeconds"`
	Grace        int        `json:"graceSeconds"`
	Integrations []int      `json:"integrations"`
	token        string
}

// Token is the secret of the check; it never goes to the web UI.
func (h IPv64Healthcheck) Token() string { return h.token }

// HealthcheckID is the name the web UI knows a check by.
func HealthcheckID(token string) string { return shortHash(token) }

// push is the view of a check the DynDNS page and the heartbeat read.
func (h IPv64Check) push() IPv64Healthcheck {
	out := IPv64Healthcheck{ID: h.ID, Name: h.Name, Status: h.Status, LastSeen: h.LastSeenAt, Integrations: h.Notify.Integrations, token: h.token}
	if out.Integrations == nil {
		out.Integrations = []int{}
	}
	if h.Push != nil {
		out.Period, out.Grace, out.ExpectedBy = h.Push.PeriodSeconds, h.Push.GraceSeconds, h.Push.ExpectedBy
	}
	return out
}

// checks is GET /healthchecks with an optional filter.
func (c *IPv64) checks(ctx context.Context, query url.Values) ([]IPv64Check, error) {
	list := []IPv64Check{}
	err := c.call(ctx, http.MethodGet, "/healthchecks", query, nil, &list)
	return list, err
}

// Healthchecks lists the push healthchecks of the account.
func (c *IPv64) Healthchecks(ctx context.Context) ([]IPv64Healthcheck, error) {
	list, err := c.checks(ctx, url.Values{"type": {"push"}})
	if err != nil {
		return nil, err
	}
	out := []IPv64Healthcheck{}
	for _, h := range list {
		if h.Type == "" || h.Type == "push" {
			out = append(out, h.push())
		}
	}
	return out, nil
}

// Healthcheck reads one check as a push check.
func (c *IPv64) Healthcheck(ctx context.Context, token string) (IPv64Healthcheck, error) {
	h, err := c.Check(ctx, token)
	return h.push(), err
}

// CreateHealthcheck makes a push check; IPv64 alarms when no ping came for
// period plus grace. Integration 0 is the e-mail address of the account.
func (c *IPv64) CreateHealthcheck(ctx context.Context, name string, period, grace time.Duration, integrations []int) (IPv64Healthcheck, error) {
	if name == "" || len(name) > 100 || period < time.Minute || grace < time.Minute || period%time.Minute != 0 || grace%time.Minute != 0 {
		return IPv64Healthcheck{}, invalid("healthcheck")
	}
	push, on := "push", true
	periodSeconds, graceSeconds := int(period.Seconds()), int(grace.Seconds())
	in := HealthcheckInput{Name: &name, Type: &push, PeriodSeconds: &periodSeconds, GraceSeconds: &graceSeconds,
		Integrations: &integrations, NotifyOnDown: &on, NotifyOnUp: &on}
	var h IPv64Check
	err := c.call(ctx, http.MethodPost, "/healthchecks", nil, in, &h)
	return h.push(), err
}

// PauseHealthcheck and ResumeHealthcheck stop and start the alarms.
func (c *IPv64) PauseHealthcheck(ctx context.Context, token string) error {
	return c.call(ctx, http.MethodPost, "/healthchecks/"+url.PathEscape(token)+"/pause", nil, nil, nil)
}

func (c *IPv64) ResumeHealthcheck(ctx context.Context, token string) error {
	return c.call(ctx, http.MethodPost, "/healthchecks/"+url.PathEscape(token)+"/resume", nil, nil, nil)
}

// DeleteHealthcheck removes a check at IPv64.
func (c *IPv64) DeleteHealthcheck(ctx context.Context, token string) error {
	return c.call(ctx, http.MethodDelete, "/healthchecks/"+url.PathEscape(token), nil, nil, nil)
}

// TestNotification sends a test message to the integrations of a check
// (IPv64 allows one every 30 seconds).
func (c *IPv64) TestNotification(ctx context.Context, token string) error {
	return c.call(ctx, http.MethodPost, "/healthchecks/"+url.PathEscape(token)+"/test-notification", nil, nil, nil)
}

// Ping tells a healthcheck that the NAS is alive (GET /health).
func (c *IPv64) Ping(ctx context.Context, token string) error {
	if err := ipv64Allow(time.Now(), false); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/health", nil)
	if err != nil {
		return errors.New("IPv64: request could not be built")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	status, header, data, err := c.roundTrip(req, c.client)
	if err != nil {
		return err
	}
	switch {
	case status == http.StatusUnauthorized:
		return &APIError{Provider: "IPv64", Status: status, Message: "healthcheck token not accepted"}
	case status >= 300:
		return ipv64Error(status, header, data)
	}
	return nil
}

// IPv64Event is an event of a healthcheck.
type IPv64Event struct {
	ID        int64           `json:"id"`
	Time      time.Time       `json:"time"`
	Status    string          `json:"status"`
	Message   string          `json:"message"`
	LatencyMS *float64        `json:"latency_ms"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

// IPv64Check is a healthcheck of any type, without its token and push URL.
type IPv64Check struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Notify struct {
		OnDown       bool  `json:"on_down"`
		OnUp         bool  `json:"on_up"`
		Integrations []int `json:"integrations"`
	} `json:"notify"`
	CreatedAt   *time.Time `json:"created_at"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
	LastAlarmAt *time.Time `json:"last_alarm_at"`
	PingsTotal  int        `json:"pings_total"`
	FailedInRow int        `json:"failed_in_row"`
	Push        *struct {
		PeriodSeconds int        `json:"period_seconds"`
		GraceSeconds  int        `json:"grace_seconds"`
		ExpectedBy    *time.Time `json:"expected_by"`
	} `json:"push,omitempty"`
	Check *struct {
		Target               string     `json:"target"`
		PreferIPv4           bool       `json:"prefer_ipv4"`
		Port                 int        `json:"port"`
		URL                  string     `json:"url"`
		VerifyTLS            bool       `json:"verify_tls"`
		Keyword              string     `json:"keyword"`
		IntervalSeconds      int        `json:"interval_seconds"`
		RetryIntervalSeconds int        `json:"retry_interval_seconds"`
		FailuresBeforeAlarm  int        `json:"failures_before_alarm"`
		NextCheckAt          *time.Time `json:"next_check_at"`
	} `json:"check,omitempty"`
	Recent []IPv64Event `json:"recent,omitempty"`
	token  string
}

// Token is the secret of the check; it never goes to the web UI.
func (h IPv64Check) Token() string { return h.token }

func (h *IPv64Check) UnmarshalJSON(data []byte) error {
	type plain IPv64Check
	var wire struct {
		plain
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	*h = IPv64Check(wire.plain)
	h.token = wire.Token
	h.ID = HealthcheckID(wire.Token)
	if h.Notify.Integrations == nil {
		h.Notify.Integrations = []int{}
	}
	return nil
}

// HealthcheckInput is the body of creating or changing a check; nil fields
// are not sent (PATCH leaves them).
type HealthcheckInput struct {
	Name                 *string `json:"name,omitempty"`
	Type                 *string `json:"type,omitempty"`
	Integrations         *[]int  `json:"integrations,omitempty"`
	NotifyOnDown         *bool   `json:"notify_on_down,omitempty"`
	NotifyOnUp           *bool   `json:"notify_on_up,omitempty"`
	PeriodSeconds        *int    `json:"period_seconds,omitempty"`
	GraceSeconds         *int    `json:"grace_seconds,omitempty"`
	Target               *string `json:"target,omitempty"`
	PreferIPv4           *bool   `json:"prefer_ipv4,omitempty"`
	Port                 *int    `json:"port,omitempty"`
	URL                  *string `json:"url,omitempty"`
	VerifyTLS            *bool   `json:"verify_tls,omitempty"`
	Keyword              *string `json:"keyword,omitempty"`
	IntervalSeconds      *int    `json:"interval_seconds,omitempty"`
	RetryIntervalSeconds *int    `json:"retry_interval_seconds,omitempty"`
	FailuresBeforeAlarm  *int    `json:"failures_before_alarm,omitempty"`
}

// Check reads one check with its last 20 events.
func (c *IPv64) Check(ctx context.Context, token string) (IPv64Check, error) {
	var h IPv64Check
	err := c.call(ctx, http.MethodGet, "/healthchecks/"+url.PathEscape(token), nil, nil, &h)
	return h, err
}
