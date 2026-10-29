package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"mime/multipart"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "golang.org/x/image/webp"
)

// The rest of API v2 of IPv64.net, field by field after its OpenAPI
// description (https://ipv64.net/api/v2/openapi.json): account, log and API
// keys, domains, healthchecks with events and statistics, integrations,
// DNS failover, status and offline pages, CDN with reverse proxy and
// portmapper, Cloud Router with gateways and peers, and the list token of
// the v64 blocklist.
//
// The types carry the field names of the API (snake_case), so the web UI
// can be checked against the same description. What is secret stays here:
// healthcheck tokens become an ID (a hash, as for the dynamic DNS page),
// push URLs, domain update tokens, secrets of integrations and the WireGuard
// configuration of peers are never part of these types.

// --- account ---

// IPv64UsageLimit is one entry of usage in /me.
type IPv64UsageLimit struct {
	Used    float64 `json:"used"`
	Limit   *int    `json:"limit"`
	Limited *bool   `json:"limited,omitempty"`
}

// IPv64Me is GET /me without the e-mail address and the account ID.
type IPv64Me struct {
	Username     *string    `json:"username"`
	RegisteredAt *time.Time `json:"registered_at"`
	LastLoginAt  *time.Time `json:"last_login_at"`
	Plan         struct {
		ID         int        `json:"id"`
		Name       string     `json:"name"`
		ValidUntil *time.Time `json:"valid_until"`
	} `json:"plan"`
	Usage            map[string]IPv64UsageLimit `json:"usage"`
	LogRetentionDays int                        `json:"log_retention_days"`
	Auth             struct {
		Type    string `json:"type"`
		KeyID   *int64 `json:"key_id"`
		KeyName string `json:"key_name"`
	} `json:"auth"`
}

// Me reads the account, its plan and the use of its limits.
func (c *IPv64) Me(ctx context.Context) (IPv64Me, error) {
	var me IPv64Me
	err := c.call(ctx, http.MethodGet, "/me", nil, nil, &me)
	if me.Usage == nil {
		me.Usage = map[string]IPv64UsageLimit{}
	}
	return me, err
}

// IPv64LogEntry is an entry of the account log.
type IPv64LogEntry struct {
	ID       int64     `json:"id"`
	DomainID *int64    `json:"domain_id"`
	Title    string    `json:"title"`
	Message  string    `json:"message"`
	Time     time.Time `json:"time"`
}

// IPv64LogPage is one page of the log; NextBefore nil is the end.
type IPv64LogPage struct {
	Entries       []IPv64LogEntry `json:"entries"`
	NextBefore    *int64          `json:"next_before"`
	RetentionDays int             `json:"retention_days"`
}

// Logs reads the account log, newest first; before is the cursor (0 = from
// the newest), limit 1 to 500.
func (c *IPv64) Logs(ctx context.Context, limit int, before int64) (IPv64LogPage, error) {
	if limit < 1 || limit > 500 {
		return IPv64LogPage{}, invalid("limit is 1 to 500")
	}
	query := url.Values{"limit": {strconv.Itoa(limit)}}
	if before > 0 {
		query.Set("before", strconv.FormatInt(before, 10))
	}
	answer, err := c.Do(ctx, http.MethodGet, "/me/logs", query, nil)
	if err != nil {
		return IPv64LogPage{}, err
	}
	page := IPv64LogPage{Entries: []IPv64LogEntry{}}
	if err := decodeData(answer.Data, &page.Entries); err != nil {
		return IPv64LogPage{}, err
	}
	var meta struct {
		NextBefore    *int64 `json:"next_before"`
		RetentionDays int    `json:"retention_days"`
	}
	if len(answer.Meta) > 0 {
		json.Unmarshal(answer.Meta, &meta)
	}
	page.NextBefore, page.RetentionDays = meta.NextBefore, meta.RetentionDays
	return page, nil
}

// IPv64APIKey is an API key of the account; the key itself is never listed.
type IPv64APIKey struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	TokenHint string     `json:"token_hint"`
	CreatedAt *time.Time `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at"`
	Expired   bool       `json:"expired"`
	Uses      int        `json:"uses"`
}

// APIKeys lists the API keys of the account.
func (c *IPv64) APIKeys(ctx context.Context) ([]IPv64APIKey, error) {
	list := []IPv64APIKey{}
	err := c.call(ctx, http.MethodGet, "/api-keys", nil, nil, &list)
	return list, err
}

// MaxIPv64KeyLifetime is the longest validity POST /api-keys takes.
const MaxIPv64KeyLifetime = 315360000

// CreateAPIKey makes a key valid for expiresIn seconds (0 = unlimited) and
// returns it with the full key, which IPv64 shows this one time only.
func (c *IPv64) CreateAPIKey(ctx context.Context, name string, expiresIn int) (IPv64APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 || strings.ContainsAny(name, "\r\n") {
		return IPv64APIKey{}, "", invalid("name")
	}
	if expiresIn < 0 || expiresIn > MaxIPv64KeyLifetime {
		return IPv64APIKey{}, "", invalid("expires_in is 0 to %d seconds", MaxIPv64KeyLifetime)
	}
	var created struct {
		IPv64APIKey
		Token string `json:"token"`
	}
	err := c.call(ctx, http.MethodPost, "/api-keys", nil, map[string]any{"name": name, "expires_in": expiresIn}, &created)
	return created.IPv64APIKey, created.Token, err
}

// DeleteAPIKey removes a key (IPv64 refuses the key of the request itself).
func (c *IPv64) DeleteAPIKey(ctx context.Context, id int64) error {
	return c.call(ctx, http.MethodDelete, "/api-keys/"+strconv.FormatInt(id, 10), nil, nil, nil)
}

// --- domains ---

// IPv64DomainInfo is a domain with its records, without the update token.
type IPv64DomainInfo struct {
	IPv64Domain
	CreatedAt *time.Time        `json:"created_at"`
	UpdatedAt *time.Time        `json:"updated_at"`
	Records   []IPv64FullRecord `json:"records"`
}

// IPv64FullRecord is a record as GET /records/{record} gives it.
type IPv64FullRecord struct {
	ID         int64      `json:"id"`
	DomainID   int64      `json:"domain_id"`
	Domain     string     `json:"domain"`
	Prefix     string     `json:"prefix"`
	FQDN       string     `json:"fqdn"`
	Type       string     `json:"type"`
	Content    string     `json:"content"`
	TTL        int        `json:"ttl"`
	State      string     `json:"state"`
	PendingDNS bool       `json:"pending_dns"`
	Token      string     `json:"token"`
	UpdatedAt  *time.Time `json:"updated_at"`
}

// IPv64RecordStates are the states of a record.
var IPv64RecordStates = []string{"active", "inactive", "cdn_record", "cdn_proxied", "cdn_partial", "failover_record", "failover_replaced"}

type ipv64WireDomainInfo struct {
	ipv64WireDomain
	CreatedAt *time.Time        `json:"created_at"`
	UpdatedAt *time.Time        `json:"updated_at"`
	Records   []IPv64FullRecord `json:"records"`
}

func (w ipv64WireDomainInfo) public() IPv64DomainInfo {
	out := IPv64DomainInfo{IPv64Domain: w.ipv64WireDomain.public(), CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt, Records: w.Records}
	if out.Records == nil {
		out.Records = []IPv64FullRecord{}
	}
	return out
}

// Domain reads one domain with all its records.
func (c *IPv64) Domain(ctx context.Context, name string) (IPv64DomainInfo, error) {
	if !ValidHost(name) {
		return IPv64DomainInfo{}, invalid("domain")
	}
	var w ipv64WireDomainInfo
	err := c.call(ctx, http.MethodGet, "/domains/"+url.PathEscape(name), nil, nil, &w)
	return w.public(), err
}

