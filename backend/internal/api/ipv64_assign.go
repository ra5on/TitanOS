package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/ipv64"
)

// Settings › IPv64 › Assignments: one table "domain → target" for the
// domains of the IPv64 account, and Settings › IPv64 › DynDNS. A domain
// goes to
//   - the NAS: a name of dynamic DNS (A/AAAA at the public addresses),
//     the web UI answers;
//   - an app or a target outside Docker (VM, device): a full domain of
//     its HTTPS name (package appproxy) plus the DNS of the NAS;
//   - an app over the internet: published through the CDN of IPv64
//     (publishApp, with the warning and the confirmation).
// Taking an assignment back leaves the domain and its records at IPv64.

// assignment is one row of the table.
type assignment struct {
	Domain string `json:"domain"`
	// Zone is the domain of the account holding it, Prefix the part before.
	Zone   string `json:"zone"`
	Prefix string `json:"prefix,omitempty"`
	// Target is nas, app, target or public.
	Target string `json:"target"`
	// App is the app (app, public) or the id of the target.
	App    string `json:"app,omitempty"`
	Port   int    `json:"port,omitempty"`
	Scheme string `json:"scheme,omitempty"`
	// DNS says how the name points at the NAS: "ddns" (a name of dynamic
	// DNS), "wildcard" (under a domain of dynamic DNS with wildcard) or
	// "" (records of its own, e.g. through the CDN).
	DNS string `json:"dns,omitempty"`
	// URL is where the target answers under this domain.
	URL string `json:"url,omitempty"`
}

// assignRequest is the body of PUT /ipv64/assignments/{domain}.
type assignRequest struct {
	// Target is nas, app or target.
	Target string `json:"target"`
	// App is the app for target app; TargetID the target outside Docker.
	App      string `json:"app"`
	TargetID string `json:"targetId"`
	// Port and Scheme make a new HTTPS name for an app without one, or
	// are the port of the app published through the CDN.
	Port   int    `json:"port"`
	Scheme string `json:"scheme"`
	// LocalOnly is the access of a new HTTPS name (nil: home network only).
	LocalOnly *bool `json:"localOnly"`
	// Public publishes the app through the CDN of IPv64 instead; it needs
	// Confirm (the warning dialog) and may open the firewall (Firewall).
	Public   bool `json:"public"`
	Firewall bool `json:"firewall"`
	Confirm  bool `json:"confirm"`
}

// refusedError carries a refusal of publishApp's checks through ipv64Run.
type refusedError struct{ publishRefusal }

func (e *refusedError) Error() string { return e.message }

// assignFailure writes errors of dynamic DNS and the app proxy as their
// own codes and leaves the rest to ipv64AreaFailure.
func assignFailure(w http.ResponseWriter, err error) {
	var refused *refusedError
	switch {
	case errors.As(err, &refused):
		writeError(w, refused.status, refused.code, refused.message)
	case errors.Is(err, ddns.ErrOtherProvider):
		writeError(w, http.StatusConflict, "ddns_other_provider", "dynamic DNS keeps names up to date with another provider")
	case errors.Is(err, ddns.ErrInvalid):
		writeError(w, http.StatusBadRequest, "invalid_ddns", err.Error())
	case errors.Is(err, ddns.ErrNotLoaded):
		writeError(w, http.StatusServiceUnavailable, "config_unavailable", err.Error())
	case errors.Is(err, appproxy.ErrInvalid):
		writeError(w, http.StatusBadRequest, "proxy_invalid", err.Error())
	case errors.Is(err, appproxy.ErrNameTaken):
		writeError(w, http.StatusConflict, "proxy_name_taken", err.Error())
	default:
		ipv64AreaFailure(w, err)
	}
}

