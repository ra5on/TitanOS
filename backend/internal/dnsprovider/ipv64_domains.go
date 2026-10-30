package dnsprovider

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"
)

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

// short is the view of a record the DynDNS page and the DNS challenge use.
func (r IPv64FullRecord) short() IPv64Record {
	return IPv64Record{ID: r.ID, Prefix: r.Prefix, FQDN: r.FQDN, Type: r.Type, Content: r.Content, TTL: r.TTL, State: r.State, PendingDNS: r.PendingDNS}
}

// records is GET /domains/{domain}/records with an optional filter.
func (c *IPv64) records(ctx context.Context, domain string, query url.Values) ([]IPv64FullRecord, error) {
	list := []IPv64FullRecord{}
	err := c.call(ctx, http.MethodGet, "/domains/"+url.PathEscape(domain)+"/records", query, nil, &list)
	return list, err
}

// Records lists the records of a domain, optionally of one type and prefix.
func (c *IPv64) Records(ctx context.Context, domain, kind, prefix string) ([]IPv64Record, error) {
	query := url.Values{}
	if kind != "" {
		query.Set("type", kind)
	}
	if prefix != "" {
		query.Set("prefix", prefix)
	}
	list, err := c.records(ctx, domain, query)
	if err != nil {
		return nil, err
	}
	out := make([]IPv64Record, 0, len(list))
	for _, r := range list {
		// The filter of the API is used, and checked again.
		if (kind == "" || r.Type == kind) && (prefix == "" || r.Prefix == prefix) {
			out = append(out, r.short())
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
	var r IPv64FullRecord
	err := c.call(ctx, http.MethodPost, "/domains/"+url.PathEscape(domain)+"/records", nil, map[string]string{"type": kind, "prefix": prefix, "content": strings.TrimSpace(content)}, &r)
	return r.short(), err
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
	return c.records(ctx, domain, nil)
}
