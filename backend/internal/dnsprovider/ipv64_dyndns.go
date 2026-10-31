package dnsprovider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// PublicIP asks GET /ip over the given network ("tcp4" or "tcp6").
func (c *IPv64) PublicIP(ctx context.Context, network string) (netip.Addr, error) {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout: 10 * time.Second,
		// The transport only lives for this request; a kept-alive
		// connection would linger unused.
		DisableKeepAlives: true,
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
	_ = json.Unmarshal(data, &answer) // a value that cannot be read leaves the zero value, which the code handles
	addr, err := netip.ParseAddr(answer.Data.IP)
	if err != nil {
		return netip.Addr{}, &APIError{Provider: "IPv64", Message: "no address in the answer"}
	}
	return addr.Unmap(), nil
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
