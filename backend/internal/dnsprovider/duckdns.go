package dnsprovider

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// duckdns speaks the update API of DuckDNS. A name holds one TXT record
// only, and names below "<name>.duckdns.org" share the records of it.
type duckdns struct {
	api   *httpAPI
	token string
}

// subdomain returns "nas" for "nas.duckdns.org" or "_acme-challenge.nas.duckdns.org".
func duckSubdomain(fqdn string) (string, error) {
	rest, ok := strings.CutSuffix(fqdn, ".duckdns.org")
	if !ok || rest == "" {
		return "", &APIError{Provider: "DuckDNS", Message: fqdn + " is not a name under duckdns.org"}
	}
	labels := strings.Split(rest, ".")
	return labels[len(labels)-1], nil
}

func (d *duckdns) update(ctx context.Context, fqdn string, params url.Values) error {
	sub, err := duckSubdomain(fqdn)
	if err != nil {
		return err
	}
	params.Set("domains", sub)
	params.Set("token", d.token)
	_, body, err := d.api.do(ctx, "DuckDNS", http.MethodGet, d.api.base+"?"+params.Encode(), nil, nil, nil)
	if err != nil {
		return err
	}
	if !strings.HasPrefix(strings.TrimSpace(string(body)), "OK") {
		return &APIError{Provider: "DuckDNS", Message: "the update was refused (KO): check the token and the name"}
	}
	return nil
}

func (d *duckdns) SetTXT(ctx context.Context, fqdn string, values []string) error {
	if len(values) == 0 {
		return d.ClearTXT(ctx, fqdn)
	}
	return d.update(ctx, fqdn, url.Values{"txt": {values[len(values)-1]}, "verbose": {"true"}})
}

func (d *duckdns) ClearTXT(ctx context.Context, fqdn string) error {
	return d.update(ctx, fqdn, url.Values{"txt": {"removed"}, "clear": {"true"}, "verbose": {"true"}})
}

func (d *duckdns) MultipleTXT() bool { return false }

func (d *duckdns) SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error {
	params := url.Values{"verbose": {"true"}}
	if v4.IsValid() {
		params.Set("ip", v4.Unmap().String())
	}
	if v6.IsValid() {
		params.Set("ipv6", v6.String())
	}
	if len(params) == 1 {
		return nil
	}
	return d.update(ctx, host, params)
}

// CombinedUpdate: a missing address may be taken from the request.
func (d *duckdns) CombinedUpdate() bool { return true }