// CreateOwnDomain adds a domain of the owner's own (the name servers
// ns1.ipv64.net and ns2.ipv64.net then go to the registrar).
func (c *IPv64) CreateOwnDomain(ctx context.Context, name string) (IPv64DomainInfo, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !ValidHost(name) || !strings.Contains(name, ".") {
		return IPv64DomainInfo{}, invalid("domain")
	}
	var w ipv64WireDomainInfo
	err := c.call(ctx, http.MethodPost, "/domains", nil, map[string]any{"name": name, "own": true}, &w)
	return w.public(), err
}

// DeleteDomain removes a domain with all its records.
func (c *IPv64) DeleteDomain(ctx context.Context, name string) error {
	if !ValidHost(name) {
		return invalid("domain")
	}
	return c.call(ctx, http.MethodDelete, "/domains/"+url.PathEscape(name), nil, nil, nil)
}

// RotateDomainToken makes a new update token; the old one stops working at
// once. It returns the old and the new token for those who keep one.
func (c *IPv64) RotateDomainToken(ctx context.Context, name string) (IPv64Domain, string, error) {
	if !ValidHost(name) {
		return IPv64Domain{}, "", invalid("domain")
	}
	var w ipv64WireDomain
	err := c.call(ctx, http.MethodPost, "/domains/"+url.PathEscape(name)+"/rotate-token", nil, nil, &w)
	return w.public(), w.UpdateToken, err
}

// DomainUpdateToken reads the current update token of a domain (only for
// titand itself, never for the web UI).
func (c *IPv64) DomainUpdateToken(ctx context.Context, name string) (string, error) {
	var w ipv64WireDomain
	err := c.call(ctx, http.MethodGet, "/domains/"+url.PathEscape(name), nil, nil, &w)
	return w.UpdateToken, err
}

// ResetDomain deletes all active records and sets the zone up anew.
func (c *IPv64) ResetDomain(ctx context.Context, name string) (IPv64DomainInfo, error) {
	if !ValidHost(name) {
		return IPv64DomainInfo{}, invalid("domain")
	}
	var w ipv64WireDomainInfo
	err := c.call(ctx, http.MethodPost, "/domains/"+url.PathEscape(name)+"/reset", nil, nil, &w)
	return w.public(), err
}

// CheckIPv6Prefix accepts a prefix like 2001:db8:1234:5600::/56 or "".
func CheckIPv6Prefix(text string) error {
	if text == "" {
		return nil
	}
	p, err := netip.ParsePrefix(text)
	if err != nil || !p.Addr().Is6() || p.Addr().Is4In6() {
		return invalid("ipv6_prefix is an IPv6 prefix like 2001:db8:1234:5600::/56")
	}
	return nil
}

// ChangeRecordFields is a PATCH of a record; nil fields stay.
type ChangeRecordFields struct {
	Type    *string `json:"type,omitempty"`
	Prefix  *string `json:"prefix,omitempty"`
	Content *string `json:"content,omitempty"`
}

// ChangeRecord changes a record. Only the IP of A/AAAA keeps ID and token.
func (c *IPv64) ChangeRecord(ctx context.Context, record string, current IPv64FullRecord, change ChangeRecordFields) (IPv64FullRecord, error) {
	if !validRef(record) {
		return IPv64FullRecord{}, invalid("record")
	}
	kind, prefix, content := current.Type, current.Prefix, current.Content
	if change.Type != nil {
		kind = *change.Type
	}
	if change.Prefix != nil {
		prefix = *change.Prefix
	}
	if change.Content != nil {
		content = *change.Content
	}
	if err := CheckRecord(kind, prefix, content); err != nil {
		return IPv64FullRecord{}, err
	}
	var r IPv64FullRecord
	err := c.call(ctx, http.MethodPatch, "/records/"+url.PathEscape(record), nil, change, &r)
	return r, err
}

// Record reads one record by ID or token.
func (c *IPv64) Record(ctx context.Context, record string) (IPv64FullRecord, error) {
	if !validRef(record) {
		return IPv64FullRecord{}, invalid("record")
	}
	var r IPv64FullRecord
	err := c.call(ctx, http.MethodGet, "/records/"+url.PathEscape(record), nil, nil, &r)
	return r, err
}

// FullRecords lists the records of a domain with their tokens.
func (c *IPv64) FullRecords(ctx context.Context, domain string) ([]IPv64FullRecord, error) {
	if !ValidHost(domain) {
		return nil, invalid("domain")
	}
	list := []IPv64FullRecord{}
	err := c.call(ctx, http.MethodGet, "/domains/"+url.PathEscape(domain)+"/records", nil, nil, &list)
	return list, err
}

// IPv64DynDNSResult is the answer of POST /domains/{domain}/dyndns.
type IPv64DynDNSResult struct {
	Domain  string `json:"domain"`
	Prefix  string `json:"prefix"`
	Changed bool   `json:"changed"`
	Results []struct {
		IP     string `json:"ip"`
		Type   string `json:"type"`
		Result string `json:"result"`
	} `json:"results"`
}

// UpdatePrefix sets A and AAAA of a prefix; without addresses IPv64 takes
// the one the request comes from. Missing records are created.
func (c *IPv64) UpdatePrefix(ctx context.Context, domain, prefix string, v4, v6 netip.Addr) (IPv64DynDNSResult, error) {
	if !ValidHost(domain) || (prefix != "" && !validName(prefix)) {
		return IPv64DynDNSResult{}, invalid("domain or prefix")
	}
	body := map[string]string{}
	if prefix != "" {
		body["prefix"] = prefix
	}
	if v4.IsValid() {
		body["ipv4"] = v4.Unmap().String()
	}
	if v6.IsValid() {
		body["ipv6"] = v6.String()
	}
	var result IPv64DynDNSResult
	err := c.call(ctx, http.MethodPost, "/domains/"+url.PathEscape(domain)+"/dyndns", nil, body, &result)
	return result, err
}

// --- healthchecks ---

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
	list := []IPv64Check{}
	err := c.call(ctx, http.MethodGet, "/healthchecks", nil, nil, &list)
	return list, err
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
		json.Unmarshal(answer.Meta, &meta)
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

// --- integrations ---

// IPv64IntegrationTypes are the kinds of integrations that can be created.
var IPv64IntegrationTypes = []string{"telegram", "gotify", "ntfy", "pushover", "discord", "webhook", "email", "sms", "app"}

