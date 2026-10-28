package dnsprovider

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
)

// desec speaks the API of deSEC (desec.io, also dedyn.io names) with a token.
// RRsets are replaced as a whole.
type desec struct {
	api   *httpAPI
	token string
}

type desecDomain struct {
	Name       string `json:"name"`
	MinimumTTL int    `json:"minimum_ttl"`
}

type desecRRset struct {
	Subname string   `json:"subname"`
	Type    string   `json:"type"`
	TTL     int      `json:"ttl,omitempty"`
	Records []string `json:"records"`
}

func (d *desec) call(ctx context.Context, method, path string, body, out any) error {
	header := http.Header{"Authorization": {"Token " + d.token}}
	_, _, err := d.api.do(ctx, "deSEC", method, d.api.base+path, header, body, out)
	return err
}

func (d *desec) domain(ctx context.Context, fqdn string) (desecDomain, error) {
	var domains []desecDomain
	if err := d.call(ctx, http.MethodGet, "/domains/?owns_qname="+url.QueryEscape(fqdn), nil, &domains); err != nil {
		return desecDomain{}, err
	}
	if len(domains) == 0 {
		return desecDomain{}, &APIError{Provider: "deSEC", Message: "no domain of this account holds " + fqdn}
	}
	return domains[0], nil
}

func (d *desec) put(ctx context.Context, fqdn string, sets func(sub string, ttl int) []desecRRset) error {
	domain, err := d.domain(ctx, fqdn)
	if err != nil {
		return err
	}
	sub := relative(fqdn, domain.Name)
	if sub == "@" {
		sub = ""
	}
	ttl := max(domain.MinimumTTL, 60)
	return d.call(ctx, http.MethodPut, "/domains/"+url.PathEscape(domain.Name)+"/rrsets/", sets(sub, ttl), nil)
}

func (d *desec) SetTXT(ctx context.Context, fqdn string, values []string) error {
	records := make([]string, 0, len(values))
	for _, value := range values {
		records = append(records, quote(value))
	}
	return d.put(ctx, fqdn, func(sub string, ttl int) []desecRRset {
		return []desecRRset{{Subname: sub, Type: "TXT", TTL: ttl, Records: records}}
	})
}

// ClearTXT empties the RRset, which deletes it.
func (d *desec) ClearTXT(ctx context.Context, fqdn string) error {
	return d.put(ctx, fqdn, func(sub string, ttl int) []desecRRset {
		return []desecRRset{{Subname: sub, Type: "TXT", TTL: ttl, Records: []string{}}}
	})
}

func (d *desec) MultipleTXT() bool { return true }

func (d *desec) SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error {
	records := addressRecords(v4, v6)
	if len(records) == 0 {
		return nil
	}
	return d.put(ctx, host, func(sub string, ttl int) []desecRRset {
		var sets []desecRRset
		for _, r := range records {
			sets = append(sets, desecRRset{Subname: sub, Type: r.kind, TTL: ttl, Records: []string{r.value}})
		}
		return sets
	})
}
