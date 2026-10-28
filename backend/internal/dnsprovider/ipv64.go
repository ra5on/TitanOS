package dnsprovider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// IPv64.net, as its OpenAPI description (ipv64.net/api/v2/openapi.json),
// ipv64.net/dyndns_updater_api and ipv64.net/healthcheck_updater_api
// describe it:
//
//   - API v2 https://ipv64.net/api/v2 with the account API key as Bearer
//     token. Answers {"data": …}, errors are problem+json (RFC 9457). A
//     token bucket of 300 per account (10 a second); 429 carries
//     Retry-After. Used for domains, records (TXT of the DNS challenge),
//     DynDNS updates (POST /domains/{d}/dyndns, not counted against the
//     daily update limit), healthchecks and integrations.
//   - The DynDNS2 updater https://ipv64.net/nic/update with a domain update
//     token, when there is no API key or the IPv6 prefix is to be sent too.
//     48 updates a day by default (per plan), at most 3 requests within 10
//     seconds; many failures lock the token for 10 minutes.
//   - Healthcheck pings https://ipv64.net/health with the healthcheck token,
//     72 a day by default, at most 3 within 10 seconds.
//
// Tokens only ever go into the Authorization header.

// IPv64Zones are the zones DynDNS names can be made under ("nas.ipv64.de"),
// from the description of POST /domains.
var IPv64Zones = []string{"ipv64.net", "ipv64.de", "any64.de", "api64.de", "dns64.de", "dyndns64.de", "dynipv6.de", "eth64.de",
	"home64.de", "iot64.de", "lan64.de", "nas64.de", "root64.de", "route64.de", "srv64.de", "tcp64.de", "udp64.de", "vpn64.de", "wan64.de"}

// IPv64RecordTypes are the record types the API takes.
var IPv64RecordTypes = []string{"A", "AAAA", "CNAME", "MX", "TXT", "NS", "SRV", "TLSA", "CAA"}

// IPv64 is a client of IPv64.net.
type IPv64 struct {
	client *http.Client
	base   string
	// key is the API key, update the domain update token; either may be empty.
	key    string
	update string
	// prefix6 is sent as ipv6prefix to the updater ("auto" or a prefix).
	prefix6 string
}

// NewIPv64 returns a client for an API key and/or a domain update token.
func NewIPv64(apiKey, updateToken string, opts Options) *IPv64 {
	client := opts.Client
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	base := opts.BaseURL
	if base == "" {
		base = defaultBase[IPv64Net]
	}
	return &IPv64{client: client, base: strings.TrimSuffix(base, "/"), key: apiKey, update: updateToken}
}

// ipv64Error turns a problem+json answer into an APIError.
func ipv64Error(status int, header http.Header, data []byte) error {
	var problem struct {
		Detail     string `json:"detail"`
		Code       string `json:"code"`
		RetryAfter int    `json:"retry_after"`
	}
	json.Unmarshal(data, &problem)
	e := &APIError{Provider: "IPv64", Status: status, Message: problem.Detail, Code: problem.Code}
	if e.Message == "" {
		e.Message = short(data)
	}
	e.RetryAfter = time.Duration(problem.RetryAfter) * time.Second
	if seconds, err := strconv.Atoi(header.Get("Retry-After")); err == nil && seconds > 0 {
		e.RetryAfter = time.Duration(seconds) * time.Second
	}
	if status == http.StatusTooManyRequests && e.RetryAfter == 0 {
		e.RetryAfter = 10 * time.Second
	}
	return e
}

func (c *IPv64) roundTrip(req *http.Request, client *http.Client) (int, http.Header, []byte, error) {
	req.Header.Set("User-Agent", "TitanOS")
	res, err := client.Do(req)
	if err != nil {
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return 0, nil, nil, fmt.Errorf("IPv64: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return res.StatusCode, res.Header, nil, fmt.Errorf("IPv64: %w", err)
	}
	return res.StatusCode, res.Header, data, nil
}

// call sends one request to API v2 and decodes data into out.
func (c *IPv64) call(ctx context.Context, method, path string, query url.Values, body, out any) error {
	if c.key == "" {
		return &APIError{Provider: "IPv64", Message: "this needs the API key of the account"}
	}
	address := c.base + "/api/v2" + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, reader)
	if err != nil {
		return errors.New("IPv64: request could not be built")
	}
	req.Header.Set("Authorization", "Bearer "+c.key)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	status, header, data, err := c.roundTrip(req, c.client)
	if err != nil {
		return err
	}
	if status >= 300 {
		return ipv64Error(status, header, data)
	}
	if out != nil && status != http.StatusNoContent {
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil || len(envelope.Data) == 0 {
			return &APIError{Provider: "IPv64", Message: "unexpected answer"}
		}
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return &APIError{Provider: "IPv64", Message: "unexpected answer"}
		}
	}
	return nil
}