// IPv64IntegrationInfo is an integration without its secrets: of options
// only the plain settings are kept.
type IPv64IntegrationInfo struct {
	ID         int            `json:"id"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Status     string         `json:"status"`
	Default    *bool          `json:"default"`
	Options    map[string]any `json:"options"`
	CreatedAt  *time.Time     `json:"created_at"`
	LastUsedAt *time.Time     `json:"last_used_at"`
	Uses       int            `json:"uses"`
}

// ipv64PlainOptions are the options of integrations that are no secrets.
var ipv64PlainOptions = []string{"priority", "priority_down", "priority_up", "device", "ping_user", "ping_group", "email", "number", "country_code", "chat_id"}

func (i *IPv64IntegrationInfo) strip() {
	kept := map[string]any{}
	for key, value := range i.Options {
		if slices.Contains(ipv64PlainOptions, key) {
			kept[key] = value
		}
	}
	i.Options = kept
}

// FullIntegrations lists the integrations, never with include_secrets.
func (c *IPv64) FullIntegrations(ctx context.Context) ([]IPv64IntegrationInfo, error) {
	list := []IPv64IntegrationInfo{}
	err := c.call(ctx, http.MethodGet, "/integrations", url.Values{"include_secrets": {"false"}}, nil, &list)
	for i := range list {
		list[i].strip()
	}
	return list, err
}

// IntegrationInput is the body of POST /integrations.
type IntegrationInput struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	Default      *bool  `json:"default,omitempty"`
	ChatID       string `json:"chat_id,omitempty"`
	APIToken     string `json:"api_token,omitempty"`
	ServerURL    string `json:"server_url,omitempty"`
	AppToken     string `json:"app_token,omitempty"`
	UserToken    string `json:"user_token,omitempty"`
	Device       string `json:"device,omitempty"`
	Priority     *int   `json:"priority,omitempty"`
	PriorityDown *int   `json:"priority_down,omitempty"`
	PriorityUp   *int   `json:"priority_up,omitempty"`
	WebhookURL   string `json:"webhook_url,omitempty"`
	PingUser     string `json:"ping_user,omitempty"`
	PingGroup    string `json:"ping_group,omitempty"`
	URLDown      string `json:"url_down,omitempty"`
	URLUp        string `json:"url_up,omitempty"`
	Email        string `json:"email,omitempty"`
	Number       string `json:"number,omitempty"`
	CountryCode  string `json:"country_code,omitempty"`
	DeviceToken  string `json:"device_token,omitempty"`
}

func inRange(v *int, low, high int) bool { return v == nil || (*v >= low && *v <= high) }

// Validate checks the fields each type needs (POST /integrations).
func (in IntegrationInput) Validate() error {
	if !slices.Contains(IPv64IntegrationTypes, in.Type) {
		return invalid("type")
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 || strings.ContainsAny(in.Name, "\r\n") {
		return invalid("name")
	}
	for _, text := range []string{in.ChatID, in.APIToken, in.AppToken, in.UserToken, in.Device, in.PingUser, in.PingGroup, in.Email, in.Number, in.CountryCode, in.DeviceToken} {
		if len(text) > 512 || strings.ContainsAny(text, "\r\n") {
			return invalid("field too long")
		}
	}
	need := func(ok bool, field string) error {
		if !ok {
			return invalid("%s is required for %s", field, in.Type)
		}
		return nil
	}
	urlOK := func(text string) bool { return text == "" || httpURL(text) }
	if !urlOK(in.ServerURL) || !urlOK(in.WebhookURL) || !urlOK(in.URLDown) || !urlOK(in.URLUp) {
		return invalid("URLs begin with http:// or https://")
	}
	switch in.Type {
	case "telegram":
		if err := need(in.ChatID != "", "chat_id"); err != nil {
			return err
		}
		return need(in.APIToken != "", "api_token")
	case "gotify":
		if err := need(in.ServerURL != "" && in.AppToken != "", "server_url and app_token"); err != nil {
			return err
		}
		if !inRange(in.Priority, 0, 10) {
			return invalid("priority is 0 to 10")
		}
	case "ntfy":
		if err := need(in.ServerURL != "", "server_url"); err != nil {
			return err
		}
		if !inRange(in.PriorityDown, 1, 5) || !inRange(in.PriorityUp, 1, 5) {
			return invalid("priorities are 1 to 5")
		}
	case "pushover":
		if err := need(in.UserToken != "" && in.AppToken != "", "user_token and app_token"); err != nil {
			return err
		}
		if !inRange(in.Priority, -2, 1) {
			return invalid("priority is -2 to 1")
		}
	case "discord":
		return need(in.WebhookURL != "", "webhook_url")
	case "webhook":
		return need(in.URLDown != "" || in.URLUp != "", "url_down or url_up")
	case "email":
		return need(strings.Count(in.Email, "@") == 1 && !strings.ContainsAny(in.Email, " <>"), "email")
	case "sms":
		if err := need(in.Number != "", "number"); err != nil {
			return err
		}
		if in.CountryCode != "" && in.CountryCode != "0049" {
			return invalid("country_code is 0049")
		}
	case "app":
		return need(in.DeviceToken != "", "device_token")
	}
	return nil
}

// CreateIntegration adds an integration; e-mail and SMS wait for a
// confirmation link.
func (c *IPv64) CreateIntegration(ctx context.Context, in IntegrationInput) (IPv64IntegrationInfo, error) {
	if in.Type == "sms" && in.CountryCode == "" {
		in.CountryCode = "0049"
	}
	if err := in.Validate(); err != nil {
		return IPv64IntegrationInfo{}, err
	}
	var i IPv64IntegrationInfo
	err := c.call(ctx, http.MethodPost, "/integrations", nil, in, &i)
	i.strip()
	return i, err
}

// ChangeIntegration renames an integration or changes its preselection.
func (c *IPv64) ChangeIntegration(ctx context.Context, id int, name *string, preselect *bool) (IPv64IntegrationInfo, error) {
	body := map[string]any{}
	if name != nil {
		if strings.TrimSpace(*name) == "" || len(*name) > 100 {
			return IPv64IntegrationInfo{}, invalid("name")
		}
		body["name"] = *name
	}
	if preselect != nil {
		body["default"] = *preselect
	}
	var i IPv64IntegrationInfo
	err := c.call(ctx, http.MethodPatch, "/integrations/"+strconv.Itoa(id), nil, body, &i)
	i.strip()
	return i, err
}

// DeleteIntegration removes an integration.
func (c *IPv64) DeleteIntegration(ctx context.Context, id int) error {
	return c.call(ctx, http.MethodDelete, "/integrations/"+strconv.Itoa(id), nil, nil, nil)
}

// TestIntegration sends a test message (one per 30 seconds and integration).
func (c *IPv64) TestIntegration(ctx context.Context, id int) error {
	return c.call(ctx, http.MethodPost, "/integrations/"+strconv.Itoa(id)+"/test", nil, nil, nil)
}

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

// --- status pages ---

// IPv64StatusPage is a public status page; healthchecks are IDs.
type IPv64StatusPage struct {
	ID                int        `json:"id"`
	Name              string     `json:"name"`
	Slug              string     `json:"slug"`
	URL               string     `json:"url"`
	Description       string     `json:"description"`
	PasswordProtected bool       `json:"password_protected"`
	Design            int        `json:"design"`
	Autorefresh       bool       `json:"autorefresh"`
	Healthchecks      []string   `json:"healthchecks"`
	CreatedAt         *time.Time `json:"created_at"`
	LastViewedAt      *time.Time `json:"last_viewed_at"`
}

func (p *IPv64StatusPage) hide() {
	for i, token := range p.Healthchecks {
		p.Healthchecks[i] = HealthcheckID(token)
	}
	if p.Healthchecks == nil {
		p.Healthchecks = []string{}
	}
}

// StatusPageInput is the body of creating or changing a status page; the
// password is write-only. Healthchecks are tokens here.
type StatusPageInput struct {
	Name         *string   `json:"name,omitempty"`
	Description  *string   `json:"description,omitempty"`
	Slug         *string   `json:"slug,omitempty"`
	Password     *string   `json:"password,omitempty"`
	Design       *int      `json:"design,omitempty"`
	Autorefresh  *bool     `json:"autorefresh,omitempty"`
	Healthchecks *[]string `json:"healthchecks,omitempty"`
}

var slugRE = regexp.MustCompile(`^[A-Za-z0-9]{3,128}$`)

// ValidSlug tells whether a status page address is acceptable.
func ValidSlug(slug string) bool { return slugRE.MatchString(slug) }

// Validate checks slug, design and lengths.
func (in StatusPageInput) Validate() error {
	if in.Name != nil && (len(*in.Name) > 200 || strings.ContainsAny(*in.Name, "\r\n")) {
		return invalid("name")
	}
	if in.Description != nil && len(*in.Description) > 2000 {
		return invalid("description")
	}
	if in.Slug != nil && !ValidSlug(*in.Slug) {
		return invalid("slug is 3 to 128 characters a-z, A-Z, 0-9")
	}
	if in.Password != nil && len(*in.Password) > 200 {
		return invalid("password")
	}
	if !inRange(in.Design, 1, 4) {
		return invalid("design is 1 to 4")
	}
	if in.Healthchecks != nil && len(*in.Healthchecks) > 200 {
		return invalid("healthchecks")
	}
	return nil
}

func (c *IPv64) statusPage(ctx context.Context, method, path string, body any) (IPv64StatusPage, error) {
	var p IPv64StatusPage
	err := c.call(ctx, method, path, nil, body, &p)
	p.hide()
	return p, err
}

// StatusPages lists the status pages.
func (c *IPv64) StatusPages(ctx context.Context) ([]IPv64StatusPage, error) {
	list := []IPv64StatusPage{}
	err := c.call(ctx, http.MethodGet, "/status-pages", nil, nil, &list)
	for i := range list {
		list[i].hide()
	}
	return list, err
}

// CreateStatusPage makes a status page (without slug a random address).
func (c *IPv64) CreateStatusPage(ctx context.Context, in StatusPageInput) (IPv64StatusPage, error) {
	if err := in.Validate(); err != nil {
		return IPv64StatusPage{}, err
	}
	return c.statusPage(ctx, http.MethodPost, "/status-pages", in)
}

// ChangeStatusPage changes the given fields of a status page.
func (c *IPv64) ChangeStatusPage(ctx context.Context, id int, in StatusPageInput) (IPv64StatusPage, error) {
	if err := in.Validate(); err != nil {
		return IPv64StatusPage{}, err
	}
	return c.statusPage(ctx, http.MethodPatch, "/status-pages/"+strconv.Itoa(id), in)
}

// DeleteStatusPage removes a status page.
func (c *IPv64) DeleteStatusPage(ctx context.Context, id int) error {
	return c.call(ctx, http.MethodDelete, "/status-pages/"+strconv.Itoa(id), nil, nil, nil)
}

// --- offline pages ---

// IPv64OfflinePage is the page IPv64 shows while a failover points to it.
type IPv64OfflinePage struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Domain         string  `json:"domain"`
	Email          string  `json:"email"`
	Phone          string  `json:"phone"`
	AlternativeURL string  `json:"alternative_url"`
	Text           string  `json:"text"`
	IconURL        *string `json:"icon_url"`
	Views          int     `json:"views"`
	InUseByPolicy  *string `json:"in_use_by_policy"`
	Active         bool    `json:"active"`
	PreviewURL     string  `json:"preview_url"`
}

// OfflinePageInput is the body of creating or changing an offline page.
type OfflinePageInput struct {
	Name           *string `json:"name,omitempty"`
	Domain         *string `json:"domain,omitempty"`
	Email          *string `json:"email,omitempty"`
	Phone          *string `json:"phone,omitempty"`
	AlternativeURL *string `json:"alternative_url,omitempty"`
	Text           *string `json:"text,omitempty"`
}

var phoneRE = regexp.MustCompile(`^\+?[0-9]{3,20}$`)

// Validate checks the formats the description gives.
func (in OfflinePageInput) Validate() error {
	if in.Name != nil && (len(*in.Name) > 200 || strings.ContainsAny(*in.Name, "\r\n")) {
		return invalid("name")
	}
	if in.Domain != nil && *in.Domain != "" && !ValidHost(*in.Domain) {
		return invalid("domain")
	}
	if in.Email != nil && *in.Email != "" && (strings.Count(*in.Email, "@") != 1 || strings.ContainsAny(*in.Email, " <>")) {
		return invalid("email")
	}
	if in.Phone != nil && *in.Phone != "" && !phoneRE.MatchString(*in.Phone) {
		return invalid("phone is digits with an optional leading +")
	}
	if in.AlternativeURL != nil && *in.AlternativeURL != "" && !httpURL(*in.AlternativeURL) {
		return invalid("alternative_url")
	}
	if in.Text != nil && len(*in.Text) > 10000 {
		return invalid("text")
	}
	return nil
}

// OfflinePages lists the offline pages.
func (c *IPv64) OfflinePages(ctx context.Context) ([]IPv64OfflinePage, error) {
	list := []IPv64OfflinePage{}
	err := c.call(ctx, http.MethodGet, "/offline-pages", nil, nil, &list)
	return list, err
}

// CreateOfflinePage makes an offline page.
func (c *IPv64) CreateOfflinePage(ctx context.Context, in OfflinePageInput) (IPv64OfflinePage, error) {
	if err := in.Validate(); err != nil {
		return IPv64OfflinePage{}, err
	}
	var p IPv64OfflinePage
	err := c.call(ctx, http.MethodPost, "/offline-pages", nil, in, &p)
	return p, err
}

// ChangeOfflinePage changes the given fields of an offline page.
func (c *IPv64) ChangeOfflinePage(ctx context.Context, id string, in OfflinePageInput) (IPv64OfflinePage, error) {
	if !validRef(id) {
		return IPv64OfflinePage{}, invalid("offline page")
	}
	if err := in.Validate(); err != nil {
		return IPv64OfflinePage{}, err
	}
	var p IPv64OfflinePage
	err := c.call(ctx, http.MethodPatch, "/offline-pages/"+url.PathEscape(id), nil, in, &p)
	return p, err
}

// DeleteOfflinePage removes an offline page (not while a rule uses it).
func (c *IPv64) DeleteOfflinePage(ctx context.Context, id string) error {
	if !validRef(id) {
		return invalid("offline page")
	}
	return c.call(ctx, http.MethodDelete, "/offline-pages/"+url.PathEscape(id), nil, nil, nil)
}

// MaxIconBytes and MaxIconSide are the limits of a logo.
const (
	MaxIconBytes = 1 << 20
	MaxIconSide  = 1024
)

// CheckIcon tells the media type of a logo: JPEG, PNG or WebP, at most
// 1 MB and 1024 × 1024 pixels.
func CheckIcon(data []byte) (string, error) {
	if len(data) == 0 || len(data) > MaxIconBytes {
		return "", invalid("the logo is at most 1 MB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", invalid("the logo is JPEG, PNG or WebP")
	}
	types := map[string]string{"jpeg": "image/jpeg", "png": "image/png", "webp": "image/webp"}
	kind, ok := types[format]
	if !ok {
		return "", invalid("the logo is JPEG, PNG or WebP")
	}
	if config.Width > MaxIconSide || config.Height > MaxIconSide || config.Width < 1 || config.Height < 1 {
		return "", invalid("the logo is at most 1024 × 1024 pixels")
	}
	return kind, nil
}

// UploadIcon sends a logo as multipart/form-data in the field icon.
func (c *IPv64) UploadIcon(ctx context.Context, id string, data []byte) (IPv64OfflinePage, error) {
	if !validRef(id) {
		return IPv64OfflinePage{}, invalid("offline page")
	}
	kind, err := CheckIcon(data)
	if err != nil {
		return IPv64OfflinePage{}, err
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="icon"; filename="icon.`+strings.TrimPrefix(kind, "image/")+`"`)
	header.Set("Content-Type", kind)
	part, err := form.CreatePart(header)
	if err != nil {
		return IPv64OfflinePage{}, err
	}
	part.Write(data)
	form.Close()
	answer, err := c.send(ctx, http.MethodPost, "/offline-pages/"+url.PathEscape(id)+"/icon", nil, body.Bytes(), form.FormDataContentType())
	if err != nil {
		return IPv64OfflinePage{}, err
	}
	var p IPv64OfflinePage
	err = decodeData(answer.Data, &p)
	return p, err
}

