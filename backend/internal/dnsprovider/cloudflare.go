package dnsprovider

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
)

// cloudflare speaks API v4 with an API token that may edit DNS of the zone
// (permissions Zone:Read and DNS:Edit).
type cloudflare struct {
	api   *httpAPI
	token string
}

type cfRecord struct {
	ID      string `json:"id,omitempty"`
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied *bool  `json:"proxied,omitempty"`
}

type cfAnswer[T any] struct {
	Success bool `json:"success"`
	Errors  []struct {
		Message string `json:"message"`
	} `json:"errors"`
	Result T `json:"result"`
}

func (c *cloudflare) call(ctx context.Context, method, path string, body any, out any) error {
	header := http.Header{"Authorization": {"Bearer " + c.token}}
	_, data, err := c.api.do(ctx, "Cloudflare", method, c.api.base+path, header, body, out)
	if apiErr, ok := err.(*APIError); ok {
		// The JSON of a refusal names the reason better than the excerpt.
		var answer cfAnswer[any]
		if decodeJSON(data, &answer) == nil && len(answer.Errors) > 0 {
			apiErr.Message = answer.Errors[0].Message
		}
	}
	return err
}

func (c *cloudflare) zone(ctx context.Context, fqdn string) (string, error) {
	for _, name := range candidates(fqdn) {
		var answer cfAnswer[[]struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}]
		if err := c.call(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(name), nil, &answer); err != nil {
			return "", err
		}
		if len(answer.Result) > 0 {
			return answer.Result[0].ID, nil
		}
	}
	return "", &APIError{Provider: "Cloudflare", Message: "no zone of this account holds " + fqdn}
}

func (c *cloudflare) records(ctx context.Context, zone, kind, name string) ([]cfRecord, error) {
	var answer cfAnswer[[]cfRecord]
	err := c.call(ctx, http.MethodGet, "/zones/"+url.PathEscape(zone)+"/dns_records?type="+kind+"&name="+url.QueryEscape(name), nil, &answer)
	return answer.Result, err
}

func (c *cloudflare) SetTXT(ctx context.Context, fqdn string, values []string) error {
	zone, err := c.zone(ctx, fqdn)
	if err != nil {
		return err
	}
	existing, err := c.records(ctx, zone, "TXT", fqdn)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, r := range existing {
		have[r.Content] = true
	}
	for _, value := range values {
		if have[value] || have[quote(value)] {
			continue
		}
		record := cfRecord{Type: "TXT", Name: fqdn, Content: value, TTL: 120}
		if err := c.call(ctx, http.MethodPost, "/zones/"+url.PathEscape(zone)+"/dns_records", record, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *cloudflare) ClearTXT(ctx context.Context, fqdn string) error {
	zone, err := c.zone(ctx, fqdn)
	if err != nil {
		return err
	}
	existing, err := c.records(ctx, zone, "TXT", fqdn)
	if err != nil {
		return err
	}
	for _, r := range existing {
		if err := c.call(ctx, http.MethodDelete, "/zones/"+url.PathEscape(zone)+"/dns_records/"+url.PathEscape(r.ID), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func (c *cloudflare) MultipleTXT() bool { return true }

func (c *cloudflare) SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error {
	zone, err := c.zone(ctx, host)
	if err != nil {
		return err
	}
	for _, a := range addressRecords(v4, v6) {
		kind, addr := a.kind, a.value
		existing, err := c.records(ctx, zone, kind, host)
		if err != nil {
			return err
		}
		if len(existing) == 0 {
			// TTL 1 is "automatic" at Cloudflare.
			if err := c.call(ctx, http.MethodPost, "/zones/"+url.PathEscape(zone)+"/dns_records", cfRecord{Type: kind, Name: host, Content: addr, TTL: 1}, nil); err != nil {
				return err
			}
			continue
		}
		for _, r := range existing {
			// Only the address changes; proxying and TTL stay as set.
			patch := map[string]string{"content": addr}
			if err := c.call(ctx, http.MethodPatch, "/zones/"+url.PathEscape(zone)+"/dns_records/"+url.PathEscape(r.ID), patch, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
