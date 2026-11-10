package api

import (
	"errors"
	"github.com/ra5on/TitanOS/backend/internal/plugins"
	"github.com/ra5on/TitanOS/backend/internal/tailscale"
	"log/slog"
	"net/http"
)

// tailscaleArea is the Tailscale plugin; an area with narrow dependencies
// (PLAN 8a, see fansArea). plugins runs changes as a runtime action of the
// plugin; nil runs them directly.
type tailscaleArea struct {
	tailscale *tailscale.Manager
	plugins   *plugins.Manager
}

func (a tailscaleArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/plugins/tailscale", admin(a.handleTailscale))
	mux.HandleFunc("GET /api/v1/plugins/tailscale/devices", admin(a.handleTailscaleDevices))
	mux.HandleFunc("PUT /api/v1/plugins/tailscale", admin(a.handleTailscaleSettings))
	mux.HandleFunc("PUT /api/v1/plugins/tailscale/https", admin(a.handleTailscaleHTTPS))
	mux.HandleFunc("POST /api/v1/plugins/tailscale/control/{action}", admin(a.handleTailscaleAction))
}

func (a tailscaleArea) handleTailscale(w http.ResponseWriter, r *http.Request) {
	if a.tailscale == nil {
		writeError(w, 503, "config_unavailable", "Tailscale settings unavailable")
		return
	}
	writeJSON(w, 200, a.tailscale.Status())
}
func writeTailscaleError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, tailscale.ErrInvalid) {
		status = 400
	}
	if errors.Is(err, tailscale.ErrUnavailable) || errors.Is(err, tailscale.ErrPending) || errors.Is(err, plugins.ErrDisabled) || errors.Is(err, plugins.ErrNotFound) || errors.Is(err, plugins.ErrUpdatePending) || errors.Is(err, plugins.ErrDriverPending) {
		status = 409
	}
	writeError(w, status, "tailscale_failed", err.Error())
}
func (a tailscaleArea) handleTailscaleSettings(w http.ResponseWriter, r *http.Request) {
	if a.tailscale == nil {
		writeError(w, 503, "config_unavailable", "Tailscale settings unavailable")
		return
	}
	var settings tailscale.Settings
	if !readJSON(w, r, &settings) {
		return
	}
	if err := a.tailscaleRuntimeAction(func() error { return a.tailscale.Set(settings) }); err != nil {
		writeTailscaleError(w, err)
		return
	}
	slog.Info("Tailscale configuration applied on trial", "by", currentUser(r).Name)
	a.handleTailscale(w, r)
}
func (a tailscaleArea) handleTailscaleAction(w http.ResponseWriter, r *http.Request) {
	if a.tailscale == nil {
		writeError(w, 503, "config_unavailable", "Tailscale settings unavailable")
		return
	}
	action := r.PathValue("action")
	var err error
	switch action {
	case "confirm":
		err = a.tailscale.Confirm()
	case "undo":
		err = a.tailscale.Undo()
	default:
		err = a.tailscaleRuntimeAction(func() error { return a.tailscale.Action(action) })
	}
	if err != nil {
		writeTailscaleError(w, err)
		return
	}
	slog.Info("Tailscale action", "action", action, "by", currentUser(r).Name)
	a.handleTailscale(w, r)
}

func (a tailscaleArea) tailscaleRuntimeAction(action func() error) error {
	if a.plugins != nil {
		return a.plugins.RuntimeAction("tailscale", action)
	}
	return action()
}

// handleTailscaleDevices lists the tailnet for the dashboard. Without an
// active plugin the answer is the state "inactive", not an error.
func (a tailscaleArea) handleTailscaleDevices(w http.ResponseWriter, r *http.Request) {
	if a.tailscale == nil {
		writeJSON(w, 200, tailscale.Devices{State: "inactive", Devices: []tailscale.Device{}})
		return
	}
	writeJSON(w, 200, a.tailscale.Devices())
}

// handleTailscaleHTTPS switches the Tailscale certificate of the NAS's own
// name on or off. Stored at once, without the trial of the other settings.
func (a tailscaleArea) handleTailscaleHTTPS(w http.ResponseWriter, r *http.Request) {
	if a.tailscale == nil {
		writeError(w, 503, "config_unavailable", "Tailscale settings unavailable")
		return
	}
	var req struct {
		Enabled bool `json:"enabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := a.tailscale.SetHTTPSCertificate(req.Enabled); err != nil {
		writeTailscaleError(w, err)
		return
	}
	slog.Info("Tailscale HTTPS certificate switched", "enabled", req.Enabled, "by", currentUser(r).Name)
	a.handleTailscale(w, r)
}