// DeleteIcon removes the logo of an offline page.
func (c *IPv64) DeleteIcon(ctx context.Context, id string) error {
	if !validRef(id) {
		return invalid("offline page")
	}
	return c.call(ctx, http.MethodDelete, "/offline-pages/"+url.PathEscape(id)+"/icon", nil, nil, nil)
}

// --- CDN ---

// IPv64CDNMapping is a port of the portmapper.
type IPv64CDNMapping struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	ListenPort int     `json:"listen_port"`
	Transport  string  `json:"transport"`
	TargetPort int     `json:"target_port"`
	Record     string  `json:"record"`
	FQDN       *string `json:"fqdn"`
}

// IPv64CDN is a domain in the CDN, with its mappings in the detail view.
type IPv64CDN struct {
	ID                string   `json:"id"`
	Domain            *string  `json:"domain"`
	DomainID          int64    `json:"domain_id"`
	Active            bool     `json:"active"`
	Pending           []string `json:"pending"`
	DNSRecordsEnabled bool     `json:"dns_records_enabled"`
	Server            *struct {
		Name     string `json:"name"`
		Hostname string `json:"hostname"`
		IPv4     string `json:"ipv4"`
		IPv6     string `json:"ipv6"`
	} `json:"server"`
	Proxy struct {
		State    string  `json:"state"`
		Frontend *string `json:"frontend"`
		Backend  struct {
			Scheme string `json:"scheme"`
			Port   int    `json:"port"`
		} `json:"backend"`
		Websocket   bool   `json:"websocket"`
		Certificate string `json:"certificate"`
		Caching     struct {
			Enabled     bool `json:"enabled"`
			TimeMinutes int  `json:"time_minutes"`
			SizeMB      int  `json:"size_mb"`
		} `json:"caching"`
		AccessList struct {
			Enabled bool   `json:"enabled"`
			Mode    string `json:"mode"`
		} `json:"access_list"`
	} `json:"proxy"`
	Portmapper struct {
		State string `json:"state"`
	} `json:"portmapper"`
	TrafficMB struct {
		In  float64 `json:"in"`
		Out float64 `json:"out"`
	} `json:"traffic_mb"`
	CreatedAt *time.Time        `json:"created_at"`
	Mappings  []IPv64CDNMapping `json:"mappings,omitempty"`
}

