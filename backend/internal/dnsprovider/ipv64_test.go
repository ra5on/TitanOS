package dnsprovider

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeIPv64 follows the OpenAPI description of ipv64.net/api/v2 for the
// parts TitanOS uses, plus /nic/update and /health.
type fakeIPv64 struct {
	t       *testing.T
	mu      sync.Mutex
	key     string
	domains []ipv64WireDomain
	records []ipv64WireRecord
	checks  []ipv64WireCheck
	paused  map[string]bool
	updates []string
	pings   int
	nextID  int64
	pending int // GETs of records that still say pending_dns
	limited bool
}

func newFakeIPv64(t *testing.T) (*fakeIPv64, *httptest.Server) {
	f := &fakeIPv64{t: t, key: "api-key", paused: map[string]bool{},
		domains: []ipv64WireDomain{{ID: 1, Name: "nas.ipv64.de", Kind: "dyndns", Zone: "ipv64.de", UpdateToken: "domain-token", Active: true}}}
	server := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(server.Close)
	return f, server
}

func (f *fakeIPv64) problem(w http.ResponseWriter, status int, code, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", "7")
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"type": "https://ipv64.net/api/v2/errors#" + code, "title": http.StatusText(status), "status": status, "detail": detail, "code": code})
}

func (f *fakeIPv64) data(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("RateLimit-Limit", "300")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"data": v})
}

