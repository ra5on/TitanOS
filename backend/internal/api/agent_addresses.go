package api

import (
	"net/http"
	"slices"
	"strings"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/auth"
)

// agentAddress is one way the phone app of the agent can reach this NAS
// (PLAN 53): the home network, Tailscale, NetBird, dynamic DNS or an own
// domain with a certificate. Only what is set up appears; nothing is looked
// up on the internet.
type agentAddress struct {
	Kind  string `json:"kind"`
	URL   string `json:"url"`
	HTTPS bool   `json:"https"`
}

func (s *Server) agentAddresses(r *http.Request) []agentAddress {
	out := []agentAddress{}
	seen := map[string]bool{}
	add := func(kind, scheme, host string) {
		host = strings.TrimSuffix(strings.TrimSpace(host), ".")
		if host == "" || seen[host] {
			return
		}
		seen[host] = true
		out = append(out, agentAddress{Kind: kind, URL: scheme + "://" + host + "/agent", HTTPS: scheme == "https"})
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	add("lan", scheme, appproxy.PrimaryIPv4())
	if s.opts.Tailscale != nil {
		ts := s.opts.Tailscale.Status()
		if ts.Name != "" {
			if ts.HTTPS.State == "ok" {
				add("tailscale", "https", ts.Name)
			} else {
				add("tailscale", "http", ts.Name)
			}
		} else if len(ts.IPs) > 0 {
			add("tailscale", "http", ts.IPs[0])
		}
	}
	if s.opts.Netbird != nil {
		if nb := s.opts.Netbird.Status(); nb.FQDN != "" {
			add("netbird", "http", nb.FQDN)
		}
	}
	var domains []string
	if s.opts.LetsEncrypt != nil {
		domains = s.opts.LetsEncrypt.Status().Settings.Domains
	}
	for _, h := range s.ipv64Area().ddnsHosts() {
		if slices.Contains(domains, h) {
			add("ddns", "https", h)
		} else {
			add("ddns", "http", h)
		}
	}
	for _, d := range domains {
		if !strings.HasPrefix(d, "*.") {
			add("domain", "https", d)
		}
	}
	return out
}

// handleAgentAddresses lists the ways to reach the agent app, for the QR
// code under Settings › KI-Agent. Owners and administrators only.
func (s *Server) handleAgentAddresses(w http.ResponseWriter, r *http.Request) {
	if role := currentUser(r).Role; role != auth.RoleOwner && role != auth.RoleAdmin {
		writeError(w, http.StatusForbidden, "forbidden", "owners and administrators only")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"addresses": s.agentAddresses(r)})
}
