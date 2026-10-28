package dnsprovider

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

var (
	v4 = netip.MustParseAddr("203.0.113.7")
	v6 = netip.MustParseAddr("2001:db8::7")
)

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return c
}

func decode(t *testing.T, r *http.Request, v any) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		t.Errorf("body: %v", err)
	}
}

func reply(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func TestValidateAndSecrets(t *testing.T) {
	good := []Config{
		{Type: Cloudflare, Token: "t"},
		{Type: DeSEC, Token: "t"},
		{Type: Hetzner, Token: "t"},
		{Type: IONOS, Token: "pre.secret"},
		{Type: Netcup, Customer: "12345", Token: "k", Password: "p"},
		{Type: INWX, User: "u", Password: "p"},
		{Type: DuckDNS, Token: "t"},
		{Type: RFC2136, Server: "ns.example.com", Zone: "example.com", KeyName: "nas", Secret: base64.StdEncoding.EncodeToString([]byte("key"))},
	}
	for _, c := range good {
		c = c.Normalize()
		if err := c.Validate(ForChallenge); err != nil {
			t.Errorf("%s: %v", c.Type, err)
		}
	}
	if err := (Config{Type: DynDNS2, Server: "https://dyn.example/nic/update"}).Validate(ForChallenge); err == nil {
		t.Error("dyndns2 cannot answer challenges")
	}
	if err := (Config{Type: DynDNS2, Server: "https://dyn.example/nic/update"}).Validate(ForAddress); err != nil {
		t.Error(err)
	}
	bad := []Config{
		{Type: "route53", Token: "t"},
		{Type: Cloudflare},
		{Type: IONOS, Token: "nodot"},
		{Type: Netcup, Customer: "abc", Token: "k", Password: "p"},
		{Type: RFC2136, Server: "ns", Zone: "example.com", KeyName: "nas", Secret: "%%%"},
		{Type: Cloudflare, Token: "a\nb"},
		{Type: DynDNS2, Server: "ftp://x"},
	}
	for _, c := range bad {
		if err := c.Normalize().Validate(ForAddress); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: want ErrInvalid, got %v", c.Public(), err)
		}
	}

	stored := Config{Type: INWX, User: "u", Password: "secret"}
	kept := Config{Type: INWX, User: "u"}.KeepSecrets(stored, nil)
	if kept.Password != "secret" {
		t.Error("same account keeps the password")
	}
	if other := (Config{Type: INWX, User: "other"}).KeepSecrets(stored, nil); other.Password != "" {
		t.Error("another account must not get the password")
	}
	if cleared := (Config{Type: INWX, User: "u"}).KeepSecrets(stored, []string{"password"}); cleared.Password != "" {
		t.Error("cleared password kept")
	}
	if p := stored.Public(); p.Password != "" || !stored.Secrets()["password"] {
		t.Error("Public/Secrets")
	}
}

func TestCandidates(t *testing.T) {
	got := candidates("_acme-challenge.nas.example.co.uk")
	want := []string{"_acme-challenge.nas.example.co.uk", "nas.example.co.uk", "example.co.uk", "co.uk"}
	if !slices.Equal(got, want) {
		t.Fatal(got)
	}
	if relative("example.com", "example.com") != "@" || relative("a.b.example.com", "example.com") != "a.b" {
		t.Fatal("relative")
	}
}

// --- Cloudflare ---

type cfFake struct {
	mu      sync.Mutex
	records []cfRecord
	next    int
}

func (f *cfFake) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Header.Get("Authorization") != "Bearer cf-token" {
			w.WriteHeader(http.StatusForbidden)
			reply(w, map[string]any{"success": false, "errors": []map[string]string{{"message": "Invalid access token"}}})
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			var result []map[string]string
			if r.URL.Query().Get("name") == "example.com" {
				result = append(result, map[string]string{"id": "z1", "name": "example.com"})
			}
			reply(w, map[string]any{"success": true, "result": result})
		case r.Method == http.MethodGet && r.URL.Path == "/zones/z1/dns_records":
			out := []cfRecord{}
			for _, rec := range f.records {
				if rec.Type == r.URL.Query().Get("type") && rec.Name == r.URL.Query().Get("name") {
					out = append(out, rec)
				}
			}
			reply(w, map[string]any{"success": true, "result": out})
		case r.Method == http.MethodPost && r.URL.Path == "/zones/z1/dns_records":
			var rec cfRecord
			decode(t, r, &rec)
			f.next++
			rec.ID = fmt.Sprint(f.next)
			f.records = append(f.records, rec)
			reply(w, map[string]any{"success": true, "result": rec})
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/zones/z1/dns_records/"):
			var patch map[string]string
			decode(t, r, &patch)
			for i := range f.records {
				if f.records[i].ID == strings.TrimPrefix(r.URL.Path, "/zones/z1/dns_records/") {
					f.records[i].Content = patch["content"]
				}
			}
			reply(w, map[string]any{"success": true})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/zones/z1/dns_records/"):
			id := strings.TrimPrefix(r.URL.Path, "/zones/z1/dns_records/")
			f.records = slices.DeleteFunc(f.records, func(rec cfRecord) bool { return rec.ID == id })
			reply(w, map[string]any{"success": true})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
		}
	})
}