// CDN enums.
var (
	IPv64CDNFrontends  = []string{"http", "https_redirect", "http_and_https"}
	IPv64CDNSchemes    = []string{"http", "https"}
	IPv64AccessModes   = []string{"allowlist", "denylist"}
	IPv64CDNRecordMode = []string{"full", "other_family_only"}
	IPv64Transports    = []string{"tcp", "udp"}
)

// CDNInput is a PATCH of the proxy settings.
type CDNInput struct {
	Frontend      *string `json:"frontend,omitempty"`
	BackendScheme *string `json:"backend_scheme,omitempty"`
	BackendPort   *int    `json:"backend_port,omitempty"`
	Websocket     *bool   `json:"websocket,omitempty"`
	Caching       *struct {
		Enabled     *bool `json:"enabled,omitempty"`
		TimeMinutes *int  `json:"time_minutes,omitempty"`
		SizeMB      *int  `json:"size_mb,omitempty"`
	} `json:"caching,omitempty"`
	AccessList *struct {
		Enabled *bool   `json:"enabled,omitempty"`
		Mode    *string `json:"mode,omitempty"`
	} `json:"access_list,omitempty"`
}

// Validate checks the enums and ranges.
func (in CDNInput) Validate() error {
	if in.Frontend != nil && !slices.Contains(IPv64CDNFrontends, *in.Frontend) {
		return invalid("frontend")
	}
	if in.BackendScheme != nil && !slices.Contains(IPv64CDNSchemes, *in.BackendScheme) {
		return invalid("backend_scheme")
	}
	if !inRange(in.BackendPort, 1, 65535) {
		return invalid("backend_port is 1 to 65535")
	}
	if in.Caching != nil {
		if !inRange(in.Caching.TimeMinutes, 1, 600) {
			return invalid("caching time is 1 to 600 minutes")
		}
		if in.Caching.SizeMB != nil && *in.Caching.SizeMB < 1 {
			return invalid("caching size is at least 1 MB")
		}
	}
	if in.AccessList != nil && in.AccessList.Mode != nil && !slices.Contains(IPv64AccessModes, *in.AccessList.Mode) {
		return invalid("access_list mode")
	}
	return nil
}

func cdnPath(token string, rest ...string) (string, error) {
	if !validRef(token) {
		return "", invalid("CDN")
	}
	return "/cdn/" + url.PathEscape(token) + strings.Join(rest, ""), nil
}

func (c *IPv64) cdn(ctx context.Context, method, token string, rest string, body any) (IPv64CDN, int, error) {
	path, err := cdnPath(token, rest)
	if err != nil {
		return IPv64CDN{}, 0, err
	}
	answer, err := c.Do(ctx, method, path, nil, body)
	if err != nil {
		return IPv64CDN{}, 0, err
	}
	var out IPv64CDN
	if answer.Status != http.StatusNoContent {
		err = decodeData(answer.Data, &out)
	}
	if out.Pending == nil {
		out.Pending = []string{}
	}
	return out, answer.Status, err
}

// CDNs lists the domains in the CDN.
func (c *IPv64) CDNs(ctx context.Context) ([]IPv64CDN, error) {
	list := []IPv64CDN{}
	err := c.call(ctx, http.MethodGet, "/cdn", nil, nil, &list)
	for i := range list {
		if list[i].Pending == nil {
			list[i].Pending = []string{}
		}
	}
	return list, err
}

// CDN reads one CDN domain with its mappings.
func (c *IPv64) CDN(ctx context.Context, token string) (IPv64CDN, error) {
	out, _, err := c.cdn(ctx, http.MethodGet, token, "", nil)
	return out, err
}

// CreateCDN takes a domain (by ID or name) into the CDN.
func (c *IPv64) CreateCDN(ctx context.Context, domain string) (IPv64CDN, error) {
	if !ValidHost(domain) && !isNumber(domain, 1<<31) {
		return IPv64CDN{}, invalid("domain")
	}
	var out IPv64CDN
	err := c.call(ctx, http.MethodPost, "/cdn", nil, map[string]string{"domain": domain}, &out)
	if out.Pending == nil {
		out.Pending = []string{}
	}
	return out, err
}

// ChangeCDN changes the proxy settings of a CDN domain.
func (c *IPv64) ChangeCDN(ctx context.Context, token string, in CDNInput) (IPv64CDN, error) {
	if err := in.Validate(); err != nil {
		return IPv64CDN{}, err
	}
	out, _, err := c.cdn(ctx, http.MethodPatch, token, "", in)
	return out, err
}

// DeleteCDN takes a domain out of the CDN (202; first switch all records off).
func (c *IPv64) DeleteCDN(ctx context.Context, token string) (IPv64CDN, error) {
	out, _, err := c.cdn(ctx, http.MethodDelete, token, "", nil)
	return out, err
}

