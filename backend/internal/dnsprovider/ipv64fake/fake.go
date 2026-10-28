// Package ipv64fake is an in-memory IPv64.net API v2 for tests, built
// after the shapes of https://ipv64.net/api/v2/openapi.json: the envelope
// {data, meta}, problem+json errors (RFC 9457), RateLimit headers, 202 for
// background changes, and bodies checked field by field (an unknown field
// or a value outside an enum is a 422, as a strict server would answer).
// Only test code imports it.
package ipv64fake

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
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
	Policies     map[string]map[string]any
	StatusPages  map[int]map[string]any
	Offline      map[string]map[string]any
	CDNs         map[string]map[string]any
	Routers      map[string]map[string]any
	Gateways     map[string]map[string]any
	Peers        map[string]map[string]any
	Keys         []map[string]any
	Blocklist    string
	next         int
	pending      map[string]int
}

// New starts a fake with a domain nas.ipv64.de holding an A record.
func New(t testing.TB) *Fake {
	f := &Fake{Remaining: 299, Domains: map[string]map[string]any{}, Records: map[string][]map[string]any{}, Checks: map[string]map[string]any{},
		Policies: map[string]map[string]any{}, StatusPages: map[int]map[string]any{}, Offline: map[string]map[string]any{}, CDNs: map[string]map[string]any{},
		Routers: map[string]map[string]any{}, Gateways: map[string]map[string]any{}, Peers: map[string]map[string]any{}, next: 100, pending: map[string]int{}}
	f.Integrations = []map[string]any{{"id": 0, "type": "account_email", "name": "E-Mail", "status": "active", "options": map[string]any{}}}
	f.Keys = []map[string]any{{"id": 1, "name": "titanos", "token_hint": "acct…0001", "expired": false, "uses": 3}, {"id": 2, "name": "laptop", "token_hint": "acct…0002", "expired": false, "uses": 0}}
	f.addDomain("nas.ipv64.de", "dyndns")
	f.addRecord("nas.ipv64.de", "A", "", "203.0.113.10")
	f.Blocklist = "# v64\n198.51.100.7\n203.0.113.0/24\n10.0.0.1\n"
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
	if r.URL.Path == "/lists/token123/ipv64_blocklist_v64.txt" {
		w.Header().Set("X-IPv64-List", "daily")
		io.WriteString(w, f.Blocklist)
		return
	}
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
	case "GET /me/logs":
		before, _ := strconv.Atoi(r.URL.Query().Get("before"))
		if before == 0 {
			data(w, 200, []any{map[string]any{"id": 20, "domain_id": nil, "title": "DynDNS", "message": "update nas.ipv64.de", "time": "2026-10-28T08:00:00+01:00"}}, map[string]any{"limit": 50, "next_before": 20, "retention_days": 7})
		} else {
			data(w, 200, []any{map[string]any{"id": 10, "domain_id": 1, "title": "Login", "message": "web login", "time": "2026-10-27T08:00:00+01:00"}}, map[string]any{"limit": 50, "next_before": nil, "retention_days": 7})
		}
	case "GET /api-keys":
		data(w, 200, f.Keys)
	case "POST /api-keys":
		b, ok := body(w, r, "name", "expires_in")
		if !ok {
			return
		}
		f.next++
		k := map[string]any{"id": f.next, "name": b["name"], "token_hint": "new…key", "expired": false, "uses": 0}
		f.Keys = append(f.Keys, k)
		created := publicCheck(k)
		created["token"] = "brand-new-full-key"
		data(w, 201, created)
	case "DELETE /api-keys/*":
		id, _ := strconv.Atoi(p(1))
		if (id == 1 && auth == "Bearer "+Key) || (id == 2 && auth == "Bearer "+OtherKey) {
			problem(w, 409, "cannot_delete_current_key", "A key cannot delete itself.")
			return
		}
		f.Keys = slices.DeleteFunc(f.Keys, func(k map[string]any) bool { return k["id"] == id })
		w.WriteHeader(204)
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
	case "DELETE /domains/*":
		delete(f.Domains, p(1))
		delete(f.Records, p(1))
		w.WriteHeader(204)
	case "POST /domains/*/rotate-token":
		d := f.Domains[p(1)]
		d["update_token"] = "rotated-" + p(1)
		data(w, 200, d)
	case "POST /domains/*/reset":
		f.Records[p(1)] = nil
		out := publicCheck(f.Domains[p(1)])
		out["records"] = []any{}
		data(w, 200, out)
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
	case "GET /healthchecks/*/events":
		data(w, 200, []any{map[string]any{"id": 9, "time": "2026-10-28T08:00:00Z", "status": "down", "message": "missed", "latency_ms": nil, "payload": nil}}, map[string]any{"limit": 50, "has_more": false, "next_before": nil, "next_after": 9})
	case "GET /healthchecks/*/incidents":
		data(w, 200, []any{map[string]any{"id": 1, "status": "closed", "created_at": "2026-10-27T08:00:00Z", "closed_at": "2026-10-27T08:30:00Z", "events": []any{map[string]any{"time": "2026-10-27T08:00:00Z", "kind": "down", "message": "missed"}}}})
	case "GET /healthchecks/*/stats":
		rng := r.URL.Query().Get("range")
		if !slices.Contains([]string{"1h", "24h", "7d", "30d"}, rng) {
			problem(w, 400, "invalid_parameter", "range")
			return
		}
		data(w, 200, map[string]any{"range": rng, "measurements": 2, "availability_percent": 99.5, "latency_ms": map[string]any{"avg": 12.5, "min": 10, "max": 15}, "bucket_seconds": 3600,
			"series": []any{map[string]any{"time": "2026-10-28T07:00:00Z", "measurements": 1, "availability_percent": 100, "latency_avg_ms": 10, "latency_max_ms": 10}, map[string]any{"time": "2026-10-28T08:00:00Z", "measurements": 1, "availability_percent": 99, "latency_avg_ms": 15, "latency_max_ms": 15}}})
	case "GET /integrations":
		if r.URL.Query().Get("include_secrets") == "true" {
			problem(w, 400, "invalid_parameter", "TitanOS must never ask for secrets")
			return
		}
		data(w, 200, f.Integrations)
	case "POST /integrations":
		b, ok := body(w, r, "type=telegram|gotify|ntfy|pushover|discord|webhook|email|sms|app", "name", "default", "chat_id", "api_token", "server_url", "app_token", "user_token", "device", "priority", "priority_down", "priority_up", "webhook_url", "ping_user", "ping_group", "url_down", "url_up", "email", "number", "country_code", "device_token")
		if !ok {
			return
		}
		f.next++
		i := map[string]any{"id": f.next, "type": b["type"], "name": b["name"], "status": "active", "default": b["default"], "options": map[string]any{"chat_id": b["chat_id"], "api_token": "1234…abcd"}, "uses": 0}
		f.Integrations = append(f.Integrations, i)
		data(w, 201, i)
	case "PATCH /integrations/*":
		b, ok := body(w, r, "name", "default")
		if !ok {
			return
		}
		for _, i := range f.Integrations {
			if fmt.Sprint(i["id"]) == p(1) {
				for k, v := range b {
					i[k] = v
				}
				data(w, 200, i)
				return
			}
		}
		problem(w, 404, "integration_not_found", "Integration not found.")
	case "DELETE /integrations/*":
		f.Integrations = slices.DeleteFunc(f.Integrations, func(i map[string]any) bool { return fmt.Sprint(i["id"]) == p(1) })
		w.WriteHeader(204)
	case "POST /integrations/*/test":
		data(w, 200, map[string]any{"sent": true})
	case "GET /failover/policies":
		list := []any{}
		for _, pol := range f.Policies {
			list = append(list, pol)
		}
		data(w, 200, list)
	case "POST /failover/policies":
		b, ok := body(w, r, "name", "mode=auto_failover|auto_failover_and_fallback|manual", "fallback_requires_all", "linking=and|or", "notifications_enabled", "integrations")
		if !ok {
			return
		}
		id := f.id("po", 16)
		pol := map[string]any{"id": id, "name": b["name"], "status": "pending", "manual": false, "mode": b["mode"], "linking": b["linking"], "notifications_enabled": b["notifications_enabled"], "integrations": b["integrations"], "monitors_down": 0, "monitors": []any{}, "rules": []any{}}
		f.Policies[id] = pol
		data(w, 201, pol)
	case "GET /failover/policies/*", "PATCH /failover/policies/*", "DELETE /failover/policies/*":
		pol, ok := f.Policies[p(2)]
		if !ok {
			problem(w, 404, "policy_not_found", "Failover policy not found.")
			return
		}
		switch r.Method {
		case "DELETE":
			if pol["status"] == "failed_over" {
				problem(w, 409, "failover_active", "A DNS failover is active.")
				return
			}
			delete(f.Policies, p(2))
			w.WriteHeader(204)
		case "PATCH":
			b, ok := body(w, r, "name", "mode=auto_failover|auto_failover_and_fallback|manual", "fallback_requires_all", "linking=and|or", "notifications_enabled", "integrations")
			if !ok {
				return
			}
			for k, v := range b {
				pol[k] = v
			}
			data(w, 200, pol)
		default:
			data(w, 200, pol)
		}
	case "POST /failover/policies/*/pause", "POST /failover/policies/*/resume":
		pol := f.Policies[p(2)]
		pol["status"] = map[string]string{"pause": "paused", "resume": "active"}[p(3)]
		data(w, 200, pol)
	case "POST /failover/policies/*/failover", "POST /failover/policies/*/fallback":
		pol := f.Policies[p(2)]
		pol["status"] = p(3) + "_requested"
		data(w, 202, pol)
	case "POST /failover/policies/*/monitors":
		b, ok := body(w, r, "healthcheck", "negate")
		if !ok {
			return
		}
		if _, exists := f.Checks[fmt.Sprint(b["healthcheck"])]; !exists {
			problem(w, 404, "healthcheck_not_found", "Healthcheck not found.")
			return
		}
		pol := f.Policies[p(2)]
		pol["monitors"] = append(pol["monitors"].([]any), map[string]any{"healthcheck": b["healthcheck"], "name": "check", "negate": b["negate"] == true, "healthcheck_status": "up"})
		data(w, 201, pol)
	case "DELETE /failover/policies/*/monitors/*":
		w.WriteHeader(204)
	case "POST /failover/policies/*/rules":
		b, ok := body(w, r, "record", "target_ip", "target_record", "offline_page")
		if !ok {
			return
		}
		pol := f.Policies[p(2)]
		pol["rules"] = append(pol["rules"].([]any), map[string]any{"record": b["record"], "fqdn": "nas.ipv64.de", "record_type": "A", "state": "standby", "target": map[string]any{"kind": "offline_page", "offline_page": b["offline_page"]}})
		data(w, 201, pol)
	case "DELETE /failover/policies/*/rules/*":
		w.WriteHeader(204)
	case "GET /status-pages":
		list := []any{}
		for _, s := range f.StatusPages {
			list = append(list, s)
		}
		data(w, 200, list)
	case "POST /status-pages":
		b, ok := body(w, r, "name", "description", "slug", "password", "design", "autorefresh", "healthchecks")
		if !ok {
			return
		}
		f.next++
		slug, _ := b["slug"].(string)
		if slug == "" {
			slug = "random" + strconv.Itoa(f.next)
		}
		s := map[string]any{"id": f.next, "name": b["name"], "slug": slug, "url": "https://ipv64.net/status/" + slug, "password_protected": b["password"] != nil && b["password"] != "", "design": b["design"], "healthchecks": b["healthchecks"]}
		f.StatusPages[f.next] = s
		data(w, 201, s)
	case "PATCH /status-pages/*", "DELETE /status-pages/*":
		id, _ := strconv.Atoi(p(1))
		s, ok := f.StatusPages[id]
		if !ok {
			problem(w, 404, "status_page_not_found", "Status page not found.")
			return
		}
		if r.Method == "DELETE" {
			delete(f.StatusPages, id)
			w.WriteHeader(204)
			return
		}
		b, ok := body(w, r, "name", "description", "slug", "password", "design", "autorefresh", "healthchecks")
		if !ok {
			return
		}
		for k, v := range b {
			if k != "password" {
				s[k] = v
			}
		}
		data(w, 200, s)
	case "GET /offline-pages":
		list := []any{}
		for _, o := range f.Offline {
			list = append(list, o)
		}
		data(w, 200, list)
	case "POST /offline-pages":
		b, ok := body(w, r, "name", "domain", "email", "phone", "alternative_url", "text")
		if !ok {
			return
		}
		id := f.id("op", 16)
		o := map[string]any{"id": id, "name": b["name"], "domain": b["domain"], "text": b["text"], "icon_url": nil, "views": 0, "in_use_by_policy": nil, "active": false, "preview_url": "https://ipv64.net/offline/preview/" + id}
		f.Offline[id] = o
		data(w, 201, o)
	case "PATCH /offline-pages/*", "DELETE /offline-pages/*":
		o, ok := f.Offline[p(1)]
		if !ok {
			problem(w, 404, "offline_page_not_found", "Offline page not found.")
			return
		}
		if r.Method == "DELETE" {
			delete(f.Offline, p(1))
			w.WriteHeader(204)
			return
		}
		b, ok := body(w, r, "name", "domain", "email", "phone", "alternative_url", "text")
		if !ok {
			return
		}
		for k, v := range b {
			o[k] = v
		}
		data(w, 200, o)
	case "POST /offline-pages/*/icon":
		media, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if media != "multipart/form-data" {
			problem(w, 415, "unsupported_media_type", "multipart/form-data")
			return
		}
		part, err := multipart.NewReader(r.Body, params["boundary"]).NextPart()
		if err != nil || part.FormName() != "icon" {
			problem(w, 422, "missing_file", "The file is missing in the multipart field.")
			return
		}
		o := f.Offline[p(1)]
		o["icon_url"] = "https://ipv64.net/offline/icon/" + p(1)
		data(w, 200, o)
	case "DELETE /offline-pages/*/icon":
		w.WriteHeader(204)
	case "GET /cdn":
		list := []any{}
		for _, c := range f.CDNs {
			list = append(list, f.cdnView(c, false))
		}
		data(w, 200, list)
	case "POST /cdn":
		b, ok := body(w, r, "domain")
		if !ok {
			return
		}
		for _, c := range f.CDNs {
			if c["domain"] == b["domain"] {
				problem(w, 409, "cdn_exists", "The domain is already in the CDN.")
				return
			}
		}
		id := f.id("cd")
		c := map[string]any{"id": id, "domain": b["domain"], "active": true, "dns_records_enabled": false, "server": nil,
			"proxy":      map[string]any{"state": "off", "frontend": nil, "backend": map[string]any{"scheme": "http", "port": 80}, "websocket": false, "certificate": "default", "caching": map[string]any{"enabled": false, "time_minutes": 60, "size_mb": 100}, "access_list": map[string]any{"enabled": false, "mode": "allowlist"}},
			"portmapper": map[string]any{"state": "off"}, "traffic_mb": map[string]any{"in": 0, "out": 0}, "mappings": []any{}, "access": []any{}, "records": []any{}}
		f.CDNs[id] = c
		f.pending[id] = f.PendingServer
		data(w, 201, f.cdnView(c, true))
	case "GET /cdn/*", "PATCH /cdn/*", "DELETE /cdn/*":
		c, ok := f.CDNs[p(1)]
		if !ok {
			problem(w, 404, "cdn_not_found", "CDN domain not found.")
			return
		}
		switch r.Method {
		case "DELETE":
			if len(c["records"].([]any)) > 0 {
				problem(w, 409, "records_enabled", "Switch the CDN off for all records first.")
				return
			}
			delete(f.CDNs, p(1))
			data(w, 202, f.cdnView(c, false))
		case "PATCH":
			b, ok := body(w, r, "frontend=http|https_redirect|http_and_https", "backend_scheme=http|https", "backend_port", "websocket", "caching", "access_list")
			if !ok {
				return
			}
			proxy := c["proxy"].(map[string]any)
			if b["websocket"] != nil && proxy["state"] != "on" {
				problem(w, 409, "proxy_disabled", "Switch the reverse proxy on first.")
				return
			}
			for k, v := range b {
				switch k {
				case "backend_scheme":
					proxy["backend"].(map[string]any)["scheme"] = v
				case "backend_port":
					proxy["backend"].(map[string]any)["port"] = v
				default:
					proxy[k] = v
				}
			}
			data(w, 200, f.cdnView(c, true))
		default:
			if f.pending[p(1)] > 0 {
				f.pending[p(1)]--
			} else {
				c["server"] = map[string]any{"name": "cdn1", "hostname": "cdn1.ipv64.net", "ipv4": "192.0.2.50", "ipv6": "2001:db8:cd::50"}
			}
			data(w, 200, f.cdnView(c, true))
		}
	case "POST /cdn/*/proxy/enable", "POST /cdn/*/proxy/disable", "POST /cdn/*/portmapper/enable", "POST /cdn/*/portmapper/disable":
		c := f.CDNs[p(1)]
		if c["server"] == nil {
			problem(w, 409, "cdn_server_pending", "The CDN server is still being assigned.")
			return
		}
		if p(3) == "enable" && len(c["records"].([]any)) == 0 {
			problem(w, 409, "no_records", "Route at least one record through the CDN first.")
			return
		}
		state := map[string]string{"enable": "on", "disable": "off"}[p(3)]
		c[p(2)].(map[string]any)["state"] = state
		data(w, 202, f.cdnView(c, false))
	case "POST /cdn/*/redeploy", "POST /cdn/*/move", "POST /cdn/*/renew-certificate":
		data(w, 202, f.cdnView(f.CDNs[p(1)], false))
	case "POST /cdn/*/reset-traffic":
		data(w, 200, f.cdnView(f.CDNs[p(1)], false))
	case "GET /cdn/*/records":
		c := f.CDNs[p(1)]
		var list []any
		for _, rec := range f.Records[fmt.Sprint(c["domain"])] {
			if rec["type"] == "A" || rec["type"] == "AAAA" {
				list = append(list, rec)
			}
		}
		data(w, 200, list)
	case "POST /cdn/*/records/*/enable", "POST /cdn/*/records/*/disable":
		c := f.CDNs[p(1)]
		if c["server"] == nil {
			problem(w, 409, "cdn_server_pending", "The CDN server is still being assigned.")
			return
		}
		var mode string
		if p(4) == "enable" {
			b, ok := body(w, r, "mode=full|other_family_only")
			if !ok {
				return
			}
			mode, _ = b["mode"].(string)
		}
		records := c["records"].([]any)
		if p(4) == "enable" {
			if slices.Contains(records, any(p(3))) {
				problem(w, 409, "already_enabled", "The record already runs through the CDN.")
				return
			}
			c["records"] = append(records, p(3))
		} else {
			if !slices.Contains(records, any(p(3))) {
				problem(w, 409, "not_enabled", "The record does not run through this CDN domain.")
				return
			}
			c["records"] = slices.DeleteFunc(records, func(x any) bool { return x == p(3) })
		}
		for _, rec := range f.Records[fmt.Sprint(c["domain"])] {
			if rec["token"] == p(3) {
				rec["state"] = map[string]string{"enable": "cdn_proxied", "disable": "active"}[p(4)]
				_ = mode
				data(w, 202, rec)
				return
			}
		}
		problem(w, 404, "record_not_found", "Record not found.")
	case "GET /cdn/*/mappings":
		data(w, 200, f.CDNs[p(1)]["mappings"])
	case "POST /cdn/*/mappings":
		b, ok := body(w, r, "record", "target_port", "transport=tcp|udp", "name", "listen_port")
		if !ok {
			return
		}
		listen := 50001
		if v, ok := b["listen_port"].(float64); ok {
			listen = int(v)
		}
		m := map[string]any{"id": f.id("mp", 12), "name": b["name"], "listen_port": listen, "transport": b["transport"], "target_port": b["target_port"], "record": b["record"], "fqdn": "nas.ipv64.de"}
		c := f.CDNs[p(1)]
		c["mappings"] = append(c["mappings"].([]any), m)
		data(w, 201, m)
	case "DELETE /cdn/*/mappings/*":
		w.WriteHeader(204)
	case "GET /cdn/*/access-list":
		data(w, 200, f.CDNs[p(1)]["access"])
	case "POST /cdn/*/access-list":
		b, ok := body(w, r, "address")
		if !ok {
			return
		}
		f.next++
		e := map[string]any{"id": f.next, "address": b["address"]}
		c := f.CDNs[p(1)]
		c["access"] = append(c["access"].([]any), e)
		data(w, 201, e)
	case "DELETE /cdn/*/access-list/*":
		w.WriteHeader(204)
	case "GET /cloud-routers":
		list := []any{}
		for _, rt := range f.Routers {
			list = append(list, rt)
		}
		data(w, 200, list)
	case "POST /cloud-routers":
		b, ok := body(w, r, "name")
		if !ok {
			return
		}
		id, gw := f.id("rt"), f.id("gw")
		port := 51820
		g := map[string]any{"id": gw, "name": "gateway", "state": "ready", "disabled": false, "traffic_limited": false, "listen_port": port, "ipv4": "10.64.0.1/24", "ipv6": nil, "public_key": "GatewayPublicKeyGatewayPublicKeyGatewayPubA=", "traffic_bytes": map[string]any{"in": 0, "out": 0}}
		f.Gateways[gw] = g
		rt := map[string]any{"id": id, "name": b["name"], "vrf": "vrf1", "traffic_limited": false, "gateways": []any{g}, "public_ips": []any{}}
		f.Routers[id] = rt
		data(w, 202, rt)
	case "GET /cloud-routers/*", "PATCH /cloud-routers/*":
		rt := f.Routers[p(1)]
		if r.Method == "PATCH" {
			b, ok := body(w, r, "name")
			if !ok {
				return
			}
			rt["name"] = b["name"]
		}
		data(w, 200, rt)
	case "POST /cloud-routers/*/gateways":
		if _, ok := body(w, r, "name"); !ok {
			return
		}
		data(w, 202, map[string]any{"id": f.id("gw"), "name": "second", "state": "creating"})
	case "GET /cloud-routers/*/routes", "GET /cloud-routers/*/firewall", "GET /cloud-routers/*/public-ips":
		data(w, 200, []any{})
	case "POST /cloud-routers/*/routes":
		b, ok := body(w, r, "destination", "gateway", "peer", "distance")
		if !ok {
			return
		}
		data(w, 202, map[string]any{"id": 1, "destination": b["destination"], "gateway": b["gateway"], "distance": 1, "peer": b["peer"], "system": false, "disabled": false, "state": "applying"})
	case "POST /cloud-routers/*/firewall":
		b, ok := body(w, r, "chain=forward|input|output", "action=accept|drop", "source", "destination")
		if !ok {
			return
		}
		data(w, 202, map[string]any{"id": 2, "chain": b["chain"], "action": b["action"], "family": "ipv4", "source": b["source"], "destination": b["destination"], "disabled": false, "state": "applying"})
	case "POST /cloud-routers/*/public-ips":
		data(w, 202, map[string]any{"id": 3, "ip": "2001:db8:ff::7", "type": "ipv6", "disabled": false, "state": "applying"})
	case "PATCH /cloud-routers/*/routes/*", "PATCH /cloud-routers/*/firewall/*", "PATCH /cloud-routers/*/public-ips/*":
		if _, ok := body(w, r, "disabled"); !ok {
			return
		}
		data(w, 202, map[string]any{"id": 1, "state": "applying"})
	case "DELETE /cloud-routers/*/routes/*", "DELETE /cloud-routers/*/firewall/*", "DELETE /cloud-routers/*/public-ips/*":
		data(w, 202, map[string]any{"id": 1, "state": "deleting"})
	case "GET /gateways/*", "PATCH /gateways/*", "DELETE /gateways/*":
		g := f.Gateways[p(1)]
		if r.Method == "PATCH" {
			b, ok := body(w, r, "name", "ipv4", "ipv6", "disabled")
			if !ok {
				return
			}
			for k, v := range b {
				g[k] = v
			}
		}
		code := 200
		if r.Method == "DELETE" {
			code = 202
		}
		data(w, code, g)
	case "POST /gateways/*/rotate-keys":
		data(w, 202, f.Gateways[p(1)])
	case "POST /gateways/*/reset-traffic":
		data(w, 200, f.Gateways[p(1)])
	case "POST /gateways/*/peers":
		b, ok := body(w, r, "name")
		if !ok {
			return
		}
		id := f.id("pe")
		peer := map[string]any{"id": id, "name": b["name"], "state": "applying", "disabled": false, "site_to_site": false, "ipv4": "10.64.0.5/32", "keepalive": "00:00:25", "has_preshared_key": true, "traffic_bytes": map[string]any{"in": 0, "out": 0}, "busy": 1}
		f.Peers[id] = peer
		data(w, 202, peer)
	case "GET /peers/*", "DELETE /peers/*":
		peer, ok := f.Peers[p(1)]
		if !ok {
			problem(w, 404, "peer_not_found", "Peer not found.")
			return
		}
		if r.Method == "DELETE" {
			delete(f.Peers, p(1))
			data(w, 202, peer)
			return
		}
		data(w, 200, peer)
	case "PUT /peers/*/keys":
		b, ok := body(w, r, "public_key", "private_key", "preshared_key")
		if !ok {
			return
		}
		if _, has := b["private_key"]; has {
			problem(w, 422, "invalid_key", "TitanOS must never send a private key")
			return
		}
		peer := f.Peers[p(1)]
		if peer["busy"] == 1 {
			peer["busy"] = 0
			w.Header().Set("Content-Type", "application/problem+json")
			w.WriteHeader(409)
			json.NewEncoder(w).Encode(map[string]any{"type": "x", "title": "Conflict", "status": 409, "detail": "The peer is being set up.", "code": "peer_busy", "retry_after": 1})
			return
		}
		peer["public_key"] = b["public_key"]
		peer["state"] = "ready"
		data(w, 202, peer)
	case "GET /peers/*/config":
		peer := f.Peers[p(1)]
		config := "[Interface]\nAddress = 10.64.0.5/32, fd64::5/128\nDNS = 10.64.0.1\n\n[Peer]\nPublicKey = GatewayPublicKeyGatewayPublicKeyGatewayPubA=\nPresharedKey = PresharedKeyPresharedKeyPresharedKeyPreshAB=\nAllowedIPs = 10.64.0.0/24, 0.0.0.0/0, ::/0\nEndpoint = vpn1.ipv64.net:51820\nPersistentKeepalive = 25\n"
		_ = peer
		data(w, 200, map[string]any{"peer": p(1), "config": config})
	case "GET /honeypot/list-token":
		data(w, 200, map[string]any{"token": "token123token123token123token123", "live": false, "base": f.Server.URL + "/lists/token123/"})
	default:
		problem(w, 404, "not_found", "The fake knows no "+route)
	}
}

func (f *Fake) cdnView(c map[string]any, detail bool) map[string]any {
	out := map[string]any{}
	for k, v := range c {
		if k == "records" || k == "access" || (k == "mappings" && !detail) {
			continue
		}
		out[k] = v
	}
	pending := []any{}
	if c["server"] == nil {
		pending = append(pending, "server_assignment")
	}
	out["pending"] = pending
	out["dns_records_enabled"] = len(c["records"].([]any)) > 0
	return out
}

// pattern replaces IDs and names in a path with "*", keeping the words of
// the API.
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
