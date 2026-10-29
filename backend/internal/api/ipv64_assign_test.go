package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider/ipv64fake"
)

type assignList struct {
	Assignments []assignment `json:"assignments"`
	DDNS        struct {
		Enabled   bool     `json:"enabled"`
		Provider  string   `json:"provider"`
		Hostnames []string `json:"hostnames"`
	} `json:"ddns"`
}

func (l assignList) find(name string) (assignment, bool) {
	for _, a := range l.Assignments {
		if a.Domain == name {
			return a, true
		}
	}
	return assignment{}, false
}

func TestIPv64Assignments(t *testing.T) {
	ts, fake := ipv64AreaServer(t)
	routes := appproxy.NewManager(ts.store)
	routes.Disabled = true
	routes.Hostname = func() string { return "nas" }
	routes.Reserved = []int{443}
	ts.server.opts.AppProxy = routes
	ts.server.ipv64.appPorts = func(app string) ([]int, bool) { return []int{2283}, app == "immich" }
	if res, body := ts.request("PUT", "/api/v1/ipv64/key", map[string]string{"apiKey": ipv64fake.Key}); res.StatusCode != 200 {
		t.Fatalf("key: %d %s", res.StatusCode, body)
	}
	list := func(res *http.Response, body []byte) assignList {
		t.Helper()
		if res.StatusCode != 200 {
			t.Fatalf("%d %s", res.StatusCode, body)
		}
		var l assignList
		decodeBody(t, body, &l)
		return l
	}
	l := list(ts.request("GET", "/api/v1/ipv64/assignments", nil))
	if len(l.Assignments) != 0 {
		t.Fatalf("assignments without any: %+v", l)
	}

	// The NAS: a name of dynamic DNS, which takes the key of the area.
	l = list(ts.request("PUT", "/api/v1/ipv64/assignments/nas.ipv64.de", map[string]string{"target": "nas"}))
	if a, ok := l.find("nas.ipv64.de"); !ok || a.Target != "nas" || a.DNS != "ddns" || !l.DDNS.Enabled || l.DDNS.Provider != "ipv64" {
		t.Fatalf("nas: %+v", l)
	}
	if ts.server.opts.DDNS.IPv64Key() != ipv64fake.Key {
		t.Fatal("dynamic DNS did not take the key of the area")
	}

	// An app without an HTTPS name gets one with the full domain.
	if res, body := ts.request("PUT", "/api/v1/ipv64/assignments/photos.nas.ipv64.de", map[string]any{"target": "app", "app": "immich", "port": 9999}); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "ipv64_port_not_published") {
		t.Fatalf("unpublished port: %d %s", res.StatusCode, body)
	}
	l = list(ts.request("PUT", "/api/v1/ipv64/assignments/photos.nas.ipv64.de", map[string]any{"target": "app", "app": "immich", "port": 2283}))
	if a, _ := l.find("photos.nas.ipv64.de"); a.Target != "app" || a.App != "immich" || a.Port != 2283 || a.DNS != "ddns" || a.URL != "https://photos.nas.ipv64.de" {
		t.Fatalf("app: %+v", l)
	}
	route, _ := routes.Route("immich")
	if !slices.Equal(route.Domains, []string{"photos.nas.ipv64.de"}) || !route.LocalOnly || route.Name != "immich" {
		t.Fatalf("route %+v", route)
	}

	// A target outside Docker; moving the name takes it from the app.
	if res, body := ts.request("PUT", "/api/v1/docker/proxy/targets/ha", map[string]any{"enabled": true, "name": "ha", "host": "192.168.1.30", "port": 8123}); res.StatusCode != 200 {
		t.Fatalf("target: %d %s", res.StatusCode, body)
	}
	if res, _ := ts.request("PUT", "/api/v1/ipv64/assignments/x.nas.ipv64.de", map[string]any{"target": "target", "targetId": "missing"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing target: %d", res.StatusCode)
	}
	l = list(ts.request("PUT", "/api/v1/ipv64/assignments/photos.nas.ipv64.de", map[string]any{"target": "target", "targetId": "ha"}))
	if a, _ := l.find("photos.nas.ipv64.de"); a.Target != "target" || a.App != "ha" {
		t.Fatalf("moved: %+v", l)
	}
	if route, _ := routes.Route("immich"); len(route.Domains) != 0 {
		t.Fatalf("app kept the moved name: %v", route.Domains)
	}

	// Under a wildcard of a DDNS name no further name is needed.
	if res, body := ts.request("PATCH", "/api/v1/ipv64/domains/nas.ipv64.de", map[string]any{"wildcard": true}); res.StatusCode != 200 {
		t.Fatalf("wildcard: %d %s", res.StatusCode, body)
	}
	l = list(ts.request("PUT", "/api/v1/ipv64/assignments/app.nas.ipv64.de", map[string]any{"target": "app", "app": "immich"}))
	if a, _ := l.find("app.nas.ipv64.de"); a.DNS != "wildcard" || slices.Contains(l.DDNS.Hostnames, "app.nas.ipv64.de") {
		t.Fatalf("wildcard: %+v", l)
	}

	// Over the internet through the CDN: confirmed, out of DDNS and the proxy.
	if res, body := ts.request("PUT", "/api/v1/ipv64/assignments/photos.nas.ipv64.de", map[string]any{"target": "app", "app": "immich", "public": true, "port": 2283}); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "ipv64_confirm_required") {
		t.Fatalf("without confirmation: %d %s", res.StatusCode, body)
	}
	if _, _, ok := routes.DomainRoute("photos.nas.ipv64.de"); !ok {
		t.Fatal("a refused publication changed the assignment")
	}
	l = list(ts.request("PUT", "/api/v1/ipv64/assignments/photos.nas.ipv64.de", map[string]any{"target": "app", "app": "immich", "public": true, "port": 2283, "confirm": true}))
	if a, _ := l.find("photos.nas.ipv64.de"); a.Target != "public" || a.App != "immich" || a.DNS != "" {
		t.Fatalf("public: %+v", l)
	}
	if len(fake.CDNs) != 1 || slices.Contains(l.DDNS.Hostnames, "photos.nas.ipv64.de") {
		t.Fatalf("CDN %d, ddns %v", len(fake.CDNs), l.DDNS.Hostnames)
	}
	if _, _, ok := routes.DomainRoute("photos.nas.ipv64.de"); ok {
		t.Fatal("the HTTPS name kept the published domain")
	}

	// Names that are no domain of the account, and web sessions only.
	if res, _ := ts.request("PUT", "/api/v1/ipv64/assignments/nas.example.org", map[string]string{"target": "nas"}); res.StatusCode != http.StatusNotFound {
		t.Fatalf("foreign domain: %d", res.StatusCode)
	}
	req := httptest.NewRequest("DELETE", "/api/v1/ipv64/assignments/nas.ipv64.de", nil).WithContext(context.WithValue(context.Background(), agentIdentity{}, "alex"))
	rec := httptest.NewRecorder()
	ts.server.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("assistant removed an assignment: %d", rec.Code)
	}

	// Taking back leaves the domain at IPv64.
	l = list(ts.request("DELETE", "/api/v1/ipv64/assignments/photos.nas.ipv64.de", nil))
	if _, ok := l.find("photos.nas.ipv64.de"); ok || len(fake.CDNs) != 0 {
		t.Fatalf("unpublish: %+v", l)
	}
	l = list(ts.request("DELETE", "/api/v1/ipv64/assignments/nas.ipv64.de", nil))
	if slices.Contains(l.DDNS.Hostnames, "nas.ipv64.de") || fake.Domains["nas.ipv64.de"] == nil {
		t.Fatalf("remove nas: %+v", l)
	}

	// Another provider of dynamic DNS keeps its names.
	dyn := map[string]any{"hostnames": []string{"nas.example.org"}, "ipv4": true, "provider": map[string]string{"type": "cloudflare", "token": "cf"}}
	if res, body := ts.request("PUT", "/api/v1/network/ddns", dyn); res.StatusCode != 200 {
		t.Fatalf("ddns: %d %s", res.StatusCode, body)
	}
	if res, body := ts.request("PUT", "/api/v1/ipv64/assignments/nas.ipv64.de", map[string]string{"target": "nas"}); res.StatusCode != http.StatusConflict || !strings.Contains(string(body), "ddns_other_provider") {
		t.Fatalf("other provider: %d %s", res.StatusCode, body)
	}
}