func TestCloudflare(t *testing.T) {
	fake := &cfFake{}
	server := httptest.NewServer(fake.handler(t))
	defer server.Close()
	p, err := New(Config{Type: Cloudflare, Token: "cf-token"}, Options{BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	ch := p.(Challenger)
	name := "_acme-challenge.nas.example.com"
	if err := ch.SetTXT(ctx(t), name, []string{"one", "two"}); err != nil {
		t.Fatal(err)
	}
	if len(fake.records) != 2 {
		t.Fatalf("records %+v", fake.records)
	}
	if err := ch.ClearTXT(ctx(t), name); err != nil || len(fake.records) != 0 {
		t.Fatalf("clear: %v %+v", err, fake.records)
	}
	up := p.(Updater)
	if err := up.SetAddresses(ctx(t), "nas.example.com", v4, v6); err != nil {
		t.Fatal(err)
	}
	if err := up.SetAddresses(ctx(t), "nas.example.com", netip.MustParseAddr("198.51.100.1"), netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	contents := map[string]string{}
	for _, rec := range fake.records {
		contents[rec.Type] = rec.Content
	}
	if contents["A"] != "198.51.100.1" || contents["AAAA"] != "2001:db8::7" || len(fake.records) != 2 {
		t.Fatalf("records %+v", fake.records)
	}
	if err := ch.SetTXT(ctx(t), "_acme-challenge.other.org", []string{"x"}); err == nil {
		t.Fatal("name outside the account accepted")
	}
	bad, _ := New(Config{Type: Cloudflare, Token: "wrong"}, Options{BaseURL: server.URL})
	err = bad.(Challenger).SetTXT(ctx(t), name, []string{"x"})
	if err == nil || !strings.Contains(err.Error(), "Invalid access token") || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("refusal: %v", err)
	}
}

// --- deSEC ---

func TestDeSEC(t *testing.T) {
	var puts [][]desecRRset
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token desec-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/domains/":
			if !strings.HasSuffix(r.URL.Query().Get("owns_qname"), "nas.dedyn.io") {
				reply(w, []any{})
				return
			}
			reply(w, []map[string]any{{"name": "nas.dedyn.io", "minimum_ttl": 60}})
		case r.Method == http.MethodPut && r.URL.Path == "/domains/nas.dedyn.io/rrsets/":
			var sets []desecRRset
			decode(t, r, &sets)
			puts = append(puts, sets)
			reply(w, sets)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	p, _ := New(Config{Type: DeSEC, Token: "desec-token"}, Options{BaseURL: server.URL})
	if err := p.(Challenger).SetTXT(ctx(t), "_acme-challenge.nas.dedyn.io", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if err := p.(Challenger).ClearTXT(ctx(t), "_acme-challenge.nas.dedyn.io"); err != nil {
		t.Fatal(err)
	}
	if err := p.(Updater).SetAddresses(ctx(t), "nas.dedyn.io", v4, v6); err != nil {
		t.Fatal(err)
	}
	if len(puts) != 3 {
		t.Fatal(puts)
	}
	if s := puts[0][0]; s.Subname != "_acme-challenge" || s.Type != "TXT" || !slices.Equal(s.Records, []string{`"a"`, `"b"`}) {
		t.Errorf("set %+v", s)
	}
	if s := puts[1][0]; len(s.Records) != 0 {
		t.Errorf("clear %+v", s)
	}
	if s := puts[2]; len(s) != 2 || s[0].Subname != "" || s[0].Type != "A" || s[0].Records[0] != "203.0.113.7" || s[1].Type != "AAAA" {
		t.Errorf("addresses %+v", s)
	}
}

// --- Hetzner ---

func TestHetzner(t *testing.T) {
	sets := map[string][]string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer hz" {
			w.WriteHeader(http.StatusUnauthorized)
			reply(w, map[string]any{"error": map[string]string{"message": "unauthorized"}})
			return
		}
		values := func() []string {
			var body struct {
				Name    string          `json:"name"`
				Type    string          `json:"type"`
				Records []hetznerRecord `json:"records"`
			}
			decode(t, r, &body)
			var out []string
			for _, rec := range body.Records {
				out = append(out, rec.Value)
			}
			return out
		}
		switch path := r.URL.Path; {
		case r.Method == http.MethodGet && path == "/zones":
			var zones []map[string]any
			if r.URL.Query().Get("name") == "example.com" {
				zones = append(zones, map[string]any{"id": 42, "name": "example.com"})
			}
			reply(w, map[string]any{"zones": zones})
		case r.Method == http.MethodGet && strings.HasPrefix(path, "/zones/42/rrsets/"):
			key := strings.TrimPrefix(path, "/zones/42/rrsets/")
			if _, ok := sets[key]; !ok {
				w.WriteHeader(http.StatusNotFound)
				reply(w, map[string]any{"error": map[string]string{"message": "not found"}})
				return
			}
			reply(w, map[string]any{"rrset": map[string]any{}})
		case r.Method == http.MethodPost && path == "/zones/42/rrsets":
			var body struct {
				Name    string          `json:"name"`
				Type    string          `json:"type"`
				Records []hetznerRecord `json:"records"`
			}
			decode(t, r, &body)
			var out []string
			for _, rec := range body.Records {
				out = append(out, rec.Value)
			}
			sets[body.Name+"/"+body.Type] = out
			reply(w, map[string]any{})
		case r.Method == http.MethodPost && strings.HasSuffix(path, "/actions/set_records"):
			key := strings.TrimSuffix(strings.TrimPrefix(path, "/zones/42/rrsets/"), "/actions/set_records")
			sets[key] = values()
			reply(w, map[string]any{})
		case r.Method == http.MethodDelete:
			key := strings.TrimPrefix(path, "/zones/42/rrsets/")
			if _, ok := sets[key]; !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			delete(sets, key)
			reply(w, map[string]any{})
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	p, _ := New(Config{Type: Hetzner, Token: "hz"}, Options{BaseURL: server.URL})
	ch := p.(Challenger)
	if err := ch.SetTXT(ctx(t), "_acme-challenge.example.com", []string{"v1"}); err != nil {
		t.Fatal(err)
	}
	if err := ch.SetTXT(ctx(t), "_acme-challenge.example.com", []string{"v1", "v2"}); err != nil {
		t.Fatal(err)
	}
	if got := sets["_acme-challenge/TXT"]; !slices.Equal(got, []string{`"v1"`, `"v2"`}) {
		t.Fatal(sets)
	}
	if err := ch.ClearTXT(ctx(t), "_acme-challenge.example.com"); err != nil || len(sets) != 0 {
		t.Fatal(err, sets)
	}
	if err := ch.ClearTXT(ctx(t), "_acme-challenge.example.com"); err != nil {
		t.Fatal("clearing nothing is fine:", err)
	}
	if err := p.(Updater).SetAddresses(ctx(t), "example.com", v4, netip.Addr{}); err != nil || sets["@/A"][0] != "203.0.113.7" {
		t.Fatal(err, sets)
	}
}

// --- IONOS ---

func TestIONOS(t *testing.T) {
	var mu sync.Mutex
	records := []ionosRecord{{ID: "r1", Name: "nas.example.com", Type: "A", Content: "192.0.2.1", TTL: 3600}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("X-Api-Key") != "pre.secret" {
			w.WriteHeader(http.StatusUnauthorized)
			reply(w, []map[string]string{{"code": "UNAUTHORIZED", "message": "missing or invalid key"}})
			return
		}
		switch path := r.URL.Path; {
		case path == "/zones":
			reply(w, []map[string]string{{"id": "zz", "name": "example.com"}, {"id": "other", "name": "com.example.net"}})
		case r.Method == http.MethodGet && path == "/zones/zz":
			var out []ionosRecord
			for _, rec := range records {
				if rec.Name == r.URL.Query().Get("recordName") {
					out = append(out, rec)
				}
			}
			reply(w, map[string]any{"records": out})
		case r.Method == http.MethodPost && path == "/zones/zz/records":
			var add []ionosRecord
			decode(t, r, &add)
			for i := range add {
				add[i].ID = fmt.Sprintf("n%d", len(records))
				records = append(records, add[i])
			}
			reply(w, add)
		case r.Method == http.MethodPut:
			var body map[string]any
			decode(t, r, &body)
			for i := range records {
				if "/zones/zz/records/"+records[i].ID == path {
					records[i].Content = body["content"].(string)
				}
			}
		case r.Method == http.MethodDelete:
			records = slices.DeleteFunc(records, func(rec ionosRecord) bool { return "/zones/zz/records/"+rec.ID == path })
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	}))
	defer server.Close()
	p, _ := New(Config{Type: IONOS, Token: "pre.secret"}, Options{BaseURL: server.URL})
	if err := p.(Challenger).SetTXT(ctx(t), "_acme-challenge.nas.example.com", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 {
		t.Fatal(records)
	}
	if err := p.(Challenger).ClearTXT(ctx(t), "_acme-challenge.nas.example.com"); err != nil || len(records) != 1 {
		t.Fatal(err, records)
	}
	if err := p.(Updater).SetAddresses(ctx(t), "nas.example.com", v4, v6); err != nil {
		t.Fatal(err)
	}
	if records[0].Content != "203.0.113.7" || records[1].Type != "AAAA" {
		t.Fatal(records)
	}
}

// --- Netcup ---

func TestNetcup(t *testing.T) {
	var records []netcupRecord
	loggedOut := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Action string         `json:"action"`
			Param  map[string]any `json:"param"`
		}
		decode(t, r, &req)
		if req.Param["customernumber"] != "4711" || req.Param["apikey"] != "key" {
			reply(w, map[string]any{"status": "error", "statuscode": 4013, "longmessage": "Validation Error."})
			return
		}
		ok := func(data any) {
			reply(w, map[string]any{"status": "success", "statuscode": 2000, "responsedata": data})
		}
		switch req.Action {
		case "login":
			if req.Param["apipassword"] != "pass" {
				reply(w, map[string]any{"status": "error", "statuscode": 4013, "longmessage": "The login was not successful."})
				return
			}
			ok(map[string]string{"apisessionid": "sess"})
		case "logout":
			loggedOut = true
			ok("")
		case "infoDnsZone":
			if req.Param["domainname"] != "example.de" {
				reply(w, map[string]any{"status": "error", "statuscode": 5012, "longmessage": "Domain not found"})
				return
			}
			ok(map[string]string{"name": "example.de"})
		case "infoDnsRecords":
			if len(records) == 0 {
				reply(w, map[string]any{"status": "error", "statuscode": 5029, "longmessage": "No records found."})
				return
			}
			ok(map[string]any{"dnsrecords": records})
		case "updateDnsRecords":
			var set struct {
				Records []netcupRecord `json:"dnsrecords"`
			}
			data, _ := json.Marshal(req.Param["dnsrecordset"])
			json.Unmarshal(data, &set)
			for _, rec := range set.Records {
				switch {
				case rec.DeleteRecord:
					records = slices.DeleteFunc(records, func(o netcupRecord) bool { return o.ID == rec.ID })
				case rec.ID != "":
					for i := range records {
						if records[i].ID == rec.ID {
							records[i] = rec
						}
					}
				default:
					rec.ID = fmt.Sprint(len(records) + 100)
					records = append(records, rec)
				}
			}
			ok(map[string]any{"dnsrecords": records})
		default:
			t.Errorf("action %s", req.Action)
		}
	}))
	defer server.Close()
	p, _ := New(Config{Type: Netcup, Customer: "4711", Token: "key", Password: "pass"}, Options{BaseURL: server.URL})
	if err := p.(Challenger).SetTXT(ctx(t), "_acme-challenge.nas.example.de", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 || records[0].Hostname != "_acme-challenge.nas" || !loggedOut {
		t.Fatal(records, loggedOut)
	}
	if err := p.(Challenger).ClearTXT(ctx(t), "_acme-challenge.nas.example.de"); err != nil || len(records) != 0 {
		t.Fatal(err, records)
	}
	if err := p.(Updater).SetAddresses(ctx(t), "example.de", v4, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	if err := p.(Updater).SetAddresses(ctx(t), "example.de", netip.MustParseAddr("198.51.100.2"), netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Hostname != "@" || records[0].Destination != "198.51.100.2" {
		t.Fatal(records)
	}
	bad, _ := New(Config{Type: Netcup, Customer: "4711", Token: "key", Password: "nope"}, Options{BaseURL: server.URL})
	if err := bad.(Challenger).ClearTXT(ctx(t), "x.example.de"); err == nil || strings.Contains(err.Error(), "nope") {
		t.Fatal(err)
	}
}

// --- INWX ---

func TestINWX(t *testing.T) {
	var records []inwxRecord
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		decode(t, r, &req)
		ok := func(data any) {
			reply(w, map[string]any{"code": 1000, "msg": "Command completed successfully", "resData": data})
		}
		if req.Method == "account.login" {
			if req.Params["user"] != "u" || req.Params["pass"] != "p" {
				reply(w, map[string]any{"code": 2200, "msg": "Authentication error"})
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "domrobot", Value: "s1"})
			ok(map[string]any{"tfa": "0"})
			return
		}
		if c, err := r.Cookie("domrobot"); err != nil || c.Value != "s1" {
			reply(w, map[string]any{"code": 2002, "msg": "Command use error"})
			return
		}
		switch req.Method {
		case "account.logout":
			ok(nil)
		case "nameserver.info":
			if req.Params["domain"] != "example.org" {
				reply(w, map[string]any{"code": 2303, "msg": "Object does not exist"})
				return
			}
			var out []inwxRecord
			for _, rec := range records {
				if rec.Name == req.Params["name"] && rec.Type == req.Params["type"] {
					out = append(out, rec)
				}
			}
			ok(map[string]any{"domain": "example.org", "record": out})
		case "nameserver.createRecord":
			records = append(records, inwxRecord{ID: int64(len(records) + 1), Name: req.Params["name"].(string), Type: req.Params["type"].(string), Content: req.Params["content"].(string)})
			ok(map[string]any{"id": len(records)})
		case "nameserver.updateRecord":
			for i := range records {
				if float64(records[i].ID) == req.Params["id"].(float64) {
					records[i].Content = req.Params["content"].(string)
				}
			}
			ok(nil)
		case "nameserver.deleteRecord":
			records = slices.DeleteFunc(records, func(rec inwxRecord) bool { return float64(rec.ID) == req.Params["id"].(float64) })
			ok(nil)
		default:
			t.Errorf("method %s", req.Method)
		}
	}))
	defer server.Close()
	p, _ := New(Config{Type: INWX, User: "u", Password: "p"}, Options{BaseURL: server.URL})
	if err := p.(Challenger).SetTXT(ctx(t), "_acme-challenge.example.org", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatal(records)
	}
	if err := p.(Challenger).ClearTXT(ctx(t), "_acme-challenge.example.org"); err != nil || len(records) != 0 {
		t.Fatal(err, records)
	}
	for _, ip := range []netip.Addr{v4, netip.MustParseAddr("198.51.100.3")} {
		if err := p.(Updater).SetAddresses(ctx(t), "nas.example.org", ip, netip.Addr{}); err != nil {
			t.Fatal(err)
		}
	}
	if len(records) != 1 || records[0].Content != "198.51.100.3" {
		t.Fatal(records)
	}
	bad, _ := New(Config{Type: INWX, User: "u", Password: "wrong"}, Options{BaseURL: server.URL})
	if err := bad.(Challenger).ClearTXT(ctx(t), "x.example.org"); err == nil || !strings.Contains(err.Error(), "Authentication") {
		t.Fatal(err)
	}
}

