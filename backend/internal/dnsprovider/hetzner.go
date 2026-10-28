package dnsprovider

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"net/url"
)

// hetzner speaks the DNS part of the Hetzner Cloud API (Hetzner Console),
// which replaced the old dns.hetzner.com API. The token is a project API
// token with read and write rights.
type hetzner struct {
	api   *httpAPI
	token string
}

type hetznerRecord struct {
	Value string `json:"value"`
}

func (h *hetzner) call(ctx context.Context, method, path string, body, out any) (int, error) {
	header := http.Header{"Authorization": {"Bearer " + h.token}}
	status, data, err := h.api.do(ctx, "Hetzner", method, h.api.base+path, header, body, out)
	if apiErr, ok := err.(*APIError); ok {
		var answer struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if decodeJSON(data, &answer) == nil && answer.Error.Message != "" {
			apiErr.Message = answer.Error.Message
		}
	}
	return status, err
}

func (h *hetzner) zone(ctx context.Context, fqdn string) (int64, string, error) {
	for _, name := range candidates(fqdn) {
		var answer struct {
			Zones []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"zones"`
		}
		if _, err := h.call(ctx, http.MethodGet, "/zones?name="+url.QueryEscape(name), nil, &answer); err != nil {
			return 0, "", err
		}
		if len(answer.Zones) > 0 {
			return answer.Zones[0].ID, answer.Zones[0].Name, nil
		}
	}
	return 0, "", &APIError{Provider: "Hetzner", Message: "no zone of this project holds " + fqdn}
}

func (h *hetzner) set(ctx context.Context, fqdn, kind string, values []string) error {
	id, name, err := h.zone(ctx, fqdn)
	if err != nil {
		return err
	}
	records := make([]hetznerRecord, 0, len(values))
	for _, value := range values {
		records = append(records, hetznerRecord{Value: value})
	}
	sub := relative(fqdn, name)
	path := fmt.Sprintf("/zones/%d/rrsets/%s/%s", id, url.PathEscape(sub), kind)
	status, err := h.call(ctx, http.MethodGet, path, nil, nil)
	var apiErr *APIError
	switch {
	case status == http.StatusNotFound && errors.As(err, &apiErr):
		body := map[string]any{"name": sub, "type": kind, "ttl": 60, "records": records}
		_, err = h.call(ctx, http.MethodPost, fmt.Sprintf("/zones/%d/rrsets", id), body, nil)
		return err
	case err != nil:
		return err
	}
	_, err = h.call(ctx, http.MethodPost, path+"/actions/set_records", map[string]any{"records": records}, nil)
	return err
}

func (h *hetzner) SetTXT(ctx context.Context, fqdn string, values []string) error {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, quote(value))
	}
	return h.set(ctx, fqdn, "TXT", quoted)
}

func (h *hetzner) ClearTXT(ctx context.Context, fqdn string) error {
	id, name, err := h.zone(ctx, fqdn)
	if err != nil {
		return err
	}
	status, err := h.call(ctx, http.MethodDelete, fmt.Sprintf("/zones/%d/rrsets/%s/TXT", id, url.PathEscape(relative(fqdn, name))), nil, nil)
	if status == http.StatusNotFound {
		return nil
	}
	return err
}

func (h *hetzner) MultipleTXT() bool { return true }

func (h *hetzner) SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error {
	for _, r := range addressRecords(v4, v6) {
		if err := h.set(ctx, host, r.kind, []string{r.value}); err != nil {
			return err
		}
	}
	return nil
}
