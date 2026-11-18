package api

import (
	"net/http"

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
	return agentAddressesOf(s.reachAddresses(), r.TLS != nil)
}

// agentAddressesOf gives each address its scheme: https with a trusted
// name, in the home network the scheme of the request (secure).
func agentAddressesOf(list []reachAddress, secure bool) []agentAddress {
	out := []agentAddress{}
	for _, a := range list {
		var https bool
		switch a.Kind {
		case "lan":
			// The scheme of the request: the home network has no trusted name.
			https = secure
		case "tailscale":
			https = a.TailscaleCert
		case "ddns":
			https = a.LetsEncrypt
		case "domain":
			https = true
		}
		scheme := "http"
		if https {
			scheme = "https"
		}
		out = append(out, agentAddress{Kind: a.Kind, URL: scheme + "://" + a.Host + "/agent", HTTPS: https})
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