// --- DuckDNS ---

func TestDuckDNS(t *testing.T) {
	var queries []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := map[string]string{}
		for k := range r.URL.Query() {
			q[k] = r.URL.Query().Get(k)
		}
		queries = append(queries, q)
		if q["token"] != "duck" {
			io.WriteString(w, "KO")
			return
		}
		io.WriteString(w, "OK\n")
	}))
	defer server.Close()
	p, _ := New(Config{Type: DuckDNS, Token: "duck"}, Options{BaseURL: server.URL})
	ch := p.(Challenger)
	if ch.MultipleTXT() {
		t.Error("DuckDNS keeps one TXT record")
	}
	if err := ch.SetTXT(ctx(t), "_acme-challenge.mynas.duckdns.org", []string{"val"}); err != nil {
		t.Fatal(err)
	}
	if err := p.(Updater).SetAddresses(ctx(t), "mynas.duckdns.org", v4, v6); err != nil {
		t.Fatal(err)
	}
	if q := queries[0]; q["domains"] != "mynas" || q["txt"] != "val" {
		t.Error(q)
	}
	if q := queries[1]; q["ip"] != "203.0.113.7" || q["ipv6"] != "2001:db8::7" {
		t.Error(q)
	}
	if err := ch.SetTXT(ctx(t), "nas.example.com", []string{"x"}); err == nil {
		t.Error("name outside duckdns.org")
	}
	bad, _ := New(Config{Type: DuckDNS, Token: "secret-token"}, Options{BaseURL: server.URL})
	if err := bad.(Updater).SetAddresses(ctx(t), "mynas.duckdns.org", v4, netip.Addr{}); err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatal(err)
	}
	// A connection error must not show the address with the token in it.
	server.Close()
	err := bad.(Updater).SetAddresses(ctx(t), "mynas.duckdns.org", v4, netip.Addr{})
	if err == nil || strings.Contains(err.Error(), "secret-token") {
		t.Fatal(err)
	}
}

