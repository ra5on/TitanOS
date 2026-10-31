package dnsprovider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
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

// IPv64HealthcheckTypes are the kinds of checks.
var IPv64HealthcheckTypes = []string{"push", "icmp", "tcp", "http", "keyword"}

// IPv64HealthcheckStates are the states of a check and of its events.
var IPv64HealthcheckStates = []string{"new", "up", "paused", "warning", "down", "stabilizing"}

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

// Validate checks the fields against the description; create needs name.
func (in HealthcheckInput) Validate(create bool) error {
	if create && (in.Name == nil || strings.TrimSpace(*in.Name) == "") {
		return invalid("name is required")
	}
	if in.Name != nil && (len(*in.Name) > 100 || strings.ContainsAny(*in.Name, "\r\n")) {
		return invalid("name")
	}
	kind := ""
	if in.Type != nil {
		kind = *in.Type
		if !slices.Contains(IPv64HealthcheckTypes, kind) {
			return invalid("type is one of %s", strings.Join(IPv64HealthcheckTypes, ", "))
		}
	}
	if in.Integrations != nil {
		if len(*in.Integrations) > 50 {
			return invalid("integrations")
		}
		for _, id := range *in.Integrations {
			if id < 0 {
				return invalid("integrations")
			}
		}
	}
	for _, d := range []*int{in.PeriodSeconds, in.GraceSeconds} {
		if d != nil && (*d < 60 || *d%60 != 0) {
			return invalid("period and grace are whole minutes, hours or days, at least 60 seconds")
		}
	}
	if in.Port != nil && (*in.Port < 1 || *in.Port > 65535) {
		return invalid("port is 1 to 65535")
	}
	if in.IntervalSeconds != nil && (*in.IntervalSeconds < 1 || *in.IntervalSeconds > 86400) {
		return invalid("interval_seconds is 1 to 86400")
	}
	if in.RetryIntervalSeconds != nil && (*in.RetryIntervalSeconds < 10 || *in.RetryIntervalSeconds > 86400) {
		return invalid("retry_interval_seconds is 10 to 86400")
	}
	if in.FailuresBeforeAlarm != nil && (*in.FailuresBeforeAlarm < 1 || *in.FailuresBeforeAlarm > 60) {
		return invalid("failures_before_alarm is 1 to 60")
	}
	if in.Target != nil && *in.Target != "" && !ValidHost(strings.ToLower(*in.Target)) {
		if _, err := netip.ParseAddr(*in.Target); err != nil {
			return invalid("target is a host name or an IP address")
		}
	}
	if in.URL != nil && *in.URL != "" && !httpURL(*in.URL) {
		return invalid("url must begin with http:// or https://")
	}
	if in.Keyword != nil && len(*in.Keyword) > 200 {
		return invalid("keyword")
	}
	if create {
		switch kind {
		case "icmp", "tcp":
			if in.Target == nil || *in.Target == "" {
				return invalid("target is required for %s", kind)
			}
			if kind == "tcp" && in.Port == nil {
				return invalid("port is required for tcp")
			}
		case "http", "keyword":
			if in.URL == nil || *in.URL == "" {
				return invalid("url is required for %s", kind)
			}
			if kind == "keyword" && (in.Keyword == nil || *in.Keyword == "") {
				return invalid("keyword is required")
			}
		}
	}
	return nil
}

func httpURL(text string) bool {
	u, err := url.Parse(text)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && len(text) <= 2048
}

// Checks lists all healthchecks.
func (c *IPv64) Checks(ctx context.Context) ([]IPv64Check, error) {
	return c.checks(ctx, nil)
}

// Check reads one check with its last 20 events.
func (c *IPv64) Check(ctx context.Context, token string) (IPv64Check, error) {
	var h IPv64Check
	err := c.call(ctx, http.MethodGet, "/healthchecks/"+url.PathEscape(token), nil, nil, &h)
	return h, err
}

// CreateCheck makes a check of any type.
func (c *IPv64) CreateCheck(ctx context.Context, in HealthcheckInput) (IPv64Check, error) {
	if in.Type == nil {
		push := "push"
		in.Type = &push
	}
	if err := in.Validate(true); err != nil {
		return IPv64Check{}, err
	}
	var h IPv64Check
	err := c.call(ctx, http.MethodPost, "/healthchecks", nil, in, &h)
	return h, err
}

// ChangeCheck changes the given fields of a check.
func (c *IPv64) ChangeCheck(ctx context.Context, token string, in HealthcheckInput) (IPv64Check, error) {
	if err := in.Validate(false); err != nil {
		return IPv64Check{}, err
	}
	var h IPv64Check
	err := c.call(ctx, http.MethodPatch, "/healthchecks/"+url.PathEscape(token), nil, in, &h)
	return h, err
}

