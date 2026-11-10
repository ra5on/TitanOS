package api

// PLAN 73: GET/PUT /api/v1/system/startup – when Docker apps and VMs start
// after boot (internal/startup). Owners and administrators only.

import (
	"encoding/json"
	"net/http"

	"github.com/ra5on/TitanOS/backend/internal/startup"
)

// startupArea is when apps and VMs start after boot (PLAN 73); an area with
// narrow dependencies (PLAN 8a, see fansArea). startup is nil where there is
// no such order.
type startupArea struct {
	startup *startup.Manager
}

func (a startupArea) register(mux *routeMux, admin func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("GET /api/v1/system/startup", admin(a.handleStartup))
	mux.HandleFunc("PUT /api/v1/system/startup", admin(a.handlePutStartup))
}

func (a startupArea) handleStartup(w http.ResponseWriter, r *http.Request) {
	if a.startup == nil {
		writeJSON(w, http.StatusOK, map[string]any{"settings": startup.Settings{Order: []startup.Entry{}}, "readySince": nil})
		return
	}
	var since any
	if t := a.startup.ReadySince(); !t.IsZero() {
		since = t
	}
	writeJSON(w, http.StatusOK, map[string]any{"settings": a.startup.Settings(), "readySince": since})
}

func (a startupArea) handlePutStartup(w http.ResponseWriter, r *http.Request) {
	if a.startup == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "not available")
		return
	}
	var in startup.Settings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid settings")
		return
	}
	if err := a.startup.Save(in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	noteTarget(r, "startup")
	writeJSON(w, http.StatusOK, a.startup.Settings())
}
