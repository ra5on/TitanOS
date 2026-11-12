package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/ra5on/TitanOS/backend/internal/appproxy"
	"github.com/ra5on/TitanOS/backend/internal/ddns"
	"github.com/ra5on/TitanOS/backend/internal/dnsprovider"
	"github.com/ra5on/TitanOS/backend/internal/docker"
	"github.com/ra5on/TitanOS/backend/internal/firewall"
	"github.com/ra5on/TitanOS/backend/internal/ipv64"
	"github.com/ra5on/TitanOS/backend/internal/letsencrypt"
)

// ipv64State keeps what the routes remember in RAM.
type ipv64State struct {
	// appPorts replaces the look at Docker in tests.
	appPorts func(app string) ([]int, bool)
}

// ipv64Area is IPv64.net as a provider of dynamic DNS (account, domains,
// the healthcheck "watch reachability") and the publishing of apps; an
// area with narrow dependencies (PLAN 8a, see fansArea). ddns and
// letsEncrypt hold the stored keys and host names, ipv64 the published
// apps, firewall and appProxy what publishing an app touches, docker the ports of an app, reserved the ports of titand that
// are never published, state what the routes remember in RAM (state of the
// server; tests replace its look at Docker).
type ipv64Area struct {
	ddns        *ddns.Manager
	letsEncrypt *letsencrypt.Manager
	ipv64       *ipv64.Manager
	firewall    *firewall.Manager
	appProxy    *appproxy.Manager
	docker      *docker.Manager
	reserved    []int
	state       *ipv64State
}

// ipv64Area is the area of the server. It reads the managers when it is
// asked for, so the agent's list of addresses (agent_addresses.go) sees
// the same ones.
func (s *Server) ipv64Area() ipv64Area {
	return ipv64Area{ddns: s.opts.DDNS, letsEncrypt: s.opts.LetsEncrypt, ipv64: s.opts.IPv64, firewall: s.opts.Firewall,
		appProxy: s.opts.AppProxy, docker: s.opts.Docker, reserved: s.opts.IPv64Reserved, state: &s.ipv64}
}

func (a ipv64Area) register(mux *routeMux, admin, browserOnly func(http.HandlerFunc) http.HandlerFunc) {
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
	// "Public through IPv64" of a Docker app: reading is GET (the key never
	// comes with a request); publishing only from a web session, never by
	// the assistant, Home Assistant or an API token.
	mux.HandleFunc("GET /api/v1/ipv64/domains", admin(a.handleIPv64AppDomains))
	mux.HandleFunc("GET /api/v1/ipv64/apps/{app}", admin(a.handleIPv64App))
	mux.HandleFunc("PUT /api/v1/ipv64/apps/{app}", admin(browserOnly(a.handleIPv64Publish)))
	mux.HandleFunc("DELETE /api/v1/ipv64/apps/{app}", admin(browserOnly(a.handleIPv64Unpublish)))
}

// dynamicDNS answers when dynamic DNS is not available.
func (a ipv64Area) dynamicDNS(w http.ResponseWriter) bool {
	return ddnsArea{ddns: a.ddns}.dynamicDNS(w)
}

// browserOnly refuses requests made by the assistant, Home Assistant or
// an API token: they act in the name of an account but are no person at a
// web session. Creating keys and everything that changes the account at
// IPv64 needs one.
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

// ipv64AreaFailure is ipv64Failure plus the answers of the bigger API.
func ipv64AreaFailure(w http.ResponseWriter, err error) {
	var apiErr *dnsprovider.APIError
	switch {
	case errors.Is(err, ipv64.ErrNoKey):
		writeError(w, http.StatusBadRequest, "ipv64_no_key", "the API key of the IPv64 account is needed")
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusNotFound:
		writeError(w, http.StatusNotFound, "ipv64_not_found", apiErr.Error())
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusConflict:
		writeError(w, http.StatusConflict, "ipv64_conflict", apiErr.Error())
	case errors.As(err, &apiErr) && apiErr.Status == http.StatusForbidden:
		writeError(w, http.StatusConflict, "ipv64_limit", apiErr.Error())
	case errors.As(err, &apiErr) && (apiErr.Status == http.StatusUnprocessableEntity || apiErr.Status == http.StatusRequestEntityTooLarge || apiErr.Status == http.StatusUnsupportedMediaType):
		writeError(w, http.StatusBadRequest, "ipv64_invalid", apiErr.Error())
	default:
		ipv64Failure(w, err)
	}
}

// ipv64Run calls fn with a client and writes its answer: nil out is 204.
func (a ipv64Area) ipv64Run(w http.ResponseWriter, r *http.Request, timeout time.Duration, fn func(ctx context.Context, c *dnsprovider.IPv64) (int, any, error)) {
	client, err := a.ipv64.Client()
	if err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	status, out, err := fn(ctx, client)
	if err != nil {
		ipv64AreaFailure(w, err)
		return
	}
	if status == 0 {
		status = http.StatusOK
	}
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, status, out)
}

// ddnsHosts are the names of dynamic DNS if it works with IPv64.
func (a ipv64Area) ddnsHosts() []string {
	st := a.ddns.Status()
	if st.Settings.Provider.Type != dnsprovider.IPv64Net {
		return []string{}
	}
	return st.Settings.Hostnames
}