// CheckAction is pause, resume or rotate-token of a check.
func (c *IPv64) CheckAction(ctx context.Context, token, action string) (IPv64Check, error) {
	if !slices.Contains([]string{"pause", "resume", "rotate-token"}, action) {
		return IPv64Check{}, invalid("action")
	}
	var h IPv64Check
	err := c.call(ctx, http.MethodPost, "/healthchecks/"+url.PathEscape(token)+"/"+action, nil, nil, &h)
	return h, err
}

// IPv64EventPage is a page of events.
type IPv64EventPage struct {
	Events     []IPv64Event `json:"events"`
	HasMore    bool         `json:"has_more"`
	NextBefore *int64       `json:"next_before"`
}

// Events reads events of a check, newest first; before is the cursor.
func (c *IPv64) Events(ctx context.Context, token string, limit int, before int64) (IPv64EventPage, error) {
	if limit < 1 || limit > 1000 {
		return IPv64EventPage{}, invalid("limit is 1 to 1000")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if before > 0 {
		query.Set("before", strconv.FormatInt(before, 10))
	}
	answer, err := c.Do(ctx, http.MethodGet, "/healthchecks/"+url.PathEscape(token)+"/events", query, nil)
	if err != nil {
		return IPv64EventPage{}, err
	}
	page := IPv64EventPage{Events: []IPv64Event{}}
	if err := decodeData(answer.Data, &page.Events); err != nil {
		return IPv64EventPage{}, err
	}
	var meta struct {
		HasMore    bool   `json:"has_more"`
		NextBefore *int64 `json:"next_before"`
	}
	if len(answer.Meta) > 0 {
		_ = json.Unmarshal(answer.Meta, &meta) // a value that cannot be read leaves the zero value, which the code handles
	}
	page.HasMore, page.NextBefore = meta.HasMore, meta.NextBefore
	return page, nil
}

// IPv64Incident is a down phase of a check.
type IPv64Incident struct {
	ID        int64      `json:"id"`
	Status    string     `json:"status"`
	CreatedAt *time.Time `json:"created_at"`
	ClosedAt  *time.Time `json:"closed_at"`
	Events    []struct {
		Time    *time.Time `json:"time"`
		Kind    string     `json:"kind"`
		Message string     `json:"message"`
	} `json:"events"`
}

// Incidents reads the incidents of a check.
func (c *IPv64) Incidents(ctx context.Context, token string, limit int) ([]IPv64Incident, error) {
	if limit < 1 || limit > 500 {
		return nil, invalid("limit is 1 to 500")
	}
	list := []IPv64Incident{}
	err := c.call(ctx, http.MethodGet, "/healthchecks/"+url.PathEscape(token)+"/incidents", url.Values{"limit": {strconv.Itoa(limit)}}, nil, &list)
	return list, err
}

// IPv64StatsRanges are the ranges of the statistics.
var IPv64StatsRanges = []string{"1h", "24h", "7d", "30d"}

// IPv64Stats is availability and latency of a check over a range.
type IPv64Stats struct {
	Range               string     `json:"range"`
	From                *time.Time `json:"from"`
	To                  *time.Time `json:"to"`
	Measurements        int        `json:"measurements"`
	AvailabilityPercent *float64   `json:"availability_percent"`
	LatencyMS           struct {
		Avg *float64 `json:"avg"`
		Min *float64 `json:"min"`
		Max *float64 `json:"max"`
	} `json:"latency_ms"`
	BucketSeconds int `json:"bucket_seconds"`
	Series        []struct {
		Time                time.Time `json:"time"`
		Measurements        int       `json:"measurements"`
		AvailabilityPercent float64   `json:"availability_percent"`
		LatencyAvgMS        *float64  `json:"latency_avg_ms"`
		LatencyMaxMS        *float64  `json:"latency_max_ms"`
	} `json:"series"`
}

// Stats reads the statistics of a check.
func (c *IPv64) Stats(ctx context.Context, token, rangeName string) (IPv64Stats, error) {
	if !slices.Contains(IPv64StatsRanges, rangeName) {
		return IPv64Stats{}, invalid("range is one of 1h, 24h, 7d, 30d")
	}
	var s IPv64Stats
	err := c.call(ctx, http.MethodGet, "/healthchecks/"+url.PathEscape(token)+"/stats", url.Values{"range": {rangeName}}, nil, &s)
	return s, err
}
