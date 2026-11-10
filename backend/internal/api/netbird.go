package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/netbird"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
)

// netbirdArea is the NetBird plugin; an area with narrow dependencies
// (PLAN 8a, see fansArea). plugins runs changes as a runtime action of the
// plugin; nil runs them directly.
type netbirdArea struct {
	netbird *netbird.Manager
	plugins *plugins.Manager
}

func (a netbirdArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/plugins/netbird", admin(a.handleNetbird))
	mux.HandleFunc("GET /api/v1/plugins/netbird/peers", admin(a.handleNetbirdPeers))
	mux.HandleFunc("PUT /api/v1/plugins/netbird", admin(a.handleNetbirdSettings))
	mux.HandleFunc("PUT /api/v1/plugins/netbird/setup-key", admin(a.handleNetbirdSetupKey))
	mux.HandleFunc("POST /api/v1/plugins/netbird/control/{action}", admin(a.handleNetbirdAction))
}

func (a netbirdArea) handleNetbird(w http.ResponseWriter, r *http.Request) {
	if a.netbird == nil {
		writeError(w, 503, "config_unavailable", "NetBird settings unavailable")
		return
	}
	writeJSON(w, 200, a.netbird.Status())
}
func writeNetbirdError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "netbird_failed"
	if errors.Is(err, netbird.ErrInvalid) {
		status = 400
	}
	if errors.Is(err, netbird.ErrSetupKey) {
		status, code = 409, "netbird_setup_key"
	}
	if errors.Is(err, netbird.ErrUnavailable) || errors.Is(err, netbird.ErrPending) || errors.Is(err, plugins.ErrDisabled) || errors.Is(err, plugins.ErrNotFound) || errors.Is(err, plugins.ErrUpdatePending) || errors.Is(err, plugins.ErrDriverPending) {
		status = 409
	}
	writeError(w, status, code, err.Error())
}

// handleNetbirdSettings starts a trial of the routing choices. The setup key
// is not part of this body; it has its own route.
func (a netbirdArea) handleNetbirdSettings(w http.ResponseWriter, r *http.Request) {
	if a.netbird == nil {
		writeError(w, 503, "config_unavailable", "NetBird settings unavailable")
		return
	}
	var settings netbird.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := a.netbirdRuntimeAction(func() error { return a.netbird.Set(settings) }); err != nil {
		writeNetbirdError(w, err)
		return
	}
	slog.Info("NetBird configuration applied on trial", "by", currentUser(r).Name)
	a.handleNetbird(w, r)
}

// handleNetbirdSetupKey stores (or with an empty value removes) the setup
// key. It is never returned; GET only says whether one is set.
func (a netbirdArea) handleNetbirdSetupKey(w http.ResponseWriter, r *http.Request) {
	if a.netbird == nil {
		writeError(w, 503, "config_unavailable", "NetBird settings unavailable")
		return
	}
	var body struct {
		SetupKey string `json:"setupKey"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := a.netbird.SetKey(body.SetupKey); err != nil {
		writeNetbirdError(w, err)
		return
	}
	slog.Info("NetBird setup key changed", "set", body.SetupKey != "", "by", currentUser(r).Name)
	a.handleNetbird(w, r)
}

func (a netbirdArea) handleNetbirdAction(w http.ResponseWriter, r *http.Request) {
	if a.netbird == nil {
		writeError(w, 503, "config_unavailable", "NetBird settings unavailable")
		return
	}
	action := r.PathValue("action")
	var err error
	switch action {
	case "confirm":
		err = a.netbird.Confirm()
	case "undo":
		err = a.netbird.Undo()
	default:
		err = a.netbirdRuntimeAction(func() error { return a.netbird.Action(action) })
	}
	if err != nil {
		writeNetbirdError(w, err)
		return
	}
	slog.Info("NetBird action", "action", action, "by", currentUser(r).Name)
	a.handleNetbird(w, r)
}

func (a netbirdArea) netbirdRuntimeAction(action func() error) error {
	if a.plugins != nil {
		return a.plugins.RuntimeAction("netbird", action)
	}
	return action()
}

// handleNetbirdPeers lists the NetBird network for the dashboard. Without an
// active plugin the answer is the state "inactive", not an error.
func (a netbirdArea) handleNetbirdPeers(w http.ResponseWriter, r *http.Request) {
	if a.netbird == nil {
		writeJSON(w, 200, netbird.Peers{State: "inactive", Peers: []netbird.Peer{}})
		return
	}
	writeJSON(w, 200, a.netbird.Peers())
}