// --- dyndns2 ---

func TestDynDNS2(t *testing.T) {
	var got []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.URL.RequestURI())
		user, pass, _ := r.BasicAuth()
		if r.URL.Path == "/nic/update" && (user != "me" || pass != "pw") {
			io.WriteString(w, "badauth")
			return
		}
		io.WriteString(w, "good 203.0.113.7")
	}))
	defer server.Close()
	p, _ := New(Config{Type: DynDNS2, Server: server.URL + "/nic/update", User: "me", Password: "pw"}, Options{})
	if err := p.(Updater).SetAddresses(ctx(t), "nas.example.net", v4, v6); err != nil {
		t.Fatal(err)
	}
	if got[0] != "/nic/update?hostname=nas.example.net&myip=203.0.113.7%2C2001%3Adb8%3A%3A7" {
		t.Error(got[0])
	}
	tmpl, _ := New(Config{Type: DynDNS2, Server: server.URL + "/fritz?host=<domain>&myip=<ipaddr>,<ip6addr>&u=<username>"}, Options{})
	if err := tmpl.(Updater).SetAddresses(ctx(t), "nas.example.net", v4, netip.Addr{}); err != nil {
		t.Fatal(err)
	}
	if got[1] != "/fritz?host=nas.example.net&myip=203.0.113.7&u=" {
		t.Error(got[1])
	}
	if _, ok := p.(Challenger); ok {
		t.Error("dyndns2 must not be a Challenger")
	}
	bad, _ := New(Config{Type: DynDNS2, Server: server.URL + "/nic/update", User: "me", Password: "hidden"}, Options{})
	if err := bad.(Updater).SetAddresses(ctx(t), "nas.example.net", v4, netip.Addr{}); err == nil || !strings.Contains(err.Error(), "badauth") || strings.Contains(err.Error(), "hidden") {
		t.Fatal(err)
	}
}

