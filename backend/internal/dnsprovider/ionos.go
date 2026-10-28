package dnsprovider

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
)

// ionos speaks the DNS API of IONOS (developer.hosting.ionos.de) with an API
// key "prefix.secret".
type ionos struct {
	api *httpAPI
	key string
}

type ionosRecord struct {
	ID       string `json:"id,omitempty"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Prio     int    `json:"prio"`
	Disabled bool   `json:"disabled"`
}

func (i *ionos) call(ctx context.Context, method, path string, body, out any) error {
	header := http.Header{"X-Api-Key": {i.key}}
	_, data, err := i.api.do(ctx, "IONOS", method, i.api.base+path, header, body, out)
	if apiErr, ok := err.(*APIError); ok {
		var answer []struct {
			Message string `json:"message"`
		}
		if decodeJSON(data, &answer) == nil && len(answer) > 0 && answer[0].Message != "" {
			apiErr.Message = answer[0].Message
		}
	}
	return err
}

// zone finds the zone with the longest name that holds fqdn.
func (i *ionos) zone(ctx context.Context, fqdn string) (string, error) {
	var zones []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := i.call(ctx, http.MethodGet, "/zones", nil, &zones); err != nil {
		return "", err
	}
	best, bestName := "", ""
	for _, z := range zones {
		if inZone(fqdn, z.Name) && len(z.Name) > len(bestName) {
			best, bestName = z.ID, z.Name
		}
	}
	if best == "" {
		return "", &APIError{Provider: "IONOS", Message: "no zone of this account holds " + fqdn}
	}
	return best, nil
}

func (i *ionos) records(ctx context.Context, zone, kind, name string) ([]ionosRecord, error) {
	var answer struct {
		Records []ionosRecord `json:"records"`
	}
	err := i.call(ctx, http.MethodGet, "/zones/"+url.PathEscape(zone)+"?recordName="+url.QueryEscape(name)+"&recordType="+kind, nil, &answer)
	// The filter matches by name; the type is checked again here.
	var out []ionosRecord
	for _, r := range answer.Records {
		if r.Type == kind && r.Name == name {
			out = append(out, r)
		}
	}
	return out, err
}

func (i *ionos) SetTXT(ctx context.Context, fqdn string, values []string) error {
	zone, err := i.zone(ctx, fqdn)
	if err != nil {
		return err
	}
	existing, err := i.records(ctx, zone, "TXT", fqdn)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, r := range existing {
		have[r.Content] = true
	}
	var add []ionosRecord
	for _, value := range values {
		if !have[value] && !have[quote(value)] {
			add = append(add, ionosRecord{Name: fqdn, Type: "TXT", Content: value, TTL: 60})
		}
	}
	if len(add) == 0 {
		return nil
	}
	return i.call(ctx, http.MethodPost, "/zones/"+url.PathEscape(zone)+"/records", add, nil)
}

func (i *ionos) ClearTXT(ctx context.Context, fqdn string) error {
	zone, err := i.zone(ctx, fqdn)
	if err != nil {
		return err
	}
	existing, err := i.records(ctx, zone, "TXT", fqdn)
	if err != nil {
		return err
	}
	for _, r := range existing {
		if err := i.call(ctx, http.MethodDelete, "/zones/"+url.PathEscape(zone)+"/records/"+url.PathEscape(r.ID), nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func (i *ionos) MultipleTXT() bool { return true }

func (i *ionos) SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error {
	zone, err := i.zone(ctx, host)
	if err != nil {
		return err
	}
	for _, a := range addressRecords(v4, v6) {
		existing, err := i.records(ctx, zone, a.kind, host)
		if err != nil {
			return err
		}
		if len(existing) == 0 {
			if err := i.call(ctx, http.MethodPost, "/zones/"+url.PathEscape(zone)+"/records", []ionosRecord{{Name: host, Type: a.kind, Content: a.value, TTL: 60}}, nil); err != nil {
				return err
			}
			continue
		}
		for _, r := range existing {
			update := map[string]any{"content": a.value, "ttl": r.TTL, "disabled": false}
			if err := i.call(ctx, http.MethodPut, "/zones/"+url.PathEscape(zone)+"/records/"+url.PathEscape(r.ID), update, nil); err != nil {
				return err
			}
		}
	}
	return nil
}
