// Package ipv64fake is an in-memory IPv64.net API v2 for tests, built
// after the shapes of https://ipv64.net/api/v2/openapi.json: the envelope
// {data, meta}, problem+json errors (RFC 9457), RateLimit headers, 202 for
// background changes, and bodies checked field by field (an unknown field
// or a value outside an enum is a 422, as a strict server would answer).
// It covers what TitanOS still uses of the API (account, domains and
// records, healthchecks, integrations). Only test code imports it.
package ipv64fake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Key is the API key the fake accepts; OtherKey a second key of the account.
const (
	Key      = "acct-key-0001"
	OtherKey = "acct-key-0002"
)

// Fake is the state of one account.
type Fake struct {
	Server *httptest.Server
	mu     sync.Mutex
	Calls  []string
	// Remaining is sent as RateLimit-Remaining (default 299).
	Remaining int
	// PendingServer is how many GETs of a new CDN still show
	// server_assignment as pending.
	PendingServer int

	Domains      map[string]map[string]any
	Records      map[string][]map[string]any
	Checks       map[string]map[string]any
	Integrations []map[string]any
	CDNs         map[string]map[string]any
	next         int
	pending      map[string]int
}

// New starts a fake with a domain nas.ipv64.de holding an A record.
func New(t testing.TB) *Fake {
	f := &Fake{Remaining: 299, Domains: map[string]map[string]any{}, Records: map[string][]map[string]any{}, Checks: map[string]map[string]any{},
		CDNs: map[string]map[string]any{}, next: 100, pending: map[string]int{}}
	f.Integrations = []map[string]any{{"id": 0, "type": "account_email", "name": "E-Mail", "status": "active", "options": map[string]any{}}}
	f.addDomain("nas.ipv64.de", "dyndns")
	f.addRecord("nas.ipv64.de", "A", "", "203.0.113.10")
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Server.Close)
	return f
}

func (f *Fake) id(prefix string, n ...int) string {
	f.next++
	size := 32
	if len(n) > 0 {
		size = n[0]
	}
	return fmt.Sprintf("%s%0*d", prefix, size-len(prefix), f.next)
}

func (f *Fake) addDomain(name, kind string) map[string]any {
	f.next++
	d := map[string]any{"id": f.next, "name": name, "kind": kind, "zone": name[strings.Index(name, ".")+1:], "wildcard": false, "ignore": "none", "ipv6_prefix": nil, "active": true, "update_token": "update-" + name, "update_count": 0}
	f.Domains[name] = d
	return d
}

func (f *Fake) addRecord(domain, kind, prefix, content string) map[string]any {
	f.next++
	fqdn := domain
	if prefix != "" {
		fqdn = prefix + "." + domain
	}
	r := map[string]any{"id": f.next, "domain_id": f.Domains[domain]["id"], "domain": domain, "prefix": prefix, "fqdn": fqdn, "type": kind, "content": content, "ttl": 60, "state": "active", "pending_dns": false, "token": fmt.Sprintf("rec%029d", f.next)}
	f.Records[domain] = append(f.Records[domain], r)
	return r
}

// Called tells whether a request "METHOD /path" was made.
func (f *Fake) Called(call string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Contains(f.Calls, call)
}

func problem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"type": "https://ipv64.net/api/v2/errors#" + code, "title": http.StatusText(status), "status": status, "detail": detail, "code": code})
}

func data(w http.ResponseWriter, status int, v any, meta ...any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	out := map[string]any{"data": v}
	if len(meta) > 0 {
		out["meta"] = meta[0]
	}
	json.NewEncoder(w).Encode(out)
}

// body reads a JSON body and refuses fields outside allowed and values
// outside the enums given as "field=a|b|c".
func body(w http.ResponseWriter, r *http.Request, allowed ...string) (map[string]any, bool) {
	out := map[string]any{}
	raw, _ := io.ReadAll(r.Body)
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			problem(w, 400, "invalid_json", "The body is not valid JSON.")
			return nil, false
		}
	}
	names := map[string][]string{}
	for _, a := range allowed {
		name, enum, _ := strings.Cut(a, "=")
		names[name] = nil
		if enum != "" {
			names[name] = strings.Split(enum, "|")
		}
	}
	for key, value := range out {
		enum, ok := names[key]
		if !ok {
			problem(w, 422, "invalid_field", "Unknown field '"+key+"'.")
			return nil, false
		}
		if enum != nil {
			text, _ := value.(string)
			if !slices.Contains(enum, text) {
				problem(w, 422, "invalid_field", "Field '"+key+"' has a value outside its enum.")
				return nil, false
			}
		}
	}
	return out, true
}