// --- RFC 2136 ---

type dnsUpdate struct {
	zone    string
	records []string
}

// fakeNameServer accepts DNS UPDATE over TCP, checks the TSIG signature and
// records what was asked for.
func fakeNameServer(t *testing.T, keyName string, secret []byte) (string, *[]dnsUpdate) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var updates []dnsUpdate
	var mu sync.Mutex
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			var length [2]byte
			io.ReadFull(conn, length[:])
			msg := make([]byte, binary.BigEndian.Uint16(length[:]))
			io.ReadFull(conn, msg)
			rcode := byte(0)
			u, ok := parseUpdate(msg, keyName, secret)
			if !ok {
				rcode = 9
			} else {
				mu.Lock()
				updates = append(updates, u)
				mu.Unlock()
			}
			answer := append([]byte(nil), msg[:12]...)
			answer[2] |= 0x80
			answer[3] = rcode
			for i := 4; i < 12; i++ {
				answer[i] = 0
			}
			conn.Write(append(binary.BigEndian.AppendUint16(nil, uint16(len(answer))), answer...))
			conn.Close()
		}
	}()
	return ln.Addr().String(), &updates
}

func readName(msg []byte, off int) (string, int) {
	var labels []string
	for msg[off] != 0 {
		n := int(msg[off])
		labels = append(labels, string(msg[off+1:off+1+n]))
		off += 1 + n
	}
	return strings.Join(labels, "."), off + 1
}