func (f *fakeIPv64) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.Contains(r.URL.RawQuery, "token") || strings.Contains(r.URL.RawQuery, "key=") {
		f.t.Errorf("secret in the address: %s", r.URL)
	}
	auth := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch r.URL.Path {
	case "/nic/update":
		if auth != "domain-token" {
			w.WriteHeader(http.StatusUnauthorized)
			io.WriteString(w, "badauth")
			return
		}
		f.updates = append(f.updates, "legacy "+r.URL.Query().Encode())
		io.WriteString(w, "good")
		return
	case "/health":
		if auth != "hc-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		f.pings++
		w.WriteHeader(http.StatusOK)
		return
	case "/api/v2/ip":
		f.data(w, 200, map[string]string{"ip": "203.0.113.64", "family": "ipv4"})
		return
	}
	if auth != f.key {
		f.problem(w, 401, "invalid_token", "The API key is invalid, expired or the account is not active.")
		return
	}
	if f.limited {
		f.problem(w, 429, "rate_limited", "Too many requests.")
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v2")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	var body map[string]any
	if r.Body != nil {
		json.NewDecoder(r.Body).Decode(&body)
	}
	str := func(k string) string { s, _ := body[k].(string); return s }
	switch {
	case r.Method == "GET" && path == "/me":
		limit := 48
		f.data(w, 200, map[string]any{"id": 1, "email": "me@example.org", "username": "nasfan",
			"plan":  map[string]any{"id": 1, "name": "Free", "valid_until": nil},
			"usage": map[string]any{"dyndns_updates_24h": map[string]any{"used": 3, "limit": limit}, "dyndns_domains": map[string]any{"used": 1, "limit": 5}},
			"auth":  map[string]any{"type": "api_key", "key_id": 7, "key_name": "titanos"}})
	case r.Method == "GET" && path == "/domains":
		f.data(w, 200, f.domains)
	case r.Method == "POST" && path == "/domains":
		f.nextID++
		d := ipv64WireDomain{ID: 10 + f.nextID, Name: str("name"), Kind: "dyndns", UpdateToken: "new-token", Active: true}
		f.domains = append(f.domains, d)
		f.data(w, 201, d)
	case r.Method == "PATCH" && len(parts) == 2 && parts[0] == "domains":
		for i := range f.domains {
			if f.domains[i].Name == parts[1] {
				if v, ok := body["wildcard"].(bool); ok {
					f.domains[i].Wildcard = v
				}
				f.data(w, 200, f.domains[i])
				return
			}
		}
		f.problem(w, 404, "domain_not_found", "Domain not found.")
	case r.Method == "POST" && len(parts) == 3 && parts[2] == "dyndns":
		f.updates = append(f.updates, fmt.Sprintf("api %s %s %s %s", parts[1], str("prefix"), str("ipv4"), str("ipv6")))
		f.data(w, 200, map[string]any{"domain": parts[1], "changed": true, "results": []map[string]string{{"ip": str("ipv4"), "type": "A", "result": "updated"}}})
	case len(parts) == 3 && parts[2] == "records":
		domain := parts[1]
		switch r.Method {
		case "GET":
			out := []ipv64WireRecord{}
			for _, rec := range f.records {
				if rec.FQDN != "" && strings.HasSuffix(rec.FQDN, domain) && (r.URL.Query().Get("type") == "" || rec.Type == r.URL.Query().Get("type")) && (r.URL.Query().Get("prefix") == "" || rec.Prefix == r.URL.Query().Get("prefix")) {
					rec.PendingDNS = f.pending > 0
					out = append(out, rec)
				}
			}
			if f.pending > 0 {
				f.pending--
			}
			f.data(w, 200, out)
		case "POST":
			f.nextID++
			fqdn := domain
			if p := str("prefix"); p != "" {
				fqdn = p + "." + domain
			}
			rec := ipv64WireRecord{ID: f.nextID, Prefix: str("prefix"), FQDN: fqdn, Type: str("type"), Content: str("content"), TTL: 60, State: "active"}
			f.records = append(f.records, rec)
			f.data(w, 201, rec)
		case "DELETE":
			q := r.URL.Query()
			before := len(f.records)
			f.records = slices.DeleteFunc(f.records, func(rec ipv64WireRecord) bool {
				return rec.Type == q.Get("type") && rec.Content == q.Get("content") && rec.Prefix == q.Get("prefix")
			})
			if before == len(f.records) {
				f.problem(w, 404, "record_not_found", "Record not found.")
				return
			}
			w.WriteHeader(204)
		}
	case r.Method == "DELETE" && len(parts) == 2 && parts[0] == "records":
		id, _ := strconv.ParseInt(parts[1], 10, 64)
		f.records = slices.DeleteFunc(f.records, func(rec ipv64WireRecord) bool { return rec.ID == id })
		w.WriteHeader(204)
	case r.Method == "GET" && path == "/healthchecks":
		f.data(w, 200, f.checks)
	case r.Method == "POST" && path == "/healthchecks":
		c := ipv64WireCheck{Token: "hc-token", Name: str("name"), Type: "push", Status: "new"}
		c.Push = &struct {
			Period     int        `json:"period_seconds"`
			Grace      int        `json:"grace_seconds"`
			ExpectedBy *time.Time `json:"expected_by"`
		}{Period: int(body["period_seconds"].(float64)), Grace: int(body["grace_seconds"].(float64))}
		f.checks = append(f.checks, c)
		f.data(w, 201, c)
	case len(parts) >= 2 && parts[0] == "healthchecks":
		action := ""
		if len(parts) == 3 {
			action = parts[2]
		}
		switch {
		case action == "pause":
			f.paused[parts[1]] = true
		case action == "resume":
			delete(f.paused, parts[1])
		case r.Method == "DELETE":
			f.checks = nil
			w.WriteHeader(204)
			return
		}
		for _, c := range f.checks {
			if c.Token == parts[1] {
				f.data(w, 200, c)
				return
			}
		}
		f.problem(w, 404, "not_found", "Healthcheck not found.")
	case r.Method == "GET" && path == "/integrations":
		if r.URL.Query().Get("include_secrets") != "false" {
			f.t.Error("integrations asked with secrets")
		}
		f.data(w, 200, []map[string]any{{"id": 0, "type": "account_email", "name": "E-Mail", "status": "active", "options": map[string]string{"to": "m***"}}, {"id": 5, "type": "ntfy", "name": "Handy", "status": "active"}})
	default:
		f.t.Errorf("fake IPv64: unexpected %s %s", r.Method, r.URL)
		f.problem(w, 404, "not_found", "Unknown endpoint.")
	}
}