func publicCheck(c map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range c {
		out[k] = v
	}
	return out
}

func (f *Fake) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api/v2")
	f.Calls = append(f.Calls, r.Method+" "+path)
	auth := r.Header.Get("Authorization")
	if auth != "Bearer "+Key && auth != "Bearer "+OtherKey {
		problem(w, 401, "invalid_token", "The API key is wrong, expired or the account is not active.")
		return
	}
	w.Header().Set("RateLimit-Limit", "300")
	w.Header().Set("RateLimit-Remaining", strconv.Itoa(f.Remaining))
	parts := strings.Split(strings.Trim(path, "/"), "/")
	route := r.Method + " /" + strings.Join(pattern(parts), "/")
	p := func(i int) string { return parts[i] }
	switch route {
	case "GET /me":
		keyID, keyName := 1, "titanos"
		if auth == "Bearer "+OtherKey {
			keyID, keyName = 2, "laptop"
		}
		data(w, 200, map[string]any{"id": 7, "email": "owner@example.org", "username": "owner", "plan": map[string]any{"id": 1, "name": "Free", "valid_until": nil},
			"usage":              map[string]any{"dyndns_domains": map[string]any{"used": len(f.Domains), "limit": 3}, "cdn_traffic_gb": map[string]any{"used": 1.5, "limit": 10}, "vpn_traffic_gb": map[string]any{"used": 0, "limit": 5, "limited": false}},
			"log_retention_days": 7, "auth": map[string]any{"type": "api_key", "key_id": keyID, "key_name": keyName}})
	case "GET /domains":
		var list []any
		for _, d := range f.Domains {
			list = append(list, d)
		}
		data(w, 200, list)
	case "POST /domains":
		b, ok := body(w, r, "name", "own")
		if !ok {
			return
		}
		name, _ := b["name"].(string)
		if _, taken := f.Domains[name]; taken {
			problem(w, 409, "domain_taken", "The domain name is taken.")
			return
		}
		kind := "dyndns"
		if b["own"] == true {
			kind = "own"
		}
		d := publicCheck(f.addDomain(name, kind))
		d["records"] = []any{}
		data(w, 201, d)
	case "GET /domains/*":
		d, ok := f.Domains[p(1)]
		if !ok {
			problem(w, 404, "domain_not_found", "Domain not found.")
			return
		}
		out := publicCheck(d)
		out["records"] = f.Records[p(1)]
		data(w, 200, out)
	case "PATCH /domains/*":
		b, ok := body(w, r, "wildcard", "ignore=none|ipv4|ipv6", "ipv6_prefix")
		if !ok {
			return
		}
		d := f.Domains[p(1)]
		for k, v := range b {
			d[k] = v
		}
		data(w, 200, d)
	case "POST /domains/*/dyndns":
		b, ok := body(w, r, "ipv4", "ipv6", "prefix")
		if !ok {
			return
		}
		prefix, _ := b["prefix"].(string)
		ip, _ := b["ipv4"].(string)
		if ip == "" {
			ip = "198.51.100.200"
		}
		f.addRecord(p(1), "A", prefix, ip)
		data(w, 200, map[string]any{"domain": p(1), "prefix": prefix, "changed": true, "results": []any{map[string]any{"ip": ip, "type": "A", "result": "created"}}})
	case "GET /domains/*/records":
		data(w, 200, f.Records[p(1)])
	case "POST /domains/*/records":
		b, ok := body(w, r, "type=A|AAAA|CNAME|MX|TXT|NS|SRV|TLSA|CAA", "prefix", "content")
		if !ok {
			return
		}
		prefix, _ := b["prefix"].(string)
		data(w, 201, f.addRecord(p(1), b["type"].(string), prefix, b["content"].(string)))
	case "GET /records/*", "PATCH /records/*", "DELETE /records/*":
		for domain, list := range f.Records {
			for i, rec := range list {
				if fmt.Sprint(rec["id"]) == p(1) || rec["token"] == p(1) {
					switch r.Method {
					case "DELETE":
						f.Records[domain] = slices.Delete(list, i, i+1)
						w.WriteHeader(204)
					case "PATCH":
						b, ok := body(w, r, "type=A|AAAA|CNAME|MX|TXT|NS|SRV|TLSA|CAA", "prefix", "content")
						if !ok {
							return
						}
						for k, v := range b {
							rec[k] = v
						}
						data(w, 200, rec)
					default:
						data(w, 200, rec)
					}
					return
				}
			}
		}
		problem(w, 404, "record_not_found", "Record not found.")
	case "GET /healthchecks":
		list := []any{}
		for _, c := range f.Checks {
			list = append(list, c)
		}
		data(w, 200, list)
	case "POST /healthchecks":
		b, ok := body(w, r, "name", "type=push|icmp|tcp|http|keyword", "integrations", "notify_on_down", "notify_on_up", "period_seconds", "grace_seconds", "target", "prefer_ipv4", "port", "url", "verify_tls", "keyword", "interval_seconds", "retry_interval_seconds", "failures_before_alarm")
		if !ok {
			return
		}
		token := f.id("hc")
		c := map[string]any{"token": token, "name": b["name"], "type": b["type"], "status": "new", "notify": map[string]any{"on_down": true, "on_up": true, "integrations": b["integrations"]}, "pings_total": 0, "failed_in_row": 0}
		if b["type"] == "push" || b["type"] == nil {
			c["type"] = "push"
			c["push"] = map[string]any{"url": "https://ipv64.net/health/" + token, "period_seconds": b["period_seconds"], "grace_seconds": b["grace_seconds"], "expected_by": nil}
		} else {
			c["check"] = map[string]any{"target": b["target"], "port": b["port"], "url": b["url"], "keyword": b["keyword"], "interval_seconds": b["interval_seconds"]}
		}
		f.Checks[token] = c
		data(w, 201, c)
	case "GET /healthchecks/*", "PATCH /healthchecks/*", "DELETE /healthchecks/*":
		c, ok := f.Checks[p(1)]
		if !ok {
			problem(w, 404, "healthcheck_not_found", "Healthcheck not found.")
			return
		}
		switch r.Method {
		case "DELETE":
			delete(f.Checks, p(1))
			w.WriteHeader(204)
		case "PATCH":
			b, ok := body(w, r, "name", "type=push|icmp|tcp|http|keyword", "integrations", "notify_on_down", "notify_on_up", "period_seconds", "grace_seconds", "target", "prefer_ipv4", "port", "url", "verify_tls", "keyword", "interval_seconds", "retry_interval_seconds", "failures_before_alarm")
			if !ok {
				return
			}
			if name, ok := b["name"]; ok {
				c["name"] = name
			}
			data(w, 200, c)
		default:
			out := publicCheck(c)
			out["recent"] = []any{map[string]any{"id": 5, "time": "2026-10-28T08:00:00Z", "status": "up", "message": "ping", "latency_ms": nil, "payload": nil}}
			data(w, 200, out)
		}
	case "POST /healthchecks/*/pause", "POST /healthchecks/*/resume", "POST /healthchecks/*/rotate-token":
		c := f.Checks[p(1)]
		switch p(2) {
		case "pause":
			c["status"] = "paused"
		case "resume":
			c["status"] = "new"
		case "rotate-token":
			delete(f.Checks, p(1))
			token := f.id("hr")
			c["token"] = token
			f.Checks[token] = c
		}
		data(w, 200, c)
	case "POST /healthchecks/*/test-notification":
		data(w, 200, map[string]any{"sent": true})
	case "GET /integrations":
		if r.URL.Query().Get("include_secrets") == "true" {
			problem(w, 400, "invalid_parameter", "TitanOS must never ask for secrets")
			return
		}
		data(w, 200, f.Integrations)
	default:
		problem(w, 404, "not_found", "The fake knows no "+route)
	}
}

func pattern(parts []string) []string {
	words := []string{"me", "logs", "api-keys", "domains", "rotate-token", "reset", "dyndns", "records", "healthchecks", "pause", "resume", "events", "incidents", "stats", "test-notification",
		"integrations", "test", "failover", "policies", "failover", "fallback", "monitors", "rules", "status-pages", "offline-pages", "icon", "cdn", "proxy", "portmapper", "enable", "disable",
		"redeploy", "reset-traffic", "move", "renew-certificate", "mappings", "access-list", "cloud-routers", "gateways", "routes", "firewall", "public-ips", "rotate-keys", "peers", "keys", "config",
		"honeypot", "list-token"}
	out := make([]string, len(parts))
	for i, part := range parts {
		if i == 0 || slices.Contains(words, part) {
			out[i] = part
		} else {
			out[i] = "*"
		}
	}
	// "failover/policies" keeps both words.
	return out
}