func parseUpdate(msg []byte, keyName string, secret []byte) (dnsUpdate, bool) {
	if msg[2]>>3&0x0f != opUpdate || binary.BigEndian.Uint16(msg[10:]) != 1 {
		return dnsUpdate{}, false
	}
	zone, off := readName(msg, 12)
	off += 4
	var u dnsUpdate
	u.zone = zone
	for i := 0; i < int(binary.BigEndian.Uint16(msg[8:])); i++ {
		name, next := readName(msg, off)
		kind := binary.BigEndian.Uint16(msg[next:])
		class := binary.BigEndian.Uint16(msg[next+2:])
		length := int(binary.BigEndian.Uint16(msg[next+8:]))
		data := msg[next+10 : next+10+length]
		off = next + 10 + length
		value := ""
		switch kind {
		case dnsTypeTXT:
			if len(data) > 0 {
				value = string(data[1:])
			}
		case dnsTypeA, dnsTypeAAAA:
			if len(data) == 0 {
				break
			}
			addr, _ := netip.AddrFromSlice(data)
			value = addr.String()
		}
		u.records = append(u.records, fmt.Sprintf("%s %d %d %s", name, kind, class, value))
	}
	// The TSIG record: recompute the MAC over the message without it.
	tsigStart := off
	name, next := readName(msg, off)
	if name != keyName || binary.BigEndian.Uint16(msg[next:]) != dnsTypeTSIG {
		return dnsUpdate{}, false
	}
	rdata := msg[next+10:]
	algorithm, p := readName(rdata, 0)
	signedAt := rdata[p : p+8]
	macSize := int(binary.BigEndian.Uint16(rdata[p+8:]))
	mac := rdata[p+10 : p+10+macSize]
	unsigned := append([]byte(nil), msg[:tsigStart]...)
	binary.BigEndian.PutUint16(unsigned[10:], 0)
	vars := appendName(nil, keyName)
	vars = binary.BigEndian.AppendUint16(vars, dnsClassANY)
	vars = binary.BigEndian.AppendUint32(vars, 0)
	vars = appendName(vars, algorithm)
	vars = append(vars, signedAt...)
	vars = append(vars, 0, 0, 0, 0)
	h := hmac.New(tsigAlgorithms[algorithm], secret)
	h.Write(unsigned)
	h.Write(vars)
	return u, hmac.Equal(h.Sum(nil), mac)
}