// --- account ---

// IPv64Usage is how much of a limit is used; Limit nil is unlimited.
type IPv64Usage struct {
	Used  float64 `json:"used"`
	Limit *int    `json:"limit"`
}

// IPv64Account is GET /me without the e-mail address and the key.
type IPv64Account struct {
	Username   string                `json:"username"`
	Plan       string                `json:"plan"`
	ValidUntil *time.Time            `json:"validUntil,omitempty"`
	Usage      map[string]IPv64Usage `json:"usage"`
	KeyName    string                `json:"keyName,omitempty"`
}

// Account tells the plan and the use of its limits.
func (c *IPv64) Account(ctx context.Context) (IPv64Account, error) {
	var me struct {
		Username *string `json:"username"`
		Plan     struct {
			Name       string     `json:"name"`
			ValidUntil *time.Time `json:"valid_until"`
		} `json:"plan"`
		Usage map[string]IPv64Usage `json:"usage"`
		Auth  struct {
			KeyName string `json:"key_name"`
		} `json:"auth"`
	}
	if err := c.call(ctx, http.MethodGet, "/me", nil, nil, &me); err != nil {
		return IPv64Account{}, err
	}
	out := IPv64Account{Plan: me.Plan.Name, ValidUntil: me.Plan.ValidUntil, Usage: me.Usage, KeyName: me.Auth.KeyName}
	if me.Username != nil {
		out.Username = *me.Username
	}
	if out.Usage == nil {
		out.Usage = map[string]IPv64Usage{}
	}
	return out, nil
}

// PublicIP asks GET /ip over the given network ("tcp4" or "tcp6").
func (c *IPv64) PublicIP(ctx context.Context, network string) (netip.Addr, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	if base, ok := c.client.Transport.(*http.Transport); ok && base.TLSClientConfig != nil {
		transport.TLSClientConfig = base.TLSClientConfig.Clone()
	}
	client := &http.Client{Timeout: 20 * time.Second, Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/api/v2/ip", nil)
	if err != nil {
		return netip.Addr{}, errors.New("IPv64: request could not be built")
	}
	status, header, data, err := c.roundTrip(req, client)
	if err != nil {
		return netip.Addr{}, err
	}
	if status != http.StatusOK {
		return netip.Addr{}, ipv64Error(status, header, data)
	}
	var answer struct {
		Data struct {
			IP string `json:"ip"`
		} `json:"data"`
	}
	json.Unmarshal(data, &answer)
	addr, err := netip.ParseAddr(answer.Data.IP)
	if err != nil {
		return netip.Addr{}, &APIError{Provider: "IPv64", Message: "no address in the answer"}
	}
	return addr.Unmap(), nil
}

// --- domains ---

// IPv64Domain is a domain of the account, without its update token.
type IPv64Domain struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Zone        string  `json:"zone"`
	Wildcard    bool    `json:"wildcard"`
	Ignore      string  `json:"ignore"`
	IPv6Prefix  *string `json:"ipv6Prefix,omitempty"`
	Active      bool    `json:"active"`
	UpdateCount int     `json:"updateCount"`
}

type ipv64WireDomain struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Zone        string  `json:"zone"`
	Wildcard    bool    `json:"wildcard"`
	Ignore      string  `json:"ignore"`
	IPv6Prefix  *string `json:"ipv6_prefix"`
	Active      bool    `json:"active"`
	UpdateToken string  `json:"update_token"`
	UpdateCount int     `json:"update_count"`
}

func (w ipv64WireDomain) public() IPv64Domain {
	return IPv64Domain{ID: w.ID, Name: w.Name, Kind: w.Kind, Zone: w.Zone, Wildcard: w.Wildcard, Ignore: w.Ignore, IPv6Prefix: w.IPv6Prefix, Active: w.Active, UpdateCount: w.UpdateCount}
}

func (c *IPv64) wireDomains(ctx context.Context) ([]ipv64WireDomain, error) {
	var list []ipv64WireDomain
	err := c.call(ctx, http.MethodGet, "/domains", nil, nil, &list)
	return list, err
}

