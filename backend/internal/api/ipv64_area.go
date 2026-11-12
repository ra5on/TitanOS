package api

import (
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
)

// ipv64Area is IPv64.net as a provider of dynamic DNS: account, domains
// and the healthcheck "watch reachability" (ipv64.go). The account area
// of IPv64 and "public through IPv64" of Docker apps are gone (PLAN 79-2).
// An area with narrow dependencies (PLAN 8a, see fansArea): ddns and
// letsEncrypt hold the stored keys and host names.
type ipv64Area struct {
	ddns        *ddns.Manager
	letsEncrypt *letsencrypt.Manager
}

// ipv64Area is the area of the server. It reads the managers when it is
// asked for, so the agent's list of addresses (agent_addresses.go) sees
// the same ones.
func (s *Server) ipv64Area() ipv64Area {
	return ipv64Area{ddns: s.opts.DDNS, letsEncrypt: s.opts.LetsEncrypt}
}

func (a ipv64Area) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	// IPv64.net: POST everywhere, so that a typed-in API key stays out of addresses.
	mux.HandleFunc("POST /api/v1/network/ipv64/account", admin(a.handleIPv64Account))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains", admin(a.handleIPv64Domains))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains/create", admin(a.handleIPv64DomainCreate))
	mux.HandleFunc("POST /api/v1/network/ipv64/domains/change", admin(a.handleIPv64DomainChange))
	mux.HandleFunc("POST /api/v1/network/ipv64/records", admin(a.handleIPv64Records))
	mux.HandleFunc("POST /api/v1/network/ipv64/records/add", admin(a.handleIPv64RecordAdd))
	mux.HandleFunc("POST /api/v1/network/ipv64/records/delete", admin(a.handleIPv64RecordDelete))
	mux.HandleFunc("POST /api/v1/network/ipv64/healthchecks", admin(a.handleIPv64Healthchecks))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/on", admin(a.handleIPv64HeartbeatOn))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/off", admin(a.handleIPv64HeartbeatOff))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/status", admin(a.handleIPv64HeartbeatStatus))
	mux.HandleFunc("POST /api/v1/network/ipv64/heartbeat/test", admin(a.handleIPv64HeartbeatTest))
}

// dynamicDNS answers when dynamic DNS is not available.
func (a ipv64Area) dynamicDNS(w http.ResponseWriter) bool {
	return ddnsArea{ddns: a.ddns}.dynamicDNS(w)
}

// browserOnly refuses requests made by the assistant, Home Assistant or
// an API token: they act in the name of an account but are no person at a
// web session.
func (s *Server) browserOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_, token := r.Context().Value(apiTokenIdentity{}).(string)
		_, agent := r.Context().Value(inProcessIdentity{}).(string)
		if token || agent {
			writeError(w, http.StatusForbidden, "browser_session_required", "this needs a web session")
			return
		}
		next(w, r)
	}
}

// ddnsHosts are the names of dynamic DNS if it works with IPv64.
func (a ipv64Area) ddnsHosts() []string {
	st := a.ddns.Status()
	if st.Settings.Provider.Type != dnsprovider.IPv64Net {
		return []string{}
	}
	return st.Settings.Hostnames
}