// The field "Own domains" of the HTTPS names (app editor, Settings › HTTPS
// names for apps) and the assignments of IPv64 are one list: a domain of the
// account entered there by hand is an assignment, and taking the assignment
// back takes it out of the field, leaving the other domains.
func TestIPv64AssignmentsShareOwnDomains(t *testing.T) {
	ts, _ := ipv64AreaServer(t)
	routes := appproxy.NewManager(ts.store)
	routes.Disabled = true
	routes.Hostname = func() string { return "nas" }
	routes.Reserved = []int{443}
	ts.server.opts.AppProxy = routes
	ts.server.ipv64.appPorts = func(app string) ([]int, bool) { return []int{2283}, app == "immich" }
	if res, body := ts.request("PUT", "/api/v1/ipv64/key", map[string]string{"apiKey": ipv64fake.Key}); res.StatusCode != 200 {
		t.Fatalf("key: %d %s", res.StatusCode, body)
	}
	if err := routes.SetRoute("immich", appproxy.Route{Enabled: true, Name: "immich", Port: 2283, Scheme: "http", Domains: []string{"photos.nas.ipv64.de", "photos.example.org"}}); err != nil {
		t.Fatal(err)
	}
	res, body := ts.request("GET", "/api/v1/ipv64/assignments", nil)
	if res.StatusCode != 200 {
		t.Fatalf("%d %s", res.StatusCode, body)
	}
	var l assignList
	decodeBody(t, body, &l)
	if a, ok := l.find("photos.nas.ipv64.de"); !ok || a.Target != "app" || a.App != "immich" || a.Port != 2283 {
		t.Fatalf("own domain not shown as assignment: %+v", l)
	}
	if _, ok := l.find("photos.example.org"); ok {
		t.Fatalf("a domain outside the account became an assignment: %+v", l)
	}
	if res, body := ts.request("DELETE", "/api/v1/ipv64/assignments/photos.nas.ipv64.de", nil); res.StatusCode != 200 {
		t.Fatalf("remove: %d %s", res.StatusCode, body)
	}
	if route, _ := routes.Route("immich"); !slices.Equal(route.Domains, []string{"photos.example.org"}) {
		t.Fatalf("own domains after taking the assignment back: %v", route.Domains)
	}
}

