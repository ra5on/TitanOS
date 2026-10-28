package dnsprovider

import (
	"context"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
)

// dyndns2 sends the update of the dyndns2 protocol, or fills in an address
// with placeholders as routers do (Fritz!Box style): <domain>, <ipaddr>,
// <ip6addr>, <username>, <passwd>.
type dyndns2 struct {
	api      *httpAPI
	address  string
	user     string
	password string
}

// dyndns2OK are the answers that mean the address is now set.
var dyndns2OK = []string{"good", "nochg"}

// updateURL builds the address of the request.
func (d *dyndns2) updateURL(host string, v4, v6 netip.Addr) string {
	ip4, ip6 := "", ""
	if v4.IsValid() {
		ip4 = v4.Unmap().String()
	}
	if v6.IsValid() {
		ip6 = v6.String()
	}
	if strings.Contains(d.address, "<") {
		address := d.address
		// "myip=<ipaddr>,<ip6addr>" loses the comma when one is missing.
		if ip6 == "" {
			address = strings.ReplaceAll(address, ",<ip6addr>", "")
		}
		if ip4 == "" {
			address = strings.ReplaceAll(address, "<ipaddr>,", "")
		}
		return strings.NewReplacer(
			"<domain>", url.QueryEscape(host),
			"<ipaddr>", url.QueryEscape(ip4),
			"<ip6addr>", url.QueryEscape(ip6),
			"<username>", url.QueryEscape(d.user),
			"<passwd>", url.QueryEscape(d.password),
		).Replace(address)
	}
	parsed, _ := url.Parse(d.address)
	query := parsed.Query()
	query.Set("hostname", host)
	var ips []string
	for _, ip := range []string{ip4, ip6} {
		if ip != "" {
			ips = append(ips, ip)
		}
	}
	query.Set("myip", strings.Join(ips, ","))
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func (d *dyndns2) SetAddresses(ctx context.Context, host string, v4, v6 netip.Addr) error {
	if !v4.IsValid() && !v6.IsValid() {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.updateURL(host, v4, v6), nil)
	if err != nil {
		return &APIError{Provider: "dyndns2", Message: "the update address is not usable"}
	}
	if d.user != "" || d.password != "" {
		req.SetBasicAuth(d.user, d.password)
	}
	header := http.Header{"Authorization": req.Header.Values("Authorization")}
	if len(header["Authorization"]) == 0 {
		header = nil
	}
	_, body, err := d.api.do(ctx, "dyndns2", http.MethodGet, req.URL.String(), header, nil, nil)
	if err != nil {
		return err
	}
	// One line per host name; every one must be fine.
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		word, _, _ := strings.Cut(strings.TrimSpace(line), " ")
		ok := false
		for _, good := range dyndns2OK {
			ok = ok || strings.EqualFold(word, good)
		}
		if !ok {
			if word == "" {
				word = "empty answer"
			}
			return &APIError{Provider: "dyndns2", Message: "the service answered " + dyndns2Explain(word)}
		}
	}
	return nil
}

// dyndns2Explain adds what the codes of the protocol mean.
func dyndns2Explain(code string) string {
	switch strings.ToLower(code) {
	case "badauth":
		return code + " (user name or password wrong)"
	case "nohost":
		return code + " (host name unknown to this account)"
	case "notfqdn":
		return code + " (not a full host name)"
	case "abuse":
		return code + " (blocked for too many updates)"
	case "911", "dnserr":
		return code + " (trouble at the service, try later)"
	}
	if len(code) > 40 {
		code = code[:40]
	}
	return code
}

// CombinedUpdate: a missing address may be taken from the request.
func (d *dyndns2) CombinedUpdate() bool { return true }