func TestIPv64ChallengeAndRecords(t *testing.T) {
	f, server := newFakeIPv64(t)
	ipv64Poll = time.Millisecond
	f.pending = 2
	p, err := New(Config{Type: IPv64Net, Token: "api-key"}, Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	ch := p.(Challenger)
	if err := ch.SetTXT(ctx(t), "_acme-challenge.nas.ipv64.de", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if len(f.records) != 2 || f.records[0].Prefix != "_acme-challenge" || f.pending != 0 {
		t.Fatalf("%+v pending %d", f.records, f.pending)
	}
	c := p.(*IPv64)
	if _, err := c.AddRecord(ctx(t), "nas.ipv64.de", "CAA", "", `0 issue "letsencrypt.org"`); err != nil {
		t.Fatal(err)
	}
	if err := ch.ClearTXT(ctx(t), "_acme-challenge.nas.ipv64.de"); err != nil {
		t.Fatal(err)
	}
	if len(f.records) != 1 || f.records[0].Type != "CAA" {
		t.Fatal("only the TXT records go", f.records)
	}
	records, err := c.Records(ctx(t), "nas.ipv64.de", "", "")
	if err != nil || len(records) != 1 {
		t.Fatal(records, err)
	}
	if err := c.DeleteRecord(ctx(t), records[0].ID); err != nil || len(f.records) != 0 {
		t.Fatal(err)
	}
	for _, bad := range [][3]string{{"A", "", "2001:db8::1"}, {"MX", "", "mail.example.org"}, {"PTR", "", "x"}, {"TXT", "a b", "x"}, {"CAA", "", "issue letsencrypt.org"}} {
		if err := CheckRecord(bad[0], bad[1], bad[2]); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	if err := CheckRecord("SRV", "_sip._tcp", "10 5 5060 sip.example.org"); err != nil {
		t.Error(err)
	}
	if err := ch.SetTXT(ctx(t), "_acme-challenge.other.example", []string{"x"}); err == nil {
		t.Error("foreign name")
	}
}

func TestIPv64Addresses(t *testing.T) {
	f, server := newFakeIPv64(t)
	// With the API key: API v2, not counted against the daily limit.
	p, _ := New(Config{Type: IPv64Net, Token: "api-key"}, Options{BaseURL: server.URL})
	if err := p.(Updater).SetAddresses(ctx(t), "nas.ipv64.de", v4, v6); err != nil {
		t.Fatal(err)
	}
	if err := p.(Updater).SetAddresses(ctx(t), "cloud.nas.ipv64.de", v4, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(f.updates, []string{"api nas.ipv64.de  203.0.113.7 2001:db8::7", "api nas.ipv64.de cloud 203.0.113.7 "}) {
		t.Fatal(f.updates)
	}
	if p.(Combined).CombinedUpdate() {
		t.Error("API updates name each family")
	}
	f.updates = nil
	ipv64Budget.updates, ipv64Budget.recent = nil, nil
	// Only the update token: the updater, combined.
	legacy, _ := New(Config{Type: IPv64Net, UpdateToken: "domain-token"}, Options{BaseURL: server.URL})
	if !legacy.(Combined).CombinedUpdate() {
		t.Error("updater wants both addresses")
	}
	if err := legacy.(Updater).SetAddresses(ctx(t), "nas.ipv64.de", v4, v6); err != nil {
		t.Fatal(err)
	}
	if f.updates[0] != "legacy domain=nas.ipv64.de&ip=203.0.113.7&ip6=2001%3Adb8%3A%3A7&output=min" {
		t.Fatal(f.updates)
	}
	// The API key and an IPv6 prefix: updater with the token looked up.
	withPrefix, _ := New(Config{Type: IPv64Net, Token: "api-key", IPv6Prefix: "auto"}, Options{BaseURL: server.URL})
	if err := withPrefix.(Updater).SetAddresses(ctx(t), "nas.ipv64.de", v4, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.updates[1], "ipv6prefix=auto") {
		t.Fatal(f.updates)
	}
	// Three requests in 10 seconds at most.
	if err := legacy.(Updater).SetAddresses(ctx(t), "nas.ipv64.de", v4, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	err := legacy.(Updater).SetAddresses(ctx(t), "nas.ipv64.de", v4, netip.Addr{})
	if apiErr, ok := err.(*APIError); !ok || apiErr.RetryAfter == 0 {
		t.Fatal("fourth request in 10 s", err)
	}
	// Half of the daily limit.
	ipv64Budget.updates, ipv64Budget.recent = nil, nil
	SetIPv64DailyLimit(4)
	defer SetIPv64DailyLimit(0)
	for i := range 3 {
		ipv64Budget.recent = nil
		err = legacy.(Updater).SetAddresses(ctx(t), "nas.ipv64.de", v4, netip.Addr{})
		if (i < 2) != (err == nil) {
			t.Fatal(i, err)
		}
	}
	bad, _ := New(Config{Type: IPv64Net, UpdateToken: "wrong-token"}, Options{BaseURL: server.URL})
	ipv64Budget.updates, ipv64Budget.recent = nil, nil
	if err := bad.(Updater).SetAddresses(ctx(t), "nas.ipv64.de", v4, netip.Addr{}); err == nil || strings.Contains(err.Error(), "wrong-token") {
		t.Fatal(err)
	}
}

func TestIPv64AccountDomainsHealthchecks(t *testing.T) {
	f, server := newFakeIPv64(t)
	c := NewIPv64("api-key", "", Options{BaseURL: server.URL})
	account, err := c.Account(ctx(t))
	if err != nil || account.Plan != "Free" || *account.Usage["dyndns_updates_24h"].Limit != 48 || account.KeyName != "titanos" {
		t.Fatal(account, err)
	}
	domains, err := c.Domains(ctx(t))
	if err != nil || len(domains) != 1 || domains[0].Name != "nas.ipv64.de" {
		t.Fatal(domains, err)
	}
	data, _ := json.Marshal(domains)
	if strings.Contains(string(data), "domain-token") {
		t.Fatal("update token leaves the package")
	}
	if _, err := c.CreateDomain(ctx(t), "evil.example.com"); err == nil {
		t.Fatal("foreign zone")
	}
	if d, err := c.CreateDomain(ctx(t), "titan.home64.de"); err != nil || d.Name != "titan.home64.de" {
		t.Fatal(d, err)
	}
	on := true
	if d, err := c.ChangeDomain(ctx(t), "nas.ipv64.de", IPv64DomainChange{Wildcard: &on}); err != nil || !d.Wildcard {
		t.Fatal(d, err)
	}
	ip, err := c.PublicIP(ctx(t), "tcp4")
	if err != nil || ip != netip.MustParseAddr("203.0.113.64") {
		t.Fatal(ip, err)
	}

	integrations, err := c.Integrations(ctx(t))
	if err != nil || len(integrations) != 2 || integrations[1].Name != "Handy" {
		t.Fatal(integrations, err)
	}
	if _, err := c.CreateHealthcheck(ctx(t), "titanos-nas", 30*time.Second, time.Minute, nil); err == nil {
		t.Fatal("period below a minute")
	}
	check, err := c.CreateHealthcheck(ctx(t), "titanos-nas", 30*time.Minute, 10*time.Minute, []int{0, 5})
	if err != nil || check.Token() != "hc-token" || check.Period != 1800 || check.ID != HealthcheckID("hc-token") {
		t.Fatal(check, err)
	}
	data, _ = json.Marshal(check)
	if strings.Contains(string(data), "hc-token") {
		t.Fatal("token in JSON")
	}
	list, err := c.Healthchecks(ctx(t))
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	if err := c.PauseHealthcheck(ctx(t), "hc-token"); err != nil || !f.paused["hc-token"] {
		t.Fatal(err)
	}
	if err := c.ResumeHealthcheck(ctx(t), "hc-token"); err != nil || f.paused["hc-token"] {
		t.Fatal(err)
	}
	ipv64Budget.recent = nil
	if err := c.Ping(ctx(t), "hc-token"); err != nil || f.pings != 1 {
		t.Fatal(err)
	}
	if err := c.DeleteHealthcheck(ctx(t), "hc-token"); err != nil || len(f.checks) != 0 {
		t.Fatal(err)
	}

	f.limited = true
	_, err = c.Domains(ctx(t))
	apiErr, ok := err.(*APIError)
	if !ok || apiErr.Status != 429 || apiErr.RetryAfter != 7*time.Second || apiErr.Code != "rate_limited" {
		t.Fatal(err)
	}
	wrong := NewIPv64("nope", "", Options{BaseURL: server.URL})
	if _, err := wrong.Account(ctx(t)); err == nil || !strings.Contains(err.Error(), "API key is invalid") {
		t.Fatal(err)
	}
	if err := (Config{Type: IPv64Net, UpdateToken: "x"}).Validate(ForChallenge); err == nil {
		t.Fatal("the DNS challenge needs the API key")
	}
	if err := (Config{Type: IPv64Net, UpdateToken: "x"}).Validate(ForAddress); err != nil {
		t.Fatal(err)
	}
	if err := (Config{Type: IPv64Net, Token: "x", IPv6Prefix: "10.0.0.0/8"}).Validate(ForAddress); err == nil {
		t.Fatal("IPv4 prefix")
	}
}