// IPv64CDNActions are the POST actions of a CDN domain.
var IPv64CDNActions = []string{"proxy/enable", "proxy/disable", "portmapper/enable", "portmapper/disable", "redeploy", "reset-traffic", "move", "renew-certificate"}

// CDNAction runs one of IPv64CDNActions; most answer 202 with pending.
func (c *IPv64) CDNAction(ctx context.Context, token, action string) (IPv64CDN, int, error) {
	if !slices.Contains(IPv64CDNActions, action) {
		return IPv64CDN{}, 0, invalid("action")
	}
	return c.cdn(ctx, http.MethodPost, token, "/"+action, nil)
}

// CDNRecords lists A and AAAA records of the domain with their CDN state.
func (c *IPv64) CDNRecords(ctx context.Context, token string) ([]IPv64FullRecord, error) {
	path, err := cdnPath(token, "/records")
	if err != nil {
		return nil, err
	}
	list := []IPv64FullRecord{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// CDNRecord routes a record through the CDN (mode full or
// other_family_only) or back to direct delivery (mode "").
func (c *IPv64) CDNRecord(ctx context.Context, token, record, mode string) (IPv64FullRecord, error) {
	if !validRef(record) {
		return IPv64FullRecord{}, invalid("record")
	}
	action, body := "/disable", any(nil)
	if mode != "" {
		if !slices.Contains(IPv64CDNRecordMode, mode) {
			return IPv64FullRecord{}, invalid("mode")
		}
		action, body = "/enable", map[string]string{"mode": mode}
	}
	path, err := cdnPath(token, "/records/", url.PathEscape(record), action)
	if err != nil {
		return IPv64FullRecord{}, err
	}
	var r IPv64FullRecord
	err = c.call(ctx, http.MethodPost, path, nil, body, &r)
	return r, err
}

// MappingInput is a new port of the portmapper.
type MappingInput struct {
	Record     string `json:"record"`
	TargetPort int    `json:"target_port"`
	Transport  string `json:"transport,omitempty"`
	Name       string `json:"name,omitempty"`
	ListenPort int    `json:"listen_port,omitempty"`
}

// Validate checks ports and transport; ListenPort 0 lets IPv64 choose.
func (in MappingInput) Validate() error {
	if !validRef(in.Record) {
		return invalid("record")
	}
	if in.TargetPort < 1 || in.TargetPort > 65535 {
		return invalid("target_port is 1 to 65535")
	}
	if in.Transport != "" && !slices.Contains(IPv64Transports, in.Transport) {
		return invalid("transport is tcp or udp")
	}
	if in.ListenPort != 0 && (in.ListenPort < 50000 || in.ListenPort > 62000) {
		return invalid("listen_port is 50000 to 62000")
	}
	if len(in.Name) > 100 || strings.ContainsAny(in.Name, "\r\n") {
		return invalid("name")
	}
	return nil
}

// Mappings lists the ports of the portmapper.
func (c *IPv64) Mappings(ctx context.Context, token string) ([]IPv64CDNMapping, error) {
	path, err := cdnPath(token, "/mappings")
	if err != nil {
		return nil, err
	}
	list := []IPv64CDNMapping{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// CreateMapping adds a port.
func (c *IPv64) CreateMapping(ctx context.Context, token string, in MappingInput) (IPv64CDNMapping, error) {
	if err := in.Validate(); err != nil {
		return IPv64CDNMapping{}, err
	}
	path, err := cdnPath(token, "/mappings")
	if err != nil {
		return IPv64CDNMapping{}, err
	}
	var m IPv64CDNMapping
	err = c.call(ctx, http.MethodPost, path, nil, in, &m)
	return m, err
}

// DeleteMapping removes a port.
func (c *IPv64) DeleteMapping(ctx context.Context, token, mapping string) error {
	if !validRef(mapping) {
		return invalid("mapping")
	}
	path, err := cdnPath(token, "/mappings/", url.PathEscape(mapping))
	if err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, path, nil, nil, nil)
}

// IPv64AccessEntry is an address of the access list.
type IPv64AccessEntry struct {
	ID      int64  `json:"id"`
	Address string `json:"address"`
}

// AccessList reads the access list of the reverse proxy.
func (c *IPv64) AccessList(ctx context.Context, token string) ([]IPv64AccessEntry, error) {
	path, err := cdnPath(token, "/access-list")
	if err != nil {
		return nil, err
	}
	list := []IPv64AccessEntry{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// AddAccess adds an address or network (CIDR).
func (c *IPv64) AddAccess(ctx context.Context, token, address string) (IPv64AccessEntry, error) {
	if _, err := netip.ParsePrefix(address); err != nil {
		if _, err := netip.ParseAddr(address); err != nil {
			return IPv64AccessEntry{}, invalid("address is an IP or a network in CIDR")
		}
	}
	path, err := cdnPath(token, "/access-list")
	if err != nil {
		return IPv64AccessEntry{}, err
	}
	var e IPv64AccessEntry
	err = c.call(ctx, http.MethodPost, path, nil, map[string]string{"address": address}, &e)
	return e, err
}

// RemoveAccess removes an entry of the access list.
func (c *IPv64) RemoveAccess(ctx context.Context, token string, id int64) error {
	path, err := cdnPath(token, "/access-list/", strconv.FormatInt(id, 10))
	if err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, path, nil, nil, nil)
}

// --- Cloud Router ---

// IPv64Peer is a WireGuard peer of a gateway (never with its config).
type IPv64Peer struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	State           string  `json:"state"`
	Disabled        bool    `json:"disabled"`
	SiteToSite      bool    `json:"site_to_site"`
	IPv4            *string `json:"ipv4"`
	IPv6            *string `json:"ipv6"`
	DNSServer       *string `json:"dns_server"`
	Endpoint        *string `json:"endpoint"`
	EndpointPort    *int    `json:"endpoint_port"`
	Keepalive       string  `json:"keepalive"`
	PublicKey       *string `json:"public_key"`
	HasPresharedKey bool    `json:"has_preshared_key"`
	CurrentEndpoint *string `json:"current_endpoint"`
	LastHandshake   *string `json:"last_handshake"`
	TrafficBytes    struct {
		In  float64 `json:"in"`
		Out float64 `json:"out"`
	} `json:"traffic_bytes"`
	AllowedIPs []struct {
		ID      int64  `json:"id"`
		Network string `json:"network"`
	} `json:"allowed_ips,omitempty"`
}

// IPv64Gateway is a WireGuard gateway of a router.
type IPv64Gateway struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	State          string `json:"state"`
	Disabled       bool   `json:"disabled"`
	TrafficLimited bool   `json:"traffic_limited"`
	Server         *struct {
		Name     string `json:"name"`
		Hostname string `json:"hostname"`
		Location string `json:"location"`
	} `json:"server"`
	ListenPort *int    `json:"listen_port"`
	IPv4       *string `json:"ipv4"`
	IPv6       *string `json:"ipv6"`
	PublicKey  *string `json:"public_key"`
	OSPF       *struct {
		AreaID      string `json:"area_id"`
		OSPFVersion int    `json:"ospf_version"`
		Cost        int    `json:"cost"`
	} `json:"ospf"`
	TrafficBytes struct {
		In  float64 `json:"in"`
		Out float64 `json:"out"`
	} `json:"traffic_bytes"`
	CreatedAt *time.Time  `json:"created_at"`
	Peers     []IPv64Peer `json:"peers,omitempty"`
}

// IPv64PublicIP is a public address of a router.
type IPv64PublicIP struct {
	ID        int64      `json:"id"`
	IP        string     `json:"ip"`
	Type      string     `json:"type"`
	Disabled  bool       `json:"disabled"`
	State     string     `json:"state"`
	CreatedAt *time.Time `json:"created_at"`
}

// IPv64Router is a Cloud Router.
type IPv64Router struct {
	ID             string          `json:"id"`
	Name           string          `json:"name"`
	VRF            string          `json:"vrf"`
	TrafficLimited bool            `json:"traffic_limited"`
	Gateways       []IPv64Gateway  `json:"gateways"`
	PublicIPs      []IPv64PublicIP `json:"public_ips,omitempty"`
}

// IPv64Route is a static route of a router.
type IPv64Route struct {
	ID          int64   `json:"id"`
	Destination string  `json:"destination"`
	Gateway     string  `json:"gateway"`
	Distance    int     `json:"distance"`
	Peer        *string `json:"peer"`
	System      bool    `json:"system"`
	Disabled    bool    `json:"disabled"`
	State       string  `json:"state"`
}

// IPv64RouterRule is a firewall rule of a router.
type IPv64RouterRule struct {
	ID          int64   `json:"id"`
	Chain       string  `json:"chain"`
	Action      string  `json:"action"`
	Family      string  `json:"family"`
	Source      *string `json:"source"`
	Destination *string `json:"destination"`
	Disabled    bool    `json:"disabled"`
	State       string  `json:"state"`
}

// Router enums.
var (
	IPv64RuleChains  = []string{"forward", "input", "output"}
	IPv64RuleActions = []string{"accept", "drop"}
)

func routerPath(token string, rest ...string) (string, error) {
	if !validRef(token) {
		return "", invalid("router")
	}
	return "/cloud-routers/" + url.PathEscape(token) + strings.Join(rest, ""), nil
}

func validName100(name string) bool {
	return len(name) <= 100 && !strings.ContainsAny(name, "\r\n")
}

// Routers lists the Cloud Routers.
func (c *IPv64) Routers(ctx context.Context) ([]IPv64Router, error) {
	list := []IPv64Router{}
	err := c.call(ctx, http.MethodGet, "/cloud-routers", nil, nil, &list)
	return list, err
}

// Router reads a router with gateways, peers and addresses.
func (c *IPv64) Router(ctx context.Context, token string) (IPv64Router, error) {
	path, err := routerPath(token)
	if err != nil {
		return IPv64Router{}, err
	}
	var r IPv64Router
	err = c.call(ctx, http.MethodGet, path, nil, nil, &r)
	return r, err
}

// CreateRouter makes a router with its first gateway (202).
func (c *IPv64) CreateRouter(ctx context.Context, name string) (IPv64Router, error) {
	if !validName100(name) {
		return IPv64Router{}, invalid("name")
	}
	var r IPv64Router
	err := c.call(ctx, http.MethodPost, "/cloud-routers", nil, map[string]string{"name": name}, &r)
	return r, err
}

// RenameRouter renames a router.
func (c *IPv64) RenameRouter(ctx context.Context, token, name string) (IPv64Router, error) {
	path, err := routerPath(token)
	if err != nil || !validName100(name) || name == "" {
		return IPv64Router{}, invalid("router or name")
	}
	var r IPv64Router
	err = c.call(ctx, http.MethodPatch, path, nil, map[string]string{"name": name}, &r)
	return r, err
}

// CreateGateway adds a gateway to a router (202).
func (c *IPv64) CreateGateway(ctx context.Context, token, name string) (IPv64Gateway, error) {
	path, err := routerPath(token, "/gateways")
	if err != nil || !validName100(name) {
		return IPv64Gateway{}, invalid("router or name")
	}
	var g IPv64Gateway
	err = c.call(ctx, http.MethodPost, path, nil, map[string]string{"name": name}, &g)
	return g, err
}

// Routes lists the routes of a router.
func (c *IPv64) Routes(ctx context.Context, token string) ([]IPv64Route, error) {
	path, err := routerPath(token, "/routes")
	if err != nil {
		return nil, err
	}
	list := []IPv64Route{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// RouteInput is a new static route into a network behind a peer.
type RouteInput struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Peer        string `json:"peer"`
	Distance    int    `json:"distance,omitempty"`
}

// Validate checks the network, the next hop of the same family and distance.
func (in RouteInput) Validate() error {
	dest, err := netip.ParsePrefix(in.Destination)
	if err != nil {
		return invalid("destination is a network in CIDR")
	}
	hop, err := netip.ParseAddr(in.Gateway)
	if err != nil || hop.Is4() != dest.Addr().Is4() {
		return invalid("gateway is an address of the same family")
	}
	if !validRef(in.Peer) {
		return invalid("peer")
	}
	if in.Distance != 0 && (in.Distance < 1 || in.Distance > 255) {
		return invalid("distance is 1 to 255")
	}
	return nil
}

// CreateRoute adds a route (202).
func (c *IPv64) CreateRoute(ctx context.Context, token string, in RouteInput) (IPv64Route, error) {
	if err := in.Validate(); err != nil {
		return IPv64Route{}, err
	}
	path, err := routerPath(token, "/routes")
	if err != nil {
		return IPv64Route{}, err
	}
	var r IPv64Route
	err = c.call(ctx, http.MethodPost, path, nil, in, &r)
	return r, err
}

// RouterItem switches a route, rule or public IP on or off (kind routes,
// firewall, public-ips) or, with remove, deletes it.
func (c *IPv64) RouterItem(ctx context.Context, token, kind string, id int64, disabled *bool) (json.RawMessage, error) {
	if !slices.Contains([]string{"routes", "firewall", "public-ips"}, kind) {
		return nil, invalid("kind")
	}
	path, err := routerPath(token, "/", kind, "/", strconv.FormatInt(id, 10))
	if err != nil {
		return nil, err
	}
	method, body := http.MethodDelete, any(nil)
	if disabled != nil {
		method, body = http.MethodPatch, map[string]bool{"disabled": *disabled}
	}
	answer, err := c.Do(ctx, method, path, nil, body)
	return answer.Data, err
}

// RouterRules lists the firewall rules of a router.
func (c *IPv64) RouterRules(ctx context.Context, token string) ([]IPv64RouterRule, error) {
	path, err := routerPath(token, "/firewall")
	if err != nil {
		return nil, err
	}
	list := []IPv64RouterRule{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// RouterRuleInput is a new firewall rule of a router.
type RouterRuleInput struct {
	Chain       string `json:"chain"`
	Action      string `json:"action"`
	Source      string `json:"source,omitempty"`
	Destination string `json:"destination,omitempty"`
}

func ipOrCIDR(text string) (netip.Addr, bool) {
	if p, err := netip.ParsePrefix(text); err == nil {
		return p.Addr(), true
	}
	a, err := netip.ParseAddr(text)
	return a, err == nil
}

// Validate checks chain, action and that source and destination share a family.
func (in RouterRuleInput) Validate() error {
	if !slices.Contains(IPv64RuleChains, in.Chain) || !slices.Contains(IPv64RuleActions, in.Action) {
		return invalid("chain or action")
	}
	if in.Source == "" && in.Destination == "" {
		return invalid("source or destination is required")
	}
	var families []bool
	for _, text := range []string{in.Source, in.Destination} {
		if text == "" {
			continue
		}
		addr, ok := ipOrCIDR(text)
		if !ok {
			return invalid("source and destination are IPs or CIDR")
		}
		families = append(families, addr.Is4())
	}
	if len(families) == 2 && families[0] != families[1] {
		return invalid("source and destination of different families")
	}
	return nil
}

// CreateRouterRule adds a firewall rule (202).
func (c *IPv64) CreateRouterRule(ctx context.Context, token string, in RouterRuleInput) (IPv64RouterRule, error) {
	if err := in.Validate(); err != nil {
		return IPv64RouterRule{}, err
	}
	path, err := routerPath(token, "/firewall")
	if err != nil {
		return IPv64RouterRule{}, err
	}
	var r IPv64RouterRule
	err = c.call(ctx, http.MethodPost, path, nil, in, &r)
	return r, err
}

// PublicIPs lists the public addresses of a router.
func (c *IPv64) PublicIPs(ctx context.Context, token string) ([]IPv64PublicIP, error) {
	path, err := routerPath(token, "/public-ips")
	if err != nil {
		return nil, err
	}
	list := []IPv64PublicIP{}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &list)
	return list, err
}

// CreatePublicIPv6 asks for the one public IPv6 of a router (202).
func (c *IPv64) CreatePublicIPv6(ctx context.Context, token string) (IPv64PublicIP, error) {
	path, err := routerPath(token, "/public-ips")
	if err != nil {
		return IPv64PublicIP{}, err
	}
	var ip IPv64PublicIP
	err = c.call(ctx, http.MethodPost, path, nil, nil, &ip)
	return ip, err
}

func gatewayPath(token string, rest ...string) (string, error) {
	if !validRef(token) {
		return "", invalid("gateway")
	}
	return "/gateways/" + url.PathEscape(token) + strings.Join(rest, ""), nil
}

// Gateway reads a gateway with its peers.
func (c *IPv64) Gateway(ctx context.Context, token string) (IPv64Gateway, error) {
	path, err := gatewayPath(token)
	if err != nil {
		return IPv64Gateway{}, err
	}
	var g IPv64Gateway
	err = c.call(ctx, http.MethodGet, path, nil, nil, &g)
	return g, err
}

// GatewayInput is a PATCH of a gateway; addresses in CIDR, "" removes one.
type GatewayInput struct {
	Name     *string `json:"name,omitempty"`
	IPv4     *string `json:"ipv4,omitempty"`
	IPv6     *string `json:"ipv6,omitempty"`
	Disabled *bool   `json:"disabled,omitempty"`
}

// Validate checks name and addresses.
func (in GatewayInput) Validate() error {
	if in.Name != nil && (!validName100(*in.Name) || *in.Name == "") {
		return invalid("name")
	}
	for _, field := range []struct {
		value *string
		v4    bool
	}{{in.IPv4, true}, {in.IPv6, false}} {
		if field.value == nil || *field.value == "" {
			continue
		}
		p, err := netip.ParsePrefix(*field.value)
		if err != nil || p.Addr().Is4() != field.v4 {
			return invalid("gateway addresses are in CIDR of their family")
		}
	}
	return nil
}

// ChangeGateway changes a gateway.
func (c *IPv64) ChangeGateway(ctx context.Context, token string, in GatewayInput) (IPv64Gateway, error) {
	if err := in.Validate(); err != nil {
		return IPv64Gateway{}, err
	}
	path, err := gatewayPath(token)
	if err != nil {
		return IPv64Gateway{}, err
	}
	var g IPv64Gateway
	err = c.call(ctx, http.MethodPatch, path, nil, in, &g)
	return g, err
}

// GatewayAction is delete, rotate-keys or reset-traffic of a gateway.
func (c *IPv64) GatewayAction(ctx context.Context, token, action string) (IPv64Gateway, error) {
	method, rest := http.MethodPost, "/"+action
	switch action {
	case "delete":
		method, rest = http.MethodDelete, ""
	case "rotate-keys", "reset-traffic":
	default:
		return IPv64Gateway{}, invalid("action")
	}
	path, err := gatewayPath(token, rest)
	if err != nil {
		return IPv64Gateway{}, err
	}
	var g IPv64Gateway
	err = c.call(ctx, method, path, nil, nil, &g)
	return g, err
}

// CreatePeer adds a peer; keys and addresses are given by IPv64 (202).
func (c *IPv64) CreatePeer(ctx context.Context, gateway, name string) (IPv64Peer, error) {
	path, err := gatewayPath(gateway, "/peers")
	if err != nil || !validName100(name) {
		return IPv64Peer{}, invalid("gateway or name")
	}
	var p IPv64Peer
	err = c.call(ctx, http.MethodPost, path, nil, map[string]string{"name": name}, &p)
	return p, err
}

func peerPath(token string, rest ...string) (string, error) {
	if !validRef(token) {
		return "", invalid("peer")
	}
	return "/peers/" + url.PathEscape(token) + strings.Join(rest, ""), nil
}

// Peer reads a peer with its AllowedIPs.
func (c *IPv64) Peer(ctx context.Context, token string) (IPv64Peer, error) {
	path, err := peerPath(token)
	if err != nil {
		return IPv64Peer{}, err
	}
	var p IPv64Peer
	err = c.call(ctx, http.MethodGet, path, nil, nil, &p)
	return p, err
}

// DeletePeer removes a peer (202).
func (c *IPv64) DeletePeer(ctx context.Context, token string) error {
	path, err := peerPath(token)
	if err != nil {
		return err
	}
	return c.call(ctx, http.MethodDelete, path, nil, nil, nil)
}

// SetPeerPublicKey hands IPv64 only the public key of a pair made on the
// NAS: the private key never leaves it, the config holds none.
func (c *IPv64) SetPeerPublicKey(ctx context.Context, token, publicKey string) (IPv64Peer, error) {
	path, err := peerPath(token, "/keys")
	if err != nil {
		return IPv64Peer{}, err
	}
	if !validWGKey(publicKey) {
		return IPv64Peer{}, invalid("public key")
	}
	var p IPv64Peer
	err = c.call(ctx, http.MethodPut, path, nil, map[string]string{"public_key": publicKey}, &p)
	return p, err
}

// PeerConfig reads the WireGuard configuration of a peer (for titand only).
func (c *IPv64) PeerConfig(ctx context.Context, token string) (string, error) {
	path, err := peerPath(token, "/config")
	if err != nil {
		return "", err
	}
	var out struct {
		Config string `json:"config"`
	}
	err = c.call(ctx, http.MethodGet, path, nil, nil, &out)
	return out.Config, err
}

var wgKeyRE = regexp.MustCompile(`^[A-Za-z0-9+/]{43}=$`)

func validWGKey(key string) bool { return wgKeyRE.MatchString(key) }

// --- blocklist ---

// IPv64ListToken is GET /honeypot/list-token: the personal token of the
// v64 blocklist and the base address of its files.
type IPv64ListToken struct {
	Token string `json:"token"`
	Live  bool   `json:"live"`
	Base  string `json:"base"`
}

// ListToken reads (or creates) the list token. Only for titand.
func (c *IPv64) ListToken(ctx context.Context) (IPv64ListToken, error) {
	var t IPv64ListToken
	err := c.call(ctx, http.MethodGet, "/honeypot/list-token", nil, nil, &t)
	return t, err
}

// --- helpers ---

var refRE = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// validRef accepts the IDs and tokens of IPv64 in a path.
func validRef(ref string) bool { return refRE.MatchString(ref) }

// ValidIPv64Ref tells whether an ID or token may go into a path.
func ValidIPv64Ref(ref string) bool { return validRef(ref) }

func decodeData(data json.RawMessage, out any) error {
	if len(data) == 0 {
		return &APIError{Provider: "IPv64", Message: "unexpected answer"}
	}
	if err := json.Unmarshal(data, out); err != nil {
		return &APIError{Provider: "IPv64", Message: fmt.Sprintf("unexpected answer: %v", err)}
	}
	return nil
}