// assignRun is ipv64Run with the errors of assignFailure.
func (s *Server) assignRun(w http.ResponseWriter, r *http.Request, timeout time.Duration, fn func(ctx context.Context, c *dnsprovider.IPv64) (any, error)) {
	if s.opts.DDNS == nil || s.opts.AppProxy == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "dynamic DNS or the app proxy is not available")
		return
	}
	client, err := s.opts.IPv64.Client()
	if err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	out, err := fn(ctx, client)
	if err != nil {
		assignFailure(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// zoneOf returns the domain of the account a name belongs to (the longest
// that holds it) and the prefix before it.
func zoneOf(domains []dnsprovider.IPv64Domain, name string) (dnsprovider.IPv64Domain, string, bool) {
	var best dnsprovider.IPv64Domain
	found := false
	for _, d := range domains {
		if (name == d.Name || strings.HasSuffix(name, "."+d.Name)) && len(d.Name) > len(best.Name) {
			best, found = d, true
		}
	}
	if !found {
		return best, "", false
	}
	return best, strings.TrimSuffix(strings.TrimSuffix(name, best.Name), "."), true
}

// ddnsHosts are the names of dynamic DNS if it works with IPv64.
func (s *Server) ddnsHosts() []string {
	st := s.opts.DDNS.Status()
	if st.Settings.Provider.Type != dnsprovider.IPv64Net {
		return []string{}
	}
	return st.Settings.Hostnames
}

// wildcardCovered tells whether a name already points at the NAS through
// the wildcard of a domain dynamic DNS keeps up to date.
func wildcardCovered(zone dnsprovider.IPv64Domain, name string, hosts []string) bool {
	return name != zone.Name && zone.Wildcard && slices.Contains(hosts, zone.Name)
}

// publishedName is the full name an app was published under.
func publishedName(p ipv64.Published) string {
	if p.Prefix == "" {
		return p.Domain
	}
	return p.Prefix + "." + p.Domain
}

// assignments lists the table for the domains of the account.
func (s *Server) assignments(ctx context.Context, c *dnsprovider.IPv64) (map[string]any, error) {
	domains, err := c.Domains(ctx)
	if err != nil {
		return nil, err
	}
	hosts := s.ddnsHosts()
	byName := map[string]*assignment{}
	add := func(name string, a assignment) {
		zone, prefix, ok := zoneOf(domains, name)
		if !ok {
			return
		}
		a.Domain, a.Zone, a.Prefix = name, zone.Name, prefix
		if a.Target != "public" {
			switch {
			case slices.Contains(hosts, name):
				a.DNS = "ddns"
			case wildcardCovered(zone, name, hosts):
				a.DNS = "wildcard"
			}
		}
		byName[name] = &a
	}
	for _, name := range hosts {
		add(name, assignment{Target: "nas", URL: "https://" + name})
	}
	proxy := s.opts.AppProxy.Info()
	for _, list := range []struct {
		kind string
		apps []appproxy.AppInfo
	}{{"app", proxy.Apps}, {"target", proxy.Targets}} {
		for _, info := range list.apps {
			for _, name := range info.Domains {
				a := assignment{Target: list.kind, App: info.App, Port: info.Port, Scheme: info.Scheme}
				for _, u := range info.URLs {
					if strings.HasPrefix(u, "https://"+name) {
						a.URL = u
					}
				}
				add(name, a)
			}
		}
	}
	for _, p := range s.opts.IPv64.Settings().Published {
		name := publishedName(p)
		add(name, assignment{Target: "public", App: p.App, Port: p.Port, URL: "https://" + name})
	}
	out := make([]assignment, 0, len(byName))
	for _, a := range byName {
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	st := s.opts.DDNS.Status()
	return map[string]any{
		"domains": domains, "assignments": out, "zones": dnsprovider.IPv64Zones,
		"ddns":     map[string]any{"enabled": st.Settings.Enabled, "provider": st.Settings.Provider.Type, "hostnames": st.Settings.Hostnames, "max": 8},
		"firewall": s.opts.Firewall.Enabled(),
	}, nil
}

func (s *Server) handleIPv64Assignments(w http.ResponseWriter, r *http.Request) {
	s.assignRun(w, r, 30*time.Second, func(ctx context.Context, c *dnsprovider.IPv64) (any, error) {
		return s.assignments(ctx, c)
	})
}

var proxyLabelChars = regexp.MustCompile(`[^a-z0-9-]+`)

// proxyLabel makes a DNS label out of an app's name (lib/app-proxy.ts
// suggestedProxyName).
func proxyLabel(app string) string {
	label := strings.Trim(proxyLabelChars.ReplaceAllString(strings.ToLower(app), "-"), "-")
	if len(label) > 63 {
		label = strings.TrimRight(label[:63], "-")
	}
	return label
}

// releaseDomain takes a name from wherever it is assigned except keep
// ("public", "proxy", "ddns"): the CDN publication (routed back), the
// HTTPS names and dynamic DNS.
func (s *Server) releaseDomain(ctx context.Context, c *dnsprovider.IPv64, name string, keep ...string) error {
	if !slices.Contains(keep, "public") {
		for _, p := range s.opts.IPv64.Settings().Published {
			if publishedName(p) != name {
				continue
			}
			if err := s.unpublish(ctx, c, p); err != nil {
				return err
			}
			if err := s.opts.IPv64.RemovePublished(p.App); err != nil {
				return err
			}
		}
	}
	if !slices.Contains(keep, "proxy") {
		if err := s.opts.AppProxy.RemoveDomain(name); err != nil {
			return err
		}
	}
	if !slices.Contains(keep, "ddns") {
		return s.opts.DDNS.RemoveHostname(name)
	}
	return nil
}

// handleIPv64Assign points a domain of the account at the NAS, an app or
// a target, taking it from where it pointed before.
func (s *Server) handleIPv64Assign(w http.ResponseWriter, r *http.Request) {
	name, err := domainParam(r)
	if err != nil {
		assignFailure(w, err)
		return
	}
	var req assignRequest
	if !readJSON(w, r, &req) {
		return
	}
	req.App, req.TargetID = strings.TrimSpace(req.App), strings.TrimSpace(req.TargetID)
	if req.Scheme == "" {
		req.Scheme = "http"
	}
	if req.Scheme != "http" && req.Scheme != "https" {
		writeError(w, http.StatusBadRequest, "invalid_ipv64", "scheme")
		return
	}
	switch {
	case req.Target == "nas":
	case req.Target == "app" && req.App != "":
	case req.Target == "target" && req.TargetID != "" && !req.Public:
	default:
		writeError(w, http.StatusBadRequest, "invalid_ipv64", "target")
		return
	}
	noteTarget(r, name, req.Target, req.App+req.TargetID)
	s.assignRun(w, r, 3*time.Minute, func(ctx context.Context, c *dnsprovider.IPv64) (any, error) {
		domains, err := c.Domains(ctx)
		if err != nil {
			return nil, err
		}
		zone, prefix, ok := zoneOf(domains, name)
		if !ok {
			return nil, &dnsprovider.APIError{Provider: "IPv64", Status: http.StatusNotFound, Message: "no domain of this account holds " + name}
		}
		if req.Target == "app" && req.Public {
			return s.assignPublic(ctx, c, name, zone, prefix, req, currentUser(r).Name)
		}
		settings := s.opts.AppProxy.Settings()
		_, hasRoute := settings.Apps[req.App]
		switch req.Target {
		case "target":
			if _, ok := settings.Targets[req.TargetID]; !ok {
				return nil, &refusedError{publishRefusal{http.StatusNotFound, "not_found", "no such target"}}
			}
		case "app":
			if !hasRoute {
				// A new HTTPS name: the app must publish the port.
				ports, found := s.appPorts(ctx, req.App)
				if !found || !slices.Contains(ports, req.Port) {
					return nil, &refusedError{publishRefusal{http.StatusBadRequest, "ipv64_port_not_published", "the app publishes no such TCP port on the NAS"}}
				}
				if s.reservedPort(req.Port) {
					return nil, &refusedError{publishRefusal{http.StatusBadRequest, "ipv64_reserved_port", "the ports of the TitanOS web UI are never used"}}
				}
			}
		}
		// Taken from the CDN first, so that dynamic DNS may set the records.
		if err := s.releaseDomain(ctx, c, name, "proxy", "ddns"); err != nil {
			return nil, err
		}
		if !wildcardCovered(zone, name, s.ddnsHosts()) {
			key, _ := s.opts.IPv64.Key()
			if err := s.opts.DDNS.AddHostname(name, key); err != nil {
				return nil, err
			}
		}
		if err := s.opts.AppProxy.RemoveDomain(name); err != nil {
			return nil, err
		}
		switch {
		case req.Target == "target":
			err = s.opts.AppProxy.AddDomain(true, req.TargetID, name)
		case req.Target == "app" && hasRoute:
			err = s.opts.AppProxy.AddDomain(false, req.App, name)
		case req.Target == "app":
			local := req.LocalOnly == nil || *req.LocalOnly
			err = s.opts.AppProxy.SetRoute(req.App, appproxy.Route{Enabled: true, Name: proxyLabel(req.App), Port: req.Port, Scheme: req.Scheme, LocalOnly: local, Domains: []string{name}})
		}
		if err != nil {
			return nil, err
		}
		slog.Info("IPv64 domain assigned", "domain", name, "target", req.Target, "app", req.App+req.TargetID, "by", currentUser(r).Name)
		return s.assignments(ctx, c)
	})
}

// assignPublic publishes an app through the CDN under a name, after the
// name left dynamic DNS and the HTTPS names.
func (s *Server) assignPublic(ctx context.Context, c *dnsprovider.IPv64, name string, zone dnsprovider.IPv64Domain, prefix string, req assignRequest, by string) (any, error) {
	if p, ok := s.opts.IPv64.PublishedApp(req.App); ok && publishedName(p) == name && p.Port == req.Port {
		return s.assignments(ctx, c)
	}
	pub := publishRequest{Domain: zone.Name, Prefix: prefix, Port: req.Port, Scheme: req.Scheme, Firewall: req.Firewall, Confirm: req.Confirm}
	if strings.Contains(prefix, ".") {
		return nil, &refusedError{publishRefusal{http.StatusBadRequest, "invalid_ipv64", "only one label before the domain can go through the CDN"}}
	}
	// Checked before anything changes; another name of this app is then
	// taken back first.
	if refusal := s.checkPublish(ctx, req.App, &pub, true); refusal != nil {
		return nil, &refusedError{*refusal}
	}
	if p, ok := s.opts.IPv64.PublishedApp(req.App); ok {
		if err := s.unpublish(ctx, c, p); err != nil {
			return nil, err
		}
		if err := s.opts.IPv64.RemovePublished(p.App); err != nil {
			return nil, err
		}
	}
	if err := s.releaseDomain(ctx, c, name); err != nil {
		return nil, err
	}
	if _, err := s.publishApp(ctx, c, req.App, pub, by); err != nil {
		return nil, err
	}
	return s.assignments(ctx, c)
}

// handleIPv64Unassign takes an assignment back; the domain and its records
// stay at IPv64.
func (s *Server) handleIPv64Unassign(w http.ResponseWriter, r *http.Request) {
	name, err := domainParam(r)
	if err != nil {
		assignFailure(w, err)
		return
	}
	noteTarget(r, name)
	s.assignRun(w, r, 3*time.Minute, func(ctx context.Context, c *dnsprovider.IPv64) (any, error) {
		if err := s.releaseDomain(ctx, c, name); err != nil {
			return nil, err
		}
		slog.Info("IPv64 domain assignment removed", "domain", name, "by", currentUser(r).Name)
		return s.assignments(ctx, c)
	})
}

// ipv64DDNSRequest is everything of dynamic DNS that Settings › IPv64 ›
// DynDNS sets; the provider is always IPv64 with the key of the area.
type ipv64DDNSRequest struct {
	Enabled    bool     `json:"enabled"`
	Hostnames  []string `json:"hostnames"`
	IPv4       bool     `json:"ipv4"`
	IPv6       bool     `json:"ipv6"`
	Interval   int      `json:"interval"`
	LookupURL  string   `json:"lookupURL"`
	Interface  string   `json:"interface"`
	IPv6Prefix string   `json:"ipv6Prefix"`
}

// handleIPv64DDNS stores dynamic DNS from Settings › IPv64: the same
// settings as Settings › Dynamic DNS, with IPv64 as provider. The key is
// the one dynamic DNS has for IPv64, else the one of this area; no second
// key is typed in.
func (s *Server) handleIPv64DDNS(w http.ResponseWriter, r *http.Request) {
	var req ipv64DDNSRequest
	if !readJSON(w, r, &req) || !s.dynamicDNS(w) {
		return
	}
	current := s.opts.DDNS.Status()
	next := current.Settings
	next.Enabled, next.Hostnames, next.IPv4, next.IPv6 = req.Enabled, req.Hostnames, req.IPv4, req.IPv6
	next.Interval, next.LookupURL, next.Interface = req.Interval, req.LookupURL, req.Interface
	if next.Hostnames == nil {
		next.Hostnames = []string{}
	}
	// The stored secrets come back in Set for the same provider.
	provider := dnsprovider.Config{Type: dnsprovider.IPv64Net, IPv6Prefix: req.IPv6Prefix}
	if current.Settings.Provider.Type != dnsprovider.IPv64Net || !current.Secrets["token"] {
		key, _ := s.opts.IPv64.Key()
		if key == "" && !(current.Settings.Provider.Type == dnsprovider.IPv64Net && current.Secrets["updateToken"]) {
			writeError(w, http.StatusBadRequest, "ipv64_no_key", "the API key of the IPv64 account is needed")
			return
		}
		provider.Token = key
	}
	next.Provider = provider
	if err := s.opts.DDNS.Set(next, nil); err != nil {
		assignFailure(w, err)
		return
	}
	slog.Info("dynamic DNS settings changed in Settings › IPv64", "by", currentUser(r).Name, "ip", remoteIP(r))
	noteTarget(r, s.opts.DDNS.Status().Settings.Hostnames...)
	writeJSON(w, http.StatusOK, s.opts.DDNS.Status())
}