func TestIPv64DynDNSTab(t *testing.T) {
	ts, _ := ipv64AreaServer(t)
	settings := map[string]any{"enabled": true, "hostnames": []string{"nas.ipv64.de"}, "ipv4": true, "ipv6": false, "interval": 30, "lookupURL": "https://ipv64.net/api/v2/ip", "interface": "eth0", "ipv6Prefix": "auto"}
	if res, body := ts.request("PUT", "/api/v1/ipv64/ddns", settings); res.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), "ipv64_no_key") {
		t.Fatalf("without a key: %d %s", res.StatusCode, body)
	}
	ts.request("PUT", "/api/v1/ipv64/key", map[string]string{"apiKey": ipv64fake.Key})
	res, body := ts.request("PUT", "/api/v1/ipv64/ddns", settings)
	if res.StatusCode != 200 || strings.Contains(string(body), ipv64fake.Key) {
		t.Fatalf("save: %d %s", res.StatusCode, body)
	}
	st := ts.server.opts.DDNS.Status()
	if !st.Settings.Enabled || st.Settings.Interval != 30 || st.Settings.IPv6 || st.Settings.Interface != "eth0" || st.Settings.Provider.IPv6Prefix != "auto" || ts.server.opts.DDNS.IPv64Key() != ipv64fake.Key {
		t.Fatalf("stored %+v", st.Settings)
	}
	// Both pages show the same settings.
	if res, body := ts.request("GET", "/api/v1/network/ddns", nil); res.StatusCode != 200 || !strings.Contains(string(body), `"interval":30`) {
		t.Fatalf("ddns page: %s", body)
	}
	// Saving again keeps the key without sending it.
	settings["interval"] = 60
	if res, body := ts.request("PUT", "/api/v1/ipv64/ddns", settings); res.StatusCode != 200 {
		t.Fatalf("again: %d %s", res.StatusCode, body)
	}
	if ts.server.opts.DDNS.IPv64Key() != ipv64fake.Key {
		t.Fatal("key lost")
	}
	settings["interval"] = 3
	if res, _ := ts.request("PUT", "/api/v1/ipv64/ddns", settings); res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad interval: %d", res.StatusCode)
	}
}