func TestRFC2136(t *testing.T) {
	secret := []byte("0123456789abcdef")
	addr, updates := fakeNameServer(t, "nas-key", secret)
	cfg := Config{Type: RFC2136, Server: addr, Zone: "example.com", KeyName: "nas-key", Secret: base64.StdEncoding.EncodeToString(secret)}.Normalize()
	if err := cfg.Validate(ForChallenge); err != nil {
		t.Fatal(err)
	}
	p, err := New(cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.(Challenger).SetTXT(ctx(t), "_acme-challenge.nas.example.com", []string{"tok1", "tok2"}); err != nil {
		t.Fatal(err)
	}
	if err := p.(Updater).SetAddresses(ctx(t), "nas.example.com", v4, v6); err != nil {
		t.Fatal(err)
	}
	if len(*updates) != 2 {
		t.Fatal(*updates)
	}
	want := []string{
		"_acme-challenge.nas.example.com 16 255 ",
		"_acme-challenge.nas.example.com 16 1 tok1",
		"_acme-challenge.nas.example.com 16 1 tok2",
	}
	if u := (*updates)[0]; u.zone != "example.com" || !slices.Equal(u.records, want) {
		t.Fatalf("%+v", u)
	}
	if u := (*updates)[1]; len(u.records) != 4 || u.records[1] != "nas.example.com 1 1 203.0.113.7" || u.records[3] != "nas.example.com 28 1 2001:db8::7" {
		t.Fatalf("%+v", u)
	}
	wrong := cfg
	wrong.Secret = base64.StdEncoding.EncodeToString([]byte("other"))
	p2, _ := New(wrong, Options{})
	if err := p2.(Challenger).ClearTXT(ctx(t), "_acme-challenge.nas.example.com"); err == nil || !strings.Contains(err.Error(), "NOTAUTH") {
		t.Fatal(err)
	}
	if err := p.(Challenger).ClearTXT(ctx(t), "_acme-challenge.other.net"); err == nil {
		t.Fatal("name outside the zone")
	}
}