// Domains lists the domains of the account.
func (c *IPv64) Domains(ctx context.Context) ([]IPv64Domain, error) {
	list, err := c.wireDomains(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]IPv64Domain, 0, len(list))
	for _, d := range list {
		out = append(out, d.public())
	}
	slices.SortFunc(out, func(a, b IPv64Domain) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

// ValidIPv64Name tells whether name is "<wish>.<zone>" for a DynDNS name.
func ValidIPv64Name(name string) bool {
	for _, zone := range IPv64Zones {
		if wish, ok := strings.CutSuffix(name, "."+zone); ok && wish != "" && !strings.Contains(wish, ".") && validName(wish) && !strings.Contains(wish, "_") {
			return true
		}
	}
	return false
}

// CreateDomain makes a new DynDNS name under one of IPv64Zones.
func (c *IPv64) CreateDomain(ctx context.Context, name string) (IPv64Domain, error) {
	if !ValidIPv64Name(name) {
		return IPv64Domain{}, invalid("name must be <name>.<zone> with one of the zones of IPv64")
	}
	var d ipv64WireDomain
	err := c.call(ctx, http.MethodPost, "/domains", nil, map[string]any{"name": name}, &d)
	return d.public(), err
}

// IPv64DomainChange is a PATCH of a domain; nil fields stay.
type IPv64DomainChange struct {
	Wildcard   *bool   `json:"wildcard,omitempty"`
	Ignore     *string `json:"ignore,omitempty"`
	IPv6Prefix *string `json:"ipv6_prefix,omitempty"`
}

// ChangeDomain changes wildcard, ignored family or fixed IPv6 prefix.
func (c *IPv64) ChangeDomain(ctx context.Context, name string, change IPv64DomainChange) (IPv64Domain, error) {
	if change.Ignore != nil && !slices.Contains([]string{"none", "ipv4", "ipv6"}, *change.Ignore) {
		return IPv64Domain{}, invalid("ignore")
	}
	var d ipv64WireDomain
	err := c.call(ctx, http.MethodPatch, "/domains/"+url.PathEscape(name), nil, change, &d)
	return d.public(), err
}

// --- records ---

// IPv64Record is a DNS record of a domain.
type IPv64Record struct {
	ID         int64  `json:"id"`
	Prefix     string `json:"prefix"`
	FQDN       string `json:"fqdn"`
	Type       string `json:"type"`
	Content    string `json:"content"`
	TTL        int    `json:"ttl"`
	State      string `json:"state"`
	PendingDNS bool   `json:"pendingDNS"`
}

type ipv64WireRecord struct {
	ID         int64  `json:"id"`
	Prefix     string `json:"prefix"`
	FQDN       string `json:"fqdn"`
	Type       string `json:"type"`
	Content    string `json:"content"`
	TTL        int    `json:"ttl"`
	State      string `json:"state"`
	PendingDNS bool   `json:"pending_dns"`
}

func (w ipv64WireRecord) public() IPv64Record { return IPv64Record(w) }

// Records lists the records of a domain, optionally of one type and prefix.
func (c *IPv64) Records(ctx context.Context, domain, kind, prefix string) ([]IPv64Record, error) {
	query := url.Values{}
	if kind != "" {
		query.Set("type", kind)
	}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	var list []ipv64WireRecord
	if err := c.call(ctx, http.MethodGet, "/domains/"+url.PathEscape(domain)+"/records", query, nil, &list); err != nil {
		return nil, err
	}
	out := make([]IPv64Record, 0, len(list))
	for _, r := range list {
		// The filter of the API is used, and checked again.
		if (kind == "" || r.Type == kind) && (prefix == "" || r.Prefix == prefix) {
			out = append(out, r.public())
		}
	}
	return out, nil
}

// CheckRecord tells what is wrong with a record before it is sent.
func CheckRecord(kind, prefix, content string) error {
	if !slices.Contains(IPv64RecordTypes, kind) {
		return invalid("record type")
	}
	if prefix != "" && prefix != "@" && !validName(strings.TrimPrefix(prefix, "*.")) && prefix != "*" {
		return invalid("prefix")
	}
	content = strings.TrimSpace(content)
	if content == "" || len(content) > 2048 || strings.ContainsAny(content, "\r\n") {
		return invalid("content")
	}
	switch kind {
	case "A":
		if a, err := netip.ParseAddr(content); err != nil || !a.Is4() {
			return invalid("an A record holds an IPv4 address")
		}
	case "AAAA":
		if a, err := netip.ParseAddr(content); err != nil || !a.Is6() || a.Is4In6() {
			return invalid("an AAAA record holds an IPv6 address")
		}
	case "CNAME", "NS":
		if !ValidHost(strings.TrimSuffix(strings.ToLower(content), ".")) {
			return invalid("a host name is expected")
		}
	case "MX":
		fields := strings.Fields(content)
		if len(fields) != 2 || !isNumber(fields[0], 65535) || !ValidHost(strings.TrimSuffix(strings.ToLower(fields[1]), ".")) {
			return invalid(`MX is "priority host", e.g. "10 mail.example.org"`)
		}
	case "SRV":
		fields := strings.Fields(content)
		if len(fields) != 4 || !isNumber(fields[0], 65535) || !isNumber(fields[1], 65535) || !isNumber(fields[2], 65535) {
			return invalid(`SRV is "priority weight port target"`)
		}
	case "CAA":
		fields := strings.Fields(content)
		if len(fields) < 3 || !isNumber(fields[0], 255) || !slices.Contains([]string{"issue", "issuewild", "iodef"}, fields[1]) {
			return invalid(`CAA is "flags tag value", e.g. 0 issue "letsencrypt.org"`)
		}
	case "TLSA":
		if len(strings.Fields(content)) != 4 {
			return invalid(`TLSA is "usage selector type data"`)
		}
	}
	return nil
}

func isNumber(s string, most int) bool {
	n, err := strconv.Atoi(s)
	return err == nil && n >= 0 && n <= most
}

// AddRecord creates a record (TTL 60 s at IPv64).
func (c *IPv64) AddRecord(ctx context.Context, domain, kind, prefix, content string) (IPv64Record, error) {
	if err := CheckRecord(kind, prefix, content); err != nil {
		return IPv64Record{}, err
	}
	var r ipv64WireRecord
	err := c.call(ctx, http.MethodPost, "/domains/"+url.PathEscape(domain)+"/records", nil, map[string]string{"type": kind, "prefix": prefix, "content": strings.TrimSpace(content)}, &r)
	return r.public(), err
}

// DeleteRecord removes one record by its ID.
func (c *IPv64) DeleteRecord(ctx context.Context, id int64) error {
	return c.call(ctx, http.MethodDelete, "/records/"+strconv.FormatInt(id, 10), nil, nil, nil)
}

// deleteExact removes the record with exactly this type, prefix and content.
func (c *IPv64) deleteExact(ctx context.Context, domain, kind, prefix, content string) error {
	query := url.Values{"type": {kind}, "content": {content}}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	err := c.call(ctx, http.MethodDelete, "/domains/"+url.PathEscape(domain)+"/records", query, nil, nil)
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound {
		return nil
	}
	return err
}

// zone finds the domain of the account holding fqdn, longest first.
func (c *IPv64) zone(ctx context.Context, fqdn string) (ipv64WireDomain, string, error) {
	list, err := c.wireDomains(ctx)
	if err != nil {
		return ipv64WireDomain{}, "", err
	}
	for _, name := range candidates(fqdn) {
		for _, d := range list {
			if d.Name == name {
				prefix := ""
				if fqdn != name {
					prefix = relative(fqdn, name)
				}
				return d, prefix, nil
			}
		}
	}
	return ipv64WireDomain{}, "", &APIError{Provider: "IPv64", Message: "no domain of this account holds " + fqdn}
}

// ipv64Poll is how often pending records are looked at; tests shorten it.
var ipv64Poll = 5 * time.Second

func (c *IPv64) SetTXT(ctx context.Context, fqdn string, values []string) error {
	d, prefix, err := c.zone(ctx, fqdn)
	if err != nil {
		return err
	}
	existing, err := c.Records(ctx, d.Name, "TXT", prefix)
	if err != nil {
		return err
	}
	for _, value := range values {
		if slices.ContainsFunc(existing, func(r IPv64Record) bool { return r.Content == value }) {
			continue
		}
		if _, err := c.AddRecord(ctx, d.Name, "TXT", prefix, value); err != nil {
			return err
		}
	}
	// Wait until IPv64 has handed the records to its name servers.
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		records, err := c.Records(ctx, d.Name, "TXT", prefix)
		if err != nil {
			return err
		}
		pending := false
		for _, r := range records {
			pending = pending || (r.PendingDNS && slices.Contains(values, r.Content))
		}
		if !pending {
			return nil
		}
		t := time.NewTimer(ipv64Poll)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

func (c *IPv64) ClearTXT(ctx context.Context, fqdn string) error {
	d, prefix, err := c.zone(ctx, fqdn)
	if err != nil {
		return err
	}
	records, err := c.Records(ctx, d.Name, "TXT", prefix)
	if err != nil {
		return err
	}
	for _, r := range records {
		if err := c.deleteExact(ctx, d.Name, "TXT", prefix, r.Content); err != nil {
			return err
		}
	}
	return nil
}

func (c *IPv64) MultipleTXT() bool { return true }

// CombinedUpdate: the updater may fill a missing address from where the
// request came from, so both known addresses are always sent together.
func (c *IPv64) CombinedUpdate() bool { return c.useUpdater() }

func (c *IPv64) useUpdater() bool { return c.key == "" || c.prefix6 != "" }

// SetAddresses sets A and AAAA: over API v2 with the API key (not counted
// against the daily limit), else over the updater with the update token,
// where also the IPv6 prefix can be sent.
func (c *IPv64) SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error {
	if !v4.IsValid() && !v6.IsValid() {
		return nil
	}
	if !c.useUpdater() {
		d, prefix, err := c.zone(ctx, host)
		if err != nil {
			return err
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
		var result struct {
			Results []struct {
				IP     string `json:"ip"`
				Result string `json:"result"`
			} `json:"results"`
		}
		if err := c.call(ctx, http.MethodPost, "/domains/"+url.PathEscape(d.Name)+"/dyndns", nil, body, &result); err != nil {
			return err
		}
		for _, r := range result.Results {
			switch r.Result {
			case "created", "updated", "unchanged", "ignored":
			default:
				return &APIError{Provider: "IPv64", Message: r.IP + ": " + r.Result}
			}
		}
		return nil
	}
	return c.legacyUpdate(ctx, host, v4, v6)
}

// ipv64Budget keeps the updates over /nic/update well under the daily
// limit: at most half of it in 24 hours, and three requests in 10 seconds.
var ipv64Budget struct {
	mu      sync.Mutex
	updates []time.Time
	recent  []time.Time
	// limit is the daily limit of the plan, learned from /me; 0 = 48.
	limit int
}

// SetIPv64DailyLimit tells the budget the limit of the plan.
func SetIPv64DailyLimit(limit int) {
	ipv64Budget.mu.Lock()
	ipv64Budget.limit = limit
	ipv64Budget.mu.Unlock()
}

// ipv64Allow takes one request from the budget; daily says whether it is an
// update counted against the daily limit.
func ipv64Allow(now time.Time, daily bool) error {
	b := &ipv64Budget
	b.mu.Lock()
	defer b.mu.Unlock()
	keep := func(list []time.Time, window time.Duration) []time.Time {
		out := list[:0]
		for _, t := range list {
			if now.Sub(t) < window {
				out = append(out, t)
			}
		}
		return out
	}
	b.recent = keep(b.recent, 10*time.Second)
	b.updates = keep(b.updates, 24*time.Hour)
	if len(b.recent) >= 3 {
		return &APIError{Provider: "IPv64", Status: http.StatusTooManyRequests, Message: "at most three requests in 10 seconds", RetryAfter: 10 * time.Second}
	}
	if daily {
		limit := b.limit
		if limit <= 0 {
			limit = 48
		}
		if len(b.updates) >= max(limit/2, 1) {
			next := b.updates[0].Add(24 * time.Hour)
			return &APIError{Provider: "IPv64", Status: http.StatusTooManyRequests, Message: fmt.Sprintf("TitanOS keeps to half of the %d daily updates of the plan; next one at %s", limit, next.Format(time.TimeOnly)), RetryAfter: next.Sub(now)}
		}
		b.updates = append(b.updates, now)
	}
	b.recent = append(b.recent, now)
	return nil
}

// updateToken returns the domain update token, from the domain list when
// only the API key was given.
func (c *IPv64) updateToken(ctx context.Context, host string) (string, error) {
	if c.update != "" {
		return c.update, nil
	}
	d, _, err := c.zone(ctx, host)
	if err != nil {
		return "", err
	}
	if d.UpdateToken == "" {
		return "", &APIError{Provider: "IPv64", Message: "the domain has no update token"}
	}
	return d.UpdateToken, nil
}

func (c *IPv64) legacyUpdate(ctx context.Context, host string, v4, v6 netip.Addr) error {
	token, err := c.updateToken(ctx, host)
	if err != nil {
		return err
	}
	query := url.Values{"domain": {host}, "output": {"min"}}
	if v4.IsValid() {
		query.Set("ip", v4.Unmap().String())
	}
	if v6.IsValid() {
		query.Set("ip6", v6.String())
	}
	if c.prefix6 != "" {
		query.Set("ipv6prefix", c.prefix6)
	}
	if err := ipv64Allow(time.Now(), true); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/nic/update?"+query.Encode(), nil)
	if err != nil {
		return errors.New("IPv64: request could not be built")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	status, header, data, err := c.roundTrip(req, c.client)
	if err != nil {
		return err
	}
	word, _, _ := strings.Cut(strings.TrimSpace(string(data)), " ")
	switch {
	case status == http.StatusUnauthorized:
		return &APIError{Provider: "IPv64", Status: status, Message: "update token not accepted"}
	case status == http.StatusTooManyRequests:
		return ipv64Error(status, header, data)
	case status >= 300 || (word != "good" && word != "nochg"):
		return &APIError{Provider: "IPv64", Status: status, Message: "the update was refused: " + dyndns2Explain(word)}
	}
	return nil
}

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

type ipv64WireCheck struct {
	Token  string `json:"token"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Status string `json:"status"`
	Notify struct {
		Integrations []int `json:"integrations"`
	} `json:"notify"`
	LastSeen *time.Time `json:"last_seen_at"`
	Push     *struct {
		Period     int        `json:"period_seconds"`
		Grace      int        `json:"grace_seconds"`
		ExpectedBy *time.Time `json:"expected_by"`
	} `json:"push"`
}

// HealthcheckID is the name the web UI knows a check by.
func HealthcheckID(token string) string { return shortHash(token) }

func (w ipv64WireCheck) public() IPv64Healthcheck {
	h := IPv64Healthcheck{ID: HealthcheckID(w.Token), Name: w.Name, Status: w.Status, LastSeen: w.LastSeen, Integrations: w.Notify.Integrations, token: w.Token}
	if h.Integrations == nil {
		h.Integrations = []int{}
	}
	if w.Push != nil {
		h.Period, h.Grace, h.ExpectedBy = w.Push.Period, w.Push.Grace, w.Push.ExpectedBy
	}
	return h
}

// Healthchecks lists the push healthchecks of the account.
func (c *IPv64) Healthchecks(ctx context.Context) ([]IPv64Healthcheck, error) {
	var list []ipv64WireCheck
	if err := c.call(ctx, http.MethodGet, "/healthchecks", url.Values{"type": {"push"}}, nil, &list); err != nil {
		return nil, err
	}
	out := []IPv64Healthcheck{}
	for _, w := range list {
		if w.Type == "" || w.Type == "push" {
			out = append(out, w.public())
		}
	}
	return out, nil
}

// Healthcheck reads one check.
func (c *IPv64) Healthcheck(ctx context.Context, token string) (IPv64Healthcheck, error) {
	var w ipv64WireCheck
	err := c.call(ctx, http.MethodGet, "/healthchecks/"+url.PathEscape(token), nil, nil, &w)
	return w.public(), err
}

// CreateHealthcheck makes a push check; IPv64 alarms when no ping came for
// period plus grace. Integration 0 is the e-mail address of the account.
func (c *IPv64) CreateHealthcheck(ctx context.Context, name string, period, grace time.Duration, integrations []int) (IPv64Healthcheck, error) {
	if name == "" || len(name) > 100 || period < time.Minute || grace < time.Minute || period%time.Minute != 0 || grace%time.Minute != 0 {
		return IPv64Healthcheck{}, invalid("healthcheck")
	}
	body := map[string]any{"name": name, "type": "push", "period_seconds": int(period.Seconds()), "grace_seconds": int(grace.Seconds()),
		"integrations": integrations, "notify_on_down": true, "notify_on_up": true}
	var w ipv64WireCheck
	err := c.call(ctx, http.MethodPost, "/healthchecks", nil, body, &w)
	return w.public(), err
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

// IPv64Integration is an alert channel of the account, names only.
type IPv64Integration struct {
	ID     int    `json:"id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// Integrations lists the alert channels (never with their secrets).
func (c *IPv64) Integrations(ctx context.Context) ([]IPv64Integration, error) {
	var list []IPv64Integration
	if err := c.call(ctx, http.MethodGet, "/integrations", url.Values{"include_secrets": {"false"}}, nil, &list); err != nil {
		return nil, err
	}
	if list == nil {
		list = []IPv64Integration{}
	}
	return list, nil
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
